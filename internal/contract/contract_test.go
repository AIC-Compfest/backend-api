package contract

import (
	"context"
	"testing"
)

func TestContractListingAndRetrieval(t *testing.T) {
	db := NewResourceDB(nil)
	res := NewResource(db)
	ctx := context.Background()

	// 1. Insert a test contract
	err := db.Insert(ctx, &RateAgreement{
		ID:          "CTR-001",
		AgreementID: "CTR-3PL-LOG-001-2026",
		VendorID:    "3PL-LOG-001",
		VendorName:  "PT Cepat Logistik Nusantara",
	})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// 2. List contracts
	contracts, err := res.ListContracts(ctx)
	if err != nil {
		t.Fatalf("ListContracts failed: %v", err)
	}

	if len(contracts) == 0 {
		t.Errorf("Expected non-empty list of contracts")
	}

	// 3. Get specific contract
	first := contracts[0]
	ctr, err := res.GetContract(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetContract failed: %v", err)
	}

	if ctr.VendorID != first.VendorID {
		t.Errorf("Expected vendor ID '%s', got '%s'", first.VendorID, ctr.VendorID)
	}
}

