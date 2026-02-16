#!/usr/bin/env bash
# Экспорт секретов из GCP в scripts/secrets.env.
# Файл secrets.env в .gitignore — не коммитить!
#
# Использование: ./scripts/export-secrets.sh [PROJECT_ID]

set -euo pipefail

PROJECT_ID="${1:-$(gcloud config get-value project 2>/dev/null)}"
[[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]] && { echo "Set project: gcloud config set project ID"; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OUT_FILE="$SCRIPT_DIR/secrets.env"

echo "Exporting secrets from $PROJECT_ID to $OUT_FILE ..."
: > "$OUT_FILE"
count=0
for s in $(gcloud secrets list --project="$PROJECT_ID" --format='value(name)' 2>/dev/null | sort); do
  v=$(gcloud secrets versions access latest --secret="$s" --project="$PROJECT_ID" 2>/dev/null | base64 2>/dev/null | tr -d '\n')
  echo "${s}=${v}" >> "$OUT_FILE"
  ((count++)) || true
done

echo "Exported $count secrets to $OUT_FILE"
echo "⚠ Файл в .gitignore — не коммитить!"
