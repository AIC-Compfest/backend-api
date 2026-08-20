package controllers

import (
	"net/http"
	"strings"

	"backend-api/internal/user"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers user & authentication routes to the router group
func RegisterRoutes(rg *gin.RouterGroup, res *user.Resource) {
	auth := rg.Group("/auth")
	{
		auth.POST("/register", Register(res))
		auth.POST("/login", Login(res))
		auth.GET("/me", AuthMiddleware(res), Me(res))
	}
}

// AuthMiddleware creates a middleware that validates JWT token
func AuthMiddleware(res *user.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing Authorization header"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid Authorization format. Expected: Bearer <token>"})
			return
		}

		tokenString := strings.TrimSpace(parts[1])
		claims, err := res.ValidateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token: " + err.Error()})
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("name", claims.Name)
		c.Set("role", claims.Role)
		c.Set("company_name", claims.CompanyName)

		c.Next()
	}
}
