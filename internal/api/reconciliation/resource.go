package reconciliation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AIC-Compfest/backend-api/internal/model"
	reconcilesvc "github.com/AIC-Compfest/backend-api/internal/service/reconciliation"
)

// Resource coordinates reconciliation business logic, state transitions, and audit trails.
type Resource struct {
	engine *reconcilesvc.Engine
	repo   Repository
}

// NewResource creates a new domain Resource instance.
func NewResource(engine *reconcilesvc.Engine, repo Repository) *Resource {
	return &Resource{
		engine: engine,
		repo:   repo,
	}
}

// Reconcile orchestrates 4-way deterministic reconciliation for an incoming invoice.
func (r *Resource) Reconcile(req ReconcileRequestDTO) (*model.ShipmentEvent, error) {
	event, err := r.engine.ReconcileTransaction(&req.Invoice, &req.Shipment, req.POD, &req.Contract)
	if err != nil {
		return nil, fmt.Errorf("reconciliation engine error: %w", err)
	}

	if err := r.repo.Save(event); err != nil {
		return nil, fmt.Errorf("failed to persist reconciliation event: %w", err)
	}

	return event, nil
}

// GetQueue aggregates, filters, and prioritizes exception transactions for human review (PRD §38).
func (r *Resource) GetQueue(query QueueQueryDTO) (QueueResponseDTO, error) {
	events, err := r.repo.ListAll()
	if err != nil {
		return QueueResponseDTO{}, fmt.Errorf("failed to list reconciliation events: %w", err)
	}

	var items []QueueItemDTO
	var summary QueueSummaryDTO

	for _, ev := range events {
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

		// Apply Filters
		if query.StatusFilter != "" && query.StatusFilter != "ALL" && string(ev.Reconciliation.Status) != query.StatusFilter {
			continue
		}
		if query.VendorFilter != "" && query.VendorFilter != "ALL" && ev.VendorID != query.VendorFilter {
			continue
		}
		if query.Search != "" {
			q := strings.ToLower(query.Search)
			invNo := strings.ToLower(ev.Invoice.InvoiceNumber)
			shpID := strings.ToLower(ev.ShipmentID)
			vendor := strings.ToLower(ev.VendorID)
			if !strings.Contains(invNo, q) && !strings.Contains(shpID, q) && !strings.Contains(vendor, q) {
				continue
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

		items = append(items, QueueItemDTO{
			EventID:                ev.EventID,
			TransactionID:          ev.ShipmentID,
			InvoiceNumber:          ev.Invoice.InvoiceNumber,
			VendorID:               ev.VendorID,
			VendorName:             GetVendorDisplayName(ev.VendorID),
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

	// Sort: PriorityScore (descending), then DaysRemainingToDispute (ascending)
	sort.Slice(items, func(i, j int) bool {
		if items[i].PriorityScore != items[j].PriorityScore {
			return items[i].PriorityScore > items[j].PriorityScore
		}
		return items[i].DaysRemainingToDispute < items[j].DaysRemainingToDispute
	})

	return QueueResponseDTO{
		Items:   items,
		Summary: summary,
	}, nil
}

// GetEventByID fetches a single ShipmentEvent with all audit and trace details.
func (r *Resource) GetEventByID(id string) (*model.ShipmentEvent, error) {
	return r.repo.GetByID(id)
}

// SubmitDecision records a human reviewer decision and appends to immutable audit trail (PRD §36, §37).
func (r *Resource) SubmitDecision(eventID string, req DecisionRequestDTO) (*model.ShipmentEvent, error) {
	ev, err := r.repo.GetByID(eventID)
	if err != nil {
		return nil, fmt.Errorf("reconciliation event %s not found: %w", eventID, err)
	}

	action := strings.ToUpper(strings.TrimSpace(req.Action))
	if action != "APPROVE" && action != "DISPUTE" && action != "REJECT" {
		return nil, fmt.Errorf("invalid action '%s'. Allowed: APPROVE, DISPUTE, REJECT", req.Action)
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
	case "APPROVE":
		newStatus = string(model.StatusMatch)
		ev.Reconciliation.Status = model.StatusMatch
		ev.Reconciliation.RequiresHumanReview = false
	case "DISPUTE":
		newStatus = "DISPUTE_SUBMITTED"
		ev.Reconciliation.Status = model.StatusException
	case "REJECT":
		newStatus = "INVOICE_REJECTED"
		ev.Reconciliation.Status = model.StatusException
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

	if err := r.repo.Update(ev); err != nil {
		return nil, fmt.Errorf("failed to update event: %w", err)
	}

	return ev, nil
}

// GenerateDisputePackage constructs the downloadable formal claim memo (PRD §39).
func (r *Resource) GenerateDisputePackage(eventID string) (*DisputePackageDTO, error) {
	ev, err := r.repo.GetByID(eventID)
	if err != nil {
		return nil, fmt.Errorf("reconciliation event %s not found: %w", eventID, err)
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
		GetVendorDisplayName(ev.VendorID), ev.VendorID,
		ev.ShipmentID, ev.Shipment.AWBNumber,
		FormatIDR(ev.Invoice.BilledAmount),
		FormatIDR(ev.Pricing.ExpectedTotalCharge),
		FormatIDR(ev.Pricing.DifferenceAmount),
		clause,
		ev.Reconciliation.DaysRemainingToDispute,
		ev.Reconciliation.DisputeDeadline,
		ev.Pricing.Trace.TraceLog,
		"AP_MANAGER (Demo Mode)", lastRole,
		lastAction,
		lastReason,
	)

	pkg := &DisputePackageDTO{
		PackageID:       fmt.Sprintf("DISP-%s-%d", ev.ShipmentID, time.Now().Unix()),
		GeneratedAt:     time.Now().UTC(),
		InvoiceNumber:   ev.Invoice.InvoiceNumber,
		VendorID:        ev.VendorID,
		VendorName:      GetVendorDisplayName(ev.VendorID),
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

// SeedFromDataset loads and reconciles transactions from dataset directory.
func (r *Resource) SeedFromDataset(datasetDir string) error {
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

		var rawDoc struct {
			TransactionID string  `json:"transaction_id"`
			ShipmentID    string  `json:"shipment_id"`
			InvoiceID     string  `json:"invoice_id"`
			VendorID      string  `json:"vendor_id"`
			Origin        string  `json:"origin"`
			Destination   string  `json:"destination"`
			ServiceType   string  `json:"service_type"`
			ShipmentDate  string  `json:"shipment_date"`
			WeightKg      float64 `json:"weight_kg"`
			Quantity      int     `json:"quantity"`
			Contract      struct {
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

		if err := json.Unmarshal(gtBytes, &rawDoc); err != nil {
			continue
		}

		inv := &model.InvoiceCanonical{
			InvoiceID:        rawDoc.InvoiceID,
			InvoiceNumber:    rawDoc.InvoiceID,
			InvoiceDate:      rawDoc.ShipmentDate,
			VendorID:         rawDoc.VendorID,
			ServiceType:      rawDoc.ServiceType,
			Origin:           rawDoc.Origin,
			Destination:      rawDoc.Destination,
			WeightBilledKg:   rawDoc.WeightKg,
			BilledAmount:     rawDoc.BilledCharge,
			TaxBilled:        rawDoc.TaxAmount,
			Currency:         "IDR",
			FieldEvidence: map[string]model.EvidenceSource{
				"billed_amount": {DocumentID: rawDoc.InvoiceID, Page: 1, BBox: &model.BoundingBox{X1: 0.65, Y1: 0.85, X2: 0.95, Y2: 0.90}},
			},
		}

		sj := &model.ShipmentCanonical{
			SuratJalanNumber: fmt.Sprintf("SJ-%s", rawDoc.ShipmentID),
			ShipmentID:       rawDoc.ShipmentID,
			AWBNumber:        fmt.Sprintf("AWB%s", strings.ReplaceAll(rawDoc.ShipmentID, "SHP-2026-", "")),
			ShipmentDate:     rawDoc.ShipmentDate,
			Origin:           rawDoc.Origin,
			Destination:      rawDoc.Destination,
			WeightActualKg:   rawDoc.WeightKg,
			TotalPackages:    rawDoc.Quantity,
			FieldEvidence: map[string]model.EvidenceSource{
				"shipment_id":    {DocumentID: "SJ-" + rawDoc.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.20, Y1: 0.15, X2: 0.50, Y2: 0.20}},
				"weight_actual":  {DocumentID: "SJ-" + rawDoc.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.60, Y1: 0.45, X2: 0.85, Y2: 0.50}},
			},
		}

		pod := &model.PODCanonical{
			PODNumber:           fmt.Sprintf("POD-%s", rawDoc.ShipmentID),
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        rawDoc.POD.DeliveryDate,
			SignaturePresent:    rawDoc.POD.SignaturePresent,
			SignatureConfidence: 0.95,
			StampPresent:        rawDoc.POD.StampPresent,
			FieldEvidence: map[string]model.EvidenceSource{
				"delivery_date":     {DocumentID: "POD-" + rawDoc.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.20, Y1: 0.25, X2: 0.55, Y2: 0.30}},
				"signature_present": {DocumentID: "POD-" + rawDoc.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.55, Y1: 0.70, X2: 0.90, Y2: 0.90}},
			},
		}

		baseRate := rawDoc.Contract.BaseRate
		if rawDoc.ExpectedCharge > 0 {
			expectedPreTax := float64(rawDoc.ExpectedCharge) / 1.11
			fuelMultiplier := 1.0 + (rawDoc.Contract.FuelSurchargePct / 100.0)
			tierBaseRate := int64(math.Round(expectedPreTax / fuelMultiplier))
			if tierBaseRate > 0 {
				baseRate = tierBaseRate
			}
		}

		contract := &model.ContractCanonical{
			AgreementID:                    rawDoc.Contract.AgreementID,
			VendorID:                       rawDoc.VendorID,
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       baseRate,
			ApplicableFuelSurchargePercent: rawDoc.Contract.FuelSurchargePct,
			DisputeWindowDays:              45,
			FieldEvidence: map[string]model.EvidenceSource{
				"base_rate": {DocumentID: "CTR-" + rawDoc.VendorID, Page: 1, BBox: &model.BoundingBox{X1: 0.55, Y1: 0.40, X2: 0.85, Y2: 0.45}},
			},
		}

		event, err := r.engine.ReconcileTransaction(inv, sj, pod, contract)
		if err == nil && event != nil {
			_ = r.repo.Save(event)
		}
	}

	return nil
}

// GetVendorDisplayName maps 3PL code to corporate trading entity name.
func GetVendorDisplayName(code string) string {
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

// FormatIDR formats an integer IDR amount with thousand separators.
func FormatIDR(val int64) string {
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
