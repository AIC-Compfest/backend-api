package handler

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"backend-api/internal/service"
	"github.com/gin-gonic/gin"
)

// DocumentHandler exposes HTTP endpoints for document ingestion and retrieval.
type DocumentHandler struct {
	ingestionService *service.IngestionService
}

// NewDocumentHandler creates a DocumentHandler instance.
func NewDocumentHandler(ingestionService *service.IngestionService) *DocumentHandler {
	return &DocumentHandler{
		ingestionService: ingestionService,
	}
}

// Upload handles multipart document uploads.
// POST /api/v1/documents/upload
func (h *DocumentHandler) Upload(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Missing file in multipart form",
			"message": "Please provide a valid file under form key 'file'",
		})
		return
	}

	src, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to open uploaded file stream",
			"message": err.Error(),
		})
		return
	}
	defer src.Close()

	resp, err := h.ingestionService.Ingest(c.Request.Context(), fileHeader.Filename, src, fileHeader.Size)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Document ingestion failed",
			"message": err.Error(),
		})
		return
	}

	if resp.IsDuplicate {
		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "Document already ingested (deduplicated via SHA-256)",
			"data":    resp,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status":  "success",
		"message": "Document successfully queued for processing",
		"data":    resp,
	})
}

// GetDocument retrieves document metadata and status by ID.
// GET /api/v1/documents/:id
func (h *DocumentHandler) GetDocument(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Document ID is required"})
		return
	}

	doc, err := h.ingestionService.GetDocument(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if doc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Document '%s' not found", id)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   doc,
	})
}

// ListDocuments lists ingested documents with pagination.
// GET /api/v1/documents?limit=20&offset=0
func (h *DocumentHandler) ListDocuments(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	docs, total, err := h.ingestionService.ListDocuments(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   docs,
		"pagination": gin.H{
			"total":  total,
			"limit":  limit,
			"offset": offset,
			"count":  len(docs),
		},
	})
}

// Download serves the raw stored document file.
// GET /api/v1/documents/:id/download
func (h *DocumentHandler) Download(c *gin.Context) {
	id := c.Param("id")
	doc, err := h.ingestionService.GetDocument(c.Request.Context(), id)
	if err != nil || doc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Document not found"})
		return
	}

	rc, err := h.ingestionService.OpenDocumentStream(c.Request.Context(), doc.StoragePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open document file stream"})
		return
	}
	defer rc.Close()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", doc.Filename))
	c.Header("Content-Type", doc.MimeType)
	_, _ = io.Copy(c.Writer, rc)
}
