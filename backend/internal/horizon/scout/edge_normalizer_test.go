package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// MIGRATION NOTE (AH taxonomy): legacy ah.FragmentClassSite collapsed onto
// (site, deposit). Class-only assertions in this file now use
// ah.FragmentClassSite — the deposit subtype distinction is no longer
// observable through FragmentClass alone.

func TestEdgeDirectionPolicies_EXTRACTS_FROM(t *testing.T) {
	// Verify EXTRACTS_FROM policy exists and is correct
	rule, exists := EdgeDirectionPolicies[DependencyType("extracts_from")]
	if !exists {
		t.Fatal("EXTRACTS_FROM edge direction policy not found")
	}

	// Verify valid source types (only FACILITY)
	if len(rule.ValidSourceClasses) != 1 {
		t.Errorf("EXTRACTS_FROM should have exactly 1 valid source type, got %d", len(rule.ValidSourceClasses))
	}
	if rule.ValidSourceClasses[0] != ah.FragmentClassSite {
		t.Errorf("EXTRACTS_FROM source should be FACILITY, got %s", rule.ValidSourceClasses[0])
	}

	// Verify valid target types (only deposit)
	if len(rule.ValidTargetClasses) != 1 {
		t.Errorf("EXTRACTS_FROM should have exactly 1 valid target type, got %d", len(rule.ValidTargetClasses))
	}
	if rule.ValidTargetClasses[0] != ah.FragmentClassSite {
		t.Errorf("EXTRACTS_FROM target should be deposit, got %s", rule.ValidTargetClasses[0])
	}
}

func TestEdgeDirectionPolicies_OWNS_IncludesDeposit(t *testing.T) {
	// Verify OWNS policy includes deposit as valid target
	rule, exists := EdgeDirectionPolicies[ah.RelationNameBeneficiallyOwns]
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
		t.Error("OWNS should allow deposit as target type")
	}
}

func TestEdgeDirectionPolicies_OWNS_IncludesCommunitySource(t *testing.T) {
	rule, exists := EdgeDirectionPolicies[ah.RelationNameBeneficiallyOwns]
	if !exists {
		t.Fatal("OWNS edge direction policy not found")
	}

	hasCommunity := false
	for _, sourceType := range rule.ValidSourceClasses {
		if sourceType == ah.FragmentClassCommunity {
			hasCommunity = true
			break
		}
	}
	if !hasCommunity {
		t.Error("OWNS should allow COMMUNITY as source type")
	}
}

func TestEdgeDirectionPolicies_PRODUCES_IncludesDeposit(t *testing.T) {
	// Verify PRODUCES policy includes deposit as valid source
	rule, exists := EdgeDirectionPolicies[ah.RelationNameProduces]
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
		t.Error("PRODUCES should allow deposit as source type")
	}
}

func TestEdgeDirectionPolicies_LOCATED_IN_IncludesDeposit(t *testing.T) {
	// Verify LOCATED_IN policy includes deposit as valid source
	rule, exists := EdgeDirectionPolicies[DependencyType("located_in")]
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
		t.Error("LOCATED_IN should allow deposit as source type")
	}
}

func TestEdgeDirectionPolicies_OPERATES_IN_IncludesDeposit(t *testing.T) {
	// Verify OPERATES_IN policy includes deposit as valid target
	rule, exists := EdgeDirectionPolicies[DependencyType("operates_in")]
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
		t.Error("OPERATES_IN should allow deposit as target type")
	}
}

