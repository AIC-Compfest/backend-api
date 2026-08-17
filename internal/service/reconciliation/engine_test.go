package reconciliation_test

import (
	"testing"

	"github.com/AIC-Compfest/backend-api/internal/model"
	"github.com/AIC-Compfest/backend-api/internal/service/reconciliation"
)

// Helper to create baseline valid transaction objects
func createBaseFixtures() (*model.InvoiceCanonical, *model.ShipmentCanonical, *model.PODCanonical, *model.ContractCanonical) {
	inv := &model.InvoiceCanonical{
		InvoiceID:           "INV-2026-0001",
		InvoiceNumber:       "INV/2026/08/0001",
		InvoiceDate:         "2026-08-10",
		VendorID:            "3PL-LOG-001",
		ServiceType:         "REGULAR",
		Origin:              "SURABAYA",
		Destination:         "JAKARTA",
		WeightBilledKg:      84.33,
		BaseChargeBilled:    480000,
		FuelSurchargeBilled: 67200,
		TaxBilled:           60192,
		BilledAmount:        607392,
		Currency:            "IDR",
		FieldEvidence: map[string]model.EvidenceSource{
			"billed_amount": {DocumentID: "INV-DOC-01", Page: 1},
		},
	}

	sj := &model.ShipmentCanonical{
		SuratJalanNumber: "SJ-2026-0001",
		ShipmentID:       "SHP-2026-0001",
		AWBNumber:        "AWB10000001",
		ShipmentDate:     "2026-08-08",
		Origin:           "SURABAYA",
		Destination:      "JAKARTA",
		WeightActualKg:   84.33,
		TotalPackages:    16,
		FieldEvidence: map[string]model.EvidenceSource{
			"shipment_id": {DocumentID: "SJ-DOC-01", Page: 1},
		},
	}

	pod := &model.PODCanonical{
		PODNumber:           "POD-2026-0001",
		DeliveryStatus:      "DELIVERED",
		DeliveryDate:        "2026-08-10",
		ReceiverName:        "Budi Santoso",
		SignaturePresent:    true,
		SignatureConfidence: 0.95,
		StampPresent:        true,
		FieldEvidence: map[string]model.EvidenceSource{
			"delivery_date":     {DocumentID: "POD-DOC-01", Page: 1},
			"signature_present": {DocumentID: "POD-DOC-01", Page: 1},
		},
	}

	contract := &model.ContractCanonical{
		AgreementID:                    "CTR-3PL-LOG-001-2026",
		VendorID:                       "3PL-LOG-001",
		EffectiveFrom:                  "2026-01-01",
		EffectiveTo:                    "2026-12-31",
		RateUnit:                       "PER_TRIP",
		BaseRate:                       480000,
		ApplicableFuelSurchargePercent: 14.0,
		DisputeWindowDays:              45,
	}

	return inv, sj, pod, contract
}

// ---------------------------------------------------------------------------
// 15 GOLDEN CASES RECONCILIATION TEST SUITE (PRD §25–§34)
// ---------------------------------------------------------------------------

