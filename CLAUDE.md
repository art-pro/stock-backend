# Stock Portfolio Management Backend - Project Documentation

## Project Overview
A Go-based backend API for managing stock portfolios with advanced analytics, multi-currency support, and automated calculations. The system tracks stocks, performs Kelly Criterion calculations, manages exchange rates, and provides portfolio assessments.

**Module:** `github.com/artpro/assessapp`
**Go Version:** 1.24
**Primary Framework:** Gin Web Framework

## Architecture

### Core Components

1. **API Layer** (`pkg/api/` and `internal/api/`)
   - RESTful API using Gin framework
   - JWT-based authentication
   - CORS-enabled for frontend integration

2. **Database Layer** (`pkg/database/` and `internal/database/`)
   - GORM ORM with SQLite (development) and PostgreSQL (production) support
   - Auto-migration on startup
   - Admin user initialization

3. **Services Layer** (`pkg/services/` and `internal/services/`)
   - Calculations: Kelly Criterion, portfolio analytics
   - External APIs: Alpha Vantage, Exchange Rate API, XAI/Grok
   - Alerts: Email notifications via SendGrid

4. **Scheduler** (`pkg/scheduler/` and `internal/scheduler/`)
   - Automated stock updates based on frequency (daily/weekly/monthly)
   - Uses gocron for job scheduling

## Directory Structure

```
stock-backend/
├── main.go                 # Application entry point
├── api/                    # Vercel deployment handler
│   └── index.go
├── internal/               # Internal implementation (not importable)
│   ├── api/
│   │   ├── router.go
│   │   └── handlers/
│   │       ├── auth_handler.go
│   │       ├── cash_handler.go
│   │       ├── portfolio_handler.go
│   │       └── stock_handler.go
│   ├── auth/
│   ├── config/
│   ├── database/
│   ├── middleware/
│   ├── models/
│   ├── scheduler/
│   └── services/
├── pkg/                    # Public packages (legacy, being refactored)
│   ├── api/
│   ├── auth/
│   ├── config/
│   ├── database/
│   ├── middleware/
│   ├── models/
│   ├── scheduler/
│   └── services/
├── data/
│   └── stocks.db          # SQLite database
├── public/
│   └── index.html         # Basic landing page
└── EXCHANGE_RATES.md      # Exchange rate documentation
```

**Note:** The project has both `internal/` and `pkg/` directories. The codebase is in transition - `internal/` contains the active implementation while `pkg/` contains legacy code. Focus on `internal/` for new development.

## Data Models

### Core Models (internal/models/models.go)

#### Stock
Primary model for portfolio holdings with comprehensive tracking:
- **Identification:** Ticker, CompanyName, Sector
- **Pricing:** CurrentPrice, Currency, FairValue
- **Risk Metrics:** UpsidePotential, DownsideRisk, Beta, Volatility
- **Calculations:** ProbabilityPositive, ExpectedValue, BRatio, KellyFraction
- **Position:** SharesOwned, AvgPriceLocal, CurrentValueUSD, Weight
- **Assessment:** BuyZoneMin/Max, Assessment (Hold/Add/Trim/Sell)
- **Updates:** UpdateFrequency (daily/weekly/monthly), LastUpdated

#### DeletedStock
Soft-deletion log with full stock data preservation:
- Stores JSON-serialized Stock object
- Tracks PortfolioID (with index)
- Records deletion reason and user
- Supports restoration

#### ExchangeRate
Multi-currency support with EUR as base:
- CurrencyCode (unique, indexed)
- Rate (relative to EUR)
- IsManual (preserves manual rates during API updates)
- IsActive (soft deletion support)

#### CashHolding
Multi-currency cash management:
- CurrencyCode (indexed)
- Amount (in local currency)
- USDValue (calculated)
- Description (optional note)

#### StockHistory
Historical tracking for trend analysis:
- Links to StockID with composite index (stock_id, recorded_at)
- Stores key metrics snapshots
- RecordedAt timestamp

#### PortfolioSettings
Global portfolio configuration:
- TotalPortfolioValue (USD)
- UpdateFrequency (default update schedule)
- Alert configuration (threshold, enabled status)

#### Alert
Notification tracking:
- Links to StockID
- AlertType (ev_change, buy_zone, etc.)
- EmailSent status (indexed)

## Key Features

### 1. Multi-Currency Portfolio Management
- EUR as base currency for all calculations
- Support for EUR, USD, DKK, GBP, RUB (configurable)
- Automatic exchange rate updates via API
- Manual rate override capability
- Two-pass calculation for accurate totals and weights

**Reference:** [EXCHANGE_RATES.md](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/EXCHANGE_RATES.md?type=file&root=%252F)

### 2. Kelly Criterion Calculations
Portfolio optimization using Kelly Criterion methodology:
- **BRatio:** Upside/Downside ratio
- **Kelly Fraction (f*):** Optimal position size
- **Half-Kelly:** Conservative sizing (capped at 15%)
- Considers probability of positive outcomes

### 3. Stock Assessment System
Automated buy/sell recommendations:
- **Hold:** Within target range
- **Add:** Below buy zone or high Kelly fraction
- **Trim:** Above target or low Kelly
- **Sell:** Negative expected value

### 4. Automated Updates
Scheduled stock data refresh:
- Configurable frequency per stock (daily/weekly/monthly)
- External API integration (Alpha Vantage)
- Alert generation on significant changes

### 5. Alert System
Email notifications via SendGrid:
- EV change threshold alerts
- Buy zone entry/exit notifications
- Configurable thresholds

## API Endpoints

### Authentication
- `POST /api/auth/login` - JWT authentication
- `POST /api/auth/register` - User registration (admin only in production)

