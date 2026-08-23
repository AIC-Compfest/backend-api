package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"backend-api/internal/model"
	"backend-api/internal/repository"
	"backend-api/internal/storage"
)

// Allowed extensions and max file size (25 MB)
var (
	AllowedExtensions = map[string]string{
		".pdf":  "application/pdf",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".heic": "image/heic",
	}
	MaxFileSizeBytes int64 = 26214400 // 25 MB
)

// IngestionService handles document upload, validation, hashing, and deduplication.
type IngestionService struct {
	repo         repository.DocumentRepository
	store        storage.Storage
	aiServiceURL string
	httpClient   *http.Client
}

// NewIngestionService creates an IngestionService instance.
func NewIngestionService(repo repository.DocumentRepository, store storage.Storage, aiServiceURL string) *IngestionService {
	return &IngestionService{
		repo:         repo,
		store:        store,
		aiServiceURL: strings.TrimRight(aiServiceURL, "/"),
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Ingest processes a document stream: validates, hashes, deduplicates, stores, and registers.
func (s *IngestionService) Ingest(ctx context.Context, filename string, r io.Reader, size int64) (*model.UploadResponse, error) {
	// 1. Validate Extension
	ext := strings.ToLower(filepath.Ext(filename))
	mimeType, allowed := AllowedExtensions[ext]
	if !allowed {
		return nil, fmt.Errorf("unsupported file extension '%s'. Allowed: .pdf, .jpg, .jpeg, .png, .heic", ext)
	}

	// 2. Validate Size Limit
	if size > MaxFileSizeBytes {
		return nil, fmt.Errorf("file size %d bytes exceeds maximum limit of 25 MB", size)
	}

	// 3. In-flight SHA-256 Hashing and Buffer
	hasher := sha256.New()
	buf := new(bytes.Buffer)
	tee := io.TeeReader(r, hasher)

	written, err := io.Copy(buf, tee)
	if err != nil {
		return nil, fmt.Errorf("failed to read document stream: %w", err)
	}
	if written == 0 {
		return nil, errors.New("cannot ingest empty file (0 bytes)")
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))

	// 4. Level 1 Exact Deduplication Check
	existingDoc, err := s.repo.FindBySHA256(ctx, hashStr)
	if err != nil {
		return nil, fmt.Errorf("database query error: %w", err)
	}

	if existingDoc != nil {
		// Idempotent return: Document already ingested
		return &model.UploadResponse{
			DocumentID:  existingDoc.ID,
			Filename:    existingDoc.Filename,
			FileSize:    existingDoc.FileSize,
			MimeType:    existingDoc.MimeType,
			SHA256Hash:  existingDoc.SHA256Hash,
			Status:      existingDoc.Status,
			IsDuplicate: true,
			CreatedAt:   existingDoc.CreatedAt,
			// SegmentsURL tells client where to poll for segment classification results
			SegmentsURL: fmt.Sprintf("/api/v1/documents/%s/segments", existingDoc.ID),
		}, nil
	}

	// 5. Store file in storage
	storageFilename := fmt.Sprintf("%s%s", hashStr, ext)
	storagePath, err := s.store.Save(ctx, "documents-original", storageFilename, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("storage error: %w", err)
	}

	// 6. Create document database record
	// Classification is NOT set here — it belongs to DocumentSegment, not Document.
	// Segments will be created after AI extraction completes.
	doc := &model.Document{
		ID:          model.NewUUID(),
		Filename:    filename,
		FileSize:    written,
		MimeType:    mimeType,
		SHA256Hash:  hashStr,
		StoragePath: storagePath,
		Status:      model.StatusQueued,
		CreatedAt:   time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, doc); err != nil {
		return nil, fmt.Errorf("failed to create document record: %w", err)
	}

	// 7. Trigger async extraction job (non-blocking)
	go s.triggerAIExtraction(doc.ID, storagePath, mimeType)

	return &model.UploadResponse{
		DocumentID:  doc.ID,
		Filename:    doc.Filename,
		FileSize:    doc.FileSize,
		MimeType:    doc.MimeType,
		SHA256Hash:  doc.SHA256Hash,
		Status:      doc.Status,
		IsDuplicate: false,
		CreatedAt:   doc.CreatedAt,
		SegmentsURL: fmt.Sprintf("/api/v1/documents/%s/segments", doc.ID),
	}, nil
}

// GetDocument retrieves a document record by ID.
func (s *IngestionService) GetDocument(ctx context.Context, id string) (*model.Document, error) {
	return s.repo.FindByID(ctx, id)
}

// ListDocuments lists documents with pagination.
func (s *IngestionService) ListDocuments(ctx context.Context, limit, offset int) ([]*model.Document, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.List(ctx, limit, offset)
}

// OpenDocumentStream opens a read stream for downloading the document.
func (s *IngestionService) OpenDocumentStream(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	return s.store.Open(ctx, storagePath)
}

// triggerAIExtraction notifies ai-service about newly queued document
func (s *IngestionService) triggerAIExtraction(docID, storagePath, mimeType string) {
	if s.aiServiceURL == "" {
		return
	}
	// Best-effort background dispatch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/v1/extract", s.aiServiceURL)
	payload := fmt.Sprintf(`{"document_id":"%s","storage_path":"%s","mime_type":"%s"}`, docID, storagePath, mimeType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}
