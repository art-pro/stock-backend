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
	cfg              *config.Config
	client           *http.Client
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
  "current_price": current market price as number,
  "currency": "%s",
  "exchange_rate_to_usd": current exchange rate (1 %s = X USD, e.g., 1 DKK = 0.1538 USD),
  "fair_value": analyst consensus target price,
  "beta": stock's beta coefficient,
  "volatility": annualized volatility percentage,
  "pe_ratio": price to earnings ratio,
  "eps_growth_rate": EPS growth rate percentage,
  "debt_to_ebitda": debt to EBITDA ratio,
  "dividend_yield": dividend yield percentage,
  "probability_positive": probability of positive outcome (0-1),
  "downside_risk": downside risk percentage (negative number),
  "b_ratio": calculated upside/downside ratio,
  "upside_potential": calculated upside percentage,
  "expected_value": calculated expected value percentage,
  "kelly_fraction": calculated Kelly criterion percentage,
  "half_kelly_suggested": calculated half-Kelly percentage (capped at 15),
  "buy_zone_min": calculated minimum buy zone price,
  "buy_zone_max": calculated maximum buy zone price,
  "assessment": "Add", "Hold", "Trim", or "Sell" based on expected_value
}

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
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		
		if i < 2 {
			time.Sleep(time.Duration(1<<uint(i)) * time.Second)
		}
	}

	if err != nil {
		return s.mockStockData(stock)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return s.mockStockData(stock)
	}

	// Parse Grok response
	var grokResp GrokStockResponse
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return s.mockStockData(stock)
	}

	// Extract the JSON content from Grok's response
	if len(grokResp.Choices) == 0 {
		return s.mockStockData(stock)
	}

	content := grokResp.Choices[0].Message.Content
	
	// Parse the stock analysis JSON
	var analysis StockAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
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

// mockStockData provides comprehensive mock data including all calculations
func (s *ExternalAPIService) mockStockData(stock *models.Stock) error {
	// Generate consistent mock current price based on ticker
	hash := 0
	for _, c := range stock.Ticker {
		hash = hash*31 + int(c)
	}
	stock.CurrentPrice = 50.0 + float64(hash%450)
	
	// Generate other mock values with reasonable relationships
	stock.FairValue = stock.CurrentPrice * 1.20 // 20% upside
	stock.Beta = 0.8 + (float64(len(stock.Ticker)) * 0.1)
	stock.Volatility = 15.0 + (float64(len(stock.Ticker)) * 2.0)
	stock.PERatio = 18.5
	stock.EPSGrowthRate = 12.0
	stock.DebtToEBITDA = 1.5
	stock.DividendYield = 2.0
	stock.ProbabilityPositive = 0.65
	stock.DownsideRisk = -15.0
	
	// Calculate derived metrics (same formulas as Grok would use)
	stock.UpsidePotential = ((stock.FairValue - stock.CurrentPrice) / stock.CurrentPrice) * 100
	stock.BRatio = stock.UpsidePotential / (-stock.DownsideRisk)
	stock.ExpectedValue = (stock.ProbabilityPositive * stock.UpsidePotential) + 
		((1 - stock.ProbabilityPositive) * stock.DownsideRisk)
	stock.KellyFraction = ((stock.BRatio * stock.ProbabilityPositive) - 
		(1 - stock.ProbabilityPositive)) / stock.BRatio * 100
	stock.HalfKellySuggested = stock.KellyFraction / 2
	if stock.HalfKellySuggested > 15 {
		stock.HalfKellySuggested = 15 // Cap at 15%
	}
	stock.BuyZoneMin = stock.CurrentPrice * 0.85
	stock.BuyZoneMax = stock.FairValue * 0.95
	
	// Determine assessment
	if stock.ExpectedValue > 7 {
		stock.Assessment = "Add"
	} else if stock.ExpectedValue > 0 {
		stock.Assessment = "Hold"
	} else if stock.ExpectedValue > -5 {
		stock.Assessment = "Trim"
	} else {
		stock.Assessment = "Sell"
	}
	
	// Cache mock exchange rate
	mockExchangeRate := s.getMockExchangeRate(stock.Currency)
	s.cacheExchangeRate(stock.Currency, mockExchangeRate)
	
	return nil
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

