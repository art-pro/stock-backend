# Exchange Rate Management

## Overview
The system now supports multi-currency portfolio tracking with EUR as the base currency. Exchange rates can be managed both manually and automatically via the Exchange Rate API.

## Features

### 1. Exchange Rate Table
- Displays all active exchange rates relative to EUR
- Shows conversion rates in both directions (EUR to currency and currency to EUR)
- Indicates whether rates are from API or manually set
- Located at the bottom of the dashboard page

### 2. Supported Currencies
Default currencies (cannot be deleted):
- **EUR** - Base currency (1.0000)
- **USD** - US Dollar
- **DKK** - Danish Krone
- **GBP** - British Pound
- **RUB** - Russian Ruble

Additional currencies can be added as needed.

### 3. Rate Management

#### Automatic Updates
- Click "Refresh Rates" to fetch latest rates from Exchange Rate API
- Requires `EXCHANGE_RATE_API_KEY` environment variable
- API: https://v6.exchangerate-api.com/
- Manual rates are preserved during API updates

#### Manual Updates
- Click the edit icon next to any rate
- Enter new rate and save
- Rate will be marked as "Manual" and preserved during API refreshes

#### Adding New Currencies
- Click "Add Currency" button
- Enter 3-letter currency code (e.g., SEK for Swedish Krona)
- Enter exchange rate to EUR
- New currency will be marked as manually added

#### Deleting Currencies
- Custom currencies can be deleted (trash icon)
- Default currencies are protected and cannot be deleted
- Deleted currencies are soft-deleted (marked as inactive)

## Portfolio Calculations

### Currency Conversion
All portfolio values are now calculated in EUR as the base currency:
1. Stock value in local currency = shares × current price
2. Stock value in EUR = stock value / exchange rate
3. Total portfolio value = sum of all stock values in EUR

### Display Values
- Portfolio total is shown in EUR
- Individual stock values can be in any supported currency
- Weights are calculated based on EUR values

## API Configuration

### Environment Variable
Add to `.env`:
```
EXCHANGE_RATE_API_KEY=your_api_key_here
```

Get a free API key from: https://www.exchangerate-api.com/

### API Limits
- Free tier: 1,500 requests/month
- Updates cached for 24 hours
- Manual refresh available anytime

## Database Schema

### ExchangeRate Table
- `currency_code` - 3-letter ISO currency code
- `rate` - Exchange rate to EUR
- `last_updated` - Timestamp of last update
- `is_active` - Whether currency is actively used
- `is_manual` - Whether rate is manually set

## Bug Fixes

### Portfolio Sum Calculation
Fixed issue where portfolio total was incorrectly calculated due to:
- Weight calculation happening during total accumulation
- Incorrect currency conversion direction

Now uses two-pass calculation:
1. First pass: Calculate total portfolio value
2. Second pass: Calculate weights based on final total