### Portfolio Management
- `GET /api/portfolio` - Get all stocks with calculations
- `GET /api/portfolio/:id` - Get single stock
- `POST /api/portfolio` - Add new stock
- `PUT /api/portfolio/:id` - Update stock
- `DELETE /api/portfolio/:id` - Delete stock (soft delete to DeletedStock)

### Exchange Rates
- `GET /api/exchange-rates` - Get all active rates
- `POST /api/exchange-rates` - Add/update currency
- `PUT /api/exchange-rates/:code` - Update rate manually
- `DELETE /api/exchange-rates/:code` - Soft delete currency
- `POST /api/exchange-rates/refresh` - Fetch latest API rates

### Cash Holdings
- `GET /api/cash` - Get all cash holdings
- `POST /api/cash` - Add cash holding
- `PUT /api/cash/:id` - Update cash amount
- `DELETE /api/cash/:id` - Remove cash holding

### Analytics
- `GET /api/portfolio/history/:ticker` - Historical data for stock
- `GET /api/portfolio/assessment` - Portfolio-wide assessment

## Environment Configuration

Required environment variables (see [env.example.txt](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/env.example.txt?type=file&root=%252F)):

### Core
- `APP_ENV`: development/production
- `PORT`: Server port (default: 8080)
- `FRONTEND_URL`: CORS origin
- `JWT_SECRET`: Authentication secret
- `DATABASE_PATH`: SQLite path or PostgreSQL URL

### Admin
- `ADMIN_USERNAME`: Initial admin user
- `ADMIN_PASSWORD`: Initial admin password

### External APIs
- `ALPHA_VANTAGE_API_KEY`: Stock data API
- `EXCHANGE_RATES_API_KEY`: Currency exchange rates
- `XAI_API_KEY`: Grok AI integration (optional)

### Email (Optional)
- `SENDGRID_API_KEY`: Email service
- `ALERT_EMAIL_FROM`: Sender address
- `ALERT_EMAIL_TO`: Recipient address

### Scheduler
- `ENABLE_SCHEDULER`: true/false
- `DEFAULT_UPDATE_FREQUENCY`: daily/weekly/monthly

## Key Dependencies

- **gin-gonic/gin** - Web framework
- **gorm.io/gorm** - ORM
- **golang-jwt/jwt** - Authentication
- **go-co-op/gocron** - Job scheduling
- **sendgrid/sendgrid-go** - Email notifications
- **zerolog** - Structured logging
- **godotenv** - Environment management

## Recent Changes (Git Status)

Modified files requiring attention:
- [internal/api/handlers/cash_handler.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/internal/api/handlers/cash_handler.go?type=file&root=%252F) - Cash management updates
- [internal/api/handlers/portfolio_handler.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/internal/api/handlers/portfolio_handler.go?type=file&root=%252F) - Portfolio handling changes
- [internal/database/database.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/internal/database/database.go?type=file&root=%252F) - Database schema updates
- [internal/models/models.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/internal/models/models.go?type=file&root=%252F) - Model structure changes
- [internal/scheduler/scheduler.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/internal/scheduler/scheduler.go?type=file&root=%252F) - Scheduler updates

Recent commits:
- **1888068** - Add PortfolioID field with indexing to Stock and DeletedStock structs
- **410a44f** - Add PortfolioID to DeletedStock entry in DeleteStock handler
- **c4335bc** - feat: add JSON stock export and remove CSV stock export/import functionality
- **ac82f24** - feat: switch stock export from CSV to JSON format

## Development Workflow

### Running Locally
```bash
# Install dependencies
go mod download

# Create .env from example
cp env.example.txt .env

# Run server
go run main.go

# Or use make
make run
```

### Database Migrations
Auto-migration runs on startup (see [internal/database/database.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/internal/database/database.go?type=file&root=%252F))

### Deployment
Configured for Vercel serverless deployment via [api/index.go](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/api/index.go?type=file&root=%252F)

## Important Implementation Notes

### Portfolio Calculation Bug Fix
Fixed incorrect total calculation (documented in EXCHANGE_RATES.md):
- Issue: Weights were calculated during total accumulation
- Solution: Two-pass calculation
  1. First pass: Calculate total portfolio value in EUR
  2. Second pass: Calculate individual weights

### Soft Deletion Pattern
DeletedStock maintains audit trail:
- Full stock data preserved as JSON
- Includes PortfolioID with index for multi-portfolio support
- Tracks deletion metadata (reason, user, timestamp)
- Supports restoration workflow

### Exchange Rate Management
Manual rates are preserved:
- API updates only affect non-manual rates
- Manual flag prevents overwrite
- Default currencies (EUR, USD, DKK, GBP, RUB) cannot be deleted

## Testing

Test deployment script available: [test-deployment.sh](fleet-file://ui1ebg2m0ls975qt32d4/Users/jetbrains/myProjects/stock–backend/test-deployment.sh?type=file&root=%252F)

## External API Integration

### Alpha Vantage
Stock price and fundamental data retrieval

### Exchange Rate API
Currency conversion rates (v6.exchangerate-api.com)

### XAI/Grok
AI-powered analysis (optional)

## Logging
Structured logging via zerolog with:
- Timestamp inclusion
- Stdout output
- Error level tracking

## Security
- JWT-based authentication
- Password hashing (bcrypt)
- CORS configuration
- Admin-only endpoints

## Future Considerations
- Multi-portfolio support (PortfolioID already added to models)
- Enhanced historical analysis
- Advanced alert conditions
- API rate limiting
- Comprehensive test coverage

---

**Last Updated:** 2025-12-04
**Main Branch:** main
