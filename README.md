# gcp-outbox-poc

Cloud Run Worker Pools + Cloud Spanner + Outbox pattern の検証用PoCです。

HTTP request path から非同期処理を切り離し、DB transactionでOutbox jobを作成し、常駐workerがpollingして外部HTTP webhookなどを実行する構成を試します。Pub/SubやCloud Tasksを使わず、Cloud Run Worker PoolsとSpannerだけでどこまで扱えるかを見るための小さなサンプルです。

## What this PoC verifies

このPoCでは、以下を確認できます。

- Cloud Run ServiceはHTTP APIとして使い、min instances 0でscale-to-zeroできる
- Cloud Run Worker Poolsで常駐Outbox consumerを動かせる
- Cloud SpannerでOutbox jobのclaim / lease / retry / failed / completedを表現できる
- 複数workerが同時にpollingしても、同じjobを二重claimしない
- workerがjobをclaimした後に落ちても、lease timeout後に別workerが再取得できる
- 指数バックオフと最大試行回数でretryを制御できる
- 期限切れbilling検出や古いOutbox data削除をCloud Run Jobsで分離できる
- 入金・収納通知のような外部webhookを受け、DB更新と後続通知のenqueueを同一transactionで扱える
- 同じ外部eventを複数回受けても、idempotency keyで二重処理を防げる

## Architecture

```text
Client / external provider
  -> Cloud Run Service: api
       - create billing
       - receive payment notification webhook
       - write domain data and outbox jobs in one Spanner transaction

Cloud Run Worker Pool: worker
  -> poll ready outbox jobs from Spanner
  -> claim jobs with lease
  -> send merchant webhook / record mail delivery
  -> mark completed or schedule retry

Cloud Run Jobs
  -> expire-billing: detect expired billings and enqueue webhook jobs
  -> outbox-cleanup: delete old completed jobs

Cloud Run Service: simulator
  -> local/demo webhook receiver
  -> can intentionally fail first N requests for retry testing
```

The service names are intentionally generic. This repository is a public PoC and does not contain production business logic, production credentials, or private infrastructure details.

## Directory structure

```text
cmd/api                         HTTP API
cmd/worker                      Outbox polling worker
cmd/simulator                   Webhook receiver simulator
cmd/jobs/expire_billing         Periodic expired-billing job
cmd/jobs/outbox_cleanup         Periodic cleanup job

internal/config                 Environment variable loading
internal/spannerdb              Spanner repository and Outbox logic
services/*                      Application logic
migrations/schema.sql           Spanner schema
deploy/terraform                Minimal GCP resources
scripts/local-outbox-scenarios.sh
```

## Local run with Docker Compose

Local development uses the Cloud Spanner Emulator. No GCP credentials are required for the local scenario tests.

```bash
docker compose up --build
```

Create a billing. `webhookUrl` must be reachable from the worker container, so the Docker Compose service name is used here.

```bash
curl -X POST localhost:8080/billings \
  -H 'content-type: application/json' \
  -d '{"tenantId":"tenant-demo","amount":1200,"dueInSeconds":3600,"webhookUrl":"http://simulator:8080/webhook","notifyEmail":"merchant@example.test"}'
```

Simulate an incoming payment notification webhook. The API stores the notification idempotently, updates the billing to `paid`, and enqueues both a merchant `billing.paid` webhook job and a mail job.

```bash
BILLING_ID="$(curl -fsS -X POST localhost:8080/billings \
  -H 'content-type: application/json' \
  -d '{"tenantId":"tenant-demo","amount":1200,"dueInSeconds":3600,"webhookUrl":"http://simulator:8080/webhook","notifyEmail":"merchant@example.test"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["billingId"])')"

curl -X POST localhost:8080/bank/webhooks/payment \
  -H 'content-type: application/json' \
  -d "{\"provider\":\"demo-bank\",\"eventId\":\"bank-event-001\",\"billingId\":\"${BILLING_ID}\",\"amount\":1200}"
```

Run the periodic jobs manually.

```bash
docker compose run --rm expire-billing
docker compose run --rm outbox-cleanup
```

## Local scenario test

Run the full local verification suite:

```bash
scripts/local-outbox-scenarios.sh
```

The script verifies:

- retry: simulator returns 503 for the first two webhook requests, and the worker retries with backoff
- multi-worker: three workers process ten jobs without duplicate delivery
- lease-timeout: a claimed job is picked up again after the first worker stops
- payment-webhook: the same incoming payment event is submitted twice, but only one payment notification, one merchant webhook, and one mail delivery are processed

