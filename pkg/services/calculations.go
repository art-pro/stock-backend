package services

import (
	"fmt"
	"math"

	"github.com/art-pro/stock-backend/pkg/models"
)

const (
	defaultProbabilityPositive = 0.65
	riskFreeRatePercent        = 4.0
	minDownsideMagnitude       = 0.1
)

func calibrateDownsideRisk(beta float64) float64 {
	if beta < 0.5 {
		return -15.0
	}
	if beta < 1.0 {
		return -20.0
	}
	if beta < 1.5 {
		return -25.0
	}
	return -30.0
}

// CalculateMetrics calculates all derived metrics for a stock
// These formulas implement the investment strategy's Kelly criterion and EV approach
func CalculateMetrics(stock *models.Stock) {
	// 1. Calibrate downside risk based on beta, unless explicitly provided.
	if stock.DownsideRisk == 0 {
		if stock.Beta > 0 {
			stock.DownsideRisk = calibrateDownsideRisk(stock.Beta)
		} else {
			stock.DownsideRisk = -20.0
		}
	}

	// 2. Upside Potential = ((Fair Value - Current Price) / Current Price) * 100
	if stock.CurrentPrice > 0 && stock.FairValue > 0 {
		stock.UpsidePotential = ((stock.FairValue - stock.CurrentPrice) / stock.CurrentPrice) * 100
	}

	// 3. Use conservative default probability when missing/invalid.
	if stock.ProbabilityPositive <= 0 || stock.ProbabilityPositive > 1 {
		stock.ProbabilityPositive = defaultProbabilityPositive
	}

	// 4. b Ratio = Upside % / |Downside %| with a small floor on downside.
	downsideMagnitude := math.Abs(stock.DownsideRisk)
	if downsideMagnitude < minDownsideMagnitude {
		downsideMagnitude = minDownsideMagnitude
	}
	stock.BRatio = stock.UpsidePotential / downsideMagnitude

	// 5. Expected Value (EV) = (p * Upside %) + ((1 - p) * Downside %)
	stock.ExpectedValue = (stock.ProbabilityPositive * stock.UpsidePotential) +
		((1 - stock.ProbabilityPositive) * stock.DownsideRisk)

	// 6. Kelly f* = ((b * p) - (1 - p)) / b, expressed in percent and clamped at 0.
	if stock.BRatio > 0 {
		stock.KellyFraction = ((stock.BRatio*stock.ProbabilityPositive)-(1-stock.ProbabilityPositive))/stock.BRatio * 100
		if stock.KellyFraction < 0 {
			stock.KellyFraction = 0
		}
	} else {
		stock.KellyFraction = 0
	}

	// 7. Half-Kelly suggested weight, capped at 15%.
	stock.HalfKellySuggested = stock.KellyFraction / 2
	if stock.HalfKellySuggested > 15 {
		stock.HalfKellySuggested = 15
	}

	// 8. Assessment thresholds under a conservative EV policy.
	if stock.ExpectedValue > 7 {
		stock.Assessment = "Add"
	} else if stock.ExpectedValue >= 3 && stock.ExpectedValue <= 7 {
		stock.Assessment = "Hold"
	} else if stock.ExpectedValue >= 0 && stock.ExpectedValue < 3 {
		stock.Assessment = "Trim"
	} else {
		stock.Assessment = "Sell"
	}

	// 9. Buy zone uses EV >= 7% entry threshold.
	if stock.FairValue > 0 && stock.ProbabilityPositive > 0 {
		targetEV := 7.0
		requiredUpside := (targetEV - (1-stock.ProbabilityPositive)*stock.DownsideRisk) / stock.ProbabilityPositive

		if requiredUpside > -100 {
			stock.BuyZoneMax = stock.FairValue / (1 + requiredUpside/100)
			stock.BuyZoneMin = stock.BuyZoneMax * 0.90 // 10% range below max
		} else {
			stock.BuyZoneMin = stock.CurrentPrice * 0.85
			stock.BuyZoneMax = stock.CurrentPrice * 0.95
		}
	}

	// 10. Sell zone thresholds:
	// - lower bound: EV = 3% (trim zone start)
	// - upper bound: EV = 0% (sell zone start)
	sellLowerBound, okTrim := solvePriceForEVThreshold(stock.FairValue, stock.ProbabilityPositive, stock.DownsideRisk, 3)
	sellUpperBound, okSell := solvePriceForEVThreshold(stock.FairValue, stock.ProbabilityPositive, stock.DownsideRisk, 0)
	if okTrim && okSell && sellLowerBound < sellUpperBound {
		stock.SellZoneLowerBound = sellLowerBound
		stock.SellZoneUpperBound = sellUpperBound
		switch {
		case stock.ExpectedValue > 3:
			stock.SellZoneStatus = "Below sell zone"
		case stock.ExpectedValue > 0:
			stock.SellZoneStatus = "In trim zone"
		default:
			stock.SellZoneStatus = "In sell zone"
		}
	} else {
		stock.SellZoneLowerBound = 0
		stock.SellZoneUpperBound = 0
		stock.SellZoneStatus = "no sell zone"
	}
}

