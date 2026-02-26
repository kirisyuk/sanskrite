#!/usr/bin/env bash
set -euo pipefail

# Скрипт для конвертации документов из .txt в JSONL и импорта в Vertex AI Search
# Использование: ./scripts/import-documents-to-vertex-search.sh [-p|--project h1horizon-488615]

PROJECT="h1horizon-488615"
DATA_STORE_ID="rag-documents-store"
LOCATION="global"
SOURCE_BUCKET="h1horizon-rag-documents"
TEMP_BUCKET="${PROJECT}-vertex-search-import"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -p|--project) PROJECT="$2"; shift 2;;
    *) echo "Unknown arg: $1" >&2; exit 2;;
  esac
done

echo "Importing documents to Vertex AI Search"
echo "Project: $PROJECT"
echo "Data Store: $DATA_STORE_ID"
echo "Source bucket: gs://${SOURCE_BUCKET}"
echo ""

# Получаем access token
ACCESS_TOKEN=$(gcloud auth print-access-token --project="$PROJECT" 2>/dev/null || echo "")
if [[ -z "$ACCESS_TOKEN" ]]; then
  echo "❌ Failed to get access token. Run: gcloud auth login"
  exit 1
fi

# Создаем временный bucket для JSONL файлов (если не существует)
echo "1. Preparing temporary bucket for JSONL files..."
if ! gcloud storage buckets describe "gs://${TEMP_BUCKET}" --project="$PROJECT" >/dev/null 2>&1; then
  gcloud storage buckets create "gs://${TEMP_BUCKET}" --location=US --project="$PROJECT" --uniform-bucket-level-access
  echo "  ✅ Created temporary bucket: gs://${TEMP_BUCKET}"
else
  echo "  ✅ Temporary bucket exists: gs://${TEMP_BUCKET}"
fi

# Создаем временную директорию для JSONL
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

echo ""
echo "2. Converting .txt files to JSONL format..."

# Используем Python для конвертации (более надежно)
python3 <<PYTHON_SCRIPT
import subprocess
import json
import sys
import os
import tempfile

project = "${PROJECT}"
source_bucket = "${SOURCE_BUCKET}"
temp_dir = "${TEMP_DIR}"
jsonl_file = os.path.join(temp_dir, "documents.jsonl")

# Получаем список всех .txt файлов
result = subprocess.run(
    ["gsutil", "ls", "-r", f"gs://{source_bucket}/**/*.txt"],
    capture_output=True,
    text=True
)

txt_files = [f.strip() for f in result.stdout.split('\n') if f.strip().endswith('.txt')]

if not txt_files:
    print("  ⚠️  No .txt files found")
    sys.exit(1)

count = 0
with open(jsonl_file, 'w', encoding='utf-8') as out:
    for txt_file in txt_files:
        try:
            # Извлекаем метаданные из пути
            rel_path = txt_file.replace(f"gs://{source_bucket}/", "")
            parts = rel_path.split('/')
            
            if len(parts) < 3:
                continue
                
            tenant_id = parts[0]
            source_id = parts[1]
            doc_id = parts[2].replace('.txt', '')
            
            # Читаем содержимое файла
            content_result = subprocess.run(
                ["gsutil", "cat", txt_file],
                capture_output=True,
                text=True,
                encoding='utf-8',
                errors='ignore'
            )
            
            if not content_result.stdout.strip():
                continue
            
            # Пытаемся получить title из метаданных GCS
            stat_result = subprocess.run(
                ["gsutil", "stat", txt_file],
                capture_output=True,
                text=True
            )
            title = doc_id
            for line in stat_result.stdout.split('\n'):
                if 'title:' in line.lower():
                    title = line.split(':', 1)[1].strip()
                    break
            
            # Формируем JSON документ для Vertex AI Search
            # Формат Discovery Engine: structData должен содержать все поля включая текстовый контент
            # Для текстовых документов content должен быть в structData как строка
            doc = {
                "id": doc_id,
                "structData": {
                    "title": title,
                    "content": content_result.stdout,  # Текстовый контент как строка в structData
                    "uri": txt_file,
                    "tenantId": tenant_id,
                    "sourceId": source_id,
                    "documentId": doc_id
                }
            }
            
            # Записываем в JSONL (одна строка JSON на строку)
            out.write(json.dumps(doc, ensure_ascii=False) + '\n')
            count += 1
            
            if count % 10 == 0:
                print(f"  Processed {count} documents...")
        except Exception as e:
            print(f"  ⚠️  Error processing {txt_file}: {e}", file=sys.stderr)
            continue

print(f"  ✅ Converted {count} documents to JSONL")
if count == 0:
    sys.exit(1)
PYTHON_SCRIPT

if [[ $? -ne 0 ]]; then
  echo "  ❌ Conversion failed"
  exit 1
fi

JSONL_FILE="${TEMP_DIR}/documents.jsonl"
COUNT=$(wc -l < "$JSONL_FILE" 2>/dev/null || echo "0")

if [[ "$COUNT" -eq 0 ]]; then
  echo "  ❌ No documents to import"
  exit 1
fi

echo ""
echo "3. Uploading JSONL file to GCS..."
JSONL_GCS_PATH="gs://${TEMP_BUCKET}/documents-$(date +%s).jsonl"
gsutil cp "$JSONL_FILE" "$JSONL_GCS_PATH"
echo "  ✅ Uploaded to: $JSONL_GCS_PATH"

echo ""
echo "4. Importing documents to Vertex AI Search..."
IMPORT_RESPONSE=$(curl -s -X POST \
  -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  -H "X-Goog-User-Project: ${PROJECT}" \
  -H "Content-Type: application/json" \
  "https://discoveryengine.googleapis.com/v1beta/projects/${PROJECT}/locations/${LOCATION}/collections/default_collection/dataStores/${DATA_STORE_ID}/branches/default_branch/documents:import" \
  -d "{
    \"gcsSource\": {
      \"inputUris\": [\"${JSONL_GCS_PATH}\"],
      \"dataSchema\": \"document\"
    }
  }" 2>/dev/null)

if echo "$IMPORT_RESPONSE" | grep -q "\"name\""; then
  OPERATION_NAME=$(echo "$IMPORT_RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('name', ''))" 2>/dev/null || echo "")
  echo "  ✅ Import operation started"
  echo "     Operation: $OPERATION_NAME"
  echo ""
  echo "  ⏳ Import is running in background. Check status:"
  echo "     (если `gcloud discovery-engine ...` доступен) gcloud discovery-engine operations describe $OPERATION_NAME --location=$LOCATION --project=$PROJECT"
  echo ""
  echo "  Or check via API:"
  echo "     ACCESS_TOKEN=\"\\$(gcloud auth print-access-token)\""
  echo "     curl -H \"Authorization: Bearer \\$ACCESS_TOKEN\" -H \"x-goog-user-project: $PROJECT\" \\"
  echo "       \"https://discoveryengine.googleapis.com/v1beta/$OPERATION_NAME\""
else
  ERROR_MSG=$(echo "$IMPORT_RESPONSE" | python3 -c "import sys, json; print(json.load(sys.stdin).get('error', {}).get('message', 'Unknown error'))" 2>/dev/null || echo "Unknown error")
  echo "  ❌ Import failed: $ERROR_MSG"
  echo "$IMPORT_RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$IMPORT_RESPONSE"
  exit 1
fi

echo ""
echo "Done. Run ./scripts/test-vertex-search.sh to verify after import completes."
