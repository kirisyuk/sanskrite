# Возможности BigQuery/Vertex AI для сложных запросов к данным в GCS

## Обзор возможностей

Система может обрабатывать **очень сложные** запросы благодаря комбинации:
- **BigQuery Object Tables** - прямое чтение файлов из GCS
- **Vertex AI Gemini** - генерация сложных SQL запросов
- **BigQuery ML** - анализ неструктурированного контента прямо в SQL

## Что система МОЖЕТ делать

### 1. Сложные аналитические запросы

**Пример: "Найди все документы, где упоминается продукт X, и покажи статистику по датам"**
```sql
WITH product_mentions AS (
  SELECT 
    m.id,
    m.title,
    m.createdAt,
    m.documentType,
    o.uri,
    -- Используем ML для поиска упоминаний продукта
    ML.GENERATE_TEXT(
      MODEL `h1horizon-488615.h1horizon_488615_documents.gemini_model`,
      (SELECT data FROM `h1horizon-488615.h1horizon_488615_documents.documents_object_table` o2 
       WHERE o2.uri = REPLACE(m.storagePath, 'gs://', '')),
      STRUCT('Does this document mention product X? Answer yes or no.' AS prompt)
    ) AS mentions_product
  FROM `h1horizon-488615.h1horizon_488615_documents.documents_metadata` m
  LEFT JOIN `h1horizon-488615.h1horizon_488615_documents.documents_object_table` o
    ON o.uri = REPLACE(m.storagePath, 'gs://', '')
  WHERE m.createdAt >= DATE_SUB(CURRENT_DATE(), INTERVAL 1 YEAR)
),
filtered AS (
  SELECT * FROM product_mentions
  WHERE mentions_product LIKE '%yes%'
)
SELECT 
  DATE(createdAt) as date,
  documentType,
  COUNT(*) as mention_count,
  COUNT(DISTINCT id) as unique_documents
FROM filtered
GROUP BY date, documentType
ORDER BY date DESC, mention_count DESC
LIMIT 100
```

### 2. Анализ JSON документов с вложенными структурами

**Пример: "Покажи все решения (decisions) из JSON файлов за последний месяц"**
```sql
WITH json_docs AS (
  SELECT 
    m.id,
    m.filename,
    m.projectId,
    m.createdAt,
    j.data as json_content
  FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
  JOIN `h1horizon-488615.h1horizon_488615_documents.core_docs_json_table` j
    ON j.uri = REPLACE(m.storagePath, 'gs://', '')
  WHERE m.contentType = 'application/json'
    AND m.filename LIKE '%decisions.json'
    AND m.createdAt >= DATE_SUB(CURRENT_DATE(), INTERVAL 1 MONTH)
),
decisions_flattened AS (
  SELECT 
    id,
    filename,
    projectId,
    createdAt,
    JSON_EXTRACT_SCALAR(json_content, '$.decisions[0].title') as decision_title,
    JSON_EXTRACT_SCALAR(json_content, '$.decisions[0].status') as decision_status,
    JSON_EXTRACT_SCALAR(json_content, '$.decisions[0].date') as decision_date
  FROM json_docs
  WHERE JSON_EXTRACT(json_content, '$.decisions') IS NOT NULL
)
SELECT * FROM decisions_flattened
ORDER BY createdAt DESC
LIMIT 100
```

### 3. Кросс-форматный анализ (текст + JSON + метаданные)

**Пример: "Сравни содержимое текстовых документов с решениями из JSON файлов того же проекта"**
```sql
WITH text_docs AS (
  SELECT 
    m.id,
    m.projectId,
    m.filename,
    m.extractedText as content,
    'text' as source_type
  FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
  WHERE m.contentType LIKE 'text/%'
    AND m.extractedText IS NOT NULL
),
json_decisions AS (
  SELECT 
    m.id,
    m.projectId,
    m.filename,
    JSON_EXTRACT_SCALAR(j.data, '$.decisions[0].description') as content,
    'json' as source_type
  FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
  JOIN `h1horizon-488615.h1horizon_488615_documents.core_docs_json_table` j
    ON j.uri = REPLACE(m.storagePath, 'gs://', '')
  WHERE m.contentType = 'application/json'
    AND m.filename LIKE '%decisions.json'
),
combined AS (
  SELECT * FROM text_docs
  UNION ALL
  SELECT * FROM json_decisions
)
SELECT 
  projectId,
  source_type,
  COUNT(*) as doc_count,
  AVG(LENGTH(content)) as avg_content_length,
  SUM(LENGTH(content)) as total_chars
FROM combined
GROUP BY projectId, source_type
ORDER BY projectId, source_type
LIMIT 100
```

### 4. Семантический поиск и анализ

