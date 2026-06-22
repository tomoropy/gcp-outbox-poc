package simulator

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/tomoropy/gcp-outbox-poc/internal/httpx"
)

type App struct{}

func New() *App {
	return &App{}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /webhook", a.webhook)
	return mux
}

func (a *App) webhook(w http.ResponseWriter, r *http.Request) {
	forceStatus := os.Getenv("SIMULATOR_FORCE_STATUS")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	slog.Info("webhook received",
		slog.String("outbox_job_id", r.Header.Get("x-outbox-job-id")),
		slog.Any("body", body))
	if forceStatus != "" {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "forced failure"})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "received"})
}
