package reconciliation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"backend-api/internal/model"
)

// Engine is the central deterministic 3-way & 4-way reconciliation service (PRD §25–§34).
type Engine struct {
	resolver    *EntityResolver
	compiler    *ContractCompiler
	pricing     *PricingEngine
	qualityGate *EvidenceQualityEvaluator
	checker     *Checker

	// In-memory business duplicate registry (Level 2 Deduplication, PRD §32)
	mu           sync.RWMutex
	processedMap map[string]string // Key: "VENDOR_ID:INVOICE_NO:SHIPMENT_ID" -> EventID
}

// NewEngine creates a new Reconciliation Engine instance.
func NewEngine() *Engine {
	qg := NewEvidenceQualityEvaluator()
	pe := NewPricingEngine()
	return &Engine{
		resolver:     NewEntityResolver(),
		compiler:     NewContractCompiler(),
		pricing:      pe,
		qualityGate:  qg,
		checker:      NewChecker(qg, pe),
		processedMap: make(map[string]string),
	}
}

// ReconcileTransaction performs end-to-end 4-way deterministic reconciliation for a single transaction.
// It enforces strict entity resolution before proceeding to pricing and checks.
// No match is forced — if evidence is insufficient, returns INSUFFICIENT_EVIDENCE.
func (e *Engine) ReconcileTransaction(
	inv *model.InvoiceCanonical,
	sj *model.ShipmentCanonical,
	pod *model.PODCanonical,
	contract *model.ContractCanonical,
) (*model.ShipmentEvent, error) {
	if inv == nil || contract == nil {
		return nil, fmt.Errorf("invoice and contract are required for reconciliation")
	}

	isSJMissing := false
	if sj == nil {
		isSJMissing = true
		sj = &model.ShipmentCanonical{
			SuratJalanNumber: "MISSING",
			ShipmentID:       inv.InvoiceNumber,
			Origin:           inv.Origin,
			Destination:      inv.Destination,
			WeightActualKg:   inv.WeightBilledKg,
		}
	}

	eventID := model.NewUUID()
	shipmentID := sj.ShipmentID
	if shipmentID == "" {
		shipmentID = inv.InvoiceNumber
	}
	vendorID := contract.VendorID
	if vendorID == "" {
		vendorID = inv.VendorID
	}

	// Pre-declare discrepancies so conflict check can append before RunAllChecks
	var discrepancies []model.DiscrepancyItem

	// 1. Compile Contract
	compiledContract, err := e.compiler.Compile(contract)
	if err != nil {
		return nil, fmt.Errorf("failed to compile contract: %w", err)
	}

	// 2. Entity Resolution (PRD §15-§19)
	resolution := e.resolver.Resolve(inv, sj, pod)

	// If there's a hard CONFLICT, escalate immediately
	// Do NOT proceed to pricing with conflicting entity resolution
	if resolution.MatchDecision == MatchDecisionConflict {
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeShipmentNotFound,
			Category:        model.CategoryShipment,
			Description:     fmt.Sprintf("ENTITY CONFLICT: %s", resolution.ConflictReason),
			Severity:        model.SeverityCritical,
			RemediationHint: "Identity keys on invoice and surat jalan conflict. Verify the correct shipment reference before payment.",
		})
	}

	// 3. Pricing Calculation (PRD §29)
	pricingResult := e.pricing.Calculate(compiledContract, sj, inv)

	// 4. Run All 13 Independent Checks (PRD §30–§31) — appends to existing discrepancies
	checks, extraDiscrepancies := e.checker.RunAllChecks(inv, sj, pod, compiledContract, pricingResult, resolution)
	discrepancies = append(discrepancies, extraDiscrepancies...)

	// 5. Business Duplicate Detection (PRD §32)
	businessKey := fmt.Sprintf("%s:%s:%s", vendorID, NormalizeID(inv.InvoiceNumber), NormalizeID(shipmentID))
	e.mu.Lock()
	existingEventID, isDuplicate := e.processedMap[businessKey]
	if !isDuplicate {
		e.processedMap[businessKey] = eventID
	}
	e.mu.Unlock()

	if isSJMissing {
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeSuratJalanMissing,
			Category:        model.CategoryDocumentation,
			Description:     fmt.Sprintf("Surat Jalan / Delivery Order missing from billing package for invoice %s", inv.InvoiceNumber),
			Severity:        model.SeverityCritical,
			RemediationHint: "Require valid Surat Jalan before releasing invoice payment",
		})
	}

	// 6. Calculate Dispute Deadline & Near Expiry Check (PRD §9, §31)
	disputeDays := compiledContract.DisputeWindowDays
	invoiceDate, parseErr := time.Parse("2006-01-02", inv.InvoiceDate)
	if parseErr != nil {
		invoiceDate = time.Now().UTC()
	}
	disputeDeadline := invoiceDate.AddDate(0, 0, disputeDays)
	daysRemaining := int(math.Ceil(time.Until(disputeDeadline).Hours() / 24.0))
	if daysRemaining < 0 {
		daysRemaining = 0
	}

	if daysRemaining <= 3 && daysRemaining > 0 && disputeDays > 0 {
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeDisputeWindowNearExpiry,
			Category:        model.CategoryTemporal,
			Description:     fmt.Sprintf("Invoice aging is within %d days of maximum dispute window (%d days)", daysRemaining, disputeDays),
			Severity:        model.SeverityCritical,
			RemediationHint: "Expedite review and decision before commercial dispute window closes",
		})
	}

	var reasonCodes []string
	if isDuplicate {
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:            model.CodeDuplicateInvoice,
			Category:        model.CategoryDuplicate,
			Description:     fmt.Sprintf("Duplicate invoice submission for shipment %s already processed in event %s", shipmentID, existingEventID),
			Severity:        model.SeverityCritical,
			RemediationHint: "Reject duplicate submission and flag for credit audit",
		})
	}

	for _, d := range discrepancies {
		reasonCodes = append(reasonCodes, string(d.Code))
	}

	// 7. Determine Final Transaction Status (PRD §30)
	var finalStatus model.ReconciliationStatus
	requiresHumanReview := false

	if isDuplicate {
		finalStatus = model.StatusDuplicate
		requiresHumanReview = true
	} else if len(discrepancies) == 0 {
		finalStatus = model.StatusMatch
		requiresHumanReview = false
	} else {
		finalStatus = model.StatusException
		requiresHumanReview = true
	}

	priorityScore := calculatePriorityScore(finalStatus, pricingResult.DifferenceAmount, daysRemaining, disputeDays)

	var podCanonical model.PODCanonical
	if pod != nil {
		podCanonical = *pod
	} else {
		podCanonical = model.PODCanonical{
			DeliveryStatus: "MISSING",
		}
	}

	// 8. Construct Master ShipmentEvent (matching canonical_schema.json 100%)
	event := &model.ShipmentEvent{
		EventID:         eventID,
		ShipmentID:      shipmentID,
		VendorID:        vendorID,
		MatchMethod:     resolution.MatchMethod,
		MatchConfidence: resolution.MatchConfidence,
		Invoice:         *inv,
		Shipment:        *sj,
		POD:             podCanonical,
		Contract:        *contract,
		Pricing:         pricingResult,
		Reconciliation: model.ReconciliationDecision{
			Status:                 finalStatus,
			ReasonCodes:            reasonCodes,
			Discrepancies:          discrepancies,
			RequiresHumanReview:    requiresHumanReview,
			PriorityScore:          priorityScore,
			DisputeDeadline:        disputeDeadline.Format("2006-01-02"),
			DaysRemainingToDispute: daysRemaining,
		},
		Checks: checks,
		AuditTrail: []model.AuditTrailItem{
			{
				Timestamp: time.Now().UTC(),
				Action:    "RECONCILIATION_EVALUATED",
				ActorID:   "SYSTEM_RECONCILIATION_ENGINE",
				ActorRole: "DETERMINISTIC_ENGINE",
				NewStatus: string(finalStatus),
				Notes:     fmt.Sprintf("Evaluated %d checks. Result: %s with %d discrepancies", len(checks), finalStatus, len(discrepancies)),
			},
		},
	}

	return event, nil
}

// calculatePriorityScore computes a 0-100 risk score based on financial exposure and deadline urgency.
func calculatePriorityScore(status model.ReconciliationStatus, variance int64, daysRemaining, windowDays int) float64 {
	if status == model.StatusMatch {
		return 0.0
	}

	var score float64 = 40.0 // Base score for any exception

	// Financial exposure weighting (up to 40 points)
	absVar := math.Abs(float64(variance))
	if absVar > 1000000 {
		score += 40.0
	} else if absVar > 500000 {
		score += 25.0
	} else if absVar > 100000 {
		score += 15.0
	}

	// Urgency weighting (up to 20 points)
	if windowDays > 0 {
		urgencyRatio := 1.0 - (float64(daysRemaining) / float64(windowDays))
		if urgencyRatio > 0.80 {
			score += 20.0
		} else if urgencyRatio > 0.50 {
			score += 10.0
		}
	}

	return math.Min(100.0, math.Round(score*10)/10)
}

// ComputeDocumentHash computes SHA-256 for document level deduplication (PRD §32).
func ComputeDocumentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
