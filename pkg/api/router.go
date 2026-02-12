package api

import (
	"github.com/art-pro/stock-backend/pkg/api/handlers"
	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/middleware"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// SetupRouter sets up the Gin router with all routes and middleware
func SetupRouter(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Custom CORS middleware to handle dynamic origins
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		allowedOrigins := []string{
			cfg.FrontendURL,
			"http://localhost:3000",
			"https://stock-frontend-silk.vercel.app",
			"https://stock-frontend-artpros-projects.vercel.app",
			"https://www.artpro.dev",
			"https://artpro.dev",
		}

		// Check if origin is allowed
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				c.Header("Access-Control-Allow-Origin", origin)
				break
			}
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Max-Age", "43200")

		// Handle preflight requests
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(db, cfg, logger)
	stockHandler := handlers.NewStockHandler(db, cfg, logger)
	portfolioHandler := handlers.NewPortfolioHandler(db, cfg, logger)
	exchangeRateHandler := handlers.NewExchangeRateHandler(db, cfg, logger)
	cashHandler := handlers.NewCashHandler(db, cfg, logger)
	assessmentHandler := handlers.NewAssessmentHandler(db, cfg, logger)
	settingsHandler := handlers.NewSettingsHandler(db, logger)

	// Public routes
	public := router.Group("/api")
	{
		public.POST("/login", authHandler.Login)
		public.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})
		public.GET("/version", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"version":    config.Version,
				"build_date": config.BuildDate,
			})
		})
	}

	// Protected routes
	protected := router.Group("/api")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		// Auth routes
		protected.POST("/logout", authHandler.Logout)
		protected.POST("/change-password", authHandler.ChangePassword)
		protected.POST("/change-username", authHandler.ChangeUsername)
		protected.GET("/me", authHandler.GetCurrentUser)

		// Stock routes
		protected.GET("/stocks", stockHandler.GetAllStocks)
		protected.GET("/stocks/batch", stockHandler.GetStocksBatch) // Must be before /:id to avoid conflict
		protected.GET("/stocks/:id", stockHandler.GetStock)
		protected.POST("/stocks", stockHandler.CreateStock)
		protected.PUT("/stocks/:id", stockHandler.UpdateStock)
		protected.PATCH("/stocks/:id/price", stockHandler.UpdateStockPrice)
		protected.PATCH("/stocks/:id/field", stockHandler.UpdateStockField)
		protected.DELETE("/stocks/:id", stockHandler.DeleteStock)
		protected.POST("/stocks/update-all", stockHandler.UpdateAllStocks)
		protected.POST("/stocks/fair-value/collect", stockHandler.CollectFairValues)
		protected.POST("/stocks/:id/update", stockHandler.UpdateSingleStock)
		protected.POST("/stocks/bulk-update", stockHandler.BulkUpdateStocks)

		// Stock history routes
		protected.GET("/stocks/:id/history", stockHandler.GetStockHistory)
		protected.GET("/stocks/:id/fair-value-history", stockHandler.GetFairValueHistory)

		// Deleted stocks (log) routes
		protected.GET("/deleted-stocks", stockHandler.GetDeletedStocks)
		protected.POST("/deleted-stocks/:id/restore", stockHandler.RestoreStock)

		// Portfolio routes
		protected.GET("/portfolio/summary", portfolioHandler.GetPortfolioSummary)
		protected.GET("/portfolio/settings", portfolioHandler.GetSettings)
		protected.PUT("/portfolio/settings", portfolioHandler.UpdateSettings)

		// API Status routes
		protected.GET("/api-status", portfolioHandler.GetAPIStatus)

		// Export routes
		protected.GET("/export/json", stockHandler.ExportJSON)

		// Alerts routes
		protected.GET("/alerts", portfolioHandler.GetAlerts)
		protected.DELETE("/alerts/:id", portfolioHandler.DeleteAlert)

		// Exchange rates routes
		protected.GET("/exchange-rates", exchangeRateHandler.GetAllRates)
		protected.POST("/exchange-rates/refresh", exchangeRateHandler.RefreshRates)
		protected.POST("/exchange-rates", exchangeRateHandler.AddCurrency)
		protected.PUT("/exchange-rates/:code", exchangeRateHandler.UpdateRate)
		protected.DELETE("/exchange-rates/:code", exchangeRateHandler.DeleteCurrency)

		// Cash holdings routes
		protected.GET("/cash", cashHandler.GetAllCashHoldings)
		protected.POST("/cash", cashHandler.CreateCashHolding)
		protected.PUT("/cash/:id", cashHandler.UpdateCashHolding)
		protected.DELETE("/cash/:id", cashHandler.DeleteCashHolding)
		protected.POST("/cash/refresh", cashHandler.RefreshUSDValues)

		// Assessment routes
		protected.POST("/assessment/request", assessmentHandler.RequestAssessment)
		protected.POST("/assessment/extract-from-images", assessmentHandler.ExtractFromImages)
		protected.GET("/assessment/recent", assessmentHandler.GetRecentAssessments)
		protected.GET("/assessment/:id", assessmentHandler.GetAssessmentById)

		// User Settings routes
		protected.GET("/settings/columns", settingsHandler.GetColumnSettings)
		protected.POST("/settings/columns", settingsHandler.SaveColumnSettings)
	}

	return router
}
