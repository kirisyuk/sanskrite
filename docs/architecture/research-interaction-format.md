# Формат Research Interaction

## Обзор

Research режим использует новый тип interaction `kind: 'research'` для отображения процесса исследования и результатов в чате.

## Структура данных

### Типы Research Interactions

#### 1. Research Progress (`research.progress`)
Отображает прогресс выполнения исследования в реальном времени.

```typescript
interface ResearchProgressInteraction {
  id: string;
  kind: 'research';
  subkind: 'research.progress';
  projectId: string;
  
  // Основной запрос пользователя
  researchQuery: string;
  
  // Текущий прогресс
  researchProgress: {
    stage: 'planning' | 'searching' | 'generating';
    status: 'in_progress' | 'completed';
    
    // Для stage='planning'
    subQueries?: string[]; // Показывать при status='completed'
    
    // Для stage='searching'
    sourcesCount?: number; // Показывать при status='completed'
    
    // Для stage='generating'
    // (без дополнительных данных, просто индикатор)
  };
  
  // Стандартные поля
  createdAt: string;
  replyTo?: string; // ID user interaction с запросом
}
```

#### 2. Research Result (`research.result`)
Финальный результат исследования с отчетом и метаданными.

```typescript
interface ResearchResultInteraction {
  id: string;
  kind: 'research';
  subkind: 'research.result';
  projectId: string;
  
  // Основной запрос пользователя
  researchQuery: string;
  
  // Финальный отчет (markdown)
  content: string;
  
  // Метаданные исследования
  subQueries: string[]; // Все сгенерированные под-вопросы
  
  sources: Array<{
    title: string;
    url: string;
    score?: number; // Релевантность от Tavily (0-1)
  }>;
  
  // Стандартные поля
  model?: string;
  usage?: {
    promptTokens: number;
    outputTokens: number;
    totalTokens: number;
  };
  createdAt: string;
  replyTo?: string; // ID user interaction с запросом
}
```

## Жизненный цикл Research Interaction

### Последовательность событий:

1. **User запрос** → создается `kind: 'user'` interaction с запросом
2. **Research start** → создается `kind: 'research', subkind: 'research.progress'` с `stage: 'planning', status: 'in_progress'`
3. **Planning completed** → обновляется тот же interaction: `stage: 'planning', status: 'completed', subQueries: [...]`
4. **Searching started** → обновляется: `stage: 'searching', status: 'in_progress'`
5. **Searching completed** → обновляется: `stage: 'searching', status: 'completed', sourcesCount: 12`
6. **Generating started** → обновляется: `stage: 'generating', status: 'in_progress'`
7. **Generating completed** → создается новый `kind: 'research', subkind: 'research.result'` с финальным отчетом
8. **Progress удаляется** → старый `research.progress` interaction можно скрыть или удалить

## Отображение в UI

### Компонент: `ResearchInteraction.tsx`

```typescript
export const ResearchInteraction: React.FC<{
  interaction: ResearchProgressInteraction | ResearchResultInteraction;
  collapsedSections?: Record<string, boolean>;
}> = ({ interaction }) => {
  if (interaction.subkind === 'research.progress') {
    return <ResearchProgressView interaction={interaction} />;
  }
  return <ResearchResultView interaction={interaction} />;
};
```

### ResearchProgressView

**Визуальный формат:**

