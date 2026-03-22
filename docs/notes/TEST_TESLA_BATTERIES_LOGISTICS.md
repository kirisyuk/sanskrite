# Тестовый сценарий: Tesla Battery Logistics

## Цель

Протестировать весь pipeline Hook→Assemble→Scout→Graph с комплексным запросом:
**"How does Tesla's battery logistics work?"**

Это проверяет способность системы обрабатывать задачу, где одновременно присутствуют:
- Компания (Tesla)
- Продукт (batteries)
- Тема/сфера (logistics/procurement)

## Подготовка

### 1. Получить Project ID

```bash
# Получить список проектов для тенанта kirisyuk@gmail.com
gcloud auth print-access-token | xargs -I {} curl -H "Authorization: Bearer {}" \
  https://YOUR_API_URL/api/v1/projects | jq '.[] | {id, displayName}'
```

Или через gcloud напрямую (если API доступен локально):
```bash
export PROJECT_ID=$(curl -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  http://localhost:8080/api/v1/projects | jq -r '.[0].id')
```

### 2. Проверить бюджетные лимиты

Система использует следующие лимиты (по умолчанию):
- **Cascade/day**: $0.50 USD
- **Lens/day**: $4.00 USD
- **Token caps**: автоматически вычисляются из USD лимитов

Проверить текущие настройки:
```bash
# В коде: backend/internal/horizon/scout/budget_policy.go
# Env vars:
# - HORIZON_BUDGETS_ENABLED (default "1")
# - HORIZON_CASCADE_DAILY_MAX_USD (default 0.50)
# - HORIZON_LENS_DAILY_MAX_USD (default 4.00)
```

### 3. Проверить, что spendLedger подключен

SpendLedger должен быть подключен в `backend/cmd/server/main.go`:
- Строка 989: `horizonSpendLedger := horizonrepo.NewHorizonSpendLedger(fsClient.Client())`
- Строка 992: `exec.WithHorizonSpendSink(horizonSpendLedger)`
- Строка 1182: `scoutEngine.WithSpendLedger(horizonSpendLedger)`

## Запуск теста

### Вариант 1: Через скрипт

```bash
export PROJECT_ID="your-project-id"
export AUTH_TOKEN=$(gcloud auth print-access-token)
./test-tesla-batteries-logistics.sh $PROJECT_ID
```

### Вариант 2: Вручную

```bash
# 1. Обновить projectId в JSON
sed "s/REPLACE_WITH_PROJECT_ID/$PROJECT_ID/g" test-tesla-batteries-logistics.json > /tmp/lens-request.json

# 2. Создать Lens
curl -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -d @/tmp/lens-request.json \
  http://localhost:8080/api/horizon/lenses | jq '.'
```

## Что проверяем

### 1. Lens создан успешно
- HTTP 200/201
- Возвращается `lensId`
- `scope` содержит `/org/tesla`
- `viewConfig` содержит `focus`, `spherePaths`, `keywords`

### 2. ScoutingJob создан и запущен
- Проверить в Firestore: `horizon_scouting_jobs` collection
- `targetCanonicalPath` = `/org/tesla`
- `state.focus` = "battery logistics supply chain procurement"
- `state.spherePaths` = `["/sphere/logistics"]`
- `state.keywords` = `["batteries", "logistics", "supply chain", "procurement"]`

### 3. ScoutPlanner использует focus/spherePaths
- Проверить логи ScoutPlanner
- Semantic search query должен включать focus и spherePaths
- Выбранные sources должны быть релевантны логистике батарей

### 4. Research Agent использует focus
- Проверить промпты Research Agent
- Должен искать информацию про "battery logistics supply chain procurement"
- Должен учитывать sphere `/sphere/logistics`

### 5. Budget enforcement работает
- Проверить Firestore: `horizon_lens_spend` collection
- Должны записываться spendUSD, inputTokens, outputTokens
- При превышении лимитов Research должен пропускаться (с логом)

### 6. Fragments созданы
- `/org/tesla` должен существовать (или быть создан)
- Должны создаться связанные fragments (например, `/product/tesla/battery`, suppliers, logistics facilities)
- Sphere fragment `/sphere/logistics` должен существовать

### 7. Edges созданы
- Должны быть edges типа `SUPPLIES_TO`, `DEPENDS_ON`, `INFLUENCES`
- Сущности должны быть связаны со сферой `/sphere/logistics` через `INFLUENCES`

## Мониторинг

### Проверить статус assembly
```bash
LENS_ID="your-lens-id"
curl -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  http://localhost:8080/api/horizon/lenses/$LENS_ID/assembly-status | jq '.'
```

### Проверить бюджет
```bash
# В Firestore консоли или через gcloud:
gcloud firestore documents get \
  projects/YOUR_PROJECT/databases/(default)/documents/horizon_lens_spend/TENANT_ID__LENS_ID__$(date +%Y-%m-%d)
```

### Проверить логи Scout
```bash
# В Cloud Logging или локальных логах искать:
# - "[ScoutEngine] Starting research"
# - "[ScoutPlanner] Planning sources"
# - "[ScoutEngine] Skipping research: lens daily USD budget exceeded" (если лимит превышен)
```

## Ожидаемые результаты

1. ✅ Lens создан с правильными параметрами
2. ✅ ScoutingJob запущен и обрабатывается
3. ✅ ScoutPlanner выбирает релевантные sources (с учетом focus/spherePaths)
4. ✅ Research Agent находит информацию про логистику батарей Tesla
5. ✅ Создаются fragments для Tesla, battery product, suppliers, logistics facilities
6. ✅ Создаются edges между сущностями и сферой logistics
7. ✅ Budget tracking работает корректно

## Troubleshooting

### Lens не создается
- Проверить, что projectId существует и принадлежит тенанту
- Проверить валидность JSON (особенно canonical paths)
- Проверить логи сервера

### ScoutingJob не запускается
- Проверить Cloud Tasks queue (если используется)
- Проверить логи Scout Engine
- Проверить, что `targetCanonicalPath` валиден

### Budget не отслеживается
- Проверить, что spendLedger подключен в main.go
- Проверить, что AITaskExecutor вызывает `AddHorizonSpend`
- Проверить Firestore коллекции `horizon_lens_spend` и `horizon_cascade_spend`

### Sources не фильтруются по focus
- Проверить логи ScoutPlanner
- Проверить, что `focus` и `spherePaths` передаются в `PlanSources`
- Проверить Vertex AI Search query (должен включать focus и spherePaths)

