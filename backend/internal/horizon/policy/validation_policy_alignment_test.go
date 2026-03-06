package policy

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type testValidationRulesRepo struct{}

func (testValidationRulesRepo) GetRules(context.Context) ([]*ValidationRule, error) {
	return nil, nil
}

func TestShouldValidateCanonicalPath_IncludesCommunityAndDomain(t *testing.T) {
	if !shouldValidateCanonicalPath("/community/sa/dynasty/house-of-saud") {
		t.Fatalf("expected /community/* paths to be canonical-path validated")
	}
	if !shouldValidateCanonicalPath("/domain/v1/supply-chain/geo/cn/application/ev") {
		t.Fatalf("expected /domain/* paths to be canonical-path validated")
	}
}

func TestValidationPolicy_ControlsAllowsCommunitySource(t *testing.T) {
	p := &ValidationPolicy{}

	valid, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeControls,
		horizonmodels.FragmentTypeCommunity,
		horizonmodels.FragmentTypeOrganization,
	)
	if !valid {
		t.Fatalf("expected CONTROLS COMMUNITY -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_ParticipatesInAllowsProductAndProgram(t *testing.T) {
	p := &ValidationPolicy{}

	validProduct, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeParticipatesIn,
		horizonmodels.FragmentTypeProduct,
		horizonmodels.FragmentTypeMarket,
	)
	if !validProduct {
		t.Fatalf("expected PARTICIPATES_IN PRODUCT -> MARKET to be valid")
	}

	validProgram, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeParticipatesIn,
		horizonmodels.FragmentTypeProgram,
		horizonmodels.FragmentTypeMarket,
	)
	if !validProgram {
		t.Fatalf("expected PARTICIPATES_IN PROGRAM -> MARKET to be valid")
	}
}

func TestValidationPolicy_HasDomainStrategicDirectionRules(t *testing.T) {
	p := &ValidationPolicy{}

	validDomainEdge, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeSurfaces,
		horizonmodels.FragmentTypeDomain,
		horizonmodels.FragmentTypeDecisionPoint,
	)
	if !validDomainEdge {
		t.Fatalf("expected SURFACES DOMAIN -> DECISION_POINT to be valid")
	}

	validStrategicEdge, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeControlsAccess,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeMarket,
	)
	if !validStrategicEdge {
		t.Fatalf("expected CONTROLS_ACCESS ORGANIZATION -> MARKET to be valid")
	}
}

func TestValidationPolicy_ExpectedPathPrefix_ExtendedTypes(t *testing.T) {
	if got := ExpectedCanonicalPathPrefix(horizonmodels.FragmentTypeDomain); got != "/domain/" {
		t.Fatalf("expected DOMAIN prefix /domain/, got %q", got)
	}
	if got := ExpectedCanonicalPathPrefix(horizonmodels.FragmentTypeDecisionPoint); got != "/decision/" {
		t.Fatalf("expected DECISION_POINT prefix /decision/, got %q", got)
	}
	if got := ExpectedCanonicalPathPrefix(horizonmodels.FragmentTypeMechanism); got != "/mechanism/" {
		t.Fatalf("expected MECHANISM prefix /mechanism/, got %q", got)
	}
}

func TestValidationPolicy_UnknownEdgeTypeIsNotFailOpen(t *testing.T) {
	p := &ValidationPolicy{}
	valid, _, rule := p.validateEdgeDirection(
		horizonmodels.DependencyType("UNKNOWN_EDGE_TYPE"),
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeOrganization,
	)
	if valid {
		t.Fatalf("expected unknown edge type to be invalid")
	}
	if rule == nil {
		t.Fatalf("expected explanation rule for unknown edge type")
	}
}

