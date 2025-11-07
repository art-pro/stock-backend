package handlers

import (
	"net/http"

	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/services"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ExchangeRateHandler handles exchange rate-related requests
type ExchangeRateHandler struct {
	db      *gorm.DB
	cfg     *config.Config
	logger  zerolog.Logger
	service *services.ExchangeRateService
}

// NewExchangeRateHandler creates a new exchange rate handler
func NewExchangeRateHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *ExchangeRateHandler {
	return &ExchangeRateHandler{
		db:      db,
		cfg:     cfg,
		logger:  logger,
		service: services.NewExchangeRateService(db, logger),
	}
}

// GetAllRates returns all exchange rates
func (h *ExchangeRateHandler) GetAllRates(c *gin.Context) {
	rates, err := h.service.GetAllRates()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch exchange rates")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch exchange rates"})
		return
	}
	
	c.JSON(http.StatusOK, rates)
}

// RefreshRates fetches latest rates from the API
func (h *ExchangeRateHandler) RefreshRates(c *gin.Context) {
	if err := h.service.FetchLatestRates(); err != nil {
		h.logger.Error().Err(err).Msg("Failed to refresh exchange rates")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	// Return updated rates
	rates, err := h.service.GetAllRates()
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch exchange rates after refresh")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated rates"})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Exchange rates refreshed successfully",
		"rates":   rates,
	})
}

// AddCurrencyRequest represents a request to add a new currency
type AddCurrencyRequest struct {
	CurrencyCode string  `json:"currency_code" binding:"required"`
	Rate         float64 `json:"rate" binding:"required"`
	IsManual     bool    `json:"is_manual"`
}

// AddCurrency adds a new currency to track
func (h *ExchangeRateHandler) AddCurrency(c *gin.Context) {
	var req AddCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	if err := h.service.AddCurrency(req.CurrencyCode, req.Rate, req.IsManual); err != nil {
		h.logger.Error().Err(err).Msg("Failed to add currency")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add currency"})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Currency added successfully"})
}

// UpdateRateRequest represents a request to update an exchange rate
type UpdateRateRequest struct {
	Rate     float64 `json:"rate" binding:"required"`
	IsManual bool    `json:"is_manual"`
}

// UpdateRate updates an exchange rate
func (h *ExchangeRateHandler) UpdateRate(c *gin.Context) {
	currencyCode := c.Param("code")
	
	var req UpdateRateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	if err := h.service.UpdateRate(currencyCode, req.Rate, req.IsManual); err != nil {
		h.logger.Error().Err(err).Str("currency", currencyCode).Msg("Failed to update rate")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rate"})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Rate updated successfully"})
}

// DeleteCurrency soft deletes a currency
func (h *ExchangeRateHandler) DeleteCurrency(c *gin.Context) {
	currencyCode := c.Param("code")
	
	if err := h.service.DeleteCurrency(currencyCode); err != nil {
		h.logger.Error().Err(err).Str("currency", currencyCode).Msg("Failed to delete currency")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Currency deleted successfully"})
}