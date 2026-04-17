#!/usr/bin/env bash
# AgentHorde smoke: 5 supervisor cycles, Vertex via ADC (gcloud application-default login).
# Uses OAuth from the environment; set GOOGLE_CLOUD_PROJECT or rely on gcloud default project.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export GOOGLE_GENAI_USE_VERTEXAI="${GOOGLE_GENAI_USE_VERTEXAI:-true}"

if [[ -z "${GOOGLE_CLOUD_PROJECT:-}" ]]; then
  if command -v gcloud >/dev/null 2>&1; then
    GCLOUD_PROJECT="$(gcloud config get-value project 2>/dev/null | tr -d '\n' || true)"
    if [[ -n "$GCLOUD_PROJECT" && "$GCLOUD_PROJECT" != "(unset)" ]]; then
      export GOOGLE_CLOUD_PROJECT="$GCLOUD_PROJECT"
    fi
  fi
fi

if [[ -z "${GOOGLE_CLOUD_PROJECT:-}" ]]; then
  echo "Set GOOGLE_CLOUD_PROJECT or run: gcloud config set project <id>" >&2
  exit 1
fi

export AGENTHORDE_MAX_SUPERVISOR_CYCLES="${AGENTHORDE_MAX_SUPERVISOR_CYCLES:-5}"

INPUT="${1:-$ROOT/fixtures/lithium_supply_sa_to_de.json}"
if [[ ! -f "$INPUT" ]]; then
  echo "Input file not found: $INPUT" >&2
  exit 1
fi

echo "AgentHorde lithium smoke: project=$GOOGLE_CLOUD_PROJECT cycles=$AGENTHORDE_MAX_SUPERVISOR_CYCLES input=$INPUT" >&2

OUT="$(python3 "$ROOT/runner_cli.py" --input-file "$INPUT")"
printf '%s\n' "$OUT"

if command -v jq >/dev/null 2>&1; then
  echo "$OUT" | jq -e '.canonicalGraph != null' >/dev/null
  echo "$OUT" | jq -e '(.canonicalGraph.graph.fragments | type) == "array" and (.canonicalGraph.graph.edges | type) == "array"' >/dev/null
  echo "$OUT" | jq -e '(.canonicalGraph.evidenceArchive.sources | type) == "array" and (.canonicalGraph.evidenceArchive.links | type) == "array"' >/dev/null
fi
