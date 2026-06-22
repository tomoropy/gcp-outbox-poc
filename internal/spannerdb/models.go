package spannerdb

import (
	"encoding/json"
	"time"
)

const (
	BillingStatusPending = "pending"
	BillingStatusExpired = "expired"

	OutboxStatusPending    = "pending"
	OutboxStatusProcessing = "processing"
	OutboxStatusRetry      = "retry"
	OutboxStatusCompleted  = "completed"
	OutboxStatusFailed     = "failed"

	JobTypeWebhookBillingCreated = "webhook.billing_created"
	JobTypeWebhookBillingExpired = "webhook.billing_expired"
)

type Tenant struct {
	TenantID      string
	WebhookURL    string
	WebhookSecret string
	CreatedAt     time.Time
}

type Billing struct {
	BillingID string
	TenantID  string
	Amount    int64
	Status    string
	DueAt     time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type OutboxJob struct {
	JobID         string
	JobType       string
	AggregateID   string
	Payload       json.RawMessage
	Status        string
	AttemptCount  int64
	MaxAttempts   int64
	NextAttemptAt time.Time
	LockedBy      *string
	LockedUntil   *time.Time
	LastError     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

type WebhookPayload struct {
	Event       string    `json:"event"`
	BillingID   string    `json:"billingId"`
	TenantID    string    `json:"tenantId"`
	Amount      int64     `json:"amount"`
	TriggeredAt time.Time `json:"triggeredAt"`
}

type WebhookJobPayload struct {
	WebhookURL    string         `json:"webhookUrl"`
	WebhookSecret string         `json:"webhookSecret"`
	Body          WebhookPayload `json:"body"`
}
