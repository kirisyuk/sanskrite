# Verified URL Analysis - Implementation Analysis

## Обзор функциональности

Создать страницу `/verified` для верификации URL с автоматической загрузкой, анализом и генерацией публичной ссылки.

## Требования

1. **Страница `/verified`** - принимает URL, загружает его в источники и фетчит как системный тенант
2. **Использование модели** - gemini-fast с search grounding и что-то типа "Socratic Counterpoint"
3. **Защита от дублирования** - URL нельзя фетчить дважды, нужно брать из уже обработанных данных
4. **Публичная ссылка** - после генерации показывается домен/verified/<короткий_код> с возможностью copy url/share
5. **Единый стиль** - страница подчиняется общим правилам сайта (topbar, bottombar, цветовая схема, шрифты, многоязычность)
6. **Системный тенант** - запросы делаются от системного тенанта, если нет - создать при старте функции
7. **Защита публичных эндпоинтов** - возможно встроенный ключ из функции (не светится в логах)
8. **Просмотр результатов** - `/verified/код` показывает ранее созданный материал

## Анализ существующего кода

### 1. Системный тенант и аутентификация

**Местоположение:** `backend/internal/http/auth.go`

**Механизм:**
- Системный тенант определяется через заголовок `x-functions-key`
- При наличии заголовка создается `AuthInfo` с `IsSystem: true`, `TenantID: "system"`, `UID: "system"`
- Системные запросы обходят проверки подписки (`RequireSubscription`)

**Реализация:**
```go
if r.Header.Get("x-functions-key") != "" {
    authInfo := &AuthInfo{
        UID:      "system",
        Email:    "system",
        TenantID: "system",
        IsSystem: true,
    }
    ctx := context.WithValue(r.Context(), AuthContextKey{}, authInfo)
    next.ServeHTTP(w, r.WithContext(ctx))
    return
}
```

**Что нужно сделать:**
- При старте функции (Cloud Function или HTTP handler) проверять наличие системного пользователя
- Если нет - создать его через `UsersRepository.Upsert()` с `TenantID: "system"`, `ID: "system"`
- Использовать системный тенант для всех операций верификации

### 2. Загрузка URL и создание источников

**Местоположение:** 
- `backend/internal/crawler/crawler.go` - фетчинг URL
- `backend/internal/discovery/orchestrator.go` - обработка источников
- `backend/internal/repo/repositories.go` - DataSourcesRepository

**Механизм:**

**Ключевое понимание:**
- **Публичные источники хранятся глобально**: `web`, `rss`, `telegram_channel` имеют `TenantID: ""` и `Pk: "global"`
- **Дедупликация работает автоматически**: `FindByCanonicalSource()` ищет по каноническому URL
- **Документы создаются идемпотентно**: `GetByExternalID()` проверяет существование перед созданием

**Реализация:**

```go
// 1. Нормализация URL
canonicalURL := crawler.CanonicalizeURL(rawURL) // убирает query params, hash, нормализует

// 2. Поиск существующего источника (для публичных источников TenantID пустой)
existingSource, err := sourcesRepo.FindByCanonicalSource(ctx, "", canonicalURL)
// Если найден - используем его sourceID

// 3. Если не найден - создаем новый публичный источник
if existingSource == nil {
    ds := &models.DataSource{
        TenantID:    "",           // ПУСТОЙ для публичных источников!
        Pk:          "global",     // Глобальный ключ
        Kind:        "web",
        DisplayName: canonicalURL,
        Status:      "active",
        Details:     SourceDetails{URI: &canonicalURL},
        OwnerUserID: "system",    // или можно оставить пустым
        // ...
    }
    sourceID, err = sourcesRepo.Create(ctx, ds)
}

// 4. Загрузка и обработка через Orchestrator
orchestrator.IngestWeb(ctx, tenantID, sourceID, canonicalURL)
// Внутри IngestWeb:
//   - FetchContent() загружает URL
//   - GetByExternalID() проверяет существующий документ
//   - Если нет - создает Document с ExternalID = canonicalURL
//   - Чанкирует и эмбеддит синхронно
//   - Создает Chunks с эмбеддингами
```

**Важные детали:**
- `IngestWeb()` идемпотентна: если документ с таким `ExternalID` уже есть, он обновляется, а не дублируется
- `CanonicalizeURL()` нормализует URL: убирает query params, hash, trailing slash, приводит к lowercase
- Для системного тенанта можно использовать `TenantID: "system"` при создании документов, но сам источник должен быть глобальным (`TenantID: ""`)

**Что нужно сделать:**
- Использовать существующий механизм: `FindByCanonicalSource()` → `IngestWeb()`
- Для verified анализа можно:
  - **Вариант 1:** Создать системный проект и связать источник через `SourceProjects` (как в `ensureProjectSourceLink()`)
  - **Вариант 2:** Работать напрямую с документами источника без проекта (проще для verified)
