package controllers

import (
	"net/http"

	"backend-api/internal/contract"
	"github.com/gin-gonic/gin"
)

// List handles GET /api/v1/contracts
func List(res *contract.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		contracts, err := res.ListContracts(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch contracts: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": contracts, "total": len(contracts)})
	}
}

// Get handles GET /api/v1/contracts/:id
func Get(res *contract.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		ctr, err := res.GetContract(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, ctr)
	}
}
