package document

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"backend-api/internal/database"
	"backend-api/internal/model"
)

// ResourceDB handles database operations for documents and pages
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	memory   map[string]*Document
	byHash   map[string]string
	order    []string
	pages    map[string][]DocumentPage    // Key: docID -> pages
	segments map[string][]DocumentSegment // Key: docID -> segments
}

// NewResourceDB creates a new document ResourceDB instance
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		memory:   make(map[string]*Document),
		byHash:   make(map[string]string),
		order:    make([]string, 0),
		pages:    make(map[string][]DocumentPage),
		segments: make(map[string][]DocumentSegment),
	}
}

// FindBySHA256 searches for an existing document by its SHA-256 checksum in Supabase
func (r *ResourceDB) FindBySHA256(ctx context.Context, hash string) (*Document, error) {
	if r.dbClient != nil {
		var results []Document
		params := url.Values{}
		params.Set("sha256_hash", "eq."+hash)
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "documents", params, &results)
		if err == nil {
			if len(results) > 0 {
				return &results[0], nil
			}
			return nil, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	id, exists := r.byHash[hash]
	if !exists {
		return nil, nil
	}
	return r.memory[id], nil
}

// FindByID retrieves a document by its UUID from Supabase
func (r *ResourceDB) FindByID(ctx context.Context, id string) (*Document, error) {
	if r.dbClient != nil {
		var results []Document
		params := url.Values{}
		params.Set("id", "eq."+id)
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "documents", params, &results)
		if err == nil {
			if len(results) > 0 {
				return &results[0], nil
			}
			return nil, fmt.Errorf("document '%s' not found", id)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	doc, exists := r.memory[id]
	if !exists {
		return nil, fmt.Errorf("document '%s' not found", id)
	}
	return doc, nil
}

// Create inserts a new document record into Supabase DB and local cache
func (r *ResourceDB) Create(ctx context.Context, doc *Document) error {
	if r.dbClient != nil {
		var created []Document
		err := r.dbClient.Insert(ctx, "documents", doc, &created)
		if err == nil && len(created) > 0 {
			*doc = created[0]
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.memory[doc.ID] = doc
	r.byHash[doc.Sha256Hash] = doc.ID
	r.order = append([]string{doc.ID}, r.order...)
	return nil
}

// UpdateStatus updates the processing status and classification of a document
func (r *ResourceDB) UpdateStatus(ctx context.Context, id string, status string, classification string, confidence float64) error {
	now := time.Now().UTC()
	payload := map[string]interface{}{
		"status":                    status,
		"classification":            classification,
		"classification_confidence": confidence,
		"updated_at":                now,
	}

	if r.dbClient != nil {
		var updated []Document
		params := url.Values{}
		params.Set("id", "eq."+id)
		_ = r.dbClient.Update(ctx, "documents", params, payload, &updated)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if doc, exists := r.memory[id]; exists {
		doc.Status = status
		doc.Classification = classification
		doc.ClassificationConfidence = confidence
		doc.UpdatedAt = &now
	}

	return nil
}

// List retrieves paginated documents from Supabase DB or cache
func (r *ResourceDB) List(ctx context.Context, page, pageSize int, statusFilter string) ([]Document, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	if r.dbClient != nil {
		params := url.Values{}
		params.Set("order", "created_at.desc")
		params.Set("limit", fmt.Sprintf("%d", pageSize))
		params.Set("offset", fmt.Sprintf("%d", offset))

		if statusFilter != "" {
			params.Set("status", "eq."+statusFilter)
		}

		var docs []Document
		err := r.dbClient.Query(ctx, "documents", params, &docs)
		if err == nil {
			countParams := url.Values{}
			if statusFilter != "" {
				countParams.Set("status", "eq."+statusFilter)
			}
			total, _ := r.dbClient.Count(ctx, "documents", countParams)
			if total == 0 && len(docs) > 0 {
				total = int64(len(docs))
			}
			return docs, total, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []Document
	for _, id := range r.order {
		doc := r.memory[id]
		if doc != nil {
			if statusFilter == "" || doc.Status == statusFilter {
				filtered = append(filtered, *doc)
			}
		}
	}

	total := int64(len(filtered))
	if offset >= len(filtered) {
		return []Document{}, total, nil
	}

	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[offset:end], total, nil
}

// InsertPage persists an individual page record into document_pages table in Supabase
func (r *ResourceDB) InsertPage(ctx context.Context, p *DocumentPage) error {
	if p.ID == "" {
		p.ID = model.NewUUID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}

	if r.dbClient != nil {
		var created []DocumentPage
		_ = r.dbClient.Insert(ctx, "document_pages", p, &created)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.pages[p.DocumentID] = append(r.pages[p.DocumentID], *p)
	return nil
}

// ListPages returns all pages associated with a document
func (r *ResourceDB) ListPages(ctx context.Context, docID string) ([]DocumentPage, error) {
	if r.dbClient != nil {
		var pages []DocumentPage
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("order", "page_number.asc")
		if err := r.dbClient.Query(ctx, "document_pages", params, &pages); err == nil && len(pages) > 0 {
			return pages, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if pages, exists := r.pages[docID]; exists {
		return pages, nil
	}
	return []DocumentPage{}, nil
}

// GetPage retrieves a specific page by document ID and page number
func (r *ResourceDB) GetPage(ctx context.Context, docID string, pageNumber int) (*DocumentPage, error) {
	if r.dbClient != nil {
		var pages []DocumentPage
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("page_number", fmt.Sprintf("eq.%d", pageNumber))
		params.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "document_pages", params, &pages); err == nil && len(pages) > 0 {
			return &pages[0], nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if pages, exists := r.pages[docID]; exists {
		for _, p := range pages {
			if p.PageNumber == pageNumber {
				return &p, nil
			}
		}
	}
	return nil, fmt.Errorf("page %d of document '%s' not found", pageNumber, docID)
}

// UpdatePageStatus updates the processing status and error message of a document page
func (r *ResourceDB) UpdatePageStatus(ctx context.Context, docID string, pageNumber int, status string, errMsg string) error {
	now := time.Now().UTC()
	if r.dbClient != nil {
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("page_number", fmt.Sprintf("eq.%d", pageNumber))
		payload := map[string]interface{}{
			"status":        status,
			"error_message": errMsg,
			"updated_at":    now,
		}
		var updated []DocumentPage
		_ = r.dbClient.Update(ctx, "document_pages", params, payload, &updated)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if pages, exists := r.pages[docID]; exists {
		for i := range pages {
			if pages[i].PageNumber == pageNumber {
				pages[i].Status = status
				pages[i].ErrorMessage = errMsg
				pages[i].UpdatedAt = &now
				break
			}
		}
	}
	return nil
}

// InsertExtraction saves extracted bounding boxes and OCR fields into document_extractions table
func (r *ResourceDB) InsertExtraction(ctx context.Context, docID string, extractedJSON map[string]interface{}, confidence float64) error {
	if r.dbClient != nil {
		payload := map[string]interface{}{
			"id":                 model.NewUUID(),
			"document_id":        docID,
			"schema_version":     "0.1.0",
			"extracted_json":     extractedJSON,
			"overall_confidence": confidence,
			"extraction_method":  "ocr_layout_ai",
			"created_at":         time.Now().UTC(),
		}
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "document_extractions", payload, &out)
	}
	return nil
}

// InsertInvoice saves structured invoice data into invoices table in Supabase
func (r *ResourceDB) InsertInvoice(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "invoices", payload, &out)
	}
	return nil
}

// InsertSuratJalan saves structured Surat Jalan data into surat_jalans table in Supabase
func (r *ResourceDB) InsertSuratJalan(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "surat_jalans", payload, &out)
	}
	return nil
}

// InsertPOD saves structured Proof of Delivery data into pods table in Supabase
func (r *ResourceDB) InsertPOD(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "pods", payload, &out)
	}
	return nil
}

// InsertRateAgreement saves structured Rate Agreement data into rate_agreements table in Supabase
func (r *ResourceDB) InsertRateAgreement(ctx context.Context, payload map[string]interface{}) error {
	if r.dbClient != nil {
		var out []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "rate_agreements", payload, &out)
	}
	return nil
}


// InsertSegment saves a document segment record into Supabase DB and local cache
func (r *ResourceDB) InsertSegment(ctx context.Context, seg *DocumentSegment) error {
	if seg.ID == "" {
		seg.ID = model.NewUUID()
	}
	if seg.CreatedAt.IsZero() {
		seg.CreatedAt = time.Now().UTC()
	}
	if seg.PageCount == 0 && seg.EndPage >= seg.StartPage {
		seg.PageCount = seg.EndPage - seg.StartPage + 1
	}

	if r.dbClient != nil {
		// Note: page_count is a GENERATED ALWAYS column in PostgreSQL, so we omit it from the INSERT payload
		payload := map[string]interface{}{
			"id":                        seg.ID,
			"document_id":               seg.DocumentID,
			"segment_number":            seg.SegmentNumber,
			"document_type":             seg.DocumentType,
			"start_page":                seg.StartPage,
			"end_page":                  seg.EndPage,
			"classification_confidence": seg.ClassificationConfidence,
			"classification_method":     seg.ClassificationMethod,
			"extraction_status":         seg.ExtractionStatus,
			"created_at":                seg.CreatedAt,
			"updated_at":                seg.CreatedAt,
		}
		if seg.DomainEntityType != "" {
			payload["domain_entity_type"] = seg.DomainEntityType
		}
		if seg.DomainEntityID != "" {
			payload["domain_entity_id"] = seg.DomainEntityID
		}
		if seg.ErrorMessage != "" {
			payload["error_message"] = seg.ErrorMessage
		}
		var created []DocumentSegment
		_ = r.dbClient.Insert(ctx, "document_segments", payload, &created)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.segments[seg.DocumentID] = append(r.segments[seg.DocumentID], *seg)
	return nil
}


// ListSegments returns all segments associated with a document ordered by segment_number
func (r *ResourceDB) ListSegments(ctx context.Context, docID string) ([]DocumentSegment, error) {
	if r.dbClient != nil {
		var segs []DocumentSegment
		params := url.Values{}
		params.Set("document_id", "eq."+docID)
		params.Set("order", "segment_number.asc")
		if err := r.dbClient.Query(ctx, "document_segments", params, &segs); err == nil && len(segs) > 0 {
			return segs, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if segs, exists := r.segments[docID]; exists {
		return segs, nil
	}
	return []DocumentSegment{}, nil
}
