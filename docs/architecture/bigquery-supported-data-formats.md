# Поддерживаемые форматы данных в BigQuery

## Форматы для загрузки данных

### 1. **Текстовые форматы**

#### CSV (Comma-Separated Values)
- ✅ **Поддерживается:** Да
- **Использование:** Табличные данные, экспорты из Excel/Google Sheets
- **Особенности:**
  - Разделитель: запятая, точка с запятой, табуляция
  - Поддержка заголовков
  - Кодировка: UTF-8
- **Пример:**
```sql
LOAD DATA INTO `dataset.table`
FROM FILES (
  format = 'CSV',
  uris = ['gs://bucket/data.csv']
)
```

#### TSV (Tab-Separated Values)
- ✅ **Поддерживается:** Да
- **Использование:** Табличные данные с табуляцией
- **Особенности:** Аналогично CSV, но разделитель - табуляция

#### JSON (JavaScript Object Notation)
- ✅ **Поддерживается:** Да
- **Использование:** Структурированные данные, API ответы, логи
- **Особенности:**
  - Поддержка вложенных объектов и массивов
  - Может быть newline-delimited JSON (NDJSON)
  - Автоматическое определение схемы или указание вручную
- **Пример:**
```sql
LOAD DATA INTO `dataset.table`
FROM FILES (
  format = 'JSON',
  uris = ['gs://bucket/data.json']
)
```

#### JSONL / NDJSON (Newline-Delimited JSON)
- ✅ **Поддерживается:** Да
- **Использование:** Большие объемы JSON данных, логи, стриминг данных
- **Особенности:** Каждая строка - отдельный JSON объект

### 2. **Бинарные форматы (колоночные)**

#### Parquet
- ✅ **Поддерживается:** Да (рекомендуется)
- **Использование:** Большие объемы аналитических данных
- **Преимущества:**
  - Высокая производительность
  - Эффективное сжатие
  - Поддержка схемы данных
  - Оптимизирован для аналитических запросов
- **Пример:**
```sql
LOAD DATA INTO `dataset.table`
FROM FILES (
  format = 'PARQUET',
  uris = ['gs://bucket/data.parquet']
)
```

#### Avro
- ✅ **Поддерживается:** Да
- **Использование:** Большие объемы данных, стриминг
- **Преимущества:**
  - Эффективное сжатие
  - Поддержка схемы и эволюции схемы
  - Быстрая сериализация/десериализация
- **Пример:**
```sql
LOAD DATA INTO `dataset.table`
FROM FILES (
  format = 'AVRO',
  uris = ['gs://bucket/data.avro']
)
```

#### ORC (Optimized Row Columnar)
- ✅ **Поддерживается:** Да
- **Использование:** Большие объемы данных (особенно из Hadoop экосистемы)
- **Преимущества:**
  - Высокая производительность
  - Эффективное сжатие
  - Поддержка сложных типов данных

### 3. **Специальные форматы**

#### Google Sheets
- ✅ **Поддерживается:** Да
- **Использование:** Импорт данных из Google Sheets
- **Особенности:** Прямая интеграция с Google Workspace

#### Cloud Datastore Backup
- ✅ **Поддерживается:** Да
- **Использование:** Импорт данных из Cloud Datastore
- **Особенности:** Автоматическая миграция данных

#### Bigtable
- ✅ **Поддерживается:** Да (через External Tables)
- **Использование:** Данные из Cloud Bigtable
- **Особенности:** Прямое чтение без загрузки

## Форматы для External Tables (чтение без загрузки)

### Object Tables (GCS)
- ✅ **Поддерживается:** Да
- **Форматы:** Любые файлы в GCS
- **Использование:** Прямое чтение файлов из Cloud Storage
- **Особенности:**
  - Не требует загрузки данных в BigQuery
  - Чтение "на лету"
  - Поддержка метаданных файлов

### Federated Queries (внешние источники)

#### Cloud SQL
- ✅ **Поддерживается:** Да
- **Использование:** Чтение данных из Cloud SQL без копирования
- **Форматы:** Любые таблицы в Cloud SQL

#### Cloud Spanner
- ✅ **Поддерживается:** Да
- **Использование:** Чтение данных из Spanner
- **Форматы:** Любые таблицы в Spanner

#### Google Drive
- ✅ **Поддерживается:** Да
- **Использование:** Чтение файлов из Google Drive
- **Форматы:** CSV, JSON, Google Sheets

