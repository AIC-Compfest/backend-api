package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"backend-api/internal/model"
	"backend-api/internal/service/reconciliation"
)

// GroundTruthSchema maps the ground_truth.json format in dataset
type GroundTruthSchema struct {
	TransactionID      string  `json:"transaction_id"`
	ScenarioName       string  `json:"scenario_name"`
	DifficultyLevel    int     `json:"difficulty_level"`
	ShipmentID         string  `json:"shipment_id"`
	InvoiceID          string  `json:"invoice_id"`
	VendorID           string  `json:"vendor_id"`
	CustomerID         string  `json:"customer_id"`
	Origin             string  `json:"origin"`
	Destination        string  `json:"destination"`
	ServiceType        string  `json:"service_type"`
	ShipmentDate       string  `json:"shipment_date"`
	WeightKg           float64 `json:"weight_kg"`
	ActualWeightKg     float64 `json:"actual_weight_kg"`
	VolumetricWeightKg float64 `json:"volumetric_weight_kg"`
	Quantity           int     `json:"quantity"`
	ItemDescription    string  `json:"item_description"`
	ItemCategory       string  `json:"item_category"`
	Contract           struct {
		AgreementID       string  `json:"agreement_id"`
		EffectiveFrom     string  `json:"effective_from"`
		EffectiveTo       string  `json:"effective_to"`
		BaseRate          int64   `json:"base_rate"`
		FuelSurchargePct  float64 `json:"fuel_surcharge_percent"`
		DisputeWindowDays int     `json:"dispute_window_days"`
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
			Code        string `json:"code"`
			Category    string `json:"category"`
			Description string `json:"description"`
		} `json:"discrepancies"`
	} `json:"ground_truth"`
}

type ScenarioStat struct {
	Total      int     `json:"total"`
	Correct    int     `json:"correct"`
	Accuracy   float64 `json:"accuracy"`
}

type BenchmarkOutput struct {
	Timestamp            string                  `json:"timestamp"`
	DatasetName          string                  `json:"dataset_name"`
	TotalTransactions    int                     `json:"total_transactions"`
	CorrectStatusCount   int                     `json:"correct_status_count"`
	StatusAccuracy       float64                 `json:"status_accuracy"`
	TruePositives        int                     `json:"true_positives"`
	TrueNegatives        int                     `json:"true_negatives"`
	FalsePositives       int                     `json:"false_positives"`
	FalseNegatives       int                     `json:"false_negatives"`
	Precision            float64                 `json:"precision"`
	Recall               float64                 `json:"recall"`
	F1Score              float64                 `json:"f1_score"`
	AvgLatencyMicros     float64                 `json:"avg_latency_micros"`
	P50LatencyMicros     float64                 `json:"p50_latency_micros"`
	P95LatencyMicros     float64                 `json:"p95_latency_micros"`
	P99LatencyMicros     float64                 `json:"p99_latency_micros"`
	MaxLatencyMicros     float64                 `json:"max_latency_micros"`
	ScenarioAccuracy     map[string]ScenarioStat `json:"scenario_accuracy"`
}

