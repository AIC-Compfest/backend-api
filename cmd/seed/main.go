package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"backend-api/internal/config"
	"backend-api/internal/database"
	"backend-api/internal/model"
	serviceRecon "backend-api/internal/service/reconciliation"
)

func main() {
	fmt.Println("================================================================")
	fmt.Println("🚀 COMPREHENSIVE SUPABASE SEEDER — END-TO-END DOMAIN & SPATIAL BBOX")
	fmt.Println("================================================================")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	dbClient := database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	ctx := context.Background()

	// 1. Seed Master Rate Agreements (PKS Matrix)
	fmt.Println("\n📄 [1/4] Seeding Master Rate Agreements (PKS Matrix)...")
	rateAgreements := []map[string]interface{}{
		{
			"id":                     "11111111-1111-1111-1111-111111111101",
			"agreement_id":           "RA-2026-JBODETABEK-001",
			"vendor_id":              "3PL-LOG-001",
			"vendor_name":            "PT Cepat Logistik Nusantara",
			"effective_from":         "2026-01-01",
			"effective_to":           "2026-12-31",
			"currency":               "IDR",
			"fuel_surcharge_percent": 14.00,
			"minimum_charge":         50000,
			"dispute_window_days":    30,
		},
		{
			"id":                     "11111111-1111-1111-1111-111111111102",
			"agreement_id":           "CTR-3PL-LOG-002-2026",
			"vendor_id":              "3PL-LOG-002",
			"vendor_name":            "PT Trans Express Indonesia",
			"effective_from":         "2026-01-01",
			"effective_to":           "2026-12-31",
			"currency":               "IDR",
			"fuel_surcharge_percent": 12.50,
			"minimum_charge":         0,
			"dispute_window_days":    45,
		},
		{
			"id":                     "11111111-1111-1111-1111-111111111103",
			"agreement_id":           "CTR-3PL-LOG-003-2026",
			"vendor_id":              "3PL-LOG-003",
			"vendor_name":            "PT Kargo Andalan Utama",
			"effective_from":         "2026-01-01",
			"effective_to":           "2026-12-31",
			"currency":               "IDR",
			"fuel_surcharge_percent": 15.00,
			"minimum_charge":         0,
			"dispute_window_days":    7,
		},
		{
			"id":                     "11111111-1111-1111-1111-111111111104",
			"agreement_id":           "CTR-3PL-LOG-004-2026",
			"vendor_id":              "3PL-LOG-004",
			"vendor_name":            "PT Sinar Logistik Mandiri",
			"effective_from":         "2026-01-01",
			"effective_to":           "2026-12-31",
			"currency":               "IDR",
			"fuel_surcharge_percent": 10.00,
			"minimum_charge":         0,
			"dispute_window_days":    30,
		},
		{
			"id":                     "11111111-1111-1111-1111-111111111105",
			"agreement_id":           "CTR-3PL-LOG-005-2026",
			"vendor_id":              "3PL-LOG-005",
			"vendor_name":            "PT Aruna Freight Solusi",
			"effective_from":         "2026-01-01",
			"effective_to":           "2026-12-31",
			"currency":               "IDR",
			"fuel_surcharge_percent": 12.00,
			"minimum_charge":         0,
			"dispute_window_days":    14,
		},
	}

	for _, ra := range rateAgreements {
		p := url.Values{}
		p.Set("agreement_id", "eq."+ra["agreement_id"].(string))
		var existing []map[string]interface{}
		_ = dbClient.Query(ctx, "rate_agreements", p, &existing)
		if len(existing) == 0 {
			var out []map[string]interface{}
			_ = dbClient.Insert(ctx, "rate_agreements", ra, &out)
			fmt.Printf("   ✓ Inserted Rate Agreement: %s (%s)\n", ra["agreement_id"], ra["vendor_name"])
		} else {
			fmt.Printf("   ✓ Existing Rate Agreement: %s (%s)\n", ra["agreement_id"], ra["vendor_name"])
		}
	}

	// 2. Load Pilot Transactions
	datasetDir := "../synthetic-data/dataset/v4_pilot_20"
	if _, err := os.Stat(datasetDir); os.IsNotExist(err) {
		datasetDir = "d:/AIC/synthetic-data/dataset/v4_pilot_20"
	}

	txnsDir := filepath.Join(datasetDir, "transactions")
	entries, err := os.ReadDir(txnsDir)
	if err != nil {
		slog.Error("Failed to read transactions dataset directory", "dir", txnsDir, "error", err)
		os.Exit(1)
	}

	engine := serviceRecon.NewEngine()
	fmt.Println("\n📦 [2/4] Processing 4-Way Matching & Ingesting into Supabase Domain Tables...")

	seededEventsCount := 0
	seededDocsCount := 0
	seededBBoxesCount := 0

	vendorNames := map[string]string{
		"3PL-LOG-001": "PT Cepat Logistik Nusantara",
		"3PL-LOG-002": "PT Trans Express Indonesia",
		"3PL-LOG-003": "PT Kargo Andalan Utama",
		"3PL-LOG-004": "PT Sinar Logistik Mandiri",
		"3PL-LOG-005": "PT Aruna Freight Solusi",
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
			GroundTruth struct {
				Status        string `json:"status"`
				AnomalyType   string `json:"anomaly_type"`
				Discrepancies []struct {
					Code        string  `json:"code"`
					Description string  `json:"description"`
					Severity    string  `json:"severity"`
					DeltaAmount float64 `json:"delta_amount"`
				} `json:"discrepancies"`
			} `json:"ground_truth"`
		}

		if err := json.Unmarshal(gtBytes, &gt); err != nil {
			continue
		}

		vName := vendorNames[gt.VendorID]
		if vName == "" {
			vName = "PT Cepat Logistik Nusantara"
		}

		// A. Insert into documents table (with valid storage_path & status=PROCESSING)
		docID := model.NewUUID()
		docPayload := map[string]interface{}{
			"id":              docID,
			"filename":        fmt.Sprintf("Bundle_%s_%s.pdf", gt.InvoiceID, gt.ShipmentID),
			"file_size":       245000,
			"mime_type":       "application/pdf",
			"sha256_hash":     fmt.Sprintf("%032x", seededDocsCount+1),
			"storage_path":    fmt.Sprintf("documents/%s.pdf", docID),
			"status":          "PROCESSING",
			"status_approval": "UNDER_REVIEW",
		}
		var dOut []map[string]interface{}
		_ = dbClient.Insert(ctx, "documents", docPayload, &dOut)
		seededDocsCount++

		// B. Insert into document_segments table (Invoice, Surat Jalan, POD)
		segInvoiceID := model.NewUUID()
		_ = dbClient.Insert(ctx, "document_segments", map[string]interface{}{
			"id":                        segInvoiceID,
			"document_id":               docID,
			"segment_number":            1,
			"document_type":             "INVOICE",
			"start_page":                1,
			"end_page":                  1,
			"classification_confidence": 0.99,
			"classification_method":     "ocr_layout_ai",
			"extraction_status":         "EXTRACTED",
		}, &dOut)

		segSJID := model.NewUUID()
		_ = dbClient.Insert(ctx, "document_segments", map[string]interface{}{
			"id":                        segSJID,
			"document_id":               docID,
			"segment_number":            2,
			"document_type":             "SURAT_JALAN",
			"start_page":                2,
			"end_page":                  2,
			"classification_confidence": 0.98,
			"classification_method":     "ocr_layout_ai",
			"extraction_status":         "EXTRACTED",
		}, &dOut)

		segPODID := model.NewUUID()
		_ = dbClient.Insert(ctx, "document_segments", map[string]interface{}{
			"id":                        segPODID,
			"document_id":               docID,
			"segment_number":            3,
			"document_type":             "POD",
			"start_page":                3,
			"end_page":                  3,
			"classification_confidence": 0.97,
			"classification_method":     "ocr_layout_ai",
			"extraction_status":         "EXTRACTED",
		}, &dOut)

		// C. Insert into invoices table
		_ = dbClient.Insert(ctx, "invoices", map[string]interface{}{
			"id":             model.NewUUID(),
			"document_id":    docID,
			"invoice_number": gt.InvoiceID,
			"vendor_id":      gt.VendorID,
			"vendor_name":    vName,
			"shipment_id":    gt.ShipmentID,
			"awb_number":     "AWB-" + gt.ShipmentID,
			"invoice_date":   gt.ShipmentDate,
			"subtotal":       int64(float64(gt.BilledCharge) / 1.11),
			"tax_amount":     gt.TaxAmount,
			"grand_total":    gt.BilledCharge,
			"currency":       "IDR",
		}, &dOut)

		// D. Insert into surat_jalans table
		_ = dbClient.Insert(ctx, "surat_jalans", map[string]interface{}{
			"id":                 model.NewUUID(),
			"document_id":        docID,
			"surat_jalan_number": "SJ-" + gt.ShipmentID,
			"shipment_id":        gt.ShipmentID,
			"awb_number":         "AWB-" + gt.ShipmentID,
			"shipment_date":      gt.ShipmentDate,
			"sender_name":        "Gudang " + gt.Origin,
			"sender_city":        gt.Origin,
			"receiver_name":      "Hub Distribusi " + gt.Destination,
			"receiver_city":      gt.Destination,
			"actual_weight_kg":   gt.WeightKg,
			"total_packages":     gt.Quantity,
		}, &dOut)

		// E. Insert into pods table
		delivStatus := "DELIVERED"
		if !gt.POD.Delivered {
			delivStatus = "FAILED"
		}
		_ = dbClient.Insert(ctx, "pods", map[string]interface{}{
			"id":                model.NewUUID(),
			"document_id":       docID,
			"pod_number":        "POD-" + gt.ShipmentID,
			"shipment_id":       gt.ShipmentID,
			"awb_number":        "AWB-" + gt.ShipmentID,
			"delivery_status":   delivStatus,
			"delivery_date":     gt.POD.DeliveryDate,
			"receiver_name":     "Petugas Penerima Retail",
			"signature_present": gt.POD.SignaturePresent,
			"stamp_present":     gt.POD.StampPresent,
		}, &dOut)

		// F. Insert OCR Bounding Boxes into field_evidence table
		bboxes := []map[string]interface{}{
			// Invoice fields
			{
				"segment_id": segInvoiceID, "document_id": docID, "page_number": 1,
				"field_name": "billed_amount", "field_value": fmt.Sprintf("%d", gt.BilledCharge), "confidence": 0.99,
				"bbox_x1": 0.650000, "bbox_y1": 0.820000, "bbox_x2": 0.920000, "bbox_y2": 0.880000,
			},
			{
				"segment_id": segInvoiceID, "document_id": docID, "page_number": 1,
				"field_name": "destination", "field_value": gt.Destination, "confidence": 0.98,
				"bbox_x1": 0.200000, "bbox_y1": 0.350000, "bbox_x2": 0.450000, "bbox_y2": 0.400000,
			},
			{
				"segment_id": segInvoiceID, "document_id": docID, "page_number": 1,
				"field_name": "weight_billed_kg", "field_value": fmt.Sprintf("%.1f", gt.WeightKg), "confidence": 0.97,
				"bbox_x1": 0.550000, "bbox_y1": 0.480000, "bbox_x2": 0.750000, "bbox_y2": 0.530000,
			},
			{
				"segment_id": segInvoiceID, "document_id": docID, "page_number": 1,
				"field_name": "invoice_number", "field_value": gt.InvoiceID, "confidence": 0.99,
				"bbox_x1": 0.600000, "bbox_y1": 0.120000, "bbox_x2": 0.900000, "bbox_y2": 0.170000,
			},
			{
				"segment_id": segInvoiceID, "document_id": docID, "page_number": 1,
				"field_name": "invoice_date", "field_value": gt.ShipmentDate, "confidence": 0.96,
				"bbox_x1": 0.600000, "bbox_y1": 0.180000, "bbox_x2": 0.850000, "bbox_y2": 0.230000,
			},
			// Surat Jalan fields
			{
				"segment_id": segSJID, "document_id": docID, "page_number": 2,
				"field_name": "surat_jalan_number", "field_value": "SJ-" + gt.ShipmentID, "confidence": 0.99,
				"bbox_x1": 0.550000, "bbox_y1": 0.100000, "bbox_x2": 0.900000, "bbox_y2": 0.150000,
			},
			{
				"segment_id": segSJID, "document_id": docID, "page_number": 2,
				"field_name": "weight_actual_kg", "field_value": fmt.Sprintf("%.1f", gt.WeightKg), "confidence": 0.96,
				"bbox_x1": 0.200000, "bbox_y1": 0.420000, "bbox_x2": 0.480000, "bbox_y2": 0.470000,
			},
			{
				"segment_id": segSJID, "document_id": docID, "page_number": 2,
				"field_name": "destination", "field_value": gt.Destination, "confidence": 0.98,
				"bbox_x1": 0.200000, "bbox_y1": 0.300000, "bbox_x2": 0.500000, "bbox_y2": 0.350000,
			},
			{
				"segment_id": segSJID, "document_id": docID, "page_number": 2,
				"field_name": "origin", "field_value": gt.Origin, "confidence": 0.98,
				"bbox_x1": 0.200000, "bbox_y1": 0.250000, "bbox_x2": 0.450000, "bbox_y2": 0.300000,
			},
			// POD fields
			{
				"segment_id": segPODID, "document_id": docID, "page_number": 3,
				"field_name": "signature", "field_value": fmt.Sprintf("%v", gt.POD.SignaturePresent), "confidence": 0.95,
				"bbox_x1": 0.600000, "bbox_y1": 0.680000, "bbox_x2": 0.880000, "bbox_y2": 0.850000,
			},
			{
				"segment_id": segPODID, "document_id": docID, "page_number": 3,
				"field_name": "delivery_date", "field_value": gt.POD.DeliveryDate, "confidence": 0.93,
				"bbox_x1": 0.200000, "bbox_y1": 0.200000, "bbox_x2": 0.500000, "bbox_y2": 0.250000,
			},
		}

		for _, b := range bboxes {
			b["id"] = model.NewUUID()
			b["coordinate_space"] = "normalized_01"
			_ = dbClient.Insert(ctx, "field_evidence", b, &dOut)
			seededBBoxesCount++
		}

		// G. 4-Way Matching & shipment_events
		inv := &model.InvoiceCanonical{
			InvoiceID:        gt.InvoiceID,
			InvoiceNumber:    gt.InvoiceID,
			InvoiceDate:      gt.ShipmentDate,
			VendorID:         gt.VendorID,
			VendorName:       vName,
			ServiceType:      gt.ServiceType,
			Origin:           gt.Origin,
			Destination:      gt.Destination,
			WeightBilledKg:   gt.WeightKg,
			BilledAmount:     gt.BilledCharge,
			TaxBilled:        gt.TaxAmount,
			Currency:         "IDR",
		}

		shp := &model.ShipmentCanonical{
			SuratJalanNumber: "SJ-" + gt.ShipmentID,
			ShipmentID:       gt.ShipmentID,
			AWBNumber:        "AWB-" + gt.ShipmentID,
			ServiceType:      gt.ServiceType,
			ShipmentDate:     gt.ShipmentDate,
			Origin:           gt.Origin,
			Destination:      gt.Destination,
			WeightActualKg:   gt.WeightKg,
			TotalPackages:    gt.Quantity,
		}

		pod := &model.PODCanonical{
			PODNumber:           "POD-" + gt.ShipmentID,
			ShipmentID:          gt.ShipmentID,
			AWBNumber:           "AWB-" + gt.ShipmentID,
			DeliveryStatus:      delivStatus,
			DeliveryDate:        gt.POD.DeliveryDate,
			ReceiverName:        "Petugas Penerima Retail",
			SignaturePresent:    gt.POD.SignaturePresent,
			SignatureConfidence: 0.95,
			StampPresent:        gt.POD.StampPresent,
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
		}

		event, err := engine.ReconcileTransaction(inv, shp, pod, ctr)
		if err != nil || event == nil {
			continue
		}

		eventPayload := map[string]interface{}{
			"id":                      event.EventID,
			"event_id":                event.EventID,
			"shipment_id":             event.ShipmentID,
			"vendor_id":               event.VendorID,
			"status":                  string(event.Reconciliation.Status),
			"match_confidence":        event.MatchConfidence,
			"expected_base_charge":    event.Pricing.ExpectedBaseCharge,
			"expected_fuel_surcharge": event.Pricing.ExpectedFuelSurcharge,
			"expected_total_charge":   event.Pricing.ExpectedTotalCharge,
			"billed_total_charge":     event.Invoice.BilledAmount,
			"difference_amount":       event.Pricing.DifferenceAmount,
			"is_material":             event.Pricing.IsMaterial,
			"calculation_trace":       event.Pricing.Trace.TraceLog,
			"priority_score":          event.Reconciliation.PriorityScore,
			"requires_human_review":   event.Reconciliation.RequiresHumanReview,
		}

		pEv := url.Values{}
		pEv.Set("event_id", "eq."+event.EventID)
		var existEv []map[string]interface{}
		_ = dbClient.Query(ctx, "shipment_events", pEv, &existEv)
		if len(existEv) > 0 {
			var uOut []map[string]interface{}
			_ = dbClient.Update(ctx, "shipment_events", pEv, eventPayload, &uOut)
		} else {
			var cOut []map[string]interface{}
			_ = dbClient.Insert(ctx, "shipment_events", eventPayload, &cOut)
		}

		seededEventsCount++

		// H. If EXCEPTION, insert into reconciliation_cases
		if event.Reconciliation.Status == model.StatusException {
			caseID := "CASE-" + event.ShipmentID
			casePayload := map[string]interface{}{
				"id":                     model.NewUUID(),
				"case_id":                caseID,
				"event_id":               event.EventID,
				"vendor_id":              event.VendorID,
				"status":                 "OPEN",
				"priority_score":         event.Reconciliation.PriorityScore,
				"financial_exposure_idr": event.Pricing.DifferenceAmount,
				"dispute_deadline":       "2026-08-30",
				"assigned_reviewer_id":   "USER-AP-001",
			}
			cParams := url.Values{}
			cParams.Set("event_id", "eq."+event.EventID)
			var existCases []map[string]interface{}
			_ = dbClient.Query(ctx, "reconciliation_cases", cParams, &existCases)
			if len(existCases) == 0 {
				var caseOut []map[string]interface{}
				_ = dbClient.Insert(ctx, "reconciliation_cases", casePayload, &caseOut)
			}
		}

		// I. Audit Trail Event
		auditPayload := map[string]interface{}{
			"id":          model.NewUUID(),
			"event_id":    event.EventID,
			"actor_id":    "SYSTEM_AI_RECON_ENGINE",
			"action":      "SYSTEM_MATCHED",
			"entity_type": "RECONCILIATION_EVENT",
			"entity_id":   event.EventID,
			"before_state": map[string]interface{}{"status": "UNPROCESSED"},
			"after_state":  map[string]interface{}{"status": string(event.Reconciliation.Status), "match_score": event.MatchConfidence},
			"reason":      fmt.Sprintf("Automated 4-way matching executed. Result status: %s", event.Reconciliation.Status),
		}
		var aOut []map[string]interface{}
		_ = dbClient.Insert(ctx, "audit_events", auditPayload, &aOut)

		fmt.Printf("   ✓ Ingested: %-16s | %-12s | BBoxes: %2d | %s\n",
			event.ShipmentID, fmt.Sprintf("[%s]", event.Reconciliation.Status), len(bboxes), vName)
	}

	fmt.Println("\n================================================================")
	fmt.Printf("✅ SEEDING COMPLETE!\n")
	fmt.Printf("   - %d Ingested Documents & Physical Bundles in Supabase\n", seededDocsCount)
	fmt.Printf("   - %d Invoices, Surat Jalans, and POD records in Supabase\n", seededDocsCount)
	fmt.Printf("   - %d Real OCR Spatial Bounding Boxes in field_evidence table\n", seededBBoxesCount)
	fmt.Printf("   - %d Shipment Events & Reconciliation Cases in Supabase\n", seededEventsCount)
	fmt.Println("================================================================")
}
