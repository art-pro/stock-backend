package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/models"
)

// ExternalAPIService handles all external API integrations
type ExternalAPIService struct {
	cfg                   *config.Config
	client                *http.Client
	exchangeRateCache     map[string]float64 // Cache for exchange rates from Grok
	lastAlphaVantageCall  time.Time          // Track last API call for rate limiting
	alphaVantageCallMutex sync.Mutex         // Mutex for thread-safe rate limiting
}

// AlphaVantageQuote represents Alpha Vantage real-time quote data
type AlphaVantageQuote struct {
	GlobalQuote struct {
		Symbol           string `json:"01. symbol"`
		Open             string `json:"02. open"`
		High             string `json:"03. high"`
		Low              string `json:"04. low"`
		Price            string `json:"05. price"`
		Volume           string `json:"06. volume"`
		LatestTradingDay string `json:"07. latest trading day"`
		PreviousClose    string `json:"08. previous close"`
		Change           string `json:"09. change"`
		ChangePercent    string `json:"10. change percent"`
	} `json:"Global Quote"`
	// Error handling fields
	Note         string `json:"Note,omitempty"`          // Rate limit message
	ErrorMessage string `json:"Error Message,omitempty"` // Invalid symbol/API key
	Information  string `json:"Information,omitempty"`   // API usage info
}

// AlphaVantageOverview represents company overview data with fundamentals
type AlphaVantageOverview struct {
	Symbol                     string `json:"Symbol"`
	Name                       string `json:"Name"`
	Description                string `json:"Description"`
	Sector                     string `json:"Sector"`
	MarketCapitalization       string `json:"MarketCapitalization"`
	PERatio                    string `json:"PERatio"`
	PEGRatio                   string `json:"PEGRatio"`
	Beta                       string `json:"Beta"`
	DividendYield              string `json:"DividendYield"`
	EPS                        string `json:"EPS"`
	RevenuePerShareTTM         string `json:"RevenuePerShareTTM"`
	ProfitMargin               string `json:"ProfitMargin"`
	AnalystTargetPrice         string `json:"AnalystTargetPrice"`
	TrailingPE                 string `json:"TrailingPE"`
	ForwardPE                  string `json:"ForwardPE"`
	PriceToSalesRatioTTM       string `json:"PriceToSalesRatioTTM"`
	PriceToBookRatio           string `json:"PriceToBookRatio"`
	EVToRevenue                string `json:"EVToRevenue"`
	EVToEBITDA                 string `json:"EVToEBITDA"`
	QuarterlyEarningsGrowthYOY string `json:"QuarterlyEarningsGrowthYOY"`
	QuarterlyRevenueGrowthYOY  string `json:"QuarterlyRevenueGrowthYOY"`
	// Error handling fields
	Note         string `json:"Note,omitempty"`          // Rate limit message
	ErrorMessage string `json:"Error Message,omitempty"` // Invalid symbol/API key
	Information  string `json:"Information,omitempty"`   // API usage info
}

// NewExternalAPIService creates a new external API service
func NewExternalAPIService(cfg *config.Config) *ExternalAPIService {
	return &ExternalAPIService{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		exchangeRateCache:    make(map[string]float64),
		lastAlphaVantageCall: time.Time{},
	}
}

// enforceAlphaVantageRateLimit ensures we don't exceed 5 calls per minute for free tier
// Premium tiers: 30 calls/min (75 calls/min for higher tiers)
func (s *ExternalAPIService) enforceAlphaVantageRateLimit() {
	s.alphaVantageCallMutex.Lock()
	defer s.alphaVantageCallMutex.Unlock()

	if !s.lastAlphaVantageCall.IsZero() {
		timeSinceLastCall := time.Since(s.lastAlphaVantageCall)
		// Free tier: 5 calls per minute = 1 call every 12 seconds
		// We use 13 seconds to be safe
		minInterval := 13 * time.Second

		if timeSinceLastCall < minInterval {
			sleepDuration := minInterval - timeSinceLastCall
			fmt.Printf("⏱ Rate limiting: waiting %.1f seconds before next Alpha Vantage call...\n", sleepDuration.Seconds())
			time.Sleep(sleepDuration)
		}
	}

	s.lastAlphaVantageCall = time.Now()
}

// cacheExchangeRate stores an exchange rate from Grok
func (s *ExternalAPIService) cacheExchangeRate(currency string, rate float64) {
	if rate > 0 {
		s.exchangeRateCache[currency] = rate
	}
}

// getCachedExchangeRate retrieves a cached exchange rate
func (s *ExternalAPIService) getCachedExchangeRate(currency string) (float64, bool) {
	rate, ok := s.exchangeRateCache[currency]
	return rate, ok
}

