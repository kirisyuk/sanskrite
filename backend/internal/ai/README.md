# AI Module - Универсальный модуль для работы с AI моделями

## Обзор

Этот модуль предоставляет полнофункциональную систему для работы с различными AI провайдерами (OpenAI, Claude, Vertex AI/Gemini) с автоматическим выбором моделей, фоллбэк механизмом при ошибках, и системой верификации результатов.

## Основные компоненты

### 1. AIModelManager
Управляет реестром доступных AI-моделей с их характеристиками:
- Автоматический выбор оптимальной модели на основе требований
- Поддержка фильтров (context window, capability score, специальные возможности)
- Расчет стоимости выполнения задач
- Фоллбэк на резервные модели

### 2. AITaskExecutor
Центральный оркестратор для выполнения AI-задач:
- Автоматический retry с переключением на другие модели при ошибках
- Поддержка структурированного вывода с верификацией
- Tool-calling (function calling) поддержка
- Встроенный веб-поиск
- Анализ изображений
- Генерация эмбеддингов

### 3. AI Services (Провайдеры)
Реализации для различных провайдеров:
- **OpenAIService**: GPT-4, GPT-4o, embeddings, vision, search
- **ClaudeService**: Claude Sonnet, Opus
- **GeminiService**: Gemini (через Google AI / generativelanguage.googleapis.com)
- **VertexAIService**: Gemini (через Vertex AI Platform / aiplatform.googleapis.com, с поддержкой Vertex AI Search grounding)

### 4. Система верификаторов
Мощная система проверки и исправления результатов:
- Автоматическое исправление JSON (trailing commas, comments, и т.д.)
- Валидация по схеме
- Кастомные правила валидации
- Возможность автоматического исправления данных

## Архитектура

```
┌─────────────────────────────────────────────────────────┐
│                    AITaskExecutor                       │
│  ┌─────────────────────────────────────────────────┐   │
│  │  1. Выбирает модель через AIModelManager       │   │
│  │  2. Получает провайдера для модели             │   │
│  │  3. Выполняет задачу через провайдер           │   │
│  │  4. Применяет верификатор (если есть)          │   │
│  │  5. При ошибке - fallback на другую модель     │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                          ↓
┌────────────────────┬──────────────────┬──────────────────┐
│   OpenAIService    │  ClaudeService   │  GeminiService   │VertexAIService│
└────────────────────┴──────────────────┴──────────────────┘
                          ↓
┌─────────────────────────────────────────────────────────┐
│                   AIModelManager                        │
│  • 25+ моделей с полными характеристиками              │
│  • Автоматический выбор по требованиям                 │
│  • Фильтрация по capabilities                          │
│  • Оптимизация по цене                                 │
└─────────────────────────────────────────────────────────┘
```

## Основные возможности

### ✅ Автоматический выбор модели
Система автоматически выбирает оптимальную модель на основе:
- Размера контекстного окна
- Требуемых capabilities (text generation, embeddings, vision, search)
- Минимального capability score
- Preference для быстрых моделей (nearly real-time)
- Стоимости (выбирается самая дешевая подходящая модель)

### ✅ Фоллбэк при ошибках
При ошибке выполнения автоматически:
- Выбирается следующая подходящая модель
- Повторяется попытка выполнения
- Поддерживается до 4 fallback попыток
- Логируются все попытки

### ✅ Верификация результатов
Система верификации поддерживает:
- Автоматическую очистку и исправление JSON
- Валидацию структуры данных
- Проверку обязательных полей
- Кастомные правила валидации
- Автоматическое исправление данных (repairable verifiers)

### ✅ Tool-calling (Function calling)
Полная поддержка вызова функций:
- OpenAI function calling
- Anthropic tool use
- Gemini function declarations
- Автоматический цикл вызовов
- Safeguard против бесконечных циклов

### ✅ Специальные возможности
- **Web Search**: встроенный поиск (OpenAI search-preview, Grok)
- **Vision**: анализ изображений (GPT-4o, Claude, Gemini)
- **Embeddings**: генерация векторных представлений (OpenAI embeddings)

## Поддерживаемые модели

