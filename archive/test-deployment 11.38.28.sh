#!/bin/bash

# Test Vercel Deployment - Stock Portfolio Backend
# This script helps you find and test your production API endpoint

echo "🔍 Testing Stock Portfolio Backend Deployment"
echo "=============================================="
echo ""

# The URL you tried (preview/branch deployment - protected)
PREVIEW_URL="https://stock-assess-app-backend-git-main-artpros-projects.vercel.app"

# Your likely production URL (public by default)
PRODUCTION_URL="https://stock-assess-app-backend.vercel.app"

echo "📍 Preview URL (protected): $PREVIEW_URL"
echo "📍 Production URL (should be public): $PRODUCTION_URL"
echo ""

echo "Testing Production API..."
echo "-------------------------"
RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" "$PRODUCTION_URL/api/stocks")

if [ "$RESPONSE" = "401" ]; then
    echo "✅ SUCCESS! Backend is working!"
    echo "   Status: 401 Unauthorized (expected - means API requires JWT)"
    echo ""
    echo "🎉 Your deployment configuration is CORRECT!"
    echo ""
    echo "Next steps:"
    echo "1. Update your frontend NEXT_PUBLIC_API_URL to: $PRODUCTION_URL/api"
    echo "2. Test login in your browser"
    echo "3. Add some stocks and verify everything works"
    echo ""
    echo "To test login via command line:"
    echo "curl -X POST $PRODUCTION_URL/api/login \\"
    echo "  -H 'Content-Type: application/json' \\"
    echo "  -d '{\"username\":\"artpro\",\"password\":\"YOUR_PASSWORD\"}'"
elif [ "$RESPONSE" = "200" ]; then
    echo "✅ Backend is accessible!"
    echo "   Status: 200 OK"
    echo ""
    echo "⚠️  Note: Getting 200 for /api/stocks without auth might indicate"
    echo "    an authentication configuration issue. Check your JWT middleware."
elif [ "$RESPONSE" = "404" ]; then
    echo "⚠️  Got 404 - Route might not be set up correctly"
    echo "   Check your vercel.json routes configuration"
elif [ "$RESPONSE" = "000" ]; then
    echo "❌ Cannot reach production URL"
    echo ""
    echo "Possible causes:"
    echo "1. Production deployment hasn't happened yet"
    echo "2. Project name is different"
    echo "3. Domain hasn't been assigned yet"
    echo ""
    echo "📋 How to find your real production URL:"
    echo "   1. Go to: https://vercel.com/dashboard"
    echo "   2. Click your backend project"
    echo "   3. Look for 'Domains' section"
    echo "   4. Copy the main domain (without 'git-' in it)"
    echo "   5. Update this script with the correct URL"
else
    echo "Got HTTP $RESPONSE"
    echo "Check Vercel dashboard for more details"
fi

echo ""
echo "=============================================="
echo "📚 For more help, see: DEPLOYMENT_PROTECTION_GUIDE.md"

