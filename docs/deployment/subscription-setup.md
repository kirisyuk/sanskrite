# Настройка системы подписок в GCP

## Требования к инфраструктуре

### 1. Firestore Collections

Коллекции создаются автоматически при первом использовании, но нужно убедиться, что есть индексы.

#### Коллекция `subscriptions`
- Создается автоматически при создании пользователя
- Используется для хранения подписок пользователей

#### Коллекция `vouchers`
- Создается автоматически при создании ваучера админом
- Используется для хранения промокодов

### 2. Firestore Indexes

Добавьте следующие индексы в `backend/firestore.indexes.json`:

```json
{
  "indexes": [
    {
      "collectionGroup": "subscriptions",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "status", "order": "ASCENDING" },
        { "fieldPath": "currentPeriodEnd", "order": "ASCENDING" }
      ]
    },
    {
      "collectionGroup": "vouchers",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "code", "order": "ASCENDING" }
      ]
    },
    {
      "collectionGroup": "vouchers",
      "queryScope": "COLLECTION",
      "fields": [
        { "fieldPath": "tier", "order": "ASCENDING" },
        { "fieldPath": "isActive", "order": "ASCENDING" },
        { "fieldPath": "createdAt", "order": "DESCENDING" }
      ]
    }
  ]
}
```

После добавления индексов, разверните их:

```bash
gcloud firestore indexes deploy backend/firestore.indexes.json
```

Или дождитесь автоматического создания при первом запросе (Firestore создаст их автоматически, но может занять время).

### 3. Cloud Scheduler (опционально, рекомендуется для production)

Для автоматической обработки биллинга каждый день рекомендуется настроить Cloud Scheduler.

#### Вариант A: Через скрипт deploy (рекомендуется)

Добавьте в `scripts/deploy` создание job для биллинга (аналогично существующему job для publish-due).

#### Вариант B: Вручную

```bash
# Получить URL вашего backend сервиса
API_URL=$(gcloud run services describe sanskrite-api --region us-central1 --format='value(status.url)')

# Создать секрет с токеном для системных вызовов (если еще не создан)
# Используйте тот же токен, что и для других admin jobs
ADMIN_TOKEN="your-secure-token-here"
echo -n "$ADMIN_TOKEN" | gcloud secrets create subscription-billing-token --data-file=- || \
  echo -n "$ADMIN_TOKEN" | gcloud secrets versions add subscription-billing-token --data-file=-

# Создать Cloud Scheduler job для ежедневного биллинга
gcloud scheduler jobs create http subscription-billing-daily \
  --location=us-central1 \
  --schedule="0 2 * * *" \
  --uri="$API_URL/api/v1/subscriptions/billing" \
  --http-method=POST \
  --headers="x-functions-key=$ADMIN_TOKEN" \
  --attempt-deadline=300s \
  --time-zone="UTC"
```

**Расписание:**
- `0 2 * * *` - каждый день в 02:00 UTC (можно изменить)
- Рекомендуется запускать в непиковое время

#### Отключить встроенный таймер

Если используете Cloud Scheduler, отключите встроенный таймер через переменную окружения:

```bash
gcloud run services update sanskrite-api \
  --region us-central1 \
  --update-env-vars DISABLE_SUBSCRIPTION_BILLING=true
```

### 4. Service Account Permissions

Убедитесь, что service account для Cloud Run имеет права на Firestore:

```bash
PROJECT_ID="your-project-id"
SA_EMAIL="backend@${PROJECT_ID}.iam.gserviceaccount.com"

# Права уже должны быть настроены (если используется стандартный deploy скрипт)
# Но проверить можно так:
gcloud projects get-iam-policy $PROJECT_ID \
  --flatten="bindings[].members" \
  --filter="bindings.members:serviceAccount:${SA_EMAIL}" \
  --format="table(bindings.role)"
```

Нужные роли:
- `roles/datastore.user` - для работы с Firestore
- `roles/secretmanager.secretAccessor` - если используете секреты для токенов

### 5. Firebase Security Rules (опционально)

Если используете Firebase SDK напрямую из фронтенда, добавьте правила доступа:

```javascript
// firestore.rules
match /subscriptions/{subscriptionId} {
  allow read: if request.auth != null && 
              resource.data.userId == request.auth.uid;
  allow write: if false; // Все изменения только через backend API
}

match /vouchers/{voucherId} {
  allow read: if request.auth != null && 
              get(/databases/$(database)/documents/core_users/$(request.auth.uid)).data.role == 'admin';
  allow write: if false; // Все изменения только через backend API
}
```

## Проверка после настройки

### 1. Проверить индексы

```bash
# Проверить статус индексов
gcloud firestore indexes list

# Дождаться статуса "ENABLED" для всех индексов
```

### 2. Проверить Cloud Scheduler job

```bash
# Список jobs
gcloud scheduler jobs list --location=us-central1

# Проверить детали job
gcloud scheduler jobs describe subscription-billing-daily --location=us-central1

# Протестировать запуск вручную
gcloud scheduler jobs run subscription-billing-daily --location=us-central1
```

### 3. Проверить работу API

```bash
# Получить свою подписку
curl -H "Authorization: Bearer YOUR_TOKEN" \
  https://your-api-url/api/v1/subscriptions/me

# Применить ваучер (как пользователь)
curl -X POST -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"code": "TEST123"}' \
  https://your-api-url/api/v1/subscriptions/voucher

# Создать ваучер (как админ)
curl -X POST -H "Authorization: Bearer ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "code": "PRO2024Q1",
    "tier": "pro",
    "durationMonths": 3,
    "maxUses": 100
  }' \
  https://your-api-url/api/v1/vouchers
```

## Минимальная настройка (без Cloud Scheduler)

Если не хотите использовать Cloud Scheduler, система будет работать с встроенным таймером:

- Таймер запускается автоматически при старте сервера
- По умолчанию обрабатывает биллинг каждые 24 часа
- Можно настроить через переменную окружения `SUBSCRIPTION_BILLING_INTERVAL`

**Важно:** Для production рекомендуется использовать Cloud Scheduler, так как:
- Более надежно (не зависит от перезапусков сервиса)
- Легче мониторить и отлаживать
- Не нагружает постоянно работающий инстанс

## Миграция существующих пользователей

Если у вас уже есть пользователи, создайте скрипт миграции для создания подписок:

```go
// cmd/migrate_subscriptions/main.go
// Скрипт для создания free подписок для всех существующих пользователей
```

Или сделайте это через API при первом запросе (уже реализовано автоматически).

