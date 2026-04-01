# BigQuery + Vertex AI Deep Integration Guide

## Современный подход: Единое пространство для анализа

Вместо параллельного использования двух сервисов, мы настраиваем **глубокую интеграцию**, где BigQuery становится универсальным аналитическим интерфейсом ко всем данным, а Vertex AI — "мозгом" для понимания неструктурированного контента прямо в SQL-запросах.

## Ключевые возможности

### 1. BigQuery Object Tables
Прямое чтение файлов из GCS как обычных таблиц. Поддерживаются все форматы документов системы:

**RAG Documents** (всегда `.txt`):
- Путь: `gs://{project}-rag-documents/{tenantId}/{sourceId}/{docId}.txt`
- Формат: текстовые файлы с извлеченным контентом

**CoreDocs** (разные форматы):
- Текстовые: `.txt`, `.md`, `.csv`, `.tsv`
- JSON: `.json` (decisions.json, facts.json, events.json и т.д.)
- PDF: `.pdf` (application/pdf)
- Word: `.docx` (application/vnd.openxmlformats-officedocument.wordprocessingml.document)
- Другие бинарные форматы

```sql
-- Читаем текстовые документы из GCS напрямую
SELECT * FROM `nv-horizon.nv_horizon_documents.documents_object_table`
WHERE uri LIKE '%tenant123%'

-- Читаем JSON документы
SELECT * FROM `nv-horizon.nv_horizon_documents.core_docs_json_table`
WHERE uri LIKE '%.json'
```

### 2. BigQuery ML с Vertex AI
Вызов моделей Vertex AI (Gemini) прямо из SQL:
```sql
-- Извлекаем структурированные данные из неструктурированного текста
SELECT 
  id,
  title,
  ML.GENERATE_TEXT(
    MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
    (SELECT content FROM documents_object_table WHERE uri = storagePath),
    STRUCT('Extract: product names, prices, dates, sentiment' AS prompt)
  ) AS extracted_data
FROM documents_metadata
```

### 3. Динамическое создание таблиц
BigQuery может создавать таблицы динамически из результатов запросов:

**CREATE TABLE AS SELECT (CTAS):**
```sql
-- Создать таблицу с результатами анализа
CREATE TABLE `nv-horizon.nv_horizon_documents.document_analysis_results` AS
SELECT 
  documentType,
  COUNT(*) as count,
  AVG(LENGTH(extractedText)) as avg_length
FROM `nv-horizon.nv_horizon_documents.documents_metadata`
GROUP BY documentType
```

**WITH clauses (CTEs) - временные таблицы в запросе:**
```sql
WITH filtered_docs AS (
  SELECT * FROM `nv-horizon.nv_horizon_documents.documents_metadata`
  WHERE createdAt > '2024-01-01'
),
aggregated AS (
  SELECT documentType, COUNT(*) as count
  FROM filtered_docs
  GROUP BY documentType
)
SELECT * FROM aggregated ORDER BY count DESC
```

### 4. Структурирование на лету
Один SQL-запрос для извлечения и анализа всех типов документов:

**Пример 1: Анализ текстовых документов (RAG и CoreDocs)**
```sql
WITH document_insights AS (
  SELECT 
    m.id,
    m.storagePath,
    m.contentType,
    ML.GENERATE_TEXT(
      MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
      (SELECT data FROM documents_object_table WHERE uri = REPLACE(m.storagePath, 'gs://', '')),
      STRUCT('Extract key entities, sentiment, and main topics' AS prompt)
    ) AS insights
  FROM documents_metadata m
  WHERE m.status = 'processed'
),
```

**Пример 2: Анализ JSON документов (decisions, facts, events)**
```sql
WITH json_analysis AS (
  SELECT 
    m.id,
    m.filename,
    m.projectId,
    -- Используем извлеченный текст или анализируем JSON структуру
    ML.GENERATE_TEXT(
      MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
      COALESCE(m.extractedText, CAST(j.data AS STRING)),
      STRUCT('Summarize key decisions and their impact' AS prompt)
    ) AS summary
  FROM core_docs_metadata m
  LEFT JOIN core_docs_json_table j 
    ON j.uri = REPLACE(m.storagePath, 'gs://', '')
  WHERE m.contentType = 'application/json'
    AND m.filename LIKE '%decisions.json'
),
```

**Пример 3: Извлечение данных из PDF и DOCX через метаданные**
```sql
-- PDF и DOCX файлы доступны через extractedText в метаданных
WITH pdf_insights AS (
  SELECT 
    m.id,
    m.filename,
    m.storagePath,
    ML.GENERATE_TEXT(
      MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
      m.extractedText,  -- Используем извлеченный текст из PDF
      STRUCT('Extract GTI score and product name from this pharmaceutical report' AS prompt)
    ) AS insights
  FROM core_docs_metadata m
  WHERE m.contentType = 'application/pdf'
    AND m.extractedText IS NOT NULL
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

# Создать Object Table для RAG документов (текстовые файлы)
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE EXTERNAL TABLE \`nv-horizon.nv_horizon_documents.documents_object_table\`
WITH CONNECTION \`nv-horizon.us.gcs-connection\`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://nv-horizon-rag-documents/**/*.txt']
)
EOF

# Создать Object Table для CoreDocs - текстовые форматы
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE EXTERNAL TABLE \`nv-horizon.nv_horizon_documents.core_docs_text_table\`
WITH CONNECTION \`nv-horizon.us.gcs-connection\`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = [
    'gs://sanskrite-core-docs/**/*.txt',
    'gs://sanskrite-core-docs/**/*.md',
    'gs://sanskrite-core-docs/**/*.csv',
    'gs://sanskrite-core-docs/**/*.tsv'
  ]
)
EOF

# Создать Object Table для CoreDocs - JSON файлы
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE EXTERNAL TABLE \`nv-horizon.nv_horizon_documents.core_docs_json_table\`
WITH CONNECTION \`nv-horizon.us.gcs-connection\`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.json'],
  format = 'JSON'
)
EOF

# Примечание: PDF и DOCX файлы доступны через метаданные (storagePath)
# Для анализа их контента используйте BigQuery ML функции с Vertex AI,
# которые могут читать бинарные форматы через API
EOF
```

### 2. BigQuery ML с Vertex AI
```bash
# Создать connection для Vertex AI
bq mk --connection --location=US --connection_type=CLOUD_RESOURCE \
  --project_id=sanskrite vertex-ai-connection

# Создать remote model
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE MODEL \`nv-horizon.nv_horizon_documents.gemini_model\`
REMOTE WITH CONNECTION \`nv-horizon.us.vertex-ai-connection\`
OPTIONS (
  remote_service_type = 'CLOUD_AI_LARGE_LANGUAGE_MODEL_V1',
  endpoint = 'us-central1-aiplatform.googleapis.com'
)
EOF
```

### 3. Firebase Extension для Firestore → BigQuery
```bash
# Установить extension для потокового экспорта
firebase ext:install firebase/firestore-bigquery-export --project=nv-horizon

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
      MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
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
    MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
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

