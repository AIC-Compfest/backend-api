package repository

import (
	"context"
	"sync"
	"time"

	"backend-api/internal/model"
)

// SegmentRepository defines database operations for document segments.
type SegmentRepository interface {
	// Create inserts a new segment. Returns error if document_id + segment_number conflicts.
	Create(ctx context.Context, seg *model.DocumentSegment) error

	// CreateBatch inserts multiple segments for a document in a single operation.
	// On conflict (same document_id + segment_number), skips without error.
	CreateBatch(ctx context.Context, segs []*model.DocumentSegment) error

	// FindByDocumentID returns all segments for a given document, ordered by segment_number.
	FindByDocumentID(ctx context.Context, documentID string) ([]*model.DocumentSegment, error)

	// FindByID returns a single segment by its UUID.
	FindByID(ctx context.Context, id string) (*model.DocumentSegment, error)

	// UpdateExtractionStatus updates the extraction lifecycle status of a segment.
	UpdateExtractionStatus(ctx context.Context, id string, status string, errMsg string) error

	// UpdateDomainEntity links a segment to its extracted domain record.
	UpdateDomainEntity(ctx context.Context, id string, entityType string, entityID string) error

	// DeleteByDocumentID removes all segments for a document (used for reprocessing).
	DeleteByDocumentID(ctx context.Context, documentID string) error
}

// MemorySegmentRepository provides an in-memory thread-safe implementation
// for development and testing without external database dependencies.
type MemorySegmentRepository struct {
	mu       sync.RWMutex
	segments map[string]*model.DocumentSegment // key: id
	byDocID  map[string][]string               // key: document_id → []segment_id (ordered)
}

// NewMemorySegmentRepository creates a MemorySegmentRepository instance.
func NewMemorySegmentRepository() *MemorySegmentRepository {
	return &MemorySegmentRepository{
		segments: make(map[string]*model.DocumentSegment),
		byDocID:  make(map[string][]string),
	}
}

func (r *MemorySegmentRepository) Create(ctx context.Context, seg *model.DocumentSegment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if seg.ID == "" {
		seg.ID = model.NewUUID()
	}
	now := time.Now().UTC()
	if seg.CreatedAt.IsZero() {
		seg.CreatedAt = now
	}
	seg.UpdatedAt = now
	seg.PageCount = seg.ComputedPageCount()

	r.segments[seg.ID] = seg
	r.byDocID[seg.DocumentID] = append(r.byDocID[seg.DocumentID], seg.ID)
	return nil
}

func (r *MemorySegmentRepository) CreateBatch(ctx context.Context, segs []*model.DocumentSegment) error {
	for _, s := range segs {
		if err := r.Create(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (r *MemorySegmentRepository) FindByDocumentID(ctx context.Context, documentID string) ([]*model.DocumentSegment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids, exists := r.byDocID[documentID]
	if !exists {
		return []*model.DocumentSegment{}, nil
	}

	result := make([]*model.DocumentSegment, 0, len(ids))
	for _, id := range ids {
		if seg, ok := r.segments[id]; ok {
			result = append(result, seg)
		}
	}

	// Sort by segment_number (insertion order preserves order in memory)
	return result, nil
}

func (r *MemorySegmentRepository) FindByID(ctx context.Context, id string) (*model.DocumentSegment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seg, exists := r.segments[id]
	if !exists {
		return nil, nil
	}
	return seg, nil
}

func (r *MemorySegmentRepository) UpdateExtractionStatus(ctx context.Context, id string, status string, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	seg, exists := r.segments[id]
	if !exists {
		return nil
	}
	seg.ExtractionStatus = status
	if errMsg != "" {
		seg.ErrorMessage = errMsg
	}
	seg.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *MemorySegmentRepository) UpdateDomainEntity(ctx context.Context, id string, entityType string, entityID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	seg, exists := r.segments[id]
	if !exists {
		return nil
	}
	seg.DomainEntityType = entityType
	seg.DomainEntityID = entityID
	seg.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *MemorySegmentRepository) DeleteByDocumentID(ctx context.Context, documentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids, exists := r.byDocID[documentID]
	if !exists {
		return nil
	}
	for _, id := range ids {
		delete(r.segments, id)
	}
	delete(r.byDocID, documentID)
	return nil
}
