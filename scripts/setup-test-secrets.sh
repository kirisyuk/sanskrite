#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   export OPENAI_API_KEY_TEST=...
#   export ANTHROPIC_API_KEY_TEST=...
#   export DEEPSEEK_API_KEY_TEST=...
#   export GROK_API_KEY_TEST=...
#   export VERTEX_API_KEY_TEST=...
#   export TAVILY_API_KEY_TEST=...
#   ./scripts/setup-test-secrets.sh [-p|--project sanskrite]

PROJECT="sanskrite"
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
  gcloud secrets create "$name" --replication-policy=automatic >/dev/null
}

add_version() {
  local name="$1"
  local value="$2"
  # Avoid printing secret value; only print masked info
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

# Known _TEST secrets
upsert_secret_from_env OPENAI_API_KEY_TEST
upsert_secret_from_env ANTHROPIC_API_KEY_TEST
upsert_secret_from_env DEEPSEEK_API_KEY_TEST
upsert_secret_from_env GROK_API_KEY_TEST
upsert_secret_from_env VERTEX_API_KEY_TEST
upsert_secret_from_env TAVILY_API_KEY_TEST

echo "Done."


