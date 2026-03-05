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

// AnalyticsHandler handles analytics-related requests
type AnalyticsHandler struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewAnalyticsHandler creates a new analytics handler
func NewAnalyticsHandler(db *gorm.DB, logger zerolog.Logger) *AnalyticsHandler {
	return &AnalyticsHandler{
		db:     db,
		logger: logger,
	}
}

func (h *AnalyticsHandler) resolvePortfolioID(c *gin.Context) (uint, error) {
	if portfolioIDParam := c.Query("portfolio_id"); portfolioIDParam != "" {
		parsed, err := strconv.ParseUint(portfolioIDParam, 10, 32)
		if err != nil {
			return 0, err
		}
		return uint(parsed), nil
	}
	return database.GetDefaultPortfolioID(h.db)
}

// TopLosersResponse represents the response structure for top losers
type TopLosersResponse struct {
	Ticker           string  `json:"ticker"`
	CompanyName      string  `json:"company_name"`
	Sector           string  `json:"sector"`
	Currency         string  `json:"currency"`
	CurrentPrice     float64 `json:"current_price"`
	UnrealizedPnL    float64 `json:"unrealized_pnl"`
	UnrealizedPnLPct float64 `json:"unrealized_pnl_pct"`
	SharesOwned      int     `json:"shares_owned"`
	AvgPriceLocal    float64 `json:"avg_price_local"`
	CurrentValueUSD  float64 `json:"current_value_usd"`
	Weight           float64 `json:"weight"`
	ExpectedValue    float64 `json:"expected_value"`
	Assessment       string  `json:"assessment"`
	BuyZoneStatus    string  `json:"buy_zone_status"`
	SellZoneStatus   string  `json:"sell_zone_status"`
}

// GetTopLosers returns stocks with the worst unrealized P&L (owned positions only)
func (h *AnalyticsHandler) GetTopLosers(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	limit := 10
	if limitParam := c.Query("limit"); limitParam != "" {
		if parsed, err := strconv.Atoi(limitParam); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	minShares := 1
	if minSharesParam := c.Query("min_shares"); minSharesParam != "" {
		if parsed, err := strconv.Atoi(minSharesParam); err == nil && parsed > 0 {
			minShares = parsed
		}
	}

	var stocks []models.Stock
	err = h.db.Where("portfolio_id = ? AND shares_owned >= ?", portfolioID, minShares).
		Order("unrealized_pnl ASC").
		Limit(limit).
		Find(&stocks).Error

	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch top losers")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch top losers"})
		return
	}

	response := make([]TopLosersResponse, len(stocks))
	for i, stock := range stocks {
		unrealizedPnLPct := 0.0
		if stock.AvgPriceLocal > 0 {
			unrealizedPnLPct = ((stock.CurrentPrice - stock.AvgPriceLocal) / stock.AvgPriceLocal) * 100
		}

		response[i] = TopLosersResponse{
			Ticker:           stock.Ticker,
			CompanyName:      stock.CompanyName,
			Sector:           stock.Sector,
			Currency:         stock.Currency,
			CurrentPrice:     stock.CurrentPrice,
			UnrealizedPnL:    stock.UnrealizedPnL,
			UnrealizedPnLPct: unrealizedPnLPct,
			SharesOwned:      stock.SharesOwned,
			AvgPriceLocal:    stock.AvgPriceLocal,
			CurrentValueUSD:  stock.CurrentValueUSD,
			Weight:           stock.Weight,
			ExpectedValue:    stock.ExpectedValue,
			Assessment:       stock.Assessment,
			BuyZoneStatus:    stock.BuyZoneStatus,
			SellZoneStatus:   stock.SellZoneStatus,
		}
	}

	c.Header("Cache-Control", "private, max-age=60, stale-while-revalidate=120")

	c.JSON(http.StatusOK, gin.H{
		"losers": response,
		"count":  len(response),
		"meta": gin.H{
			"portfolio_id": portfolioID,
			"limit":        limit,
			"min_shares":   minShares,
		},
	})
}

// MoverData represents a stock's movement data
type MoverData struct {
	StockID            uint      `json:"stock_id"`
	Ticker             string    `json:"ticker"`
	CompanyName        string    `json:"company_name"`
	Sector             string    `json:"sector"`
	CurrentPrice       float64   `json:"current_price"`
	PreviousPrice      float64   `json:"previous_price"`
	PriceChange        float64   `json:"price_change"`
	PriceChangePercent float64   `json:"price_change_percent"`
	CurrentEV          float64   `json:"current_ev"`
	PreviousEV         float64   `json:"previous_ev"`
	EVChange           float64   `json:"ev_change"`
	CurrentAssessment  string    `json:"current_assessment"`
	PreviousAssessment string    `json:"previous_assessment"`
	LastUpdated        time.Time `json:"last_updated"`
}

// TopMoversResponse represents the response for top movers
type TopMoversResponse struct {
	Timeframe      string      `json:"timeframe"`
	TopGainers     []MoverData `json:"top_gainers"`
	TopLosers      []MoverData `json:"top_losers"`
	BiggestEVRises []MoverData `json:"biggest_ev_rises"`
	BiggestEVDrops []MoverData `json:"biggest_ev_drops"`
	GeneratedAt    time.Time   `json:"generated_at"`
}

