package reconciliation_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AIC-Compfest/backend-api/internal/model"
	"github.com/AIC-Compfest/backend-api/internal/service/reconciliation"
)

// TestSignatureComplianceSuite verifies the business logic of CHECK_07 and CHECK_08
// per PRD §15, §33, §70 (Signature & Stamp Presence Compliance).
func TestSignatureComplianceSuite(t *testing.T) {
	newTestSetup := func(suffix string) (*reconciliation.Engine, model.InvoiceCanonical, model.ShipmentCanonical, model.ContractCanonical) {
		engine := reconciliation.NewEngine()
		inv := model.InvoiceCanonical{
			InvoiceNumber:    "INV-SIG-" + suffix,
			VendorID:         "3PL-LOG-001",
			InvoiceDate:      "2026-08-01",
			WeightBilledKg:   100.0,
			BaseChargeBilled: 500000,
			BilledAmount:     555000,
			Currency:         "IDR",
		}
		sj := model.ShipmentCanonical{
			SuratJalanNumber: "SJ-SIG-" + suffix,
			ShipmentID:       "SHP-SIG-" + suffix,
			ShipmentDate:     "2026-08-01",
			Origin:           "JAKARTA",
			Destination:      "SURABAYA",
			WeightActualKg:   100.0,
			TotalPackages:    10,
		}
		contract := model.ContractCanonical{
			AgreementID:                    "AGR-001",
			VendorID:                       "3PL-LOG-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_KG",
			BaseRate:                       5000,
			ApplicableFuelSurchargePercent: 0.0,
			DisputeWindowDays:              45,
		}
		return engine, inv, sj, contract
	}

	t.Run("1_POD_Exists_With_Valid_Signature_No_Signature_Missing", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("001")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-001",
			ShipmentID:          "SHP-SIG-001",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    true,
			SignatureConfidence: 0.94,
			StampPresent:        false,
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected reconciliation error: %v", err)
		}

		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeSignatureMissing {
				t.Fatalf("Unexpected CodeSignatureMissing discrepancy for valid signature POD")
			}
		}

		foundCheck := false
		for _, c := range event.Checks {
			if c.CheckCode == "CHECK_08_SIGNATURE_PRESENCE" {
				foundCheck = true
				if c.Status != "PASS" {
					t.Errorf("Expected CHECK_08 to PASS, got %s", c.Status)
				}
			}
		}
		if !foundCheck {
			t.Errorf("CHECK_08_SIGNATURE_PRESENCE not found in checks")
		}
	})

	t.Run("2_POD_Exists_With_Valid_Stamp_No_Signature_Missing", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("002")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-002",
			ShipmentID:          "SHP-SIG-002",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    false,
			SignatureConfidence: 0.0,
			StampPresent:        true,
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeSignatureMissing {
				t.Fatalf("Official company stamp should satisfy POD delivery acknowledgment")
			}
		}
	})

	t.Run("3_POD_Missing_Generates_POD_Missing_Critical", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("003")
		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, nil, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if event.Reconciliation.Status != model.StatusException {
			t.Errorf("Expected EXCEPTION for missing POD, got %s", event.Reconciliation.Status)
		}

		found := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodePODMissing {
				found = true
				if d.Severity != model.SeverityCritical {
					t.Errorf("Expected SeverityCritical for POD_MISSING, got %s", d.Severity)
				}
			}
		}
		if !found {
			t.Errorf("Expected CodePODMissing discrepancy")
		}
	})

	t.Run("4_POD_Exists_Without_Signature_And_Without_Stamp_Generates_Signature_Missing", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("004")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-004",
			ShipmentID:          "SHP-SIG-004",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    false,
			SignatureConfidence: 0.0,
			StampPresent:        false,
			FieldEvidence: map[string]model.EvidenceSource{
				"signature_present": {
					DocumentID: "DOC-POD-004",
					Page:       1,
					BBox:       &model.BoundingBox{X1: 0.50, Y1: 0.70, X2: 0.90, Y2: 0.90},
				},
			},
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if event.Reconciliation.Status != model.StatusException {
			t.Errorf("Expected EXCEPTION for missing signature POD, got %s", event.Reconciliation.Status)
		}

		foundSigMissing := false
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeSignatureMissing {
				foundSigMissing = true
				if d.EvidencePointer == nil {
					t.Errorf("Expected EvidencePointer to be attached to SIGNATURE_MISSING discrepancy")
				}
			}
		}
		if !foundSigMissing {
			t.Errorf("Expected CodeSignatureMissing discrepancy")
		}
	})

	t.Run("5_Low_Confidence_Signature_Triggers_Human_Review_Without_False_Accusation", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("005")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-005",
			ShipmentID:          "SHP-SIG-005",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    true,
			SignatureConfidence: 0.55, // Low confidence / noisy scan
			StampPresent:        false,
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !event.Reconciliation.RequiresHumanReview {
			t.Errorf("Low confidence signature MUST trigger RequiresHumanReview = true")
		}

		// Must NOT falsely accuse vendor of completely missing signature when it is simply uncertain
		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeSignatureMissing {
				t.Fatalf("RULE 5 VIOLATION: Low confidence signature must not emit CodeSignatureMissing accusation")
			}
		}
	})

	t.Run("6_Evidence_Pointer_Contains_Valid_BBox", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("006")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-006",
			ShipmentID:          "SHP-SIG-006",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    false,
			StampPresent:        false,
			FieldEvidence: map[string]model.EvidenceSource{
				"signature_present": {
					DocumentID: "DOC-POD-006",
					Page:       1,
					BBox:       &model.BoundingBox{X1: 0.55, Y1: 0.65, X2: 0.92, Y2: 0.88},
				},
			},
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		for _, d := range event.Reconciliation.Discrepancies {
			if d.Code == model.CodeSignatureMissing {
				if d.EvidencePointer == nil || d.EvidencePointer.BBox == nil {
					t.Fatalf("Missing evidence bounding box in discrepancy item")
				}
				bbox := d.EvidencePointer.BBox
				if bbox.X1 != 0.55 || bbox.Y1 != 0.65 || bbox.X2 != 0.92 || bbox.Y2 != 0.88 {
					t.Errorf("BBox coordinates mismatch: %+v", bbox)
				}
			}
		}
	})

	t.Run("7_Zero_Identity_Fields_In_Serialized_Response", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("007")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-007",
			ShipmentID:          "SHP-SIG-007",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    true,
			SignatureConfidence: 0.92,
			StampPresent:        false,
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		jsonBytes, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Failed to serialize event: %v", err)
		}
		jsonStr := strings.ToLower(string(jsonBytes))

		prohibited := []string{
			"signer_identity",
			"signature_owner",
			"signature_authentic",
			"signature_valid",
			"biometric_match",
			"identity_match",
		}

		for _, p := range prohibited {
			if strings.Contains(jsonStr, p) {
				t.Errorf("Prohibited identity/authenticity field found in serialized event: %s", p)
			}
		}
	})

	t.Run("8_No_Authenticity_Decision_Generated", func(t *testing.T) {
		engine, baseInv, baseSj, baseContract := newTestSetup("008")
		qty := 10
		pod := &model.PODCanonical{
			PODNumber:           "POD-SIG-008",
			ShipmentID:          "SHP-SIG-008",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-03",
			QuantityReceived:    &qty,
			SignaturePresent:    true,
			SignatureConfidence: 0.95,
			StampPresent:        true,
		}

		event, err := engine.ReconcileTransaction(&baseInv, &baseSj, pod, &baseContract)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Reconciliation check descriptions must refer only to presence, not authenticity
		for _, c := range event.Checks {
			descLower := strings.ToLower(c.Description)
			if strings.Contains(descLower, "authentic") || strings.Contains(descLower, "genuine") {
				t.Errorf("Prohibited authenticity assertion in check description: %s", c.Description)
			}
		}
	})
}
