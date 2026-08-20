package service

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"backend-api/internal/repository"
	"backend-api/internal/storage"
)

func TestIngestionService_IngestAndDeduplicate(t *testing.T) {
	repo := repository.NewMemoryDocumentRepository()
	store, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create temp local storage: %v", err)
	}

	svc := NewIngestionService(repo, store, "http://localhost:8100")
	ctx := context.Background()

	fileContent := []byte("%PDF-1.4 Fake Invoice 3PL Test Content")
	filename := "test_invoice.pdf"

	// 1. First Ingestion: Should succeed with IsDuplicate: false
	resp1, err := svc.Ingest(ctx, filename, bytes.NewReader(fileContent), int64(len(fileContent)))
	if err != nil {
		t.Fatalf("first ingestion failed: %v", err)
	}
	if resp1.IsDuplicate {
		t.Errorf("expected IsDuplicate to be false on first upload")
	}
	if resp1.DocumentID == "" {
		t.Errorf("expected non-empty DocumentID")
	}
	if resp1.SHA256Hash == "" {
		t.Errorf("expected non-empty SHA256Hash")
	}

	// 2. Second Ingestion (Identical file): Should trigger Level 1 Deduplication (IsDuplicate: true)
	resp2, err := svc.Ingest(ctx, filename, bytes.NewReader(fileContent), int64(len(fileContent)))
	if err != nil {
		t.Fatalf("second ingestion failed: %v", err)
	}
	if !resp2.IsDuplicate {
		t.Errorf("expected IsDuplicate to be true on second upload of identical hash")
	}
	if resp2.DocumentID != resp1.DocumentID {
		t.Errorf("expected identical DocumentID (%s), got %s", resp1.DocumentID, resp2.DocumentID)
	}

	// 3. Third Ingestion (Unsupported extension): Should fail validation
	_, err = svc.Ingest(ctx, "malicious.exe", strings.NewReader("bad"), 3)
	if err == nil {
		t.Errorf("expected error for unsupported .exe extension, got nil")
	}
}
