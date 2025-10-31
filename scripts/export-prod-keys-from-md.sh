#!/usr/bin/env bash
set -euo pipefail

# Source production keys from TEST_KEYS.md without printing values,
# then delegate to setup-prod-secrets.sh with forwarded args.

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
MD_FILE="$ROOT_DIR/TEST_KEYS.md"

if [[ ! -f "$MD_FILE" ]]; then
  echo "TEST_KEYS.md not found at $MD_FILE" >&2
  exit 1
fi

extract_between_code_fence_after_header() {
  local header="$1"
  awk -v hdr="$header" '
    BEGIN { found=0; inblock=0; }
    $0 ~ hdr { found=1; next }
    found && /^```/ { inblock++; next }
    found && inblock==1 { print }
    found && inblock==2 { exit }
  ' "$MD_FILE" | sed -e 's/^\s\+//;s/\s\+$//' | head -n 1
}

CLAUDE_KEY="$(extract_between_code_fence_after_header '^### Claude')"
DEEPSEEK_KEY="$(extract_between_code_fence_after_header '^### DeepSeek')"

if [[ -z "${CLAUDE_KEY}" || -z "${DEEPSEEK_KEY}" ]]; then
  echo "Failed to extract production keys from TEST_KEYS.md" >&2
  exit 2
fi

export ANTHROPIC_API_KEY="$CLAUDE_KEY"
export DEEPSEEK_API_KEY="$DEEPSEEK_KEY"

echo "[ok] Loaded production keys from TEST_KEYS.md (masked)."

exec "$ROOT_DIR/scripts/setup-prod-secrets.sh" "$@"


