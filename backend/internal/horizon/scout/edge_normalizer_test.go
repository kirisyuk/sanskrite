package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// MIGRATION NOTE (AH taxonomy): legacy ah.FragmentClassSite collapsed onto
// (site, deposit). Class-only assertions in this file now use
// ah.FragmentClassSite — the DEPOSIT subtype distinction is no longer
// observable through FragmentClass alone.

func TestEdgeDirectionPolicies_EXTRACTS_FROM(t *testing.T) {
	// Verify EXTRACTS_FROM policy exists and is correct
	rule, exists := EdgeDirectionPolicies[DependencyTypeExtractsFrom]
	if !exists {
		t.Fatal("EXTRACTS_FROM edge direction policy not found")
	}

	// Verify valid source types (only FACILITY)
	if len(rule.ValidSourceClasses) != 1 {
		t.Errorf("EXTRACTS_FROM should have exactly 1 valid source type, got %d", len(rule.ValidSourceClasses))
	}
	if rule.ValidSourceClasses[0] != FragmentTypeFacility {
		t.Errorf("EXTRACTS_FROM source should be FACILITY, got %s", rule.ValidSourceClasses[0])
	}

	// Verify valid target types (only DEPOSIT)
	if len(rule.ValidTargetClasses) != 1 {
		t.Errorf("EXTRACTS_FROM should have exactly 1 valid target type, got %d", len(rule.ValidTargetClasses))
	}
	if rule.ValidTargetClasses[0] != ah.FragmentClassSite {
		t.Errorf("EXTRACTS_FROM target should be DEPOSIT, got %s", rule.ValidTargetClasses[0])
	}
}

