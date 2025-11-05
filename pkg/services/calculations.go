package services

import (
	"math"

	"github.com/artpro/assessapp/pkg/models"
)

// CalculateMetrics calculates all derived metrics for a stock
// These formulas implement the investment strategy's Kelly criterion and EV approach
func CalculateMetrics(stock *models.Stock) {
	// Calculate Upside Potential (%)
	// Formula: ((Fair Value - Current Price) / Current Price) * 100
	if stock.CurrentPrice > 0 {
		stock.UpsidePotential = ((stock.FairValue - stock.CurrentPrice) / stock.CurrentPrice) * 100
	}

	// Calculate Expected Value (EV) (%)
	// Formula: (p * Upside %) + ((1 - p) * Downside %)
	// This is the probabilistic edge - the core decision metric
	stock.ExpectedValue = (stock.ProbabilityPositive * stock.UpsidePotential) +
		((1 - stock.ProbabilityPositive) * stock.DownsideRisk)

	// Calculate b ratio (Upside/Downside ratio)
	// Formula: Upside % / |Downside %|
	if stock.DownsideRisk != 0 {
		stock.BRatio = stock.UpsidePotential / math.Abs(stock.DownsideRisk)
	}

	// Calculate Kelly Fraction (f*)
	// Formula: ((b * p) - (1 - p)) / b
	// This is the optimal betting fraction according to Kelly criterion
	if stock.BRatio > 0 {
		stock.KellyFraction = ((stock.BRatio * stock.ProbabilityPositive) - (1 - stock.ProbabilityPositive)) / stock.BRatio
		stock.KellyFraction = stock.KellyFraction * 100 // Convert to percentage
	}

	// Calculate Half-Kelly Suggested Weight (%)
	// Formula: f* / 2, capped at 15%
	// Using half-Kelly for more conservative sizing
	stock.HalfKellySuggested = stock.KellyFraction / 2
	if stock.HalfKellySuggested > 15 {
		stock.HalfKellySuggested = 15 // Cap at 15% max position size
	}

	// Determine Assessment based on EV
	// Strategy rules: EV > 7% = Add, EV > 0% = Hold, EV < 0% = Trim/Sell
	if stock.ExpectedValue > 7 {
		stock.Assessment = "Add"
	} else if stock.ExpectedValue > 0 {
		stock.Assessment = "Hold"
	} else if stock.ExpectedValue > -5 {
		stock.Assessment = "Trim"
	} else {
		stock.Assessment = "Sell"
	}

	// Calculate Buy Zone (approximate range where EV > 7%)
	// This is a simplified calculation - could be refined with more complex modeling
	if stock.FairValue > 0 && stock.ProbabilityPositive > 0 {
		// Find price where EV would be ~15% (attractive entry)
		// Working backwards from EV formula: EV = p * ((FV - P)/P * 100) + (1-p) * downside
		// For attractive entry, we want EV >= 15%
		targetEV := 15.0

		// Calculate the price where upside potential gives us target EV
		// Assuming downside risk stays proportional to current estimate
		// targetEV = p * upside + (1-p) * downside
		// Solve for upside: upside = (targetEV - (1-p)*downside) / p
		requiredUpside := (targetEV - (1-stock.ProbabilityPositive)*stock.DownsideRisk) / stock.ProbabilityPositive

		// upside = ((FV - P) / P) * 100, solve for P
		// P = FV / (1 + upside/100)
		if requiredUpside > 0 {
			stock.BuyZoneMax = stock.FairValue / (1 + requiredUpside/100)
			stock.BuyZoneMin = stock.BuyZoneMax * 0.90 // 10% range below max
		} else {
			// Fallback to simple percentage if calculation doesn't work
			stock.BuyZoneMin = stock.CurrentPrice * 0.85
			stock.BuyZoneMax = stock.CurrentPrice * 0.95
		}
	}
}

// CalculatePortfolioMetrics calculates portfolio-level metrics
func CalculatePortfolioMetrics(stocks []models.Stock, fxRates map[string]float64) PortfolioMetrics {
	var totalValue float64
	var weightedEV float64
	var weightedVolatility float64
	sectorWeights := make(map[string]float64)

	for _, stock := range stocks {
		// Calculate position value in USD
		fxRate := fxRates[stock.Currency]
		if fxRate == 0 {
			fxRate = 1.0 // Default to 1 if no rate available (assume USD)
		}

		valueUSD := float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
		totalValue += valueUSD

		// Accumulate weighted metrics
		weight := valueUSD / totalValue
		weightedEV += stock.ExpectedValue * weight
		weightedVolatility += stock.Volatility * weight

		// Accumulate sector weights
		sectorWeights[stock.Sector] += weight * 100
	}

	// Calculate Sharpe Ratio (simplified: EV / Volatility)
	sharpeRatio := 0.0
	if weightedVolatility > 0 {
		sharpeRatio = weightedEV / weightedVolatility
	}

	// Calculate Kelly Utilization (sum of half-Kelly weights)
	kellyUtilization := 0.0
	for _, stock := range stocks {
		fxRate := fxRates[stock.Currency]
		if fxRate == 0 {
			fxRate = 1.0
		}
		valueUSD := float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
		weight := (valueUSD / totalValue) * 100
		kellyUtilization += weight
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
