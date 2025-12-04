package handlers

import (
	"net/http"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/art-pro/stock-backend/pkg/services"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// PortfolioHandler handles portfolio-related requests
type PortfolioHandler struct {
	db                  *gorm.DB
	cfg                 *config.Config
	logger              zerolog.Logger
	apiService          *services.ExternalAPIService
	exchangeRateService *services.ExchangeRateService
}

// NewPortfolioHandler creates a new portfolio handler
func NewPortfolioHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *PortfolioHandler {
	return &PortfolioHandler{
		db:                  db,
		cfg:                 cfg,
		logger:              logger,
		apiService:          services.NewExternalAPIService(cfg),
		exchangeRateService: services.NewExchangeRateService(db, logger),
	}
}

// GetPortfolioSummary returns aggregated portfolio metrics
func (h *PortfolioHandler) GetPortfolioSummary(c *gin.Context) {
	var stocks []models.Stock
	if err := h.db.Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	// Fetch exchange rates from our exchange rate service
	fxRates, err := h.exchangeRateService.GetRatesMap()
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch exchange rates from database")
		// Use fallback rates
		fxRates = make(map[string]float64)
		fxRates["EUR"] = 1.0
		fxRates["USD"] = 1.154
		fxRates["DKK"] = 7.4604
		fxRates["GBP"] = 0.8796
		fxRates["RUB"] = 93.7594
	}

	// Calculate portfolio metrics
	metrics := services.CalculatePortfolioMetrics(stocks, fxRates)

	// Update weights for each stock
	for i := range stocks {
		if stocks[i].SharesOwned <= 0 {
			continue
		}

		fxRate := fxRates[stocks[i].Currency]
		if fxRate == 0 {
			fxRate = 1.0
		}
		// Convert to EUR (base currency)
		valueEUR := float64(stocks[i].SharesOwned) * stocks[i].CurrentPrice / fxRate
		if metrics.TotalValue > 0 {
			stocks[i].Weight = (valueEUR / metrics.TotalValue) * 100
			stocks[i].CurrentValueUSD = valueEUR * fxRates["USD"] // Store in USD for backward compatibility
			h.db.Save(&stocks[i])
		}
	}

	// Add caching headers - cache for 30 seconds
	c.Header("Cache-Control", "private, max-age=30, stale-while-revalidate=60")

	c.JSON(http.StatusOK, gin.H{
		"summary": metrics,
		"stocks":  stocks,
	})
}

// GetSettings returns portfolio settings
func (h *PortfolioHandler) GetSettings(c *gin.Context) {
	var settings models.PortfolioSettings
	if err := h.db.First(&settings).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create default settings
			settings = models.PortfolioSettings{
				UpdateFrequency:  "daily",
				AlertsEnabled:    true,
				AlertThresholdEV: 10.0,
			}
			h.db.Create(&settings)
		} else {
			h.logger.Error().Err(err).Msg("Failed to fetch settings")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
			return
		}
	}

	c.JSON(http.StatusOK, settings)
}

// GetAPIStatus returns the status of external API connections
func (h *PortfolioHandler) GetAPIStatus(c *gin.Context) {
	status := gin.H{
		"grok": gin.H{
			"configured": h.cfg.XAIAPIKey != "",
			"status":     "unknown",
		},
		"alpha_vantage": gin.H{
			"configured": h.cfg.AlphaVantageAPIKey != "",
			"status":     "unknown",
		},
		"timestamp": time.Now(),
	}

	// Test Alpha Vantage connection if configured
	if h.cfg.AlphaVantageAPIKey != "" {
		// Try a simple quote fetch
		_, err := h.apiService.FetchAlphaVantageQuote("AAPL")
		if err != nil {
			status["alpha_vantage"].(gin.H)["status"] = "error"
			status["alpha_vantage"].(gin.H)["error"] = err.Error()
		} else {
			status["alpha_vantage"].(gin.H)["status"] = "connected"
		}
	} else {
		status["alpha_vantage"].(gin.H)["status"] = "not_configured"
		status["alpha_vantage"].(gin.H)["message"] = "Add ALPHA_VANTAGE_API_KEY to .env for real-time financial data"
	}

	// Test Grok connection if configured
	if h.cfg.XAIAPIKey != "" {
		// Create a minimal test stock
		testStock := models.Stock{
			Ticker:      "TEST",
			CompanyName: "Test Company",
			Sector:      "Technology",
			Currency:    "USD",
		}

		// Try to fetch data
		err := h.apiService.FetchAllStockData(&testStock)
		if err != nil {
			status["grok"].(gin.H)["status"] = "error"
			status["grok"].(gin.H)["error"] = err.Error()
		} else {
			status["grok"].(gin.H)["status"] = "connected"
			status["grok"].(gin.H)["using_mock"] = false
		}
	} else {
		status["grok"].(gin.H)["status"] = "not_configured"
		status["grok"].(gin.H)["using_mock"] = true
		status["grok"].(gin.H)["message"] = "Using mock data. Add XAI_API_KEY to .env for real data"
	}

	c.JSON(http.StatusOK, status)
}

// UpdateSettings updates portfolio settings
func (h *PortfolioHandler) UpdateSettings(c *gin.Context) {
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var settings models.PortfolioSettings
	if err := h.db.First(&settings).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			settings = models.PortfolioSettings{}
			h.db.Create(&settings)
		} else {
			h.logger.Error().Err(err).Msg("Failed to fetch settings")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
			return
		}
	}

	if err := h.db.Model(&settings).Updates(req).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to update settings")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

// GetAlerts returns all alerts
func (h *PortfolioHandler) GetAlerts(c *gin.Context) {
	var alerts []models.Alert
	if err := h.db.Order("created_at DESC").Limit(100).Find(&alerts).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch alerts")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch alerts"})
		return
	}

	c.JSON(http.StatusOK, alerts)
}

// DeleteAlert deletes an alert
func (h *PortfolioHandler) DeleteAlert(c *gin.Context) {
	id := c.Param("id")

	if err := h.db.Delete(&models.Alert{}, id).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to delete alert")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete alert"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Alert deleted successfully"})
}
