# BigQuery + Vertex AI Deep Integration Guide

## Современный подход: Единое пространство для анализа

Вместо параллельного использования двух сервисов, мы настраиваем **глубокую интеграцию**, где BigQuery становится универсальным аналитическим интерфейсом ко всем данным, а Vertex AI — "мозгом" для понимания неструктурированного контента прямо в SQL-запросах.

## Ключевые возможности

### 1. BigQuery Object Tables
Прямое чтение файлов из GCS как обычных таблиц:
```sql
-- Читаем документы из GCS напрямую
SELECT * FROM `sanskrite.sanskrite_documents.documents_object_table`
WHERE uri LIKE '%tenant123%'
```

### 2. BigQuery ML с Vertex AI
Вызов моделей Vertex AI (Gemini) прямо из SQL:
```sql
-- Извлекаем структурированные данные из неструктурированного текста
SELECT 
  id,
  title,
  ML.GENERATE_TEXT(
    MODEL `sanskrite.sanskrite_documents.gemini_model`,
    (SELECT content FROM documents_object_table WHERE uri = storagePath),
    STRUCT('Extract: product names, prices, dates, sentiment' AS prompt)
  ) AS extracted_data
FROM documents_metadata
```

### 3. Структурирование на лету
Один SQL-запрос для извлечения и анализа:
```sql
-- Пример: Извлечь баллы GTI из PDF-отчетов и связать с продажами
WITH document_insights AS (
  SELECT 
    m.id,
    m.storagePath,
    ML.GENERATE_TEXT(
      MODEL `sanskrite.sanskrite_documents.gemini_model`,
      (SELECT content FROM documents_object_table WHERE uri = m.storagePath),
      STRUCT('Extract GTI score and product name from this pharmaceutical report' AS prompt)
    ) AS insights
  FROM documents_metadata m
  WHERE m.documentType = 'pharmaceutical_report'
),
parsed_insights AS (
  SELECT 
    id,
    JSON_EXTRACT_SCALAR(insights, '$.product_name') AS product_name,
    CAST(JSON_EXTRACT_SCALAR(insights, '$.gti_score') AS FLOAT64) AS gti_score
  FROM document_insights
)
SELECT 
  s.transaction_id,
  s.product_name,
  s.sales_amount,
  s.sale_date,
  p.gti_score,
  p.gti_score * s.sales_amount AS toxicity_weighted_sales
FROM sales s
JOIN parsed_insights p ON p.product_name = s.product_name
ORDER BY toxicity_weighted_sales DESC
```

## Настройка

### 1. Object Tables для GCS
```bash
# Создать connection для Object Tables
bq mk --connection --location=US --connection_type=GCS \
  --project_id=sanskrite gcs-connection

# Создать Object Table
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE EXTERNAL TABLE \`sanskrite.sanskrite_documents.documents_object_table\`
WITH CONNECTION \`sanskrite.us.gcs-connection\`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-rag-documents/**/*.txt']
)
EOF
```

### 2. BigQuery ML с Vertex AI
```bash
# Создать connection для Vertex AI
bq mk --connection --location=US --connection_type=CLOUD_RESOURCE \
  --project_id=sanskrite vertex-ai-connection

# Создать remote model
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE MODEL \`sanskrite.sanskrite_documents.gemini_model\`
REMOTE WITH CONNECTION \`sanskrite.us.vertex-ai-connection\`
OPTIONS (
  remote_service_type = 'CLOUD_AI_LARGE_LANGUAGE_MODEL_V1',
  endpoint = 'us-central1-aiplatform.googleapis.com'
)
EOF
```

### 3. Firebase Extension для Firestore → BigQuery
```bash
# Установить extension для потокового экспорта
firebase ext:install firebase/firestore-bigquery-export --project=sanskrite

# Настроить экспорт коллекции rag_documents
# Extension автоматически создаст таблицу и будет стримить изменения
```

## Преимущества подхода

1. **Единый интерфейс**: Все данные доступны через SQL
2. **На лету**: Структурирование происходит в запросе, без предварительной обработки
3. **JOIN с любыми данными**: Документы можно связать с продажами, пользователями и т.д.
4. **Масштабируемость**: BigQuery обрабатывает большие объемы данных
5. **Реальное время**: Firebase Extension стримит изменения из Firestore

## Примеры использования

### Анализ тональности документов
```sql
SELECT 
  documentType,
  AVG(CAST(JSON_EXTRACT_SCALAR(insights, '$.sentiment_score') AS FLOAT64)) AS avg_sentiment
FROM (
  SELECT 
    m.documentType,
    ML.GENERATE_TEXT(
      MODEL `sanskrite.sanskrite_documents.gemini_model`,
      (SELECT content FROM documents_object_table WHERE uri = m.storagePath),
      STRUCT('Analyze sentiment: return JSON with sentiment_score 0-1' AS prompt)
    ) AS insights
  FROM documents_metadata m
)
GROUP BY documentType
```

### Поиск документов по смыслу + аналитика
```sql
-- Используем Vertex AI Search для поиска, затем анализируем в BigQuery
WITH relevant_docs AS (
  -- Результаты из Vertex AI Search API (можно интегрировать)
  SELECT id FROM UNNEST(['doc1', 'doc2', 'doc3']) AS id
)
SELECT 
  m.*,
  ML.GENERATE_TEXT(
    MODEL `sanskrite.sanskrite_documents.gemini_model`,
    (SELECT content FROM documents_object_table WHERE uri = m.storagePath),
    STRUCT('Summarize key points' AS prompt)
  ) AS summary
FROM documents_metadata m
JOIN relevant_docs r ON r.id = m.id
```

## Следующие шаги

1. ✅ Настроить Object Tables для GCS
2. ✅ Настроить BigQuery ML connection с Vertex AI
3. ✅ Установить Firebase Extension для Firestore → BigQuery
4. ✅ Создать примеры SQL-запросов для ваших use cases
5. ✅ Настроить автоматические дашборды в Data Studio/Looker