## Run without Docker Compose app containers

You can also run the Go processes directly while using only the Spanner Emulator container.

```bash
docker compose up spanner spanner-init

export SPANNER_EMULATOR_HOST=localhost:9010
export GCP_PROJECT_ID=demo-gcp-project
export SPANNER_INSTANCE_ID=gcp-outbox-poc
export SPANNER_DATABASE_ID=gcp-outbox-poc

PORT=8081 go run ./cmd/simulator
PORT=8080 go run ./cmd/api
go run ./cmd/worker
```

## GCP PoC target

The Terraform under `deploy/terraform` creates a minimal GCP environment:

- Cloud Spanner instance: Regional, 100 processing units by default
- Cloud Spanner database and schema
- Artifact Registry repository
- Cloud Run Service: `api`
- Cloud Run Service: `simulator`
- Cloud Run Worker Pool: `worker`
- Cloud Run Jobs: `expire-billing`, `outbox-cleanup`
- Optional Cloud Scheduler for the periodic jobs
- IAM service accounts for api / worker / jobs

It intentionally does not create a Load Balancer, Pub/Sub topic, or Cloud Tasks queue.

## Deploy sketch

Set your own project ID first.

```bash
export PROJECT_ID="your-gcp-project-id"
export REGION="asia-northeast1"
```

Create the Artifact Registry repository first, because the application image must be pushed before the Cloud Run resources can be deployed.

```bash
cd deploy/terraform
terraform init
terraform apply \
  -target=google_artifact_registry_repository.repo \
  -var="project_id=${PROJECT_ID}" \
  -var="region=${REGION}" \
  -var="image=${REGION}-docker.pkg.dev/${PROJECT_ID}/gcp-outbox-poc/app:bootstrap"
```

Build and push the image.

```bash
cd ../..
IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/gcp-outbox-poc/app:$(git rev-parse --short HEAD)"

gcloud auth configure-docker "${REGION}-docker.pkg.dev"
docker build --platform linux/amd64 -t "${IMAGE}" .
docker push "${IMAGE}"
```

Apply the full Terraform configuration.

```bash
cd deploy/terraform
terraform apply \
  -var="project_id=${PROJECT_ID}" \
  -var="region=${REGION}" \
  -var="image=${IMAGE}"
```

Cloud Run Service is authenticated by default. Use an identity token for manual smoke tests.

```bash
API_URL="$(terraform output -raw api_url)"
SIMULATOR_URL="$(terraform output -raw simulator_url)"
TOKEN="$(gcloud auth print-identity-token)"

BILLING_ID="$(curl -fsS -X POST "${API_URL}/billings" \
  -H "authorization: Bearer ${TOKEN}" \
  -H 'content-type: application/json' \
  -d "{\"tenantId\":\"tenant-demo\",\"amount\":1200,\"dueInSeconds\":3600,\"webhookUrl\":\"${SIMULATOR_URL}/webhook\",\"notifyEmail\":\"merchant@example.test\"}" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["billingId"])')"

curl -X POST "${API_URL}/bank/webhooks/payment" \
  -H "authorization: Bearer ${TOKEN}" \
  -H 'content-type: application/json' \
  -d "{\"provider\":\"demo-bank\",\"eventId\":\"bank-event-001\",\"billingId\":\"${BILLING_ID}\",\"amount\":1200}"
```

## CI checks

Useful checks for this repository:

```bash
go test ./...
docker compose config
docker build -t gcp-outbox-poc:ci .
terraform -chdir=deploy/terraform fmt -check -recursive
terraform -chdir=deploy/terraform validate
```

## Public repository safety notes

- No production credentials, service account keys, private keys, or `.env` files should be committed.
- `.env` and `.env.*` are ignored; `.env.example` contains only dummy values.
- Terraform requires `project_id` to be passed explicitly and does not hard-code a real project ID.
- Local Docker Compose uses `demo-gcp-project` with the Spanner Emulator only.
- The sample email domain is `example.test`.
- The webhook secret field exists only to demonstrate payload shape; do not put real secrets in sample commands.

## Production hardening ideas

This PoC intentionally keeps some production concerns out of scope. Before using this pattern for a real external webhook path, consider adding:

- provider-specific signature verification
- raw body preservation and timestamp/replay protection
- explicit idempotency key strategy per provider
- alerting for backlog age, failed jobs, and retry spikes
- dead-letter inspection workflow
- schema migration strategy that does not replace the database
- separate public/private API binaries or containers if the inbound trust boundary differs