// GrokStockRequest represents the request to Grok API for complete stock analysis
type GrokStockRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GrokStockResponse represents the complete response from Grok API
type GrokStockResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// FetchAlphaVantageQuote fetches real-time price from Alpha Vantage
func (s *ExternalAPIService) FetchAlphaVantageQuote(ticker string) (*AlphaVantageQuote, error) {
	if s.cfg.AlphaVantageAPIKey == "" {
		return nil, fmt.Errorf("Alpha Vantage API key not configured")
	}

	// Enforce rate limiting
	s.enforceAlphaVantageRateLimit()

	// Build URL with proper parameters
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=GLOBAL_QUOTE&symbol=%s&apikey=%s&datatype=json",
		ticker, s.cfg.AlphaVantageAPIKey)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch quote: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limit (Alpha Vantage returns 200 with error message)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Read response body for error checking
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for rate limit error in response
	if bytes.Contains(body, []byte("API call frequency")) || bytes.Contains(body, []byte("Thank you for using Alpha Vantage")) {
		return nil, fmt.Errorf("Alpha Vantage rate limit reached (5 calls/minute for free tier)")
	}

	// Check for invalid API key
	if bytes.Contains(body, []byte("Invalid API key")) {
		return nil, fmt.Errorf("invalid Alpha Vantage API key")
	}

	var quote AlphaVantageQuote
	if err := json.Unmarshal(body, &quote); err != nil {
		return nil, fmt.Errorf("failed to decode quote: %w", err)
	}

	// Check for structured error responses
	if quote.Note != "" {
		return nil, fmt.Errorf("Alpha Vantage rate limit: %s", quote.Note)
	}
	if quote.ErrorMessage != "" {
		return nil, fmt.Errorf("Alpha Vantage error: %s", quote.ErrorMessage)
	}
	if quote.Information != "" {
		fmt.Printf("ℹ️ Alpha Vantage info: %s\n", quote.Information)
	}

	// Check if we got valid data
	if quote.GlobalQuote.Symbol == "" {
		return nil, fmt.Errorf("no data returned for ticker %s", ticker)
	}

	return &quote, nil
}

// FetchAlphaVantageOverview fetches company fundamentals from Alpha Vantage
func (s *ExternalAPIService) FetchAlphaVantageOverview(ticker string) (*AlphaVantageOverview, error) {
	if s.cfg.AlphaVantageAPIKey == "" {
		return nil, fmt.Errorf("Alpha Vantage API key not configured")
	}

	// Enforce rate limiting
	s.enforceAlphaVantageRateLimit()

	url := fmt.Sprintf("https://www.alphavantage.co/query?function=OVERVIEW&symbol=%s&apikey=%s&datatype=json",
		ticker, s.cfg.AlphaVantageAPIKey)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch overview: %w", err)
	}
	defer resp.Body.Close()

	// Check for rate limit
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Read response body for error checking
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for rate limit error in response
	if bytes.Contains(body, []byte("API call frequency")) || bytes.Contains(body, []byte("Thank you for using Alpha Vantage")) {
		return nil, fmt.Errorf("Alpha Vantage rate limit reached (5 calls/minute for free tier)")
	}

	// Check for invalid API key
	if bytes.Contains(body, []byte("Invalid API key")) {
		return nil, fmt.Errorf("invalid Alpha Vantage API key")
	}

	var overview AlphaVantageOverview
	if err := json.Unmarshal(body, &overview); err != nil {
		return nil, fmt.Errorf("failed to decode overview: %w", err)
	}

	// Check for structured error responses
	if overview.Note != "" {
		return nil, fmt.Errorf("Alpha Vantage rate limit: %s", overview.Note)
	}
	if overview.ErrorMessage != "" {
		return nil, fmt.Errorf("Alpha Vantage error: %s", overview.ErrorMessage)
	}
	if overview.Information != "" {
		fmt.Printf("ℹ️ Alpha Vantage info: %s\n", overview.Information)
	}

	// Check if we got valid data
	if overview.Symbol == "" {
		return nil, fmt.Errorf("no overview data returned for ticker %s", ticker)
	}

	return &overview, nil
}

