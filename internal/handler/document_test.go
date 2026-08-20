package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend-api/internal/repository"
	"backend-api/internal/service"
	"backend-api/internal/storage"
	"github.com/gin-gonic/gin"
)

func setupTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	repo := repository.NewMemoryDocumentRepository()
	store, _ := storage.NewLocalStorage(t.TempDir())
	ingestionService := service.NewIngestionService(repo, store, "http://localhost:8100")
	docHandler := NewDocumentHandler(ingestionService)

	v1 := router.Group("/api/v1")
	{
		docs := v1.Group("/documents")
		{
			docs.POST("/upload", docHandler.Upload)
			docs.GET("", docHandler.ListDocuments)
			docs.GET("/:id", docHandler.GetDocument)
		}
	}
	return router
}

func TestDocumentHandler_UploadAndGet(t *testing.T) {
	router := setupTestRouter(t)

	// Create multipart upload request
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "sample_invoice.pdf")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write([]byte("%PDF-1.4 Mock 3PL Invoice Content for API test"))
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/documents/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var uploadResp struct {
		Status string `json:"status"`
		Data   struct {
			DocumentID string `json:"document_id"`
			Filename   string `json:"filename"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	docID := uploadResp.Data.DocumentID
	if docID == "" {
		t.Fatalf("expected non-empty DocumentID")
	}

	// Test GET /api/v1/documents/:id
	getReq, _ := http.NewRequest(http.MethodGet, "/api/v1/documents/"+docID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for GET document, got %d", getRec.Code)
	}

	// Test GET /api/v1/documents (List)
	listReq, _ := http.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for list documents, got %d", listRec.Code)
	}
}
