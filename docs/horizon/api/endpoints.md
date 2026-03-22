# REST API Endpoints

## Обзор

API системы мониторинга горизонта предоставляет endpoints для управления Lens, HorizonFragment, DependencyEdge и мониторинга процессов Scout и Propagation.

## Источник правды по маршрутам

Полный перечень путей и методов задаётся в коде (при расхождении с этой страницей верен **код**):

- `backend/internal/http/horizon/handlers.go` — `HorizonHandlers.RegisterRoutes` (большинство `/api/horizon/*`).
- `backend/internal/http/horizon/handlers.go` — `HandleLensRoutes`, `HandleFragmentRoutes`, `HandleEdgeRoutes`, `HandleScoutRoutes` (подпути).
- `backend/internal/http/horizon/enforcement_handlers.go` — `EnforcementHandlers.RegisterRoutes` (`/api/horizon/enforcement/*`).
- `backend/internal/http/horizon/horizonmonitor_external_routes.go` — `RegisterFeedRoutes` (`/api/horizon/feeds/*`, см. ниже про аутентификацию).

Ниже — **сводный реестр** (без дублирования всех query-параметров). Детальные JSON-примеры в следующих разделах страницы носят иллюстративный характер.

### Аутентификация

Большинство маршрутов проходят через тот же Firebase **Bearer**, что и остальной API: заголовок `Authorization: Bearer <firebase_id_token>`. Tenant извлекается из контекста авторизации (профиль пользователя), а не обязательно из отдельного заголовка — см. `backend/internal/http/auth.go`.

Исключение: **`/api/horizon/feeds/*`** регистрируются без `authMiddleware` (прокси для поллинга и обхода CORS); ключи внешних API при необходимости на стороне сервера.

### Реестр: корневые префиксы (`RegisterRoutes`)

| Метод (типично) | Путь | Назначение |
|-----------------|------|------------|
| POST | `/api/horizon/intent/resolve` | Разбор NL → intent |
| GET, POST | `/api/horizon/lenses` | Список / создание линз |
| POST | `/api/horizon/lenses/preflight` | Preflight без записи |
| POST | `/api/horizon/lenses/from-query` | Создание линзы из запроса |
| * | `/api/horizon/lenses/{lensId}/…` | См. таблицу подпутей линзы |
| GET | `/api/horizon/fragments?lensId=…` | Список фрагментов в scope линзы |
| GET | `/api/horizon/fragments/by-path` | Фрагмент по каноническому пути |
| * | `/api/horizon/fragments/{id}/…` | См. таблицу подпутей фрагмента |
| GET | `/api/horizon/supply-chains/by-node` | Цепочки по узлу |
| GET | `/api/horizon/supply-chains/by-lens` | Цепочки по линзе |
| GET | `/api/horizon/supply-chains/search` | Поиск цепочек |
| GET | `/api/horizon/geo-objects` | Гео-объекты |
| POST | `/api/horizon/geo-objects/by-paths` | Гео по путям |
| GET | `/api/horizon/map-activity` | Активность для карты |
| * | `/api/horizon/profiles/{profileId}` | Профиль по id (если handler подключён) |
| GET | `/api/horizon/rollup/{fragmentId}` | Агрегат (например `kind=risk`) |
| GET | `/api/horizon/rollup/{fragmentId}/explain` | Объяснение rollup |
| GET | `/api/horizon/risk/concentration` | Концентрация риска |
| * | `/api/horizon/risk/jobs`, `/api/horizon/risk/jobs/{id}/…` | Risk jobs |
| * | `/api/horizon/watch`, `/api/horizon/watch/…` | Watch: targets, page, scenarios, triggers, sync, metrics, … |
| GET, POST | `/api/horizon/recommendations`, `/api/horizon/recommendations/count`, `/api/horizon/recommendations/{id}/…` | Стратегические рекомендации |
| GET, PUT | `/api/horizon/ai-explanations/edge/{edgeId}` | Кэш объяснений рёбер |
| POST | `/api/horizon/edges` | Создание ребра |
| GET, PUT, DELETE | `/api/horizon/edges/{id}` | Чтение / обновление / удаление |
| POST | `/api/horizon/edges/{id}/hitl` | Human-in-the-loop по ребру |
| GET | `/api/horizon/scout/jobs/` | Список scouting jobs |
| GET | `/api/horizon/scout/jobs/{jobId}` | Конкретный job |
| GET | `/api/horizon/propagation/status` | Статус propagation |
| POST | `/api/horizon/propagation/trigger` | Запуск propagation |
| GET | `/api/horizon/propagation/trace` | Трассировка путей |
| POST | `/api/horizon/events:inject` | Инъекция события (отладка / pipeline) |
| POST | `/api/horizon/events:extract-signals` | Извлечение 12-axis по фактам |
| POST | `/api/horizon/canvas/news-by-fragments` | Canvas: новости по фрагментам |
| GET | `/api/horizon/ontology-facts/by-canonical-path` | Факты онтологии по пути |
| POST | `/api/horizon/scenarios/run` | Сценарий encode → propagate → decode |
| GET | `/api/horizon/entities/search` | Поиск сущностей (LER) |
| POST | `/api/horizon/co-pilot/chat` | Co-pilot чат |

