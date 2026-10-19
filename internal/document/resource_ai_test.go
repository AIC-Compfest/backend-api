package document

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractViaAIUsesOfficialMultipartContractAndPreservesAdditiveMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/extract/pipeline/full" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("X-Correlation-ID"), "sha256:") {
			t.Errorf("missing correlation id: %q", r.Header.Get("X-Correlation-ID"))
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file form part: %v", err)
		}
		defer file.Close()
		content, _ := io.ReadAll(file)
		if header.Filename != "invoice.pdf" || string(content) != "pdf-bytes" {
			t.Errorf("uploaded part = %q / %q", header.Filename, content)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"document_id": "ai-doc-1", "status": "SUCCESS", "page_count": 1,
			"classification": map[string]any{"document_type": "INVOICE", "confidence": 0.94},
			"extracted_fields": []any{map[string]any{
				"field": "invoice_number", "value": "INV-1", "confidence": 0.9,
				"confidence_source": "line_scan_heuristic", "method": "rule_based", "is_unknown": false,
				"source": map[string]any{"document_id": "ai-doc-1", "page": 1},
			}},
			"table_groups":          []any{map[string]any{"group_id": "g1", "pages": []int{1, 2}, "row_count": 4}},
			"ml_extraction":         map[string]any{"status": "no_model_for_doc_type"},
			"arithmetic_validation": map[string]any{"status": "ok"},
			"requires_human_review": true, "pipeline_version": "phase6_v3_segment_aware",
		})
	}))
	defer server.Close()

	resource := NewResource(nil, nil)
	resource.SetAIServiceURL(server.URL)
	resource.SetAIServiceTimeout(2 * time.Second)
	result, err := resource.extractViaAI(context.Background(), "invoice.pdf", []byte("pdf-bytes"))
	if err != nil {
		t.Fatalf("extractViaAI: %v", err)
	}
	if result.Status != "SUCCESS" || len(result.ExtractedFields) != 1 || result.TableGroups[0]["group_id"] != "g1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ExtractedFields[0].ConfidenceSource != "line_scan_heuristic" || !result.RequiresHumanReview {
		t.Fatalf("additive metadata lost: %+v", result.ExtractedFields[0])
	}
}

func TestExtractViaAIRejectsNonSuccessHTTPResponsesWithBoundedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(strings.Repeat("x", 100000)))
	}))
	defer server.Close()

	resource := NewResource(nil, nil)
	resource.SetAIServiceURL(server.URL)
	result, err := resource.extractViaAI(context.Background(), "large.pdf", []byte("bytes"))
	if result != nil || err == nil || !strings.Contains(err.Error(), "status=413") {
		t.Fatalf("expected bounded 413 error, result=%v err=%v", result, err)
	}
	if len(err.Error()) > 20000 {
		t.Fatalf("error body was not bounded: %d", len(err.Error()))
	}
}

func TestValidateAIResultAllowsHumanReviewButRejectsMalformedSuccess(t *testing.T) {
	if err := validateAIResult(&AIExtractionResult{Status: "REQUIRES_HUMAN_REVIEW"}); err != nil {
		t.Fatalf("human review is a valid outcome: %v", err)
	}
	if err := validateAIResult(&AIExtractionResult{Status: "SUCCESS"}); err == nil {
		t.Fatal("success without document_id must be rejected")
	}
}
