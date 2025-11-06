# Debug Grok Connection - XAI_API_KEY is Set But Still Fails

## The Problem
- ✅ `XAI_API_KEY` is set in Vercel environment variables
- ✅ Backend has been redeployed
- ❌ Still showing "no API configured - stock data unavailable"

---

## Step 1: Check Vercel Function Logs

The backend has extensive debug logging. Let's see what's actually happening:

### How to Access Logs:
1. Go to: https://vercel.com/artpros-projects/stock-assess-app
2. Click **"Deployments"** tab
3. Click on the **latest deployment** (should be most recent)
4. Click **"Functions"** tab
5. You'll see a list of serverless functions - click on any that ran recently
6. Look for logs with these keywords:
   - `"Alpha Vantage"`
   - `"Grok API"`
   - `"XAI_API_KEY"`
   - `"API request error"`

### What to Look For:

#### Scenario A: Key Not Found
```
⚠ Grok API: XAI_API_KEY not configured
```
**Means**: Environment variable isn't being read
**Fix**: Redeploy with "Redeploy with existing build cache cleared"

#### Scenario B: Grok API Error
```
Grok API request error: <some error>
```
or
```
Grok API returned status: 401
Grok API error response: {"error": "Invalid API key"}
```
**Means**: API key is invalid or expired
**Fix**: Check your API key at https://console.x.ai/

#### Scenario C: Grok API Rate Limit
```
Grok API returned status: 429
Grok API error response: {"error": "Rate limit exceeded"}
```
**Means**: Too many requests
**Fix**: Wait a few minutes and try again

#### Scenario D: Network Error
```
Grok API request error: dial tcp: lookup api.x.ai: no such host
```
**Means**: Network connectivity issue from Vercel
**Fix**: Temporary outage, try again later

---

## Step 2: Test API Status Endpoint Directly

Let's test the backend API directly to see what it returns:

### Using Browser:
Open this URL (replace with your backend URL):
```
https://stock-assess-app.vercel.app/api/api-status
```

### Using curl (from terminal):
```bash
curl https://stock-assess-app.vercel.app/api/api-status
```

### Expected Response:

**If working correctly:**
```json
{
  "grok": {
    "configured": true,
    "status": "connected",
    "using_mock": false
  },
  "timestamp": "2025-11-06T..."
}
```

**If key not found:**
```json
{
  "grok": {
    "configured": false,
    "status": "not_configured",
    "using_mock": true,
    "message": "Using mock data. Add XAI_API_KEY to .env for real data"
  }
}
```

**If API error:**
```json
{
  "grok": {
    "configured": true,
    "status": "error",
    "error": "actual error message here"
  }
}
```

---

## Step 3: Force Clear Vercel Build Cache

Sometimes Vercel caches the old build. Let's force a clean rebuild:

### Option A: Redeploy with Cache Clear
1. Go to **Deployments** tab
2. Click **⋯** on latest deployment
3. Click **"Redeploy"**
4. ✅ **Enable "Redeploy with existing build cache cleared"** (IMPORTANT!)
5. Click **"Redeploy"**

### Option B: Make a Dummy Code Change
```bash
cd /Users/jetbrains/GolandProjects/stock–backend

# Add a comment to trigger rebuild
echo "# Build cache clear - $(date)" >> pkg/config/version.go

# Commit and push
git add .
git commit -m "Force rebuild - clear cache"
git push origin main
```

---

## Step 4: Verify Environment Variable in Runtime

Let's add temporary debug logging to verify the key is actually loaded:

Add this to `pkg/config/config.go`:

```go
func Load() *Config {
	enableScheduler := os.Getenv("ENABLE_SCHEDULER") == "true"
	
	// DEBUG: Log if XAI_API_KEY is loaded (remove after debugging)
	xaiKey := os.Getenv("XAI_API_KEY")
	if xaiKey != "" {
		fmt.Printf("✓ XAI_API_KEY loaded: %s...%s\n", xaiKey[:10], xaiKey[len(xaiKey)-10:])
	} else {
		fmt.Println("✗ XAI_API_KEY NOT FOUND in environment")
	}
	
	return &Config{
		// ... rest of config
	}
}
```

Then redeploy and check logs. You should see the debug message.

---

## Step 5: Test Grok API Key Directly

