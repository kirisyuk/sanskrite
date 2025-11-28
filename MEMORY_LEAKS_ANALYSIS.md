# Анализ потенциальных утечек памяти

## 🔴 Критические проблемы

### 1. Горутины для cleanup orphaned WebSocket соединений (ws.go:319)
**Проблема:** В `BroadcastJSONProject` при обнаружении orphaned соединений запускается горутина для cleanup:
```go
go func(orphanedConn *websocket.Conn) {
    h.mu.Lock()
    defer h.mu.Unlock()
    // cleanup logic
}(c)
```

**Риск:** При большом количестве orphaned connections одновременно может накопиться много горутин. Хотя они завершаются быстро, при пиковых нагрузках это может привести к утечке.

**Рекомендация:** Использовать worker pool или ограничить количество параллельных cleanup операций.

### 2. Горутины в subscriber.go без отслеживания (subscriber.go:69)
**Проблема:** В цикле для каждого топика запускается горутина:
```go
for _, tname := range topicNames {
    // ...
    go s.runWithRetry(ctx, sub, tname)
}
```

**Риск:** Хотя `Start()` вызывается только один раз при старте сервера, нет механизма отслеживания запущенных горутин. При проблемах с graceful shutdown или если `Start()` будет вызван повторно, горутины могут накапливаться.

**Рекомендация:** Добавить map для отслеживания активных подписок и проверку перед созданием новой горутины. Также добавить метод `Stop()` для корректной остановки всех подписок.

## 🟡 Средние проблемы

### 3. Кэш DomainCache - неполная очистка при TTL истечении (domain/cache.go:51)
**Проблема:** При проверке TTL в `Get()` удаление происходит только при чтении:
```go
if time.Now().Unix()-cached.CachedAt < int64(c.ttl.Seconds()) {
    return cached, true
}
delete(c.memoryCache, key) // удаляется только при Get()
```

**Риск:** Записи с истекшим TTL остаются в памяти до следующего обращения, если к ним не обращаются.

**Рекомендация:** Добавить периодическую фоновую очистку истекших записей или использовать более эффективный механизм TTL.

### 4. Мапы в orchestrator.Execute() без ограничения размера (orchestrator.go:1090, 1104)
**Проблема:** Мапы `webSeen` и `tgSeen` создаются для каждой операции Execute без ограничения:
```go
webSeen := map[string]struct{}{}
for _, raw := range in.WebSources {
    // ...
    webSeen[canonical] = struct{}{}
}
```

**Риск:** Если `in.WebSources` или `in.TgHandles` очень большие (например, тысячи URL), мапы будут занимать много памяти.

**Рекомендация:** Добавить ограничение на количество источников или использовать более эффективную структуру данных.

### 5. Срезы в Chat() без ограничения (orchestrator.go:830-890)
**Проблема:** В методе `Chat()` собираются данные без явных ограничений:
```go
var historyPairs []map[string]string
var profileSummaries []string
var contextSnippets []string
var contextEntries []ctxEntry
```

Хотя есть ограничения (до 4 пар истории, до 6 профилей), общий размер может быть большим при большом количестве interactions.

**Рекомендация:** Добавить общий лимит на размер собранных данных (например, 200KB).

## 🟢 Низкие риски (но стоит проверить)

### 6. WebSocket Hub мапы без периодической очистки (ws.go:19-25)
**Проблема:** Мапы `conns`, `projConns`, `verifiedConns`, `connIDs` растут при подключениях и очищаются только при отключении.

**Риск:** При неаккуратных отключениях клиентов (например, при сетевых проблемах) соединения могут остаться в мапах.

**Рекомендация:** Добавить периодическую проверку "живости" соединений и очистку мертвых.

### 7. Pub/Sub подписки - нет явного закрытия (subscriber.go:94)
**Проблема:** `sub.Receive()` вызывается в цикле с retry, но нет явного механизма для остановки при отсутствии сообщений.

**Риск:** Низкий, так как используется контекст с отменой, но стоит убедиться, что все подписки корректно останавливаются.

