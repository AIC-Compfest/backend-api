package reconciliation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend-api/internal/database"
	"backend-api/internal/model"
	serviceRecon "backend-api/internal/service/reconciliation"
)

// ResourceDB handles database persistence for reconciliation events, cases, decisions, and audit logs
type ResourceDB struct {
	dbClient *database.Client
	mu       sync.RWMutex
	events   map[string]*model.ShipmentEvent
	audit    []model.AuditTrailItem
}

func (r *ResourceDB) GetDocumentBatchID(ctx context.Context, documentID string) string {
	if r.dbClient == nil || documentID == "" {
		return ""
	}
	var rows []map[string]interface{}
	params := url.Values{}
	params.Set("id", "eq."+documentID)
	params.Set("select", "ingest_batch_id")
	params.Set("limit", "1")
	if err := r.dbClient.Query(ctx, "documents", params, &rows); err != nil || len(rows) == 0 {
		return ""
	}
	batchID, _ := rows[0]["ingest_batch_id"].(string)
	return batchID
}

// NewResourceDB creates a new reconciliation ResourceDB
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		events:   make(map[string]*model.ShipmentEvent),
		audit:    make([]model.AuditTrailItem, 0),
	}
}

// GetVendorName queries vendors table first, then rate_agreements as fallback
func (r *ResourceDB) GetVendorName(ctx context.Context, vendorID string) string {
	if vendorID == "" {
		return "Vendor 3PL"
	}
	if r.dbClient != nil {
		// Try vendors table first (canonical master data)
		var vendors []map[string]interface{}
		vParams := url.Values{}
		vParams.Set("vendor_id", "eq."+vendorID)
		vParams.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "vendors", vParams, &vendors); err == nil && len(vendors) > 0 {
			if name, ok := vendors[0]["vendor_name"].(string); ok && name != "" {
				return name
			}
		}
		// Fallback: rate_agreements table
		var agreements []map[string]interface{}
		params := url.Values{}
		params.Set("vendor_id", "eq."+vendorID)
		params.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "rate_agreements", params, &agreements); err == nil && len(agreements) > 0 {
			if name, ok := agreements[0]["vendor_name"].(string); ok && name != "" {
				return name
			}
		}
	}

	// Do not manufacture a partner name when master data is unavailable.  The
	// vendor identifier remains the truthful, auditable display value.
	return vendorID
}

// ListVendors returns all active vendors from the vendors table (strictly single source of truth from DB)
func (r *ResourceDB) ListVendors(ctx context.Context) ([]map[string]interface{}, error) {
	if r.dbClient != nil {
		var rows []map[string]interface{}
		params := url.Values{}
		params.Set("is_active", "eq.true")
		params.Set("order", "vendor_name.asc")
		if err := r.dbClient.Query(ctx, "vendors", params, &rows); err == nil {
			return rows, nil
		}
		// Fallback to rate_agreements
		var raRows []map[string]interface{}
		raParams := url.Values{}
		raParams.Set("select", "vendor_id,vendor_name")
		raParams.Set("order", "vendor_name.asc")
		if err := r.dbClient.Query(ctx, "rate_agreements", raParams, &raRows); err == nil {
			return raRows, nil
		}
	}
	return []map[string]interface{}{}, nil
}

// EnsureVendorExists checks if vendor_id exists in vendors table, if not automatically creates it
func (r *ResourceDB) EnsureVendorExists(ctx context.Context, vendorID, vendorName string) {
	if vendorID == "" || r.dbClient == nil {
		return
	}
	if vendorName == "" {
		vendorName = vendorID
	}

	var existing []map[string]interface{}
	params := url.Values{}
	params.Set("vendor_id", "eq."+vendorID)
	params.Set("limit", "1")
	if err := r.dbClient.Query(ctx, "vendors", params, &existing); err == nil && len(existing) > 0 {
		return
	}

	now := time.Now().UTC()
	payload := map[string]interface{}{
		"id":          model.NewUUID(),
		"vendor_id":   vendorID,
		"vendor_name": vendorName,
		"is_active":   true,
		"created_at":  now,
		"updated_at":  now,
	}
	var out []map[string]interface{}
	_ = r.dbClient.Insert(ctx, "vendors", payload, &out)
	fmt.Printf("[Auto-Vendor Pipeline] Registered new vendor in DB: %s (%s)\n", vendorID, vendorName)
}

