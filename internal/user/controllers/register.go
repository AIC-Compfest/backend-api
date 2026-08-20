package controllers

import (
	"net/http"

	"backend-api/internal/user"
	"github.com/gin-gonic/gin"
)

// Register handles POST /api/v1/auth/register
func Register(res *user.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req user.RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload: " + err.Error()})
			return
		}

		resp, err := res.Register(c.Request.Context(), req)
		if err != nil {
			status := http.StatusBadRequest
			if err == user.ErrInvalidCredentials {
				status = http.StatusUnauthorized
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, resp)
	}
}