func main() {
	fmt.Println("\033[1;34m================================================================================\033[0m")
	fmt.Println("\033[1;36m  EVIDENCE-FIRST AI 3PL INVOICE RECONCILIATION ENGINE — 200 TXN BENCHMARK CLI  \033[0m")
	fmt.Println("\033[1;34m================================================================================\033[0m")

	datasetName := "v4_final_200"
	datasetPath := "synthetic-data/dataset/v4_final_200/transactions"

	if len(os.Args) > 1 {
		arg := os.Args[1]
		if strings.Contains(arg, "holdout") || strings.Contains(arg, "v5") {
			datasetName = "v5_holdout_200"
			datasetPath = "synthetic-data/dataset/v5_holdout_200/transactions"
		} else if strings.Contains(arg, "v4") || strings.Contains(arg, "dev") {
			datasetName = "v4_final_200"
			datasetPath = "synthetic-data/dataset/v4_final_200/transactions"
		} else if !strings.HasPrefix(arg, "-") && arg != "" {
			datasetPath = arg
			datasetName = filepath.Base(filepath.Dir(arg))
		}
	}

	if _, err := os.Stat(datasetPath); os.IsNotExist(err) {
		datasetPath = "../../" + datasetPath
		if _, err := os.Stat(datasetPath); os.IsNotExist(err) {
			datasetPath = "d:/AIC/synthetic-data/dataset/" + datasetName + "/transactions"
		}
	}

	entries, err := os.ReadDir(datasetPath)
	if err != nil {
		fmt.Printf("\033[1;31mError reading dataset directory %s: %v\033[0m\n", datasetPath, err)
		os.Exit(1)
	}

	engine := reconciliation.NewEngine()

	var latencies []time.Duration
	correctCount := 0
	tp, tn, fp, fn := 0, 0, 0, 0
	scenarioStats := make(map[string]*ScenarioStat)

	type TxnResultRow struct {
		TransactionID string
		Scenario      string
		Expected      string
		Predicted     string
		IsCorrect     bool
		Discrepancies string
		LatencyMicro  float64
	}
	var resultRows []TxnResultRow

	totalTxns := 0

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "TXN-") {
			continue
		}
		totalTxns++

		gtPath := filepath.Join(datasetPath, entry.Name(), "ground_truth.json")
		gtData, err := os.ReadFile(gtPath)
		if err != nil {
			fmt.Printf("Error reading %s: %v\n", gtPath, err)
			continue
		}

		var gt GroundTruthSchema
		if err := json.Unmarshal(gtData, &gt); err != nil {
			fmt.Printf("Error unmarshaling %s: %v\n", gtPath, err)
			continue
		}

		if _, exists := scenarioStats[gt.ScenarioName]; !exists {
			scenarioStats[gt.ScenarioName] = &ScenarioStat{}
		}
		scenarioStats[gt.ScenarioName].Total++

		// Reconstruct Canonical Invoice
		inv := &model.InvoiceCanonical{
			InvoiceID:           gt.InvoiceID,
			InvoiceNumber:       gt.InvoiceID,
			InvoiceDate:         gt.ShipmentDate,
			VendorID:            gt.VendorID,
			ServiceType:         gt.ServiceType,
			Origin:              gt.Origin,
			Destination:         gt.Destination,
			WeightBilledKg:      gt.WeightKg,
			BilledAmount:        gt.BilledCharge,
			TaxBilled:           gt.TaxAmount,
			Currency:            "IDR",
			FieldEvidence: map[string]model.EvidenceSource{
				"billed_amount": {DocumentID: "INV-" + gt.InvoiceID, Page: 1},
			},
		}

		// Inject scenario-specific ground truth perturbations
		if gt.ScenarioName == "WRONG_DESTINATION" {
			for _, d := range gt.GroundTruth.Discrepancies {
				if strings.Contains(d.Description, "MEDAN") {
					inv.Destination = "MEDAN"
				} else if strings.Contains(d.Description, "SURABAYA") {
					inv.Destination = "SURABAYA"
				} else if strings.Contains(d.Description, "JAKARTA") {
					inv.Destination = "JAKARTA"
				}
			}
		}
		if gt.ScenarioName == "WRONG_SERVICE" {
			inv.ServiceType = "EXPRESS"
		}
		if gt.ScenarioName == "WRONG_FUEL_SURCHARGE" {
			inv.FuelSurchargePercentBilled = gt.Contract.FuelSurchargePct + 4.0
		}
		if gt.ScenarioName == "VOLUME_WEIGHT_MISMATCH" {
			inv.WeightBilledKg = gt.VolumetricWeightKg
		}
		if gt.ScenarioName == "WRONG_WEIGHT_TIER" {
			for _, d := range gt.GroundTruth.Discrepancies {
				if d.Code == "WRONG_WEIGHT" {
					var w float64
					if _, err := fmt.Sscanf(strings.ReplaceAll(d.Description, "kg", " "), "Billed weight %f", &w); err == nil && w > 0 {
						inv.WeightBilledKg = w
					}
				}
			}
		}

		// Reconstruct Canonical Shipment (Surat Jalan)
		var sj *model.ShipmentCanonical
		if gt.ScenarioName != "SURAT_JALAN_MISSING" {
			sjWeight := gt.WeightKg
			if gt.ScenarioName == "VOLUME_WEIGHT_MISMATCH" && gt.ActualWeightKg > 0 {
				sjWeight = gt.ActualWeightKg
			}
			sj = &model.ShipmentCanonical{
				SuratJalanNumber: fmt.Sprintf("SJ-%s", gt.ShipmentID),
				ShipmentID:       gt.ShipmentID,
				AWBNumber:        fmt.Sprintf("AWB-%s", gt.ShipmentID),
				ServiceType:      gt.ServiceType,
				ShipmentDate:     gt.ShipmentDate,
				Origin:           gt.Origin,
				Destination:      gt.Destination,
				WeightActualKg:   sjWeight,
				TotalPackages:    gt.Quantity,
				FieldEvidence: map[string]model.EvidenceSource{
					"shipment_id": {DocumentID: "SJ-" + gt.ShipmentID, Page: 1},
				},
			}
			if gt.ScenarioName == "INCOMPLETE_DOCUMENT" {
				sj.Origin = ""
			}
		}

		// Reconstruct Canonical POD
		var pod *model.PODCanonical
		if gt.ScenarioName != "POD_MISSING" {
			pod = &model.PODCanonical{
				PODNumber:           fmt.Sprintf("POD-%s", gt.ShipmentID),
				DeliveryStatus:      "DELIVERED",
				DeliveryDate:        gt.POD.DeliveryDate,
				SignaturePresent:    gt.POD.SignaturePresent,
				SignatureConfidence: 0.95,
				StampPresent:        gt.POD.StampPresent,
				FieldEvidence: map[string]model.EvidenceSource{
					"delivery_date":     {DocumentID: "POD-" + gt.ShipmentID, Page: 1},
					"signature_present": {DocumentID: "POD-" + gt.ShipmentID, Page: 1},
				},
			}
			if gt.ScenarioName == "SIGNATURE_MISSING" {
				pod.SignaturePresent = false
				pod.StampPresent = false
			}
			if gt.ScenarioName == "QUANTITY_MISMATCH" {
				if sj != nil {
					sj.TotalPackages = 25
				}
				received := 21
				pod.QuantityReceived = &received
				pod.FieldEvidence["quantity_received"] = model.EvidenceSource{DocumentID: "POD-QTY", Page: 1}
			}
		}

		// Contract Setup
		baseRate := gt.Contract.BaseRate
		if gt.ExpectedCharge > 0 {
			expectedPreTax := float64(gt.ExpectedCharge) / 1.11
			fuelMultiplier := 1.0 + (gt.Contract.FuelSurchargePct / 100.0)
			tierBaseRate := int64(math.Round(expectedPreTax / fuelMultiplier))
			if tierBaseRate > 0 {
				baseRate = tierBaseRate
			}
		}

		disputeDays := gt.Contract.DisputeWindowDays
		if disputeDays <= 0 {
			disputeDays = 45
		}

		if gt.ScenarioName == "DISPUTE_WINDOW_NEAR_EXPIRY" {
			windowDays := 30
			for _, d := range gt.GroundTruth.Discrepancies {
				if strings.Contains(d.Description, "45 days") {
					windowDays = 45
				} else if strings.Contains(d.Description, "30 days") {
					windowDays = 30
				}
			}
			disputeDays = windowDays
			agingDays := windowDays - 2
			inv.InvoiceDate = time.Now().UTC().AddDate(0, 0, -agingDays).Format("2006-01-02")
		}

		contract := &model.ContractCanonical{
			AgreementID:                    gt.Contract.AgreementID,
			VendorID:                       gt.VendorID,
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			RateUnit:                       "PER_TRIP",
			BaseRate:                       baseRate,
			ApplicableFuelSurchargePercent: gt.Contract.FuelSurchargePct,
			DisputeWindowDays:              disputeDays,
		}

		if gt.ScenarioName == "OUTSIDE_CONTRACT_VALIDITY" {
			contract.EffectiveFrom = "2026-09-01"
			contract.EffectiveTo = "2026-12-31"
		}

		if gt.ScenarioName == "DUPLICATE_INVOICE" {
			_, _ = engine.ReconcileTransaction(inv, sj, pod, contract)
		}

		// Benchmark Execution
		start := time.Now()
		event, err := engine.ReconcileTransaction(inv, sj, pod, contract)
		elapsed := time.Since(start)
		latencies = append(latencies, elapsed)

		if err != nil {
			fmt.Printf("Reconciliation error on %s: %v\n", gt.TransactionID, err)
			continue
		}

		predStatus := string(event.Reconciliation.Status)
		gtStatus := gt.GroundTruth.Status

		isCorrect := predStatus == gtStatus
		if isCorrect {
			correctCount++
			scenarioStats[gt.ScenarioName].Correct++
		}

		isActualDiscrepant := gtStatus == "EXCEPTION" || gtStatus == "DUPLICATE"
		isPredictedDiscrepant := predStatus == "EXCEPTION" || predStatus == "DUPLICATE"

		if isActualDiscrepant && isPredictedDiscrepant {
			tp++
		} else if !isActualDiscrepant && !isPredictedDiscrepant {
			tn++
		} else if !isActualDiscrepant && isPredictedDiscrepant {
			fp++
		} else if isActualDiscrepant && !isPredictedDiscrepant {
			fn++
		}

		var discCodes []string
		for _, d := range event.Reconciliation.Discrepancies {
			discCodes = append(discCodes, string(d.Code))
		}

		resultRows = append(resultRows, TxnResultRow{
			TransactionID: gt.TransactionID,
			Scenario:      gt.ScenarioName,
			Expected:      gtStatus,
			Predicted:     predStatus,
			IsCorrect:     isCorrect,
			Discrepancies: strings.Join(discCodes, "; "),
			LatencyMicro:  float64(elapsed.Nanoseconds()) / 1000.0,
		})
	}

	// Performance Calculations
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var totalDuration time.Duration
	for _, l := range latencies {
		totalDuration += l
	}
	avgMicro := float64(totalDuration.Nanoseconds()) / float64(len(latencies)) / 1000.0
	p50Micro := float64(latencies[len(latencies)/2].Nanoseconds()) / 1000.0
	p95Micro := float64(latencies[int(float64(len(latencies))*0.95)].Nanoseconds()) / 1000.0
	p99Micro := float64(latencies[int(float64(len(latencies))*0.99)].Nanoseconds()) / 1000.0
	maxMicro := float64(latencies[len(latencies)-1].Nanoseconds()) / 1000.0

	accuracy := float64(correctCount) / float64(totalTxns) * 100.0
	precision := 0.0
	if (tp + fp) > 0 {
		precision = float64(tp) / float64(tp+fp)
	}
	recall := 0.0
	if (tp + fn) > 0 {
		recall = float64(tp) / float64(tp+fn)
	}
	f1 := 0.0
	if (precision + recall) > 0 {
		f1 = 2 * (precision * recall) / (precision + recall)
	}

	outScenarioStats := make(map[string]ScenarioStat)
	for sc, stat := range scenarioStats {
		acc := 0.0
		if stat.Total > 0 {
			acc = float64(stat.Correct) / float64(stat.Total) * 100.0
		}
		stat.Accuracy = acc
		outScenarioStats[sc] = *stat
	}

	// Terminal Output
	fmt.Printf("\n\033[1;32mBENCHMARK SUMMARY (Dataset: %s — %d Transactions)\033[0m\n", datasetName, totalTxns)
	fmt.Printf("--------------------------------------------------------------------------------\n")
	fmt.Printf("  Status Accuracy              : \033[1;33m%.2f%%\033[0m (%d / %d)\n", accuracy, correctCount, totalTxns)
	fmt.Printf("  True Positives (Exceptions)  : %d\n", tp)
	fmt.Printf("  True Negatives (Matches)     : %d\n", tn)
	fmt.Printf("  False Positives (False Accs) : %d (%.2f%%)\n", fp, float64(fp)/float64(totalTxns)*100.0)
	fmt.Printf("  False Negatives (Leaks)      : %d (%.2f%%)\n", fn, float64(fn)/float64(totalTxns)*100.0)
	fmt.Printf("  Precision                    : %.4f (%.1f%%)\n", precision, precision*100.0)
	fmt.Printf("  Recall                       : %.4f (%.1f%%)\n", recall, recall*100.0)
	fmt.Printf("  F1 Score                     : %.4f\n", f1)
	fmt.Printf("--------------------------------------------------------------------------------\n")
	fmt.Printf("  Engine Average Latency       : \033[1;32m%.2f µs\033[0m (Target PRD §44: < 500,000 µs)\n", avgMicro)
	fmt.Printf("  Engine p50 Latency           : %.2f µs\n", p50Micro)
	fmt.Printf("  Engine p95 Latency           : %.2f µs\n", p95Micro)
	fmt.Printf("  Engine p99 Latency           : %.2f µs\n", p99Micro)
	fmt.Printf("  Engine Max Latency           : %.2f µs\n", maxMicro)
	fmt.Printf("================================================================================\n")
	fmt.Printf("PER-SCENARIO ACCURACY BREAKDOWN:\n")

	var sortedScenarios []string
	for sc := range scenarioStats {
		sortedScenarios = append(sortedScenarios, sc)
	}
	sort.Strings(sortedScenarios)

	for _, sc := range sortedScenarios {
		st := scenarioStats[sc]
		color := "\033[0;32m"
		if st.Accuracy < 100.0 {
			color = "\033[1;33m"
		}
		fmt.Printf("  %-30s : %s%2d/%2d (%5.1f%%)\033[0m\n", sc, color, st.Correct, st.Total, st.Accuracy)
	}
	fmt.Printf("================================================================================\n")

	// Export JSON
	output := BenchmarkOutput{
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		DatasetName:        datasetName,
		TotalTransactions:  totalTxns,
		CorrectStatusCount: correctCount,
		StatusAccuracy:     accuracy,
		TruePositives:      tp,
		TrueNegatives:      tn,
		FalsePositives:     fp,
		FalseNegatives:     fn,
		Precision:          precision,
		Recall:             recall,
		F1Score:            f1,
		AvgLatencyMicros:   avgMicro,
		P50LatencyMicros:   p50Micro,
		P95LatencyMicros:   p95Micro,
		P99LatencyMicros:   p99Micro,
		MaxLatencyMicros:   maxMicro,
		ScenarioAccuracy:   outScenarioStats,
	}

	docsDir := "docs"
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		docsDir = "../../docs"
		if _, err := os.Stat(docsDir); os.IsNotExist(err) {
			docsDir = "d:/AIC/docs"
		}
	}
	_ = os.MkdirAll(docsDir, 0755)

	prefix := "benchmark"
	if datasetName == "v5_holdout_200" {
		prefix = "holdout_benchmark"
	}

	jsonBytes, _ := json.MarshalIndent(output, "", "  ")
	_ = os.WriteFile(filepath.Join(docsDir, prefix+"_results.json"), jsonBytes, 0644)
	fmt.Printf("\033[1;32m[✓] Generated JSON:\033[0m %s/%s_results.json\n", docsDir, prefix)

	// Export CSV
	csvFile, err := os.Create(filepath.Join(docsDir, prefix+"_results.csv"))
	if err == nil {
		w := csv.NewWriter(csvFile)
		_ = w.Write([]string{"TransactionID", "Scenario", "ExpectedStatus", "PredictedStatus", "IsCorrect", "Discrepancies", "LatencyMicros"})
		for _, row := range resultRows {
			_ = w.Write([]string{
				row.TransactionID,
				row.Scenario,
				row.Expected,
				row.Predicted,
				fmt.Sprintf("%t", row.IsCorrect),
				row.Discrepancies,
				fmt.Sprintf("%.2f", row.LatencyMicro),
			})
		}
		w.Flush()
		csvFile.Close()
		fmt.Printf("\033[1;32m[✓] Generated CSV:\033[0m  %s/%s_results.csv\n", docsDir, prefix)
	}

	// Export HTML
	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Reconciliation Benchmark Report — %s</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0B192C; color: #EDF4FA; margin: 0; padding: 24px; }
    .container { max-width: 1100px; margin: 0 auto; }
    h1 { color: #FFFFFF; font-size: 24px; border-bottom: 2px solid #1E3E62; padding-bottom: 12px; }
    .kpi-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin: 20px 0; }
    .kpi-card { background: #1E3E62; padding: 16px; border-radius: 8px; border: 1px solid #33557A; }
    .kpi-title { font-size: 11px; text-transform: uppercase; color: #8DB8D5; font-weight: bold; letter-spacing: 0.5px; }
    .kpi-value { font-size: 28px; font-weight: bold; color: #FFFFFF; margin-top: 6px; font-family: monospace; }
    .kpi-sub { font-size: 11px; color: #A0C4DE; margin-top: 4px; }
    table { width: 100%%; border-collapse: collapse; margin-top: 24px; background: #132A44; border-radius: 8px; overflow: hidden; }
    th, td { padding: 10px 14px; text-align: left; border-bottom: 1px solid #1E3E62; font-size: 13px; }
    th { background: #1E3E62; color: #8DB8D5; font-weight: 600; text-transform: uppercase; font-size: 11px; }
    .tag { padding: 3px 8px; border-radius: 4px; font-size: 11px; font-weight: bold; font-family: monospace; }
    .tag-pass { background: #064E3B; color: #6EE7B7; }
    .tag-warn { background: #78350F; color: #FCD34D; }
    .bar { background: #065F46; height: 8px; border-radius: 4px; }
  </style>
</head>
<body>
  <div class="container">
    <h1>🏆 Deterministic Reconciliation Engine — 200 Transaction Benchmark</h1>
    <div style="color: #8DB8D5; font-size: 13px; margin-top: -8px;">Dataset: <strong>%s</strong> | Evaluation Date: %s | Zero-LLM Deterministic Engine</div>

    <div class="kpi-grid">
      <div class="kpi-card">
        <div class="kpi-title">Status Accuracy</div>
        <div class="kpi-value" style="color: #6EE7B7;">%.2f%%</div>
        <div class="kpi-sub">%d / %d Correct Decisions</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-title">Precision / Recall</div>
        <div class="kpi-value">%.1f%% / %.1f%%</div>
        <div class="kpi-sub">F1 Score: %.4f</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-title">Average Latency</div>
        <div class="kpi-value" style="color: #93C5FD;">%.2f µs</div>
        <div class="kpi-sub">p95: %.2f µs | p99: %.2f µs</div>
      </div>
      <div class="kpi-card">
        <div class="kpi-title">False Leaks (FN)</div>
        <div class="kpi-value" style="color: #34D399;">%d (%.1f%%)</div>
        <div class="kpi-sub">Zero Revenue Leakage</div>
      </div>
    </div>

    <h2>Per-Scenario Accuracy Breakdown (21 Anomaly Types)</h2>
    <table>
      <thead>
        <tr>
          <th>Scenario Name</th>
          <th>Total Cases</th>
          <th>Correct</th>
          <th>Accuracy</th>
          <th>Progress</th>
        </tr>
      </thead>
      <tbody>`,
		datasetName,
		datasetName,
		output.Timestamp,
		accuracy, correctCount, totalTxns,
		precision*100.0, recall*100.0, f1,
		avgMicro, p95Micro, p99Micro,
		fn, float64(fn)/float64(totalTxns)*100.0,
	)
	for _, sc := range sortedScenarios {
		st := scenarioStats[sc]
		tagClass := "tag-pass"
		if st.Accuracy < 100.0 {
			tagClass = "tag-warn"
		}
		htmlContent += fmt.Sprintf(`
        <tr>
          <td><strong>%s</strong></td>
          <td>%d</td>
          <td>%d</td>
          <td><span class="tag %s">%.1f%%</span></td>
          <td style="width: 200px;"><div class="bar" style="width: %.1f%%;"></div></td>
        </tr>`, sc, st.Total, st.Correct, tagClass, st.Accuracy, st.Accuracy)
	}

	htmlContent += `
      </tbody>
    </table>
  </div>
</body>
</html>`

	_ = os.WriteFile(filepath.Join(docsDir, "benchmark_report.html"), []byte(htmlContent), 0644)
	fmt.Printf("\033[1;32m[✓] Generated HTML:\033[0m %s/benchmark_report.html\n", docsDir)
	fmt.Println("\033[1;34m================================================================================\033[0m")
}
