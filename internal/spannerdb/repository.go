package spannerdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
)

type Repository struct {
	client      *spanner.Client
	backoffBase time.Duration
	backoffMax  time.Duration
}

func NewRepository(client *spanner.Client) *Repository {
	return &Repository{
		client:      client,
		backoffBase: 2 * time.Second,
		backoffMax:  1024 * time.Second,
	}
}

func (r *Repository) WithBackoff(base, max time.Duration) *Repository {
	if base > 0 {
		r.backoffBase = base
	}
	if max > 0 {
		r.backoffMax = max
	}
	if r.backoffMax < r.backoffBase {
		r.backoffMax = r.backoffBase
	}
	return r
}

func (r *Repository) UpsertTenant(ctx context.Context, tenant Tenant) error {
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = time.Now()
	}
	_, err := r.client.Apply(ctx, []*spanner.Mutation{
		spanner.InsertOrUpdate("Tenants", []string{
			"TenantId", "WebhookUrl", "WebhookSecret", "CreatedAt",
		}, []any{
			tenant.TenantID, tenant.WebhookURL, tenant.WebhookSecret, tenant.CreatedAt,
		}),
	})
	return err
}

func (r *Repository) CreateBillingWithOutbox(ctx context.Context, tenant Tenant, billing Billing, event string) (*Billing, *OutboxJob, error) {
	now := time.Now()
	if billing.BillingID == "" {
		billing.BillingID = uuid.NewString()
	}
	if billing.Status == "" {
		billing.Status = BillingStatusPending
	}
	if billing.CreatedAt.IsZero() {
		billing.CreatedAt = now
	}
	if billing.UpdatedAt.IsZero() {
		billing.UpdatedAt = now
	}
	tenant.TenantID = billing.TenantID
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = now
	}

	payload, err := json.Marshal(WebhookJobPayload{
		WebhookURL:    tenant.WebhookURL,
		WebhookSecret: tenant.WebhookSecret,
		Body: WebhookPayload{
			Event:       event,
			BillingID:   billing.BillingID,
			TenantID:    billing.TenantID,
			Amount:      billing.Amount,
			TriggeredAt: now,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	job := &OutboxJob{
		JobID:         uuid.NewString(),
		JobType:       JobTypeWebhookBillingCreated,
		AggregateID:   billing.BillingID,
		Payload:       payload,
		Status:        OutboxStatusPending,
		AttemptCount:  0,
		MaxAttempts:   10,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	_, err = r.client.ReadWriteTransaction(ctx, func(ctx context.Context, tx *spanner.ReadWriteTransaction) error {
		tx.BufferWrite([]*spanner.Mutation{
			spanner.InsertOrUpdate("Tenants", []string{
				"TenantId", "WebhookUrl", "WebhookSecret", "CreatedAt",
			}, []any{
				tenant.TenantID, tenant.WebhookURL, tenant.WebhookSecret, tenant.CreatedAt,
			}),
			spanner.Insert("Billings", []string{
				"BillingId", "TenantId", "Amount", "Status", "DueAt", "CreatedAt", "UpdatedAt",
			}, []any{
				billing.BillingID, billing.TenantID, billing.Amount, billing.Status, billing.DueAt, billing.CreatedAt, billing.UpdatedAt,
			}),
			outboxInsertMutation(job),
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &billing, job, nil
}

func (r *Repository) ClaimReadyJobs(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*OutboxJob, error) {
	if limit <= 0 {
		limit = 1
	}
	candidates, err := r.readyJobIDs(ctx, limit*3)
	if err != nil {
		return nil, err
	}

	jobs := make([]*OutboxJob, 0, limit)
	for _, id := range candidates {
		if len(jobs) >= limit {
			break
		}
		job, claimed, err := r.claimOne(ctx, id, workerID, lease)
		if err != nil {
			return nil, err
		}
		if claimed {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (r *Repository) readyJobIDs(ctx context.Context, limit int) ([]string, error) {
	stmt := spanner.Statement{
		SQL: `SELECT JobId
FROM OutboxJobs@{FORCE_INDEX=OutboxJobsByStatusNextAttempt}
WHERE Status IN UNNEST(@statuses)
  AND NextAttemptAt <= CURRENT_TIMESTAMP()
  AND (LockedUntil IS NULL OR LockedUntil < CURRENT_TIMESTAMP())
ORDER BY Status, NextAttemptAt
LIMIT @limit`,
		Params: map[string]any{
			"statuses": []string{OutboxStatusPending, OutboxStatusRetry, OutboxStatusProcessing},
			"limit":    int64(limit),
		},
	}
	iter := r.client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var ids []string
	for {
		row, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		var id string
		if err := row.Columns(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *Repository) claimOne(ctx context.Context, jobID, workerID string, lease time.Duration) (*OutboxJob, bool, error) {
	now := time.Now()
	lockedUntil := now.Add(lease)
	var claimed *OutboxJob

	_, err := r.client.ReadWriteTransaction(ctx, func(ctx context.Context, tx *spanner.ReadWriteTransaction) error {
		job, err := readJobInTx(ctx, tx, jobID)
		if err != nil {
			return err
		}
		if !isReady(job, now) {
			return nil
		}
		job.Status = OutboxStatusProcessing
		job.AttemptCount++
		job.LockedBy = &workerID
		job.LockedUntil = &lockedUntil
		job.UpdatedAt = now
		tx.BufferWrite([]*spanner.Mutation{outboxUpdateMutation(job)})
		claimed = job
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return claimed, claimed != nil, nil
}

func (r *Repository) MarkCompleted(ctx context.Context, jobID string) error {
	now := time.Now()
	_, err := r.client.ReadWriteTransaction(ctx, func(ctx context.Context, tx *spanner.ReadWriteTransaction) error {
		job, err := readJobInTx(ctx, tx, jobID)
		if err != nil {
			return err
		}
		job.Status = OutboxStatusCompleted
		job.LockedBy = nil
		job.LockedUntil = nil
		job.LastError = nil
		job.CompletedAt = &now
		job.UpdatedAt = now
		tx.BufferWrite([]*spanner.Mutation{outboxUpdateMutation(job)})
		return nil
	})
	return err
}

func (r *Repository) MarkRetry(ctx context.Context, jobID string, cause error) error {
	now := time.Now()
	_, err := r.client.ReadWriteTransaction(ctx, func(ctx context.Context, tx *spanner.ReadWriteTransaction) error {
		job, err := readJobInTx(ctx, tx, jobID)
		if err != nil {
			return err
		}
		msg := cause.Error()
		job.LockedBy = nil
		job.LockedUntil = nil
		job.LastError = &msg
		job.UpdatedAt = now
		if job.AttemptCount >= job.MaxAttempts {
			job.Status = OutboxStatusFailed
		} else {
			job.Status = OutboxStatusRetry
			job.NextAttemptAt = now.Add(r.backoff(job.AttemptCount))
		}
		tx.BufferWrite([]*spanner.Mutation{outboxUpdateMutation(job)})
		return nil
	})
	return err
}

func (r *Repository) EnqueueExpiredBillingWebhooks(ctx context.Context, lookback time.Duration) (int64, error) {
	now := time.Now()
	from := now.Add(-lookback)
	stmt := spanner.Statement{
		SQL: `SELECT b.BillingId, b.TenantId, b.Amount, b.Status, b.DueAt, b.CreatedAt, b.UpdatedAt, t.WebhookUrl, t.WebhookSecret
FROM Billings b
JOIN Tenants t ON b.TenantId = t.TenantId
WHERE b.Status = @status
  AND b.DueAt BETWEEN @from AND @now`,
		Params: map[string]any{
			"status": BillingStatusPending,
			"from":   from,
			"now":    now,
		},
	}
	iter := r.client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var count int64
	for {
		row, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return count, err
		}
		var billing Billing
		var webhookURL string
		var webhookSecret string
		if err := row.Columns(&billing.BillingID, &billing.TenantID, &billing.Amount, &billing.Status, &billing.DueAt, &billing.CreatedAt, &billing.UpdatedAt, &webhookURL, &webhookSecret); err != nil {
			return count, err
		}
		created, err := r.enqueueExpiredForBilling(ctx, billing, webhookURL, webhookSecret)
		if err != nil {
			return count, err
		}
		if created {
			count++
		}
	}
	return count, nil
}

func (r *Repository) enqueueExpiredForBilling(ctx context.Context, billing Billing, webhookURL, webhookSecret string) (bool, error) {
	now := time.Now()
	payload, err := json.Marshal(WebhookJobPayload{
		WebhookURL:    webhookURL,
		WebhookSecret: webhookSecret,
		Body: WebhookPayload{
			Event:       "billing.expired",
			BillingID:   billing.BillingID,
			TenantID:    billing.TenantID,
			Amount:      billing.Amount,
			TriggeredAt: now,
		},
	})
	if err != nil {
		return false, err
	}
	job := &OutboxJob{
		JobID:         uuid.NewString(),
		JobType:       JobTypeWebhookBillingExpired,
		AggregateID:   billing.BillingID,
		Payload:       payload,
		Status:        OutboxStatusPending,
		AttemptCount:  0,
		MaxAttempts:   10,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	var created bool
	_, err = r.client.ReadWriteTransaction(ctx, func(ctx context.Context, tx *spanner.ReadWriteTransaction) error {
		current, err := readBillingInTx(ctx, tx, billing.BillingID)
		if err != nil {
			return err
		}
		if current.Status != BillingStatusPending || current.DueAt.After(now) {
			return nil
		}
		current.Status = BillingStatusExpired
		current.UpdatedAt = now
		tx.BufferWrite([]*spanner.Mutation{
			spanner.Update("Billings", []string{
				"BillingId", "TenantId", "Amount", "Status", "DueAt", "CreatedAt", "UpdatedAt",
			}, []any{
				current.BillingID, current.TenantID, current.Amount, current.Status, current.DueAt, current.CreatedAt, current.UpdatedAt,
			}),
			outboxInsertMutation(job),
		})
		created = true
		return nil
	})
	return created, err
}

func (r *Repository) CleanupCompleted(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)
	stmt := spanner.Statement{
		SQL: `DELETE FROM OutboxJobs
WHERE Status = @status
  AND CompletedAt < @cutoff`,
		Params: map[string]any{
			"status": OutboxStatusCompleted,
			"cutoff": cutoff,
		},
	}
	count, err := r.client.PartitionedUpdate(ctx, stmt)
	return count, err
}

func (r *Repository) RecordWebhookDelivery(ctx context.Context, jobID, billingID string, statusCode int, responseBody string) error {
	now := time.Now()
	_, err := r.client.Apply(ctx, []*spanner.Mutation{
		spanner.Insert("WebhookDeliveries", []string{
			"DeliveryId", "JobId", "BillingId", "StatusCode", "ResponseBody", "CreatedAt",
		}, []any{
			uuid.NewString(), jobID, billingID, int64(statusCode), responseBody, now,
		}),
	})
	return err
}

func readJobInTx(ctx context.Context, tx *spanner.ReadWriteTransaction, jobID string) (*OutboxJob, error) {
	row, err := tx.ReadRow(ctx, "OutboxJobs", spanner.Key{jobID}, []string{
		"JobId", "JobType", "AggregateId", "Payload", "Status", "AttemptCount", "MaxAttempts", "NextAttemptAt", "LockedBy", "LockedUntil", "LastError", "CreatedAt", "UpdatedAt", "CompletedAt",
	})
	if err != nil {
		return nil, err
	}
	return decodeJob(row)
}

func readBillingInTx(ctx context.Context, tx *spanner.ReadWriteTransaction, billingID string) (*Billing, error) {
	row, err := tx.ReadRow(ctx, "Billings", spanner.Key{billingID}, []string{
		"BillingId", "TenantId", "Amount", "Status", "DueAt", "CreatedAt", "UpdatedAt",
	})
	if err != nil {
		return nil, err
	}
	var billing Billing
	if err := row.Columns(&billing.BillingID, &billing.TenantID, &billing.Amount, &billing.Status, &billing.DueAt, &billing.CreatedAt, &billing.UpdatedAt); err != nil {
		return nil, err
	}
	return &billing, nil
}

func decodeJob(row *spanner.Row) (*OutboxJob, error) {
	var job OutboxJob
	var payload spanner.NullString
	var lockedBy spanner.NullString
	var lockedUntil spanner.NullTime
	var lastError spanner.NullString
	var completedAt spanner.NullTime
	if err := row.Columns(
		&job.JobID,
		&job.JobType,
		&job.AggregateID,
		&payload,
		&job.Status,
		&job.AttemptCount,
		&job.MaxAttempts,
		&job.NextAttemptAt,
		&lockedBy,
		&lockedUntil,
		&lastError,
		&job.CreatedAt,
		&job.UpdatedAt,
		&completedAt,
	); err != nil {
		return nil, err
	}
	if payload.Valid {
		job.Payload = json.RawMessage(payload.StringVal)
	}
	if lockedBy.Valid {
		job.LockedBy = &lockedBy.StringVal
	}
	if lockedUntil.Valid {
		job.LockedUntil = &lockedUntil.Time
	}
	if lastError.Valid {
		job.LastError = &lastError.StringVal
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return &job, nil
}

func outboxInsertMutation(job *OutboxJob) *spanner.Mutation {
	return spanner.Insert("OutboxJobs", outboxColumns(), outboxValues(job))
}

func outboxUpdateMutation(job *OutboxJob) *spanner.Mutation {
	return spanner.Update("OutboxJobs", outboxColumns(), outboxValues(job))
}

func outboxColumns() []string {
	return []string{
		"JobId", "JobType", "AggregateId", "Payload", "Status", "AttemptCount", "MaxAttempts", "NextAttemptAt", "LockedBy", "LockedUntil", "LastError", "CreatedAt", "UpdatedAt", "CompletedAt",
	}
}

func outboxValues(job *OutboxJob) []any {
	var payload *string
	if len(job.Payload) > 0 {
		payloadValue := string(job.Payload)
		payload = &payloadValue
	}
	return []any{
		job.JobID,
		job.JobType,
		job.AggregateID,
		payload,
		job.Status,
		job.AttemptCount,
		job.MaxAttempts,
		job.NextAttemptAt,
		job.LockedBy,
		job.LockedUntil,
		job.LastError,
		job.CreatedAt,
		job.UpdatedAt,
		job.CompletedAt,
	}
}

func isReady(job *OutboxJob, now time.Time) bool {
	if job.Status == OutboxStatusProcessing {
		return job.LockedUntil != nil && job.LockedUntil.Before(now)
	}
	if job.Status != OutboxStatusPending && job.Status != OutboxStatusRetry {
		return false
	}
	if job.NextAttemptAt.After(now) {
		return false
	}
	if job.LockedUntil != nil && job.LockedUntil.After(now) {
		return false
	}
	return true
}

func (r *Repository) backoff(attempt int64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	power := math.Min(float64(attempt-1), 10)
	delay := time.Duration(math.Pow(2, power)) * r.backoffBase
	if delay > r.backoffMax {
		return r.backoffMax
	}
	return delay
}

func PermanentError(msg string) error {
	return fmt.Errorf("permanent error: %s", msg)
}
