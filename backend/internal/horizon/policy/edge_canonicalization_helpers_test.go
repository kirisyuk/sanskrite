package policy

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// TestCanonicalizeEdgeWithPaths_LegacyRewrites covers the path-aware rewrites
// previously hosted in models.CanonicalizeEdge (now deleted). These rules are
// the single source of truth used by scout via policy.CanonicalizeEdgeWithPaths.
func TestCanonicalizeEdgeWithPaths_LegacyRewrites(t *testing.T) {
	tests := []struct {
		name        string
		edgeType    ah.RelationName
		sourceClass ah.FragmentClass
		targetClass ah.FragmentClass
		sourcePath  string
		targetPath  string
		wantType    ah.RelationName
		wantSwap    bool
		wantChange  bool
	}{
		{
			name:        "PRODUCT OWNS ORG -> PRODUCES with swap",
			edgeType:    relOwns,
			sourceClass: ah.FragmentClassProduct,
			targetClass: ah.FragmentClassOrg,
			wantType:    relProduces,
			wantSwap:    true,
			wantChange:  true,
		},
		{
			name:        "ORG OWNS PRODUCT stays OWNS",
			edgeType:    relOwns,
			sourceClass: ah.FragmentClassOrg,
			targetClass: ah.FragmentClassProduct,
			wantType:    relOwns,
			wantSwap:    false,
			wantChange:  false,
		},
		{
			name:        "PRODUCT PRODUCES ORG is not rewritten here",
			edgeType:    relProduces,
			sourceClass: ah.FragmentClassProduct,
			targetClass: ah.FragmentClassOrg,
			wantType:    relProduces,
			wantSwap:    false,
			wantChange:  false,
		},
		{
			name:        "PRODUCT PRODUCES PRODUCT -> TRANSFORMS_INTO",
			edgeType:    relProduces,
			sourceClass: ah.FragmentClassProduct,
			targetClass: ah.FragmentClassProduct,
			wantType:    relTransformsInto,
			wantSwap:    false,
			wantChange:  true,
		},
		{
			name:        "ORG EXTRACTS_FROM SITE -> OPERATES_IN no swap",
			edgeType:    relExtractsFrom,
			sourceClass: ah.FragmentClassOrg,
			targetClass: ah.FragmentClassSite,
			wantType:    relOperatesIn,
			wantSwap:    false,
			wantChange:  true,
		},
		{
			name:        "SITE EXTRACTS_FROM ORG -> OPERATES_IN with swap",
			edgeType:    relExtractsFrom,
			sourceClass: ah.FragmentClassSite,
			targetClass: ah.FragmentClassOrg,
			wantType:    relOperatesIn,
			wantSwap:    true,
			wantChange:  true,
		},
		{
			name:        "ENTITY APPLIES_TO MECHANISM -> swap",
			edgeType:    relAppliesTo,
			sourceClass: ah.FragmentClassOrg,
			targetClass: ah.FragmentClassMechanism,
			wantType:    relAppliesTo,
			wantSwap:    true,
			wantChange:  true,
		},
		{
			name:        "MECHANISM APPLIES_TO ENTITY stays canonical",
			edgeType:    relAppliesTo,
			sourceClass: ah.FragmentClassMechanism,
			targetClass: ah.FragmentClassOrg,
			wantType:    relAppliesTo,
			wantSwap:    false,
			wantChange:  false,
		},
		{
			name:        "Non-mechanism APPLIES_TO -> INFLUENCES",
			edgeType:    relAppliesTo,
			sourceClass: ah.FragmentClassOrg,
			targetClass: ah.FragmentClassMarket,
			wantType:    relInfluences,
			wantSwap:    false,
			wantChange:  true,
		},
		{
			name:        "COMMUNITY FOUNDED PERSON -> swap",
			edgeType:    relFounded,
			sourceClass: ah.FragmentClassCommunity,
			targetClass: ah.FragmentClassPerson,
			wantType:    relFounded,
			wantSwap:    true,
			wantChange:  true,
		},
		{
			name:        "ORG FOUNDED PERSON -> swap",
			edgeType:    relFounded,
			sourceClass: ah.FragmentClassOrg,
			targetClass: ah.FragmentClassPerson,
			wantType:    relFounded,
			wantSwap:    true,
			wantChange:  true,
		},
		{
			name:        "PERSON FOUNDED COMMUNITY stays canonical",
			edgeType:    relFounded,
			sourceClass: ah.FragmentClassPerson,
			targetClass: ah.FragmentClassCommunity,
			wantType:    relFounded,
			wantSwap:    false,
			wantChange:  false,
		},
		{
			name:        "MARKET INVESTS_IN ORG -> ORG PARTICIPATES_IN MARKET (swap+rename)",
			edgeType:    relInvestsIn,
			sourceClass: ah.FragmentClassMarket,
			targetClass: ah.FragmentClassOrg,
			wantType:    relParticipatesIn,
			wantSwap:    true,
			wantChange:  true,
		},
		{
			name:        "ORG RELATED_TO MARKET -> ORG PARTICIPATES_IN MARKET",
			edgeType:    relRelatedTo,
			sourceClass: ah.FragmentClassOrg,
			targetClass: ah.FragmentClassMarket,
			wantType:    relParticipatesIn,
			wantSwap:    false,
			wantChange:  true,
		},
		{
			name:        "MARKET RELATED_TO ORG -> ORG PARTICIPATES_IN MARKET (swap)",
			edgeType:    relRelatedTo,
			sourceClass: ah.FragmentClassMarket,
			targetClass: ah.FragmentClassOrg,
			wantType:    relParticipatesIn,
			wantSwap:    true,
			wantChange:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotSwap, gotChanged, _ := CanonicalizeEdgeWithPaths(
				tt.edgeType, tt.sourceClass, tt.targetClass, tt.sourcePath, tt.targetPath,
			)
			if gotType != tt.wantType {
				t.Fatalf("type=%s, want %s", gotType, tt.wantType)
			}
			if gotSwap != tt.wantSwap {
				t.Fatalf("swap=%v, want %v", gotSwap, tt.wantSwap)
			}
			if gotChanged != tt.wantChange {
				t.Fatalf("changed=%v, want %v", gotChanged, tt.wantChange)
			}
		})
	}
}
