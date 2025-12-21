# JSON Schemas для API

## Обзор

Данный документ содержит JSON схемы для всех запросов и ответов API системы мониторинга горизонта.

## Lens Schemas

### CreateLensRequest

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["projectId", "name", "scope"],
  "properties": {
    "projectId": {
      "type": "string",
      "format": "uuid",
      "description": "ID проекта, к которому привязан Lens"
    },
    "name": {
      "type": "string",
      "minLength": 1,
      "maxLength": 255,
      "description": "Название Lens"
    },
    "scope": {
      "type": "array",
      "items": {
        "type": "string",
        "pattern": "^/[a-zA-Z0-9_/-]+$",
        "description": "Канонический путь HorizonFragment"
      },
      "minItems": 1,
      "description": "Массив канонических путей узлов для мониторинга"
    },
    "viewConfig": {
      "type": "object",
      "properties": {
        "depth": {
          "type": "integer",
          "minimum": 1,
          "maximum": 10,
          "default": 3,
          "description": "Глубина графа для визуализации"
        },
        "viewType": {
          "type": "string",
          "enum": ["graph", "table", "timeline"],
          "default": "graph",
          "description": "Тип представления"
        }
      },
      "additionalProperties": false
    },
    "alertConfig": {
      "type": "object",
      "properties": {
        "thresholds": {
          "type": "object",
          "additionalProperties": {
            "type": "number",
            "minimum": -1,
            "maximum": 1
          },
          "description": "Пороги для метрик (ключ - имя метрики, значение - порог). Диапазон зависит от типа метрики: magnitude [0..1], directional [-1..1]."
        },
        "notifications": {
          "type": "array",
          "items": {
            "type": "string",
            "enum": ["email", "websocket", "sms"]
          },
          "description": "Каналы уведомлений"
        }
      },
      "additionalProperties": false
    }
  },
  "additionalProperties": false
}
```

### LensResponse

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["id", "tenantId", "projectId", "name", "scope", "status", "createdAt", "updatedAt"],
  "properties": {
    "id": {
      "type": "string",
      "format": "uuid"
    },
    "tenantId": {
      "type": "string",
      "format": "uuid"
    },
    "projectId": {
      "type": "string",
      "format": "uuid"
    },
    "name": {
      "type": "string"
    },
    "scope": {
      "type": "array",
      "items": {
        "type": "string"
      }
    },
    "viewConfig": {
      "type": "object"
    },
    "alertConfig": {
      "type": "object"
    },
    "status": {
      "type": "string",
      "enum": ["ACTIVE", "ARCHIVED"]
    },
    "metrics": {
      "type": "object",
      "properties": {
        "overallRisk": {
          "type": "number",
          "minimum": 0,
          "maximum": 1
        },
        "criticalNodes": {
          "type": "integer",
          "minimum": 0
        },
        "lastUpdatedAt": {
          "type": "string",
          "format": "date-time"
        }
      }
    },
    "createdAt": {
      "type": "string",
      "format": "date-time"
    },
    "updatedAt": {
      "type": "string",
      "format": "date-time"
    }
  }
}
```

## HorizonFragment Schemas

