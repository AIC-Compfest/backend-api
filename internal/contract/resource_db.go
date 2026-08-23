package contract

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"backend-api/internal/database"
)


// ResourceDB handles database operations for the contract resource
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	memory   []RateAgreement
}

// NewResourceDB creates a ResourceDB instance
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		memory:   []RateAgreement{},
	}
}


// ListAll retrieves all rate agreements from Supabase DB (Source of Truth)
func (r *ResourceDB) ListAll(ctx context.Context) ([]RateAgreement, error) {
	if r.dbClient != nil {
		var results []RateAgreement
		params := url.Values{}
		params.Set("order", "vendor_id.asc")
		err := r.dbClient.Query(ctx, "rate_agreements", params, &results)
		if err == nil {
			return results, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.memory, nil
}

// GetByID finds a rate agreement by ID or vendor ID from Supabase
func (r *ResourceDB) GetByID(ctx context.Context, id string) (*RateAgreement, error) {
	if r.dbClient != nil {
		var results []RateAgreement
		params := url.Values{}
		params.Set("or", fmt.Sprintf("(id.eq.%s,agreement_id.eq.%s,vendor_id.eq.%s)", id, id, id))
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "rate_agreements", params, &results)
		if err == nil {
			if len(results) > 0 {
				return &results[0], nil
			}
			return nil, fmt.Errorf("rate agreement '%s' not found", id)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.memory {
		if c.ID == id || c.AgreementID == id || c.VendorID == id {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("rate agreement '%s' not found", id)
}

// Insert saves a new rate agreement into Supabase DB and in-memory cache
func (r *ResourceDB) Insert(ctx context.Context, ctr *RateAgreement) error {
	if r.dbClient != nil {
		var out []RateAgreement
		_ = r.dbClient.Insert(ctx, "rate_agreements", ctr, &out)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.memory = append(r.memory, *ctr)
	return nil
}


