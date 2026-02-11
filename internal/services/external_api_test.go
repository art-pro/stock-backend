package services

import (
	"testing"

	"github.com/art-pro/stock-backend/internal/config"
	"github.com/art-pro/stock-backend/internal/models"
)

func TestExternalAPIServiceCacheHelpers(t *testing.T) {
	t.Parallel()

	svc := NewExternalAPIService(&config.Config{})

	svc.cacheExchangeRate("EUR", 1.12)
	if got, ok := svc.getCachedExchangeRate("EUR"); !ok || got != 1.12 {
		t.Fatalf("cached EUR rate: got (%v, %v) want (%v, %v)", got, ok, 1.12, true)
	}

	// Zero/negative rates must not be cached.
	svc.cacheExchangeRate("BAD", 0)
	if _, ok := svc.getCachedExchangeRate("BAD"); ok {
		t.Fatalf("expected zero-value rate not to be cached")
	}
}

func TestGetMockExchangeRate(t *testing.T) {
	t.Parallel()

	svc := NewExternalAPIService(&config.Config{})
	if got := svc.getMockExchangeRate("USD"); got != 1.0 {
		t.Fatalf("USD mock rate: got %.4f want %.4f", got, 1.0)
	}
	if got := svc.getMockExchangeRate("UNKNOWN"); got != 0.15 {
		t.Fatalf("fallback mock rate: got %.4f want %.4f", got, 0.15)
	}
}

func TestFetchExchangeRateUsesCachedAndFallbackRates(t *testing.T) {
	t.Parallel()

	svc := NewExternalAPIService(&config.Config{})

	if got, err := svc.FetchExchangeRate("USD"); err != nil || got != 1.0 {
		t.Fatalf("USD rate: got (%.4f, %v) want (1.0, nil)", got, err)
	}

	svc.cacheExchangeRate("DKK", 0.1538)
	if got, err := svc.FetchExchangeRate("DKK"); err != nil || got != 0.1538 {
		t.Fatalf("cached DKK rate: got (%.4f, %v) want (0.1538, nil)", got, err)
	}

	// With no API keys set and no cache entry, the service should use mock fallback.
	if got, err := svc.FetchExchangeRate("SEK"); err != nil || got != 0.096 {
		t.Fatalf("mock SEK rate: got (%.4f, %v) want (0.096, nil)", got, err)
	}
}

func TestMockStockDataSetsNAFieldsAndCachesExchangeRate(t *testing.T) {
	t.Parallel()

	svc := NewExternalAPIService(&config.Config{})
	stock := &models.Stock{Currency: "GBP"}

	err := svc.mockStockData(stock)
	if err == nil {
		t.Fatalf("mockStockData should return a not-configured error")
	}
	if stock.Assessment != "N/A" {
		t.Fatalf("Assessment: got %q want %q", stock.Assessment, "N/A")
	}
	if stock.CurrentPrice != 0 || stock.ExpectedValue != 0 || stock.KellyFraction != 0 {
		t.Fatalf("expected calculated numeric fields to be zeroed")
	}
	if got, ok := svc.getCachedExchangeRate("GBP"); !ok || got != 1.27 {
		t.Fatalf("GBP cached mock rate: got (%v, %v) want (%v, %v)", got, ok, 1.27, true)
	}
}
