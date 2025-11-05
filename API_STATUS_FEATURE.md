# API Status Indicator Feature

## Overview

A visual indicator and test button that shows the connection status to Grok API in real-time.

## Features

### 1. **Header Status Indicator**

Located in the dashboard header, next to Settings:

**States:**
- 🟢 **Grok Connected** (green, pulsing) - API key configured and working
- 🟡 **Mock Data** (yellow) - No API key, using mock data
- 🔴 **Grok Error** (red) - API key present but connection failed

### 2. **Test Button**

Click "Test" to verify Grok connection instantly:
- Tests API connectivity
- Updates status in real-time
- Shows "..." while testing

### 3. **Detailed Status Banner**

Below the portfolio summary:

**When using Mock Data:**
```
ℹ️ Using mock data. Add XAI_API_KEY to .env for real Grok data. [Get API Key]
```

**When Connected:**
```
✓ Connected to Grok - Real-time data active
```

**When Error:**
```
⚠️ Grok API Error: [error message]
```

## Backend Endpoint

### GET `/api/api-status`

Returns the current API configuration and connection status.

**Response (No API Key):**
```json
{
  "grok": {
    "configured": false,
    "status": "not_configured",
    "using_mock": true,
    "message": "Using mock data. Add XAI_API_KEY to .env for real data"
  },
  "timestamp": "2025-01-15T10:30:00Z"
}
```

**Response (Connected):**
```json
{
  "grok": {
    "configured": true,
    "status": "connected",
    "using_mock": false
  },
  "timestamp": "2025-01-15T10:30:00Z"
}
```

**Response (Error):**
```json
{
  "grok": {
    "configured": true,
    "status": "error",
    "error": "API key invalid or rate limit exceeded"
  },
  "timestamp": "2025-01-15T10:30:00Z"
}
```

## How It Works

### Backend Logic

1. **Check Configuration:**
   - Reads `XAI_API_KEY` from config
   - If empty → status: "not_configured"

2. **Test Connection:**
   - Creates test stock: `TEST` / Technology / USD
   - Calls `FetchAllStockData()`
   - Success → status: "connected"
   - Error → status: "error" + error message

3. **Return Status:**
   - JSON response with status and timestamp

### Frontend Logic

1. **Auto-Check on Load:**
   - `useEffect` calls `checkAPIStatus()` on page load
   - Stores status in React state

2. **Manual Test:**
   - Click "Test" button
   - Calls `handleTestAPI()`
   - Shows loading state ("...")
   - Updates status indicator

3. **Visual Feedback:**
   - Header indicator changes color/text
   - Banner shows detailed message
   - Link to get API key if needed

## User Experience

### Scenario 1: New User (No API Key)

1. **Dashboard loads**
2. **Header shows:** 🟡 Mock Data
3. **Banner shows:** "ℹ️ Using mock data. Add XAI_API_KEY to .env for real Grok data. [Get API Key]"
4. **User clicks "Get API Key"**
5. **Opens:** https://console.x.ai/
6. **User adds key to .env**
7. **Restarts backend**
8. **Header shows:** 🟢 Grok Connected
9. **Banner shows:** "✓ Connected to Grok - Real-time data active"

### Scenario 2: Existing User (API Key Present)

1. **Dashboard loads**
2. **Header shows:** 🟢 Grok Connected (pulsing green dot)
3. **Banner shows:** "✓ Connected to Grok - Real-time data active"
4. **User clicks "Test"**
5. **Button shows:** "..."
6. **Test completes**
7. **Status confirms:** Still connected

### Scenario 3: API Key Invalid

1. **Dashboard loads**
2. **Backend tests connection**
3. **Test fails**
4. **Header shows:** 🔴 Grok Error
5. **Banner shows:** "⚠️ Grok API Error: unauthorized"
6. **User checks API key**
7. **Fixes key in .env**
8. **Restarts backend**
9. **Clicks "Test"**
10. **Header updates:** 🟢 Grok Connected

## Configuration

### Backend (.env)

```env
# Add this to enable Grok
XAI_API_KEY=xai-your-api-key-here
```

### Frontend (Automatic)

No configuration needed. Automatically detects backend status.

## API Implementation

### Handler: `portfolio_handler.go`

