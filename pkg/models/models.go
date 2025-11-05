package models

import (
	"time"

	"gorm.io/gorm"
)

// User represents the admin user
type User struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Username  string    `gorm:"unique;not null" json:"username"`
	Password  string    `gorm:"not null" json:"-"` // Password hash, never expose in JSON
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Stock represents a stock in the portfolio with all tracking metrics
type Stock struct {
	ID                     uint      `gorm:"primarykey" json:"id"`
	Ticker                 string    `gorm:"not null;index" json:"ticker"`
	CompanyName            string    `gorm:"not null" json:"company_name"`
	Sector                 string    `json:"sector"`
	CurrentPrice           float64   `json:"current_price"`            // In local currency
	Currency               string    `json:"currency"`                 // Local currency (DKK, EUR, USD, etc.)
	FairValue              float64   `json:"fair_value"`               // Consensus target in local currency
	UpsidePotential        float64   `json:"upside_potential"`         // Percentage
	DownsideRisk           float64   `json:"downside_risk"`            // Percentage (negative)
	ProbabilityPositive    float64   `json:"probability_positive"`     // p value (0-1)
	ExpectedValue          float64   `json:"expected_value"`           // EV percentage
	Beta                   float64   `json:"beta"`
	Volatility             float64   `json:"volatility"`               // Sigma percentage
	PERatio                float64   `json:"pe_ratio"`
	EPSGrowthRate          float64   `json:"eps_growth_rate"`          // Percentage
	DebtToEBITDA           float64   `json:"debt_to_ebitda"`
	DividendYield          float64   `json:"dividend_yield"`           // Percentage
	BRatio                 float64   `json:"b_ratio"`                  // Upside/Downside ratio
	KellyFraction          float64   `json:"kelly_fraction"`           // f* percentage
	HalfKellySuggested     float64   `json:"half_kelly_suggested"`     // ½-Kelly percentage (capped at 15%)
	SharesOwned            int       `json:"shares_owned"`
	AvgPriceLocal          float64   `json:"avg_price_local"`          // Entry cost in local currency
	CurrentValueUSD        float64   `json:"current_value_usd"`        // Position value in USD
	Weight                 float64   `json:"weight"`                   // Portfolio allocation percentage
	UnrealizedPnL          float64   `json:"unrealized_pnl"`           // In USD
	BuyZoneMin             float64   `json:"buy_zone_min"`             // Minimum price for buy zone
	BuyZoneMax             float64   `json:"buy_zone_max"`             // Maximum price for buy zone
	Assessment             string    `json:"assessment"`               // Hold/Add/Trim/Sell
	UpdateFrequency        string    `json:"update_frequency"`         // daily/weekly/monthly
	LastUpdated            time.Time `json:"last_updated"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// StockHistory stores historical calculation data for each stock
type StockHistory struct {
	ID                  uint      `gorm:"primarykey" json:"id"`
	StockID             uint      `gorm:"not null;index" json:"stock_id"`
	Ticker              string    `json:"ticker"`
	CurrentPrice        float64   `json:"current_price"`
	FairValue           float64   `json:"fair_value"`
	UpsidePotential     float64   `json:"upside_potential"`
	DownsideRisk        float64   `json:"downside_risk"`
	ProbabilityPositive float64   `json:"probability_positive"`
	ExpectedValue       float64   `json:"expected_value"`
	KellyFraction       float64   `json:"kelly_fraction"`
	Weight              float64   `json:"weight"`
	Assessment          string    `json:"assessment"`
	RecordedAt          time.Time `gorm:"index" json:"recorded_at"`
}

// DeletedStock stores soft-deleted stocks in a log
type DeletedStock struct {
	ID           uint           `gorm:"primarykey" json:"id"`
	StockData    string         `gorm:"type:text" json:"stock_data"` // JSON serialized Stock object
	Ticker       string         `gorm:"index" json:"ticker"`
	CompanyName  string         `json:"company_name"`
	Reason       string         `json:"reason"` // Optional deletion reason
	DeletedAt    time.Time      `json:"deleted_at"`
	DeletedBy    string         `json:"deleted_by"` // Username
	RestoredAt   *time.Time     `json:"restored_at,omitempty"`
}

// PortfolioSettings stores portfolio-level configuration
type PortfolioSettings struct {
	ID                  uint      `gorm:"primarykey" json:"id"`
	TotalPortfolioValue float64   `json:"total_portfolio_value"` // In USD
	UpdateFrequency     string    `json:"update_frequency"`      // daily/weekly/monthly
	LastUpdateRun       time.Time `json:"last_update_run"`
	AlertsEnabled       bool      `json:"alerts_enabled"`
	AlertThresholdEV    float64   `json:"alert_threshold_ev"`    // Alert when EV changes by this %
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Alert represents an alert that was triggered
type Alert struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	StockID     uint      `json:"stock_id"`
	Ticker      string    `json:"ticker"`
	AlertType   string    `json:"alert_type"`   // ev_change, buy_zone, etc.
	Message     string    `json:"message"`
	EmailSent   bool      `json:"email_sent"`
	CreatedAt   time.Time `json:"created_at"`
}

// BeforeCreate hook for Stock to set defaults
func (s *Stock) BeforeCreate(tx *gorm.DB) error {
	if s.UpdateFrequency == "" {
		s.UpdateFrequency = "daily"
	}
	if s.Currency == "" {
		s.Currency = "USD"
	}
	return nil
}

