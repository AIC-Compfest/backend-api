package controllers

import (
	"backend-api/internal/document"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers document ingestion & retrieval routes
func RegisterRoutes(rg *gin.RouterGroup, res *document.Resource) {
	group := rg.Group("/documents")
	{
		group.POST("/upload", Upload(res))
		group.GET("", List(res))
		group.GET("/:id", Get(res))
		group.GET("/:id/download", Download(res))
		group.GET("/:id/evidence", Evidence(res))
		group.GET("/:id/pipeline-status", PipelineStatus(res))
		group.GET("/:id/segments", ListSegments(res))
		group.GET("/:id/pages", ListPages(res))
		group.GET("/:id/pages/:page_number", GetPage(res))
		group.POST("/:id/pages/:page_number/retry", RetryPage(res))
	}
}
