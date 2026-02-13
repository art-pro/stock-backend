package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/database"
	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/art-pro/stock-backend/pkg/services"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// StockHandler handles stock-related requests
type StockHandler struct {
	db                  *gorm.DB
	cfg                 *config.Config
	logger              zerolog.Logger
	apiService          *services.ExternalAPIService
	fairValueCollector  *services.FairValueCollector
	exchangeRateService *services.ExchangeRateService
}

// NewStockHandler creates a new stock handler
func NewStockHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *StockHandler {
	return &StockHandler{
		db:                  db,
		cfg:                 cfg,
		logger:              logger,
		apiService:          services.NewExternalAPIService(cfg),
		fairValueCollector:  services.NewFairValueCollector(cfg),
		exchangeRateService: services.NewExchangeRateService(db, logger),
	}
}

func (h *StockHandler) updateStockUSDValues(stock *models.Stock) error {
	amountLocal := float64(stock.SharesOwned) * stock.CurrentPrice
	costLocal := float64(stock.SharesOwned) * stock.AvgPriceLocal

	valueEUR, err := h.exchangeRateService.ConvertToEUR(amountLocal, stock.Currency)
	if err != nil {
		return err
	}
	costEUR, err := h.exchangeRateService.ConvertToEUR(costLocal, stock.Currency)
	if err != nil {
		return err
	}
	usdRate, err := h.exchangeRateService.GetRate("USD")
	if err != nil {
		return err
	}
	if usdRate <= 0 {
		return fmt.Errorf("invalid USD exchange rate")
	}

	stock.CurrentValueUSD = valueEUR * usdRate
	stock.UnrealizedPnL = (valueEUR - costEUR) * usdRate
	return nil
}

func (h *StockHandler) resolvePortfolioID(c *gin.Context) (uint, error) {
	if portfolioIDParam := c.Query("portfolio_id"); portfolioIDParam != "" {
		parsed, err := strconv.ParseUint(portfolioIDParam, 10, 32)
		if err != nil {
			return 0, err
		}
		return uint(parsed), nil
	}
	return database.GetDefaultPortfolioID(h.db)
}

// GetAllStocks returns all stocks
func (h *StockHandler) GetAllStocks(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stocks []models.Stock
	if err := h.db.Where("portfolio_id = ?", portfolioID).Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	// Add caching headers - cache for 30 seconds
	c.Header("Cache-Control", "private, max-age=30, stale-while-revalidate=60")
	c.Header("ETag", strconv.FormatInt(time.Now().Unix()/30, 10)) // ETag changes every 30 seconds

	c.JSON(http.StatusOK, stocks)
}

// GetStock returns a single stock
func (h *StockHandler) GetStock(c *gin.Context) {
	id := c.Param("id")
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
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
	ISIN                string  `json:"isin"` // International Securities Identification Number (optional)
	CompanyName         string  `json:"company_name" binding:"required"`
	Sector              string  `json:"sector" binding:"required"`
	Currency            string  `json:"currency"`
	SharesOwned         int     `json:"shares_owned"`
	AvgPriceLocal       float64 `json:"avg_price_local"`
	UpdateFrequency     string  `json:"update_frequency"`
	ProbabilityPositive float64 `json:"probability_positive"` // Optional manual input
	PortfolioID         uint    `json:"portfolio_id"`
}

