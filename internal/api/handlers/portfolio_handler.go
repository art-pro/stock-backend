package handlers

import (
	"net/http"
	"time"

	"github.com/art-pro/stock-backend/internal/config"
	"github.com/art-pro/stock-backend/internal/models"
	"github.com/art-pro/stock-backend/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// PortfolioHandler handles portfolio-related requests
type PortfolioHandler struct {
	db         *gorm.DB
	cfg        *config.Config
	logger     zerolog.Logger
	apiService *services.ExternalAPIService
}

// NewPortfolioHandler creates a new portfolio handler
func NewPortfolioHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *PortfolioHandler {
	return &PortfolioHandler{
		db:         db,
		cfg:        cfg,
		logger:     logger,
		apiService: services.NewExternalAPIService(cfg),
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

	// Get unique currencies
	currencyMap := make(map[string]bool)
	for _, stock := range stocks {
		currencyMap[stock.Currency] = true
	}

	var currencies []string
	for currency := range currencyMap {
		currencies = append(currencies, currency)
	}

	// Fetch exchange rates
	fxRates, err := h.apiService.FetchAllExchangeRates(currencies)
	if err != nil {
		h.logger.Warn().Err(err).Msg("Failed to fetch exchange rates")
		// Continue with default rates
		fxRates = make(map[string]float64)
		fxRates["USD"] = 1.0
	}

	// Calculate portfolio metrics
	metrics := services.CalculatePortfolioMetrics(stocks, fxRates)

	// Update weights for each stock (batch update to avoid N+1)
	if metrics.TotalValue > 0 && len(stocks) > 0 {
		for i := range stocks {
			fxRate := fxRates[stocks[i].Currency]
			if fxRate == 0 {
				fxRate = 1.0
			}
			valueUSD := float64(stocks[i].SharesOwned) * stocks[i].CurrentPrice * fxRate
			stocks[i].Weight = (valueUSD / metrics.TotalValue) * 100
		}

		// Batch update all stocks at once
		tx := h.db.Begin()
		for i := range stocks {
			if err := tx.Model(&stocks[i]).Update("weight", stocks[i].Weight).Error; err != nil {
				tx.Rollback()
				h.logger.Error().Err(err).Msg("Failed to update stock weights")
				break
			}
		}
		if err := tx.Commit().Error; err != nil {
			h.logger.Error().Err(err).Msg("Failed to commit stock weight updates")
		}
	}

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
		"timestamp": time.Now(),
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
