# Проверка работы Tavily Web Search

## Текущий статус

✅ **Код готов и задеплоен:**
- TavilyClient реализован
- WebSearchToolExecutor использует Tavily
- TAVILY_API_KEY настроен в Cloud Run через секреты
- Ревизия: `h1horizon-api-00469-jht`

⚠️ **Примечание:** Dev ключи могут работать только из Cloud Run (IP whitelist или активация при первом использовании)

## Как проверить что всё работает

### Вариант 1: Через UI
1. Откройте приложение
2. Используйте GenAI чат с web_search tool
3. Попросите модель выполнить поиск, например: "Найди последние новости об AI"

### Вариант 2: Через API (с аутентификацией)
```bash
# Получите токен аутентификации, затем:
curl -X POST "https://h1horizon-api-sqt63d2k2q-uc.a.run.app/api/v1/genai/complete-with-tools" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Use web_search to find: latest AI news",
    "tools": [{
      "name": "web_search",
      "description": "Search the web",
      "parameters": {
        "type": "object",
        "properties": {
          "query": {"type": "string"},
          "limit": {"type": "integer"}
        },
        "required": ["query"]
      }
    }],
    "options": {
      "modelOverride": "gemini_fast"
    }
  }'
```

### Вариант 3: Проверка логов после вызова
```bash
# После вызова web_search tool, проверьте логи:
gcloud logging read \
  'resource.type=cloud_run_revision AND 
   resource.labels.service_name=h1horizon-api AND 
   (textPayload~"Tavily" OR textPayload~"WebSearch" OR textPayload~"web_search")' \
  --limit=20 \
  --format="table(timestamp,textPayload)" \
  --project=h1horizon-488615 \
  --freshness=10m
```

## Что искать в логах

**Успешный вызов:**
```
[WebSearch] Tavily client initialized
[WebSearch] Searching for: <query> (limit: X)
[WebSearch] Tavily response: query=<query>, resultsCount=X, responseTime=X.XXs
[WebSearch] Extracted X results from Tavily
[WebSearch] Found X results
```

**Ошибка (если ключ невалидный):**
```
[WebSearch] Error: tavily search failed: Tavily API error 401: ...
```

**Ошибка (если ключ не настроен):**
```
[WebSearch] WARNING: TAVILY_API_KEY not set, web search will fail
```

## Диагностика проблем

### Если видите ошибку 401:
1. Проверьте что ключ правильный в секретах:
   ```bash
   gcloud secrets versions access latest --secret=tavily_api_key --project=h1horizon-488615
   ```

2. Убедитесь что ключ активирован в Tavily dashboard

3. Проверьте что Cloud Run имеет доступ к секрету:
   ```bash
   gcloud run services describe h1horizon-api \
     --region=us-central1 \
     --format="get(spec.template.spec.containers[0].env)" \
     --project=h1horizon-488615 | grep TAVILY
   ```

### Если tool не вызывается:
1. Убедитесь что `toolCallingEnabled: true` в запросе
2. Проверьте что tool schema правильно определен
3. Проверьте логи GenAI для ошибок tool calling

## Обновление ключа (если нужно)

```bash
# Обновить ключ в секретах:
echo -n "NEW_KEY_HERE" | gcloud secrets versions add tavily_api_key --data-file=-

# Cloud Run автоматически подхватит новый ключ (используется :latest)
# Перезапуск не требуется, но можно принудительно:
gcloud run services update h1horizon-api --region=us-central1 --project=h1horizon-488615
```

## Мониторинг

Для постоянного мониторинга работы Tavily:
```bash
# Создать alert на ошибки Tavily:
gcloud logging metrics create tavily_errors \
  --description="Tavily API errors" \
  --log-filter='resource.type=cloud_run_revision AND 
                resource.labels.service_name=h1horizon-api AND 
                textPayload~"Tavily API error"'
```

