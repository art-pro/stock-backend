package database

import (
	"path/filepath"
	"testing"

	"github.com/art-pro/stock-backend/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func testDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.db")
}

func TestInitDBSQLiteRunsMigrations(t *testing.T) {
	t.Parallel()

	db, err := InitDB(testDB(t))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	if !db.Migrator().HasTable(&models.User{}) {
		t.Fatalf("expected users table to exist")
	}
	if !db.Migrator().HasTable(&models.Stock{}) {
		t.Fatalf("expected stocks table to exist")
	}
	if !db.Migrator().HasTable(&models.PortfolioSettings{}) {
		t.Fatalf("expected portfolio_settings table to exist")
	}
}

func TestInitializeAdminUserIsIdempotent(t *testing.T) {
	t.Parallel()

	db, err := InitDB(testDB(t))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	if err := InitializeAdminUser(db, "admin", "strong-password"); err != nil {
		t.Fatalf("InitializeAdminUser first call failed: %v", err)
	}
	if err := InitializeAdminUser(db, "admin", "strong-password"); err != nil {
		t.Fatalf("InitializeAdminUser second call failed: %v", err)
	}

	var users []models.User
	if err := db.Where("username = ?", "admin").Find(&users).Error; err != nil {
		t.Fatalf("query users failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("user count: got %d want %d", len(users), 1)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(users[0].Password), []byte("strong-password")); err != nil {
		t.Fatalf("stored password hash invalid: %v", err)
	}
}

func TestInitializePortfolioSettingsIsIdempotent(t *testing.T) {
	t.Parallel()

	db, err := InitDB(testDB(t))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	if err := InitializePortfolioSettings(db); err != nil {
		t.Fatalf("InitializePortfolioSettings first call failed: %v", err)
	}
	if err := InitializePortfolioSettings(db); err != nil {
		t.Fatalf("InitializePortfolioSettings second call failed: %v", err)
	}

	var settings []models.PortfolioSettings
	if err := db.Find(&settings).Error; err != nil {
		t.Fatalf("query settings failed: %v", err)
	}
	if len(settings) != 1 {
		t.Fatalf("settings count: got %d want %d", len(settings), 1)
	}
	if settings[0].UpdateFrequency != "daily" {
		t.Fatalf("UpdateFrequency: got %q want %q", settings[0].UpdateFrequency, "daily")
	}
	if settings[0].AlertThresholdEV != 10.0 {
		t.Fatalf("AlertThresholdEV: got %.2f want %.2f", settings[0].AlertThresholdEV, 10.0)
	}
}
