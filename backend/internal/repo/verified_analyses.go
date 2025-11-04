package repo

import (
	"context"
	"errors"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

var ErrCodeNotFound = errors.New("verified analysis code not found")

type VerifiedAnalysesRepository struct {
	col *firestore.CollectionRef
}

func NewVerifiedAnalysesRepository(db *firestore.Client) *VerifiedAnalysesRepository {
	return &VerifiedAnalysesRepository{col: db.Collection("core_verified_analyses")}
}

func (r *VerifiedAnalysesRepository) Create(ctx context.Context, va *models.VerifiedAnalysis) (string, error) {
	if va == nil {
		return "", ErrInvalidDocument
	}
	now := time.Now().UTC()
	va.CreatedAt = now
	va.UpdatedAt = now
	doc := r.col.NewDoc()
	va.ID = doc.ID
	_, err := doc.Set(ctx, va)
	if err != nil {
		return "", err
	}
	return va.ID, nil
}

func (r *VerifiedAnalysesRepository) GetByCode(ctx context.Context, code string) (*models.VerifiedAnalysis, error) {
	if code == "" {
		return nil, ErrCodeNotFound
	}
	q := r.col.Where("code", "==", code).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, ErrCodeNotFound
	}
	var va models.VerifiedAnalysis
	if err := docs[0].DataTo(&va); err != nil {
		return nil, err
	}
	return &va, nil
}

func (r *VerifiedAnalysesRepository) GetByCanonicalURL(ctx context.Context, canonicalURL string) (*models.VerifiedAnalysis, error) {
	if canonicalURL == "" {
		return nil, ErrCodeNotFound
	}
	q := r.col.Where("canonicalUrl", "==", canonicalURL).Where("status", "==", "completed").OrderBy("createdAt", firestore.Desc).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil || len(docs) == 0 {
		return nil, ErrCodeNotFound
	}
	var va models.VerifiedAnalysis
	if err := docs[0].DataTo(&va); err != nil {
		return nil, err
	}
	return &va, nil
}

func (r *VerifiedAnalysesRepository) Update(ctx context.Context, va *models.VerifiedAnalysis) error {
	if va == nil || va.ID == "" {
		return ErrInvalidDocument
	}
	va.UpdatedAt = time.Now().UTC()
	// Use Set without MergeAll - we're updating the entire document
	_, err := r.col.Doc(va.ID).Set(ctx, va)
	return err
}

func (r *VerifiedAnalysesRepository) CodeExists(ctx context.Context, code string) (bool, error) {
	if code == "" {
		return false, nil
	}
	q := r.col.Where("code", "==", code).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return false, err
	}
	return len(docs) > 0, nil
}


