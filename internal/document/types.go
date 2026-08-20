package document

import "time"

// Document represents a registered digital document in Supabase
type Document struct {
	ID                       string     `json:"id"`
	Filename                 string     `json:"filename"`
	FileSize                 int64      `json:"file_size"`
	MimeType                 string     `json:"mime_type"`
	Sha256Hash               string     `json:"sha256_hash"`
	StoragePath              string     `json:"storage_path"`
	Status                   string     `json:"status"` // PENDING, PROCESSING, COMPLETED, FAILED
	Classification           string     `json:"classification,omitempty"`
	ClassificationConfidence float64    `json:"classification_confidence,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

// UploadResponse payload
type UploadResponse struct {
	ID              string                 `json:"id"`
	Filename        string                 `json:"filename"`
	FileSize        int64                  `json:"file_size"`
	MimeType        string                 `json:"mime_type"`
	Sha256Hash      string                 `json:"sha256_hash"`
	Classification  string                 `json:"classification"`
	Status          string                 `json:"status"`
	Confidence      float64                `json:"confidence"`
	IsDuplicate     bool                   `json:"is_duplicate"`
	StorageURL      string                 `json:"storage_url,omitempty"`
	ExtractedFields map[string]interface{} `json:"extracted_fields,omitempty"`
	ReconEventID    string                 `json:"recon_event_id,omitempty"`
}

// Pagination metadata
type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

// ListResponse payload
type ListResponse struct {
	Data       []Document `json:"data"`
	Pagination Pagination `json:"pagination"`
}
