# Fix 404 Stock Not Found Issue

## Problem
Your frontend is trying to update stock ID 1, but it doesn't exist in the database.

## Cause
The database was likely reset or the stock was deleted, but the frontend still has old data cached.

## Solution

### Option 1: Refresh the Page (EASIEST)
1. Click the browser refresh button
2. This will reload stocks from the database
3. You should now see the correct stocks (or no stocks if database is empty)

### Option 2: Check Database
```bash
# Connect to your database and run:
SELECT id, ticker, company_name FROM stocks;

# This will show you what stocks actually exist
```

### Option 3: Clear and Start Fresh
1. Delete all stocks from the frontend (they'll fail with 404, that's OK)
2. Click "Refresh" button in the frontend
3. Click "Add Stock" to add a new stock
4. New stock will have the correct ID

## Why This Happens
- Frontend caches stock data (ID, ticker, etc.)
- If backend database is reset/cleared, IDs no longer match
- Refreshing the page fetches fresh data from backend

