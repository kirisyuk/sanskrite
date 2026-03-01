# Масштабируемость BigQuery для новых форматов данных

## ✅ Да, источник данных останется тот же (GCS)

### Текущая архитектура:

```
GCS Buckets (источник данных):
├── {project}-rag-documents/
│   └── {tenantId}/{sourceId}/{docId}.txt
└── {project}-core-docs/
    └── {tenantId}/{projectId}/{filename}
        ├── *.txt, *.md, *.csv, *.tsv
        ├── *.json
        ├── *.pdf
        └── *.docx
```

### BigQuery Object Tables (читают из GCS):

```
BigQuery Dataset: etigram_documents
├── documents_object_table (GCS: rag-documents/*.txt)
├── core_docs_object_table (GCS: core-docs/*)
└── core_docs_json_table (GCS: core-docs/*.json)
```

## Как добавить поддержку новых форматов

### Сценарий: Добавляем поддержку Parquet, Avro, Excel

#### 1. **Данные остаются в тех же GCS buckets** ✅

Не нужно менять место хранения! Просто загружаем новые форматы в существующие buckets:

```
{project}-core-docs/
├── {tenantId}/{projectId}/
│   ├── data.parquet      ← новый формат
│   ├── data.avro        ← новый формат
│   ├── data.xlsx        ← новый формат
│   └── ... (существующие форматы)
```

#### 2. **Создаем новые Object Tables в BigQuery**

Просто добавляем новые таблицы для новых форматов:

```sql
-- Для Parquet файлов
CREATE OR REPLACE EXTERNAL TABLE `etigram.etigram_documents.core_docs_parquet_table`
WITH CONNECTION `etigram.us.gcs-connection`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.parquet'],
  format = 'PARQUET'
)

-- Для Avro файлов
CREATE OR REPLACE EXTERNAL TABLE `etigram.etigram_documents.core_docs_avro_table`
WITH CONNECTION `etigram.us.gcs-connection`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.avro'],
  format = 'AVRO'
)

-- Для Excel файлов (конвертируем в CSV или используем как бинарные)
CREATE OR REPLACE EXTERNAL TABLE `etigram.etigram_documents.core_docs_excel_table`
WITH CONNECTION `etigram.us.gcs-connection`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.xlsx', 'gs://sanskrite-core-docs/**/*.xls']
)
```

#### 3. **Обновляем промпт для AI**

Добавляем информацию о новых таблицах в промпт:

```go
AVAILABLE TABLES AND FORMATS:
- documents_metadata: RAG documents metadata
- core_docs_metadata: CoreDocs metadata
- documents_object_table: Object table for RAG documents (text)
- core_docs_text_table: Object table for CoreDocs text formats
- core_docs_json_table: Object table for CoreDocs JSON files
- core_docs_parquet_table: Object table for CoreDocs Parquet files  ← новый
- core_docs_avro_table: Object table for CoreDocs Avro files       ← новый
```

#### 4. **Обновляем схему в getSchemaInfo()**

Система автоматически обнаружит новые таблицы:

```go
tables, err := s.bqClient.ListTables(ctx)
// Автоматически найдет новые таблицы: core_docs_parquet_table, core_docs_avro_table
```

## Преимущества текущей архитектуры

### ✅ Гибкость

**Не нужно менять:**
- Место хранения (GCS buckets остаются теми же)
- Структуру папок в GCS
- Код загрузки файлов
- Метаданные в Firestore

**Нужно только:**
- Создать новые Object Tables в BigQuery
- Обновить промпт для AI (опционально, для лучшей генерации SQL)

### ✅ Масштабируемость

Можно добавлять неограниченное количество форматов:

```
BigQuery Object Tables:
├── core_docs_text_table      (существующая)
├── core_docs_json_table      (существующая)
├── core_docs_parquet_table   (новая)
├── core_docs_avro_table      (новая)
├── core_docs_excel_table     (новая)
├── core_docs_xml_table       (новая)
└── ... (любые другие форматы)
```

### ✅ Обратная совместимость

Старые данные и запросы продолжают работать:
- Существующие Object Tables остаются
- Старые SQL запросы работают
- Новые форматы добавляются параллельно

## Пример: Добавление поддержки Parquet

### Шаг 1: Загружаем Parquet файлы в GCS

```go
// Код загрузки остается тем же
objectPath := fmt.Sprintf("%s/%s/data.parquet", tenantID, projectID)
storagePath := fmt.Sprintf("gs://%s/%s", bucket, objectPath)

// Загружаем в тот же bucket
gcsClient.UploadBytes(ctx, bucket, objectPath, parquetData, "application/parquet", metadata)
```

### Шаг 2: Создаем Object Table

```bash
bq query --use_legacy_sql=false <<EOF
CREATE OR REPLACE EXTERNAL TABLE `etigram.etigram_documents.core_docs_parquet_table`
WITH CONNECTION `etigram.us.gcs-connection`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.parquet'],
  format = 'PARQUET'
)
EOF
```

### Шаг 3: Используем в запросах

```sql
-- AI автоматически сгенерирует правильный SQL
SELECT * FROM `etigram.etigram_documents.core_docs_parquet_table`
WHERE uri LIKE '%tenant123%'
```

## Автоматическое обнаружение новых форматов

### Система автоматически:

1. **Обнаруживает новые таблицы** через `ListTables()`
2. **Получает схемы** через `GetTableSchema()`
3. **Включает в промпт** для AI
4. **Генерирует правильный SQL** для новых форматов

### Не нужно менять код:

```go
// Этот код автоматически найдет все таблицы, включая новые
tables, err := s.bqClient.ListTables(ctx)
for _, tableName := range tables {
    schema, err := s.bqClient.GetTableSchema(ctx, tableName)
    // Автоматически включит новые таблицы в схему
}
```

## Рекомендации для расширения

### 1. **Используйте тот же bucket структуру**

```
{project}-core-docs/
└── {tenantId}/{projectId}/
    ├── existing-formats/
    └── new-formats/  ← можно добавить подпапки для организации
```

### 2. **Создавайте Object Tables по форматам**

Один Object Table на формат:
- `core_docs_parquet_table` для Parquet
- `core_docs_avro_table` для Avro
- `core_docs_excel_table` для Excel

### 3. **Обновляйте промпт постепенно**

Можно добавлять информацию о новых форматах в промпт для лучшей генерации SQL, но это не обязательно - система найдет таблицы автоматически.

## Итог

### ✅ Ответ: Да, источник данных останется тот же

**GCS buckets остаются:**
- `{project}-rag-documents` - для RAG документов
- `{project}-core-docs` - для CoreDocs (все форматы)

**Что нужно для новых форматов:**
1. Загружать файлы в те же buckets (можно в подпапки)
2. Создать новые Object Tables в BigQuery
3. (Опционально) Обновить промпт для AI

**Преимущества:**
- ✅ Не нужно менять место хранения
- ✅ Не нужно менять код загрузки
- ✅ Автоматическое обнаружение новых таблиц
- ✅ Обратная совместимость
- ✅ Неограниченное масштабирование

**Архитектура полностью готова для расширения!**

