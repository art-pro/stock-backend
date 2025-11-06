# Changelog

## Version 1.0.0 (2025-11-05)

### Features
- ✅ N/A values instead of mock data when Grok API is not available
- ✅ Progress indicators for stock updates
- ✅ Manual price editing with inline UI
- ✅ Improved Grok prompt to distinguish current price from fair value target
- ✅ Version display in UI (frontend + backend)
- ✅ Debug logging for Grok API calls
- ✅ Better error handling for 404 and API failures
- ✅ Sequential stock updates with visual progress bar

### API Endpoints
- `GET /api/version` - Get backend version info
- `PATCH /api/stocks/:id/price` - Update stock price manually

### Bug Fixes
- Fixed Grok confusion between current_price and consensus_target
- Fixed mock data showing realistic values instead of N/A
- Fixed missing progress indicators during updates
- Fixed frontend caching issues with stale stock IDs

### Backend Changes
- Added version constants in `internal/config/version.go`
- Enhanced `FetchAllStockData()` with detailed logging
- New `UpdateStockPrice()` handler for manual price updates
- Improved error messages with visual indicators (⚠️, ✓)

### Frontend Changes
- Added version display in dashboard header
- Added inline price editing with pencil icon
- Added progress bar for bulk updates
- Added spinning indicators for individual stock updates
- Improved N/A formatting for all numeric fields
- Auto-refresh on 404 errors with user notification