// loadFieldEvidence queries field_evidence table for a given document_id and
// returns a map of field_name → EvidenceSource with real BBox from OCR/PP-Layout.
func (r *ResourceDB) loadFieldEvidence(ctx context.Context, documentID string) map[string]model.EvidenceSource {
	evMap := make(map[string]model.EvidenceSource)

	if r.dbClient != nil && documentID != "" {
		var rows []map[string]interface{}
		params := url.Values{}
		params.Set("document_id", "eq."+documentID)
		params.Set("order", "page_number.asc")
		params.Set("limit", "2000")

		if err := r.dbClient.Query(ctx, "field_evidence", params, &rows); err == nil && len(rows) > 0 {
			for _, row := range rows {
				fieldName, _ := row["field_name"].(string)
				if fieldName == "" {
					continue
				}
				page := 1
				if pv, ok := row["page_number"].(float64); ok {
					page = int(pv)
				}
				ev := model.EvidenceSource{
					DocumentID: documentID,
					Page:       page,
				}
				// Extract bbox coordinates (stored as normalized [0,1] floats)
				bx1, hasBx1 := row["bbox_x1"].(float64)
				by1, hasBY1 := row["bbox_y1"].(float64)
				bx2, hasBx2 := row["bbox_x2"].(float64)
				by2, hasBY2 := row["bbox_y2"].(float64)
				if hasBx1 && hasBY1 && hasBx2 && hasBY2 && bx2 > bx1 && by2 > by1 {
					ev.BBox = &model.BoundingBox{
						X1: bx1,
						Y1: by1,
						X2: bx2,
						Y2: by2,
					}
				}
				// Never let a bbox-less row (e.g. empty value) shadow a located one.
				if prev, ok := evMap[fieldName]; ok && prev.BBox != nil && ev.BBox == nil {
					continue
				}
				evMap[fieldName] = ev
			}
		}
	}

	return evMap
}

// SeedFromPilotDataset boots up pilot benchmark dataset into memory cache
func (r *ResourceDB) SeedFromPilotDataset(datasetDir string) error {
	txnsDir := filepath.Join(datasetDir, "transactions")
	entries, err := os.ReadDir(txnsDir)
	if err != nil {
		return err
	}

	engine := serviceRecon.NewEngine()

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

		if err := json.Unmarshal(gtBytes, &gt); err != nil {
			continue
		}

		inv := &model.InvoiceCanonical{
			InvoiceID:      gt.InvoiceID,
			InvoiceNumber:  gt.InvoiceID,
			InvoiceDate:    gt.ShipmentDate,
			VendorID:       gt.VendorID,
			ServiceType:    gt.ServiceType,
			Origin:         gt.Origin,
			Destination:    gt.Destination,
			WeightBilledKg: gt.WeightKg,
			BilledAmount:   gt.BilledCharge,
			TaxBilled:      gt.TaxAmount,
			Currency:       "IDR",
			FieldEvidence: map[string]model.EvidenceSource{
				"billed_amount": {DocumentID: gt.InvoiceID, Page: 1, BBox: &model.BoundingBox{X1: 0.65, Y1: 0.85, X2: 0.95, Y2: 0.90}},
			},
		}

		shp := &model.ShipmentCanonical{
			SuratJalanNumber: "SJ-" + gt.ShipmentID,
			ShipmentID:       gt.ShipmentID,
			AWBNumber:        gt.ShipmentID,
			ServiceType:      gt.ServiceType,
			ShipmentDate:     gt.ShipmentDate,
			Origin:           gt.Origin,
			Destination:      gt.Destination,
			WeightActualKg:   gt.WeightKg,
			TotalPackages:    gt.Quantity,
			FieldEvidence: map[string]model.EvidenceSource{
				"weight_actual_kg": {DocumentID: gt.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.20, Y1: 0.40, X2: 0.45, Y2: 0.45}},
			},
		}

		deliveryStatus := "DELIVERED"
		if !gt.POD.Delivered {
			deliveryStatus = "FAILED"
		}

		pod := &model.PODCanonical{
			PODNumber:           "POD-" + gt.ShipmentID,
			ShipmentID:          gt.ShipmentID,
			AWBNumber:           gt.ShipmentID,
			DeliveryStatus:      deliveryStatus,
			DeliveryDate:        gt.POD.DeliveryDate,
			ReceiverName:        "Gudang Receiving",
			SignaturePresent:    gt.POD.SignaturePresent,
			SignatureConfidence: 0.95,
			StampPresent:        gt.POD.StampPresent,
			FieldEvidence: map[string]model.EvidenceSource{
				"signature": {DocumentID: "POD-" + gt.ShipmentID, Page: 1, BBox: &model.BoundingBox{X1: 0.60, Y1: 0.70, X2: 0.85, Y2: 0.85}},
			},
		}

		ctr := &model.ContractCanonical{
			AgreementID:                    gt.Contract.AgreementID,
			VendorID:                       gt.VendorID,
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_KG",
			BaseRate:                       gt.Contract.BaseRate,
			ApplicableFuelSurchargePercent: gt.Contract.FuelSurchargePct,
			MinimumCharge:                  0,
			DisputeWindowDays:              30,
			RateMatrix: []model.RateMatrixEntry{
				{
					OriginZone:      gt.Origin,
					DestinationZone: gt.Destination,
					ServiceType:     gt.ServiceType,
					BaseRate:        gt.Contract.BaseRate,
					RateType:        "PER_KG",
				},
			},
			FieldEvidence: map[string]model.EvidenceSource{
				"base_rate": {DocumentID: gt.Contract.AgreementID, Page: 1, BBox: &model.BoundingBox{X1: 0.10, Y1: 0.30, X2: 0.50, Y2: 0.35}},
			},
		}

		event, err := engine.ReconcileTransaction(inv, shp, pod, ctr)
		if err == nil && event != nil {
			_ = r.SaveEvent(context.Background(), event)
		}
	}
	return nil
}

