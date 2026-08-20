package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"backend-api/internal/config"
	"backend-api/internal/supabase"
	"github.com/gin-gonic/gin"
)

func router(cfg config.Config, upstream http.HandlerFunc) *gin.Engine {
	server := httptest.NewServer(upstream)
	// The server is intentionally kept for the request lifetime in each test.
	g := gin.New(); h := NewHealthHandler(cfg, supabase.NewClient(server.URL, cfg.SupabasePublishableKey))
	g.GET("/healthz", h.Liveness); g.GET("/readyz", h.Readiness)
	return g
}
func TestLiveness(t *testing.T) { gin.SetMode(gin.TestMode); g := router(config.Config{}, func(w http.ResponseWriter, _ *http.Request){w.WriteHeader(200)}); r := httptest.NewRecorder(); g.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/healthz", nil)); if r.Code != http.StatusOK { t.Fatalf("status = %d", r.Code) } }
func TestReadinessRejectsMissingConfig(t *testing.T) { gin.SetMode(gin.TestMode); g := router(config.Config{}, func(w http.ResponseWriter, _ *http.Request){w.WriteHeader(200)}); r := httptest.NewRecorder(); g.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil)); if r.Code != http.StatusServiceUnavailable { t.Fatalf("status = %d", r.Code) } }
