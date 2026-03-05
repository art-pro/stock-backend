package services

import (
	"testing"
	"time"

	"github.com/art-pro/stock-backend/pkg/models"
)

func TestComputeRealizedPnL_FIFO(t *testing.T) {
	t.Parallel()
	// EUR = base. USD rate 1.2 => 1 EUR = 1.2 USD => 1 USD = 1/1.2 EUR
	fxRates := map[string]float64{"USD": 1.2, "EUR": 1.0}

	ops := []models.Operation{
		{OperationType: "Buy", Ticker: "AAPL", Currency: "USD", Quantity: 10, Price: 100, TradeDate: "01.01.2024", CreatedAt: time.Now()},
		{OperationType: "Sell", Ticker: "AAPL", Currency: "USD", Quantity: 5, Price: 120, TradeDate: "02.01.2024", CreatedAt: time.Now()},
	}
	// Buy 10 @ 100 USD => 1000/1.2 = 833.33 EUR cost. Sell 5 @ 120 USD => 600/1.2 = 500 EUR proceeds. FIFO cost for 5 = 5*(833.33/10) = 416.67. PnL = 500 - 416.67 = 83.33
	realized, err := ComputeRealizedPnL(ops, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized < 83 || realized > 84 {
		t.Errorf("realized PnL: got %.2f, want ~83.33", realized)
	}
}

func TestComputeRealizedPnL_NoTrades(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1.2}
	realized, err := ComputeRealizedPnL(nil, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized != 0 {
		t.Errorf("realized PnL: got %.2f, want 0", realized)
	}

	// Also test empty slice
	realized, err = ComputeRealizedPnL([]models.Operation{}, fxRates)
	if err != nil {
		t.Fatalf("unexpected error for empty slice: %v", err)
	}
	if realized != 0 {
		t.Errorf("realized PnL for empty slice: got %.2f, want 0", realized)
	}
}

func TestParseTradeDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantY   int
		wantM   time.Month
		wantD   int
	}{
		{"valid date", "15.02.2026", false, 2026, 2, 15},
		{"single digit day", "5.02.2026", false, 2026, 2, 5},
		{"single digit month", "15.2.2026", false, 2026, 2, 15},
		{"year 2000", "01.01.2000", false, 2000, 1, 1},
		{"invalid format", "2026-02-15", true, 0, 0, 0},
		{"invalid date", "32.02.2026", true, 0, 0, 0},
		{"empty string", "", true, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTradeDate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTradeDate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if result.Year() != tt.wantY || result.Month() != tt.wantM || result.Day() != tt.wantD {
					t.Errorf("parseTradeDate(%q) = %v, want %d-%02d-%02d", tt.input, result, tt.wantY, tt.wantM, tt.wantD)
				}
			}
		})
	}
}

func TestComputeRealizedPnL_MultipleBuysAndSells(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1.2, "EUR": 1.0}

	ops := []models.Operation{
		{OperationType: "Buy", Ticker: "AAPL", Currency: "USD", Quantity: 10, Price: 100, TradeDate: "01.01.2024", CreatedAt: time.Now()},
		{OperationType: "Buy", Ticker: "AAPL", Currency: "USD", Quantity: 5, Price: 110, TradeDate: "05.01.2024", CreatedAt: time.Now()},
		{OperationType: "Sell", Ticker: "AAPL", Currency: "USD", Quantity: 8, Price: 120, TradeDate: "10.01.2024", CreatedAt: time.Now()},
	}
	// Buy 10 @ 100 = 1000 USD cost (833.33 EUR)
	// Buy 5 @ 110 = 550 USD cost (458.33 EUR)
	// Total cost: 1291.67 EUR for 15 shares
	// Sell 8 @ 120 = 960 USD proceeds (800 EUR)
	// FIFO cost for 8 shares: first 8 from the first buy = 8 * (833.33/10) = 666.67 EUR
	// PnL = 800 - 666.67 = 133.33 EUR
	realized, err := ComputeRealizedPnL(ops, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized < 133 || realized > 134 {
		t.Errorf("realized PnL: got %.2f, want ~133.33", realized)
	}
}

