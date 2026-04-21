# Настройка инфраструктуры

Этот документ описывает скрипты для настройки инфраструктуры проекта.

## GCP проект

**Проект по умолчанию:** `prevalent-horizon-test` (имя: prevalent-horizon-test)

Перед развёртыванием убедитесь, что gcloud настроен на нужный проект:
```bash
gcloud config set project prevalent-horizon-test
```

## Скрипты

### 1. `scripts/deploy` - Основной скрипт развертывания

Скрипт автоматически создает и настраивает всю необходимую инфраструктуру:

- **Pub/Sub топики** (33 топика):
  - Discovery топики: `discovery.status`, `discovery.profile`, `discovery.answer`, `discovery.project`
  - Document топики: `document.process`, `ground.refresh`
  - Source топики: `source.poll`, `source.poll.due`
  - GenAI топики: `genai.stream`, `genai.stream.error`, `genai.stream.tool_call`, `genai.stream.tool_result`
  - Artifact топики: `artifact.generation.*`, `artifact.section.*`, `artifact.assembly.*`
  - Analysis топики: `analysis.status`
  - Agent топики: `agent.update`
  - Interaction топики: `interaction.created`, `interaction.updated`
  - Verified топики: `verified.stream`
  - Aggregated топики: `prevalent-horizon-test.*` (9 топиков)

- **Pub/Sub подписки** (11 подписок):
  - Подписки для ws (aggregated топиков): `<topic>-ws-broker` (9 подписок)

- **Cloud Scheduler Jobs** (4 jobs):
  - `prevalent-horizon-test-source-poller` - опрос источников каждые 5 минут
  - `prevalent-horizon-test-artifact-scheduler` - проверка запланированных обновлений артефактов каждый час
  - `subscription-billing-daily` - ежедневный биллинг подписок в 02:00 UTC
  - `prevalent-horizon-test-watchdog` - опциональный watchdog для Horizon (Nordic Vision) Horizon (только с флагом `--prevalent-horizon-test-watchdog`)

### 2. `scripts/create-secrets.sh` - Создание секретов

Скрипт для создания всех необходимых секретов в GCP Secret Manager.

**Использование:**
```bash
# 1. Отредактируйте скрипт и замените все PLACEHOLDER_* значения на реальные
vim scripts/create-secrets.sh

# 2. Запустите скрипт
./scripts/create-secrets.sh [PROJECT_ID]
```

**Секреты, которые создаются:**

- API ключи (UPPERCASE и lowercase версии):
  - `ANTHROPIC_API_KEY`, `anthropic_api_key`
  - `DEEPSEEK_API_KEY`, `deepseek_api_key`
  - `GROK_API_KEY`, `grok_api_key`
  - `OPENAI_API_KEY_TEST`, `openai_api_key`
  - `VERTEX_API_KEY`, `VERTEX_API_KEY_TEST`, `vertex_api_key`
  - `gemini_api_key`
  - `tavily_api_key`

- Telegram секреты:
  - `TELEGRAM_API_ID`
  - `TELEGRAM_API_HASH`
  - `TELEGRAM_BOT_TOKEN`
  - `TELEGRAM_SESSION_STRING`

- PayPal секреты:
  - `paypal_client_id`
  - `paypal_secret`
  - `paypal_webhook_id`

- Google сервисы:
  - `google-custom-search-api-key`
  - `google-custom-search-cx`
  - `google_maps_api_key` — для Geocoding API (геокодирование адресов org/facility)
  - `open_corporates_api_token` — для OpenCorporates API (обогащение org registry IDs; без токена API возвращает 401)

- Другие:
  - `GEONAMES_USERNAME`
  - `admin_job_token`
  - `functions-key`

**Важно:** Скрипт идемпотентен - можно запускать многократно, он обновит существующие секреты новыми версиями.

## Порядок развертывания

1. **Создание секретов** (один раз):
   ```bash
   ./scripts/create-secrets.sh
   ```

2. **Развертывание инфраструктуры и сервисов**:
   ```bash
   ./scripts/deploy
   ```

   Или частично:
   ```bash
   ./scripts/deploy --api    # только backend
   ./scripts/deploy --web    # только frontend
   ./scripts/deploy --check-infra  # только проверка инфраструктуры
   ```

## Проверка инфраструктуры

Для проверки соответствия инфраструктуры в GCP и скрипте deploy используйте:

```bash
python3 /tmp/check_infrastructure.py
```

Скрипт покажет:
- Какие топики/подписки/jobs есть в GCP, но отсутствуют в скрипте
- Какие топики/подписки/jobs есть в скрипте, но отсутствуют в GCP
- Статистику по всем элементам

## Дополнительные скрипты

- `scripts/setup-interaction-topics.sh` - создание топиков для interaction событий (интегрировано в deploy)
- `scripts/setup-horizon-pubsub.sh` - создание топиков для Horizon (интегрировано в deploy)
- `scripts/create-vertex-search-datastore.sh` - создание Vertex AI Search Data Store (rag-documents-store)
- `scripts/ensure-vertex-ai-sa-bucket-access.sh` - выдача Vertex AI SA доступа к RAG bucket (если deploy не смог — SA создаётся при первом использовании Vertex AI)

## Примечания

- Большинство подписок создаются автоматически при использовании топиков
- Worker подписки (например, `chat.ontology.process.worker`) создаются отдельно в коде
- Секреты обычно создаются вручную перед первым развертыванием
- Все скрипты идемпотентны - можно запускать многократно без побочных эффектов
