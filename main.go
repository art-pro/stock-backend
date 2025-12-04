package main

import (
	"log"
	"os"

	"github.com/art-pro/stock-backend/pkg/api"
	"github.com/art-pro/stock-backend/pkg/config"
	"github.com/art-pro/stock-backend/pkg/database"
	"github.com/art-pro/stock-backend/pkg/scheduler"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Initialize logger
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	// Load configuration
	cfg := config.Load()

	// Initialize database
	db, err := database.InitDB(cfg.DatabasePath)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}

	// Initialize admin user
	if err := database.InitializeAdminUser(db, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize admin user")
	}

	// Initialize scheduler if enabled
	if cfg.EnableScheduler {
		scheduler.InitScheduler(db, cfg, logger)
	}

	// Initialize and start API server
	router := api.SetupRouter(db, cfg, logger)

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	logger.Info().Msgf("Starting server on port %s", port)
	if err := router.Run(":" + port); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start server")
	}
}
