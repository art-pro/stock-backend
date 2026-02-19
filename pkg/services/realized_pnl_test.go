package services

import (
	"testing"
	"time"

	"github.com/art-pro/stock-backend/pkg/models"
)

func TestComputeRealizedPnL_FIFO(t *testing.T) {
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
	fxRates := map[string]float64{"USD": 1.2}
	realized, err := ComputeRealizedPnL(nil, fxRates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if realized != 0 {
		t.Errorf("realized PnL: got %.2f, want 0", realized)
	}
}

func TestParseTradeDate(t *testing.T) {
	tt, err := parseTradeDate("15.02.2026")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tt.Year() != 2026 || tt.Month() != 2 || tt.Day() != 15 {
		t.Errorf("parsed date: %v", tt)
	}
}