### 8. Горутины в main.go без явного ожидания завершения (cmd/server/main.go:411-439)
**Проблема:** Множество горутин запускается с `go func()`, но нет явного механизма для их graceful shutdown.

**Риск:** При остановке сервера горутины могут не успеть завершиться корректно.

**Рекомендация:** Использовать `sync.WaitGroup` или `errgroup` для отслеживания всех фоновых горутин.

## ✅ Хорошие практики (уже реализованы)

1. **DomainCache** имеет LRU eviction при 1000 записях
2. **genaiChatStore.ts** ограничивает attributions до 500 записей
3. **errgroup** используется в `orchestrator.Execute()` для управления параллельными операциями
4. **WebSocket соединения** правильно очищаются в `removeConn()`
5. **Контексты с отменой** используются для управления жизненным циклом горутин

## Рекомендации по приоритетам

1. **Высокий приоритет:**
   - Исправить горутины для cleanup orphaned connections (ws.go:319)
   - Добавить ограничения на размер входных данных в `orchestrator.Execute()`

2. **Средний приоритет:**
   - Добавить периодическую очистку истекших записей в DomainCache
   - Добавить проверку на уже запущенные подписки в subscriber.go

3. **Низкий приоритет:**
   - Добавить периодическую проверку "живости" WebSocket соединений
   - Улучшить graceful shutdown для фоновых горутин в main.go

---

# Анализ потенциальных утечек памяти на фронтенде

## 🔴 Критические проблемы

### 1. Таймер в PerspectivesPanel может не очищаться (PerspectivesPanel.tsx:528)
**Проблема:** `runTimerRef` создается при запуске генерации перспективы, но может не очищаться при unmount компонента:
```typescript
runTimerRef.current = window.setInterval(() => {
  const s = Math.max(0, Math.floor((Date.now() - started) / 1000));
  setRunSeconds(s);
}, 1000);
```

**Риск:** Если компонент размонтируется во время генерации, таймер продолжит работать и будет обновлять состояние несуществующего компонента.

**Рекомендация:** Добавить cleanup в `useEffect` или очищать таймер при успешном завершении/ошибке.

### 2. Массивы messages и interactions в stores без ограничений (genaiChatStore.ts)
**Проблема:** Массивы `messages` и `interactions` могут расти неограниченно:
```typescript
messages: ChatMessage[];  // без ограничения размера
interactions: LocalInteraction[];  // без ограничения размера
```

**Риск:** При длительных сессиях чата массивы могут стать очень большими и занимать много памяти.

**Рекомендация:** Добавить ограничение на количество сообщений (например, последние 1000) и interactions (например, последние 500).

### 3. WebSocket listeners могут накапливаться (discoveryStore.ts:1367-1368)
**Проблема:** Event listeners для `visibilitychange` и `online` добавляются, но могут не удаляться при повторных вызовах `connectWs`:
```typescript
if (!listenersAttached) {
  window.addEventListener('visibilitychange', onVisibility);
  window.addEventListener('online', onOnline);
  listenersAttached = true;
}
```

**Риск:** Хотя есть проверка `listenersAttached`, при ошибках или повторных подключениях listeners могут дублироваться.

**Рекомендация:** Убедиться, что listeners всегда удаляются перед добавлением новых, или использовать более надежный механизм отслеживания.

## 🟡 Средние проблемы

### 4. Таймеры в VerifiedViewPage (VerifiedViewPage.tsx:162, 209)
**Проблема:** Два таймера (`checkInterval` и `pollIntervalRef`) используются для проверки WebSocket и polling:
```typescript
const checkInterval = setInterval(() => {
  checkAndSubscribe();
}, 1000);

pollIntervalRef.current = setInterval(async () => {
  // polling logic
}, 5000);
```

**Риск:** Хотя cleanup есть в return функции useEffect, при быстром перемонтировании компонента таймеры могут накапливаться.

**Рекомендация:** Убедиться, что cleanup всегда выполняется, даже при ошибках.

### 5. Массив statusMessages в UnfoldRequest без ограничения (UnfoldRequest.tsx:474)
**Проблема:** Массив `statusMessages` может расти при частых операциях:
```typescript
setStatusMessages(prev => [...prev, message]);
```

