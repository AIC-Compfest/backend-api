package controllers

import (
	"net/http"

	apireconcile "backend-api/internal/api/reconciliation"
	"github.com/gin-gonic/gin"
)

// DetailController handles fetching single reconciliation event with audit trace.
type DetailController struct {
	resource *apireconcile.Resource
}

// NewDetailController creates a new detail controller instance.
func NewDetailController(resource *apireconcile.Resource) *DetailController {
	return &DetailController{resource: resource}
}

// Handle processes GET /api/v1/reconcile/:id
func (ctrl *DetailController) Handle(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, apireconcile.ErrorResponseDTO{
			Error: "missing required event or shipment id parameter",
		})
		return
	}

	event, err := ctrl.resource.GetEventByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, apireconcile.ErrorResponseDTO{
			Error:   "reconciliation event not found",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, event)
}
