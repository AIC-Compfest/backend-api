package controllers

import (
	"net/http"
	"strconv"

	"backend-api/internal/model"
	"backend-api/internal/reconciliation"
	"github.com/gin-gonic/gin"
)

// Reconcile handles POST /api/v1/reconcile
func Reconcile(res *reconciliation.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Invoice  *model.InvoiceCanonical  `json:"invoice"`
			Shipment *model.ShipmentCanonical `json:"shipment"`
			POD      *model.PODCanonical      `json:"pod"`
			Contract *model.ContractCanonical `json:"contract"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document payload: " + err.Error()})
			return
		}

		event, err := res.Reconcile(c.Request.Context(), req.Invoice, req.Shipment, req.POD, req.Contract)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, event)
	}
}

// Queue handles GET /api/v1/reconcile/queue
func Queue(res *reconciliation.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		status := c.Query("status")
		severity := c.Query("severity")
		vendor := c.Query("vendor")
		search := c.Query("search")

		filter := reconciliation.QueueFilter{
			Page:     page,
			PageSize: pageSize,
			Status:   status,
			Severity: severity,
			Vendor:   vendor,
			Search:   search,
		}

		resp, err := res.GetQueue(c.Request.Context(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch queue: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// Detail handles GET /api/v1/reconcile/:id
func Detail(res *reconciliation.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		event, err := res.GetDetail(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, event)
	}
}

// Decision handles POST /api/v1/reconcile/:id/decision
func Decision(res *reconciliation.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var req reconciliation.DecisionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid decision payload: " + err.Error()})
			return
		}

		// Fallback reviewer from context if not set
		if req.ReviewerID == "" {
			if emailVal, ok := c.Get("email"); ok {
				req.ReviewerID, _ = emailVal.(string)
			}
		}
		if req.Role == "" {
			if roleVal, ok := c.Get("role"); ok {
				req.Role, _ = roleVal.(string)
			}
		}

		resp, err := res.SubmitDecision(c.Request.Context(), id, req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

// DisputePackage handles GET /api/v1/reconcile/:id/dispute-package
func DisputePackage(res *reconciliation.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		reviewer := c.DefaultQuery("reviewer", "AP Manager")

		pkg, err := res.GetDisputePackage(c.Request.Context(), id, reviewer)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, pkg)
	}
}
