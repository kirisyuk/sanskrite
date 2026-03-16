package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type ScopeAgentRunsRepository struct {
	col *firestore.CollectionRef
}

func NewScopeAgentRunsRepository(db *firestore.Client) *ScopeAgentRunsRepository {
	return &ScopeAgentRunsRepository{col: db.Collection("horizon_scope_agent_runs")}
}

func (r *ScopeAgentRunsRepository) Create(ctx context.Context, run *models.ScopeAgentRun) (string, error) {
	if r == nil || r.col == nil || run == nil {
		return "", ErrInvalidDocument
	}
	if err := run.Validate(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	doc := r.col.NewDoc()
	run.ID = doc.ID
	_, err := doc.Set(ctx, run)
	return run.ID, err
}

func (r *ScopeAgentRunsRepository) Update(ctx context.Context, run *models.ScopeAgentRun) error {
	if r == nil || r.col == nil || run == nil || strings.TrimSpace(run.ID) == "" {
		return ErrInvalidDocument
	}
	if err := run.Validate(); err != nil {
		return err
	}
	run.UpdatedAt = time.Now().UTC()
	_, err := r.col.Doc(strings.TrimSpace(run.ID)).Set(ctx, run)
	return err
}

func (r *ScopeAgentRunsRepository) Get(ctx context.Context, id string) (*models.ScopeAgentRun, error) {
	if r == nil || r.col == nil || strings.TrimSpace(id) == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(strings.TrimSpace(id)).Get(ctx)
	if err != nil {
		return nil, err
	}
	var run models.ScopeAgentRun
	if err := snap.DataTo(&run); err != nil {
		return nil, err
	}
	if run.ID == "" {
		run.ID = snap.Ref.ID
	}
	return &run, nil
}

func (r *ScopeAgentRunsRepository) ListPending(ctx context.Context, tenantID string, limit int) ([]*models.ScopeAgentRun, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).Where("status", "==", string(models.ScopeAgentRunStatusPending)).OrderBy("createdAt", firestore.Asc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentRun, 0, len(docs))
	for _, doc := range docs {
		var run models.ScopeAgentRun
		if err := doc.DataTo(&run); err != nil {
			continue
		}
		if run.ID == "" {
			run.ID = doc.Ref.ID
		}
		out = append(out, &run)
	}
	return out, nil
}

func (r *ScopeAgentRunsRepository) ListByConfigID(ctx context.Context, configID string, limit int) ([]*models.ScopeAgentRun, error) {
	if r == nil || r.col == nil || strings.TrimSpace(configID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	docs, err := r.col.Where("configId", "==", strings.TrimSpace(configID)).OrderBy("createdAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentRun, 0, len(docs))
	for _, doc := range docs {
		var run models.ScopeAgentRun
		if err := doc.DataTo(&run); err != nil {
			continue
		}
		if run.ID == "" {
			run.ID = doc.Ref.ID
		}
		out = append(out, &run)
	}
	return out, nil
}

func (r *ScopeAgentRunsRepository) ListByTenant(ctx context.Context, tenantID string, limit int) ([]*models.ScopeAgentRun, error) {
	if r == nil || r.col == nil || strings.TrimSpace(tenantID) == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}
	docs, err := r.col.Where("tenantId", "==", strings.TrimSpace(tenantID)).OrderBy("createdAt", firestore.Desc).Limit(limit).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScopeAgentRun, 0, len(docs))
	for _, doc := range docs {
		var run models.ScopeAgentRun
		if err := doc.DataTo(&run); err != nil {
			continue
		}
		if run.ID == "" {
			run.ID = doc.Ref.ID
		}
		out = append(out, &run)
	}
	return out, nil
}
