package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

// ColumnSchema represents a column definition in query results
type ColumnSchema struct {
	Name string `json:"name" firestore:"name"`
	Type string `json:"type" firestore:"type"`
}

// QueryExecutionResult represents the result of executing a single SQL query
type QueryExecutionResult struct {
	QueryIndex      int                    `json:"queryIndex" firestore:"queryIndex"`
	SQL             string                 `json:"sql" firestore:"sql"`
	Explanation     *string                `json:"explanation,omitempty" firestore:"explanation,omitempty"`
	ExecutionTimeMs *int64                 `json:"executionTimeMs,omitempty" firestore:"executionTimeMs,omitempty"`
	Schema          []ColumnSchema         `json:"schema" firestore:"schema"`
	Rows            []map[string]interface{} `json:"rows" firestore:"rows"`
	RowCount        int                    `json:"rowCount" firestore:"rowCount"`
	Error           *string                `json:"error,omitempty" firestore:"error,omitempty"`
	ErrorHistory    []string               `json:"errorHistory,omitempty" firestore:"errorHistory,omitempty"`
}

// ExecutionStage represents a stage in query execution progress
type ExecutionStage struct {
	Name        string  `json:"name" firestore:"name"`
	Status      string  `json:"status" firestore:"status"` // pending|running|completed|error
	StartTime   *int64  `json:"startTime,omitempty" firestore:"startTime,omitempty"`
	EndTime     *int64  `json:"endTime,omitempty" firestore:"endTime,omitempty"`
	DurationMs  *int64  `json:"durationMs,omitempty" firestore:"durationMs,omitempty"`
	Error       *string `json:"error,omitempty" firestore:"error,omitempty"`
}

// DatabaseSchema represents database schema information
type DatabaseSchema struct {
	ProjectID      string `json:"projectID" firestore:"projectID"`
	DatasetID      string `json:"datasetID" firestore:"datasetID"`
	SchemaInfo     string `json:"schemaInfo" firestore:"schemaInfo"`
	TenantID       string `json:"tenantID" firestore:"tenantID"`
	ProjectIDFilter string `json:"projectIDFilter" firestore:"projectIDFilter"`
}

// QueryInterpretation represents the interpretation of user query into SQL queries
type QueryInterpretation struct {
	Queries    []QueryInterpretationItem `json:"queries" firestore:"queries"`
	ExecutedAt time.Time                 `json:"executedAt" firestore:"executedAt"`
}

// QueryInterpretationItem represents a single query in interpretation
type QueryInterpretationItem struct {
	SQL         string `json:"sql" firestore:"sql"`
	Description string `json:"description" firestore:"description"`
	Execute     bool   `json:"execute" firestore:"execute"`
}

// ScenarioData represents scenario query data stored in Firestore
type ScenarioData struct {
	ID              string                 `json:"id" firestore:"id"`
	TenantID        string                 `json:"tenantId" firestore:"tenantId"`
	ProjectID       string                 `json:"projectId" firestore:"projectId"`
	ScenarioID      string                 `json:"scenarioId" firestore:"scenarioId"`
	UserQuery       string                 `json:"userQuery" firestore:"userQuery"`
	DatabaseSchema  *DatabaseSchema        `json:"databaseSchema,omitempty" firestore:"databaseSchema,omitempty"`
	ExecutionStages []ExecutionStage       `json:"executionStages,omitempty" firestore:"executionStages,omitempty"`
	Interpretation  QueryInterpretation    `json:"interpretation" firestore:"interpretation"`
	Results         []QueryExecutionResult `json:"results" firestore:"results"`
	IsComposite     bool                   `json:"isComposite" firestore:"isComposite"`
	CreatedAt       time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt       time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

type ScenarioDataRepository struct{ col *firestore.CollectionRef }

func NewScenarioDataRepository(db *firestore.Client) *ScenarioDataRepository {
	return &ScenarioDataRepository{col: db.Collection("core_scenario_data")}
}

// Create creates a new scenario data document
func (r *ScenarioDataRepository) Create(ctx context.Context, sd *ScenarioData) (string, error) {
	doc := r.col.NewDoc()
	sd.ID = doc.ID
	now := time.Now().UTC()
	sd.CreatedAt = now
	sd.UpdatedAt = now
	_, err := doc.Set(ctx, sd)
	if err != nil {
		return "", err
	}
	return doc.ID, nil
}

// Get returns a scenario data document by id
func (r *ScenarioDataRepository) Get(ctx context.Context, id string) (*ScenarioData, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	var sd ScenarioData
	if err := snap.DataTo(&sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

// Update applies a partial update to a scenario data document by id using merge semantics
func (r *ScenarioDataRepository) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	if id == "" {
		return ErrInvalidDocument
	}
	if len(updates) == 0 {
		return nil
	}
	// Prevent overwriting immutable fields
	delete(updates, "id")
	delete(updates, "tenantId")
	delete(updates, "projectId")
	delete(updates, "scenarioId")
	delete(updates, "createdAt")
	
	// Always update UpdatedAt
	updates["updatedAt"] = time.Now().UTC()
	
	_, err := r.col.Doc(id).Set(ctx, updates, firestore.MergeAll)
	return err
}

// DeleteByID deletes a scenario data document by id
func (r *ScenarioDataRepository) DeleteByID(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalidDocument
	}
	_, err := r.col.Doc(id).Delete(ctx)
	return err
}

// ListByProjectAndScenario returns scenario data for a project and scenario ordered by createdAt asc
func (r *ScenarioDataRepository) ListByProjectAndScenario(ctx context.Context, projectID, scenarioID string) ([]*ScenarioData, error) {
	q := r.col.Where("projectId", "==", projectID).Where("scenarioId", "==", scenarioID).OrderBy("createdAt", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var res []*ScenarioData
	for _, d := range docs {
		var sd ScenarioData
		if err := d.DataTo(&sd); err != nil {
			continue
		}
		res = append(res, &sd)
	}
	return res, nil
}

// ListByProject returns all scenario data for a project ordered by createdAt asc
func (r *ScenarioDataRepository) ListByProject(ctx context.Context, projectID string) ([]*ScenarioData, error) {
	q := r.col.Where("projectId", "==", projectID).OrderBy("createdAt", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	var res []*ScenarioData
	for _, d := range docs {
		var sd ScenarioData
		if err := d.DataTo(&sd); err != nil {
			continue
		}
		res = append(res, &sd)
	}
	return res, nil
}

// DeleteByProject removes all scenario data for a given project
func (r *ScenarioDataRepository) DeleteByProject(ctx context.Context, projectID string) error {
	docs, err := r.col.Where("projectId", "==", projectID).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
	}
	return nil
}

