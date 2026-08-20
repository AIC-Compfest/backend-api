package audit

import "time"

// AuditEvent represents an immutable audit log record in Supabase
type AuditEvent struct {
	ID          string                 `json:"id"`
	EventID     string                 `json:"event_id,omitempty"`
	ActorID     string                 `json:"actor_id"`
	Action      string                 `json:"action"`
	EntityType  string                 `json:"entity_type"`
	EntityID    string                 `json:"entity_id"`
	BeforeState map[string]interface{} `json:"before_state,omitempty"`
	AfterState  map[string]interface{} `json:"after_state,omitempty"`
	Reason      string                 `json:"reason,omitempty"`
	RequestID   string                 `json:"request_id,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// AuditListResponse payload
type AuditListResponse struct {
	Data  []AuditEvent `json:"data"`
	Total int          `json:"total"`
}
