package repo

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// HorizonTaxonomyRepository stores the canonical taxonomy document in Firestore.
// Collection: horizon_taxonomy, DocID: version (e.g. "v1").
type HorizonTaxonomyRepository struct {
	col *firestore.CollectionRef
}

func NewHorizonTaxonomyRepository(db *firestore.Client) *HorizonTaxonomyRepository {
	return &HorizonTaxonomyRepository{col: db.Collection("horizon_taxonomy")}
}

func (r *HorizonTaxonomyRepository) Get(ctx context.Context, version string) (*models.HorizonTaxonomy, error) {
	if version == "" {
		version = "v1"
	}
	snap, err := r.col.Doc(version).Get(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.Exists() {
		return nil, fmt.Errorf("taxonomy not found: %s", version)
	}
	var t models.HorizonTaxonomy
	if err := snap.DataTo(&t); err != nil {
		return nil, err
	}
	if t.Version == "" {
		t.Version = version
	}
	if t.ID == "" {
		t.ID = snap.Ref.ID
	}
	return &t, nil
}

func (r *HorizonTaxonomyRepository) Upsert(ctx context.Context, t *models.HorizonTaxonomy) error {
	if t == nil {
		return fmt.Errorf("taxonomy is nil")
	}
	if t.Version == "" {
		t.Version = "v1"
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now().UTC()
	}
	t.ID = t.Version
	_, err := r.col.Doc(t.Version).Set(ctx, t)
	return err
}

