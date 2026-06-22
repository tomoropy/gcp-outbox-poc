package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
)

type App struct {
	repo         *spannerdb.Repository
	workerID     string
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
	httpClient   *http.Client
	processDelay time.Duration
}

func New(repo *spannerdb.Repository, workerID string, batchSize int, pollInterval, lease, httpTimeout, processDelay time.Duration) *App {
	return &App{
		repo:         repo,
		workerID:     workerID,
		batchSize:    batchSize,
		pollInterval: pollInterval,
		lease:        lease,
		httpClient:   &http.Client{Timeout: httpTimeout},
		processDelay: processDelay,
	}
}

func (a *App) Run(ctx context.Context) error {
	slog.Info("worker started", slog.String("worker_id", a.workerID))
	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		if err := a.tick(ctx); err != nil {
			slog.Error("worker tick failed", slog.String("error", err.Error()))
		}

		select {
		case <-ctx.Done():
			slog.Info("worker stopped", slog.String("worker_id", a.workerID))
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *App) tick(ctx context.Context) error {
	jobs, err := a.repo.ClaimReadyJobs(ctx, a.workerID, a.batchSize, a.lease)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if a.processDelay > 0 {
			slog.Info("worker processing delay",
				slog.String("worker_id", a.workerID),
				slog.String("job_id", job.JobID),
				slog.Duration("delay", a.processDelay))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(a.processDelay):
			}
		}
		if err := a.process(ctx, job); err != nil {
			slog.Warn("job failed, scheduling retry",
				slog.String("job_id", job.JobID),
				slog.String("job_type", job.JobType),
				slog.Int64("attempt", job.AttemptCount),
				slog.String("error", err.Error()))
			if markErr := a.repo.MarkRetry(ctx, job.JobID, err); markErr != nil {
				return markErr
			}
			continue
		}
		if err := a.repo.MarkCompleted(ctx, job.JobID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) process(ctx context.Context, job *spannerdb.OutboxJob) error {
	switch job.JobType {
	case spannerdb.JobTypeWebhookBillingCreated, spannerdb.JobTypeWebhookBillingExpired:
		return a.sendWebhook(ctx, job)
	default:
		return fmt.Errorf("unknown job type: %s", job.JobType)
	}
}

func (a *App) sendWebhook(ctx context.Context, job *spannerdb.OutboxJob) error {
	var payload spannerdb.WebhookJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("decode webhook payload: %w", err)
	}
	body, err := json.Marshal(payload.Body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, payload.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-outbox-job-id", job.JobID)
	if payload.WebhookSecret != "" {
		req.Header.Set("x-webhook-secret", payload.WebhookSecret)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = a.repo.RecordWebhookDelivery(ctx, job.JobID, payload.Body.BillingID, resp.StatusCode, string(respBody))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		slog.Info("webhook delivered", slog.String("job_id", job.JobID), slog.Int("status", resp.StatusCode))
		return nil
	}
	return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
}