// SaveEvent persists the canonical shipment, event, and review case projection.
func (r *ResourceDB) SaveEvent(ctx context.Context, event *model.ShipmentEvent) error {
	if event == nil || event.EventID == "" {
		return fmt.Errorf("invalid shipment event payload")
	}
	if r.dbClient != nil {
		vendorID, err := r.ensureCanonicalVendor(ctx, event.VendorID, event.Invoice.VendorName)
		if err != nil {
			return err
		}
		shipmentID, err := r.ensureCanonicalShipment(ctx, event, vendorID)
		if err != nil {
			return err
		}
		status := canonicalEventStatus(event.Reconciliation.Status)
		payload := map[string]interface{}{
			"id": event.EventID, "shipment_id": shipmentID, "event_type": "RECONCILIATION",
			"status": status, "expected_amount": event.Pricing.ExpectedTotalCharge,
			"billed_amount": event.Invoice.BilledAmount, "variance_amount": event.Pricing.DifferenceAmount,
			"calculation_trace": map[string]interface{}{
				"pricing": event.Pricing, "checks": event.Checks, "match_method": event.MatchMethod,
				"match_confidence": event.MatchConfidence,
			},
			"requires_human_review": event.Reconciliation.RequiresHumanReview,
			"is_released":           false, "created_at": time.Now().UTC(),
		}
		if event.IngestBatchID != "" {
			payload["ingest_batch_id"] = event.IngestBatchID
		}
		params := url.Values{"id": []string{"eq." + event.EventID}}
		var existing []map[string]interface{}
		if err := r.dbClient.Query(ctx, "shipment_events", params, &existing); err != nil {
			return fmt.Errorf("check shipment event: %w", err)
		}
		var saved []map[string]interface{}
		if len(existing) > 0 {
			if err := r.dbClient.Update(ctx, "shipment_events", params, payload, &saved); err != nil {
				return fmt.Errorf("update shipment event: %w", err)
			}
		} else if err := r.dbClient.Insert(ctx, "shipment_events", payload, &saved); err != nil {
			return fmt.Errorf("insert shipment event: %w", err)
		}
		r.EnsureReconciliationCase(ctx, event.EventID, event)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.EventID] = event
	return nil
}

func (r *ResourceDB) ensureCanonicalVendor(ctx context.Context, vendorID, vendorName string) (interface{}, error) {
	if vendorID == "" {
		return nil, nil
	}
	var rows []map[string]interface{}
	params := url.Values{"vendor_id": []string{"eq." + vendorID}, "limit": []string{"1"}}
	if err := r.dbClient.Query(ctx, "vendors", params, &rows); err != nil {
		return nil, fmt.Errorf("lookup vendor: %w", err)
	}
	if len(rows) > 0 {
		return rows[0]["id"], nil
	}
	if vendorName == "" {
		vendorName = vendorID
	}
	payload := map[string]interface{}{"id": model.NewUUID(), "vendor_id": vendorID, "vendor_name": vendorName, "is_active": true}
	if err := r.dbClient.Insert(ctx, "vendors", payload, &rows); err != nil {
		return nil, fmt.Errorf("insert vendor: %w", err)
	}
	return payload["id"], nil
}

func (r *ResourceDB) ensureCanonicalShipment(ctx context.Context, event *model.ShipmentEvent, vendorID interface{}) (string, error) {
	number := event.Shipment.ShipmentID
	if number == "" {
		number = event.ShipmentID
	}
	if number == "" {
		return "", fmt.Errorf("shipment event has no shipment number")
	}
	normalized := strings.ToUpper(strings.TrimSpace(number))
	params := url.Values{"normalized_shipment_number": []string{"eq." + normalized}, "limit": []string{"1"}}
	if vendorID != nil {
		params.Set("vendor_id", "eq."+fmt.Sprint(vendorID))
	}
	var rows []map[string]interface{}
	if err := r.dbClient.Query(ctx, "shipments", params, &rows); err != nil {
		return "", fmt.Errorf("lookup shipment: %w", err)
	}
	if len(rows) > 0 {
		return fmt.Sprint(rows[0]["id"]), nil
	}
	payload := map[string]interface{}{
		"id": model.NewUUID(), "shipment_number": number, "normalized_shipment_number": normalized,
		"vendor_id": vendorID, "origin": event.Shipment.Origin, "destination": event.Shipment.Destination,
		"shipment_date": nullableDate(event.Shipment.ShipmentDate),
	}
	if err := r.dbClient.Insert(ctx, "shipments", payload, &rows); err != nil {
		return "", fmt.Errorf("insert shipment: %w", err)
	}
	return fmt.Sprint(payload["id"]), nil
}

func canonicalEventStatus(status model.ReconciliationStatus) string {
	switch status {
	case model.StatusMatch:
		return "MATCH"
	case model.StatusApproved:
		return "APPROVED"
	case model.StatusDisputed:
		return "DISPUTED"
	case model.StatusRejected:
		return "REJECTED"
	case model.StatusResolved:
		return "RESOLVED"
	case model.StatusInsufficientEvidence:
		return "INSUFFICIENT_EVIDENCE"
	default:
		return "EXCEPTION"
	}
}

func nullableDate(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return nil
	}
	return value
}

func nullableUUID(value string) interface{} {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return nil
	}
	return value
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func floatValue(value interface{}) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case int64:
		return float64(number)
	default:
		return 0
	}
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}

