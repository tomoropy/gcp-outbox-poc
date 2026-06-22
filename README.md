# gcp-outbox-poc

Cloud Run Worker Pools + Cloud Spanner + Outbox pattern の検証用PoCです。

HTTP request path から非同期処理を切り離し、DB transaction で Outbox job を作成し、常駐 worker が polling して外部 HTTP webhook などを実行する構成を試します。Pub/Sub や Cloud Tasks を使わず、Cloud Run Worker Pools と Spanner だけでどこまで扱えるかを見るための小さなサンプルです。

## このPoCで確認できること

このPoCでは、以下を確認できます。

- Cloud Run Service を HTTP API として使い、min instances 0 で scale-to-zero できる
- Cloud Run Worker Pools で常駐 Outbox consumer を動かせる
- Cloud Spanner で Outbox job の claim / lease / retry / failed / completed を表現できる
- 複数 worker が同時に polling しても、同じ job を二重 claim しない
- worker が job を claim した後に落ちても、lease timeout 後に別 worker が再取得できる
- 指数バックオフと最大試行回数で retry を制御できる
- Worker Pool の manual instance count を増やすことで、backlog 処理を水平に伸ばせる
- 期限切れ billing 検出や古い Outbox data 削除を Cloud Run Jobs で分離できる
- 入金・収納通知のような外部 webhook を受け、DB 更新と後続通知の enqueue を同一 transaction で扱える
- 同じ外部 event を複数回受けても、idempotency key で二重処理を防げる

## アーキテクチャ

```text
Client / external provider
  -> Cloud Run Service: api
       - billingを作成する
       - payment notification webhookを受ける
       - domain dataとoutbox jobsを1つのSpanner transactionで書き込む

Cloud Run Worker Pool: worker
  -> Spannerからreadyなoutbox jobsをpollingする
  -> lease付きでjobをclaimする
  -> merchant webhook送信 / mail delivery記録を行う
  -> completedにする、またはretryを予約する

Cloud Run Jobs
  -> expire-billing: 期限切れbillingを検出してwebhook jobをenqueueする
  -> outbox-cleanup: 古いcompleted jobsを削除する

Cloud Run Service: simulator
  -> local/demo用のwebhook receiver
  -> retry検証のため、最初のN回だけ意図的に失敗させられる
```

service名やdomain名は意図的に汎用的にしています。このrepositoryはpublic PoCであり、本番の業務ロジック・本番credential・非公開インフラ情報は含めません。

## ディレクトリ構成

```text
cmd/api                         HTTP API
cmd/worker                      Outbox polling worker
cmd/simulator                   Webhook receiver simulator
cmd/jobs/expire_billing         期限切れbillingを検出する定期job
cmd/jobs/outbox_cleanup         古いOutbox dataを削除する定期job

internal/config                 環境変数の読み込み
internal/spannerdb              Spanner repositoryとOutbox logic
services/*                      Application logic
migrations/schema.sql           Spanner schema
deploy/terraform                最小限のGCP resources
scripts/local-outbox-scenarios.sh
```

## Docker Composeでローカル実行

ローカル開発では Cloud Spanner Emulator を使います。local scenario test には GCP credential は不要です。

```bash
docker compose up --build
```

billingを作成します。`webhookUrl` は worker container から到達できる必要があるため、Docker Compose の service 名を指定します。

```bash
curl -X POST localhost:8080/billings \
  -H 'content-type: application/json' \
  -d '{"tenantId":"tenant-demo","amount":1200,"dueInSeconds":3600,"webhookUrl":"http://simulator:8080/webhook","notifyEmail":"merchant@example.test"}'
```

入金・収納通知 webhook を模擬します。API は通知を冪等に保存し、billing を `paid` に更新し、merchant 向け `billing.paid` webhook job と mail job を enqueue します。

```bash
BILLING_ID="$(curl -fsS -X POST localhost:8080/billings \
  -H 'content-type: application/json' \
  -d '{"tenantId":"tenant-demo","amount":1200,"dueInSeconds":3600,"webhookUrl":"http://simulator:8080/webhook","notifyEmail":"merchant@example.test"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["billingId"])')"

curl -X POST localhost:8080/bank/webhooks/payment \
  -H 'content-type: application/json' \
  -d "{\"provider\":\"demo-bank\",\"eventId\":\"bank-event-001\",\"billingId\":\"${BILLING_ID}\",\"amount\":1200}"
```

定期jobを手動実行します。

```bash
docker compose run --rm expire-billing
docker compose run --rm outbox-cleanup
```

## ローカルシナリオテスト

ローカルで一通りの検証をまとめて実行します。

```bash
scripts/local-outbox-scenarios.sh
```

このscriptでは以下を確認します。

