-- BigQuery + Vertex AI Integration Examples
-- Практические примеры SQL-запросов для работы с документами

-- ============================================
-- 1. Базовые запросы к метаданным документов
-- ============================================

-- Количество документов по типам
SELECT 
  documentType,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as unique_tenants,
  COUNT(DISTINCT sourceId) as unique_sources
FROM `sanskrite.sanskrite_documents.documents_metadata`
GROUP BY documentType
ORDER BY count DESC;

-- Документы по датам создания
SELECT 
  DATE(createdAt) as date,
  COUNT(*) as documents_created,
  COUNT(DISTINCT tenantId) as active_tenants
FROM `sanskrite.sanskrite_documents.documents_metadata`
WHERE createdAt >= DATE_SUB(CURRENT_DATE(), INTERVAL 30 DAY)
GROUP BY date
ORDER BY date DESC;

-- ============================================
-- 2. JOIN с другими данными (когда будут доступны)
-- ============================================

-- Пример: Документы по проектам (когда таблица projects будет в BigQuery)
/*
SELECT 
  p.name as project_name,
  COUNT(d.id) as documents_count,
  COUNT(DISTINCT d.sourceId) as sources_count,
  ARRAY_AGG(DISTINCT d.documentType) as document_types
FROM `sanskrite.projects.projects` p
JOIN `sanskrite.sanskrite_documents.documents_metadata` d 
  ON d.projectId = p.id
GROUP BY p.name
ORDER BY documents_count DESC;
*/

-- ============================================
-- 3. Анализ с использованием Vertex AI (когда модель настроена)
-- ============================================

-- Пример: Извлечение структурированных данных из документов
-- Требует настройки BigQuery ML remote model
/*
WITH document_analysis AS (
  SELECT 
    m.id,
    m.title,
    m.storagePath,
    -- Вызов Vertex AI для анализа контента
    ML.GENERATE_TEXT(
      MODEL `sanskrite.sanskrite_documents.gemini_model`,
      (
        SELECT content 
        FROM `sanskrite.sanskrite_documents.documents_object_table` 
        WHERE uri = m.storagePath
      ),
      STRUCT('Extract: key entities, sentiment score (0-1), main topics' AS prompt)
    ) AS analysis_result
  FROM `sanskrite.sanskrite_documents.documents_metadata` m
  WHERE m.status = 'processed'
  LIMIT 10
)
SELECT 
  id,
  title,
  JSON_EXTRACT_SCALAR(analysis_result, '$.sentiment_score') as sentiment,
  JSON_EXTRACT_ARRAY(analysis_result, '$.topics') as topics
FROM document_analysis;
*/

-- ============================================
-- 4. Комбинированный анализ: Vertex AI Search + BigQuery
-- ============================================

-- Шаг 1: Используем Vertex AI Search API для поиска релевантных документов
-- Шаг 2: Анализируем найденные документы в BigQuery

-- Пример: Анализ найденных документов (после поиска через API)
/*
WITH relevant_documents AS (
  -- Это результаты из Vertex AI Search API
  SELECT document_id FROM UNNEST(['doc1', 'doc2', 'doc3']) AS document_id
)
SELECT 
  m.id,
  m.title,
  m.documentType,
  m.language,
  m.createdAt,
  m.storagePath
FROM `sanskrite.sanskrite_documents.documents_metadata` m
JOIN relevant_documents r ON r.document_id = m.id
ORDER BY m.createdAt DESC;
*/

-- ============================================
-- 5. Практические use cases
-- ============================================

-- Use case: Анализ документов по источникам
SELECT 
  sourceId,
  COUNT(*) as total_documents,
  COUNT(DISTINCT documentType) as document_types,
  MIN(createdAt) as first_document,
  MAX(createdAt) as last_document,
  AVG(LENGTH(storagePath)) as avg_path_length
FROM `sanskrite.sanskrite_documents.documents_metadata`
GROUP BY sourceId
ORDER BY total_documents DESC;

-- Use case: Тренды по языкам
SELECT 
  language,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as tenants,
  DATE_TRUNC(createdAt, MONTH) as month
FROM `sanskrite.sanskrite_documents.documents_metadata`
WHERE language != ''
GROUP BY language, month
ORDER BY month DESC, count DESC;

-- Use case: Статусы документов
SELECT 
  status,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as tenants,
  AVG(TIMESTAMP_DIFF(updatedAt, createdAt, MINUTE)) as avg_processing_minutes
FROM `sanskrite.sanskrite_documents.documents_metadata`
GROUP BY status
ORDER BY count DESC;