func TestEdgeDirectionPolicies_OWNS_IncludesDeposit(t *testing.T) {
	// Verify OWNS policy includes DEPOSIT as valid target
	rule, exists := EdgeDirectionPolicies[DependencyTypeOwns]
	if !exists {
		t.Fatal("OWNS edge direction policy not found")
	}

	hasDeposit := false
	for _, targetType := range rule.ValidTargetClasses {
		if targetType == ah.FragmentClassSite {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit {
		t.Error("OWNS should allow DEPOSIT as target type")
	}
}

func TestEdgeDirectionPolicies_OWNS_IncludesCommunitySource(t *testing.T) {
	rule, exists := EdgeDirectionPolicies[DependencyTypeOwns]
	if !exists {
		t.Fatal("OWNS edge direction policy not found")
	}

	hasCommunity := false
	for _, sourceType := range rule.ValidSourceClasses {
		if sourceType == FragmentTypeCommunity {
			hasCommunity = true
			break
		}
	}
	if !hasCommunity {
		t.Error("OWNS should allow COMMUNITY as source type")
	}
}

func TestEdgeDirectionPolicies_PRODUCES_IncludesDeposit(t *testing.T) {
	// Verify PRODUCES policy includes DEPOSIT as valid source
	rule, exists := EdgeDirectionPolicies[DependencyTypeProduces]
	if !exists {
		t.Fatal("PRODUCES edge direction policy not found")
	}

	hasDeposit := false
	for _, sourceType := range rule.ValidSourceClasses {
		if sourceType == ah.FragmentClassSite {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit {
		t.Error("PRODUCES should allow DEPOSIT as source type")
	}
}

func TestEdgeDirectionPolicies_LOCATED_IN_IncludesDeposit(t *testing.T) {
	// Verify LOCATED_IN policy includes DEPOSIT as valid source
	rule, exists := EdgeDirectionPolicies[DependencyTypeLocatedIn]
	if !exists {
		t.Fatal("LOCATED_IN edge direction policy not found")
	}

	hasDeposit := false
	for _, sourceType := range rule.ValidSourceClasses {
		if sourceType == ah.FragmentClassSite {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit {
		t.Error("LOCATED_IN should allow DEPOSIT as source type")
	}
}

func TestEdgeDirectionPolicies_OPERATES_IN_IncludesDeposit(t *testing.T) {
	// Verify OPERATES_IN policy includes DEPOSIT as valid target
	rule, exists := EdgeDirectionPolicies[DependencyTypeOperatesIn]
	if !exists {
		t.Fatal("OPERATES_IN edge direction policy not found")
	}

	hasDeposit := false
	for _, targetType := range rule.ValidTargetClasses {
		if targetType == ah.FragmentClassSite {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit {
		t.Error("OPERATES_IN should allow DEPOSIT as target type")
	}
}

func TestEdgeDirectionPolicies_CanonicalAndLegacyAliasCoverage(t *testing.T) {
	cases := []DependencyType{
		DependencyTypeAssociatedWith,
		DependencyTypeCorporateTransition,
		DependencyTypeDomainRelation,
		DependencyTypeSubsidiaryOf,
		DependencyTypeHasSubmarket,
	}
	for _, edgeType := range cases {
		if _, exists := EdgeDirectionPolicies[edgeType]; !exists {
			t.Fatalf("edge direction policy missing for %s", edgeType)
		}
	}
}

func TestNormalizeSymmetricEdge(t *testing.T) {
	tests := []struct {
		name        string
		sourceID    string
		targetID    string
		edgeType    DependencyType
		wantSource  string
		wantTarget  string
		wantSwapped bool
	}{
		{
			name:        "symmetric COMPETES_WITH - no swap needed",
			sourceID:    "aaa",
			targetID:    "bbb",
			edgeType:    DependencyTypeCompetesWith,
			wantSource:  "aaa",
			wantTarget:  "bbb",
			wantSwapped: false,
		},
		{
			name:        "symmetric COMPETES_WITH - swap needed",
			sourceID:    "bbb",
			targetID:    "aaa",
			edgeType:    DependencyTypeCompetesWith,
			wantSource:  "aaa",
			wantTarget:  "bbb",
			wantSwapped: true,
		},
		{
			name:        "non-symmetric OWNS - never swaps",
			sourceID:    "bbb",
			targetID:    "aaa",
			edgeType:    DependencyTypeOwns,
			wantSource:  "bbb",
			wantTarget:  "aaa",
			wantSwapped: false,
		},
		{
			name:        "non-symmetric EXTRACTS_FROM - never swaps",
			sourceID:    "facility-z",
			targetID:    "deposit-a",
			edgeType:    DependencyTypeExtractsFrom,
			wantSource:  "facility-z",
			wantTarget:  "deposit-a",
			wantSwapped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSource, gotTarget, _, gotSwapped := NormalizeSymmetricEdge(tt.sourceID, tt.targetID, tt.edgeType)
			if gotSource != tt.wantSource {
				t.Errorf("NormalizeSymmetricEdge() source = %v, want %v", gotSource, tt.wantSource)
			}
			if gotTarget != tt.wantTarget {
				t.Errorf("NormalizeSymmetricEdge() target = %v, want %v", gotTarget, tt.wantTarget)
			}
			if gotSwapped != tt.wantSwapped {
				t.Errorf("NormalizeSymmetricEdge() swapped = %v, want %v", gotSwapped, tt.wantSwapped)
			}
		})
	}
}

func TestCanonicalizeEdge_ProductOwnsOrganization_ToProduces(t *testing.T) {
	tests := []struct {
		name       string
		edgeType   DependencyType
		sourceType FragmentType
		targetType FragmentType
		wantType   DependencyType
		wantSwap   bool
		wantChange bool
	}{
		{
			name:       "PRODUCT OWNS ORGANIZATION -> PRODUCES with swap",
			edgeType:   DependencyTypeOwns,
			sourceType: FragmentTypeProduct,
			targetType: FragmentTypeOrganization,
			wantType:   DependencyTypeProduces,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION OWNS PRODUCT stays OWNS (no canonicalization)",
			edgeType:   DependencyTypeOwns,
			sourceType: FragmentTypeOrganization,
			targetType: FragmentTypeProduct,
			wantType:   DependencyTypeOwns,
			wantSwap:   false,
			wantChange: false,
		},
		{
			name:       "PRODUCT PRODUCES ORGANIZATION (nonsense) is not rewritten here (direction rules handle it)",
			edgeType:   DependencyTypeProduces,
			sourceType: FragmentTypeProduct,
			targetType: FragmentTypeOrganization,
			wantType:   DependencyTypeProduces,
			wantSwap:   false,
			wantChange: false,
		},
		{
			name:       "PRODUCT PRODUCES PRODUCT -> TRANSFORMS_INTO",
			edgeType:   DependencyTypeProduces,
			sourceType: FragmentTypeProduct,
			targetType: FragmentTypeProduct,
			wantType:   DependencyTypeTransformsInto,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION EXTRACTS_FROM DEPOSIT -> OPERATES_IN (best-effort)",
			edgeType:   DependencyTypeExtractsFrom,
			sourceType: FragmentTypeOrganization,
			targetType: ah.FragmentClassSite,
			wantType:   DependencyTypeOperatesIn,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "DEPOSIT EXTRACTS_FROM ORGANIZATION -> OPERATES_IN with swap (best-effort)",
			edgeType:   DependencyTypeExtractsFrom,
			sourceType: ah.FragmentClassSite,
			targetType: FragmentTypeOrganization,
			wantType:   DependencyTypeOperatesIn,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ENTITY APPLIES_TO MECHANISM -> swap to MECHANISM APPLIES_TO ENTITY",
			edgeType:   DependencyTypeAppliesTo,
			sourceType: FragmentTypeOrganization,
			targetType: FragmentTypeMechanism,
			wantType:   DependencyTypeAppliesTo,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "Non-mechanism APPLIES_TO -> INFLUENCES",
			edgeType:   DependencyTypeAppliesTo,
			sourceType: FragmentTypeOrganization,
			targetType: FragmentTypeMarket,
			wantType:   DependencyTypeInfluences,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "COMMUNITY FOUNDED PERSON -> swap to PERSON FOUNDED COMMUNITY",
			edgeType:   DependencyTypeFounded,
			sourceType: FragmentTypeCommunity,
			targetType: FragmentTypePerson,
			wantType:   DependencyTypeFounded,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION FOUNDED PERSON -> swap to PERSON FOUNDED ORGANIZATION",
			edgeType:   DependencyTypeFounded,
			sourceType: FragmentTypeOrganization,
			targetType: FragmentTypePerson,
			wantType:   DependencyTypeFounded,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET (swap)",
			edgeType:   DependencyTypeInvestsIn,
			sourceType: FragmentTypeMarket,
			targetType: FragmentTypeOrganization,
			wantType:   DependencyTypeParticipatesIn,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION RELATED_TO MARKET -> ORGANIZATION PARTICIPATES_IN MARKET",
			edgeType:   DependencyTypeRelatedTo,
			sourceType: FragmentTypeOrganization,
			targetType: FragmentTypeMarket,
			wantType:   DependencyTypeParticipatesIn,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "MARKET RELATED_TO ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET (swap)",
			edgeType:   DependencyTypeRelatedTo,
			sourceType: FragmentTypeMarket,
			targetType: FragmentTypeOrganization,
			wantType:   DependencyTypeParticipatesIn,
			wantSwap:   true,
			wantChange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotSwap, gotChanged, _ := CanonicalizeEdge(tt.edgeType, tt.sourceType, tt.targetType)
			if gotType != tt.wantType {
				t.Fatalf("CanonicalizeEdge() type=%s, want %s", gotType, tt.wantType)
			}
			if gotSwap != tt.wantSwap {
				t.Fatalf("CanonicalizeEdge() swap=%v, want %v", gotSwap, tt.wantSwap)
			}
			if gotChanged != tt.wantChange {
				t.Fatalf("CanonicalizeEdge() changed=%v, want %v", gotChanged, tt.wantChange)
			}
		})
	}
}

func TestNormalizeRelationEdge_OwnsCommunityToOrganization_IsPreserved(t *testing.T) {
	sourceFrag := &models.HorizonFragment{Class: FragmentTypeCommunity}

	gotSource, gotTarget, gotType, changed := normalizeRelationEdge(
		sourceFrag,
		"community-id",
		"org-id",
		DependencyTypeOwns,
		FragmentTypeOrganization,
		false,
		nil,
	)
	if changed {
		t.Fatalf("expected COMMUNITY -> ORGANIZATION OWNS to remain canonical")
	}
	if gotSource != "community-id" || gotTarget != "org-id" {
		t.Fatalf("expected source/target unchanged, got %q -> %q", gotSource, gotTarget)
	}
	if gotType != DependencyTypeOwns {
		t.Fatalf("expected OWNS, got %s", gotType)
	}
}
