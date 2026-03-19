package repo

import (
	"context"
	"log"

	"cloud.google.com/go/firestore"
)

const sanctionsCollection = "criticalchain_sanctions"

// SanctionRecord stores a sanction entity (OFAC, EU, etc.) for screening.
type SanctionRecord struct {
	ID        string                 `json:"id" firestore:"id"`
	Name      string                 `json:"name" firestore:"name"`
	Source    string                 `json:"source" firestore:"source"`
	Payload   map[string]interface{} `json:"payload,omitempty" firestore:"payload,omitempty"`
	UpdatedAt interface{}            `json:"updatedAt" firestore:"updatedAt"`
}

// SanctionsRepository manages sanction records.
type SanctionsRepository struct {
	col *firestore.CollectionRef
}

// NewSanctionsRepository creates a new sanctions repository.
func NewSanctionsRepository(db *firestore.Client) *SanctionsRepository {
	return &SanctionsRepository{
		col: db.Collection(sanctionsCollection),
	}
}

// Set upserts a sanction record.
func (r *SanctionsRepository) Set(ctx context.Context, rec *SanctionRecord) error {
	if rec == nil || rec.ID == "" {
		return errInvalidID
	}
	_, err := r.col.Doc(rec.ID).Set(ctx, rec)
	return err
}

// List returns all sanction records (for screening).
func (r *SanctionsRepository) List(ctx context.Context) ([]*SanctionRecord, error) {
	iter := r.col.Documents(ctx)
	defer iter.Stop()
	var out []*SanctionRecord
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var rec SanctionRecord
		if err := doc.DataTo(&rec); err != nil {
			log.Printf("[CriticalChain] skip sanction %s: %v", doc.Ref.ID, err)
			continue
		}
		if rec.ID == "" {
			rec.ID = doc.Ref.ID
		}
		out = append(out, &rec)
	}
	return out, nil
}
