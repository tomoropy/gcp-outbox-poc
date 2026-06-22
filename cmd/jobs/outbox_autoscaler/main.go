package main

import (
	"context"
	"log"

	"github.com/tomoropy/gcp-outbox-poc/internal/config"
	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
	"github.com/tomoropy/gcp-outbox-poc/services/jobs"
	run "google.golang.org/api/run/v2"
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

	runSvc, err := run.NewService(ctx)
	if err != nil {
		log.Fatal(err)
	}

	policy := jobs.OutboxAutoscalerPolicy{
		ProjectID:        cfg.ProjectID,
		Region:           cfg.AutoscalerRegion,
		WorkerPoolName:   cfg.AutoscalerWorkerPool,
		MinWorkers:       cfg.AutoscalerMinWorkers,
		MaxWorkers:       cfg.AutoscalerMaxWorkers,
		ScaleUpBacklog:   cfg.AutoscalerScaleUpBacklog,
		ScaleUpWorkers:   cfg.AutoscalerScaleUpWorkers,
		MaxBacklog:       cfg.AutoscalerMaxBacklog,
		OldestBacklogAge: cfg.AutoscalerOldestBacklogAge,
		DryRun:           cfg.AutoscalerDryRun,
	}
	if err := jobs.NewOutboxAutoscalerJob(spannerdb.NewRepository(client), runSvc, policy).Run(ctx); err != nil {
		log.Fatal(err)
	}
}
