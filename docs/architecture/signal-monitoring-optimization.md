# Signal Monitoring Optimization

## Обзор

Система мониторинга сигналов была оптимизирована для снижения стоимости, повышения производительности и улучшения качества анализа.

## Проблемы предыдущей реализации

### 1. Избыточные LLM вызовы
**До оптимизации:** 3 LLM вызова на каждый сигнал
- `analyzeSearchResults()` - первичный анализ
- `verifyFindingsRelevance()` - проверка релевантности
- `deduplicateFindings()` - дедупликация

**Проблемы:**
- Утроение стоимости обработки
- Увеличение времени обработки в 3 раза
- Избыточность: первый промпт уже содержал инструкции для фильтрации

### 2. Отсутствие structured output
- Использовался `SimpleJSONVerifier` вместо `StrictJSONEnabled + JSONSchema`
- Менее надежно: модель могла не следовать схеме
- Отсутствие валидации на уровне провайдера

### 3. Отсутствие параметризации
- Hardcoded пороги: `Confidence < 0.5`
- Нет настройки режимов (strict/relaxed)
- Нет конфигурации через environment variables

## Оптимизированная реализация

### 1. Единый LLM вызов с Structured Output

**После оптимизации:** 1 LLM вызов на сигнал

```go
// Используется gen_combined.GenAIRequest с StrictJSONEnabled
req := gen_combined.GenAIRequest{
    Model:             "gemini_fast",
    Prompt:            prompt,
    Temperature:       0.3,
    MaxOutputTokens:   2048,
    StrictJSONEnabled: true,
    JSONSchema:        schema,
}
```

**Преимущества:**
- ✅ Экономия 66% стоимости (1 вызов вместо 3)
- ✅ Ускорение в 3 раза
- ✅ Гарантированная валидация JSON на уровне провайдера
- ✅ Более надежный парсинг результатов

### 2. Chain-of-Thought Reasoning

Новая структура результата включает явные шаги рассуждения:

```go
type SignalAnalysisResult struct {
    Reasoning    ReasoningSteps `json:"reasoning"`
    Findings     []Finding      `json:"findings"`
    Summary      string         `json:"summary"`
    ContentDelta string         `json:"contentDelta"`
    Confidence   float64        `json:"confidence"`
}

type ReasoningSteps struct {
    RelevanceCheck      string // Как результаты связаны с сигналом
    NoveltyCheck        string // Что нового относительно предыдущего снимка
    ConfidenceRationale string // Почему такой confidence score
}

type Finding struct {
    Text           string      // Текст находки
    RelevanceScore float64     // 0-1: релевантность сигналу
    IsNew          bool        // true если не в предыдущем снимке
    SourceRefs     []SourceRef // Источники
}
```

**Преимущества:**
- Прозрачность: видно, как модель пришла к выводу
- Качество: chain-of-thought улучшает точность анализа
- Отладка: легче понять, почему модель отфильтровала/включила находку

### 3. Параметризация через Environment Variables

```bash
# Минимальный relevance score для включения находки (default: 0.6)
SIGNAL_MIN_RELEVANCE_SCORE=0.7

# Минимальный confidence для создания снимка (default: 0.5)
SIGNAL_MIN_CONFIDENCE=0.6

# Интервал проверки сигналов (default: 6h)
SIGNAL_MONITORING_INTERVAL=4h
```

**Программная настройка:**

```go
signalMonitoringService.
    WithRelevanceThreshold(0.7).
    WithConfidenceThreshold(0.6).
    WithCheckInterval(4 * time.Hour)
```

### 4. Fallback на AITaskExecutor

Если `genaiClient` не доступен, система автоматически переключается на `AITaskExecutor`:

```go
if s.genaiClient != nil {
    return s.analyzeWithStructuredOutput(ctx, ...)
}
// Fallback to TaskExecutor (legacy mode)
return s.analyzeWithTaskExecutor(ctx, ...)
```

## Структура JSON Schema

```json
{
  "type": "object",
  "properties": {
    "reasoning": {
      "type": "object",
      "properties": {
        "relevanceCheck": { "type": "string" },
        "noveltyCheck": { "type": "string" },
        "confidenceRationale": { "type": "string" }
      },
      "required": ["relevanceCheck", "noveltyCheck", "confidenceRationale"]
    },
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "text": { "type": "string" },
          "relevanceScore": { 
            "type": "number", 
            "minimum": 0, 
            "maximum": 1 
          },
          "isNew": { "type": "boolean" },
          "sourceRefs": { "type": "array" }
        },
        "required": ["text", "relevanceScore", "isNew", "sourceRefs"]
      }
    },
    "summary": { "type": "string" },
    "contentDelta": { "type": "string" },
    "confidence": { 
      "type": "number", 
      "minimum": 0, 
      "maximum": 1 
    }
  },
  "required": ["reasoning", "findings", "summary", "contentDelta", "confidence"]
}
```

