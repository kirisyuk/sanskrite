# Agent System Architecture

## Overview

Система агентов с capabilities-based подходом, где агенты выполняют задачи через последовательность шагов (capabilities). Каждый шаг может быть критичным или некритичным, что определяет поведение при ошибках.

## Core Concepts

### AgentJob
Сущность, представляющая задачу агента с историей выполнения шагов.

**Firestore Collection**: `agent_jobs`

**Индексы**:
1. `(tenantId, projectId, createdAt DESC)` - для списка jobs проекта
2. `(tenantId, status, createdAt DESC)` - для списка jobs по статусу
3. `(tenantId, projectId, status, createdAt DESC)` - для списка running jobs проекта

### AgentStep
Один шаг выполнения агента на основе capability.

**Поля**:
- `capability` - тип capability (например, "document_read")
- `status` - статус выполнения
- `input` / `output` - входные и выходные данные
- `dependsOn` - зависимости от других шагов
- `isCritical` - критичность (сбой критичного шага останавливает job)

### Capability
Тип шага, который может выполнить агент.

**Критичные** (сбой = остановка job):
- `document_read` - чтение документов
- `document_write` - запись документов

**Некритичные** (сбой = пропуск шага, продолжение):
- `data_search` - поиск данных
- `data_enhancement` - улучшение данных
- `web_search` - поиск через Tavily
- `llm_generate` - генерация через LLM
- `llm_analyze` - анализ через LLM
- `entity_extract` - извлечение сущностей
- `entity_search` - поиск сущностей с дедупликацией
- `entity_deduplicate` - дедупликация сущностей
- `entity_relation_build` - построение связей между сущностями
- `event_create` - создание событий
- `artifact_create` - создание артефактов
- `bigquery_query` - запросы к BigQuery

## Architecture Components

### 1. CapabilityRegistry
Реестр исполнителей capabilities. Регистрирует `CapabilityExecutor` для каждого типа capability.

### 2. AgentOrchestrator
Управляет выполнением AgentJob:
- Определяет готовые к выполнению шаги (на основе зависимостей)
- Выполняет шаги последовательно/параллельно
- Реагирует на ошибки:
  - Критичный шаг упал → остановить job (status = failed)
  - Некритичный шаг упал → пропустить, продолжить (status = partial)
- Обновляет прогресс и отправляет обновления через WebSocket

### 3. CapabilityExecutor
Интерфейс для выполнения capability:
- `Capability()` - возвращает тип capability
- `ValidateInput()` - валидирует входные данные
- `Execute()` - выполняет шаг с контекстом

## Execution Flow

1. **Создание Job**: `orchestrator.CreateJob()` создает AgentJob со статусом `pending`
2. **Планирование шагов**: Агент планирует шаги на основе задачи (может быть динамически)
3. **Выполнение**: `orchestrator.ExecuteJob()`:
   - Находит готовые шаги (все зависимости выполнены)
   - Выполняет шаги через соответствующий `CapabilityExecutor`
   - Обновляет статусы шагов и прогресс job
   - Отправляет обновления через WebSocket
4. **Завершение**: Определяет финальный статус на основе результатов шагов

## WebSocket Updates

Обновления отправляются через событие `agent.update`:

```json
{
  "type": "agent.update",
  "data": {
    "taskId": "job_id",
    "agentId": "document_preparation",
    "agentName": "Document Preparation",
    "projectId": "project_id",
    "stage": "running",
    "progress": 0.5,
    "message": "Step 2/3: Generating improvements",
    "timestamp": 1234567890,
    "error": null
  }
}
```

## Firestore Indexes Required

Создать следующие составные индексы в Firestore:

1. **agent_jobs** collection:
   - `tenantId` (Ascending) + `projectId` (Ascending) + `createdAt` (Descending)
   - `tenantId` (Ascending) + `status` (Ascending) + `createdAt` (Descending)
   - `tenantId` (Ascending) + `projectId` (Ascending) + `status` (Ascending) + `createdAt` (Descending)

## Usage Example

```go
// Создать orchestrator
orchestrator := agents.NewAgentOrchestrator(jobsRepo, capabilityRegistry, publisher, logger).
    WithRepositories(coreDocsRepo, documentsRepo, entitiesRepo, eventsRepo, interactionsRepo)

// Создать job
job, _ := orchestrator.CreateJob(ctx, tenantID, projectID, userID, "document_preparation", task, true)

// Добавить шаги
job.Steps = []models.AgentStep{
    {
        ID: "step1",
        Capability: "document_read",
        Status: models.AgentStepStatusPending,
        Input: map[string]interface{}{"documentIds": []string{"doc1"}},
        IsCritical: true,
    },
    // ... другие шаги
}

// Выполнить job
orchestrator.ExecuteJob(ctx, job.ID, onUpdate)
```

## Testing

Тестовые сценарии в `backend/cmd/test-agent-orchestrator/main.go`:
1. Простой job с 3 шагами
2. Job с зависимостями между шагами
3. Job с некритичным шагом (должен продолжить при ошибке)

## Available Capability Executors

### Basic Executors (`capability_executors.go`)
- **DocumentReadExecutor** - чтение документов из CoreDocs
- **DocumentWriteExecutor** - запись/обновление документов
- **LLMGenerateExecutor** - генерация текста через LLM

### Advanced Executors (`capability_executors_advanced.go`)
- **WebSearchExecutor** - поиск через Tavily API (требует TavilySearchClient)
- **BigQueryQueryExecutor** - выполнение SQL запросов к BigQuery
- **LLMAnalyzeExecutor** - структурированный анализ через LLM (GenerateJSONStrict)
- **DataSearchExecutor** - поиск данных в документах проекта через Vertex AI Search

### Ontology Executors (`capability_executors_ontology.go`)
- **EntitySearchExecutor** - поиск сущностей с автоматической дедупликацией (использует EntityResolver)
- **EntityDeduplicateExecutor** - дедупликация существующих сущностей с использованием LLM
- **EntityRelationBuildExecutor** - построение графа связей между сущностями до указанной глубины

## Integration Notes

### Tavily Client Integration
Для использования `web_search` capability необходимо:
1. Создать адаптер для `httphandlers.TavilyClient`:
```go
type tavilyClientAdapter struct {
    client *httphandlers.TavilyClient
}
func (a *tavilyClientAdapter) Search(ctx context.Context, query string, maxResults int) (*agents.TavilySearchResponse, error) {
    // Адаптировать вызов
}
```
2. Зарегистрировать executor:
```go
tavilyClient := httphandlers.GetTavilyClientFromEnv()
if tavilyClient != nil {
    adapter := &tavilyClientAdapter{client: tavilyClient}
    registry.Register(agents.NewWebSearchExecutor(adapter, logger))
}
```

### Avoiding Circular Imports
Интерфейс `TavilySearchClient` определен в `agents` пакете для избежания циклических импортов между `agents` и `http` пакетами.