```go
func (h *PortfolioHandler) GetAPIStatus(c *gin.Context) {
    // Check if configured
    status := gin.H{
        "grok": gin.H{
            "configured": h.cfg.XAIAPIKey != "",
        },
    }
    
    // Test connection if configured
    if h.cfg.XAIAPIKey != "" {
        testStock := models.Stock{
            Ticker: "TEST",
            CompanyName: "Test Company",
            Sector: "Technology",
            Currency: "USD",
        }
        
        err := h.apiService.FetchAllStockData(&testStock)
        if err != nil {
            status["grok"].(gin.H)["status"] = "error"
        } else {
            status["grok"].(gin.H)["status"] = "connected"
        }
    }
    
    c.JSON(http.StatusOK, status)
}
```

### Route: `router.go`

```go
// API Status routes
protected.GET("/api-status", portfolioHandler.GetAPIStatus)
```

### Frontend: `dashboard/page.tsx`

```typescript
const checkAPIStatus = async () => {
  const response = await portfolioAPI.getAPIStatus();
  setApiStatus(response.data);
};

// Auto-check on load
useEffect(() => {
  checkAPIStatus();
}, []);

// Manual test
const handleTestAPI = async () => {
  setCheckingAPI(true);
  await checkAPIStatus();
  setTimeout(() => setCheckingAPI(false), 1000);
};
```

## UI Components

### Header Indicator

```tsx
{apiStatus && (
  <div className="flex items-center space-x-2 px-3 py-2 rounded-lg bg-gray-700">
    {apiStatus.grok.status === 'connected' && (
      <>
        <div className="h-2 w-2 rounded-full bg-green-500 animate-pulse"></div>
        <span className="text-sm text-green-400 font-medium">Grok Connected</span>
      </>
    )}
    <button onClick={handleTestAPI}>
      {checkingAPI ? '...' : 'Test'}
    </button>
  </div>
)}
```

### Status Banner

```tsx
{apiStatus && apiStatus.grok.status === 'not_configured' && (
  <div className="bg-yellow-900 bg-opacity-30 border border-yellow-700 rounded-lg">
    <span>ℹ️ Using mock data. Add XAI_API_KEY to .env for real Grok data.</span>
    <a href="https://console.x.ai/" target="_blank">Get API Key</a>
  </div>
)}
```

## Testing

### Test the Feature

1. **Start backend without API key:**
```bash
cd /Users/jetbrains/GolandProjects/stock–backend
# Ensure no XAI_API_KEY in .env
make run-backend
```

2. **Open dashboard**
   - Should show: 🟡 Mock Data

3. **Click "Test" button**
   - Should show: "..." then stay on Mock Data

4. **Add API key to .env:**
```bash
echo "XAI_API_KEY=xai-test-key" >> .env
```

5. **Restart backend:**
```bash
make run-backend
```

6. **Refresh dashboard**
   - Should show: 🟢 Grok Connected (or 🔴 if key invalid)

7. **Click "Test" button**
   - Should re-verify connection

## Benefits

1. **User Awareness:**
   - Users know if they're using mock or real data
   - Clear indication of connection status
   - No confusion about data source

2. **Troubleshooting:**
   - Easy to diagnose API issues
   - Test button for quick verification
   - Error messages for debugging

3. **Onboarding:**
   - New users see how to configure Grok
   - Link to get API key
   - Clear path to real data

4. **Confidence:**
   - Green indicator = real data ✓
   - No guessing about connection status
   - Test anytime for peace of mind

## Future Enhancements

Possible additions:

1. **Connection History:**
   - Log of connection attempts
   - Success/failure timestamps
   - Rate limit tracking

2. **API Usage Stats:**
   - Number of requests today
   - Cost tracking
   - Rate limit warnings

3. **Auto-Retry:**
   - Automatic reconnection on failure
   - Exponential backoff
   - Success notifications

4. **Multiple Providers:**
   - Show status for multiple APIs
   - Fallback chain visualization
   - Provider selection

## Summary

The API Status Indicator provides:

✅ **Real-time connection status**
✅ **Visual feedback** (colored indicator)
✅ **Manual testing** (Test button)
✅ **Clear messaging** (Mock Data / Connected / Error)
✅ **Easy setup** (Get API Key link)
✅ **Automatic detection** (Checks on load)

Users always know:
- What data source they're using (Mock vs Real)
- If Grok is connected and working
- How to configure it if needed
- How to test the connection

The feature improves transparency and user confidence! 🎯

