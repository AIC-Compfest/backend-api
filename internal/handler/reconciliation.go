package handler

import (
	"net/http"

	"backend-api/internal/model"
	"backend-api/internal/service/reconciliation"
	"github.com/gin-gonic/gin"
)

// ReconcileRequest payload for REST endpoint
type ReconcileRequest struct {
	Invoice  model.InvoiceCanonical  `json:"invoice" binding:"required"`
	Shipment model.ShipmentCanonical `json:"shipment" binding:"required"`
	POD      *model.PODCanonical     `json:"pod,omitempty"`
	Contract model.ContractCanonical `json:"contract" binding:"required"`
}

// ReconciliationHandler exposes HTTP endpoints for Phase 7 & 8 Reconciliation & Review Workflows.
type ReconciliationHandler struct {
	engine        *reconciliation.Engine
	reviewService *reconciliation.ReviewService
}

// NewReconciliationHandler creates a new handler instance.
func NewReconciliationHandler(engine *reconciliation.Engine, reviewService *reconciliation.ReviewService) *ReconciliationHandler {
	return &ReconciliationHandler{
		engine:        engine,
		reviewService: reviewService,
	}
}

// Reconcile handles POST /api/v1/reconcile
func (h *ReconciliationHandler) Reconcile(c *gin.Context) {
	var req ReconcileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reconciliation request payload: " + err.Error()})
		return
	}

	event, err := h.engine.ReconcileTransaction(&req.Invoice, &req.Shipment, req.POD, &req.Contract)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reconciliation error: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, event)
}

// GetQueue handles GET /api/v1/reconcile/queue (PRD §38)
func (h *ReconciliationHandler) GetQueue(c *gin.Context) {
	status := c.Query("status")
	severity := c.Query("severity")
	vendor := c.Query("vendor")
	search := c.Query("search")

	items, summary := h.reviewService.GetQueue(status, severity, vendor, search)
	c.JSON(http.StatusOK, gin.H{
		"items":   items,
		"summary": summary,
	})
}

// GetReconciliation handles GET /api/v1/reconcile/:id
func (h *ReconciliationHandler) GetReconciliation(c *gin.Context) {
	id := c.Param("id")
	event, exists := h.reviewService.GetEvent(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "reconciliation event not found"})
		return
	}

	c.JSON(http.StatusOK, event)
}

// SubmitDecision handles POST /api/v1/reconcile/:id/decision (PRD §36, §37)
func (h *ReconciliationHandler) SubmitDecision(c *gin.Context) {
	id := c.Param("id")
	var req reconciliation.DecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid decision payload: " + err.Error()})
		return
	}

	event, err := h.reviewService.SubmitDecision(id, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, event)
}

// GetDisputePackage handles GET /api/v1/reconcile/:id/dispute-package (PRD §39)
func (h *ReconciliationHandler) GetDisputePackage(c *gin.Context) {
	id := c.Param("id")
	pkg, err := h.reviewService.GenerateDisputePackage(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, pkg)
}
