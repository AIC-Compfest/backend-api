package main

import (
	"log/slog"
	"os"

	"github.com/AIC-Compfest/backend-api/internal/app"
	"github.com/AIC-Compfest/backend-api/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	if err := app.New(cfg).Run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}
