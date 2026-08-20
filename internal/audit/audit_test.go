package audit

import (
	"context"
	"testing"
)

func TestAuditLogPersistence(t *testing.T) {
	db := NewResourceDB(nil)
	res := NewResource(db)
	ctx := context.Background()

	// 1. Append audit log
	entry := AuditEvent{
		ID:         "audit-001",
		ActorID:    "ap.manager@enterprise.com",
		Action:     "APPROVE",
		EntityType: "RECONCILIATION_CASE",
		EntityID:   "EVT-TXN-000001",
		Reason:     "Clean match verified against physical POD",
	}

	if err := db.Append(ctx, &entry); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// 2. Query audit logs
	logs, err := res.ListAuditLogs(ctx, "EVT-TXN-000001", 10)
	if err != nil {
		t.Fatalf("ListAuditLogs failed: %v", err)
	}

	if logs.Total == 0 {
		t.Errorf("Expected at least 1 audit record")
	}
	if logs.Data[0].Action != "APPROVE" {
		t.Errorf("Expected action 'APPROVE', got '%s'", logs.Data[0].Action)
	}
}