// GetTopMovers returns top gaining/losing stocks by price and EV change
func (h *AnalyticsHandler) GetTopMovers(c *gin.Context) {
	portfolioID, err := h.resolvePortfolioID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid portfolio_id"})
		return
	}

	timeframe := c.DefaultQuery("timeframe", "24h")
	limitStr := c.DefaultQuery("limit", "5")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 20 {
		limit = 5
	}

	var cutoffTime time.Time
	now := time.Now()
	switch timeframe {
	case "24h":
		cutoffTime = now.Add(-24 * time.Hour)
	case "7d":
		cutoffTime = now.Add(-7 * 24 * time.Hour)
	case "30d":
		cutoffTime = now.Add(-30 * 24 * time.Hour)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid timeframe. Use 24h, 7d, or 30d"})
		return
	}

	var stocks []models.Stock
	if err := h.db.Where("portfolio_id = ?", portfolioID).Find(&stocks).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to fetch stocks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stocks"})
		return
	}

	if len(stocks) == 0 {
		c.JSON(http.StatusOK, TopMoversResponse{
			Timeframe:      timeframe,
			TopGainers:     []MoverData{},
			TopLosers:      []MoverData{},
			BiggestEVRises: []MoverData{},
			BiggestEVDrops: []MoverData{},
			GeneratedAt:    now,
		})
		return
	}

	var movers []MoverData

	for _, stock := range stocks {
		var history models.StockHistory
		err := h.db.Where("stock_id = ? AND portfolio_id = ? AND recorded_at <= ?",
			stock.ID, portfolioID, cutoffTime).
			Order("recorded_at DESC").
			First(&history).Error

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				continue
			}
			h.logger.Error().Err(err).Msg("Failed to fetch history")
			continue
		}

		priceChange := stock.CurrentPrice - history.CurrentPrice
		priceChangePercent := 0.0
		if history.CurrentPrice > 0 {
			priceChangePercent = (priceChange / history.CurrentPrice) * 100
		}

		movers = append(movers, MoverData{
			StockID:            stock.ID,
			Ticker:             stock.Ticker,
			CompanyName:        stock.CompanyName,
			Sector:             stock.Sector,
			CurrentPrice:       stock.CurrentPrice,
			PreviousPrice:      history.CurrentPrice,
			PriceChange:        priceChange,
			PriceChangePercent: priceChangePercent,
			CurrentEV:          stock.ExpectedValue,
			PreviousEV:         history.ExpectedValue,
			EVChange:           stock.ExpectedValue - history.ExpectedValue,
			CurrentAssessment:  stock.Assessment,
			PreviousAssessment: history.Assessment,
			LastUpdated:        stock.LastUpdated,
		})
	}

	response := h.buildTopMoversLists(movers, limit, timeframe, now)
	c.Header("Cache-Control", "private, max-age=300, stale-while-revalidate=600")
	c.JSON(http.StatusOK, response)
}

func (h *AnalyticsHandler) buildTopMoversLists(movers []MoverData, limit int, timeframe string, now time.Time) TopMoversResponse {
	topGainers := make([]MoverData, 0, limit)
	topLosers := make([]MoverData, 0, limit)
	biggestEVRises := make([]MoverData, 0, limit)
	biggestEVDrops := make([]MoverData, 0, limit)

	for i := range movers {
		if movers[i].PriceChangePercent > 0 {
			h.insertSorted(&topGainers, movers[i], limit, func(a, b MoverData) bool {
				return a.PriceChangePercent > b.PriceChangePercent
			})
		}
		if movers[i].PriceChangePercent < 0 {
			h.insertSorted(&topLosers, movers[i], limit, func(a, b MoverData) bool {
				return a.PriceChangePercent < b.PriceChangePercent
			})
		}
		if movers[i].EVChange > 0 {
			h.insertSorted(&biggestEVRises, movers[i], limit, func(a, b MoverData) bool {
				return a.EVChange > b.EVChange
			})
		}
		if movers[i].EVChange < 0 {
			h.insertSorted(&biggestEVDrops, movers[i], limit, func(a, b MoverData) bool {
				return a.EVChange < b.EVChange
			})
		}
	}

	return TopMoversResponse{
		Timeframe:      timeframe,
		TopGainers:     topGainers,
		TopLosers:      topLosers,
		BiggestEVRises: biggestEVRises,
		BiggestEVDrops: biggestEVDrops,
		GeneratedAt:    now,
	}
}

func (h *AnalyticsHandler) insertSorted(list *[]MoverData, item MoverData, limit int, compare func(a, b MoverData) bool) {
	inserted := false
	for j := 0; j < len(*list); j++ {
		if compare(item, (*list)[j]) {
			*list = append((*list)[:j], append([]MoverData{item}, (*list)[j:]...)...)
			inserted = true
			break
		}
	}
	if !inserted && len(*list) < limit {
		*list = append(*list, item)
	}
	if len(*list) > limit {
		*list = (*list)[:limit]
	}
}
