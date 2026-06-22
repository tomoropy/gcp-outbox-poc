package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"

	"github.com/tomoropy/gcp-outbox-poc/internal/config"
	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
	"github.com/tomoropy/gcp-outbox-poc/services/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	client, err := spannerdb.NewClient(ctx, cfg.SpannerDatabasePath())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	repo := spannerdb.NewRepository(client).WithBackoff(cfg.OutboxBackoffBase, cfg.OutboxBackoffMax)
	app := worker.New(repo, cfg.WorkerID, cfg.WorkerBatchSize, cfg.WorkerPollInterval, cfg.OutboxLease, cfg.WorkerHTTPTimeout, cfg.WorkerProcessDelay)
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
