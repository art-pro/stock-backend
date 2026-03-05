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
	t.Parallel()
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
	t.Parallel()
	stock := models.Stock{
		Beta:         0.4,
		CurrentPrice: 100,
		FairValue:    120,
		DownsideRisk: -10,
	}

	CalculateMetrics(&stock)

	assertClose(t, stock.DownsideRisk, -10, 0.0001, "DownsideRisk")
	// Verify other fields are still calculated correctly with custom downside
	assertClose(t, stock.UpsidePotential, 20, 0.0001, "UpsidePotential")
	assertClose(t, stock.BRatio, 2.0, 0.0001, "BRatio")
}

func TestCalculateMetricsClampsNegativeKelly(t *testing.T) {
	t.Parallel()
	stock := models.Stock{
		Beta:                2.0,
		CurrentPrice:        100,
		FairValue:           102,
		ProbabilityPositive: 0.1,
	}

	CalculateMetrics(&stock)

	assertClose(t, stock.KellyFraction, 0, 0.0001, "KellyFraction")
	assertClose(t, stock.HalfKellySuggested, 0, 0.0001, "HalfKellySuggested")
	// Negative EV should result in Sell assessment
	if stock.Assessment != "Sell" {
		t.Errorf("Assessment: got %s, want Sell for negative EV", stock.Assessment)
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
			Weight:        50, // This should be recalculated
		},
		{
			SharesOwned:   5,
			CurrentPrice:  200,
			Currency:      "EUR",
			ExpectedValue: 1,
			Volatility:    20,
			Sector:        "Health",
			Weight:        30,
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

	// sector_weights are fractions 0–1 (DATA_CONTRACT.md)
	assertClose(t, metrics.SectorWeights["Tech"], 0.666667, 0.0005, "SectorWeights[Tech]")
	assertClose(t, metrics.SectorWeights["Health"], 0.333333, 0.0005, "SectorWeights[Health]")

	// Verify ignored stock is not in sector weights
	if _, exists := metrics.SectorWeights["Ignored"]; exists {
		t.Errorf("Sector weights should not include stocks with SharesOwned=0")
	}
}

func TestCalculateBuyZoneResult_ValidInput(t *testing.T) {
	t.Parallel()
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

	// Verify zone bounds are properly ordered
	if result.BuyZone.LowerBound >= result.BuyZone.UpperBound {
		t.Errorf("LowerBound should be less than UpperBound")
	}
}

func TestCalculateBuyZoneResult_ValidationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		ticker      string
		fairValue   float64
		probability float64
		downside    float64
		currentPrice float64
		wantErr     bool
	}{
		{"probability > 1", "X", 100, 1.2, -20, 90, true},
		{"probability < 0", "X", 100, -0.1, -20, 90, true},
		{"positive downside", "X", 100, 0.65, 20, 90, true},
		{"zero downside", "X", 100, 0.65, 0, 90, true},
		{"non-positive fair value", "X", 0, 0.65, -20, 90, true},
		{"negative fair value", "X", -100, 0.65, -20, 90, true},
		{"valid input", "X", 100, 0.65, -20, 90, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateBuyZoneResult(tt.ticker, tt.fairValue, tt.probability, tt.downside, tt.currentPrice)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateBuyZoneResult() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateBuyZoneResult_StatusClassifications(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		price      float64
		wantStatus string
	}{
		{"below lower bound", 80, "EV >> 15%"},
		{"within buy zone", 90, "within buy zone"},
		{"above upper bound", 130, "outside buy zone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CalculateBuyZoneResult("ABC", 120, 0.65, -25, tt.price)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.ZoneStatus != tt.wantStatus {
				t.Errorf("ZoneStatus: got %s want %s", result.ZoneStatus, tt.wantStatus)
			}
		})
	}
}

