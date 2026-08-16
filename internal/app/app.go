package app

import (
	"github.com/AIC-Compfest/backend-api/internal/config"
	"github.com/AIC-Compfest/backend-api/internal/handler"
	"github.com/AIC-Compfest/backend-api/internal/repository"
	"github.com/AIC-Compfest/backend-api/internal/service"
	"github.com/AIC-Compfest/backend-api/internal/storage"
	"github.com/AIC-Compfest/backend-api/internal/supabase"
	"github.com/gin-gonic/gin"
)

type Application struct {
	config config.Config
	router *gin.Engine
}

func New(cfg config.Config) *Application {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// CORS middleware for frontend access
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 1. Health Handlers
	health := handler.NewHealthHandler(cfg, supabase.NewClient(cfg.SupabaseURL, cfg.SupabasePublishableKey))
	router.GET("/healthz", health.Liveness)
	router.GET("/readyz", health.Readiness)

	// 2. Storage & Repository Setup
	localStorage, _ := storage.NewLocalStorage("storage")
	var store storage.Storage
	if cfg.SupabaseURL != "" && cfg.SupabaseServiceRoleKey != "" {
		store = storage.NewSupabaseStorage(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, localStorage)
	} else {
		store = localStorage
	}

	docRepo := repository.NewMemoryDocumentRepository()
	ingestionService := service.NewIngestionService(docRepo, store, cfg.AIServiceURL)
	docHandler := handler.NewDocumentHandler(ingestionService)

	// 3. API v1 Document Routes
	v1 := router.Group("/api/v1")
	{
		docs := v1.Group("/documents")
		{
			docs.POST("/upload", docHandler.Upload)
			docs.GET("", docHandler.ListDocuments)
			docs.GET("/:id", docHandler.GetDocument)
			docs.GET("/:id/download", docHandler.Download)
		}
	}

	return &Application{config: cfg, router: router}
}

func (a *Application) Run() error {
	return a.router.Run(":" + a.config.Port)
}

func (a *Application) Router() *gin.Engine {
	return a.router
}
