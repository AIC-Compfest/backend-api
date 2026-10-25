package reconciliation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"backend-api/internal/model"
	serviceRecon "backend-api/internal/service/reconciliation"
	"backend-api/internal/storage"
)

// Resource orchestrates reconciliation logic, queue prioritization, and dispute management
type Resource struct {
	db      *ResourceDB
	engine  *serviceRecon.Engine
	storage storage.Storage
}

// NewResource creates a reconciliation Resource instance
func NewResource(db *ResourceDB, engine *serviceRecon.Engine) *Resource {
	if engine == nil {
		engine = serviceRecon.NewEngine()
	}
	return &Resource{
		db:     db,
		engine: engine,
	}
}

// SetStorage sets the storage provider for persisting dispute packages
func (r *Resource) SetStorage(store storage.Storage) {
	r.storage = store
}

// Reconcile executes 4-way matching and persists the result
func (r *Resource) Reconcile(ctx context.Context, inv *model.InvoiceCanonical, shp *model.ShipmentCanonical, pod *model.PODCanonical, ctr *model.ContractCanonical) (*model.ShipmentEvent, error) {
	event, err := r.engine.ReconcileTransaction(inv, shp, pod, ctr)
	if err != nil {
		return nil, fmt.Errorf("reconciliation engine error: %w", err)
	}

	if err := r.db.SaveEvent(ctx, event); err != nil {
		return nil, fmt.Errorf("failed to persist event: %w", err)
	}

	return event, nil
}

