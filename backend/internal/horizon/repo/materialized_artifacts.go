package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrMaterializedArtifactNotFound = errors.New("materialized artifact not found")

// MaterializedArtifactsRepository репозиторий для работы с MaterializedArtifact
type MaterializedArtifactsRepository struct {
	col *firestore.CollectionRef
	db  *firestore.Client
}

// NewMaterializedArtifactsRepository создает новый репозиторий
func NewMaterializedArtifactsRepository(db *firestore.Client) *MaterializedArtifactsRepository {
	return &MaterializedArtifactsRepository{
		col: db.Collection("horizon_materialized_artifacts"),
		db:  db,
	}
}

// Create создает новый MaterializedArtifact
func (r *MaterializedArtifactsRepository) Create(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) (string, error) {
	if artifact == nil {
		return "", fmt.Errorf("artifact is nil")
	}

	if err := artifact.Validate(); err != nil {
		return "", fmt.Errorf("validation failed: %w", err)
	}

	now := time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	artifact.UpdatedAt = now
	if artifact.Status == "" {
		artifact.Status = "active"
	}
	if artifact.CreatedBy == "" {
		artifact.CreatedBy = "system"
	}
	if artifact.UpdatedBy == "" {
		artifact.UpdatedBy = "system"
	}

	doc := r.col.NewDoc()
	artifact.ID = doc.ID

	if _, err := doc.Set(ctx, artifact); err != nil {
		return "", fmt.Errorf("firestore set failed: %w", err)
	}

	return artifact.ID, nil
}

// Get получает MaterializedArtifact по ID
func (r *MaterializedArtifactsRepository) Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error) {
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") {
			return nil, ErrMaterializedArtifactNotFound
		}
		return nil, fmt.Errorf("firestore get failed: %w", err)
	}

	var artifact horizonmodels.MaterializedArtifact
	if err := snap.DataTo(&artifact); err != nil {
		return nil, fmt.Errorf("data unmarshal failed: %w", err)
	}

	return &artifact, nil
}

// GetByCanonicalKey получает MaterializedArtifact по TenantID + Kind + CanonicalKey.
// This is the primary idempotency key for overlays.
func (r *MaterializedArtifactsRepository) GetByCanonicalKey(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, canonicalKey string) (*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if !kind.Valid() {
		return nil, fmt.Errorf("kind is required")
	}
	if canonicalKey == "" {
		return nil, fmt.Errorf("canonicalKey is required")
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("kind", "==", string(kind)).
		Where("canonicalKey", "==", canonicalKey).
		Limit(1)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	if len(docs) == 0 {
		return nil, ErrMaterializedArtifactNotFound
	}

	var artifact horizonmodels.MaterializedArtifact
	if err := docs[0].DataTo(&artifact); err != nil {
		return nil, fmt.Errorf("data unmarshal failed: %w", err)
	}

	return &artifact, nil
}

// Update обновляет существующий MaterializedArtifact
func (r *MaterializedArtifactsRepository) Update(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.ID == "" {
		return fmt.Errorf("id is required")
	}

	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	artifact.UpdatedAt = time.Now().UTC()
	if artifact.UpdatedBy == "" {
		artifact.UpdatedBy = "system"
	}

	if _, err := r.col.Doc(artifact.ID).Set(ctx, artifact); err != nil {
		return fmt.Errorf("firestore set failed: %w", err)
	}

	return nil
}

// Upsert создает или обновляет MaterializedArtifact по CanonicalKey
func (r *MaterializedArtifactsRepository) Upsert(ctx context.Context, artifact *horizonmodels.MaterializedArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact is nil")
	}
	if artifact.CanonicalKey == "" {
		return fmt.Errorf("canonicalKey is required for upsert")
	}
	if artifact.TenantID == "" {
		return fmt.Errorf("tenantID is required")
	}
	if !artifact.Kind.Valid() {
		return fmt.Errorf("kind is required")
	}

	existing, err := r.GetByCanonicalKey(ctx, artifact.TenantID, artifact.Kind, artifact.CanonicalKey)
	if err != nil && err != ErrMaterializedArtifactNotFound {
		return fmt.Errorf("check existing failed: %w", err)
	}

	if existing != nil {
		// Update existing
		artifact.ID = existing.ID
		artifact.CreatedAt = existing.CreatedAt
		artifact.CreatedBy = existing.CreatedBy
		return r.Update(ctx, artifact)
	}

	// Create new
	_, err = r.Create(ctx, artifact)
	return err
}

// ListByTenantAndKind получает список MaterializedArtifact по TenantID и Kind
func (r *MaterializedArtifactsRepository) ListByTenantAndKind(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if limit <= 0 {
		limit = 100
	}

	q := r.col.
		Where("tenantId", "==", tenantID).
		Where("kind", "==", string(kind)).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue // Skip invalid docs
		}
		artifacts = append(artifacts, &artifact)
	}

	return artifacts, nil
}

// ListByLens получает список MaterializedArtifact по LensID
func (r *MaterializedArtifactsRepository) ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if lensID == "" {
		return nil, fmt.Errorf("lensID is required")
	}
	if limit <= 0 {
		limit = 100
	}

	q := r.col.
		Where("lensId", "==", lensID).
		OrderBy("updatedAt", firestore.Desc).
		Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("firestore query failed: %w", err)
	}

	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(docs))
	for _, doc := range docs {
		var artifact horizonmodels.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			continue
		}
		artifacts = append(artifacts, &artifact)
	}

	return artifacts, nil
}

// Delete удаляет MaterializedArtifact
func (r *MaterializedArtifactsRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}

	if _, err := r.col.Doc(id).Delete(ctx); err != nil {
		return fmt.Errorf("firestore delete failed: %w", err)
	}

	return nil
}