**Пример: "Найди документы, которые говорят о проблемах с безопасностью"**
```sql
WITH security_analysis AS (
  SELECT 
    m.id,
    m.title,
    m.storagePath,
    ML.GENERATE_TEXT(
      MODEL `h1horizon-488615.h1horizon_488615_documents.gemini_model`,
      CONCAT('Document: ', COALESCE(m.extractedText, '')),
      STRUCT('Does this document discuss security issues or vulnerabilities? Answer: yes or no, and briefly explain why.' AS prompt)
    ) AS security_check
  FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
  WHERE m.extractedText IS NOT NULL
    AND LENGTH(m.extractedText) > 100
)
SELECT 
  id,
  title,
  storagePath,
  security_check
FROM security_analysis
WHERE security_check LIKE '%yes%'
LIMIT 100
```

### 5. Временной анализ с группировкой

**Пример: "Покажи динамику создания документов по типам за последние 6 месяцев"**
```sql
WITH monthly_stats AS (
  SELECT 
    DATE_TRUNC(DATE(createdAt), MONTH) as month,
    documentType,
    COUNT(*) as doc_count,
    COUNT(DISTINCT tenantId) as unique_tenants,
    AVG(sizeBytes) as avg_size
  FROM `h1horizon-488615.h1horizon_488615_documents.documents_metadata`
  WHERE createdAt >= DATE_SUB(CURRENT_DATE(), INTERVAL 6 MONTH)
  GROUP BY month, documentType
),
ranked AS (
  SELECT 
    month,
    documentType,
    doc_count,
    unique_tenants,
    avg_size,
    ROW_NUMBER() OVER (PARTITION BY month ORDER BY doc_count DESC) as rank
  FROM monthly_stats
)
SELECT * FROM ranked
WHERE rank <= 5
ORDER BY month DESC, doc_count DESC
LIMIT 100
```

### 6. Извлечение структурированных данных из неструктурированного текста

**Пример: "Извлеки все даты, имена людей и суммы денег из документов проекта X"**
```sql
WITH extracted_data AS (
  SELECT 
    m.id,
    m.filename,
    ML.GENERATE_TEXT(
      MODEL `h1horizon-488615.h1horizon_488615_documents.gemini_model`,
      CONCAT('Extract structured data from this document:\n', m.extractedText),
      STRUCT('Extract: dates (YYYY-MM-DD format), person names, monetary amounts. Return as JSON: {"dates": [...], "names": [...], "amounts": [...]}' AS prompt)
    ) AS extracted_json
  FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
  WHERE m.projectId = 'project-x'
    AND m.extractedText IS NOT NULL
)
SELECT 
  id,
  filename,
  JSON_EXTRACT(extracted_json, '$.dates') as dates,
  JSON_EXTRACT(extracted_json, '$.names') as names,
  JSON_EXTRACT(extracted_json, '$.amounts') as amounts
FROM extracted_data
WHERE extracted_json IS NOT NULL
LIMIT 100
```

## Что система НЕ МОЖЕТ делать (ограничения)

### 1. Ограничения BigQuery Object Tables

**❌ Нельзя:**
- Читать бинарные файлы (PDF, DOCX) напрямую через Object Tables
- Парсить сложные форматы без предварительной обработки
- Читать файлы с вложенными архивами (ZIP, TAR)

**✅ Решение:**
- Использовать поле `extractedText` из метаданных для PDF/DOCX
- Предварительно обрабатывать файлы перед загрузкой в GCS

### 2. Ограничения размера и производительности

**❌ Нельзя:**
- Обрабатывать файлы > 100MB через Object Tables
- Выполнять запросы, которые сканируют миллионы файлов за раз
- Использовать сложные JOIN между Object Tables (медленно)

**✅ Решение:**
- Использовать фильтры по URI (WHERE uri LIKE '...')
- Ограничивать количество файлов через LIMIT
- Использовать метаданные таблицы для предварительной фильтрации

### 3. Ограничения Vertex AI генерации SQL

**❌ Может ошибиться:**
- При очень сложных многошаговых запросах
- При запросах с неоднозначной формулировкой
- При запросах, требующих знаний о бизнес-логике

**✅ Решение:**
- Система возвращает сгенерированный SQL для проверки
- Можно исправить SQL вручную и выполнить повторно
- Использовать более конкретные формулировки запросов

### 4. Ограничения BigQuery ML

**❌ Нельзя:**
- Использовать ML функции без настроенной модели
- Вызывать ML функции слишком часто (лимиты API)
- Обрабатывать очень длинные тексты (> 32K токенов)

**✅ Решение:**
- Использовать `extractedText` из метаданных (уже обработан)
- Разбивать длинные документы на части
- Кэшировать результаты ML запросов

## Примеры "идиотских" запросов, которые система ОБРАБОТАЕТ

### 1. Очень расплывчатый запрос
**Запрос:** "Что там у нас с документами?"

