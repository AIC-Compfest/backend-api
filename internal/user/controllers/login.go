package controllers

import (
	"net/http"

	"backend-api/internal/user"
	"github.com/gin-gonic/gin"
)

// Login handles POST /api/v1/auth/login
func Login(res *user.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req user.LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid login payload: " + err.Error()})
			return
		}

		resp, err := res.Login(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}
