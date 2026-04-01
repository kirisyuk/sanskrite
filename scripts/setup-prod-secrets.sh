#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   export ANTHROPIC_API_KEY=...
#   export DEEPSEEK_API_KEY=...
#   export TAVILY_API_KEY=...
#   ./scripts/setup-prod-secrets.sh [-p|--project nv-horizon]

PROJECT="nv-horizon"
while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--project)
      PROJECT="$2"; shift 2 ;;
    *)
      echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done

if ! command -v gcloud >/dev/null 2>&1; then
  echo "gcloud not found. Install Google Cloud SDK first." >&2
  exit 1
fi

echo "Setting project: ${PROJECT}"
gcloud config set project "${PROJECT}" >/dev/null

create_if_missing() {
  local name="$1"
  if gcloud secrets describe "$name" >/dev/null 2>&1; then
    return 0
  fi
  gcloud secrets create "$name" --replication-policy=automatic \
    --labels=environment=prod,owner=nv-horizon >/dev/null
}

add_version() {
  local name="$1"
  local value="$2"
  echo -n "$value" | gcloud secrets versions add "$name" --data-file=- >/dev/null
}

upsert_secret_from_env() {
  local name="$1"
  local val
  val="${!name-}"
  if [[ -z "${val}" ]]; then
    echo "[skip] $name not set in env"
    return 0
  fi
  create_if_missing "$name"
  add_version "$name" "$val"
  echo "[ok]   $name updated"
}

# Production secrets
upsert_secret_from_env ANTHROPIC_API_KEY
upsert_secret_from_env DEEPSEEK_API_KEY
upsert_secret_from_env TAVILY_API_KEY

# Also upsert lowercase alias secrets expected by deploy script
if [[ -n "${ANTHROPIC_API_KEY-}" ]]; then
  create_if_missing anthropic_api_key
  add_version anthropic_api_key "$ANTHROPIC_API_KEY"
  echo "[ok]   anthropic_api_key updated"
fi
if [[ -n "${DEEPSEEK_API_KEY-}" ]]; then
  create_if_missing deepseek_api_key
  add_version deepseek_api_key "$DEEPSEEK_API_KEY"
  echo "[ok]   deepseek_api_key updated"
fi
if [[ -n "${TAVILY_API_KEY-}" ]]; then
	create_if_missing tavily_api_key
	add_version tavily_api_key "$TAVILY_API_KEY"
	echo "[ok]   tavily_api_key updated"
fi

echo "Done."


