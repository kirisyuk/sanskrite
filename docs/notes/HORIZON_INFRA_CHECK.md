# Отчет о проверке инфраструктуры GCP для Horizon

**Дата проверки:** $(date)  
**Проект:** sanskrite

## Результаты проверки

### ✅ Настроено и работает

1. **Firestore**
   - ✓ Коллекции будут созданы автоматически при первом использовании
   - ✓ Индексы определены в `web/firestore.indexes.json`
   - ✓ Security Rules добавлены в `backend/firestore.rules`

2. **GCS Buckets**
   - ✓ `sanskrite-core-docs` - существует
   - ✓ `nv-horizon-rag-documents` - существует

3. **Vertex AI Search**
   - ✓ Discovery Engine API включен
   - ✓ Data Store `rag-documents-store` - существует (проверено через Discovery Engine REST API)

4. **Pub/Sub**
   - ✓ Топик `source.poll` - существует
   - ✓ Топик `nv_horizon.operations.events` - существует
   - ✓ Топик `nv_horizon.interaction.events` - существует
   - ✓ Остальные агрегированные топики создаются автоматически

5. **Secret Manager**
   - ✓ `vertex_api_key` - существует
   - ✓ `gemini_api_key` - существует
   - ✓ `openai_api_key` - существует
   - ✓ `anthropic_api_key` - существует

6. **IAM роли для Service Account**
   - ✓ Service Account: `backend@sanskrite.iam.gserviceaccount.com`
   - ✓ `roles/datastore.user`
   - ✓ `roles/secretmanager.secretAccessor`
   - ✓ `roles/pubsub.admin`
   - ✓ `roles/storage.objectAdmin`
   - ✓ `roles/discoveryengine.editor`

7. **Переменные окружения Cloud Run**
   - ✓ `VERTEX_SEARCH_DATA_STORE_ID` = `rag-documents-store`
   - ✓ `VERTEX_SEARCH_LOCATION` = `global`
   - ✓ `RAG_DOCUMENTS_BUCKET` = `nv-horizon-rag-documents`
   - ✓ `CORE_DOCS_BUCKET` = `sanskrite-core-docs`

8. **Firestore Security Rules**
   - ✓ Правила для Horizon коллекций добавлены

9. **Firestore индексы**
   - ✓ Индексы определены в `firestore.indexes.json`
   - ⚠️ Нужно развернуть: `firebase deploy --only firestore:indexes`

## Что нужно сделать

### 1. Развернуть Firestore индексы

```bash
cd backend
firebase deploy --only firestore:indexes
```

Или через gcloud:
```bash
gcloud firestore indexes create --project=nv-horizon
```

### 3. Настроить Watchdog (рекомендация)

См. `scripts/setup-horizon-watchdog.md` для деталей.

**Рекомендация:** Cloud Functions 2nd Gen + Cloud Scheduler

## Скрипты для проверки и настройки

1. **`scripts/check-nv-horizon-infra.sh`** - полная проверка инфраструктуры
2. **`scripts/setup-nv-horizon-pubsub.sh`** - настройка Pub/Sub топиков
3. **`scripts/create-vertex-search-datastore.sh`** - создание Data Store
4. **`scripts/setup-horizon-watchdog.md`** - рекомендации по Watchdog

## Итог

**Статус:** Почти готово! Осталось только:
1. Развернуть Firestore индексы
2. Настроить Watchdog (опционально, можно отложить)

Все остальные компоненты настроены и готовы к работе.
