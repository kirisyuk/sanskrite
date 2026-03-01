#!/bin/bash
# Скрипт проверки инфраструктуры GCP для Etigram

set -e

PROJECT_ID=$(gcloud config get-value project)
if [ -z "$PROJECT_ID" ]; then
  echo "ERROR: GCP project not set. Run: gcloud config set project PROJECT_ID"
  exit 1
fi

echo "=== Проверка инфраструктуры Etigram для проекта: $PROJECT_ID ==="
echo ""

# Цвета для вывода
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

ERRORS=0
WARNINGS=0

# Функция проверки
check() {
  if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓${NC} $1"
  else
    echo -e "${RED}✗${NC} $1"
    ERRORS=$((ERRORS + 1))
  fi
}

warn() {
  echo -e "${YELLOW}⚠${NC} $1"
  WARNINGS=$((WARNINGS + 1))
}

echo "1. Проверка Firestore коллекций..."
echo "   (Коллекции создаются автоматически при первом использовании)"
check "Firestore доступен"

echo ""
echo "2. Проверка GCS Buckets..."
BUCKETS=$(gcloud storage buckets list --format="value(name)" 2>/dev/null)
if echo "$BUCKETS" | grep -q "core-docs\|rag-documents"; then
  echo "$BUCKETS" | grep -E "core-docs|rag-documents" | while read bucket; do
    check "Bucket существует: $bucket"
  done
else
  warn "Не найдены buckets для Etigram (core-docs или rag-documents)"
fi

echo ""
echo "3. Проверка Vertex AI Search..."
# Проверка API
if gcloud services list --enabled --filter="name:discoveryengine.googleapis.com" --format="value(name)" 2>/dev/null | grep -q discoveryengine; then
  check "Discovery Engine API включен"
else
  warn "Discovery Engine API не включен. Включите: gcloud services enable discoveryengine.googleapis.com"
fi

# Проверка Data Store (через REST API, т.к. gcloud discovery-engine может быть недоступен)
DATA_STORE_ID="${VERTEX_SEARCH_DATA_STORE_ID:-rag-documents-store}"
LOCATION="${VERTEX_SEARCH_LOCATION:-global}"
ACCESS_TOKEN="$(gcloud auth print-access-token 2>/dev/null || true)"
if [ -z "$ACCESS_TOKEN" ]; then
  warn "Не удалось получить access token (gcloud auth print-access-token). Пропускаем проверку Data Store."
else
  RESP_FILE="$(mktemp)"
  HTTP_CODE="$(
    curl -sS -o "$RESP_FILE" -w "%{http_code}" \
      -H "Authorization: Bearer $ACCESS_TOKEN" \
      -H "x-goog-user-project: $PROJECT_ID" \
      "https://discoveryengine.googleapis.com/v1beta/projects/$PROJECT_ID/locations/$LOCATION/collections/default_collection/dataStores" \
      2>/dev/null || echo "000"
  )"

  if [ "$HTTP_CODE" != "200" ]; then
    warn "Не удалось проверить Data Store через Discovery Engine API (HTTP $HTTP_CODE). Проверьте права/квоты (quota project) и доступ к discoveryengine.googleapis.com."
  else
    if grep -q "/dataStores/$DATA_STORE_ID\"" "$RESP_FILE"; then
      check "Data Store существует: $DATA_STORE_ID"
    else
      warn "Data Store не найден: $DATA_STORE_ID (проверьте в Gen App Builder → Data Stores)"
    fi
  fi
fi

echo ""
echo "4. Проверка Pub/Sub топиков..."
# Проверка основных топиков (агрегированные создаются автоматически)
REQUIRED_TOPICS=("source.poll" "etigram.operations.events" "etigram.interaction.events" "etigram.horizon.events")
for topic in "${REQUIRED_TOPICS[@]}"; do
  if gcloud pubsub topics describe "$topic" --format="value(name)" 2>/dev/null | grep -q "$topic"; then
    check "Топик существует: $topic"
  else
    warn "Топик не найден: $topic (будет создан автоматически при первом использовании)"
  fi
done

echo ""
echo "4.1 Проверка Pub/Sub подписок для etigram-ws..."
# Подписка имеет фиксированный нейминг: <topic>-ws-broker
REQUIRED_SUBS=("etigram.horizon.events-ws-broker")
for sub in "${REQUIRED_SUBS[@]}"; do
  if gcloud pubsub subscriptions describe "$sub" --format="value(name)" 2>/dev/null | grep -q "$sub"; then
    check "Подписка существует: $sub"
  else
    warn "Подписка не найдена: $sub (создайте: scripts/setup-etigram-pubsub.sh)"
  fi
