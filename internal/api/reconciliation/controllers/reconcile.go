package controllers

import (
	"net/http"

	apireconcile "backend-api/internal/api/reconciliation"
	"github.com/gin-gonic/gin"
)

// ReconcileController handles single-transaction deterministic reconciliation.
type ReconcileController struct {
	resource *apireconcile.Resource
}

// NewReconcileController creates a new controller instance.
func NewReconcileController(resource *apireconcile.Resource) *ReconcileController {
	return &ReconcileController{resource: resource}
}

// Handle processes POST /api/v1/reconcile
func (ctrl *ReconcileController) Handle(c *gin.Context) {
	var req apireconcile.ReconcileRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apireconcile.ErrorResponseDTO{
			Error:   "invalid reconciliation request payload",
			Details: err.Error(),
		})
		return
	}

	event, err := ctrl.resource.Reconcile(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apireconcile.ErrorResponseDTO{
			Error:   "reconciliation execution failed",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, event)
}