- Документы автоматически чанкируются и эмбеддятся - их можно использовать для AI анализа
- `IngestWeb()` принимает `tenantID` для документов, но источник остается глобальным

**Дополнительно - связь источников с проектами:**
- `SourceProjectsRepository` связывает источники с проектами через таблицу `rag_source_projects`
- Для verified можно создать системный проект (`TenantID: "system"`) и связать источник
- Или работать напрямую с документами через `SourceID` без привязки к проекту

### 3. Генерация коротких кодов

**Местоположение:** `backend/internal/http/share.go`

**Механизм:**
- Используется функция `randomToken(n int)` с символами `a-zA-Z0-9`
- Для share токенов используется длина 48 символов
- Токены хранятся в `core_share_tokens` коллекции

**Реализация:**
```go
func randomToken(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    b := make([]byte, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}
```

**Что нужно сделать:**
- Создать аналогичный механизм для verified кодов
- Использовать более короткие коды (8-12 символов достаточно для уникальности)
- Хранить в новой коллекции `core_verified_analyses` или расширить существующую

### 4. Публичные эндпоинты

**Местоположение:** `backend/internal/http/share.go` - `GetPublicShare()`

**Механизм:**
- Публичные эндпоинты начинаются с `/api/v1/public/`
- Не требуют аутентификации (но можно добавить защиту через встроенный ключ)
- Возвращают данные в публичном формате

**Реализация:**
```go
// GET /api/v1/public/share/{token}
func (h *ShareHandler) GetPublicShare(w http.ResponseWriter, r *http.Request) {
    // Проверка токена, загрузка данных, возврат публичного формата
}
```

**Что нужно сделать:**
- Создать `/api/v1/public/verified/{code}` эндпоинт
- Опционально: проверять встроенный ключ из функции (env переменная `VERIFIED_API_KEY`)
- Возвращать результаты анализа в публичном формате

### 5. Фронтенд - роутинг и компоненты

**Местоположение:**
- `web/src/App.tsx` - роутинг
- `web/src/components/TopBar.tsx` - верхняя панель
- `web/src/components/BottomBar.tsx` - нижняя панель
- `web/src/i18n.ts` - многоязычность

**Механизм:**
- React Router используется для маршрутизации
- TopBar и BottomBar отображаются на всех страницах (кроме специальных)
- i18n настроен через `react-i18next` с поддержкой нескольких языков

**Реализация:**
```tsx
<Routes>
    <Route path="/shared/:token" element={<ShareViewPage />} />
    <Route path="/" element={...} />
</Routes>
```

**Что нужно сделать:**
- Добавить маршруты `/verified` и `/verified/:code` в `App.tsx`
- Создать компонент `VerifiedPage.tsx` (аналогично `ShareViewPage.tsx`)
- Добавить переводы в `i18n.ts` для всех текстов страницы
- Использовать TopBar и BottomBar для единообразия

### 6. AI анализ с gemini-fast и search grounding

**Местоположение:**
- `backend/internal/ai/vertex_service.go` - ExecuteSearch с search grounding
- `backend/internal/ai/model_manager.go` - управление моделями
- `backend/internal/genai/gemini/client.go` - Gemini клиент

**Механизм:**
- Gemini модели поддерживают search grounding через `googleSearch` tool
- Модель `gemini-fast` должна быть настроена в `ModelManager`
- Search grounding включается через параметр `tools` в запросе

**Реализация:**
```go
requestBody := map[string]interface{}{
    "model": fmt.Sprintf("models/%s", modelName),
    "tools": []map[string]interface{}{
        {"googleSearch": map[string]interface{}{}},
    },
}
```

**Что нужно сделать:**
- Определить промпт для "Socratic Counterpoint" анализа
- Использовать модель с search grounding (например, `gemini-1.5-flash` или `gemini-1.5-pro`)
- Вызвать через `AITaskExecutor.ExecuteTask()` или напрямую через `VertexAIService`

### 7. Структура данных для Verified Analysis

**Важное уточнение после изучения кода:**

Система источников уже умеет:
- Хранить публичные источники глобально (`TenantID: ""`, `Pk: "global"`)
- Дедуплицировать по каноническому URL
- Создавать документы идемпотентно (через `ExternalID`)
- Чанкировать и эмбеддить контент

**Новая модель данных:**

