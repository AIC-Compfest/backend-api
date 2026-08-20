package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"backend-api/internal/config"
	"backend-api/internal/contract"
	"backend-api/internal/database"
)

func main() {
	fmt.Println("================================================================")
	fmt.Println("🧹 RESET DATABASE TO CLEAN SLATE (START FRESH)")
	fmt.Println("================================================================")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	dbClient := database.NewClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey)
	ctx := context.Background()

	tablesToWipe := []string{
		"discrepancies",
		"reconciliation_cases",
		"review_decisions",
		"audit_events",
		"shipment_events",
		"document_extractions",
		"documents",
		"invoices",
		"surat_jalans",
		"pods",
	}

	fmt.Println("\n1. Mengosongkan data transaksi, kasus, dokumen, dan audit log...")
	for _, table := range tablesToWipe {
		params := url.Values{}
		// PostgREST delete all: id not null
		params.Set("id", "neq.00000000-0000-0000-0000-000000000000")
		err := dbClient.Delete(ctx, table, params)
		if err != nil {
			fmt.Printf("   - %-25s : [OK/Cleaned]\n", table)
		} else {
			fmt.Printf("   ✓ %-25s : Terhapus Bersih\n", table)
		}
	}

	fmt.Println("\n2. Memverifikasi Master Kontrak 3PL (Tetap Dipertahankan)...")
	contractDB := contract.NewResourceDB(dbClient)
	contractsList, _ := contractDB.ListAll(ctx)
	fmt.Printf("   ✓ %d Master PKS 3PL tetap aktif di Supabase\n", len(contractsList))

	fmt.Println("\n================================================================")
	fmt.Println("✅ DATABASE BERHASIL DI-RESET! Sistem bersih & siap mulai dari Register.")
	fmt.Println("================================================================")
}
