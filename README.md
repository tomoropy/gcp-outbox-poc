# gcp-outbox-poc

Cloud Run Worker Pools + Spanner + Outbox pattern の検証用PoCです。

## 検証したいこと

- Cloud Run Service は scale-to-zero できるか
- Cloud Run Worker Pool で常駐Outbox consumerを動かせるか
- SpannerでOutbox jobのclaim / lease / retry / failed / completedを表現できるか
- 複数workerでも同じjobを二重claimしないか
- Cloud Run Jobsで期限切れbilling検出とcleanupを担えるか

## 構成

```text
cmd/api                 Billing作成API
cmd/worker              Outbox polling worker
cmd/simulator           Webhook受信先シミュレータ
cmd/jobs/expire_billing 期限切れbillingを検出してOutboxへenqueue
cmd/jobs/outbox_cleanup 完了済みOutboxの掃除

internal/config         環境変数
internal/spannerdb      Spanner repository
services/*              アプリケーションロジック
migrations/schema.sql   Spanner schema
deploy/terraform        GCP最小構成
```

## Spanner schema

```bash
gcloud spanner databases ddl update gcp-outbox-poc \
  --instance=gcp-outbox-poc \
  --project=sandbox-500107 \
  --ddl-file=migrations/schema.sql
```

## Local run with Docker Compose

まずはSpanner Emulatorで検証します。

```bash
docker compose up --build
```

Billingを作成します。`webhookUrl`はworkerコンテナから見えるCompose service名を指定します。

```bash
curl -X POST localhost:8080/billings \
  -H 'content-type: application/json' \
  -d '{"tenantId":"tenant-demo","amount":1200,"dueInSeconds":3600,"webhookUrl":"http://simulator:8080/webhook"}'
```

期限切れjobを手動実行します。

```bash
docker compose run --rm expire-billing
```

cleanup jobを手動実行します。

```bash
docker compose run --rm outbox-cleanup
```

Spanner Emulatorだけを使ってGoを直接起動する場合は、`SPANNER_EMULATOR_HOST`を指定します。

```bash
docker compose up spanner spanner-init
export SPANNER_EMULATOR_HOST=localhost:9010
export GCP_PROJECT_ID=sandbox-500107
export SPANNER_INSTANCE_ID=gcp-outbox-poc
export SPANNER_DATABASE_ID=gcp-outbox-poc

PORT=8081 go run ./cmd/simulator
PORT=8080 go run ./cmd/api
go run ./cmd/worker
```

## GCP PoC target

- Spanner: Regional Standard 100 PU
- Cloud Run Service: api / simulator, min instances 0
- Cloud Run Worker Pool: worker, instance_count 1
- Cloud Run Jobs: expire_billing / outbox_cleanup
- Cloud Scheduler: expire_billing起動
- LB / PubSub / Cloud Tasks は使わない

## CI/CD

今のPoCでは、GitHub ActionsでCIのみ実行します。

- `go test ./...`
- `terraform fmt -check`
- `terraform validate`
- `docker build`

GCPへdeployするCDは、Workload Identity Federation用のprovider/service accountを作ってから追加します。

## Deploy sketch

Artifact Registry作成前はimage push先がないため、初回だけrepositoryを先に作ります。

```bash
cd deploy/terraform
terraform init
terraform apply -target=google_artifact_registry_repository.repo \
  -var='image=asia-northeast1-docker.pkg.dev/sandbox-500107/gcp-outbox-poc/app:bootstrap'
```

imageをbuild/pushします。

```bash
IMAGE=asia-northeast1-docker.pkg.dev/sandbox-500107/gcp-outbox-poc/app:$(git rev-parse --short HEAD)
gcloud auth configure-docker asia-northeast1-docker.pkg.dev
docker build -t "$IMAGE" .
docker push "$IMAGE"
```

本体をapplyします。

```bash
cd deploy/terraform
terraform apply -var="image=$IMAGE"
```

Cloud Run Serviceはデフォルトでは認証付きです。PoCで叩くときはidentity tokenを付けます。

```bash
API_URL=$(terraform output -raw api_url)
SIMULATOR_URL=$(terraform output -raw simulator_url)

curl -X POST "$API_URL/billings" \
  -H "authorization: Bearer $(gcloud auth print-identity-token)" \
  -H 'content-type: application/json' \
  -d "{\"tenantId\":\"tenant-demo\",\"amount\":1200,\"dueInSeconds\":3600,\"webhookUrl\":\"$SIMULATOR_URL/webhook\"}"
```
