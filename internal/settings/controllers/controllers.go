package controllers

import (
	"net/http"

	"backend-api/internal/settings"
	"github.com/gin-gonic/gin"
)

// Get handles GET /api/v1/settings
func Get(res *settings.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := res.GetSettings(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, cfg)
	}
}

// Update handles PUT /api/v1/settings
func Update(res *settings.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req settings.UpdateSettingsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
			return
		}

		userVal, _ := c.Get("email")
		userEmail, _ := userVal.(string)

		updated, err := res.UpdateSettings(c.Request.Context(), req, userEmail)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Policy settings updated successfully",
			"data":    updated,
		})
	}
}