### Подпути линзы: `/api/horizon/lenses/{lensId}/…`

| Подпуть | Назначение |
|---------|------------|
| `product-io:apply` | Применение product IO |
| `items`, `items/{itemId}` | Элементы линзы (CRUD) |
| `assembly-status` | Статус сборки |
| `cockpit` | Данные cockpit |
| `simulate` | What-if |
| `recalculate` | Пересчёт линзы |
| `transfer` | Перенос |
| `scope-scout`, `scope-scout-query`, `scope-scout-full` | Scout по scope |
| `identify-stresses` | Canvas: идентификация стрессов |
| `complete-substrate` | Дозаполнение материализованного субстрата |
| `recover-metrics` | Восстановление метрик supply |
| `qa` | Lens Q&A (SSE) |
| `telemetry/…` | Телеметрия job |
| `graph-query` | Запрос к графу |
| `graph-intent/plan`, `graph-intent/apply`, `graph-intent/dry-run` | План / применение graph intent |
| `control-corridor` | Control corridor |
| `artifacts`, `artifacts/stats`, `artifacts/context` | Артефакты линзы |
| `attention` | Attention слой |

Метод (GET/POST/PATCH/DELETE) для каждого подпути смотрите в `HandleLensRoutes` и соответствующих файлах обработчиков.

### Подпути фрагмента: `/api/horizon/fragments/{id}/…`

| Суффикс | Назначение |
|---------|------------|
| `/profile`, `/profile/refresh`, `/profile/versions` | Профиль узла |
| `/ensure-entity`, `/entity`, `/enrich-entity` | Связь с core entity |
| `/product-io:extract` | Извлечение product IO |
| `/dependencies` | Зависимости |
| `/trace` | Трассировка |
| `/metrics`, `/provenance`, `/interpretation` | Метрики, происхождение, интерпретация |
| `/versions` | Версии фрагмента |
| `/refresh` | Обновление / scout refresh |
| `/expand-materials` | Расширение материалов |

Корень: **GET/DELETE** `/api/horizon/fragments/{id}` — получить / удалить фрагмент.

### Enforcement

| Метод | Путь |
|-------|------|
| GET | `/api/horizon/enforcement/jobs` |
| GET, POST | `/api/horizon/enforcement/jobs/{id}`, `…/execute`, `…/skip` |
| POST | `/api/horizon/enforcement/execute-batch` |
| GET | `/api/horizon/enforcement/check-results`, `…/{jobId}` |

### Внешние фиды (без Firebase middleware)

`GET` прокси: `/api/horizon/feeds/usgs/earthquakes`, `opensky/flights`, `gdelt/geo`, `ucdp/events`, `hapi/conflict`, `noaa/climate`, `nga/warnings`, `faa/status`, `acled/events`, `firms/fires` — см. `horizonmonitor_external_routes.go`.

## Базовый URL

```
https://api.sanskrite.com/api/horizon
```

## Аутентификация (кратко)

