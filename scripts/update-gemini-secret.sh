#!/usr/bin/env bash
# Обновить секрет gemini_api_key в GCP Secret Manager.
# Ключ можно получить: https://aistudio.google.com/apikey
#
# Использование:
#   echo -n "YOUR_GEMINI_API_KEY" | ./scripts/update-gemini-secret.sh
#   GEMINI_API_KEY="YOUR_KEY" ./scripts/update-gemini-secret.sh
#   ./scripts/update-gemini-secret.sh   # запросит ввод (без эха)

set -euo pipefail

PROJECT_ID="${1:-$(gcloud config get-value project 2>/dev/null)}"
[[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]] && { echo "Set project: gcloud config set project ID"; exit 1; }

SECRET_NAME="gemini_api_key"

if [[ -n "${GEMINI_API_KEY:-}" ]]; then
  KEY="$GEMINI_API_KEY"
elif [[ ! -t 0 ]]; then
  KEY=$(cat)
else
  echo -n "Paste Gemini API key (from https://aistudio.google.com/apikey): "
  read -rs KEY
  echo
fi

[[ -z "$KEY" ]] && { echo "Empty key, aborting."; exit 1; }

if gcloud secrets describe "$SECRET_NAME" --project="$PROJECT_ID" >/dev/null 2>&1; then
  echo -n "$KEY" | gcloud secrets versions add "$SECRET_NAME" --project="$PROJECT_ID" --data-file=-
  echo "✓ Updated secret: $SECRET_NAME (new version added)"
else
  echo -n "$KEY" | gcloud secrets create "$SECRET_NAME" --project="$PROJECT_ID" --data-file=- --replication-policy=automatic
  echo "✓ Created secret: $SECRET_NAME"
fi

echo "Cloud Run uses GEMINI_API_KEY=gemini_api_key:latest — new revisions will pick the new value."
echo "To apply immediately: redeploy API (e.g. bash scripts/deploy --api)"
