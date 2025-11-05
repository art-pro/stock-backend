package handlers

import (
	"net/http"

	"github.com/artpro/assessapp/pkg/auth"
	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// AuthHandler handles authentication-related requests
type AuthHandler struct {
	db     *gorm.DB
	cfg    *config.Config
	logger zerolog.Logger
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) *AuthHandler {
	return &AuthHandler{
		db:     db,
		cfg:    cfg,
		logger: logger,
	}
}

// LoginRequest represents login request body
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse represents login response
type LoginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Message  string `json:"message"`
}

// Login handles user login
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Find user
	var user models.User
	if err := h.db.Where("username = ?", req.Username).First(&user).Error; err != nil {
		h.logger.Warn().Str("username", req.Username).Msg("Login attempt with invalid username")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Check password
	if err := auth.CheckPassword(user.Password, req.Password); err != nil {
		h.logger.Warn().Str("username", req.Username).Msg("Login attempt with invalid password")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate JWT token
	token, err := auth.GenerateToken(user.ID, user.Username, h.cfg.JWTSecret)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to generate token")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	h.logger.Info().Str("username", user.Username).Msg("User logged in successfully")

	c.JSON(http.StatusOK, LoginResponse{
		Token:    token,
		Username: user.Username,
		Message:  "Login successful",
	})
}

// Logout handles user logout
func (h *AuthHandler) Logout(c *gin.Context) {
	username, _ := c.Get("username")
	h.logger.Info().Str("username", username.(string)).Msg("User logged out")
	
	c.JSON(http.StatusOK, gin.H{"message": "Logout successful"})
}

// ChangePasswordRequest represents password change request
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
}

// ChangePassword handles password change
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, _ := c.Get("user_id")
	
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request, password must be at least 8 characters"})
		return
	}

	// Get user
	var user models.User
	if err := h.db.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Verify current password
	if err := auth.CheckPassword(user.Password, req.CurrentPassword); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Current password is incorrect"})
		return
	}

	// Hash new password
	hashedPassword, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to hash password")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	// Update password
	user.Password = hashedPassword
	if err := h.db.Save(&user).Error; err != nil {
		h.logger.Error().Err(err).Msg("Failed to save password")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
		return
	}

	h.logger.Info().Str("username", user.Username).Msg("Password changed successfully")

	c.JSON(http.StatusOK, gin.H{"message": "Password changed successfully"})
}

// GetCurrentUser returns current user info
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userID, _ := c.Get("user_id")
	username, _ := c.Get("username")

	c.JSON(http.StatusOK, gin.H{
		"id":       userID,
		"username": username,
	})
}

