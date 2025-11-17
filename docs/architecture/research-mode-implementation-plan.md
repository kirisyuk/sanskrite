# План реализации Research режима

> **Примечание:** Детальная спецификация формата Research Interaction находится в [research-interaction-format.md](./research-interaction-format.md)

## Принципы
- ✅ Не ломаем существующий код
- ✅ Минимально инвазивные изменения
- ✅ Апгрейдим только при необходимости
- ✅ Постепенная реализация с тестированием на каждом этапе

## Этап 1: Frontend - Передача флага (1-2 часа)

### 1.1 Обновить API типы
**Файл:** `web/src/lib/api.ts`
- Добавить `researchEnabled?: boolean` в параметры `genaiGenerate()`
- Передавать в body запроса аналогично `webSearchEnabled`

**Изменения:**
```typescript
async genaiGenerate(data: {
  // ... existing fields
  researchEnabled?: boolean;  // NEW
}): Promise<...>
```

### 1.2 Передать researchEnabled из settings
**Файл:** `web/src/stores/genaiChatStore.ts`
- В функции `startStream()` передавать `settings.researchEnabled` в API вызов
- Аналогично как передается `groundingEnabled`

**Место изменения:** ~строка 2999, где вызывается `api.genaiGenerate()`

**Минимальное изменение:** Добавить одну строку в существующий вызов

---

## Этап 2: Backend - Прием флага (30 минут)

### 2.1 Расширить GenAIRequest
**Файл:** `backend/internal/genai/combined/client.go`
- Добавить поле `ResearchEnabled bool` в структуру `GenAIRequest`
- Аналогично существующим `WebSearchEnabled`, `ToolCallingEnabled`

**Изменения:**
```go
type GenAIRequest struct {
    // ... existing fields
    ResearchEnabled bool `json:"researchEnabled,omitempty"`  // NEW
}
```

### 2.2 Обновить HTTP handler
**Файл:** `backend/internal/http/genai.go`
- В `GenAIGenerate()` проверить `req.ResearchEnabled`
- Если `true` → вызвать новый research pipeline
- Если `false` → использовать существующий flow (без изменений)

**Минимальное изменение:** Одна проверка `if req.ResearchEnabled` с новым кодом, иначе fallback на существующий код

---

## Этап 3: Backend - Research Pipeline (5-7 дней)

### 3.1 Создать новую структуру (не трогаем существующий код)
**Новая директория:** `backend/internal/research/`

**Структура:**
```
internal/research/
├── researcher.go          // Главный класс (аналог GPTResearcher)
├── planner.go             // Генерация под-вопросов
├── executor.go            // Параллельный сбор информации
├── scraper.go             // Скрапинг URL (переиспользуем Tavily)
├── context_manager.go     // Управление контекстом
├── report_generator.go    // Генерация финального отчета
└── curator.go             // Фильтрация источников
```

### 3.2 Использовать существующие компоненты
- ✅ `TavilyClient` - уже есть в `backend/internal/http/tavily_client.go`
- ✅ `AITaskExecutor` - для LLM запросов
- ✅ `EmbeddingProvider` - для семантического поиска
- ✅ WebSocket стриминг - для прогресса

### 3.3 Реализация компонентов

#### 3.3.1 Researcher (researcher.go)
```go
type Researcher struct {
    query string
    cfg *Config
    tavilyClient *TavilyClient
    executor *AITaskExecutor
    // ... другие зависимости
}

func (r *Researcher) ConductResearch(ctx context.Context) (*ResearchResult, error) {
    // 1. Планирование (planner)
    // 2. Параллельный сбор (executor)
    // 3. Генерация отчета (report_generator)
}
```

#### 3.3.2 Planner (planner.go)
- Использовать `AITaskExecutor` для генерации под-вопросов
- Один LLM вызов для разбиения запроса на аспекты

#### 3.3.3 Executor (executor.go)
- Параллельный поиск через `TavilyClient` для каждого под-вопроса
- Использовать `goroutines` для параллелизма
- Скрапинг найденных URL (простой HTTP + парсинг)

