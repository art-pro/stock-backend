package handlers

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/models"
	"github.com/artpro/assessapp/pkg/services"
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
		Field       string      `json:"field" binding:"required"`
		Value       interface{} `json:"value"`
		StringValue string      `json:"string_value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Update the specified field
	fieldUpdated := false
	switch req.Field {
	// Numeric fields
	case "current_price":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.CurrentPrice = floatVal
			fieldUpdated = true
		}
	case "avg_price_local":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.AvgPriceLocal = floatVal
			fieldUpdated = true
		}
	case "fair_value":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.FairValue = floatVal
			fieldUpdated = true
		}
	case "shares_owned":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.SharesOwned = int(floatVal)
			fieldUpdated = true
		}
	case "beta":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.Beta = floatVal
			fieldUpdated = true
		}
	case "volatility":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.Volatility = floatVal
			fieldUpdated = true
		}
	case "probability_positive":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 && floatVal <= 1 {
			stock.ProbabilityPositive = floatVal
			fieldUpdated = true
		}
	case "downside_risk":
		if floatVal, ok := req.Value.(float64); ok && floatVal <= 0 {
			stock.DownsideRisk = floatVal
			fieldUpdated = true
		}
	case "pe_ratio":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.PERatio = floatVal
			fieldUpdated = true
		}
	case "eps_growth_rate":
		if floatVal, ok := req.Value.(float64); ok {
			stock.EPSGrowthRate = floatVal
			fieldUpdated = true
		}
	case "debt_to_ebitda":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.DebtToEBITDA = floatVal
			fieldUpdated = true
		}
	case "dividend_yield":
		if floatVal, ok := req.Value.(float64); ok && floatVal >= 0 {
			stock.DividendYield = floatVal
			fieldUpdated = true
		}
	// String fields
	case "comment":
		if req.StringValue != "" {
			stock.Comment = req.StringValue
			fieldUpdated = true
		} else if strVal, ok := req.Value.(string); ok {
			stock.Comment = strVal
			fieldUpdated = true
		}
	case "company_name":
		if req.StringValue != "" {
			stock.CompanyName = req.StringValue
			fieldUpdated = true
		} else if strVal, ok := req.Value.(string); ok && strVal != "" {
			stock.CompanyName = strVal
			fieldUpdated = true
		}
	case "sector":
		if req.StringValue != "" {
			stock.Sector = req.StringValue
			fieldUpdated = true
		} else if strVal, ok := req.Value.(string); ok && strVal != "" {
			stock.Sector = strVal
			fieldUpdated = true
		}
	case "update_frequency":
		if req.StringValue != "" {
			stock.UpdateFrequency = req.StringValue
			fieldUpdated = true
		} else if strVal, ok := req.Value.(string); ok && strVal != "" {
			stock.UpdateFrequency = strVal
			fieldUpdated = true
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid field name"})
		return
	}

	if !fieldUpdated {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to update field - invalid value"})
		return
	}

	stock.LastUpdated = time.Now()

	// Recalculate all derived metrics (only if numeric fields changed)
	if req.Field != "comment" && req.Field != "company_name" && req.Field != "sector" && req.Field != "update_frequency" {
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
	}

	// Save to database
	if err := h.db.Save(&stock).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to save stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save stock"})
		return
	}

	h.logger.Info().Str("ticker", stock.Ticker).Str("field", req.Field).Msg("Stock field manually updated")

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
	source := c.Query("source") // Optional: "grok", "alphavantage", or "" for auto

	var stock models.Stock
	if err := h.db.First(&stock, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	if err := h.updateStockDataWithSource(&stock, source); err != nil {
		h.logger.Warn().Err(err).Str("ticker", stock.Ticker).Msg("Failed to update stock data from API, using mock data")
		// Don't return error - the updateStockData should have fallback to mock data
		// Try to at least recalculate metrics with existing data
		services.CalculateMetrics(&stock)
		h.db.Save(&stock)
	}

	c.JSON(http.StatusOK, stock)
}

// updateStockData is a helper function to update stock data from external APIs (auto-mode)
func (h *StockHandler) updateStockData(stock *models.Stock) error {
	return h.updateStockDataWithSource(stock, "")
}

// updateStockDataWithSource updates stock data from specified source
func (h *StockHandler) updateStockDataWithSource(stock *models.Stock, source string) error {
	// Store old EV for alert comparison
	oldEV := stock.ExpectedValue

	var err error
	switch source {
	case "grok":
		// Fetch only from Grok (interpretive/analytical data)
		err = h.apiService.FetchFromGrok(stock)
	case "alphavantage":
		// Fetch only from Alpha Vantage (raw financial data)
		err = h.apiService.FetchFromAlphaVantage(stock)
	default:
		// Auto mode: try Alpha Vantage first, then Grok
		err = h.apiService.FetchAllStockData(stock)
	}

	if err != nil {
		h.logger.Error().Err(err).Str("ticker", stock.Ticker).Str("source", source).Msg("⚠️ API FETCH FAILED - Check API key and logs above")
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

// ExportCSV exports all stocks to CSV
func (h *StockHandler) ExportCSV(c *gin.Context) {
	var stocks []models.Stock
	if err := h.db.Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment;filename=stocks.csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write header
	header := []string{
		"Ticker", "Company Name", "Sector", "Current Price", "Currency", "Fair Value",
		"Upside Potential (%)", "Downside Risk (%)", "Probability Positive", "Expected Value (%)",
		"Beta", "Volatility (%)", "P/E Ratio", "EPS Growth Rate (%)", "Debt to EBITDA",
		"Dividend Yield (%)", "b Ratio", "Kelly f* (%)", "½-Kelly Suggested (%)",
		"Shares Owned", "Avg Price", "Current Value USD", "Weight (%)", "Unrealized P&L",
		"Buy Zone Min", "Buy Zone Max", "Assessment",
	}
	writer.Write(header)

	// Write data
	for _, stock := range stocks {
		row := []string{
			stock.Ticker, stock.CompanyName, stock.Sector,
			strconv.FormatFloat(stock.CurrentPrice, 'f', 2, 64), stock.Currency,
			strconv.FormatFloat(stock.FairValue, 'f', 2, 64),
			strconv.FormatFloat(stock.UpsidePotential, 'f', 2, 64),
			strconv.FormatFloat(stock.DownsideRisk, 'f', 2, 64),
			strconv.FormatFloat(stock.ProbabilityPositive, 'f', 2, 64),
			strconv.FormatFloat(stock.ExpectedValue, 'f', 2, 64),
			strconv.FormatFloat(stock.Beta, 'f', 2, 64),
			strconv.FormatFloat(stock.Volatility, 'f', 2, 64),
			strconv.FormatFloat(stock.PERatio, 'f', 2, 64),
			strconv.FormatFloat(stock.EPSGrowthRate, 'f', 2, 64),
			strconv.FormatFloat(stock.DebtToEBITDA, 'f', 2, 64),
			strconv.FormatFloat(stock.DividendYield, 'f', 2, 64),
			strconv.FormatFloat(stock.BRatio, 'f', 2, 64),
			strconv.FormatFloat(stock.KellyFraction, 'f', 2, 64),
			strconv.FormatFloat(stock.HalfKellySuggested, 'f', 2, 64),
			strconv.Itoa(stock.SharesOwned),
			strconv.FormatFloat(stock.AvgPriceLocal, 'f', 2, 64),
			strconv.FormatFloat(stock.CurrentValueUSD, 'f', 2, 64),
			strconv.FormatFloat(stock.Weight, 'f', 2, 64),
			strconv.FormatFloat(stock.UnrealizedPnL, 'f', 2, 64),
			strconv.FormatFloat(stock.BuyZoneMin, 'f', 2, 64),
			strconv.FormatFloat(stock.BuyZoneMax, 'f', 2, 64),
			stock.Assessment,
		}
		writer.Write(row)
	}
}

// ImportCSV imports stocks from CSV
func (h *StockHandler) ImportCSV(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse CSV"})
		return
	}

	if len(records) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CSV file is empty"})
		return
	}

	imported := 0
	skipped := 0

	// Skip header row
	for _, record := range records[1:] {
		if len(record) < 3 {
			skipped++
			continue
		}

		// Check if stock already exists
		var existing models.Stock
		if err := h.db.Where("ticker = ?", record[0]).First(&existing).Error; err == nil {
			skipped++
			continue
		}

		// Create basic stock entry
		stock := models.Stock{
			Ticker:      record[0],
			CompanyName: record[1],
			Sector:      record[2],
		}

		if err := h.db.Create(&stock).Error; err != nil {
			skipped++
			continue
		}

		imported++
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Import completed",
		"imported": imported,
		"skipped":  skipped,
	})
}
