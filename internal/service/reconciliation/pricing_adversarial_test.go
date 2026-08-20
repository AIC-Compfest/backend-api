package reconciliation_test

import (
	"testing"
	"time"

	"backend-api/internal/model"
	"backend-api/internal/service/reconciliation"
)

func TestPricingEngine_AdversarialSuite(t *testing.T) {
	pe := reconciliation.NewPricingEngine()
	cc := reconciliation.NewContractCompiler()

	// =========================================================================
	// CATEGORY A & B: INTEGER ARITHMETIC, ROUNDING, AND VAT 11%
	// =========================================================================
	t.Run("ADV_A01_Rp1_Rounding_Tolerance", func(t *testing.T) {
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-001",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       100000,
			ApplicableFuelSurchargePercent: 14.0,
			DisputeWindowDays:              30,
		}
		compiled, _ := cc.Compile(contract)

		sj := &model.ShipmentCanonical{
			ShipmentID:     "SHP-001",
			Origin:         "JAKARTA",
			Destination:    "SURABAYA",
			WeightActualKg: 10.0,
		}
		// Expected: (100,000 + 14,000) * 1.11 = 126,540 IDR
		// Billed: 126,541 IDR (Rp 1 difference)
		inv := &model.InvoiceCanonical{
			InvoiceNumber:  "INV-001",
			BilledAmount:   126541,
			WeightBilledKg: 10.0,
		}

		res := pe.Calculate(compiled, sj, inv)
		if res.ExpectedTotalCharge != 126540 {
			t.Errorf("expected 126540, got %d", res.ExpectedTotalCharge)
		}
		if res.IsMaterial {
			t.Errorf("Rp 1 difference must NOT be classified as material overcharge")
		}
	})

	t.Run("ADV_A02_Large_Amount_Integer_Arithmetic", func(t *testing.T) {
		// Large enterprise invoice: 500,000,000 IDR base rate
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-LARGE",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       500000000,
			ApplicableFuelSurchargePercent: 14.0,
		}
		compiled, _ := cc.Compile(contract)
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-LARGE", Origin: "JAKARTA", Destination: "BALI", WeightActualKg: 20000.0}

		// Expected: Base 500,000,000 + Fuel 70,000,000 = 570,000,000 Pre-tax. Tax 11% = 62,700,000. Total = 632,700,000 IDR.
		inv := &model.InvoiceCanonical{InvoiceNumber: "INV-LARGE", BilledAmount: 632700000, WeightBilledKg: 20000.0}
		res := pe.Calculate(compiled, sj, inv)

		if res.ExpectedTotalCharge != 632700000 {
			t.Errorf("expected 632,700,000 IDR, got %d", res.ExpectedTotalCharge)
		}
		if res.DifferenceAmount != 0 {
			t.Errorf("expected 0 difference, got %d", res.DifferenceAmount)
		}
	})

	t.Run("ADV_A03_Fractional_Tax_Rounding_Precision", func(t *testing.T) {
		// Base rate 33,333 IDR with 14% fuel = 4,666.62 -> 4,667 IDR
		// Pretax = 38,000 IDR -> Tax 11% = 4,180 IDR -> Total = 42,180 IDR
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-FRAC",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			BaseRate:                       33333,
			ApplicableFuelSurchargePercent: 14.0,
		}
		compiled, _ := cc.Compile(contract)
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-FRAC", WeightActualKg: 1.0}
		inv := &model.InvoiceCanonical{InvoiceNumber: "INV-FRAC", BilledAmount: 42180, WeightBilledKg: 1.0}

		res := pe.Calculate(compiled, sj, inv)
		if res.ExpectedFuelSurcharge != 4667 {
			t.Errorf("expected fuel surcharge 4667, got %d", res.ExpectedFuelSurcharge)
		}
		if res.ExpectedTax != 4180 {
			t.Errorf("expected tax 4180, got %d", res.ExpectedTax)
		}
		if res.ExpectedTotalCharge != 42180 {
			t.Errorf("expected total charge 42180, got %d", res.ExpectedTotalCharge)
		}
	})

	// =========================================================================
	// CATEGORY C & I: MATERIALITY THRESHOLDS (Rp 999 vs Rp 1,000 vs Rp 1,001)
	// =========================================================================
	t.Run("ADV_CI_Rp999_vs_Rp1000_vs_Rp1001_Tolerance", func(t *testing.T) {
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-002",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       100000,
			ApplicableFuelSurchargePercent: 14.0,
		}
		compiled, _ := cc.Compile(contract)
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-002", Origin: "JAKARTA", Destination: "SURABAYA", WeightActualKg: 10.0}

		// Rp 999 overcharge -> DifferenceAmount = 999 <= 1000 tolerance
		inv999 := &model.InvoiceCanonical{InvoiceNumber: "INV-999", BilledAmount: 126540 + 999, WeightBilledKg: 10.0}
		res999 := pe.Calculate(compiled, sj, inv999)
		if res999.IsMaterial {
			t.Errorf("Rp 999 overcharge should be within Rp 1,000 tolerance")
		}

		// Rp 1,000 overcharge -> DifferenceAmount = 1000 <= 1000 tolerance
		inv1000 := &model.InvoiceCanonical{InvoiceNumber: "INV-1000", BilledAmount: 126540 + 1000, WeightBilledKg: 10.0}
		res1000 := pe.Calculate(compiled, sj, inv1000)
		if res1000.IsMaterial {
			t.Errorf("Rp 1,000 overcharge should be within tolerance boundary")
		}

		// Rp 1,001 overcharge -> DifferenceAmount = 1001 > 1000 tolerance
		inv1001 := &model.InvoiceCanonical{InvoiceNumber: "INV-1001", BilledAmount: 126540 + 1001, WeightBilledKg: 10.0}
		res1001 := pe.Calculate(compiled, sj, inv1001)
		if !res1001.IsMaterial {
			t.Errorf("Rp 1,001 overcharge MUST be classified as material")
		}
	})

	// =========================================================================
	// CATEGORY D: WEIGHT TIERS AND BOUNDARIES (10.0kg vs 10.01kg)
	// =========================================================================
	t.Run("ADV_D01_Weight_Tier_Boundary_Clamping", func(t *testing.T) {
		// Contract with tiered rate card in RateMatrix
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-WT",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			ApplicableFuelSurchargePercent: 10.0,
			RateMatrix: []model.RateMatrixEntry{
				{
					OriginZone:      "JAKARTA",
					DestinationZone: "SURABAYA",
					ServiceType:     "REGULAR",
					WeightMinKg:     0.0,
					WeightMaxKg:     10.0,
					BaseRate:        100000, // Tier 1: 0 - 10 kg -> 100,000 IDR
				},
				{
					OriginZone:      "JAKARTA",
					DestinationZone: "SURABAYA",
					ServiceType:     "REGULAR",
					WeightMinKg:     10.01,
					WeightMaxKg:     20.0,
					BaseRate:        180000, // Tier 2: 10.01 - 20 kg -> 180,000 IDR
				},
			},
		}
		compiled, _ := cc.Compile(contract)

		// 1. 10.0 kg shipment -> matches Tier 1 (100,000 IDR base + 10% fuel = 110,000 * 1.11 = 122,100 IDR)
		sj10 := &model.ShipmentCanonical{ShipmentID: "SHP-10KG", Origin: "JAKARTA", Destination: "SURABAYA", WeightActualKg: 10.0}
		inv10 := &model.InvoiceCanonical{InvoiceNumber: "INV-10KG", Origin: "JAKARTA", Destination: "SURABAYA", WeightBilledKg: 10.0, BilledAmount: 122100}
		res10 := pe.Calculate(compiled, sj10, inv10)
		if res10.ExpectedTotalCharge != 122100 {
			t.Errorf("expected 122,100 IDR for Tier 1 (10.0kg), got %d", res10.ExpectedTotalCharge)
		}

		// 2. 10.01 kg shipment -> matches Tier 2 (180,000 IDR base + 10% fuel = 198,000 * 1.11 = 219,780 IDR)
		sj1001 := &model.ShipmentCanonical{ShipmentID: "SHP-1001KG", Origin: "JAKARTA", Destination: "SURABAYA", WeightActualKg: 10.01}
		inv1001 := &model.InvoiceCanonical{InvoiceNumber: "INV-1001KG", Origin: "JAKARTA", Destination: "SURABAYA", WeightBilledKg: 10.01, BilledAmount: 219780}
		res1001 := pe.Calculate(compiled, sj1001, inv1001)
		if res1001.ExpectedTotalCharge != 219780 {
			t.Errorf("expected 219,780 IDR for Tier 2 (10.01kg), got %d", res1001.ExpectedTotalCharge)
		}
	})

	// =========================================================================
	// CATEGORY E: MINIMUM FLOOR CHARGE CLAMPING
	// =========================================================================
	t.Run("ADV_E01_Minimum_Floor_Charge_Below_At_Above", func(t *testing.T) {
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-MIN",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       50000,
			ApplicableFuelSurchargePercent: 14.0,
			MinimumCharge:                  200000, // Minimum 200,000 IDR pretax
		}
		compiled, _ := cc.Compile(contract)

		// 1. Below minimum (50,000 < 200,000) -> Clamped to 200,000 pretax * 1.11 = 222,000 IDR
		sjBelow := &model.ShipmentCanonical{ShipmentID: "SHP-MIN-BELOW", WeightActualKg: 1.0}
		invBelow := &model.InvoiceCanonical{InvoiceNumber: "INV-MIN-BELOW", BilledAmount: 222000, WeightBilledKg: 1.0}
		resBelow := pe.Calculate(compiled, sjBelow, invBelow)
		if resBelow.ExpectedTotalCharge != 222000 {
			t.Errorf("expected minimum charge clamped total 222000, got %d", resBelow.ExpectedTotalCharge)
		}

		// 2. Above minimum (base rate 300,000 > 200,000)
		contractAbove := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-MIN-ABOVE",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       300000,
			ApplicableFuelSurchargePercent: 14.0,
			MinimumCharge:                  200000,
		}
		compiledAbove, _ := cc.Compile(contractAbove)
		invAbove := &model.InvoiceCanonical{InvoiceNumber: "INV-MIN-ABOVE", BilledAmount: 379620, WeightBilledKg: 1.0}
		// 300,000 + 42,000 = 342,000 * 1.11 = 379,620 IDR
		resAbove := pe.Calculate(compiledAbove, sjBelow, invAbove)
		if resAbove.ExpectedTotalCharge != 379620 {
			t.Errorf("expected un-clamped total 379620, got %d", resAbove.ExpectedTotalCharge)
		}
	})

	// =========================================================================
	// CATEGORY F: CONTRACT TEMPORAL BOUNDARIES
	// =========================================================================
	t.Run("ADV_F01_Contract_Temporal_Boundaries", func(t *testing.T) {
		effFrom, _ := time.Parse("2006-01-02", "2026-06-01")
		effTo, _ := time.Parse("2006-01-02", "2026-06-30")

		compiled := &reconciliation.CompiledContract{
			AgreementID:   "CTR-ADV-004",
			EffectiveFrom: effFrom,
			EffectiveTo:   effTo,
		}

		// Exact start date -> Valid
		if !compiled.IsValidOnDate("2026-06-01") {
			t.Errorf("start date 2026-06-01 must be valid")
		}
		// Exact end date -> Valid
		if !compiled.IsValidOnDate("2026-06-30") {
			t.Errorf("end date 2026-06-30 must be valid")
		}
		// Day before start -> Invalid
		if compiled.IsValidOnDate("2026-05-31") {
			t.Errorf("date 2026-05-31 must be invalid (before effective period)")
		}
		// Day after end -> Invalid
		if compiled.IsValidOnDate("2026-07-01") {
			t.Errorf("date 2026-07-01 must be invalid (expired)")
		}
	})

	// =========================================================================
	// CATEGORY G: FUEL SURCHARGE VARIATIONS (0%, EXACT, DECIMAL)
	// =========================================================================
	t.Run("ADV_G01_Zero_And_Decimal_Fuel_Surcharge", func(t *testing.T) {
		// Zero fuel surcharge clause
		contractZero := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-ZERO-FUEL",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			BaseRate:                       100000,
			ApplicableFuelSurchargePercent: 0.0,
		}
		compiledZero, _ := cc.Compile(contractZero)
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-FUEL", Origin: "JAKARTA", Destination: "SURABAYA", WeightActualKg: 10.0}
		invZero := &model.InvoiceCanonical{InvoiceNumber: "INV-ZERO-FUEL", BilledAmount: 111000, WeightBilledKg: 10.0}

		resZero := pe.Calculate(compiledZero, sj, invZero)
		if resZero.ExpectedFuelSurcharge != 0 {
			t.Errorf("expected 0 fuel surcharge, got %d", resZero.ExpectedFuelSurcharge)
		}
		if resZero.ExpectedTotalCharge != 111000 {
			t.Errorf("expected 111000 IDR total with 0%% fuel, got %d", resZero.ExpectedTotalCharge)
		}

		// Decimal fuel surcharge clause (14.5%)
		contractDec := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-DEC-FUEL",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			BaseRate:                       100000,
			ApplicableFuelSurchargePercent: 14.5,
		}
		compiledDec, _ := cc.Compile(contractDec)
		invDec := &model.InvoiceCanonical{InvoiceNumber: "INV-DEC-FUEL", BilledAmount: 127095, WeightBilledKg: 10.0}
		// 100,000 + 14,500 = 114,500 * 1.11 = 127,095 IDR
		resDec := pe.Calculate(compiledDec, sj, invDec)
		if resDec.ExpectedFuelSurcharge != 14500 {
			t.Errorf("expected 14,500 fuel surcharge for 14.5%%, got %d", resDec.ExpectedFuelSurcharge)
		}
		if resDec.ExpectedTotalCharge != 127095 {
			t.Errorf("expected 127095 IDR total for 14.5%% fuel, got %d", resDec.ExpectedTotalCharge)
		}
	})

	// =========================================================================
	// CATEGORY H: ACCESSORIAL CHARGES & UNAUTHORIZED FEES
	// =========================================================================
	t.Run("ADV_H01_Accessorial_Charges_Evaluation", func(t *testing.T) {
		contract := &model.ContractCanonical{
			AgreementID:                    "CTR-ADV-ACC",
			VendorID:                       "3PL-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			BaseRate:                       100000,
			ApplicableFuelSurchargePercent: 10.0,
		}
		compiled, _ := cc.Compile(contract)
		sj := &model.ShipmentCanonical{ShipmentID: "SHP-ACC", WeightActualKg: 10.0}

		// Clean invoice without accessorials: 100k + 10k fuel = 110k * 1.11 = 122,100 IDR
		invClean := &model.InvoiceCanonical{InvoiceNumber: "INV-CLEAN", BilledAmount: 122100, WeightBilledKg: 10.0}
		resClean := pe.Calculate(compiled, sj, invClean)
		if resClean.ExpectedAccessorialCharge != 0 {
			t.Errorf("expected 0 accessorial charges, got %d", resClean.ExpectedAccessorialCharge)
		}

		// Invoice with injected unauthorized fee of 50,000 IDR (Billed 177,600 vs expected 122,100)
		invFee := &model.InvoiceCanonical{InvoiceNumber: "INV-FEE", BilledAmount: 177600, WeightBilledKg: 10.0}
		resFee := pe.Calculate(compiled, sj, invFee)
		if !resFee.IsMaterial {
			t.Errorf("55,500 IDR unauthorized accessorial variance MUST be classified as material")
		}
		if resFee.DifferenceAmount != 55500 {
			t.Errorf("expected 55,500 difference amount, got %d", resFee.DifferenceAmount)
		}
	})
}