func TestValidationPolicy_ExtractsFromAndKeyPersonRulesPresent(t *testing.T) {
	p := &ValidationPolicy{}

	okExtracts, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeExtractsFrom,
		horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeDeposit,
	)
	if !okExtracts {
		t.Fatalf("expected EXTRACTS_FROM FACILITY -> DEPOSIT to be valid")
	}

	okKeyPerson, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeKeyPerson,
		horizonmodels.FragmentTypePerson,
		horizonmodels.FragmentTypeOrganization,
	)
	if !okKeyPerson {
		t.Fatalf("expected KEY_PERSON PERSON -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_CompetesWithAuditedByAndAssuresRulesPresent(t *testing.T) {
	p := &ValidationPolicy{}

	okCompetes, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeCompetesWith,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeOrganization,
	)
	if !okCompetes {
		t.Fatalf("expected COMPETES_WITH ORGANIZATION -> ORGANIZATION to be valid")
	}

	okAuditedBy, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeAuditedBy,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeOrganization,
	)
	if !okAuditedBy {
		t.Fatalf("expected AUDITED_BY ORGANIZATION -> ORGANIZATION to be valid")
	}

	okAssures, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeAssures,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeOrganization,
	)
	if !okAssures {
		t.Fatalf("expected ASSURES ORGANIZATION -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_RelatedAndRelativeRulesPresent(t *testing.T) {
	p := &ValidationPolicy{}

	okRelated, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeRelatedTo,
		horizonmodels.FragmentTypePerson,
		horizonmodels.FragmentTypePerson,
	)
	if !okRelated {
		t.Fatalf("expected RELATED_TO PERSON -> PERSON to be valid")
	}

	okRelative, _, _ := p.validateEdgeDirection(
		horizonmodels.DependencyTypeRelativeOf,
		horizonmodels.FragmentTypePerson,
		horizonmodels.FragmentTypePerson,
	)
	if !okRelative {
		t.Fatalf("expected RELATIVE_OF PERSON -> PERSON to be valid")
	}
}

func TestValidationPolicy_InfluencesPersonToSphereAllowed(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypePerson),
			"targetType":     string(horizonmodels.FragmentTypeSphere),
			"relationType":   string(horizonmodels.DependencyTypeInfluences),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != "allow" {
		t.Fatalf("expected allow decision, got %s", decision.Action)
	}
}

func TestValidationPolicy_InfluencesOrgToSphereGetsPenaltyPotential(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeSphere),
			"relationType":   string(horizonmodels.DependencyTypeInfluences),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != "allow_with_penalty" {
		t.Fatalf("expected allow_with_penalty decision, got %s", decision.Action)
	}
	if got, _ := decision.Params["edgeModality"].(string); got != "potential" {
		t.Fatalf("expected edgeModality=potential, got %q", got)
	}
}

func TestValidationPolicy_OrgOrgInvestsInRequiresDirectionEvidence(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeInvestsIn),
			"reason":         "Vanguard invested in TSMC",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for ambiguous ORG->ORG INVESTS_IN, got %s", ActionQuarantineSoft, decision.Action)
	}

	decision2, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeInvestsIn),
			"reason":         "Vanguard invested in TSMC",
			"qualifiers": map[string]interface{}{
				"source_role": "investor",
				"target_role": "investee",
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide() with roles error = %v", err)
	}
	if decision2.Action == ActionQuarantineSoft {
		t.Fatalf("expected non-quarantine with explicit investor/investee roles, got %s", decision2.Action)
	}
}

func TestValidationPolicy_OrgOrgAssuresPassiveCueWithoutRolesIsQuarantined(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeAssures),
			"reason":         "TSMC is audited by Deloitte",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for passive ambiguous ASSURES without roles, got %s", ActionQuarantineSoft, decision.Action)
	}
}

func TestValidationPolicy_DependsOnActorPairRequiresDirectionEvidence(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeDependsOn),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for ambiguous ORG->ORG DEPENDS_ON, got %s", ActionQuarantineSoft, decision.Action)
	}

	decision2, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeDependsOn),
			"qualifiers": map[string]interface{}{
				"source_role": "dependent",
				"target_role": "dependency",
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide() with roles error = %v", err)
	}
	if decision2.Action == ActionQuarantineSoft {
		t.Fatalf("expected non-quarantine with explicit dependent/dependency roles, got %s", decision2.Action)
	}
}

func TestValidationPolicy_SuppliesToActorPairRequiresDirectionEvidence(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeSuppliesTo),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for ambiguous ORG->ORG SUPPLIES_TO, got %s", ActionQuarantineSoft, decision.Action)
	}

	decision2, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeSuppliesTo),
			"qualifiers": map[string]interface{}{
				"source_role": "supplier",
				"target_role": "customer",
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide() with roles error = %v", err)
	}
	if decision2.Action == ActionQuarantineSoft {
		t.Fatalf("expected non-quarantine with explicit supplier/customer roles, got %s", decision2.Action)
	}
}

