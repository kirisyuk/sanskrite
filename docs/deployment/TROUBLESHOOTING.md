# Troubleshooting Guide

Быстрая справка по диагностике и решению проблем при деплое и работе Sanskrite Discovery Platform.

---

## 🔍 Диагностические команды

### Проверить статус сервисов

```bash
# Backend API
gcloud run services describe h1horizon-api --region us-central1

# Telegram Service
gcloud run services describe h1horizon-telegram --region us-central1

# На какой ревизии трафик?
gcloud run services describe h1horizon-api --region us-central1 \
  --format="value(status.traffic[0].revisionName)"

# Список всех ревизий
gcloud run revisions list --service h1horizon-api --region us-central1
```

### Проверить логи

```bash
# Последние 50 строк
gcloud run services logs read h1horizon-api --region us-central1 --limit 50

# Follow в реальном времени
gcloud run services logs tail h1horizon-api --region us-central1

# Только ошибки
gcloud run services logs read h1horizon-api --region us-central1 --limit 50 --log-filter="severity>=ERROR"
```

### Проверить конфигурацию

```bash
# Env vars
gcloud run services describe h1horizon-api --region us-central1 \
  --format="value(spec.template.spec.containers[0].env)"

# Secrets binding
gcloud run services describe h1horizon-api --region us-central1 \
  --format="value(spec.template.spec.containers[0].env)" | grep -i secret

# IAM политики service account
SA="backend@$(gcloud config get-value project).iam.gserviceaccount.com"
gcloud projects get-iam-policy $(gcloud config get-value project) \
  --flatten="bindings[].members" \
  --filter="bindings.members:serviceAccount:$SA"
```

### Проверить инфраструктуру

```bash
# Pub/Sub топики
gcloud pubsub topics list

# Секреты
gcloud secrets list

# Firestore indexes
gcloud firestore indexes composite list
```

---

## 🐛 Типовые проблемы и решения

### 1. Backend не стартует: "Permission denied on secret"

**Симптомы**: Логи показывают `rpc error: code = PermissionDenied desc = Permission denied on secret`

**Диагностика**:
```bash
# Проверить что у SA есть роль
SA="backend@$(gcloud config get-value project).iam.gserviceaccount.com"
gcloud projects get-iam-policy $(gcloud config get-value project) \
  --flatten="bindings[].members" \
  --filter="bindings.members:serviceAccount:$SA" \
  --format="table(bindings.role)"
```

**Решение**:
```bash
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:$SA" \
  --role=roles/secretmanager.secretAccessor

# Redeploy
bash scripts/deploy --api
```

---

### 2. Backend не стартует: "Topic not found"

**Симптомы**: `rpc error: code = NotFound desc = Resource not found (resource=discovery.status)`

**Диагностика**:
```bash
gcloud pubsub topics list | grep discovery
```

**Решение**:
```bash
# Создать недостающие топики
for topic in discovery.status discovery.profile discovery.answer discovery.project; do
  gcloud pubsub topics create "$topic" --quiet
done

# Убедиться что у SA есть права
gcloud projects add-iam-policy-binding $(gcloud config get-value project) \
  --member="serviceAccount:backend@$(gcloud config get-value project).iam.gserviceaccount.com" \
  --role=roles/pubsub.admin
```

---

### 3. Backend работает, но "projectID was empty"

**Симптомы**: Логи показывают `projectID was empty` или Firestore calls падают

**Диагностика**:
```bash
# Проверить env vars
gcloud run services describe h1horizon-api --region us-central1 \
  --format="value(spec.template.spec.containers[0].env)" | grep PROJECT
```

**Решение**:
```bash
# Update с явным указанием обоих env vars
gcloud run services update h1horizon-api --region us-central1 \
  --set-env-vars "GCP_PROJECT_ID=$(gcloud config get-value project),GOOGLE_CLOUD_PROJECT=$(gcloud config get-value project)"
```

---

### 4. Deploy успешен, но код не изменился

**Симптомы**: После deploy функциональность работает как раньше

**Диагностика**:
```bash
# Проверить на какой ревизии трафик
gcloud run services describe h1horizon-api --region us-central1 \
  --format="value(status.traffic[0].revisionName)"

# Сравнить с последней ревизией
gcloud run revisions list --service h1horizon-api --region us-central1 --limit 3
```

**Решение**:
```bash
# Принудительно переключить трафик
gcloud run services update-traffic h1horizon-api --region us-central1 --to-latest

# Проверить
curl $(gcloud run services describe h1horizon-api --region us-central1 --format='value(status.url)')/health
```

---

### 5. Frontend показывает старую версию

**Симптомы**: После deploy frontend не изменился

**Диагностика**:
```bash
# Проверить bucket
gsutil ls -lh gs://$(gcloud config get-value project)-web-static/

# Проверить Cache-Control headers
gsutil stat gs://$(gcloud config get-value project)-web-static/index.html
```

**Решение**:
```bash
# Hard refresh в браузере: Ctrl+Shift+R (Windows) или Cmd+Shift+R (Mac)

# Или очистить cache headers
gcloud storage objects update --cache-control="no-store" \
  gs://$(gcloud config get-value project)-web-static/index.html

# Redeploy
bash scripts/deploy --web
```

---

### 6. Firebase Auth ошибка: "invalid-api-key"

**Симптомы**: Frontend console показывает `FirebaseError: auth/invalid-api-key`

**Диагностика**:
```bash
# Проверить что .env.local существует
cat web/.env.local

# Проверить что Firebase Web App настроен
firebase apps:list
```