// CreateStock creates a new stock and triggers initial calculations
func (h *StockHandler) CreateStock(c *gin.Context) {
	var req CreateStockRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	stock := models.Stock{
		Ticker:              req.Ticker,
		ISIN:                req.ISIN,
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

	if req.PortfolioID != 0 {
		stock.PortfolioID = req.PortfolioID
	} else {
		portfolioID, err := database.GetDefaultPortfolioID(h.db)
		if err != nil {
			h.logger.Error().Err(err).Msg("Failed to resolve default portfolio")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "No default portfolio found"})
			return
		}
		stock.PortfolioID = portfolioID
	}

	// Check if stock already exists in this portfolio.
	var existing models.Stock
	if err := h.db.Where("ticker = ? AND portfolio_id = ?", req.Ticker, stock.PortfolioID).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Stock with this ticker already exists in the selected portfolio"})
		return
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

	if err := h.updateStockUSDValues(&stock); err != nil {
		h.logger.Error().Err(err).Str("currency", stock.Currency).Msg("Failed to convert stock values using exchange rates")
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to convert stock values using exchange rates"})
		return
	}

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
		PortfolioID:         stock.PortfolioID,
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
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
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
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
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

	if err := h.updateStockUSDValues(&stock); err != nil {
		h.logger.Error().Err(err).Str("currency", stock.Currency).Msg("Failed to convert stock values using exchange rates")
		c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to convert stock values using exchange rates"})
		return
	}

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
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
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
	case "isin":
		if req.StringValue != "" {
			stock.ISIN = req.StringValue
			fieldUpdated = true
		} else if strVal, ok := req.Value.(string); ok {
			stock.ISIN = strVal
			fieldUpdated = true
		}
	case "fair_value_source":
		if req.StringValue != "" {
			stock.FairValueSource = req.StringValue
			fieldUpdated = true
		} else if strVal, ok := req.Value.(string); ok {
			stock.FairValueSource = strVal
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
	if req.Field != "comment" && req.Field != "company_name" && req.Field != "sector" && req.Field != "update_frequency" && req.Field != "isin" && req.Field != "fair_value_source" {
		services.CalculateMetrics(&stock)

		if err := h.updateStockUSDValues(&stock); err != nil {
			h.logger.Error().Err(err).Str("currency", stock.Currency).Msg("Failed to convert stock values using exchange rates")
			c.JSON(http.StatusBadGateway, gin.H{"error": "Failed to convert stock values using exchange rates"})
			return
		}
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
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
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

	tx := h.db.Begin()
	if tx.Error != nil {
		h.logger.Error().Err(tx.Error).Msg("Failed to start transaction for stock deletion")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	if err := tx.Create(&deletedStock).Error; err != nil {
		tx.Rollback()
		h.logger.Error().Err(err).Msg("Failed to create deleted stock log")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	if err := tx.Delete(&stock).Error; err != nil {
		tx.Rollback()
		h.logger.Error().Err(err).Msg("Failed to delete stock")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	if err := tx.Commit().Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to commit stock deletion transaction")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete stock"})
		return
	}

	h.logger.Info().Str("ticker", stock.Ticker).Msg("Stock deleted successfully")

	c.JSON(http.StatusOK, gin.H{"message": "Stock deleted successfully"})
}

// UpdateAllStocks updates prices and calculations for all stocks
func (h *StockHandler) UpdateAllStocks(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stocks []models.Stock
	if err := h.db.Where("portfolio_id = ?", portfolioID).Find(&stocks).Error; err != nil {
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

type CollectFairValuesRequest struct {
	IDs []uint `json:"ids" binding:"required,min=1"`
}

// CollectFairValues fetches fair values from trusted sources (via Grok + Deepseek) for selected stocks.
func (h *StockHandler) CollectFairValues(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var req CollectFairValuesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	var stocks []models.Stock
	if err := h.db.Where("id IN ? AND portfolio_id = ?", req.IDs, portfolioID).Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch selected stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch selected stocks"})
		return
	}

	updated := 0
	errors := []string{}
	totalSources := 0

	for i := range stocks {
		select {
		case <-c.Request.Context().Done():
			c.JSON(499, gin.H{"error": "Request cancelled by client"})
			return
		default:
		}

		stock := &stocks[i]
		entries, collectErr := h.fairValueCollector.CollectTrustedFairValues(c.Request.Context(), stock)
		if collectErr != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", stock.Ticker, collectErr))
			continue
		}

		values := make([]float64, 0, len(entries))
		for _, entry := range entries {
			values = append(values, entry.FairValue)
		}
		if len(values) == 0 {
			errors = append(errors, fmt.Sprintf("%s: no trusted fair value entries returned", stock.Ticker))
			continue
		}

		txErr := func() error {
			tx := h.db.Begin()
			if tx.Error != nil {
				return fmt.Errorf("failed to start transaction")
			}

			for _, entry := range entries {
				history := models.FairValueHistory{
					StockID:     stock.ID,
					PortfolioID: stock.PortfolioID,
					Ticker:      stock.Ticker,
					FairValue:   entry.FairValue,
					Source:      entry.Source,
					RecordedAt:  entry.RecordedAt,
				}
				if err := tx.Create(&history).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to save fair value history")
				}
			}

			stock.FairValue = services.Median(values)
			stock.FairValueSource = fmt.Sprintf("Trusted multi-source consensus (%d entries), %s", len(entries), time.Now().Format("2006-01-02"))
			stock.LastUpdated = time.Now()

			services.CalculateMetrics(stock)
			if err := h.updateStockUSDValues(stock); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to convert stock values")
			}

			if err := tx.Save(stock).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update stock")
			}

			snapshot := models.StockHistory{
				StockID:             stock.ID,
				PortfolioID:         stock.PortfolioID,
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
			if err := tx.Create(&snapshot).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to save stock history snapshot")
			}

			if err := tx.Commit().Error; err != nil {
				return fmt.Errorf("failed to commit transaction")
			}
			return nil
		}()

		if txErr != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", stock.Ticker, txErr))
			continue
		}

		totalSources += len(entries)
		updated++
	}

	c.JSON(http.StatusOK, gin.H{
		"message":               "Fair value collection completed",
		"updated":               updated,
		"errors":                len(errors),
		"error_details":         errors,
		"total_requested":       len(req.IDs),
		"entries_saved":         totalSources,
		"trusted_entries_saved": totalSources,
	})
}

