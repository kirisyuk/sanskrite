# BigQuery + Vertex AI Integration - Practical Setup

## Текущий статус

✅ **Vertex AI connection создан** - можно использовать для BigQuery ML
⚠️ **Object Tables** - требуют специального синтаксиса или альтернативного подхода
⚠️ **ML Model** - требует правильного remote_service_type

## Альтернативный подход для работы с документами

Поскольку Object Tables для текстовых файлов имеют ограничения, используем комбинированный подход:

### 1. External Table для метаданных документов
```sql
-- Уже создана: documents_metadata
-- Будет заполняться через Firebase Extension
```

### 2. Функция для чтения контента из GCS через Vertex AI
Вместо Object Tables, используем BigQuery ML функции для работы с контентом:

```sql
-- Создаем функцию для анализа документов
CREATE OR REPLACE FUNCTION `sanskrite.sanskrite_documents.analyze_document`(
  gcs_uri STRING,
  prompt STRING
)
RETURNS STRING
LANGUAGE js AS """
  // Используем BigQuery ML для вызова Vertex AI
  // Читаем файл из GCS и отправляем в модель
  return ml_generate_text_result;
""";
```

### 3. Практический пример использования

```sql
-- Извлечение структурированных данных из документов
WITH document_content AS (
  -- Для каждого документа читаем контент из GCS
  SELECT 
    m.id,
    m.storagePath,
    -- Здесь будет вызов Vertex AI для чтения и анализа
    -- Пока используем storagePath как ссылку
    m.storagePath as content_uri
  FROM `sanskrite.sanskrite_documents.documents_metadata` m
  WHERE m.status = 'processed'
)
SELECT 
  id,
  content_uri,
  -- Когда ML модель настроена:
  -- ML.GENERATE_TEXT(MODEL gemini_model, content, prompt) as insights
  'Analysis pending' as insights
FROM document_content
LIMIT 10
```

## Рекомендуемый workflow

### Вариант 1: Через Vertex AI Search API + BigQuery
1. Используем Vertex AI Search для семантического поиска
2. Получаем список релевантных документов
3. В BigQuery анализируем метаданные этих документов
4. JOIN с другими структурированными данными

### Вариант 2: Прямой анализ через BigQuery ML (когда модель настроена)
1. Метаданные документов в BigQuery (через Firebase Extension)
2. Контент документов в GCS
3. BigQuery ML функции читают контент и анализируют
4. Результаты структурируются и JOIN с другими данными

## Следующие шаги

1. ✅ Vertex AI connection создан
2. ⏳ Настроить Firebase Extension для Firestore → BigQuery
3. ⏳ Создать примеры SQL-запросов с использованием ML функций
4. ⏳ Настроить автоматическую обработку документов

## Firebase Extension установка

```bash
# Интерактивная установка
firebase ext:install firebase/firestore-bigquery-export --project=sanskrite

# Параметры:
# - Collection: rag_documents
# - Dataset: sanskrite_documents  
# - Table: documents_metadata
# - Location: US
```

