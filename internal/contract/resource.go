package contract

import (
	"context"
	"fmt"
)

// Resource handles business logic for rate agreements
type Resource struct {
	db *ResourceDB
}

// NewResource creates a contract Resource instance
func NewResource(db *ResourceDB) *Resource {
	return &Resource{db: db}
}

// ListContracts retrieves and formats all active contracts
func (r *Resource) ListContracts(ctx context.Context) ([]ContractSummaryDTO, error) {
	agreements, err := r.db.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	dtoList := make([]ContractSummaryDTO, 0, len(agreements))
	for _, a := range agreements {
		dtoList = append(dtoList, ContractSummaryDTO{
			ID:               a.ID,
			AgreementID:      a.AgreementID,
			VendorID:         a.VendorID,
			VendorName:       a.VendorName,
			EffectiveFrom:    a.EffectiveFrom,
			EffectiveTo:      a.EffectiveTo,
			EffectiveRange:   fmt.Sprintf("%s s/d %s", a.EffectiveFrom, a.EffectiveTo),
			FuelSurchargeCap: fmt.Sprintf("%.1f%%", a.FuelSurchargePercent),
			DisputeWindow:    fmt.Sprintf("%d Hari", a.DisputeWindowDays),
			MinimumCharge:    a.MinimumCharge,
			Status:           "ACTIVE",
		})
	}

	return dtoList, nil
}

// GetContract retrieves a single contract by ID
func (r *Resource) GetContract(ctx context.Context, id string) (*RateAgreement, error) {
	return r.db.GetByID(ctx, id)
}
