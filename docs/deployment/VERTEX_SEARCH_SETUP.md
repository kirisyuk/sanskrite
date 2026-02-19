# Vertex AI Search: Настройка для деплоя

## Что проверяет скрипт `check-vertex-search-infra.sh`

### ✅ Проверки:

1. **Переменные окружения** (ожидаемые в Cloud Run):
   - `VERTEX_SEARCH_DATA_STORE_ID` - должен быть `rag-documents-store`
   - `VERTEX_SEARCH_LOCATION` - должен быть `global`
   - `GEMINI_API_KEY` - обязателен
   - `RAG_DOCUMENTS_BUCKET` - должен быть `h1horizon-rag-documents`
   - `GCP_PROJECT_ID` - обязателен

2. **Секреты в Secret Manager**:
   - `gemini_api_key` - должен существовать и иметь значение

3. **Data Store**:
   - Существование Data Store `rag-documents-store`
   - Количество проиндексированных документов

4. **GCS Bucket**:
   - Существование bucket `h1horizon-rag-documents`
   - Количество .txt файлов

5. **API**:
   - `discoveryengine.googleapis.com` - должен быть включен
   - `secretmanager.googleapis.com` - должен быть включен
   - `storage.googleapis.com` - должен быть включен

6. **IAM Permissions**:
   - `roles/secretmanager.secretAccessor` для backend SA
   - `roles/storage.objectAdmin` для backend SA
   - `roles/discoveryengine.editor` для backend SA (рекомендуется)

7. **Cloud Run Configuration**:
   - Существование сервиса `h1horizon-api`
   - Наличие `VERTEX_SEARCH_DATA_STORE_ID` в env vars
   - Наличие `RAG_DOCUMENTS_BUCKET` в env vars
   - Binding секрета `GEMINI_API_KEY`

## Что нужно исправить

### ⚠️ Найденные проблемы:

1. **VERTEX_SEARCH_DATA_STORE_ID не установлен в Cloud Run**
   - Решение: Добавить в deploy скрипт или установить вручную

2. **GEMINI_API_KEY secret не привязан к Cloud Run**
   - Решение: Добавить в `--set-secrets` при деплое

3. **roles/discoveryengine.editor не выдан**
   - Решение: Выдать роль для импорта документов

4. **Документы не проиндексированы**
   - Решение: Запустить импорт после деплоя

## Исправления в deploy скрипте

### 1. Добавить VERTEX_SEARCH_DATA_STORE_ID в env vars

В `scripts/deploy` функция `deploy_api()` должна включать:

```bash
local env_vars="...,VERTEX_SEARCH_DATA_STORE_ID=rag-documents-store,..."
```

Или устанавливать динамически после проверки Data Store.

### 2. Убедиться что GEMINI_API_KEY в secrets

Уже есть в строке 842:
```bash
local paypal_secrets="GEMINI_API_KEY=gemini_api_key:latest,..."
```

### 3. Выдать роль discoveryengine.editor

В `scripts/deploy` функция `setup_infrastructure()` уже выдает роли (строки 343-350), но нужно проверить что `roles/discoveryengine.editor` выдан.

## Ручная настройка (если нужно)

### 1. Установить VERTEX_SEARCH_DATA_STORE_ID:

```bash
gcloud run services update h1horizon-api \
  --region=us-central1 \
  --update-env-vars="VERTEX_SEARCH_DATA_STORE_ID=rag-documents-store" \
  --project=h1horizon
```

### 2. Выдать роль discoveryengine.editor:

```bash
gcloud projects add-iam-policy-binding sanskrite \
  --member="serviceAccount:backend@sanskrite.iam.gserviceaccount.com" \
  --role="roles/discoveryengine.editor"
```

### 3. Проверить что все работает:

```bash
./scripts/check-vertex-search-infra.sh -p sanskrite
```