func TestValidationPolicy_OrgConsumesOrgQuarantine(t *testing.T) {
	// CONSUMES removed from taxonomy; legacy CONSUMES is canonicalized to SUPPLIES_TO before validation.
	// This test now verifies MARKET->MARKET PARTICIPATES_IN is quarantined (same pattern).
	t.Skip("CONSUMES removed; test superseded by other quarantine tests")
}

func TestValidationPolicy_GovPathOrgWhitelist(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "fragment",
		Fragment: &horizonmodels.HorizonFragment{
			Type:          horizonmodels.FragmentTypeOrganization,
			CanonicalPath: "/gov/sa/ministry-of-finance",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action == "penalty" || decision.Action == "reject" || decision.Action == "quarantine_soft" {
		t.Fatalf("gov path should not be penalized/rejected, got %s", decision.Action)
	}
}

func TestValidationPolicy_OrgMarketNonParticipatesQuarantine(t *testing.T) {
	// CONSUMES removed; ORG->MARKET with invalid type. Use MARKET->MARKET PARTICIPATES_IN for quarantine test.
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(horizonmodels.FragmentTypeMarket),
			"targetType":          string(horizonmodels.FragmentTypeMarket),
			"sourceCanonicalPath": "/market/global/materials",
			"targetCanonicalPath": "/market/global/materials/raw-materials",
			"relationType":        string(horizonmodels.DependencyTypeParticipatesIn),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected quarantine_soft for MARKET->MARKET PARTICIPATES_IN, got %s", decision.Action)
	}
}

func TestValidationPolicy_GovRegulatesMarketAllowed(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(horizonmodels.FragmentTypeOrganization),
			"targetType":          string(horizonmodels.FragmentTypeMarket),
			"sourceCanonicalPath": "/gov/us/federal-reserve",
			"targetCanonicalPath": "/market/us/finance/banking",
			"relationType":        string(horizonmodels.DependencyTypeRegulates),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action == ActionQuarantineSoft || decision.Action == "reject" {
		t.Fatalf("GOV REGULATES MARKET should be allowed, got %s (reason: %v)", decision.Action, decision.Params["reason"])
	}
}

func TestValidationPolicy_OrgNonGovMarketStillQuarantine(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(horizonmodels.FragmentTypeOrganization),
			"targetType":          string(horizonmodels.FragmentTypeMarket),
			"sourceCanonicalPath": "/org/us/jpmorgan-chase",
			"targetCanonicalPath": "/market/us/finance/banking",
			"relationType":        string(horizonmodels.DependencyTypeRegulates),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("non-GOV ORG REGULATES MARKET should be quarantined, got %s", decision.Action)
	}
}

func TestValidationPolicy_FragmentLegacyPathRejectedByContract(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	tests := []struct {
		name         string
		fragmentType horizonmodels.FragmentType
		path         string
		wantDecision string
	}{
		{
			name:         "legacy org path",
			fragmentType: horizonmodels.FragmentTypeOrganization,
			path:         "/org/acme",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy market path",
			fragmentType: horizonmodels.FragmentTypeMarket,
			path:         "/market/semiconductors",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy program path",
			fragmentType: horizonmodels.FragmentTypeProgram,
			path:         "/program/alem-ai",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy community path",
			fragmentType: horizonmodels.FragmentTypeCommunity,
			path:         "/community/ae/hawala-network",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy deposit path",
			fragmentType: horizonmodels.FragmentTypeDeposit,
			path:         "/deposit/permian-basin",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := p.Decide(context.Background(), PolicyInput{
				Kind: "fragment",
				Fragment: &horizonmodels.HorizonFragment{
					Type:          tt.fragmentType,
					CanonicalPath: tt.path,
				},
			})
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.Action != ActionQuarantineSoft {
				t.Fatalf("expected %s, got %s", ActionQuarantineSoft, decision.Action)
			}
			if decision.DecisionPath != tt.wantDecision {
				t.Fatalf("expected decisionPath %q, got %q", tt.wantDecision, decision.DecisionPath)
			}
		})
	}
}
