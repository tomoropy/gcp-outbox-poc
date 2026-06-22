package main

import (
	"context"
	"log"

	"github.com/tomoropy/gcp-outbox-poc/internal/config"
	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
	"github.com/tomoropy/gcp-outbox-poc/services/jobs"
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

	if err := jobs.NewOutboxCleanupJob(spannerdb.NewRepository(client), cfg.OutboxCleanupRetention).Run(ctx); err != nil {
		log.Fatal(err)
	}
}
