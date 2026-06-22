package simulator

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/httpx"
)

type App struct {
	mu         sync.Mutex
	total      int
	byJob      map[string]int
	forceCount int
}

func New() *App {
	return &App{
		byJob:      map[string]int{},
		forceCount: getenvInt("SIMULATOR_FAIL_FIRST_N", 0),
	}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /webhook", a.webhook)
	mux.HandleFunc("GET /stats", a.stats)
	return mux
}

func (a *App) webhook(w http.ResponseWriter, r *http.Request) {
	forceStatus := os.Getenv("SIMULATOR_FORCE_STATUS")
	if forceStatus == "" {
		forceStatus = "503"
	}
	delay := getenvDuration("SIMULATOR_DELAY", 0)
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	jobID := r.Header.Get("x-outbox-job-id")

	a.mu.Lock()
	a.total++
	a.byJob[jobID]++
	shouldForce := a.forceCount > 0
	if shouldForce {
		a.forceCount--
	}
	total := a.total
	jobCount := a.byJob[jobID]
	remainingForcedFailures := a.forceCount
	a.mu.Unlock()

	slog.Info("webhook received",
		slog.String("outbox_job_id", jobID),
		slog.Int("total", total),
		slog.Int("job_delivery_count", jobCount),
		slog.Any("body", body))
	if delay > 0 {
		time.Sleep(delay)
	}
	if shouldForce || os.Getenv("SIMULATOR_FORCE_STATUS") != "" {
		status, err := strconv.Atoi(forceStatus)
		if err != nil {
			status = http.StatusServiceUnavailable
		}
		httpx.JSON(w, status, map[string]any{
			"status":                  "forced failure",
			"remainingForcedFailures": remainingForcedFailures,
		})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "received"})
}

func (a *App) stats(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	byJob := make(map[string]int, len(a.byJob))
	duplicates := 0
	for jobID, count := range a.byJob {
		byJob[jobID] = count
		if jobID != "" && count > 1 {
			duplicates++
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"total":                   a.total,
		"byJob":                   byJob,
		"duplicateJobIds":         duplicates,
		"remainingForcedFailures": a.forceCount,
	})
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