См. раздел **«Источник правды → Аутентификация»** выше: обычно достаточно `Authorization: Bearer <firebase_id_token>`; tenant берётся из данных пользователя. Заголовок `X-Tenant-ID` для Horizon **не является** обязательным контрактом в коде авторизации.

## Endpoints

### Lens Management

#### Создание Lens

**POST** `/api/horizon/lenses`

**Request Body**:
```json
{
  "projectId": "uuid",
  "name": "Риски поставок полупроводников из Тайваня",
  "scope": [
    "/fac/tsmc/fab-21",
    "/loc/tw/na/na"
  ],
  "viewConfig": {
    "depth": 3,
    "viewType": "graph"
  },
  "alertConfig": {
    "thresholds": {
      "operational_health": 0.5
    },
    "notifications": ["email", "websocket"]
  }
}
```

**Response** (201 Created):
```json
{
  "id": "uuid",
  "tenantId": "uuid",
  "projectId": "uuid",
  "name": "Риски поставок полупроводников из Тайваня",
  "scope": ["/fac/tsmc/fab-21", "/loc/tw/na/na"],
  "viewConfig": {...},
  "alertConfig": {...},
  "status": "ACTIVE",
  "createdAt": "2025-12-17T14:30:00Z",
  "updatedAt": "2025-12-17T14:30:00Z"
}
```

#### Получение Lens

**GET** `/api/horizon/lenses/{lensId}`

**Response** (200 OK):
```json
{
  "id": "uuid",
  "tenantId": "uuid",
  "projectId": "uuid",
  "name": "Риски поставок полупроводников из Тайваня",
  "scope": ["/fac/tsmc/fab-21", "/loc/tw/na/na"],
  "viewConfig": {...},
  "alertConfig": {...},
  "status": "ACTIVE",
  "metrics": {
    "overallRisk": 0.65,
    "criticalNodes": 2,
    "lastUpdatedAt": "2025-12-17T14:30:00Z"
  },
  "createdAt": "2025-12-17T14:30:00Z",
  "updatedAt": "2025-12-17T14:30:00Z"
}
```

#### Обновление Lens

**PUT** `/api/horizon/lenses/{lensId}`

**Request Body**:
```json
{
  "name": "Обновленное название",
  "scope": ["/fac/tsmc/fab-21", "/loc/tw/na/na", "/org/smic"],
  "viewConfig": {
    "depth": 4,
    "viewType": "table"
  }
}
```

**Response** (200 OK): Обновленный объект Lens

#### Удаление Lens

**DELETE** `/api/horizon/lenses/{lensId}`

**Response** (204 No Content)

#### Список Lens проекта

**GET** `/api/horizon/lenses?projectId={projectId}&status={status}`

**Query Parameters**:
- `projectId` (required): ID проекта
- `status` (optional): Фильтр по статусу (ACTIVE, ARCHIVED)

**Response** (200 OK):
```json
{
  "lenses": [
    {
      "id": "uuid",
      "name": "Риски поставок полупроводников из Тайваня",
      "status": "ACTIVE",
      "scope": ["/fac/tsmc/fab-21", "/loc/tw/na/na"],
      "updatedAt": "2025-12-17T14:30:00Z"
    }
  ],
  "total": 1
}
```

#### Принудительный пересчет Lens

**POST** `/api/horizon/lenses/{lensId}/recalculate`

**Response** (202 Accepted):
```json
{
  "jobId": "uuid",
  "status": "pending",
  "estimatedCompletion": "2025-12-17T14:35:00Z"
}
```

### HorizonFragment Management

#### Получение HorizonFragment

**GET** `/api/horizon/fragments/{fragmentId}`

