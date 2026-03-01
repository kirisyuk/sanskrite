# Deployment Checklist & Best Practices

Этот документ содержит чеклист и best practices для деплоя на GCP, основанный на реальных проблемах, с которыми мы столкнулись.

## 🚀 Pre-Deployment Checklist

### 1. Infrastructure Setup (Выполнить ОДИН раз для нового проекта)

```bash
# ✓ Убедиться что выбран правильный проект
gcloud config get-value project

# ✓ Создать service account для backend (если не существует)
gcloud iam service-accounts create backend --display-name "Sanskrite Backend"

# ✓ Дать ВСЕ необходимые роли СРАЗУ
SA="backend@$(gcloud config get-value project).iam.gserviceaccount.com"
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:$SA" --role=roles/datastore.user
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:$SA" --role=roles/secretmanager.secretAccessor
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:$SA" --role=roles/pubsub.admin
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:$SA" --role=roles/logging.logWriter

# ✓ Создать секреты (lowercase с underscore!)
echo "YOUR_VERTEX_KEY" | gcloud secrets create vertex_api_key --data-file=-
echo "YOUR_OPENAI_KEY" | gcloud secrets create openai_api_key --data-file=-
echo "YOUR_ANTHROPIC_KEY" | gcloud secrets create anthropic_api_key --data-file=-
echo "YOUR_GROK_KEY" | gcloud secrets create grok_api_key --data-file=-

# ✓ Создать Pub/Sub топики
gcloud pubsub topics create discovery.status
gcloud pubsub topics create discovery.profile
gcloud pubsub topics create discovery.answer
gcloud pubsub topics create discovery.project
# Interaction events (for tool usage - graphs, etc.)
gcloud pubsub topics create interaction.created
gcloud pubsub topics create interaction.updated
# Or use the setup script:
./scripts/setup-interaction-topics.sh
```

### 2. Pre-Deployment Validation

```bash
# ✓ Go код компилируется локально
cd backend && go build ./cmd/server

# ✓ TypeScript без ошибок типов
cd web && npx tsc --noEmit

# ✓ Если изменились backend модели - обновить сгенерированные типы
cd web
npm run gen:schemas && npm run gen:types
git add web/src/types/
git commit -m "Update generated types from backend models"

# ✓ Frontend собирается успешно
npm run build

# ✓ Нет uncommitted изменений (опционально)
git status
```

---

## 🔧 Deployment Process

### Полный деплой (все сервисы)

```bash
# Windows (Git Bash или WSL)
wsl bash scripts/deploy

# Linux/Mac
bash scripts/deploy
```

### Частичный деплой

```bash
# Только backend
bash scripts/deploy --api

# Только frontend
bash scripts/deploy --web

# Только Telegram сервис
bash scripts/deploy --py
```

---

## ✅ Post-Deployment Verification

### 1. Проверить что трафик на новой ревизии

```bash
# Backend
gcloud run services describe etigram-api --region us-central1 \
  --format="value(status.traffic[0].revisionName)"

# Если не последняя - принудительно переключить
gcloud run services update-traffic etigram-api --region us-central1 --to-latest
```

### 2. Проверить логи на ошибки

```bash
# Последние 50 строк логов backend
gcloud run services logs read etigram-api --region us-central1 --limit 50

# Следить за логами в реальном времени
gcloud run services logs tail etigram-api --region us-central1
```

### 3. Health Check

```bash
# Backend API
API_URL=$(gcloud run services describe etigram-api --region us-central1 --format='value(status.url)')
curl $API_URL/health

# Telegram Service
TG_URL=$(gcloud run services describe etigram-telegram --region us-central1 --format='value(status.url)')
curl $TG_URL/health
```

---

## 🔴 Типовые Проблемы и Решения

### Проблема 1: Permission Denied на Secret Manager
**Симптом**: `Permission denied on secret "vertex_api_key"`

**Причина**: Service account не имеет роли `secretmanager.secretAccessor`

**Решение**: 
```bash
SA="backend@$(gcloud config get-value project).iam.gserviceaccount.com"
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:$SA" \
  --role=roles/secretmanager.secretAccessor
```

---

### Проблема 2: Pub/Sub Topics Not Found
**Симптом**: `rpc error: code = NotFound desc = Resource not found`

**Причина**: Топики не созданы до запуска приложения

**Решение**: Создать топики заранее (см. Infrastructure Setup выше)

---

### Проблема 3: Secret Not Found или Invalid Name
**Симптом**: `Secret "GEMINI_API_KEY" was not found`

**Причина**: Секреты в GCP имеют lowercase имена, но в --set-secrets используется uppercase

**Решение**: 
- В GCP Secret Manager: `gemini_api_key` (lowercase_underscore)
- В Cloud Run binding: `GEMINI_API_KEY=gemini_api_key:latest`

---

### Проблема 4: Environment Variable GCP_PROJECT_ID теряется
**Симптом**: Backend логи показывают "projectID was empty"

**Причина**: `gcloud run services update` НЕ сохраняет старые env vars автоматически

