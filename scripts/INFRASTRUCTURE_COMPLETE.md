# Полная инфраструктура - что создается в скрипте deploy

## Резюме

Скрипт `scripts/deploy` теперь создает все необходимые элементы инфраструктуры для проекта.

## Создаваемые ресурсы

### 1. Cloud Run Services (4 сервиса)
- `api` - основной backend API
- `ws` - WebSocket broker для realtime событий
- `telegram` - Telegram сервис
- `verified-proxy` - прокси для verified URL анализа

### 2. Pub/Sub Topics (33 топика)

**Основные топики:**
- Discovery: `discovery.status`, `discovery.profile`, `discovery.answer`, `discovery.project`
- Document: `document.process`, `ground.refresh`
- Source: `source.poll`, `source.poll.due`
- GenAI: `genai.stream`, `genai.stream.error`, `genai.stream.tool_call`, `genai.stream.tool_result`
- Artifact: `artifact.generation.*`, `artifact.section.*`, `artifact.assembly.*`
- Analysis: `analysis.status`
- Agent: `agent.update`
- Interaction: `interaction.created`, `interaction.updated`
- Verified: `verified.stream`

**Aggregated топики (для ws):**
- `horizon.genai.streams`
- `horizon.discovery.events`
- `horizon.artifact.events`
- `horizon.interaction.events`
- `horizon.analysis.events`
- `horizon.verified.streams`
- `horizon.core.events`
- `horizon.operations.events`
- `source.poll`

### 3. Pub/Sub Subscriptions (14 подписок)
- Подписки для ws (aggregated топиков): `<topic>-ws-broker` (9 подписок)
- Worker подписки: `source.poll.worker`, `source.poll.due.worker`, `ground.refresh.worker`, и др.

### 4. Cloud Storage Buckets (3 основных)
- `prevalent-horizon-test-core-docs` - для хранения core документов
- `prevalent-horizon-test-rag-documents` - для хранения RAG документов
- `prevalent-horizon-test-fragments` - для хранения Horizon Horizon fragments (ДОБАВЛЕНО)
- `prevalent-horizon-test-web-static` - опционально (создается только если `CREATE_WEB_STATIC_BUCKET=1`)

**Примечание:** Другие buckets (для Vertex AI Search импорта, системные) создаются автоматически и не требуют явного создания.

### 5. Cloud Tasks Queues (3 очереди)
- `prevalent-horizon-test-scout` - для Scout jobs (maxConcurrentDispatches=50)
- `prevalent-horizon-test-profile-refresh` - для обновления профилей Horizon fragments (maxConcurrentDispatches=1000)
- `prevalent-horizon-test-risk` - для Risk jobs

### 6. Cloud Scheduler Jobs (4 jobs)
- `prevalent-horizon-test-source-poller` - опрос источников каждые 5 минут
- `prevalent-horizon-test-artifact-scheduler` - проверка запланированных обновлений артефактов каждый час
- `subscription-billing-daily` - ежедневный биллинг подписок в 02:00 UTC (ДОБАВЛЕНО)
- `prevalent-horizon-test-watchdog` - опциональный watchdog для Horizon (Nordic Vision) Horizon (только с флагом `--prevalent-horizon-test-watchdog`)

### 7. Redis Instance (1 instance)
- `prevalent-horizon-test-redis` - Memorystore Redis для кеширования

### 8. Service Accounts (1 основной)
- `backend@<PROJECT_ID>.iam.gserviceaccount.com` - основной service account для backend сервисов

**Примечание:** Системные service accounts (Compute Engine default, Firebase admin SDK) создаются автоматически.

### 9. Secrets (28 секретов)
Создаются через отдельный скрипт `scripts/create-secrets.sh`:
- API ключи (Anthropic, DeepSeek, Grok, OpenAI, Vertex, Gemini, Tavily)
- Telegram секреты
- PayPal секреты
- Google сервисы
- Другие (Geonames, admin_job_token, functions-key)

### 10. Firestore Indexes
Создаются через `firebase deploy --only firestore:indexes` или вручную через gcloud для векторных индексов.

### 11. Vertex AI Search Data Stores
Создаются в скрипте deploy для RAG документов и Horizon fragments.

### 12. BigQuery Datasets и Tables
Создаются в скрипте deploy для синхронизации core документов.

## Что было добавлено в последнем обновлении

1. ✅ **Cloud Tasks Queue `prevalent-horizon-test-profile-refresh`** - для обновления профилей Horizon fragments
2. ✅ **Cloud Scheduler Job `subscription-billing-daily`** - для ежедневного биллинга
3. ✅ **GCS Bucket `prevalent-horizon-test-fragments`** - для хранения Horizon Horizon fragments
4. ✅ **Pub/Sub топики** - `genai.stream.error`, `genai.stream.tool_call`, `genai.stream.tool_result`, `interaction.created`, `interaction.updated`
5. ✅ **Pub/Sub подписки** - для interaction топиков

## Элементы, которые создаются автоматически (не требуют добавления)

- Buckets для Vertex AI Search импорта (`1031480025965_*`)
- Системные buckets (`cloud-ai-platform-*`, `run-sources-*`, `horizon_cloudbuild`)
- Системные Service Accounts (Compute Engine default, Firebase admin SDK)
- VPC Network `default` (создается автоматически при первом использовании)
- Worker подписки Pub/Sub (создаются автоматически в коде)

## Использование

```bash
# Полное развертывание инфраструктуры и сервисов
./scripts/deploy

# Только инфраструктура (без деплоя сервисов)
./scripts/deploy --check-infra

# Создание секретов (отдельно)
./scripts/create-secrets.sh
```

## Проверка инфраструктуры

Для проверки соответствия инфраструктуры в GCP и скрипте deploy:

```bash
python3 /tmp/check_infrastructure.py  # Pub/Sub, Secrets, Scheduler
python3 /tmp/check_all_infrastructure.py  # Все ресурсы
```
