package document_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend-api/internal/document"
	"backend-api/internal/document/controllers"
	"backend-api/internal/storage"
	"github.com/gin-gonic/gin"
)

func setupDocTestRouter(t *testing.T) (*gin.Engine, *document.Resource) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	docDB := document.NewResourceDB(nil)
	store, _ := storage.NewLocalStorage(t.TempDir())
	docRes := document.NewResource(docDB, store)

	v1 := router.Group("/api/v1")
	controllers.RegisterRoutes(v1, docRes)

	return router, docRes
}

func TestDocument_MultiPageAndPipelineStatus(t *testing.T) {
	router, _ := setupDocTestRouter(t)

	// 1. Test Multipart Upload
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "INV-2026-00001_multi.pdf")
	_, _ = part.Write([]byte("%PDF-1.4 3PL Multi-Page Test Invoice Content"))
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/documents/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var uploadResp document.UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	docID := uploadResp.ID
	if docID == "" {
		t.Fatalf("expected non-empty docID")
	}

	// 2. Test GET /api/v1/documents/:id/pipeline-status
	statusReq, _ := http.NewRequest(http.MethodGet, "/api/v1/documents/"+docID+"/pipeline-status", nil)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for pipeline-status, got %d: %s", statusRec.Code, statusRec.Body.String())
	}

	var statusResp document.PipelineStatusResponse
	if err := json.Unmarshal(statusRec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to decode pipeline-status: %v", err)
	}

	if statusResp.DocumentID != docID {
		t.Errorf("expected docID %s in status response, got %s", docID, statusResp.DocumentID)
	}

	// 3. Test GET /api/v1/documents/:id/pages
	pagesReq, _ := http.NewRequest(http.MethodGet, "/api/v1/documents/"+docID+"/pages", nil)
	pagesRec := httptest.NewRecorder()
	router.ServeHTTP(pagesRec, pagesReq)

	if pagesRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for pages list, got %d", pagesRec.Code)
	}

	// 4. Test GET /api/v1/documents/:id/pages/1
	pageReq, _ := http.NewRequest(http.MethodGet, "/api/v1/documents/"+docID+"/pages/1", nil)
	pageRec := httptest.NewRecorder()
	router.ServeHTTP(pageRec, pageReq)

	if pageRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for page 1, got %d", pageRec.Code)
	}

	// 5. Test POST /api/v1/documents/:id/pages/1/retry
	retryReq, _ := http.NewRequest(http.MethodPost, "/api/v1/documents/"+docID+"/pages/1/retry", nil)
	retryRec := httptest.NewRecorder()
	router.ServeHTTP(retryRec, retryReq)

	if retryRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for page retry, got %d", retryRec.Code)
	}
}

func TestDocument_Deduplication(t *testing.T) {
	router, _ := setupDocTestRouter(t)

	content := []byte("%PDF-1.4 Same SHA-256 Hash Invariant Test Content")

	// First upload
	body1 := new(bytes.Buffer)
	writer1 := multipart.NewWriter(body1)
	part1, _ := writer1.CreateFormFile("file", "doc_first.pdf")
	_, _ = part1.Write(content)
	_ = writer1.Close()

	req1, _ := http.NewRequest(http.MethodPost, "/api/v1/documents/upload", body1)
	req1.Header.Set("Content-Type", writer1.FormDataContentType())
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first upload expected HTTP 201, got %d", rec1.Code)
	}

	// Second upload with identical bytes
	body2 := new(bytes.Buffer)
	writer2 := multipart.NewWriter(body2)
	part2, _ := writer2.CreateFormFile("file", "doc_second_same_bytes.pdf")
	_, _ = part2.Write(content)
	_ = writer2.Close()

	req2, _ := http.NewRequest(http.MethodPost, "/api/v1/documents/upload", body2)
	req2.Header.Set("Content-Type", writer2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	var secondResp document.UploadResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &secondResp)

	if !secondResp.IsDuplicate {
		t.Errorf("expected IsDuplicate == true for identical SHA-256 hash submission")
	}
}

func TestDocument_SegmentsEndpoint(t *testing.T) {
	router, _ := setupDocTestRouter(t)

	// 1. Upload a document
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test_segmented_invoice.pdf")
	_, _ = part.Write([]byte("%PDF-1.4 Segment Verification Test Content"))
	_ = writer.Close()

	req, _ := http.NewRequest(http.MethodPost, "/api/v1/documents/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var uploadResp document.UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	if uploadResp.SegmentsURL == "" {
		t.Errorf("expected non-empty SegmentsURL in upload response")
	}

	// 2. Test GET /api/v1/documents/:id/segments
	segReq, _ := http.NewRequest(http.MethodGet, "/api/v1/documents/"+uploadResp.ID+"/segments", nil)
	segRec := httptest.NewRecorder()
	router.ServeHTTP(segRec, segReq)

	if segRec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for segments, got %d: %s", segRec.Code, segRec.Body.String())
	}

	var segResp struct {
		DocumentID string                     `json:"document_id"`
		Count      int                        `json:"count"`
		Segments   []document.DocumentSegment `json:"segments"`
	}
	if err := json.Unmarshal(segRec.Body.Bytes(), &segResp); err != nil {
		t.Fatalf("failed to decode segments response: %v", err)
	}

	if segResp.DocumentID != uploadResp.ID {
		t.Errorf("expected document_id %s, got %s", uploadResp.ID, segResp.DocumentID)
	}
	if segResp.Count == 0 || len(segResp.Segments) == 0 {
		t.Errorf("expected at least 1 segment, got count=%d", segResp.Count)
	}
	if segResp.Segments[0].SegmentNumber != 1 {
		t.Errorf("expected segment_number 1, got %d", segResp.Segments[0].SegmentNumber)
	}
}