// CalculatePortfolioMetrics calculates portfolio-level metrics
func CalculatePortfolioMetrics(stocks []models.Stock, fxRates map[string]float64) PortfolioMetrics {
	var totalValue float64
	stockValues := make([]float64, len(stocks))

	// First pass: Calculate total portfolio value
	for i, stock := range stocks {
		// Skip stocks with no shares owned
		if stock.SharesOwned <= 0 {
			continue
		}

		// Convert position value to EUR (rates are stored as currency per 1 EUR).
		fxRate := fxRates[stock.Currency]
		if fxRate <= 0 {
			continue
		}

		valueEUR := float64(stock.SharesOwned) * stock.CurrentPrice / fxRate
		stockValues[i] = valueEUR
		totalValue += valueEUR
	}

	// Second pass: Calculate weighted metrics with correct total
	var weightedEV float64
	var weightedVolatility float64
	sectorWeights := make(map[string]float64)
	kellyUtilization := 0.0

	for i, stock := range stocks {
		// Skip stocks with no shares owned
		if stock.SharesOwned <= 0 {
			continue
		}

		if totalValue > 0 && stockValues[i] > 0 {
			weight := stockValues[i] / totalValue
			weightedEV += stock.ExpectedValue * weight
			weightedVolatility += stock.Volatility * weight

			// Accumulate sector weights
			sectorWeights[stock.Sector] += weight * 100

			// Kelly utilization is sum of actual weights
			kellyUtilization += weight * 100
		}
	}

	// Sharpe Ratio = (Rp - Rf) / sigma, using weighted EV as Rp proxy.
	sharpeRatio := 0.0
	if weightedVolatility > 0 {
		sharpeRatio = (weightedEV - riskFreeRatePercent) / weightedVolatility
	}

	return PortfolioMetrics{
		TotalValue:         totalValue,
		OverallEV:          weightedEV,
		WeightedVolatility: weightedVolatility,
		SharpeRatio:        sharpeRatio,
		KellyUtilization:   kellyUtilization,
		SectorWeights:      sectorWeights,
	}
}

// PortfolioMetrics holds portfolio-level aggregated metrics
type PortfolioMetrics struct {
	TotalValue         float64            `json:"total_value"`
	OverallEV          float64            `json:"overall_ev"`
	WeightedVolatility float64            `json:"weighted_volatility"`
	SharpeRatio        float64            `json:"sharpe_ratio"`
	KellyUtilization   float64            `json:"kelly_utilization"`
	SectorWeights      map[string]float64 `json:"sector_weights"`
}

type BuyZone struct {
	LowerBound float64 `json:"lower_bound"`
	UpperBound float64 `json:"upper_bound"`
}

type BuyZoneCalculationResult struct {
	Ticker              string  `json:"ticker"`
	FairValue           float64 `json:"fair_value"`
	ProbabilityPositive float64 `json:"probability_positive"`
	DownsideRisk        float64 `json:"downside_risk"`
	BuyZone             BuyZone `json:"buy_zone"`
	CurrentExpectedValue float64 `json:"current_expected_value"`
	ZoneStatus          string  `json:"zone_status,omitempty"`
}

type SellZone struct {
	LowerBound float64 `json:"sell_zone_lower_bound"`
	UpperBound float64 `json:"sell_zone_upper_bound"`
}

type SellZoneCalculationResult struct {
	Ticker               string   `json:"ticker"`
	FairValue            float64  `json:"fair_value"`
	ProbabilityPositive  float64  `json:"probability_positive"`
	DownsideRisk         float64  `json:"downside_risk"`
	SellZone             SellZone `json:"sell_zone"`
	CurrentExpectedValue float64  `json:"current_expected_value"`
	SellZoneStatus       string   `json:"sell_zone_status,omitempty"`
}

