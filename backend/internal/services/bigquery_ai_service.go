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
	ProjectID            string `json:"projectID,omitempty"`         // GCP project ID (BigQuery project)
	DatasetID            string `json:"datasetID,omitempty"`         // BigQuery dataset ID
	MaxResults           int    `json:"maxResults,omitempty"`        // Max results to return
	TenantID             string `json:"tenantID,omitempty"`          // Tenant ID for filtering documents
	DocumentProjectID    string `json:"documentProjectID,omitempty"` // Document project ID for filtering (CoreDoc projectId)
}

// QueryResponse represents the result of query execution
type QueryResponse struct {
	SQL          string                   `json:"sql"`
	Results      []map[string]interface{} `json:"results"`
	RowCount     int64                    `json:"rowCount"`
	Explanation  string                   `json:"explanation,omitempty"`
	Error        string                   `json:"error,omitempty"`
	ErrorHistory []string                 `json:"errorHistory,omitempty"` // History of errors during retries
}

// GenerateSQLResponse represents the result of SQL generation
type GenerateSQLResponse struct {
	SQL         string `json:"sql"`
	Explanation string `json:"explanation,omitempty"`
}

// ExecuteSQLResponse represents the result of SQL execution
type ExecuteSQLResponse struct {
	Results  []map[string]interface{} `json:"results"`
	RowCount int64                    `json:"rowCount"`
	Error    string                   `json:"error,omitempty"`
}

// ExecuteQuery processes a natural language query and returns results
func (s *BigQueryAIService) ExecuteQuery(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] ExecuteQuery called: query=%q, tenantID=%s, documentProjectID=%s, projectID=%s, datasetID=%s, maxResults=%d",
			req.NaturalLanguageQuery, req.TenantID, req.DocumentProjectID, req.ProjectID, req.DatasetID, req.MaxResults)
	}

	if req.NaturalLanguageQuery == "" {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] ERROR: NaturalLanguageQuery is empty")
		}
		return nil, fmt.Errorf("natural language query is required")
	}

	// Get database schema information
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Getting schema information...")
	}
	schemaInfo, err := s.getSchemaInfo(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] WARNING: Failed to get schema info: %v, continuing without schema", err)
		}
		// Continue without schema, AI can work with general BigQuery knowledge
		schemaInfo = "Schema information unavailable"
	} else {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Schema info retrieved successfully (length: %d chars)", len(schemaInfo))
		}
	}

	// Generate SQL query using Vertex AI
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Generating SQL from natural language query...")
	}
	sqlQuery, explanation, err := s.generateSQL(ctx, req.NaturalLanguageQuery, schemaInfo, req.TenantID, req.DocumentProjectID)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] ERROR: Failed to generate SQL: %v", err)
		}
		return &QueryResponse{
			SQL:   "",
			Error: fmt.Sprintf("Failed to generate SQL: %v", err),
		}, nil
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] SQL generated successfully: %s", sqlQuery)
		s.logger.Printf("[BigQueryAI] Explanation: %s", explanation)
	}

	// Execute SQL query with automatic retry on errors
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Executing SQL query with retry logic...")
	}
	results, rowCount, err, errorHistory := s.executeSQLWithRetry(ctx, sqlQuery, req.NaturalLanguageQuery, schemaInfo, req.MaxResults)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] ERROR: Failed to execute SQL after retries: %v", err)
			if len(errorHistory) > 0 {
				s.logger.Printf("[BigQueryAI] Error history: %v", errorHistory)
			}
		}
		return &QueryResponse{
			SQL:          sqlQuery,
			Explanation:  explanation,
			Error:        fmt.Sprintf("Failed to execute SQL after retries: %v", err),
			ErrorHistory: errorHistory,
		}, nil
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Query executed successfully: rowCount=%d, resultsCount=%d", rowCount, len(results))
	}

	return &QueryResponse{
		SQL:         sqlQuery,
		Results:     results,
		RowCount:    rowCount,
		Explanation: explanation,
	}, nil
}

// GetSchemaInfo retrieves database schema information (public method)
func (s *BigQueryAIService) GetSchemaInfo(ctx context.Context) (string, error) {
	return s.getSchemaInfo(ctx)
}

// ProjectID returns the BigQuery project ID
func (s *BigQueryAIService) ProjectID() string {
	return s.bqClient.ProjectID()
}