**Response** (200 OK):
```json
{
  "id": "uuid",
  "tenantId": "uuid",
  "canonicalPath": "/fac/tsmc/fab-21",
  "type": "FACILITY",
  "entityPath": "/Infrastructure/Manufacturing/Factory",
  "sectorPath": "/Technology/Semiconductors/Manufacturing",
  "state": {
    "operational_health": {
      "value": 0.85,
      "confidence": 0.95,
      "observedAt": "2025-12-17T14:30:00Z",
      "unit": null,
      "errorMargin": { "low": 0.80, "high": 0.90 }
    },
    "supply_chain_risk": {
      "value": 0.4,
      "confidence": 0.88,
      "observedAt": "2025-12-17T14:30:00Z"
    }
  },
  "homeLocation": {
    "name": "Hsinchu, Taiwan",
    "iso6709": "+2457+12058/",
    "iso3166_1": "TW",
    "iso3166_2": "TW-HSQ"
  },
  "kbRefs": [
    {
      "kb": "Wikidata",
      "externalId": "Q123456",
      "confidence": 0.95
    }
  ],
  "identifiers": {
    "ISIN": "US1234567890",
    "Ticker": "TSM"
  },
  "metadata": {
    "createdAt": "2025-12-15T10:00:00Z",
    "lastUpdatedAt": "2025-12-17T14:30:00Z",
    "lastVerifiedAt": "2025-12-17T14:25:00Z",
    "epistemicStatus": "VERIFIED"
  },
  "governance": {
    "owner": "SYSTEM",
    "activeSubscribers": 5,
    "status": "ACTIVE"
  },
  "version": 15
}
```

#### Hypothesis: Product IO (v1, PRODUCT-only)

Этот набор endpoints предназначен для “живого тестирования гипотез” из инспектора:
- **LLM** извлекает кандидатов продуктов (produces/consumes)
- **Бэкенд** материализует продукты как **HorizonFragment типа PRODUCT** и создаёт ребра `PRODUCES` / `CONSUMES`

Важно:
- граф глобальный (`tenantId = "global"` для фрагментов и ребер)
- HorizonFragment первичен, Entity — best-effort enrichment по ссылке
- v1 поддерживает **только PRODUCT** (позже расширим на market/service/technology)

##### Extract products (LLM only)

**POST** `/api/horizon/fragments/{fragmentId}/product-io:extract`

**Request Body**:
```json
{
  "direction": "both",
  "hint": "optional short hint for the model",
  "lang": "en"
}
```

**Response** (200 OK):
```json
{
  "fragmentId": "uuid",
  "direction": "both",
  "produces": [{ "productName": "string", "confidence": 0.8, "reason": "string" }],
  "consumes": [{ "productName": "string", "confidence": 0.7, "reason": "string" }]
}
```

##### Apply materialization (create/dedup + edges)

**POST** `/api/horizon/lenses/{lensId}/product-io:apply`

**Request Body**:
```json
{
  "objectiveFragmentId": "uuid",
  "produces": [{ "productName": "string", "confidence": 0.8, "reason": "string" }],
  "consumes": [{ "productName": "string", "confidence": 0.7, "reason": "string" }]
}
```

**Response** (200 OK):
```json
{
  "lensId": "uuid",
  "objectiveFragmentId": "uuid",
  "delta": {
    "fragmentsUpserted": [{ "...": "HorizonFragment" }],
    "edgesUpserted": [{ "...": "DependencyEdge" }]
  }
}
```

#### Получение HorizonFragment по canonical path

**GET** `/api/horizon/fragments/by-path?path={canonicalPath}`

**Query Parameters**:
- `path` (required): Канонический путь узла

**Response** (200 OK): Объект HorizonFragment

#### История версий HorizonFragment

**GET** `/api/horizon/fragments/{fragmentId}/versions?limit={limit}&offset={offset}`

**Query Parameters**:
- `limit` (optional): Количество версий (по умолчанию 50)
- `offset` (optional): Смещение для пагинации

**Response** (200 OK):
```json
{
  "versions": [
    {
      "versionId": "uuid",
      "state": {
        "operational_health": {
          "value": 0.85,
          "confidence": 0.95,
          "observedAt": "2025-12-17T14:30:00Z"
        },
        "supply_chain_risk": {
          "value": 0.4,
          "confidence": 0.88,
          "observedAt": "2025-12-17T14:30:00Z"
        }
      },
      "sourceFrameId": "uuid",
      "timestamp": "2025-12-17T14:30:00Z"
    }
  ],
  "total": 15,
  "limit": 50,
  "offset": 0
}
```

#### Принудительное обновление HorizonFragment

**POST** `/api/horizon/fragments/{fragmentId}/refresh`

