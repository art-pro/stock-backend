// Package models: sector taxonomy for portfolio summary and stock.sector.
// See DATA_CONTRACT.md for full semantics. Keep in sync with frontend sector targets (e.g. lib/sectorTargets.ts).

package models

// CanonicalSectors is the list of sector names used in sector_weights and stock.sector.
// Frontend matches these (case-insensitive) to target ranges. Unmatched sectors are allowed but show no target.
var CanonicalSectors = []string{
	"Technology",
	"Insurance",
	"Industrials",
	"Healthcare",
	"Financials",
	"Financial Services",
	"Energy",
	"Crypto",
	"Consumer Defensive",
	"Consumer Cyclical",
	"Communication Services",
	"Basic Materials",
}

// NormalizeSector maps common provider variants to a canonical name when possible.
// Returns the input unchanged if no mapping exists. Used when ingesting from Alpha Vantage, Grok, etc.
func NormalizeSector(s string) string {
	if s == "" {
		return s
	}
	switch s {
	case "Tech":
		return "Technology"
	case "Health", "Health Care", "Healthcare":
		return "Healthcare"
	case "Financial":
		return "Financials"
	case "Industrial":
		return "Industrials"
	case "Consumer Staples":
		return "Consumer Defensive"
	case "Consumer Discretionary":
		return "Consumer Cyclical"
	case "Communications", "Telecom":
		return "Communication Services"
	case "Materials":
		return "Basic Materials"
	default:
		return s
	}
}