// DatasetID returns the BigQuery dataset ID
func (s *BigQueryAIService) DatasetID() string {
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		return "h1horizon_documents"
	}
	return datasetID
}

// getSchemaInfo retrieves database schema information
func (s *BigQueryAIService) getSchemaInfo(ctx context.Context) (string, error) {
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		datasetID = "h1horizon_documents"
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
func (s *BigQueryAIService) generateSQL(ctx context.Context, naturalQuery, schemaInfo, tenantID, documentProjectID string) (string, string, error) {
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] generateSQL called: naturalQuery=%q, tenantID=%s, documentProjectID=%s", naturalQuery, tenantID, documentProjectID)
	}

	projectID := s.bqClient.ProjectID()
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		datasetID = "h1horizon_documents"
	}

	// Build filter instructions for project/tenant filtering
	filterInstructions := ""
	if tenantID != "" || documentProjectID != "" {
		filterInstructions = "\n\nCRITICAL FILTERING REQUIREMENTS:\n"
		if tenantID != "" {
			filterInstructions += fmt.Sprintf("- ALWAYS filter by tenantId = '%s' when querying core_docs_metadata or documents_metadata tables\n", tenantID)
		}
		if documentProjectID != "" {
			filterInstructions += fmt.Sprintf("- ALWAYS filter by projectId = '%s' when querying core_docs_metadata table\n", documentProjectID)
			filterInstructions += "- When reading CSV/TSV files from core_docs_text_table, filter by matching uri with storagePath from core_docs_metadata WHERE projectId = '" + documentProjectID + "'\n"
		}
		filterInstructions += "- These filters MUST be included in WHERE clauses to ensure data isolation\n"
		filterInstructions += "- Example: WHERE tenantId = '" + tenantID + "' AND projectId = '" + documentProjectID + "'\n"
	}

	prompt := fmt.Sprintf(`You are a BigQuery SQL expert. Convert natural language to valid BigQuery SQL.

PROJECT: %s
DATASET: %s

SCHEMA:
%s

AVAILABLE DATA:
- core_docs_metadata: Document metadata (id, filename, contentType, storagePath, extractedText, parseMeta, createdAt, etc.)
- documents_metadata: RAG document metadata
- Files in GCS accessible via storagePath field
- BigQuery can read CSV, JSON, Parquet, Avro directly from GCS using EXTERNAL TABLE
%s
USER QUERY:
%s

GENERATE SQL:
- Use `+"`%s.%s.table_name`"+` format
- Filter by tenantId = '%s' AND projectId = '%s' when querying core_docs_metadata
- For file content: use extractedText OR create EXTERNAL TABLE with format options
- For ML/forecasting: use CREATE MODEL with model_type (ARIMA_PLUS for time series)
- IMPORTANT: For TIMESTAMP fields, use DATE_TRUNC(timestamp_field, MONTH) or EXTRACT(YEAR FROM timestamp_field), EXTRACT(MONTH FROM timestamp_field) - NOT TIMESTAMP_TRUNC with MONTH
- For DATE fields, use DATE_TRUNC(date_field, MONTH)
- If you need to create a model AND query it, use TWO statements separated by semicolon: CREATE MODEL ... AS SELECT ...; SELECT ... FROM ML.FORECAST(...);
- Return ONLY SQL, no markdown code blocks, no explanations, no trailing backticks
- Use BigQuery features flexibly

Return ONLY the SQL query text, nothing else. No markdown, no backticks.`,
		projectID, datasetID, schemaInfo, filterInstructions, naturalQuery, projectID, datasetID, tenantID, documentProjectID)

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Prompt prepared (length: %d chars), calling Gemini to generate SQL...", len(prompt))
	}

	// Generate SQL
	opts := gen_combined.Options{
		ModelOverride:     "gemini_quality",
		Temperature:       0.1,  // Low temperature for more accurate SQL
		MaxOutputTokens:   4096, // Increased to support complex queries with multiple CTEs and subqueries
		SystemInstruction: "You are a BigQuery SQL expert. Generate only valid SQL queries without any explanations or markdown formatting. Handle complex queries with multiple CTEs, JOINs, and subqueries. Always optimize for performance by using appropriate filters and limits.",
	}

	response, err := s.genaiClient.Generate(ctx, prompt, nil, opts)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] ERROR: Failed to generate SQL from Gemini: %v", err)
		}
		return "", "", fmt.Errorf("failed to generate SQL: %w", err)
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Received response from Gemini (length: %d chars)", len(response.Content))
	}

	sqlQuery := strings.TrimSpace(response.Content)

	// Check if response is empty
	if sqlQuery == "" {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] WARNING: Gemini returned empty SQL query")
		}
		return "", "", fmt.Errorf("generated SQL query is empty")
	}

	// Remove markdown code blocks if present
	sqlQuery = strings.TrimPrefix(sqlQuery, "```sql")
	sqlQuery = strings.TrimPrefix(sqlQuery, "```")
	sqlQuery = strings.TrimSpace(sqlQuery)

	// Remove trailing backticks (common AI mistake)
	sqlQuery = strings.TrimSuffix(sqlQuery, "`")
	sqlQuery = strings.TrimSuffix(sqlQuery, "```")
	sqlQuery = strings.TrimSpace(sqlQuery)

	// Validate SQL for common issues
	if strings.Contains(sqlQuery, "``") {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] WARNING: SQL contains empty backticks, attempting to fix...")
		}
		// Remove empty backticks
		sqlQuery = strings.ReplaceAll(sqlQuery, "``", "")
		sqlQuery = strings.TrimSpace(sqlQuery)
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] SQL query cleaned and ready (length: %d):\n%s", len(sqlQuery), sqlQuery)
	}

	// Generate query explanation
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Generating explanation for SQL query...")
	}
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
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Explanation generated: %s", explanation)
		}
	} else {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] WARNING: Failed to generate explanation: %v", err)
		}
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] generateSQL completed successfully")
	}

	return sqlQuery, explanation, nil
}

