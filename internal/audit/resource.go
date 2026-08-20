package audit

import (
	"context"
)

// Resource handles audit trail retrieval
type Resource struct {
	db *ResourceDB
}

// NewResource creates an audit Resource instance
func NewResource(db *ResourceDB) *Resource {
	return &Resource{db: db}
}

// ListAuditLogs retrieves audit logs
func (r *Resource) ListAuditLogs(ctx context.Context, entityID string, limit int) (*AuditListResponse, error) {
	events, err := r.db.ListAll(ctx, entityID, limit)
	if err != nil {
		return nil, err
	}

	return &AuditListResponse{
		Data:  events,
		Total: len(events),
	}, nil
}
