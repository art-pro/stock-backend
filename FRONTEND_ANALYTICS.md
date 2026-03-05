# Frontend Analytics Integration Guide

This document describes the backend analytics endpoints and the frontend changes needed to integrate them into the analytics page.

## Backend Endpoints

### 1. GET /api/analytics/top-losers

Returns stocks with the worst unrealized P&L, sorted from worst to best.

**Authentication:** Required (JWT token)

**Query Parameters:**
- `portfolio_id` (optional): Portfolio to query. If omitted, uses default portfolio.
- `limit` (optional, default: 10, max: 100): Number of stocks to return.
- `min_shares` (optional, default: 1): Minimum shares owned to include in results.

**Response Format:**
```json
{
  "losers": [
    {
      "ticker": "LOSS1",
      "company_name": "Big Loss Inc",
      "sector": "Technology",
      "currency": "USD",
      "current_price": 80.0,
      "unrealized_pnl": -2000.0,
      "unrealized_pnl_pct": -20.0,
      "shares_owned": 100,
      "avg_price_local": 100.0,
      "current_value_usd": 8000.0,
      "weight": 0.1,
      "expected_value": -5.0,
      "assessment": "Sell",
      "buy_zone_status": "outside buy zone",
      "sell_zone_status": "In sell zone"
    }
  ],
  "count": 1,
  "meta": {
    "portfolio_id": 1,
    "limit": 10,
    "min_shares": 1
  }
}
```

**Response Fields:**
- `unrealized_pnl`: Total unrealized P&L in USD
- `unrealized_pnl_pct`: Percentage gain/loss calculated as `((current_price - avg_price) / avg_price) * 100`
- `weight`: Portfolio allocation as fraction (0-1), multiply by 100 for percentage
- `expected_value`: Current EV percentage
- `assessment`: Stock recommendation (Add/Hold/Trim/Sell)
- `buy_zone_status`: Buy zone status from calculation engine
- `sell_zone_status`: Sell zone status from calculation engine

**HTTP Status Codes:**
- `200 OK`: Success
- `400 Bad Request`: Invalid portfolio_id parameter
- `401 Unauthorized`: Missing or invalid JWT token
- `500 Internal Server Error`: Database or server error

**Cache Headers:** `Cache-Control: private, max-age=60, stale-while-revalidate=120`

**Example Usage:**
```javascript
// Fetch top 5 losers with at least 10 shares
const response = await fetch('/api/analytics/top-losers?limit=5&min_shares=10', {
  headers: {
    'Authorization': `Bearer ${token}`
  }
});
const data = await response.json();
```

---

### 2. GET /api/analytics/top-movers

Returns top gaining/losing stocks by price change and EV change over a specified timeframe.

**Authentication:** Required (JWT token)

**Query Parameters:**
- `portfolio_id` (optional): Portfolio to query. If omitted, uses default portfolio.
- `timeframe` (optional, default: "24h"): Time period for comparison. Valid values: `24h`, `7d`, `30d`
- `limit` (optional, default: 5, max: 20): Number of stocks per category.

**Response Format:**
```json
{
  "timeframe": "24h",
  "top_gainers": [
    {
      "stock_id": 1,
      "ticker": "GAINER1",
      "company_name": "Big Gainer Inc",
      "sector": "Technology",
      "current_price": 110.0,
      "previous_price": 100.0,
      "price_change": 10.0,
      "price_change_percent": 10.0,
      "current_ev": 15.0,
      "previous_ev": 10.0,
      "ev_change": 5.0,
      "current_assessment": "Add",
      "previous_assessment": "Hold",
      "last_updated": "2026-03-05T10:30:00Z"
    }
  ],
  "top_losers": [
    {
      "ticker": "LOSER1",
      "price_change_percent": -20.0,
      "ev_change": -7.0,
      ...
    }
  ],
  "biggest_ev_rises": [
    {
      "ticker": "EVUP1",
      "ev_change": 8.0,
      ...
    }
  ],
  "biggest_ev_drops": [
    {
      "ticker": "EVDOWN1",
      "ev_change": -10.0,
      ...
    }
  ],
  "generated_at": "2026-03-05T12:00:00Z"
}
```

