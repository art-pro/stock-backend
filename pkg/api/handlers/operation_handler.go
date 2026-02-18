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

		// Cash impact: Buy/Withdraw decrease; Sell/Deposit/Dividend increase
		var cashDelta float64
		switch req.OperationType {
		case "Buy", "Withdraw":
			cashDelta = -amount
		case "Sell", "Deposit", "Dividend":
			cashDelta = amount
		default:
			cashDelta = 0
		}
		if cashDelta != 0 {
			if err := h.cashHandler.AdjustCash(tx, portfolioID, req.Currency, cashDelta); err != nil {
				return err
			}
		}

		// For Buy: add to stock position (create stock if new, or update shares_owned and avg price)
		// For Sell: reduce stock position
		if req.OperationType == "Buy" || req.OperationType == "Sell" {
			if req.Ticker == "" {
				// No ticker: skip stock update (e.g. Deposit/Withdraw already handled cash)
				return nil
			}
			var stock models.Stock
			errStock := tx.Where("portfolio_id = ? AND ticker = ?", portfolioID, req.Ticker).First(&stock).Error
			if req.OperationType == "Buy" {
				if errStock != nil {
					// Create new stock with shares and avg price (user can refresh from Grok/Alpha Vantage later)
					companyName := req.CompanyName
					if companyName == "" {
						companyName = req.Ticker
					}
					stock = models.Stock{
						PortfolioID:         portfolioID,
						Ticker:              req.Ticker,
						ISIN:                req.ISIN,
						CompanyName:         companyName,
						Sector:              req.Sector,
						Currency:            req.Currency,
						CurrentPrice:        req.Price,
						SharesOwned:         int(req.Quantity),
						AvgPriceLocal:       req.Price,
						UpdateFrequency:     "daily",
						ProbabilityPositive: 0.65,
					}
					if err := tx.Create(&stock).Error; err != nil {
						return err
					}
					op.StockID = &stock.ID
					tx.Save(&op)
				} else {
					// Add to existing: new total shares, new volume-weighted avg price
					newShares := stock.SharesOwned + int(req.Quantity)
					totalCost := float64(stock.SharesOwned)*stock.AvgPriceLocal + req.Quantity*req.Price
					stock.SharesOwned = newShares
					if newShares > 0 {
						stock.AvgPriceLocal = totalCost / float64(newShares)
					}
					stock.LastUpdated = time.Now()
					if err := tx.Save(&stock).Error; err != nil {
						return err
					}
					op.StockID = &stock.ID
					tx.Save(&op)
				}
			} else {
				// Sell
				if errStock != nil {
					return nil // stock not found; operation still recorded
				}
				sellQty := int(req.Quantity)
				if sellQty > stock.SharesOwned {
					sellQty = stock.SharesOwned
				}
				stock.SharesOwned -= sellQty
				stock.LastUpdated = time.Now()
				if err := tx.Save(&stock).Error; err != nil {
					return err
				}
				op.StockID = &stock.ID
				tx.Save(&op)
			}
		}
		return nil
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
