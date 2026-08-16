package repository

import (
	"context"
	"sync"
	"time"

	"github.com/AIC-Compfest/backend-api/internal/model"
)

// DocumentRepository defines database operations for documents.
type DocumentRepository interface {
	FindBySHA256(ctx context.Context, hash string) (*model.Document, error)
	FindByID(ctx context.Context, id string) (*model.Document, error)
	Create(ctx context.Context, doc *model.Document) error
	UpdateStatus(ctx context.Context, id string, status string, classification string, confidence float64) error
	List(ctx context.Context, limit, offset int) ([]*model.Document, int64, error)
}

// MemoryDocumentRepository provides an in-memory thread-safe implementation
// used during early development and testing, with zero external database dependencies.
type MemoryDocumentRepository struct {
	mu        sync.RWMutex
	documents map[string]*model.Document
	byHash    map[string]string
	order     []string
}

// NewMemoryDocumentRepository creates a MemoryDocumentRepository instance.
func NewMemoryDocumentRepository() *MemoryDocumentRepository {
	return &MemoryDocumentRepository{
		documents: make(map[string]*model.Document),
		byHash:    make(map[string]string),
		order:     make([]string, 0),
	}
}

func (r *MemoryDocumentRepository) FindBySHA256(ctx context.Context, hash string) (*model.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, exists := r.byHash[hash]
	if !exists {
		return nil, nil
	}
	doc, exists := r.documents[id]
	if !exists {
		return nil, nil
	}
	return doc, nil
}

func (r *MemoryDocumentRepository) FindByID(ctx context.Context, id string) (*model.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	doc, exists := r.documents[id]
	if !exists {
		return nil, nil
	}
	return doc, nil
}

func (r *MemoryDocumentRepository) Create(ctx context.Context, doc *model.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if doc.ID == "" {
		doc.ID = model.NewUUID()
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now().UTC()
	}
	doc.UpdatedAt = doc.CreatedAt

	r.documents[doc.ID] = doc
	r.byHash[doc.SHA256Hash] = doc.ID
	r.order = append(r.order, doc.ID)
	return nil
}

func (r *MemoryDocumentRepository) UpdateStatus(ctx context.Context, id string, status string, classification string, confidence float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	doc, exists := r.documents[id]
	if !exists {
		return nil
	}
	doc.Status = status
	if classification != "" {
		doc.Classification = classification
		doc.ClassificationConfidence = confidence
	}
	doc.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *MemoryDocumentRepository) List(ctx context.Context, limit, offset int) ([]*model.Document, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := int64(len(r.order))
	if offset >= len(r.order) {
		return []*model.Document{}, total, nil
	}

	end := offset + limit
	if end > len(r.order) {
		end = len(r.order)
	}

	result := make([]*model.Document, 0, end-offset)
	// Iterate in reverse chronological order (newest first)
	for i := len(r.order) - 1 - offset; i >= 0 && len(result) < limit; i-- {
		id := r.order[i]
		result = append(result, r.documents[id])
	}

	return result, total, nil
}
