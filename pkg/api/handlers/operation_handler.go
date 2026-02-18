package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/art-pro/stock-backend/pkg/database"
	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// OperationHandler handles operation (trade) creation and listing
type OperationHandler struct {
	db          *gorm.DB
	cashHandler *CashHandler
	logger      zerolog.Logger
}

// NewOperationHandler creates a new operation handler
func NewOperationHandler(db *gorm.DB, cashHandler *CashHandler, logger zerolog.Logger) *OperationHandler {
	return &OperationHandler{db: db, cashHandler: cashHandler, logger: logger}
}

func (h *OperationHandler) resolvePortfolioID(c *gin.Context) (uint, error) {
	if portfolioIDParam := c.Query("portfolio_id"); portfolioIDParam != "" {
		parsed, err := strconv.ParseUint(portfolioIDParam, 10, 32)
		if err != nil {
			return 0, err
		}
		return uint(parsed), nil
	}
	return database.GetDefaultPortfolioID(h.db)
}

// CreateOperationRequest represents the request to create an operation
type CreateOperationRequest struct {
	OperationType string  `json:"operation_type" binding:"required"` // Buy, Sell, Deposit, Withdraw, Dividend
	Ticker        string  `json:"ticker"`
	ISIN          string  `json:"isin"`
	CompanyName   string  `json:"company_name"`
	Sector        string  `json:"sector"`
	Currency      string  `json:"currency" binding:"required"`
	Quantity      float64 `json:"quantity" binding:"required,gte=0"`
	Price         float64 `json:"price" binding:"gte=0"`
	Amount        float64 `json:"amount"`   // Optional; if 0 for Buy/Sell computed as Quantity*Price
	Note          string  `json:"note"`
	TradeDate     string  `json:"trade_date" binding:"required"` // DD.MM.YYYY
	StockID       *uint   `json:"stock_id,omitempty"`            // Optional; for Buy/Sell link to existing stock
}

// CreateOperation creates a new operation, persists it, updates cash and optionally stock position
func (h *OperationHandler) CreateOperation(c *gin.Context) {
	var req CreateOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	validTypes := map[string]bool{"Buy": true, "Sell": true, "Deposit": true, "Withdraw": true, "Dividend": true}
	if !validTypes[req.OperationType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operation_type must be one of: Buy, Sell, Deposit, Withdraw, Dividend"})
		return
	}

	amount := req.Amount
	if amount == 0 && (req.OperationType == "Buy" || req.OperationType == "Sell") {
		amount = req.Quantity * req.Price
	}
	if req.OperationType == "Deposit" || req.OperationType == "Withdraw" || req.OperationType == "Dividend" {
		if amount == 0 {
			amount = req.Quantity // use quantity as cash amount for these
		}
	}

	op := models.Operation{
		PortfolioID:   portfolioID,
		StockID:       req.StockID,
		OperationType: req.OperationType,
		Ticker:        req.Ticker,
		ISIN:          req.ISIN,
		CompanyName:   req.CompanyName,
		Sector:        req.Sector,
		Currency:      req.Currency,
		Quantity:      req.Quantity,
		Price:         req.Price,
		Amount:        amount,
		Note:          req.Note,
		TradeDate:     req.TradeDate,
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&op).Error; err != nil {
			return err
		}
		return h.applyOperationEffects(tx, portfolioID, &op)
	}); err != nil {
		h.logger.Error().Err(err).Msg("Failed to create operation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create operation"})
		return
	}

	c.JSON(http.StatusCreated, op)
}

// ListOperations returns operations for the portfolio (history), newest first
func (h *OperationHandler) ListOperations(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var operations []models.Operation
	if err := h.db.Where("portfolio_id = ?", portfolioID).Order("trade_date DESC, created_at DESC").Find(&operations).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to list operations")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list operations"})
		return
	}

	c.JSON(http.StatusOK, operations)
}

