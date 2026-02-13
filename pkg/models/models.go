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

// UserSettings stores user-specific UI settings like column visibility
type UserSettings struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_user_settings_uk" json:"user_id"`
	Key       string    `gorm:"not null;uniqueIndex:idx_user_settings_uk" json:"key"` // e.g., "stock_table_columns"
	Value     string    `gorm:"type:text" json:"value"`                               // JSON string
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Portfolio represents a user's portfolio
type Portfolio struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	Description string    `json:"description"`
	IsDefault   bool      `gorm:"default:false" json:"is_default"`
	TotalValue  float64   `json:"total_value"`
	UserID      uint      `gorm:"not null;index" json:"user_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Stock represents a stock in the portfolio with all tracking metrics
type Stock struct {
	ID                    uint       `gorm:"primarykey" json:"id"`
	PortfolioID           uint       `gorm:"not null;index" json:"portfolio_id"`
	Ticker                string     `gorm:"not null;index" json:"ticker"`
	ISIN                  string     `gorm:"index" json:"isin"` // International Securities Identification Number
	CompanyName           string     `gorm:"not null" json:"company_name"`
	Sector                string     `json:"sector"`
	CurrentPrice          float64    `json:"current_price"`        // In local currency
	Currency              string     `json:"currency"`             // Local currency (DKK, EUR, USD, etc.)
	FairValue             float64    `json:"fair_value"`           // Consensus target in local currency
	UpsidePotential       float64    `json:"upside_potential"`     // Percentage
	DownsideRisk          float64    `json:"downside_risk"`        // Percentage (negative)
	ProbabilityPositive   float64    `json:"probability_positive"` // p value (0-1)
	ExpectedValue         float64    `json:"expected_value"`       // EV percentage
	Beta                  float64    `json:"beta"`
	Volatility            float64    `json:"volatility"` // Sigma percentage
	PERatio               float64    `json:"pe_ratio"`
	EPSGrowthRate         float64    `json:"eps_growth_rate"` // Percentage
	DebtToEBITDA          float64    `json:"debt_to_ebitda"`
	DividendYield         float64    `json:"dividend_yield"`       // Percentage
	BRatio                float64    `json:"b_ratio"`              // Upside/Downside ratio
	KellyFraction         float64    `json:"kelly_fraction"`       // f* percentage
	HalfKellySuggested    float64    `json:"half_kelly_suggested"` // ½-Kelly percentage (capped at 15%)
	SharesOwned           int        `json:"shares_owned"`
	AvgPriceLocal         float64    `json:"avg_price_local"`                         // Entry cost in local currency
	CurrentValueUSD       float64    `json:"current_value_usd"`                       // Position value in USD
	Weight                float64    `json:"weight"`                                  // Portfolio allocation percentage
	UnrealizedPnL         float64    `json:"unrealized_pnl"`                          // In USD
	BuyZoneMin            float64    `json:"buy_zone_min"`                            // Minimum price for buy zone
	BuyZoneMax            float64    `json:"buy_zone_max"`                            // Maximum price for buy zone
	SellZoneLowerBound    float64    `json:"sell_zone_lower_bound"`                   // Trim zone start (EV = 3%)
	SellZoneUpperBound    float64    `json:"sell_zone_upper_bound"`                   // Sell zone start (EV = 0%)
	SellZoneStatus        string     `json:"sell_zone_status"`                        // Below/In trim/In sell zone
	Assessment            string     `json:"assessment"`                              // Hold/Add/Trim/Sell
	UpdateFrequency       string     `json:"update_frequency"`                        // daily/weekly/monthly/manually
	DataSource            string     `json:"data_source"`                             // Source of data (e.g., "Grok", "Alpha Vantage", "Manual")
	FairValueSource       string     `json:"fair_value_source"`                       // Source of fair value (e.g., "TipRanks, Nov 5, 2025")
	AlphaVantageFetchedAt *time.Time `json:"alpha_vantage_fetched_at"`                // When data was last fetched from Alpha Vantage
	GrokFetchedAt         *time.Time `json:"grok_fetched_at"`                         // When data was last fetched from Grok
	AlphaVantageRawJSON   string     `gorm:"type:text" json:"alpha_vantage_raw_json"` // Raw JSON response from Alpha Vantage
	GrokRawJSON           string     `gorm:"type:text" json:"grok_raw_json"`          // Raw JSON response from Grok
	Comment               string     `gorm:"type:text" json:"comment"`                // User notes and memos for this stock
	LastUpdated           time.Time  `json:"last_updated"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// StockHistory stores historical calculation data for each stock
