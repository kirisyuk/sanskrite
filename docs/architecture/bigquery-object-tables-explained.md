# Что такое Object Tables в BigQuery

## Объяснение простыми словами

### Обычные таблицы BigQuery:
```
BigQuery Table (обычная)
├── Данные хранятся ВНУТРИ BigQuery
├── Нужно загружать данные (LOAD DATA)
├── Данные копируются в BigQuery
└── Быстрые запросы, но занимают место в BigQuery
```

### Object Tables (внешние таблицы):
```
Object Table (внешняя)
├── Данные хранятся В GCS (Google Cloud Storage)
├── НЕ нужно загружать данные
├── BigQuery читает файлы "на лету" из GCS
└── Медленнее, но не занимают место в BigQuery
```

## Что такое Object Tables?

**Object Tables** - это специальный тип таблиц в BigQuery, которые читают файлы напрямую из Google Cloud Storage (GCS) без необходимости загружать данные в BigQuery.

### Аналогия:
- **Обычная таблица** = книга в библиотеке (данные внутри BigQuery)
- **Object Table** = ссылка на книгу в другой библиотеке (данные в GCS, BigQuery читает по ссылке)

## Как это работает

### 1. Файлы в GCS:
```
gs://sanskrite-core-docs/
├── tenant1/project1/document1.txt
├── tenant1/project1/document2.json
└── tenant2/project1/data.csv
```

### 2. Создаем Object Table:
```sql
CREATE EXTERNAL TABLE `etigram.etigram_documents.core_docs_text_table`
WITH CONNECTION `etigram.us.gcs-connection`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.txt']
)
```

### 3. Используем как обычную таблицу:
```sql
SELECT * FROM `etigram.etigram_documents.core_docs_text_table`
WHERE uri LIKE '%tenant1%'
```

BigQuery автоматически читает файлы из GCS при выполнении запроса!

## Что означает "новые Object Tables"?

Когда мы говорим "новые Object Tables", мы имеем в виду:

### Текущие Object Tables (уже созданы):
```sql
-- Для текстовых файлов
documents_object_table          → gs://etigram-rag-documents/**/*.txt
core_docs_text_table           → gs://sanskrite-core-docs/**/*.{txt,md,csv,tsv}
core_docs_json_table           → gs://sanskrite-core-docs/**/*.json
```

### Новые Object Tables (если добавим поддержку новых форматов):
```sql
-- Для Parquet файлов (если добавим поддержку)
core_docs_parquet_table        → gs://sanskrite-core-docs/**/*.parquet

-- Для Avro файлов (если добавим поддержку)
core_docs_avro_table           → gs://sanskrite-core-docs/**/*.avro

-- Для Excel файлов (если добавим поддержку)
core_docs_excel_table          → gs://sanskrite-core-docs/**/*.{xlsx,xls}
```

## Структура Object Table

### Что возвращает Object Table:

```sql
SELECT * FROM `etigram.etigram_documents.documents_object_table`
```

Результат:
```
uri                                    | size | content_type | updated
---------------------------------------|------|--------------|----------
gs://bucket/tenant1/doc1.txt          | 1024 | text/plain   | 2024-01-01
gs://bucket/tenant1/doc2.txt          | 2048 | text/plain   | 2024-01-02
```

### Поля Object Table:
- `uri` - путь к файлу в GCS
- `size` - размер файла
- `content_type` - тип контента
- `updated` - дата обновления
- `metadata` - дополнительные метаданные файла

## Преимущества Object Tables

### ✅ Не нужно загружать данные
- Файлы остаются в GCS
- Не занимают место в BigQuery
- Не нужно платить за хранение в BigQuery

### ✅ Автоматическое обновление
- Новые файлы в GCS автоматически доступны
- Не нужно обновлять таблицу

### ✅ Гибкость
- Можем создавать разные Object Tables для разных форматов
- Можем фильтровать по URI

## Недостатки Object Tables

### ⚠️ Медленнее обычных таблиц
- Чтение из GCS медленнее, чем из BigQuery
- Для больших объемов лучше загрузить в BigQuery

### ⚠️ Ограничения
- Нельзя использовать некоторые функции BigQuery
- JOIN между Object Tables медленные

## Примеры использования

### Пример 1: Чтение текстовых файлов
```sql
-- Object Table для текстовых файлов
SELECT 
  uri,
  size,
  content_type
FROM `etigram.etigram_documents.documents_object_table`
WHERE uri LIKE '%tenant123%'
LIMIT 10
```

### Пример 2: Чтение JSON файлов
```sql
-- Object Table для JSON файлов
SELECT 
  uri,
  JSON_EXTRACT_SCALAR(data, '$.decisions') as decisions
FROM `etigram.etigram_documents.core_docs_json_table`
WHERE uri LIKE '%.json'
```

### Пример 3: JOIN с метаданными
```sql
-- Объединяем Object Table с метаданными
SELECT 
  m.id,
  m.filename,
  m.createdAt,
  o.size,
  o.content_type
FROM `etigram.etigram_documents.core_docs_metadata` m
JOIN `etigram.etigram_documents.core_docs_object_table` o
  ON o.uri = REPLACE(m.storagePath, 'gs://', '')
WHERE m.projectId = 'project-x'
```

## Когда создавать новые Object Tables?

### Создавайте новую Object Table когда:
1. **Добавляете новый формат файлов**
   - Например: Parquet, Avro, Excel
   - Каждый формат = своя Object Table

2. **Нужна отдельная таблица для определенных файлов**
   - Например: только JSON файлы решений
   - Или только CSV файлы с данными

3. **Оптимизация запросов**
   - Разделение по форматам ускоряет запросы
   - Можно фильтровать по URI эффективнее

## Как создать новую Object Table

### Шаг 1: Файлы уже в GCS
```
gs://sanskrite-core-docs/tenant1/project1/data.parquet
```

### Шаг 2: Создаем Object Table
```sql
CREATE OR REPLACE EXTERNAL TABLE `etigram.etigram_documents.core_docs_parquet_table`
WITH CONNECTION `etigram.us.gcs-connection`
OPTIONS (
  object_metadata = 'SIMPLE',
  uris = ['gs://sanskrite-core-docs/**/*.parquet'],
  format = 'PARQUET'
)
```

### Шаг 3: Используем в запросах
```sql
SELECT * FROM `etigram.etigram_documents.core_docs_parquet_table`
```

## Итог

**Object Tables** = способ читать файлы из GCS как обычные таблицы BigQuery, без загрузки данных в BigQuery.

**"Новые Object Tables"** = дополнительные таблицы для новых форматов файлов (Parquet, Avro, Excel и т.д.), которые мы можем создать в будущем.

**Преимущества:**
- ✅ Не нужно загружать данные
- ✅ Автоматическое обновление
- ✅ Гибкость для разных форматов

**Недостатки:**
- ⚠️ Медленнее обычных таблиц
- ⚠️ Ограничения на некоторые функции