// reverseOperationEffects undoes the cash and stock impact of an operation (for delete or before update).
func (h *OperationHandler) reverseOperationEffects(tx *gorm.DB, op *models.Operation) error {
	portfolioID := op.PortfolioID
	amount := op.Amount
	if amount == 0 && (op.OperationType == "Buy" || op.OperationType == "Sell") {
		amount = op.Quantity * op.Price
	}
	if (op.OperationType == "Deposit" || op.OperationType == "Withdraw" || op.OperationType == "Dividend") && amount == 0 {
		amount = op.Quantity
	}

	// Reverse cash: opposite of original delta
	var reverseCashDelta float64
	switch op.OperationType {
	case "Buy", "Withdraw":
		reverseCashDelta = amount // was -amount
	case "Sell", "Deposit", "Dividend":
		reverseCashDelta = -amount // was +amount
	default:
		reverseCashDelta = 0
	}
	if reverseCashDelta != 0 {
		if err := h.cashHandler.AdjustCash(tx, portfolioID, op.Currency, reverseCashDelta); err != nil {
			return err
		}
	}

	// Reverse stock for Buy/Sell
	if op.OperationType == "Buy" || op.OperationType == "Sell" {
		if op.Ticker == "" {
			return nil
		}
		var stock models.Stock
		errStock := tx.Where("portfolio_id = ? AND ticker = ?", portfolioID, op.Ticker).First(&stock).Error
		if errStock != nil {
			return nil // stock already gone or never created
		}
		qty := int(op.Quantity)
		if op.OperationType == "Buy" {
			// We had added shares; subtract them (or delete stock if goes to 0)
			newShares := stock.SharesOwned - qty
			if newShares <= 0 {
				if err := tx.Delete(&stock).Error; err != nil {
					return err
				}
				return nil
			}
			oldTotal := float64(stock.SharesOwned) * stock.AvgPriceLocal
			newTotal := oldTotal - op.Quantity*op.Price
			stock.SharesOwned = newShares
			if newShares > 0 {
				stock.AvgPriceLocal = newTotal / float64(newShares)
			}
			stock.LastUpdated = time.Now()
			return tx.Save(&stock).Error
		}
		// Sell reversal: add shares back
		newShares := stock.SharesOwned + qty
		totalCost := float64(stock.SharesOwned)*stock.AvgPriceLocal + op.Quantity*op.Price
		stock.SharesOwned = newShares
		if newShares > 0 {
			stock.AvgPriceLocal = totalCost / float64(newShares)
		}
		stock.LastUpdated = time.Now()
		return tx.Save(&stock).Error
	}
	return nil
}

// applyOperationEffects applies the cash and stock impact of an operation (after create or update).
func (h *OperationHandler) applyOperationEffects(tx *gorm.DB, portfolioID uint, op *models.Operation) error {
	amount := op.Amount
	if amount == 0 && (op.OperationType == "Buy" || op.OperationType == "Sell") {
		amount = op.Quantity * op.Price
	}
	if (op.OperationType == "Deposit" || op.OperationType == "Withdraw" || op.OperationType == "Dividend") && amount == 0 {
		amount = op.Quantity
	}
	op.Amount = amount

	var cashDelta float64
	switch op.OperationType {
	case "Buy", "Withdraw":
		cashDelta = -amount
	case "Sell", "Deposit", "Dividend":
		cashDelta = amount
	default:
		cashDelta = 0
	}
	if cashDelta != 0 {
		if err := h.cashHandler.AdjustCash(tx, portfolioID, op.Currency, cashDelta); err != nil {
			return err
		}
	}

	if op.OperationType == "Buy" || op.OperationType == "Sell" {
		if op.Ticker == "" {
			return nil
		}
		var stock models.Stock
		errStock := tx.Where("portfolio_id = ? AND ticker = ?", portfolioID, op.Ticker).First(&stock).Error
		if op.OperationType == "Buy" {
			if errStock != nil {
				companyName := op.CompanyName
				if companyName == "" {
					companyName = op.Ticker
				}
				stock = models.Stock{
					PortfolioID:         portfolioID,
					Ticker:              op.Ticker,
					ISIN:                op.ISIN,
					CompanyName:         companyName,
					Sector:              op.Sector,
					Currency:            op.Currency,
					CurrentPrice:        op.Price,
					SharesOwned:         int(op.Quantity),
					AvgPriceLocal:       op.Price,
					UpdateFrequency:     "daily",
					ProbabilityPositive: 0.65,
				}
				if err := tx.Create(&stock).Error; err != nil {
					return err
				}
				op.StockID = &stock.ID
				return tx.Save(op).Error
			}
			newShares := stock.SharesOwned + int(op.Quantity)
			totalCost := float64(stock.SharesOwned)*stock.AvgPriceLocal + op.Quantity*op.Price
			stock.SharesOwned = newShares
			if newShares > 0 {
				stock.AvgPriceLocal = totalCost / float64(newShares)
			}
			stock.LastUpdated = time.Now()
			if err := tx.Save(&stock).Error; err != nil {
				return err
			}
			op.StockID = &stock.ID
			return tx.Save(op).Error
		}
		// Sell
		if errStock != nil {
			return nil
		}
		sellQty := int(op.Quantity)
		if sellQty > stock.SharesOwned {
			sellQty = stock.SharesOwned
		}
		stock.SharesOwned -= sellQty
		stock.LastUpdated = time.Now()
		if err := tx.Save(&stock).Error; err != nil {
			return err
		}
		op.StockID = &stock.ID
		return tx.Save(op).Error
	}
	return nil
}

