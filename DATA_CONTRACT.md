# Backend Data Contract

This document defines the semantics of key API fields so the frontend and backend stay aligned. It is the single source of truth for weights, timestamps, and sector naming.

---

## 1. Weights and percentages

All weight-like values are **fractions in the range 0–1**. Multiply by 100 for display as percentage.

### Portfolio summary: `sector_weights`

- **Type:** `map[string]float64`
- **Semantics:** Fraction of portfolio value in each sector. Sum over sectors ≤ 1.
- **Example:** `"Healthcare": 0.35` means 35% of portfolio value in Healthcare.
- **Frontend:** Multiply by 100 for display (e.g. "35%"). Frontend may normalize 0–1 or 0–100 for backward compatibility; backend always returns 0–1.

### Per-stock: `weight`

- **Type:** `float64` on `Stock`
- **Semantics:** Fraction of portfolio value this position represents. 0–1; 0 when `shares_owned == 0`.
- **Example:** `0.08` means 8% of portfolio.
- **Use:** Concentration, Kelly hint, and "position size vs ½-Kelly" ratios. Consistent 0–1 semantics prevent wrong ratios.

### Other percentage fields (unchanged)

- **`kelly_utilization`** (portfolio summary): Still **0–100** (percentage) for display. Frontend shows it as "X%".
- **`kelly_fraction`**, **`half_kelly_suggested`**, **`upside_potential`**, **`downside_risk`**, **`expected_value`**: Percentages (0–100 scale) where applicable; see `pkg/services/calculations.go` and model comments.

---

## 2. Fair value and dates

### `last_updated` (Stock)

- **Semantics:** Last time **any persisted field** on the stock was updated (create, update, PATCH, price refresh, fair value sync, etc.). It is **not** restricted to "last price/fair value refresh."
- **Frontend use:** General "last changed" indicator. Do **not** use it alone to show "Fair value as of &lt;date&gt;"; use fair value history or `fair_value_recorded_at` when available.

### Fair value as-of date

- **Preferred:** For "Fair value: 412 USD (Grok, 15 Jan 2026)", the frontend should use:
  - **`GET /stocks/:id/fair-value-history`** and take the **latest** `recorded_at` (and `source`) from the returned list; or
  - **`fair_value_recorded_at`** on the stock when the backend exposes it (see below).
- **Backend option:** Expose **`fair_value_recorded_at`** on the stock (e.g. timestamp of the latest fair value history row used for the current `fair_value`). When not set, frontend falls back to fair-value-history latest `recorded_at`.
- **`FairValueSource`** on the stock remains a human-readable string (e.g. "TipRanks, Nov 5, 2025"); it does not replace a machine-readable date for tooltips.

---

## 3. Sector names (taxonomy)

- **`sector_weights`** keys (portfolio summary) and **`stock.sector`** must use the **same taxonomy** so the frontend can match sectors to targets (e.g. "Healthcare (46.8%, target 30–35%)").
- **Convention:** One canonical list; backend normalizes to it where possible (e.g. "Tech" → "Technology", "Health" → "Healthcare"). See **Canonical sector names** below.
- **Frontend:** Does case-insensitive matching to target ranges. Consistent naming avoids "no target" for a sector that actually has one.

### Canonical sector names

Use these exact strings when storing or returning sector identifiers. External sources (Alpha Vantage, Grok, manual) may use variants; normalize to this list when feasible.

| Canonical name           | Notes                          |
|--------------------------|--------------------------------|
| Technology               |                                |
| Insurance                |                                |
| Industrials              |                                |
| Healthcare               |                                |
| Financials               |                                |
| Financial Services       | Alternative label for same bucket |
| Energy                   |                                |
| Crypto                   |                                |
| Consumer Defensive       |                                |
| Consumer Cyclical        |                                |
| Communication Services  |                                |
| Basic Materials          |                                |

- Unmatched sectors (e.g. "Real Estate") may be stored as-is; frontend will show sector name with no target range until added to the philosophy table.
- Canonical list and normalizer are in **`pkg/models/sectors.go`**. Ingest paths (Alpha Vantage, Grok) call `NormalizeSector()` so stored sectors match the list where possible. Keep in sync with the frontend’s sector targets (e.g. `lib/sectorTargets.ts`).

---

## Summary

| Field / concept           | Convention        | Display                    |
|---------------------------|-------------------|----------------------------|
| `sector_weights` values   | 0–1 (fraction)    | × 100 → "X%"               |
| `stock.weight`            | 0–1 (fraction)    | × 100 → "X%"               |
| `kelly_utilization`       | 0–100 (percentage)| Use as "X%"                |
| `last_updated`            | Any stock update  | "Last updated" only        |
| Fair value as-of          | History or FV date| "Fair value (Source, date)"|
| Sector names              | Canonical list    | Case-insensitive match     |

Last updated: 2026-02-15
