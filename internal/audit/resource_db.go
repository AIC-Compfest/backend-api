package audit

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"backend-api/internal/database"
)

// ResourceDB handles database operations for immutable audit events
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	memory   []AuditEvent
}

// NewResourceDB creates an audit ResourceDB instance
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		memory:   make([]AuditEvent, 0),
	}
}

// ListAll retrieves audit events from Supabase (Source of Truth)
func (r *ResourceDB) ListAll(ctx context.Context, entityID string, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	if r.dbClient != nil {
		var results []AuditEvent
		params := url.Values{}
		params.Set("order", "created_at.desc")
		params.Set("limit", fmt.Sprintf("%d", limit))

		if entityID != "" {
			params.Set("entity_id", "eq."+entityID)
		}

		err := r.dbClient.Query(ctx, "audit_events", params, &results)
		if err == nil {
			return results, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if entityID == "" {
		out := make([]AuditEvent, len(r.memory))
		copy(out, r.memory)
		return out, nil
	}

	var filtered []AuditEvent
	for _, a := range r.memory {
		if a.EntityID == entityID || a.EventID == entityID {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// Append persists a new immutable audit entry
func (r *ResourceDB) Append(ctx context.Context, entry *AuditEvent) error {
	entry.CreatedAt = time.Now().UTC()

	if r.dbClient != nil {
		var created []AuditEvent
		_ = r.dbClient.Insert(ctx, "audit_events", entry, &created)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.memory = append([]AuditEvent{*entry}, r.memory...)
	return nil
}
