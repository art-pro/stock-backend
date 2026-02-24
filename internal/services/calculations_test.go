package services

import (
	"math"
	"testing"

	"github.com/art-pro/stock-backend/internal/models"
)

func assertClose(t *testing.T, got, want, tol float64, field string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.6f want %.6f (tol %.6f)", field, got, want, tol)
	}
}

func TestCalculateMetricsDerivedValues(t *testing.T) {
	t.Parallel()
	stock := models.Stock{
		Beta:                1.2,
		CurrentPrice:        100,
		FairValue:           120,
		ProbabilityPositive: 0.65,
	}

	CalculateMetrics(&stock)

	assertClose(t, stock.DownsideRisk, -25, 0.0001, "DownsideRisk")
	assertClose(t, stock.UpsidePotential, 20, 0.0001, "UpsidePotential")
	assertClose(t, stock.BRatio, 0.8, 0.0001, "BRatio")
	assertClose(t, stock.ExpectedValue, 4.25, 0.0001, "ExpectedValue")
	assertClose(t, stock.KellyFraction, 21.25, 0.0001, "KellyFraction")
	assertClose(t, stock.HalfKellySuggested, 10.625, 0.0001, "HalfKellySuggested")

	if stock.Assessment != "Hold" {
		t.Fatalf("Assessment: got %s want %s", stock.Assessment, "Hold")
	}

	assertClose(t, stock.BuyZoneMax, 96.5944, 0.02, "BuyZoneMax")
	assertClose(t, stock.BuyZoneMin, 86.9350, 0.02, "BuyZoneMin")

	// Verify buy zone bounds are properly ordered
	if stock.BuyZoneMin >= stock.BuyZoneMax {
		t.Errorf("BuyZoneMin should be less than BuyZoneMax")
	}
}

func TestCalculateMetricsNegativeEVAssessment(t *testing.T) {
	t.Parallel()
	stock := models.Stock{
		Beta:                0.4,
		CurrentPrice:        100,
		FairValue:           100,
		ProbabilityPositive: 0.1,
	}

	CalculateMetrics(&stock)

	if stock.Assessment != "Sell" {
		t.Fatalf("Assessment: got %s want %s", stock.Assessment, "Sell")
	}

	// Verify EV is actually negative
	if stock.ExpectedValue >= 0 {
		t.Errorf("ExpectedValue should be negative for Sell assessment, got %.2f", stock.ExpectedValue)
	}

	// Verify Kelly is clamped at 0 for negative EV
	if stock.KellyFraction != 0 {
		t.Errorf("KellyFraction should be 0 for negative EV, got %.2f", stock.KellyFraction)
	}
}

func TestCalculatePortfolioMetrics(t *testing.T) {
	t.Parallel()
	stocks := []models.Stock{
		{
			SharesOwned:   10,
			CurrentPrice:  100,
			Currency:      "USD",
			ExpectedValue: 5,
			Volatility:    10,
			Sector:        "Tech",
		},
		{
			SharesOwned:   5,
			CurrentPrice:  200,
			Currency:      "EUR",
			ExpectedValue: 1,
			Volatility:    20,
			Sector:        "Health",
		},
	}
	fxRates := map[string]float64{
		"USD": 1,
		"EUR": 2,
	}

	metrics := CalculatePortfolioMetrics(stocks, fxRates)

	assertClose(t, metrics.TotalValue, 1500, 0.01, "TotalValue")
	assertClose(t, metrics.OverallEV, 3.6667, 0.01, "OverallEV")
	assertClose(t, metrics.WeightedVolatility, 13.3333, 0.01, "WeightedVolatility")
	assertClose(t, metrics.SharpeRatio, -0.025, 0.01, "SharpeRatio")
	assertClose(t, metrics.KellyUtilization, 100, 0.01, "KellyUtilization")

	// Note: internal/services uses percentage values (0-100), not fractions (0-1)
	assertClose(t, metrics.SectorWeights["Tech"], 66.6667, 0.05, "SectorWeights[Tech]")
	assertClose(t, metrics.SectorWeights["Health"], 33.3333, 0.05, "SectorWeights[Health]")

	// Verify total sector weights
	totalWeight := 0.0
	for _, weight := range metrics.SectorWeights {
		totalWeight += weight
	}
	assertClose(t, totalWeight, 100.0, 0.1, "Total sector weights")
}

func TestCalculatePortfolioMetricsEmpty(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1}

	metrics := CalculatePortfolioMetrics([]models.Stock{}, fxRates)

	assertClose(t, metrics.TotalValue, 0, 0.01, "TotalValue")
	assertClose(t, metrics.OverallEV, 0, 0.01, "OverallEV")
	assertClose(t, metrics.WeightedVolatility, 0, 0.01, "WeightedVolatility")
	if len(metrics.SectorWeights) != 0 {
		t.Errorf("Empty portfolio should have no sector weights, got %d", len(metrics.SectorWeights))
	}
}

func TestCalculatePortfolioMetricsWithZeroShares(t *testing.T) {
	t.Parallel()
	stocks := []models.Stock{
		{
			SharesOwned:   10,
			CurrentPrice:  100,
			Currency:      "USD",
			ExpectedValue: 5,
			Volatility:    10,
			Sector:        "Tech",
		},
		{
			SharesOwned:  0,
			CurrentPrice: 200,
			Currency:     "USD",
			Sector:       "Ignored",
		},
	}
	fxRates := map[string]float64{"USD": 1}

	metrics := CalculatePortfolioMetrics(stocks, fxRates)

	assertClose(t, metrics.TotalValue, 1000, 0.01, "TotalValue")
	assertClose(t, metrics.SectorWeights["Tech"], 100.0, 0.01, "SectorWeights[Tech]")
	if _, exists := metrics.SectorWeights["Ignored"]; exists {
		t.Errorf("Sector weights should not include stocks with SharesOwned=0")
	}
}
