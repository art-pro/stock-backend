# Stock Backend Context Guide

This document is the authoritative context for backend development and AI-assisted work in `stock–backend`.

## What This Backend Is

Go + Gin API for portfolio management using a probabilistic investing framework:
- Expected Value (EV) for decision quality
- Kelly-based sizing for risk-controlled allocation
- Multi-currency portfolio normalization (EUR base)
- Automated updates, alerts, and AI-assisted assessments

Primary runtime path is `pkg/*` (see `main.go` imports).  
`internal/*` exists but should be treated as secondary unless explicitly migrated.

## Architecture and Subsystems

- `pkg/api/` - HTTP layer (routing, handlers, auth-protected endpoints)
- `pkg/services/` - domain logic (calculations, exchange rates, external APIs, alerts, fair value collection)
- `pkg/scheduler/` - cron jobs for periodic stock updates and alert checks
- `pkg/database/` - DB init, migrations, default entities, portfolio helpers
- `pkg/models/` - data model schema and persisted fields
- `pkg/auth/`, `pkg/middleware/` - JWT auth and request protection
- `api/index.go` - serverless/Vercel entry point
- `main.go` - local/server runtime entry point

## Core Product Principles

1. **Probabilistic decisions, not deterministic predictions**
   - Inputs are uncertainty-aware (`ProbabilityPositive`, downside scenarios, volatility).
2. **Positive expected value is required**
   - EV drives recommendation and portfolio-level return expectation.
3. **Risk control is explicit**
   - Kelly formula is used, but operationalized with Half-Kelly and hard cap.
4. **Currency correctness is non-negotiable**
   - Portfolio aggregation converts local values into EUR using stored FX rates.
5. **Portfolio integrity over convenience**
   - Scoped by `portfolio_id`, atomic multi-step writes where data consistency matters.

## Data and Unit Semantics (Important)

**See `DATA_CONTRACT.md`** for the formal API contract: **weights** (0–1 fractions for `sector_weights` and `stock.weight`), **`last_updated`** semantics, **fair value as-of** (history or future `fair_value_recorded_at`), and **sector taxonomy** (canonical names in `pkg/models/sectors.go`). Frontend and backend must stay aligned on these.

- `ExchangeRate.Rate` means **currency units per 1 EUR**.
  - Example: if USD rate is 1.15, then `1 EUR = 1.15 USD`.
- Convert local currency to EUR: `value_eur = value_local / rate[currency]`
- Convert EUR to USD: `value_usd = value_eur * rate["USD"]`
- `Stock.CurrentPrice` is in stock local currency.
- `Stock.CurrentValueUSD` and `Stock.UnrealizedPnL` are stored in USD (backward compatibility).
- Portfolio summary response includes a `units` block to avoid frontend ambiguity.

## Calculation Engine (Source of Truth)

Implemented in `pkg/services/calculations.go`.

### Constants
- `defaultProbabilityPositive = 0.65`
- `riskFreeRatePercent = 4.0`
- `minDownsideMagnitude = 0.1` (to prevent unstable b-ratio near zero downside)

### Stock-Level Pipeline (`CalculateMetrics`)
1. **Downside calibration**
   - If `DownsideRisk == 0`, calibrate from beta:
     - beta < 0.5 -> -15%
     - 0.5 <= beta < 1.0 -> -20%
     - 1.0 <= beta < 1.5 -> -25%
     - beta >= 1.5 -> -30%
   - If beta also missing/non-positive -> fallback downside = -20%.
2. **Upside potential**
   - `UpsidePotential = ((FairValue - CurrentPrice) / CurrentPrice) * 100`
3. **Probability sanity**
   - If `ProbabilityPositive <= 0` or `> 1`, set to `0.65`.
4. **b-ratio**
   - `BRatio = UpsidePotential / max(abs(DownsideRisk), 0.1)`
5. **Expected value**
   - `ExpectedValue = p*UpsidePotential + (1-p)*DownsideRisk`
6. **Kelly fraction**
   - `KellyFraction = (((b*p) - (1-p)) / b) * 100`, clamped at minimum 0.
7. **Half-Kelly suggestion**
   - `HalfKellySuggested = min(KellyFraction/2, 15)`
8. **Assessment mapping**
   - `Add` if EV > 7
   - `Hold` if 3 <= EV <= 7
   - `Trim` if 0 <= EV < 3
   - `Sell` if EV < 0