```go
type VerifiedAnalysis struct {
    ID          string    `json:"id" firestore:"id"`
    Code        string    `json:"code" firestore:"code"` // короткий код (8-12 символов)
    URL         string    `json:"url" firestore:"url"`   // оригинальный URL
    CanonicalURL string   `json:"canonicalUrl" firestore:"canonicalUrl"` // для дедупликации
    TenantID    string    `json:"tenantId" firestore:"tenantId"` // "system"
    SourceID    string    `json:"sourceId" firestore:"sourceId"` // ID глобального источника (TenantID="")
    DocumentID  string    `json:"documentId" firestore:"documentId"` // ID документа из core_docs
    Analysis    string    `json:"analysis" firestore:"analysis"` // результат анализа
    Model       string    `json:"model" firestore:"model"` // использованная модель
    Status      string    `json:"status" firestore:"status"` // "pending" | "processing" | "completed" | "failed"
    Error       *string   `json:"error,omitempty" firestore:"error,omitempty"`
    CreatedAt   time.Time `json:"createdAt" firestore:"createdAt"`
    UpdatedAt   time.Time `json:"updatedAt" firestore:"updatedAt"`
    ExpiresAt   *time.Time `json:"expiresAt,omitempty" firestore:"expiresAt,omitempty"` // опционально
}
```

**Коллекция:** `core_verified_analyses`

**Индексы:**
- `code` (unique)
- `canonicalUrl` (для поиска существующих анализов по URL)
- `sourceId` (для быстрого поиска по источнику)
- `status`

**Связь с существующей системой:**
- `SourceID` указывает на глобальный источник в `core_datasources` (TenantID="")
- `DocumentID` указывает на документ в `core_docs` с этим источником
- Можно использовать существующие документы и чанки для анализа

## План реализации

### Backend

1. **Создать репозиторий для Verified Analysis**
   - `backend/internal/repo/verified_analyses.go`
   - Методы: `Create()`, `GetByCode()`, `GetByURL()`, `Update()`

2. **Создать HTTP handler**
   - `backend/internal/http/verified.go`
   - Эндпоинты:
     - `POST /api/v1/verified` - создать анализ (системный запрос)
     - `GET /api/v1/public/verified/{code}` - получить публичный анализ
   - Защита через `x-functions-key` для создания

3. **Создать сервис для обработки**
   - **Использовать существующую систему источников:**
     - `crawler.CanonicalizeURL()` для нормализации
     - `sourcesRepo.FindByCanonicalSource(ctx, "", canonicalURL)` для поиска существующего источника
     - Если источник существует - использовать его `SourceID`
     - Если нет - создать новый с `TenantID: ""`, `Pk: "global"`
   - **Использовать Orchestrator для обработки:**
     - `orchestrator.IngestWeb(ctx, "system", sourceID, canonicalURL)` для загрузки и обработки
     - Метод автоматически создаст/обновит документ и чанки
     - Получить `DocumentID` через `docsRepo.GetByExternalID(ctx, "system", sourceID, canonicalURL)`
   - **Запуск анализа через AI:**
     - Использовать документ и чанки для контекста
     - Запустить анализ с gemini-fast + search grounding
   - **Генерация короткого кода:**
     - Использовать `randomToken(10)` для генерации
     - Проверить уникальность в `core_verified_analyses`
   - **Сохранение результата:**
     - Сохранить `VerifiedAnalysis` с ссылками на `SourceID` и `DocumentID`

4. **Интеграция с системным тенантом**
   - Проверка/создание системного пользователя при старте
   - Использование системного тенанта для всех операций

### Frontend

1. **Создать страницу VerifiedPage**
   - `web/src/pages/VerifiedPage.tsx`
   - Форма ввода URL
   - Отображение прогресса обработки
   - Показ результата с кнопками copy/share

2. **Создать страницу VerifiedViewPage**
   - `web/src/pages/VerifiedViewPage.tsx`
   - Показ ранее созданного анализа
   - Публичный доступ без аутентификации

3. **Добавить роутинг**
   - `/verified` - форма создания
   - `/verified/:code` - просмотр результата

4. **Добавить переводы**
   - Все тексты в `i18n.ts`
   - Поддержка всех языков

5. **API методы**
   - `api.verifiedCreate(url: string)` - создать анализ
   - `api.verifiedGetPublic(code: string)` - получить публичный анализ

## Вопросы для уточнения

1. **Длина короткого кода:** 8, 10 или 12 символов?
2. **Срок хранения:** нужен ли `expiresAt` или хранить бессрочно?
3. **Socratic Counterpoint промпт:** какой именно промпт использовать для анализа?
4. **Модель:** какая именно модель gemini-fast с search grounding доступна?
5. **Защита публичных эндпоинтов:** нужна ли дополнительная защита или достаточно короткого кода?
6. **Ограничения:** нужны ли rate limits или другие ограничения?

## Зависимости

- Существующие компоненты работают корректно
- Системный тенант может быть создан
- Firestore доступен для записи
- AI сервисы настроены и работают
- Frontend может делать публичные запросы без аутентификации

## Следующие шаги

1. Уточнить детали у пользователя
2. Создать структуру данных и репозиторий
3. Реализовать backend логику
4. Реализовать frontend компоненты
5. Добавить переводы
6. Протестировать функциональность

