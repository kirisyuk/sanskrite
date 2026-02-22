package adapters

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

// CanonizationPolicyAdapter adapts Horizon CanonizationPolicy to ontology.CanonicalPathNormalizer.
// Unifies path normalization between ontology extraction and Horizon core.
type CanonizationPolicyAdapter struct {
	policy *horizonpolicy.CanonizationPolicy
}

// NewCanonizationPolicyAdapter creates an adapter for OntologyExtractor.
func NewCanonizationPolicyAdapter(p *horizonpolicy.CanonizationPolicy) ontology.CanonicalPathNormalizer {
	if p == nil {
		return nil
	}
	return &CanonizationPolicyAdapter{policy: p}
}

// NormalizePath implements ontology.CanonicalPathNormalizer.
func (a *CanonizationPolicyAdapter) NormalizePath(ctx context.Context, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return path, nil
	}
	decision, err := a.policy.Decide(ctx, horizonpolicy.PolicyInput{
		Kind: "canonization",
		Context: map[string]interface{}{
			"inputType":  "canonical_path",
			"inputValue": path,
		},
	})
	if err != nil {
		return path, err
	}
	if decision.Action != "normalize_to" {
		return path, nil
	}
	if out, ok := decision.Params["output"].(string); ok && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out), nil
	}
	return path, nil
}
