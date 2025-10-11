# Frontend Build Scripts

## generate-types.mjs

Автоматически генерирует TypeScript типы из JSON Schema файлов.

### Использование

```bash
node scripts/generate-types.mjs
```

Или через npm:

```bash
npm run gen:types
```

### Как это работает

1. Читает все `*.schema.json` файлы из `src/types/schemas/`
2. Использует `json-schema-to-typescript` для конвертации в TypeScript интерфейсы
3. Добавляет удобные type aliases (например, `DataSource` вместо `HttpsGithub...`)
4. Записывает результат в `src/types/generated.ts`

### Требования

- Node.js v14+
- Пакет `json-schema-to-typescript` должен быть установлен

### Вывод

Генерирует файл `src/types/generated.ts` со следующей структурой:

```typescript
// Полные интерфейсы с длинными именами
export interface HttpsGithub... { ... }

// Удобные алиасы
export type User = HttpsGithub...;
export type Project = ...;
```

### Примечания

- **НЕ редактируйте** `generated.ts` вручную - он перезаписывается при каждой генерации
- Скрипт автоматически запускается при `npm run build`
- Для добавления новых типов, добавьте соответствующий `.schema.json` файл