func TestCalculateSellZoneResult_ValidInput(t *testing.T) {
	t.Parallel()
	result, err := CalculateSellZoneResult("UNH", 380, 0.65, -15, 350)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertClose(t, result.SellZone.LowerBound, 337.2014, 0.05, "SellZoneLowerBound")
	assertClose(t, result.SellZone.UpperBound, 351.6014, 0.05, "SellZoneUpperBound")
	assertClose(t, result.CurrentExpectedValue, 0.3214, 0.02, "CurrentExpectedValue")

	if result.SellZoneStatus != "In trim zone" {
		t.Fatalf("SellZoneStatus: got %s want %s", result.SellZoneStatus, "In trim zone")
	}

	// Verify zone bounds are properly ordered
	if result.SellZone.LowerBound >= result.SellZone.UpperBound {
		t.Errorf("SellZone LowerBound should be less than UpperBound")
	}

	// Verify ticker is set correctly
	if result.Ticker != "UNH" {
		t.Errorf("Ticker: got %s want %s", result.Ticker, "UNH")
	}
}

func TestCalculateSellZoneResult_ValidationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		ticker      string
		fairValue   float64
		probability float64
		downside    float64
		currentPrice float64
		wantErr     bool
	}{
		{"probability < 0", "X", 100, -0.1, -20, 90, true},
		{"probability > 1", "X", 100, 1.5, -20, 90, true},
		{"positive downside", "X", 100, 0.65, 10, 90, true},
		{"zero downside", "X", 100, 0.65, 0, 90, true},
		{"non-positive fair value", "X", 0, 0.65, -20, 90, true},
		{"negative fair value", "X", -100, 0.65, -20, 90, true},
		{"valid input", "X", 100, 0.65, -20, 90, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateSellZoneResult(tt.ticker, tt.fairValue, tt.probability, tt.downside, tt.currentPrice)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateSellZoneResult() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCalculateMetricsSetsSellZoneFields(t *testing.T) {
	t.Parallel()
	stock := models.Stock{
		CurrentPrice:        340,
		FairValue:           380,
		ProbabilityPositive: 0.65,
		DownsideRisk:        -15,
	}

	CalculateMetrics(&stock)

	if stock.SellZoneLowerBound <= 0 || stock.SellZoneUpperBound <= 0 {
		t.Fatalf("expected sell zone bounds to be set, got lower=%f upper=%f", stock.SellZoneLowerBound, stock.SellZoneUpperBound)
	}
	if stock.SellZoneLowerBound >= stock.SellZoneUpperBound {
		t.Fatalf("expected sell lower bound < upper bound, got lower=%f upper=%f", stock.SellZoneLowerBound, stock.SellZoneUpperBound)
	}
	// Verify sell zone status is set
	if stock.SellZoneStatus == "" {
		t.Errorf("expected SellZoneStatus to be set")
	}
}
func TestCalculateMetricsCalibratesDownsideRiskByBetaBuckets(t *testing.T) {
	tests := []struct {
		name string
		beta float64
		want float64
	}{
		{name: "below 0.5", beta: 0.4, want: -15},
		{name: "at 0.5", beta: 0.5, want: -20},
		{name: "at 1.0", beta: 1.0, want: -25},
		{name: "at 1.5", beta: 1.5, want: -30},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stock := models.Stock{
				Beta:         tc.beta,
				CurrentPrice: 100,
				FairValue:    120,
			}
			CalculateMetrics(&stock)
			assertClose(t, stock.DownsideRisk, tc.want, 0.0001, "DownsideRisk")
		})
	}
}
func TestCalculateMetricsUsesFallbackDownsideRiskWithoutBeta(t *testing.T) {
	stock := models.Stock{
		CurrentPrice: 100,
		FairValue:    120,
	}
	CalculateMetrics(&stock)
	assertClose(t, stock.DownsideRisk, -20, 0.0001, "DownsideRisk")
}
func TestCalculateMetricsUsesMinDownsideMagnitudeForBRatio(t *testing.T) {
	stock := models.Stock{
		CurrentPrice:        100,
		FairValue:           110,
		ProbabilityPositive: 0.5,
		DownsideRisk:        -0.01,
	}
	CalculateMetrics(&stock)
	assertClose(t, stock.BRatio, 100, 0.0001, "BRatio")
}
func TestCalculateMetricsCapsHalfKellySuggested(t *testing.T) {
	stock := models.Stock{
		CurrentPrice:        100,
		FairValue:           200,
		ProbabilityPositive: 0.9,
		DownsideRisk:        -10,
	}
	CalculateMetrics(&stock)
	assertClose(t, stock.HalfKellySuggested, 15, 0.0001, "HalfKellySuggested")
}
func TestCalculateMetricsAssessmentThresholds(t *testing.T) {
	tests := []struct {
		name       string
		fairValue  float64
		assessment string
	}{
		{name: "above 7 is add", fairValue: 125, assessment: "Add"},
		{name: "at 7 is hold", fairValue: 124, assessment: "Hold"},
		{name: "at 3 is hold", fairValue: 116, assessment: "Hold"},
		{name: "between 0 and 3 is trim", fairValue: 115.5, assessment: "Trim"},
		{name: "below 0 is sell", fairValue: 109, assessment: "Sell"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stock := models.Stock{
				CurrentPrice:        100,
				FairValue:           tc.fairValue,
				ProbabilityPositive: 0.5,
				DownsideRisk:        -10,
			}
			CalculateMetrics(&stock)
			if stock.Assessment != tc.assessment {
				t.Fatalf("Assessment: got %s want %s", stock.Assessment, tc.assessment)
			}
		})
	}
}
func TestCalculateMetricsResetsUpsidePotentialWhenPricesAreInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		currentPrice float64
		fairValue    float64
	}{
		{"zero current price", 0, 120},
		{"zero fair value", 100, 0},
		{"both zero", 0, 0},
		{"negative current price", -50, 120},
		{"negative fair value", 100, -50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stock := models.Stock{
				CurrentPrice:        tt.currentPrice,
				FairValue:           tt.fairValue,
				ProbabilityPositive: 0.6,
				DownsideRisk:        -20,
				UpsidePotential:     35,
				BuyZoneStatus:       "within buy zone",
			}
			CalculateMetrics(&stock)
			assertClose(t, stock.UpsidePotential, 0, 0.0001, "UpsidePotential")
			assertClose(t, stock.BRatio, 0, 0.0001, "BRatio")
			assertClose(t, stock.BuyZoneMin, 0, 0.0001, "BuyZoneMin")
			assertClose(t, stock.BuyZoneMax, 0, 0.0001, "BuyZoneMax")
			if stock.BuyZoneStatus != "no buy zone available" {
				t.Fatalf("BuyZoneStatus: got %s want %s", stock.BuyZoneStatus, "no buy zone available")
			}
		})
	}
}
func TestCalculateSellZoneResult_StatusClassifications(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		price      float64
		wantStatus string
	}{
		{name: "below sell zone", price: 300, wantStatus: "Below sell zone"},
		{name: "in trim zone", price: 350, wantStatus: "In trim zone"},
		{name: "in sell zone", price: 370, wantStatus: "In sell zone"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := CalculateSellZoneResult("UNH", 380, 0.65, -15, tc.price)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.SellZoneStatus != tc.wantStatus {
				t.Fatalf("SellZoneStatus: got %s want %s", result.SellZoneStatus, tc.wantStatus)
			}
			// Verify EV is consistent with status
			if tc.wantStatus == "Below sell zone" && result.CurrentExpectedValue <= 3 {
				t.Errorf("EV should be > 3 for 'Below sell zone', got %.2f", result.CurrentExpectedValue)
			}
			if tc.wantStatus == "In trim zone" && (result.CurrentExpectedValue <= 0 || result.CurrentExpectedValue > 3) {
				t.Errorf("EV should be 0 < EV <= 3 for 'In trim zone', got %.2f", result.CurrentExpectedValue)
			}
			if tc.wantStatus == "In sell zone" && result.CurrentExpectedValue > 0 {
				t.Errorf("EV should be <= 0 for 'In sell zone', got %.2f", result.CurrentExpectedValue)
			}
		})
	}
}
func TestCalculateSellZoneResult_NoZoneWhenProbabilityIsZero(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		probability float64
		downside    float64
	}{
		{"probability zero", 0, -20},
		{"probability one", 1, -20},
		{"extreme downside", 0.5, -99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CalculateSellZoneResult("X", 100, tt.probability, tt.downside, 90)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// These edge cases may result in invalid zones
			if result.SellZoneStatus == "no sell zone" {
				assertClose(t, result.SellZone.LowerBound, 0, 0.0001, "SellZoneLowerBound")
				assertClose(t, result.SellZone.UpperBound, 0, 0.0001, "SellZoneUpperBound")
			}
		})
	}
}
func TestCalculatePortfolioMetricsSkipsStocksWithoutUsableFXRate(t *testing.T) {
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
			SharesOwned:   10,
			CurrentPrice:  100,
			Currency:      "JPY",
			ExpectedValue: 15,
			Volatility:    30,
			Sector:        "Ignored",
		},
	}
	fxRates := map[string]float64{"USD": 1}
	metrics := CalculatePortfolioMetrics(stocks, fxRates)
	assertClose(t, metrics.TotalValue, 1000, 0.01, "TotalValue")
	assertClose(t, metrics.OverallEV, 5, 0.01, "OverallEV")
	assertClose(t, metrics.WeightedVolatility, 10, 0.01, "WeightedVolatility")
	assertClose(t, metrics.SharpeRatio, 0.1, 0.01, "SharpeRatio")
	assertClose(t, metrics.KellyUtilization, 100, 0.01, "KellyUtilization")
	assertClose(t, metrics.SectorWeights["Tech"], 1, 0.0001, "SectorWeights[Tech]")
	// Verify JPY stock is excluded from sector weights
	if _, exists := metrics.SectorWeights["Ignored"]; exists {
		t.Errorf("Sector weights should not include stocks without usable FX rate")
	}
}

