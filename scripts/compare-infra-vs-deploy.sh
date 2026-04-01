#!/usr/bin/env bash
# Сравнение базовых настроек GCP с тем, что создаёт deploy.
# Deploy поднимает инфру через --check-infra или полный deploy (web+api).
# GCS population / import scripts — не входят в базовую инфру.
#
# Output: ресурсы, которые ЕСТЬ в GCP, но deploy их НЕ создаёт.

set -e
REGION=${REGION:-us-central1}
PROJECT_ID=$(gcloud config get-value project 2>/dev/null || true)
[[ -z "$PROJECT_ID" || "$PROJECT_ID" == "(unset)" ]] && { echo "Set GCP project: gcloud config set project <ID>"; exit 1; }

echo "=== Базовые настройки инфра: есть в GCP, deploy не создаёт ==="
echo "Project: $PROJECT_ID (deploy: --check-infra или полный deploy)"
echo ""

# --- Pub/Sub Topics: deploy creates these ---
DEPLOY_TOPICS=(
  discovery.status discovery.profile discovery.answer discovery.project
  document.process ground.refresh source.poll source.poll.due chat.ontology.process
  genai.stream genai.stream.error genai.stream.tool_call genai.stream.tool_result
  artifact.generation.started artifact.section.generating artifact.section.chunk artifact.section.completed
  artifact.assembly.started artifact.assembly.chunk artifact.assembly.completed
  artifact.generation.completed artifact.generation.error
  analysis.status agent.update
  interaction.created interaction.updated verified.stream
  nv_horizon.genai.streams nv_horizon.discovery.events nv_horizon.artifact.events
  nv_horizon.interaction.events nv_horizon.analysis.events nv_horizon.verified.streams
  nv_horizon.horizon.events nv_horizon.operations.events source.poll
)

