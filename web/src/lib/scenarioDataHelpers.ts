/**
 * Helper functions for ScenarioData - interpreting queries and executing BigQuery
 */

import { api } from './api';
import type { 
  ScenarioData, 
  BigQueryQueriesInterpretation,
  QueryExecutionResult,
  ColumnSchema,
  ExecutionStage,
} from '../types/scenarioData';
import { BIGQUERY_QUERIES_JSON_SCHEMA } from '../types/scenarioData';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { useScenarioDataStore } from '../stores/scenarioDataStore';

/**
 * Interprets user query and generates BigQuery SQL queries using LLM with JSON Schema
 */
export async function interpretBigQueryQuery(
  userQuery: string,
  projectId: string,
  model?: string
): Promise<BigQueryQueriesInterpretation> {
  console.log('[ScenarioData] Interpreting query:', userQuery);
  
  // Get schema info for context
  const schemaData = await api.getBigQuerySchema(projectId);
  
  // Get model from settings if not provided
  const settings = useGenAISettings.getState();
  const selectedModel = model || settings.model;
  
  // Build prompt for query interpretation
  const filterInstructions = `\n\nCRITICAL FILTERING REQUIREMENTS:
- ALWAYS filter by tenantId = '${schemaData.tenantID}' when querying core_docs_metadata or documents_metadata tables
- ALWAYS filter by projectId = '${schemaData.projectIDFilter}' when querying core_docs_metadata table
- These filters MUST be included in WHERE clauses to ensure data isolation
- Example: WHERE tenantId = '${schemaData.tenantID}' AND projectId = '${schemaData.projectIDFilter}'`;

  const prompt = `You are a BigQuery SQL expert. Analyze the user's query and determine what SQL queries need to be executed.

=== DATABASE SCHEMA ===
PROJECT: ${schemaData.projectID}
DATASET: ${schemaData.datasetID}
TENANT: ${schemaData.tenantID}

FULL SCHEMA INFORMATION:
${schemaData.schemaInfo}

=== END OF SCHEMA ===

AVAILABLE DATA:
- core_docs_metadata: Document metadata (id, filename, contentType, storagePath, extractedText, parseMeta, createdAt, etc.)
  * extractedText: STRING field containing plain text content (NOT JSON). Use LIKE, CONTAINS, or REGEXP_CONTAINS for text search.
  * parseMeta: JSON field. For CSV/Excel files, it contains a 'preview' array with the first ~100-500 rows.
    - Structure: {"preview": [{"col1": "val1", "col2": "val2"}, ...], "columns": [...]}
    - To query CSV data: SELECT JSON_EXTRACT_SCALAR(row, '$.col_name') FROM core_docs_metadata t, UNNEST(JSON_EXTRACT_ARRAY(t.parseMeta, '$.preview')) row
- documents_metadata: RAG document metadata (id, title, url, documentPath, storagePath, status, contentType, createdAt, etc.)
  * IMPORTANT: This table DOES NOT contain document text content (no fullText/extractedText field). It only contains metadata.
- analytics_cleaned_data: PREPARED ANALYTICS TABLE (Cleaned & Normalized).
  * Use this table for full data analysis. It contains clean data from the uploaded CSV.
  * Schema:
    - id: INT64
    - contract_id: INT64
    - sender: STRING (Отправитель)
    - receiver: STRING (Получатель)
    - category: STRING
    - product: STRING (Товар)
    - volume: FLOAT64 (Объем/Количество)
    - unit: STRING (Ед. изм.)
    - amount: FLOAT64 (Сумма)
    - delivery_date: TIMESTAMP (Дата Поставки)
    - delivery_period: STRING
    - status_overdue: STRING
  * Example: SELECT delivery_date, product, volume, amount FROM \`etigram.etigram_documents.analytics_cleaned_data\` WHERE EXTRACT(YEAR FROM delivery_date) = 2025

IMPORTANT ABOUT extractedText and parseMeta:
- extractedText is a STRING field, NOT JSON. Do NOT use JSON_EXTRACT_ARRAY or JSON_EXTRACT on extractedText.
- To query CSV/Excel data, ALWAYS use 'analytics_cleaned_data' table.
- documents_metadata has NO text content. Do not try to query fullText or extractedText on it.
${filterInstructions}

USER QUERY:
${userQuery}

TASK:
1. Analyze the user query to understand what data they need
2. Determine if this requires ONE SQL query or MULTIPLE independent SQL queries
3. For each query needed:
   - Generate valid BigQuery SQL
   - Provide a clear description of what the query does
   - Set execute: true (unless the query is clearly not needed)

EXAMPLES:
- "прогноз цен" → ONE query for price forecast
- "прогноз цен и прогноз поставок" → TWO queries (one for prices, one for supplies)
- "прогноз цен с графиком сезонности" → TWO queries (forecast + seasonality chart data)

REQUIRED JSON FORMAT:
You MUST return a JSON object with this exact structure:
{
  "queries": [
    {
      "sql": "SELECT * FROM \`${schemaData.projectID}.${schemaData.datasetID}.table_name\` WHERE condition",
      "description": "Clear description of what this query does",
      "execute": true
    }
  ]
}

EXAMPLE RESPONSE:
{
  "queries": [
    {
      "sql": "SELECT delivery_date, product, volume, amount FROM \`${schemaData.projectID}.${schemaData.datasetID}.analytics_cleaned_data\` WHERE EXTRACT(YEAR FROM delivery_date) = 2025 ORDER BY delivery_date ASC",
      "description": "Fetch all delivery records for year 2025 with product details",
      "execute": true
    }
  ]
}

IMPORTANT:
- Use \`${schemaData.projectID}.${schemaData.datasetID}.table_name\` format
- Filter by tenantId = '${schemaData.tenantID}' AND projectId = '${schemaData.projectIDFilter}' when querying core_docs_metadata
- For ML/forecasting:
  1. Use CREATE OR REPLACE MODEL (not just CREATE MODEL) to avoid "Already Exists" errors.
  2. Use model_type='ARIMA_PLUS' for time series.
  3. REQUIRED: You MUST specify 'time_series_data_col' (the value to forecast, e.g. 'volume') in OPTIONS.
  4. REQUIRED: You MUST specify 'time_series_timestamp_col' in OPTIONS.
  5. REQUIRED: If aggregating by MONTH, you MUST specify 'data_frequency' = 'MONTHLY' in OPTIONS. If aggregating by DAY, use 'DAILY'. Do NOT rely on auto-detection for small datasets (< 20 points).
  6. Specify 'time_series_id_col' (e.g. 'series_id') in OPTIONS if creating multiple series.
  7. CRITICAL: When selecting from ML.FORECAST(...), the output columns depend on creation OPTIONS:
     - If you set time_series_id_col='my_id_col', the output column is 'my_id_col' (NOT 'series_id'!).
     - 'forecast_timestamp' (always this name, TIMESTAMP type)
     - 'forecast_value' (always this name, FLOAT64 type)
  8. IMPORTANT: Inside STRUCT(), use 'AS' syntax, NOT '=>'. Example: STRUCT(6 AS horizon, 0.95 AS confidence_level).
  9. TIP: If training data covers < 1 year, prefer aggregating by WEEK or DAY (not MONTH) to provide enough data points for the model, then aggregate the forecast result to MONTH if needed.
- DATETIME/TIMESTAMP handling:
  1. DATE_TRUNC(timestamp_col, MONTH) returns TIMESTAMP.
  2. ML.FORECAST returns 'forecast_timestamp' as TIMESTAMP.
  3. To join correctly, ensure BOTH sides are the same type.
  4. BEST PRACTICE: Cast to DATE immediately in CTEs if you plan to join by day/month.
     Example: CAST(DATE_TRUNC(delivery_date, MONTH) AS DATE) AS month_date
  5. Then join: ON t1.month_date = t2.month_date (both are DATE).
- Return ONLY valid JSON matching the schema above, no markdown, no explanations
- Field name MUST be "sql" (not "query" or any other name)`;

  // Call genAI with strict JSON Schema
  const response = await api.genaiGenerate({
    model: selectedModel,
    prompt,
    systemInstruction: 'You are a BigQuery SQL expert. Generate only valid JSON matching the provided schema. Return structured queries array.',
    temperature: 0.1,
    maxOutputTokens: 4096,
    webSearchEnabled: false,
    toolCallingEnabled: false,
    strictJSONEnabled: true,
    jsonSchema: BIGQUERY_QUERIES_JSON_SCHEMA as any,
  });

  // Parse JSON response
  let interpretation: BigQueryQueriesInterpretation;
  try {
    const jsonContent = response.rawJSON || response.content;
    if (!jsonContent) {
      throw new Error('Empty response from LLM');
    }
    
    // Log raw response for debugging
    console.log('[ScenarioData] Raw LLM response:', {
      rawJSON: response.rawJSON,
      content: response.content,
      jsonContent: jsonContent,
    });
    
    interpretation = JSON.parse(jsonContent);
    
    // Handle case when LLM returns array directly instead of {queries: [...]}
    if (Array.isArray(interpretation)) {
      interpretation = { queries: interpretation };
    }
    
    // Log parsed interpretation
    console.log('[ScenarioData] Parsed interpretation:', {
      queriesCount: interpretation.queries?.length,
      queries: interpretation.queries,
      fullInterpretation: JSON.stringify(interpretation, null, 2),
    });
    
    // Validate structure
    if (!interpretation.queries || !Array.isArray(interpretation.queries) || interpretation.queries.length === 0) {
      console.error('[ScenarioData] Invalid interpretation structure:', {
        hasQueries: !!interpretation.queries,
        isArray: Array.isArray(interpretation.queries),
        length: interpretation.queries?.length,
        interpretation,
      });
      throw new Error('Invalid interpretation: queries array is required');
    }
    
    // Normalize and validate each query
    for (let i = 0; i < interpretation.queries.length; i++) {
      const query = interpretation.queries[i] as any;
      
      // Handle case when LLM returns 'query' instead of 'sql'
      if (!query.sql && query.query) {
        query.sql = query.query;
        delete query.query;
      }
      
      if (!query.sql || typeof query.sql !== 'string') {
        console.error(`[ScenarioData] Query ${i} missing sql field:`, query);
        throw new Error(`Query ${i} is missing required 'sql' field`);
      }
      if (!query.description || typeof query.description !== 'string') {
        console.error(`[ScenarioData] Query ${i} missing description field:`, query);
        throw new Error(`Query ${i} is missing required 'description' field`);
      }
    }
    
    // Set default execute: true if not provided
    interpretation.queries = interpretation.queries.map(q => ({
      ...q,
      execute: q.execute !== false, // default to true
    }));
    
    console.log('[ScenarioData] Interpretation successful:', {
      queryCount: interpretation.queries.length,
      queries: interpretation.queries.map(q => ({ description: q.description, execute: q.execute })),
    });
    
    return interpretation;
  } catch (error) {
    console.error('[ScenarioData] Failed to parse interpretation:', error);
    throw new Error(`Failed to interpret query: ${error instanceof Error ? error.message : 'Unknown error'}`);
  }
}

