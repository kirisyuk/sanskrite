package services

import (
	"context"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/bigquery"
	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"google.golang.org/api/iterator"
)

// BigQueryAIService handles natural language queries to BigQuery
type BigQueryAIService struct {
	bqClient    *gcp.BigQueryClient
	genaiClient *gen_combined.Client
	logger      *log.Logger
}

// NewBigQueryAIService creates a new BigQuery AI service
func NewBigQueryAIService(bqClient *gcp.BigQueryClient, genaiClient *gen_combined.Client, logger *log.Logger) *BigQueryAIService {
	return &BigQueryAIService{
		bqClient:    bqClient,
		genaiClient: genaiClient,
		logger:      logger,
	}
}

// QueryRequest represents a natural language query request
type QueryRequest struct {
	NaturalLanguageQuery string `json:"naturalLanguageQuery"`
	ProjectID            string `json:"projectID,omitempty"`
	DatasetID            string `json:"datasetID,omitempty"`
	MaxResults           int    `json:"maxResults,omitempty"`
}

// QueryResponse represents the result of query execution
type QueryResponse struct {
	SQL         string                   `json:"sql"`
	Results     []map[string]interface{} `json:"results"`
	RowCount    int64                    `json:"rowCount"`
	Explanation string                   `json:"explanation,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

// ExecuteQuery processes a natural language query and returns results
func (s *BigQueryAIService) ExecuteQuery(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if req.NaturalLanguageQuery == "" {
		return nil, fmt.Errorf("natural language query is required")
	}

	// Get database schema information
	schemaInfo, err := s.getSchemaInfo(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Failed to get schema info: %v", err)
		}
		// Continue without schema, AI can work with general BigQuery knowledge
		schemaInfo = "Schema information unavailable"
	}

	// Generate SQL query using Vertex AI
	sqlQuery, explanation, err := s.generateSQL(ctx, req.NaturalLanguageQuery, schemaInfo)
	if err != nil {
		return &QueryResponse{
			SQL:   "",
			Error: fmt.Sprintf("Failed to generate SQL: %v", err),
		}, nil
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Generated SQL: %s", sqlQuery)
	}

	// Execute SQL query with automatic retry on errors
	results, rowCount, err := s.executeSQLWithRetry(ctx, sqlQuery, req.NaturalLanguageQuery, schemaInfo, req.MaxResults)
	if err != nil {
		return &QueryResponse{
			SQL:         sqlQuery,
			Explanation: explanation,
			Error:       fmt.Sprintf("Failed to execute SQL after retries: %v", err),
		}, nil
	}

	return &QueryResponse{
		SQL:         sqlQuery,
		Results:     results,
		RowCount:    rowCount,
		Explanation: explanation,
	}, nil
}

// getSchemaInfo retrieves database schema information
func (s *BigQueryAIService) getSchemaInfo(ctx context.Context) (string, error) {
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		datasetID = "sanskrite_documents"
	}

	tables, err := s.bqClient.ListTables(ctx)
	if err != nil {
		return "", err
	}

	var schemaParts []string
	schemaParts = append(schemaParts, fmt.Sprintf("Dataset: %s", datasetID))
	schemaParts = append(schemaParts, fmt.Sprintf("Tables: %s", strings.Join(tables, ", ")))

	// Get schemas for main tables
	for _, tableName := range tables {
		if len(schemaParts) > 20 { // Limit amount of information
			break
		}
		schema, err := s.bqClient.GetTableSchema(ctx, tableName)
		if err != nil {
			continue
		}
		var fields []string
		if schema != nil {
			for _, field := range *schema {
				fieldType := string(field.Type)
				fields = append(fields, fmt.Sprintf("%s (%s)", field.Name, fieldType))
			}
		}
		schemaParts = append(schemaParts, fmt.Sprintf("\nTable %s:\n  Fields: %s", tableName, strings.Join(fields, ", ")))
	}

	return strings.Join(schemaParts, "\n"), nil
}

// generateSQL generates SQL query from natural language query
func (s *BigQueryAIService) generateSQL(ctx context.Context, naturalQuery, schemaInfo string) (string, string, error) {
	projectID := s.bqClient.ProjectID()
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		datasetID = "sanskrite_documents"
	}

	prompt := fmt.Sprintf(`You are a BigQuery SQL expert. Your task is to convert a natural language query into a valid BigQuery SQL query.

PROJECT: %s
DATASET: %s

DATABASE SCHEMA INFORMATION:
%s

AVAILABLE TABLES AND FORMATS:
- documents_metadata: RAG documents metadata (all stored as .txt files in GCS)
- core_docs_metadata: CoreDocs metadata (supports multiple formats: .txt, .md, .csv, .tsv, .json, .pdf, .docx)
- documents_object_table: Object table for RAG documents text files (gs://{project}-rag-documents/**/*.txt)
- core_docs_text_table: Object table for CoreDocs text formats (.txt, .md, .csv, .tsv)
- core_docs_json_table: Object table for CoreDocs JSON files (.json)

IMPORTANT NOTES:
- RAG documents are always stored as .txt files with extracted text content
- CoreDocs can be in various formats: text files (.txt, .md, .csv, .tsv), JSON (.json), PDF (.pdf), Word (.docx)
- For PDF and DOCX files, use extractedText field from core_docs_metadata table
- For JSON files, you can join with core_docs_json_table or use extractedText
- For text files, you can join with core_docs_text_table or use extractedText
- Use storagePath field to match with Object Tables (remove 'gs://' prefix when joining)

USER QUERY (in natural language):
%s

REQUIREMENTS (CRITICAL - FOLLOW STRICTLY):
1. Generate ONLY valid BigQuery SQL (Standard SQL dialect) - MUST be syntactically correct
2. Use proper table names with project and dataset prefixes: `+"`%s.%s.table_name`"+`
3. ALWAYS verify table and column names exist in the schema before using them
4. Use appropriate WHERE clauses, JOINs, GROUP BY, ORDER BY as needed
5. Include LIMIT clause if the query might return many rows (default LIMIT 100)
6. Return ONLY the SQL query, no explanations or markdown formatting
7. If the query asks for aggregation, use appropriate aggregate functions (COUNT, SUM, AVG, etc.)
8. Handle date/time filtering properly using BigQuery date functions
9. Use proper data types and casting when needed - check schema for correct types
10. When working with different document formats, use appropriate tables (documents_metadata for RAG, core_docs_metadata for CoreDocs)
11. For content analysis, prefer extractedText field when available, or join with Object Tables
12. You can use WITH clauses to create temporary tables within the query
13. You can use CREATE TABLE AS SELECT (CTAS) if the user explicitly asks to save results
14. BigQuery can read directly from GCS Object Tables without pre-creating metadata tables
15. For complex queries, use subqueries or CTEs (WITH clauses) to structure the logic
16. ALWAYS handle NULL values properly - use COALESCE, IFNULL, or NULL-safe comparisons
17. When joining Object Tables, use REPLACE(storagePath, 'gs://', '') to match with uri field
18. For JSON fields, use JSON_EXTRACT_SCALAR or JSON_EXTRACT functions correctly
19. Double-check all column names against the schema - typos will cause errors
20. If user asks for specific format, ensure the query returns data in that format

DYNAMIC TABLE CREATION:
- BigQuery supports CREATE TABLE AS SELECT to create tables from query results
- You can use WITH clauses for temporary tables within a single query
- Object Tables allow reading directly from GCS without metadata tables
- If user asks to "create a table" or "save results", use CREATE TABLE AS SELECT syntax

MACHINE LEARNING AND FORECASTING:
- BigQuery ML supports creating ML models directly in SQL
- Model types: LINEAR_REG, LOGISTIC_REG, KMEANS, ARIMA, ARIMA_PLUS, XGBOOST
- For time series forecasting, use ARIMA_PLUS model type
- Use ML.FORECAST() function for predictions with ARIMA models
- Use ML.PREDICT() function for predictions with regression/classification models
- Use ML.EVALUATE() to assess model quality
- If user asks to "predict", "forecast", "model", "analyze trends", generate SQL with CREATE MODEL and ML.FORECAST/ML.PREDICT
- Example: CREATE MODEL ... OPTIONS(model_type='ARIMA_PLUS', horizon=180) AS SELECT ...
- Example: SELECT * FROM ML.FORECAST(MODEL model_name, STRUCT(horizon AS horizon))
- For complex ML tasks, can use Vertex AI Remote Models via ML.GENERATE_TEXT

VALIDATION CHECKLIST (before returning SQL):
✓ All table names use correct project.dataset.table format
✓ All column names exist in the schema
✓ JOIN conditions are correct and use proper keys
✓ Data types match (no string vs number mismatches)
✓ NULL handling is appropriate
✓ LIMIT clause is present for large result sets
✓ Syntax is valid BigQuery Standard SQL

IMPORTANT: Return ONLY the SQL query text, nothing else. No markdown code blocks, no explanations, just pure SQL. The query MUST work on first try.`,
		projectID, datasetID, schemaInfo, naturalQuery, projectID, datasetID)

	// Generate SQL
	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		Temperature:       0.1,  // Low temperature for more accurate SQL
		MaxOutputTokens:   4096, // Increased to support complex queries with multiple CTEs and subqueries
		SystemInstruction: "You are a BigQuery SQL expert. Generate only valid SQL queries without any explanations or markdown formatting. Handle complex queries with multiple CTEs, JOINs, and subqueries. Always optimize for performance by using appropriate filters and limits.",
	}

	response, err := s.genaiClient.Generate(ctx, prompt, nil, opts)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate SQL: %w", err)
	}

	sqlQuery := strings.TrimSpace(response.Content)
	// Remove markdown code blocks if present
	sqlQuery = strings.TrimPrefix(sqlQuery, "```sql")
	sqlQuery = strings.TrimPrefix(sqlQuery, "```")
	sqlQuery = strings.TrimSpace(sqlQuery)

	// Generate query explanation
	explanationPrompt := fmt.Sprintf(`Explain what this BigQuery SQL query does in simple terms:

%s

Provide a brief explanation (1-2 sentences).`, sqlQuery)

	explanationResponse, err := s.genaiClient.Generate(ctx, explanationPrompt, nil, gen_combined.Options{
		ModelOverride:   "gemini_fast",
		Temperature:     0.3,
		MaxOutputTokens: 256,
	})

	explanation := ""
	if err == nil {
		explanation = explanationResponse.Content
	}

	return sqlQuery, explanation, nil
}

// executeSQLWithRetry executes SQL query with automatic regeneration on errors
func (s *BigQueryAIService) executeSQLWithRetry(ctx context.Context, sqlQuery, naturalQuery, schemaInfo string, maxResults int) ([]map[string]interface{}, int64, error) {
	maxRetries := 2
	var lastErr error
	currentSQL := sqlQuery

	for attempt := 0; attempt <= maxRetries; attempt++ {
		results, rowCount, err := s.executeSQL(ctx, currentSQL, maxResults)
		if err == nil {
			return results, rowCount, nil
		}

		lastErr = err

		if attempt < maxRetries {
			// Attempt to fix SQL based on error
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] Query failed (attempt %d/%d), attempting to fix: %v", attempt+1, maxRetries+1, err)
			}

			fixedSQL, err := s.fixSQLQuery(ctx, currentSQL, err.Error(), naturalQuery, schemaInfo)
			if err != nil {
				if s.logger != nil {
					s.logger.Printf("[BigQueryAI] Failed to fix SQL: %v", err)
				}
				break
			}

			currentSQL = fixedSQL
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] Regenerated SQL: %s", currentSQL)
			}
		}
	}

	return nil, 0, fmt.Errorf("query execution failed after %d attempts: %w", maxRetries+1, lastErr)
}

