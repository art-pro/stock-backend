package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-pro/stock-backend/internal/auth"
	"github.com/art-pro/stock-backend/internal/config"
	"github.com/gin-gonic/gin"
)

func setupRouterWithAuth(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthMiddleware(&config.Config{JWTSecret: secret}))
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"username": c.GetString("username"),
			"user_id":  c.GetUint("user_id"),
		})
	})
	return r
}

func TestAuthMiddlewareRejectsMissingHeader(t *testing.T) {
	t.Parallel()

	r := setupRouterWithAuth("secret")
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareRejectsBadBearerFormat(t *testing.T) {
	t.Parallel()

	r := setupRouterWithAuth("secret")
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Token abc")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	r := setupRouterWithAuth("secret")
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareAllowsValidToken(t *testing.T) {
	t.Parallel()

	const secret = "secret"
	token, err := auth.GenerateToken(7, "alice", secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	r := setupRouterWithAuth(secret)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", w.Code, http.StatusOK)
	}
}
