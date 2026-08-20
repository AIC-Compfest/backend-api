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
	"backend-api/internal/contract"
	"backend-api/internal/database"
	"backend-api/internal/model"
	serviceRecon "backend-api/internal/service/reconciliation"
)

func main() {
	fmt.Println("================================================================")
	fmt.Println("🚀 SEEDING DATABASE SUPABASE — PILOT BENCHMARK DATASET (20 TXN)")
	fmt.Println("================================================================")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	dbClient := database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	ctx := context.Background()

	// 1. Seed Rate Agreements
	fmt.Println("\n📄 [1/3] Seeding Master Rate Agreements (PKS Matrix)...")
	contractDB := contract.NewResourceDB(dbClient)
	contractsList, _ := contractDB.ListAll(ctx)
	fmt.Printf("   ✓ Verified %d master PKS rate agreements in Supabase\n", len(contractsList))

	// 2. Load and Reconcile 20 Pilot Transactions
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
	fmt.Println("\n📦 [2/3] Processing 4-Way Matching & Ingesting into Supabase...")

	seededEventsCount := 0
	seededCasesCount := 0

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
		if err != nil || event == nil {
			continue
		}

		// Insert or Update shipment_events in Supabase
		eventPayload := map[string]interface{}{
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

		// Upsert logic: query existing by event_id
		params := url.Values{}
		params.Set("event_id", "eq."+event.EventID)
		var existingEvents []map[string]interface{}
		_ = dbClient.Query(ctx, "shipment_events", params, &existingEvents)

		if len(existingEvents) > 0 {
			var uOut []map[string]interface{}
			_ = dbClient.Update(ctx, "shipment_events", params, eventPayload, &uOut)
		} else {
			var cOut []map[string]interface{}
			_ = dbClient.Insert(ctx, "shipment_events", eventPayload, &cOut)
		}

		seededEventsCount++
		statusBadge := fmt.Sprintf("[%s]", event.Reconciliation.Status)
		fmt.Printf("   ✓ Ingested %-12s | %-16s | %-12s | Billed: Rp %-8d | %-20s\n",
			entry.Name(), event.ShipmentID, statusBadge, event.Invoice.BilledAmount, event.VendorID)

		// If EXCEPTION, insert into reconciliation_cases
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
			var existingCases []map[string]interface{}
			_ = dbClient.Query(ctx, "reconciliation_cases", cParams, &existingCases)
			if len(existingCases) == 0 {
				var caseOut []map[string]interface{}
				_ = dbClient.Insert(ctx, "reconciliation_cases", casePayload, &caseOut)
				seededCasesCount++
			}
		}
	}

	fmt.Println("\n================================================================")
	fmt.Printf("✅ SEEDING BERHASIL! Total Ingested: %d Events, %d Open Cases\n", seededEventsCount, seededCasesCount)
	fmt.Println("================================================================")
}