### HorizonFragmentResponse

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["id", "tenantId", "canonicalPath", "type", "entityPath", "sectorPath", "state", "metadata", "governance", "version"],
  "properties": {
    "id": {
      "type": "string",
      "format": "uuid"
    },
    "tenantId": {
      "type": "string",
      "format": "uuid"
    },
    "canonicalPath": {
      "type": "string",
      "pattern": "^/[a-zA-Z0-9_/-]+$"
    },
    "type": {
      "type": "string",
      "enum": ["ORGANIZATION", "FACILITY", "GEO_REGION", "SPHERE", "EVENT", "PRODUCT", "PROGRAM", "MARKET", "PERSON"]
    },
    "entityPath": {
      "type": "string",
      "pattern": "^/[A-Za-z0-9_/-]+$",
      "description": "Иерархический путь физического типа сущности (Core Taxonomy v1.0), например: /Infrastructure/Manufacturing/Factory"
    },
    "sectorPath": {
      "type": "string",
      "pattern": "^/[A-Za-z0-9_/-]+$",
      "description": "Иерархический отраслевой путь (Core Taxonomy v1.0), например: /Technology/Semiconductors/Manufacturing"
    },
    "entityId": {
      "type": "string",
      "format": "uuid",
      "description": "Опциональная ссылка на Entity в основной онтологии Sanskrite"
    },
    "state": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "required": ["value", "confidence", "observedAt"],
        "properties": {
          "value": {
            "type": "number",
            "minimum": -1,
            "maximum": 1,
            "description": "Нормализованное значение метрики. Для magnitude: [0..1], для directional: [-1..1]."
          },
          "unit": {
            "type": ["string", "null"],
            "description": "Единица измерения (если применимо), например: %, USD, hours"
          },
          "errorMargin": {
            "type": ["object", "null"],
            "properties": {
              "low": { "type": "number" },
              "high": { "type": "number" },
              "value": { "type": "number" }
            },
            "additionalProperties": false,
            "description": "Интервал ошибки (опционально)"
          },
          "confidence": {
            "type": "number",
            "minimum": 0,
            "maximum": 1,
            "description": "Уверенность в значении (0..1)"
          },
          "observedAt": {
            "type": "string",
            "format": "date-time",
            "description": "Время наблюдения/измерения"
          },
          "provenance": {
            "type": "object",
            "properties": {
              "sourceIds": { "type": "array", "items": { "type": "string" } },
              "sourceRefs": {
                "type": "array",
                "items": {
                  "type": "object",
                  "properties": {
                    "sourceId": { "type": "string" },
                    "sourceType": { "type": "string" }
                  },
                  "required": ["sourceId", "sourceType"],
                  "additionalProperties": false
                }
              },
              "observedAt": { "type": "string", "format": "date-time" },
              "method": { "type": "string" }
            },
            "additionalProperties": true,
            "description": "Прослеживаемость источника данных (опционально)"
          }
        },
        "additionalProperties": false
      },
      "description": "Метрики состояния узла. Значения представлены объектами MetricValue (value, confidence, observedAt, ...)."
    },
    "homeLocation": {
      "type": ["object", "null"],
      "properties": {
        "name": { "type": "string" },
        "iso6709": { "type": "string", "description": "Координаты ISO 6709" },
        "iso3166_1": { "type": "string", "description": "ISO 3166-1 alpha-2" },
        "iso3166_2": { "type": "string", "description": "ISO 3166-2" }
      },
      "additionalProperties": false,
      "description": "Локация (ISO 3166-1/3166-2/6709)"
    },
    "kbRefs": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "kb": { "type": "string" },
          "externalId": { "type": "string" },
          "confidence": { "type": "number", "minimum": 0, "maximum": 1 },
          "lastSync": { "type": "string", "format": "date-time" }
        },
        "required": ["kb", "externalId", "confidence"],
        "additionalProperties": false
      }
    },
    "identifiers": {
      "type": "object",
      "additionalProperties": { "type": "string" },
      "description": "Идентификаторы внешних систем (scheme -> value), например ISIN/LEI/Ticker"
    },
    "provenance": {
      "type": "object",
      "description": "Прослеживаемость для узла в целом (опционально)",
      "additionalProperties": true
    },
    "vectorText": {
      "type": "string",
      "description": "Текст для семантического поиска (векторизуется)"
    },
    "vectorEmbedding": {
      "type": "array",
      "items": { "type": "number" },
      "description": "Векторное представление vectorText (для Firestore Vector Search / Vertex AI embeddings)"
    },
    "vectorModel": { "type": "string" },
    "vectorVersion": { "type": "string" },
    "metadata": {
      "type": "object",
      "required": ["createdAt", "lastUpdatedAt", "lastVerifiedAt", "epistemicStatus"],
      "properties": {
        "createdAt": {
          "type": "string",
          "format": "date-time"
        },
        "lastUpdatedAt": {
          "type": "string",
          "format": "date-time"
        },
        "lastVerifiedAt": {
          "type": "string",
          "format": "date-time"
        },
        "sourceFrameId": {
          "type": "string",
          "format": "uuid"
        },
        "epistemicStatus": {
          "type": "string",
          "enum": ["VERIFIED", "REJECTED", "LOW_CONFIDENCE"]
        }
      }
    },
    "governance": {
      "type": "object",
      "required": ["owner", "activeSubscribers", "status"],
      "properties": {
        "owner": {
          "type": "string",
          "enum": ["SYSTEM"]
        },
        "activeSubscribers": {
          "type": "integer",
          "minimum": 0
        },
        "status": {
          "type": "string",
          "enum": ["ACTIVE", "DORMANT", "ARCHIVED"]
        }
      }
    },
    "version": {
      "type": "integer",
      "minimum": 1
    }
  }
}
```

### RefreshFragmentRequest

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "strategy": {
      "type": "string",
      "enum": ["Snapshot", "DeepDive"],
      "default": "Snapshot",
      "description": "Стратегия разведки"
    },
    "maxCost": {
      "type": "number",
      "minimum": 0,
      "description": "Максимальная стоимость разведки"
    }
  },
  "additionalProperties": false
}
```