// EnsureReconciliationCase auto-creates a reconciliation_case if one does not exist for this event.
func (r *ResourceDB) EnsureReconciliationCase(ctx context.Context, eventUUID string, event *model.ShipmentEvent) {
	if r.dbClient == nil || eventUUID == "" || event == nil {
		return
	}

	// Check if case already exists
	var existing []map[string]interface{}
	pCheck := url.Values{}
	pCheck.Set("event_id", "eq."+eventUUID)
	pCheck.Set("limit", "1")
	if err := r.dbClient.Query(ctx, "reconciliation_cases", pCheck, &existing); err == nil && len(existing) > 0 {
		return // Case already exists
	}

	caseStatus := "OPEN"
	switch event.Reconciliation.Status {
	case model.StatusMatch:
		caseStatus = "RESOLVED"
	case model.StatusApproved:
		caseStatus = "APPROVED"
	case model.StatusRejected:
		caseStatus = "REJECTED"
	case model.StatusDisputed:
		caseStatus = "DISPUTED"
	case model.StatusResolved:
		caseStatus = "RESOLVED"
	}

	now := time.Now().UTC()
	casePayload := map[string]interface{}{
		"id":                 model.NewUUID(),
		"case_number":        "CASE-" + strings.ToUpper(eventUUID[:8]),
		"event_id":           eventUUID,
		"status":             caseStatus,
		"priority_score":     event.Reconciliation.PriorityScore,
		"financial_exposure": event.Pricing.DifferenceAmount,
		"created_at":         now,
		"updated_at":         now,
	}
	var cOut []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "reconciliation_cases", casePayload, &cOut); err != nil {
		fmt.Printf("[ReconciliationCase] Failed to auto-create case for event %s: %v\n", eventUUID, err)
	} else {
		fmt.Printf("[ReconciliationCase] Auto-created case for event %s (status=%s)\n", eventUUID, caseStatus)
	}
}

