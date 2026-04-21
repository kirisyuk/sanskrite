package agents

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
	"testing"
)

func TestGeneratedGeneralRelationTaxonomyBlock_CoversAllNonDomainEdgePolicies(t *testing.T) {
	block := generatedGeneralRelationTaxonomyBlock()
	for edgeType := range edgesemantics.EdgeDirectionPolicies {
		if _, isDomain := domainPromptEdges[edgeType]; isDomain {
			continue
		}
		if !strings.Contains(block, string(edgeType)) {
			t.Fatalf("generated general relation taxonomy missing edge type %s", edgeType)
		}
	}
}

func TestGeneratedIntentDecomposerRelationRulesBlock_RespectsSupplyChainAllowlist(t *testing.T) {
	block := generatedIntentDecomposerRelationRulesBlock([]string{taxonomy.DomainModeSupplyChain})
	for _, allowed := range []ah.RelationName{
		ah.RelationNameProduces,
		ah.RelationNameSupplies,
		ah.RelationName("transforms_into"),
		ah.RelationName("has_market_segment"),
		ah.RelationName("competes_with"),
		ah.RelationNameJointVentureWith,
		ah.RelationName("operates_via"),
		ah.RelationNameMemberOf,
		ah.RelationNameInvestedIn,
	} {
		if !strings.Contains(block, string(allowed)) {
			t.Fatalf("expected supply-chain decomposer block to include %s", allowed)
		}
	}
	for _, forbidden := range []ah.RelationName{
		ah.RelationNameBeneficiallyOwns,
		ah.RelationName("depends_on"),
	} {
		if strings.Contains(block, string(forbidden)+":") {
			t.Fatalf("did not expect supply-chain decomposer block to include %s entry", forbidden)
		}
	}
}
