package handler

import (
	"net/http"

	"backend-api/internal/config"
	"backend-api/internal/supabase"
	"github.com/gin-gonic/gin"
)

type HealthHandler struct { config config.Config; supabase *supabase.Client }
func NewHealthHandler(cfg config.Config, client *supabase.Client) *HealthHandler { return &HealthHandler{config: cfg, supabase: client} }
func (h *HealthHandler) Liveness(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status":"ok", "service":"backend-api"}) }
func (h *HealthHandler) Readiness(c *gin.Context) {
	if err := h.config.ValidateSupabase(); err != nil { c.JSON(http.StatusServiceUnavailable, gin.H{"status":"not_ready", "error":"configuration"}); return }
	if err := h.supabase.Check(c.Request.Context()); err != nil { c.JSON(http.StatusServiceUnavailable, gin.H{"status":"not_ready", "error":"supabase"}); return }
	c.JSON(http.StatusOK, gin.H{"status":"ready", "schema_version":h.config.SchemaVersion, "config_version":h.config.ConfigVersion})
}