// fixSQLQuery attempts to fix SQL query based on error message
func (s *BigQueryAIService) fixSQLQuery(ctx context.Context, originalSQL, errorMsg, naturalQuery, schemaInfo string) (string, error) {
	projectID := s.bqClient.ProjectID()
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		datasetID = "sanskrite_documents"
	}

	prompt := fmt.Sprintf(`You are a BigQuery SQL expert. A SQL query failed with an error. Your task is to fix the query.

PROJECT: %s
DATASET: %s

ORIGINAL USER QUERY (in natural language):
%s

ORIGINAL SQL QUERY (that failed):
%s

ERROR MESSAGE:
%s

DATABASE SCHEMA INFORMATION:
%s

AVAILABLE TABLES:
- documents_metadata: RAG documents metadata
- core_docs_metadata: CoreDocs metadata
- documents_object_table: Object table for RAG documents (GCS)
- core_docs_object_table: Object table for CoreDocs (GCS)
- core_docs_json_table: Object table for CoreDocs JSON files (GCS)

INSTRUCTIONS:
1. Analyze the error message carefully
2. Fix the SQL query to resolve the error
3. Common fixes:
   - Fix table/column names (use proper project.dataset.table format)
   - Fix JOIN conditions
   - Fix data type mismatches
   - Add missing WHERE clauses or filters
   - Fix syntax errors
   - Handle NULL values properly
4. Ensure the fixed query still answers the original user query
5. Return ONLY the fixed SQL query, nothing else

FIXED SQL QUERY:`,
		projectID, datasetID, naturalQuery, originalSQL, errorMsg, schemaInfo)

	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		Temperature:       0.1,
		MaxOutputTokens:   4096,
		SystemInstruction: "You are a BigQuery SQL expert. Fix SQL queries based on error messages. Return only the fixed SQL query without explanations.",
	}

	response, err := s.genaiClient.Generate(ctx, prompt, nil, opts)
	if err != nil {
		return "", fmt.Errorf("failed to fix SQL: %w", err)
	}

	fixedSQL := strings.TrimSpace(response.Content)
	// Remove markdown code blocks if present
	fixedSQL = strings.TrimPrefix(fixedSQL, "```sql")
	fixedSQL = strings.TrimPrefix(fixedSQL, "```")
	fixedSQL = strings.TrimSuffix(fixedSQL, "```")
	fixedSQL = strings.TrimSpace(fixedSQL)

	return fixedSQL, nil
}

