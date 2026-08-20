package settings

import (
	"context"
	"net/url"
	"sync"
	"time"

	"backend-api/internal/database"
)

// ResourceDB handles database operations for the settings resource
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	current  PolicySettings
}

// NewResourceDB creates a ResourceDB instance
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		current: PolicySettings{
			ID:                       "default-policy",
			MaterialityThresholdIDR:  50000.0,
			MaterialityPercentage:    2.0,
			WeightToleranceKg:        0.5,
			DisputeAlertWindowDays:   7,
			AutoApproveCleanMatch:    true,
			EnableStrictZoneMatching: true,
			UpdatedAt:                time.Now().UTC(),
			UpdatedBy:                "system",
		},
	}
}

// Get retrieves active policy settings
func (r *ResourceDB) Get(ctx context.Context) (*PolicySettings, error) {
	if r.dbClient != nil {
		var results []PolicySettings
		params := url.Values{}
		params.Set("limit", "1")
		err := r.dbClient.Query(ctx, "pricing_rules", params, &results)
		if err == nil && len(results) > 0 {
			return &results[0], nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	res := r.current
	return &res, nil
}

// Update updates policy settings
func (r *ResourceDB) Update(ctx context.Context, settings *PolicySettings) error {
	settings.UpdatedAt = time.Now().UTC()

	if r.dbClient != nil {
		var updated []PolicySettings
		params := url.Values{}
		params.Set("id", "eq."+settings.ID)
		_ = r.dbClient.Update(ctx, "pricing_rules", params, settings, &updated)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.current = *settings
	return nil
}
