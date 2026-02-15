package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/art-pro/stock-backend/pkg/auth"
	"github.com/art-pro/stock-backend/pkg/config"
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

// rateLimiter provides in-memory token bucket rate limiting per IP.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // requests per window
	window   time.Duration // window duration
}

type visitor struct {
	tokens    int
	lastReset time.Time
}

var defaultLimiter = &rateLimiter{
	visitors: make(map[string]*visitor),
	rate:     100,             // 100 requests
	window:   1 * time.Minute, // per minute
}

var loginLimiter = &rateLimiter{
	visitors: make(map[string]*visitor),
	rate:     10,               // 10 login attempts
	window:   15 * time.Minute, // per 15 minutes
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()

	if !exists || now.Sub(v.lastReset) > rl.window {
		rl.visitors[ip] = &visitor{tokens: rl.rate - 1, lastReset: now}
		return true
	}

	if v.tokens > 0 {
		v.tokens--
		return true
	}

	return false
}

// cleanup removes stale entries periodically (call in background goroutine if desired)
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, v := range rl.visitors {
		if now.Sub(v.lastReset) > rl.window*2 {
			delete(rl.visitors, ip)
		}
	}
}

// RateLimitMiddleware implements token bucket rate limiting per IP.
// For production at scale, consider a Redis-based solution.
func RateLimitMiddleware() gin.HandlerFunc {
	// Start background cleanup every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			defaultLimiter.cleanup()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !defaultLimiter.allow(ip) {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limit exceeded. Please try again later."})
			c.Abort()
			return
		}
		c.Next()
	}
}

// LoginRateLimitMiddleware provides stricter rate limiting for authentication endpoints.
func LoginRateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !loginLimiter.allow(ip) {
			c.Header("Retry-After", "900")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many login attempts. Please try again later."})
			c.Abort()
			return
		}
		c.Next()
	}
}

// SecurityHeadersMiddleware adds security-related HTTP headers.
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prevent MIME type sniffing
		c.Header("X-Content-Type-Options", "nosniff")
		// XSS protection for older browsers
		c.Header("X-XSS-Protection", "1; mode=block")
		// Prevent clickjacking
		c.Header("X-Frame-Options", "DENY")
		// Strict transport security (1 year)
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// Referrer policy
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		// Content Security Policy for API
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

		c.Next()
	}
}

// RequestSizeLimitMiddleware limits request body size.
// defaultMaxBytes is the limit for most endpoints (1MB).
// For routes needing larger payloads (e.g., image uploads), apply a separate limit.
func RequestSizeLimitMiddleware(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Request body too large"})
			c.Abort()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