// GetQueue retrieves filtered and paginated queue items
func (r *Resource) GetQueue(ctx context.Context, filter QueueFilter) (*QueueResponseDTO, error) {
	events, err := r.db.ListAllEvents(ctx)
	if err != nil {
		return nil, err
	}

	var items []QueueItemDTO
	summary := QueueSummaryDTO{}

	// Support comma-separated statuses: e.g. "EXCEPTION,DUPLICATE,INSUFFICIENT_EVIDENCE"
	allowedStatuses := map[string]bool{}
	if filter.Status != "" {
		for _, s := range strings.Split(filter.Status, ",") {
			allowedStatuses[strings.ToUpper(strings.TrimSpace(s))] = true
		}
	}

	for _, ev := range events {
		billed := float64(ev.Invoice.BilledAmount)
		expected := float64(ev.Pricing.ExpectedTotalCharge)
		variance := float64(ev.Pricing.DifferenceAmount)

		topDiscrepancy := ""
		severity := "LOW"
		if len(ev.Reconciliation.Discrepancies) > 0 {
			topDiscrepancy = fmt.Sprintf("[%s] %s", ev.Reconciliation.Discrepancies[0].Code, ev.Reconciliation.Discrepancies[0].Description)
			severity = string(ev.Reconciliation.Discrepancies[0].Severity)
		}

		statusStr := string(ev.Reconciliation.Status)

		// Apply Filters — supports comma-separated statuses
		if len(allowedStatuses) > 0 && !allowedStatuses[strings.ToUpper(statusStr)] {
			continue
		}
		if filter.Severity != "" && !strings.EqualFold(severity, filter.Severity) {
			continue
		}
		if filter.Vendor != "" && !strings.Contains(strings.ToLower(ev.VendorID), strings.ToLower(filter.Vendor)) {
			continue
		}
		if filter.Search != "" {
			query := strings.ToLower(filter.Search)
			match := strings.Contains(strings.ToLower(ev.EventID), query) ||
				strings.Contains(strings.ToLower(ev.ShipmentID), query) ||
				strings.Contains(strings.ToLower(ev.Invoice.InvoiceNumber), query) ||
				strings.Contains(strings.ToLower(ev.VendorID), query)
			if !match {
				continue
			}
		}

		// Accumulate summary ONLY on items present in this filtered queue page
		summary.TotalItems++
		summary.TotalInvoicesCount++
		summary.TotalBilledIDR += billed
		summary.TotalInvoicedAmount += billed
		summary.TotalVarianceIDR += variance
		summary.TotalVarianceAmount += variance

		switch statusStr {
		case "MATCH":
			summary.CleanMatchesCount++
			summary.MatchesCount++
		case "EXCEPTION", "DUPLICATE", "INSUFFICIENT_EVIDENCE":
			summary.OpenExceptionsCount++
			summary.UrgentDisputesCount++
		default:
			summary.ResolvedCount++
			if variance > 0 {
				summary.LeakagePreventedIDR += variance
			}
		}

		createdAtStr := ev.Invoice.InvoiceDate
		if createdAtStr == "" {
			createdAtStr = time.Now().Format("2006-01-02")
		}

		vendorName := r.db.GetVendorName(ctx, ev.VendorID)
		// Prefer ingest_batch_id already on the event row (canonical schema); fall back to document lookup.
		ingestBatchID := ev.IngestBatchID
		if ingestBatchID == "" {
			ingestBatchID = r.db.GetDocumentBatchID(ctx, ev.Invoice.DocumentID)
		}

		items = append(items, QueueItemDTO{
			EventID:        ev.EventID,
			TransactionID:  ev.EventID,
			ShipmentID:     ev.ShipmentID,
			InvoiceNumber:  ev.Invoice.InvoiceNumber,
			VendorID:       ev.VendorID,
			VendorName:     vendorName,
			BilledAmount:   billed,
			ExpectedAmount: expected,
			VarianceAmount: variance,
			MatchScore:     ev.MatchConfidence,
			PriorityScore:  ev.Reconciliation.PriorityScore,
			Status:             statusStr,
			Severity:           severity,
			PrimaryDiscrepancy: topDiscrepancy,
			TopDiscrepancy:     topDiscrepancy,
			CreatedAt:          createdAtStr,
			IngestBatchID:      ingestBatchID,
		})
	}

	// Sort by PriorityScore descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].PriorityScore > items[j].PriorityScore
	})

	total := int64(len(items))
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 100
	}

	offset := (page - 1) * pageSize
	totalPages := int(total) / pageSize
	if int(total)%pageSize != 0 || totalPages == 0 {
		totalPages++
	}

	paginatedItems := []QueueItemDTO{}
	if offset < len(items) {
		end := offset + pageSize
		if end > len(items) {
			end = len(items)
		}
		paginatedItems = items[offset:end]
	}

	return &QueueResponseDTO{
		Summary:    summary,
		Items:      paginatedItems,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// GetDetail retrieves full shipment event details
func (r *Resource) GetDetail(ctx context.Context, id string) (*model.ShipmentEvent, error) {
	return r.db.GetEventByID(ctx, id)
}

// SubmitDecision records a human review decision and updates case status.
// SoD policy: ALL roles (including AP_MANAGER) approve directly to APPROVED.
// Finance Controller final approval via FINAL_APPROVAL page also sets APPROVED.
func (r *Resource) SubmitDecision(ctx context.Context, eventID string, req DecisionRequest) (*DecisionResponse, error) {
	ev, err := r.db.GetEventByID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("reconciliation event '%s' not found: %w", eventID, err)
	}

	prevStatus := string(ev.Reconciliation.Status)
	action := strings.ToUpper(strings.TrimSpace(req.Decision))
	if action == "" {
		action = strings.ToUpper(strings.TrimSpace(req.Action))
	}

	if (action == "DISPUTE" || action == "REJECT") && strings.TrimSpace(req.Reason) == "" {
		return nil, fmt.Errorf("written reason is mandatory for %s action", action)
	}

	role := strings.ToUpper(strings.TrimSpace(req.Role))
	if role == "" {
		role = strings.ToUpper(strings.TrimSpace(req.ReviewerRole))
	}
	if role == "" {
		role = "AP_MANAGER"
	}
	validRoles := map[string]bool{
		"AP_MANAGER":         true,
		"FINANCE_CONTROLLER": true,
		"LOGISTICS_MANAGER":  true,
		"AUDITOR":            true,
		"ADMIN":              true,
	}
	if !validRoles[role] {
		return nil, fmt.Errorf("invalid reviewer role '%s'. Allowed: AP_MANAGER, FINANCE_CONTROLLER, LOGISTICS_MANAGER, AUDITOR", req.Role)
	}

	var newStatus string
	switch action {
	case "RELEASE":
		if err := r.db.ReleaseEvent(ctx, eventID, req.ReviewerID, role, req.Reason); err != nil {
			return nil, fmt.Errorf("failed to release event: %w", err)
		}
		now := time.Now().UTC()
		return &DecisionResponse{
			EventID:        eventID,
			PreviousStatus: prevStatus,
			NewStatus:      "RESOLVED",
			Decision: model.AuditTrailItem{
				Timestamp:      now,
				Action:         "RELEASE",
				ActorID:        req.ReviewerID,
				ActorRole:      role,
				PreviousStatus: prevStatus,
				NewStatus:      "RESOLVED",
				Notes:          req.Reason,
			},
			AuditLogID: model.NewUUID(),
			Timestamp:  now,
		}, nil
	case "APPROVE":
		// All roles: direct APPROVED (SOD enforced at page level, not status level)
		newStatus = "APPROVED"
	case "REJECT":
		newStatus = "REJECTED"
	case "DISPUTE":
		newStatus = "DISPUTED"
	case "OVERRIDE":
		newStatus = "APPROVED"
	default:
		return nil, fmt.Errorf("unsupported decision action '%s'. Allowed: APPROVE, REJECT, DISPUTE, OVERRIDE, RELEASE", action)
	}

	now := time.Now().UTC()
	auditItem := model.AuditTrailItem{
		Timestamp:      now,
		Action:         action,
		ActorID:        req.ReviewerID,
		ActorRole:      role,
		PreviousStatus: prevStatus,
		NewStatus:      newStatus,
		Notes:          req.Reason,
	}

	auditEntry, err := r.db.SaveDecision(ctx, eventID, auditItem, prevStatus, newStatus)
	if err != nil {
		return nil, fmt.Errorf("failed to record decision: %w", err)
	}

	return &DecisionResponse{
		EventID:        eventID,
		PreviousStatus: prevStatus,
		NewStatus:      newStatus,
		Decision:       *auditEntry,
		AuditLogID:     model.NewUUID(),
		Timestamp:      now,
	}, nil
}

