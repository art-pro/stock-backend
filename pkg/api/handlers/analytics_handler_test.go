package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAnalyticsTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	err = db.AutoMigrate(
		&models.User{},
		&models.Portfolio{},
		&models.Stock{},
		&models.StockHistory{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

func TestGetTopMovers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAnalyticsTestDB(t)
	logger := zerolog.Nop()

	// Create test portfolio
	portfolio := models.Portfolio{Name: "Test Portfolio", IsDefault: true}
	db.Create(&portfolio)

	// Create test stocks
	now := time.Now()
	oneDayAgo := now.Add(-24 * time.Hour)

	stocks := []models.Stock{
		{
			PortfolioID:   portfolio.ID,
			Ticker:        "GAINER1",
			CompanyName:   "Big Gainer Inc",
			Sector:        "Technology",
			CurrentPrice:  110.0,
			ExpectedValue: 15.0,
			Assessment:    "Add",
			LastUpdated:   now,
		},
		{
			PortfolioID:   portfolio.ID,
			Ticker:        "LOSER1",
			CompanyName:   "Big Loser Corp",
			Sector:        "Energy",
			CurrentPrice:  80.0,
			ExpectedValue: -5.0,
			Assessment:    "Sell",
			LastUpdated:   now,
		},
		{
			PortfolioID:   portfolio.ID,
			Ticker:        "STABLE1",
			CompanyName:   "Stable Holdings",
			Sector:        "Utilities",
			CurrentPrice:  100.0,
			ExpectedValue: 5.0,
			Assessment:    "Hold",
			LastUpdated:   now,
		},
	}

	for i := range stocks {
		db.Create(&stocks[i])
	}

	// Create historical data (1 day ago)
	histories := []models.StockHistory{
		{
			StockID:       stocks[0].ID,
			PortfolioID:   portfolio.ID,
			Ticker:        "GAINER1",
			CurrentPrice:  100.0,
			ExpectedValue: 10.0,
			Assessment:    "Hold",
			RecordedAt:    oneDayAgo,
		},
		{
			StockID:       stocks[1].ID,
			PortfolioID:   portfolio.ID,
			Ticker:        "LOSER1",
			CurrentPrice:  100.0,
			ExpectedValue: 2.0,
			Assessment:    "Hold",
			RecordedAt:    oneDayAgo,
		},
		{
			StockID:       stocks[2].ID,
			PortfolioID:   portfolio.ID,
			Ticker:        "STABLE1",
			CurrentPrice:  100.0,
			ExpectedValue: 5.0,
			Assessment:    "Hold",
			RecordedAt:    oneDayAgo,
		},
	}

	for i := range histories {
		db.Create(&histories[i])
	}

	handler := NewAnalyticsHandler(db, logger)
	router := gin.New()
	router.GET("/analytics/top-movers", handler.GetTopMovers)

	t.Run("GetTopMovers_24h_Success", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/analytics/top-movers?timeframe=24h&limit=5", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
		}

		var response TopMoversResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if response.Timeframe != "24h" {
			t.Errorf("expected timeframe '24h', got '%s'", response.Timeframe)
		}

		if len(response.TopGainers) == 0 {
			t.Error("expected top gainers to not be empty")
		}

		if len(response.TopLosers) == 0 {
			t.Error("expected top losers to not be empty")
		}

		// Check top gainer
		if len(response.TopGainers) > 0 {
			if response.TopGainers[0].Ticker != "GAINER1" {
				t.Errorf("expected top gainer ticker 'GAINER1', got '%s'", response.TopGainers[0].Ticker)
			}
			if response.TopGainers[0].PriceChangePercent != 10.0 {
				t.Errorf("expected price change 10.0%%, got %.2f%%", response.TopGainers[0].PriceChangePercent)
			}
		}

		// Check top loser
		if len(response.TopLosers) > 0 {
			if response.TopLosers[0].Ticker != "LOSER1" {
				t.Errorf("expected top loser ticker 'LOSER1', got '%s'", response.TopLosers[0].Ticker)
			}
			if response.TopLosers[0].PriceChangePercent != -20.0 {
				t.Errorf("expected price change -20.0%%, got %.2f%%", response.TopLosers[0].PriceChangePercent)
			}
		}
	})

	t.Run("GetTopMovers_InvalidTimeframe", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/analytics/top-movers?timeframe=invalid", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestGetTopLosers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAnalyticsTestDB(t)
	logger := zerolog.Nop()

	// Create test portfolio
	portfolio := models.Portfolio{Name: "Test Portfolio", IsDefault: true}
	db.Create(&portfolio)

	// Create test stocks with varying P&L
	stocks := []models.Stock{
		{
			PortfolioID:     portfolio.ID,
			Ticker:          "LOSS1",
			CompanyName:     "Big Loss Inc",
			Sector:          "Technology",
			CurrentPrice:    80.0,
			SharesOwned:     100,
			AvgPriceLocal:   100.0,
			UnrealizedPnL:   -2000.0,
			CurrentValueUSD: 8000.0,
			Weight:          0.1,
			ExpectedValue:   -5.0,
			Assessment:      "Sell",
		},
		{
			PortfolioID:     portfolio.ID,
			Ticker:          "LOSS2",
			CompanyName:     "Small Loss Corp",
			Sector:          "Energy",
			CurrentPrice:    95.0,
			SharesOwned:     50,
			AvgPriceLocal:   100.0,
			UnrealizedPnL:   -250.0,
			CurrentValueUSD: 4750.0,
			Weight:          0.05,
			ExpectedValue:   2.0,
			Assessment:      "Hold",
		},
	}

	for i := range stocks {
		db.Create(&stocks[i])
	}

	handler := NewAnalyticsHandler(db, logger)
	router := gin.New()
	router.GET("/analytics/top-losers", handler.GetTopLosers)

	t.Run("GetTopLosers_Success", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/analytics/top-losers?limit=10", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
		}

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		losers, ok := response["losers"].([]interface{})
		if !ok {
			t.Fatal("expected 'losers' field in response")
		}

		if len(losers) != 2 {
			t.Errorf("expected 2 losers, got %d", len(losers))
		}

		// First loser should be LOSS1 (worst P&L)
		if len(losers) > 0 {
			firstLoser := losers[0].(map[string]interface{})
			if firstLoser["ticker"] != "LOSS1" {
				t.Errorf("expected first loser ticker 'LOSS1', got '%s'", firstLoser["ticker"])
			}
			if firstLoser["unrealized_pnl"] != -2000.0 {
				t.Errorf("expected unrealized_pnl -2000.0, got %v", firstLoser["unrealized_pnl"])
			}
		}
	})
}

