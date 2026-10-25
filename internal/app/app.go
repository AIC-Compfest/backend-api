package app

import (
	"backend-api/internal/audit"
	auditControllers "backend-api/internal/audit/controllers"
	"backend-api/internal/config"
	"backend-api/internal/contract"
	contractControllers "backend-api/internal/contract/controllers"
	"backend-api/internal/database"
	"backend-api/internal/document"
	documentControllers "backend-api/internal/document/controllers"
	"backend-api/internal/handler"
	"backend-api/internal/reconciliation"
	reconciliationControllers "backend-api/internal/reconciliation/controllers"
	"backend-api/internal/settings"
	settingsControllers "backend-api/internal/settings/controllers"
	"backend-api/internal/storage"
	"backend-api/internal/supabase"
	"backend-api/internal/user"
	userControllers "backend-api/internal/user/controllers"
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
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, Range")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, Content-Length, Content-Range, Accept-Ranges")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 1. Database & Storage Infrastructure
	var dbClient *database.Client
	if cfg.SupabaseURL != "" && cfg.SupabaseServiceRoleKey != "" {
		dbClient = database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	}

	localStorage, _ := storage.NewLocalStorage("storage")
	var store storage.Storage
	if cfg.SupabaseURL != "" && cfg.SupabaseServiceRoleKey != "" {
		store = storage.NewSupabaseStorage(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, localStorage)
	} else {
		store = localStorage
	}

	// 2. Health Check Probes
	health := handler.NewHealthHandler(cfg, supabase.NewClient(cfg.SupabaseURL, cfg.SupabasePublishableKey))
	router.GET("/healthz", health.Liveness)
	router.GET("/readyz", health.Readiness)

	// 3. Domain Modular MVC Resources
	// User Resource
	userDB := user.NewResourceDB(dbClient)
	userRes := user.NewResource(userDB, cfg.JWTSecret)

	// Contract Resource
	contractDB := contract.NewResourceDB(dbClient)
	contractRes := contract.NewResource(contractDB)

	// Settings Resource
	settingsDB := settings.NewResourceDB(dbClient)
	settingsRes := settings.NewResource(settingsDB)

	// Audit Resource
	auditDB := audit.NewResourceDB(dbClient)
	auditRes := audit.NewResource(auditDB)

	// Reconciliation Resource
	reconDB := reconciliation.NewResourceDB(dbClient)
	reconRes := reconciliation.NewResource(reconDB, nil)
	reconRes.SetStorage(store)

	// Document Resource
	docDB := document.NewResourceDB(dbClient)
	docRes := document.NewResource(docDB, store)
	docRes.SetEventSink(reconDB)
	docRes.SetAIServiceURL(cfg.AIServiceURL)
	docRes.SetAIServiceTimeout(cfg.AIServiceTimeout)

	// 4. Register API v1 Modular Routes
	v1 := router.Group("/api/v1")
	{
		userControllers.RegisterRoutes(v1, userRes)
		documentControllers.RegisterRoutes(v1, docRes)
		contractControllers.RegisterRoutes(v1, contractRes)
		settingsControllers.RegisterRoutes(v1, settingsRes)
		auditControllers.RegisterRoutes(v1, auditRes)
		reconciliationControllers.RegisterRoutes(v1, reconRes)
	}

	return &Application{config: cfg, router: router}
}

func (a *Application) Run() error {
	return a.router.Run(":" + a.config.Port)
}

func (a *Application) Router() *gin.Engine {
	return a.router
}