func TestCalculatePortfolioMetricsEmptyPortfolio(t *testing.T) {
	t.Parallel()
	metrics := CalculatePortfolioMetrics([]models.Stock{}, map[string]float64{"USD": 1})
	assertClose(t, metrics.TotalValue, 0, 0.01, "TotalValue")
	assertClose(t, metrics.OverallEV, 0, 0.01, "OverallEV")
	assertClose(t, metrics.WeightedVolatility, 0, 0.01, "WeightedVolatility")
	if len(metrics.SectorWeights) != 0 {
		t.Errorf("Empty portfolio should have no sector weights, got %d", len(metrics.SectorWeights))
	}
}

func TestCalculatePortfolioMetricsMultipleSectorsWithDifferentWeights(t *testing.T) {
	t.Parallel()
	stocks := []models.Stock{
		{
			SharesOwned:   100,
			CurrentPrice:  10,
			Currency:      "USD",
			ExpectedValue: 8,
			Volatility:    15,
			Sector:        "Technology",
			Weight:        50,
		},
		{
			SharesOwned:   50,
			CurrentPrice:  10,
			Currency:      "USD",
			ExpectedValue: 5,
			Volatility:    10,
			Sector:        "Healthcare",
			Weight:        25,
		},
		{
			SharesOwned:   50,
			CurrentPrice:  10,
			Currency:      "USD",
			ExpectedValue: 3,
			Volatility:    8,
			Sector:        "Financials",
			Weight:        25,
		},
	}
	fxRates := map[string]float64{"USD": 1}
	metrics := CalculatePortfolioMetrics(stocks, fxRates)

	assertClose(t, metrics.TotalValue, 2000, 0.01, "TotalValue")
	// Weighted EV: (1000/2000)*8 + (500/2000)*5 + (500/2000)*3 = 4 + 1.25 + 0.75 = 6
	assertClose(t, metrics.OverallEV, 6, 0.01, "OverallEV")

	// Verify sector weights sum to 1
	sumWeights := 0.0
	for _, weight := range metrics.SectorWeights {
		sumWeights += weight
	}
	assertClose(t, sumWeights, 1.0, 0.0001, "Sum of sector weights")

	// Verify correct sector allocations
	assertClose(t, metrics.SectorWeights["Technology"], 0.5, 0.0001, "SectorWeights[Technology]")
	assertClose(t, metrics.SectorWeights["Healthcare"], 0.25, 0.0001, "SectorWeights[Healthcare]")
	assertClose(t, metrics.SectorWeights["Financials"], 0.25, 0.0001, "SectorWeights[Financials]")
}
