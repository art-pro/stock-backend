package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/art-pro/stock-backend/internal/auth"
	"github.com/art-pro/stock-backend/internal/config"
	"github.com/art-pro/stock-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAuthHandlerTest(t *testing.T) (*gorm.DB, *AuthHandler) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "auth-handler-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("migrate user failed: %v", err)
	}

	h := NewAuthHandler(db, &config.Config{JWTSecret: "test-secret"}, zerolog.Nop())
	return db, h
}

func TestAuthHandlerLoginSuccess(t *testing.T) {
	t.Parallel()

	db, h := setupAuthHandlerTest(t)
	hashed, _ := auth.HashPassword("correct-password")
	if err := db.Create(&models.User{Username: "alice", Password: hashed}).Error; err != nil {
		t.Fatalf("seed user failed: %v", err)
	}

	r := gin.New()
	r.POST("/login", h.Login)

	body := []byte(`{"username":"alice","password":"correct-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusOK)
	}

	var resp LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("expected token in successful login response")
	}
	if resp.Username != "alice" {
		t.Fatalf("Username: got %q want %q", resp.Username, "alice")
	}
}

func TestAuthHandlerLoginInvalidCredentials(t *testing.T) {
	t.Parallel()

	db, h := setupAuthHandlerTest(t)
	hashed, _ := auth.HashPassword("correct-password")
	if err := db.Create(&models.User{Username: "alice", Password: hashed}).Error; err != nil {
		t.Fatalf("seed user failed: %v", err)
	}

	r := gin.New()
	r.POST("/login", h.Login)

	body := []byte(`{"username":"alice","password":"wrong-password"}`)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandlerChangePasswordSuccess(t *testing.T) {
	t.Parallel()

	db, h := setupAuthHandlerTest(t)
	hashed, _ := auth.HashPassword("old-password")
	user := models.User{Username: "alice", Password: hashed}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user failed: %v", err)
	}

	r := gin.New()
	r.POST("/change-password", func(c *gin.Context) {
		c.Set("user_id", user.ID)
		h.ChangePassword(c)
	})

	body := []byte(`{"current_password":"old-password","new_password":"new-password-123"}`)
	req := httptest.NewRequest(http.MethodPost, "/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusOK)
	}

	var updated models.User
	if err := db.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("load updated user failed: %v", err)
	}
	if err := auth.CheckPassword(updated.Password, "new-password-123"); err != nil {
		t.Fatalf("new password hash verification failed: %v", err)
	}
}