**Решение**: При deploy **ВСЕГДА** передавать ВСЕ env vars:
```bash
--set-env-vars "GCP_PROJECT_ID=$PROJECT_ID,GOOGLE_CLOUD_PROJECT=$PROJECT_ID,OTHER_VAR=value"
```

---

### Проблема 5: Code Changes не применяются
**Симптом**: После deploy код ведет себя по-старому

**Причина**: 
1. Трафик не переключен на новую ревизию
2. Кэш браузера (для frontend)

**Решение**:
```bash
# 1. Принудительно переключить трафик
gcloud run services update-traffic etigram-api --region us-central1 --to-latest

# 2. Очистить кэш браузера или Hard Refresh (Ctrl+Shift+R)
```

---

### Проблема 6: PowerShell не понимает bash команды
**Симптом**: `The term '&&' is not recognized`

**Причина**: Windows PowerShell не поддерживает bash синтаксис

**Решение**: Использовать WSL или Git Bash:
```bash
wsl bash scripts/deploy
# или
"C:\Program Files\Git\bin\bash.exe" scripts/deploy
```

---

### Проблема 7: Firestore Index Creation Failed
**Симптом**: `already exists` или `invalid field-config`

**Причина**: Индекс уже создан или неправильный синтаксис

**Решение**: 
- Команда идемпотентная, ошибку можно игнорировать
- Синтаксис: `--field-config field-path=...,order=...` (НЕ `fieldPath`)

---

## 📋 Quick Reference: Naming Conventions

| Компонент | Формат | Пример |
|-----------|--------|--------|
| GCP Secrets | `lowercase_underscore` | `vertex_api_key` |
| Env Vars (код) | `UPPERCASE_UNDERSCORE` | `GEMINI_API_KEY` |
| Cloud Run Services | `lowercase-dash` | `etigram-api` |
| Pub/Sub Topics | `lowercase.dot` | `discovery.status` |
| Firestore Collections | `lowercase_underscore` | `core_projects` |
| Service Accounts | `name@project.iam.gserviceaccount.com` | `backend@sanskrite.iam.gserviceaccount.com` |

---

## 🎯 Best Practices

### 1. Infrastructure as Code
- **Всегда** создавать инфраструктуру ПЕРЕД деплоем приложения
- Использовать идемпотентные команды (можно запускать многократно)
- Проверять существование ресурсов перед созданием

### 2. Secrets Management
- **Никогда** не коммитить секреты в git
- Хранить в GCP Secret Manager
- Использовать latest version для автоматических обновлений

### 3. Environment Variables
- **Всегда** передавать `GCP_PROJECT_ID` и `GOOGLE_CLOUD_PROJECT` вместе
- При update сервиса - передавать ВСЕ env vars, не только новые
- Использовать `--set-env-vars` (а не `--update-env-vars`) для полного контроля

### 4. Traffic Management
- После deploy **всегда** проверять на какой ревизии трафик
- По умолчанию использовать `--to-latest` для instant rollout
- Для blue-green deployment - использовать `--set-traffic`

### 5. Local Validation
- **Всегда** компилировать Go код локально перед deploy
- Проверять TypeScript типы перед frontend deploy
- Использовать линтеры

### 6. Monitoring
- Проверять логи после КАЖДОГО deploy
- Настроить алерты на ошибки в Cloud Logging
- Использовать Cloud Trace для latency

---

## 🛠 Troubleshooting Commands

```bash
# Проверить статус сервиса
gcloud run services describe etigram-api --region us-central1

# Посмотреть все ревизии
gcloud run revisions list --service etigram-api --region us-central1

# Посмотреть env vars текущего сервиса
gcloud run services describe etigram-api --region us-central1 \
  --format="value(spec.template.spec.containers[0].env)"

# Посмотреть привязанные секреты
gcloud run services describe etigram-api --region us-central1 \
  --format="value(spec.template.spec.containers[0].env)"

# Проверить IAM роли service account
gcloud projects get-iam-policy $(gcloud config get-value project) \
  --flatten="bindings[].members" \
  --filter="bindings.members:serviceAccount:backend@*"

# Список всех секретов
gcloud secrets list

# Список всех Pub/Sub топиков
gcloud pubsub topics list
```

---

## ⏱ Expected Timings

- **Infrastructure setup** (первый раз): ~2-3 минуты
- **Backend deploy** (с build): ~3-5 минут
- **Frontend deploy** (с build): ~2-3 минуты
- **Python service deploy**: ~2-3 минуты
- **Firestore index creation**: ~2-5 минут (асинхронно)

**Цель**: Полный деплой < 10 минут (после первого setup)

---

## 📞 Emergency Rollback

Если после deploy всё сломалось:

```bash
# 1. Посмотреть предыдущую ревизию
gcloud run revisions list --service etigram-api --region us-central1 --limit 5

# 2. Переключить трафик на предыдущую (например, etigram-api-00006)
gcloud run services update-traffic etigram-api \
  --region us-central1 \
  --to-revisions etigram-api-00006=100

# 3. Проверить что откатились
curl $(gcloud run services describe etigram-api --region us-central1 --format='value(status.url)')/health
```

---

**Последнее обновление**: 2025-10-11