9. **Buy zone (EV target = 7%)**
   - Solve required upside from EV equation.
   - `BuyZoneMax = FairValue / (1 + requiredUpside/100)`
   - `BuyZoneMin = BuyZoneMax * 0.90`
10. **Sell zone (EV targets = 3% and 0%)**
   - Uses closed-form threshold solving with same EV model:
     - `CP = (100 * p * FV) / (EV_threshold + 100*p - (1-p)*D)`
   - Where `D` is negative downside risk.
   - `SellZoneLowerBound` uses `EV_threshold = 3` (trim start).
   - `SellZoneUpperBound` uses `EV_threshold = 0` (sell start).
   - A valid sell zone requires `SellZoneLowerBound < SellZoneUpperBound`.
   - Otherwise set status to `no sell zone`.

### Dedicated Buy Zone Calculator (`CalculateBuyZoneResult`)
- Added dedicated helper to compute buy-zone limits and current EV for explicit inputs:
  - inputs: `ticker`, `fair_value`, `probability_positive`, `downside_risk`, `current_price`
  - output: JSON-friendly struct with:
    - `buy_zone.lower_bound` (EV threshold 15%)
    - `buy_zone.upper_bound` (EV threshold 7%)
    - `current_expected_value`
    - `zone_status`
- Uses closed-form threshold solving:
  - `CP = (100 * p * FV) / (EV_threshold + 100*p + (1-p)*|D|)`
- Validation rules:
  - `probability_positive` in `[0,1]`
  - `downside_risk` must be negative
  - `fair_value` must be positive
- Status semantics:
  - below lower bound -> `EV >> 15%`
  - within bounds -> `within buy zone`
  - above upper bound -> `outside buy zone`
  - invalid ordering -> `no buy zone available`
- Covered by unit tests in `pkg/services/calculations_test.go`.

### Dedicated Sell Zone Calculator (`CalculateSellZoneResult`)
- Added dedicated helper to compute sell-zone limits and current EV for explicit inputs:
  - inputs: `ticker`, `fair_value`, `probability_positive`, `downside_risk`, `current_price`
  - output: JSON-friendly struct with:
    - `sell_zone.sell_zone_lower_bound` (EV threshold 3%)
    - `sell_zone.sell_zone_upper_bound` (EV threshold 0%)
    - `current_expected_value`
    - `sell_zone_status`
- Uses the same closed-form threshold solving with EV target substitution.
- Validation rules:
  - `probability_positive` in `[0,1]`
  - `downside_risk` must be negative
  - `fair_value` must be positive
- Status semantics:
  - `current_expected_value > 3` -> `Below sell zone`
  - `0 < current_expected_value <= 3` -> `In trim zone`
  - `current_expected_value <= 0` -> `In sell zone`
  - invalid ordering (`trim >= sell`) -> `no sell zone`
- Covered by unit tests in `pkg/services/calculations_test.go`.

### Portfolio-Level Pipeline (`CalculatePortfolioMetrics`)
1. First pass:
   - For each owned position (`SharesOwned > 0`), convert to EUR and sum total.
2. Second pass:
   - Compute weights, weighted EV, weighted volatility, sector weights.
3. Sharpe ratio:
   - `SharpeRatio = (weightedEV - 4.0) / weightedVolatility`
4. Kelly utilization:
   - Sum of computed position weights (%).

### Why this matters
- The calculation service defines the backend's quantitative truth.
- UI, scheduler, and handlers should not implement alternative formulas.

## Decision-Making Rules (Operational)

- **Entry priority**: positive EV with acceptable downside and volatility context.
- **Sizing discipline**: use Half-Kelly output, never exceed 15% suggested single-position sizing.
- **Portfolio construction**: weight and diversification should be evaluated on EUR-normalized values.
- **Action bands**:
  - Add: compelling EV
  - Hold: fair EV
  - Trim: weakening edge
  - Sell: negative EV

## Request/Update Lifecycle for Stocks

Typical path for create/update/scheduler refresh:
1. Fetch/receive base fields (price, fair value, risk inputs).
2. Run `CalculateMetrics`.
3. Convert position and cost to EUR via `ExchangeRateService`.
4. Convert EUR totals to USD where persisted legacy fields require USD.
5. Save stock.
6. Optionally append `StockHistory`.
7. Trigger alert checks where configured.

## Exchange Rate Subsystem

Implemented in `pkg/services/exchange_rate_service.go`.