// rowToShipmentEvent converts a Supabase shipment_events row to model.ShipmentEvent with domain table lookups
func (r *ResourceDB) rowToShipmentEvent(ctx context.Context, row map[string]interface{}) *model.ShipmentEvent {
	// The canonical schema stores the reconciliation projection directly on the
	// event row. Keep reads independent from the removed document-specific tables.
	evID := stringValue(row["id"])
	if evID == "" {
		return nil
	}
	status := model.ReconciliationStatus(stringValue(row["status"]))
	expected := int64(floatValue(row["expected_amount"]))
	billed := int64(floatValue(row["billed_amount"]))
	variance := int64(floatValue(row["variance_amount"]))
	if canonicalSchemaRead(row) {
		return &model.ShipmentEvent{
			EventID:       evID,
			ShipmentID:    stringValue(row["shipment_id"]),
			IngestBatchID: stringValue(row["ingest_batch_id"]),
			Reconciliation: model.ReconciliationDecision{
				Status:              status,
				RequiresHumanReview: boolValue(row["requires_human_review"]),
			},
			Pricing: model.PricingResult{
				ExpectedTotalCharge: expected,
				DifferenceAmount:    variance,
			},
			Invoice: model.InvoiceCanonical{BilledAmount: billed},
		}
	}

	/*
		evID, _ := row["event_id"].(string)
		if evID == "" {
			evID, _ = row["id"].(string)
		}

		if evID == "" {
			return nil
		}
	*/

	statusStr, _ := row["status"].(string)
	shpID, _ := row["shipment_id"].(string)
	if shpID == "" {
		shpID = evID
	}
	vendorID, _ := row["vendor_id"].(string)
	billedVal, _ := row["billed_total_charge"].(float64)
	expectedVal, _ := row["expected_total_charge"].(float64)
	expectedBaseVal, _ := row["expected_base_charge"].(float64)
	expectedFuelVal, _ := row["expected_fuel_surcharge"].(float64)
	diffVal, _ := row["difference_amount"].(float64)
	scoreVal, _ := row["priority_score"].(float64)
	confVal, _ := row["match_confidence"].(float64)
	traceStr, _ := row["calculation_trace"].(string)

	status = model.ReconciliationStatus(statusStr)

	var discrepancies []model.DiscrepancyItem
	if diffVal > 0 || status == model.StatusException {
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:        model.CodeRateOvercharge,
			Category:    model.CategoryPricing,
			Description: fmt.Sprintf("Billed total Rp %.0f exceeds contract rate calculation Rp %.0f (Selisih +Rp %.0f)", billedVal, expectedVal, diffVal),
			Severity:    model.SeverityCritical,
		})
	}

	vendorName := r.GetVendorName(ctx, vendorID)

	inv := model.InvoiceCanonical{
		VendorID:      vendorID,
		VendorName:    vendorName,
		BilledAmount:  int64(billedVal),
		FieldEvidence: make(map[string]model.EvidenceSource),
	}

	shp := model.ShipmentCanonical{
		ShipmentID:    shpID,
		FieldEvidence: make(map[string]model.EvidenceSource),
	}

	pod := model.PODCanonical{
		ShipmentID:    shpID,
		FieldEvidence: make(map[string]model.EvidenceSource),
	}

	ctr := model.ContractCanonical{
		VendorID:      vendorID,
		FieldEvidence: make(map[string]model.EvidenceSource),
	}

	if r.dbClient != nil {
		var invRows []map[string]interface{}
		pInv := url.Values{}
		pInv.Set("or", fmt.Sprintf("(shipment_id.eq.%s,invoice_number.eq.%s)", shpID, shpID))
		pInv.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "invoices", pInv, &invRows); err == nil && len(invRows) > 0 {
			rInv := invRows[0]
			if v, ok := rInv["invoice_number"].(string); ok && v != "" {
				inv.InvoiceNumber = v
			}
			if v, ok := rInv["vendor_name"].(string); ok && v != "" {
				inv.VendorName = v
			}
			if v, ok := rInv["invoice_date"].(string); ok && v != "" {
				inv.InvoiceDate = v
			}
			if v, ok := rInv["subtotal"].(float64); ok && v > 0 {
				inv.BaseChargeBilled = int64(v)
			}
			if v, ok := rInv["tax_amount"].(float64); ok && v > 0 {
				inv.TaxBilled = int64(v)
			}
			if v, ok := rInv["grand_total"].(float64); ok && v > 0 {
				inv.BilledAmount = int64(v)
			}
			// Load real OCR BBox from field_evidence table
			if docID, ok := rInv["document_id"].(string); ok && docID != "" {
				inv.DocumentID = docID
				inv.FieldEvidence = r.loadFieldEvidence(ctx, docID)
			}
		}

		var sjRows []map[string]interface{}
		pSJ := url.Values{}
		pSJ.Set("or", fmt.Sprintf("(shipment_id.eq.%s,surat_jalan_number.eq.%s)", shpID, shpID))
		pSJ.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "surat_jalans", pSJ, &sjRows); err == nil && len(sjRows) > 0 {
			rSJ := sjRows[0]
			if v, ok := rSJ["surat_jalan_number"].(string); ok && v != "" {
				shp.SuratJalanNumber = v
			}
			if v, ok := rSJ["awb_number"].(string); ok && v != "" {
				shp.AWBNumber = v
				inv.AWBNumber = v
				pod.AWBNumber = v
			}
			if v, ok := rSJ["sender_city"].(string); ok && v != "" {
				shp.Origin = v
				inv.Origin = v
			}
			if v, ok := rSJ["receiver_city"].(string); ok && v != "" {
				shp.Destination = v
				inv.Destination = v
			}
			if v, ok := rSJ["actual_weight_kg"].(float64); ok && v > 0 {
				shp.WeightActualKg = v
				inv.WeightBilledKg = v
			}
			if v, ok := rSJ["total_packages"].(float64); ok && v > 0 {
				shp.TotalPackages = int(v)
			}
			if v, ok := rSJ["shipment_date"].(string); ok && v != "" {
				shp.ShipmentDate = v
			}
			// Load real OCR BBox from field_evidence table for surat_jalan
			if docID, ok := rSJ["document_id"].(string); ok && docID != "" {
				shp.FieldEvidence = r.loadFieldEvidence(ctx, docID)
			}
		}

		var podRows []map[string]interface{}
		pPOD := url.Values{}
		pPOD.Set("or", fmt.Sprintf("(shipment_id.eq.%s,pod_number.eq.%s)", shpID, shpID))
		pPOD.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "pods", pPOD, &podRows); err == nil && len(podRows) > 0 {
			rPOD := podRows[0]
			if v, ok := rPOD["pod_number"].(string); ok && v != "" {
				pod.PODNumber = v
			}
			if v, ok := rPOD["delivery_status"].(string); ok && v != "" {
				pod.DeliveryStatus = v
			}
			if v, ok := rPOD["delivery_date"].(string); ok && v != "" {
				pod.DeliveryDate = v
			}
			if v, ok := rPOD["receiver_name"].(string); ok && v != "" {
				pod.ReceiverName = v
			}
			if v, ok := rPOD["signature_present"].(bool); ok {
				pod.SignaturePresent = v
			}
			if v, ok := rPOD["stamp_present"].(bool); ok {
				pod.StampPresent = v
			}
			// Load real OCR BBox from field_evidence table for POD
			if docID, ok := rPOD["document_id"].(string); ok && docID != "" {
				pod.FieldEvidence = r.loadFieldEvidence(ctx, docID)
			}
		}

		var raRows []map[string]interface{}
		pRA := url.Values{}
		pRA.Set("vendor_id", "eq."+vendorID)
		pRA.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "rate_agreements", pRA, &raRows); err == nil && len(raRows) > 0 {
			rRA := raRows[0]
			if v, ok := rRA["agreement_id"].(string); ok && v != "" {
				ctr.AgreementID = v
			}
			if v, ok := rRA["effective_from"].(string); ok && v != "" {
				ctr.EffectiveFrom = v
			}
			if v, ok := rRA["effective_to"].(string); ok && v != "" {
				ctr.EffectiveTo = v
			}
			if v, ok := rRA["fuel_surcharge_percent"].(float64); ok {
				ctr.ApplicableFuelSurchargePercent = v
			}
			if v, ok := rRA["minimum_charge"].(float64); ok {
				ctr.MinimumCharge = int64(v)
			}
			if v, ok := rRA["dispute_window_days"].(float64); ok && v > 0 {
				ctr.DisputeWindowDays = int(v)
			}
		}
	}

	return &model.ShipmentEvent{
		EventID:         evID,
		ShipmentID:      shpID,
		VendorID:        vendorID,
		MatchMethod:     model.MatchExactID,
		MatchConfidence: confVal,
		Invoice:         inv,
		Shipment:        shp,
		POD:             pod,
		Contract:        ctr,
		Pricing: model.PricingResult{
			ExpectedBaseCharge:    int64(expectedBaseVal),
			ExpectedFuelSurcharge: int64(expectedFuelVal),
			ExpectedTotalCharge:   int64(expectedVal),
			DifferenceAmount:      int64(diffVal),
			Trace: model.CalculationTrace{
				TraceLog: traceStr,
			},
		},
		Reconciliation: model.ReconciliationDecision{
			Status:        status,
			PriorityScore: scoreVal,
			Discrepancies: discrepancies,
		},
	}
}

