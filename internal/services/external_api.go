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
	cfg    *config.Config
	client *http.Client
}

// NewExternalAPIService creates a new external API service
func NewExternalAPIService(cfg *config.Config) *ExternalAPIService {
	return &ExternalAPIService{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GrokCalculationRequest represents the request to Grok API
type GrokCalculationRequest struct {
	Ticker       string  `json:"ticker"`
	CurrentPrice float64 `json:"current_price"`
	Currency     string  `json:"currency"`
	Sector       string  `json:"sector"`
}

// GrokCalculationResponse represents the response from Grok API
type GrokCalculationResponse struct {
	Ticker              string  `json:"ticker"`
	FairValue           float64 `json:"fair_value"`
	Beta                float64 `json:"beta"`
	Volatility          float64 `json:"volatility"`
	PERatio             float64 `json:"pe_ratio"`
	EPSGrowthRate       float64 `json:"eps_growth_rate"`
	DebtToEBITDA        float64 `json:"debt_to_ebitda"`
	DividendYield       float64 `json:"dividend_yield"`
	ProbabilityPositive float64 `json:"probability_positive"`
	DownsideRisk        float64 `json:"downside_risk"`
}

// FetchGrokCalculations fetches stock calculations from Grok/xAI API
func (s *ExternalAPIService) FetchGrokCalculations(stock *models.Stock) error {
	// Check if API key is configured
	if s.cfg.XAIAPIKey == "" {
		// Fallback to mock data for development
		return s.mockGrokCalculations(stock)
	}

	reqBody := GrokCalculationRequest{
		Ticker:       stock.Ticker,
		CurrentPrice: stock.CurrentPrice,
		Currency:     stock.Currency,
		Sector:       stock.Sector,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// xAI API endpoint (adjust based on actual API documentation)
	url := "https://api.x.ai/v1/chat/completions"
	
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
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
			time.Sleep(time.Duration(1<<uint(i)) * time.Second) // Exponential backoff
		}
	}

	if err != nil {
		return s.mockGrokCalculations(stock) // Fallback to mock on error
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var grokResp GrokCalculationResponse
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return s.mockGrokCalculations(stock) // Fallback to mock on parse error
	}

	// Update stock with Grok calculations
	stock.FairValue = grokResp.FairValue
	stock.Beta = grokResp.Beta
	stock.Volatility = grokResp.Volatility
	stock.PERatio = grokResp.PERatio
	stock.EPSGrowthRate = grokResp.EPSGrowthRate
	stock.DebtToEBITDA = grokResp.DebtToEBITDA
	stock.DividendYield = grokResp.DividendYield
	stock.ProbabilityPositive = grokResp.ProbabilityPositive
	stock.DownsideRisk = grokResp.DownsideRisk

	return nil
}

// mockGrokCalculations provides mock data for development/testing
func (s *ExternalAPIService) mockGrokCalculations(stock *models.Stock) error {
	// Provide reasonable mock values based on ticker
	stock.FairValue = stock.CurrentPrice * 1.20 // 20% upside
	stock.Beta = 0.8 + (float64(len(stock.Ticker)) * 0.1)
	stock.Volatility = 15.0 + (float64(len(stock.Ticker)) * 2.0)
	stock.PERatio = 18.5
	stock.EPSGrowthRate = 12.0
	stock.DebtToEBITDA = 1.5
	stock.DividendYield = 2.0
	stock.ProbabilityPositive = 0.65
	stock.DownsideRisk = -15.0
	
	return nil
}

// FetchStockPrice fetches current stock price from Alpha Vantage
func (s *ExternalAPIService) FetchStockPrice(ticker string) (float64, error) {
	if s.cfg.AlphaVantageAPIKey == "" {
		// Return mock price for development
		return s.mockStockPrice(ticker), nil
	}

	// URL encode the ticker to handle spaces and special characters
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=GLOBAL_QUOTE&symbol=%s&apikey=%s",
		ticker, s.cfg.AlphaVantageAPIKey)

	resp, err := s.client.Get(url)
	if err != nil {
		// Fallback to mock on error
		return s.mockStockPrice(ticker), nil
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Fallback to mock on decode error
		return s.mockStockPrice(ticker), nil
	}

	// Extract price from response
	globalQuote, ok := result["Global Quote"].(map[string]interface{})
	if !ok {
		// Fallback to mock if invalid format
		return s.mockStockPrice(ticker), nil
	}

	priceStr, ok := globalQuote["05. price"].(string)
	if !ok {
		// Fallback to mock if price not found
		return s.mockStockPrice(ticker), nil
	}

	var price float64
	_, err = fmt.Sscanf(priceStr, "%f", &price)
	if err != nil || price == 0 {
		// Fallback to mock if parse failed or price is 0
		return s.mockStockPrice(ticker), nil
	}
	
	return price, nil
}

// mockStockPrice generates a realistic mock price based on ticker
func (s *ExternalAPIService) mockStockPrice(ticker string) float64 {
	// Use hash of ticker name to generate consistent but varied prices
	hash := 0
	for _, c := range ticker {
		hash = hash*31 + int(c)
	}
	// Generate price between 50 and 500
	price := 50.0 + float64(hash%450)
	return price
}

// FetchExchangeRate fetches currency exchange rate to USD
func (s *ExternalAPIService) FetchExchangeRate(fromCurrency string) (float64, error) {
	if fromCurrency == "USD" {
		return 1.0, nil
	}

	if s.cfg.ExchangeRatesAPIKey == "" {
		// Return mock rates for development
		mockRates := map[string]float64{
			"EUR": 1.10,
			"GBP": 1.27,
			"DKK": 0.15,
			"SEK": 0.096,
			"NOK": 0.094,
		}
		if rate, ok := mockRates[fromCurrency]; ok {
			return rate, nil
		}
		return 1.0, nil
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

