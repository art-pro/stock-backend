package database

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/artpro/assessapp/pkg/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes the database connection and runs migrations
func InitDB(dbPath string) (*gorm.DB, error) {
	// Ensure the directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
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

