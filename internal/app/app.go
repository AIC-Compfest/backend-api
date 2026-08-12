package app

import (
	"github.com/AIC-Compfest/backend-api/internal/config"
	"github.com/AIC-Compfest/backend-api/internal/handler"
	"github.com/AIC-Compfest/backend-api/internal/supabase"
	"github.com/gin-gonic/gin"
)

type Application struct { config config.Config; router *gin.Engine }
func New(cfg config.Config) *Application {
	router := gin.New(); router.Use(gin.Logger(), gin.Recovery())
	health := handler.NewHealthHandler(cfg, supabase.NewClient(cfg.SupabaseURL, cfg.SupabasePublishableKey))
	router.GET("/healthz", health.Liveness); router.GET("/readyz", health.Readiness)
	return &Application{config:cfg, router:router}
}
func (a *Application) Run() error { return a.router.Run(":"+a.config.Port) }
func (a *Application) Router() *gin.Engine { return a.router }
