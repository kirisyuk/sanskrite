package repo

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

// ValidationRulesRepository manages validation rules in Firestore.
type ValidationRulesRepository struct {
	col *firestore.CollectionRef
}

// NewValidationRulesRepository creates a new ValidationRulesRepository.
func NewValidationRulesRepository(client *firestore.Client) *ValidationRulesRepository {
	return &ValidationRulesRepository{
		col: client.Collection("horizon_validation_rules"),
	}
}

// GetRules retrieves all active validation rules.
func (r *ValidationRulesRepository) GetRules(ctx context.Context) ([]*policy.ValidationRule, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	rules := make([]*policy.ValidationRule, 0, len(docs))
	for _, doc := range docs {
		var rule policy.ValidationRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		rule.ID = doc.Ref.ID
		rules = append(rules, &rule)
	}

	return rules, nil
}