func TestBuildTopMoversLists(t *testing.T) {
	db := setupAnalyticsTestDB(t)
	logger := zerolog.Nop()
	handler := NewAnalyticsHandler(db, logger)

	movers := []MoverData{
		{Ticker: "G1", PriceChangePercent: 10.0, EVChange: 5.0},
		{Ticker: "G2", PriceChangePercent: 8.0, EVChange: 3.0},
		{Ticker: "L1", PriceChangePercent: -15.0, EVChange: -8.0},
		{Ticker: "L2", PriceChangePercent: -10.0, EVChange: -4.0},
		{Ticker: "S1", PriceChangePercent: 0.5, EVChange: 0.1},
	}

	result := handler.buildTopMoversLists(movers, 2, "24h", time.Now())

	if len(result.TopGainers) != 2 {
		t.Errorf("expected 2 top gainers, got %d", len(result.TopGainers))
	}

	if len(result.TopGainers) > 0 && result.TopGainers[0].Ticker != "G1" {
		t.Errorf("expected first gainer 'G1', got '%s'", result.TopGainers[0].Ticker)
	}

	if len(result.TopLosers) != 2 {
		t.Errorf("expected 2 top losers, got %d", len(result.TopLosers))
	}

	if len(result.TopLosers) > 0 && result.TopLosers[0].Ticker != "L1" {
		t.Errorf("expected first loser 'L1', got '%s'", result.TopLosers[0].Ticker)
	}

	if len(result.BiggestEVRises) != 2 {
		t.Errorf("expected 2 EV rises, got %d", len(result.BiggestEVRises))
	}

	if len(result.BiggestEVDrops) != 2 {
		t.Errorf("expected 2 EV drops, got %d", len(result.BiggestEVDrops))
	}
}
