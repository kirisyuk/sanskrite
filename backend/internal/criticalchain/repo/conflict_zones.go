package repo

import (
	"context"
	"log"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
)

const conflictZonesCollection = "criticalchain_conflict_zones"

// ConflictZonesRepository manages conflict zones.
type ConflictZonesRepository struct {
	col *firestore.CollectionRef
}

// NewConflictZonesRepository creates a new ConflictZonesRepository.
func NewConflictZonesRepository(db *firestore.Client) *ConflictZonesRepository {
	return &ConflictZonesRepository{
		col: db.Collection(conflictZonesCollection),
	}
}

// List returns all conflict zones.
func (r *ConflictZonesRepository) List(ctx context.Context) ([]*models.ConflictZone, error) {
	iter := r.col.Documents(ctx)
	defer iter.Stop()
	var out []*models.ConflictZone
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var cz models.ConflictZone
		if err := doc.DataTo(&cz); err != nil {
			log.Printf("[CriticalChain] skip conflict zone %s: %v", doc.Ref.ID, err)
			continue
		}
		if cz.ID == "" {
			cz.ID = doc.Ref.ID
		}
		out = append(out, &cz)
	}
	return out, nil
}

// Set upserts a conflict zone.
func (r *ConflictZonesRepository) Set(ctx context.Context, cz *models.ConflictZone) error {
	if cz.ID == "" {
		return errInvalidID
	}
	_, err := r.col.Doc(cz.ID).Set(ctx, cz)
	return err
}
