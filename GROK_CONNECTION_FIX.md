# Grok Connection Error: "no API configured"

## Problem
Frontend shows: **"Grok API Error: no API configured - stock data unavailable"**

This error appears when the backend cannot find the `XAI_API_KEY` environment variable.

---

## Root Cause

The code path is:
1. `FetchAllStockData()` tries Alpha Vantage first
2. Alpha Vantage fails (no key or API limit)
3. Checks for `XAI_API_KEY`
4. If `XAI_API_KEY == ""`, calls `mockStockData()`
5. `mockStockData()` returns error: `"no API configured - stock data unavailable"`

**This means Vercel backend doesn't have XAI_API_KEY environment variable set!**

---

## Solution: Fix Vercel Environment Variables

### Step 1: Check Current Deployment
1. Go to: https://vercel.com/artpros-projects/stock-assess-app
2. Click **"Deployments"** tab
3. Check if latest commit is deployed (should show commit: "Update version to v1.1.0")

### Step 2: Verify Environment Variables
1. Go to: https://vercel.com/artpros-projects/stock-assess-app/settings/environment-variables
2. **Required variables** (must be set):
   ```
   XAI_API_KEY=xai-...your-key...
   DATABASE_URL=postgresql://...
   FRONTEND_URL=https://stock-frontend-silk.vercel.app
   JWT_SECRET=your-secret
   ```

3. **Optional but recommended**:
   ```
   ALPHA_VANTAGE_API_KEY=your-key
   EXCHANGE_RATES_API_KEY=your-key
   ```

### Step 3: Add XAI_API_KEY if Missing
If `XAI_API_KEY` is not in the list:

1. Click **"Add New"** button
2. Set:
   - **Key**: `XAI_API_KEY`
   - **Value**: Your xAI API key (starts with `xai-`)
   - **Environment**: Select all (Production, Preview, Development)
3. Click **"Save"**

### Step 4: Redeploy After Adding Variables
**Important**: Adding/changing environment variables requires redeployment!

1. Go back to **"Deployments"** tab
2. Find the latest deployment
3. Click the three dots (**⋯**) next to it
4. Click **"Redeploy"**
5. Wait 2-3 minutes

### Step 5: Verify Connection
1. Refresh your frontend
2. Check top-right corner:
   - ✅ Should show: **"Grok: Connected"** with green dot
   - ❌ If still error, check Vercel logs

---

## How to Get Your xAI API Key

If you don't have your xAI API key:

1. Go to: https://console.x.ai/
2. Sign in with your account
3. Click **"API Keys"** in sidebar
4. Copy your existing key OR click **"Create API Key"**
5. Copy the key (starts with `xai-`)
6. Add it to Vercel environment variables (see Step 3 above)

---

## Verify Locally (Optional Test)

To test if the Grok prompt works locally:

```bash
# Set environment variable (temporary, just for testing)
export XAI_API_KEY="xai-your-key-here"

# Run backend locally
cd /Users/jetbrains/GolandProjects/stock–backend
go run main.go
```

Then in another terminal:
```bash
# Test API status
curl http://localhost:8080/api/api-status

# Should return:
# {
#   "grok": {
#     "configured": true,
#     "status": "connected",
#     "using_mock": false
#   }
# }
```

---

## Common Issues

### Issue 1: "XAI_API_KEY is set but still shows error"
**Solution**: Redeploy after adding the variable. Vercel needs restart to pick up new env vars.

### Issue 2: "Grok returns error after adding key"
**Possible causes**:
- Invalid API key (check for typos)
- API key expired or revoked
- xAI API rate limit exceeded
- Network issue from Vercel to xAI

**Check Vercel logs**:
1. Go to deployment details
2. Click "Functions" tab
3. Look for error messages

### Issue 3: "Sometimes works, sometimes doesn't"
**Cause**: Alpha Vantage rate limits (5 calls/minute free tier)
**Solution**: Add `ALPHA_VANTAGE_API_KEY` or accept that first call uses Alpha Vantage, subsequent calls use Grok.

---

## Expected Behavior After Fix

### API Status Check
- **Before fix**: `"status": "not_configured"` or `"status": "error"`
- **After fix**: `"status": "connected"`

### Adding New Stock
- **Before fix**: Stock saved with all N/A values
- **After fix**: Stock fetched with real data from Grok/Alpha Vantage

### Frontend Display
- **Before fix**: Red error banner "Grok API Error: no API configured"
- **After fix**: Green dot "Grok: Connected" or Test badge showing status

---

## Quick Checklist

- [ ] Latest backend code deployed to Vercel (v1.1.0)
- [ ] `XAI_API_KEY` added to Vercel environment variables
- [ ] Backend redeployed after adding variables
- [ ] Frontend shows "Grok: Connected"
- [ ] Can add stocks and get real data (not N/A)

---

## Contact xAI Support

If your API key doesn't work:
- Email: support@x.ai
- Console: https://console.x.ai/
- Check billing/quota: https://console.x.ai/billing

---

**Version**: v1.1.0  
**Last Updated**: November 6, 2025