// executeSQLWithRetry executes SQL query with automatic regeneration on errors
func (s *BigQueryAIService) executeSQLWithRetry(ctx context.Context, sqlQuery, naturalQuery, schemaInfo string, maxResults int) ([]map[string]interface{}, int64, error, []string) {
	maxRetries := 2
	var lastErr error
	var errorHistory []string
	currentSQL := sqlQuery

	for attempt := 0; attempt <= maxRetries; attempt++ {
		results, rowCount, err := s.executeSQL(ctx, currentSQL, maxResults)
		if err == nil {
			if s.logger != nil && len(errorHistory) > 0 {
				s.logger.Printf("[BigQueryAI] Query succeeded after %d retries, error history: %v", len(errorHistory), errorHistory)
			}
			return results, rowCount, nil, errorHistory
		}

		lastErr = err
		errorMsg := err.Error()
		errorHistory = append(errorHistory, fmt.Sprintf("Attempt %d: %s", attempt+1, errorMsg))

		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Query failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
			if attempt > 0 {
				s.logger.Printf("[BigQueryAI] Error history so far: %v", errorHistory)
			}
		}

		if attempt < maxRetries {
			// Attempt to fix SQL based on error
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] Attempting to fix SQL based on error...")
			}

			// Pass all previous errors to help AI understand the pattern
			allErrors := strings.Join(errorHistory, "\n")
			fixedSQL, err := s.fixSQLQuery(ctx, currentSQL, allErrors, naturalQuery, schemaInfo)
			if err != nil {
				if s.logger != nil {
					s.logger.Printf("[BigQueryAI] Failed to fix SQL: %v", err)
				}
				break
			}

			currentSQL = fixedSQL
			if s.logger != nil {
				// Log first 500 chars of fixed SQL
				if len(currentSQL) > 500 {
					s.logger.Printf("[BigQueryAI] Regenerated SQL (length: %d):\nFirst 500 chars:\n%s", len(currentSQL), currentSQL[:500])
				} else {
					s.logger.Printf("[BigQueryAI] Regenerated SQL:\n%s", currentSQL)
				}
			}
		}
	}

	// Build comprehensive error message with history
	errorSummary := fmt.Sprintf("Query execution failed after %d attempts", maxRetries+1)
	if len(errorHistory) > 0 {
		errorSummary += fmt.Sprintf(". Error history:\n%s", strings.Join(errorHistory, "\n"))
	}
	errorSummary += fmt.Sprintf("\nLast error: %v", lastErr)

	return nil, 0, fmt.Errorf("%s", errorSummary), errorHistory
}

