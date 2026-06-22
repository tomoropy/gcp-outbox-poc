package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
)

type OutboxCleanupJob struct {
	repo      *spannerdb.Repository
	retention time.Duration
}

func NewOutboxCleanupJob(repo *spannerdb.Repository, retention time.Duration) *OutboxCleanupJob {
	return &OutboxCleanupJob{repo: repo, retention: retention}
}

func (j *OutboxCleanupJob) Run(ctx context.Context) error {
	count, err := j.repo.CleanupCompleted(ctx, j.retention)
	if err != nil {
		return err
	}
	slog.Info("outbox cleanup completed", slog.Int64("deleted", count))
	return nil
}
