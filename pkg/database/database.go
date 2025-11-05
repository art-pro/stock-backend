package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/artpro/assessapp/pkg/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes the database connection and runs migrations
// Supports both PostgreSQL (via DATABASE_URL) and SQLite (via dbPath for local dev)
func InitDB(dbPath string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	// Check if DATABASE_URL is set (PostgreSQL for production)
	databaseURL := os.Getenv("DATABASE_URL")

	if databaseURL != "" {
		// Use PostgreSQL for production (Vercel)
		fmt.Println("Using PostgreSQL database")

		// Handle Vercel Postgres format: postgres:// -> postgresql://
		if strings.HasPrefix(databaseURL, "postgres://") {
			databaseURL = strings.Replace(databaseURL, "postgres://", "postgresql://", 1)
		}

		db, err = gorm.Open(postgres.Open(databaseURL), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
	} else {
		// Use SQLite for local development
		fmt.Printf("Using SQLite database: %s\n", dbPath)

		// Ensure the directory exists
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}

		db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
		}
	}

	// Run auto migrations
	if err := db.AutoMigrate(
		&models.User{},
		&models.Stock{},
		&models.StockHistory{},
		&models.DeletedStock{},
		&models.PortfolioSettings{},
		&models.Alert{},
	); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

// InitializeAdminUser creates the admin user if it doesn't exist
func InitializeAdminUser(db *gorm.DB, username, password string) error {
	var user models.User
	result := db.Where("username = ?", username).First(&user)

	if result.Error == gorm.ErrRecordNotFound {
		// Create admin user
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		user = models.User{
			Username: username,
			Password: string(hashedPassword),
		}

		if err := db.Create(&user).Error; err != nil {
			return fmt.Errorf("failed to create admin user: %w", err)
		}

		fmt.Printf("Admin user '%s' created successfully\n", username)
	}

	return nil
}

// InitializePortfolioSettings creates default portfolio settings if none exist
func InitializePortfolioSettings(db *gorm.DB) error {
	var settings models.PortfolioSettings
	result := db.First(&settings)

	if result.Error == gorm.ErrRecordNotFound {
		settings = models.PortfolioSettings{
			TotalPortfolioValue: 0,
			UpdateFrequency:     "daily",
			AlertsEnabled:       true,
			AlertThresholdEV:    10.0, // Alert on 10% EV change
		}

		if err := db.Create(&settings).Error; err != nil {
			return fmt.Errorf("failed to create portfolio settings: %w", err)
		}
	}

	return nil
}
