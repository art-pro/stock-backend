# PostgreSQL Setup for Vercel Deployment

## Problem
SQLite stores data in a local file which is **ephemeral** on Vercel. Data gets cleared after each function invocation or deployment.

## Solution
Use PostgreSQL for production (Vercel) and SQLite for local development.

## Setup Instructions

### Option 1: Vercel Postgres (Recommended)

1. Go to your Vercel dashboard
2. Select your backend project
3. Go to **Storage** tab
4. Click **Create Database** → **Postgres**
5. Follow the prompts to create a database
6. Vercel will automatically add the `DATABASE_URL` environment variable
7. **Redeploy** your backend

### Option 2: Neon (Free PostgreSQL)

1. Go to [neon.tech](https://neon.tech) and create a free account
2. Create a new project
3. Copy the connection string (looks like: `postgresql://user:pass@host/dbname`)
4. In Vercel:
   - Go to your backend project settings
   - Navigate to **Environment Variables**
   - Add: `DATABASE_URL` = `your-connection-string`
5. **Redeploy** your backend

### Option 3: Supabase (Free PostgreSQL)

1. Go to [supabase.com](https://supabase.com) and create a free account
2. Create a new project
3. Go to **Project Settings** → **Database**
4. Copy the connection string under **Connection string** → **URI**
5. In Vercel:
   - Add environment variable: `DATABASE_URL` = `your-connection-string`
6. **Redeploy** your backend

## How It Works

The code now automatically detects the database:
- **If `DATABASE_URL` is set**: Uses PostgreSQL (production)
- **If `DATABASE_URL` is NOT set**: Uses SQLite at `./data/stocks.db` (local dev)

## After Setup

Once you've added `DATABASE_URL` and redeployed:
1. Your data will persist across deployments
2. Stocks won't disappear on page reload
3. All data is stored permanently in PostgreSQL

## Local Development

No changes needed! The app will continue using SQLite locally for development.