/**
 * Executes a single SQL query and returns result
 */
async function executeSingleQuery(
  sql: string,
  projectId: string,
  maxResults?: number
): Promise<QueryExecutionResult> {
  const startTime = Date.now();
  
  try {
    // Execute SQL
    const result = await api.executeSQL(projectId, sql, maxResults);
    const executionTimeMs = Date.now() - startTime;
    
    // Check for errors first
    if (result.error) {
      return {
        queryIndex: 0,
        sql,
        executionTimeMs,
        schema: [],
        rows: [],
        rowCount: 0,
        error: result.error,
      };
    }
    
    // Handle null/undefined results - this is a valid empty result, not an error
    // Backend returns null when query succeeds but returns no rows
    const resultsArray = result.results === null || result.results === undefined 
      ? [] 
      : Array.isArray(result.results) 
        ? result.results 
        : [];
    
    if (result.results !== null && result.results !== undefined && !Array.isArray(result.results)) {
      console.warn('[ScenarioData] Unexpected results type:', {
        type: typeof result.results,
        results: result.results,
      });
      return {
        queryIndex: 0,
        sql,
        executionTimeMs,
        schema: [],
        rows: [],
        rowCount: 0,
        error: 'Invalid response: results is not an array',
      };
    }
    
    // Extract schema from first row if available
    const schema: ColumnSchema[] = [];
    if (resultsArray.length > 0) {
      const firstRow = resultsArray[0];
      for (const key in firstRow) {
        // Infer type from value
        const value = firstRow[key];
        let type = 'STRING';
        if (typeof value === 'number') {
          type = Number.isInteger(value) ? 'INTEGER' : 'FLOAT';
        } else if (typeof value === 'boolean') {
          type = 'BOOLEAN';
        } else if (value instanceof Date || (typeof value === 'string' && /^\d{4}-\d{2}-\d{2}/.test(value))) {
          type = 'TIMESTAMP';
        }
        schema.push({ name: key, type });
      }
    }
    
    return {
      queryIndex: 0, // will be set by caller
      sql,
      executionTimeMs,
      schema,
      rows: resultsArray,
      rowCount: result.rowCount || resultsArray.length,
      error: undefined,
    };
  } catch (error) {
    const executionTimeMs = Date.now() - startTime;
    return {
      queryIndex: 0,
      sql,
      executionTimeMs,
      schema: [],
      rows: [],
      rowCount: 0,
      error: error instanceof Error ? error.message : 'Unknown error',
    };
  }
}