done

echo ""
echo "5. Проверка Secret Manager..."
REQUIRED_SECRETS=("vertex_api_key" "gemini_api_key" "openai_api_key" "anthropic_api_key")
for secret in "${REQUIRED_SECRETS[@]}"; do
  if gcloud secrets describe "$secret" --format="value(name)" 2>/dev/null | grep -q "$secret"; then
    check "Секрет существует: $secret"
  else
    warn "Секрет не найден: $secret"
  fi
done

echo ""
echo "6. Проверка IAM ролей для Service Account..."
SA="backend@${PROJECT_ID}.iam.gserviceaccount.com"
if gcloud iam service-accounts describe "$SA" --format="value(email)" 2>/dev/null | grep -q backend; then
  check "Service Account существует: $SA"
  
  # Проверка ролей
  ROLES=$(gcloud projects get-iam-policy "$PROJECT_ID" --flatten="bindings[].members" --filter="bindings.members:$SA" --format="value(bindings.role)" 2>/dev/null)
  
  REQUIRED_ROLES=("roles/datastore.user" "roles/secretmanager.secretAccessor" "roles/pubsub.admin" "roles/storage.objectAdmin" "roles/discoveryengine.editor")
  for role in "${REQUIRED_ROLES[@]}"; do
    if echo "$ROLES" | grep -q "$role"; then
      check "Роль назначена: $role"
    else
      warn "Роль не назначена: $role"
    fi
  done
else
  warn "Service Account не найден: $SA"
fi

echo ""
echo "7. Проверка переменных окружения Cloud Run..."
SERVICE="etigram-api"
REGION="us-central1"
if gcloud run services describe "$SERVICE" --region="$REGION" --format="value(metadata.name)" 2>/dev/null | grep -q "$SERVICE"; then
  check "Cloud Run сервис существует: $SERVICE"
  
  ENV_VARS=$(gcloud run services describe "$SERVICE" --region="$REGION" --format="value(spec.template.spec.containers[0].env)" 2>/dev/null)
  
  REQUIRED_VARS=("VERTEX_SEARCH_DATA_STORE_ID" "VERTEX_SEARCH_LOCATION" "RAG_DOCUMENTS_BUCKET" "CORE_DOCS_BUCKET")
  for var in "${REQUIRED_VARS[@]}"; do
    if echo "$ENV_VARS" | grep -q "$var"; then
      check "Переменная установлена: $var"
    else
      warn "Переменная не установлена: $var"
    fi
  done
else
  warn "Cloud Run сервис не найден: $SERVICE (проверка переменных окружения пропущена)"
fi

echo ""
echo "8. Проверка Firestore Security Rules..."
if [ -f "backend/firestore.rules" ]; then
  if grep -q "etigram\|horizon" backend/firestore.rules; then
    check "Правила для Etigram найдены в firestore.rules"
  else
    warn "Правила для Etigram не найдены в firestore.rules (добавьте правила для коллекций horizon_*)"
  fi
else
  warn "Файл firestore.rules не найден"
fi

echo ""
echo "9. Проверка Firestore индексов..."
if [ -f "web/firestore.indexes.json" ]; then
  if grep -q "horizon_fragments\|horizon_lenses\|horizon_dependency_edges\|horizon_frames\|horizon_watchdog_configs" web/firestore.indexes.json; then
    check "Индексы для Etigram найдены в firestore.indexes.json"
    echo "   Разверните индексы: firebase deploy --only firestore:indexes"
  else
    warn "Индексы для Etigram не найдены в firestore.indexes.json"
  fi
else
  warn "Файл firestore.indexes.json не найден"
fi

echo ""
echo "=== Итоги проверки ==="
echo -e "${GREEN}Успешно: $(( $(echo "$BUCKETS" | wc -l) + 1 ))${NC}"
if [ $WARNINGS -gt 0 ]; then
  echo -e "${YELLOW}Предупреждений: $WARNINGS${NC}"
fi
if [ $ERRORS -gt 0 ]; then
  echo -e "${RED}Ошибок: $ERRORS${NC}"
  exit 1
fi

echo ""
echo "Все проверки пройдены успешно!"
