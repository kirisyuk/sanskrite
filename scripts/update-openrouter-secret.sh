#!/usr/bin/env bash
# Обновить секрет openrouter_api_key в GCP Secret Manager.
# Ключ: https://openrouter.ai/keys
#
# Использование:
#   echo -n "YOUR_OPENROUTER_KEY" | ./scripts/update-openrouter-secret.sh
#   OPENROUTER_API_KEY="YOUR_KEY" ./scripts/update-openrouter-secret.sh
#   ./scripts/update-openrouter-secret.sh   # запросит ввод (без эха)

set -euo pipefail

PROJECT_ID="${1:-$(gcloud config get-value project 2>/dev/null)}"
[[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]] && { echo "Set project: gcloud config set project ID"; exit 1; }

SECRET_NAME="openrouter_api_key"

if [[ -n "${OPENROUTER_API_KEY:-}" ]]; then
  KEY="$OPENROUTER_API_KEY"
elif [[ ! -t 0 ]]; then
  KEY=$(cat)
else
  echo -n "Paste OpenRouter API key (from https://openrouter.ai/keys): "
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

echo "Cloud Run: bind OPENROUTER_API_KEY=openrouter_api_key:latest (scripts/deploy adds this when the secret exists)."