/**
 * Executes all queries from interpretation and returns results
 */
export async function executeBigQueryQueries(
  interpretation: BigQueryQueriesInterpretation,
  projectId: string,
  maxResults?: number
): Promise<QueryExecutionResult[]> {
  console.log('[ScenarioData] Executing queries:', {
    totalQueries: interpretation.queries.length,
    queries: interpretation.queries.map((q, idx) => ({
      index: idx,
      description: q.description,
      sql: q.sql,
      execute: q.execute,
    })),
  });
  
  const results: QueryExecutionResult[] = [];
  
  // Execute each query that has execute: true
  for (let i = 0; i < interpretation.queries.length; i++) {
    const query = interpretation.queries[i];
    
    if (!query.execute) {
      console.log(`[ScenarioData] Skipping query ${i}: execute=false`);
      continue;
    }
    
    console.log(`[ScenarioData] Executing query ${i + 1}/${interpretation.queries.length}:`, {
      description: query.description,
      sql: query.sql,
      sqlLength: query.sql?.length,
    });
    
    const result = await executeSingleQuery(query.sql, projectId, maxResults);
    result.queryIndex = i;
    
    console.log(`[ScenarioData] Query ${i + 1} result:`, {
      success: !result.error,
      error: result.error,
      rowCount: result.rowCount,
      rowsLength: result.rows?.length,
    });
    
    // Generate explanation if not provided (optional - don't fail if this fails)
    if (!result.error && !result.explanation) {
      try {
        const explanationResponse = await api.genaiGenerate({
          model: 'gemini_fast',
          prompt: `Explain what this BigQuery SQL query does in simple terms:\n\n${query.sql}\n\nProvide a brief explanation (1-2 sentences).`,
          temperature: 0.3,
          maxOutputTokens: 256,
          webSearchEnabled: false,
          toolCallingEnabled: false,
          strictJSONEnabled: false,
        });
        result.explanation = explanationResponse.content?.trim();
      } catch (err: any) {
        // Log error but don't fail the entire query execution
        // This is a non-critical operation - explanation is optional
        const errorMsg = err?.response?.data?.message || err?.message || 'Unknown error';
        const statusCode = err?.response?.status;
        console.warn('[ScenarioData] Failed to generate explanation (non-critical):', {
          error: errorMsg,
          statusCode,
          queryIndex: i,
          sql: query.sql?.substring(0, 100) + '...',
        });
        // Don't set result.error - explanation is optional
        // The query results are still valid without explanation
      }
    }
    
    results.push(result);
  }
  
  console.log('[ScenarioData] Execution completed:', {
    total: results.length,
    successful: results.filter(r => !r.error).length,
    failed: results.filter(r => r.error).length,
  });
  
  return results;
}