echo "--- Pub/Sub Topics (есть в GCP, нет в deploy) ---"
INFRA_TOPICS=$(gcloud pubsub topics list --format='value(name)' --project="$PROJECT_ID" 2>/dev/null | sed 's|projects/.*/topics/||' || true)
for t in $INFRA_TOPICS; do
  found=0
  for d in "${DEPLOY_TOPICS[@]}"; do
    [[ "$t" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $t"
done
[[ -z "$INFRA_TOPICS" ]] && echo "  (нет топиков или ошибка доступа)"

# --- Pub/Sub Subscriptions ---
# Deploy создаёт: nv-horizon-ws (agg_topics), worker subs.
DEPLOY_SUBS=(
  "nv_horizon.genai.streams-ws-broker" "nv_horizon.discovery.events-ws-broker" "nv_horizon.artifact.events-ws-broker"
  "nv_horizon.interaction.events-ws-broker" "nv_horizon.analysis.events-ws-broker" "nv_horizon.verified.streams-ws-broker"
  "nv_horizon.horizon.events-ws-broker" "nv_horizon.operations.events-ws-broker" "source.poll-ws-broker"
  "source.poll.worker" "source.poll.due.worker" "ground.refresh.worker"
  "nv_horizon.operations.events.worker" "nv_horizon.artifact.events.document-processor.worker"
  "chat.ontology.process.worker"
)

echo ""
echo "--- Pub/Sub Subscriptions (есть в GCP, нет в deploy) ---"
INFRA_SUBS=$(gcloud pubsub subscriptions list --format='value(name)' --project="$PROJECT_ID" 2>/dev/null | sed 's|projects/.*/subscriptions/||' || true)
for s in $INFRA_SUBS; do
  found=0
  for d in "${DEPLOY_SUBS[@]}"; do
    [[ "$s" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $s"
done

# --- Secrets: deploy creates (placeholders or from secrets.env via apply-secrets) ---
DEPLOY_SECRETS=(
  gemini_api_key openai_api_key anthropic_api_key deepseek_api_key grok_api_key tavily_api_key
  vertex_api_key admin_job_token
  ANTHROPIC_API_KEY ANTHROPIC_API_KEY_TEST DEEPSEEK_API_KEY DEEPSEEK_API_KEY_TEST
  GROK_API_KEY GROK_API_KEY_TEST OPENAI_API_KEY_TEST VERTEX_API_KEY VERTEX_API_KEY_TEST
  TELEGRAM_API_ID TELEGRAM_API_HASH TELEGRAM_BOT_TOKEN TELEGRAM_SESSION_STRING
  paypal_client_id paypal_secret paypal_webhook_id google-custom-search-api-key
  GEONAMES_USERNAME functions-key
  ACLED_ACCESS_TOKEN NASA_FIRMS_API_KEY FRED_API_KEY EIA_API_KEY FINNHUB_API_KEY
  CLOUDFLARE_API_TOKEN OPENSKY_USERNAME OPENSKY_PASSWORD
)

echo ""
echo "--- Secrets (есть в GCP, нет в deploy) ---"
INFRA_SECRETS=$(gcloud secrets list --format='value(name)' --project="$PROJECT_ID" 2>/dev/null || true)
for s in $INFRA_SECRETS; do
  found=0
  for d in "${DEPLOY_SECRETS[@]}"; do
    [[ "$s" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $s"
done

# --- Cloud Tasks Queues: deploy creates ---
DEPLOY_QUEUES=(
  nv-horizon-scout nv-horizon-profile-refresh nv-horizon-risk
)

echo ""
echo "--- Cloud Tasks Queues (есть в GCP, нет в deploy) ---"
INFRA_QUEUES=$(gcloud tasks queues list --location="$REGION" --format='value(name)' --project="$PROJECT_ID" 2>/dev/null || true)
for q in $INFRA_QUEUES; do
  found=0
  for d in "${DEPLOY_QUEUES[@]}"; do
    [[ "$q" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $q"
done

# --- GCS Buckets: deploy creates (базовые; population scripts — отдельно) ---
DEPLOY_BUCKETS=(
  "${PROJECT_ID}-core-docs"
  "${PROJECT_ID}-rag-documents"
  "${PROJECT_ID}-nv-horizon-fragments"
  "${PROJECT_ID}-web-static"  # only if CREATE_WEB_STATIC_BUCKET=1
)

echo ""
echo "--- GCS Buckets (есть в GCP, нет в deploy) ---"
echo "  (исключены системные: cloudbuild, run-sources, *_import_*)"
INFRA_BUCKETS=$(gcloud storage buckets list --format='value(name)' --project="$PROJECT_ID" 2>/dev/null | sed 's|gs://||' || true)
for b in $INFRA_BUCKETS; do
  # Пропустить системные бакеты (создаёт GCP, не deploy)
  [[ "$b" == *"cloudbuild"* ]] && continue
  [[ "$b" == *"run-sources"* ]] && continue
  [[ "$b" == *"_import_"* ]] && continue
  found=0
  for d in "${DEPLOY_BUCKETS[@]}"; do
    [[ "$b" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $b"
done

# --- Cloud Scheduler Jobs: deploy creates ---
DEPLOY_JOBS=(
  nv-horizon-source-poller nv-horizon-artifact-scheduler subscription-billing-daily
  nv-horizon-watchdog  # only with --nv-horizon-watchdog
)

echo ""
echo "--- Cloud Scheduler Jobs (есть в GCP, нет в deploy) ---"
INFRA_JOBS=$(gcloud scheduler jobs list --location="$REGION" --format='value(name)' --project="$PROJECT_ID" 2>/dev/null | sed 's|projects/.*/locations/.*/jobs/||' || true)
for j in $INFRA_JOBS; do
  found=0
  for d in "${DEPLOY_JOBS[@]}"; do
    [[ "$j" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $j"
done

# --- Cloud Run Services: deploy creates ---
DEPLOY_SERVICES=(
  nv-horizon-api nv-horizon-ws verified-proxy nv-horizon-telegram
)

echo ""
echo "--- Cloud Run Services (есть в GCP, нет в deploy) ---"
INFRA_SERVICES=$(gcloud run services list --region="$REGION" --format='value(metadata.name)' --project="$PROJECT_ID" 2>/dev/null || true)
for svc in $INFRA_SERVICES; do
  found=0
  for d in "${DEPLOY_SERVICES[@]}"; do
    [[ "$svc" == "$d" ]] && { found=1; break; }
  done
  [[ $found -eq 0 ]] && echo "  $svc"
done

# --- Service Accounts (deploy создаёт backend@) ---
echo ""
echo "--- Service Accounts (есть в GCP, deploy не создаёт) ---"
echo "  (исключены: backend, firebase-adminsdk, gcp-sa-*, default compute)"
INFRA_SA=$(gcloud iam service-accounts list --format='value(email)' --project="$PROJECT_ID" 2>/dev/null || true)
for sa in $INFRA_SA; do
  [[ "$sa" != *"@${PROJECT_ID}.iam.gserviceaccount.com" ]] && continue
  [[ "$sa" == "backend@${PROJECT_ID}.iam.gserviceaccount.com" ]] && continue
  [[ "$sa" == *"firebase-adminsdk"* ]] && continue
  [[ "$sa" == *"gcp-sa-"* ]] && continue
  [[ "$sa" == *"@appspot.gserviceaccount.com" ]] && continue
  echo "  $sa"
done

# --- Firebase (deploy: hosting + firestore:indexes; НЕ rules) ---
echo ""
echo "--- Firebase (есть в проекте, deploy не разворачивает) ---"
ROOT=$(cd "$(dirname "$0")/.." && pwd)
if [[ -f "$ROOT/backend/firestore.rules" ]] && ! grep -q '"rules"' "$ROOT/web/firebase.json" 2>/dev/null; then
  echo "  Firestore Security Rules: deploy НЕ разворачивает"
  echo "    (backend/firestore.rules есть, firebase.json не ссылается, deploy не вызывает firebase deploy --only firestore:rules)"
fi

echo ""
echo "=== Конец проверки ==="