// parseFloat safely parses a string to float64, returning 0 on error
func parseFloat(s string) float64 {
	if s == "" || s == "None" || s == "-" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// StockAnalysis represents the parsed stock data from Grok
type StockAnalysis struct {
	Ticker              string  `json:"ticker"`
	CompanyName         string  `json:"company_name"`
	CurrentPrice        float64 `json:"current_price"`
	Currency            string  `json:"currency"`
	ExchangeRateToUSD   float64 `json:"exchange_rate_to_usd"`
	FairValue           float64 `json:"fair_value"`
	Beta                float64 `json:"beta"`
	Volatility          float64 `json:"volatility"`
	PERatio             float64 `json:"pe_ratio"`
	EPSGrowthRate       float64 `json:"eps_growth_rate"`
	DebtToEBITDA        float64 `json:"debt_to_ebitda"`
	DividendYield       float64 `json:"dividend_yield"`
	ProbabilityPositive float64 `json:"probability_positive"`
	DownsideRisk        float64 `json:"downside_risk"`
	BRatio              float64 `json:"b_ratio"`
	UpsidePotential     float64 `json:"upside_potential"`
	ExpectedValue       float64 `json:"expected_value"`
	KellyFraction       float64 `json:"kelly_fraction"`
	HalfKellySuggested  float64 `json:"half_kelly_suggested"`
	BuyZoneMin          float64 `json:"buy_zone_min"`
	BuyZoneMax          float64 `json:"buy_zone_max"`
	Assessment          string  `json:"assessment"`
}

// GrokStockData extends StockAnalysis with additional metadata fields
type GrokStockData struct {
	CurrentPrice        float64 `json:"current_price"`
	FairValue           float64 `json:"fair_value"`
	Beta                float64 `json:"beta"`
	Volatility          float64 `json:"volatility"`
	PERatio             float64 `json:"pe_ratio"`
	EPSGrowthRate       float64 `json:"eps_growth_rate"`
	DebtToEBITDA        float64 `json:"debt_to_ebitda"`
	DividendYield       float64 `json:"dividend_yield"`
	ProbabilityPositive float64 `json:"probability_positive"`
	DownsideRisk        float64 `json:"downside_risk"`
	BRatio              float64 `json:"b_ratio"`
	UpsidePotential     float64 `json:"upside_potential"`
	ExpectedValue       float64 `json:"expected_value"`
	KellyFraction       float64 `json:"kelly_fraction"`
	HalfKellySuggested  float64 `json:"half_kelly_suggested"`
	BuyZoneMin          float64 `json:"buy_zone_min"`
	BuyZoneMax          float64 `json:"buy_zone_max"`
	Assessment          string  `json:"assessment"`
	Sector              string  `json:"sector"`
	FairValueSource     string  `json:"fair_value_source"`
	DataSource          string  `json:"data_source"`
}

// FetchAllStockData fetches all stock data using Alpha Vantage (primary) and Grok (analysis)
func (s *ExternalAPIService) FetchAllStockData(stock *models.Stock) error {
	var dataSource string
	var fairValueSource string

	// Step 1: Try to fetch real-time data from Alpha Vantage (most accurate)
	if s.cfg.AlphaVantageAPIKey != "" {
		fmt.Printf("Fetching Alpha Vantage data for %s...\n", stock.Ticker)

		// Fetch current price
		quote, err := s.FetchAlphaVantageQuote(stock.Ticker)
		if err == nil && quote.GlobalQuote.Price != "" {
			stock.CurrentPrice = parseFloat(quote.GlobalQuote.Price)
			dataSource = "Alpha Vantage"
			fmt.Printf("✓ Current price from Alpha Vantage: %.2f\n", stock.CurrentPrice)
		} else {
			fmt.Printf("⚠ Alpha Vantage quote error: %v\n", err)
		}

		// Fetch fundamentals (beta, fair value, etc.)
		overview, err := s.FetchAlphaVantageOverview(stock.Ticker)
		if err == nil && overview.Symbol != "" {
			// Update beta
			if overview.Beta != "" && overview.Beta != "None" {
				stock.Beta = parseFloat(overview.Beta)
				fmt.Printf("✓ Beta from Alpha Vantage: %.2f\n", stock.Beta)
			}

			// Update fair value from analyst target price
			if overview.AnalystTargetPrice != "" && overview.AnalystTargetPrice != "None" {
				stock.FairValue = parseFloat(overview.AnalystTargetPrice)
				fairValueSource = fmt.Sprintf("Alpha Vantage Consensus, %s", time.Now().Format("Jan 2, 2006"))
				fmt.Printf("✓ Fair value from Alpha Vantage: %.2f\n", stock.FairValue)
			}

			// Update other fundamentals
			stock.PERatio = parseFloat(overview.PERatio)
			stock.DividendYield = parseFloat(overview.DividendYield)

			// Calculate EPS growth from quarterly data
			if overview.QuarterlyEarningsGrowthYOY != "" {
				stock.EPSGrowthRate = parseFloat(overview.QuarterlyEarningsGrowthYOY) * 100
			}

			// Update sector if provided
			if overview.Sector != "" {
				stock.Sector = overview.Sector
			}
		} else {
			fmt.Printf("⚠ Alpha Vantage overview error: %v\n", err)
		}

		// If we got basic data from Alpha Vantage, use it
		if stock.CurrentPrice > 0 {
			stock.DataSource = dataSource
			if fairValueSource != "" {
				stock.FairValueSource = fairValueSource
			}

			// Use CalculateMetrics to compute derived values
			CalculateMetrics(stock)
			stock.LastUpdated = time.Now()
			return nil
		}
	}

	// Step 2: If Alpha Vantage not available or failed, try Grok
	if s.cfg.XAIAPIKey == "" {
		// No APIs configured - return error
		return s.mockStockData(stock)
	}

	// Create comprehensive prompt based on the probabilistic investment strategy
	prompt := fmt.Sprintf(`You are a financial analyst following a strict probabilistic investment strategy. The core philosophy is built on probabilistic thinking, expected value (EV) optimization, and ½-Kelly sizing to maximize long-term growth while minimizing ruin probability.

Key principles:

1. Probabilistic Thinking: Assign probabilities to scenarios (growth, stagnation, decline) rather than binary outcomes.

2. Expected Value (EV): Calculate EV = (p × upside %%) + ((1 - p) × downside %%). Only hold if EV > 0%%, add if >7%%, trim if <3%%, sell if <0%%.

3. Kelly Criterion: f* = [(b × p) - q] / b, where b = upside %% / |downside %%|, q = 1 - p. Use ½-Kelly for sizing, capped at 15%% for high-conviction/low-vol assets (typical 3–6%%).

4. Sector targets: Healthcare 30–35%%, Technology 15%%, Energy 8–10%%, Financials 5–7%%, Industrials 3–4%%, Consumer Staples 8–10%%, REITs 5–7%%, Cash 8–12%%.

Analyze the following stock (use ISIN and ticker together to ensure correct security identification):
- Ticker: %s
- ISIN: %s
- Company Name: %s
- Sector: %s
- Currency: %s

CRITICAL REQUIREMENTS:
- "current_price" = ACTUAL REAL-TIME TRADING PRICE on the stock exchange (what you can buy TODAY)
- "fair_value" = MEDIAN ANALYST CONSENSUS TARGET PRICE (12-month target from TipRanks/Yahoo Finance/Bloomberg)
- These are DIFFERENT values. Current price is TODAY's market price. Fair value is FUTURE analyst target.
- Use p=0.65 as default probability (adjust based on analyst ratings: 0.7 for Strong Buy, 0.65 for Buy, 0.5 for Hold)
- Calibrate downside by beta: <0.5 = -15%%, 0.5-1 = -20%%, 1-1.5 = -25%%, >1.5 = -30%%
- Buy zone: typically 85-95%% of current price or where EV >15%%

Provide response in valid JSON format with these EXACT fields (no additional text):

{
  "ticker": "%s",
  "company_name": "Full company name",
  "sector": "%s",
  "current_price": ACTUAL TRADING PRICE RIGHT NOW (number only),
  "currency": "%s",
  "exchange_rate_to_usd": 1 %s = X USD (e.g., 1 DKK = 0.1538 USD),
  "fair_value": MEDIAN analyst consensus target price (future 12-month target),
  "beta": beta coefficient (market sensitivity),
  "volatility": annualized volatility percentage,
  "pe_ratio": price to earnings ratio,
  "eps_growth_rate": EPS growth rate percentage,
  "debt_to_ebitda": debt to EBITDA ratio,
  "dividend_yield": dividend yield percentage,
  "probability_positive": probability p (0.65 default, 0.7 for Strong Buy, 0.5 for Hold),
  "downside_risk": downside %% (negative number, calibrated by beta),
  "upside_potential": ((fair_value - current_price) / current_price) × 100,
  "b_ratio": upside_potential / |downside_risk|,
  "expected_value": (p × upside_potential) + ((1-p) × downside_risk),
  "kelly_fraction": [(b × p) - q] / b × 100,
  "half_kelly_suggested": kelly_fraction / 2 (capped at 15%%),
  "buy_zone_min": minimum attractive entry price,
  "buy_zone_max": maximum attractive entry price,
  "assessment": "Add" if EV>7, "Hold" if EV>0, "Trim" if EV>-3, else "Sell"
}

VERIFY: Current price must be LOWER than fair value if upside is positive. Use real market data. Calculate all metrics using the formulas provided. Return ONLY the JSON object with NO additional text.`,
		stock.Ticker, stock.ISIN, stock.CompanyName, stock.Sector, stock.Currency,
		stock.Ticker, stock.Sector, stock.Currency, stock.Currency)

	// Build Grok API request
	reqBody := GrokStockRequest{
		Model: "grok-4-fast-reasoning",
		Messages: []Message{
			{
				Role:    "system",
				Content: "You are a financial analyst AI. Respond only with valid JSON data, no additional text.",
			},
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return s.mockStockData(stock)
	}

	// xAI API endpoint
	url := "https://api.x.ai/v1/chat/completions"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return s.mockStockData(stock)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.XAIAPIKey)

	// Implement exponential backoff for retries
	var resp *http.Response
	for i := 0; i < 3; i++ {
		resp, err = s.client.Do(req)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			break
		}

		if i < 2 {
			time.Sleep(time.Duration(1<<uint(i)) * time.Second)
		}
	}

	if err != nil {
		fmt.Printf("Grok API request error: %v\n", err)
		return s.mockStockData(stock)
	}
	if resp == nil {
		fmt.Printf("Grok API: no response received\n")
		return s.mockStockData(stock)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Grok API returned status: %d\n", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("Grok API error response: %s\n", string(bodyBytes))
		return s.mockStockData(stock)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read Grok response body: %v\n", err)
		return s.mockStockData(stock)
	}

	fmt.Printf("Grok raw response: %s\n", string(body))

	// Parse Grok response
	var grokResp GrokStockResponse
	if err := json.Unmarshal(body, &grokResp); err != nil {
		fmt.Printf("Failed to parse Grok response JSON: %v\n", err)
		return s.mockStockData(stock)
	}

	// Extract the JSON content from Grok's response
	if len(grokResp.Choices) == 0 {
		fmt.Printf("Grok response has no choices\n")
		return s.mockStockData(stock)
	}

	content := grokResp.Choices[0].Message.Content
	fmt.Printf("Grok content: %s\n", content)

	// Parse the stock analysis JSON
	var analysis StockAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		fmt.Printf("Failed to parse Grok stock analysis: %v\n", err)
		return s.mockStockData(stock)
	}

	// Update stock with all data from Grok (raw data + calculations)
	stock.CurrentPrice = analysis.CurrentPrice
	stock.FairValue = analysis.FairValue
	stock.Beta = analysis.Beta
	stock.Volatility = analysis.Volatility
	stock.PERatio = analysis.PERatio
	stock.EPSGrowthRate = analysis.EPSGrowthRate
	stock.DebtToEBITDA = analysis.DebtToEBITDA
	stock.DividendYield = analysis.DividendYield
	stock.ProbabilityPositive = analysis.ProbabilityPositive
	stock.DownsideRisk = analysis.DownsideRisk

	// Set data source
	stock.DataSource = "Grok AI"
	stock.FairValueSource = fmt.Sprintf("Grok AI Analysis, %s", time.Now().Format("Jan 2, 2006"))
	stock.LastUpdated = time.Now()

	// Validate fair value (warn if it seems inflated)
	if stock.FairValue > 0 && stock.CurrentPrice > 0 {
		upsidePercent := ((stock.FairValue - stock.CurrentPrice) / stock.CurrentPrice) * 100
		if upsidePercent > 100 {
			fmt.Printf("⚠️ WARNING: Fair value %.2f for %s seems inflated (%.1f%% upside). Please verify consensus target.\n",
				stock.FairValue, stock.Ticker, upsidePercent)
		}
	}

	// Recalculate metrics using our corrected formulas
	CalculateMetrics(stock)

	// Store exchange rate for later use (will be retrieved by FetchExchangeRate)
	s.cacheExchangeRate(stock.Currency, analysis.ExchangeRateToUSD)

	return nil
}