**Response Fields:**
- `top_gainers`: Stocks with highest positive price change percentage
- `top_losers`: Stocks with lowest (most negative) price change percentage
- `biggest_ev_rises`: Stocks with largest EV increase
- `biggest_ev_drops`: Stocks with largest EV decrease
- `price_change`: Absolute price change in local currency
- `price_change_percent`: Percentage price change
- `ev_change`: Absolute EV change in percentage points

**HTTP Status Codes:**
- `200 OK`: Success (returns empty arrays if no history data available)
- `400 Bad Request`: Invalid portfolio_id or timeframe parameter
- `401 Unauthorized`: Missing or invalid JWT token
- `500 Internal Server Error`: Database or server error

**Cache Headers:** `Cache-Control: private, max-age=300, stale-while-revalidate=600`

**Example Usage:**
```javascript
// Fetch top movers for the last 7 days
const response = await fetch('/api/analytics/top-movers?timeframe=7d&limit=10', {
  headers: {
    'Authorization': `Bearer ${token}`
  }
});
const data = await response.json();
```

---

## Frontend Integration Requirements

### 1. Analytics Page Structure

Create or update the analytics page to include the following sections:

#### A. Top Losers Section

**Display Components:**
- Table or card grid showing top losing positions
- Sortable columns (if table format)
- Color-coded indicators for negative P&L
- Filter controls for:
  - Limit (dropdown: 5, 10, 20, 50)
  - Minimum shares (input field)

**Table Columns (Recommended):**
1. **Ticker** - Link to stock details page
2. **Company Name**
3. **Sector** - Color-coded badge
4. **Unrealized P&L** - Red text with dollar amount
5. **Unrealized P&L %** - Red text with percentage
6. **Current Price** - With currency symbol
7. **Shares Owned**
8. **Weight** - Percentage of portfolio
9. **Assessment** - Badge with color coding (Sell=red, Trim=orange)
10. **Actions** - View details, sell, etc.

**Visual Design Recommendations:**
- Use red/negative color scheme for P&L values
- Add warning icons for stocks in "Sell" assessment
- Show portfolio weight as progress bar
- Add tooltips for EV, buy zone, and sell zone status

**State Management:**
```typescript
interface TopLosersState {
  losers: TopLoser[];
  loading: boolean;
  error: string | null;
  filters: {
    limit: number;
    minShares: number;
  };
}

interface TopLoser {
  ticker: string;
  company_name: string;
  sector: string;
  currency: string;
  current_price: number;
  unrealized_pnl: number;
  unrealized_pnl_pct: number;
  shares_owned: number;
  avg_price_local: number;
  current_value_usd: number;
  weight: number;
  expected_value: number;
  assessment: string;
  buy_zone_status: string;
  sell_zone_status: string;
}
```

**API Integration Example (React/TypeScript):**
```typescript
import { useState, useEffect } from 'react';

const useTopLosers = (limit: number = 10, minShares: number = 1) => {
  const [data, setData] = useState<TopLosersState>({
    losers: [],
    loading: true,
    error: null,
    filters: { limit, minShares }
  });

  useEffect(() => {
    const fetchTopLosers = async () => {
      try {
        setData(prev => ({ ...prev, loading: true }));
        const token = localStorage.getItem('jwt_token');

        const response = await fetch(
          `/api/analytics/top-losers?limit=${limit}&min_shares=${minShares}`,
          {
            headers: {
              'Authorization': `Bearer ${token}`,
              'Content-Type': 'application/json'
            }
          }
        );

        if (!response.ok) {
          throw new Error(`Failed to fetch top losers: ${response.statusText}`);
        }

        const result = await response.json();
        setData({
          losers: result.losers,
          loading: false,
          error: null,
          filters: { limit, minShares }
        });
      } catch (error) {
        setData(prev => ({
          ...prev,
          loading: false,
          error: error.message
        }));
      }
    };

    fetchTopLosers();
  }, [limit, minShares]);

  return data;
};
```

