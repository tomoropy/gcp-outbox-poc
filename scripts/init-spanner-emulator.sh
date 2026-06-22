#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${CLOUDSDK_CORE_PROJECT:-sandbox-500107}"
INSTANCE_ID="${SPANNER_INSTANCE_ID:-gcp-outbox-poc}"
DATABASE_ID="${SPANNER_DATABASE_ID:-gcp-outbox-poc}"

echo "Waiting for Spanner emulator..."
for _ in $(seq 1 30); do
  if gcloud spanner instance-configs list --project "${PROJECT_ID}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! gcloud spanner instances describe "${INSTANCE_ID}" --project "${PROJECT_ID}" >/dev/null 2>&1; then
  echo "Creating Spanner emulator instance: ${INSTANCE_ID}"
  gcloud spanner instances create "${INSTANCE_ID}" \
    --project "${PROJECT_ID}" \
    --config=emulator-config \
    --description="GCP Outbox PoC local emulator" \
    --nodes=1
else
  echo "Spanner emulator instance already exists: ${INSTANCE_ID}"
fi

if ! gcloud spanner databases describe "${DATABASE_ID}" --instance "${INSTANCE_ID}" --project "${PROJECT_ID}" >/dev/null 2>&1; then
  echo "Creating Spanner emulator database: ${DATABASE_ID}"
  gcloud spanner databases create "${DATABASE_ID}" \
    --project "${PROJECT_ID}" \
    --instance="${INSTANCE_ID}" \
    --ddl-file=/workspace/migrations/schema.sql
else
  echo "Spanner emulator database already exists: ${DATABASE_ID}"
fi

echo "Spanner emulator is ready."
