package services

import (
	"math"
	"testing"

	"github.com/artpro/assessapp/internal/models"
)

func assertClose(t *testing.T, got, want, tol float64, field string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.6f want %.6f (tol %.6f)", field, got, want, tol)
	}
}

func TestCalculateMetricsDerivedValues(t *testing.T) {
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

	assertClose(t, stock.BuyZoneMax, 96.6003, 0.01, "BuyZoneMax")
	assertClose(t, stock.BuyZoneMin, 86.9403, 0.02, "BuyZoneMin")
}

func TestCalculateMetricsNegativeEVAssessment(t *testing.T) {
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
}

func TestCalculatePortfolioMetrics(t *testing.T) {
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

	assertClose(t, metrics.TotalValue, 3000, 0.01, "TotalValue")
	assertClose(t, metrics.OverallEV, 5.6667, 0.01, "OverallEV")
	assertClose(t, metrics.WeightedVolatility, 23.3333, 0.01, "WeightedVolatility")
	assertClose(t, metrics.SharpeRatio, 0.243, 0.01, "SharpeRatio")
	assertClose(t, metrics.KellyUtilization, 100, 0.01, "KellyUtilization")

	assertClose(t, metrics.SectorWeights["Tech"], 100, 0.01, "SectorWeights[Tech]")
	assertClose(t, metrics.SectorWeights["Health"], 66.6667, 0.05, "SectorWeights[Health]")
}
