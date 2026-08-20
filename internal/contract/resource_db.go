package contract

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"backend-api/internal/database"
)

// ResourceDB handles database operations for the contract resource
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	memory   []RateAgreement
}

// NewResourceDB creates a ResourceDB instance with default rate cards
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	now := time.Now().UTC()
	seedContracts := []RateAgreement{
		{
			ID:                   "CTR-001",
			AgreementID:          "CTR-3PL-LOG-001-2026",
			VendorID:             "3PL-LOG-001",
			VendorName:           "PT Cepat Logistik Nusantara",
			EffectiveFrom:        "2026-01-01",
			EffectiveTo:          "2026-12-31",
			Currency:             "IDR",
			FuelSurchargePercent: 14.0,
			MinimumCharge:        0,
			DisputeWindowDays:    30,
			CreatedAt:            now,
		},
		{
			ID:                   "CTR-002",
			AgreementID:          "CTR-3PL-LOG-002-2026",
			VendorID:             "3PL-LOG-002",
			VendorName:           "PT Trans Express Indonesia",
			EffectiveFrom:        "2026-01-01",
			EffectiveTo:          "2026-12-31",
			Currency:             "IDR",
			FuelSurchargePercent: 12.5,
			MinimumCharge:        0,
			DisputeWindowDays:    45,
			CreatedAt:            now,
		},
		{
			ID:                   "CTR-003",
			AgreementID:          "CTR-3PL-LOG-003-2026",
			VendorID:             "3PL-LOG-003",
			VendorName:           "PT Kargo Andalan Utama",
			EffectiveFrom:        "2026-02-01",
			EffectiveTo:          "2027-01-31",
			Currency:             "IDR",
			FuelSurchargePercent: 15.0,
			MinimumCharge:        0,
			DisputeWindowDays:    30,
			CreatedAt:            now,
		},
		{
			ID:                   "CTR-004",
			AgreementID:          "CTR-3PL-LOG-004-2026",
			VendorID:             "3PL-LOG-004",
			VendorName:           "PT Sinar Logistik Mandiri",
			EffectiveFrom:        "2026-01-01",
			EffectiveTo:          "2026-12-31",
			Currency:             "IDR",
			FuelSurchargePercent: 10.0,
			MinimumCharge:        0,
			DisputeWindowDays:    30,
			CreatedAt:            now,
		},
		{
			ID:                   "CTR-005",
			AgreementID:          "CTR-3PL-LOG-005-2026",
			VendorID:             "3PL-LOG-005",
			VendorName:           "PT Aruna Freight Solusi",
			EffectiveFrom:        "2026-03-01",
			EffectiveTo:          "2027-02-28",
			Currency:             "IDR",
			FuelSurchargePercent: 13.0,
			MinimumCharge:        0,
			DisputeWindowDays:    30,
			CreatedAt:            now,
		},
	}

	return &ResourceDB{
		dbClient: dbClient,
		memory:   seedContracts,
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
