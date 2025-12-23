package repo

import (
	"context"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

// PropagationPoliciesRepository manages propagation policy rules in Firestore.
type PropagationPoliciesRepository struct {
	col *firestore.CollectionRef
}

// NewPropagationPoliciesRepository creates a new PropagationPoliciesRepository.
func NewPropagationPoliciesRepository(client *firestore.Client) *PropagationPoliciesRepository {
	return &PropagationPoliciesRepository{
		col: client.Collection("horizon_propagation_policies"),
	}
}

// GetRules retrieves all active propagation policy rules.
func (r *PropagationPoliciesRepository) GetRules(ctx context.Context) ([]*policy.PropagationPolicyRule, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	rules := make([]*policy.PropagationPolicyRule, 0, len(docs))
	for _, doc := range docs {
		var rule policy.PropagationPolicyRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		rule.ID = doc.Ref.ID
		rules = append(rules, &rule)
	}

	return rules, nil
}

// Create creates a new propagation policy rule.
func (r *PropagationPoliciesRepository) Create(ctx context.Context, rule *policy.PropagationPolicyRule) (string, error) {
	doc := r.col.NewDoc()
	rule.ID = doc.ID
	_, err := doc.Set(ctx, rule)
	return doc.ID, err
}

