package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/models"
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

	// Update USD values using current exchange rates
	for i := range cashHoldings {
		usdValue, err := h.calculateUSDValue(cashHoldings[i].CurrencyCode, cashHoldings[i].Amount)
		if err != nil {
			h.logger.Warn().Err(err).Str("currency", cashHoldings[i].CurrencyCode).Msg("Failed to calculate USD value")
			// Keep existing USD value if calculation fails
		} else {
			cashHoldings[i].USDValue = usdValue
			cashHoldings[i].LastUpdated = time.Now()
			h.db.Save(&cashHoldings[i])
		}
	}

	// Cash holdings change infrequently - cache for 2 minutes
	c.Header("Cache-Control", "private, max-age=120, stale-while-revalidate=240")

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

	updatedCount := 0
	for i := range cashHoldings {
		usdValue, err := h.calculateUSDValue(cashHoldings[i].CurrencyCode, cashHoldings[i].Amount)
		if err != nil {
			h.logger.Warn().Err(err).Str("currency", cashHoldings[i].CurrencyCode).Msg("Failed to calculate USD value")
			continue
		}

		cashHoldings[i].USDValue = usdValue
		cashHoldings[i].LastUpdated = time.Now()

		if err := h.db.Save(&cashHoldings[i]).Error; err != nil {
			h.logger.Warn().Err(err).Uint("id", cashHoldings[i].ID).Msg("Failed to update cash holding USD value")
			continue
		}
		updatedCount++
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
	// If EUR (base currency), convert to USD using USD rate
	if currencyCode == "EUR" {
		var usdRate models.ExchangeRate
		if err := h.db.Where("currency_code = ?", "USD").First(&usdRate).Error; err != nil {
			// If USD rate not found, assume 1:1 for development
			h.logger.Warn().Msg("USD exchange rate not found, using 1:1")
			return amount, nil
		}
		// EUR to USD: multiply by USD rate
		return amount * usdRate.Rate, nil
	}

	// If USD, return as is
	if currencyCode == "USD" {
		return amount, nil
	}

	// For other currencies, convert via EUR base
	var exchangeRate models.ExchangeRate
	if err := h.db.Where("currency_code = ?", currencyCode).First(&exchangeRate).Error; err != nil {
		h.logger.Warn().Str("currency", currencyCode).Msg("Exchange rate not found")
		return amount, nil // Return original amount if no rate found
	}

	// Convert to EUR first (amount / rate), then to USD
	var usdRate models.ExchangeRate
	if err := h.db.Where("currency_code = ?", "USD").First(&usdRate).Error; err != nil {
		// If USD rate not found, return EUR equivalent
		h.logger.Warn().Msg("USD exchange rate not found, returning EUR equivalent")
		return amount / exchangeRate.Rate, nil
	}

	// Convert: amount in currency -> EUR -> USD
	amountInEUR := amount / exchangeRate.Rate
	usdValue := amountInEUR * usdRate.Rate

	return usdValue, nil
}
