package controllers

import (
	"net/http"

	apireconcile "backend-api/internal/api/reconciliation"
	"github.com/gin-gonic/gin"
)

// DecisionController handles reviewer decisions (Approve, Dispute, Reject) with audit log.
type DecisionController struct {
	resource *apireconcile.Resource
}

// NewDecisionController creates a new decision controller instance.
func NewDecisionController(resource *apireconcile.Resource) *DecisionController {
	return &DecisionController{resource: resource}
}

// Handle processes POST /api/v1/reconcile/:id/decision (PRD §36, §37)
func (ctrl *DecisionController) Handle(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, apireconcile.ErrorResponseDTO{
			Error: "missing required event id parameter",
		})
		return
	}

	var req apireconcile.DecisionRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, apireconcile.ErrorResponseDTO{
			Error:   "invalid decision request payload",
			Details: err.Error(),
		})
		return
	}

	event, err := ctrl.resource.SubmitDecision(id, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, apireconcile.ErrorResponseDTO{
			Error:   "failed to record reviewer decision",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, event)
}