**Решение**:
```bash
# Получить config из Firebase Console
# Project Settings > Your apps > Web app > SDK setup and configuration

# Создать web/.env.local
cat > web/.env.local << EOF
VITE_FIREBASE_API_KEY=your_api_key
VITE_FIREBASE_AUTH_DOMAIN=your_project.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=your_project_id
VITE_FIREBASE_STORAGE_BUCKET=your_project.appspot.com
VITE_FIREBASE_MESSAGING_SENDER_ID=your_sender_id
VITE_FIREBASE_APP_ID=your_app_id

VITE_API_BASE_URL=https://h1horizon-api-xxxx.run.app
EOF

# Restart dev server
cd web && npm run dev
```

---

### 7. CORS ошибка на frontend

**Симптомы**: Browser console: `CORS policy: No 'Access-Control-Allow-Origin' header`

**Диагностика**:
```bash
# Проверить FRONTEND_ORIGIN env var
gcloud run services describe h1horizon-api --region us-central1 \
  --format="value(spec.template.spec.containers[0].env)" | grep FRONTEND_ORIGIN
```

**Решение**:
```bash
# Update CORS origin
gcloud run services update h1horizon-api --region us-central1 \
  --update-env-vars "FRONTEND_ORIGIN=http://localhost:5173"

# Для production
gcloud run services update h1horizon-api --region us-central1 \
  --update-env-vars "FRONTEND_ORIGIN=https://your-domain.com"
```

---

### 8. Go build failed локально: "command not found: go"

**Симптомы**: PowerShell: `go : The term 'go' is not recognized`

**Решение**:

**Option 1**: Добавить Go в PATH (PowerShell):
```powershell
$env:PATH += ";C:\Go\bin"
go version
```

**Option 2**: Использовать WSL:
```bash
wsl bash scripts/deploy
```

**Option 3**: Полный путь:
```powershell
& "C:\Go\bin\go.exe" build ./cmd/server
```

---

### 9. Secret не найден: "Secret ... was not found"

**Симптомы**: Deploy падает с `Secret "vertex_api_key" was not found`

**Диагностика**:
```bash
# Проверить существование секрета
gcloud secrets describe vertex_api_key

# Проверить имя (case-sensitive!)
gcloud secrets list | grep -i vertex
```

**Решение**:
```bash
# Создать секрет если не существует
echo "PLACEHOLDER" | gcloud secrets create vertex_api_key --data-file=-

# Обновить значением
echo "$YOUR_KEY" | gcloud secrets versions add vertex_api_key --data-file=-
```

---

### 10. Firestore index creation failed

**Симптомы**: `gcloud firestore indexes composite create` падает с `already exists`

**Решение**: Это OK! Индекс уже создан. Команда идемпотентная, можно игнорировать.

Проверить статус:
```bash
gcloud firestore indexes composite list
```

---

## 🔧 Useful One-Liners

```bash
# Полный health check всех сервисов
for svc in h1horizon-api h1horizon-telegram; do
  URL=$(gcloud run services describe $svc --region us-central1 --format='value(status.url)' 2>/dev/null)
  if [[ -n "$URL" ]]; then
    echo "$svc: $URL"
    curl -s "$URL/health" | jq . || echo "No /health endpoint"
  fi
done

# Получить все URLs
echo "API: $(gcloud run services describe h1horizon-api --region us-central1 --format='value(status.url)')"
echo "TG: $(gcloud run services describe h1horizon-telegram --region us-central1 --format='value(status.url)')"
echo "Web: gs://$(gcloud config get-value project)-web-static"

# Проверить что все секреты созданы
for secret in vertex_api_key openai_api_key anthropic_api_key grok_api_key; do
  if gcloud secrets describe $secret >/dev/null 2>&1; then
    echo "✓ $secret"
  else
    echo "✗ $secret - MISSING"
  fi
done

# Проверить что все топики созданы
for topic in discovery.status discovery.profile discovery.answer discovery.project; do
  if gcloud pubsub topics describe $topic >/dev/null 2>&1; then
    echo "✓ $topic"
  else
    echo "✗ $topic - MISSING"
  fi
done
```

---

## 🚨 Emergency Procedures

### Полный rollback

```bash
# 1. Посмотреть предыдущие ревизии
gcloud run revisions list --service h1horizon-api --region us-central1 --limit 5

# 2. Откат на конкретную ревизию (например, h1horizon-api-00006)
gcloud run services update-traffic h1horizon-api \
  --region us-central1 \
  --to-revisions h1horizon-api-00006=100

# 3. Проверить
curl $(gcloud run services describe h1horizon-api --region us-central1 --format='value(status.url)')/health
```

### Полная переустановка инфраструктуры

```bash
# ⚠️ ВНИМАНИЕ: Это удалит все данные!

# 1. Удалить сервисы
gcloud run services delete h1horizon-api --region us-central1 --quiet
gcloud run services delete h1horizon-telegram --region us-central1 --quiet

# 2. Удалить топики
for topic in discovery.status discovery.profile discovery.answer discovery.project; do
  gcloud pubsub topics delete $topic --quiet
done

# 3. Удалить секреты (опционально)
for secret in vertex_api_key openai_api_key anthropic_api_key grok_api_key; do
  gcloud secrets delete $secret --quiet
done

# 4. Заново создать всё
wsl bash scripts/deploy
```

---

## 📞 Support Contacts

- **GCP Console**: https://console.cloud.google.com
- **Cloud Run Logs**: https://console.cloud.google.com/run
- **Firestore Console**: https://console.cloud.google.com/firestore
- **Secret Manager**: https://console.cloud.google.com/security/secret-manager

---

**См. также**:
- [Deployment Checklist](deployment-checklist.md)
- [Deployment Pitfalls](../lessons-learned/deployment-pitfalls.md)
- [Quick Start](../../DEPLOY_QUICK_START.md)






