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
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
