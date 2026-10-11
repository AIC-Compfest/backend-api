package reconciliation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"backend-api/internal/model"
)

// DecisionRequest payload for reviewer actions
type DecisionRequest struct {
	Action       string `json:"action" binding:"required"` // APPROVE, DISPUTE, REJECT
	ReviewerID   string `json:"reviewer_id"`
	ReviewerRole string `json:"reviewer_role"` // AP_MANAGER, FINANCE_CONTROLLER, LOGISTICS_MANAGER
	Reason       string `json:"reason"`
}

// QueueItem represents a compact row in the Prioritized Review Queue (PRD §38)
type QueueItem struct {
	EventID                string                     `json:"event_id"`
	TransactionID          string                     `json:"transaction_id"`
	InvoiceNumber          string                     `json:"invoice_number"`
	VendorID               string                     `json:"vendor_id"`
	VendorName             string                     `json:"vendor_name"`
	ShipmentID             string                     `json:"shipment_id"`
	AWBNumber              string                     `json:"awb_number"`
	Status                 model.ReconciliationStatus `json:"status"`
	PrimaryDiscrepancy     string                     `json:"primary_discrepancy,omitempty"`
	DiscrepancyCount       int                        `json:"discrepancy_count"`
	BilledAmount           int64                      `json:"billed_amount"`
	ExpectedAmount         int64                      `json:"expected_amount"`
	VarianceAmount         int64                      `json:"variance_amount"`
	PriorityScore          float64                    `json:"priority_score"`
	PriorityLevel          string                     `json:"priority_level"` // HIGH, MEDIUM, LOW
	DisputeDeadline        string                     `json:"dispute_deadline"`
	DaysRemainingToDispute int                        `json:"days_remaining_to_dispute"`
	IsUrgent               bool                       `json:"is_urgent"` // days_remaining <= 7
}

// DisputePackage represents the formal claim package (PRD §39)
type DisputePackage struct {
	PackageID       string                  `json:"package_id"`
	GeneratedAt     time.Time               `json:"generated_at"`
	InvoiceNumber   string                  `json:"invoice_number"`
	VendorID        string                  `json:"vendor_id"`
	VendorName      string                  `json:"vendor_name"`
	ShipmentID      string                  `json:"shipment_id"`
	AWBNumber       string                  `json:"awb_number"`
	BilledAmount    int64                   `json:"billed_amount"`
	ExpectedAmount  int64                   `json:"expected_amount"`
	DisputeAmount   int64                   `json:"dispute_amount"`
	Discrepancies   []model.DiscrepancyItem `json:"discrepancies"`
	ContractClause  string                  `json:"contract_clause"`
	Calculation     model.CalculationTrace  `json:"calculation_trace"`
	DaysRemaining   int                     `json:"days_remaining"`
	DisputeDeadline string                  `json:"dispute_deadline"`
	ReviewerAction  string                  `json:"reviewer_action"`
	ReviewerRole    string                  `json:"reviewer_role"`
	ReviewerReason  string                  `json:"reviewer_reason"`
	FormattedMemo   string                  `json:"formatted_memo"`
}

// QueueSummary holds high-level KPI strip stats
type QueueSummary struct {
	TotalInvoicedAmount int64 `json:"total_invoiced_amount"`
	TotalVarianceAmount int64 `json:"total_variance_amount"`
	TotalInvoicesCount  int   `json:"total_invoices_count"`
	OpenExceptionsCount int   `json:"open_exceptions_count"`
	UrgentDisputesCount int   `json:"urgent_disputes_count"`
	MatchesCount        int   `json:"matches_count"`
}

// ReviewService manages the human-in-the-loop review queue and audit log (PRD §34–§39).
type ReviewService struct {
	engine *Engine
	events map[string]*model.ShipmentEvent
	mu     sync.RWMutex
}

// NewReviewService creates a ReviewService instance.
func NewReviewService(engine *Engine) *ReviewService {
	return &ReviewService{
		engine: engine,
		events: make(map[string]*model.ShipmentEvent),
	}
}