// fixSQLQuery attempts to fix SQL query based on error message
func (s *BigQueryAIService) fixSQLQuery(ctx context.Context, originalSQL, errorMsg, naturalQuery, schemaInfo string) (string, error) {
	projectID := s.bqClient.ProjectID()
	datasetID := s.bqClient.DatasetID()
	if datasetID == "" {
		datasetID = "h1horizon_documents"
	}

	prompt := fmt.Sprintf(`You are a BigQuery SQL expert. A SQL query failed with errors. Your task is to fix the query.

PROJECT: %s
DATASET: %s

ORIGINAL USER QUERY (in natural language):
%s

ORIGINAL SQL QUERY (that failed):
%s

ERROR MESSAGES (may include multiple attempts):
%s

DATABASE SCHEMA INFORMATION:
%s

AVAILABLE TABLES:
- documents_metadata: RAG documents metadata
- core_docs_metadata: CoreDocs metadata
- documents_object_table: Object table for RAG documents (GCS)
- core_docs_object_table: Object table for CoreDocs (GCS)
- core_docs_json_table: Object table for CoreDocs JSON files (GCS)

COMMON FIXES:
- TIMESTAMP fields: Use DATE_TRUNC(timestamp_field, MONTH) or EXTRACT(YEAR FROM timestamp_field), EXTRACT(MONTH FROM timestamp_field) - NOT TIMESTAMP_TRUNC with MONTH
- DATE fields: Use DATE_TRUNC(date_field, MONTH)
- Fix table/column names (use proper project.dataset.table format)
- Fix JOIN conditions
- Fix data type mismatches
- Add missing WHERE clauses or filters
- Fix syntax errors
- Handle NULL values properly

INSTRUCTIONS:
1. Analyze ALL error messages to understand the pattern
2. Fix the SQL query to resolve ALL errors
3. Ensure the fixed query still answers the original user query
4. Return ONLY the fixed SQL query, nothing else

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

// GenerateSQLOnly generates SQL from natural language query without executing it
func (s *BigQueryAIService) GenerateSQLOnly(ctx context.Context, req *QueryRequest) (*GenerateSQLResponse, error) {
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] GenerateSQLOnly called: query=%q, tenantID=%s, documentProjectID=%s",
			req.NaturalLanguageQuery, req.TenantID, req.DocumentProjectID)
	}

	if req.NaturalLanguageQuery == "" {
		return nil, fmt.Errorf("natural language query is required")
	}

	// Get database schema information
	schemaInfo, err := s.getSchemaInfo(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] WARNING: Failed to get schema info: %v, continuing without schema", err)
		}
		schemaInfo = "Schema information unavailable"
	} else {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Schema info retrieved successfully (length: %d chars)", len(schemaInfo))
		}
	}

	// Generate SQL query using Vertex AI
	sqlQuery, explanation, err := s.generateSQL(ctx, req.NaturalLanguageQuery, schemaInfo, req.TenantID, req.DocumentProjectID)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] ERROR: Failed to generate SQL: %v", err)
		}
		return nil, fmt.Errorf("failed to generate SQL: %w", err)
	}

	return &GenerateSQLResponse{
		SQL:         sqlQuery,
		Explanation: explanation,
	}, nil
}

// ExecuteSQLOnly executes SQL query and returns results
// Handles multiple SQL statements by executing them sequentially
func (s *BigQueryAIService) ExecuteSQLOnly(ctx context.Context, sqlQuery string, maxResults int) (*ExecuteSQLResponse, error) {
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] ExecuteSQLOnly called: sqlQuery length=%d, maxResults=%d", len(sqlQuery), maxResults)
	}

	if strings.TrimSpace(sqlQuery) == "" {
		return nil, fmt.Errorf("SQL query is required")
	}

	// Clean SQL: remove trailing backticks and other markdown artifacts
	sqlQuery = strings.TrimSpace(sqlQuery)
	sqlQuery = strings.TrimSuffix(sqlQuery, "`")
	sqlQuery = strings.TrimSuffix(sqlQuery, "```")
	sqlQuery = strings.TrimSpace(sqlQuery)

	// Split SQL into individual statements (separated by semicolons)
	statements := s.splitSQLStatements(sqlQuery)

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Found %d SQL statements to execute", len(statements))
	}

	// Execute statements sequentially
	// For CREATE MODEL statements, we execute them but don't return results
	// For SELECT statements, we return results
	var lastResults []map[string]interface{}
	var lastRowCount int64

	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}

		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] Executing statement %d/%d (length: %d)", i+1, len(statements), len(stmt))
		}

		stmtUpper := strings.TrimSpace(strings.ToUpper(stmt))
		isCreateModel := strings.HasPrefix(stmtUpper, "CREATE") && strings.Contains(stmtUpper, "MODEL")

		results, rowCount, err := s.executeSQL(ctx, stmt, maxResults)
		if err != nil {
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] ERROR: Failed to execute statement %d: %v", i+1, err)
			}
			return &ExecuteSQLResponse{
				Results:  []map[string]interface{}{},
				RowCount: 0,
				Error:    fmt.Sprintf("Failed to execute statement %d/%d: %v", i+1, len(statements), err),
			}, nil
		}

		// For CREATE MODEL, continue to next statement
		// For SELECT/FORECAST, save results
		if !isCreateModel {
			lastResults = results
			lastRowCount = rowCount
		} else {
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] CREATE MODEL statement executed successfully, continuing to next statement")
			}
		}
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] All SQL statements executed successfully: rowCount=%d, resultsCount=%d", lastRowCount, len(lastResults))
	}

	return &ExecuteSQLResponse{
		Results:  lastResults,
		RowCount: lastRowCount,
	}, nil
}

