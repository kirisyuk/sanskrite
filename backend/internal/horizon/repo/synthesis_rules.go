package repo

import (
	"context"

	"cloud.google.com/go/firestore"
)

// SynthesisRule defines a deterministic fact → signal transformation.
type SynthesisRule struct {
	ID                 string   `json:"id" firestore:"id"`
	FactKind           string   `json:"factKind" firestore:"factKind"`           // "event" | "claim" | "metric"
	FactTypePattern    string   `json:"factTypePattern" firestore:"factTypePattern"` // "port_closure" | "sanction_*"
	SpherePattern      string   `json:"spherePattern" firestore:"spherePattern"`     // "/sphere/logistics/*"
	OutputSignalType   string   `json:"outputSignalType" firestore:"outputSignalType"` // "risk:supply_disruption"
	ConfidenceMultiplier float64 `json:"confidenceMultiplier" firestore:"confidenceMultiplier"` // 0.8 = 80% of fact confidence
	MetricName         string   `json:"metricName" firestore:"metricName"`         // "risk_supply_disruption_score"
	MetricValue        *float64 `json:"metricValue,omitempty" firestore:"metricValue,omitempty"` // Fixed value or nil (use confidence)
	Priority           int      `json:"priority" firestore:"priority"`
}

// SynthesisRulesRepository manages synthesis rules in Firestore.
type SynthesisRulesRepository struct {
	col *firestore.CollectionRef
}

// NewSynthesisRulesRepository creates a new SynthesisRulesRepository.
func NewSynthesisRulesRepository(client *firestore.Client) *SynthesisRulesRepository {
	return &SynthesisRulesRepository{
		col: client.Collection("horizon_synthesis_rules"),
	}
}

// GetRules retrieves all active synthesis rules.
func (r *SynthesisRulesRepository) GetRules(ctx context.Context) ([]*SynthesisRule, error) {
	docs, err := r.col.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	rules := make([]*SynthesisRule, 0, len(docs))
	for _, doc := range docs {
		var rule SynthesisRule
		if err := doc.DataTo(&rule); err != nil {
			continue
		}
		rule.ID = doc.Ref.ID
		rules = append(rules, &rule)
	}

	return rules, nil
}