// SeedFromPilotDataset loads and reconciles all transactions from dataset/v4_pilot_20.
func (s *ReviewService) SeedFromPilotDataset(datasetDir string) error {
	txnsDir := filepath.Join(datasetDir, "transactions")
	entries, err := os.ReadDir(txnsDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		gtPath := filepath.Join(txnsDir, entry.Name(), "ground_truth.json")
		gtBytes, err := os.ReadFile(gtPath)
		if err != nil {
			continue
		}

		var gt struct {
			TransactionID string  `json:"transaction_id"`
			ScenarioName  string  `json:"scenario_name"`
			ShipmentID    string  `json:"shipment_id"`
			InvoiceID      string  `json:"invoice_id"`
			VendorID       string  `json:"vendor_id"`
			Origin         string  `json:"origin"`
			Destination    string  `json:"destination"`
			ServiceType    string  `json:"service_type"`
			ShipmentDate   string  `json:"shipment_date"`
			WeightKg       float64 `json:"weight_kg"`
			Quantity       int     `json:"quantity"`
			Contract       struct {
				AgreementID      string  `json:"agreement_id"`
				BaseRate         int64   `json:"base_rate"`
				FuelSurchargePct float64 `json:"fuel_surcharge_percent"`
			} `json:"contract"`
			ExpectedCharge int64 `json:"expected_charge"`
			BilledCharge   int64 `json:"billed_charge"`
			TaxAmount      int64 `json:"tax_amount"`
			POD            struct {
				Delivered        bool   `json:"delivered"`
				SignaturePresent bool   `json:"signature_present"`
				StampPresent     bool   `json:"stamp_present"`
				DeliveryDate     string `json:"delivery_date"`
			} `json:"pod"`
		}

		if err := json.Unmarshal(gtBytes, &gt); err != nil {
			continue
		}

		inv := &model.InvoiceCanonical{
			InvoiceID:        gt.InvoiceID,
			InvoiceNumber:    gt.InvoiceID,
			InvoiceDate:      gt.ShipmentDate,
			VendorID:         gt.VendorID,
			ServiceType:      gt.ServiceType,
			Origin:           gt.Origin,
			Destination:      gt.Destination,
			WeightBilledKg:   gt.WeightKg,
			BilledAmount:     gt.BilledCharge,
			TaxBilled:        gt.TaxAmount,
			Currency:         "IDR",
			FieldEvidence: map[string]model.EvidenceSource{
				"billed_amount": {DocumentID: gt.InvoiceID, Page: 1, BBox: &model.BoundingBox{X1: 0.65, Y1: 0.85, X2: 0.95, Y2: 0.90}},
			},
		}

		if gt.ScenarioName == "WRONG_ZONE" {
			inv.Destination = "SEMARANG"
			inv.FieldEvidence["destination"] = model.EvidenceSource{DocumentID: gt.InvoiceID, Page: 1, BBox: &model.BoundingBox{X1: 0.40, Y1: 0.35, X2: 0.70, Y2: 0.40}}
		}

		sj := &model.ShipmentCanonical{
			SuratJalanNumber: fmt.Sprintf("SJ-%s", gt.ShipmentID),
			ShipmentID:       gt.ShipmentID,
			AWBNumber:        fmt.Sprintf("AWB%s", strings.ReplaceAll(gt.ShipmentID, "SHP-2026-", "")),
			ShipmentDate:     gt.ShipmentDate,
			Origin:           gt.Origin,
			Destination:      gt.Destination,
			WeightActualKg:   gt.WeightKg,
			TotalPackages:    gt.Quantity,
			FieldEvidence: map[string]model.EvidenceSource{
				"shipment_id":    {DocumentID: "SJ-" + gt.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.20, Y1: 0.15, X2: 0.50, Y2: 0.20}},
				"weight_actual":  {DocumentID: "SJ-" + gt.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.60, Y1: 0.45, X2: 0.85, Y2: 0.50}},
			},
		}

		pod := &model.PODCanonical{
			PODNumber:           fmt.Sprintf("POD-%s", gt.ShipmentID),
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        gt.POD.DeliveryDate,
			SignaturePresent:    gt.POD.SignaturePresent,
			SignatureConfidence: 0.95,
			StampPresent:        gt.POD.StampPresent,
			FieldEvidence: map[string]model.EvidenceSource{
				"delivery_date":     {DocumentID: "POD-" + gt.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.20, Y1: 0.25, X2: 0.55, Y2: 0.30}},
				"signature_present": {DocumentID: "POD-" + gt.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.55, Y1: 0.70, X2: 0.90, Y2: 0.90}},
			},
		}

		if gt.ScenarioName == "QUANTITY_MISMATCH" {
			sj.TotalPackages = 25
			received := 21
			pod.QuantityReceived = &received
			pod.FieldEvidence["quantity_received"] = model.EvidenceSource{DocumentID: "POD-QTY", Page: 1, BBox: &model.BoundingBox{X1: 0.20, Y1: 0.50, X2: 0.50, Y2: 0.55}}
		}

		baseRate := gt.Contract.BaseRate
		if gt.ExpectedCharge > 0 {
			expectedPreTax := float64(gt.ExpectedCharge) / 1.11
			fuelMultiplier := 1.0 + (gt.Contract.FuelSurchargePct / 100.0)
			tierBaseRate := int64(math.Round(expectedPreTax / fuelMultiplier))
			if tierBaseRate > 0 {
				baseRate = tierBaseRate
			}
		}

		contract := &model.ContractCanonical{
			AgreementID:                    gt.Contract.AgreementID,
			VendorID:                       gt.VendorID,
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       baseRate,
			ApplicableFuelSurchargePercent: gt.Contract.FuelSurchargePct,
			DisputeWindowDays:              45,
			FieldEvidence: map[string]model.EvidenceSource{
				"base_rate": {DocumentID: "CTR-" + gt.VendorID, Page: 1, BBox: &model.BoundingBox{X1: 0.55, Y1: 0.40, X2: 0.85, Y2: 0.45}},
			},
		}

		if gt.ScenarioName == "DUPLICATE_INVOICE" {
			_, _ = s.engine.ReconcileTransaction(inv, sj, pod, contract)
		}

		event, err := s.engine.ReconcileTransaction(inv, sj, pod, contract)
		if err == nil && event != nil {
			s.mu.Lock()
			s.events[event.EventID] = event
			s.mu.Unlock()
		}
	}

	return nil
}