#### B. Top Movers Section

**Display Components:**
- Four subsections in 2x2 grid or tabs:
  1. Top Gainers (price)
  2. Top Losers (price)
  3. Biggest EV Rises
  4. Biggest EV Drops
- Timeframe selector (24h, 7d, 30d)
- Limit selector (5, 10, 20)
- Auto-refresh toggle

**Card/List Item Format:**
```
[Ticker] Company Name
Current: $110.00 (+10.0%)
Previous: $100.00
EV: 15.0% (↑5.0 pp)
Assessment: Add → Add
Last Updated: 2h ago
```

**State Management:**
```typescript
interface TopMoversState {
  topGainers: MoverData[];
  topLosers: MoverData[];
  biggestEVRises: MoverData[];
  biggestEVDrops: MoverData[];
  timeframe: '24h' | '7d' | '30d';
  loading: boolean;
  error: string | null;
  generatedAt: string;
}

interface MoverData {
  stock_id: number;
  ticker: string;
  company_name: string;
  sector: string;
  current_price: number;
  previous_price: number;
  price_change: number;
  price_change_percent: number;
  current_ev: number;
  previous_ev: number;
  ev_change: number;
  current_assessment: string;
  previous_assessment: string;
  last_updated: string;
}
```

**API Integration Example:**
```typescript
const useTopMovers = (timeframe: '24h' | '7d' | '30d' = '24h', limit: number = 5) => {
  const [data, setData] = useState<TopMoversState>({
    topGainers: [],
    topLosers: [],
    biggestEVRises: [],
    biggestEVDrops: [],
    timeframe,
    loading: true,
    error: null,
    generatedAt: ''
  });

  useEffect(() => {
    const fetchTopMovers = async () => {
      try {
        setData(prev => ({ ...prev, loading: true }));
        const token = localStorage.getItem('jwt_token');

        const response = await fetch(
          `/api/analytics/top-movers?timeframe=${timeframe}&limit=${limit}`,
          {
            headers: {
              'Authorization': `Bearer ${token}`,
              'Content-Type': 'application/json'
            }
          }
        );

        if (!response.ok) {
          throw new Error(`Failed to fetch top movers: ${response.statusText}`);
        }

        const result = await response.json();
        setData({
          topGainers: result.top_gainers,
          topLosers: result.top_losers,
          biggestEVRises: result.biggest_ev_rises,
          biggestEVDrops: result.biggest_ev_drops,
          timeframe: result.timeframe,
          loading: false,
          error: null,
          generatedAt: result.generated_at
        });
      } catch (error) {
        setData(prev => ({
          ...prev,
          loading: false,
          error: error.message
        }));
      }
    };

    fetchTopMovers();
  }, [timeframe, limit]);

  return data;
};
```

### 2. UI/UX Recommendations

