#!/usr/bin/env bash
# Применение секретов из scripts/secrets.env в GCP.
# Deploy вызывает этот скрипт, если secrets.env существует.
# Формат файла: SECRET_NAME=<base64-encoded-value> (по одному на строку)
#
# Использование: ./scripts/apply-secrets.sh [PROJECT_ID]

set -euo pipefail

PROJECT_ID="${1:-$(gcloud config get-value project 2>/dev/null)}"
[[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]] && { echo "Set project: gcloud config set project ID"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SECRETS_FILE="${SECRETS_FILE:-$SCRIPT_DIR/secrets.env}"

if [[ ! -f "$SECRETS_FILE" ]]; then
  echo "Secrets file not found: $SECRETS_FILE"
  echo "Run ./scripts/export-secrets.sh to export from existing GCP project."
  exit 1
fi

echo "Applying secrets from $SECRETS_FILE to $PROJECT_ID ..."
count=0
while IFS= read -r line || [[ -n "$line" ]]; do
  [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
  name="${line%%=*}"
  b64="${line#*=}"
  [[ -z "$name" || -z "$b64" ]] && continue

  value=$(echo "$b64" | base64 -d 2>/dev/null || echo "$b64" | base64 -D 2>/dev/null) || { echo "⚠ Skipping $name (invalid base64)"; continue; }

  if gcloud secrets describe "$name" --project="$PROJECT_ID" >/dev/null 2>&1; then
    echo -n "$value" | gcloud secrets versions add "$name" --project="$PROJECT_ID" --data-file=- --quiet 2>/dev/null || true
    echo "  ✓ $name (updated)"
  else
    echo -n "$value" | gcloud secrets create "$name" --project="$PROJECT_ID" --data-file=- --replication-policy=automatic --quiet 2>/dev/null || true
    echo "  ✓ $name (created)"
  fi
  ((count++)) || true
done < "$SECRETS_FILE"

echo "Applied $count secrets."
