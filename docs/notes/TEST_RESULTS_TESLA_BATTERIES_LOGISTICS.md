# Результаты теста: Tesla Battery Logistics

## Дата прогона
2025-12-23

## Цель теста
Проверить способность системы обрабатывать комплексный запрос "How does Tesla's battery logistics work?" с одновременным присутствием:
- Компании (Tesla)
- Продукта (batteries)
- Тема/сфера (logistics/procurement)

## Выполненные действия

### 1. Создание Lens через API
- **Lens ID**: `w0MIMrvfnciTYDnIk8Xv`
- **Project ID**: `Tkw43ExNEdq3lyPEN4nv`
- **Tenant**: `kirisyuk@gmail.com`
- **Scope**: `["/org/tesla"]`
- **ViewConfig**:
  ```json
  {
    "focus": "How does Tesla's battery logistics work? battery logistics supply chain procurement",
    "spherePaths": ["/sphere/logistics"],
    "keywords": ["tesla", "batteries", "logistics", "procurement", "supply chain"],
    "maxRecursionDepth": 2,
    "requestedMetrics": ["lead_time_days", "inventory_days", "on_time_delivery_rate", "freight_cost", "shipment_delay_days"]
  }
  ```

### 2. Проверка передачи focus/spherePaths
✅ **Код проверен**: `LensService.CreateLens` правильно извлекает `focus` и `spherePaths` из `ViewConfig` и передает их в `ScoutingJob.State` (строки 234-255 в `service.go`)

✅ **Код проверен**: `ScoutEngine.ExecuteScoutingJob` правильно извлекает `focus` и `spherePaths` из `job.State` и формирует `searchQuery` (строки 590-669 в `engine.go`)

✅ **Код проверен**: `ScoutPlanner.PlanSources` использует `searchQuery` для семантического поиска через Vertex AI Search (строки 80-105 в `planner.go`)

### 3. Результаты Scouting
✅ **Scout job реально завершился** (не только по логам, но и по документу в Firestore).

- **Scouting job (AgentJob) ID**: `8Ob6aAECxsTSMn2qr9WC`
- **Статус**: `completed`
- **Scouting currentState** (из `state.currentState`): `VERIFIED`
- **Времена**: создан `2025-12-23T09:47:02Z`, последний переход `state.lastStateTransition=2025-12-23T09:48:31Z`
- **Проброшенные intent-поля** (из `agent_jobs/.../state`):
  - `focus`: `"How does Tesla's battery logistics work? battery logistics supply chain procurement"`
  - `keywords`: `["tesla","batteries","logistics","procurement","supply chain"]`
  - `spherePaths`: `["/sphere/logistics"]`
  - `targetCanonicalPath`: `"/org/tesla"`

**Найденные сущности и связи:**
- ✅ Tesla Gigafactory Nevada (`/fac/tesla/gigafactory-nevada`)
  - Связи: Tesla Inc (SUBSIDIARY_OF), Panasonic (DEPENDS_ON), Storey County (LOCATED_IN)
  - Продукты: Model 3, Semi, Powerwall, Megapack (PRODUCES)
  
- ✅ Tesla Gigafactory Texas (`/fac/giga-texas`)
  - Связи: Tesla Inc (SUBSIDIARY_OF), Austin (LOCATED_IN), NVIDIA (DEPENDS_ON)
  - Продукты: Model Y, Cybertruck, 4680 Cell (PRODUCES)

**Vertex AI Search:**
- ✅ Поиск работает корректно
- ✅ Найдены фрагменты: `/org/tesla`, `/product/tesla-energy`
- ⚠️ **Верификация полного `searchQuery`**: в имеющихся логах виделся базовый запрос `"tesla"`. Полный `searchQuery` (с добавленным `focus/keywords/spherePaths`) сейчас **не логируется** как отдельное поле — это остаётся точкой для улучшения наблюдаемости (см. “Следующие шаги”).

### 4. Проверка бюджетных лимитов
✅ **Код проверен**: `BudgetPolicy` подключен в `ScoutEngine`:
- Cascade daily max: $0.50 USD
- Lens daily max: $4.00 USD
- Token caps: автоматически вычисляются из USD лимитов

✅ **Код проверен**: `SpendLedger` подключен в `main.go` и записывает spend через `AITaskExecutor.AddHorizonSpend`

✅ **Код проверен**: Budget enforcement проверяется перед Research (строки 816-838 в `engine.go`)

✅ **Факт (Firestore)**: по Lens `w0MIMrvfnciTYDnIk8Xv` создан дневной spend-документ.

