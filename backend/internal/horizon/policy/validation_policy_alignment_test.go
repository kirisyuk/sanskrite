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
	p := &ValidationPolicy{}

	if got := p.expectedPathPrefix(horizonmodels.FragmentTypeDomain); got != "/domain/" {
		t.Fatalf("expected DOMAIN prefix /domain/, got %q", got)
	}
	if got := p.expectedPathPrefix(horizonmodels.FragmentTypeDecisionPoint); got != "/decision/" {
		t.Fatalf("expected DECISION_POINT prefix /decision/, got %q", got)
	}
	if got := p.expectedPathPrefix(horizonmodels.FragmentTypeMechanism); got != "/mechanism/" {
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

func TestValidationPolicy_CompetesWithAndAuditedByRulesPresent(t *testing.T) {
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

func TestValidationPolicy_OrgConsumesOrgQuarantine(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(horizonmodels.FragmentTypeOrganization),
			"targetType":     string(horizonmodels.FragmentTypeOrganization),
			"relationType":   string(horizonmodels.DependencyTypeConsumes),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != "quarantine" {
		t.Fatalf("expected quarantine decision, got %s", decision.Action)
	}
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
	if decision.Action == "penalty" || decision.Action == "reject" {
		t.Fatalf("gov path should not be penalized/rejected, got %s", decision.Action)
	}
}
