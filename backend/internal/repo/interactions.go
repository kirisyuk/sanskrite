package repo

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"
)

type Interaction struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	ProjectID string    `json:"projectId"`
	Kind      string    `json:"kind"` // e.g., "discovery.answer"
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type InteractionsRepository struct{ col *firestore.CollectionRef }

func NewInteractionsRepository(db *firestore.Client) *InteractionsRepository {
	return &InteractionsRepository{col: db.Collection("core_interactions")}
}

func (r *InteractionsRepository) Create(ctx context.Context, it *Interaction) (string, error) {
	doc := r.col.NewDoc()
	it.ID = doc.ID
	it.CreatedAt = time.Now().UTC()
	_, err := doc.Set(ctx, it)
	if err != nil {
		return "", err
	}
	return doc.ID, nil
}

// DeleteByProject removes all interactions for a given project
func (r *InteractionsRepository) DeleteByProject(ctx context.Context, projectID string) error {
	// Firestore: scan collection and delete matches (low volume acceptable)
	docs, err := r.col.Where("projectId", "==", projectID).Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, d := range docs {
		_, _ = r.col.Doc(d.Ref.ID).Delete(ctx)
	}
	return nil
}

// ListByProject returns interactions for a project ordered by createdAt asc
func (r *InteractionsRepository) ListByProject(ctx context.Context, projectID string) ([]*Interaction, error) {
	q := r.col.Where("projectId", "==", projectID).OrderBy("createdAt", firestore.Asc)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	res := make([]*Interaction, 0, len(docs))
	for _, d := range docs {
		var it Interaction
		if err := d.DataTo(&it); err != nil {
			continue
		}
		res = append(res, &it)
	}
	return res, nil
}