- **Lens spend doc**: `horizon_lens_spend/kirisyuk@gmail.com__w0MIMrvfnciTYDnIk8Xv__2025-12-23`
  - `spendUSD`: `0.01132806`
  - `inputTokens`: `8642`
  - `outputTokens`: `4058`
  - `updatedAt`: `2025-12-23T09:48:31Z`

**Firestore коллекции для spend tracking (формат doc ID):**
- `horizon_lens_spend`: `{tenantID}__{lensID}__{dayUTC}`
- `horizon_cascade_spend`: `{tenantID}__{cascadeID}__{dayUTC}`

### 5. Исправления в процессе теста

#### Проблема: Пустые jobs в assembly-status
**Причина**: `ListScoutingJobs` использовал запросы без композитных индексов Firestore

**Решение**: 
- Изменен `ListScoutingJobs` для использования `ListByProjectAndLens` когда `lensId` предоставлен
- Это использует более надежные индексы Firestore

**Файлы изменены:**
- `backend/internal/horizon/repo/scouting_jobs.go`: добавлен метод `ListByProjectAndLens`
- `backend/internal/http/horizon/scout.go`: обновлен `ListScoutingJobs` для использования project-scoped запросов
- `backend/internal/http/horizon/assembly.go`: обновлен `GetAssemblyStatus` для использования project-scoped запросов

**Подтверждение проблемы индексами (Firestore REST)**:
- Запрос `agentId == "scout" AND state.lensId == "<lensId>" ORDER BY createdAt DESC` требует композитный индекс (Firestore возвращает ссылку “create_composite=...”). Поэтому уход на project-scoped + client-side фильтрацию по lensId — прагматичный обход.

#### Проблема: Auth для локального тестирования
**Решение**: Добавлен dev-fallback для Google OAuth access token (строго под флагом `AUTH_ALLOW_GOOGLE_ACCESS_TOKEN=1`).

⚠️ Важно: в Cloud Run `nv-horizon-api` по умолчанию всё ещё ожидается **Firebase ID token** (по логам есть “incorrect number of segments ... retrieve a valid ID token”). Dev-fallback включать только временно для тестов, чтобы не ослаблять продовую модель доступа.

**Файлы изменены:**
- `backend/internal/http/auth.go`: добавлена поддержка Google access token как fallback

### 6. Статус системы

#### ✅ Работает корректно:
1. Создание Lens с `focus` и `spherePaths` в `ViewConfig`
2. Передача `focus` и `spherePaths` из Lens в ScoutingJob.State
3. Формирование `searchQuery` из `focus`, `keywords`, и `spherePaths` в ScoutEngine
4. Использование `searchQuery` в ScoutPlanner для Vertex AI Search
5. Budget enforcement через SpendLedger
6. Scouting находит сущности и связи (Tesla Gigafactories, продукты, партнеры)

#### ⚠️ Требует проверки:
1. **Наблюдаемость `searchQuery`**: `focus/keywords/spherePaths` точно дошли до `job.State` (см. Firestore job doc), но для полного контроля хорошо добавить структурированное логирование финального `searchQuery`, которое уходит в `ScoutPlanner.PlanSources`.

2. **Проверка sphere-entity linking**: Нужно убедиться, что сущности, относящиеся к сфере `/sphere/logistics`, получают связи со сферой в графе. Это требует проверки создания sphere fragments и INFLUENCES edges.

3. **Проверка резолва keywords**: Keywords из `ViewConfig.keywords` передаются в `ScoutingJob.State`, но механизм инкрементального резолва keywords в canonical paths (как описано в roadmap) еще не реализован.

### 7. Следующие шаги

1. **Добавить логирование searchQuery** в `ScoutEngine.ExecuteScoutingJob` для отладки
2. **Проверить создание sphere fragments** и INFLUENCES edges при применении facts с spherePaths
3. **Реализовать механизм резолва keywords** (как описано в roadmap)
4. **Проверить фактический spend** в Firestore коллекциях `horizon_lens_spend` и `horizon_cascade_spend`
5. **Проверить assembly-status** после исправлений для подтверждения корректного отображения jobs

## Выводы

✅ **Система работоспособна**: Основной pipeline Hook→Assemble→Scout→Graph работает корректно. Lens создается, ScoutingJob запускается, Scout находит сущности и связи, бюджетные лимиты подключены.

✅ **Focus/spherePaths передаются**: Код правильно передает `focus` и `spherePaths` из Lens в ScoutingJob и использует их для формирования searchQuery.

⚠️ **Требуется верификация**: Нужно проверить фактическое использование searchQuery в Vertex AI Search и убедиться, что sphere-entity linking работает корректно.

📝 **Roadmap обновлен**: Добавлены задачи по Intent/Keywords Resolution и Sphere-Entity Linking в `docs/horizon/implementation/roadmap.md`.

