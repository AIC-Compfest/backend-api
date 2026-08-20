package controllers

import (
	"net/http"

	"backend-api/internal/user"
	"github.com/gin-gonic/gin"
)

// Me handles GET /api/v1/auth/me
func Me(res *user.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		userID, ok := userIDVal.(string)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user context"})
			return
		}

		profile, err := res.GetProfile(c.Request.Context(), userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}

		c.JSON(http.StatusOK, profile)
	}
}
