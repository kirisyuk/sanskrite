package repo

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

// CanonizationRulesRepository manages canonization rules in Firestore.
type CanonizationRulesRepository struct {
	col *firestore.CollectionRef
}

// NewCanonizationRulesRepository creates a new CanonizationRulesRepository.
func NewCanonizationRulesRepository(client *firestore.Client) *CanonizationRulesRepository {
	return &CanonizationRulesRepository{
		col: client.Collection("horizon_canonization_rules"),
	}
}

// GetRules retrieves all active canonization rules.
func (r *CanonizationRulesRepository) GetRules(ctx context.Context) ([]*policy.CanonizationRule, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	rules := make([]*policy.CanonizationRule, 0, len(docs))
	for _, doc := range docs {
		var rule policy.CanonizationRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		rule.ID = doc.Ref.ID
		rules = append(rules, &rule)
	}

	return rules, nil
}