- retry: simulator が最初の2回だけ 503 を返し、worker が backoff 後に再実行する
- multi-worker: 3つの worker で10件処理しても、同じ job が二重 delivery されない
- lease-timeout: claim 済み job が、最初の worker 停止後に再取得される
- payment-webhook: 同じ incoming payment event を2回送っても、payment notification / merchant webhook / mail delivery が1回だけ処理される

## Spanner EmulatorだけDockerで動かす場合

Spanner Emulator container だけを使い、Go process を直接起動することもできます。

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

## GCP PoCの構成

`deploy/terraform` は最小限の GCP environment を作成します。

- Cloud Spanner instance: Regional、default 100 processing units
- Cloud Spanner database と schema
- Artifact Registry repository
- Cloud Run Service: `api`
- Cloud Run Service: `simulator`
- Cloud Run Worker Pool: `worker`
- Cloud Run Jobs: `expire-billing`, `outbox-cleanup`
- 定期job用の Cloud Scheduler。defaultでは無効
- api / worker / jobs 用の IAM service accounts

Load Balancer、Pub/Sub topic、Cloud Tasks queue は意図的に作成しません。

## Worker Pool scalingの検証メモ

このPoCでは、Worker Pool の instance 数を手動で増やした場合の水平処理を検証しています。

```bash
terraform apply \
  -var="project_id=${PROJECT_ID}" \
  -var="image=${IMAGE}" \
  -var="worker_instance_count=3" \
  -var="worker_batch_size=1" \
  -var="worker_process_delay=2s"
```

この設定で Outbox backlog を積むと、3つの worker が並列に job を claim して処理します。GCP上の検証では、60件の backlog を3 workerで処理し、`WebhookDeliveries` の重複が0件であることを確認しました。

一方で、Cloud Run Worker Pools の automatic scaling はこのPoCでは採用していません。Terraform provider schema上は `AUTOMATIC` が見えますが、検証時点では期待通りに worker が起動せず、実リソース上は `manualInstanceCount: 0` 相当になりました。そのため、現時点では以下のどちらかを前提にするのが安全です。

- backlog監視を見て `worker_instance_count` を明示的に変更する
- 通常時は1台、ピーク時だけ手動または別の制御で増やす

また、Spanner の `ReadWriteTransaction` callback は retry されることがあります。callback内で外部変数へ結果を書き出す場合、abortされた試行の値が残ると二重処理につながるため注意が必要です。このPoCでは、claim結果を各transaction attemptの先頭でresetすることで、abort済みattemptのclaim結果をworkerへ返さないようにしています。

## Deploy手順の概要

まず、自分の GCP project ID を設定します。

```bash
export PROJECT_ID="your-gcp-project-id"
export REGION="asia-northeast1"
```

Cloud Run resources を作る前に application image を push する必要があるため、最初に Artifact Registry repository だけを作成します。

```bash
cd deploy/terraform
terraform init
terraform apply \
  -target=google_artifact_registry_repository.repo \
  -var="project_id=${PROJECT_ID}" \
  -var="region=${REGION}" \
  -var="image=${REGION}-docker.pkg.dev/${PROJECT_ID}/gcp-outbox-poc/app:bootstrap"
```

image を build / push します。

```bash
cd ../..
IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/gcp-outbox-poc/app:$(git rev-parse --short HEAD)"

gcloud auth configure-docker "${REGION}-docker.pkg.dev"
docker build --platform linux/amd64 -t "${IMAGE}" .
docker push "${IMAGE}"
```

Terraform全体をapplyします。

```bash
cd deploy/terraform
terraform apply \
  -var="project_id=${PROJECT_ID}" \
  -var="region=${REGION}" \
  -var="image=${IMAGE}"
```

Cloud Run Service は default では認証付きです。手動 smoke test では identity token を付けて叩きます。

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

## CIで確認したいこと

このrepositoryでは、以下を確認するとよいです。

```bash
go test ./...
docker compose config
docker build -t gcp-outbox-poc:ci .
terraform -chdir=deploy/terraform fmt -check -recursive
terraform -chdir=deploy/terraform validate
```

## public repositoryとしての安全メモ

- 本番credential、service account key、private key、`.env` file はcommitしない
- `.env` と `.env.*` は ignore 対象。`.env.example` には dummy value のみを書く
- Terraform は `project_id` の明示指定を必須にし、実project IDをhard-codeしない
- Local Docker Compose は Spanner Emulator 用の `demo-gcp-project` だけを使う
- sample email domain は `example.test` を使う
- webhook secret field は payload shape の例示用。sample command に実secretを入れない

## 本番利用に向けて追加で検討すること

このPoCでは、本番運用に必要な要素の一部をあえてscope外にしています。実際の外部 webhook path で使う場合は、少なくとも以下を検討します。

- providerごとの署名検証
- raw body 保存と timestamp / replay protection
- providerごとの明示的な idempotency key strategy
- backlog age、failed jobs、retry spike のalert
- dead-letter inspection workflow
- database replacementを避けるschema migration strategy
- inbound trust boundary が異なる場合の public/private API binary または container 分離
