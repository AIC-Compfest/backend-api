package model

import "time"

// DocumentSegment represents a logical document within a physical uploaded file.
// Architecture principle: Physical File ≠ Logical Document.
//
// One physical PDF (Document) can contain N logical documents (DocumentSegment):
//   - 10-page PDF → [Invoice:p1-2, SuratJalan:p3, POD:p4-5, RateAgreement:p6-8, Invoice:p9-10]
//   - Single-page Invoice PDF → [Invoice:p1-1]
//
// Downstream pipeline operates on segments, not documents.
// This ensures identical business logic for both multi-doc PDFs and single-file uploads.
type DocumentSegment struct {
	ID                      string    `json:"id"`
	DocumentID              string    `json:"document_id"`
	SegmentNumber           int       `json:"segment_number"` // 1-indexed, unique within document
	DocumentType            string    `json:"document_type"`  // INVOICE | SURAT_JALAN | POD | RATE_AGREEMENT | UNKNOWN
	StartPage               int       `json:"start_page"`
	EndPage                 int       `json:"end_page"`
	PageCount               int       `json:"page_count"` // computed: end_page - start_page + 1
	ClassificationConfidence float64  `json:"classification_confidence"`
	ClassificationMethod    string    `json:"classification_method"`
	ExtractionStatus        string    `json:"extraction_status"` // PENDING | EXTRACTING | EXTRACTED | FAILED | REQUIRES_HUMAN_REVIEW
	DomainEntityType        string    `json:"domain_entity_type,omitempty"` // invoice | surat_jalan | pod | rate_agreement
	DomainEntityID          string    `json:"domain_entity_id,omitempty"`
	ErrorMessage            string    `json:"error_message,omitempty"`
	RetryCount              int       `json:"retry_count"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// Segment extraction status constants
const (
	SegmentStatusPending            = "PENDING"
	SegmentStatusExtracting         = "EXTRACTING"
	SegmentStatusExtracted          = "EXTRACTED"
	SegmentStatusFailed             = "FAILED"
	SegmentStatusRequiresHumanReview = "REQUIRES_HUMAN_REVIEW"
)

// Domain entity type constants (maps to DB table name)
const (
	DomainEntityInvoice       = "invoice"
	DomainEntitySuratJalan    = "surat_jalan"
	DomainEntityPOD           = "pod"
	DomainEntityRateAgreement = "rate_agreement"
)

// DocumentWithSegments is the response when a document is fetched with its segments.
// This replaces the old pattern of returning document.classification.
type DocumentWithSegments struct {
	ID          string            `json:"id"`
	Filename    string            `json:"filename"`
	FileSize    int64             `json:"file_size"`
	MimeType    string            `json:"mime_type"`
	SHA256Hash  string            `json:"sha256_hash"`
	StoragePath string            `json:"storage_path"`
	Status      string            `json:"status"`
	PageCount   int               `json:"page_count"`
	Segments    []DocumentSegment `json:"segments"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// SegmentPageRange is a lightweight struct for page range operations.
type SegmentPageRange struct {
	SegmentID    string `json:"segment_id"`
	SegmentNumber int   `json:"segment_number"`
	DocumentType string `json:"document_type"`
	StartPage    int    `json:"start_page"`
	EndPage      int    `json:"end_page"`
}

// ValidateSegment checks that a DocumentSegment has consistent fields.
// Returns a list of validation errors (empty = valid).
func (s *DocumentSegment) Validate() []string {
	var errs []string
	if s.DocumentID == "" {
		errs = append(errs, "document_id is required")
	}
	if s.SegmentNumber < 1 {
		errs = append(errs, "segment_number must be >= 1")
	}
	if s.StartPage < 1 {
		errs = append(errs, "start_page must be >= 1")
	}
	if s.EndPage < s.StartPage {
		errs = append(errs, "end_page must be >= start_page")
	}
	validTypes := map[string]bool{
		ClassInvoice: true, ClassSuratJalan: true,
		ClassPOD: true, ClassRateAgreement: true, ClassUnknown: true,
	}
	if !validTypes[s.DocumentType] {
		errs = append(errs, "document_type must be one of: INVOICE, SURAT_JALAN, POD, RATE_AGREEMENT, UNKNOWN")
	}
	return errs
}

// PageCount computed from start/end pages.
func (s *DocumentSegment) ComputedPageCount() int {
	if s.EndPage < s.StartPage {
		return 0
	}
	return s.EndPage - s.StartPage + 1
}