```
┌─────────────────────────────────────────────┐
│ 🔬 Research Mode                            │
│                                              │
│ Query: "What is quantum computing?"         │
│                                              │
│ ┌─────────────────────────────────────────┐ │
│ │ ⏳ Planning sub-queries...              │ │
│ │    ✓ Generated 5 sub-queries            │ │
│ │                                          │ │
│ │    • What is quantum computing?         │ │
│ │    • How does quantum computing work?  │ │
│ │    • What are applications of QC?        │ │
│ │    • What are challenges in QC?         │ │
│ │    • What is the future of QC?          │ │
│ └─────────────────────────────────────────┘ │
│                                              │
│ ┌─────────────────────────────────────────┐ │
│ │ 🔍 Searching sources...                 │ │
│ │    ✓ Found 12 sources                   │ │
│ └─────────────────────────────────────────┘ │
│                                              │
│ ┌─────────────────────────────────────────┐ │
│ │ ✍️ Generating report...                 │ │
│ │    [Анимированные точки]                 │ │
│ └─────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

**Цветовая схема:**
- Planning: `var(--accent-blue)` / `var(--accent-blue-10)`
- Searching: `var(--accent-orange)` / `var(--accent-orange-10)`
- Generating: `var(--accent-green)` / `var(--accent-green-10)`

**Статусы:**
- `in_progress`: анимированные точки `...`
- `completed`: чекмарк ✓

### ResearchResultView

**Визуальный формат:**

```
┌─────────────────────────────────────────────┐
│ 🔬 Research Report                          │
│                                              │
│ [Markdown содержимое отчета]                │
│                                              │
│ ─────────────────────────────────────────── │
│                                              │
│ 📋 Sub-queries explored (5):                │
│    • What is quantum computing?             │
│    • How does quantum computing work?       │
│    • What are applications of QC?            │
│    • What are challenges in QC?             │
│    • What is the future of QC?              │
│                                              │
│ 📚 Sources (12)                             │
│    [Collapsible секция]                     │
│    ▼ Show sources                           │
│                                              │
│ 💡 Usage: 2,450 tokens                      │
│    Model: gemini_quality                     │
└─────────────────────────────────────────────┘
```

**Детали:**
- Markdown рендеринг через `MarkdownRenderer`
- Sub-queries: простой список с буллетами
- Sources: collapsible секция, показывает первые 5, остальные по клику "Show more"
- Sources сортируются по `score` (если доступно)
- Каждый source - кликабельная ссылка

## Интеграция в существующую систему

### 1. Обновление типов

**`web/src/types/interaction.ts`:**
```typescript
export type InteractionKind = 
  | 'user' 
  | 'assistant' 
  | 'system' 
  | 'note' 
  | 'section_start' 
  | 'section_end'
  | 'action'
  | 'research'; // Новый тип

export interface ResearchProgressInteraction extends Interaction {
  kind: 'research';
  subkind: 'research.progress';
  researchQuery: string;
  researchProgress: {
    stage: 'planning' | 'searching' | 'generating';
    status: 'in_progress' | 'completed';
    subQueries?: string[];
    sourcesCount?: number;
  };
}

export interface ResearchResultInteraction extends Interaction {
  kind: 'research';
  subkind: 'research.result';
  researchQuery: string;
  content: string;
  subQueries: string[];
  sources: Array<{
    title: string;
    url: string;
    score?: number;
  }>;
}
```

### 2. Обновление LocalInteraction

**`web/src/stores/genaiChatStore.ts`:**
```typescript
type InteractionKind = 
  | 'user' 
  | 'assistant' 
  | 'system' 
  | 'note' 
  | 'section_start' 
  | 'section_end' 
  | 'action'
  | 'research'; // Добавить

export interface LocalInteraction {
  // ... существующие поля
  kind: InteractionKind;
  subkind?: string;
  
  // Research-specific поля (опционально)
  researchQuery?: string;
  researchProgress?: {
    stage: 'planning' | 'searching' | 'generating';
    status: 'in_progress' | 'completed';
    subQueries?: string[];
    sourcesCount?: number;
  };
  subQueries?: string[];
  sources?: Array<{
    title: string;
    url: string;
    score?: number;
  }>;
}
```

### 3. Интеграция в ChatMessagesList

**`web/src/components/ChatMessagesList.tsx`:**
```typescript
// В рендеринге interactions
if (item.type === 'interaction') {
  const it = item.data;
  
  if (it.kind === 'research') {
    return (
      <ResearchInteraction 
        key={it.id} 
        interaction={it}
        collapsedSections={collapsedSections}
      />
    );
  }
  
  // ... остальные типы
}
```

### 4. Создание Research Interaction на бэкенде

**`backend/internal/http/genai.go`** - в `executeResearchMode`:

```go
// После получения результата исследования
researchResultInteraction := map[string]interface{}{
    "kind": "research",
    "subkind": "research.result",
    "content": result.Report,
    "researchQuery": req.Prompt,
    "subQueries": result.SubQueries,
    "sources": convertSources(result.Sources),
    "model": req.Model,
    "usage": usage,
    "replyTo": userInteractionID, // ID user interaction
}

