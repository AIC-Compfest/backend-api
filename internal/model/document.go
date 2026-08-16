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

// DocumentClassification constants
const (
	ClassInvoice       = "INVOICE"
	ClassSuratJalan    = "SURAT_JALAN"
	ClassPOD           = "POD"
	ClassRateAgreement = "RATE_AGREEMENT"
	ClassUnknown       = "UNKNOWN"
)

// Document represents an ingested logistics file record.
type Document struct {
	ID                       string    `json:"id"`
	Filename                 string    `json:"filename"`
	FileSize                 int64     `json:"file_size"`
	MimeType                 string    `json:"mime_type"`
	SHA256Hash               string    `json:"sha256_hash"`
	StoragePath              string    `json:"storage_path"`
	Status                   string    `json:"status"`
	Classification           string    `json:"classification"`
	ClassificationConfidence float64   `json:"classification_confidence"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// UploadResponse returned on successful ingestion.
type UploadResponse struct {
	DocumentID     string    `json:"document_id"`
	Filename       string    `json:"filename"`
	FileSize       int64     `json:"file_size"`
	MimeType       string    `json:"mime_type"`
	SHA256Hash     string    `json:"sha256_hash"`
	Status         string    `json:"status"`
	IsDuplicate    bool      `json:"is_duplicate"`
	Classification string    `json:"classification,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// NewUUID generates a standard RFC4122 version 4 UUID without external dependencies.
func NewUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant RFC4122
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
