package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/artpro/assessapp/internal/config"
	"github.com/artpro/assessapp/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// CashHandler handles cash management requests
type CashHandler struct {
	db     *gorm.DB
	cfg    *config.Config
	logger zerolog.Logger
}

// NewCashHandler creates a new cash handler
func NewCashHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *CashHandler {
	return &CashHandler{
		db:     db,
		cfg:    cfg,
		logger: logger,
	}
}

// CreateCashHoldingRequest represents the request to create a cash holding
type CreateCashHoldingRequest struct {
	CurrencyCode string  `json:"currency_code" binding:"required"`
	Amount       float64 `json:"amount" binding:"required,gte=0"`
	Description  string  `json:"description"`
}

// UpdateCashHoldingRequest represents the request to update a cash holding
type UpdateCashHoldingRequest struct {
	Amount      float64 `json:"amount" binding:"required,gte=0"`
	Description string  `json:"description"`
}

// GetAllCashHoldings returns all cash holdings with USD values calculated
func (h *CashHandler) GetAllCashHoldings(c *gin.Context) {
	var cashHoldings []models.CashHolding
	if err := h.db.Find(&cashHoldings).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch cash holdings")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cash holdings"})
		return
	}

	// Fetch all exchange rates once to avoid N+1 queries
	rateMap, err := h.getAllExchangeRates()
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch exchange rates")
		// Return holdings with existing USD values
		c.JSON(http.StatusOK, cashHoldings)
		return
	}

	// Update USD values using cached exchange rates (batch update to avoid N+1)
	if len(cashHoldings) > 0 {
		tx := h.db.Begin()
		for i := range cashHoldings {
			usdValue, err := h.calculateUSDValueWithCache(cashHoldings[i].CurrencyCode, cashHoldings[i].Amount, rateMap)
			if err != nil {
				h.logger.Warn().Err(err).Str("currency", cashHoldings[i].CurrencyCode).Msg("Failed to calculate USD value")
				continue
			}
			cashHoldings[i].USDValue = usdValue
			cashHoldings[i].LastUpdated = time.Now()

			if err := tx.Model(&cashHoldings[i]).Updates(map[string]interface{}{
				"usd_value":    usdValue,
				"last_updated": time.Now(),
			}).Error; err != nil {
				h.logger.Warn().Err(err).Uint("id", cashHoldings[i].ID).Msg("Failed to update cash holding")
			}
		}
		if err := tx.Commit().Error; err != nil {
			h.logger.Error().Err(err).Msg("Failed to commit cash holding updates")
		}
	}

	c.JSON(http.StatusOK, cashHoldings)
}

// CreateCashHolding creates a new cash holding
func (h *CashHandler) CreateCashHolding(c *gin.Context) {
	var req CreateCashHoldingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Check if currency exists in exchange rates
	var exchangeRate models.ExchangeRate
	if err := h.db.Where("currency_code = ?", req.CurrencyCode).First(&exchangeRate).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Currency not supported. Please add it to exchange rates first."})
		return
	}

	// Check if cash holding already exists for this currency
	var existingCash models.CashHolding
	if err := h.db.Where("currency_code = ?", req.CurrencyCode).First(&existingCash).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Cash holding already exists for this currency. Use update instead."})
		return
	}

	// Calculate USD value
	usdValue, err := h.calculateUSDValue(req.CurrencyCode, req.Amount)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to calculate USD value")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate USD value"})
		return
	}

	cashHolding := models.CashHolding{
		CurrencyCode: req.CurrencyCode,
		Amount:       req.Amount,
		USDValue:     usdValue,
		Description:  req.Description,
		LastUpdated:  time.Now(),
	}

	if err := h.db.Create(&cashHolding).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to create cash holding")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create cash holding"})
		return
	}

	h.logger.Info().Str("currency", req.CurrencyCode).Float64("amount", req.Amount).Msg("Cash holding created")
	c.JSON(http.StatusCreated, cashHolding)
}

// UpdateCashHolding updates an existing cash holding
func (h *CashHandler) UpdateCashHolding(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var req UpdateCashHoldingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var cashHolding models.CashHolding
	if err := h.db.First(&cashHolding, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Cash holding not found"})
			return
		}
		h.logger.Error().Err(err).Msg("Failed to fetch cash holding")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cash holding"})
		return
	}

	// Calculate new USD value
	usdValue, err := h.calculateUSDValue(cashHolding.CurrencyCode, req.Amount)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to calculate USD value")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate USD value"})
		return
	}

	cashHolding.Amount = req.Amount
	cashHolding.USDValue = usdValue
	cashHolding.Description = req.Description
	cashHolding.LastUpdated = time.Now()

	if err := h.db.Save(&cashHolding).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to update cash holding")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cash holding"})
		return
	}

	h.logger.Info().Uint("id", uint(id)).Str("currency", cashHolding.CurrencyCode).Float64("amount", req.Amount).Msg("Cash holding updated")
	c.JSON(http.StatusOK, cashHolding)
}

