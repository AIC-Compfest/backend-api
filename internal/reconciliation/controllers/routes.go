package controllers

import (
	"backend-api/internal/reconciliation"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers reconciliation routes
func RegisterRoutes(rg *gin.RouterGroup, res *reconciliation.Resource) {
	group := rg.Group("/reconcile")
	{
		group.POST("", Reconcile(res))
		group.GET("/queue", Queue(res))
		group.GET("/:id", Detail(res))
		group.POST("/:id/decision", Decision(res))
		group.GET("/:id/dispute-package", DisputePackage(res))
	}
}
