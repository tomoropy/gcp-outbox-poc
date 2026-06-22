package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/httpx"
	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
)

type App struct {
	repo *spannerdb.Repository
}

func New(repo *spannerdb.Repository) *App {
	return &App{repo: repo}
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /billings", a.createBilling)
	return mux
}

type createBillingRequest struct {
	TenantID      string `json:"tenantId"`
	Amount        int64  `json:"amount"`
	DueInSeconds  int64  `json:"dueInSeconds"`
	WebhookURL    string `json:"webhookUrl"`
	WebhookSecret string `json:"webhookSecret"`
}

func (a *App) createBilling(w http.ResponseWriter, r *http.Request) {
	var req createBillingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.TenantID == "" || req.Amount <= 0 || req.WebhookURL == "" {
		httpx.Error(w, http.StatusBadRequest, "tenantId, amount and webhookUrl are required")
		return
	}
	if req.DueInSeconds == 0 {
		req.DueInSeconds = 3600
	}

	billing, job, err := a.repo.CreateBillingWithOutbox(r.Context(), spannerdb.Tenant{
		TenantID:      req.TenantID,
		WebhookURL:    req.WebhookURL,
		WebhookSecret: req.WebhookSecret,
	}, spannerdb.Billing{
		TenantID: req.TenantID,
		Amount:   req.Amount,
		DueAt:    time.Now().Add(time.Duration(req.DueInSeconds) * time.Second),
	}, "billing.created")
	if err != nil {
		slog.Error("failed to create billing", slog.String("error", err.Error()))
		httpx.Error(w, http.StatusInternalServerError, "failed to create billing")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"billingId":   billing.BillingID,
		"outboxJobId": job.JobID,
	})
}
