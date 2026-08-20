package controllers

import (
	"backend-api/internal/settings"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers settings endpoints
func RegisterRoutes(rg *gin.RouterGroup, res *settings.Resource) {
	group := rg.Group("/settings")
	{
		group.GET("", Get(res))
		group.PUT("", Update(res))
	}
}
