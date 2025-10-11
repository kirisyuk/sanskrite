# Автогенерация типов из Backend во Frontend

## Обзор

В проекте настроена автоматическая генерация TypeScript типов из Go моделей бэкенда. Это обеспечивает type-safety между backend и frontend, исключая рассинхронизацию типов.

## Архитектура

```
Backend (Go)                Frontend (TypeScript)
    ↓                              ↓
models.go                   generated.ts
    ↓                              ↑
schemagen                          |
    ↓                              |
schemas/*.json  →  generate-types.mjs
```

### Компоненты

1. **Go модели** (`backend/internal/models/models.go`)
   - Определяют структуры данных (User, Project, DataSource и т.д.)
   - Используют JSON tags для маппинга полей

2. **Schema Generator** (`backend/internal/tools/schemagen/main.go`)
   - Конвертирует Go структуры в JSON Schema
   - Использует библиотеку `github.com/invopop/jsonschema`
   - Генерирует файлы в `web/src/types/schemas/`

3. **Type Generator** (`web/scripts/generate-types.mjs`)
   - Конвертирует JSON Schema в TypeScript интерфейсы
   - Использует библиотеку `json-schema-to-typescript`
   - Создает удобные type aliases (DataSource, User, Project)
   - Генерирует файл `web/src/types/generated.ts`

## Использование

### Автоматическая генерация (при сборке)

Типы автоматически генерируются при запуске сборки фронтенда:

```bash
cd web
npm run build
```

Команда `build` выполняет:
1. `gen:schemas` - генерация JSON схем из Go
2. `gen:types` - генерация TypeScript типов из схем
3. `tsc -b` - компиляция TypeScript
4. `vite build` - сборка приложения

### Ручная генерация

#### Генерация JSON схем из Go моделей

```bash
cd backend
go run ./internal/tools/schemagen
```

Или из директории `web`:

```bash
npm run gen:schemas
```

#### Генерация TypeScript типов из схем

```bash
cd web
npm run gen:types
```

### Использование типов во Frontend

```typescript
// Импорт типов
import { User, Project, DataSource } from '@/types';

// Использование
const user: User = {
  id: '123',
  tenantId: 'tenant-1',
  email: 'user@example.com',
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
  schemaVersion: '1.0',
};

const project: Project = {
  id: 'proj-1',
  pk: 'Projects/proj-1',
  tenantId: 'tenant-1',
  displayName: 'My Project',
  status: 'active',
  ownerUserId: 'user-123',
  createdAt: new Date().toISOString(),
  updatedAt: new Date().toISOString(),
};
```

## Добавление новых типов

### 1. Создайте Go модель

В `backend/internal/models/models.go`:

```go
type MyNewType struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Description *string   `json:"description,omitempty"`
    CreatedAt   time.Time `json:"createdAt"`
}
```

### 2. Добавьте модель в генератор схем

В `backend/internal/tools/schemagen/main.go`:

```go
targets := []struct {
    File string
    Val  any
}{
    {File: "User.schema.json", Val: new(models.User)},
    {File: "Project.schema.json", Val: new(models.Project)},
    {File: "DataSource.schema.json", Val: new(models.DataSource)},
    {File: "MyNewType.schema.json", Val: new(models.MyNewType)}, // Добавьте это
}
```

### 3. Обновите экспорты в index.ts

В `web/src/types/index.ts`:

```typescript
// JSON Schemas
export { default as MyNewTypeSchema } from "../types/schemas/MyNewType.schema.json";

// TypeScript types
export type {
  User,
  Project,
  DataSource,
  MyNewType, // Добавьте это
} from './generated';
```

### 4. Запустите генерацию

```bash
cd backend
go run ./internal/tools/schemagen

cd ../web
npm run gen:types
```

## Проверка типов

Убедитесь, что все типы корректны:

```bash
cd web
npx tsc --noEmit
```

## Troubleshooting

### Ошибка: "EEXIST: file already exists"

Если вы получаете ошибку при генерации типов, убедитесь, что `web/src/types/generated.ts` является файлом, а не директорией.

### Типы не обновляются

1. Проверьте, что Go модели содержат правильные JSON tags
2. Убедитесь, что модель добавлена в `schemagen/main.go`
3. Запустите генерацию вручную для диагностики:
   ```bash
   cd backend
   go run ./internal/tools/schemagen
   
   cd ../web
   npm run gen:types
   ```

### TypeScript ошибки компиляции

1. Убедитесь, что типы сгенерированы: проверьте `web/src/types/generated.ts`
2. Проверьте, что экспорты добавлены в `web/src/types/index.ts`
3. Запустите `npx tsc --noEmit` для диагностики

## Best Practices

### 1. Не редактируйте сгенерированные файлы вручную

Файлы `generated.ts` и `*.schema.json` генерируются автоматически. Изменения будут перезаписаны.

### 2. Используйте опциональные поля правильно

В Go:
```go
type Example struct {
    Required  string  `json:"required"`
    Optional  *string `json:"optional,omitempty"`
}
```

В TypeScript:
```typescript
interface Example {
  required: string;
  optional?: string;
}
```

### 3. Обработка времени

Go `time.Time` конвертируется в `string` с форматом `date-time` в JSON Schema. Во frontend:

```typescript
const createdAt: string = user.createdAt; // ISO 8601 string
const date: Date = new Date(user.createdAt); // Convert to Date object
```

### 4. Null vs Undefined

- В Go: `nil` указатели становятся `undefined` в TS (поля с `omitempty`)
- Убедитесь, что backend и frontend согласованы в обработке отсутствующих значений

### 5. Регулярно обновляйте типы

При изменении backend моделей, обязательно регенерируйте типы:
```bash
npm run gen:schemas && npm run gen:types
```

Или автоматически при сборке:
```bash
npm run build
```

## CI/CD Integration

В CI пайплайне типы генерируются автоматически при сборке frontend:

```yaml
- name: Build frontend
  run: |
    cd web
    npm ci
    npm run build  # включает gen:schemas и gen:types
```

Для production используется `build:ci`, который пропускает генерацию (типы должны быть закоммичены):

```yaml
- name: Build frontend (CI)
  run: |
    cd web
    npm ci
    npm run build:ci  # без генерации
```

## Примеры использования

### API запросы с типизацией

```typescript
import { User, Project } from '@/types';
import axios from 'axios';

// GET запрос
async function getUser(id: string): Promise<User> {
  const response = await axios.get<User>(`/api/users/${id}`);
  return response.data;
}

// POST запрос
async function createProject(data: Partial<Project>): Promise<Project> {
  const response = await axios.post<Project>('/api/projects', data);
  return response.data;
}
```

### State management с типами

```typescript
import { create } from 'zustand';
import { User, Project } from '@/types';

interface AppState {
  user: User | null;
  projects: Project[];
  setUser: (user: User) => void;
  setProjects: (projects: Project[]) => void;
}

const useStore = create<AppState>((set) => ({
  user: null,
  projects: [],
  setUser: (user) => set({ user }),
  setProjects: (projects) => set({ projects }),
}));
```

### Компоненты с пропсами

```typescript
import { User } from '@/types';

interface UserCardProps {
  user: User;
  onEdit?: (user: User) => void;
}

export function UserCard({ user, onEdit }: UserCardProps) {
  return (
    <div>
      <h3>{user.displayName || user.email}</h3>
      <p>Role: {user.role || 'N/A'}</p>
      {onEdit && <button onClick={() => onEdit(user)}>Edit</button>}
    </div>
  );
}
```