type StockHistory struct {
	ID                  uint      `gorm:"primarykey" json:"id"`
	StockID             uint      `gorm:"not null;index" json:"stock_id"`
	PortfolioID         uint      `gorm:"not null;index" json:"portfolio_id"`
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

// FairValueHistory stores source-level fair value observations for each stock.
type FairValueHistory struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	StockID     uint      `gorm:"not null;index" json:"stock_id"`
	PortfolioID uint      `gorm:"not null;index" json:"portfolio_id"`
	Ticker      string    `gorm:"index" json:"ticker"`
	FairValue   float64   `json:"fair_value"`
	Source      string    `gorm:"not null" json:"source"`
	RecordedAt  time.Time `gorm:"index" json:"recorded_at"`
}

// DeletedStock stores soft-deleted stocks in a log
type DeletedStock struct {
	ID          uint       `gorm:"primarykey" json:"id"`
	PortfolioID uint       `gorm:"not null;index" json:"portfolio_id"`
	StockData   string     `gorm:"type:text" json:"stock_data"` // JSON serialized Stock object
	Ticker      string     `gorm:"index" json:"ticker"`
	CompanyName string     `json:"company_name"`
	Reason      string     `json:"reason"` // Optional deletion reason
	DeletedAt   time.Time  `json:"deleted_at"`
	DeletedBy   string     `json:"deleted_by"` // Username
	RestoredAt  *time.Time `json:"restored_at,omitempty"`
}

// PortfolioSettings stores portfolio-level configuration
type PortfolioSettings struct {
	ID                  uint      `gorm:"primarykey" json:"id"`
	PortfolioID         uint      `gorm:"not null;index" json:"portfolio_id"`
	TotalPortfolioValue float64   `json:"total_portfolio_value"` // In USD
	UpdateFrequency     string    `json:"update_frequency"`      // daily/weekly/monthly
	LastUpdateRun       time.Time `json:"last_update_run"`
	AlertsEnabled       bool      `json:"alerts_enabled"`
	AlertThresholdEV    float64   `json:"alert_threshold_ev"` // Alert when EV changes by this %
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Alert represents an alert that was triggered
type Alert struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	PortfolioID uint      `gorm:"not null;index" json:"portfolio_id"`
	StockID     uint      `json:"stock_id"`
	Ticker      string    `json:"ticker"`
	AlertType   string    `json:"alert_type"` // ev_change, buy_zone, etc.
	Message     string    `json:"message"`
	EmailSent   bool      `json:"email_sent"`
	CreatedAt   time.Time `json:"created_at"`
}

// ExchangeRate represents currency exchange rates
type ExchangeRate struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	CurrencyCode string    `gorm:"unique;not null" json:"currency_code"` // EUR, USD, DKK, GBP, RUB, etc.
	Rate         float64   `json:"rate"`                                 // Rate relative to EUR (base currency)
	LastUpdated  time.Time `json:"last_updated"`
	IsActive     bool      `json:"is_active" gorm:"default:true"`  // Whether this currency is actively used
	IsManual     bool      `json:"is_manual" gorm:"default:false"` // Whether rate is manually set
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CashHolding represents available cash in different currencies
type CashHolding struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	PortfolioID  uint      `gorm:"not null;index" json:"portfolio_id"`
	CurrencyCode string    `gorm:"not null;index" json:"currency_code"` // EUR, USD, DKK, GBP, etc.
	Amount       float64   `json:"amount"`                              // Amount available in this currency
	USDValue     float64   `json:"usd_value"`                           // Current value in USD (calculated)
	Description  string    `json:"description"`                         // Optional description/note
	LastUpdated  time.Time `json:"last_updated"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Assessment represents a stock assessment generated by AI
type Assessment struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	Ticker     string    `gorm:"not null;index" json:"ticker"`
	Source     string    `gorm:"not null" json:"source"`          // 'grok' or 'deepseek'
	Assessment string    `gorm:"type:text" json:"assessment"`     // Full assessment text
	Status     string    `gorm:"default:'pending'" json:"status"` // 'pending', 'completed', 'failed'
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