// DeleteCashHolding deletes a cash holding
func (h *CashHandler) DeleteCashHolding(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var cashHolding models.CashHolding
	if err := h.db.First(&cashHolding, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Cash holding not found"})
			return
		}
		h.logger.Error().Err(err).Msg("Failed to fetch cash holding")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cash holding"})
		return
	}

	if err := h.db.Delete(&cashHolding).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to delete cash holding")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete cash holding"})
		return
	}

	h.logger.Info().Uint("id", uint(id)).Str("currency", cashHolding.CurrencyCode).Msg("Cash holding deleted")
	c.JSON(http.StatusOK, gin.H{"message": "Cash holding deleted successfully"})
}

// RefreshUSDValues recalculates USD values for all cash holdings
func (h *CashHandler) RefreshUSDValues(c *gin.Context) {
	var cashHoldings []models.CashHolding
	if err := h.db.Find(&cashHoldings).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch cash holdings")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cash holdings"})
		return
	}

	// Fetch all exchange rates once to avoid N+1 queries
	rateMap, err := h.getAllExchangeRates()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch exchange rates")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch exchange rates"})
		return
	}

	// Batch update to avoid N+1
	updatedCount := 0
	tx := h.db.Begin()
	for i := range cashHoldings {
		usdValue, err := h.calculateUSDValueWithCache(cashHoldings[i].CurrencyCode, cashHoldings[i].Amount, rateMap)
		if err != nil {
			h.logger.Warn().Err(err).Str("currency", cashHoldings[i].CurrencyCode).Msg("Failed to calculate USD value")
			continue
		}

		cashHoldings[i].USDValue = usdValue
		cashHoldings[i].LastUpdated = time.Now()

		if err := tx.Model(&cashHoldings[i]).Updates(map[string]interface{}{
			"usd_value":    usdValue,
			"last_updated": time.Now(),
		}).Error; err != nil {
			h.logger.Warn().Err(err).Uint("id", cashHoldings[i].ID).Msg("Failed to update cash holding USD value")
			continue
		}
		updatedCount++
	}

	if err := tx.Commit().Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to commit USD value updates")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update USD values"})
		return
	}

	h.logger.Info().Int("updated_count", updatedCount).Msg("Cash holdings USD values refreshed")
	c.JSON(http.StatusOK, gin.H{
		"message": "USD values refreshed successfully",
		"updated": updatedCount,
		"total":   len(cashHoldings),
	})
}

// calculateUSDValue converts amount from given currency to USD
func (h *CashHandler) calculateUSDValue(currencyCode string, amount float64) (float64, error) {
	if currencyCode == "USD" {
		return amount, nil
	}

	// Get exchange rate for the currency (relative to EUR)
	var exchangeRate models.ExchangeRate
	if err := h.db.Where("currency_code = ?", currencyCode).First(&exchangeRate).Error; err != nil {
		return 0, err
	}

	// Get USD to EUR rate
	var usdRate models.ExchangeRate
	if err := h.db.Where("currency_code = ?", "USD").First(&usdRate).Error; err != nil {
		return 0, err
	}

	// Convert: amount in currency -> EUR -> USD
	// amount / exchangeRate.Rate = amount in EUR
	// amountInEUR * usdRate.Rate = amount in USD
	amountInEUR := amount / exchangeRate.Rate
	usdValue := amountInEUR * usdRate.Rate

	return usdValue, nil
}

// getAllExchangeRates fetches all exchange rates in a single query
func (h *CashHandler) getAllExchangeRates() (map[string]float64, error) {
	var rates []models.ExchangeRate
	if err := h.db.Find(&rates).Error; err != nil {
		return nil, err
	}

	rateMap := make(map[string]float64)
	for _, rate := range rates {
		rateMap[rate.CurrencyCode] = rate.Rate
	}
	return rateMap, nil
}

// calculateUSDValueWithCache converts amount from given currency to USD using cached rates
func (h *CashHandler) calculateUSDValueWithCache(currencyCode string, amount float64, rateMap map[string]float64) (float64, error) {
	if currencyCode == "USD" {
		return amount, nil
	}

	// Get exchange rate for the currency (relative to EUR)
	exchangeRate, ok := rateMap[currencyCode]
	if !ok {
		return 0, gorm.ErrRecordNotFound
	}

	// Get USD to EUR rate
	usdRate, ok := rateMap["USD"]
	if !ok {
		return 0, gorm.ErrRecordNotFound
	}

	// Convert: amount in currency -> EUR -> USD
	// amount / exchangeRate = amount in EUR
	// amountInEUR * usdRate = amount in USD
	amountInEUR := amount / exchangeRate
	usdValue := amountInEUR * usdRate

	return usdValue, nil
}