// CalculateBuyZoneResult calculates buy-zone bounds from EV thresholds and returns
// the current EV plus status classification for a provided current price.
func CalculateBuyZoneResult(
	ticker string,
	fairValue float64,
	probabilityPositive float64,
	downsideRisk float64,
	currentPrice float64,
) (BuyZoneCalculationResult, error) {
	result := BuyZoneCalculationResult{
		Ticker:              ticker,
		FairValue:           fairValue,
		ProbabilityPositive: probabilityPositive,
		DownsideRisk:        downsideRisk,
	}

	if probabilityPositive < 0 || probabilityPositive > 1 {
		return result, fmt.Errorf("probability_positive must be between 0 and 1")
	}
	if downsideRisk >= 0 {
		return result, fmt.Errorf("downside_risk must be negative")
	}
	if fairValue <= 0 {
		return result, fmt.Errorf("fair_value must be positive")
	}

	lowerBound, okLower := solvePriceForEVThreshold(fairValue, probabilityPositive, downsideRisk, 15)
	upperBound, okUpper := solvePriceForEVThreshold(fairValue, probabilityPositive, downsideRisk, 7)
	if !okLower || !okUpper {
		result.ZoneStatus = "no buy zone available"
		return result, nil
	}

	result.BuyZone = BuyZone{
		LowerBound: lowerBound,
		UpperBound: upperBound,
	}

	if result.BuyZone.LowerBound > result.BuyZone.UpperBound {
		result.ZoneStatus = "no buy zone available"
		return result, nil
	}

	if currentPrice > 0 {
		result.CurrentExpectedValue = expectedValueAtPrice(fairValue, probabilityPositive, downsideRisk, currentPrice)
		switch {
		case currentPrice < result.BuyZone.LowerBound:
			result.ZoneStatus = "EV >> 15%"
		case currentPrice <= result.BuyZone.UpperBound:
			result.ZoneStatus = "within buy zone"
		default:
			result.ZoneStatus = "outside buy zone"
		}
	}

	return result, nil
}

// CalculateSellZoneResult calculates sell-zone bounds from EV thresholds and returns
// current EV plus classification for trim/sell actioning.
func CalculateSellZoneResult(
	ticker string,
	fairValue float64,
	probabilityPositive float64,
	downsideRisk float64,
	currentPrice float64,
) (SellZoneCalculationResult, error) {
	result := SellZoneCalculationResult{
		Ticker:              ticker,
		FairValue:           fairValue,
		ProbabilityPositive: probabilityPositive,
		DownsideRisk:        downsideRisk,
	}

	if probabilityPositive < 0 || probabilityPositive > 1 {
		return result, fmt.Errorf("probability_positive must be between 0 and 1")
	}
	if downsideRisk >= 0 {
		return result, fmt.Errorf("downside_risk must be negative")
	}
	if fairValue <= 0 {
		return result, fmt.Errorf("fair_value must be positive")
	}

	trimPrice, okTrim := solvePriceForEVThreshold(fairValue, probabilityPositive, downsideRisk, 3)
	sellPrice, okSell := solvePriceForEVThreshold(fairValue, probabilityPositive, downsideRisk, 0)
	if !okTrim || !okSell || trimPrice >= sellPrice {
		result.SellZoneStatus = "no sell zone"
		return result, nil
	}

	result.SellZone = SellZone{
		LowerBound: trimPrice,
		UpperBound: sellPrice,
	}

	if currentPrice > 0 {
		result.CurrentExpectedValue = expectedValueAtPrice(fairValue, probabilityPositive, downsideRisk, currentPrice)
		switch {
		case result.CurrentExpectedValue > 3:
			result.SellZoneStatus = "Below sell zone"
		case result.CurrentExpectedValue > 0:
			result.SellZoneStatus = "In trim zone"
		default:
			result.SellZoneStatus = "In sell zone"
		}
	}

	return result, nil
}

func expectedValueAtPrice(fairValue, probabilityPositive, downsideRisk, currentPrice float64) float64 {
	if currentPrice <= 0 {
		return 0
	}
	upsidePercent := ((fairValue - currentPrice) / currentPrice) * 100
	return (probabilityPositive * upsidePercent) + ((1 - probabilityPositive) * downsideRisk)
}

func solvePriceForEVThreshold(
	fairValue float64,
	probabilityPositive float64,
	downsideRisk float64,
	evThreshold float64,
) (float64, bool) {
	downsideMagnitude := math.Abs(downsideRisk)
	denominator := evThreshold + (100 * probabilityPositive) + ((1 - probabilityPositive) * downsideMagnitude)
	if denominator <= 0 {
		return 0, false
	}

	price := (100 * probabilityPositive * fairValue) / denominator
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0, false
	}
	return price, true
}