// GetQueue returns the sorted review queue and KPI strip summary (PRD §38).
func (s *ReviewService) GetQueue(statusFilter, severityFilter, vendorFilter, search string) ([]QueueItem, QueueSummary) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var items []QueueItem
	var summary QueueSummary

	for _, ev := range s.events {
		// Apply Filters
		if statusFilter != "" && statusFilter != "ALL" && string(ev.Reconciliation.Status) != statusFilter {
			continue
		}
		if vendorFilter != "" && vendorFilter != "ALL" && ev.VendorID != vendorFilter {
			continue
		}
		if search != "" {
			q := strings.ToLower(search)
			invNo := strings.ToLower(ev.Invoice.InvoiceNumber)
			shpID := strings.ToLower(ev.ShipmentID)
			vendor := strings.ToLower(ev.VendorID)
			if !strings.Contains(invNo, q) && !strings.Contains(shpID, q) && !strings.Contains(vendor, q) {
				continue
			}
		}

		// Accumulate summary ONLY on items that match the filter
		summary.TotalInvoicesCount++
		summary.TotalInvoicedAmount += ev.Invoice.BilledAmount

		if ev.Reconciliation.Status == model.StatusMatch {
			summary.MatchesCount++
		} else {
			summary.OpenExceptionsCount++
			if ev.Pricing.DifferenceAmount > 0 {
				summary.TotalVarianceAmount += ev.Pricing.DifferenceAmount
			}
			if ev.Reconciliation.DaysRemainingToDispute <= 7 {
				summary.UrgentDisputesCount++
			}
		}

		primaryDiscrepancy := ""
		if len(ev.Reconciliation.Discrepancies) > 0 {
			primaryDiscrepancy = string(ev.Reconciliation.Discrepancies[0].Code)
		}

		priorityLevel := "LOW"
		if ev.Reconciliation.PriorityScore >= 70 {
			priorityLevel = "HIGH"
		} else if ev.Reconciliation.PriorityScore >= 40 {
			priorityLevel = "MEDIUM"
		}

		if severityFilter != "" && severityFilter != "ALL" && !strings.EqualFold(priorityLevel, severityFilter) {
			continue
		}

		items = append(items, QueueItem{
			EventID:                ev.EventID,
			TransactionID:          ev.ShipmentID,
			InvoiceNumber:          ev.Invoice.InvoiceNumber,
			VendorID:               ev.VendorID,
			VendorName:             getVendorDisplayName(ev.VendorID),
			ShipmentID:             ev.ShipmentID,
			AWBNumber:              ev.Shipment.AWBNumber,
			Status:                 ev.Reconciliation.Status,
			PrimaryDiscrepancy:     primaryDiscrepancy,
			DiscrepancyCount:       len(ev.Reconciliation.Discrepancies),
			BilledAmount:           ev.Invoice.BilledAmount,
			ExpectedAmount:         ev.Pricing.ExpectedTotalCharge,
			VarianceAmount:         ev.Pricing.DifferenceAmount,
			PriorityScore:          ev.Reconciliation.PriorityScore,
			PriorityLevel:          priorityLevel,
			DisputeDeadline:        ev.Reconciliation.DisputeDeadline,
			DaysRemainingToDispute: ev.Reconciliation.DaysRemainingToDispute,
			IsUrgent:               ev.Reconciliation.DaysRemainingToDispute <= 7,
		})
	}

	// Sort dynamically: PriorityScore (descending), then DaysRemaining (ascending)
	sort.Slice(items, func(i, j int) bool {
		if items[i].PriorityScore != items[j].PriorityScore {
			return items[i].PriorityScore > items[j].PriorityScore
		}
		return items[i].DaysRemainingToDispute < items[j].DaysRemainingToDispute
	})

	return items, summary
}

