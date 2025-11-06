# N/A Values and Progress Indicator Implementation

## Overview
This document describes the changes made to replace mock data with N/A values and add progress indicators while waiting for Grok API responses.

## Changes Made

### Backend Changes

#### 1. Modified Mock Data Function (`internal/services/external_api.go`)
- **File**: `internal/services/external_api.go`
- **Function**: `mockStockData()`
- **Changes**:
  - Replaced all generated mock values with zeros (0)
  - Set `Assessment` to "N/A" instead of calculated values
  - Returns an error to indicate data is unavailable
  - This ensures that when Grok API is not configured or fails, all stock data fields display as "N/A" in the frontend

**Before**: Mock data generated realistic-looking values (prices, ratios, etc.)
**After**: All numeric fields set to 0, which frontend interprets as "N/A"

### Frontend Changes

#### 2. Updated Stock Table Component (`components/StockTable.tsx`)
- Added `updatingStockIds` prop to track which stocks are currently being updated
- Created new formatting functions:
  - `formatNumber()`: Returns "N/A" for zero/null/undefined values
  - `formatCurrency()`: Returns "N/A" for zero/null/undefined values
  - `formatPercentage()`: Returns "N/A" for zero/null/undefined values with % sign
- Added visual indicators:
  - Spinning refresh icon next to ticker when stock is updating
  - Row opacity reduction (60%) when stock is updating
  - Disabled update/delete buttons during update
- Updated assessment badge to show "N/A" with gray styling
- Conditionally display currency symbols only when values exist

#### 3. Updated Dashboard Page (`app/dashboard/page.tsx`)
- Added state management:
  - `updatingStockIds`: Tracks which stocks are currently updating
  - `updateProgress`: Tracks progress of bulk updates (current/total)
- Modified `handleUpdateAll()`:
  - Updates stocks sequentially instead of all at once
  - Updates progress state after each stock
  - Shows progress in button text: "Updating (1/5)..."
- Modified `handleUpdateSingle()`:
  - Adds stock ID to updating list before update
  - Removes stock ID from list after update completes
- Added progress bar component:
  - Displays when bulk update is in progress
  - Shows current progress (e.g., "3 of 5")
  - Shows percentage complete
  - Animated progress bar with smooth transitions
- Passes `updatingStockIds` to StockTable component

#### 4. Updated Portfolio Summary Component (`components/PortfolioSummary.tsx`)
- Updated formatting functions to handle N/A values:
  - `formatCurrency()`: Returns "N/A" for zero/null/undefined
  - `formatPercent()`: Returns "N/A" for zero/null/undefined

#### 5. Updated Add Stock Modal (`components/AddStockModal.tsx`)
- Updated informational note to mention:
  - Data will be fetched from Grok AI
  - If Grok is not configured, data will display as "N/A"
  - Data will be updated when Grok returns real values

## User Experience

### When Grok is NOT Configured
1. User adds a new stock
2. Backend attempts to fetch from Grok, fails, calls `mockStockData()`
3. All numeric fields are set to 0
4. Frontend displays "N/A" for all data fields
5. User sees clearly that data is not available
6. Assessment badge shows "N/A" with gray styling

### When Grok IS Configured
1. User clicks "Update All Prices" button
2. Button text changes to "Updating (1/5)..."
3. Progress bar appears showing percentage complete
4. Each stock being updated shows a spinning icon next to its ticker
5. Row becomes slightly transparent during update
6. Update/Delete buttons are disabled during update
7. After all stocks complete, data is refreshed from backend
8. Progress bar disappears
9. User sees real data from Grok with proper values and colors

### Single Stock Update
1. User clicks refresh icon next to a stock
2. Icon starts spinning
3. Row becomes slightly transparent
4. Stock is added to `updatingStockIds` array
5. Backend fetches data from Grok
6. Frontend refreshes data
7. Stock is removed from `updatingStockIds`
8. Spinner stops, row returns to normal opacity

## Benefits

1. **Clear Data State**: Users immediately know when data is unavailable (N/A) vs. when it's real
2. **No Confusion**: No more mock/fake data that could be mistaken for real values
3. **Progress Feedback**: Users see exactly what's happening during updates
4. **Better UX**: Visual feedback prevents users from thinking the app is frozen
5. **Sequential Updates**: Updates happen one at a time, showing clear progress
6. **Responsive UI**: Buttons and rows disabled/dimmed during updates prevent double-clicks

## Testing Checklist

- [ ] Add stock without Grok API key → All fields show "N/A"
- [ ] Add stock with Grok API key → Real values appear
- [ ] Click "Update All Prices" → Progress bar shows correct progress
- [ ] Click single stock update → Spinner appears on that row only
- [ ] Verify N/A values don't break calculations
- [ ] Verify Assessment badge shows "N/A" with gray styling
- [ ] Verify currency symbols hidden when values are N/A
- [ ] Verify portfolio summary handles N/A values gracefully

## Technical Details

### Zero vs. N/A
- Backend stores: `0` (numeric zero)
- Frontend displays: `"N/A"` (string)
- This allows for proper type safety while providing clear UX

### Progress Tracking
- Uses React state to track which stocks are updating
- Sequential updates allow for clear progress visualization
- Each stock update is awaited before moving to next

### Error Handling
- If individual stock update fails, error is logged but process continues
- Failed updates don't break the progress bar
- User is notified after all updates complete

## Future Enhancements

1. Add retry mechanism for failed Grok API calls
2. Cache Grok responses for a period of time
3. Add "Last Updated" timestamp to each stock row
4. Add ability to manually input values when Grok is unavailable
5. Add notification when Grok API quota is exceeded



