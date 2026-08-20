package reconciliation_test

import (
	"testing"
	"time"

	"backend-api/internal/model"
	"backend-api/internal/service/reconciliation"
)

func TestPhase9V3_AdversarialCombinations(t *testing.T) {
	engine := reconciliation.NewEngine()

	// -------------------------------------------------------------------------
	// A01: Wrong Destination + Wrong Weight
	// -------------------------------------------------------------------------
	t.Run("A01_WrongDestination_And_WrongWeight", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:      "INV-A01",
			InvoiceNumber:  "INV-A01",
			Origin:         "JAKARTA",
			Destination:    "SURABAYA", // Billed Surabaya
			WeightBilledKg: 150.0,      // Billed 150 kg
			BilledAmount:   1500000,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-A01",
			Origin:         "JAKARTA",
			Destination:    "MEDAN", // Actual Medan
			WeightActualKg: 100.0,   // Actual 100 kg
		}
		pod := &model.PODCanonical{PODNumber: "POD-A01", DeliveryStatus: "DELIVERED", SignaturePresent: true}
		contract := &model.ContractCanonical{AgreementID: "CTR-A01", BaseRate: 1000000, ApplicableFuelSurchargePercent: 10.0}

		res, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		if err != nil {
			t.Fatalf("unexpected engine error: %v", err)
		}
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
		hasDest, hasWeight := false, false
		for _, d := range res.Reconciliation.Discrepancies {
			if d.Code == model.CodeWrongDestination || d.Code == model.CodeWrongZone {
				hasDest = true
			}
			if d.Code == model.CodeWrongWeightTier || d.Code == model.CodeWrongWeight || d.Code == model.CodeRateOvercharge {
				hasWeight = true
			}
		}
		if !hasDest || !hasWeight {
			t.Errorf("expected both destination and weight/rate discrepancies, got: %+v", res.Reconciliation.Discrepancies)
		}
	})

	// -------------------------------------------------------------------------
	// A02: Wrong Service + Unauthorized Fee
	// -------------------------------------------------------------------------
	t.Run("A02_WrongService_And_UnauthorizedFee", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:                "INV-A02",
			InvoiceNumber:            "INV-A02",
			ServiceType:              "EXPRESS", // Express billed
			AccessorialChargesBilled: 75000,     // Unauthorized accessorial fee
			BilledAmount:             1500000,
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A02", ServiceType: "REGULAR"}
		pod := &model.PODCanonical{PODNumber: "POD-A02", DeliveryStatus: "DELIVERED", SignaturePresent: true}
		contract := &model.ContractCanonical{AgreementID: "CTR-A02", BaseRate: 1000000, ApplicableFuelSurchargePercent: 10.0}

		res, _ := engine.ReconcileTransaction(inv, sj, pod, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
		hasService, hasOvercharge := false, false
		for _, d := range res.Reconciliation.Discrepancies {
			if d.Code == model.CodeWrongService {
				hasService = true
			}
			if d.Code == model.CodeUnauthorizedFee || d.Code == model.CodeWrongAccessorial || d.Code == model.CodeRateOvercharge {
				hasOvercharge = true
			}
		}
		if !hasService || !hasOvercharge {
			t.Errorf("expected wrong service and unauthorized fee/overcharge discrepancies, got: %+v", res.Reconciliation.Discrepancies)
		}
	})

	// -------------------------------------------------------------------------
	// A03: Wrong Zone + Fuel Surcharge Mismatch
	// -------------------------------------------------------------------------
	t.Run("A03_WrongZone_And_FuelSurcharge", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:                 "INV-A03",
			InvoiceNumber:             "INV-A03",
			Origin:                    "JAKARTA",
			Destination:               "BANDUNG", // Discrepant zone
			FuelSurchargePercentBilled: 20.0,      // Billed 20% vs contracted 14%
			BilledAmount:              500000,
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A03", Origin: "JAKARTA", Destination: "SURABAYA"}
		pod := &model.PODCanonical{PODNumber: "POD-A03", DeliveryStatus: "DELIVERED", SignaturePresent: true}
		contract := &model.ContractCanonical{AgreementID: "CTR-A03", BaseRate: 400000, ApplicableFuelSurchargePercent: 14.0}

		res, _ := engine.ReconcileTransaction(inv, sj, pod, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A04: Missing POD + Signature Missing on SJ
	// -------------------------------------------------------------------------
	t.Run("A04_MissingPOD_And_MissingSignature", func(t *testing.T) {
		inv := &model.InvoiceCanonical{InvoiceID: "INV-A04", InvoiceNumber: "INV-A04", BilledAmount: 500000}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A04"}
		// POD is nil (Missing POD)
		res, _ := engine.ReconcileTransaction(inv, sj, nil, &model.ContractCanonical{AgreementID: "CTR-A04", BaseRate: 500000})
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION for missing POD, got %s", res.Reconciliation.Status)
		}
		hasPODMissing := false
		for _, d := range res.Reconciliation.Discrepancies {
			if d.Code == model.CodePODMissing {
				hasPODMissing = true
			}
		}
		if !hasPODMissing {
			t.Errorf("expected POD_MISSING code, got: %+v", res.Reconciliation.Discrepancies)
		}
	})

	// -------------------------------------------------------------------------
	// A05: Duplicate Invoice + Wrong Amount
	// -------------------------------------------------------------------------
	t.Run("A05_DuplicateInvoice_And_WrongAmount", func(t *testing.T) {
		dupEngine := reconciliation.NewEngine()
		inv1 := &model.InvoiceCanonical{InvoiceID: "INV-DUP", InvoiceNumber: "INV-DUP", VendorID: "3PL-01", BilledAmount: 500000}
		sj1 := &model.ShipmentCanonical{ShipmentID: "SHP-DUP", SuratJalanNumber: "SJ-DUP"}
		ctr := &model.ContractCanonical{AgreementID: "CTR-DUP", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}

		// First submission (Valid)
		_, _ = dupEngine.ReconcileTransaction(inv1, sj1, nil, ctr)

		// Second submission with wrong amount (Overcharge + Duplicate)
		inv2 := &model.InvoiceCanonical{InvoiceID: "INV-DUP", InvoiceNumber: "INV-DUP", VendorID: "3PL-01", BilledAmount: 900000}
		res2, _ := dupEngine.ReconcileTransaction(inv2, sj1, nil, ctr)

		if res2.Reconciliation.Status != model.StatusDuplicate && res2.Reconciliation.Status != model.StatusException {
			t.Errorf("expected DUPLICATE/EXCEPTION status, got %s", res2.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A06: OCR Typo + Wrong Destination
	// -------------------------------------------------------------------------
	t.Run("A06_OCRTypo_And_WrongDestination", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "SHP-2026-OOO123", // Typo in ID
			InvoiceNumber: "INV-A06",
			Origin:        "JAKARTA",
			Destination:   "SURABAYA", // Billed Surabaya
			InvoiceDate:   "2026-08-10",
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:   "SHP-2026-000123",
			Origin:       "JAKARTA",
			Destination:  "MEDAN", // Actual Medan
			ShipmentDate: "2026-08-10",
		}
		pod := &model.PODCanonical{PODNumber: "POD-A06", DeliveryStatus: "DELIVERED", SignaturePresent: true}
		contract := &model.ContractCanonical{AgreementID: "CTR-A06", BaseRate: 500000}

		res, _ := engine.ReconcileTransaction(inv, sj, pod, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A07: OCR Typo + Weight Discrepancy
	// -------------------------------------------------------------------------
	t.Run("A07_OCRTypo_And_WeightDiscrepancy", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:      "SHP-2026-OOO789",
			InvoiceNumber:  "INV-A07",
			WeightBilledKg: 100.0,
			BilledAmount:   1200000,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000789",
			WeightActualKg: 50.0, // 50% discrepancy
		}
		contract := &model.ContractCanonical{AgreementID: "CTR-A07", BaseRate: 500000}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A08: Contract Expired + Rate Overcharge
	// -------------------------------------------------------------------------
	t.Run("A08_ContractExpired_And_Overcharge", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "INV-A08",
			InvoiceNumber: "INV-A08",
			InvoiceDate:   "2026-08-15", // After contract expiry
			BilledAmount:  999000,
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A08", ShipmentDate: "2026-08-15"}
		contract := &model.ContractCanonical{
			AgreementID:   "CTR-A08",
			EffectiveFrom: "2026-01-01",
			EffectiveTo:   "2026-06-30", // Expired
			BaseRate:      500000,
		}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A09: Minimum Charge Violated + Unauthorized Fee
	// -------------------------------------------------------------------------
	t.Run("A09_MinimumCharge_And_UnauthorizedFee", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:                "INV-A09",
			InvoiceNumber:            "INV-A09",
			BilledAmount:             40000, // Below minimum 100,000
			AccessorialChargesBilled: 10000, // Unauthorized fee
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A09"}
		contract := &model.ContractCanonical{AgreementID: "CTR-A09", BaseRate: 30000, MinimumCharge: 100000}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A10: Volume Weight Mismatch + Surcharge Mismatch
	// -------------------------------------------------------------------------
	t.Run("A10_VolumeWeight_And_SurchargeMismatch", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:                 "INV-A10",
			InvoiceNumber:             "INV-A10",
			WeightBilledKg:            25.0, // Charged on volume weight instead of physical
			FuelSurchargePercentBilled: 18.0, // Inflated fuel surcharge
			BilledAmount:              500000,
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A10", WeightActualKg: 10.0}
		contract := &model.ContractCanonical{AgreementID: "CTR-A10", BaseRate: 100000, ApplicableFuelSurchargePercent: 14.0}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A11: Missing Shipment ID + Valid Route
	// -------------------------------------------------------------------------
	t.Run("A11_MissingShipmentID_And_ValidRoute", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:      "", // Missing ID
			InvoiceNumber:  "INV-A11",
			InvoiceDate:    "2026-08-10",
			Origin:         "JAKARTA",
			Destination:    "SURABAYA",
			WeightBilledKg: 50.0,
			BilledAmount:   555000,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-A11",
			ShipmentDate:   "2026-08-10",
			Origin:         "JAKARTA",
			Destination:    "SURABAYA",
			WeightActualKg: 50.0,
		}
		contract := &model.ContractCanonical{AgreementID: "CTR-A11", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		// Matched via Level 2 composite key
		if res.Reconciliation.Status != model.StatusMatch && res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected valid status, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A12: Conflicting Origin + OCR Typo
	// -------------------------------------------------------------------------
	t.Run("A12_ConflictingOrigin_And_OCRTypo", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "SHP-2026-OOOA12",
			InvoiceNumber: "INV-A12",
			Origin:        "MEDAN", // Conflicting origin
			Destination:   "SURABAYA",
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:  "SHP-2026-000A12",
			Origin:      "JAKARTA", // Actual Jakarta
			Destination: "SURABAYA",
		}
		contract := &model.ContractCanonical{AgreementID: "CTR-A12", BaseRate: 500000}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A13: Conflicting Destination + Alias City
	// -------------------------------------------------------------------------
	t.Run("A13_ConflictingDestination_And_AliasCity", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "INV-A13",
			InvoiceNumber: "INV-A13",
			Origin:        "DKI Jakarta",
			Destination:   "SEMARANG", // Conflicting destination
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:  "SHP-A13",
			Origin:      "JAKARTA",
			Destination: "SURABAYA", // Actual Surabaya
		}
		contract := &model.ContractCanonical{AgreementID: "CTR-A13", BaseRate: 500000}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A14: Low OCR Confidence + Financial Discrepancy
	// -------------------------------------------------------------------------
	t.Run("A14_LowOCRConfidence_And_FinancialDiscrepancy", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "INV-A14",
			InvoiceNumber: "INV-A14",
			BilledAmount:  999000,
			FieldEvidence: map[string]model.EvidenceSource{
				"billed_amount": {DocumentID: "INV-A14", Page: 1},
			},
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A14"}
		contract := &model.ContractCanonical{AgreementID: "CTR-A14", BaseRate: 500000}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		// Financial discrepancy overcharge creates exception
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("financial discrepancy MUST trigger EXCEPTION, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A15: Low POD Signature Confidence + Apparent Clean Match
	// -------------------------------------------------------------------------
	t.Run("A15_LowPODSignatureConfidence_And_ApparentMatch", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "INV-A15",
			InvoiceNumber: "INV-A15",
			BilledAmount:  555000,
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A15"}
		pod := &model.PODCanonical{
			PODNumber:           "POD-A15",
			DeliveryStatus:      "DELIVERED",
			SignaturePresent:    false, // Signature missing -> triggers review
			SignatureConfidence: 0.35,
		}
		contract := &model.ContractCanonical{AgreementID: "CTR-A15", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}
		res, _ := engine.ReconcileTransaction(inv, sj, pod, contract)
		if !res.Reconciliation.RequiresHumanReview {
			t.Errorf("missing signature MUST trigger human review flag even on clean numbers")
		}
	})

	// -------------------------------------------------------------------------
	// A16: Duplicate Document Re-Submission with Same Amount
	// -------------------------------------------------------------------------
	t.Run("A16_DuplicateDocSubmission_And_SameAmount", func(t *testing.T) {
		dupEngine := reconciliation.NewEngine()
		inv1 := &model.InvoiceCanonical{
			InvoiceID:     "INV-A16-ORIG",
			InvoiceNumber: "INV-A16-ORIG",
			VendorID:      "3PL-A16",
			BilledAmount:  500000,
		}
		sj1 := &model.ShipmentCanonical{ShipmentID: "SHP-A16", SuratJalanNumber: "SJ-A16"}
		contract := &model.ContractCanonical{AgreementID: "CTR-A16", BaseRate: 500000}
		_, _ = dupEngine.ReconcileTransaction(inv1, sj1, nil, contract)

		// Re-submitting identical vendor, invoice, and shipment
		res2, _ := dupEngine.ReconcileTransaction(inv1, sj1, nil, contract)
		if res2.Reconciliation.Status != model.StatusDuplicate && res2.Reconciliation.Status != model.StatusException {
			t.Errorf("expected DUPLICATE status on re-submission, got %s", res2.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A17: Same Invoice Number + Different Shipment
	// -------------------------------------------------------------------------
	t.Run("A17_SameInvoiceNumber_And_DifferentShipment", func(t *testing.T) {
		inv := &model.InvoiceCanonical{InvoiceID: "INV-MULTI", InvoiceNumber: "INV-MULTI", BilledAmount: 500000}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-DISTINCT-01"}
		contract := &model.ContractCanonical{AgreementID: "CTR-MULTI", BaseRate: 500000}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res == nil {
			t.Fatalf("expected valid reconciliation event")
		}
	})

	// -------------------------------------------------------------------------
	// A18: Same Shipment + Different Invoices
	// -------------------------------------------------------------------------
	t.Run("A18_SameShipment_And_DifferentInvoice", func(t *testing.T) {
		dupEngine := reconciliation.NewEngine()
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-SHARED-01", SuratJalanNumber: "SJ-SHARED-01"}
		contract := &model.ContractCanonical{AgreementID: "CTR-SHARED", BaseRate: 500000}

		inv1 := &model.InvoiceCanonical{InvoiceID: "INV-ALPHA", InvoiceNumber: "INV-ALPHA", VendorID: "3PL-01", BilledAmount: 555000}
		_, _ = dupEngine.ReconcileTransaction(inv1, sj, nil, contract)

		inv2 := &model.InvoiceCanonical{InvoiceID: "INV-BETA", InvoiceNumber: "INV-BETA", VendorID: "3PL-01", BilledAmount: 555000}
		res2, _ := dupEngine.ReconcileTransaction(inv2, sj, nil, contract)

		if res2.Reconciliation.Status != model.StatusDuplicate && res2.Reconciliation.Status != model.StatusException {
			t.Errorf("expected duplicate shipment re-billing detection, got %s", res2.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A19: Boundary Amount Materiality Overcharge
	// -------------------------------------------------------------------------
	t.Run("A19_BoundaryAmount_And_OCRAmbiguity", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:     "INV-A19",
			InvoiceNumber: "INV-A19",
			BilledAmount:  556001, // Rp 1,001 over expected 555,000
			FieldEvidence: map[string]model.EvidenceSource{
				"billed_amount": {DocumentID: "INV-A19", Page: 1},
			},
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A19"}
		contract := &model.ContractCanonical{AgreementID: "CTR-A19", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION for Rp 1,001 material overcharge, got %s", res.Reconciliation.Status)
		}
	})

	// -------------------------------------------------------------------------
	// A20: Multiple Minor Discrepancies + Exactly One Material Discrepancy
	// -------------------------------------------------------------------------
	t.Run("A20_MultipleDiscrepancies_OnlyOneMaterial", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceID:                 "INV-A20",
			InvoiceNumber:             "INV-A20",
			FuelSurchargePercentBilled: 14.0,   // Surcharge matches
			WeightBilledKg:            10.05,  // Minor 0.05 kg weight variance (< 10%)
			BilledAmount:              750000, // Material overcharge (Rp 195,000 variance)
		}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-A20", WeightActualKg: 10.0}
		contract := &model.ContractCanonical{AgreementID: "CTR-A20", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, contract)
		if res.Reconciliation.Status != model.StatusException {
			t.Errorf("expected EXCEPTION, got %s", res.Reconciliation.Status)
		}
		if res.Pricing.DifferenceAmount != 195000 {
			t.Errorf("expected variance amount 195,000 IDR, got %d", res.Pricing.DifferenceAmount)
		}
	})

	// -------------------------------------------------------------------------
	// Additional Hardening: Financial Invariants (E)
	// -------------------------------------------------------------------------
	t.Run("E_FinancialInvariants", func(t *testing.T) {
		inv := &model.InvoiceCanonical{InvoiceID: "INV-INV", InvoiceNumber: "INV-INV", BilledAmount: 600000}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-INV"}
		ctr := &model.ContractCanonical{AgreementID: "CTR-INV", BaseRate: 500000, ApplicableFuelSurchargePercent: 10.0}
		res, _ := engine.ReconcileTransaction(inv, sj, nil, ctr)

		p := res.Pricing
		if p.ExpectedTotalCharge < 0 {
			t.Errorf("expected_total must be >= 0, got %d", p.ExpectedTotalCharge)
		}
		if p.ExpectedTax < 0 {
			t.Errorf("expected_tax must be >= 0, got %d", p.ExpectedTax)
		}
		if p.ExpectedFuelSurcharge < 0 {
			t.Errorf("fuel must be >= 0, got %d", p.ExpectedFuelSurcharge)
		}
		if p.DifferenceAmount != inv.BilledAmount-p.ExpectedTotalCharge {
			t.Errorf("difference must equal billed - expected")
		}
	})

	// -------------------------------------------------------------------------
	// Additional Hardening: Idempotency (F)
	// -------------------------------------------------------------------------
	t.Run("F_Reconciliation_Idempotency", func(t *testing.T) {
		inv := &model.InvoiceCanonical{InvoiceID: "INV-IDEMP", InvoiceNumber: "INV-IDEMP", BilledAmount: 555000}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-IDEMP"}
		ctr := &model.ContractCanonical{AgreementID: "CTR-IDEMP", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}

		res1, _ := engine.ReconcileTransaction(inv, sj, nil, ctr)
		for i := 0; i < 5; i++ {
			resN, _ := engine.ReconcileTransaction(inv, sj, nil, ctr)
			if resN.Pricing.ExpectedTotalCharge != res1.Pricing.ExpectedTotalCharge {
				t.Errorf("non-idempotent calculation on iteration %d", i)
			}
		}
	})

	// -------------------------------------------------------------------------
	// Additional Hardening: Order Independence (G)
	// -------------------------------------------------------------------------
	t.Run("G_Reconciliation_OrderIndependence", func(t *testing.T) {
		eng1 := reconciliation.NewEngine()
		eng2 := reconciliation.NewEngine()

		inv := &model.InvoiceCanonical{InvoiceID: "INV-ORD", InvoiceNumber: "INV-ORD", BilledAmount: 555000}
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-ORD"}
		pod := &model.PODCanonical{PODNumber: "POD-ORD", DeliveryStatus: "DELIVERED", SignaturePresent: true}
		ctr := &model.ContractCanonical{AgreementID: "CTR-ORD", BaseRate: 500000, ApplicableFuelSurchargePercent: 0.0}

		// Reconcile on instance 1
		res1, _ := eng1.ReconcileTransaction(inv, sj, pod, ctr)
		// Reconcile on instance 2
		res2, _ := eng2.ReconcileTransaction(inv, sj, pod, ctr)

		if res1.Reconciliation.Status != res2.Reconciliation.Status ||
			res1.Pricing.ExpectedTotalCharge != res2.Pricing.ExpectedTotalCharge {
			t.Errorf("order / parameter evaluation must produce identical deterministic results")
		}
	})

	// -------------------------------------------------------------------------
	// Additional Hardening: Concurrency Race Safety (H)
	// -------------------------------------------------------------------------
	t.Run("H_ConcurrencyRaceSafety", func(t *testing.T) {
		concurEngine := reconciliation.NewEngine()
		done := make(chan bool, 50)

		for i := 0; i < 50; i++ {
			go func(idx int) {
				inv := &model.InvoiceCanonical{InvoiceID: "INV-CONCUR", InvoiceNumber: "INV-CONCUR", BilledAmount: int64(100000 + idx*1000)}
				sj := &model.ShipmentCanonical{ShipmentID: "SHP-CONCUR"}
				ctr := &model.ContractCanonical{AgreementID: "CTR-CONCUR", BaseRate: 100000}
				_, _ = concurEngine.ReconcileTransaction(inv, sj, nil, ctr)
				done <- true
			}(i)
		}

		for i := 0; i < 50; i++ {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatalf("concurrency timeout at iteration %d", i)
			}
		}
	})

	// -------------------------------------------------------------------------
	// Workstream 5: UNKNOWN Field Safety Test
	// -------------------------------------------------------------------------
	t.Run("WS5_UnknownField_NeverCreatesFinancialAccusation", func(t *testing.T) {
		// When billed amount or weight is missing (unreadable scan), engine must NOT hallucinate or accuse vendor of overcharge
		inv := &model.InvoiceCanonical{
			InvoiceID:     "INV-UNREADABLE",
			InvoiceNumber: "INV-UNREADABLE",
			BilledAmount:  0, // Missing/unreadable billed amount
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-UNREADABLE",
			WeightActualKg: 50.0,
		}
		contract := &model.ContractCanonical{AgreementID: "CTR-UNREADABLE", BaseRate: 500000}

		res, err := engine.ReconcileTransaction(inv, sj, nil, contract)
		if err != nil {
			t.Fatalf("engine should gracefully handle unreadable fields, got: %v", err)
		}

		// When billed amount is 0/unreadable, the difference is negative/zero - engine MUST NOT falsely flag RATE_OVERCHARGE
		hasRateOvercharge := false
		for _, d := range res.Reconciliation.Discrepancies {
			if d.Code == model.CodeRateOvercharge {
				hasRateOvercharge = true
			}
		}
		if hasRateOvercharge {
			t.Errorf("unreadable billed amount must NEVER trigger false RATE_OVERCHARGE accusation against vendor")
		}
	})
}

