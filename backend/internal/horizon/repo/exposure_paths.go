package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var ErrExposurePathNotFound = errors.New("exposure path not found")

// ExposurePathsRepository manages ExposurePathArtifact documents.
// Collection: horizon_exposure_paths
type ExposurePathsRepository struct {
	col *firestore.CollectionRef
}

func NewExposurePathsRepository(db *firestore.Client) *ExposurePathsRepository {
	return &ExposurePathsRepository{
		col: db.Collection("horizon_exposure_paths"),
	}
}

// Upsert stores an exposure path artifact idempotently by canonical key.
// Doc ID is deterministic, so repeated upserts converge to the same document.
func (r *ExposurePathsRepository) Upsert(ctx context.Context, a *models.ExposurePathArtifact) (string, error) {
	if a == nil {
		return "", ErrInvalidDocument
	}

	// Canonicalize key and populate derived fields
	canon, parts, err := models.CanonicalizeExposureKey(a.CanonicalKey)
	if err != nil {
		return "", fmt.Errorf("canonicalize exposure key: %w", err)
	}
	a.CanonicalKey = canon
	a.Type = parts.Type
	a.PrefixKey = parts.PrefixKey()
	a.ObjectiveKind = parts.ObjectiveKind
	a.ObjectiveID = parts.ObjectiveID
	a.RelatedKind = parts.RelatedKind
	a.RelatedID = parts.RelatedID
	a.PathID = parts.PathID

	if err := a.Validate(); err != nil {
		return "", fmt.Errorf("validation error: %w", err)
	}

	docID := models.ExposureDocID(a.TenantID, a.LensID, a.CanonicalKey)
	doc := r.col.Doc(docID)

	now := time.Now().UTC()
	a.UpdatedAt = now

	// Preserve CreatedAt on re-upsert
	snap, gerr := doc.Get(ctx)
	if gerr == nil && snap.Exists() {
		// Keep existing CreatedAt if present
		var existing models.ExposurePathArtifact
		if derr := snap.DataTo(&existing); derr == nil && !existing.CreatedAt.IsZero() {
			a.CreatedAt = existing.CreatedAt
		}
	} else if gerr != nil {
		// If it's a real error other than "not found", return it
		// Firestore returns an error for missing docs; snap.Exists() handles that when no err.
		a.CreatedAt = now
	} else {
		a.CreatedAt = now
	}

	a.ID = docID
	if _, err := doc.Set(ctx, a); err != nil {
		return "", err
	}
	return docID, nil
}

func (r *ExposurePathsRepository) Get(ctx context.Context, id string) (*models.ExposurePathArtifact, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	snap, err := r.col.Doc(id).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, ErrExposurePathNotFound
	}
	var out models.ExposurePathArtifact
	if err := snap.DataTo(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetByCanonicalKey loads exposure artifact directly using deterministic doc ID.
func (r *ExposurePathsRepository) GetByCanonicalKey(ctx context.Context, tenantID, lensID, canonicalKey string) (*models.ExposurePathArtifact, error) {
	canon, _, err := models.CanonicalizeExposureKey(canonicalKey)
	if err != nil {
		return nil, fmt.Errorf("canonicalize exposure key: %w", err)
	}
	id := models.ExposureDocID(tenantID, lensID, canon)
	return r.Get(ctx, id)
}

// ListByLens returns exposure artifacts for a lens, ordered by updatedAt desc.
// NOTE: This requires a Firestore composite index:
// - (tenantId ASC, lensId ASC, updatedAt DESC)
// If filtering by type, add:
// - (tenantId ASC, lensId ASC, type ASC, updatedAt DESC)
func (r *ExposurePathsRepository) ListByLens(ctx context.Context, tenantID, lensID string, typ *models.ExposureType, limit int) ([]*models.ExposurePathArtifact, error) {
	if tenantID == "" || lensID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 100
	}

	q := r.col.Where("tenantId", "==", tenantID).Where("lensId", "==", lensID)
	if typ != nil && typ.Valid() {
		q = q.Where("type", "==", string(*typ))
	}
	q = q.OrderBy("updatedAt", firestore.Desc).Limit(limit)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ExposurePathArtifact, 0, len(docs))
	for _, d := range docs {
		var a models.ExposurePathArtifact
		if err := d.DataTo(&a); err != nil {
			continue
		}
		out = append(out, &a)
	}
	return out, nil
}


