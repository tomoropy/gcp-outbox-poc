package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
	run "google.golang.org/api/run/v2"
)

type OutboxAutoscalerPolicy struct {
	ProjectID        string
	Region           string
	WorkerPoolName   string
	MinWorkers       int
	MaxWorkers       int
	ScaleUpBacklog   int64
	ScaleUpWorkers   int
	MaxBacklog       int64
	OldestBacklogAge time.Duration
	DryRun           bool
}

type OutboxAutoscalerJob struct {
	repo   *spannerdb.Repository
	runSvc *run.Service
	policy OutboxAutoscalerPolicy
}

func NewOutboxAutoscalerJob(repo *spannerdb.Repository, runSvc *run.Service, policy OutboxAutoscalerPolicy) *OutboxAutoscalerJob {
	return &OutboxAutoscalerJob{repo: repo, runSvc: runSvc, policy: policy.normalized()}
}

func (p OutboxAutoscalerPolicy) normalized() OutboxAutoscalerPolicy {
	if p.MinWorkers < 0 {
		p.MinWorkers = 0
	}
	if p.MaxWorkers < p.MinWorkers {
		p.MaxWorkers = p.MinWorkers
	}
	if p.ScaleUpWorkers < p.MinWorkers {
		p.ScaleUpWorkers = p.MinWorkers
	}
	if p.ScaleUpWorkers > p.MaxWorkers {
		p.ScaleUpWorkers = p.MaxWorkers
	}
	if p.ScaleUpBacklog <= 0 {
		p.ScaleUpBacklog = 100
	}
	if p.MaxBacklog < p.ScaleUpBacklog {
		p.MaxBacklog = p.ScaleUpBacklog
	}
	if p.OldestBacklogAge <= 0 {
		p.OldestBacklogAge = 5 * time.Minute
	}
	return p
}

func (j *OutboxAutoscalerJob) Run(ctx context.Context) error {
	stats, err := j.repo.OutboxBacklogStats(ctx)
	if err != nil {
		return err
	}

	workerPoolResource := fmt.Sprintf("projects/%s/locations/%s/workerPools/%s",
		j.policy.ProjectID,
		j.policy.Region,
		j.policy.WorkerPoolName,
	)
	workerPool, err := j.runSvc.Projects.Locations.WorkerPools.Get(workerPoolResource).Context(ctx).Do()
	if err != nil {
		return err
	}
	currentWorkers := int64(0)
	if workerPool.Scaling != nil {
		currentWorkers = workerPool.Scaling.ManualInstanceCount
	}

	desiredWorkers, reason := j.desiredWorkers(stats, int(currentWorkers), time.Now())
	slog.Info("outbox autoscaler decision",
		slog.Int64("ready_count", stats.ReadyCount),
		slog.Int64("processing_count", stats.ProcessingCount),
		slog.Any("oldest_ready_at", stats.OldestReadyAt),
		slog.Int64("current_workers", currentWorkers),
		slog.Int("desired_workers", desiredWorkers),
		slog.String("reason", reason),
		slog.Bool("dry_run", j.policy.DryRun))

	if int64(desiredWorkers) == currentWorkers || j.policy.DryRun {
		return nil
	}

	workerPool.Scaling = &run.GoogleCloudRunV2WorkerPoolScaling{
		ManualInstanceCount: int64(desiredWorkers),
		ForceSendFields:     []string{"ManualInstanceCount"},
	}
	workerPool.ForceSendFields = []string{"Scaling"}
	_, err = j.runSvc.Projects.Locations.WorkerPools.Patch(workerPoolResource, workerPool).
		UpdateMask("scaling").
		Context(ctx).
		Do()
	if err != nil {
		return err
	}
	slog.Info("outbox autoscaler updated worker pool",
		slog.Int64("previous_workers", currentWorkers),
		slog.Int("desired_workers", desiredWorkers))
	return nil
}

func (j *OutboxAutoscalerJob) desiredWorkers(stats spannerdb.OutboxBacklogStats, currentWorkers int, now time.Time) (int, string) {
	minWorkers := j.policy.MinWorkers
	maxWorkers := j.policy.MaxWorkers

	if stats.ReadyCount == 0 && stats.ProcessingCount == 0 {
		return minWorkers, "idle"
	}

	desired := maxInt(minWorkers, currentWorkers)
	reason := "keep_current"

	if stats.ReadyCount >= j.policy.ScaleUpBacklog {
		desired = maxInt(desired, j.policy.ScaleUpWorkers)
		reason = "scale_up_backlog"
	}
	if stats.ReadyCount >= j.policy.MaxBacklog {
		desired = maxWorkers
		reason = "scale_up_max_backlog"
	}
	if stats.OldestReadyAt != nil && now.Sub(*stats.OldestReadyAt) >= j.policy.OldestBacklogAge {
		desired = maxInt(desired, minInt(maxWorkers, desired+1))
		reason = "scale_up_oldest_age"
	}
	if desired > maxWorkers {
		desired = maxWorkers
	}
	if desired < minWorkers {
		desired = minWorkers
	}
	return desired, reason
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