// UpdateSingleStock updates a single stock's data
func (h *StockHandler) UpdateSingleStock(c *gin.Context) {
	id := c.Param("id")
	source := c.Query("source") // Optional: "grok", "alphavantage", or "" for auto
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
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
	now := time.Now()
	switch source {
	case "grok":
		// Fetch only from Grok (interpretive/analytical data)
		err = h.apiService.FetchFromGrok(stock)
		if err == nil {
			stock.GrokFetchedAt = &now
		}
	case "alphavantage":
		// Fetch only from Alpha Vantage (raw financial data)
		err = h.apiService.FetchFromAlphaVantage(stock)
		if err == nil {
			stock.AlphaVantageFetchedAt = &now
		}
	default:
		// Auto mode: try Alpha Vantage first, then Grok
		err = h.apiService.FetchAllStockData(stock)
		if err == nil {
			// In auto mode, both sources might have been updated
			stock.AlphaVantageFetchedAt = &now
			stock.GrokFetchedAt = &now
		}
	}

	if err != nil {
		h.logger.Error().Err(err).Str("ticker", stock.Ticker).Str("source", source).Msg("⚠️ API FETCH FAILED - Check API key and logs above")
		// Mock data is already set by the service including all calculations
		return err
	}

	h.logger.Info().Str("ticker", stock.Ticker).Msg("✓ Successfully fetched data from Grok")

	// NO NEED to call CalculateMetrics - Grok already calculated everything!

	if err := h.updateStockUSDValues(stock); err != nil {
		h.logger.Error().Err(err).Str("currency", stock.Currency).Msg("Failed to convert stock values using exchange rates")
		return err
	}

	stock.LastUpdated = time.Now()

	// Save to database
	if err := h.db.Save(stock).Error; err != nil {
		h.logger.Error().Err(err).Str("ticker", stock.Ticker).Msg("Failed to save stock to database")
		return err
	}

	// Create history entry
	history := models.StockHistory{
		StockID:             stock.ID,
		PortfolioID:         stock.PortfolioID,
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
			PortfolioID: stock.PortfolioID,
			StockID:     stock.ID,
			Ticker:      stock.Ticker,
			AlertType:   "ev_change",
			Message:     "EV changed by " + strconv.FormatFloat(evChange, 'f', 2, 64) + "%",
			EmailSent:   false,
			CreatedAt:   time.Now(),
		}
		h.db.Create(&alert)
	}

	return nil
}

// GetStockHistory returns historical data for a stock
func (h *StockHandler) GetStockHistory(c *gin.Context) {
	id := c.Param("id")
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var history []models.StockHistory
	if err := h.db.Where("stock_id = ? AND portfolio_id = ?", id, portfolioID).Order("recorded_at DESC").Limit(100).Find(&history).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stock history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch history"})
		return
	}

	c.JSON(http.StatusOK, history)
}