#### 3.3.4 Context Manager (context_manager.go)
- Использовать существующий `EmbeddingProvider` для семантического поиска
- Фильтрация и ранжирование по релевантности

#### 3.3.5 Report Generator (report_generator.go)
- Использовать `AITaskExecutor` для генерации финального отчета
- Агрегация собранного контекста
- Добавление цитат к источникам

#### 3.3.6 Curator (curator.go)
- Простая фильтрация дубликатов
- Ранжирование источников

### 3.4 Интеграция в GenAIHandler
**Файл:** `backend/internal/http/genai.go`

**Минимальное изменение:**
```go
func (h *GenAIHandler) GenAIGenerate(w http.ResponseWriter, r *http.Request) {
    // ... existing code ...
    
    // NEW: Check for research mode
    if req.ResearchEnabled {
        result, err := h.executeResearchMode(ctx, req)
        if err != nil {
            // Fallback to regular mode or return error
            _ = WriteError(w, http.StatusInternalServerError, "RESEARCH_ERROR", err.Error(), nil)
            return
        }
        _ = WriteJSON(w, http.StatusOK, result)
        return
    }
    
    // Existing code continues unchanged
    resp, err := h.client.GenAIGenerate(ctx, req)
    // ...
}

func (h *GenAIHandler) executeResearchMode(ctx context.Context, req GenAIRequest) (*GenAIResponse, error) {
    // New research pipeline
    researcher := research.NewResearcher(req.Prompt, h.tavilyClient, h.executor)
    result, err := researcher.ConductResearch(ctx)
    // Convert to GenAIResponse format
    return convertToGenAIResponse(result), nil
}
```

---

## Этап 4: Стриминг прогресса (1-2 дня)

### 4.1 WebSocket события для Research
**Использовать существующую инфраструктуру:**
- Существующий WebSocket handler
- Добавить новые типы событий: `research_planning`, `research_searching`, `research_scraping`, `research_generating`

### 4.2 Frontend отображение прогресса
**Файл:** `web/src/stores/genaiChatStore.ts`
- Обработка новых событий из WebSocket
- Отображение этапов исследования в UI

---

## Этап 5: Тестирование и оптимизация (2-3 дня)

### 5.1 Unit тесты
- Тесты для каждого компонента research pipeline
- Моки для внешних зависимостей

### 5.2 Интеграционные тесты
- Тест полного flow от запроса до отчета
- Проверка параллельной обработки

### 5.3 Оптимизация
- Настройка лимитов параллелизма
- Оптимизация промптов для LLM
- Кеширование результатов поиска

---

## Порядок реализации (приоритет)

### Фаза 1: Минимальный MVP (2-3 дня)
1. ✅ Frontend: передача флага (уже сделано)
2. Backend: прием флага и базовая проверка
3. Backend: простой research pipeline (без глубокой рекурсии)
   - Один раунд под-вопросов
   - Фиксированное количество источников (5-10)
   - Простая генерация отчета

### Фаза 2: Полная функциональность (3-4 дня)
4. Backend: улучшенный research pipeline
   - Оптимизация промптов
   - Улучшенная курация источников
   - Поддержка большего количества источников

### Фаза 3: Полировка (1-2 дня)
5. Стриминг прогресса
6. Обработка ошибок
7. Оптимизация производительности

---

## Риски и митигация

### Риск 1: Производительность
- **Митигация:** Лимиты на параллелизм, таймауты, graceful degradation

### Риск 2: Стоимость LLM вызовов
- **Митигация:** Использование быстрых моделей для планирования, умных для генерации

### Риск 3: Качество результатов
- **Митигация:** Агрегация из множества источников, фильтрация низкокачественного контента

---

## Критерии успеха

1. ✅ Research режим не ломает существующий код
2. ✅ Режим работает независимо от web search и tool usage
3. ✅ Генерируются качественные отчеты с цитатами
4. ✅ Прогресс исследования отображается в UI
5. ✅ Производительность приемлемая (2-5 минут на исследование)

---

## Следующие шаги

1. Начать с Фазы 1 - минимальный MVP
2. Тестировать на простых запросах
3. Постепенно добавлять функциональность
4. Оптимизировать по мере необходимости

