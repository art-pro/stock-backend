package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/artpro/assessapp/internal/config"
	"github.com/artpro/assessapp/internal/models"
)

// ExternalAPIService handles all external API integrations
type ExternalAPIService struct {
	cfg               *config.Config
	client            *http.Client
	exchangeRateCache map[string]float64 // Cache for exchange rates from Grok
}

// NewExternalAPIService creates a new external API service
func NewExternalAPIService(cfg *config.Config) *ExternalAPIService {
	return &ExternalAPIService{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		exchangeRateCache: make(map[string]float64),
	}
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

// FetchAllStockData fetches all stock data from Grok API in one call
func (s *ExternalAPIService) FetchAllStockData(stock *models.Stock) error {
	// Check if API key is configured
	if s.cfg.XAIAPIKey == "" {
		// Fallback to mock data for development
		return s.mockStockData(stock)
	}

	// Create comprehensive prompt for Grok to analyze the stock AND calculate metrics
	prompt := fmt.Sprintf(`Analyze the stock ticker "%s" (%s) in the %s sector with currency %s.

Provide a COMPLETE financial analysis including raw data AND calculated investment metrics.

CRITICAL DEFINITIONS:
- "current_price" = ACTUAL TRADING PRICE RIGHT NOW on the stock exchange (NOT the target/fair value)
- "fair_value" = Analyst consensus TARGET price (what analysts think it should reach)
- These are DIFFERENT values. Current price is what you can buy it for TODAY.

IMPORTANT FORMULAS:
- upside_potential = ((fair_value - current_price) / current_price) × 100
- b_ratio = upside_potential / |downside_risk|
- expected_value = (probability_positive × upside_potential) + ((1 - probability_positive) × downside_risk)
- kelly_fraction = ((b_ratio × probability_positive) - (1 - probability_positive)) / b_ratio × 100
- half_kelly_suggested = kelly_fraction / 2, capped at maximum 15
- buy_zone_min = current_price × 0.85
- buy_zone_max = fair_value × 0.95
- assessment = "Add" if expected_value > 7, "Hold" if > 0, "Trim" if > -5, else "Sell"

Return ONLY a valid JSON object with these EXACT fields (no additional text):

{
  "ticker": "%s",
  "company_name": "Full company name",
  "current_price": THE ACTUAL MARKET PRICE RIGHT NOW (what someone would pay today on the exchange),
  "currency": "%s",
  "exchange_rate_to_usd": current exchange rate (1 %s = X USD, e.g., 1 DKK = 0.1538 USD),
  "fair_value": analyst consensus TARGET price (future price target, typically higher than current),
  "beta": stock's beta coefficient,
  "volatility": annualized volatility percentage,
  "pe_ratio": price to earnings ratio,
  "eps_growth_rate": EPS growth rate percentage,
  "debt_to_ebitda": debt to EBITDA ratio,
  "dividend_yield": dividend yield percentage,
  "probability_positive": probability of positive outcome (0-1),
  "downside_risk": downside risk percentage (negative number),
  "b_ratio": calculated upside/downside ratio,
  "upside_potential": calculated upside percentage (must be (fair_value - current_price) / current_price × 100),
  "expected_value": calculated expected value percentage,
  "kelly_fraction": calculated Kelly criterion percentage,
  "half_kelly_suggested": calculated half-Kelly percentage (capped at 15),
  "buy_zone_min": calculated minimum buy zone price,
  "buy_zone_max": calculated maximum buy zone price,
  "assessment": "Add", "Hold", "Trim", or "Sell" based on expected_value
}

VERIFY: The current_price must be LOWER than fair_value if there is positive upside potential.
Calculate ALL fields using the formulas provided. Return ONLY the JSON object.`,
		stock.Ticker, stock.CompanyName, stock.Sector, stock.Currency,
		stock.Ticker, stock.Currency, stock.Currency)

	// Build Grok API request
	reqBody := GrokStockRequest{
		Model: "grok-beta",
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

	// Use Grok's calculated metrics (no need to calculate locally!)
	stock.UpsidePotential = analysis.UpsidePotential
	stock.BRatio = analysis.BRatio
	stock.ExpectedValue = analysis.ExpectedValue
	stock.KellyFraction = analysis.KellyFraction
	stock.HalfKellySuggested = analysis.HalfKellySuggested
	stock.BuyZoneMin = analysis.BuyZoneMin
	stock.BuyZoneMax = analysis.BuyZoneMax
	stock.Assessment = analysis.Assessment

	// Store exchange rate for later use (will be retrieved by FetchExchangeRate)
	s.cacheExchangeRate(stock.Currency, analysis.ExchangeRateToUSD)

	return nil
}

// mockStockData provides N/A values when Grok data is not available
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

	// Cache mock exchange rate
	mockExchangeRate := s.getMockExchangeRate(stock.Currency)
	s.cacheExchangeRate(stock.Currency, mockExchangeRate)

	return fmt.Errorf("Grok API not configured - stock data unavailable")
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
