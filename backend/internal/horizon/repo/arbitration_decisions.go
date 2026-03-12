package repo

import (
	"context"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type ArbitrationDecisionsRepository struct {
	col *firestore.CollectionRef
}

func NewArbitrationDecisionsRepository(db *firestore.Client) *ArbitrationDecisionsRepository {
	return &ArbitrationDecisionsRepository{col: db.Collection("horizon_arbitration_decisions")}
}

func (r *ArbitrationDecisionsRepository) Create(ctx context.Context, decision *models.ArbitrationDecision) (string, error) {
	if r == nil || r.col == nil || decision == nil {
		return "", ErrInvalidDocument
	}
	if err := decision.Validate(); err != nil {
		return "", err
	}
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = time.Now().UTC()
	}
	doc := r.col.NewDoc()
	decision.ID = doc.ID
	_, err := doc.Set(ctx, decision)
	return decision.ID, err
}

func (r *ArbitrationDecisionsRepository) ListByProposalID(ctx context.Context, proposalID string) ([]*models.ArbitrationDecision, error) {
	if r == nil || r.col == nil || strings.TrimSpace(proposalID) == "" {
		return nil, ErrInvalidDocument
	}
	docs, err := r.col.Where("proposalId", "==", strings.TrimSpace(proposalID)).OrderBy("createdAt", firestore.Desc).Limit(10).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.ArbitrationDecision, 0, len(docs))
	for _, doc := range docs {
		var decision models.ArbitrationDecision
		if err := doc.DataTo(&decision); err != nil {
			continue
		}
		if decision.ID == "" {
			decision.ID = doc.Ref.ID
		}
		out = append(out, &decision)
	}
	return out, nil
}