func canonicalSchemaRead(row map[string]interface{}) bool {
	return row != nil
}

// GetEventByID retrieves an event by EventID, ShipmentID, or InvoiceNumber
func (r *ResourceDB) GetEventByID(ctx context.Context, id string) (*model.ShipmentEvent, error) {
	ensureFieldEvidence := func(ev *model.ShipmentEvent) {
		_ = ev
	}

	if r.dbClient != nil {
		var results []map[string]interface{}
		params := url.Values{}
		params.Set("id", "eq."+id)
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "shipment_events", params, &results)
		if err == nil && len(results) > 0 {
			ev := r.rowToShipmentEvent(ctx, results[0])
			if ev != nil {
				ensureFieldEvidence(ev)
				auditLogs, _ := r.ListAuditLogs(ctx, ev.EventID)
				ev.AuditTrail = auditLogs
				return ev, nil
			}
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if ev, exists := r.events[id]; exists {
		ensureFieldEvidence(ev)
		auditLogs, _ := r.ListAuditLogs(ctx, ev.EventID)
		ev.AuditTrail = auditLogs
		return ev, nil
	}
	for _, ev := range r.events {
		if ev.ShipmentID == id || ev.Invoice.InvoiceNumber == id {
			ensureFieldEvidence(ev)
			auditLogs, _ := r.ListAuditLogs(ctx, ev.EventID)
			ev.AuditTrail = auditLogs
			return ev, nil
		}
	}

	return nil, fmt.Errorf("reconciliation event '%s' not found", id)
}

// ListAllEvents returns all stored events from Supabase DB (Single Source of Truth)
func (r *ResourceDB) ListAllEvents(ctx context.Context) ([]*model.ShipmentEvent, error) {
	if r.dbClient != nil {
		var dbRows []map[string]interface{}
		params := url.Values{}
		params.Set("order", "created_at.desc")
		params.Set("limit", "2000")
		err := r.dbClient.Query(ctx, "shipment_events", params, &dbRows)
		if err == nil {
			// Supabase query succeeded: Build directly from Supabase rows
			eventsList := make([]*model.ShipmentEvent, 0, len(dbRows))
			r.mu.Lock()
			for _, row := range dbRows {
				evID := stringValue(row["id"])
				if existing, ok := r.events[evID]; ok && existing != nil {
					eventsList = append(eventsList, existing)
					continue
				}
				ev := r.rowToShipmentEvent(ctx, row)
				if ev != nil {
					r.events[ev.EventID] = ev
					eventsList = append(eventsList, ev)
				}
			}
			r.mu.Unlock()
			return eventsList, nil
		}

	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*model.ShipmentEvent, 0, len(r.events))
	for _, ev := range r.events {
		list = append(list, ev)
	}
	return list, nil
}

// SaveDecision stores a reviewer decision and records immutable audit log.
// Also syncs status ke reconciliation_cases.
func (r *ResourceDB) SaveDecision(ctx context.Context, eventID string, decision model.AuditTrailItem, previousStatus string, newStatus string) (*model.AuditTrailItem, error) {
	now := time.Now().UTC()
	decision.Timestamp = now

	// Persist to Supabase audit_events and review_decisions
	if r.dbClient != nil {
		eventUUID := eventID
		params := url.Values{}
		params.Set("id", "eq."+eventID)
		params.Set("limit", "1")
		var seRows []map[string]interface{}
		if err := r.dbClient.Query(ctx, "shipment_events", params, &seRows); err == nil && len(seRows) > 0 {
			if v, ok := seRows[0]["id"].(string); ok {
				eventUUID = v
			}
		}
		if eventUUID == "" {
			eventUUID = eventID
		}

		// Map action ke nilai yang valid di DB constraint
		dbAction := strings.ToUpper(decision.Action)
		switch dbAction {
		case "APPROVE":
			dbAction = "APPROVE"
		case "REJECT":
			dbAction = "REJECT"
		case "DISPUTE":
			dbAction = "DISPUTE"
		case "RELEASE":
			dbAction = "RELEASE"
		default:
			dbAction = "APPROVE"
		}

		decisionPayload := map[string]interface{}{
			"id": model.NewUUID(), "action": dbAction, "reviewer_id": nullableUUID(decision.ActorID),
			"reason": decision.Notes, "created_at": now,
		}

		// Cari case_id yang terkait
		var rcRows []map[string]interface{}
		pRC := url.Values{}
		pRC.Set("event_id", "eq."+eventUUID)
		pRC.Set("limit", "1")
		if err := r.dbClient.Query(ctx, "reconciliation_cases", pRC, &rcRows); err == nil && len(rcRows) > 0 {
			if caseUUID, ok := rcRows[0]["id"].(string); ok && caseUUID != "" {
				decisionPayload["case_id"] = caseUUID

				// Sync status reconciliation_case
				caseStatus := "IN_REVIEW"
				switch newStatus {
				case "APPROVED":
					caseStatus = "APPROVED"
				case "REJECTED":
					caseStatus = "REJECTED"
				case "DISPUTED":
					caseStatus = "DISPUTED"
				case "RESOLVED":
					caseStatus = "RESOLVED"
				}
				rcUpdate := url.Values{}
				rcUpdate.Set("id", "eq."+caseUUID)
				var rcOut []map[string]interface{}
				_ = r.dbClient.Update(ctx, "reconciliation_cases", rcUpdate, map[string]interface{}{
					"status":     caseStatus,
					"updated_at": now,
				}, &rcOut)
			}
		}

		var dOut []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "review_decisions", decisionPayload, &dOut); err != nil {
			fmt.Printf("[SaveDecision] Failed to insert review_decisions: %v\n", err)
		}

		// Update status in Supabase shipment_events
		seParams := url.Values{}
		seParams.Set("id", "eq."+eventUUID)
		updatePayload := map[string]interface{}{
			"status":                newStatus,
			"requires_human_review": false,
		}
		var uOut []map[string]interface{}
		if err := r.dbClient.Update(ctx, "shipment_events", seParams, updatePayload, &uOut); err != nil {
			fmt.Printf("[Supabase Update Error] Failed to update shipment_events status: %v\n", err)
		} else {
			fmt.Printf("[Supabase Update Success] Updated shipment_events for %s to %s\n", eventID, newStatus)
		}

		// Write to audit_events (append-only immutable ledger)
		auditPayload := map[string]interface{}{
			"id": model.NewUUID(), "event_id": eventUUID, "actor_id": nullableUUID(decision.ActorID),
			"action": decision.Action, "previous_status": previousStatus,
			"new_status": newStatus, "notes": decision.Notes, "created_at": now,
		}
		var aOut []map[string]interface{}
		if err := r.dbClient.Insert(ctx, "audit_events", auditPayload, &aOut); err != nil {
			fmt.Printf("[AuditEvent] Failed to write audit_events: %v\n", err)
		} else {
			fmt.Printf("[AuditEvent] Recorded audit event for %s: %s → %s\n", eventID, previousStatus, newStatus)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Update in-memory event
	if ev, exists := r.events[eventID]; exists {
		ev.Reconciliation.Status = model.ReconciliationStatus(newStatus)
		ev.AuditTrail = append(ev.AuditTrail, decision)
	}

	r.audit = append(r.audit, decision)
	return &decision, nil
}

// ReleaseEvent moves an approved/rejected event to the immutable audit_ledger
// and marks it as RESOLVED. This is the final action by Finance Controller (SoD).
// After release, the event disappears from the SoD queue and appears in Audit Ledger Trail.
func (r *ResourceDB) ReleaseEvent(ctx context.Context, eventID string, releasedBy, releasedByRole, notes string) error {
	if r.dbClient == nil {
		return fmt.Errorf("database client not available")
	}

	// Resolve event from DB
	params := url.Values{}
	params.Set("or", fmt.Sprintf("(id.eq.%s,event_id.eq.%s,shipment_id.eq.%s)", eventID, eventID, eventID))
	params.Set("limit", "1")
	var seRows []map[string]interface{}
	if err := r.dbClient.Query(ctx, "shipment_events", params, &seRows); err != nil || len(seRows) == 0 {
		return fmt.Errorf("shipment event '%s' not found for release", eventID)
	}
	row := seRows[0]

	eventUUID, _ := row["id"].(string)
	shipmentID, _ := row["shipment_id"].(string)
	vendorID, _ := row["vendor_id"].(string)
	statusStr, _ := row["status"].(string)
	billedVal, _ := row["billed_total_charge"].(float64)
	expectedVal, _ := row["expected_total_charge"].(float64)
	diffVal, _ := row["difference_amount"].(float64)

	// Only allow RELEASE if status is APPROVED or REJECTED
	if statusStr != "APPROVED" && statusStr != "REJECTED" && statusStr != "DISPUTED" {
		return fmt.Errorf("event '%s' has status '%s' — only APPROVED/REJECTED/DISPUTED can be released", eventID, statusStr)
	}

	// Prevent double-release
	var existLedger []map[string]interface{}
	pLedger := url.Values{}
	pLedger.Set("event_id", "eq."+eventUUID)
	pLedger.Set("limit", "1")
	if err := r.dbClient.Query(ctx, "audit_ledger", pLedger, &existLedger); err == nil && len(existLedger) > 0 {
		return fmt.Errorf("event '%s' already released to audit_ledger", eventID)
	}

	// Resolve vendor name and invoice number
	vendorName := r.GetVendorName(ctx, vendorID)
	invoiceNumber := shipmentID
	if ev, ok := r.events[eventID]; ok && ev.Invoice.InvoiceNumber != "" {
		invoiceNumber = ev.Invoice.InvoiceNumber
	}

	// Determine final_decision for ledger
	finalDecision := "APPROVED"
	if statusStr == "REJECTED" {
		finalDecision = "REJECTED"
	} else if statusStr == "DISPUTED" {
		finalDecision = "DISPUTED_RESOLVED"
	}

	// Get reconciliation_case_id if exists
	var caseUUID interface{}
	var rcRows []map[string]interface{}
	pRC := url.Values{}
	pRC.Set("event_id", "eq."+eventUUID)
	pRC.Set("limit", "1")
	if err := r.dbClient.Query(ctx, "reconciliation_cases", pRC, &rcRows); err == nil && len(rcRows) > 0 {
		if v, ok := rcRows[0]["id"].(string); ok {
			caseUUID = v
		}
	}

	now := time.Now().UTC()

	// Insert into audit_ledger (immutable)
	ledgerPayload := map[string]interface{}{
		"id":                     model.NewUUID(),
		"event_id":               eventUUID,
		"reconciliation_case_id": caseUUID,
		"shipment_id":            shipmentID,
		"invoice_number":         invoiceNumber,
		"vendor_id":              vendorID,
		"vendor_name":            vendorName,
		"billed_amount":          billedVal,
		"expected_amount":        expectedVal,
		"variance_amount":        diffVal,
		"final_decision":         finalDecision,
		"released_by":            releasedBy,
		"released_by_role":       releasedByRole,
		"released_at":            now,
		"reviewer_notes":         notes,
		"discrepancy_summary":    "[]",
		"created_at":             now,
	}
	var ledgerOut []map[string]interface{}
	if err := r.dbClient.Insert(ctx, "audit_ledger", ledgerPayload, &ledgerOut); err != nil {
		return fmt.Errorf("failed to insert into audit_ledger: %v", err)
	}
	fmt.Printf("[AuditLedger] Released event %s to audit_ledger (decision=%s)\n", eventID, finalDecision)

	// Mark event as RESOLVED in shipment_events
	seParams := url.Values{}
	seParams.Set("or", fmt.Sprintf("(id.eq.%s,event_id.eq.%s)", eventUUID, eventUUID))
	var seOut []map[string]interface{}
	_ = r.dbClient.Update(ctx, "shipment_events", seParams, map[string]interface{}{
		"status": "RESOLVED",
	}, &seOut)

	// Mark reconciliation_case as RESOLVED
	if caseUUID != nil {
		rcParams := url.Values{}
		rcParams.Set("id", "eq."+caseUUID.(string))
		var rcOut []map[string]interface{}
		_ = r.dbClient.Update(ctx, "reconciliation_cases", rcParams, map[string]interface{}{
			"status":     "RESOLVED",
			"updated_at": now,
		}, &rcOut)
	}

	// Write to audit_events
	auditPayload := map[string]interface{}{
		"id":              model.NewUUID(),
		"event_id":        eventUUID,
		"actor_id":        releasedBy,
		"actor_role":      releasedByRole,
		"action":          "RELEASE",
		"previous_status": statusStr,
		"new_status":      "RESOLVED",
		"notes":           notes,
		"timestamp":       now,
	}
	var aOut []map[string]interface{}
	_ = r.dbClient.Insert(ctx, "audit_events", auditPayload, &aOut)

	// Update in-memory cache
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev, exists := r.events[eventID]; exists {
		ev.Reconciliation.Status = model.StatusResolved
	}

	return nil
}

// ListAuditLogs retrieves all audit trail logs from Supabase
func (r *ResourceDB) ListAuditLogs(ctx context.Context, eventID string) ([]model.AuditTrailItem, error) {
	if r.dbClient != nil {
		var results []map[string]interface{}
		params := url.Values{}
		params.Set("order", "created_at.desc")
		params.Set("limit", "2000")
		if eventID != "" {
			params.Set("or", fmt.Sprintf("(event_id.eq.%s,actor_id.eq.%s)", eventID, eventID))
		}
		err := r.dbClient.Query(ctx, "audit_events", params, &results)
		if err == nil {
			items := make([]model.AuditTrailItem, 0, len(results))
			for _, row := range results {
				actorID, _ := row["actor_id"].(string)
				action, _ := row["action"].(string)
				reason, _ := row["reason"].(string)
				if reason == "" {
					reason, _ = row["notes"].(string)
				}
				tsStr, _ := row["created_at"].(string)
				if tsStr == "" {
					tsStr, _ = row["timestamp"].(string)
				}
				ts, _ := time.Parse(time.RFC3339, tsStr)
				if ts.IsZero() {
					ts = time.Now().UTC()
				}
				actorRole, _ := row["actor_role"].(string)
				prevStatus, _ := row["previous_status"].(string)
				newStatus, _ := row["new_status"].(string)

				items = append(items, model.AuditTrailItem{
					ActorID:        actorID,
					ActorRole:      actorRole,
					Action:         action,
					PreviousStatus: prevStatus,
					NewStatus:      newStatus,
					Timestamp:      ts,
					Notes:          reason,
				})
			}
			return items, nil
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if eventID == "" {
		out := make([]model.AuditTrailItem, len(r.audit))
		copy(out, r.audit)
		return out, nil
	}

	var filtered []model.AuditTrailItem
	for _, a := range r.audit {
		if strings.EqualFold(a.ActorID, eventID) {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}
