/**
 * ScenarioData types for storing BigQuery query results in Scenarios mode
 * These are temporary frontend-only types for testing functionality
 */

// JSON Schema for LLM structured response
export const BIGQUERY_QUERIES_JSON_SCHEMA = {
  type: 'object',
  properties: {
    queries: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          sql: {
            type: 'string',
            description: 'SQL query to execute in BigQuery',
          },
          description: {
            type: 'string',
            description: 'Human-readable description of what this query does',
          },
          execute: {
            type: 'boolean',
            description: 'Whether to execute this query (default: true)',
          },
        },
        required: ['sql', 'description'],
      },
      minItems: 1,
    },
  },
  required: ['queries'],
} as const;

// Type for LLM interpretation response
export interface BigQueryQueriesInterpretation {
  queries: Array<{
    sql: string;
    description: string;
    execute?: boolean;
  }>;
}

// Schema column definition
export interface ColumnSchema {
  name: string;
  type: string;
}

// Result of executing a single query
export interface QueryExecutionResult {
  queryIndex: number; // index in interpretation.queries
  sql: string; // фактически выполненный SQL
  explanation?: string; // объяснение запроса
  executionTimeMs?: number;
  
  // Данные из BQ
  schema: ColumnSchema[];
  rows: Array<Record<string, any>>;
  rowCount: number;
  
  // Ошибки (если были)
  error?: string;
  errorHistory?: string[]; // Note: not populated by executeSQL API, only used by queryBigQuery
}

// Execution stage for progress tracking
export interface ExecutionStage {
  name: string;
  status: 'pending' | 'running' | 'completed' | 'error';
  startTime?: number; // timestamp in ms
  endTime?: number; // timestamp in ms
  durationMs?: number;
  error?: string;
}

// Main ScenarioData type
export interface ScenarioData {
  id: string;
  scenarioId: string; // привязка к сценарию
  projectId: string;
  
  // Исходный запрос пользователя
  userQuery: string;
  
  // Схема БД для отображения
  databaseSchema?: {
    projectID: string;
    datasetID: string;
    schemaInfo: string;
    tenantID: string;
    projectIDFilter: string;
  };
  
  // Стадии выполнения
  executionStages?: ExecutionStage[];
  
  // Результат интерпретации
  interpretation: {
    queries: Array<{
      sql: string;
      description: string;
      execute: boolean;
    }>;
    executedAt: string; // ISO timestamp
  };
  
  // Результаты выполнения
  results: QueryExecutionResult[];
  
  // Флаг составного запроса
  isComposite: boolean; // true если queries.length > 1
  
  // Метаданные
  createdAt: string; // ISO timestamp
  updatedAt: string; // ISO timestamp
}

