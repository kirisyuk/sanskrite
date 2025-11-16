# Tavily Web Search Testing

## Скрипты для тестирования

### 1. Основной тестовый скрипт (bash)

```bash
./scripts/test-tavily-web-search.sh
```

**Что тестирует:**
- ✅ Прямое подключение к Tavily API
- ✅ Web search tool через GenAI API endpoint
- ✅ Проверка конфигурации секретов в Cloud Run
- ✅ Проверка наличия секрета в GCP Secret Manager

**Переменные окружения:**
- `API_URL` - URL API (по умолчанию: production URL)
- `TEST_QUERY` - Тестовый запрос (по умолчанию: "latest news about AI")
- `TAVILY_API_KEY` - API ключ (автоматически загружается из GCP secrets)

**Примеры использования:**
```bash
# Базовый тест
./scripts/test-tavily-web-search.sh

# С кастомным запросом
TEST_QUERY="quantum computing news" ./scripts/test-tavily-web-search.sh

# С кастомным API URL (для тестового окружения)
API_URL="https://test-api.example.com" ./scripts/test-tavily-web-search.sh
```

### 2. Локальный тест tool executor (Go)

```bash
cd backend
TAVILY_API_KEY="your-key" go run ./cmd/test-web-search-tool "your search query"
```

**Что тестирует:**
- ✅ Прямое выполнение web_search tool executor
- ✅ Преобразование результатов Tavily в формат tool response
- ✅ Валидация структуры ответа

**Пример:**
```bash
cd backend
export TAVILY_API_KEY=$(gcloud secrets versions access latest --secret=tavily_api_key)
go run ./cmd/test-web-search-tool "latest AI developments"
```

## Структура тестов

### Test 1: Direct Tavily API
Проверяет прямое подключение к Tavily Search API без промежуточных слоев.

### Test 2: Web Search Tool via GenAI API
Тестирует интеграцию через `/api/v1/genai/complete-with-tools` endpoint.

**Требования:**
- Валидный API ключ Tavily
- Доступ к Cloud Run сервису (или локальный запуск)
- Для полного теста может потребоваться аутентификация

### Test 3: Direct Tool Executor (локально)
Тестирует executor напрямую, минуя HTTP слой.

### Test 4: Cloud Run Configuration
Проверяет что секреты правильно настроены в Cloud Run.

## Устранение проблем

### "Unauthorized: missing or invalid API key"
- Проверьте что ключ правильно сохранен в GCP Secret Manager
- Убедитесь что ключ активен в Tavily dashboard
- Проверьте формат ключа (не должно быть лишних пробелов/переносов строк)

### "No tool calls detected"
- Убедитесь что `toolCallingEnabled: true` в запросе
- Проверьте что tool schema правильно определен
- Проверьте логи Cloud Run для деталей ошибки

### "Authentication required"
- Для тестирования через API нужен валидный токен
- Используйте локальный тест (`test-web-search-tool`) для обхода аутентификации

## Интеграция в CI/CD

Скрипт можно использовать в CI/CD пайплайне:

```yaml
# Пример для GitHub Actions
- name: Test Tavily Integration
  run: |
    export TAVILY_API_KEY=${{ secrets.TAVILY_API_KEY }}
    ./scripts/test-tavily-web-search.sh
```

## Примечания

- Dev ключи Tavily могут иметь ограничения или быть неактивными
- Для production используйте валидный production ключ
- Скрипт продолжает выполнение даже если прямой API тест провалился (для проверки других компонентов)

