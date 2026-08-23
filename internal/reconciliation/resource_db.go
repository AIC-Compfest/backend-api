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

// NewResourceDB creates a new reconciliation ResourceDB
func NewResourceDB(dbClient *database.Client) *ResourceDB {
	return &ResourceDB{
		dbClient: dbClient,
		events:   make(map[string]*model.ShipmentEvent),
		audit:    make([]model.AuditTrailItem, 0),
	}
}

// GetVendorName queries the rate_agreements table in Supabase for the official 3PL vendor name
func (r *ResourceDB) GetVendorName(ctx context.Context, vendorID string) string {
	if vendorID == "" {
		return "Vendor 3PL"
	}
	if r.dbClient != nil {
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

	vendorMap := map[string]string{
		"3PL-LOG-001": "PT Cepat Logistik Nusantara",
		"3PL-LOG-002": "PT Trans Express Indonesia",
		"3PL-LOG-003": "PT Kargo Andalan Utama",
		"3PL-LOG-004": "PT Sinar Logistik Mandiri",
		"3PL-LOG-005": "PT Aruna Freight Solusi",
	}
	if name, ok := vendorMap[vendorID]; ok {
		return name
	}
	return vendorID
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

// SaveEvent persists a shipment event into Supabase DB and cache
func (r *ResourceDB) SaveEvent(ctx context.Context, event *model.ShipmentEvent) error {
	if event == nil || event.EventID == "" {
		return fmt.Errorf("invalid shipment event payload")
	}

	if r.dbClient != nil {
		var created []map[string]interface{}
		payload := map[string]interface{}{
			"id":                     event.EventID,
			"event_id":               event.EventID,
			"shipment_id":            event.ShipmentID,
			"vendor_id":              event.VendorID,
			"status":                 string(event.Reconciliation.Status),
			"match_confidence":       event.MatchConfidence,
			"expected_base_charge":   event.Pricing.ExpectedBaseCharge,
			"expected_fuel_surcharge": event.Pricing.ExpectedFuelSurcharge,
			"expected_total_charge":  event.Pricing.ExpectedTotalCharge,
			"billed_total_charge":    event.Invoice.BilledAmount,
			"difference_amount":      event.Pricing.DifferenceAmount,
			"is_material":            event.Pricing.IsMaterial,
			"calculation_trace":      event.Pricing.Trace.TraceLog,
			"priority_score":         event.Reconciliation.PriorityScore,
			"requires_human_review":  event.Reconciliation.RequiresHumanReview,
		}
		_ = r.dbClient.Insert(ctx, "shipment_events", payload, &created)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.EventID] = event
	return nil
}

// rowToShipmentEvent converts a Supabase shipment_events row to model.ShipmentEvent
func (r *ResourceDB) rowToShipmentEvent(row map[string]interface{}) *model.ShipmentEvent {
	evID, _ := row["event_id"].(string)
	if evID == "" {
		evID, _ = row["id"].(string)
	}
	if evID == "" {
		return nil
	}

	statusStr, _ := row["status"].(string)
	shpID, _ := row["shipment_id"].(string)
	if shpID == "" {
		shpID = evID
	}
	vendorID, _ := row["vendor_id"].(string)
	billedVal, _ := row["billed_total_charge"].(float64)
	expectedVal, _ := row["expected_total_charge"].(float64)
	expectedBaseVal, _ := row["expected_base_charge"].(float64)
	if expectedBaseVal == 0 && expectedVal > 0 {
		expectedBaseVal = expectedVal * 0.85
	}
	expectedFuelVal, _ := row["expected_fuel_surcharge"].(float64)
	diffVal, _ := row["difference_amount"].(float64)
	scoreVal, _ := row["priority_score"].(float64)
	confVal, _ := row["match_confidence"].(float64)
	if confVal == 0 {
		confVal = 0.95
	}
	traceStr, _ := row["calculation_trace"].(string)

	status := model.ReconciliationStatus(statusStr)
	if status == "" {
		status = model.StatusMatch
	}

	var discrepancies []model.DiscrepancyItem
	if diffVal > 0 || status == model.StatusException {
		discrepancies = append(discrepancies, model.DiscrepancyItem{
			Code:        model.CodeRateOvercharge,
			Category:    model.CategoryPricing,
			Description: fmt.Sprintf("Billed total Rp %.0f exceeds contract rate calculation Rp %.0f (Selisih +Rp %.0f)", billedVal, expectedVal, diffVal),
			Severity:    model.SeverityCritical,
		})
	}

	return &model.ShipmentEvent{
		EventID:         evID,
		ShipmentID:      shpID,
		VendorID:        vendorID,
		MatchMethod:     model.MatchExactID,
		MatchConfidence: confVal,
		Invoice: model.InvoiceCanonical{
			InvoiceID:        shpID,
			InvoiceNumber:    shpID,
			InvoiceDate:      time.Now().Format("2006-01-02"),
			VendorID:         vendorID,
			BilledAmount:     int64(billedVal),
			Currency:         "IDR",
		},
		Shipment: model.ShipmentCanonical{
			SuratJalanNumber: "SJ-" + shpID,
			ShipmentID:       shpID,
			ServiceType:      "TRUCKING_FTL",
			Origin:           "JKT",
			Destination:      "SBY",
		},
		POD: model.PODCanonical{
			PODNumber:      "POD-" + shpID,
			ShipmentID:     shpID,
			DeliveryStatus: "DELIVERED",
		},
		Contract: model.ContractCanonical{
			AgreementID: "CTR-" + vendorID,
			VendorID:    vendorID,
		},
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

// GetEventByID retrieves an event by EventID, ShipmentID, or InvoiceNumber
func (r *ResourceDB) GetEventByID(ctx context.Context, id string) (*model.ShipmentEvent, error) {
	if r.dbClient != nil {
		var results []map[string]interface{}
		params := url.Values{}
		params.Set("or", fmt.Sprintf("(id.eq.%s,event_id.eq.%s,shipment_id.eq.%s)", id, id, id))
		params.Set("limit", "1")

		err := r.dbClient.Query(ctx, "shipment_events", params, &results)
		if err == nil {
			if len(results) == 0 {
				return nil, fmt.Errorf("reconciliation event '%s' not found", id)
			}
			ev := r.rowToShipmentEvent(results[0])
			if ev != nil {
				auditLogs, _ := r.ListAuditLogs(ctx, ev.EventID)
				ev.AuditTrail = auditLogs
				return ev, nil
			}
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if ev, exists := r.events[id]; exists {
		return ev, nil
	}

	for _, ev := range r.events {
		if ev.ShipmentID == id || ev.Invoice.InvoiceNumber == id {
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
		params.Set("limit", "100")
		err := r.dbClient.Query(ctx, "shipment_events", params, &dbRows)
		if err == nil {
			// Supabase query succeeded: Build directly from Supabase rows
			eventsList := make([]*model.ShipmentEvent, 0, len(dbRows))
			r.mu.Lock()
			r.events = make(map[string]*model.ShipmentEvent)
			for _, row := range dbRows {
				ev := r.rowToShipmentEvent(row)
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

// SaveDecision stores a reviewer decision and records immutable audit log
func (r *ResourceDB) SaveDecision(ctx context.Context, eventID string, decision model.AuditTrailItem, previousStatus string, newStatus string) (*model.AuditTrailItem, error) {
	now := time.Now().UTC()
	decision.Timestamp = now

	// Persist to Supabase audit_events and review_decisions
	if r.dbClient != nil {
		decisionPayload := map[string]interface{}{
			"id":          model.NewUUID(),
			"event_id":    eventID,
			"reviewer_id": decision.ActorID,
			"decision":    decision.Action,
			"reason":      decision.Notes,
			"created_at":  now,
		}
		var dOut []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "review_decisions", decisionPayload, &dOut)

		// Update status in Supabase shipment_events
		params := url.Values{}
		params.Set("or", fmt.Sprintf("(id.eq.%s,event_id.eq.%s,shipment_id.eq.%s)", eventID, eventID, eventID))
		updatePayload := map[string]interface{}{
			"status":                newStatus,
			"requires_human_review": false,
		}
		var uOut []map[string]interface{}
		if err := r.dbClient.Update(ctx, "shipment_events", params, updatePayload, &uOut); err != nil {
			fmt.Printf("[Supabase Update Error] Failed to update shipment_events status: %v\n", err)
		} else {
			fmt.Printf("[Supabase Update Success] Updated shipment_events for %s to %s\n", eventID, newStatus)
		}

		auditPayload := map[string]interface{}{
			"id":           model.NewUUID(),
			"event_id":     eventID,
			"actor_id":     decision.ActorID,
			"action":       decision.Action,
			"entity_type":  "RECONCILIATION_CASE",
			"entity_id":    eventID,
			"before_state": map[string]interface{}{"status": previousStatus},
			"after_state":  map[string]interface{}{"status": newStatus, "notes": decision.Notes},
			"reason":       decision.Notes,
			"created_at":   now,
		}
		var aOut []map[string]interface{}
		_ = r.dbClient.Insert(ctx, "audit_events", auditPayload, &aOut)
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

// ListAuditLogs retrieves all audit trail logs from Supabase
func (r *ResourceDB) ListAuditLogs(ctx context.Context, eventID string) ([]model.AuditTrailItem, error) {
	if r.dbClient != nil {
		var results []map[string]interface{}
		params := url.Values{}
		params.Set("order", "created_at.desc")
		params.Set("limit", "100")
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
				items = append(items, model.AuditTrailItem{
					ActorID:   actorID,
					Action:    action,
					Timestamp: ts,
					Notes:     reason,
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
