# Анализ оптимизации производительности Horizon Graph

## Критические проблемы производительности

### 1. **elkLayout.ts: Повторные вычисления в циклах**

**Проблема:**
- `leftPaneLayoutHeightFor()` вызывается **множество раз** для одних и тех же нод в разных местах:
  - В `groupDimsWrap` при вычислении `rowHeights` (для каждой ноды в каждом типе)
  - В `attachChildrenWrap` при позиционировании
  - В `maxVerticalForNodes()` при проверке размера
- `normalizeFragmentType()` и `normalizeEdgeType()` вызываются повторно для одних и тех же строк
- Множественные `.sort()`, `.filter()`, `.map()` на одних и тех же массивах

**Решение:**
```typescript
// Кэшировать результаты нормализации и высот
const nodeHeightCache = new Map<string, number>();
const normalizedTypeCache = new Map<string, string>();

const getCachedNodeHeight = (nodeData: any): number => {
  const key = `${nodeData?.type || ''}:${nodeData?.nodeSize || 'default'}`;
  if (!nodeHeightCache.has(key)) {
    nodeHeightCache.set(key, leftPaneLayoutHeightFor(nodeData));
  }
  return nodeHeightCache.get(key)!;
};
```

**Ожидаемый эффект:** Снижение времени layout на 30-50% для больших графов (100+ нод)

---

### 2. **elkLayout.ts: Избыточные сортировки**

**Проблема:**
- Массивы сортируются **несколько раз** подряд:
  - `list.slice().sort()` в `groupDimsWrap` (строки 1820, 1924, 1954, 2015)
  - `nodesRel.slice().sort()` внутри циклов по relation-блокам
  - `rightSorted = [...rightNodes].sort()` (строка 2081)

**Решение:**
```typescript
// Сортировать один раз в начале, сохранять результат
const sortedByType = new Map<string, Node<T>[]>();
for (const [typeKey, list] of byType.entries()) {
  sortedByType.set(typeKey, list.slice().sort((a, b) => {
    const al = String((a.data as any)?.label || a.id);
    const bl = String((b.data as any)?.label || b.id);
    return al.localeCompare(bl);
  }));
}
```

**Ожидаемый эффект:** Снижение времени на 10-15% для графов с множеством типов

---

### 3. **LensCanvasCockpit.tsx: Отсутствие мемоизации дорогих вычислений**

**Проблема:**
- `displayNodes` пересчитывается при каждом рендере, даже если входные данные не изменились
- `childrenByGroupId` и `groupIdByChildId` пересчитываются полностью каждый раз
- Edge aggregation логика выполняется синхронно на каждом рендере

**Решение:**
```typescript
const displayNodes = React.useMemo(() => {
  // ... существующая логика
}, [fragments, layoutMode, compactLeftPane, /* другие зависимости */]);

const childrenByGroupId = React.useMemo(() => {
  // ... существующая логика
}, [displayNodes]);
```

**Ожидаемый эффект:** Снижение времени рендера на 40-60% при изменении только визуальных параметров (zoom, pan)

---

### 4. **HorizonGraphNodeTypes.tsx: Дорогие вычисления в рендере**

**Проблема:**
- `segmentIconTypes` использует `React.useMemo`, но зависимости могут меняться часто
- `computeRiskSegments()` вызывается для каждой ноды, даже если `state` не изменился
- `getNodeColors()` вызывается без мемоизации

**Решение:**
```typescript
// Мемоизировать getNodeColors на уровне модуля
const nodeColorsCache = new Map<string, NodeColorScheme>();
const getCachedNodeColors = (type: string, theme: 'light' | 'dark'): NodeColorScheme => {
  const key = `${type}:${theme}`;
  if (!nodeColorsCache.has(key)) {
    nodeColorsCache.set(key, getNodeColors(type, theme));
  }
  return nodeColorsCache.get(key)!;
};
```

**Ожидаемый эффект:** Снижение времени рендера нод на 20-30%

---

### 5. **elkLayout.ts: Неоптимальная работа с Map/Set**

**Проблема:**
- Множественные `.has()` и `.get()` для одних и тех же ключей
- Создание временных Set/Map в циклах без переиспользования

**Решение:**
```typescript
// Предварительно вычислить все нужные Set/Map один раз
const precomputedSets = {
  preHiddenIds: new Set(/* ... */),
  rightIds: new Set(/* ... */),
  focusIslandIds: new Set(/* ... */),
};
```

**Ожидаемый эффект:** Небольшое улучшение (5-10%), но важно для больших графов

---

### 6. **ReactFlow: Рендеринг всех нод одновременно**

**Проблема:**
- ReactFlow рендерит **все** ноды, даже те, что вне viewport
- Для графов с 200+ нодами это создаёт огромное DOM-дерево

**Решение:**
- Использовать `react-window` или `react-virtual` для виртуализации
- Или использовать встроенную оптимизацию ReactFlow через `onlyRenderVisibleElements` (если доступно)

**Ожидаемый эффект:** Снижение времени первого рендера на 70-80% для больших графов

---

### 7. **elkLayout.ts: Синхронные тяжёлые вычисления**

**Проблема:**
- `layoutLevelsTwoPane` выполняется синхронно и блокирует UI
- Для графов с 100+ нодами это может занимать 100-500ms

**Решение:**
```typescript
// Разбить на chunks и использовать requestIdleCallback или Web Workers
const layoutInChunks = async (nodes: Node<T>[], chunkSize = 50) => {
  for (let i = 0; i < nodes.length; i += chunkSize) {
    await new Promise(resolve => requestIdleCallback(resolve));
    // Обработать chunk
  }
};
```

**Ожидаемый эффект:** UI остаётся отзывчивым даже при больших графах

---

## Приоритеты оптимизации

### Высокий приоритет (быстрый эффект):
1. ✅ Мемоизация `displayNodes` и `childrenByGroupId` в LensCanvasCockpit
2. ✅ Кэширование `leftPaneLayoutHeightFor` и нормализаций
3. ✅ Устранение повторных сортировок

### Средний приоритет (значительный эффект):
4. ✅ Мемоизация `getNodeColors` и `computeRiskSegments`
5. ✅ Оптимизация работы с Map/Set

### Низкий приоритет (требует архитектурных изменений):
6. ⚠️ Виртуализация рендеринга нод
7. ⚠️ Асинхронный layout с chunking

---

## Метрики для измерения

- Время первого рендера графа (target: <200ms для 100 нод)
- Время пересчёта layout при изменении (target: <50ms)
- FPS при панорамировании/зуме (target: >30 FPS)
- Память (target: <100MB для графа с 200 нодами)
