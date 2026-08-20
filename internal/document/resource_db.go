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

// ResourceDB handles database operations for documents
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	memory   map[string]*Document
	byHash   map[string]string
	order    []string
}

// NewResourceDB creates a new document ResourceDB instance
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		memory:   make(map[string]*Document),
		byHash:   make(map[string]string),
		order:    make([]string, 0),
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