- API source: ExchangeRate-API (`latest/EUR`)
- HTTP client uses request timeouts and fails fast on non-200 responses to avoid hanging refreshes
- Supports tracked currencies in DB (`ExchangeRate` table)
- Manual rates (`IsManual`) are preserved on refresh
- Soft-delete for currencies (`IsActive=false`)
- EUR cannot be deleted; default core currencies are protected
- Provides conversion helpers:
  - `ConvertToEUR(amount, currency)`
  - `ConvertFromEUR(amount, currency)`

### Important caveat
- `GetRate` currently returns `1.0` when currency record is missing.
  - Some handlers defensively reject invalid/missing rates.
  - Prefer explicit validation when calculations must be strict.

## Portfolio Scoping and Multi-Portfolio Behavior

- Many stock/portfolio/alert endpoints resolve `portfolio_id` from query param.
- If absent, backend falls back to default portfolio via `database.GetDefaultPortfolioID`.
- Scoping exists on key stock operations, export/history/deleted stocks, alerts, and summary.
- Cash endpoints now resolve `portfolio_id` (query param or default) for reads/writes to avoid cross-portfolio access.

### Known consistency gap
- Not all handlers are fully scoped (assessment context paths fetch broadly).
- When extending features, include `portfolio_id` scoping by default.

## Transactional Integrity Rules

- Use DB transactions for multi-step state changes that must remain atomic.
  - Example: delete stock + create audit/deleted record.
  - Example: batch weight/current value updates in summary refresh.
  - Example: create operation + adjust cash + optional stock update (all in one tx); `CashHandler.AdjustCash(tx, ...)` accepts transaction so cash runs inside the same tx.
- If any write fails, rollback and return error.

## API Surface (Current Shape)

Public (`/api`):
- `POST /login`
- `GET /health`
- `GET /version`

Protected (`/api`, JWT):
- Auth/user: logout, change password/username, current user
- Stocks: CRUD, field/price patch, single/bulk/all updates, batch fetch
- Trusted fair value sync:
  - `POST /stocks/fair-value/collect`
  - `GET /stocks/:id/fair-value-history`
- History: stock history
- Deleted log: list + restore
- Portfolio: summary + settings
- Alerts: list + delete
- **Operations**: `POST /operations` (body: operation_type, currency, quantity, trade_date, optional ticker/price/amount/note etc.; creates operation, updates cash via `CashHandler.AdjustCash`, and for Buy/Sell creates or updates stock); `GET /operations` (query `portfolio_id` optional; returns operations for portfolio, newest first). Cash impact: Buy/Withdraw decrease cash; Sell/Deposit/Dividend increase. Scoped by `portfolio_id` (query or default).
- FX: list, refresh, add/update/delete currency
- Cash: list/create/update/delete + refresh USD values
- Assessment: request (body may include optional `rebalance_hint`, `concentration_hint`, `suggested_actions_hint` from frontend dashboard panes), vision extraction, recent/history
- User settings: table column configuration; **sector allocation targets** (persistent per user):
  - `GET /settings/sector-targets` – returns `{ "rows": [ { "sector", "min", "max", "rationale" }, ... ] }` or `{ "rows": null }` if none saved. Stored in `UserSettings` with key `sector_targets`.
  - `POST /settings/sector-targets` – body `{ "rows": [...] }`; creates or updates the user's sector targets (equity sectors + Cash). Used by frontend for rebalance hints and sector headers.
- **Analytics**: unrealized PnL statistics and portfolio performance analysis:
  - `GET /analytics/unrealized-pnl` – query `portfolio_id` optional; returns comprehensive unrealized PnL analytics:
    - `summary`: total unrealized PnL (USD/EUR), total cost basis, current value, return %, winning/losing positions count, win rate %
    - `by_sector`: unrealized PnL breakdown by sector with position counts and sector return %
    - `top_gainers`: top 10 positions with highest unrealized gains (sorted descending)
    - `top_losers`: top 10 positions with highest unrealized losses (sorted by PnL ascending)
    - `units`: metadata clarifying units (USD for PnL/values, percent for returns)
  - All calculations use EUR as base currency (consistent with portfolio summary), then convert to USD for display
  - FX conversion follows standard semantics: rates stored as currency units per 1 EUR
  - Only includes positions with `shares_owned > 0`
  - Cost basis calculated from `shares_owned * avg_price_local` converted to EUR, then to USD
  - Scoped by `portfolio_id` (query param or default portfolio)

## Scheduler Responsibilities

Implemented in `pkg/scheduler/scheduler.go`.