#### AWS S3
- ✅ **Поддерживается:** Да (через External Tables)
- **Использование:** Чтение данных из S3
- **Форматы:** CSV, JSON, Parquet, Avro, ORC

#### Azure Blob Storage
- ✅ **Поддерживается:** Да (через External Tables)
- **Использование:** Чтение данных из Azure
- **Форматы:** CSV, JSON, Parquet, Avro, ORC

## Форматы для Streaming (real-time)

### Streaming API
- ✅ **Поддерживается:** Да
- **Форматы:** JSON, Protobuf
- **Использование:** Real-time загрузка данных
- **Особенности:** Данные доступны сразу после вставки

## Форматы для экспорта данных

### Экспорт из BigQuery поддерживает:
- ✅ CSV
- ✅ JSON
- ✅ Avro
- ✅ Parquet

## Что мы используем в нашей системе

### Текущие форматы в GCS:

#### 1. **Текстовые документы**
- `.txt` - извлеченный текст из документов
- `.md` - Markdown файлы
- `.csv` - табличные данные
- `.tsv` - табличные данные с табуляцией

#### 2. **JSON файлы**
- `decisions.json` - решения проекта
- `facts.json` - факты
- `events.json` - события
- Любые другие JSON файлы

#### 3. **Бинарные документы**
- `.pdf` - PDF документы (текст извлекается в `extractedText`)
- `.docx` - Word документы (текст извлекается в `extractedText`)

### BigQuery может читать все эти форматы:

```sql
-- Текстовые файлы через Object Tables
SELECT * FROM `h1horizon.h1horizon_documents.documents_object_table`
WHERE uri LIKE '%.txt'

-- JSON файлы
SELECT * FROM `h1horizon.h1horizon_documents.core_docs_json_table`
WHERE uri LIKE '%.json'

-- CSV файлы (можно создать отдельную Object Table)
SELECT * FROM `h1horizon.h1horizon_documents.core_docs_text_table`
WHERE uri LIKE '%.csv'
```

## Рекомендации по выбору формата

### Для аналитических данных:
1. **Parquet** - лучший выбор для больших объемов
2. **Avro** - если нужна эволюция схемы
3. **CSV** - для небольших объемов или экспортов

### Для документов и текста:
1. **JSON** - для структурированных данных (decisions, facts)
2. **TXT** - для извлеченного текста
3. **Object Tables** - для прямого чтения из GCS

### Для real-time данных:
1. **Streaming API** - для real-time загрузки
2. **JSON** - формат для streaming

## Ограничения

### Размер файлов:
- Максимальный размер файла: 5 TB
- Рекомендуется разбивать большие файлы

### Количество файлов:
- Можно загружать миллионы файлов
- Рекомендуется использовать wildcards в URI

### Кодировки:
- Поддерживается UTF-8
- Другие кодировки могут требовать конвертации

## Примеры использования разных форматов

### Загрузка CSV:
```sql
LOAD DATA INTO `h1horizon.h1horizon_documents.sales_data`
FROM FILES (
  format = 'CSV',
  uris = ['gs://bucket/sales/*.csv'],
  skip_leading_rows = 1
)
```

### Загрузка JSON:
```sql
LOAD DATA INTO `h1horizon.h1horizon_documents.events`
FROM FILES (
  format = 'JSON',
  uris = ['gs://bucket/events/*.json']
)
```

### Загрузка Parquet:
```sql
LOAD DATA INTO `h1horizon.h1horizon_documents.analytics`
FROM FILES (
  format = 'PARQUET',
  uris = ['gs://bucket/analytics/*.parquet']
)
```

### Чтение через External Table:
```sql
CREATE OR REPLACE EXTERNAL TABLE `h1horizon.h1horizon_documents.external_data`
OPTIONS (
  format = 'CSV',
  uris = ['gs://bucket/data/*.csv']
)
```

## Итог

BigQuery поддерживает **широкий спектр форматов данных**:

### ✅ Основные форматы:
- CSV, TSV
- JSON, JSONL
- Parquet (рекомендуется)
- Avro
- ORC

### ✅ Специальные источники:
- Google Sheets
- Cloud SQL
- Cloud Spanner
- Google Drive
- AWS S3
- Azure Blob Storage

### ✅ Наша система использует:
- Текстовые: `.txt`, `.md`, `.csv`, `.tsv`
- JSON: `.json` файлы
- Бинарные: `.pdf`, `.docx` (текст извлекается)

**Все эти форматы доступны для анализа через BigQuery!**

