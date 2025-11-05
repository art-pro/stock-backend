# Deployment Guide: Fixing the Database Issue

## Problem Summary

Your application was experiencing an issue where stocks would appear to be added (showing "Stock with this ticker already exists") but then not appear in the list. 

**Root Cause**: The app was using SQLite on Vercel's serverless functions, which have ephemeral (temporary) filesystems. Each API request gets a fresh filesystem, so data doesn't persist between requests.

**Solution**: We've updated the application to support PostgreSQL for production while keeping SQLite for local development.

## Changes Made

1. ✅ Added PostgreSQL driver support
2. ✅ Updated database initialization to detect and use PostgreSQL when `DATABASE_URL` is set
3. ✅ Maintained backward compatibility with SQLite for local development
4. ✅ Updated configuration to support both database types

## Deployment Steps for Vercel

### Step 1: Set Up Vercel Postgres

1. Go to your Vercel project dashboard
2. Navigate to the **Storage** tab
3. Click **Create Database** → Select **Postgres**
4. Choose a name for your database (e.g., `stock-portfolio-db`)
5. Select a region close to your users
6. Click **Create**

### Step 2: Connect Database to Your Project

Vercel will automatically add the `DATABASE_URL` environment variable to your project. To verify:

1. Go to **Settings** → **Environment Variables**
2. Confirm `DATABASE_URL` is present (it should look like: `postgres://...`)
3. Make sure it's enabled for **Production**, **Preview**, and **Development** environments

### Step 3: Add Other Required Environment Variables

In **Settings** → **Environment Variables**, ensure you have:

```bash
# Required
ADMIN_USERNAME=your-username
ADMIN_PASSWORD=your-secure-password
JWT_SECRET=your-super-secret-jwt-key-change-this
FRONTEND_URL=https://your-frontend-domain.vercel.app

# Optional (for stock price fetching and alerts)
ALPHA_VANTAGE_API_KEY=your-key
XAI_API_KEY=your-key
EXCHANGE_RATES_API_KEY=your-key
SENDGRID_API_KEY=your-key
ALERT_EMAIL_FROM=alerts@yourapp.com
ALERT_EMAIL_TO=admin@yourapp.com

# Database (automatically set by Vercel when you create Postgres)
DATABASE_URL=postgres://... (auto-configured by Vercel)
```

### Step 4: Deploy

```bash
# Commit your changes
git add .
git commit -m "Add PostgreSQL support for Vercel deployment"
git push origin main
```

Vercel will automatically deploy. The first deployment will:
1. Connect to PostgreSQL using `DATABASE_URL`
2. Run migrations to create all necessary tables
3. Create the admin user
4. Initialize portfolio settings

### Step 5: Verify Deployment

1. Check deployment logs in Vercel dashboard
2. Look for: `"Using PostgreSQL database"`
3. Visit your API endpoint: `https://your-backend.vercel.app/api/health`
4. Test adding a stock from your frontend

## Local Development

For local development, the app will continue using SQLite:

```bash
# Create .env file (copy from env.example.txt)
cp env.example.txt .env

# Edit .env - leave DATABASE_URL empty or unset
# The app will use DATABASE_PATH=./data/stocks.db

# Run locally
go run main.go
```

## Database Management

### View Database Contents (Vercel)

1. In Vercel dashboard, go to **Storage** → Select your database
2. Click **Query** tab to run SQL queries
3. Example queries:
   ```sql
   -- View all stocks
   SELECT * FROM stocks;
   
   -- Count stocks
   SELECT COUNT(*) FROM stocks;
   
   -- View users
   SELECT id, username, created_at FROM users;
   ```

### Backup Database

Vercel Postgres provides automatic backups. To create a manual backup:

1. Go to Storage → Your Database → **Backups** tab
2. Click **Create Backup**

## Troubleshooting

### Issue: "Failed to connect to PostgreSQL"

**Solution**: 
- Verify `DATABASE_URL` is set in Vercel environment variables
- Check the database status in Storage tab
- Ensure your Vercel plan supports the database (requires Hobby plan or higher)

### Issue: "Stock not appearing after adding"

**Solution**:
- Check Vercel logs for errors
- Verify database connection is using PostgreSQL (look for "Using PostgreSQL database" in logs)
- Try deleting and recreating the stock

### Issue: Local development not working

**Solution**:
- Ensure `DATABASE_URL` is NOT set in your local `.env` file
- Check that `./data` directory exists (created automatically)
- Delete `./data/stocks.db` and restart to recreate fresh database

## Migration from SQLite to PostgreSQL (If you had local data)

If you were previously running on SQLite locally and want to migrate data:

```bash
# Export from SQLite (from the deployed app)
curl -X GET https://your-backend.vercel.app/api/export/csv \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -o stocks_backup.csv

# Import to new PostgreSQL deployment
curl -X POST https://your-backend.vercel.app/api/import/csv \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -F "file=@stocks_backup.csv"
```

## Cost Considerations

**Vercel Postgres Pricing:**
- Hobby plan: $0.10/GB stored per month + $0.30/GB data transfer
- Free tier available with limitations
- See: https://vercel.com/docs/storage/vercel-postgres/pricing

## Next Steps

1. ✅ Deploy to Vercel with PostgreSQL
2. Test adding stocks - they should now persist!
3. Set up monitoring alerts (optional)
4. Configure backup schedule (automatic on Vercel)

## Support

If you encounter issues:
1. Check Vercel deployment logs
2. Verify all environment variables are set
3. Test database connectivity from Vercel Query interface
4. Check this repository's issues page

---

**Note**: After deploying with PostgreSQL, your data will persist across all serverless function invocations, and the "stock not showing" issue will be completely resolved! 🎉

