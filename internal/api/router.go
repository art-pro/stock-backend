package api

import (
	"github.com/artpro/assessapp/internal/api/handlers"
	"github.com/artpro/assessapp/internal/config"
	"github.com/artpro/assessapp/internal/middleware"
	"github.com/gin-contrib/cors"
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

	// CORS configuration
	corsConfig := cors.Config{
		AllowOrigins:     []string{cfg.FrontendURL, "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}
	router.Use(cors.New(corsConfig))

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(db, cfg, logger)
	stockHandler := handlers.NewStockHandler(db, cfg, logger)
	portfolioHandler := handlers.NewPortfolioHandler(db, cfg, logger)

	// Public routes
	public := router.Group("/api")
	{
		public.POST("/login", authHandler.Login)
		public.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})
	}

	// Protected routes
	protected := router.Group("/api")
	protected.Use(middleware.AuthMiddleware(cfg))
	{
		// Auth routes
		protected.POST("/logout", authHandler.Logout)
		protected.POST("/change-password", authHandler.ChangePassword)
		protected.GET("/me", authHandler.GetCurrentUser)

		// Stock routes
		protected.GET("/stocks", stockHandler.GetAllStocks)
		protected.GET("/stocks/:id", stockHandler.GetStock)
		protected.POST("/stocks", stockHandler.CreateStock)
		protected.PUT("/stocks/:id", stockHandler.UpdateStock)
		protected.DELETE("/stocks/:id", stockHandler.DeleteStock)
		protected.POST("/stocks/update-all", stockHandler.UpdateAllStocks)
		protected.POST("/stocks/:id/update", stockHandler.UpdateSingleStock)
		
		// Stock history routes
		protected.GET("/stocks/:id/history", stockHandler.GetStockHistory)
		
		// Deleted stocks (log) routes
		protected.GET("/deleted-stocks", stockHandler.GetDeletedStocks)
		protected.POST("/deleted-stocks/:id/restore", stockHandler.RestoreStock)

		// Portfolio routes
		protected.GET("/portfolio/summary", portfolioHandler.GetPortfolioSummary)
		protected.GET("/portfolio/settings", portfolioHandler.GetSettings)
		protected.PUT("/portfolio/settings", portfolioHandler.UpdateSettings)
		
		// API Status routes
		protected.GET("/api-status", portfolioHandler.GetAPIStatus)
		
		// Export/Import routes
		protected.GET("/export/csv", stockHandler.ExportCSV)
		protected.POST("/import/csv", stockHandler.ImportCSV)

		// Alerts routes
		protected.GET("/alerts", portfolioHandler.GetAlerts)
		protected.DELETE("/alerts/:id", portfolioHandler.DeleteAlert)
	}

	return router
}

