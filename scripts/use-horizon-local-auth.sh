#!/usr/bin/env bash
set -euo pipefail

# Local auth helper for prevalent-horizon-test without service-account keys.
# Uses gcloud ADC JSON at the standard location.

PROJECT_ID="${GCP_PROJECT_ID:-prevalent-horizon-test}"
ADC_PATH="${GOOGLE_APPLICATION_CREDENTIALS:-$HOME/.config/gcloud/application_default_credentials.json}"

if [[ ! -f "$ADC_PATH" ]]; then
  echo "ADC file not found: $ADC_PATH" >&2
  echo "Run: gcloud auth application-default login" >&2
  exit 1
fi

export GCP_PROJECT_ID="$PROJECT_ID"
export GOOGLE_CLOUD_PROJECT="$PROJECT_ID"
export FIREBASE_AUTH_PROJECT_ID="$PROJECT_ID"
export GOOGLE_APPLICATION_CREDENTIALS="$ADC_PATH"

if [[ $# -gt 0 ]]; then
  exec "$@"
fi

echo "Local auth env is set:"
echo "  GCP_PROJECT_ID=$GCP_PROJECT_ID"
echo "  GOOGLE_CLOUD_PROJECT=$GOOGLE_CLOUD_PROJECT"
echo "  FIREBASE_AUTH_PROJECT_ID=$FIREBASE_AUTH_PROJECT_ID"
echo "  GOOGLE_APPLICATION_CREDENTIALS=$GOOGLE_APPLICATION_CREDENTIALS"
echo
echo "Usage:"
echo "  source scripts/use-horizon-local-auth.sh"
echo "  scripts/use-horizon-local-auth.sh go run ./backend/cmd/server"
