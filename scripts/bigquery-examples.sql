-- BigQuery + Vertex AI Integration Examples
-- Практические примеры SQL-запросов для работы с документами

-- ============================================
-- 1. Базовые запросы к метаданным документов
-- ============================================

-- Работа с RAG документами (текстовые файлы)
SELECT 
  uri,
  content_type,
  size,
  last_modified
FROM `nv-horizon.nv_horizon_documents.documents_object_table`
WHERE uri LIKE '%tenant123%'
LIMIT 10;

-- Работа с CoreDocs - текстовые форматы
SELECT 
  uri,
  content_type,
  size,
  last_modified
FROM `nv-horizon.nv_horizon_documents.core_docs_text_table`
WHERE uri LIKE '%.md' OR uri LIKE '%.txt'
LIMIT 10;

-- Работа с CoreDocs - JSON файлы
SELECT 
  uri,
  JSON_EXTRACT_SCALAR(data, '$.decisions') as decisions_data
FROM `nv-horizon.nv_horizon_documents.core_docs_json_table`
WHERE uri LIKE '%decisions.json'
LIMIT 10;

-- Анализ метаданных CoreDocs по типам контента
SELECT 
  contentType,
  COUNT(*) as count,
  SUM(sizeBytes) as total_size_bytes,
  COUNT(DISTINCT tenantId) as unique_tenants,
  COUNT(DISTINCT projectId) as unique_projects
FROM `nv-horizon.nv_horizon_documents.core_docs_metadata`
GROUP BY contentType
ORDER BY count DESC;

-- Количество RAG документов по типам
SELECT 
  documentType,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as unique_tenants,
  COUNT(DISTINCT sourceId) as unique_sources
FROM `nv-horizon.nv_horizon_documents.documents_metadata`
GROUP BY documentType
ORDER BY count DESC;

-- Количество CoreDocs по типам контента
SELECT 
  contentType,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as unique_tenants,
  COUNT(DISTINCT projectId) as unique_projects
FROM `nv-horizon.nv_horizon_documents.core_docs_metadata`
GROUP BY contentType
ORDER BY count DESC;

-- Документы по датам создания
SELECT 
  DATE(createdAt) as date,
  COUNT(*) as documents_created,
  COUNT(DISTINCT tenantId) as active_tenants
FROM `nv-horizon.nv_horizon_documents.documents_metadata`
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
FROM `nv-horizon.projects.projects` p
JOIN `nv-horizon.nv_horizon_documents.documents_metadata` d 
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
      MODEL `nv-horizon.nv_horizon_documents.gemini_model`,
      (
        SELECT content 
        FROM `nv-horizon.nv_horizon_documents.documents_object_table` 
        WHERE uri = m.storagePath
      ),
      STRUCT('Extract: key entities, sentiment score (0-1), main topics' AS prompt)
    ) AS analysis_result
  FROM `nv-horizon.nv_horizon_documents.documents_metadata` m
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
FROM `nv-horizon.nv_horizon_documents.documents_metadata` m
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
FROM `nv-horizon.nv_horizon_documents.documents_metadata`
GROUP BY sourceId
ORDER BY total_documents DESC;

-- Use case: Тренды по языкам
SELECT 
  language,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as tenants,
  DATE_TRUNC(createdAt, MONTH) as month
FROM `nv-horizon.nv_horizon_documents.documents_metadata`
WHERE language != ''
GROUP BY language, month
ORDER BY month DESC, count DESC;

-- Use case: Статусы RAG документов
SELECT 
  status,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as tenants,
  AVG(TIMESTAMP_DIFF(updatedAt, createdAt, MINUTE)) as avg_processing_minutes
FROM `nv-horizon.nv_horizon_documents.documents_metadata`
GROUP BY status
ORDER BY count DESC;

-- Use case: Статусы парсинга CoreDocs
SELECT 
  parseStatus,
  COUNT(*) as count,
  COUNT(DISTINCT tenantId) as tenants,
  COUNT(DISTINCT projectId) as projects
FROM `nv-horizon.nv_horizon_documents.core_docs_metadata`
GROUP BY parseStatus
ORDER BY count DESC;

-- ============================================
-- 6. Работа с разными форматами документов
-- ============================================

-- Анализ JSON документов (decisions, facts, events)
SELECT 
  m.filename,
  m.contentType,
  m.projectId,
  m.createdAt,
  -- Извлекаем данные из JSON через Object Table
  j.data as json_content
FROM `nv-horizon.nv_horizon_documents.core_docs_metadata` m
JOIN `nv-horizon.nv_horizon_documents.core_docs_json_table` j
  ON j.uri = REPLACE(m.storagePath, 'gs://', '')
WHERE m.contentType = 'application/json'
  AND m.filename LIKE '%.json'
LIMIT 10;

-- Анализ текстовых документов (markdown, plain text)
SELECT 
  m.filename,
  m.contentType,
  m.projectId,
  m.createdAt,
  -- Читаем контент из Object Table
  t.data as text_content
FROM `nv-horizon.nv_horizon_documents.core_docs_metadata` m
JOIN `nv-horizon.nv_horizon_documents.core_docs_text_table` t
  ON t.uri = REPLACE(m.storagePath, 'gs://', '')
WHERE m.contentType LIKE 'text/%'
LIMIT 10;

-- Комбинированный анализ: RAG документы + CoreDocs
WITH rag_stats AS (
  SELECT 
    tenantId,
    COUNT(*) as rag_count,
    COUNT(DISTINCT sourceId) as rag_sources
  FROM `nv-horizon.nv_horizon_documents.documents_metadata`
  GROUP BY tenantId
),
core_stats AS (
  SELECT 
    tenantId,
    COUNT(*) as core_count,
    COUNT(DISTINCT projectId) as core_projects
  FROM `nv-horizon.nv_horizon_documents.core_docs_metadata`
  GROUP BY tenantId
)
SELECT 
  COALESCE(r.tenantId, c.tenantId) as tenantId,
  COALESCE(r.rag_count, 0) as rag_documents,
  COALESCE(r.rag_sources, 0) as rag_sources,
  COALESCE(c.core_count, 0) as core_documents,
  COALESCE(c.core_projects, 0) as core_projects,
  COALESCE(r.rag_count, 0) + COALESCE(c.core_count, 0) as total_documents
FROM rag_stats r
FULL OUTER JOIN core_stats c ON r.tenantId = c.tenantId
ORDER BY total_documents DESC;

