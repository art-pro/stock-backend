package handlers

import (
	"net/http"

	"github.com/art-pro/stock-backend/pkg/models"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type SettingsHandler struct {
	db     *gorm.DB
	logger zerolog.Logger
}

func NewSettingsHandler(db *gorm.DB, logger zerolog.Logger) *SettingsHandler {
	return &SettingsHandler{
		db:     db,
		logger: logger,
	}
}

type ColumnSettingsRequest struct {
	Settings string `json:"settings" binding:"required"` // JSON string of settings
}

func (h *SettingsHandler) GetColumnSettings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}
	uid := userID.(uint)

	var setting models.UserSettings
	if err := h.db.Where("user_id = ? AND key = ?", uid, "stock_table_columns").First(&setting).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusOK, gin.H{"settings": nil})
			return
		}
		h.logger.Error().Err(err).Msg("Failed to fetch column settings")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"settings": setting.Value})
}

func (h *SettingsHandler) SaveColumnSettings(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found in context"})
		return
	}
	uid := userID.(uint)

	var req ColumnSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var setting models.UserSettings
	err := h.db.Where("user_id = ? AND key = ?", uid, "stock_table_columns").First(&setting).Error

	if err == gorm.ErrRecordNotFound {
		// Create new
		setting = models.UserSettings{
			UserID: uid,
			Key:    "stock_table_columns",
			Value:  req.Settings,
		}
		if err := h.db.Create(&setting).Error; err != nil {
			h.logger.Error().Err(err).Msg("Failed to create column settings")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
			return
		}
	} else if err == nil {
		// Update existing
		setting.Value = req.Settings
		if err := h.db.Save(&setting).Error; err != nil {
			h.logger.Error().Err(err).Msg("Failed to update column settings")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
			return
		}
	} else {
		h.logger.Error().Err(err).Msg("Failed to check existing settings")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