**Система сгенерирует:**
```sql
SELECT 
  documentType,
  COUNT(*) as total_count,
  COUNT(DISTINCT tenantId) as unique_tenants,
  MIN(createdAt) as oldest_doc,
  MAX(createdAt) as newest_doc
FROM `h1horizon-488615.h1horizon_488615_documents.documents_metadata`
GROUP BY documentType
ORDER BY total_count DESC
LIMIT 100
```

### 2. Запрос с противоречивыми требованиями
**Запрос:** "Покажи все документы, но только те, которых нет"

**Система сгенерирует:**
```sql
-- AI поймет, что это невозможно, и вернет пустой результат или ошибку
SELECT * FROM `h1horizon-488615.h1horizon_488615_documents.documents_metadata`
WHERE 1 = 0
LIMIT 100
```

### 3. Запрос на несуществующие данные
**Запрос:** "Покажи все документы про марсиан"

**Система сгенерирует:**
```sql
-- AI попытается найти через ML анализ
SELECT 
  m.id,
  m.title,
  ML.GENERATE_TEXT(
    MODEL `h1horizon-488615.h1horizon_488615_documents.gemini_model`,
    m.extractedText,
    STRUCT('Does this document mention Martians or Mars? Answer yes or no.' AS prompt)
  ) AS mentions_mars
FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
WHERE m.extractedText IS NOT NULL
LIMIT 100
```

### 4. Очень сложный многошаговый запрос
**Запрос:** "Найди все документы, где упоминается продукт X, сравни их с решениями из JSON файлов, найди пересечения по проектам, и покажи статистику по месяцам"

**Система сгенерирует:**
```sql
WITH product_docs AS (
  SELECT m.*, 'product_mention' as source
  FROM `h1horizon-488615.h1horizon_488615_documents.documents_metadata` m
  WHERE m.extractedText LIKE '%product X%' OR m.title LIKE '%product X%'
),
json_decisions AS (
  SELECT 
    m.*,
    JSON_EXTRACT_SCALAR(j.data, '$.decisions[0].title') as decision_title,
    'json_decision' as source
  FROM `h1horizon-488615.h1horizon_488615_documents.core_docs_metadata` m
  JOIN `h1horizon-488615.h1horizon_488615_documents.core_docs_json_table` j
    ON j.uri = REPLACE(m.storagePath, 'gs://', '')
  WHERE m.contentType = 'application/json'
),
project_intersection AS (
  SELECT DISTINCT projectId
  FROM product_docs
  WHERE projectId IS NOT NULL
  INTERSECT DISTINCT
  SELECT DISTINCT projectId
  FROM json_decisions
  WHERE projectId IS NOT NULL
),
combined AS (
  SELECT 
    projectId,
    DATE_TRUNC(DATE(createdAt), MONTH) as month,
    source,
    COUNT(*) as doc_count
  FROM (
    SELECT projectId, createdAt, source FROM product_docs
    UNION ALL
    SELECT projectId, createdAt, source FROM json_decisions
  )
  WHERE projectId IN (SELECT projectId FROM project_intersection)
  GROUP BY projectId, month, source
)
SELECT * FROM combined
ORDER BY month DESC, projectId, source
LIMIT 100
```

## Рекомендации для сложных запросов

### ✅ Делайте так:
1. **Конкретизируйте запросы:** "Покажи документы проекта X за последний месяц" вместо "Покажи документы"
2. **Используйте фильтры:** Указывайте tenantId, projectId, даты для ускорения
3. **Разбивайте сложные запросы:** Лучше несколько простых запросов, чем один очень сложный
4. **Проверяйте SQL:** Система возвращает сгенерированный SQL - проверьте его перед выполнением

### ❌ Избегайте:
1. **Слишком общие запросы:** "Покажи всё" - может вернуть миллионы строк
2. **Запросы без фильтров:** Всегда указывайте временные рамки или другие фильтры
3. **Очень длинные тексты:** Разбивайте анализ больших документов на части
4. **Слишком частые ML запросы:** Кэшируйте результаты анализа

## Итоговая оценка возможностей

### ✅ Система ОЧЕНЬ мощная и может:
- Обрабатывать запросы любой сложности (в пределах BigQuery)
- Работать с данными напрямую из GCS без предварительной обработки
- Использовать AI для анализа неструктурированного контента
- Создавать сложные аналитические запросы с множеством JOIN и подзапросов
- Работать с JSON, текстом, метаданными одновременно

### ⚠️ Но есть ограничения:
- Размер файлов и производительность
- Сложность бинарных форматов (PDF, DOCX)
- Лимиты API Vertex AI
- Точность генерации SQL зависит от формулировки запроса

### 💡 Вывод:
Система может обработать **практически любые** запросы, включая очень сложные и "идиотские", но:
- Чем конкретнее запрос - тем лучше результат
- Сложные запросы могут выполняться долго
- Всегда проверяйте сгенерированный SQL перед выполнением