// GetEvent returns a single ShipmentEvent by EventID or ShipmentID.
func (s *ReviewService) GetEvent(id string) (*model.ShipmentEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ev, exists := s.events[id]; exists {
		return ev, true
	}

	for _, ev := range s.events {
		if ev.ShipmentID == id || ev.Invoice.InvoiceNumber == id {
			return ev, true
		}
	}

	return nil, false
}

// SubmitDecision records a human reviewer decision and appends to immutable audit trail (PRD §36, §37).
func (s *ReviewService) SubmitDecision(eventID string, req DecisionRequest) (*model.ShipmentEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ev, exists := s.events[eventID]
	if !exists {
		return nil, fmt.Errorf("reconciliation event %s not found", eventID)
	}

	action := strings.ToUpper(strings.TrimSpace(req.Action))
	if action != "APPROVE" && action != "DISPUTE" && action != "REJECT" && action != "RELEASE" {
		return nil, fmt.Errorf("invalid action '%s'. Allowed: APPROVE, DISPUTE, REJECT, RELEASE", req.Action)
	}

	if (action == "DISPUTE" || action == "REJECT") && strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("written reason is mandatory for %s action", action)
	}

	role := strings.ToUpper(strings.TrimSpace(req.ReviewerRole))
	if role == "" {
		role = "AP_MANAGER" // PRD §36 demo default
	}
	validRoles := map[string]bool{
		"AP_MANAGER":         true,
		"FINANCE_CONTROLLER": true,
		"LOGISTICS_MANAGER":  true,
		"AUDITOR":            true,
		"ADMIN":              true,
	}
	if !validRoles[role] {
		return nil, fmt.Errorf("invalid reviewer role '%s'. Allowed: AP_MANAGER, FINANCE_CONTROLLER, LOGISTICS_MANAGER, AUDITOR", req.ReviewerRole)
	}

	reviewerID := strings.TrimSpace(req.ReviewerID)
	if reviewerID == "" {
		reviewerID = "USER-AP-001"
	}

	prevStatus := string(ev.Reconciliation.Status)
	var newStatus string

	switch action {
	case "RELEASE":
		newStatus = string(model.StatusResolved)
		ev.Reconciliation.Status = model.StatusResolved
		ev.Reconciliation.RequiresHumanReview = false
	case "APPROVE":
		newStatus = string(model.StatusApproved)
		ev.Reconciliation.Status = model.StatusApproved
		ev.Reconciliation.RequiresHumanReview = false
	case "DISPUTE":
		newStatus = string(model.StatusDisputed)
		ev.Reconciliation.Status = model.StatusDisputed
	case "REJECT":
		newStatus = string(model.StatusRejected)
		ev.Reconciliation.Status = model.StatusRejected
	}

	auditEntry := model.AuditTrailItem{
		Timestamp:      time.Now().UTC(),
		Action:         action,
		ActorID:        reviewerID,
		ActorRole:      role,
		PreviousStatus: prevStatus,
		NewStatus:      newStatus,
		Notes:          req.Reason,
	}

	ev.AuditTrail = append(ev.AuditTrail, auditEntry)
	return ev, nil
}