// GetVendors returns list of all active vendors from the DB
func (r *Resource) GetVendors(ctx context.Context) ([]map[string]interface{}, error) {
	return r.db.ListVendors(ctx)
}

// GetAllAuditLogs returns all audit events from the DB for the global audit ledger
func (r *Resource) GetAllAuditLogs(ctx context.Context) ([]model.AuditTrailItem, error) {
	return r.db.ListAuditLogs(ctx, "")
}

// GetDisputePackage generates an official dispute claim package citing contractual clauses
func (r *Resource) GetDisputePackage(ctx context.Context, eventID string, reviewerID string) (*DisputePackageDTO, error) {
	ev, err := r.db.GetEventByID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("event not found: %w", err)
	}

	memo := r.formatDisputeMemo(ev, reviewerID)
	vendorName := r.db.GetVendorName(ctx, ev.VendorID)

	pkg := &DisputePackageDTO{
		EventID:          ev.EventID,
		TransactionID:    ev.EventID,
		VendorName:       vendorName,
		VendorID:         ev.VendorID,
		InvoiceNumber:    ev.Invoice.InvoiceNumber,
		ShipmentID:       ev.ShipmentID,
		BilledAmount:     float64(ev.Invoice.BilledAmount),
		ExpectedAmount:   float64(ev.Pricing.ExpectedTotalCharge),
		DisputedAmount:   float64(ev.Pricing.DifferenceAmount),
		GeneratedAt:      time.Now().UTC(),
		GeneratedBy:      reviewerID,
		DiscrepancyItems: ev.Reconciliation.Discrepancies,
		FormattedMemo:    memo,
	}

	// Persist dispute package artifacts to Supabase Storage (dispute-package bucket)
	if r.storage != nil {
		memoPath := fmt.Sprintf("%s/memo.txt", ev.EventID)
		jsonPath := fmt.Sprintf("%s/dispute.json", ev.EventID)
		_, _ = r.storage.Save(ctx, "dispute-package", memoPath, strings.NewReader(memo))
		if pkgBytes, err := json.MarshalIndent(pkg, "", "  "); err == nil {
			_, _ = r.storage.Save(ctx, "dispute-package", jsonPath, bytes.NewReader(pkgBytes))
		}
	}

	return pkg, nil
}

