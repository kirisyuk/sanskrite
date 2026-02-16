#!/usr/bin/env bash
# Compare Firestore indexes: infra (gcloud) vs web/firestore.indexes.json
# Output: indexes that exist in infra but are NOT in the JSON file

set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
JSON_FILE="$ROOT/web/firestore.indexes.json"
INFRA_FILE="${1:-/tmp/infra_indexes.json}"
JSON_SIGS_FILE=$(mktemp)
trap 'rm -f "$JSON_SIGS_FILE"' EXIT

if [[ ! -f "$INFRA_FILE" ]]; then
  echo "Fetching indexes from GCP..."
  gcloud firestore indexes composite list --format=json > "$INFRA_FILE"
fi

# Build set of JSON index signatures (collectionGroup|field1:order1,field2:order2,...)
# Field order matters for Firestore - we preserve it
jq -r '.indexes[] | 
  .collectionGroup as $cg | 
  .fields | 
  [.[] | select(.fieldPath != "__name__") | 
   if .arrayConfig then "\(.fieldPath):CONTAINS"
   else "\(.fieldPath):\(.order)"
   end] | "\($cg)|" + (join(","))
' "$JSON_FILE" | sort -u > "$JSON_SIGS_FILE"

# Add fieldOverrides (vector indexes)
jq -r '.fieldOverrides[]? | "\(.collectionGroup)|\(.fieldPath):VECTOR"' "$JSON_FILE" 2>/dev/null >> "$JSON_SIGS_FILE" || true

echo "=== Индексы в инфраструктуре, отсутствующие в web/firestore.indexes.json ==="
echo ""

missing=0
while IFS= read -r idx; do
  name=$(echo "$idx" | jq -r '.name')
  cg=$(echo "$name" | sed 's|.*/collectionGroups/\([^/]*\)/indexes/.*|\1|')
  fields=$(echo "$idx" | jq -c '.fields')
  
  # Skip pure vector indexes (only __name__ + vector field) - they're in fieldOverrides
  if echo "$fields" | jq -e '[.[] | select(.fieldPath != "__name__")] | length == 1 and .[0].vectorConfig != null' >/dev/null 2>&1; then
    continue
  fi
  
  # Build signature - preserve field order for composite indexes
  sig=$(echo "$fields" | jq -r --arg cg "$cg" '
    [.[] | select(.fieldPath != "__name__") |
     if .arrayConfig then "\(.fieldPath):CONTAINS"
     elif .vectorConfig then "\(.fieldPath):VECTOR"
     else "\(.fieldPath):\(.order)"
     end] | "\($cg)|" + (join(","))
  ')
  
  if ! grep -Fxq "$sig" "$JSON_SIGS_FILE" 2>/dev/null; then
    ((missing++)) || true
    echo "Collection: $cg"
    echo "$fields" | jq -r '.[] | "  - \(.fieldPath) \(if .order then .order elif .arrayConfig then .arrayConfig elif .vectorConfig then "VECTOR" else "" end)"'
    echo ""
  fi
done < <(jq -c '.[]' "$INFRA_FILE")

if [[ $missing -eq 0 ]]; then
  echo "Все индексы из инфраструктуры присутствуют в JSON."
else
  echo "Всего отсутствующих в JSON: $missing"
fi