## Enhanced Prompt Structure

Промпт теперь структурирован в 4 этапа:

```
=== SIGNAL DEFINITION ===
Signal Name, Text, Search Queries

=== SEARCH RESULTS ===
Formatted search results (local + Tavily)

=== PREVIOUS SNAPSHOT FINDINGS ===
Already captured findings (if exists)

=== TASK ===
1. RELEVANCE CHECK
   - Assign relevance score (0-1)
   - Only include if score >= threshold

2. NOVELTY CHECK
   - Compare semantically with previous findings
   - Mark isNew=true only for genuinely new info

3. EXTRACTION
   - Extract relevant AND new findings
   - Include source references

4. CONFIDENCE
   - Based on source quality, clarity, evidence
```

## Фильтрация результатов

Фильтрация происходит на двух уровнях:

### 1. В промпте (инструкции для LLM)
```
- ONLY include findings with relevance score >= 0.6
- Mark isNew=true ONLY if it adds genuinely new factual information
```

### 2. Post-processing (код)
```go
filteredFindings := make([]Finding, 0)
for _, finding := range analysisResult.Findings {
    if finding.RelevanceScore >= s.minRelevanceScore && finding.IsNew {
        filteredFindings = append(filteredFindings, finding)
    }
}
```

## Хранение результатов

ParseMeta содержит как legacy, так и новые поля:

```go
parseMeta := map[string]interface{}{
    "signalId":     signal.ID,
    "capturedAt":   now.Format(time.RFC3339),
    "summary":      analysisResult.Summary,
    "keyFindings":  keyFindings,          // Legacy: []string
    "findings":     analysisResult.Findings, // New: []Finding
    "sources":      sources,
    "contentDelta": analysisResult.ContentDelta,
    "matchCount":   len(analysisResult.Findings),
    "confidence":   analysisResult.Confidence,
    "reasoning":    analysisResult.Reasoning, // Chain-of-thought
}
```

## Метрики производительности

### Экономия стоимости
- **До:** 3 LLM вызова × 100 сигналов = 300 вызовов каждые 6 часов
- **После:** 1 LLM вызов × 100 сигналов = 100 вызовов каждые 6 часов
- **Экономия:** 66% стоимости

### Ускорение обработки
- **До:** ~3-6 секунд на сигнал (3 последовательных вызова)
- **После:** ~1-2 секунды на сигнал (1 вызов)
- **Ускорение:** 2-3x

### Качество
- **Chain-of-thought:** Улучшает точность анализа
- **Structured output:** Устраняет ошибки парсинга
- **Explicit reasoning:** Повышает прозрачность решений

## Миграция

Для существующих систем:

1. **Обновить environment variables** (опционально):
   ```bash
   SIGNAL_MIN_RELEVANCE_SCORE=0.6
   SIGNAL_MIN_CONFIDENCE=0.5
   ```

2. **Убедиться, что genaiClient доступен**:
   ```go
   signalMonitoringService.WithGenAIClient(genaiClient)
   ```

3. **Старые снимки совместимы**:
   - Новая система читает `keyFindings` (legacy)
   - Новые снимки содержат оба формата

## Рекомендации

### Production настройки
```bash
SIGNAL_MIN_RELEVANCE_SCORE=0.7  # Строже для production
SIGNAL_MIN_CONFIDENCE=0.6       # Выше порог для создания снимков
SIGNAL_MONITORING_INTERVAL=6h   # Стандартный интервал
```

### Development/Testing
```bash
SIGNAL_MIN_RELEVANCE_SCORE=0.5  # Мягче для тестирования
SIGNAL_MIN_CONFIDENCE=0.4       # Ниже порог
SIGNAL_MONITORING_INTERVAL=6h   # Стандартный интервал (как в production)
```

### Мониторинг

Логи содержат полезную информацию:

```
[SignalMonitoring] Analysis complete: 5 relevant + new findings (confidence: 0.85)
[SignalMonitoring] Low confidence (0.42 < 0.50) for signal abc123, skipping snapshot
[SignalMonitoring] All findings filtered out (relevance < 0.60 or not new) for signal xyz789
```

## Будущие улучшения

1. **Batch processing**: Обработка нескольких сигналов в одном LLM вызове
2. **Adaptive thresholds**: Автоматическая настройка порогов на основе истории
3. **Semantic caching**: Кэширование похожих запросов для экономии
4. **Multi-model ensemble**: Использование нескольких моделей для критичных сигналов

