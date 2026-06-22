#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

API_URL="${API_URL:-http://localhost:8080}"
SIMULATOR_URL="${SIMULATOR_URL:-http://localhost:8081}"
GCP_PROJECT_ID="${GCP_PROJECT_ID:-demo-gcp-project}"

cleanup() {
  docker compose down --remove-orphans >/dev/null 2>&1 || true
}

wait_http() {
  local url="$1"
  local timeout_seconds="${2:-30}"

  for _ in $(seq 1 "${timeout_seconds}"); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  echo "Timed out waiting for ${url}" >&2
  return 1
}

create_billing() {
  local amount="${1:-1200}"
  curl -fsS -X POST "${API_URL}/billings" \
    -H 'content-type: application/json' \
    -d "{\"tenantId\":\"tenant-demo\",\"amount\":${amount},\"dueInSeconds\":3600,\"webhookUrl\":\"http://simulator:8080/webhook\"}" >/dev/null
}

create_billing_json() {
  local amount="${1:-1200}"
  local notify_email="${2:-merchant@example.test}"
  curl -fsS -X POST "${API_URL}/billings" \
    -H 'content-type: application/json' \
    -d "{\"tenantId\":\"tenant-demo\",\"amount\":${amount},\"dueInSeconds\":3600,\"webhookUrl\":\"http://simulator:8080/webhook\",\"notifyEmail\":\"${notify_email}\"}"
}

post_payment_webhook() {
  local event_id="$1"
  local billing_id="$2"
  local amount="${3:-1200}"
  curl -fsS -X POST "${API_URL}/bank/webhooks/payment" \
    -H 'content-type: application/json' \
    -d "{\"provider\":\"demo-bank\",\"eventId\":\"${event_id}\",\"billingId\":\"${billing_id}\",\"amount\":${amount}}"
}

spanner_count() {
  local sql="$1"
  docker compose run --rm --entrypoint gcloud spanner-init \
    spanner databases execute-sql gcp-outbox-poc \
    --project="${GCP_PROJECT_ID}" \
    --instance=gcp-outbox-poc \
    --sql="${sql}" | awk 'NF { value=$NF } END { print value }'
}

wait_spanner_count() {
  local sql="$1"
  local expected="$2"
  local timeout_seconds="${3:-30}"

  for _ in $(seq 1 "${timeout_seconds}"); do
    local count
    count="$(spanner_count "${sql}")"
    if [[ "${count}" == "${expected}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "Timed out waiting for Spanner count=${expected}: ${sql}" >&2
  echo "Last observed count: $(spanner_count "${sql}")" >&2
  return 1
}

stats_value() {
  local key="$1"
  curl -fsS "${SIMULATOR_URL}/stats" | python3 -c "import json,sys; print(json.load(sys.stdin)['${key}'])"
}

wait_stats_total() {
  local expected="$1"
  local timeout_seconds="${2:-30}"

  for _ in $(seq 1 "${timeout_seconds}"); do
    local total
    total="$(stats_value total)"
    if [[ "${total}" == "${expected}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "Timed out waiting for simulator total=${expected}" >&2
  curl -fsS "${SIMULATOR_URL}/stats" >&2 || true
  docker compose logs --tail=120 worker simulator >&2 || true
  return 1
}

assert_duplicates() {
  local expected="$1"
  local actual
  actual="$(stats_value duplicateJobIds)"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Expected duplicateJobIds=${expected}, got ${actual}" >&2
    curl -fsS "${SIMULATOR_URL}/stats" >&2 || true
    return 1
  fi
}

scenario_retry() {
  echo "== retry: first two webhook attempts fail, third succeeds =="
  cleanup
  SIMULATOR_FAIL_FIRST_N=2 \
    OUTBOX_BACKOFF_BASE=1s \
    OUTBOX_BACKOFF_MAX=1s \
    docker compose up --build -d spanner spanner-init api simulator worker
  wait_http "${API_URL}/healthz"
  wait_http "${SIMULATOR_URL}/healthz"

  create_billing 1200
  wait_stats_total 3 30
  assert_duplicates 1
}

scenario_multi_worker() {
  echo "== multi-worker: three workers process ten jobs without duplicate delivery =="
  cleanup
  WORKER_BATCH_SIZE=1 docker compose up --build -d spanner spanner-init api simulator
  WORKER_BATCH_SIZE=1 docker compose up -d --scale worker=3 worker
  wait_http "${API_URL}/healthz"
  wait_http "${SIMULATOR_URL}/healthz"

  for i in $(seq 1 10); do
    create_billing "$((1000 + i))"
  done
  wait_stats_total 10 30
  assert_duplicates 0
}

scenario_lease_timeout() {
  echo "== lease-timeout: claimed job is picked up again after worker dies =="
  cleanup
  OUTBOX_LEASE_SECONDS=2 \
    WORKER_PROCESS_DELAY=10s \
    docker compose up --build -d spanner spanner-init api simulator worker
  wait_http "${API_URL}/healthz"
  wait_http "${SIMULATOR_URL}/healthz"

  create_billing 1300
  sleep 1
  docker compose stop worker >/dev/null
  sleep 3
  OUTBOX_LEASE_SECONDS=2 \
    WORKER_PROCESS_DELAY=0s \
    docker compose up -d --force-recreate worker
  wait_stats_total 1 30
  assert_duplicates 0
}

scenario_payment_webhook() {
  echo "== payment-webhook: incoming bank webhook enqueues merchant webhook and mail =="
  cleanup
  docker compose up --build -d spanner spanner-init api simulator worker
  wait_http "${API_URL}/healthz"
  wait_http "${SIMULATOR_URL}/healthz"

  local billing_json
  billing_json="$(create_billing_json 2200 merchant@example.test)"
  local billing_id
  billing_id="$(python3 -c "import json,sys; print(json.load(sys.stdin)['billingId'])" <<<"${billing_json}")"

  post_payment_webhook "bank-event-001" "${billing_id}" 2200 >/tmp/gcp-outbox-payment-response.json
  post_payment_webhook "bank-event-001" "${billing_id}" 2200 >/tmp/gcp-outbox-payment-duplicate-response.json

  wait_stats_total 2 30
  assert_duplicates 0
  wait_spanner_count "SELECT COUNT(*) AS Count FROM IncomingPaymentNotifications" 1 30
  wait_spanner_count "SELECT COUNT(*) AS Count FROM MailDeliveries" 1 30
  wait_spanner_count "SELECT COUNT(*) AS Count FROM OutboxJobs WHERE Status = 'completed'" 3 30
}

main() {
  trap cleanup EXIT

  scenario_retry
  scenario_multi_worker
  scenario_lease_timeout
  scenario_payment_webhook

  echo "All local outbox scenarios passed."
}

main "$@"
