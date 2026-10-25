package document

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"backend-api/internal/database"
	"backend-api/internal/model"
)

func TestPersistAIResultPersistsNoModelRunAsReviewableLineage(t *testing.T) {
	var mu sync.Mutex
	var tables []string
	payloadsByTable := map[string][]map[string]interface{}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		table := strings.TrimPrefix(r.URL.Path, "/rest/v1/")
		mu.Lock()
		tables = append(tables, table)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && table == "document_segments" {
			_, _ = w.Write([]byte(`[{"id":"22222222-2222-4222-8222-222222222222","segment_number":1}]`))
			return
		}
		if r.Method == http.MethodPost {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			payloadsByTable[table] = append(payloadsByTable[table], payload)
			mu.Unlock()
			if id, ok := payload["id"].(string); ok {
				_, _ = w.Write([]byte(`[{"id":"` + id + `"}]`))
				return
			}
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := database.NewClient(server.URL, "test-key")
	db := NewResourceDB(client)
	result := &AIExtractionResult{
		Status:          "SUCCESS",
		PipelineVersion: "phase6-test",
		Classification:  map[string]any{"document_type": "INVOICE"},
		MLExtraction:    map[string]any{"status": "no_model_for_doc_type", "fields": []any{}},
		SegmentExtractions: []AISegmentExtraction{{
			DocumentType: "INVOICE",
			ExtractedFields: []model.ExtractedField{{
				Field:      "invoice_number",
				Value:      "INV-TEST-001",
				Confidence: 0.91,
				Source:     model.EvidenceSource{Page: 1},
			}, {
				Field:      "unknown_field",
				Value:      nil,
				Confidence: 0,
				IsUnknown:  true,
				Source:     model.EvidenceSource{Page: 1},
			}, {
				Field: "low_confidence_suggestion", Value: "PO-TEST-0388", Confidence: 0.55,
				RawText: "No Pesanan: PO-TEST-0388", ReviewStatus: "REVIEW", LowConfidence: true,
				SourceKind: "rule", Source: model.EvidenceSource{Page: 1},
			}},
		}},
		Pages:      []AIPageResult{{PageNumber: 1, PageImage: AIPageImage{Width: 100, Height: 100}}},
		Evidence:   json.RawMessage(`{"pages":[{"page_number":1,"lines":[{"text":"Invoice INV-TEST-001","confidence":0.98,"bbox":[0.1,0.2,0.8,0.3]}],"tables":[{"table_id":"page-1-table-1","headers":["Field","Value"],"rows":[["Invoice Number","INV-TEST-001"]],"bbox_norm":[0.1,0.2,0.8,0.4],"confidence":0.95}]}],"table_groups":[]}`),
		RawPayload: json.RawMessage(`{"status":"SUCCESS","ml_extraction":{"status":"no_model_for_doc_type"}}`),
	}

	if err := db.PersistAIResult(context.Background(), "11111111-1111-4111-8111-111111111111", "", "invoice.pdf", "hash", "application/pdf", result); err != nil {
		t.Fatalf("PersistAIResult: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, table := range []string{
		"ai_pipeline_runs", "ai_pipeline_steps", "ai_evidence_pages",
		"ai_evidence_lines", "ai_evidence_tables",
		"ai_candidate_generation_runs", "ai_extraction_runs",
		"ai_field_candidates", "ai_field_predictions",
		"ai_prediction_candidate_scores", "ai_field_decisions",
	} {
		if !contains(tables, table) {
			t.Errorf("expected persistence to %s; calls=%v", table, tables)
		}
	}
	var unknownPredictionID string
	for _, table := range []string{"ai_field_candidates", "ai_field_predictions"} {
		found := false
		for _, payload := range payloadsByTable[table] {
			if payload["field_name"] != "unknown_field" {
				continue
			}
			found = true
			if payload["parsed_value"] != nil || payload["predicted_value"] != nil || payload["final_value"] != nil {
				t.Errorf("%s persisted an unknown field value: %#v", table, payload)
			}
			if payload["raw_text"] == "<nil>" || payload["predicted_raw_text"] == "<nil>" || payload["final_raw_text"] == "<nil>" {
				t.Errorf("%s persisted the Go nil sentinel as raw text: %#v", table, payload)
			}
			if table == "ai_field_predictions" && (payload["prediction_status"] != "UNKNOWN" || payload["score"] != float64(0)) {
				t.Errorf("unknown prediction should have UNKNOWN status and zero score: %#v", payload)
			}
			if table == "ai_field_predictions" {
				unknownPredictionID, _ = payload["id"].(string)
			}
		}
		if !found {
			t.Errorf("expected an unknown_field payload in %s", table)
		}
	}
	decisionFound := false
	for _, payload := range payloadsByTable["ai_field_decisions"] {
		if payload["prediction_id"] != unknownPredictionID {
			continue
		}
		decisionFound = true
		if payload["final_value"] != nil || payload["final_raw_text"] == "<nil>" || payload["final_raw_text"] != "" {
			t.Errorf("unknown field decision should stay empty: %#v", payload)
		}
	}
	if !decisionFound {
		t.Errorf("expected a decision for the unknown prediction %q", unknownPredictionID)
	}
	for _, payload := range payloadsByTable["ai_field_predictions"] {
		if payload["field_name"] == "low_confidence_suggestion" {
			if payload["predicted_value"] != "PO-TEST-0388" || payload["prediction_status"] != "REVIEW" || payload["score"] != 0.55 {
				t.Errorf("low-confidence suggestion was not preserved for review: %#v", payload)
			}
			if payload["predicted_raw_text"] != "PO-TEST-0388" {
				t.Errorf("display text should be parsed value, raw source stays in metadata: %#v", payload)
			}
			return
		}
	}
	t.Error("expected low-confidence suggestion prediction")
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
