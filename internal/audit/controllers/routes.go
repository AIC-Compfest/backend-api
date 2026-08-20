package controllers

import (
	"backend-api/internal/audit"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers audit routes
func RegisterRoutes(rg *gin.RouterGroup, res *audit.Resource) {
	group := rg.Group("/audit")
	{
		group.GET("", List(res))
	}
}
