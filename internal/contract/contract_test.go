package contract

import (
	"context"
	"testing"
)

func TestContractListingAndRetrieval(t *testing.T) {
	db := NewResourceDB(nil)
	res := NewResource(db)
	ctx := context.Background()

	// 1. List contracts
	contracts, err := res.ListContracts(ctx)
	if err != nil {
		t.Fatalf("ListContracts failed: %v", err)
	}

	if len(contracts) == 0 {
		t.Errorf("Expected non-empty list of contracts")
	}

	// 2. Get specific contract
	first := contracts[0]
	ctr, err := res.GetContract(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetContract failed: %v", err)
	}

	if ctr.VendorID != first.VendorID {
		t.Errorf("Expected vendor ID '%s', got '%s'", first.VendorID, ctr.VendorID)
	}
}
