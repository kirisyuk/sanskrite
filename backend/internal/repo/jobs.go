package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type JobsRepository struct{ col *firestore.CollectionRef }

func NewJobsRepository(db *firestore.Client) *JobsRepository {
	return &JobsRepository{col: db.Collection("rag_jobs")}
}

func (r *JobsRepository) Upsert(ctx context.Context, j *models.Job) error {
	if j == nil || j.ID == "" || j.TenantID == "" || j.Type == "" {
		return ErrInvalidDocument
	}
	now := time.Now().UTC()
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
	}
	j.UpdatedAt = now
	_, err := r.col.Doc(j.ID).Set(ctx, j)
	return err
}

func (r *JobsRepository) Create(ctx context.Context, j *models.Job) (string, error) {
	if j == nil || j.TenantID == "" || j.Type == "" {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	doc := r.col.NewDoc()
	j.ID = doc.ID
	if _, err := doc.Set(ctx, j); err != nil {
		return "", err
	}
	return doc.ID, nil
}