- Daily/weekly/monthly stock updates by `update_frequency`
- Hourly alert processing
- Each stock update:
  - refreshes market/fundamental values
  - recomputes metrics using shared calculation engine
  - updates USD legacy fields from EUR normalized values
  - writes history + potential alerts

## AI Assessment Subsystem

Implemented in `pkg/api/handlers/assessment_handler.go`.

- Supports Grok and Deepseek chat completion flows
- Vision flow extracts portfolio-like rows from screenshots (strict JSON output)
- Text assessment prompt includes:
  - current date
  - ticker context
  - probabilistic framework instructions
  - portfolio/cash context block (`buildPortfolioContext`: owned stocks table, sector allocations, cash)
  - optional **dashboard hints** block when the frontend sends them (see below)

### Single-ticker assessment (`POST /assessment/request`)

- **Request body:** `ticker` (required), `source` (`grok`|`deepseek`), optional `company_name`, `current_price`, `currency`. Optional fields sent by the Request Stock Assessment page when portfolio/settings are available:
  - `rebalance_hint` — text summary of sector rebalance (over/at/under/no target), from dashboard “Sector rebalance hint” pane.
  - `concentration_hint` — largest position, top 3, top 5 % of equity, from “Concentration & tail risk” pane.
  - `suggested_actions_hint` — suggested next actions (sector trim, sell/trim zone, buy zone add, high EV underweight), from “Suggested next actions” pane.
- **Prompt building:** `buildAssessmentPrompt` builds the portfolio/cash context, then if any of the three hint strings are non-empty, appends a **“DASHBOARD HINTS (current portfolio state)”** section with those three blocks and a short instruction to consider them for recommendations. Grok and Deepseek both receive this full prompt; batch assessment does not send dashboard hints (empty strings passed).

### LLM text-only endpoints (no DB write unless user applies)

- **`POST /assessment/batch`** – Body: `{ "tickers": ["AAPL", "MSFT"], "source": "grok"|"deepseek" }` (source optional, default grok). Runs LLM assessment per ticker (max 10); returns `{ "assessments": [ { "ticker", "assessment_text", "source" } ] }`. Uses portfolio context from `portfolio_id` (query or default). Frontend can “Run assessment for selected tickers” from the dashboard.
- **`POST /assessment/explain`** – Body: `{ "stock_id": 1 }` or `{ "ticker", "ev", "upside", "downside", "probability", "assessment" }`. Returns short paragraph: “Why is the recommendation Add/Hold/Trim/Sell?” in `{ "text": "..." }`. For modal or tooltip.
- **`POST /assessment/sector-summary`** – Body: `{ "portfolio_id"?: id, "sector": "Technology" }` or `{ "tickers": ["AAPL", "MSFT"], "sector"?: "..." }`. Returns narrative (outlook, risks, fit with targets) in `{ "text": "..." }`. Frontend can “Summarise sector: Technology”.

Same auth and optional `portfolio_id` query as other assessment routes.

### Important caveat
- Portfolio context builder for single-ticker assessment currently reads owned stocks/cash without strict portfolio scoping. Batch, explain, and sector-summary use `resolvePortfolioID` (query or default).

## Persistence Model Highlights

Key entities in `pkg/models/models.go`:
- `Stock`, `StockHistory`, `DeletedStock`
- `FairValueHistory` (source-level fair value audit trail)
- `Portfolio`, `PortfolioSettings`
- `ExchangeRate`, `CashHolding`
- `Alert`, `Assessment`, `User`, `UserSettings`

Design intent:
- preserve audit/history
- keep calculated values queryable
- support multiple portfolios per user

## Environment Configuration

Primary variables:
- Core: `APP_ENV`, `PORT`, `FRONTEND_URL`, `JWT_SECRET`, `DATABASE_PATH` / `DATABASE_URL`
- Admin bootstrap: `ADMIN_USERNAME`, `ADMIN_PASSWORD`
- Data providers: `ALPHA_VANTAGE_API_KEY`, `EXCHANGE_RATES_API_KEY`, `XAI_API_KEY`, `DEEPSEEK_API_KEY`
- Alerts: `SENDGRID_API_KEY`, `ALERT_EMAIL_FROM`, `ALERT_EMAIL_TO`
- Scheduler: `ENABLE_SCHEDULER`, `DEFAULT_UPDATE_FREQUENCY`

## Engineering Guardrails for Future Work

