# Отчет: Система автогенерации типов Backend → Frontend

## ✅ Статус: Полностью настроена и работает

Система автоматической генерации TypeScript типов из Go моделей backend настроена и протестирована.

## 📋 Что было сделано

### 1. Проверка существующей инфраструктуры
- ✅ Go генератор схем: `backend/internal/tools/schemagen/main.go`
- ✅ JSON схемы: `web/src/types/schemas/*.json`
- ✅ Пакет `json-schema-to-typescript` установлен

### 2. Исправление процесса генерации TypeScript типов
**Проблема:** Команда `json2ts` в `package.json` не работала корректно с wildcard'ами

**Решение:**
- ✅ Создан новый скрипт `web/scripts/generate-types.mjs`
- ✅ Скрипт автоматически обрабатывает все `*.schema.json` файлы
- ✅ Добавляет удобные type aliases (DataSource, User, Project)
- ✅ Обновлен `package.json` для использования нового скрипта

### 3. Улучшение типизации во frontend

**Обновлены файлы:**
- ✅ `web/src/types/index.ts` - добавлен экспорт сгенерированных типов
- ✅ `web/src/stores/projectsStore.ts` - использует `Project` из автогенерации
- ✅ `web/src/stores/discoveryStore.ts` - использует `DataSource` из автогенерации  
- ✅ `web/src/lib/api.ts` - все методы типизированы с использованием автогенерированных типов

### 4. Документация

**Созданы файлы:**
- ✅ `docs/development/type-generation.md` - полное руководство
- ✅ `web/scripts/README.md` - описание скрипта генерации
- ✅ `web/src/types/README.md` - краткое руководство по использованию
- ✅ `web/src/types/example.ts` - примеры использования типов

## 🔄 Процесс генерации

```
Backend (Go)                      Frontend (TypeScript)
     ↓                                    ↓
models.go ──────────────────────→  generated.ts
     ↓                                    ↑
schemagen (Go tool)                      |
     ↓                                    |
schemas/*.json ──→ generate-types.mjs ───┘
```

### Команды для генерации

#### Автоматическая (при сборке)
```bash
cd web
npm run build  # Включает gen:schemas + gen:types + компиляцию
```

#### Ручная
```bash
# Генерация JSON схем из Go моделей
cd backend
go run ./internal/tools/schemagen

# Генерация TypeScript типов из схем
cd ../web
npm run gen:types
```

## 📦 Типы Frontend

### Автогенерированные типы (из `backend/internal/models/`):
- `User` - пользователь системы (из `models.User`)
- `Project` - проект (из `models.Project`)
- `DataSource` - источник данных (из `models.DataSource`)
- `Document` - документ (из `models.Document`)
- `SourceProfile` - профиль источника (из `models.SourceProfile`)

### Ручные типы (из `backend/internal/repo/`):
- `Interaction` - взаимодействие/сообщение (из `repo.Interaction`)
  - `InteractionMetadata` - метаданные взаимодействия (pinned, и др.)
  - `ParticipantRef` - ссылка на участника
  - `AgentProfileSnapshot` - снимок профиля агента
  - `UsageMeta` - метрики использования токенов
  - `InteractionRelation` - связи между взаимодействиями
  - `GenerationParams` - параметры генерации
  - `HighRisk` - информация о высоком риске

**Примечание:** Типы `Interaction` и связанные с ним поддерживаются вручную, так как они находятся в `repo` пакете, который не включен в автоматическую генерацию. См. `docs/development/type-organization.md` для деталей.

### Использование в коде:

```typescript
// Автогенерированные типы
import { User, Project, DataSource } from '@/types';

// Ручные типы
import { Interaction, InteractionMetadata } from '@/types';

// API клиент с типизацией
async function getUser(id: string): Promise<User> {
  const response = await axios.get<User>(`/api/users/${id}`);
  return response.data;
}

// Store с типизацией
interface AppState {
  projects: Project[];
  currentProject: Project | null;
}

// Компонент с типизированными пропсами
interface ProjectCardProps {
  project: Project;
  onEdit?: (project: Project) => void;
}
```

## ✅ Проверка работоспособности

### Выполнено тестирование:

1. **Генерация JSON схем из Go:**
   ```bash
   cd backend
   go run ./internal/tools/schemagen
   # ✅ Успешно: сгенерированы User.schema.json, Project.schema.json, DataSource.schema.json
   ```

2. **Генерация TypeScript типов из схем:**
   ```bash
   cd web
   npm run gen:types
   # ✅ Успешно: создан generated.ts с интерфейсами и алиасами
   ```

3. **TypeScript компиляция:**
   ```bash
   npx tsc --noEmit
   # ✅ Успешно: нет ошибок типизации
   ```

4. **Использование в существующем коде:**
   - ✅ Stores обновлены и используют автогенерированные типы
   - ✅ API клиент полностью типизирован
   - ✅ Все компилируется без ошибок

## 📚 Документация

Полная документация доступна в:
- **Руководство разработчика:** `docs/development/type-generation.md`
- **Организация типов:** `docs/development/type-organization.md` ⭐ NEW
- **Краткая справка:** `web/src/types/README.md`
- **Примеры использования:** `web/src/types/example.ts`
- **Скрипты генерации:** `web/scripts/README.md`

## 🎯 Преимущества

1. **Type Safety** - полная типизация между backend и frontend
2. **Автоматическая синхронизация** - типы всегда актуальны
3. **Developer Experience** - автодополнение и проверка типов в IDE
4. **Предотвращение ошибок** - ошибки типов обнаруживаются на этапе компиляции
5. **Документирование** - типы служат документацией для API

## 🔧 Для добавления новых типов

### Автогенерируемые типы (предпочтительно):
1. Создайте Go модель в `backend/internal/models/models.go`
2. Добавьте её в `backend/internal/tools/schemagen/main.go`:
   ```go
   {File: "NewModel.schema.json", Val: new(models.NewModel)}
   ```
3. Запустите `npm run gen:schemas && npm run gen:types`
4. Добавьте экспорт в `web/src/types/index.ts` (секция auto-generated)

### Ручные типы (только если необходимо):
1. Создайте файл `web/src/types/[name].ts`
2. Добавьте JSDoc с объяснением, почему тип ручной
3. Синхронизируйте с Go структурой вручную
4. Добавьте экспорт в `web/src/types/index.ts` (секция manually maintained)
5. Документируйте в `docs/development/type-organization.md`

**См. также:** `docs/development/type-organization.md` - полное описание организации типов

## 📝 Примечания

- **НЕ редактируйте** файлы `generated.ts` и `*.schema.json` вручную
- Генерация типов происходит автоматически при `npm run build`
- Для production (CI) используйте `npm run build:ci` (без генерации)
- При изменении backend моделей обязательно регенерируйте типы

## 🎉 Итог

Система автогенерации типов полностью настроена, протестирована и готова к использованию. Все существующие stores и API клиент обновлены для использования автогенерированных типов. Создана подробная документация и примеры.

**Статус:** ✅ Готово к работе