// mockStockData provides N/A values when no API is available
func (s *ExternalAPIService) mockStockData(stock *models.Stock) error {
	// Set all values to 0 or empty to indicate data is not available (N/A)
	// These will be displayed as "N/A" in the frontend
	stock.CurrentPrice = 0
	stock.FairValue = 0
	stock.Beta = 0
	stock.Volatility = 0
	stock.PERatio = 0
	stock.EPSGrowthRate = 0
	stock.DebtToEBITDA = 0
	stock.DividendYield = 0
	stock.ProbabilityPositive = 0
	stock.DownsideRisk = 0
	stock.UpsidePotential = 0
	stock.BRatio = 0
	stock.ExpectedValue = 0
	stock.KellyFraction = 0
	stock.HalfKellySuggested = 0
	stock.BuyZoneMin = 0
	stock.BuyZoneMax = 0
	stock.Assessment = "N/A"
	stock.DataSource = "None"
	stock.FairValueSource = "Not available"
	stock.LastUpdated = time.Now()

	// Cache mock exchange rate
	mockExchangeRate := s.getMockExchangeRate(stock.Currency)
	s.cacheExchangeRate(stock.Currency, mockExchangeRate)

	return fmt.Errorf("no API configured - stock data unavailable")
}

// getMockExchangeRate returns a mock exchange rate for a currency
func (s *ExternalAPIService) getMockExchangeRate(currency string) float64 {
	mockRates := map[string]float64{
		"USD": 1.0,
		"EUR": 1.10,
		"GBP": 1.27,
		"DKK": 0.1538,
		"SEK": 0.096,
		"NOK": 0.094,
	}
	if rate, ok := mockRates[currency]; ok {
		return rate
	}
	return 0.15 // Default fallback
}

