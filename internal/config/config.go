package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ProjectID       string
	SpannerInstance string
	SpannerDatabase string
	Port            string

	WorkerID           string
	WorkerBatchSize    int
	WorkerPollInterval time.Duration
	OutboxLease        time.Duration
	OutboxBackoffBase  time.Duration
	OutboxBackoffMax   time.Duration
	WorkerHTTPTimeout  time.Duration
	WorkerProcessDelay time.Duration

	ExpireBillingLookback  time.Duration
	OutboxCleanupRetention time.Duration

	AutoscalerRegion           string
	AutoscalerWorkerPool       string
	AutoscalerMinWorkers       int
	AutoscalerMaxWorkers       int
	AutoscalerScaleUpBacklog   int64
	AutoscalerScaleUpWorkers   int
	AutoscalerMaxBacklog       int64
	AutoscalerOldestBacklogAge time.Duration
	AutoscalerDryRun           bool
}

func Load() (Config, error) {
	cfg := Config{
		ProjectID:                  getenv("GCP_PROJECT_ID", "demo-gcp-project"),
		SpannerInstance:            getenv("SPANNER_INSTANCE_ID", "gcp-outbox-poc"),
		SpannerDatabase:            getenv("SPANNER_DATABASE_ID", "gcp-outbox-poc"),
		Port:                       getenv("PORT", "8080"),
		WorkerID:                   getenv("WORKER_ID", hostnameFallback()),
		WorkerBatchSize:            getenvInt("WORKER_BATCH_SIZE", 10),
		WorkerPollInterval:         getenvDuration("WORKER_POLL_INTERVAL", time.Second),
		OutboxLease:                time.Duration(getenvInt("OUTBOX_LEASE_SECONDS", 60)) * time.Second,
		OutboxBackoffBase:          getenvDuration("OUTBOX_BACKOFF_BASE", 2*time.Second),
		OutboxBackoffMax:           getenvDuration("OUTBOX_BACKOFF_MAX", 1024*time.Second),
		WorkerHTTPTimeout:          getenvDuration("WORKER_HTTP_TIMEOUT", 20*time.Second),
		WorkerProcessDelay:         getenvDuration("WORKER_PROCESS_DELAY", 0),
		ExpireBillingLookback:      getenvDuration("EXPIRE_BILLING_LOOKBACK", 24*time.Hour),
		OutboxCleanupRetention:     getenvDuration("OUTBOX_CLEANUP_RETENTION", 30*24*time.Hour),
		AutoscalerRegion:           getenv("AUTOSCALER_REGION", "asia-northeast1"),
		AutoscalerWorkerPool:       getenv("AUTOSCALER_WORKER_POOL", "gcp-outbox-poc-worker"),
		AutoscalerMinWorkers:       getenvInt("AUTOSCALER_MIN_WORKERS", 1),
		AutoscalerMaxWorkers:       getenvInt("AUTOSCALER_MAX_WORKERS", 5),
		AutoscalerScaleUpBacklog:   int64(getenvInt("AUTOSCALER_SCALE_UP_BACKLOG", 100)),
		AutoscalerScaleUpWorkers:   getenvInt("AUTOSCALER_SCALE_UP_WORKERS", 3),
		AutoscalerMaxBacklog:       int64(getenvInt("AUTOSCALER_MAX_BACKLOG", 500)),
		AutoscalerOldestBacklogAge: getenvDuration("AUTOSCALER_OLDEST_BACKLOG_AGE", 5*time.Minute),
		AutoscalerDryRun:           getenvBool("AUTOSCALER_DRY_RUN", false),
	}
	if cfg.ProjectID == "" {
		return Config{}, fmt.Errorf("GCP_PROJECT_ID is required")
	}
	if cfg.SpannerInstance == "" {
		return Config{}, fmt.Errorf("SPANNER_INSTANCE_ID is required")
	}
	if cfg.SpannerDatabase == "" {
		return Config{}, fmt.Errorf("SPANNER_DATABASE_ID is required")
	}
	return cfg, nil
}

func (c Config) SpannerDatabasePath() string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", c.ProjectID, c.SpannerInstance, c.SpannerDatabase)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getenvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func hostnameFallback() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "worker-local"
	}
	return host
}
