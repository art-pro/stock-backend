package handler

import (
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/artpro/assessapp/pkg/api"
	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/database"
)

var (
	router  *gin.Engine
	db      *gorm.DB
	cfg     *config.Config
	logger  zerolog.Logger
	once    sync.Once
	initErr error
)

// Initialize the application once
func initialize() {
	once.Do(func() {
		// Set Gin to release mode for production
		gin.SetMode(gin.ReleaseMode)

		// Initialize logger
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

		// Load configuration
		cfg = config.Load()

		// Initialize database
		var err error
		db, err = database.InitDB(cfg.DatabasePath)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to initialize database")
			initErr = err
			return
		}

		// Initialize admin user
		if err := database.InitializeAdminUser(db, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			logger.Error().Err(err).Msg("Failed to initialize admin user")
			initErr = err
			return
		}

		// Initialize portfolio settings
		if err := database.InitializePortfolioSettings(db); err != nil {
			logger.Warn().Err(err).Msg("Failed to initialize portfolio settings")
		}

		// Note: Scheduler is disabled in serverless environment
		logger.Info().Msg("Running in serverless mode - scheduler disabled")

		// Setup router
		router = api.SetupRouter(db, cfg, logger)

		logger.Info().Msg("Serverless function initialized successfully")
	})
}

// Handler is the entry point for Vercel serverless function
func Handler(w http.ResponseWriter, r *http.Request) {
	// Initialize on first request
	initialize()

	// Check if initialization failed
	if initErr != nil {
		log.Printf("Initialization error: %v", initErr)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Serve the request
	router.ServeHTTP(w, r)
}