1. Keep formulas centralized in `pkg/services/calculations.go`.
2. Treat FX conversion semantics (`currency per 1 EUR`) as invariant.
3. Add `portfolio_id` scoping to all new reads/writes by default.
4. Prefer explicit failure to silent fallback for financial-critical computations.
5. Keep DB writes atomic when state spans multiple tables.
6. Update tests whenever formulas, thresholds, or unit semantics change.
7. Mutable endpoints are allow-listed (stock update, portfolio settings); extend the allow list explicitly when adding new editable fields to avoid mass assignment.
8. JWT validation rejects unexpected signing methods; continue issuing HS256 tokens with the shared secret.
9. **Rate limiting**: All endpoints are rate-limited (100 req/min per IP). Login has stricter limits (10 attempts/15 min). Extend `middleware.rateLimiter` for custom limits.
10. **Security headers**: All responses include security headers (X-Content-Type-Options, X-Frame-Options, HSTS, CSP). Do not remove these.
11. **Request size limits**: Image upload endpoints validate payload size (10 MB/image, max 10 images). Extend validation for new large-payload endpoints.
12. **Thread-safety**: Shared caches (e.g., `exchangeRateCache` in `ExternalAPIService`) must use mutex protection for concurrent access.

## Security Middleware Stack

Implemented in `pkg/middleware/auth.go` and applied in `pkg/api/router.go`.

### Rate Limiting
- **Global rate limiter**: 100 requests per minute per IP address
- **Login rate limiter**: 10 attempts per 15 minutes per IP (brute-force protection)
- Implementation uses in-memory token bucket with automatic cleanup every 5 minutes
- For production at scale, consider replacing with Redis-based solution

### Security Headers
All responses include:
- `X-Content-Type-Options: nosniff`
- `X-XSS-Protection: 1; mode=block`
- `X-Frame-Options: DENY`
- `Strict-Transport-Security: max-age=31536000; includeSubDomains`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`

### Request Size Limits
- Router-level multipart limit: 50 MB
- Image extraction endpoint validates: max 10 images, max 10 MB per image
- `RequestSizeLimitMiddleware(maxBytes)` available for custom per-route limits

### Extending Security
- To add custom rate limits: create new `rateLimiter` instance with desired `rate` and `window`
- To add route-specific size limits: apply `middleware.RequestSizeLimitMiddleware(bytes)` to route group

## Trusted Fair Value Collection (New in v2.6.0)

Implemented flow:
- New service: `pkg/services/fair_value_collector.go`
- Uses both LLM providers:
  - Grok (`XAI_API_KEY`)
  - Deepseek (`DEEPSEEK_API_KEY`)
- Each provider is prompted to return source-level fair values with:
  - numeric fair value
  - source name
  - source URL
  - as-of date

Trust and freshness enforcement:
- Accept only trusted domains:
  - `reuters.com`, `bloomberg.com`, `marketscreener.com`, `finance.yahoo.com`, `morningstar.com`, `wsj.com`, `marketwatch.com`
- Require parseable date and reject stale entries older than 45 days.
- Require at least 2 validated entries per stock.

Update behavior:
- Persist each accepted entry into `FairValueHistory`.
- Set stock fair value to median of accepted entries.
- Update `FairValueSource` as trusted multi-source consensus metadata.
- Recalculate EV/Kelly/assessment and persist stock + `StockHistory` snapshot in one transaction.

## Tests

- **`pkg/api/handlers/settings_handler_test.go`** – Sector targets: `GetSectorTargets` when no record (returns `rows: null`), `SaveSectorTargets` then GET roundtrip, empty rows returns 400, missing `user_id` returns 401. Uses in-memory SQLite and test user.
- **`pkg/api/handlers/operation_handler_test.go`** – Operations: `CreateOperation` Deposit returns 201 and cash holding is created/updated; `ListOperations` returns created operations; invalid operation_type returns 400; empty list returns 200. Uses temp SQLite, default portfolio, USD/EUR exchange rates, `CashHandler` and `OperationHandler`.

## Quick Runbook

- Install dependencies: `go mod download`
- Configure environment: `cp env.example.txt .env`
- Run locally: `go run main.go` (or `make run-backend` if available)
- Run tests: `go test ./...`

---

**Changelog (recent):** v2.10.0 – Stored assessments capped at 100; `cleanupOldAssessments()` runs after each upsert and deletes oldest by `created_at`. v2.9.0 – Operations API (POST/GET/DELETE/PUT /operations), Operation model, cash/stock create/update/delete with reversal; `AdjustCash(tx, ...)` for transactional use.

Last updated: 2026-02-18
