package middleware

import (
	"net/http"
	"strings"

	"github.com/artpro/assessapp/pkg/auth"
	"github.com/artpro/assessapp/pkg/config"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware validates JWT tokens
func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Extract token from "Bearer <token>"
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		token := parts[1]
		claims, err := auth.ValidateToken(token, cfg.JWTSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Set user info in context
		c.Set("username", claims.Username)
		c.Set("user_id", claims.UserID)
		c.Next()
	}
}

// RateLimitMiddleware implements simple rate limiting (for production, use redis-based solution)
func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Simple implementation - in production, use middleware like github.com/ulule/limiter
		c.Next()
	}
}