/**
 * Main function: interprets user query and executes BigQuery queries
 * Returns ScenarioData ready to be stored
 */
export async function processScenarioQuery(
  userQuery: string,
  scenarioId: string,
  projectId: string,
  model?: string,
  maxResults?: number,
  onProgress?: (stages: ExecutionStage[]) => void
): Promise<ScenarioData> {
  console.log('[ScenarioData] Processing scenario query:', { userQuery, scenarioId, projectId });
  
  const now = new Date().toISOString();
  const id = `${Date.now()}-${Math.random().toString(36).substring(2, 11)}`;
  
  // Initialize execution stages (using translation keys)
  const stages: ExecutionStage[] = [
    { name: 'unfold.scenarios.display.stageDataStructure', status: 'pending' },
    { name: 'unfold.scenarios.display.stageQueryFormulation', status: 'pending' },
    { name: 'unfold.scenarios.display.stageQueryExecution', status: 'pending' },
  ];
  
  // Create initial ScenarioData and save to store immediately
  let scenarioData: ScenarioData = {
    id,
    scenarioId,
    projectId,
    userQuery,
    executionStages: [...stages],
    interpretation: {
      queries: [],
      executedAt: now,
    },
    results: [],
    isComposite: false,
    createdAt: now,
    updatedAt: now,
  };
  
  // Save initial state to store
  useScenarioDataStore.getState().addScenarioData(scenarioData);
  
  // Helper to update store
  const updateStore = (updater: (data: ScenarioData) => ScenarioData) => {
    scenarioData = updater(scenarioData);
    useScenarioDataStore.getState().addScenarioData(scenarioData);
  };
  
  // Get schema info (Step 0)
  stages[0].status = 'running';
  stages[0].startTime = Date.now();
  updateStore((data) => ({
    ...data,
    executionStages: [...stages],
    updatedAt: new Date().toISOString(),
  }));
  if (onProgress) onProgress([...stages]);
  
  let schemaData: Awaited<ReturnType<typeof api.getBigQuerySchema>>;
  try {
    schemaData = await api.getBigQuerySchema(projectId);
    stages[0].status = 'completed';
    stages[0].endTime = Date.now();
    stages[0].durationMs = stages[0].endTime - (stages[0].startTime || 0);
    
    // Update with schema data
    updateStore((data) => ({
      ...data,
      databaseSchema: {
        projectID: schemaData.projectID,
        datasetID: schemaData.datasetID,
        schemaInfo: schemaData.schemaInfo,
        tenantID: schemaData.tenantID,
        projectIDFilter: schemaData.projectIDFilter,
      },
      executionStages: [...stages],
      updatedAt: new Date().toISOString(),
    }));
    if (onProgress) onProgress([...stages]);
  } catch (error) {
    stages[0].status = 'error';
    stages[0].endTime = Date.now();
    stages[0].durationMs = stages[0].endTime - (stages[0].startTime || 0);
    stages[0].error = error instanceof Error ? error.message : 'Unknown error';
    updateStore((data) => ({
      ...data,
      executionStages: [...stages],
      updatedAt: new Date().toISOString(),
    }));
    if (onProgress) onProgress([...stages]);
    throw error;
  }
  
  // Step 1: Interpret query
  stages[1].status = 'running';
  stages[1].startTime = Date.now();
  updateStore((data) => ({
    ...data,
    executionStages: [...stages],
    updatedAt: new Date().toISOString(),
  }));
  if (onProgress) onProgress([...stages]);
  
  let interpretation: BigQueryQueriesInterpretation;
  try {
    interpretation = await interpretBigQueryQuery(userQuery, projectId, model);
    stages[1].status = 'completed';
    stages[1].endTime = Date.now();
    stages[1].durationMs = stages[1].endTime - (stages[1].startTime || 0);
    
    // Map queries to ensure execute is boolean (not optional)
    const queriesWithExecute = interpretation.queries.map(q => ({
      sql: q.sql,
      description: q.description,
      execute: q.execute !== false, // convert optional to required boolean
    }));
    
    updateStore((data) => ({
      ...data,
      interpretation: {
        queries: queriesWithExecute,
        executedAt: new Date().toISOString(),
      },
      isComposite: interpretation.queries.length > 1,
      executionStages: [...stages],
      updatedAt: new Date().toISOString(),
    }));
    if (onProgress) onProgress([...stages]);
  } catch (error) {
    stages[1].status = 'error';
    stages[1].endTime = Date.now();
    stages[1].durationMs = stages[1].endTime - (stages[1].startTime || 0);
    stages[1].error = error instanceof Error ? error.message : 'Unknown error';
    updateStore((data) => ({
      ...data,
      executionStages: [...stages],
      updatedAt: new Date().toISOString(),
    }));
    if (onProgress) onProgress([...stages]);
    throw error;
  }
  
  // Step 2: Execute queries
  stages[2].status = 'running';
  stages[2].startTime = Date.now();
  updateStore((data) => ({
    ...data,
    executionStages: [...stages],
    updatedAt: new Date().toISOString(),
  }));
  if (onProgress) onProgress([...stages]);
  
  let results: QueryExecutionResult[];
  try {
    results = await executeBigQueryQueries(interpretation, projectId, maxResults);
    stages[2].status = 'completed';
    stages[2].endTime = Date.now();
    stages[2].durationMs = stages[2].endTime - (stages[2].startTime || 0);
    
    updateStore((data) => ({
      ...data,
      results,
      executionStages: [...stages],
      updatedAt: new Date().toISOString(),
    }));
    if (onProgress) onProgress([...stages]);
  } catch (error) {
    stages[2].status = 'error';
    stages[2].endTime = Date.now();
    stages[2].durationMs = stages[2].endTime - (stages[2].startTime || 0);
    stages[2].error = error instanceof Error ? error.message : 'Unknown error';
    updateStore((data) => ({
      ...data,
      executionStages: [...stages],
      updatedAt: new Date().toISOString(),
    }));
    if (onProgress) onProgress([...stages]);
    throw error;
  }
  
  console.log('[ScenarioData] ScenarioData created:', {
    id: scenarioData.id,
    isComposite: scenarioData.isComposite,
    resultsCount: scenarioData.results.length,
    stagesCount: scenarioData.executionStages?.length,
  });
  
  return scenarioData;
}

