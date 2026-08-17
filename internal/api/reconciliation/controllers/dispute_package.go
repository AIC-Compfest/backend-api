package controllers

import (
	"net/http"

	apireconcile "github.com/AIC-Compfest/backend-api/internal/api/reconciliation"
	"github.com/gin-gonic/gin"
)

// DisputePackageController handles constructing formal dispute claim memos.
type DisputePackageController struct {
	resource *apireconcile.Resource
}

// NewDisputePackageController creates a new dispute package controller instance.
func NewDisputePackageController(resource *apireconcile.Resource) *DisputePackageController {
	return &DisputePackageController{resource: resource}
}

// Handle processes GET /api/v1/reconcile/:id/dispute-package (PRD §39)
func (ctrl *DisputePackageController) Handle(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, apireconcile.ErrorResponseDTO{
			Error: "missing required event id parameter",
		})
		return
	}

	pkg, err := ctrl.resource.GenerateDisputePackage(id)
	if err != nil {
		c.JSON(http.StatusNotFound, apireconcile.ErrorResponseDTO{
			Error:   "failed to generate dispute claim package",
			Details: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, pkg)
}