## DependencyEdge Schemas

### CreateEdgeRequest

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["sourceFragmentId", "targetFragmentId", "type", "properties"],
  "properties": {
    "sourceFragmentId": {
      "type": "string",
      "format": "uuid",
      "description": "ID исходного узла (который влияет)"
    },
    "targetFragmentId": {
      "type": "string",
      "format": "uuid",
      "description": "ID целевого узла (на который влияют)"
    },
    "type": {
      "type": "string",
      "enum": ["SUPPLIES_TO", "LOCATED_IN", "OWNS", "PRODUCES", "OPERATES_IN", "REGULATES", "COMPETES_WITH", "DEPENDS_ON", "INFLUENCES"],
      "description": "Тип зависимости"
    },
    "properties": {
      "type": "object",
      "required": ["criticality", "amplification"],
      "properties": {
        "criticality": {
          "type": "number",
          "minimum": 0,
          "maximum": 1,
          "description": "Коэффициент критичности (0.0-1.0)"
        },
        "amplification": {
          "type": "number",
          "minimum": 0,
          "description": "Коэффициент усиления/ослабления влияния"
        },
        "latencyHours": {
          "type": "integer",
          "minimum": 0,
          "description": "Задержка распространения влияния в часах"
        }
      },
      "additionalProperties": false
    }
  },
  "additionalProperties": false
}
```

### EdgeResponse

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["id", "tenantId", "sourceFragmentId", "targetFragmentId", "type", "properties", "metadata"],
  "properties": {
    "id": {
      "type": "string",
      "format": "uuid"
    },
    "tenantId": {
      "type": "string",
      "format": "uuid"
    },
    "sourceFragmentId": {
      "type": "string",
      "format": "uuid"
    },
    "targetFragmentId": {
      "type": "string",
      "format": "uuid"
    },
    "type": {
      "type": "string",
      "enum": ["SUPPLIES_TO", "LOCATED_IN", "OWNS", "PRODUCES", "OPERATES_IN", "REGULATES", "COMPETES_WITH", "DEPENDS_ON", "INFLUENCES"]
    },
    "properties": {
      "type": "object",
      "properties": {
        "criticality": {
          "type": "number",
          "minimum": 0,
          "maximum": 1
        },
        "amplification": {
          "type": "number",
          "minimum": 0
        },
        "latencyHours": {
          "type": "integer",
          "minimum": 0
        }
      }
    },
    "metadata": {
      "type": "object",
      "properties": {
        "sourceFrameId": {
          "type": "string",
          "format": "uuid"
        },
        "createdAt": {
          "type": "string",
          "format": "date-time"
        },
        "lastUpdatedAt": {
          "type": "string",
          "format": "date-time"
        }
      }
    }
  }
}
```

## Scout Job Schemas

