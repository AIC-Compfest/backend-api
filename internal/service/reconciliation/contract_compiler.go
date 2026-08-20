package reconciliation

import (
	"fmt"
	"strings"
	"time"

	"backend-api/internal/model"
)

// CompiledContract is the immutable executable rate card compiled from canonical RateAgreement (PRD §28).
type CompiledContract struct {
	AgreementID         string
	VendorID            string
	EffectiveFrom       time.Time
	EffectiveTo         time.Time
	DefaultBaseRate     int64
	FuelSurchargePct    float64
	MinimumCharge       int64
	DisputeWindowDays   int
	LaneRates           map[string]int64 // Key: "ORIGIN_ZONE:DEST_ZONE:SERVICE" -> BaseRate
	WeightTierRates     []CompiledTierRate
	AccessorialRules    map[string]float64
}

// CompiledTierRate specifies lane rate for weight bracket.
type CompiledTierRate struct {
	LaneKey     string
	WeightMinKg float64
	WeightMaxKg float64
	BaseRate    int64
}

// ContractCompiler transforms canonical RateAgreement into an immutable executable rate card.
type ContractCompiler struct{}

// NewContractCompiler creates a compiler instance.
func NewContractCompiler() *ContractCompiler {
	return &ContractCompiler{}
}

// Compile parses and compiles ContractCanonical into an executable CompiledContract.
func (c *ContractCompiler) Compile(contract *model.ContractCanonical) (*CompiledContract, error) {
	if contract == nil {
		return nil, fmt.Errorf("contract canonical object is nil")
	}

	from, err := time.Parse("2006-01-02", contract.EffectiveFrom)
	if err != nil {
		from = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	to, err := time.Parse("2006-01-02", contract.EffectiveTo)
	if err != nil {
		to = time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	}

	disputeDays := contract.DisputeWindowDays
	if disputeDays <= 0 {
		disputeDays = 30 // PRD §9 default fallback
	}

	fuelPct := contract.ApplicableFuelSurchargePercent
	if fuelPct < 0.0 {
		fuelPct = 14.0 // Fallback only if uninitialized / negative
	}

	laneRates := make(map[string]int64)
	var tierRates []CompiledTierRate

	for _, entry := range contract.RateMatrix {
		orig := NormalizeCity(entry.OriginZone)
		dest := NormalizeCity(entry.DestinationZone)
		srv := strings.ToUpper(strings.TrimSpace(entry.ServiceType))
		if srv == "" {
			srv = "REGULAR"
		}
		laneKey := fmt.Sprintf("%s:%s:%s", orig, dest, srv)

		if entry.WeightMaxKg > 0 {
			tierRates = append(tierRates, CompiledTierRate{
				LaneKey:     laneKey,
				WeightMinKg: entry.WeightMinKg,
				WeightMaxKg: entry.WeightMaxKg,
				BaseRate:    entry.BaseRate,
			})
		} else {
			laneRates[laneKey] = entry.BaseRate
		}
	}

	return &CompiledContract{
		AgreementID:       contract.AgreementID,
		VendorID:          contract.VendorID,
		EffectiveFrom:     from,
		EffectiveTo:       to,
		DefaultBaseRate:   contract.BaseRate,
		FuelSurchargePct:  fuelPct,
		MinimumCharge:     contract.MinimumCharge,
		DisputeWindowDays: disputeDays,
		LaneRates:         laneRates,
		WeightTierRates:   tierRates,
		AccessorialRules:  contract.Surcharges,
	}, nil
}

// LookupRate finds the applicable base rate for a specific shipment lane and weight.
func (cc *CompiledContract) LookupRate(origin, dest, service string, weightKg float64) (int64, string) {
	orig := NormalizeCity(origin)
	dst := NormalizeCity(dest)
	srv := strings.ToUpper(strings.TrimSpace(service))
	if srv == "" {
		srv = "REGULAR"
	}
	laneKey := fmt.Sprintf("%s:%s:%s", orig, dst, srv)

	// 1. Check Tiered Weight Rates
	for _, tier := range cc.WeightTierRates {
		if tier.LaneKey == laneKey && weightKg >= tier.WeightMinKg && (tier.WeightMaxKg == 0 || weightKg <= tier.WeightMaxKg) {
			return tier.BaseRate, fmt.Sprintf("Lane Matrix [%s] Tier (%.1f-%.1f kg)", laneKey, tier.WeightMinKg, tier.WeightMaxKg)
		}
	}

	// 2. Check Flat Lane Rate
	if rate, exists := cc.LaneRates[laneKey]; exists && rate > 0 {
		return rate, fmt.Sprintf("Lane Matrix [%s]", laneKey)
	}

	// 3. Fallback to Contract Default Base Rate
	if cc.DefaultBaseRate > 0 {
		return cc.DefaultBaseRate, fmt.Sprintf("Contract Default Base Rate (%s)", cc.AgreementID)
	}

	return 0, "No applicable rate found"
}

// IsValidOnDate checks whether the contract is effective on the shipment date.
func (cc *CompiledContract) IsValidOnDate(shipmentDateStr string) bool {
	if shipmentDateStr == "" {
		return true
	}
	d, err := time.Parse("2006-01-02", shipmentDateStr)
	if err != nil {
		return true
	}
	return !d.Before(cc.EffectiveFrom) && !d.After(cc.EffectiveTo)
}
