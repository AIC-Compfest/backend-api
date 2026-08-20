package controllers

import (
	"net/http"
	"strconv"

	"backend-api/internal/audit"
	"github.com/gin-gonic/gin"
)

// List handles GET /api/v1/audit
func List(res *audit.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		entityID := c.Query("entity_id")
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

		resp, err := res.ListAuditLogs(c.Request.Context(), entityID, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}
