package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/option"
	"google.golang.org/api/googleapi"
)

// BigQueryClient wraps BigQuery client
type BigQueryClient struct {
	client    *bigquery.Client
	projectID string
	datasetID string
}

// NewBigQueryClient creates a new BigQuery client
func NewBigQueryClient(ctx context.Context, projectID, datasetID string, opts ...option.ClientOption) (*BigQueryClient, error) {
	client, err := bigquery.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, err
	}
	bq := &BigQueryClient{
		client:    client,
		projectID: projectID,
		datasetID: datasetID,
	}
	if err := bq.ValidateDataset(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return bq, nil
}

// Close closes the client
func (b *BigQueryClient) Close() error {
	return b.client.Close()
}

// Client returns the underlying BigQuery client
func (b *BigQueryClient) Client() *bigquery.Client {
	return b.client
}

// ProjectID returns the project ID
func (b *BigQueryClient) ProjectID() string {
	return b.projectID
}

// DatasetID returns the dataset ID
func (b *BigQueryClient) DatasetID() string {
	return b.datasetID
}

// ValidateDataset ensures the configured dataset exists and is accessible.
func (b *BigQueryClient) ValidateDataset(ctx context.Context) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("bigquery client is not initialized")
	}
	_, err := b.client.Dataset(b.datasetID).Metadata(ctx)
	if err == nil {
		return nil
	}
	if isBigQueryNotFoundError(err) {
		return fmt.Errorf("bigquery dataset %s.%s not found", b.projectID, b.datasetID)
	}
	return fmt.Errorf("bigquery dataset %s.%s is not accessible: %w", b.projectID, b.datasetID, err)
}

// ExecuteQuery executes SQL query and returns results
func (b *BigQueryClient) ExecuteQuery(ctx context.Context, query string) (*bigquery.RowIterator, error) {
	q := b.client.Query(query)
	q.Location = "US" // Set location for queries
	return q.Read(ctx)
}

// GetTableSchema returns table schema
func (b *BigQueryClient) GetTableSchema(ctx context.Context, tableName string) (*bigquery.Schema, error) {
	table := b.client.Dataset(b.datasetID).Table(tableName)
	meta, err := table.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	return &meta.Schema, nil
}

// ListTables returns list of tables in dataset
func (b *BigQueryClient) ListTables(ctx context.Context) ([]string, error) {
	dataset := b.client.Dataset(b.datasetID)
	it := dataset.Tables(ctx)
	var tables []string
	for {
		table, err := it.Next()
		if err != nil {
			break
		}
		tables = append(tables, table.TableID)
	}
	return tables, nil
}

// CreateDataset creates dataset if it doesn't exist
func (b *BigQueryClient) CreateDataset(ctx context.Context) error {
	dataset := b.client.Dataset(b.datasetID)
	meta := &bigquery.DatasetMetadata{
		Location: "US",
	}
	return dataset.Create(ctx, meta)
}

// CreateTableIfNotExists creates table if it doesn't exist
func (b *BigQueryClient) CreateTableIfNotExists(ctx context.Context, tableName string, schema bigquery.Schema) error {
	table := b.client.Dataset(b.datasetID).Table(tableName)
	meta := &bigquery.TableMetadata{
		Schema: schema,
	}
	err := table.Create(ctx, meta)
	// Ignore error if table already exists
	if err != nil && !isAlreadyExistsError(err) {
		return err
	}
	return nil
}

// isAlreadyExistsError checks if error is "already exists"
func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "already exists") ||
		strings.Contains(errStr, "Already Exists") ||
		strings.Contains(errStr, "409")
}

func isBigQueryNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == 404 {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not found") || strings.Contains(errStr, "404")
}

// InsertRow inserts a single row into a BigQuery table
func (b *BigQueryClient) InsertRow(ctx context.Context, tableName string, row map[string]interface{}) error {
	return b.InsertRows(ctx, tableName, []map[string]interface{}{row})
}

// mapSaver implements bigquery.ValueSaver for map payloads
type mapSaver map[string]interface{}

func (m mapSaver) Save() (map[string]bigquery.Value, string, error) {
	values := make(map[string]bigquery.Value, len(m))
	for k, v := range m {
		values[k] = v
	}
	return values, "", nil
}

// InsertRows inserts multiple rows into a BigQuery table
func (b *BigQueryClient) InsertRows(ctx context.Context, tableName string, rows []map[string]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	inserter := b.client.Dataset(b.datasetID).Table(tableName).Inserter()
	values := make([]bigquery.ValueSaver, len(rows))
	for i, row := range rows {
		values[i] = mapSaver(row)
	}
	return inserter.Put(ctx, values)
}

// DocumentExistsInTable checks if a document exists in a BigQuery table by ID
func (b *BigQueryClient) DocumentExistsInTable(ctx context.Context, tableName string, docID string) (bool, error) {
	query := fmt.Sprintf(
		"SELECT COUNT(*) as cnt FROM `%s.%s.%s` WHERE id = @docID LIMIT 1",
		b.projectID, b.datasetID, tableName,
	)

	q := b.client.Query(query)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "docID", Value: docID},
	}
	q.Location = "US"

	it, err := q.Read(ctx)
	if err != nil {
		return false, err
	}

	var result struct {
		Cnt int64 `bigquery:"cnt"`
	}

	if err := it.Next(&result); err != nil {
		// If iterator is done (no rows), document doesn't exist
		return false, nil
	}

	return result.Cnt > 0, nil
}

