package document

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPayloadSummaryKeepsTablesStructuredAndReadable(t *testing.T) {
	payload := map[string]interface{}{
		"kind": "table", "pages": []interface{}{float64(1)},
		"headers": []interface{}{"Kode", "Nama", "Jumlah"},
		"rows":    []interface{}{map[string]interface{}{"cells": []interface{}{"1", "item", "2"}}},
	}
	if got := payloadSummary(payload); got != "Tabel p1 · 1 baris × 3 kolom" {
		t.Fatalf("table summary = %q", got)
	}
	if strings.Contains(payloadSummary([]interface{}{map[string]interface{}{"x": 1}}), "[map[") {
		t.Fatal("object value leaked a Go map dump")
	}
}

func TestReviewValueValidatesStructuredTableRows(t *testing.T) {
	predicted := map[string]interface{}{"kind": "table"}
	valid := map[string]interface{}{"kind": "table", "headers": []interface{}{"Kode", "Nama"}, "rows": []interface{}{map[string]interface{}{"cells": []interface{}{"1", "A"}}}}
	if err := validateReviewValue(predicted, valid); err != nil {
		t.Fatalf("valid table correction rejected: %v", err)
	}
	invalid := map[string]interface{}{"kind": "table", "headers": []interface{}{"Kode", "Nama"}, "rows": []interface{}{map[string]interface{}{"cells": []interface{}{"1"}}}}
	if err := validateReviewValue(predicted, invalid); err == nil {
		t.Fatal("table correction with missing cell accepted")
	}
}

func TestReviewValueRejectsUnboundedLegacyScalar(t *testing.T) {
	if err := validateReviewValue("old", strings.Repeat("x", 161)); err == nil {
		t.Fatal("overlong legacy scalar was allowed into final review")
	}
	if err := validateReviewValue("old", "two\nlines"); err == nil {
		t.Fatal("multiline legacy scalar was allowed into final review")
	}
}

func TestFieldRegistryInSync(t *testing.T) {
	path := filepath.Join("..", "..", "..", "ai-service", "config", "candidates", "field_registry.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read source registry: %v", err)
	}
	want := fmt.Sprintf("sha256 %x", sha256.Sum256(raw))
	generated, err := os.ReadFile("field_registry_gen.go")
	if err != nil {
		t.Fatalf("read generated registry: %v", err)
	}
	if !strings.Contains(string(generated), want) {
		t.Fatalf("generated registry does not match YAML hash %s", want)
	}
	if !isRegistryField("SURAT_JALAN", "shipment_id") || isRegistryField("SURAT_JALAN", "awb") {
		t.Fatal("registry visibility does not follow the canonical source")
	}
}
