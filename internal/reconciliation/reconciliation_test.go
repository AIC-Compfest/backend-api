package reconciliation

import (
	"context"
	"testing"
)

func TestReconciliationResourceWorkflow(t *testing.T) {
	db := NewResourceDB(nil)
	_ = db.SeedFromPilotDataset("../../synthetic-data/dataset/v4_pilot_20")
	_ = db.SeedFromPilotDataset("d:/AIC/synthetic-data/dataset/v4_pilot_20")
	res := NewResource(db, nil)
	ctx := context.Background()

	// 1. Get queue
	queueResp, err := res.GetQueue(ctx, QueueFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("GetQueue failed: %v", err)
	}

	if queueResp.Total == 0 {
		t.Fatalf("Expected non-empty queue items")
	}

	first := queueResp.Items[0]

	// 2. Submit Decision: APPROVE
	decResp, err := res.SubmitDecision(ctx, first.EventID, DecisionRequest{
		Decision:   "APPROVE",
		Reason:     "Automated approval verified by test suite",
		ReviewerID: "ap.manager@enterprise.com",
		Role:       "AP_MANAGER",
	})
	if err != nil {
		t.Fatalf("SubmitDecision failed: %v", err)
	}

	if decResp.NewStatus != "APPROVED" {
		t.Errorf("Expected new status 'APPROVED', got '%s'", decResp.NewStatus)
	}

	// 3. Get Dispute Package
	disputePkg, err := res.GetDisputePackage(ctx, first.EventID, "AP Manager")
	if err != nil {
		t.Fatalf("GetDisputePackage failed: %v", err)
	}

	if disputePkg.EventID != first.EventID {
		t.Errorf("Expected event ID '%s', got '%s'", first.EventID, disputePkg.EventID)
	}
	if disputePkg.FormattedMemo == "" {
		t.Errorf("Expected non-empty formatted dispute memo")
	}

	// 4. Adversarial: Empty reason on DISPUTE
	_, err = res.SubmitDecision(ctx, first.EventID, DecisionRequest{
		Decision: "DISPUTE",
		Reason:   "",
	})
	if err == nil {
		t.Errorf("Expected error when reason is empty on DISPUTE, got nil")
	}

	// 5. Adversarial: Invalid Role
	_, err = res.SubmitDecision(ctx, first.EventID, DecisionRequest{
		Decision: "APPROVE",
		Role:     "HACKER_ROLE",
	})
	if err == nil {
		t.Errorf("Expected error on invalid role, got nil")
	}
}
