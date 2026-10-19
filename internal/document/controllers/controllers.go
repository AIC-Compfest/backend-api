package controllers

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

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

		// Uploads are capped at MaxDocumentSizeBytes, so buffering is bounded.
		// A seekable body lets ServeContent set Content-Length and honour Range (pdf.js).
		data, err := io.ReadAll(reader)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read stored document"})
			return
		}
		c.Header("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": doc.Filename}))
		c.Header("Content-Type", doc.MimeType)
		c.Header("Cache-Control", "private, max-age=300")
		http.ServeContent(c.Writer, c.Request, doc.Filename, time.Time{}, bytes.NewReader(data))
	}
}

// Evidence handles GET /api/v1/documents/:id/evidence?page=N
func Evidence(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
		rows, err := res.ListFieldEvidence(c.Request.Context(), id, page)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"document_id": id, "count": len(rows), "evidence": rows})
	}
}

// PipelineStatus handles GET /api/v1/documents/:id/pipeline-status
func PipelineStatus(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		status, err := res.GetPipelineStatus(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, status)
	}
}

// ListPages handles GET /api/v1/documents/:id/pages
func ListPages(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		pages, err := res.ListPages(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"document_id": id, "count": len(pages), "pages": pages})
	}
}

// GetPage handles GET /api/v1/documents/:id/pages/:page_number
func GetPage(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		pageNumber, err := strconv.Atoi(c.Param("page_number"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_number parameter"})
			return
		}

		page, err := res.GetPage(c.Request.Context(), id, pageNumber)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, page)
	}
}

// RetryPage handles POST /api/v1/documents/:id/pages/:page_number/retry
func RetryPage(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		pageNumber, err := strconv.Atoi(c.Param("page_number"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_number parameter"})
			return
		}

		page, err := res.RetryPage(c.Request.Context(), id, pageNumber)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Page extraction retry scheduled", "page": page})
	}
}

// ListSegments handles GET /api/v1/documents/:id/segments
func ListSegments(res *document.Resource) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		segments, err := res.ListSegments(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"document_id": id,
			"count":       len(segments),
			"segments":    segments,
		})
	}
}
