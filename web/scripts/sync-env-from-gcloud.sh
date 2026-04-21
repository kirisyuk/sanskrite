#!/usr/bin/env bash
# Refresh local Vite env from Cloud Run and firebase.sdkconfig.web.json in the active gcloud project.
# If .env.local exists, update it because Vite gives it higher priority than .env.
set -euo pipefail
WEB_ROOT=$(cd "$(dirname "$0")/.." && pwd)
PROJECT=$(gcloud config get-value project 2>/dev/null | tr -d '\n' || true)
REGION="${REGION:-us-central1}"
if [[ -z "$PROJECT" || "$PROJECT" == "(unset)" ]]; then
  echo "gcloud project is not set. Run: gcloud config set project <ID>" >&2
  exit 1
fi

API_URL=$(gcloud run services describe api --region "$REGION" --project "$PROJECT" --format='value(status.url)' 2>/dev/null || true)
WS_URL=$(gcloud run services describe ws --region "$REGION" --project "$PROJECT" --format='value(status.url)' 2>/dev/null || true)
if [[ -z "$API_URL" ]]; then
  echo "api not found in project $PROJECT (region $REGION)." >&2
  exit 1
fi
if [[ -z "$WS_URL" ]]; then
  echo "ws not found in project $PROJECT (region $REGION)." >&2
  exit 1
fi

ENV_FILE="$WEB_ROOT/.env"
if [[ -f "$WEB_ROOT/.env.local" ]]; then
  ENV_FILE="$WEB_ROOT/.env.local"
fi

FIREBASE_PROJECT_ID="$PROJECT"
FIREBASE_AUTH_DOMAIN="${PROJECT}.firebaseapp.com"
FIREBASE_STORAGE_BUCKET="${PROJECT}.firebasestorage.app"
FIREBASE_API_KEY=""
FIREBASE_MESSAGING_SENDER_ID=""
FIREBASE_APP_ID=""
SDK_FILE="$WEB_ROOT/firebase.sdkconfig.web.json"
if [[ -f "$SDK_FILE" ]]; then
  FIREBASE_API_KEY=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1],encoding='utf-8')).get('apiKey',''))" "$SDK_FILE" 2>/dev/null || true)
  FIREBASE_MESSAGING_SENDER_ID=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1],encoding='utf-8')).get('messagingSenderId',''))" "$SDK_FILE" 2>/dev/null || true)
  FIREBASE_APP_ID=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1],encoding='utf-8')).get('appId',''))" "$SDK_FILE" 2>/dev/null || true)
fi

TMP=$(mktemp)
if [[ -f "$ENV_FILE" ]]; then
  grep -v '^VITE_API_BASE_URL=' "$ENV_FILE" | \
    grep -v '^VITE_WS_BROKER_URL=' | \
    grep -v '^VITE_WS_BASE_URL=' | \
    grep -v '^VITE_FIREBASE_PROJECT_ID=' | \
    grep -v '^VITE_FIREBASE_AUTH_DOMAIN=' | \
    grep -v '^VITE_FIREBASE_STORAGE_BUCKET=' | \
    grep -v '^VITE_FIREBASE_API_KEY=' | \
    grep -v '^VITE_FIREBASE_MESSAGING_SENDER_ID=' | \
    grep -v '^VITE_FIREBASE_APP_ID=' >"$TMP" || true
else
  echo "# Synced from gcloud project $PROJECT" >"$TMP"
fi
{
  echo "VITE_FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID"
  echo "VITE_FIREBASE_AUTH_DOMAIN=$FIREBASE_AUTH_DOMAIN"
  echo "VITE_FIREBASE_STORAGE_BUCKET=$FIREBASE_STORAGE_BUCKET"
  [[ -n "$FIREBASE_API_KEY" ]] && echo "VITE_FIREBASE_API_KEY=$FIREBASE_API_KEY"
  [[ -n "$FIREBASE_MESSAGING_SENDER_ID" ]] && echo "VITE_FIREBASE_MESSAGING_SENDER_ID=$FIREBASE_MESSAGING_SENDER_ID"
  [[ -n "$FIREBASE_APP_ID" ]] && echo "VITE_FIREBASE_APP_ID=$FIREBASE_APP_ID"
  echo "VITE_API_BASE_URL=$API_URL"
  echo "VITE_WS_BROKER_URL=$WS_URL"
} >>"$TMP"
mv "$TMP" "$ENV_FILE"
echo "Updated $ENV_FILE (project=$PROJECT):"
echo "  VITE_FIREBASE_PROJECT_ID=$FIREBASE_PROJECT_ID"
echo "  VITE_API_BASE_URL=$API_URL"
echo "  VITE_WS_BROKER_URL=$WS_URL"
