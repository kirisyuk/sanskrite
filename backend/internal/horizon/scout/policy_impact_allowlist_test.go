package scout

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEdgeAllowlistByDomainMode_PolicyImpactTracksTaxonomyContract(t *testing.T) {
	allow := EdgeAllowlistByDomainMode(taxonomy.DomainModePolicyImpact)
	for _, edgeType := range taxonomy.PolicyImpactEdgeTypes {
		edgeType = strings.TrimSpace(edgeType)
		if edgeType == "" {
			continue
		}
		if !AllowlistContains(allow, models.DependencyType(edgeType)) {
			t.Fatalf("policy_impact runtime allowlist must include taxonomy edge %q", edgeType)
		}
	}
}
