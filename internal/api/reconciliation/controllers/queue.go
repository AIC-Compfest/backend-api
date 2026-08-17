package controllers

import (
	"net/http"

	apireconcile "github.com/AIC-Compfest/backend-api/internal/api/reconciliation"
	"github.com/gin-gonic/gin"
)

// QueueController handles fetching and filtering prioritized review queue.
type QueueController struct {
	resource *apireconcile.Resource
}

// NewQueueController creates a new queue controller instance.
func NewQueueController(resource *apireconcile.Resource) *QueueController {
	return &QueueController{resource: resource}
}

// Handle processes GET /api/v1/reconcile/queue (PRD §38)
func (ctrl *QueueController) Handle(c *gin.Context) {
	query := apireconcile.QueueQueryDTO{
		StatusFilter:   c.Query("status"),
		SeverityFilter: c.Query("severity"),
		VendorFilter:   c.Query("vendor"),
		Search:         c.Query("search"),
	}

	queueResponse, err := ctrl.resource.GetQueue(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, apireconcile.ErrorResponseDTO{
			Error:   "failed to retrieve reconciliation queue",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, queueResponse)
}
