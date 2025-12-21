#!/bin/bash
# Скрипт настройки Pub/Sub топиков для Horizon

set -e

PROJECT_ID=$(gcloud config get-value project)
if [ -z "$PROJECT_ID" ]; then
  echo "ERROR: GCP project not set. Run: gcloud config set project PROJECT_ID"
  exit 1
fi

echo "=== Настройка Pub/Sub топиков для Horizon ==="
echo "Проект: $PROJECT_ID"
echo ""

# Агрегированные топики (используются системой realtime)
TOPICS=(
  "hatlone.genai.streams"
  "hatlone.discovery.events"
  "hatlone.artifact.events"
  "hatlone.interaction.events"
  "hatlone.analysis.events"
  "hatlone.verified.streams"
  "hatlone.operations.events"
  "source.poll"
)

for topic in "${TOPICS[@]}"; do
  echo "Проверка топика: $topic"
  if gcloud pubsub topics describe "$topic" --format="value(name)" 2>/dev/null | grep -q "$topic"; then
    echo "  ✓ Топик уже существует"
  else
    echo "  → Создание топика..."
    gcloud pubsub topics create "$topic" --message-ordering || echo "  ⚠ Ошибка создания (возможно уже существует)"
    echo "  ✓ Топик создан"
  fi
done

echo ""
echo "=== Настройка подписок (опционально) ==="
echo "Подписки создаются автоматически при использовании Subscriber"
echo ""

echo "Готово!"