**Request Body**:
```json
{
  "strategy": "Snapshot",
  "maxCost": 50.00
}
```

**Response** (202 Accepted):
```json
{
  "scoutingJobId": "uuid",
  "status": "pending",
  "estimatedCompletion": "2025-12-17T14:31:00Z"
}
```

### DependencyEdge Management

#### Получение зависимостей узла

**GET** `/api/horizon/fragments/{fragmentId}/dependencies?direction={direction}&type={type}`

**Query Parameters**:
- `direction` (optional): Направление (outgoing, incoming, both) - по умолчанию both
- `type` (optional): Фильтр по типу зависимости

**Response** (200 OK):
```json
{
  "edges": [
    {
      "id": "uuid",
      "sourceFragmentId": "uuid",
      "targetFragmentId": "uuid",
      "type": "SUPPLIES_TO",
      "properties": {
        "criticality": 0.9,
        "amplification": 1.1,
        "latencyHours": 72
      }
    }
  ],
  "total": 1
}
```

#### Создание DependencyEdge

**POST** `/api/horizon/edges`

**Request Body**:
```json
{
  "sourceFragmentId": "uuid",
  "targetFragmentId": "uuid",
  "type": "SUPPLIES_TO",
  "properties": {
    "criticality": 0.9,
    "amplification": 1.1,
    "latencyHours": 72
  }
}
```

**Response** (201 Created): Созданный DependencyEdge

#### Обновление DependencyEdge

**PUT** `/api/horizon/edges/{edgeId}`

**Request Body**:
```json
{
  "properties": {
    "criticality": 0.95,
    "amplification": 1.2
  }
}
```

**Response** (200 OK): Обновленный DependencyEdge

#### Удаление DependencyEdge

**DELETE** `/api/horizon/edges/{edgeId}`

**Response** (204 No Content)

### Scout Jobs

#### Получение статуса ScoutingJob

**GET** `/api/horizon/scout/jobs/{jobId}`

**Response** (200 OK):
```json
{
  "id": "uuid",
  "status": "EXTRACTION",
  "targetCanonicalPath": "/fac/tsmc/fab-21",
  "progress": 0.6,
  "currentStep": "extraction",
  "steps": [
    {
      "name": "planning",
      "status": "completed",
      "completedAt": "2025-12-17T14:25:00Z"
    },
    {
      "name": "execution",
      "status": "completed",
      "completedAt": "2025-12-17T14:26:00Z"
    },
    {
      "name": "extraction",
      "status": "running",
      "startedAt": "2025-12-17T14:27:00Z"
    }
  ],
  "createdAt": "2025-12-17T14:24:00Z",
  "estimatedCompletion": "2025-12-17T14:31:00Z"
}
```

#### Список ScoutingJob

**GET** `/api/horizon/scout/jobs?lensId={lensId}&status={status}&limit={limit}`

**Query Parameters**:
- `lensId` (optional): Фильтр по Lens
- `status` (optional): Фильтр по статусу
- `limit` (optional): Количество результатов (по умолчанию 50)

**Response** (200 OK):
```json
{
  "jobs": [
    {
      "id": "uuid",
      "status": "VERIFIED",
      "targetCanonicalPath": "/fac/tsmc/fab-21",
      "createdAt": "2025-12-17T14:24:00Z",
      "completedAt": "2025-12-17T14:30:00Z"
    }
  ],
  "total": 1
}
```

### Propagation Status

#### Получение статуса распространения

**GET** `/api/horizon/propagation/status?fragmentId={fragmentId}`

**Query Parameters**:
- `fragmentId` (required): ID узла для проверки статуса распространения

**Response** (200 OK):
```json
{
  "fragmentId": "uuid",
  "isPropagating": false,
  "lastPropagation": {
    "startedAt": "2025-12-17T14:30:00Z",
    "completedAt": "2025-12-17T14:30:05Z",
    "nodesUpdated": 12,
    "lensesRecalculated": 3
  }
}
```

#### Принудительное распространение изменений

**POST** `/api/horizon/propagation/trigger?fragmentId={fragmentId}`

**Query Parameters**:
- `fragmentId` (required): ID узла для запуска распространения