### ScoutingJobResponse

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["id", "status", "targetCanonicalPath", "createdAt"],
  "properties": {
    "id": {
      "type": "string",
      "format": "uuid"
    },
    "status": {
      "type": "string",
      "enum": ["PENDING", "PLANNING", "EXECUTION", "EXTRACTION", "VALIDATION", "RESOLUTION", "VERIFIED", "REJECTED", "FAILED"]
    },
    "targetCanonicalPath": {
      "type": "string",
      "pattern": "^/[a-zA-Z0-9_/-]+$"
    },
    "progress": {
      "type": "number",
      "minimum": 0,
      "maximum": 1,
      "description": "Прогресс выполнения (0.0-1.0)"
    },
    "currentStep": {
      "type": "string",
      "description": "Текущий этап выполнения"
    },
    "steps": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": {
            "type": "string"
          },
          "status": {
            "type": "string",
            "enum": ["pending", "running", "completed", "failed"]
          },
          "startedAt": {
            "type": "string",
            "format": "date-time"
          },
          "completedAt": {
            "type": "string",
            "format": "date-time"
          }
        }
      }
    },
    "createdAt": {
      "type": "string",
      "format": "date-time"
    },
    "estimatedCompletion": {
      "type": "string",
      "format": "date-time"
    },
    "completedAt": {
      "type": "string",
      "format": "date-time"
    }
  }
}
```

## Propagation Schemas

### PropagationStatusResponse

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["fragmentId", "isPropagating"],
  "properties": {
    "fragmentId": {
      "type": "string",
      "format": "uuid"
    },
    "isPropagating": {
      "type": "boolean",
      "description": "Идет ли в данный момент распространение изменений"
    },
    "lastPropagation": {
      "type": "object",
      "properties": {
        "startedAt": {
          "type": "string",
          "format": "date-time"
        },
        "completedAt": {
          "type": "string",
          "format": "date-time"
        },
        "nodesUpdated": {
          "type": "integer",
          "minimum": 0,
          "description": "Количество обновленных узлов"
        },
        "lensesRecalculated": {
          "type": "integer",
          "minimum": 0,
          "description": "Количество пересчитанных Lens"
        }
      }
    }
  }
}
```

## Error Schemas

### ErrorResponse

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["error", "message"],
  "properties": {
    "error": {
      "type": "string",
      "description": "Код ошибки"
    },
    "message": {
      "type": "string",
      "description": "Сообщение об ошибке"
    },
    "details": {
      "type": "object",
      "description": "Дополнительные детали ошибки",
      "additionalProperties": true
    },
    "requestId": {
      "type": "string",
      "format": "uuid",
      "description": "ID запроса для отслеживания"
    }
  }
}
```

## WebSocket Event Schemas

### WebSocketEvent

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["type"],
  "properties": {
    "type": {
      "type": "string",
      "enum": [
        "lens.updated",
        "horizon_fragment.updated",
        "scout.job.progress",
        "propagation.started",
        "propagation.completed"
      ]
    },
    "data": {
      "type": "object",
      "description": "Данные события (структура зависит от типа события)",
      "additionalProperties": true
    }
  }
}
```

### LensUpdatedEvent

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["type", "lensId", "data"],
  "properties": {
    "type": {
      "type": "string",
      "const": "lens.updated"
    },
    "lensId": {
      "type": "string",
      "format": "uuid"
    },
    "data": {
      "type": "object",
      "properties": {
        "metrics": {
          "type": "object",
          "properties": {
            "overallRisk": {
              "type": "number",
              "minimum": 0,
              "maximum": 1
            },
            "criticalNodes": {
              "type": "integer",
              "minimum": 0
            }
          }
        },
        "updatedAt": {
          "type": "string",
          "format": "date-time"
        }
      }
    }
  }
}
```

## Валидация

Все схемы соответствуют JSON Schema Draft 07 и могут быть использованы для валидации запросов и ответов.

### Примеры использования

**Валидация запроса создания Lens**:
```go
schema := loadSchema("CreateLensRequest")
if err := validateJSON(requestBody, schema); err != nil {
    return err
}
```

**Валидация ответа**:
```go
schema := loadSchema("LensResponse")
if err := validateJSON(responseBody, schema); err != nil {
    log.Error("Invalid response format", "error", err)
}
```

## См. также

- [API Endpoints](endpoints.md)
- [Архитектурный обзор](../architecture/overview.md)

