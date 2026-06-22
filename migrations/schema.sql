CREATE TABLE Tenants (
  TenantId STRING(64) NOT NULL,
  WebhookUrl STRING(MAX) NOT NULL,
  WebhookSecret STRING(MAX),
  CreatedAt TIMESTAMP NOT NULL
) PRIMARY KEY (TenantId);

CREATE TABLE Billings (
  BillingId STRING(36) NOT NULL,
  TenantId STRING(64) NOT NULL,
  Amount INT64 NOT NULL,
  Status STRING(32) NOT NULL,
  DueAt TIMESTAMP NOT NULL,
  CreatedAt TIMESTAMP NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL
) PRIMARY KEY (BillingId);

CREATE INDEX BillingsByStatusDueAt
ON Billings(Status, DueAt);

CREATE TABLE OutboxJobs (
  JobId STRING(36) NOT NULL,
  JobType STRING(64) NOT NULL,
  AggregateId STRING(36) NOT NULL,
  Payload STRING(MAX),
  Status STRING(32) NOT NULL,
  AttemptCount INT64 NOT NULL,
  MaxAttempts INT64 NOT NULL,
  NextAttemptAt TIMESTAMP NOT NULL,
  LockedBy STRING(128),
  LockedUntil TIMESTAMP,
  LastError STRING(MAX),
  CreatedAt TIMESTAMP NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL,
  CompletedAt TIMESTAMP
) PRIMARY KEY (JobId);

CREATE INDEX OutboxJobsByStatusNextAttempt
ON OutboxJobs(Status, NextAttemptAt)
STORING (JobType, AggregateId, AttemptCount, MaxAttempts, LockedBy, LockedUntil);

CREATE TABLE WebhookDeliveries (
  DeliveryId STRING(36) NOT NULL,
  JobId STRING(36) NOT NULL,
  BillingId STRING(36) NOT NULL,
  StatusCode INT64 NOT NULL,
  ResponseBody STRING(MAX),
  CreatedAt TIMESTAMP NOT NULL
) PRIMARY KEY (DeliveryId);

CREATE INDEX WebhookDeliveriesByJob
ON WebhookDeliveries(JobId, CreatedAt);
