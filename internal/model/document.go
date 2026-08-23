package model

import (
	"crypto/rand"
	"fmt"
	"time"
)

// DocumentStatus constants
const (
	StatusQueued     = "QUEUED"
	StatusProcessing = "PROCESSING"
	StatusCompleted  = "COMPLETED"
	StatusFailed     = "FAILED"
)

// DocumentClassification constants — used by DocumentSegment.DocumentType, NOT Document.Classification
// These are defined here for shared use across models and services.
const (
	ClassInvoice       = "INVOICE"
	ClassSuratJalan    = "SURAT_JALAN"
	ClassPOD           = "POD"
	ClassRateAgreement = "RATE_AGREEMENT"
	ClassUnknown       = "UNKNOWN"
)

// ClassificationIsValid returns true if the given classification string is a known document type.
func ClassificationIsValid(c string) bool {
	switch c {
	case ClassInvoice, ClassSuratJalan, ClassPOD, ClassRateAgreement, ClassUnknown:
		return true
	}
	return false
}

// Document represents an ingested physical file record.
// IMPORTANT: Document is the physical container only.
// Classification lives in DocumentSegment, NOT here.
// One Document can have N DocumentSegments (one per logical document within the file).
type Document struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	FileSize    int64     `json:"file_size"`
	MimeType    string    `json:"mime_type"`
	SHA256Hash  string    `json:"sha256_hash"`
	StoragePath string    `json:"storage_path"`
	Status      string    `json:"status"`     // Processing lifecycle: QUEUED → PROCESSING → COMPLETED | FAILED
	PageCount   int       `json:"page_count"` // Filled after PDF is split into pages
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// DEPRECATED fields — kept for backward-compatibility during migration.
	// Do NOT write to these. Read from document_segments instead.
	// Will be removed after full migration.
	Classification           string  `json:"classification,omitempty"`
	ClassificationConfidence float64 `json:"classification_confidence,omitempty"`
}

// UploadResponse returned on successful ingestion.
// Note: No classification here — classification is done per-segment after OCR.
// Client should poll GET /documents/:id/segments to get classification results.
type UploadResponse struct {
	DocumentID  string    `json:"document_id"`
	Filename    string    `json:"filename"`
	FileSize    int64     `json:"file_size"`
	MimeType    string    `json:"mime_type"`
	SHA256Hash  string    `json:"sha256_hash"`
	Status      string    `json:"status"`
	IsDuplicate bool      `json:"is_duplicate"`
	CreatedAt   time.Time `json:"created_at"`
	// SegmentsURL is where to poll for segment classification results
	SegmentsURL string `json:"segments_url,omitempty"`
}

// NewUUID generates a standard RFC4122 version 4 UUID without external dependencies.
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
