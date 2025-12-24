package repo

import (
	"context"
	"sort"

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

	// Dedupe by (metricPattern, relationType) to avoid ambiguous behavior when the seed tool
	// is re-run or rules are manually inserted multiple times.
	//
	// Selection:
	// - higher Priority wins
	// - if Priority ties, lexicographically smaller doc ID wins (stable)
	byKey := make(map[string]*policy.PropagationPolicyRule, len(docs))
	for _, doc := range docs {
		var rule policy.PropagationPolicyRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		rule.ID = doc.Ref.ID
		key := rule.MetricPattern + "|" + rule.RelationType
		if cur, ok := byKey[key]; !ok || rule.Priority > cur.Priority || (rule.Priority == cur.Priority && rule.ID < cur.ID) {
			rr := rule
			byKey[key] = &rr
		}
	}

	rules := make([]*policy.PropagationPolicyRule, 0, len(byKey))
	for _, rule := range byKey {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		if rules[i].MetricPattern != rules[j].MetricPattern {
			return rules[i].MetricPattern < rules[j].MetricPattern
		}
		if rules[i].RelationType != rules[j].RelationType {
			return rules[i].RelationType < rules[j].RelationType
		}
		return rules[i].ID < rules[j].ID
	})

	return rules, nil
}

// Create creates a new propagation policy rule.
func (r *PropagationPoliciesRepository) Create(ctx context.Context, rule *policy.PropagationPolicyRule) (string, error) {
	doc := r.col.NewDoc()
	rule.ID = doc.ID
	_, err := doc.Set(ctx, rule)
	return doc.ID, err
}

// UpsertByMetricPatternAndRelationType updates an existing rule (if any) matching the same
// (metricPattern, relationType) pair, otherwise creates a new one.
func (r *PropagationPoliciesRepository) UpsertByMetricPatternAndRelationType(ctx context.Context, rule *policy.PropagationPolicyRule) (id string, created bool, err error) {
	q := r.col.Where("metricPattern", "==", rule.MetricPattern).Where("relationType", "==", rule.RelationType).Limit(1)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return "", false, err
	}
	if len(docs) > 0 {
		id := docs[0].Ref.ID
		rule.ID = id
		_, err := r.col.Doc(id).Set(ctx, rule)
		return id, false, err
	}
	id, err = r.Create(ctx, rule)
	return id, true, err
}

