package spannerdb

import (
	"encoding/json"
	"time"
)

const (
	BillingStatusPending = "pending"
	BillingStatusExpired = "expired"
	BillingStatusPaid    = "paid"

	OutboxStatusPending    = "pending"
	OutboxStatusProcessing = "processing"
	OutboxStatusRetry      = "retry"
	OutboxStatusCompleted  = "completed"
	OutboxStatusFailed     = "failed"

	JobTypeWebhookBillingCreated = "webhook.billing_created"
	JobTypeWebhookBillingExpired = "webhook.billing_expired"
	JobTypeWebhookBillingPaid    = "webhook.billing_paid"
	JobTypeMailBillingPaid       = "mail.billing_paid"
)

type Tenant struct {
	TenantID          string
	WebhookURL        string
	WebhookSecret     string
	NotificationEmail string
	CreatedAt         time.Time
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

type IncomingPaymentNotification struct {
	Provider        string
	ProviderEventID string
	BillingID       string
	TenantID        string
	Amount          int64
	Status          string
	RawPayload      json.RawMessage
	ReceivedAt      time.Time
	ProcessedAt     *time.Time
}

type MailJobPayload struct {
	ToEmail   string `json:"toEmail"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	BillingID string `json:"billingId"`
}

type PaymentWebhookResult struct {
	Accepted             bool   `json:"accepted"`
	Duplicate            bool   `json:"duplicate"`
	Ignored              bool   `json:"ignored"`
	BillingID            string `json:"billingId"`
	MerchantWebhookJobID string `json:"merchantWebhookJobId"`
	MailJobID            string `json:"mailJobId"`
}

type OutboxBacklogStats struct {
	ReadyCount      int64
	ProcessingCount int64
	OldestReadyAt   *time.Time
}