// executeSQL executes SQL query and returns results
// Supports both SELECT queries and CREATE TABLE AS SELECT
func (s *BigQueryAIService) executeSQL(ctx context.Context, sqlQuery string, maxResults int) ([]map[string]interface{}, int64, error) {
	if maxResults <= 0 {
		maxResults = 1000
	}

	// Check if query is CREATE TABLE AS SELECT
	queryUpper := strings.TrimSpace(strings.ToUpper(sqlQuery))
	if strings.HasPrefix(queryUpper, "CREATE TABLE") || strings.HasPrefix(queryUpper, "CREATE OR REPLACE TABLE") {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Executing CREATE TABLE AS SELECT query")
		}
	}

	iter, err := s.bqClient.ExecuteQuery(ctx, sqlQuery)
	if err != nil {
		return nil, 0, fmt.Errorf("query execution failed: %w", err)
	}

	var results []map[string]interface{}
	var rowCount int64

	for {
		var row map[string]bigquery.Value
		err := iter.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read row: %w", err)
		}

		if len(results) >= maxResults {
			break
		}

		// Convert bigquery.Value to standard Go types
		resultRow := make(map[string]interface{})
		for k, v := range row {
			resultRow[k] = convertBigQueryValue(v)
		}
		results = append(results, resultRow)
		rowCount++
	}

	return results, rowCount, nil
}

// convertBigQueryValue converts bigquery.Value to standard Go types
func convertBigQueryValue(v bigquery.Value) interface{} {
	switch val := v.(type) {
	case []bigquery.Value:
		// Arrays
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = convertBigQueryValue(item)
		}
		return result
	case map[string]bigquery.Value:
		// Structures
		result := make(map[string]interface{})
		for k, item := range val {
			result[k] = convertBigQueryValue(item)
		}
		return result
	default:
		// Primitive types
		return v
	}
}