// DeleteOperation deletes an operation and reverses its cash and stock effects.
func (h *OperationHandler) DeleteOperation(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid operation id"})
		return
	}
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var op models.Operation
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&op).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Operation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load operation"})
		return
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := h.reverseOperationEffects(tx, &op); err != nil {
			return err
		}
		return tx.Delete(&op).Error
	}); err != nil {
		h.logger.Error().Err(err).Msg("Failed to delete operation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete operation"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// UpdateOperation updates an operation: reverses old effects, updates record, applies new effects.
func (h *OperationHandler) UpdateOperation(c *gin.Context) {
	idParam := c.Param("id")
	id, err := strconv.ParseUint(idParam, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid operation id"})
		return
	}
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var req CreateOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}
	validTypes := map[string]bool{"Buy": true, "Sell": true, "Deposit": true, "Withdraw": true, "Dividend": true}
	if !validTypes[req.OperationType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operation_type must be one of: Buy, Sell, Deposit, Withdraw, Dividend"})
		return
	}

	var existing models.Operation
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&existing).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Operation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load operation"})
		return
	}

	amount := req.Amount
	if amount == 0 && (req.OperationType == "Buy" || req.OperationType == "Sell") {
		amount = req.Quantity * req.Price
	}
	if req.OperationType == "Deposit" || req.OperationType == "Withdraw" || req.OperationType == "Dividend" {
		if amount == 0 {
			amount = req.Quantity
		}
	}

	updated := models.Operation{
		ID:           existing.ID,
		PortfolioID:  existing.PortfolioID,
		StockID:      req.StockID,
		OperationType: req.OperationType,
		Ticker:       req.Ticker,
		ISIN:         req.ISIN,
		CompanyName:  req.CompanyName,
		Sector:       req.Sector,
		Currency:     req.Currency,
		Quantity:     req.Quantity,
		Price:        req.Price,
		Amount:       amount,
		Note:         req.Note,
		TradeDate:    req.TradeDate,
		CreatedAt:    existing.CreatedAt,
		UpdatedAt:    time.Now(),
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		// Reverse the existing operation's effects (use a copy so we don't mutate existing)
		toReverse := existing
		if err := h.reverseOperationEffects(tx, &toReverse); err != nil {
			return err
		}
		if err := tx.Save(&updated).Error; err != nil {
			return err
		}
		return h.applyOperationEffects(tx, portfolioID, &updated)
	}); err != nil {
		h.logger.Error().Err(err).Msg("Failed to update operation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update operation"})
		return
	}

	if err := h.db.Where("id = ?", existing.ID).First(&updated).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reload operation"})
		return
	}
	c.JSON(http.StatusOK, updated)
}