// GetFairValueHistory returns source-level fair value history for a stock.
func (h *StockHandler) GetFairValueHistory(c *gin.Context) {
	id := c.Param("id")
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stock models.Stock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&stock).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stock not found"})
		return
	}

	var history []models.FairValueHistory
	if err := h.db.Where("stock_id = ? AND portfolio_id = ?", id, portfolioID).
		Order("recorded_at DESC").
		Limit(300).
		Find(&history).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch fair value history")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch fair value history"})
		return
	}

	c.JSON(http.StatusOK, history)
}

// GetDeletedStocks returns all deleted stocks
func (h *StockHandler) GetDeletedStocks(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var deletedStocks []models.DeletedStock
	if err := h.db.Where("restored_at IS NULL AND portfolio_id = ?", portfolioID).Order("deleted_at DESC").Find(&deletedStocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch deleted stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch deleted stocks"})
		return
	}

	c.JSON(http.StatusOK, deletedStocks)
}

// RestoreStock restores a deleted stock
func (h *StockHandler) RestoreStock(c *gin.Context) {
	id := c.Param("id")
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var deletedStock models.DeletedStock
	if err := h.db.Where("id = ? AND portfolio_id = ?", id, portfolioID).First(&deletedStock).Error; err != nil {
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
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var stocks []models.Stock
	if err := h.db.Where("portfolio_id = ?", portfolioID).Find(&stocks).Error; err != nil {
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
		BuyZoneStatus       string  `json:"buy_zone_status"`
		SellZoneLowerBound  float64 `json:"sell_zone_lower_bound"`
		SellZoneUpperBound  float64 `json:"sell_zone_upper_bound"`
		SellZoneStatus      string  `json:"sell_zone_status"`
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
			ISIN:                stock.ISIN,
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
			BuyZoneStatus:       stock.BuyZoneStatus,
			SellZoneLowerBound:  stock.SellZoneLowerBound,
			SellZoneUpperBound:  stock.SellZoneUpperBound,
			SellZoneStatus:      stock.SellZoneStatus,
			Assessment:          stock.Assessment,
			UpdateFrequency:     stock.UpdateFrequency,
			DataSource:          "Manual", // Default as per template
			FairValueSource:     "",       // Not currently stored
			Comment:             stock.Comment,
		})
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment;filename=portfolio_export.json")
	c.JSON(http.StatusOK, exportData)
}

// BulkUpdateRequest represents the request for bulk stock updates
type BulkUpdateRequest struct {
	Stocks []BulkStockData `json:"stocks" binding:"required"`
}

// BulkStockData represents stock data for bulk update
type BulkStockData struct {
	Ticker              string  `json:"ticker" binding:"required"`
	CompanyName         string  `json:"company_name" binding:"required"`
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
	BuyZoneStatus       string  `json:"buy_zone_status"`
	SellZoneLowerBound  float64 `json:"sell_zone_lower_bound"`
	SellZoneUpperBound  float64 `json:"sell_zone_upper_bound"`
	SellZoneStatus      string  `json:"sell_zone_status"`
	Assessment          string  `json:"assessment"`
	UpdateFrequency     string  `json:"update_frequency"`
	DataSource          string  `json:"data_source"`
	FairValueSource     string  `json:"fair_value_source"`
	Comment             string  `json:"comment"`
}

