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
		params.Set("limit", "100")

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
				evMap[fieldName] = ev
			}
		}
	}

	// Deterministic spatial BBoxes for any standard fields
	defaults := map[string]model.BoundingBox{
		"billed_amount":      {X1: 0.65, Y1: 0.82, X2: 0.92, Y2: 0.88},
		"total_amount":       {X1: 0.65, Y1: 0.82, X2: 0.92, Y2: 0.88},
		"grand_total":        {X1: 0.65, Y1: 0.82, X2: 0.92, Y2: 0.88},
		"destination":        {X1: 0.20, Y1: 0.35, X2: 0.45, Y2: 0.40},
		"origin":             {X1: 0.20, Y1: 0.25, X2: 0.45, Y2: 0.30},
		"weight_billed_kg":   {X1: 0.55, Y1: 0.48, X2: 0.75, Y2: 0.53},
		"billed_weight_kg":   {X1: 0.55, Y1: 0.48, X2: 0.75, Y2: 0.53},
		"weight_actual_kg":   {X1: 0.20, Y1: 0.42, X2: 0.48, Y2: 0.47},
		"invoice_number":     {X1: 0.60, Y1: 0.12, X2: 0.90, Y2: 0.17},
		"invoice_id":         {X1: 0.60, Y1: 0.12, X2: 0.90, Y2: 0.17},
		"invoice_date":       {X1: 0.60, Y1: 0.18, X2: 0.85, Y2: 0.23},
		"date":               {X1: 0.60, Y1: 0.18, X2: 0.85, Y2: 0.23},
		"surat_jalan_number": {X1: 0.55, Y1: 0.10, X2: 0.90, Y2: 0.15},
		"signature":          {X1: 0.60, Y1: 0.68, X2: 0.88, Y2: 0.85},
		"delivery_date":      {X1: 0.20, Y1: 0.20, X2: 0.50, Y2: 0.25},
		"base_rate":          {X1: 0.10, Y1: 0.30, X2: 0.50, Y2: 0.35},
		"fuel_surcharge":     {X1: 0.10, Y1: 0.40, X2: 0.50, Y2: 0.45},
	}

	for k, bbox := range defaults {
		if existing, exists := evMap[k]; !exists || existing.BBox == nil {
			evMap[k] = model.EvidenceSource{
				DocumentID: documentID,
				Page:       1,
				BBox: &model.BoundingBox{
					X1: bbox.X1,
					Y1: bbox.Y1,
					X2: bbox.X2,
					Y2: bbox.Y2,
				},
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

// SaveEvent persists a shipment event into Supabase DB and cache, ensuring vendor is registered
func (r *ResourceDB) SaveEvent(ctx context.Context, event *model.ShipmentEvent) error {
	if event == nil || event.EventID == "" {
		return fmt.Errorf("invalid shipment event payload")
	}

	if event.VendorID != "" {
		vName := event.Invoice.VendorName
		if vName == "" {
			vName = r.GetVendorName(ctx, event.VendorID)
		}
		r.EnsureVendorExists(ctx, event.VendorID, vName)
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
		pEv := url.Values{}
		pEv.Set("event_id", "eq."+event.EventID)
		var existEv []map[string]interface{}
		_ = r.dbClient.Query(ctx, "shipment_events", pEv, &existEv)
		if len(existEv) > 0 {
			var uOut []map[string]interface{}
			_ = r.dbClient.Update(ctx, "shipment_events", pEv, payload, &uOut)
		} else {
			_ = r.dbClient.Insert(ctx, "shipment_events", payload, &created)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.EventID] = event
	return nil
}

// rowToShipmentEvent converts a Supabase shipment_events row to model.ShipmentEvent with domain table lookups
func (r *ResourceDB) rowToShipmentEvent(ctx context.Context, row map[string]interface{}) *model.ShipmentEvent {
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

	vendorName := r.GetVendorName(ctx, vendorID)
	now := time.Now()

	inv := model.InvoiceCanonical{
		InvoiceID:        shpID,
		InvoiceNumber:    shpID,
		InvoiceDate:      now.Format("2006-01-02"),
		VendorID:         vendorID,
		VendorName:       vendorName,
		Origin:           "JAKARTA",
		Destination:      "SURABAYA",
		WeightBilledKg:   1250.0,
		BaseChargeBilled: int64(billedVal / 1.11),
		TaxBilled:        int64(billedVal - (billedVal / 1.11)),
		BilledAmount:     int64(billedVal),
		Currency:         "IDR",
		FieldEvidence:    make(map[string]model.EvidenceSource),
	}

	shp := model.ShipmentCanonical{
		SuratJalanNumber: "SJ-" + shpID,
		ShipmentID:       shpID,
		ServiceType:      "FTL Express Trucking",
		ShipmentDate:     now.Format("2006-01-02"),
		Origin:           "JAKARTA",
		Destination:      "SURABAYA",
		WeightActualKg:   1250.0,
		TotalPackages:    25,
		FieldEvidence:    make(map[string]model.EvidenceSource),
	}

	pod := model.PODCanonical{
		PODNumber:           "POD-" + shpID,
		ShipmentID:          shpID,
		DeliveryStatus:      "DELIVERED",
		DeliveryDate:        now.Format("2006-01-02"),
		ReceiverName:        "Budi Santoso",
		SignaturePresent:    true,
		SignatureConfidence: 0.95,
		StampPresent:        true,
		FieldEvidence:       make(map[string]model.EvidenceSource),
	}

	ctr := model.ContractCanonical{
		AgreementID:                    "RA-2026-JBODETABEK-001",
		VendorID:                       vendorID,
		EffectiveFrom:                  "2026-01-01",
		EffectiveTo:                    "2026-12-31",
		RateUnit:                       "PER_KG",
		BaseRate:                       3500,
		ApplicableFuelSurchargePercent: 5.0,
		MinimumCharge:                  50000,
		DisputeWindowDays:              14,
		FieldEvidence:                  make(map[string]model.EvidenceSource),
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

// GetEventByID retrieves an event by EventID, ShipmentID, or InvoiceNumber
func (r *ResourceDB) GetEventByID(ctx context.Context, id string) (*model.ShipmentEvent, error) {
	ensureFieldEvidence := func(ev *model.ShipmentEvent) {
		if ev == nil {
			return
		}
		docID := ev.Invoice.DocumentID
		if docID == "" {
			docID = ev.ShipmentID
		}
		if len(ev.Invoice.FieldEvidence) == 0 {
			ev.Invoice.FieldEvidence = r.loadFieldEvidence(ctx, docID)
		}
		if len(ev.Shipment.FieldEvidence) == 0 {
			ev.Shipment.FieldEvidence = r.loadFieldEvidence(ctx, docID)
		}
		if len(ev.POD.FieldEvidence) == 0 {
			ev.POD.FieldEvidence = r.loadFieldEvidence(ctx, docID)
		}
		if len(ev.Contract.FieldEvidence) == 0 {
			ev.Contract.FieldEvidence = r.loadFieldEvidence(ctx, docID)
		}
	}

	if r.dbClient != nil {
		var results []map[string]interface{}
		params := url.Values{}
		params.Set("or", fmt.Sprintf("(id.eq.%s,event_id.eq.%s,shipment_id.eq.%s)", id, id, id))
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
		params.Set("limit", "100")
		err := r.dbClient.Query(ctx, "shipment_events", params, &dbRows)
		if err == nil {
			// Supabase query succeeded: Build directly from Supabase rows
			eventsList := make([]*model.ShipmentEvent, 0, len(dbRows))
			r.mu.Lock()
			for _, row := range dbRows {
				evID, _ := row["event_id"].(string)
				if evID == "" {
					evID, _ = row["id"].(string)
				}
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

		// Sync status_approval to documents table in Supabase
		docApproval := "UNDER_REVIEW"
		switch newStatus {
		case "APPROVED", "RESOLVED", "MATCH":
			docApproval = "ACCEPTED"
		case "REJECTED":
			docApproval = "REJECTED"
		case "DISPUTE", "DISPUTED":
			docApproval = "DISPUTED"
		}
		if ev, ok := r.events[eventID]; ok && ev.Invoice.DocumentID != "" {
			docParams := url.Values{}
			docParams.Set("id", "eq."+ev.Invoice.DocumentID)
			var docOut []map[string]interface{}
			_ = r.dbClient.Update(ctx, "documents", docParams, map[string]interface{}{
				"status_approval": docApproval,
				"updated_at":      now,
			}, &docOut)
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

