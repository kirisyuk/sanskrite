#!/bin/bash
# Скрипт настройки Pub/Sub топиков для Horizon (Nordic Vision)

set -e

PROJECT_ID=$(gcloud config get-value project)
if [ -z "$PROJECT_ID" ]; then
  echo "ERROR: GCP project not set. Run: gcloud config set project PROJECT_ID"
  exit 1
fi

echo "=== Настройка Pub/Sub топиков для Horizon (Nordic Vision) ==="
echo "Проект: $PROJECT_ID"
echo ""

# Service account that publishes/subscribes (API + ws usually share it)
SA="${PUBSUB_SERVICE_ACCOUNT:-backend@${PROJECT_ID}.iam.gserviceaccount.com}"

# Агрегированные топики (используются системой realtime / ws broker)
TOPICS=(
  "nv_horizon.genai.streams"
  "nv_horizon.discovery.events"
  "nv_horizon.artifact.events"
  "nv_horizon.interaction.events"
  "nv_horizon.analysis.events"
  "nv_horizon.verified.streams"
  "nv_horizon.horizon.events"
  "nv_horizon.operations.events"
  "source.poll"
)

for topic in "${TOPICS[@]}"; do
  echo "Проверка топика: $topic"
  if gcloud pubsub topics describe "$topic" --project="$PROJECT_ID" --format="value(name)" 2>/dev/null | grep -q "$topic"; then
    echo "  ✓ Топик уже существует"
  else
    echo "  → Создание топика..."
    gcloud pubsub topics create "$topic" --project="$PROJECT_ID" --message-ordering || echo "  ⚠ Ошибка создания (возможно уже существует)"
    echo "  ✓ Топик создан"
  fi

  # Ensure publisher permissions for the service account (best-effort; may already be granted at project-level)
  gcloud pubsub topics add-iam-policy-binding "$topic" \
    --project="$PROJECT_ID" \
    --member="serviceAccount:${SA}" \
    --role="roles/pubsub.publisher" \
    --quiet >/dev/null 2>&1 || true
done

echo ""
echo "=== Настройка подписок для ws ==="
echo "Subscriber создает подписки автоматически, но для надежности фиксируем их здесь (idempotent)."
echo ""

for topic in "${TOPICS[@]}"; do
  sub_id="${topic}-ws-broker"
  echo "Проверка подписки: $sub_id"

  if gcloud pubsub subscriptions describe "$sub_id" --project="$PROJECT_ID" --format="value(name)" 2>/dev/null | grep -q "$sub_id"; then
    echo "  ✓ Подписка уже существует"
  else
    echo "  → Создание подписки..."
    gcloud pubsub subscriptions create "$sub_id" \
      --project="$PROJECT_ID" \
      --topic="$topic" \
      --enable-message-ordering \
      --quiet || echo "  ⚠ Ошибка создания (возможно уже существует)"
    echo "  ✓ Подписка создана"
  fi

  # Ensure subscriber permissions for the service account (best-effort; may already be granted at project-level)
  gcloud pubsub subscriptions add-iam-policy-binding "$sub_id" \
    --project="$PROJECT_ID" \
    --member="serviceAccount:${SA}" \
    --role="roles/pubsub.subscriber" \
    --quiet >/dev/null 2>&1 || true
done

echo "Готово!"