func (r *Resource) formatDisputeMemo(ev *model.ShipmentEvent, reviewer string) string {
	var sb strings.Builder
	sb.WriteString("================================================================================\n")
	sb.WriteString("              SURAT SANGGAHAN TAGIHAN LOGISTIK 3PL (OFFICIAL DISPUTE MEMO)      \n")
	sb.WriteString("================================================================================\n\n")
	sb.WriteString(fmt.Sprintf("NOMOR KASUS     : %s\n", ev.EventID))
	sb.WriteString(fmt.Sprintf("TANGGAL KLAIM   : %s\n", time.Now().Format("02 January 2006")))
	sb.WriteString(fmt.Sprintf("VENDOR 3PL      : %s\n", ev.VendorID))
	sb.WriteString(fmt.Sprintf("NOMOR INVOICE   : %s\n", ev.Invoice.InvoiceNumber))
	sb.WriteString(fmt.Sprintf("NOMOR SHIPMENT  : %s\n", ev.ShipmentID))
	sb.WriteString(fmt.Sprintf("AUDITOR / AP    : %s\n\n", reviewer))
	sb.WriteString("--------------------------------------------------------------------------------\n")
	sb.WriteString("RINCIAN REKONSILIASI KEUANGAN:\n")
	sb.WriteString(fmt.Sprintf(" - Total Ditagihkan Vendor : Rp %s\n", formatIDR(float64(ev.Invoice.BilledAmount))))
	sb.WriteString(fmt.Sprintf(" - Total Seharusnya (PKS)  : Rp %s\n", formatIDR(float64(ev.Pricing.ExpectedTotalCharge))))
	sb.WriteString(fmt.Sprintf(" - NILAI SELISIH SANGGAHAN : Rp %s\n\n", formatIDR(float64(ev.Pricing.DifferenceAmount))))
	sb.WriteString("DASAR PERHITUNGAN SISTEM:\n")
	sb.WriteString(fmt.Sprintf(" %s\n\n", ev.Pricing.Trace.TraceLog))
	sb.WriteString("DAFTAR TEMUAN DISKREPANSI:\n")
	for i, d := range ev.Reconciliation.Discrepancies {
		delta := int64(0)
		if d.DeltaAmount != nil {
			delta = *d.DeltaAmount
		}
		sb.WriteString(fmt.Sprintf(" %d. [%s] %s (Dampak Finansial: Rp %s)\n", i+1, d.Code, d.Description, formatIDR(float64(delta))))
	}
	sb.WriteString("\n--------------------------------------------------------------------------------\n")
	sb.WriteString("Permohonan: Mohon dilakukan penyesuaian (Credit Note / Invoice Revision) sejumlah nilai selisih.\n")
	sb.WriteString("================================================================================\n")
	return sb.String()
}

func formatIDR(val float64) string {
	s := fmt.Sprintf("%.0f", val)
	var res []rune
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			res = append(res, '.')
		}
		res = append(res, c)
	}
	return string(res)
}
