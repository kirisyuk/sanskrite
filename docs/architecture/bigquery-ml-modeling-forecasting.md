# Матмоделирование и прогнозирование в BigQuery

## Возможности BigQuery для матмоделирования

### 1. **BigQuery ML** - встроенное машинное обучение

BigQuery позволяет создавать и обучать модели машинного обучения **прямо в SQL**, без необходимости экспорта данных или использования внешних инструментов.

### 2. **Vertex AI интеграция** - продвинутые модели

Через Remote Models можно использовать модели Vertex AI (Gemini, AutoML и т.д.) прямо в SQL запросах.

## Типы моделей в BigQuery ML

### 1. **Линейная регрессия** (Linear Regression)
- Прогнозирование числовых значений
- Пример: прогноз продаж, запасов, спроса

### 2. **Логистическая регрессия** (Logistic Regression)
- Классификация (да/нет, категории)
- Пример: вероятность дефицита запасов

### 3. **K-means кластеризация** (K-means Clustering)
- Группировка похожих объектов
- Пример: сегментация объектов по характеристикам

### 4. **Матричная факторизация** (Matrix Factorization)
- Рекомендательные системы
- Пример: рекомендации оптимальных запасов

### 5. **ARIMA** (AutoRegressive Integrated Moving Average)
- Прогнозирование временных рядов
- Пример: прогноз запасов на полгода вперед

### 6. **ARIMA_PLUS** (улучшенный ARIMA)
- Автоматический выбор параметров
- Обработка сезонности
- Пример: прогноз с учетом сезонных колебаний

### 7. **XGBoost** (Gradient Boosting)
- Продвинутые модели для сложных задач
- Пример: комплексное прогнозирование с множеством факторов

## Пример: Прогнозирование уровня запасов

### Задача: "Спрогнозировать уровень запасов на объекте N на ближайшие полгода"

### Шаг 1: Подготовка данных

Предположим, у нас есть исторические данные о запасах:

```sql
-- Исторические данные о запасах (из нашей системы или внешних источников)
CREATE OR REPLACE TABLE `sanskrite.sanskrite_documents.inventory_history` AS
SELECT 
  objectId,
  date,
  inventory_level,
  sales_volume,
  orders_placed,
  seasonality_factor
FROM `sanskrite.sanskrite_documents.inventory_data`
WHERE objectId = 'object-N'
ORDER BY date
```

### Шаг 2: Создание модели прогнозирования

#### Вариант A: ARIMA_PLUS (для временных рядов)

```sql
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.inventory_forecast_model`
OPTIONS(
  model_type='ARIMA_PLUS',
  time_series_timestamp_col='date',
  time_series_data_col='inventory_level',
  time_series_id_col='objectId',
  horizon=180,  -- прогноз на 180 дней (полгода)
  auto_arima=TRUE,
  data_frequency='AUTO_FREQUENCY',
  decompose_time_series=TRUE
) AS
SELECT 
  objectId,
  date,
  inventory_level
FROM `sanskrite.sanskrite_documents.inventory_history`
WHERE objectId = 'object-N'
```

#### Вариант B: Linear Regression (с дополнительными факторами)

```sql
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.inventory_regression_model`
OPTIONS(
  model_type='LINEAR_REG',
  input_label_cols=['inventory_level']
) AS
SELECT 
  inventory_level,
  sales_volume,
  orders_placed,
  seasonality_factor,
  EXTRACT(DAYOFWEEK FROM date) as day_of_week,
  EXTRACT(MONTH FROM date) as month
FROM `sanskrite.sanskrite_documents.inventory_history`
WHERE objectId = 'object-N'
```

### Шаг 3: Прогнозирование

#### Прогноз с ARIMA_PLUS:

```sql
SELECT 
  forecast_timestamp,
  forecast_value,
  prediction_interval_lower_bound,
  prediction_interval_upper_bound,
  confidence_level
FROM ML.FORECAST(
  MODEL `sanskrite.sanskrite_documents.inventory_forecast_model`,
  STRUCT(180 AS horizon, 0.95 AS confidence_level)
)
WHERE time_series_id = 'object-N'
ORDER BY forecast_timestamp
```

#### Прогноз с Linear Regression:

```sql
SELECT 
  predicted_inventory_level,
  prediction_interval_lower_bound,
  prediction_interval_upper_bound
FROM ML.PREDICT(
  MODEL `sanskrite.sanskrite_documents.inventory_regression_model`,
  (
    SELECT 
      sales_volume,
      orders_placed,
      seasonality_factor,
      EXTRACT(DAYOFWEEK FROM date) as day_of_week,
      EXTRACT(MONTH FROM date) as month
    FROM UNNEST(GENERATE_DATE_ARRAY(CURRENT_DATE(), DATE_ADD(CURRENT_DATE(), INTERVAL 180 DAY))) AS date
    CROSS JOIN (SELECT 1000 as sales_volume, 50 as orders_placed, 1.2 as seasonality_factor)
  )
)
```

### Шаг 4: Оценка качества модели

```sql
SELECT 
  mean_absolute_error,
  mean_squared_error,
  mean_squared_log_error,
  mean_absolute_percentage_error,
  r2_score
FROM ML.EVALUATE(
  MODEL `sanskrite.sanskrite_documents.inventory_forecast_model`,
  (
    SELECT * FROM `sanskrite.sanskrite_documents.inventory_history`
    WHERE objectId = 'object-N'
    AND date >= DATE_SUB(CURRENT_DATE(), INTERVAL 30 DAY)
  )
)
```

## Интеграция с нашей системой

### Как это работает с запросами на естественном языке:

**Пользователь:** "Спрогнозируй уровень запасов на объекте N на ближайшие полгода"

**AI генерирует SQL:**