### OpenAI
- gpt-4o-mini (128K context, fast, cheap)
- gpt-4.1-mini-2025-04-14 (1M context!)
- gpt-5-mini, gpt-5-nano (400K context)
- gpt-4o-search-preview, gpt-4o-mini-search-preview (с веб-поиском)
- text-embedding-3-large, text-embedding-3-small
- gpt-image-1

### Claude (Anthropic)
- claude-sonnet-4-20250514 (200K context, vision)
- claude-opus-4-1-20250805 (200K context, highest quality)

### Gemini (Vertex AI)
- gemini-2.0-flash (1M context, fast)
- gemini-2.0-flash-lite (1M context, very cheap)
- gemini-2.0-pro (2M context, vision)

### Другие
- deepseek-chat (64K context, very cheap)
- grok-4-0709, grok-3, grok-3-mini (с Live Search)

## Примеры использования

См. файл `example_usage.go` для полных примеров:
- Базовое использование
- Структурированный вывод с верификацией
- Tool-calling
- Кастомные верификаторы
- Выбор моделей с различными требованиями
- Fallback behavior

## Быстрый старт

```go
package main

import (
    "context"
    "log"
    "your-project/internal/ai"
)

func main() {
    // 1. Создаем model manager
    modelManager := ai.NewAIModelManager()
    
    // 2. Создаем AI services
    openAI := ai.NewOpenAIService("your-api-key", modelManager)
    claude := ai.NewClaudeService("your-api-key", modelManager)
    
    // 3. Создаем executor
    executor := ai.NewAITaskExecutor(
        []ai.AIService{openAI, claude},
        modelManager,
    )
    
    // 4. Создаем и выполняем задачу
    task := &ai.AITask{
        TaskType:     ai.TaskTypeSummarization,
        InputContent: "Your text here...",
        AIRequirements: ai.AIRequirements{
            MinContextWindow:         4000,
            MaxOutputSize:            500,
            MinCapabilityScore:       7,
            TextGenerationCapability: true,
        },
    }
    
    result, err := executor.ExecuteTask(context.Background(), task)
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Result: %s\nModel: %s\nCost: $%.6f\n", 
        result.Content, result.Model, result.ProcessingCost)
}
```

## Расширение

### Добавление нового провайдера

1. Создайте новый тип, реализующий интерфейс `AIService`:
```go
type MyAIService struct {
    *BaseAIService
    // ваши поля
}

func (s *MyAIService) Provider() AIProvider {
    return AIProvider("MyProvider")
}

func (s *MyAIService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
    // ваша реализация
}

// ... остальные методы интерфейса
```

2. Добавьте модели в `AIModelManager.initializeModels()`

### Создание кастомного верификатора

```go
type MyVerifier struct {
    ai.GenericVerifier[MyResultType]
}

func NewMyVerifier() *MyVerifier {
    return &MyVerifier{
        GenericVerifier: ai.GenericVerifier[MyResultType]{
            ValidateFunc: func(result MyResultType) error {
                // ваша логика валидации
                return nil
            },
        },
    }
}
```

## Лучшие практики

1. **Используйте верификаторы** для структурированного вывода - это обеспечит автоматический retry при некорректном JSON
2. **Указывайте MinCapabilityScore** чтобы избежать использования слабых моделей для сложных задач
3. **Используйте MaxOutputSize** для контроля стоимости
4. **Для embedding задач** явно указывайте `EmbeddingGenerationCapability: true`
5. **Для vision задач** используйте `ImageAnalysisCapability: true`
6. **Для поиска** используйте `BuiltInSearchCapability: true`

## Телеметрия

Модуль готов к интеграции с системой телеметрии (в текущей версии телеметрия заглушена, как и было запрошено). 
Для включения телеметрии добавьте реализацию `ITelemetryService`.

## Безопасность

- API ключи передаются в конструкторы сервисов - используйте переменные окружения
- Все HTTP запросы используют context для таймаутов
- Safeguard механизмы предотвращают бесконечные циклы tool-calling

## Производительность

- Все HTTP клиенты используют пулы соединений
- Таймауты настроены на 120 секунд
- Automatic retry с exponential backoff (можно добавить при необходимости)
- Кэширование результатов можно добавить через inputHash

## Лицензия

(Ваша лицензия)

## Контакты

(Ваши контакты)

