# REST API Endpoints

## Обзор

API системы мониторинга горизонта предоставляет endpoints для управления Lens, HorizonFragment, DependencyEdge и мониторинга процессов Scout и Propagation.

## Базовый URL

```
https://api.sanskrite.com/api/horizon
```

## Аутентификация

Все запросы требуют аутентификации через Firebase Auth:

```
Authorization: Bearer <firebase_token>
X-Tenant-ID: <tenant_id>
```

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
События сборки (Hook → Sled): фазы и прогресс.

```json
{
  "type": "horizon.assembly.phase",
  "data": {
    "tenantId": "uuid",
    "projectId": "uuid",
    "lensId": "uuid",
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

