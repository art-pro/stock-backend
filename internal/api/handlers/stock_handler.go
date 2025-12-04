package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/art-pro/stock-backend/pkg/services"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// StockHandler handles stock-related requests
type StockHandler struct {
	db         *gorm.DB
	cfg        *config.Config
	logger     zerolog.Logger
	apiService *services.ExternalAPIService
}

// NewStockHandler creates a new stock handler
func NewStockHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *StockHandler {
	return &StockHandler{
		db:         db,
		cfg:        cfg,
		logger:     logger,
		apiService: services.NewExternalAPIService(cfg),
	}
}

// GetAllStocks returns all stocks
func (h *StockHandler) GetAllStocks(c *gin.Context) {
	var stocks []models.Stock
	if err := h.db.Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	c.JSON(http.StatusOK, stocks)
}

// GetStock returns a single stock
func (h *StockHandler) GetStock(c *gin.Context) {
	id := c.Param("id")

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		} else {
			h.logger.Error().Err(err).Msg("Failed to fetch stock")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stock"})
		}
		return
	}

	c.JSON(http.StatusOK, stock)
}

// CreateStockRequest represents request to create a stock
type CreateStockRequest struct {
	Ticker              string  `json:"ticker" binding:"required"`
	CompanyName         string  `json:"company_name" binding:"required"`
	Sector              string  `json:"sector" binding:"required"`
	Currency            string  `json:"currency"`
	SharesOwned         int     `json:"shares_owned"`
	AvgPriceLocal       float64 `json:"avg_price_local"`
	UpdateFrequency     string  `json:"update_frequency"`
	ProbabilityPositive float64 `json:"probability_positive"` // Optional manual input
}

// CreateStock creates a new stock and triggers initial calculations
func (h *StockHandler) CreateStock(c *gin.Context) {
	var req CreateStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Check if stock already exists
	var existing models.Stock
	if err := h.db.Where("ticker = ?", req.Ticker).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Stock with this ticker already exists"})
		return
	}

	stock := models.Stock{
		Ticker:              req.Ticker,
		CompanyName:         req.CompanyName,
		Sector:              req.Sector,
		Currency:            req.Currency,
		SharesOwned:         req.SharesOwned,
		AvgPriceLocal:       req.AvgPriceLocal,
		UpdateFrequency:     req.UpdateFrequency,
		ProbabilityPositive: req.ProbabilityPositive,
	}

	if stock.Currency == "" {
		stock.Currency = "USD"
	}
	if stock.UpdateFrequency == "" {
		stock.UpdateFrequency = "daily"
	}
	if stock.ProbabilityPositive == 0 {
		stock.ProbabilityPositive = 0.65 // Default conservative value
	}

	// Fetch all stock data from Grok in one call (includes ALL calculations!)
	// With automatic fallback to mock data that also includes calculations
	if err := h.apiService.FetchAllStockData(&stock); err != nil {
		h.logger.Error().Err(err).Str("ticker", stock.Ticker).Msg("⚠️ GROK FETCH FAILED during stock creation - Check API key and logs above")
		// Return error to prevent saving stock with N/A data
		c.JSON(http.StatusBadGateway, gin.H{
			"error":  "Failed to fetch stock data from Grok API. Please check your XAI_API_KEY configuration.",
			"ticker": stock.Ticker,
		})
		return
	}

	h.logger.Info().Str("ticker", stock.Ticker).Msg("✓ Successfully fetched data from Grok")

	// NO NEED to call CalculateMetrics - Grok already calculated everything!
	// The following fields are now provided by Grok:
	// - UpsidePotential, BRatio, ExpectedValue
	// - KellyFraction, HalfKellySuggested
	// - BuyZoneMin, BuyZoneMax, Assessment

	// Get FX rate for USD conversion
	fxRate, err := h.apiService.FetchExchangeRate(stock.Currency)
	if err != nil {
		h.logger.Warn().Err(err).Str("currency", stock.Currency).Msg("Failed to fetch FX rate")
		fxRate = 1.0
	}

	// Calculate USD values
	stock.CurrentValueUSD = float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
	costBasis := float64(stock.SharesOwned) * stock.AvgPriceLocal * fxRate
	stock.UnrealizedPnL = stock.CurrentValueUSD - costBasis

	stock.LastUpdated = time.Now()

	// Save to database
	if err := h.db.Create(&stock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to create stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create stock"})
		return
	}

	// Create initial history entry
	history := models.StockHistory{
		StockID:             stock.ID,
		Ticker:              stock.Ticker,
		CurrentPrice:        stock.CurrentPrice,
		FairValue:           stock.FairValue,
		UpsidePotential:     stock.UpsidePotential,
		DownsideRisk:        stock.DownsideRisk,
		ProbabilityPositive: stock.ProbabilityPositive,
		ExpectedValue:       stock.ExpectedValue,
		KellyFraction:       stock.KellyFraction,
		Weight:              stock.Weight,
		Assessment:          stock.Assessment,
		RecordedAt:          time.Now(),
	}
	h.db.Create(&history)

	h.logger.Info().Str("ticker", stock.Ticker).Msg("Stock created successfully")

	c.JSON(http.StatusCreated, stock)
}