**Response** (202 Accepted):
```json
{
  "propagationId": "uuid",
  "status": "started",
  "estimatedCompletion": "2025-12-17T14:30:10Z"
}
```

## Коды ошибок

### 400 Bad Request
Некорректные параметры запроса

```json
{
  "error": "invalid_request",
  "message": "Invalid canonical path format",
  "details": {
    "field": "canonicalPath",
    "value": "invalid-path"
  }
}
```

### 401 Unauthorized
Отсутствует или невалидный токен аутентификации

```json
{
  "error": "unauthorized",
  "message": "Invalid or missing authentication token"
}
```

### 403 Forbidden
Нет доступа к ресурсу

```json
{
  "error": "forbidden",
  "message": "Access denied to this resource"
}
```

### 404 Not Found
Ресурс не найден

```json
{
  "error": "not_found",
  "message": "Lens not found",
  "resource": "lens",
  "id": "uuid"
}
```

### 409 Conflict
Конфликт при создании ресурса

```json
{
  "error": "conflict",
  "message": "Lens with this name already exists",
  "resource": "lens"
}
```

### 429 Too Many Requests
Превышен лимит запросов

```json
{
  "error": "rate_limit_exceeded",
  "message": "Rate limit exceeded",
  "retryAfter": 60
}
```

### 500 Internal Server Error
Внутренняя ошибка сервера

```json
{
  "error": "internal_error",
  "message": "An internal error occurred",
  "requestId": "uuid"
}
```

## WebSocket Events

### Подписка на события

**WebSocket URL**: `wss://<ws-host>/ws`

**Аутентификация**: Токен передается в query параметре `token`

**Project subscription (обязательно)**: после подключения клиент отправляет:

```json
{"type":"subscribe","projectId":"<projectId>"}
```

**События**:

#### `horizon.assembly.*`
События сборки (Hook → Sled): stages (preferred) и прогресс.

```json
{
  "type": "horizon.assembly.stage",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "stage": "scouting",
    "phase": "scouting",
    "progress": 0.43,
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

#### `horizon.graph.node.upserted`
Материализация/обновление узла в графе (cache-hit / scouting / verified / propagation).

```json
{
  "type": "horizon.graph.node.upserted",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "fragmentId": "uuid",
    "canonicalPath": "/fac/tsmc/fab-21",
    "status": "cached",
    "epistemicStatus": "VERIFIED",
    "version": 15,
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

#### `horizon.scout.job.state`
Переходы состояний ScoutingJob (PENDING/PLANNING/EXECUTION/EXTRACTION/VALIDATION/RESOLUTION/VERIFIED/FAILED).

```json
{
  "type": "horizon.scout.job.state",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "jobId": "uuid",
    "targetCanonicalPath": "/fac/tsmc/fab-21",
    "state": "EXTRACTION",
    "progress": 0.5,
    "hint": "extraction",
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

#### `horizon.propagation.started`
Начало распространения изменений

```json
{
  "type": "horizon.propagation.started",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "sourceFragmentId": "uuid",
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

#### `horizon.propagation.node_updated`
Обновление узла в ходе propagation (granular update)

```json
{
  "type": "horizon.propagation.node_updated",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "sourceFragmentId": "uuid",
    "fragmentId": "uuid",
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

#### `horizon.propagation.completed`
Завершение propagation (summary)

```json
{
  "type": "horizon.propagation.completed",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "sourceFragmentId": "uuid",
    "nodesUpdated": 12,
    "lensesAffected": 3,
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

#### `horizon.graph.edge.upserted`
Создание/обновление ребра в графе.

```json
{
  "type": "horizon.graph.edge.upserted",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
    "edgeId": "uuid",
    "sourceFragmentId": "uuid",
    "targetFragmentId": "uuid",
    "type": "SUPPLIES_TO",
    "properties": { "criticality": 0.9, "amplification": 1.1, "latencyHours": 72 },
    "correlationId": "uuid",
    "ts": "2025-12-22T12:34:56.123456Z"
  }
}
```

## См. также

- [JSON Schemas](schemas.md)
- [Архитектурный обзор](../architecture/overview.md)
- [Realtime (implementation)](../realtime.md)

