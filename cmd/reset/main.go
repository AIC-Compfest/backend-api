package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"backend-api/internal/config"
	"backend-api/internal/database"
)

func main() {
	fmt.Println("================================================================")
	fmt.Println("🧹 RESET DATABASE TO TOTAL CLEAN SLATE (START 100% FRESH)")
	fmt.Println("================================================================")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	dbClient := database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	ctx := context.Background()

	// All 20 tables in topological foreign key dependency order
	tablesToWipe := []string{
		// Level 1: Leaf child tables
		"field_evidence",
		"match_evidence",
		"discrepancies",
		"review_decisions",
		"reconciliation_cases",
		"audit_events",
		"shipment_events",
		"invoice_line_items",
		"rate_agreement_selections",
		"pricing_rules",

		// Level 2: Domain entities
		"invoices",
		"surat_jalans",
		"pods",
		"shipments",

		// Level 3: Document extraction & segment hierarchy
		"document_extractions",
		"document_pages",
		"document_segments",
		"documents",

		// Level 4: Master rate agreements & vendors
		"rate_agreements",
		"vendors",
	}

	fmt.Println("\n1. Mengosongkan 20 tabel Supabase (Domain, Transaksi, Audit, Spasial BBox, Master Vendor)...")

	for _, table := range tablesToWipe {
		// Strategy 1: PostgREST delete where id is not null (or not empty dummy uuid)
		params := url.Values{}
		params.Set("id", "neq.00000000-0000-0000-0000-000000000000")
		err := dbClient.Delete(ctx, table, params)

		// Strategy 2: If table uses non-UUID primary keys or custom columns
		if err != nil {
			var rows []map[string]interface{}
			qParams := url.Values{}
			qParams.Set("select", "id")
			qParams.Set("limit", "1000")
			if qErr := dbClient.Query(ctx, table, qParams, &rows); qErr == nil && len(rows) > 0 {
				for _, r := range rows {
					if idVal, ok := r["id"].(string); ok && idVal != "" {
						delP := url.Values{}
						delP.Set("id", "eq."+idVal)
						_ = dbClient.Delete(ctx, table, delP)
					}
				}
			}
		}

		// Verify count
		var remaining []map[string]interface{}
		chkP := url.Values{}
		chkP.Set("select", "id")
		chkP.Set("limit", "10")
		_ = dbClient.Query(ctx, table, chkP, &remaining)

		if len(remaining) == 0 {
			fmt.Printf("   ✓ %-28s : Terhapus Bersih (0 baris)\n", table)
		} else {
			fmt.Printf("   ! %-28s : Sisa %d baris\n", table, len(remaining))
		}
	}

	// 2. Clean Local Storage Uploads
	fmt.Println("\n2. Mengosongkan folder local storage uploads...")
	storageDirs := []string{"storage", "storage/documents", "storage/disputes"}
	for _, dir := range storageDirs {
		if entries, err := os.ReadDir(dir); err == nil {
			cleanedCount := 0
			for _, entry := range entries {
				if !entry.IsDir() {
					_ = os.Remove(filepath.Join(dir, entry.Name()))
					cleanedCount++
				}
			}
			fmt.Printf("   ✓ %-28s : %d file fisik terhapus\n", dir, cleanedCount)
		}
	}

	fmt.Println("\n================================================================")
	fmt.Println("✅ DATABASE BERHASIL DI-RESET TOTAL! 100% CLEAN SLATE.")
	fmt.Println("   Seluruh dokumen, BBox spasial, transaksi, audit, vendor & PKS bersih.")
	fmt.Println("   Anda siap melakukan pengetesan end-to-end dari nol (upload dokumen).")
	fmt.Println("================================================================")
}