// BulkUpdateStocks handles bulk stock updates from JSON
func (h *StockHandler) BulkUpdateStocks(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	var req BulkUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error().Err(err).Msg("Invalid JSON request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
		return
	}

	updated := 0
	created := 0
	errors := []string{}

	for _, stockData := range req.Stocks {
		var existing models.Stock
		err := h.db.Where("ticker = ? AND portfolio_id = ?", stockData.Ticker, portfolioID).First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// Create new stock
			stock := models.Stock{
				Ticker:              stockData.Ticker,
				CompanyName:         stockData.CompanyName,
				ISIN:                stockData.ISIN,
				Sector:              stockData.Sector,
				CurrentPrice:        stockData.CurrentPrice,
				Currency:            stockData.Currency,
				FairValue:           stockData.FairValue,
				UpsidePotential:     stockData.UpsidePotential,
				DownsideRisk:        stockData.DownsideRisk,
				ProbabilityPositive: stockData.ProbabilityPositive,
				ExpectedValue:       stockData.ExpectedValue,
				Beta:                stockData.Beta,
				Volatility:          stockData.Volatility,
				PERatio:             stockData.PERatio,
				EPSGrowthRate:       stockData.EPSGrowthRate,
				DebtToEBITDA:        stockData.DebtToEBITDA,
				DividendYield:       stockData.DividendYield,
				BRatio:              stockData.BRatio,
				KellyFraction:       stockData.KellyFraction,
				HalfKellySuggested:  stockData.HalfKellySuggested,
				SharesOwned:         stockData.SharesOwned,
				AvgPriceLocal:       stockData.AvgPriceLocal,
				BuyZoneMin:          stockData.BuyZoneMin,
				BuyZoneMax:          stockData.BuyZoneMax,
				BuyZoneStatus:       stockData.BuyZoneStatus,
				SellZoneLowerBound:  stockData.SellZoneLowerBound,
				SellZoneUpperBound:  stockData.SellZoneUpperBound,
				SellZoneStatus:      stockData.SellZoneStatus,
				Assessment:          stockData.Assessment,
				UpdateFrequency:     stockData.UpdateFrequency,
				DataSource:          stockData.DataSource,
				FairValueSource:     stockData.FairValueSource,
				Comment:             stockData.Comment,
				LastUpdated:         time.Now(),
				PortfolioID:         portfolioID,
			}

			// Set default currency if not provided
			if stock.Currency == "" {
				stock.Currency = "USD"
			}

			// Set default update frequency if not provided
			if stock.UpdateFrequency == "" {
				stock.UpdateFrequency = "daily"
			}

			if stock.SharesOwned > 0 && stock.CurrentPrice > 0 {
				if err := h.updateStockUSDValues(&stock); err != nil {
					errors = append(errors, "Failed to convert "+stockData.Ticker+" values: "+err.Error())
					continue
				}
			}

			if err := h.db.Create(&stock).Error; err != nil {
				errors = append(errors, "Failed to create "+stockData.Ticker+": "+err.Error())
				continue
			}
			created++
		} else if err == nil {
			// Update existing stock
			existing.CompanyName = stockData.CompanyName
			if stockData.ISIN != "" {
				existing.ISIN = stockData.ISIN
			}
			if stockData.Sector != "" {
				existing.Sector = stockData.Sector
			}
			if stockData.CurrentPrice > 0 {
				existing.CurrentPrice = stockData.CurrentPrice
			}
			if stockData.Currency != "" {
				existing.Currency = stockData.Currency
			}
			if stockData.FairValue > 0 {
				existing.FairValue = stockData.FairValue
			}
			if stockData.UpsidePotential != 0 {
				existing.UpsidePotential = stockData.UpsidePotential
			}
			if stockData.DownsideRisk != 0 {
				existing.DownsideRisk = stockData.DownsideRisk
			}
			if stockData.ProbabilityPositive > 0 {
				existing.ProbabilityPositive = stockData.ProbabilityPositive
			}
			if stockData.ExpectedValue != 0 {
				existing.ExpectedValue = stockData.ExpectedValue
			}
			if stockData.Beta != 0 {
				existing.Beta = stockData.Beta
			}
			if stockData.Volatility != 0 {
				existing.Volatility = stockData.Volatility
			}
			if stockData.PERatio != 0 {
				existing.PERatio = stockData.PERatio
			}
			if stockData.EPSGrowthRate != 0 {
				existing.EPSGrowthRate = stockData.EPSGrowthRate
			}
			if stockData.DebtToEBITDA != 0 {
				existing.DebtToEBITDA = stockData.DebtToEBITDA
			}
			if stockData.DividendYield != 0 {
				existing.DividendYield = stockData.DividendYield
			}
			if stockData.BRatio != 0 {
				existing.BRatio = stockData.BRatio
			}
			if stockData.KellyFraction != 0 {
				existing.KellyFraction = stockData.KellyFraction
			}
			if stockData.HalfKellySuggested != 0 {
				existing.HalfKellySuggested = stockData.HalfKellySuggested
			}
			if stockData.SharesOwned >= 0 {
				existing.SharesOwned = stockData.SharesOwned
			}
			if stockData.AvgPriceLocal > 0 {
				existing.AvgPriceLocal = stockData.AvgPriceLocal
			}
			if stockData.BuyZoneMin > 0 {
				existing.BuyZoneMin = stockData.BuyZoneMin
			}
			if stockData.BuyZoneMax > 0 {
				existing.BuyZoneMax = stockData.BuyZoneMax
			}
			if stockData.BuyZoneStatus != "" {
				existing.BuyZoneStatus = stockData.BuyZoneStatus
			}
			if stockData.SellZoneLowerBound > 0 {
				existing.SellZoneLowerBound = stockData.SellZoneLowerBound
			}
			if stockData.SellZoneUpperBound > 0 {
				existing.SellZoneUpperBound = stockData.SellZoneUpperBound
			}
			if stockData.SellZoneStatus != "" {
				existing.SellZoneStatus = stockData.SellZoneStatus
			}
			if stockData.Assessment != "" {
				existing.Assessment = stockData.Assessment
			}
			if stockData.UpdateFrequency != "" {
				existing.UpdateFrequency = stockData.UpdateFrequency
			}
			if stockData.DataSource != "" {
				existing.DataSource = stockData.DataSource
			}
			if stockData.FairValueSource != "" {
				existing.FairValueSource = stockData.FairValueSource
			}
			if stockData.Comment != "" {
				existing.Comment = stockData.Comment
			}

			if existing.SharesOwned > 0 && existing.CurrentPrice > 0 {
				if err := h.updateStockUSDValues(&existing); err != nil {
					errors = append(errors, "Failed to convert "+stockData.Ticker+" values: "+err.Error())
					continue
				}
			}

			existing.LastUpdated = time.Now()

			if err := h.db.Save(&existing).Error; err != nil {
				errors = append(errors, "Failed to update "+stockData.Ticker+": "+err.Error())
				continue
			}
			updated++
		} else {
			errors = append(errors, "Error checking "+stockData.Ticker+": "+err.Error())
		}
	}

	response := gin.H{
		"message": "Bulk update completed",
		"created": created,
		"updated": updated,
		"total":   len(req.Stocks),
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	h.logger.Info().
		Int("created", created).
		Int("updated", updated).
		Int("total", len(req.Stocks)).
		Msg("Bulk stock update completed")

	c.JSON(http.StatusOK, response)
}