func TestComputeRealizedPnL_MultipleStocks(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1.2, "EUR": 1.0}

	ops := []models.Operation{
		{OperationType: "Buy", Ticker: "AAPL", Currency: "USD", Quantity: 10, Price: 100, TradeDate: "01.01.2024", CreatedAt: time.Now()},
		{OperationType: "Sell", Ticker: "AAPL", Currency: "USD", Quantity: 5, Price: 120, TradeDate: "02.01.2024", CreatedAt: time.Now()},
		{OperationType: "Buy", Ticker: "MSFT", Currency: "USD", Quantity: 10, Price: 200, TradeDate: "03.01.2024", CreatedAt: time.Now()},
		{OperationType: "Sell", Ticker: "MSFT", Currency: "USD", Quantity: 5, Price: 220, TradeDate: "04.01.2024", CreatedAt: time.Now()},
	}
	// AAPL: Buy 10 @ 100 USD = 833.33 EUR, Sell 5 @ 120 USD = 500 EUR proceeds
	// FIFO cost for 5 = 416.67 EUR, PnL = 83.33 EUR
	// MSFT: Buy 10 @ 200 USD = 1666.67 EUR, Sell 5 @ 220 USD = 916.67 EUR proceeds
	// FIFO cost for 5 = 833.33 EUR, PnL = 83.33 EUR
	// Total PnL = 166.67 EUR
	realized, err := ComputeRealizedPnL(ops, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized < 166 || realized > 167 {
		t.Errorf("realized PnL: got %.2f, want ~166.67", realized)
	}
}

func TestComputeRealizedPnL_MixedCurrencies(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1.2, "EUR": 1.0}

	ops := []models.Operation{
		{OperationType: "Buy", Ticker: "AAPL", Currency: "USD", Quantity: 10, Price: 100, TradeDate: "01.01.2024", CreatedAt: time.Now()},
		{OperationType: "Sell", Ticker: "AAPL", Currency: "EUR", Quantity: 5, Price: 90, TradeDate: "02.01.2024", CreatedAt: time.Now()},
	}
	// Buy 10 @ 100 USD = 833.33 EUR cost
	// Sell 5 @ 90 EUR = 450 EUR proceeds
	// FIFO cost for 5 = 416.67 EUR
	// PnL = 450 - 416.67 = 33.33 EUR
	realized, err := ComputeRealizedPnL(ops, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized < 33 || realized > 34 {
		t.Errorf("realized PnL: got %.2f, want ~33.33", realized)
	}
}

func TestComputeRealizedPnL_OnlyBuysNoSells(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1.2, "EUR": 1.0}

	ops := []models.Operation{
		{OperationType: "Buy", Ticker: "AAPL", Currency: "USD", Quantity: 10, Price: 100, TradeDate: "01.01.2024", CreatedAt: time.Now()},
		{OperationType: "Buy", Ticker: "MSFT", Currency: "USD", Quantity: 5, Price: 200, TradeDate: "02.01.2024", CreatedAt: time.Now()},
	}
	// No sells, so realized PnL should be 0
	realized, err := ComputeRealizedPnL(ops, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized != 0 {
		t.Errorf("realized PnL: got %.2f, want 0 (no sells)", realized)
	}
}

func TestComputeRealizedPnL_SellWithoutBuy(t *testing.T) {
	t.Parallel()
	fxRates := map[string]float64{"USD": 1.2, "EUR": 1.0}

	ops := []models.Operation{
		{OperationType: "Sell", Ticker: "AAPL", Currency: "USD", Quantity: 5, Price: 120, TradeDate: "01.01.2024", CreatedAt: time.Now()},
	}
	// Selling without a prior buy - this may be an error case or result in zero cost basis
	// The function should handle this gracefully
	realized, err := ComputeRealizedPnL(ops, fxRates)
	if err != nil {
		t.Logf("Note: Sell without buy resulted in error: %v", err)
	} else if realized != 0 {
		t.Logf("Note: Sell without buy resulted in PnL: %.2f", realized)
	}
}