// splitSQLStatements splits SQL into individual statements
// Handles semicolons that might be inside strings or comments
func (s *BigQueryAIService) splitSQLStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	inString := false
	stringChar := byte(0)
	escapeNext := false

	for i := 0; i < len(sql); i++ {
		char := sql[i]

		if escapeNext {
			current.WriteByte(char)
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			current.WriteByte(char)
			continue
		}

		if !inString {
			switch char {
			case '\'', '"', '`':
				inString = true
				stringChar = char
				current.WriteByte(char)
			case ';':
				stmt := strings.TrimSpace(current.String())
				if stmt != "" {
					statements = append(statements, stmt)
				}
				current.Reset()
			default:
				current.WriteByte(char)
			}
		} else {
			current.WriteByte(char)
			if char == stringChar {
				inString = false
				stringChar = 0
			}
		}
	}

	// Add remaining statement
	stmt := strings.TrimSpace(current.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}

	return statements
}

// executeSQL executes SQL query and returns results
// Supports both SELECT queries and CREATE TABLE AS SELECT
func (s *BigQueryAIService) executeSQL(ctx context.Context, sqlQuery string, maxResults int) ([]map[string]interface{}, int64, error) {
	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] executeSQL called: maxResults=%d", maxResults)
		s.logger.Printf("[BigQueryAI] SQL query to execute (length: %d chars):\n%s", len(sqlQuery), sqlQuery)
	}

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

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Executing query via BigQuery client...")
	}
	iter, err := s.bqClient.ExecuteQuery(ctx, sqlQuery)
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[BigQueryAI] ERROR: Query execution failed: %v", err)
		}
		return nil, 0, fmt.Errorf("query execution failed: %w", err)
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] Query executed successfully, reading results (maxResults=%d)...", maxResults)
	}

	var results []map[string]interface{}
	var rowCount int64

	for {
		var row map[string]bigquery.Value
		err := iter.Next(&row)
		if err == iterator.Done {
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] Finished reading results: totalRows=%d, returnedRows=%d", rowCount, len(results))
			}
			break
		}
		if err != nil {
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] ERROR: Failed to read row: %v", err)
			}
			return nil, 0, fmt.Errorf("failed to read row: %w", err)
		}

		if len(results) >= maxResults {
			if s.logger != nil {
				s.logger.Printf("[BigQueryAI] Reached maxResults limit (%d), stopping result reading", maxResults)
			}
			break
		}

		// Convert bigquery.Value to standard Go types
		resultRow := make(map[string]interface{})
		for k, v := range row {
			resultRow[k] = convertBigQueryValue(v)
		}
		results = append(results, resultRow)
		rowCount++

		if rowCount%100 == 0 && s.logger != nil {
			s.logger.Printf("[BigQueryAI] Processed %d rows so far...", rowCount)
		}
	}

	if s.logger != nil {
		s.logger.Printf("[BigQueryAI] executeSQL completed: rowCount=%d, resultsCount=%d", rowCount, len(results))
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
