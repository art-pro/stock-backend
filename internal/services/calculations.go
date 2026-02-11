package services

import (
	"math"

	"github.com/art-pro/stock-backend/internal/models"
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
}

// CalculatePortfolioMetrics calculates portfolio-level metrics
func CalculatePortfolioMetrics(stocks []models.Stock, fxRates map[string]float64) PortfolioMetrics {
	var totalValue float64
	stockValues := make([]float64, len(stocks))

	// First pass: calculate total portfolio value in USD.
	for i, stock := range stocks {
		if stock.SharesOwned <= 0 {
			continue
		}

		fxRate := fxRates[stock.Currency]
		if fxRate == 0 {
			fxRate = 1.0 // Default to EUR base if no rate is available.
		}

		valueEUR := float64(stock.SharesOwned) * stock.CurrentPrice / fxRate
		stockValues[i] = valueEUR
		totalValue += valueEUR
	}

	var weightedEV float64
	var weightedVolatility float64
	sectorWeights := make(map[string]float64)
	kellyUtilization := 0.0

	// Second pass: calculate weighted aggregates.
	for i, stock := range stocks {
		if stock.SharesOwned <= 0 {
			continue
		}

		if totalValue > 0 && stockValues[i] > 0 {
			weight := stockValues[i] / totalValue
			weightedEV += stock.ExpectedValue * weight
			weightedVolatility += stock.Volatility * weight
			sectorWeights[stock.Sector] += weight * 100
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
