package reconciliation_test

import (
	"testing"

	"backend-api/internal/model"
	"backend-api/internal/service/reconciliation"
)

func TestEntityResolution_AdversarialSuite(t *testing.T) {
	resolver := reconciliation.NewEntityResolver()

	// -------------------------------------------------------------------------
	// 1 & 2. EXACT MATCH NORMALIZATION & OCR STRING CORRUPTIONS
	// -------------------------------------------------------------------------
	t.Run("ADV_ER_01_Exact_ID_Punctuation_And_Case_Normalization", func(t *testing.T) {
		// Invoice carries a reference to shipment "shp-2026-000123" (with lowercase, spaces, hyphens).
		// Surat Jalan has the same shipment as "SHP-2026-000123" (uppercase, proper format).
		// After NormalizeID both should produce "20260001 23" → "2026000123"
		// Entity resolver MUST use inv.ShipmentID (PRIMARY key) for Level 1 matching.
		inv := &model.InvoiceCanonical{
			InvoiceNumber: "INV-001",
			InvoiceID:     "INV-2026-001",
			// ShipmentID is the PRIMARY entity resolution key (ARCHITECTURE_AUDIT fix)
			ShipmentID:  "  shp-2026-000123  ", // normalized → "2026000123"
			Origin:      "JAKARTA",
			Destination: "SURABAYA",
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:  "SHP-2026-000123", // normalized → "2026000123"
			AWBNumber:   "AWB-10000001",
			Origin:      "JAKARTA",
			Destination: "SURABAYA",
		}

		res := resolver.Resolve(inv, sj, nil)
		if !res.IsMatched {
			t.Errorf("normalized exact ShipmentID should match: inv.ShipmentID=%q sj.ShipmentID=%q", inv.ShipmentID, sj.ShipmentID)
		}
		if res.MatchConfidence < 0.95 {
			t.Errorf("exact ID match confidence must be >= 0.95, got %f", res.MatchConfidence)
		}
		if res.MatchMethod != model.MatchExactID {
			t.Errorf("exact ID match must use MatchExactID method, got %q", res.MatchMethod)
		}
	})


	t.Run("ADV_ER_02_OCR_One_Digit_Substitution_Or_Transposition", func(t *testing.T) {
		// OCR misread '0' as 'O' or swapped digits
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-002",
			InvoiceID:      "SHP-2026-OOO123", // Misread '0' as 'O'
			InvoiceDate:    "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightBilledKg: 50.0,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000123",
			ShipmentDate:   "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightActualKg: 50.0,
		}

		res := resolver.Resolve(inv, sj, nil)
		// Should resolve via composite attributes (origin, dest, date, weight)
		if !res.IsMatched {
			t.Errorf("should match via Level 2 composite fallback despite OCR single-character corruption")
		}
		if res.MatchConfidence < 0.80 {
			t.Errorf("composite match confidence must be >= 0.80, got %f", res.MatchConfidence)
		}
	})

	// -------------------------------------------------------------------------
	// 3. COMPOSITE KEY MATCHING & WEIGHT TOLERANCES (10% RULE)
	// -------------------------------------------------------------------------
	t.Run("ADV_ER_03_Weight_Within_10_Percent_Composite_Tolerance", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-003",
			InvoiceDate:    "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightBilledKg: 85.0,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000456",
			ShipmentDate:   "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightActualKg: 84.0, // 84.0 kg vs 85.0 kg is ~1.1% difference (< 10%)
		}

		res := resolver.Resolve(inv, sj, nil)
		if !res.IsMatched {
			t.Errorf("Level 2 Composite Key should resolve when physical route, date, and weight match within 10%%")
		}
	})

	t.Run("ADV_ER_04_Weight_Difference_Outside_10_Percent_Rejection", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-004",
			InvoiceDate:    "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightBilledKg: 85.0,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000789",
			ShipmentDate:   "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightActualKg: 50.0, // 50 kg vs 85 kg is a 41% disparity (> 10%)
		}

		res := resolver.Resolve(inv, sj, nil)
		if res.MatchMethod == model.MatchComposite && res.MatchConfidence > 0.85 {
			t.Errorf("severe weight disparity should not produce high-confidence composite match")
		}
	})

	// -------------------------------------------------------------------------
	// 4. CONFLICTING ROUTES & ATTRIBUTES REJECTION
	// -------------------------------------------------------------------------
	t.Run("ADV_ER_05_Conflicting_Destination_Rejection", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-005",
			InvoiceDate:    "2026-08-10",
			Origin:         "JAKARTA",
			Destination:    "BANDUNG",
			WeightBilledKg: 50.0,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000999",
			ShipmentDate:   "2026-08-10",
			Origin:         "JAKARTA",
			Destination:    "MEDAN", // Conflicting destination
			WeightActualKg: 50.0,
		}

		res := resolver.Resolve(inv, sj, nil)
		if res.IsMatched && res.MatchConfidence >= 0.85 {
			t.Errorf("conflicting destination must NOT match with high confidence")
		}
	})

	t.Run("ADV_ER_06_Conflicting_Origin_Rejection", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-006",
			InvoiceDate:    "2026-08-10",
			Origin:         "SURABAYA",
			Destination:    "JAKARTA",
			WeightBilledKg: 50.0,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000998",
			ShipmentDate:   "2026-08-10",
			Origin:         "MEDAN", // Conflicting origin
			Destination:    "JAKARTA",
			WeightActualKg: 50.0,
		}

		res := resolver.Resolve(inv, sj, nil)
		if res.IsMatched && res.MatchConfidence >= 0.85 {
			t.Errorf("conflicting origin must NOT match with high confidence")
		}
	})

	// -------------------------------------------------------------------------
	// 5. CITY ALIASES & NORMALIZATION (DKI / JKT / SBY / BDG)
	// -------------------------------------------------------------------------
	t.Run("ADV_ER_07_Indonesian_City_Aliases_Normalization", func(t *testing.T) {
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-007",
			InvoiceDate:    "2026-08-10",
			Origin:         "DKI Jakarta",
			Destination:    "SBY",
			WeightBilledKg: 20.0,
		}
		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-2026-000777",
			ShipmentDate:   "2026-08-10",
			Origin:         "JAKARTA",
			Destination:    "SURABAYA",
			WeightActualKg: 20.0,
		}

		res := resolver.Resolve(inv, sj, nil)
		if !res.IsMatched {
			t.Errorf("Indonesian city aliases (DKI Jakarta -> JAKARTA, SBY -> SURABAYA) must resolve successfully")
		}
	})

	// -------------------------------------------------------------------------
	// 6. NIL DATA & MISSING CANDIDATE HANDLING (NO AUTO-MATCH)
	// -------------------------------------------------------------------------
	t.Run("ADV_ER_08_Nil_Inputs_Return_Unmatched_Safely", func(t *testing.T) {
		res1 := resolver.Resolve(nil, nil, nil)
		if res1.IsMatched {
			t.Errorf("nil inputs must return IsMatched = false")
		}

		inv := &model.InvoiceCanonical{InvoiceNumber: "INV-008"}
		res2 := resolver.Resolve(inv, nil, nil)
		if res2.IsMatched {
			t.Errorf("nil shipment must return IsMatched = false")
		}
	})
}
