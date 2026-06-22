package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
)

type ExpireBillingJob struct {
	repo     *spannerdb.Repository
	lookback time.Duration
}

func NewExpireBillingJob(repo *spannerdb.Repository, lookback time.Duration) *ExpireBillingJob {
	return &ExpireBillingJob{repo: repo, lookback: lookback}
}

func (j *ExpireBillingJob) Run(ctx context.Context) error {
	count, err := j.repo.EnqueueExpiredBillingWebhooks(ctx, j.lookback)
	if err != nil {
		return err
	}
	slog.Info("expired billing scan completed", slog.Int64("enqueued", count))
	return nil
}
