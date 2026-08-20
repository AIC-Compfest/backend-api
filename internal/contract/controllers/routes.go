package controllers

import (
	"backend-api/internal/contract"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers contract routes
func RegisterRoutes(rg *gin.RouterGroup, res *contract.Resource) {
	group := rg.Group("/contracts")
	{
		group.GET("", List(res))
		group.GET("/:id", Get(res))
	}
}
