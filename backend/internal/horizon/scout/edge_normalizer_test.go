package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEdgeDirectionPolicies_EXTRACTS_FROM(t *testing.T) {
	// Verify EXTRACTS_FROM policy exists and is correct
	rule, exists := EdgeDirectionPolicies[models.DependencyTypeExtractsFrom]
	if !exists {
		t.Fatal("EXTRACTS_FROM edge direction policy not found")
	}

	// Verify valid source types (only FACILITY)
	if len(rule.ValidSourceTypes) != 1 {
		t.Errorf("EXTRACTS_FROM should have exactly 1 valid source type, got %d", len(rule.ValidSourceTypes))
	}
	if rule.ValidSourceTypes[0] != models.FragmentTypeFacility {
		t.Errorf("EXTRACTS_FROM source should be FACILITY, got %s", rule.ValidSourceTypes[0])
	}

	// Verify valid target types (only DEPOSIT)
	if len(rule.ValidTargetTypes) != 1 {
		t.Errorf("EXTRACTS_FROM should have exactly 1 valid target type, got %d", len(rule.ValidTargetTypes))
	}
	if rule.ValidTargetTypes[0] != models.FragmentTypeDeposit {
		t.Errorf("EXTRACTS_FROM target should be DEPOSIT, got %s", rule.ValidTargetTypes[0])
	}
}

