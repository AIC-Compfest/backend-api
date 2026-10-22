package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"backend-api/internal/config"
	"backend-api/internal/database"
	"backend-api/internal/model"
)

// This seeder only creates canonical reference data. Documents, AI lineage,
// predictions, and HITL decisions must come from the real upload pipeline.
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.SupabaseURL == "" || cfg.SupabaseServiceRoleKey == "" {
		slog.Error("Supabase configuration is incomplete")
		os.Exit(1)
	}

	client := database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	ctx := context.Background()
	vendors := []struct {
		code string
		name string
	}{
		{"3PL-LOG-001", "PT Cepat Logistik Nusantara"},
		{"3PL-LOG-002", "PT Trans Express Indonesia"},
		{"3PL-LOG-003", "PT Kargo Andalan Utama"},
		{"3PL-LOG-004", "PT Sinar Logistik Mandiri"},
		{"3PL-LOG-005", "PT Aruna Freight Solusi"},
	}

	for _, vendor := range vendors {
		var existing []map[string]interface{}
		if err := client.Query(ctx, "vendors", mapValues("vendor_id", "eq."+vendor.code), &existing); err != nil {
			slog.Error("lookup vendor failed", "vendor_id", vendor.code, "error", err)
			os.Exit(1)
		}
		if len(existing) > 0 {
			continue
		}
		payload := map[string]interface{}{
			"id": model.NewUUID(), "vendor_id": vendor.code, "vendor_name": vendor.name,
			"is_active": true, "created_at": time.Now().UTC(), "updated_at": time.Now().UTC(),
		}
		if err := client.Insert(ctx, "vendors", payload, nil); err != nil {
			slog.Error("insert vendor failed", "vendor_id", vendor.code, "error", err)
			os.Exit(1)
		}
		fmt.Printf("seeded vendor %s\n", vendor.code)
	}

	for _, vendor := range vendors {
		var rows []map[string]interface{}
		if err := client.Query(ctx, "vendors", mapValues("vendor_id", "eq."+vendor.code), &rows); err != nil || len(rows) == 0 {
			slog.Error("vendor lookup for rate agreement failed", "vendor_id", vendor.code, "error", err)
			os.Exit(1)
		}
		vendorUUID, ok := rows[0]["id"].(string)
		if !ok || vendorUUID == "" {
			slog.Error("vendor row has no UUID", "vendor_id", vendor.code)
			os.Exit(1)
		}
		agreementNumber := "RA-" + vendor.code + "-2026"
		var existing []map[string]interface{}
		if err := client.Query(ctx, "rate_agreements", mapValues("agreement_number", "eq."+agreementNumber), &existing); err != nil {
			slog.Error("lookup rate agreement failed", "agreement_number", agreementNumber, "error", err)
			os.Exit(1)
		}
		if len(existing) > 0 {
			continue
		}
		payload := map[string]interface{}{
			"id": model.NewUUID(), "agreement_number": agreementNumber, "vendor_id": vendorUUID,
			"effective_from": "2026-01-01", "effective_to": "2026-12-31",
			"currency": "IDR", "minimum_charge": 0, "fuel_surcharge_percent": 0,
			"dispute_window_days": 30, "rules_json": map[string]interface{}{},
		}
		if err := client.Insert(ctx, "rate_agreements", payload, nil); err != nil {
			slog.Error("insert rate agreement failed", "agreement_number", agreementNumber, "error", err)
			os.Exit(1)
		}
		fmt.Printf("seeded rate agreement %s\n", agreementNumber)
	}
}

func mapValues(key, value string) url.Values {
	return url.Values{key: []string{value}}
}
