package services

import (
	"math"
	"testing"

	"github.com/art-pro/stock-backend/pkg/models"
)

func assertClose(t *testing.T, got, want, tol float64, field string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.6f want %.6f (tol %.6f)", field, got, want, tol)
	}
}

func TestCalculateMetricsDefaultsAndDerivedValues(t *testing.T) {
	stock := models.Stock{
		Beta:         1.2,
		CurrentPrice: 100,
		FairValue:    120,
	}

	CalculateMetrics(&stock)

	assertClose(t, stock.DownsideRisk, -25, 0.0001, "DownsideRisk")
	assertClose(t, stock.ProbabilityPositive, 0.65, 0.0001, "ProbabilityPositive")
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
}

func TestCalculateMetricsPreservesDownsideRisk(t *testing.T) {
	stock := models.Stock{
		Beta:         0.4,
		CurrentPrice: 100,
		FairValue:    120,
		DownsideRisk: -10,
	}

	CalculateMetrics(&stock)

	assertClose(t, stock.DownsideRisk, -10, 0.0001, "DownsideRisk")
}

func TestCalculateMetricsClampsNegativeKelly(t *testing.T) {
	stock := models.Stock{
		Beta:                2.0,
		CurrentPrice:        100,
		FairValue:           102,
		ProbabilityPositive: 0.1,
	}

	CalculateMetrics(&stock)

	assertClose(t, stock.KellyFraction, 0, 0.0001, "KellyFraction")
	assertClose(t, stock.HalfKellySuggested, 0, 0.0001, "HalfKellySuggested")
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
		{
			SharesOwned:  0,
			CurrentPrice: 999,
			Currency:     "USD",
			Sector:       "Ignored",
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

	assertClose(t, metrics.SectorWeights["Tech"], 66.6667, 0.05, "SectorWeights[Tech]")
	assertClose(t, metrics.SectorWeights["Health"], 33.3333, 0.05, "SectorWeights[Health]")
}

func TestCalculateBuyZoneResult_ValidInput(t *testing.T) {
	result, err := CalculateBuyZoneResult("UNH", 380, 0.65, -15, 284.37)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Ticker != "UNH" {
		t.Fatalf("Ticker: got %s want %s", result.Ticker, "UNH")
	}

	assertClose(t, result.BuyZone.LowerBound, 289.7361, 0.02, "LowerBound")
	assertClose(t, result.BuyZone.UpperBound, 319.7411, 0.02, "UpperBound")
	assertClose(t, result.CurrentExpectedValue, 16.6087, 0.02, "CurrentExpectedValue")

	if result.ZoneStatus != "EV >> 15%" {
		t.Fatalf("ZoneStatus: got %s want %s", result.ZoneStatus, "EV >> 15%")
	}
}

func TestCalculateBuyZoneResult_ValidationErrors(t *testing.T) {
	_, err := CalculateBuyZoneResult("X", 100, 1.2, -20, 90)
	if err == nil {
		t.Fatalf("expected error for probability_positive > 1")
	}

	_, err = CalculateBuyZoneResult("X", 100, 0.65, 20, 90)
	if err == nil {
		t.Fatalf("expected error for non-negative downside_risk")
	}
}

func TestCalculateBuyZoneResult_StatusClassifications(t *testing.T) {
	result, err := CalculateBuyZoneResult("ABC", 120, 0.65, -25, 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ZoneStatus != "EV >> 15%" {
		t.Fatalf("ZoneStatus below zone: got %s want %s", result.ZoneStatus, "EV >> 15%")
	}

	result, err = CalculateBuyZoneResult("ABC", 120, 0.65, -25, 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ZoneStatus != "within buy zone" {
		t.Fatalf("ZoneStatus within zone: got %s want %s", result.ZoneStatus, "within buy zone")
	}
}