**Риск:** Хотя сообщения удаляются через 5 секунд, при очень частых операциях массив может стать большим.

**Рекомендация:** Добавить ограничение на максимальное количество сообщений (например, 20).

### 6. Event listeners в useTextSelection (useTextSelection.ts:521-526)
**Проблема:** Множество event listeners добавляются, но cleanup зависит от правильной работы useEffect:
```typescript
document.addEventListener('selectionchange', handleSelectionChange);
window.addEventListener('scroll', onScrollOrResize, { passive: true });
window.addEventListener('resize', onScrollOrResize);
document.addEventListener('pointerdown', onPointerDown);
container.addEventListener('pointerdown', onContainerPointerDown);
container.addEventListener('pointerup', onContainerPointerUp);
```

**Риск:** Если useEffect не выполнит cleanup (например, при ошибке), listeners останутся висеть.

**Рекомендация:** Убедиться, что cleanup всегда выполняется, даже при ошибках.

### 7. Таймеры в useChatTimers (useChatTimers.ts:29)
**Проблема:** Таймер `genaiThinkingIntervalRef` может не очищаться при быстром переключении между состояниями:
```typescript
genaiThinkingIntervalRef.current = window.setInterval(() => {
  setGenaiThinkingSeconds((s) => s + 1);
}, 1000);
```

**Риск:** Хотя есть cleanup в отдельном useEffect, при быстрых изменениях состояния может накопиться несколько таймеров.

**Рекомендация:** Очищать таймер перед созданием нового.

## 🟢 Низкие риски (но стоит проверить)

### 8. WebSocket соединение в discoveryStore (discoveryStore.ts:921)
**Проблема:** WebSocket соединение хранится в модульной переменной `wsRef`, которая может не очищаться при ошибках:
```typescript
wsRef = new WebSocket(wsUrl);
```

**Риск:** При ошибках соединения или переподключениях старые соединения могут остаться в памяти.

**Рекомендация:** Убедиться, что все обработчики событий удаляются перед закрытием соединения.

### 9. Массивы в discoveryStore без ограничений (discoveryStore.ts)
**Проблема:** Массивы `sources`, `interactions`, `pendingSources` могут расти без ограничений:
```typescript
sources: DataSource[];
interactions: any[];
pendingSources: string[];
```

**Риск:** При большом количестве источников и interactions память может расти.

**Рекомендация:** Добавить ограничения или периодическую очистку старых данных.

### 10. Таймеры в компонентах без явного cleanup
**Проблема:** Некоторые компоненты используют `setTimeout` без сохранения ID для cleanup:
- `ModelGraph.tsx:813, 1302`
- `DiscoveryPage.tsx:1051, 1189`
- `CollapsibleText.tsx:62, 76, 87-89`

**Риск:** При unmount компонента таймеры могут продолжать работать.

**Рекомендация:** Сохранять ID таймеров в ref и очищать их в cleanup функции useEffect.

## ✅ Хорошие практики (уже реализованы)

1. **genaiChatStore.ts** ограничивает attributions до 500 записей
2. **useTextSelection** правильно очищает все event listeners в cleanup
3. **VerifiedViewPage** правильно очищает таймеры в cleanup функции
4. **UnfoldRequest** правильно очищает таймеры и event listeners
5. **discoveryStore** имеет функцию `disconnectWs()` для корректного закрытия WebSocket

## Рекомендации по приоритетам для фронтенда

1. **Высокий приоритет:**
   - Исправить таймер в PerspectivesPanel (добавить cleanup)
   - Добавить ограничения на массивы messages и interactions в genaiChatStore
   - Улучшить управление WebSocket listeners в discoveryStore

2. **Средний приоритет:**
   - Добавить ограничение на statusMessages в UnfoldRequest
   - Улучшить cleanup таймеров в useChatTimers
   - Проверить все setTimeout/setInterval на наличие cleanup

3. **Низкий приоритет:**
   - Добавить ограничения на массивы в discoveryStore
   - Улучшить обработку ошибок в WebSocket соединениях
   - Добавить мониторинг размера массивов в stores