func TestGoldenCases(t *testing.T) {
	// CASE 01: Perfect MATCH
	t.Run("CASE_01_Perfect_MATCH", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event.Reconciliation.Status != model.StatusMatch {
			t.Errorf("expected status %s, got %s (discrepancies: %v)", model.StatusMatch, event.Reconciliation.Status, event.Reconciliation.Discrepancies)
		}
		if len(event.Reconciliation.Discrepancies) != 0 {
			t.Errorf("expected 0 discrepancies, got %d", len(event.Reconciliation.Discrepancies))
		}
	})

	// CASE 02: Invoice Overcharge
	t.Run("CASE_02_Invoice_Overcharge", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		inv.BilledAmount = 850000 // Overcharge vs expected ~607392
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event.Reconciliation.Status != model.StatusException {
			t.Errorf("expected status %s, got %s", model.StatusException, event.Reconciliation.Status)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeRateOvercharge {
				found = true
				if d.DeltaAmount == nil || *d.DeltaAmount <= 0 {
					t.Errorf("expected positive delta amount for overcharge, got %v", d.DeltaAmount)
				}
			}
		}
		if !found {
			t.Errorf("expected RATE_OVERCHARGE discrepancy")
		}
	})

	// CASE 03: Wrong Weight Tier
	t.Run("CASE_03_Wrong_Weight_Tier", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		inv.WeightBilledKg = 150.0 // Billed 150 kg vs Actual 84.33 kg
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeWrongWeight {
				found = true
			}
		}
		if !found {
			t.Errorf("expected WRONG_WEIGHT discrepancy")
		}
	})

	// CASE 04: Wrong Destination Zone
	t.Run("CASE_04_Wrong_Destination_Zone", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		inv.Destination = "SEMARANG" // Invoiced SEMARANG vs Delivered JAKARTA
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeWrongZone {
				found = true
			}
		}
		if !found {
			t.Errorf("expected WRONG_ZONE discrepancy")
		}
	})

	// CASE 05: Missing POD
	t.Run("CASE_05_Missing_POD", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, _, contract := createBaseFixtures()
		event, err := engine.ReconcileTransaction(inv, sj, nil, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodePODMissing {
				found = true
			}
		}
		if !found {
			t.Errorf("expected POD_MISSING discrepancy")
		}
	})

	// CASE 06: Missing Signature on POD
	t.Run("CASE_06_Missing_Signature", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		pod.SignaturePresent = false
		pod.StampPresent = false
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeSignatureMissing {
				found = true
			}
		}
		if !found {
			t.Errorf("expected SIGNATURE_MISSING discrepancy")
		}
	})

	// CASE 07: Quantity Mismatch with High-Confidence POD
	t.Run("CASE_07_Quantity_Mismatch_High_Confidence", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		qtyReceived := 12 // SJ has 16 packages, POD confirms 12 received
		pod.QuantityReceived = &qtyReceived
		pod.FieldEvidence["quantity_received"] = model.EvidenceSource{DocumentID: "POD-01", Page: 1}

		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeQuantityMismatch {
				found = true
			}
		}
		if !found {
			t.Errorf("expected QUANTITY_MISMATCH discrepancy")
		}
	})

	// CASE 08: Quantity Ambiguity with Low-Confidence POD
	t.Run("CASE_08_Quantity_Ambiguity_No_False_Discrepancy", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		pod.QuantityReceived = nil // No reliable quantity on POD

		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeQuantityMismatch {
				t.Errorf("must NOT emit false QUANTITY_MISMATCH when POD quantity is unextracted")
			}
		}
	})

	// CASE 09: Wrong Shipment ID but Valid Composite Match
	t.Run("CASE_09_Composite_Entity_Resolution", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		inv.InvoiceNumber = "TYPO-INV-9999" // Shipment ID not directly in invoice number
		sj.ShipmentID = "SHP-2026-DIFFERENT"

		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if event.MatchMethod != model.MatchComposite {
			t.Errorf("expected match method %s, got %s", model.MatchComposite, event.MatchMethod)
		}
	})

	// CASE 10 & 11: Business Duplicate Billing
	t.Run("CASE_11_Business_Duplicate_Billing", func(t *testing.T) {
		eng := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()

		// First submission: PASS
		ev1, err1 := eng.ReconcileTransaction(inv, sj, pod, contract)
		if err1 != nil || ev1.Reconciliation.Status != model.StatusMatch {
			t.Fatalf("first submission expected MATCH, got %s (%v)", ev1.Reconciliation.Status, err1)
		}

		// Second submission of same invoice & shipment: DUPLICATE
		ev2, err2 := eng.ReconcileTransaction(inv, sj, pod, contract)
		if err2 != nil {
			t.Fatalf("second submission error: %v", err2)
		}
		if ev2.Reconciliation.Status != model.StatusDuplicate {
			t.Errorf("expected status %s for duplicate submission, got %s", model.StatusDuplicate, ev2.Reconciliation.Status)
		}
	})

	// CASE 12: Expired Contract
	t.Run("CASE_12_Expired_Contract", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		contract.EffectiveTo = "2026-06-30" // Expired before August shipment
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeOutsideContractValidity {
				found = true
			}
		}
		if !found {
			t.Errorf("expected OUTSIDE_CONTRACT_VALIDITY discrepancy")
		}
	})

	// CASE 13: Tax Calculation Error
	t.Run("CASE_13_Tax_Calculation_Error", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		inv.TaxBilled = 120000 // Billed 20% tax instead of 11% (60,192 IDR)
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeTaxCalculationError {
				found = true
			}
		}
		if !found {
			t.Errorf("expected TAX_CALCULATION_ERROR discrepancy")
		}
	})

	// CASE 14: Multiple Simultaneous Discrepancies
	t.Run("CASE_14_Multiple_Simultaneous_Discrepancies", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		inv.BilledAmount = 900000    // Overcharge
		pod.SignaturePresent = false // Missing signature
		pod.StampPresent = false     // Missing stamp
		inv.Destination = "MEDAN"     // Wrong destination

		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(event.Reconciliation.Discrepancies) < 3 {
			t.Errorf("expected at least 3 discrepancies, got %d", len(event.Reconciliation.Discrepancies))
		}
	})

	// CASE 15: POD Delivery Date Mismatch
	t.Run("CASE_15_POD_Date_Mismatch", func(t *testing.T) {
		engine := reconciliation.NewEngine()
		inv, sj, pod, contract := createBaseFixtures()
		pod.DeliveryDate = "2026-08-01" // Delivered before dispatch (2026-08-08)
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodePODDateMismatch {
				found = true
			}
		}
		if !found {
			t.Errorf("expected POD_DATE_MISMATCH discrepancy")
		}
	})
}