**Layout:**
```
┌─────────────────────────────────────────────────┐
│ Analytics Dashboard                             │
├─────────────────────────────────────────────────┤
│ ┌─────────────────────────────────────────────┐ │
│ │ Top Losers                    [Filters ▼]   │ │
│ │ ┌─────┬──────┬────────┬──────┬──────┐      │ │
│ │ │Tick │ Name │ Sector │ P&L  │ P&L% │      │ │
│ │ ├─────┼──────┼────────┼──────┼──────┤      │ │
│ │ │LOSS1│ Big  │  Tech  │-$2000│ -20% │      │ │
│ │ └─────┴──────┴────────┴──────┴──────┘      │ │
│ └─────────────────────────────────────────────┘ │
│                                                 │
│ ┌─────────────────────────────────────────────┐ │
│ │ Top Movers (24h) [▼]          [Limit: 5 ▼] │ │
│ │                                             │ │
│ │ ┌─────────────┐ ┌─────────────┐            │ │
│ │ │Top Gainers  │ │Top Losers   │            │ │
│ │ │ GAIN1 +10.0%│ │ LOSS1 -20.0%│            │ │
│ │ └─────────────┘ └─────────────┘            │ │
│ │                                             │ │
│ │ ┌─────────────┐ ┌─────────────┐            │ │
│ │ │EV Rises     │ │EV Drops     │            │ │
│ │ │ EVUP1 +8.0pp│ │ DOWN1 -10.0pp│           │ │
│ │ └─────────────┘ └─────────────┘            │ │
│ └─────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

**Color Coding:**
- **Positive values:** Green (#10b981 or similar)
- **Negative values:** Red (#ef4444 or similar)
- **Assessment badges:**
  - Add: Green
  - Hold: Blue
  - Trim: Orange
  - Sell: Red

**Loading States:**
- Skeleton loaders for initial load
- Spinner or progress indicator for refreshes
- Disable filters during loading

**Error States:**
- Toast notification for API errors
- Inline error message with retry button
- Fallback to cached data if available

**Empty States:**
- "No losing positions found" for top losers
- "No historical data available" for top movers
- Suggestion to add stocks or wait for history to accumulate

### 3. Additional Features (Optional)

**Export Functionality:**
- CSV export button for top losers table
- PDF report generation
- Email scheduled reports

**Filters & Sorting:**
- Filter by sector
- Filter by assessment (only show "Sell" recommendations)
- Sort by any column (P&L, %, weight, etc.)

**Drill-Down:**
- Click ticker to view stock details
- Click sector to view all stocks in sector
- Hover for tooltips with additional context

**Auto-Refresh:**
- Toggle for auto-refresh every 60 seconds
- Manual refresh button
- Last updated timestamp display

**Alerts Integration:**
- Set alerts for when a stock enters top losers
- Notification when P&L drops below threshold
- Badge showing number of new entries since last view

### 4. Testing Checklist

Frontend developers should test:

- [ ] Top losers table displays correctly with default parameters
- [ ] Limit filter changes number of results
- [ ] Min shares filter excludes small positions
- [ ] Unrealized P&L percentage calculates correctly
- [ ] Portfolio weight displays as percentage (multiply by 100)
- [ ] Assessment badges render with correct colors
- [ ] Links to stock details work
- [ ] Top movers grid displays all four categories
- [ ] Timeframe selector changes data correctly
- [ ] Historical data displays when available
- [ ] Empty state shows when no history exists
- [ ] Error handling works for API failures
- [ ] Loading states appear during fetch
- [ ] Responsive design works on mobile
- [ ] Auth token included in all requests
- [ ] 401 errors redirect to login page

### 5. Performance Considerations

**Caching:**
- Implement client-side caching for 60 seconds (matches backend cache)
- Use React Query, SWR, or similar for automatic cache management
- Invalidate cache on manual refresh

**Optimization:**
- Virtualize long lists (if showing 50+ losers)
- Debounce filter changes
- Use memo/useMemo for expensive calculations
- Lazy load historical charts if added

**Network:**
- Respect backend cache headers
- Implement retry logic with exponential backoff
- Show stale data while revalidating
- Cancel pending requests on unmount

---

## Migration Notes

If you already have an analytics page:

1. **Add new endpoints** to your API client/service layer
2. **Create new components** or update existing analytics components
3. **Update routing** if adding new analytics sub-pages
4. **Add filters state** to analytics page state management
5. **Update tests** to cover new analytics features
6. **Update documentation** for users explaining new features

## Support

For backend issues or API questions:
- Check backend logs for error details
- Verify JWT token is valid and not expired
- Ensure portfolio_id exists if provided
- Check that stocks have historical data for top movers

For calculation questions:
- Review `CLAUDE.md` "Calculation Engine" section
- Check `pkg/services/calculations.go` for formulas
- Unrealized P&L is stored in USD for backward compatibility
- EV and assessments follow probabilistic framework

---

**Document Version:** 1.0
**Last Updated:** 2026-03-05
**Backend Version Required:** v2.11.0+
