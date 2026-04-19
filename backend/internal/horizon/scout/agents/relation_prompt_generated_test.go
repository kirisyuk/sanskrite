package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
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
	for _, allowed := range []DependencyType{
		DependencyTypeProduces,
		DependencyTypeSuppliesTo,
		DependencyTypeTransformsInto,
		DependencyTypeHasMarketSegment,
		DependencyTypeCompetesWith,
		DependencyTypePartnersWith,
		DependencyTypeOperatesVia,
		DependencyTypeMemberOf,
		DependencyTypeInvestsIn,
	} {
		if !strings.Contains(block, string(allowed)) {
			t.Fatalf("expected supply-chain decomposer block to include %s", allowed)
		}
	}
	for _, forbidden := range []DependencyType{
		DependencyTypeOwns,
		DependencyTypeDependsOn,
	} {
		if strings.Contains(block, string(forbidden)+":") {
			t.Fatalf("did not expect supply-chain decomposer block to include %s entry", forbidden)
		}
	}
}
