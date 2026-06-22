package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/tomoropy/gcp-outbox-poc/internal/config"
	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
	"github.com/tomoropy/gcp-outbox-poc/services/api"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	client, err := spannerdb.NewClient(ctx, cfg.SpannerDatabasePath())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	app := api.New(spannerdb.NewRepository(client))
	addr := ":" + cfg.Port
	slog.Info("api listening", slog.String("addr", addr), slog.String("database", cfg.SpannerDatabasePath()))
	if err := http.ListenAndServe(addr, app.Handler()); err != nil {
		slog.Error("api stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
