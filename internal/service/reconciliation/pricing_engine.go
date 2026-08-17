package reconciliation

import (
	"fmt"
	"math"

	"github.com/AIC-Compfest/backend-api/internal/model"
)

// PricingConfig holds monetary tolerances and VAT policies.
type PricingConfig struct {
	VATRatePercent        float64 // Standard PPN rate (default: 11.0%)
	AbsoluteMaterialityIDR int64   // Difference in IDR considered material (default: 1000 IDR)
	RelativeMaterialityRatio float64 // Percentage difference considered material (default: 0.01 / 1%)
}

// DefaultPricingConfig returns standard Indonesian 3PL financial policies.
func DefaultPricingConfig() PricingConfig {
	return PricingConfig{
		VATRatePercent:          11.0,
		AbsoluteMaterialityIDR:   1000,
		RelativeMaterialityRatio: 0.01,
	}
}

// PricingEngine executes pure deterministic arithmetic calculations without LLMs (PRD §29, §60).
type PricingEngine struct {
	cfg PricingConfig
}

// NewPricingEngine creates an engine instance with configurable policies.
func NewPricingEngine(cfg ...PricingConfig) *PricingEngine {
	c := DefaultPricingConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return &PricingEngine{cfg: c}
}

// Calculate computes the exact expected charge and full calculation trace.
func (pe *PricingEngine) Calculate(
	contract *CompiledContract,
	sj *model.ShipmentCanonical,
	inv *model.InvoiceCanonical,
) model.PricingResult {
	weightKg := sj.WeightActualKg
	if weightKg <= 0 && inv != nil && inv.WeightBilledKg > 0 {
		weightKg = inv.WeightBilledKg
	}
	if weightKg <= 0 {
		weightKg = 1.0 // Default 1 kg floor
	}

	serviceType := "REGULAR"
	if inv != nil && inv.ServiceType != "" {
		serviceType = inv.ServiceType
	}

	origin := sj.Origin
	if origin == "" && inv != nil {
		origin = inv.Origin
	}
	dest := sj.Destination
	if dest == "" && inv != nil {
		dest = inv.Destination
	}

	// 1. Rate Lookup
	baseRateUnit, tierDesc := contract.LookupRate(origin, dest, serviceType, weightKg)

	// 2. Base Amount Calculation
	// Indonesian 3PL base rate is per-trip or per-kg depending on tier
	baseAmount := baseRateUnit

	// 3. Fuel Surcharge Calculation
	fuelPercent := contract.FuelSurchargePct
	fuelAmount := int64(math.Round(float64(baseAmount) * (fuelPercent / 100.0)))

	// 4. Accessorial & Surcharges
	var accessorialAmount int64
	if inv != nil && inv.AccessorialChargesBilled > 0 {
		// If specific accessorial is billed, evaluate if contract supports it
		accessorialAmount = 0 // Evaluated separately in checks
	}

	// 5. Pre-Tax Subtotal
	preTaxAmount := baseAmount + fuelAmount + accessorialAmount

	// 6. Minimum Charge Clamping
	if contract.MinimumCharge > 0 && preTaxAmount < contract.MinimumCharge {
		preTaxAmount = contract.MinimumCharge
	}

	// 7. Tax / VAT (PPN 11%) Calculation
	taxRate := pe.cfg.VATRatePercent
	taxAmount := int64(math.Round(float64(preTaxAmount) * (taxRate / 100.0)))

	// 8. Expected Total
	expectedTotal := preTaxAmount + taxAmount

	// 9. Billed Comparison
	var billedTotal int64
	if inv != nil {
		billedTotal = inv.BilledAmount
	}

	differenceAmount := billedTotal - expectedTotal
	var relativeDiff float64
	if expectedTotal > 0 {
		relativeDiff = math.Abs(float64(differenceAmount)) / float64(expectedTotal)
	}

	// Materiality: difference > absolute threshold (e.g. > Rp 1,000) OR (> 0 and relative ratio > 1%)
	isMaterial := math.Abs(float64(differenceAmount)) > float64(pe.cfg.AbsoluteMaterialityIDR) ||
		(math.Abs(float64(differenceAmount)) > 0 && relativeDiff > pe.cfg.RelativeMaterialityRatio)

	traceLog := fmt.Sprintf(
		"Base Rate: Rp%d (%s) + Fuel Surcharge (%.1f%%): Rp%d = Pre-Tax: Rp%d | VAT (%.1f%%): Rp%d => Expected Total: Rp%d | Billed Total: Rp%d | Variance: Rp%d",
		baseAmount, tierDesc, fuelPercent, fuelAmount, preTaxAmount, taxRate, taxAmount, expectedTotal, billedTotal, differenceAmount,
	)

	trace := model.CalculationTrace{
		BaseRateUnit:      baseRateUnit,
		WeightUsedKg:      weightKg,
		WeightTier:        tierDesc,
		BaseAmount:        baseAmount,
		FuelPercent:       fuelPercent,
		FuelAmount:        fuelAmount,
		AccessorialAmount: accessorialAmount,
		MinimumCharge:     contract.MinimumCharge,
		PreTaxAmount:      preTaxAmount,
		TaxRatePercent:    taxRate,
		TaxAmount:         taxAmount,
		ExpectedTotal:     expectedTotal,
		BilledTotal:       billedTotal,
		VarianceAmount:    differenceAmount,
		TraceLog:          traceLog,
	}

	return model.PricingResult{
		ExpectedBaseCharge:        baseAmount,
		ExpectedFuelSurcharge:     fuelAmount,
		ExpectedAccessorialCharge: accessorialAmount,
		ExpectedTax:               taxAmount,
		ExpectedTotalCharge:       expectedTotal,
		DifferenceAmount:          differenceAmount,
		RelativeDifferenceRatio:   relativeDiff,
		IsMaterial:                isMaterial,
		Trace:                     trace,
	}
}