func TestEdgeDirectionPolicies_CanonicalAndLegacyAliasCoverage(t *testing.T) {
	cases := []DependencyType{
		ah.RelationNameJointVentureWith,
		ah.RelationNameMergedInto,
		ah.RelationNameInfluences,
		ah.RelationNameSubsidiaryOf,
		DependencyType("corporate_transition"),
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
			edgeType:    DependencyType("competes_with"),
			wantSource:  "aaa",
			wantTarget:  "bbb",
			wantSwapped: false,
		},
		{
			name:        "symmetric COMPETES_WITH - swap needed",
			sourceID:    "bbb",
			targetID:    "aaa",
			edgeType:    DependencyType("competes_with"),
			wantSource:  "aaa",
			wantTarget:  "bbb",
			wantSwapped: true,
		},
		{
			name:        "non-symmetric OWNS - never swaps",
			sourceID:    "bbb",
			targetID:    "aaa",
			edgeType:    ah.RelationNameBeneficiallyOwns,
			wantSource:  "bbb",
			wantTarget:  "aaa",
			wantSwapped: false,
		},
		{
			name:        "non-symmetric EXTRACTS_FROM - never swaps",
			sourceID:    "facility-z",
			targetID:    "deposit-a",
			edgeType:    DependencyType("extracts_from"),
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
			edgeType:   ah.RelationNameBeneficiallyOwns,
			sourceType: ah.FragmentClassProduct,
			targetType: ah.FragmentClassOrg,
			wantType:   ah.RelationNameProduces,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION OWNS PRODUCT stays OWNS (no canonicalization)",
			edgeType:   ah.RelationNameBeneficiallyOwns,
			sourceType: ah.FragmentClassOrg,
			targetType: ah.FragmentClassProduct,
			wantType:   ah.RelationNameBeneficiallyOwns,
			wantSwap:   false,
			wantChange: false,
		},
		{
			name:       "PRODUCT PRODUCES ORGANIZATION (nonsense) is not rewritten here (direction rules handle it)",
			edgeType:   ah.RelationNameProduces,
			sourceType: ah.FragmentClassProduct,
			targetType: ah.FragmentClassOrg,
			wantType:   ah.RelationNameProduces,
			wantSwap:   false,
			wantChange: false,
		},
		{
			name:       "PRODUCT PRODUCES PRODUCT -> transforms_into",
			edgeType:   ah.RelationNameProduces,
			sourceType: ah.FragmentClassProduct,
			targetType: ah.FragmentClassProduct,
			wantType:   DependencyType("transforms_into"),
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION EXTRACTS_FROM deposit -> OPERATES_IN (best-effort)",
			edgeType:   DependencyType("extracts_from"),
			sourceType: ah.FragmentClassOrg,
			targetType: ah.FragmentClassSite,
			wantType:   DependencyType("operates_in"),
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "deposit EXTRACTS_FROM ORGANIZATION -> OPERATES_IN with swap (best-effort)",
			edgeType:   DependencyType("extracts_from"),
			sourceType: ah.FragmentClassSite,
			targetType: ah.FragmentClassOrg,
			wantType:   DependencyType("operates_in"),
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ENTITY APPLIES_TO MECHANISM -> swap to MECHANISM APPLIES_TO ENTITY",
			edgeType:   DependencyType("applies_to"),
			sourceType: ah.FragmentClassOrg,
			targetType: ah.FragmentClassMechanism,
			wantType:   DependencyType("applies_to"),
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "Non-mechanism APPLIES_TO -> INFLUENCES",
			edgeType:   DependencyType("applies_to"),
			sourceType: ah.FragmentClassOrg,
			targetType: ah.FragmentClassMarket,
			wantType:   ah.RelationNameInfluences,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "COMMUNITY FOUNDED PERSON -> swap to PERSON FOUNDED COMMUNITY",
			edgeType:   DependencyType("founded"),
			sourceType: ah.FragmentClassCommunity,
			targetType: ah.FragmentClassPerson,
			wantType:   DependencyType("founded"),
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION FOUNDED PERSON -> swap to PERSON FOUNDED ORGANIZATION",
			edgeType:   DependencyType("founded"),
			sourceType: ah.FragmentClassOrg,
			targetType: ah.FragmentClassPerson,
			wantType:   DependencyType("founded"),
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET (swap)",
			edgeType:   ah.RelationNameInvestedIn,
			sourceType: ah.FragmentClassMarket,
			targetType: ah.FragmentClassOrg,
			wantType:   DependencyType("participates_in"),
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION RELATED_TO MARKET -> ORGANIZATION PARTICIPATES_IN MARKET",
			edgeType:   DependencyType("related_to"),
			sourceType: ah.FragmentClassOrg,
			targetType: ah.FragmentClassMarket,
			wantType:   DependencyType("participates_in"),
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "MARKET RELATED_TO ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET (swap)",
			edgeType:   DependencyType("related_to"),
			sourceType: ah.FragmentClassMarket,
			targetType: ah.FragmentClassOrg,
			wantType:   DependencyType("participates_in"),
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
	sourceFrag := &models.HorizonFragment{Class: ah.FragmentClassCommunity}

	gotSource, gotTarget, gotType, changed := normalizeRelationEdge(
		sourceFrag,
		"community-id",
		"org-id",
		ah.RelationNameBeneficiallyOwns,
		ah.FragmentClassOrg,
		false,
		nil,
	)
	if changed {
		t.Fatalf("expected COMMUNITY -> ORGANIZATION OWNS to remain canonical")
	}
	if gotSource != "community-id" || gotTarget != "org-id" {
		t.Fatalf("expected source/target unchanged, got %q -> %q", gotSource, gotTarget)
	}
	if gotType != ah.RelationNameBeneficiallyOwns {
		t.Fatalf("expected OWNS, got %s", gotType)
	}
}