// UpdateStock updates an existing stock
func (h *StockHandler) UpdateStock(c *gin.Context) {
	id := c.Param("id")

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Update allowed fields
	if err := h.db.Model(&stock).Updates(req).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to update stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update stock"})
		return
	}

	// Recalculate metrics
	services.CalculateMetrics(&stock)
	h.db.Save(&stock)

	h.logger.Info().Str("ticker", stock.Ticker).Msg("Stock updated successfully")

	c.JSON(http.StatusOK, stock)
}

// UpdateStockPrice updates just the current price and recalculates metrics
func (h *StockHandler) UpdateStockPrice(c *gin.Context) {
	id := c.Param("id")

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	var req struct {
		CurrentPrice float64 `json:"current_price" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid price value"})
		return
	}

	// Update price
	stock.CurrentPrice = req.CurrentPrice
	stock.LastUpdated = time.Now()

	// Recalculate all derived metrics based on new price
	services.CalculateMetrics(&stock)

	// Get FX rate for USD conversion
	fxRate, err := h.apiService.FetchExchangeRate(stock.Currency)
	if err != nil {
		h.logger.Warn().Err(err).Str("currency", stock.Currency).Msg("Failed to fetch FX rate")
		fxRate = 1.0
	}

	// Recalculate USD values
	stock.CurrentValueUSD = float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
	costBasis := float64(stock.SharesOwned) * stock.AvgPriceLocal * fxRate
	stock.UnrealizedPnL = stock.CurrentValueUSD - costBasis

	// Save to database
	if err := h.db.Save(&stock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to save stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save stock"})
		return
	}

	h.logger.Info().Str("ticker", stock.Ticker).Float64("new_price", req.CurrentPrice).Msg("Stock price manually updated")

	c.JSON(http.StatusOK, stock)
}

// UpdateStockField updates a single field (avg_price_local, fair_value, shares_owned) and recalculates metrics
func (h *StockHandler) UpdateStockField(c *gin.Context) {
	id := c.Param("id")

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	var req struct {
		Field string  `json:"field" binding:"required"`
		Value float64 `json:"value" binding:"required,gte=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Update the specified field
	switch req.Field {
	case "avg_price_local":
		stock.AvgPriceLocal = req.Value
	case "fair_value":
		stock.FairValue = req.Value
	case "shares_owned":
		stock.SharesOwned = int(req.Value)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid field name"})
		return
	}

	stock.LastUpdated = time.Now()

	// Recalculate all derived metrics
	services.CalculateMetrics(&stock)

	// Get FX rate for USD conversion
	fxRate, err := h.apiService.FetchExchangeRate(stock.Currency)
	if err != nil {
		h.logger.Warn().Err(err).Str("currency", stock.Currency).Msg("Failed to fetch FX rate")
		fxRate = 1.0
	}

	// Recalculate USD values
	stock.CurrentValueUSD = float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
	costBasis := float64(stock.SharesOwned) * stock.AvgPriceLocal * fxRate
	stock.UnrealizedPnL = stock.CurrentValueUSD - costBasis

	// Save to database
	if err := h.db.Save(&stock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to save stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save stock"})
		return
	}

	h.logger.Info().Str("ticker", stock.Ticker).Str("field", req.Field).Float64("new_value", req.Value).Msg("Stock field manually updated")

	c.JSON(http.StatusOK, stock)
}

// DeleteStock soft-deletes a stock (moves to log)
func (h *StockHandler) DeleteStock(c *gin.Context) {
	id := c.Param("id")
	username, _ := c.Get("username")

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	// Get reason from query params
	reason := c.Query("reason")

	// Serialize stock data
	stockData, err := json.Marshal(stock)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to serialize stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	// Create deleted stock entry
	deletedStock := models.DeletedStock{
		PortfolioID: stock.PortfolioID,
		StockData:   string(stockData),
		Ticker:      stock.Ticker,
		CompanyName: stock.CompanyName,
		Reason:      reason,
		DeletedAt:   time.Now(),
		DeletedBy:   username.(string),
	}

	if err := h.db.Create(&deletedStock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to create deleted stock log")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	// Delete the stock
	if err := h.db.Delete(&stock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to delete stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	h.logger.Info().Str("ticker", stock.Ticker).Msg("Stock deleted successfully")

	c.JSON(http.StatusOK, gin.H{"message": "Stock deleted successfully"})
}

// UpdateAllStocks updates prices and calculations for all stocks
func (h *StockHandler) UpdateAllStocks(c *gin.Context) {
	var stocks []models.Stock
	if err := h.db.Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	updatedCount := 0
	errorCount := 0

	for i := range stocks {
		if err := h.updateStockData(&stocks[i]); err != nil {
			h.logger.Warn().Err(err).Str("ticker", stocks[i].Ticker).Msg("Failed to update stock")
			errorCount++
		} else {
			updatedCount++
		}
	}

	h.logger.Info().Int("updated", updatedCount).Int("errors", errorCount).Msg("Bulk stock update completed")

	c.JSON(http.StatusOK, gin.H{
		"message": "Update completed",
		"updated": updatedCount,
		"errors":  errorCount,
		"total":   len(stocks),
	})
}

// UpdateSingleStock updates a single stock's data
func (h *StockHandler) UpdateSingleStock(c *gin.Context) {
	id := c.Param("id")

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	if err := h.updateStockData(&stock); err != nil {
		h.logger.Warn().Err(err).Str("ticker", stock.Ticker).Msg("Failed to update stock data from API, using mock data")
		// Don't return error - the updateStockData should have fallback to mock data
		// Try to at least recalculate metrics with existing data
		services.CalculateMetrics(&stock)
		h.db.Save(&stock)
	}

	c.JSON(http.StatusOK, stock)
}

// updateStockData is a helper function to update stock data from external APIs
func (h *StockHandler) updateStockData(stock *models.Stock) error {
	// Store old EV for alert comparison
	oldEV := stock.ExpectedValue

	// Fetch all stock data from Grok in one call (includes ALL calculations!)
	if err := h.apiService.FetchAllStockData(stock); err != nil {
		h.logger.Error().Err(err).Str("ticker", stock.Ticker).Msg("⚠️ GROK FETCH FAILED - Check API key and logs above")
		// Mock data is already set by the service including all calculations
		return err
	}

	h.logger.Info().Str("ticker", stock.Ticker).Msg("✓ Successfully fetched data from Grok")

	// NO NEED to call CalculateMetrics - Grok already calculated everything!

	// Get FX rate for USD conversion
	fxRate, err := h.apiService.FetchExchangeRate(stock.Currency)
	if err != nil {
		h.logger.Warn().Err(err).Str("currency", stock.Currency).Msg("Failed to fetch FX rate, using default")
		fxRate = 1.0
	}

	// Calculate USD values
	stock.CurrentValueUSD = float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
	costBasis := float64(stock.SharesOwned) * stock.AvgPriceLocal * fxRate
	stock.UnrealizedPnL = stock.CurrentValueUSD - costBasis

	stock.LastUpdated = time.Now()

	// Save to database
	if err := h.db.Save(stock).Error; err != nil {
		h.logger.Error().Err(err).Str("ticker", stock.Ticker).Msg("Failed to save stock to database")
		return err
	}

	// Create history entry
	history := models.StockHistory{
		StockID:             stock.ID,
		Ticker:              stock.Ticker,
		CurrentPrice:        stock.CurrentPrice,
		FairValue:           stock.FairValue,
		UpsidePotential:     stock.UpsidePotential,
		DownsideRisk:        stock.DownsideRisk,
		ProbabilityPositive: stock.ProbabilityPositive,
		ExpectedValue:       stock.ExpectedValue,
		KellyFraction:       stock.KellyFraction,
		Weight:              stock.Weight,
		Assessment:          stock.Assessment,
		RecordedAt:          time.Now(),
	}
	h.db.Create(&history)

	// Check for alerts (EV change > threshold)
	evChange := stock.ExpectedValue - oldEV
	if evChange > 10.0 || evChange < -10.0 {
		alert := models.Alert{
			StockID:   stock.ID,
			Ticker:    stock.Ticker,
			AlertType: "ev_change",
			Message:   "EV changed by " + strconv.FormatFloat(evChange, 'f', 2, 64) + "%",
			EmailSent: false,
			CreatedAt: time.Now(),
		}
		h.db.Create(&alert)
	}

	return nil
}

// GetStockHistory returns historical data for a stock
func (h *StockHandler) GetStockHistory(c *gin.Context) {
	id := c.Param("id")

	var history []models.StockHistory
	if err := h.db.Where("stock_id = ?", id).Order("recorded_at DESC").Limit(100).Find(&history).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stock history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch history"})
		return
	}

	c.JSON(http.StatusOK, history)
}

// GetDeletedStocks returns all deleted stocks
func (h *StockHandler) GetDeletedStocks(c *gin.Context) {
	var deletedStocks []models.DeletedStock
	if err := h.db.Where("restored_at IS NULL").Order("deleted_at DESC").Find(&deletedStocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch deleted stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch deleted stocks"})
		return
	}

	c.JSON(http.StatusOK, deletedStocks)
}

// RestoreStock restores a deleted stock
func (h *StockHandler) RestoreStock(c *gin.Context) {
	id := c.Param("id")

	var deletedStock models.DeletedStock
	if err := h.db.First(&deletedStock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Deleted stock not found"})
		return
	}

	// Deserialize stock data
	var stock models.Stock
	if err := json.Unmarshal([]byte(deletedStock.StockData), &stock); err != nil {
		h.logger.Error().Err(err).Msg("Failed to deserialize stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore stock"})
		return
	}

	// Reset ID to create a new record
	stock.ID = 0
	stock.CreatedAt = time.Time{}
	stock.UpdatedAt = time.Time{}

	// Create restored stock
	if err := h.db.Create(&stock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to restore stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore stock"})
		return
	}

	// Mark as restored
	now := time.Now()
	deletedStock.RestoredAt = &now
	h.db.Save(&deletedStock)

	h.logger.Info().Str("ticker", stock.Ticker).Msg("Stock restored successfully")

	c.JSON(http.StatusOK, stock)
}

// ExportJSON exports all stocks to JSON matching the template format
func (h *StockHandler) ExportJSON(c *gin.Context) {
	var stocks []models.Stock
	if err := h.db.Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	type ExportStock struct {
		Ticker              string  `json:"ticker"`
		CompanyName         string  `json:"company_name"`
		ISIN                string  `json:"isin"`
		Sector              string  `json:"sector"`
		CurrentPrice        float64 `json:"current_price"`
		Currency            string  `json:"currency"`
		FairValue           float64 `json:"fair_value"`
		UpsidePotential     float64 `json:"upside_potential"`
		DownsideRisk        float64 `json:"downside_risk"`
		ProbabilityPositive float64 `json:"probability_positive"`
		ExpectedValue       float64 `json:"expected_value"`
		Beta                float64 `json:"beta"`
		Volatility          float64 `json:"volatility"`
		PERatio             float64 `json:"pe_ratio"`
		EPSGrowthRate       float64 `json:"eps_growth_rate"`
		DebtToEBITDA        float64 `json:"debt_to_ebitda"`
		DividendYield       float64 `json:"dividend_yield"`
		BRatio              float64 `json:"b_ratio"`
		KellyFraction       float64 `json:"kelly_fraction"`
		HalfKellySuggested  float64 `json:"half_kelly_suggested"`
		SharesOwned         int     `json:"shares_owned"`
		AvgPriceLocal       float64 `json:"avg_price_local"`
		BuyZoneMin          float64 `json:"buy_zone_min"`
		BuyZoneMax          float64 `json:"buy_zone_max"`
		Assessment          string  `json:"assessment"`
		UpdateFrequency     string  `json:"update_frequency"`
		DataSource          string  `json:"data_source"`
		FairValueSource     string  `json:"fair_value_source"`
		Comment             string  `json:"comment"`
	}

	exportData := make([]ExportStock, 0, len(stocks))

	for _, stock := range stocks {
		exportData = append(exportData, ExportStock{
			Ticker:              stock.Ticker,
			CompanyName:         stock.CompanyName,
			ISIN:                "", // Not currently stored
			Sector:              stock.Sector,
			CurrentPrice:        stock.CurrentPrice,
			Currency:            stock.Currency,
			FairValue:           stock.FairValue,
			UpsidePotential:     stock.UpsidePotential,
			DownsideRisk:        stock.DownsideRisk,
			ProbabilityPositive: stock.ProbabilityPositive,
			ExpectedValue:       stock.ExpectedValue,
			Beta:                stock.Beta,
			Volatility:          stock.Volatility,
			PERatio:             stock.PERatio,
			EPSGrowthRate:       stock.EPSGrowthRate,
			DebtToEBITDA:        stock.DebtToEBITDA,
			DividendYield:       stock.DividendYield,
			BRatio:              stock.BRatio,
			KellyFraction:       stock.KellyFraction,
			HalfKellySuggested:  stock.HalfKellySuggested,
			SharesOwned:         stock.SharesOwned,
			AvgPriceLocal:       stock.AvgPriceLocal,
			BuyZoneMin:          stock.BuyZoneMin,
			BuyZoneMax:          stock.BuyZoneMax,
			Assessment:          stock.Assessment,
			UpdateFrequency:     stock.UpdateFrequency,
			DataSource:          "Manual", // Default as per template
			FairValueSource:     "",       // Not currently stored
			Comment:             "",       // Not currently stored
		})
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment;filename=portfolio_export.json")
	c.JSON(http.StatusOK, exportData)
}
