## GenAI (Gemini on Vertex AI) — архитектура модуля

### Зачем
- Отдельный изолированный контур работы с Gemini (Vertex AI), без зависимостей от текущих `task executor` и `model manager`.
- Две модели: быстрые ответы и качественные с grounding:
  - fast: `gemini-2.5-flash-lite`
  - quality: `gemini-2.5-pro` (+ опциональный Google Search Grounding)
- Поддержка tool/function calling, в т.ч. в стриминге.
- Чистый HTTP-слой и интеграция с существующим realtime (Pub/Sub → WS) без засорения старых слоёв.

### Ключевые компоненты
- Пакет `internal/genai/gemini/`:
  - `client.go` — изолированный Gemini-клиент:
    - `Generate(...)` — синхронная генерация (text/JSON, grounding, tools)
    - `GenerateJSON(...)` — принудительный JSON-режим, опц. `responseSchema`
    - `Stream(...)` и `StreamConversation(...)` — реальный `streamGenerateContent` с инкрементальными событиями
    - Формат стрим-событий: `start`, `chunk`, `tool_call`, `tool_result`, `done`, `error`
  - `tools.go` — минимальный исполнятель инструментов:
    - `ToolExecutor` интерфейс, `SimpleRegistry` (in-memory)
    - Встроенные безопасные tools: `time_now`, `echo`
    - `ExecuteWithTools(...)` — итеративная схема (до 4 шагов)
    - `StreamWithTools(...)` — стрим + один «круг» инструментов, затем продолжение стрима

Код-референсы (фрагменты):
```1:20:backend/internal/genai/gemini/client.go
package gemini

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
)
```

```60:80:backend/internal/genai/gemini/client.go
type GenerateResponse struct {
    Content      string        `json:"content"`
    ToolCalls    []ToolCall    `json:"toolCalls,omitempty"`
    IsToolStep   bool          `json:"isToolStep"`
    Usage        Usage         `json:"usage"`
    Attributions []Attribution `json:"attributions,omitempty"`
    Model        string        `json:"model"`
}
```

```134:172:backend/internal/genai/gemini/tools.go
func NewSimpleRegistry() *SimpleRegistry {
    r := &SimpleRegistry{allowed: map[string]func(context.Context, json.RawMessage) (json.RawMessage, error){}}
    r.Register("time_now", ...)
    r.Register("echo", ...)
    return r
}
```

### HTTP API (временные и постоянные)
См. фронтенд-интеграцию и примеры вызовов: [Frontend · GenAI Realtime & API](../../frontend/realtime-and-api.md).
- Хэндлер: `internal/http/genai.go`
- Маршруты (см. `cmd/server/main.go`):
  - POST `/api/v1/genai/complete` — синхронная генерация (text/JSON)
  - POST `/api/v1/genai/complete-json` — принудительный JSON-режим, опционально `jsonSchema`
  - POST `/api/v1/genai/complete-with-tools` — tool-loop (минимальный runtime)
  - POST `/api/v1/genai/stream` — реальный стрим (→ Pub/Sub → WS)
  - POST `/api/v1/genai/adhoc-json` — временный endpoint для FE, «произвольный промпт → JSON»

Формат запроса (общий):
```json
{
  "prompt": "...",
  "tools": [
    {"name": "echo", "description": "...", "parameters": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"]}}
  ],
  "options": {
    "profile": "fast | quality",
    "modelOverride": "gemini-2.5-pro | gemini-2.5-flash-lite",
    "groundingEnabled": true,
    "temperature": 0.2,
    "maxOutputTokens": 1024,
    "systemInstruction": "...",
    "responseFormat": "text | json"
  },
  "jsonSchema": {"type": "object", ...} // только для complete-json
}
```

`/adhoc-json` (временный):
- Принудительно устанавливает JSON-режим без схемы; возвращает:
  - `ok`, `model`, `usage`, `validJson`, `json`, `raw`
- Предназначен для фронтовых экспериментов, позже будет удалён.

### Стриминг и realtime
Как фронтенд подписывается и обрабатывает события `genai.stream.*`: [Frontend · GenAI Realtime & API](../../frontend/realtime-and-api.md).
- Клиент вызывает `models/{model}:streamGenerateContent` и парсит чанки/вызовы tools «на лету».
- HTTP-слой транслирует события в Pub/Sub с типами (см. `internal/realtime/pubsub.go`):
  - `genai.stream.start`, `genai.stream.chunk`, `genai.stream.tool_call`, `genai.stream.tool_result`, `genai.stream.done`, `genai.stream.error`
- Существующий `Subscriber` уже подписывается и ретранслирует в WS-хаб по тенанту.

Код-референсы событий:
```15:31:backend/internal/realtime/pubsub.go
const (
    EventGenAIStreamStart   EventType = "genai.stream.start"
    EventGenAIStreamChunk   EventType = "genai.stream.chunk"
    EventGenAIStreamTool    EventType = "genai.stream.tool_call"
    EventGenAIStreamToolRes EventType = "genai.stream.tool_result"
    EventGenAIStreamDone    EventType = "genai.stream.done"
    EventGenAIStreamError   EventType = "genai.stream.error"
)
```

### Конфигурация/ENV
- `VERTEX_API_KEY` — API-ключ Gemini (как у существующего контура)
- `GCP_PROJECT_ID` / `GOOGLE_CLOUD_PROJECT` — проект
- `VERTEX_LOCATION` — регион (по умолчанию `us-central1`)
- `GENAI_FAST_MODEL` — override для fast-модели (опционально)
- `GENAI_QUALITY_MODEL` — override для quality-модели (опционально)

### Безопасность и ограничения (на старте)
- Аутентификация — как у всех `/api/v1/*` (Firebase ID Token). Тенант определяется middleware через `UsersRepository`.
- Tool runtime — минимальный. Защита: таймаут на вызов, white-list по имени инструмента, max depth в итеративном режиме.
- Ограничения (QPS, ретраи, CB, бюджет) — планируются в следующих итерациях.

### Будущее расширение
- Настоящая строгая валидация входов инструментов по JSON Schema
- Больше safe-инструментов (ограниченный `web_fetch`, `kv_store` и т.д.)
- Ретраи/бэкоффы, circuit breaker, квоты и backpressure
- Расширенные метрики/трейсинг/аудит, подсчёт стоимости
- SSE endpoint (альтернатива WS-трассировке)

### Примеры
Параллельные примеры использования на фронтенде и структура Zustand стора: [Frontend · GenAI Realtime & API](../../frontend/realtime-and-api.md).
Синхронный JSON без схемы:
```bash
curl -X POST http://localhost:8080/api/v1/genai/adhoc-json \
  -H "Authorization: Bearer <FirebaseIDToken>" -H "Content-Type: application/json" \
  -d '{
    "prompt":"Верни JSON с полями title и items(2 строки).",
    "options":{"profile":"fast","maxOutputTokens":512}
  }'
```

Стрим c инструментами:
```bash
curl -X POST http://localhost:8080/api/v1/genai/stream \
  -H "Authorization: Bearer <FirebaseIDToken>" -H "Content-Type: application/json" \
  -d '{
    "prompt":"Скажи текущее время и повтори фразу Hello",
    "tools":[
      {"name":"time_now","description":"UTC time","parameters":{"type":"object","properties":{}}},
      {"name":"echo","description":"Echo","parameters":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}}
    ],
    "options":{"profile":"fast"}
  }'
```


