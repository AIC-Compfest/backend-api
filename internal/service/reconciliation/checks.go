package reconciliation

import (
	"fmt"
	"math"
	"time"

	"github.com/AIC-Compfest/backend-api/internal/model"
)

// Checker executes the 13 independent reconciliation checks (PRD §30–§31).
type Checker struct {
	qualityGate *EvidenceQualityEvaluator
	pricing     *PricingEngine
}

// NewChecker creates a checker instance.
func NewChecker(qg *EvidenceQualityEvaluator, pe *PricingEngine) *Checker {
	return &Checker{
		qualityGate: qg,
		pricing:     pe,
	}
}

// RunAllChecks performs all 13 independent validation checks across the 4 canonical document models.
func (c *Checker) RunAllChecks(
	inv *model.InvoiceCanonical,
	sj *model.ShipmentCanonical,
	pod *model.PODCanonical,
	contract *CompiledContract,
	pricing model.PricingResult,
	matchResolution ResolutionResult,
) ([]model.ReconciliationCheck, []model.DiscrepancyItem) {
	var checks []model.ReconciliationCheck
	var discrepancies []model.DiscrepancyItem

	// -----------------------------------------------------------------------
	// 1. Entity & Identity Resolution Check
	// -----------------------------------------------------------------------
	check01 := model.ReconciliationCheck{
		CheckCode:   "CHECK_01_ENTITY_RESOLUTION",
		Confidence:  matchResolution.MatchConfidence,
		Description: "Verify invoice line item links to a physical shipment",
	}
	if !matchResolution.IsMatched {
		check01.Status = "FAIL"
		check01.Actual = "UNRESOLVED"
		check01.Expected = "MATCHED_SHIPMENT"
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeShipmentNotFound,
			Category:        model.CategoryShipment,
			Description:     fmt.Sprintf("Invoice shipment %s could not be matched to any physical Surat Jalan", inv.InvoiceNumber),
			Severity:        model.SeverityCritical,
			RemediationHint: "Check AWB number or verify if shipment was billed under a different reference ID",
		})
	} else {
		check01.Status = "PASS"
		check01.Actual = string(matchResolution.MatchMethod)
		check01.Expected = "MATCHED"
	}
	checks = append(checks, check01)

	// -----------------------------------------------------------------------
	// 2. Vendor Identity Check
	// -----------------------------------------------------------------------
	check02 := model.ReconciliationCheck{
		CheckCode:   "CHECK_02_VENDOR_IDENTITY",
		Confidence:  1.0,
		Description: "Verify invoice vendor matches contract provider",
	}
	if inv.VendorID != "" && contract.VendorID != "" && inv.VendorID != contract.VendorID {
		check02.Status = "FAIL"
		check02.Actual = inv.VendorID
		check02.Expected = contract.VendorID
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeOutsideContractValidity,
			Category:        model.CategoryTemporal,
			Description:     fmt.Sprintf("Invoice vendor %s does not match contract vendor %s", inv.VendorID, contract.VendorID),
			Severity:        model.SeverityCritical,
			RemediationHint: "Ensure the invoice is billed against the correct 3PL master agreement",
		})
	} else {
		check02.Status = "PASS"
		check02.Actual = inv.VendorID
		check02.Expected = contract.VendorID
	}
	checks = append(checks, check02)

	// -----------------------------------------------------------------------
	// 3. Route & Zone Consistency Check
	// -----------------------------------------------------------------------
	check03 := model.ReconciliationCheck{
		CheckCode:   "CHECK_03_ROUTE_CONSISTENCY",
		Confidence:  1.0,
		Description: "Verify origin and destination route consistency",
	}
	invDest := NormalizeCity(inv.Destination)
	sjDest := NormalizeCity(sj.Destination)
	if invDest != "" && sjDest != "" && invDest != sjDest {
		check03.Status = "FAIL"
		check03.Actual = inv.Destination
		check03.Expected = sj.Destination
		var ev *model.EvidenceSource
		if s, ok := inv.FieldEvidence["destination"]; ok {
			ev = &s
		}
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeWrongZone,
			Category:        model.CategoryPricing,
			Description:     fmt.Sprintf("Applied destination zone %s instead of actual %s from Surat Jalan", inv.Destination, sj.Destination),
			Severity:        model.SeverityCritical,
			BilledValue:     inv.Destination,
			ExpectedValue:   sj.Destination,
			EvidencePointer: ev,
			RemediationHint: "Rebill using the physical delivery destination zone rate",
		})
	} else {
		check03.Status = "PASS"
		check03.Actual = sj.Destination
		check03.Expected = sj.Destination
	}
	checks = append(checks, check03)

	// -----------------------------------------------------------------------
	// 4. Contract Validity Date Check
	// -----------------------------------------------------------------------
	check04 := model.ReconciliationCheck{
		CheckCode:   "CHECK_04_CONTRACT_VALIDITY",
		Confidence:  1.0,
		Description: "Verify shipment date is within contract effective period",
	}
	if !contract.IsValidOnDate(sj.ShipmentDate) {
		check04.Status = "FAIL"
		check04.Actual = sj.ShipmentDate
		check04.Expected = fmt.Sprintf("%s to %s", contract.EffectiveFrom.Format("2006-01-02"), contract.EffectiveTo.Format("2006-01-02"))
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeOutsideContractValidity,
			Category:        model.CategoryTemporal,
			Description:     fmt.Sprintf("Shipment date %s falls outside contract validity period (%s to %s)", sj.ShipmentDate, contract.EffectiveFrom.Format("2006-01-02"), contract.EffectiveTo.Format("2006-01-02")),
			Severity:        model.SeverityWarning,
			RemediationHint: "Verify addendum or renewed commercial agreement",
		})
	} else {
		check04.Status = "PASS"
		check04.Actual = sj.ShipmentDate
		check04.Expected = "WITHIN_VALID_RANGE"
	}
	checks = append(checks, check04)

	// -----------------------------------------------------------------------
	// 5. Weight Consistency Check
	// -----------------------------------------------------------------------
	check05 := model.ReconciliationCheck{
		CheckCode:   "CHECK_05_WEIGHT_CONSISTENCY",
		Confidence:  0.90,
		Description: "Compare billed weight against physical scale measurement",
	}
	if inv.WeightBilledKg > 0 && sj.WeightActualKg > 0 {
		diff := inv.WeightBilledKg - sj.WeightActualKg
		check05.Actual = inv.WeightBilledKg
		check05.Expected = sj.WeightActualKg
		check05.Difference = diff

		if (math.Abs(diff) > 1.0 && (math.Abs(diff)/sj.WeightActualKg) > 0.05) ||
			(math.Abs(diff) >= 0.5 && (math.Abs(diff)/sj.WeightActualKg) > 0.30) {
			check05.Status = "FAIL"
			var ev *model.EvidenceSource
			if s, ok := inv.FieldEvidence["weight_billed_kg"]; ok {
				ev = &s
			}
			code := model.CodeWrongWeight
			if diff < 0 {
				code = model.CodeVolumeWeightMismatch
			}
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            code,
				Category:        model.CategoryShipment,
				Description:     fmt.Sprintf("Billed weight %.2f kg differs from actual scale weight %.2f kg by %.2f kg", inv.WeightBilledKg, sj.WeightActualKg, diff),
				Severity:        model.SeverityCritical,
				BilledValue:     inv.WeightBilledKg,
				ExpectedValue:   sj.WeightActualKg,
				EvidencePointer: ev,
				RemediationHint: "Adjust invoice line item to match calibrated warehouse scale ticket",
			})
		} else {
			check05.Status = "PASS"
		}
	} else {
		check05.Status = "PASS"
	}
	checks = append(checks, check05)

	// -----------------------------------------------------------------------
	// 6. Quantity Verification Check (with Evidence Quality Gate, PRD §18, §60)
	// -----------------------------------------------------------------------
	check06 := model.ReconciliationCheck{
		CheckCode:   "CHECK_06_QUANTITY_VERIFICATION",
		Description: "Verify billed package count against POD delivery confirmation",
	}
	isPODQtyReliable, _, podQtyEv := c.qualityGate.EvaluatePODQuantity(pod, sj)
	if isPODQtyReliable && pod != nil && pod.QuantityReceived != nil {
		check06.Confidence = 0.85
		check06.Actual = *pod.QuantityReceived
		check06.Expected = sj.TotalPackages

		if *pod.QuantityReceived != sj.TotalPackages && sj.TotalPackages > 0 {
			check06.Status = "FAIL"
			diff := int64(sj.TotalPackages - *pod.QuantityReceived)
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodeQuantityMismatch,
				Category:        model.CategoryShipment,
				Description:     fmt.Sprintf("Invoice claims %d units but POD confirms %d units received", sj.TotalPackages, *pod.QuantityReceived),
				Severity:        model.SeverityCritical,
				BilledValue:     sj.TotalPackages,
				ExpectedValue:   *pod.QuantityReceived,
				DeltaAmount:     &diff,
				EvidencePointer: podQtyEv,
				RemediationHint: "Issue claim for short delivery / damaged koli discrepancy",
			})
		} else {
			check06.Status = "PASS"
		}
	} else {
		// Quantity not on POD or low confidence: Do NOT emit false QUANTITY_MISMATCH
		check06.Status = "UNCERTAIN"
		check06.Confidence = 0.40
		check06.Actual = "UNEXTRACTED_ON_POD"
		check06.Expected = sj.TotalPackages
	}
	checks = append(checks, check06)

	// -----------------------------------------------------------------------
	// 7. Proof of Delivery (POD) Presence Check
	// -----------------------------------------------------------------------
	check07 := model.ReconciliationCheck{
		CheckCode:   "CHECK_07_POD_PRESENCE",
		Confidence:  1.0,
		Description: "Verify delivery receipt exists for shipment",
	}
	if pod == nil || pod.DeliveryStatus == "MISSING" {
		check07.Status = "FAIL"
		check07.Actual = "MISSING"
		check07.Expected = "DELIVERED"
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodePODMissing,
			Category:        model.CategoryDocumentation,
			Description:     fmt.Sprintf("Proof of Delivery (POD) missing for shipment %s", sj.ShipmentID),
			Severity:        model.SeverityCritical,
			RemediationHint: "Request signed proof of delivery from 3PL transporter before payment",
		})
	} else {
		check07.Status = "PASS"
		check07.Actual = pod.DeliveryStatus
		check07.Expected = "DELIVERED"
	}
	checks = append(checks, check07)

	// -----------------------------------------------------------------------
	// 8. Signature & Signoff Presence Check (PRD §15, §33)
	// -----------------------------------------------------------------------
	check08 := model.ReconciliationCheck{
		CheckCode:   "CHECK_08_SIGNATURE_PRESENCE",
		Confidence:  0.92,
		Description: "Verify physical receiver signature or official stamp presence",
	}
	if pod != nil {
		if pod.SignatureConfidence > 0 {
			check08.Confidence = pod.SignatureConfidence
		}
		if !pod.SignaturePresent && !pod.StampPresent {
			check08.Status = "FAIL"
			check08.Actual = false
			check08.Expected = true
			var ev *model.EvidenceSource
			if s, ok := pod.FieldEvidence["signature_present"]; ok {
				ev = &s
			}
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodeSignatureMissing,
				Category:        model.CategoryDocumentation,
				Description:     fmt.Sprintf("POD document %s has no receiver signature or official company stamp", pod.PODNumber),
				Severity:        model.SeverityCritical,
				BilledValue:     "UNOFFICIAL_RECEIPT",
				ExpectedValue:   "SIGNED_RECEIPT",
				EvidencePointer: ev,
				RemediationHint: "Obtain consignee signed acknowledgment to authorize release of payment",
			})
		} else if pod.SignatureConfidence > 0 && pod.SignatureConfidence < 0.70 && !pod.StampPresent {
			check08.Status = "UNCERTAIN"
			check08.Actual = "LOW_CONFIDENCE_PRESENCE"
			check08.Expected = true
			var ev *model.EvidenceSource
			if s, ok := pod.FieldEvidence["signature_present"]; ok {
				ev = &s
			}
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodeLowOCRConfidence,
				Category:        model.CategoryDocumentation,
				Description:     fmt.Sprintf("POD receiver signature confidence (%.2f) is below threshold (< 0.70)", pod.SignatureConfidence),
				Severity:        model.SeverityWarning,
				BilledValue:     fmt.Sprintf("%.2f", pod.SignatureConfidence),
				ExpectedValue:   ">= 0.70",
				EvidencePointer: ev,
				RemediationHint: "Human verification required for degraded POD physical signature area",
			})
		} else {
			check08.Status = "PASS"
			check08.Actual = true
			check08.Expected = true
		}
	} else {
		check08.Status = "PASS"
		check08.Actual = false
		check08.Expected = false
	}
	checks = append(checks, check08)

	// -----------------------------------------------------------------------
	// 9. Delivery Date Temporal Validity Check
	// -----------------------------------------------------------------------
	check09 := model.ReconciliationCheck{
		CheckCode:   "CHECK_09_DELIVERY_DATE_TEMPORAL",
		Confidence:  0.90,
		Description: "Verify delivery date is after dispatch date",
	}
	if pod != nil && pod.DeliveryDate != "" && sj.ShipmentDate != "" {
		check09.Actual = pod.DeliveryDate
		check09.Expected = fmt.Sprintf(">= %s", sj.ShipmentDate)

		dDate, err1 := time.Parse("2006-01-02", pod.DeliveryDate)
		sDate, err2 := time.Parse("2006-01-02", sj.ShipmentDate)
		if err1 == nil && err2 == nil && dDate.Before(sDate) {
			check09.Status = "FAIL"
			var ev *model.EvidenceSource
			if s, ok := pod.FieldEvidence["delivery_date"]; ok {
				ev = &s
			}
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodePODDateMismatch,
				Category:        model.CategoryDocumentation,
				Description:     fmt.Sprintf("POD delivery date (%s) is earlier than dispatch (%s)", pod.DeliveryDate, sj.ShipmentDate),
				Severity:        model.SeverityWarning,
				BilledValue:     pod.DeliveryDate,
				ExpectedValue:   sj.ShipmentDate,
				EvidencePointer: ev,
				RemediationHint: "Verify date logging timestamp on delivery receipt",
			})
		} else {
			check09.Status = "PASS"
		}
	} else {
		check09.Status = "PASS"
	}
	checks = append(checks, check09)

	// -----------------------------------------------------------------------
	// 10. Base Rate & Pricing Verification Check (PRD §29)
	// -----------------------------------------------------------------------
	check10 := model.ReconciliationCheck{
		CheckCode:   "CHECK_10_PRICING_VERIFICATION",
		Confidence:  1.0,
		Description: "Verify billed base rate against contracted commercial rate",
	}
	diffAmount := pricing.DifferenceAmount
	check10.Actual = pricing.Trace.BilledTotal
	check10.Expected = pricing.Trace.ExpectedTotal
	check10.Difference = diffAmount

	if pricing.IsMaterial {
		check10.Status = "FAIL"
		var ev *model.EvidenceSource
		if s, ok := inv.FieldEvidence["billed_amount"]; ok {
			ev = &s
		}
		if diffAmount > 0 {
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodeRateOvercharge,
				Category:        model.CategoryPricing,
				Description:     fmt.Sprintf("Billed base rate Rp%s exceeds contract Rp%s by Rp%s", formatIDR(pricing.Trace.BilledTotal), formatIDR(pricing.Trace.ExpectedTotal), formatIDR(diffAmount)),
				Severity:        model.SeverityCritical,
				BilledValue:     pricing.Trace.BilledTotal,
				ExpectedValue:   pricing.Trace.ExpectedTotal,
				DeltaAmount:     &diffAmount,
				EvidencePointer: ev,
				RemediationHint: "Debit note / credit memo request for base rate overcharge",
			})
		} else {
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodeMinimumChargeViolated,
				Category:        model.CategoryPricing,
				Description:     fmt.Sprintf("Billed charge Rp%s violates minimum floor charge or deviates from expected contract charge Rp%s by Rp%s", formatIDR(pricing.Trace.BilledTotal), formatIDR(pricing.Trace.ExpectedTotal), formatIDR(diffAmount)),
				Severity:        model.SeverityCritical,
				BilledValue:     pricing.Trace.BilledTotal,
				ExpectedValue:   pricing.Trace.ExpectedTotal,
				DeltaAmount:     &diffAmount,
				EvidencePointer: ev,
				RemediationHint: "Verify billing minimum floor clause and re-invoice accordingly",
			})
		}
	} else {
		check10.Status = "PASS"
	}
	checks = append(checks, check10)

	// -----------------------------------------------------------------------
	// 11. Tax (PPN 11%) Calculation Check
	// -----------------------------------------------------------------------
	check11 := model.ReconciliationCheck{
		CheckCode:   "CHECK_11_TAX_CALCULATION",
		Confidence:  1.0,
		Description: "Verify VAT 11% accuracy against taxable base",
	}
	if inv.TaxBilled > 0 && pricing.ExpectedTax > 0 {
		taxDiff := math.Abs(float64(inv.TaxBilled - pricing.ExpectedTax))
		check11.Actual = inv.TaxBilled
		check11.Expected = pricing.ExpectedTax
		check11.Difference = taxDiff

		if taxDiff > 1000 {
			check11.Status = "FAIL"
			diffInt := int64(taxDiff)
			discrepancies = append(discrepancies, model.DiscrepancyItem{
				Code:            model.CodeTaxCalculationError,
				Category:        model.CategoryPricing,
				Description:     fmt.Sprintf("Billed VAT Rp%s differs from expected 11%% VAT Rp%s", formatIDR(inv.TaxBilled), formatIDR(pricing.ExpectedTax)),
				Severity:        model.SeverityWarning,
				BilledValue:     inv.TaxBilled,
				ExpectedValue:   pricing.ExpectedTax,
				DeltaAmount:     &diffInt,
				RemediationHint: "Request revised tax invoice (Faktur Pajak) with corrected taxable base",
			})
		} else {
			check11.Status = "PASS"
		}
	} else {
		check11.Status = "PASS"
	}
	checks = append(checks, check11)

	// -----------------------------------------------------------------------
	// 12. Service Type Consistency Check (PRD §31)
	// -----------------------------------------------------------------------
	check12 := model.ReconciliationCheck{
		CheckCode:   "CHECK_12_SERVICE_TYPE",
		Confidence:  1.0,
		Description: "Verify billed service type matches dispatch tier",
	}
	if inv.ServiceType != "" && sj.ServiceType != "" && inv.ServiceType != sj.ServiceType {
		check12.Status = "FAIL"
		check12.Actual = inv.ServiceType
		check12.Expected = sj.ServiceType
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeWrongService,
			Category:        model.CategoryShipment,
			Description:     fmt.Sprintf("Billed for %s service when %s was dispatched", inv.ServiceType, sj.ServiceType),
			Severity:        model.SeverityCritical,
			BilledValue:     inv.ServiceType,
			ExpectedValue:   sj.ServiceType,
			RemediationHint: "Rebill according to the actual service level agreed on dispatch note",
		})
	} else {
		check12.Status = "PASS"
		check12.Actual = sj.ServiceType
		check12.Expected = sj.ServiceType
	}
	checks = append(checks, check12)

	// -----------------------------------------------------------------------
	// 13. Fuel Surcharge Rate Check (PRD §31)
	// -----------------------------------------------------------------------
	check13 := model.ReconciliationCheck{
		CheckCode:   "CHECK_13_FUEL_SURCHARGE_RATE",
		Confidence:  1.0,
		Description: "Verify applied fuel surcharge percent adheres to contract clause",
	}
	if inv.FuelSurchargePercentBilled > contract.FuelSurchargePct {
		check13.Status = "FAIL"
		check13.Actual = inv.FuelSurchargePercentBilled
		check13.Expected = contract.FuelSurchargePct
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeWrongFuelSurcharge,
			Category:        model.CategoryPricing,
			Description:     fmt.Sprintf("Billed fuel surcharge %.1f%% exceeds contracted rate %.1f%%", inv.FuelSurchargePercentBilled, contract.FuelSurchargePct),
			Severity:        model.SeverityWarning,
			BilledValue:     inv.FuelSurchargePercentBilled,
			ExpectedValue:   contract.FuelSurchargePct,
			RemediationHint: "Adjust fuel surcharge percentage to match valid rate card addendum",
		})
	} else {
		check13.Status = "PASS"
		check13.Actual = contract.FuelSurchargePct
		check13.Expected = contract.FuelSurchargePct
	}
	checks = append(checks, check13)

	return checks, discrepancies
}

func formatIDR(val int64) string {
	// Simple IDR thousand-separated string
	in := fmt.Sprintf("%d", val)
	n := len(in)
	if n <= 3 {
		return in
	}
	var out []byte
	rem := n % 3
	if rem > 0 {
		out = append(out, in[:rem]...)
		if n > rem {
			out = append(out, ',')
		}
	}
	for i := rem; i < n; i += 3 {
		out = append(out, in[i:i+3]...)
		if i+3 < n {
			out = append(out, ',')
		}
	}
	return string(out)
}