func TestEdgeDirectionPolicies_OWNS_IncludesDeposit(t *testing.T) {
	// Verify OWNS policy includes DEPOSIT as valid target
	rule, exists := EdgeDirectionPolicies[models.DependencyTypeOwns]
	if !exists {
		t.Fatal("OWNS edge direction policy not found")
	}

	hasDeposit := false
	for _, targetType := range rule.ValidTargetTypes {
		if targetType == models.FragmentTypeDeposit {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit {
		t.Error("OWNS should allow DEPOSIT as target type")
	}
}

func TestEdgeDirectionPolicies_OWNS_IncludesCommunitySource(t *testing.T) {
	rule, exists := EdgeDirectionPolicies[models.DependencyTypeOwns]
	if !exists {
		t.Fatal("OWNS edge direction policy not found")
	}

	hasCommunity := false
	for _, sourceType := range rule.ValidSourceTypes {
		if sourceType == models.FragmentTypeCommunity {
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
	rule, exists := EdgeDirectionPolicies[models.DependencyTypeProduces]
	if !exists {
		t.Fatal("PRODUCES edge direction policy not found")
	}

	hasDeposit := false
	for _, sourceType := range rule.ValidSourceTypes {
		if sourceType == models.FragmentTypeDeposit {
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
	rule, exists := EdgeDirectionPolicies[models.DependencyTypeLocatedIn]
	if !exists {
		t.Fatal("LOCATED_IN edge direction policy not found")
	}

	hasDeposit := false
	for _, sourceType := range rule.ValidSourceTypes {
		if sourceType == models.FragmentTypeDeposit {
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
	rule, exists := EdgeDirectionPolicies[models.DependencyTypeOperatesIn]
	if !exists {
		t.Fatal("OPERATES_IN edge direction policy not found")
	}

	hasDeposit := false
	for _, targetType := range rule.ValidTargetTypes {
		if targetType == models.FragmentTypeDeposit {
			hasDeposit = true
			break
		}
	}
	if !hasDeposit {
		t.Error("OPERATES_IN should allow DEPOSIT as target type")
	}
}

func TestEdgeDirectionPolicies_CanonicalAndLegacyAliasCoverage(t *testing.T) {
	cases := []models.DependencyType{
		models.DependencyTypeAssociatedWith,
		models.DependencyTypeCorporateTransition,
		models.DependencyTypeDomainRelation,
		models.DependencyTypeSubsidiaryOf,
		models.DependencyTypeHasSubmarket,
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
		edgeType    models.DependencyType
		wantSource  string
		wantTarget  string
		wantSwapped bool
	}{
		{
			name:        "symmetric COMPETES_WITH - no swap needed",
			sourceID:    "aaa",
			targetID:    "bbb",
			edgeType:    models.DependencyTypeCompetesWith,
			wantSource:  "aaa",
			wantTarget:  "bbb",
			wantSwapped: false,
		},
		{
			name:        "symmetric COMPETES_WITH - swap needed",
			sourceID:    "bbb",
			targetID:    "aaa",
			edgeType:    models.DependencyTypeCompetesWith,
			wantSource:  "aaa",
			wantTarget:  "bbb",
			wantSwapped: true,
		},
		{
			name:        "non-symmetric OWNS - never swaps",
			sourceID:    "bbb",
			targetID:    "aaa",
			edgeType:    models.DependencyTypeOwns,
			wantSource:  "bbb",
			wantTarget:  "aaa",
			wantSwapped: false,
		},
		{
			name:        "non-symmetric EXTRACTS_FROM - never swaps",
			sourceID:    "facility-z",
			targetID:    "deposit-a",
			edgeType:    models.DependencyTypeExtractsFrom,
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
		edgeType   models.DependencyType
		sourceType models.FragmentType
		targetType models.FragmentType
		wantType   models.DependencyType
		wantSwap   bool
		wantChange bool
	}{
		{
			name:       "PRODUCT OWNS ORGANIZATION -> PRODUCES with swap",
			edgeType:   models.DependencyTypeOwns,
			sourceType: models.FragmentTypeProduct,
			targetType: models.FragmentTypeOrganization,
			wantType:   models.DependencyTypeProduces,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION OWNS PRODUCT stays OWNS (no canonicalization)",
			edgeType:   models.DependencyTypeOwns,
			sourceType: models.FragmentTypeOrganization,
			targetType: models.FragmentTypeProduct,
			wantType:   models.DependencyTypeOwns,
			wantSwap:   false,
			wantChange: false,
		},
		{
			name:       "PRODUCT PRODUCES ORGANIZATION (nonsense) is not rewritten here (direction rules handle it)",
			edgeType:   models.DependencyTypeProduces,
			sourceType: models.FragmentTypeProduct,
			targetType: models.FragmentTypeOrganization,
			wantType:   models.DependencyTypeProduces,
			wantSwap:   false,
			wantChange: false,
		},
		{
			name:       "PRODUCT PRODUCES PRODUCT -> TRANSFORMS_INTO",
			edgeType:   models.DependencyTypeProduces,
			sourceType: models.FragmentTypeProduct,
			targetType: models.FragmentTypeProduct,
			wantType:   models.DependencyTypeTransformsInto,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION EXTRACTS_FROM DEPOSIT -> OPERATES_IN (best-effort)",
			edgeType:   models.DependencyTypeExtractsFrom,
			sourceType: models.FragmentTypeOrganization,
			targetType: models.FragmentTypeDeposit,
			wantType:   models.DependencyTypeOperatesIn,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "DEPOSIT EXTRACTS_FROM ORGANIZATION -> OPERATES_IN with swap (best-effort)",
			edgeType:   models.DependencyTypeExtractsFrom,
			sourceType: models.FragmentTypeDeposit,
			targetType: models.FragmentTypeOrganization,
			wantType:   models.DependencyTypeOperatesIn,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ENTITY APPLIES_TO MECHANISM -> swap to MECHANISM APPLIES_TO ENTITY",
			edgeType:   models.DependencyTypeAppliesTo,
			sourceType: models.FragmentTypeOrganization,
			targetType: models.FragmentTypeMechanism,
			wantType:   models.DependencyTypeAppliesTo,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "Non-mechanism APPLIES_TO -> INFLUENCES",
			edgeType:   models.DependencyTypeAppliesTo,
			sourceType: models.FragmentTypeOrganization,
			targetType: models.FragmentTypeMarket,
			wantType:   models.DependencyTypeInfluences,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "COMMUNITY FOUNDED PERSON -> swap to PERSON FOUNDED COMMUNITY",
			edgeType:   models.DependencyTypeFounded,
			sourceType: models.FragmentTypeCommunity,
			targetType: models.FragmentTypePerson,
			wantType:   models.DependencyTypeFounded,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION FOUNDED PERSON -> swap to PERSON FOUNDED ORGANIZATION",
			edgeType:   models.DependencyTypeFounded,
			sourceType: models.FragmentTypeOrganization,
			targetType: models.FragmentTypePerson,
			wantType:   models.DependencyTypeFounded,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET (swap)",
			edgeType:   models.DependencyTypeInvestsIn,
			sourceType: models.FragmentTypeMarket,
			targetType: models.FragmentTypeOrganization,
			wantType:   models.DependencyTypeParticipatesIn,
			wantSwap:   true,
			wantChange: true,
		},
		{
			name:       "ORGANIZATION RELATED_TO MARKET -> ORGANIZATION PARTICIPATES_IN MARKET",
			edgeType:   models.DependencyTypeRelatedTo,
			sourceType: models.FragmentTypeOrganization,
			targetType: models.FragmentTypeMarket,
			wantType:   models.DependencyTypeParticipatesIn,
			wantSwap:   false,
			wantChange: true,
		},
		{
			name:       "MARKET RELATED_TO ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET (swap)",
			edgeType:   models.DependencyTypeRelatedTo,
			sourceType: models.FragmentTypeMarket,
			targetType: models.FragmentTypeOrganization,
			wantType:   models.DependencyTypeParticipatesIn,
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
	sourceFrag := &models.HorizonFragment{Type: models.FragmentTypeCommunity}

	gotSource, gotTarget, gotType, changed := normalizeRelationEdge(
		sourceFrag,
		"community-id",
		"org-id",
		models.DependencyTypeOwns,
		models.FragmentTypeOrganization,
		false,
		nil,
	)
	if changed {
		t.Fatalf("expected COMMUNITY -> ORGANIZATION OWNS to remain canonical")
	}
	if gotSource != "community-id" || gotTarget != "org-id" {
		t.Fatalf("expected source/target unchanged, got %q -> %q", gotSource, gotTarget)
	}
	if gotType != models.DependencyTypeOwns {
		t.Fatalf("expected OWNS, got %s", gotType)
	}
}