// UpsertRow inserts or updates a row in BigQuery table using MERGE statement
func (b *BigQueryClient) UpsertRow(ctx context.Context, tableName string, row map[string]interface{}) error {
	if len(row) == 0 {
		return nil
	}

	// Convert values to proper types for BigQuery parameters
	convertValue := func(v interface{}) interface{} {
		if v == nil {
			return nil
		}
		// Handle pointers
		switch val := v.(type) {
		case *int:
			if val == nil {
				return nil
			}
			return *val
		case *int64:
			if val == nil {
				return nil
			}
			return *val
		case *string:
			if val == nil {
				return nil
			}
			return *val
		case map[string]interface{}:
			// Convert map to JSON string for BigQuery
			jsonBytes, err := json.Marshal(val)
			if err != nil {
				return nil
			}
			return string(jsonBytes)
		default:
			return v
		}
	}

	// Build MERGE statement
	// For core_docs_metadata, the primary key is 'id'
	query := fmt.Sprintf(`
		MERGE `+"`%s.%s.%s`"+` AS target
		USING (SELECT @id AS id) AS source
		ON target.id = source.id
		WHEN MATCHED THEN
			UPDATE SET
				tenantId = @tenantId,
				projectId = @projectId,
				filename = @filename,
				contentType = @contentType,
				description = @description,
				sizeBytes = @sizeBytes,
				storagePath = @storagePath,
				extractedText = @extractedText,
				parseStatus = @parseStatus,
				parseMeta = PARSE_JSON(@parseMeta),
				pageCount = @pageCount,
				docType = @docType,
				createdBy = @createdBy,
				updatedAt = @updatedAt
		WHEN NOT MATCHED THEN
			INSERT (id, tenantId, projectId, filename, contentType, description, sizeBytes, storagePath, extractedText, parseStatus, parseMeta, pageCount, docType, createdBy, createdAt, updatedAt)
			VALUES (@id, @tenantId, @projectId, @filename, @contentType, @description, @sizeBytes, @storagePath, @extractedText, @parseStatus, PARSE_JSON(@parseMeta), @pageCount, @docType, @createdBy, @createdAt, @updatedAt)
	`, b.projectID, b.datasetID, tableName)

	q := b.client.Query(query)

	// Build parameters from row with proper type conversion
	// Only include non-nil parameters (BigQuery doesn't accept nil parameters)
	params := []bigquery.QueryParameter{}
	paramMap := make(map[string]bool) // Track which parameters we're including
	for k, v := range row {
		converted := convertValue(v)
		if converted != nil {
			params = append(params, bigquery.QueryParameter{
				Name:  k,
				Value: converted,
			})
			paramMap[k] = true
		}
	}

	// Build dynamic MERGE query that only updates fields that are provided
	// For fields not in paramMap, use COALESCE to keep existing value or set to NULL
	updateFields := []string{}
	insertFields := []string{}
	insertValues := []string{}

	// Required fields
	requiredFields := []string{"id", "tenantId", "projectId", "filename", "contentType", "createdAt", "updatedAt"}
	for _, field := range requiredFields {
		if paramMap[field] {
			insertFields = append(insertFields, field)
			insertValues = append(insertValues, fmt.Sprintf("@%s", field))
		}
	}

	// Optional fields
	optionalFields := []string{"description", "sizeBytes", "storagePath", "extractedText", "parseStatus", "parseMeta", "pageCount", "docType", "createdBy"}
	for _, field := range optionalFields {
		if paramMap[field] {
			insertFields = append(insertFields, field)
			if field == "parseMeta" {
				insertValues = append(insertValues, "PARSE_JSON(@parseMeta)")
			} else {
				insertValues = append(insertValues, fmt.Sprintf("@%s", field))
			}
			if field == "parseMeta" {
				updateFields = append(updateFields, fmt.Sprintf("%s = PARSE_JSON(@parseMeta)", field))
			} else {
				updateFields = append(updateFields, fmt.Sprintf("%s = @%s", field, field))
			}
		} else {
			// For optional fields not provided, set to NULL on insert, keep existing on update
			insertFields = append(insertFields, field)
			if field == "parseMeta" {
				insertValues = append(insertValues, "NULL")
			} else {
				insertValues = append(insertValues, "NULL")
			}
		}
	}

	query = fmt.Sprintf(`
		MERGE `+"`%s.%s.%s`"+` AS target
		USING (SELECT @id AS id) AS source
		ON target.id = source.id
		WHEN MATCHED THEN
			UPDATE SET
				%s
		WHEN NOT MATCHED THEN
			INSERT (%s)
			VALUES (%s)
	`, b.projectID, b.datasetID, tableName,
		strings.Join(updateFields, ",\n\t\t\t"),
		strings.Join(insertFields, ", "),
		strings.Join(insertValues, ", "))

	q = b.client.Query(query)
	q.Parameters = params
	q.Location = "US"

	it, err := q.Read(ctx)
	if err != nil {
		return fmt.Errorf("UpsertRow MERGE query failed for table %s: %w", tableName, err)
	}
	// Consume iterator to ensure query completes
	for {
		var row map[string]interface{}
		if err := it.Next(&row); err != nil {
			break
		}
	}
	return nil
}
