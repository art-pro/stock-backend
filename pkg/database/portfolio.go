package database

import (
	"fmt"

	"github.com/art-pro/stock-backend/pkg/models"
	"gorm.io/gorm"
)

// GetDefaultPortfolioID returns the default portfolio ID, falling back to the first portfolio.
func GetDefaultPortfolioID(db *gorm.DB) (uint, error) {
	var portfolio models.Portfolio
	if err := db.Where("is_default = ?", true).First(&portfolio).Error; err == nil {
		return portfolio.ID, nil
	}

	if err := db.First(&portfolio).Error; err == nil {
		return portfolio.ID, nil
	}

	return 0, fmt.Errorf("no portfolio found")
}

// EnsureDefaultPortfolio ensures a default portfolio exists for the given user.
func EnsureDefaultPortfolio(db *gorm.DB, username string) (uint, error) {
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		return 0, fmt.Errorf("failed to fetch user for default portfolio: %w", err)
	}

	var portfolio models.Portfolio
	if err := db.Where("user_id = ? AND is_default = ?", user.ID, true).First(&portfolio).Error; err == nil {
		return portfolio.ID, nil
	}

	if err := db.Where("user_id = ?", user.ID).First(&portfolio).Error; err == nil {
		return portfolio.ID, nil
	}

	portfolio = models.Portfolio{
		Name:        "Main Portfolio",
		Description: "Default portfolio",
		IsDefault:   true,
		TotalValue:  0,
		UserID:      user.ID,
	}

	if err := db.Create(&portfolio).Error; err != nil {
		return 0, fmt.Errorf("failed to create default portfolio: %w", err)
	}

	return portfolio.ID, nil
}