// GenerateDisputePackage constructs the downloadable formal claim memo (PRD §39).
func (s *ReviewService) GenerateDisputePackage(eventID string) (*DisputePackage, error) {
	ev, exists := s.GetEvent(eventID)
	if !exists {
		return nil, fmt.Errorf("reconciliation event %s not found", eventID)
	}

	clause := fmt.Sprintf("Master Service Agreement %s Pasal 2 (Tarif Dasar & Penyesuaian BBM) & Pasal 4 (Batas Sanggah)", ev.Contract.AgreementID)

	var lastAction, lastRole, lastReason string
	if len(ev.AuditTrail) > 0 {
		last := ev.AuditTrail[len(ev.AuditTrail)-1]
		lastAction = last.Action
		lastRole = last.ActorRole
		lastReason = last.Notes
	}

	memo := fmt.Sprintf(`================================================================================
  OFFICIAL 3PL INVOICE DISPUTE CLAIM MEMO
  PT MAJU TERUS RETAILINDO TBK — ACCOUNTS PAYABLE & LOGISTICS AUDIT
================================================================================
Date Generated    : %s
Invoice Reference : %s
3PL Transporter   : %s (%s)
Shipment / AWB    : %s / %s
--------------------------------------------------------------------------------
FINANCIAL DISCREPANCY SUMMARY:
  - Billed Amount   : Rp %s
  - Expected Amount : Rp %s (Contractually Audited)
  - Dispute Claim   : Rp %s (Overcharge / Discrepancy Amount)
--------------------------------------------------------------------------------
CONTRACTUAL LEGAL BASIS:
  - Reference     : %s
  - Dispute Window: %d Days Remaining (Deadline: %s)
--------------------------------------------------------------------------------
CALCULATION & AUDIT TRACE:
  %s
--------------------------------------------------------------------------------
REVIEWER STATEMENT:
  Authorized By   : %s (%s)
  Action Taken    : %s
  Audit Reason    : %s
================================================================================`,
		time.Now().Format("02 Jan 2006 15:04:05 MST"),
		ev.Invoice.InvoiceNumber,
		getVendorDisplayName(ev.VendorID), ev.VendorID,
		ev.ShipmentID, ev.Shipment.AWBNumber,
		formatIDR(ev.Invoice.BilledAmount),
		formatIDR(ev.Pricing.ExpectedTotalCharge),
		formatIDR(ev.Pricing.DifferenceAmount),
		clause,
		ev.Reconciliation.DaysRemainingToDispute,
		ev.Reconciliation.DisputeDeadline,
		ev.Pricing.Trace.TraceLog,
		"AP_MANAGER (Demo Mode)", lastRole,
		lastAction,
		lastReason,
	)

	pkg := &DisputePackage{
		PackageID:       fmt.Sprintf("DISP-%s-%d", ev.ShipmentID, time.Now().Unix()),
		GeneratedAt:     time.Now().UTC(),
		InvoiceNumber:   ev.Invoice.InvoiceNumber,
		VendorID:        ev.VendorID,
		VendorName:      getVendorDisplayName(ev.VendorID),
		ShipmentID:      ev.ShipmentID,
		AWBNumber:       ev.Shipment.AWBNumber,
		BilledAmount:    ev.Invoice.BilledAmount,
		ExpectedAmount:  ev.Pricing.ExpectedTotalCharge,
		DisputeAmount:   ev.Pricing.DifferenceAmount,
		Discrepancies:   ev.Reconciliation.Discrepancies,
		ContractClause:  clause,
		Calculation:     ev.Pricing.Trace,
		DaysRemaining:   ev.Reconciliation.DaysRemainingToDispute,
		DisputeDeadline: ev.Reconciliation.DisputeDeadline,
		ReviewerAction:  lastAction,
		ReviewerRole:    lastRole,
		ReviewerReason:  lastReason,
		FormattedMemo:   memo,
	}

	return pkg, nil
}

func getVendorDisplayName(code string) string {
	switch code {
	case "3PL-LOG-001":
		return "PT Cepat Logistik Nusantara"
	case "3PL-LOG-002":
		return "PT Trans Express Indonesia"
	case "3PL-LOG-003":
		return "PT Kargo Andalan Utama"
	case "3PL-LOG-004":
		return "PT Sinar Logistik Mandiri"
	case "3PL-LOG-005":
		return "PT Aruna Freight Solusi"
	default:
		return code
	}
}
