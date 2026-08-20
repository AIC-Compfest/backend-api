package settings

import (
	"context"
	"testing"
)

func TestSettingsManagement(t *testing.T) {
	db := NewResourceDB(nil)
	res := NewResource(db)
	ctx := context.Background()

	// 1. Get default settings
	current, err := res.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if current.MaterialityThresholdIDR <= 0 {
		t.Errorf("Expected positive materiality threshold, got %f", current.MaterialityThresholdIDR)
	}

	// 2. Update settings
	newThreshold := 75000.0
	newPct := 2.5
	updateReq := UpdateSettingsRequest{
		MaterialityThresholdIDR: &newThreshold,
		MaterialityPercentage:   &newPct,
	}

	updated, err := res.UpdateSettings(ctx, updateReq, "controller@enterprise.com")
	if err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	if updated.MaterialityThresholdIDR != 75000.0 {
		t.Errorf("Expected 75000.0, got %f", updated.MaterialityThresholdIDR)
	}
	if updated.UpdatedBy != "controller@enterprise.com" {
		t.Errorf("Expected updatedBy 'controller@enterprise.com', got '%s'", updated.UpdatedBy)
	}

	// 3. Adversarial: Negative threshold
	badThreshold := -100.0
	_, err = res.UpdateSettings(ctx, UpdateSettingsRequest{MaterialityThresholdIDR: &badThreshold}, "attacker")
	if err == nil {
		t.Errorf("Expected error on negative threshold, got nil")
	}
}
