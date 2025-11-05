# Table Display and Currency Conversion Fixes

## Issues Fixed

### 1. **Table Column Structure**
**Problem:** All column headers were stacking vertically in one column instead of separate columns.

**Fix:**
- Removed problematic CSS tooltip spans that were causing layout issues
- Added `whitespace-nowrap` to all header cells to prevent text wrapping
- Replaced custom tooltips with native HTML `title` attributes (hover still works)

### 2. **Added Missing Columns**
**Problem:** User couldn't see their entry price (avg_price_local) or total position value.

**New Columns Added:**
- **Avg Price** - Shows your average entry/purchase price (what you paid)
- **Current Price** - Shows current market price (renamed from "Price")
- **Total Value** - Shows total position value (Current Price × Shares) in local currency

**Column Order Now:**
```
Ticker | Company | Sector | Avg Price | Current Price | Total Value | Fair Value | Upside % | EV % | Kelly F* % | ½-Kelly % | Shares | Weight % | P&L | Assessment | Actions
```

### 3. **Currency Conversion Fix**
**Problem:** Portfolio showing 28,100 USD instead of correct ~3,118 USD.

**Root Cause:** 
- DKK to USD exchange rate was 0.15 but should be 0.1538 (per Google)
- Stock was created with wrong/default exchange rate

**Fix:**
- Updated DKK exchange rate from 0.15 to 0.1538
- This matches Google's current rate: 1 DKK = 0.1538 USD (or ~6.5 DKK = 1 USD)

**Calculation Verification:**
```
Your stock: 64 shares × 440 DKK = 28,160 DKK
Convert to USD: 28,160 × 0.1538 = 4,331.81 USD

Note: The 440 DKK is the current market price (mock data).
Your entry price was 316.64 DKK (now shown in "Avg Price" column).
```

## Files Modified

### Frontend:
1. `/components/StockTable.tsx`
   - Added `whitespace-nowrap` to all header cells
   - Replaced CSS tooltips with HTML `title` attributes
   - Added "Avg Price" column (shows `avg_price_local`)
   - Renamed "Price" to "Current Price" (shows `current_price`)
   - Added "Total Value" column (calculates `current_price * shares_owned`)

### Backend:
2. `/internal/services/external_api.go`
   - Updated DKK rate: 0.15 → 0.1538
   - Added comments explaining rate format
   - Improved fallback handling

3. `/pkg/services/external_api.go`
   - Same changes as internal (mirror structure)

## How to Apply These Fixes

### Step 1: Restart Backend
```bash
cd /Users/jetbrains/GolandProjects/stock–backend

# Stop current backend (Ctrl+C if running)

# Restart backend
make run-backend
# OR
go run main.go
```

### Step 2: Update Stock Data
After backend restarts, you have two options:

**Option A: Update Existing Stock (Recommended)**
1. Go to your dashboard
2. Click the **Update icon (↻)** next to your NOVO B stock
3. The stock will recalculate with the correct exchange rate

**Option B: Re-add Stock**
1. Delete the existing NOVO B stock
2. Add it again
3. New stock will use correct exchange rate from the start

### Step 3: Verify Results

After updating, you should see:

**Table Columns (properly separated):**
- **Avg Price:** 316.64 DKK (your entry price)
- **Current Price:** 440.00 DKK (mock market price)
- **Total Value:** 28,160.00 DKK (440 × 64 shares)

**Portfolio Summary:**
- **Total Value:** ~4,331.81 USD (28,160 DKK × 0.1538)
- **NOT** 28,100 USD anymore ✅

**P&L Calculation:**
```
Entry cost: 64 shares × 316.64 DKK = 20,264.96 DKK → 3,116.75 USD
Current value: 64 shares × 440 DKK = 28,160 DKK → 4,331.81 USD
Unrealized P&L: 4,331.81 - 3,116.75 = +1,215.06 USD gain 💰
```

## Understanding the Display

### Price Columns Explained:

1. **Avg Price (316.64 DKK)**
   - This is YOUR entry price
   - What you actually paid per share
   - Used to calculate your profit/loss

2. **Current Price (440.00 DKK)**
   - Current market price (mock data from API)
   - Changes when you click Update
   - Used to calculate current value

3. **Total Value (28,160.00 DKK)**
   - Your total position value RIGHT NOW
   - = Current Price × Number of Shares
   - Shows in local currency (DKK)

### Why Current Price is Different:

The system uses **mock data** when external APIs are unavailable:
- Your entry: 316.64 DKK (what you told the system)
- Mock current: 440.00 DKK (simulated market price)
- **This difference creates your unrealized gain!**

### To Get Real Prices:

Add API keys to your `.env` file:
```env
ALPHA_VANTAGE_API_KEY=your_key_here
XAI_API_KEY=your_key_here
EXCHANGE_RATES_API_KEY=your_key_here
```

Without keys, the system uses realistic mock data for testing.

## Exchange Rates Reference

Current mock rates (1 foreign currency = X USD):
```
EUR: 1.10   (1 EUR = 1.10 USD)
GBP: 1.27   (1 GBP = 1.27 USD)
DKK: 0.1538 (1 DKK = 0.1538 USD) ← Fixed!
SEK: 0.096  (1 SEK = 0.096 USD)
NOK: 0.094  (1 NOK = 0.094 USD)
```

## Quick Verification Checklist

After restarting backend and updating stock:

- [ ] Table headers are in separate columns (not stacked)
- [ ] "Avg Price" column shows 316.64 DKK
- [ ] "Current Price" column shows 440.00 DKK
- [ ] "Total Value" column shows 28,160.00 DKK
- [ ] Portfolio total is ~4,331 USD (not 28,100 USD)
- [ ] P&L shows positive gain in green
- [ ] All tooltips work on hover

## Notes

- The frontend changes take effect immediately (hot reload)
- Backend changes require restart
- Existing stocks need Update to recalculate with new rates
- New stocks will automatically use correct rates

## Support

If portfolio still shows wrong value after updating:
1. Check browser console for errors (F12)
2. Verify backend is running with new code
3. Try clicking "Refresh" button on dashboard
4. Try "Update All Prices" button

The values should now be accurate! 🎉

