## GenAI (Gemini) — интеграция фронтенда

### REST API
- Базовый URL: `VITE_API_BASE_URL` (по умолчанию `http://localhost:8080`).
- Токен: Firebase ID Token в заголовке `Authorization: Bearer <token>`.

Методы клиента (см. `web/src/lib/api.ts`):
- `genaiAdhocJSON({ prompt, options })` → `POST /api/v1/genai/adhoc-json`
- `genaiComplete({ prompt, tools?, options? })` → `POST /api/v1/genai/complete`
- `genaiCompleteJSON({ prompt, tools?, jsonSchema?, options? })` → `POST /api/v1/genai/complete-json`
- `genaiCompleteWithTools({ prompt, tools, options? })` → `POST /api/v1/genai/complete-with-tools`
- `genaiStream({ prompt, tools?, options? })` → `POST /api/v1/genai/stream` (ответы приходят через WS)

`options` поддерживает:
```ts
type GenAIOptions = {
  profile?: 'fast' | 'quality';
  modelOverride?: string;
  groundingEnabled?: boolean;
  temperature?: number;
  maxOutputTokens?: number;
  systemInstruction?: string;
  responseFormat?: 'text' | 'json';
}
```

### WebSocket события (WS → Hub)
- Эндпоинт: `GET /ws?token=<FirebaseIDToken>`.
- События:
  - `genai.stream.start` `{ ts }`
  - `genai.stream.chunk` `{ text }`
  - `genai.stream.tool_call` `{ toolCalls: {name,arguments}[] }`
  - `genai.stream.tool_result` `{ toolCalls: {name,arguments}[] }`
  - `genai.stream.done` `{ ts }`
  - `genai.stream.error` `{ error }`

Фронтенд-обработка (см. `web/src/stores/discoveryStore.ts`):
```12:40:web/src/stores/discoveryStore.ts
if (typeof msg?.type === 'string' && msg.type.startsWith('genai.stream.')) {
  import('./genaiStore').then(({ useGenaiStore }) => {
    const s = useGenaiStore.getState();
    switch (msg.type) {
      case 'genai.stream.start': s.reset(); break;
      case 'genai.stream.chunk': useGenaiStore.setState((st) => ({ chunks: [...st.chunks, { text: msg.data.text }] })); break;
      case 'genai.stream.tool_call': useGenaiStore.setState({ toolCalls: msg.data.toolCalls }); break;
      case 'genai.stream.tool_result': break;
      case 'genai.stream.done': useGenaiStore.setState({ streaming: false }); break;
      case 'genai.stream.error': useGenaiStore.setState({ streaming: false, error: msg.data?.error || 'Stream error' }); break;
    }
  });
}
```

### Zustand store
- `web/src/stores/genaiStore.ts`:
  - `startStream({ prompt, tools?, options? })` — триггерит POST /genai/stream
  - `chunks[]` — текстовые куски ответа
  - `toolCalls[]` — текущие запросы инструментов (если модель их просит)
  - `adhocJSON({ prompt, options? })` — вызывает `/genai/adhoc-json`

### Декларация инструмента Telegram на фронте
- `web/src/lib/genaiTools.ts` экспортирует:
```ts
export const TelegramFetchUpdatesTool = {
  name: 'telegram_fetch_updates',
  description: 'Fetch recent messages from a public Telegram channel by username',
  parameters: {
    type: 'object',
    properties: {
      channel: { type: 'string', description: 'Channel reference: @name or t.me/name' },
      lastId: { type: 'integer', minimum: 0 },
      limit: { type: 'integer', minimum: 1, maximum: 100 },
    },
    required: ['channel'],
  },
} as const;
```

Пример включения инструмента в стрим-запрос:
```ts
import { api } from '../../lib/api';
import { TelegramFetchUpdatesTool } from '../../lib/genaiTools';

await api.genaiStream({
  prompt: 'Найди последние 5 постов канала и сделай сводку',
  tools: [TelegramFetchUpdatesTool],
  options: { profile: 'quality', groundingEnabled: false },
});
```

### Пример UI-потока
1) Пользователь вводит промпт, выбирает `profile/modelOverride`, включает `groundingEnabled`.
2) Кнопка “Stream” → `useGenaiStore.getState().startStream(...)` → слушаем WS события для инкрементального рендера.
3) Кнопка “Adhoc JSON” → `genaiAdhocJSON` → если `validJson`, показываем как структурированный результат, иначе `raw`.

## Realtime and API

### Realtime
- WebSocket client: `web/src/lib/ws.ts`
- Consumers: pages and stores subscribe to updates for discovery/profiling

### API
- REST calls from components/pages to backend handlers under `backend/internal/http/*`

### Auth
- Auth context and guard: `web/src/contexts/AuthContext.tsx`, `web/src/components/AuthGuard.tsx`


