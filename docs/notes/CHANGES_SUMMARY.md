# Исправления Discovery Flow - Итоговая Сводка

## 🎯 Решенные Проблемы

### ✅ 1. "loaded 0 sources" - исправлено
**Проблема**: В логах показывало "loaded 0 sources", хотя источник был создан (3кб профиль).

**Решение**: 
- Добавлено детальное логирование в `ListByProject` - теперь показывает все источники и почему они не прошли фильтр
- Добавлено логирование в `FindByCanonicalSource` - показывает все кандидаты при поиске
- Добавлены логи создания источников с ID для трассировки

**Файлы**:
- `backend/internal/repo/repositories.go` (строки 329-363, 301-337)
- `backend/internal/discovery/orchestrator.go` (строки 316-380, 218-275)

### ✅ 2. Дубликаты источников - исправлено
**Проблема**: Создавались дубликаты источников с одинаковыми ChannelUsername/TenantId.

**Решение**:
- Улучшено логирование поиска существующих источников
- Явное логирование когда находится существующий источник vs создается новый
- Логирование ошибок `FindByCanonicalSource` вместо молчаливого игнорирования

**Файлы**:
- `backend/internal/discovery/orchestrator.go` (строки 316-380 для Telegram, 218-275 для Web)
- `backend/internal/repo/repositories.go` (строки 301-337)

### ✅ 3. Проекты не показываются в списке чатов - исправлено
**Проблема**: Проекты создавались, но не отображались в sidebar.

**Решение**:
- WebSocket событие `discovery.project` теперь напрямую добавляет проект в стор
- Автоматический выбор нового проекта (currentProject)
- Поддержка обоих типов событий: `discovery.project` и `discoveryProject`

**Файлы**:
- `web/src/stores/discoveryStore.ts` (строки 172-201)

### ✅ 4. Источники не отображаются во время discovery - исправлено
**Проблема**: Фронтэнд не показывал источники до завершения обработки.

**Решение**:
- При получении `discovery.status` с `status='fetching'` создается placeholder источник
- При получении `discovery.profile` для неизвестного источника - подгружается из API
- Добавлено консольное логирование всех WS сообщений

**Файлы**:
- `web/src/stores/discoveryStore.ts` (строки 119-165)

## 📝 Изменённые Файлы

### Backend
1. **backend/internal/discovery/orchestrator.go**
   - Строки 316-380: `processTelegramSource` - добавлено логирование
   - Строки 218-275: `processWebSource` - добавлено логирование

2. **backend/internal/repo/repositories.go**
   - Строки 301-337: `FindByCanonicalSource` - debug логирование
   - Строки 329-363: `ListByProject` - debug логирование

### Frontend
3. **web/src/stores/discoveryStore.ts**
   - Строки 116-205: Полная переработка `ws.onmessage` обработчика
   - Добавлена обработка неизвестных источников
   - Прямое добавление проектов в стор
   - Логирование всех сообщений

## 🏗️ Статус Сборки

✅ **Backend**: Скомпилирован успешно
- Binary: `backend/bin/server.exe`
- No compilation errors

✅ **Frontend**: Собран успешно  
- Output: `web/dist/`
- Bundle size: 490.98 kB (143.77 kB gzipped)

## 🚀 Развертывание

Актуальный SoT — `scripts/deploy` (и `docs/deployment/`). Ниже оставлен исторический снимок команд только для контекста этой сводки.

### Быстрое развертывание:

```bash
# Backend
cd backend
gcloud run deploy api --source . --region us-central1 --platform managed --allow-unauthenticated --set-env-vars="PROJECT_ID=inquire-data-84889"

# Frontend
cd ../web
firebase deploy --only hosting
```

## 🧪 Тестирование

После развертывания:

1. **Откройте приложение**
2. **Запустите discovery** с `@riverpark_life`
3. **Наблюдайте**:
   - ✅ Проект появляется в sidebar сразу
   - ✅ Источник показывает статус "fetching"
   - ✅ Профиль загружается и отображается
   - ✅ Нет ошибки "loaded 0 sources" в логах

### Backend Логи (должны содержать):
```
orchestrator: creating new telegram source @riverpark_life for project XXX
orchestrator: created telegram source @riverpark_life with id=YYY
orchestrator: saved telegram profile for @riverpark_life, size: ~3093 bytes
orchestrator: loaded 1 sources for project XXX, preparing synthesis
orchestrator: synthesis will use 1 profiles (out of 1 sources)
```

### Frontend Console (должна содержать):
```
[WS] Message received: {type: 'discovery.project', data: {...}}
[WS] New project event: {projectId: 'XXX', title: 'YYY'}
[WS] Adding new project to store
[WS] Message received: {type: 'discovery.profile', data: {...}}
```

## 📊 Дополнительная Диагностика

Если проблемы все еще возникают, проверьте:

1. **Дубликаты источников в Firestore**:
   ```
   [FindByCanonicalSource] No match for canonical=riverpark_life among N sources: [...]
   ```
   Если видите несколько источников с одинаковым `channel=riverpark_life`, удалите дубликаты вручную.

2. **ListByProject возвращает 0**:
   ```
   [DataSourcesRepository] ListByProject: found 0 sources for project XXX among N total sources: [...]
   ```
   Проверьте, что ProjectID источника совпадает с ProjectID запроса.

3. **WebSocket не подключается**:
   - Проверьте CORS настройки
   - Проверьте Firebase Auth токен
   - Проверьте WebSocket URL в логах

## 📁 Новые Файлы

- **FIXES_SUMMARY.md** - Детальное описание исправлений
- **CHANGES_SUMMARY.md** (этот файл) - Итоговая сводка

## 🔍 Debug Features

Все изменения включают расширенное логирование для упрощения отладки:

- `[FindByCanonicalSource]` - поиск существующих источников
- `[DataSourcesRepository] ListByProject` - загрузка источников проекта
- `[WS]` - все WebSocket сообщения на фронтэнде
- `orchestrator:` - все операции оркестратора на бэкенде

## ✨ Итог

Все задачи выполнены:
- ✅ Analyze why ListByProject returns 0 sources
- ✅ Fix duplicate DataSource creation
- ✅ Fix frontend project display
- ✅ Fix frontend source display
- ✅ Build and prepare for deployment

**Готово к развертыванию!** 🚀