Let's verify the API key works outside of your app:

```bash
# Replace with your actual API key
export XAI_API_KEY="xai-your-actual-api-key-here"

# Test the xAI API directly
curl -X POST https://api.x.ai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $XAI_API_KEY" \
  -d '{
    "model": "grok-4-latest",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Say hello"}
    ]
  }'
```

**Expected response:**
```json
{
  "id": "chatcmpl-...",
  "model": "grok-4-latest",
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "Hello! How can I help you today?"
      }
    }
  ]
}
```

**If error:**
```json
{
  "error": {
    "message": "Invalid API key",
    "type": "invalid_request_error"
  }
}
```

---

## Step 6: Check for Typos in Environment Variable Name

Double-check the variable name is **exactly** `XAI_API_KEY`:
- ✅ Correct: `XAI_API_KEY`
- ❌ Wrong: `XAI_API_KEY ` (trailing space)
- ❌ Wrong: `XAI_API_KEY_` (underscore)
- ❌ Wrong: `XAI_API_KEYs` (plural)
- ❌ Wrong: `XAI_KEY` (missing _API)

In Vercel, click the **Edit** button next to `XAI_API_KEY` and verify:
1. Name is exact: `XAI_API_KEY`
2. Value starts with: `xai-`
3. Value has no extra spaces or characters
4. All environments are selected (Production, Preview, Development)

---

## Step 7: Try Different Grok Model

The code uses `grok-4-latest`. Maybe this model isn't available. Let's try `grok-beta`:

Edit `pkg/services/external_api.go` line 335:

```go
// Try grok-beta instead
reqBody := GrokStockRequest{
	Model: "grok-beta",  // Changed from "grok-4-latest"
	// ...
}
```

Commit, push, and redeploy.

---

## Step 8: Simplify Test - Remove Alpha Vantage Fallback

Temporarily disable Alpha Vantage to force Grok usage:

Edit `pkg/services/external_api.go` around line 208:

```go
func (s *ExternalAPIService) FetchAllStockData(stock *models.Stock) error {
	var dataSource string = "Grok AI"
	var fairValueSource string

	// TEMPORARILY COMMENT OUT ALPHA VANTAGE
	/*
	if s.cfg.AlphaVantageAPIKey != "" {
		// ... Alpha Vantage code ...
	}
	*/
	
	// Step 2: Try Grok immediately
	if s.cfg.XAIAPIKey == "" {
		fmt.Println("DEBUG: XAI_API_KEY is empty")
		return s.mockStockData(stock)
	}
	
	fmt.Printf("DEBUG: XAI_API_KEY found: %s...%s\n", 
		s.cfg.XAIAPIKey[:10], 
		s.cfg.XAIAPIKey[len(s.cfg.XAIAPIKey)-10:])
	
	// Continue with Grok...
}
```

---

## Most Likely Issues (in order):

1. **Build cache not cleared** → Use "Redeploy with existing build cache cleared"
2. **Wrong Grok model** → Change from `grok-4-latest` to `grok-beta`
3. **API key invalid/expired** → Generate new key at https://console.x.ai/
4. **Rate limit hit** → Wait 5-10 minutes
5. **Network issue** → Check Vercel status page

---

## Quick Test Checklist

- [ ] Checked Vercel function logs for actual error
- [ ] Tested `/api/api-status` endpoint directly
- [ ] Verified `XAI_API_KEY` name has no typos
- [ ] Redeployed with "clear build cache" enabled
- [ ] Tested API key with curl command (works outside app)
- [ ] Added debug logging to see if key is loaded
- [ ] Tried different Grok model (`grok-beta`)

---

## Emergency Fallback: Use Alpha Vantage Instead

If Grok still doesn't work, use Alpha Vantage as primary data source:

1. Get free API key from: https://www.alphavantage.co/support/#api-key
2. Add to Vercel: `ALPHA_VANTAGE_API_KEY=your-key`
3. Redeploy

Alpha Vantage provides:
- Real-time stock prices ✅
- Beta values ✅
- Analyst consensus targets ✅
- P/E, dividend yield ✅

Your app will work with Alpha Vantage alone (Grok is optional).

---

**Next Step**: Please share what you see in the Vercel function logs or the output from the `/api/api-status` endpoint, and I'll help diagnose the exact issue!

