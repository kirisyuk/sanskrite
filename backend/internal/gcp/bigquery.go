package gcp

import (
	"context"
	"strings"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/option"
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
	return &BigQueryClient{
		client:    client,
		projectID: projectID,
		datasetID: datasetID,
	}, nil
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