// Создать interaction через API или напрямую в БД
```

## WebSocket события → Research Interactions

### Маппинг событий:

| WebSocket Event | Action |
|----------------|--------|
| `genai.stream.research.start` | Создать `research.progress` с `stage: 'planning', status: 'in_progress'` |
| `genai.stream.research.planning` (completed) | Обновить `research.progress`: `stage: 'planning', status: 'completed', subQueries: [...]` |
| `genai.stream.research.searching` (in_progress) | Обновить `research.progress`: `stage: 'searching', status: 'in_progress'` |
| `genai.stream.research.searching` (completed) | Обновить `research.progress`: `stage: 'searching', status: 'completed', sourcesCount: N` |
| `genai.stream.research.generating` (in_progress) | Обновить `research.progress`: `stage: 'generating', status: 'in_progress'` |
| `genai.stream.research.done` | Создать `research.result` с финальным отчетом, удалить/скрыть `research.progress` |
| `genai.stream.research.error` | Показать ошибку в `research.progress` или создать error interaction |

## Обработка на фронтенде

### В `discoveryStore.ts`:

```typescript
// В обработчике genai.stream.research.* событий
case 'genai.stream.research.start':
  // Создать research.progress interaction
  const progressInteraction: LocalInteraction = {
    id: `research-${streamID}-progress`,
    projectId: eventPid,
    kind: 'research',
    subkind: 'research.progress',
    researchQuery: msg.data?.query,
    researchProgress: {
      stage: 'planning',
      status: 'in_progress',
    },
    createdAt: new Date().toISOString(),
    replyTo: userInteractionId,
  };
  // Добавить в store

case 'genai.stream.research.planning':
  if (msg.data?.status === 'completed') {
    // Обновить существующий research.progress
    // Добавить subQueries
  }

case 'genai.stream.research.done':
  // Создать research.result interaction
  const resultInteraction: LocalInteraction = {
    id: `research-${streamID}-result`,
    projectId: eventPid,
    kind: 'research',
    subkind: 'research.result',
    content: msg.data?.report || '',
    researchQuery: msg.data?.query,
    subQueries: msg.data?.subQueries || [],
    sources: msg.data?.sources || [],
    model: msg.data?.model,
    usage: msg.data?.usage,
    createdAt: new Date().toISOString(),
    replyTo: userInteractionId,
  };
  // Удалить/скрыть research.progress
```

## Стилизация

### CSS переменные (если нужно добавить):

```css
--accent-research: var(--accent-blue);
--accent-research-10: rgba(/* blue with 10% opacity */);
--accent-research-20: rgba(/* blue with 20% opacity */);
```

### Компонент стилей:

- Использовать существующие стили из `ActionInteraction` как референс
- Research progress: более информативный, с секциями для каждой стадии
- Research result: похож на обычный assistant message, но с дополнительными метаданными

## Примеры использования

### Пример 1: Простой Research запрос

**User:**
```
Research: What is quantum computing?
```

**Research Progress:**
```
🔬 Research Mode
Query: "What is quantum computing?"

⏳ Planning sub-queries...
   ✓ Generated 3 sub-queries

🔍 Searching sources...
   ✓ Found 8 sources

✍️ Generating report...
```

**Research Result:**
```
🔬 Research Report

[Markdown отчет о quantum computing]

📋 Sub-queries explored (3):
   • What is quantum computing?
   • How does quantum computing work?
   • What are applications of quantum computing?

📚 Sources (8):
   • Quantum Computing Explained (example.com)
   • ...
```

### Пример 2: Research с ошибкой

**Research Progress:**
```
🔬 Research Mode
Query: "..."

⏳ Planning sub-queries...
   ✓ Generated 5 sub-queries

🔍 Searching sources...
   ✗ Error: Tavily API unavailable
```

## Следующие шаги

1. ✅ Определить формат данных
2. ⏳ Создать TypeScript типы
3. ⏳ Создать компоненты `ResearchInteraction`, `ResearchProgressView`, `ResearchResultView`
4. ⏳ Интегрировать в `ChatMessagesList`
5. ⏳ Обновить бэкенд для создания Research interactions
6. ⏳ Обновить обработку WebSocket событий на фронтенде
7. ⏳ Добавить стили
8. ⏳ Тестирование