```sql
-- 1. Проверяет, есть ли модель
SELECT model_name 
FROM `sanskrite.sanskrite_documents.INFORMATION_SCHEMA.MODELS`
WHERE model_name = 'inventory_forecast_model'

-- 2. Если модели нет, создает её
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.inventory_forecast_model`
OPTIONS(
  model_type='ARIMA_PLUS',
  time_series_timestamp_col='date',
  time_series_data_col='inventory_level',
  time_series_id_col='objectId',
  horizon=180
) AS
SELECT objectId, date, inventory_level
FROM `sanskrite.sanskrite_documents.inventory_history`
WHERE objectId = 'object-N'

-- 3. Делает прогноз
SELECT 
  forecast_timestamp,
  forecast_value,
  prediction_interval_lower_bound,
  prediction_interval_upper_bound
FROM ML.FORECAST(
  MODEL `sanskrite.sanskrite_documents.inventory_forecast_model`,
  STRUCT(180 AS horizon, 0.95 AS confidence_level)
)
WHERE time_series_id = 'object-N'
ORDER BY forecast_timestamp
```

## Расширенные возможности

### 1. **Vertex AI Remote Models** (для сложных моделей)

Если нужны более сложные модели (нейросети, трансформеры):

```sql
-- Создание Remote Model для Vertex AI
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.advanced_forecast_model`
REMOTE WITH CONNECTION `sanskrite.us.vertex-ai-connection`
OPTIONS (
  remote_service_type = 'CLOUD_AI_LARGE_LANGUAGE_MODEL_V1'
)

-- Использование для анализа и прогнозирования
SELECT 
  ML.GENERATE_TEXT(
    MODEL `sanskrite.sanskrite_documents.advanced_forecast_model`,
    CONCAT('Analyze inventory trends and predict:', historical_data),
    STRUCT('Generate forecast for next 6 months' AS prompt)
  ) AS forecast_analysis
FROM inventory_data
```

### 2. **Комбинирование моделей**

Можно комбинировать несколько моделей:

```sql
WITH arima_forecast AS (
  SELECT * FROM ML.FORECAST(MODEL `inventory_arima_model`, STRUCT(180 AS horizon))
),
regression_forecast AS (
  SELECT * FROM ML.PREDICT(MODEL `inventory_regression_model`, future_features)
),
ensemble AS (
  SELECT 
    arima.forecast_value * 0.6 + regression.predicted_value * 0.4 AS ensemble_forecast
  FROM arima_forecast arima
  JOIN regression_forecast regression ON arima.forecast_timestamp = regression.date
)
SELECT * FROM ensemble
```

### 3. **Автоматическое обновление моделей**

Можно настроить автоматическое переобучение:

```sql
-- Переобучение модели на новых данных
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.inventory_forecast_model`
OPTIONS(
  model_type='ARIMA_PLUS',
  ...
) AS
SELECT * FROM `sanskrite.sanskrite_documents.inventory_history`
WHERE date >= DATE_SUB(CURRENT_DATE(), INTERVAL 1 YEAR)  -- последний год данных
```

## Примеры использования в нашей системе

### Пример 1: Прогнозирование документов

**Запрос:** "Спрогнозируй количество документов, которые будут созданы в проекте X в следующем месяце"

```sql
-- AI генерирует:
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.documents_forecast_model`
OPTIONS(
  model_type='ARIMA_PLUS',
  time_series_timestamp_col='date',
  time_series_data_col='doc_count',
  horizon=30
) AS
SELECT 
  DATE(createdAt) as date,
  COUNT(*) as doc_count
FROM `sanskrite.sanskrite_documents.core_docs_metadata`
WHERE projectId = 'project-X'
GROUP BY date
ORDER BY date

-- Прогноз
SELECT * FROM ML.FORECAST(
  MODEL `sanskrite.sanskrite_documents.documents_forecast_model`,
  STRUCT(30 AS horizon)
)
```

### Пример 2: Прогнозирование активности

**Запрос:** "Предскажи активность пользователей на следующую неделю"

```sql
-- AI генерирует модель на основе исторических данных активности
CREATE OR REPLACE MODEL `sanskrite.sanskrite_documents.activity_forecast_model`
OPTIONS(model_type='ARIMA_PLUS', ...) AS
SELECT date, activity_count FROM activity_history

-- Прогноз
SELECT * FROM ML.FORECAST(MODEL `activity_forecast_model`, STRUCT(7 AS horizon))
```

## Ограничения и рекомендации

### Ограничения BigQuery ML:

1. **Размер данных:** Модели обучаются на данных в BigQuery
2. **Типы моделей:** Ограниченный набор встроенных моделей
3. **Время обучения:** Может занимать время для больших объемов

### Рекомендации:

1. **Для простых прогнозов:** Используйте ARIMA_PLUS
2. **Для сложных моделей:** Используйте Vertex AI Remote Models
3. **Для реального времени:** Используйте предобученные модели
4. **Для экспериментов:** Используйте BigQuery ML для быстрого прототипирования

## Итог

### BigQuery поддерживает:

✅ **Встроенное ML** - создание и обучение моделей прямо в SQL
✅ **Прогнозирование** - ARIMA, ARIMA_PLUS для временных рядов
✅ **Классификация** - Logistic Regression, XGBoost
✅ **Кластеризация** - K-means
✅ **Vertex AI интеграция** - использование продвинутых моделей

### Для вашего примера:

**"Спрогнозировать уровень запасов на объекте N на ближайшие полгода"**

1. ✅ Создать модель ARIMA_PLUS на исторических данных
2. ✅ Использовать ML.FORECAST для прогноза на 180 дней
3. ✅ Получить прогноз с доверительными интервалами
4. ✅ Визуализировать результаты

**Все это можно сделать через запрос на естественном языке!**

