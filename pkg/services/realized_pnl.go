package services

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/art-pro/stock-backend/pkg/models"
)

// lot is a FIFO buy lot: qty_remaining and unit_cost in base currency (EUR).
type lot struct {
	qtyRemaining float64
	unitCost     float64
}

// parseTradeDate parses DD.MM.YYYY to time.Time for ordering.
func parseTradeDate(s string) (time.Time, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid trade_date format: want DD.MM.YYYY, got %q", s)
	}
	day, err := strconv.Atoi(parts[0])
	if err != nil {
		return time.Time{}, err
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return time.Time{}, err
	}
	year, err := strconv.Atoi(parts[2])
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}

// ComputeRealizedPnL computes lifetime realized PnL from Buy/Sell operations using FIFO.
// All amounts are converted to base currency (EUR) using fxRates (currency units per 1 EUR).
// Fees are not stored on Operation; fee is treated as 0.
func ComputeRealizedPnL(operations []models.Operation, fxRates map[string]float64) (float64, error) {
	// Filter Buy/Sell and sort by trade date ascending (oldest first) for FIFO.
	var trades []models.Operation
	for _, op := range operations {
		if op.OperationType != "Buy" && op.OperationType != "Sell" {
			continue
		}
		if op.Quantity <= 0 {
			continue
		}
		trades = append(trades, op)
	}
	sort.Slice(trades, func(i, j int) bool {
		ti, ei := parseTradeDate(trades[i].TradeDate)
		tj, ej := parseTradeDate(trades[j].TradeDate)
		if ei != nil || ej != nil {
			// Fallback to created_at if parse fails
			return trades[i].CreatedAt.Before(trades[j].CreatedAt)
		}
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return trades[i].CreatedAt.Before(trades[j].CreatedAt)
	})

	var totalRealizedPnL float64
	// Per-ticker FIFO queue of buy lots (in EUR).
	lotsByTicker := make(map[string][]*lot)

	for _, op := range trades {
		rate := fxRates[op.Currency]
		if rate <= 0 {
			rate = 1 // treat as EUR if missing
		}
		// Convert to EUR: amount_eur = amount_local / rate (rate = local per 1 EUR)
		amountEUR := (op.Quantity * op.Price) / rate
		ticker := strings.TrimSpace(op.Ticker)
		if ticker == "" {
			continue
		}

		switch op.OperationType {
		case "Buy":
			unitCost := amountEUR / op.Quantity
			lotsByTicker[ticker] = append(lotsByTicker[ticker], &lot{qtyRemaining: op.Quantity, unitCost: unitCost})
		case "Sell":
			sellProceedsEUR := amountEUR
			qtyToMatch := op.Quantity
			costBasis := 0.0
			queue := lotsByTicker[ticker]
			for qtyToMatch > 0 && len(queue) > 0 {
				l := queue[0]
				matched := qtyToMatch
				if matched > l.qtyRemaining {
					matched = l.qtyRemaining
				}
				costBasis += matched * l.unitCost
				l.qtyRemaining -= matched
				qtyToMatch -= matched
				if l.qtyRemaining <= 0 {
					queue = queue[1:]
				}
			}
			lotsByTicker[ticker] = queue
			if qtyToMatch > 0 {
				// Sold more than held: no short support; treat as best-effort (cost basis only for what we had)
				// Optionally return error: return 0, fmt.Errorf("sell qty exceeds position for %s", ticker)
			}
			// sell_fee = 0 (Operation has no fee field)
			pnl := sellProceedsEUR - costBasis
			totalRealizedPnL += pnl
		}
	}

	return totalRealizedPnL, nil
}
