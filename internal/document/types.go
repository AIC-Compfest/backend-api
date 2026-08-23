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
	PageCount                int        `json:"page_count,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

// DocumentPage represents an individual page record within a multi-page document
type DocumentPage struct {
	ID                       string     `json:"id"`
	DocumentID               string     `json:"document_id"`
	PageNumber               int        `json:"page_number"`
	StoragePath              string     `json:"storage_path,omitempty"`
	PreprocessedStoragePath  string     `json:"preprocessed_storage_path,omitempty"`
	Status                   string     `json:"status"` // PENDING, PREPROCESSED, CLASSIFIED, OCR_COMPLETED, EXTRACTED, FAILED
	Classification           string     `json:"classification,omitempty"`
	ClassificationConfidence float64    `json:"classification_confidence,omitempty"`
	OCREngine                string     `json:"ocr_engine,omitempty"`
	ClassifierModel          string     `json:"classifier_model,omitempty"`
	LineCount                int        `json:"line_count,omitempty"`
	SignaturePresent         bool       `json:"signature_present,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	RetryCount               int        `json:"retry_count"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

// DocumentSegment represents a logical document boundary within a multi-page physical file
type DocumentSegment struct {
	ID                       string     `json:"id"`
	DocumentID               string     `json:"document_id"`
	SegmentNumber            int        `json:"segment_number"`
	DocumentType             string     `json:"document_type"`
	StartPage                int        `json:"start_page"`
	EndPage                  int        `json:"end_page"`
	PageCount                int        `json:"page_count"`
	ClassificationConfidence float64    `json:"classification_confidence"`
	ClassificationMethod     string     `json:"classification_method"`
	ExtractionStatus         string     `json:"extraction_status"`
	DomainEntityType         string     `json:"domain_entity_type,omitempty"`
	DomainEntityID           string     `json:"domain_entity_id,omitempty"`
	ErrorMessage             string     `json:"error_message,omitempty"`
	RequiresHumanReview      bool       `json:"requires_human_review,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                *time.Time `json:"updated_at,omitempty"`
}

// SegmentsResponse payload for GET /api/v1/documents/:id/segments
type SegmentsResponse struct {
	DocumentID string            `json:"document_id"`
	Count      int               `json:"count"`
	Segments   []DocumentSegment `json:"segments"`
}

// PipelineStatusResponse provides real-time progress for multi-page processing
type PipelineStatusResponse struct {
	DocumentID     string            `json:"document_id"`
	Status         string            `json:"status"`
	Filename       string            `json:"filename"`
	PageCount      int               `json:"page_count"`
	ProcessedPages int               `json:"processed_pages"`
	FailedPages    int               `json:"failed_pages"`
	Classification string            `json:"classification"`
	Confidence     float64           `json:"confidence"`
	Pages          []DocumentPage    `json:"pages"`
	Segments       []DocumentSegment `json:"segments,omitempty"`
	ReconEventID   string            `json:"recon_event_id,omitempty"`
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
	PageCount       int                    `json:"page_count,omitempty"`
	StorageURL      string                 `json:"storage_url,omitempty"`
	SegmentsURL     string                 `json:"segments_url,omitempty"`
	Segments        []DocumentSegment      `json:"segments,omitempty"`
	ExtractedFields map[string]interface{} `json:"extracted_fields,omitempty"`
	Pages           []DocumentPage         `json:"pages,omitempty"`
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

