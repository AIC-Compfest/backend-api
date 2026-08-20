package controllers

import (
	"io"
	"net/http"
	"strconv"

	"backend-api/internal/document"
	"github.com/gin-gonic/gin"
)

// Upload handles POST /api/v1/documents/upload
func Upload(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded. Expected form field 'file'"})
			return
		}

		resp, err := res.Upload(c.Request.Context(), fileHeader)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, resp)
	}
}

// Get handles GET /api/v1/documents/:id
func Get(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		doc, err := res.GetDocument(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, doc)
	}
}

// List handles GET /api/v1/documents
func List(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		statusFilter := c.Query("status")

		resp, err := res.ListDocuments(c.Request.Context(), page, pageSize, statusFilter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// Download handles GET /api/v1/documents/:id/download
func Download(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		reader, doc, err := res.Download(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		defer reader.Close()

		c.Header("Content-Disposition", "inline; filename="+doc.Filename)
		c.Header("Content-Type", doc.MimeType)
		_, _ = io.Copy(c.Writer, reader)
	}
}