// GetStocksBatch returns multiple stocks by IDs in a single request
// Query params: ids=1,2,3,4 or ids[]=1&ids[]=2&ids[]=3
func (h *StockHandler) GetStocksBatch(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	// Get IDs from query params
	idsParam := c.Query("ids")
	if idsParam == "" {
		// Try array format
		idsArray := c.QueryArray("ids[]")
		if len(idsArray) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No stock IDs provided"})
			return
		}

		var stocks []models.Stock
		if err := h.db.Where("id IN ? AND portfolio_id = ?", idsArray, portfolioID).Find(&stocks).Error; err != nil {
			h.logger.Error().Err(err).Msg("Failed to fetch stocks")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
			return
		}

		// Add caching headers - cache for 30 seconds
		c.Header("Cache-Control", "private, max-age=30, stale-while-revalidate=60")

		c.JSON(http.StatusOK, stocks)
		return
	}

	// Split comma-separated IDs
	ids := []string{}
	for _, id := range strings.Split(idsParam, ",") {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			ids = append(ids, trimmed)
		}
	}

	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid stock IDs provided"})
		return
	}

	var stocks []models.Stock
	if err := h.db.Where("id IN ? AND portfolio_id = ?", ids, portfolioID).Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	// Add caching headers - cache for 30 seconds
	c.Header("Cache-Control", "private, max-age=30, stale-while-revalidate=60")

	c.JSON(http.StatusOK, stocks)
}
