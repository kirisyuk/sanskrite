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
