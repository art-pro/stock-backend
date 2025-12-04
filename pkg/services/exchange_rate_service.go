package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// ExchangeRateService handles exchange rate operations
type ExchangeRateService struct {
	db     *gorm.DB
	logger zerolog.Logger
	apiKey string
}

// NewExchangeRateService creates a new exchange rate service
func NewExchangeRateService(db *gorm.DB, logger zerolog.Logger) *ExchangeRateService {
	return &ExchangeRateService{
		db:     db,
		logger: logger,
		apiKey: os.Getenv("EXCHANGE_RATE_API_KEY"),
	}
}

// ExchangeRateAPIResponse represents the API response structure
type ExchangeRateAPIResponse struct {
	Result          string             `json:"result"`
	Documentation   string             `json:"documentation"`
	TermsOfUse      string             `json:"terms_of_use"`
	TimeLastUpdate  int64              `json:"time_last_update_unix"`
	TimeNextUpdate  int64              `json:"time_next_update_unix"`
	BaseCode        string             `json:"base_code"`
	ConversionRates map[string]float64 `json:"conversion_rates"`
	ErrorType       string             `json:"error-type,omitempty"`
}

// FetchLatestRates fetches the latest exchange rates from the API
func (s *ExchangeRateService) FetchLatestRates() error {
	// If no API key, skip fetching
	if s.apiKey == "" {
		s.logger.Warn().Msg("No exchange rate API key configured, using default rates")
		return nil
	}

	// Construct API URL
	url := fmt.Sprintf("https://v6.exchangerate-api.com/v6/%s/latest/EUR", s.apiKey)

	// Make HTTP request
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch exchange rates: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON response
	var apiResp ExchangeRateAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API errors
	if apiResp.Result != "success" {
		return fmt.Errorf("API error: %s", apiResp.ErrorType)
	}

	// Update rates in database
	for code, rate := range apiResp.ConversionRates {
		// Check if we track this currency
		var exchangeRate models.ExchangeRate
		result := s.db.Where("currency_code = ?", code).First(&exchangeRate)

		if result.Error == nil {
			// Update existing rate if not manually set
			if !exchangeRate.IsManual {
				exchangeRate.Rate = rate
				exchangeRate.LastUpdated = time.Now()
				if err := s.db.Save(&exchangeRate).Error; err != nil {
					s.logger.Error().Err(err).Str("currency", code).Msg("Failed to update exchange rate")
				}
			}
		}
	}

	s.logger.Info().Msg("Exchange rates updated successfully")
	return nil
}

// GetAllRates returns all exchange rates
func (s *ExchangeRateService) GetAllRates() ([]models.ExchangeRate, error) {
	var rates []models.ExchangeRate
	if err := s.db.Where("is_active = ?", true).Order("currency_code").Find(&rates).Error; err != nil {
		return nil, err
	}
	return rates, nil
}

// GetRate returns the exchange rate for a specific currency
func (s *ExchangeRateService) GetRate(currencyCode string) (float64, error) {
	var rate models.ExchangeRate
	if err := s.db.Where("currency_code = ? AND is_active = ?", currencyCode, true).First(&rate).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Default to 1.0 if currency not found (assume EUR)
			return 1.0, nil
		}
		return 0, err
	}
	return rate.Rate, nil
}

// GetRatesMap returns a map of currency codes to rates
func (s *ExchangeRateService) GetRatesMap() (map[string]float64, error) {
	rates, err := s.GetAllRates()
	if err != nil {
		return nil, err
	}

	rateMap := make(map[string]float64)
	for _, rate := range rates {
		rateMap[rate.CurrencyCode] = rate.Rate
	}

	return rateMap, nil
}

// AddCurrency adds a new currency to track
func (s *ExchangeRateService) AddCurrency(currencyCode string, rate float64, isManual bool) error {
	exchangeRate := models.ExchangeRate{
		CurrencyCode: currencyCode,
		Rate:         rate,
		LastUpdated:  time.Now(),
		IsActive:     true,
		IsManual:     isManual,
	}

	return s.db.Create(&exchangeRate).Error
}

// UpdateRate updates an exchange rate
func (s *ExchangeRateService) UpdateRate(currencyCode string, rate float64, isManual bool) error {
	var exchangeRate models.ExchangeRate
	if err := s.db.Where("currency_code = ?", currencyCode).First(&exchangeRate).Error; err != nil {
		return err
	}

	exchangeRate.Rate = rate
	exchangeRate.IsManual = isManual
	exchangeRate.LastUpdated = time.Now()

	return s.db.Save(&exchangeRate).Error
}

// DeleteCurrency soft deletes a currency (sets is_active to false)
func (s *ExchangeRateService) DeleteCurrency(currencyCode string) error {
	// Don't allow deleting EUR (base currency)
	if currencyCode == "EUR" {
		return fmt.Errorf("cannot delete base currency EUR")
	}

	// Don't allow deleting default currencies
	defaultCurrencies := []string{"USD", "DKK", "GBP", "RUB"}
	for _, dc := range defaultCurrencies {
		if currencyCode == dc {
			return fmt.Errorf("cannot delete default currency %s", currencyCode)
		}
	}

	return s.db.Model(&models.ExchangeRate{}).
		Where("currency_code = ?", currencyCode).
		Update("is_active", false).Error
}

// ConvertToEUR converts an amount from a given currency to EUR
func (s *ExchangeRateService) ConvertToEUR(amount float64, fromCurrency string) (float64, error) {
	if fromCurrency == "EUR" {
		return amount, nil
	}

	rate, err := s.GetRate(fromCurrency)
	if err != nil {
		return 0, err
	}

	if rate == 0 {
		return 0, fmt.Errorf("invalid exchange rate for %s", fromCurrency)
	}

	return amount / rate, nil
}

// ConvertFromEUR converts an amount from EUR to a given currency
func (s *ExchangeRateService) ConvertFromEUR(amount float64, toCurrency string) (float64, error) {
	if toCurrency == "EUR" {
		return amount, nil
	}

	rate, err := s.GetRate(toCurrency)
	if err != nil {
		return 0, err
	}

	return amount * rate, nil
}
