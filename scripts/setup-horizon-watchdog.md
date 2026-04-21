# Рекомендация по реализации Watchdog для Horizon

## Анализ требований

Watchdog Service должен:
1. Периодически проверять источники для активных HorizonFragment
2. Обнаруживать изменения контента (через сравнение хешей)
3. Создавать ScoutingJob для обновления данных
4. Управлять жизненным циклом (RUNNING/PAUSED на основе active_subscribers)

## Варианты реализации

### Вариант 1: Cloud Scheduler → HTTP endpoint в Cloud Run `api` (РЕКОМЕНДУЕТСЯ)

**Преимущества:**
- ✅ Простота развертывания и управления
- ✅ Автоматическое масштабирование
- ✅ Низкие операционные затраты (pay-per-use)
- ✅ Интеграция с GCP экосистемой
- ✅ Поддержка до 60 минут выполнения (2nd gen)
- ✅ Простая интеграция с Firestore triggers

**Недостатки:**
- ⚠️ Холодный старт может быть медленным (но не критично для периодических задач)
- ⚠️ Ограничения памяти (до 32GB для 2nd gen)

**Архитектура:**
```
Cloud Scheduler (каждые 5 минут)
  ↓
Cloud Run: api (cron endpoint)
  ↓
Watchdog Service (internal/horizon/watchdog)
  ↓
Firestore (чтение watchdog_configs)
  ↓
Scout Engine (создание ScoutingJob)
```

**Реализация:**
- Cloud Scheduler вызывает cron endpoint в `api`
- Endpoint защищён через `X-Job-Token` (переменная `ADMIN_JOB_TOKEN`), как и другие scheduler jobs
- Endpoint берет due-конфиги из Firestore, сканирует источники, при необходимости запускает Scout

### Вариант 2: Cloud Run Jobs + Cloud Scheduler

**Преимущества:**
- ✅ Больше контроля над окружением
- ✅ Поддержка длительных задач (до 24 часов)
- ✅ Больше памяти и CPU
- ✅ Лучше для сложной логики

**Недостатки:**
- ⚠️ Сложнее развертывание
- ⚠️ Выше операционные затраты (всегда запущен контейнер)
- ⚠️ Больше конфигурации

**Когда использовать:**
- Если нужна сложная логика обработки
- Если задачи выполняются дольше 60 минут
- Если нужен больший контроль над ресурсами

### Вариант 3: Внутренний сервис в Cloud Run

**Преимущества:**
- ✅ Полный контроль
- ✅ Использование существующей инфраструктуры
- ✅ Проще отладка

**Недостатки:**
- ⚠️ Всегда запущен (выше затраты)
- ⚠️ Нужно управлять расписанием внутри сервиса
- ⚠️ Больше операционных задач

**Когда использовать:**
- Если Watchdog интегрирован в основной сервис
- Если нужна тесная интеграция с другими компонентами

## Рекомендация: Cloud Scheduler → HTTP endpoint в Cloud Run `api`

### Обоснование

1. **Единый деплой**: всё разворачивается через `scripts/deploy` (без отдельных Cloud Functions/Jobs)
2. **Код рядом с Horizon**: использует уже инициализированные repos/ScoutEngine внутри API
3. **Безопасность**: общий механизм `ADMIN_JOB_TOKEN` / `X-Job-Token`
4. **Отсутствие фонового тика**: Cloud Scheduler становится единственным источником периодичности

### План реализации

#### 1. Cron endpoint в API

Endpoint:
- `POST /api/v1/admin/horizon/watchdog/run?limit=200`
- защищён через `X-Job-Token`
- использует distributed-lock в Firestore, чтобы не было пересечений запусков

#### 2. Создание/обновление Cloud Scheduler Job (через deploy)

```bash
./scripts/deploy --api --nv-horizon-watchdog
```

#### 3. Параметры (env vars)

- `ADMIN_JOB_TOKEN` (в Cloud Run): используется Cloud Scheduler job-ом в заголовке `X-Job-Token`
- `HORIZON_WATCHDOG_CRON_LIMIT` (default: 200)
- `HORIZON_WATCHDOG_CRON_LOCK_TTL` (default: 9m)
- `HORIZON_WATCHDOG_CRON_SCHEDULE` (default: `*/5 * * * *`) — задается при деплое через переменную окружения shell

### Альтернатива: Firestore Triggers

Если нужна более реактивная модель, можно использовать Firestore Triggers:

```bash
# Cloud Function с Firestore trigger
gcloud functions deploy horizon-watchdog-trigger \
  --gen2 \
  --runtime=go121 \
  --region=us-central1 \
  --source=./backend/cmd/watchdog \
  --entry-point=OnWatchdogConfigChange \
  --trigger-event-filters="type=document.update" \
  --trigger-event-filters="database=(default)" \
  --trigger-resource="projects/nv-horizon/databases/(default)/documents/horizon_watchdog_configs/{configId}" \
  --service-account=backend@inquire-data-84889.iam.gserviceaccount.com
```

**Преимущества триггеров:**
- Реактивная модель (срабатывает при изменении конфигурации)
- Не нужен Cloud Scheduler
- Более эффективно для редких изменений

**Недостатки:**
- Не подходит для периодических проверок
- Может пропустить изменения, если конфигурация не обновляется

## Итоговая рекомендация

**Использовать Cloud Scheduler → HTTP endpoint в Cloud Run `api`** для периодических проверок.

**Причины:**
1. Простота реализации и поддержки
2. Эффективность затрат
3. Достаточная функциональность для задач Watchdog
4. Легкая интеграция с существующим кодом

**Следующие шаги:**
1. Создать Cloud Function handler в `backend/cmd/watchdog/`
2. Интегрировать с существующим `WatchdogService`
3. Настроить Cloud Scheduler для периодического запуска
4. Протестировать на тестовом окружении