// Legacy functions for backward compatibility
// These now call the unified FetchAllStockData function

// FetchStockPrice fetches current stock price (now from Grok)
func (s *ExternalAPIService) FetchStockPrice(ticker string) (float64, error) {
	// Create temporary stock for fetching
	tempStock := &models.Stock{
		Ticker:      ticker,
		CompanyName: ticker,
		Sector:      "Unknown",
		Currency:    "USD",
	}

	err := s.FetchAllStockData(tempStock)
	if err != nil {
		return 0, err
	}

	return tempStock.CurrentPrice, nil
}

// FetchGrokCalculations fetches stock calculations (now part of FetchAllStockData)
func (s *ExternalAPIService) FetchGrokCalculations(stock *models.Stock) error {
	// This now does nothing as FetchAllStockData handles everything
	return nil
}

// FetchExchangeRate fetches currency exchange rate to USD
// Prefers cached rate from Grok if available
func (s *ExternalAPIService) FetchExchangeRate(fromCurrency string) (float64, error) {
	if fromCurrency == "USD" {
		return 1.0, nil
	}

	// Check if we have a cached rate from Grok (most recent and accurate)
	if cachedRate, ok := s.getCachedExchangeRate(fromCurrency); ok {
		return cachedRate, nil
	}

	// If no Grok API key, use mock rates
	if s.cfg.ExchangeRatesAPIKey == "" && s.cfg.XAIAPIKey == "" {
		return s.getMockExchangeRate(fromCurrency), nil
	}

	url := fmt.Sprintf("https://api.exchangeratesapi.io/v1/latest?access_key=%s&base=%s&symbols=USD",
		s.cfg.ExchangeRatesAPIKey, fromCurrency)

	resp, err := s.client.Get(url)
	if err != nil {
		return 1.0, fmt.Errorf("failed to fetch exchange rate: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 1.0, fmt.Errorf("failed to decode response: %w", err)
	}

	rates, ok := result["rates"].(map[string]interface{})
	if !ok {
		return 1.0, fmt.Errorf("invalid response format")
	}

	rate, ok := rates["USD"].(float64)
	if !ok {
		return 1.0, fmt.Errorf("USD rate not found")
	}

	return rate, nil
}

// FetchAllExchangeRates fetches all needed exchange rates
func (s *ExternalAPIService) FetchAllExchangeRates(currencies []string) (map[string]float64, error) {
	rates := make(map[string]float64)
	rates["USD"] = 1.0 // USD to USD is always 1

	for _, currency := range currencies {
		if currency == "USD" {
			continue
		}
		rate, err := s.FetchExchangeRate(currency)
		if err != nil {
			// Use fallback rate on error
			rates[currency] = 1.0
		} else {
			rates[currency] = rate
		}
	}

	return rates, nil
}

// FetchFromAlphaVantage fetches ONLY raw financial data from Alpha Vantage
// Best for: current_price, beta, volatility, P/E, EPS growth, debt/EBITDA, dividend yield
func (s *ExternalAPIService) FetchFromAlphaVantage(stock *models.Stock) error {
	if s.cfg.AlphaVantageAPIKey == "" {
		return fmt.Errorf("Alpha Vantage API key not configured")
	}

	fmt.Printf("📊 Fetching RAW financial data from Alpha Vantage for %s...\n", stock.Ticker)

	// Fetch current price
	quote, err := s.FetchAlphaVantageQuote(stock.Ticker)
	if err != nil {
		return fmt.Errorf("failed to fetch quote: %w", err)
	}

	if quote.GlobalQuote.Price == "" {
		return fmt.Errorf("no price data returned for %s", stock.Ticker)
	}

	stock.CurrentPrice = parseFloat(quote.GlobalQuote.Price)
	fmt.Printf("✓ Current price: %.2f\n", stock.CurrentPrice)

	// Fetch fundamentals
	overview, err := s.FetchAlphaVantageOverview(stock.Ticker)
	if err != nil {
		return fmt.Errorf("failed to fetch overview: %w", err)
	}

	// Store raw JSON responses
	combinedResponse := map[string]interface{}{
		"quote":    quote,
		"overview": overview,
	}
	rawJSON, _ := json.MarshalIndent(combinedResponse, "", "  ")
	stock.AlphaVantageRawJSON = string(rawJSON)

	// Update all available raw data
	if overview.Beta != "" && overview.Beta != "None" {
		stock.Beta = parseFloat(overview.Beta)
		fmt.Printf("✓ Beta: %.2f\n", stock.Beta)
	}

	if overview.AnalystTargetPrice != "" && overview.AnalystTargetPrice != "None" {
		stock.FairValue = parseFloat(overview.AnalystTargetPrice)
		stock.FairValueSource = fmt.Sprintf("Alpha Vantage Consensus, %s", time.Now().Format("Jan 2, 2006"))
		fmt.Printf("✓ Fair value (analyst target): %.2f\n", stock.FairValue)
	}

	stock.PERatio = parseFloat(overview.PERatio)
	stock.DividendYield = parseFloat(overview.DividendYield)

	// Calculate EPS growth from quarterly data
	if overview.QuarterlyEarningsGrowthYOY != "" {
		stock.EPSGrowthRate = parseFloat(overview.QuarterlyEarningsGrowthYOY) * 100
	}

	// Update sector if provided
	if overview.Sector != "" {
		stock.Sector = overview.Sector
	}

	// Set data source
	stock.DataSource = "Alpha Vantage (Raw Data)"

	// Calculate all derived metrics (EV, Kelly, assessment, etc.)
	CalculateMetrics(stock)
	stock.LastUpdated = time.Now()

	fmt.Printf("✅ Alpha Vantage fetch complete for %s\n", stock.Ticker)
	return nil
}

// FetchFromGrok fetches ONLY analytical/interpretive data from Grok
// Best for: fair_value (consensus), probability (p), EV, Kelly, downside risk, assessment, recommendations
func (s *ExternalAPIService) FetchFromGrok(stock *models.Stock) error {
	if s.cfg.XAIAPIKey == "" {
		return fmt.Errorf("Grok API key not configured")
	}

	fmt.Printf("🤖 Fetching ANALYTICAL data from Grok for %s...\n", stock.Ticker)

	// Create comprehensive prompt for Grok focusing on analytical data
	prompt := fmt.Sprintf(`You are a financial analyst following a strict probabilistic investment strategy. The core philosophy is built on probabilistic thinking, expected value (EV) optimization, and ½-Kelly sizing to maximize long-term growth while minimizing ruin probability.

Key principles:

1. Probabilistic Thinking: Assign probabilities to scenarios (growth, stagnation, decline) rather than binary outcomes.

2. Expected Value (EV): Calculate EV = (p × upside %%) + ((1 - p) × downside %%). Only hold if EV > 0%%, add if >7%%, trim if <3%%, sell if <0%%.

3. Kelly Criterion: f* = [(b × p) - q] / b, where b = upside %% / |downside %%|, q = 1 - p. Use ½-Kelly for sizing, capped at 15%% for high-conviction/low-vol assets (typical 3–6%%).

4. Sector targets: Healthcare 30–35%%, Technology 15%%, Energy 8–10%%, Financials 5–7%%, Industrials 3–4%%, Consumer Staples 8–10%%, REITs 5–7%%, Cash 8–12%%.

Analyze the following stock (use ISIN and ticker together to ensure correct security identification):
- Ticker: %s
- ISIN: %s
- Company Name: %s
- Sector: %s
- Currency: %s

CRITICAL REQUIREMENTS:
- "current_price" = ACTUAL REAL-TIME TRADING PRICE on the stock exchange (what you can buy TODAY)
- "fair_value" = MEDIAN ANALYST CONSENSUS TARGET PRICE (12-month target from TipRanks/Yahoo Finance/Bloomberg)
- These are DIFFERENT values. Current price is TODAY's market price. Fair value is FUTURE analyst target.
- Use p=0.65 as default probability (adjust based on analyst ratings: 0.7 for Strong Buy, 0.65 for Buy, 0.5 for Hold)
- Calibrate downside by beta: <0.5 = -15%%, 0.5-1 = -20%%, 1-1.5 = -25%%, >1.5 = -30%%
- Buy zone: typically 85-95%% of current price or where EV >15%%

IMPORTANT FORMULAS:
1. upside_potential = ((fair_value - current_price) / current_price) × 100
2. b = upside_potential / |downside_risk|
3. expected_value = (probability_positive × upside_potential) + ((1 - probability_positive) × downside_risk)
4. kelly_fraction = ((b × probability_positive) - (1 - probability_positive)) / b
5. half_kelly_suggested = (kelly_fraction / 2), capped at 15%%

Return ONLY valid JSON (no markdown, no extra text):
{
  "current_price": <actual trading price TODAY>,
  "fair_value": <12-month analyst consensus target>,
  "beta": <market sensitivity>,
  "volatility": <annualized %% std dev>,
  "probability_positive": <0.5-0.7 based on ratings/fundamentals>,
  "downside_risk": <negative %%, e.g. -20>,
  "upside_potential": <positive %%>,
  "expected_value": <EV %%>,
  "b_ratio": <upside/|downside|>,
  "kelly_fraction": <optimal %% uncapped>,
  "half_kelly_suggested": <conservative %% capped at 15>,
  "buy_zone_min": <lower price bound>,
  "buy_zone_max": <upper price bound>,
  "assessment": "<Add/Hold/Trim/Sell>",
  "pe_ratio": <trailing P/E>,
  "eps_growth_rate": <YoY %%>,
  "debt_to_ebitda": <leverage ratio>,
  "dividend_yield": <%%>,
  "sector": "%s",
  "currency": "%s",
  "exchange_rate_to_usd": <rate if non-USD>,
  "fair_value_source": "<source, date>",
  "data_source": "Grok AI"
}`, stock.Ticker, stock.ISIN, stock.CompanyName, stock.Sector, stock.Currency, stock.Sector, stock.Currency)

	req := GrokStockRequest{
		Model: "grok-4-fast-reasoning",
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Stream: false,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", "https://api.x.ai/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.cfg.XAIAPIKey)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to call Grok API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Grok API returned status: %d, body: %s", resp.StatusCode, string(body))
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Grok response: %w", err)
	}

	var grokResp GrokStockResponse
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return fmt.Errorf("failed to decode Grok response: %w", err)
	}

	// Store the raw Grok response
	stock.GrokRawJSON = string(body)

	if len(grokResp.Choices) == 0 {
		return fmt.Errorf("no choices in Grok response")
	}

	content := grokResp.Choices[0].Message.Content

	// Extract JSON from markdown code blocks if present
	if bytes.Contains([]byte(content), []byte("```json")) {
		start := bytes.Index([]byte(content), []byte("```json")) + 7
		end := bytes.LastIndex([]byte(content), []byte("```"))
		if start > 7 && end > start {
			content = string([]byte(content)[start:end])
		}
	}

	var data GrokStockData
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return fmt.Errorf("failed to parse Grok JSON: %w, content: %s", err, content)
	}

	// Update stock with Grok data
	stock.CurrentPrice = data.CurrentPrice
	stock.FairValue = data.FairValue
	stock.Beta = data.Beta
	stock.Volatility = data.Volatility
	stock.ProbabilityPositive = data.ProbabilityPositive
	stock.DownsideRisk = data.DownsideRisk
	stock.UpsidePotential = data.UpsidePotential
	stock.ExpectedValue = data.ExpectedValue
	stock.BRatio = data.BRatio
	stock.KellyFraction = data.KellyFraction
	stock.HalfKellySuggested = data.HalfKellySuggested
	stock.BuyZoneMin = data.BuyZoneMin
	stock.BuyZoneMax = data.BuyZoneMax
	stock.Assessment = data.Assessment
	stock.PERatio = data.PERatio
	stock.EPSGrowthRate = data.EPSGrowthRate
	stock.DebtToEBITDA = data.DebtToEBITDA
	stock.DividendYield = data.DividendYield

	if data.Sector != "" {
		stock.Sector = data.Sector
	}

	stock.DataSource = "Grok AI (Analytical)"
	if data.FairValueSource != "" {
		stock.FairValueSource = data.FairValueSource
	}

	stock.LastUpdated = time.Now()

	fmt.Printf("✅ Grok fetch complete for %s\n", stock.Ticker)
	fmt.Printf("   Current Price: %.2f, Fair Value: %.2f, EV: %.1f%%, Assessment: %s\n",
		stock.CurrentPrice, stock.FairValue, stock.ExpectedValue, stock.Assessment)

	return nil
}
