# TypeScript Types

Этот каталог содержит автогенерированные TypeScript типы из Go моделей backend.

## Структура

```
types/
├── schemas/           # JSON Schema файлы (автогенерированные из Go)
│   ├── User.schema.json
│   ├── Project.schema.json
│   └── DataSource.schema.json
├── generated.ts       # TypeScript интерфейсы (автогенерированные из JSON Schema)
├── index.ts           # Экспорты типов для использования в приложении
└── README.md          # Этот файл
```

## Использование

```typescript
import { User, Project, DataSource } from '@/types';

// Используйте типы в компонентах
const user: User = { ... };
const project: Project = { ... };
```

## Генерация типов

**⚠️ НЕ РЕДАКТИРУЙТЕ файлы `generated.ts` и `schemas/*.json` вручную!**

Эти файлы автоматически генерируются из Go моделей.

### Автоматическая генерация (рекомендуется)

При запуске `npm run build` типы генерируются автоматически.

### Ручная генерация

```bash
# 1. Генерация JSON схем из Go моделей
npm run gen:schemas

# 2. Генерация TypeScript типов из схем
npm run gen:types
```

## Добавление новых типов

См. [docs/development/type-generation.md](../../../docs/development/type-generation.md)

## Доступные типы

- `User` - пользователь системы
- `Project` - проект
- `DataSource` - источник данных

Полный список полей и их типов смотрите в файле `generated.ts`.

