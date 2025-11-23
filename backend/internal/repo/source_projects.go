package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type SourceProjectsRepository struct{ col *firestore.CollectionRef }

func NewSourceProjectsRepository(db *firestore.Client) *SourceProjectsRepository {
	return &SourceProjectsRepository{col: db.Collection("rag_source_projects")}
}

func (r *SourceProjectsRepository) Upsert(ctx context.Context, sp *models.SourceProject) error {
	if sp == nil || sp.ID == "" || sp.SourceID == "" || sp.ProjectID == "" || sp.TenantID == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if sp.CreatedAt.IsZero() {
		sp.CreatedAt = now
	}
	sp.UpdatedAt = now
	_, err := r.col.Doc(sp.ID).Set(ctx, sp)
	return err
}

func (r *SourceProjectsRepository) Create(ctx context.Context, sp *models.SourceProject) (string, error) {
	if sp == nil || sp.SourceID == "" || sp.ProjectID == "" || sp.TenantID == "" {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	sp.CreatedAt = now
	sp.UpdatedAt = now
	doc := r.col.NewDoc()
	sp.ID = doc.ID
	if _, err := doc.Set(ctx, sp); err != nil {
		return "", err
	}
	return doc.ID, nil
}

func (r *SourceProjectsRepository) ListByProject(ctx context.Context, tenantID, projectID string, status *string) ([]*models.SourceProject, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID)
	if status != nil && *status != "" {
		q = q.Where("status", "==", *status)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.SourceProject, 0, len(docs))
	for _, d := range docs {
		var sp models.SourceProject
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		res = append(res, &sp)
	}
	return res, nil
}

func (r *SourceProjectsRepository) ListBySource(ctx context.Context, tenantID, sourceID string, status *string) ([]*models.SourceProject, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("sourceId", "==", sourceID)
	if status != nil && *status != "" {
		q = q.Where("status", "==", *status)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*models.SourceProject, 0, len(docs))
	for _, d := range docs {
		var sp models.SourceProject
		if err := d.DataTo(&sp); err != nil {
			continue
		}
		res = append(res, &sp)
	}
	return res, nil
}

func (r *SourceProjectsRepository) GetByProjectAndSource(ctx context.Context, tenantID, projectID, sourceID string) (*models.SourceProject, error) {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID).Where("sourceId", "==", sourceID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil // Not found
	}
	var sp models.SourceProject
	if err := docs[0].DataTo(&sp); err != nil {
		return nil, err
	}
	return &sp, nil
}

func (r *SourceProjectsRepository) DeleteByProjectAndSource(ctx context.Context, tenantID, projectID, sourceID string) error {
	q := r.col.Where("tenantId", "==", tenantID).Where("projectId", "==", projectID).Where("sourceId", "==", sourceID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
	}
	return nil
}

// CountBySource counts all project-source links for a given source (across all tenants)
// Used to determine if a source should be deleted when last link is removed
func (r *SourceProjectsRepository) CountBySource(ctx context.Context, sourceID string) (int, error) {
	q := r.col.Where("sourceId", "==", sourceID)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}
