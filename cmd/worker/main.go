package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/AIC-Compfest/backend-api/internal/config"
	"github.com/AIC-Compfest/backend-api/internal/jobs"
	"github.com/AIC-Compfest/backend-api/internal/supabase"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	client := supabase.NewClient(cfg.SupabaseURL, cfg.SupabasePublishableKey)
	worker := jobs.NewWorker(jobs.NewRESTRepository(client), jobs.NoopProcessor{}, slog.Default())
	ticker := time.NewTicker(cfg.WorkerPollInterval)
	defer ticker.Stop()

	for {
		if err := worker.RunOnce(context.Background()); err != nil && !errors.Is(err, jobs.ErrNoJob) {
			slog.Error("worker cycle failed", "error", err)
		}
		<-ticker.C
	}
}
