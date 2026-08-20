package settings

import (
	"context"
	"fmt"
)

// Resource handles business logic for settings
type Resource struct {
	db *ResourceDB
}

// NewResource creates a settings Resource instance
func NewResource(db *ResourceDB) *Resource {
	return &Resource{db: db}
}

// GetSettings retrieves current policy settings
func (r *Resource) GetSettings(ctx context.Context) (*PolicySettings, error) {
	return r.db.Get(ctx)
}

// UpdateSettings validates and updates policy settings
func (r *Resource) UpdateSettings(ctx context.Context, req UpdateSettingsRequest, updatedBy string) (*PolicySettings, error) {
	current, err := r.db.Get(ctx)
	if err != nil {
		return nil, err
	}

	if req.MaterialityThresholdIDR != nil {
		if *req.MaterialityThresholdIDR < 0 {
			return nil, fmt.Errorf("materiality threshold cannot be negative")
		}
		current.MaterialityThresholdIDR = *req.MaterialityThresholdIDR
	}

	if req.MaterialityPercentage != nil {
		if *req.MaterialityPercentage < 0 || *req.MaterialityPercentage > 100 {
			return nil, fmt.Errorf("materiality percentage must be between 0 and 100")
		}
		current.MaterialityPercentage = *req.MaterialityPercentage
	}

	if req.WeightToleranceKg != nil {
		if *req.WeightToleranceKg < 0 {
			return nil, fmt.Errorf("weight tolerance cannot be negative")
		}
		current.WeightToleranceKg = *req.WeightToleranceKg
	}

	if req.DisputeAlertWindowDays != nil {
		if *req.DisputeAlertWindowDays < 1 {
			return nil, fmt.Errorf("dispute alert window must be at least 1 day")
		}
		current.DisputeAlertWindowDays = *req.DisputeAlertWindowDays
	}

	if req.AutoApproveCleanMatch != nil {
		current.AutoApproveCleanMatch = *req.AutoApproveCleanMatch
	}

	if req.EnableStrictZoneMatching != nil {
		current.EnableStrictZoneMatching = *req.EnableStrictZoneMatching
	}

	if updatedBy != "" {
		current.UpdatedBy = updatedBy
	}

	if err := r.db.Update(ctx, current); err != nil {
		return nil, err
	}

	return current, nil
}
