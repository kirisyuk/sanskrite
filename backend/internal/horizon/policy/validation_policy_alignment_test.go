package policy

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
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
		relControls,
		classCommunity,
		classOrganization,
	)
	if !valid {
		t.Fatalf("expected CONTROLS COMMUNITY -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_OwnsAllowsCommunitySource(t *testing.T) {
	p := &ValidationPolicy{}

	valid, _, _ := p.validateEdgeDirection(
		relOwns,
		classCommunity,
		classOrganization,
	)
	if !valid {
		t.Fatalf("expected OWNS COMMUNITY -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_ProductMarketUsesTradedInAndProgramUsesParticipatesIn(t *testing.T) {
	p := &ValidationPolicy{}

	validProductParticipates, _, _ := p.validateEdgeDirection(
		relParticipatesIn,
		classProduct,
		classMarket,
	)
	if validProductParticipates {
		t.Fatalf("expected PARTICIPATES_IN PRODUCT -> MARKET to be invalid")
	}

	validProductTradedIn, _, _ := p.validateEdgeDirection(
		relTradedIn,
		classProduct,
		classMarket,
	)
	if !validProductTradedIn {
		t.Fatalf("expected TRADED_IN PRODUCT -> MARKET to be valid")
	}

	validProgram, _, _ := p.validateEdgeDirection(
		relParticipatesIn,
		classProgram,
		classMarket,
	)
	if !validProgram {
		t.Fatalf("expected PARTICIPATES_IN PROGRAM -> MARKET to be valid")
	}
}

func TestValidationPolicy_DecideQuarantinesProductMarketNonTradedIn(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(classProduct),
			"targetType":     string(classMarket),
			"relationType":   string(relParticipatesIn),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for PRODUCT->MARKET PARTICIPATES_IN, got %s", ActionQuarantineSoft, decision.Action)
	}
}

func TestValidationPolicy_HasDomainStrategicDirectionRules(t *testing.T) {
	p := &ValidationPolicy{}

	validDomainEdge, _, _ := p.validateEdgeDirection(
		relSurfaces,
		classDomain,
		classDecisionPoint,
	)
	if !validDomainEdge {
		t.Fatalf("expected SURFACES DOMAIN -> DECISION_POINT to be valid")
	}

	validStrategicEdge, _, _ := p.validateEdgeDirection(
		relControlsAccess,
		classOrganization,
		classMarket,
	)
	if !validStrategicEdge {
		t.Fatalf("expected CONTROLS_ACCESS ORGANIZATION -> MARKET to be valid")
	}
}

func TestValidationPolicy_ExpectedPathPrefix_ExtendedTypes(t *testing.T) {
	if got := ExpectedCanonicalPathPrefix(classEvidence, ""); got != "/evidence/" {
		t.Fatalf("expected EVIDENCE prefix /evidence/, got %q", got)
	}
	if got := ExpectedCanonicalPathPrefix(classDecisionPoint, ""); got != "/decision/" {
		t.Fatalf("expected DECISION_POINT prefix /decision/, got %q", got)
	}
	if got := ExpectedCanonicalPathPrefix(classMechanism, ""); got != "/mechanism/" {
		t.Fatalf("expected MECHANISM prefix /mechanism/, got %q", got)
	}
}

func TestValidationPolicy_UnknownEdgeTypeIsNotFailOpen(t *testing.T) {
	p := &ValidationPolicy{}
	valid, _, rule := p.validateEdgeDirection(
		ah.RelationName("UNKNOWN_EDGE_TYPE"),
		classOrganization,
		classOrganization,
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
		relExtractsFrom,
		classFacility,
		classDeposit,
	)
	if !okExtracts {
		t.Fatalf("expected EXTRACTS_FROM FACILITY -> deposit to be valid")
	}

	okKeyPerson, _, _ := p.validateEdgeDirection(
		relKeyPerson,
		classPerson,
		classOrganization,
	)
	if !okKeyPerson {
		t.Fatalf("expected KEY_PERSON PERSON -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_CompetesWithAuditedByAndAssuresRulesPresent(t *testing.T) {
	p := &ValidationPolicy{}

	okCompetes, _, _ := p.validateEdgeDirection(
		relCompetesWith,
		classOrganization,
		classOrganization,
	)
	if !okCompetes {
		t.Fatalf("expected COMPETES_WITH ORGANIZATION -> ORGANIZATION to be valid")
	}

	okAuditedBy, _, _ := p.validateEdgeDirection(
		relAuditedBy,
		classOrganization,
		classOrganization,
	)
	if !okAuditedBy {
		t.Fatalf("expected AUDITED_BY ORGANIZATION -> ORGANIZATION to be valid")
	}

	okAssures, _, _ := p.validateEdgeDirection(
		relAssures,
		classOrganization,
		classOrganization,
	)
	if !okAssures {
		t.Fatalf("expected ASSURES ORGANIZATION -> ORGANIZATION to be valid")
	}
}

func TestValidationPolicy_RelatedAndRelativeRulesPresent(t *testing.T) {
	p := &ValidationPolicy{}

	okRelated, _, _ := p.validateEdgeDirection(
		relRelatedTo,
		classPerson,
		classPerson,
	)
	if !okRelated {
		t.Fatalf("expected RELATED_TO PERSON -> PERSON to be valid")
	}

	okRelative, _, _ := p.validateEdgeDirection(
		relRelativeOf,
		classPerson,
		classPerson,
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
			"sourceType":     string(classPerson),
			"targetType":     string(classEvidence),
			"relationType":   string(relInfluences),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classEvidence),
			"relationType":   string(relInfluences),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relInvestsIn),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relInvestsIn),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relAssures),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relDependsOn),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relDependsOn),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relSuppliesTo),
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
			"sourceType":     string(classOrganization),
			"targetType":     string(classOrganization),
			"relationType":   string(relSuppliesTo),
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

func TestValidationPolicy_ControlsAccessRequiresDirectionEvidence(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(classOrganization),
			"targetType":          string(classMarket),
			"sourceCanonicalPath": "/gov/ae/ministry-of-finance",
			"targetCanonicalPath": "/market/ae/capital/foreign-direct-investment",
			"relationType":        string(relControlsAccess),
			"reason":              "The ministry can restrict foreign investor access to the market.",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for CONTROLS_ACCESS without policy roles, got %s", ActionQuarantineSoft, decision.Action)
	}

	decision2, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(classOrganization),
			"targetType":          string(classMarket),
			"sourceCanonicalPath": "/gov/ae/ministry-of-finance",
			"targetCanonicalPath": "/market/ae/capital/foreign-direct-investment",
			"relationType":        string(relControlsAccess),
			"reason":              "The ministry can restrict foreign investor access to the market.",
			"qualifiers": map[string]interface{}{
				"source_role":     "controller",
				"target_role":     "access_subject",
				"direction_basis": "controller_to_access_subject",
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide() with roles error = %v", err)
	}
	if decision2.Action == ActionQuarantineSoft {
		t.Fatalf("expected non-quarantine with explicit controller/access_subject roles, got %s", decision2.Action)
	}
}

func TestValidationPolicy_ImposesConditionMechanismRolesAllow(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(classMechanism),
			"targetType":          string(classOrganization),
			"sourceCanonicalPath": "/mechanism/regulatory/corporate-income-tax-rate-increase",
			"targetCanonicalPath": "/org/ae/dubai-international-financial-centre",
			"relationType":        string(relImposesCondition),
			"reason":              "The rate increase imposes a higher tax burden on DIFC entities.",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected %s for IMPOSES_CONDITION without policy roles, got %s", ActionQuarantineSoft, decision.Action)
	}

	decision2, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(classMechanism),
			"targetType":          string(classOrganization),
			"sourceCanonicalPath": "/mechanism/regulatory/corporate-income-tax-rate-increase",
			"targetCanonicalPath": "/org/ae/dubai-international-financial-centre",
			"relationType":        string(relImposesCondition),
			"reason":              "The rate increase imposes a higher tax burden on DIFC entities.",
			"qualifiers": map[string]interface{}{
				"source_role":     "mechanism",
				"target_role":     "conditioned_entity",
				"direction_basis": "mechanism_to_constrained_entity",
			},
		},
	})
	if err != nil {
		t.Fatalf("Decide() with roles error = %v", err)
	}
	if decision2.Action == ActionQuarantineSoft {
		t.Fatalf("expected non-quarantine with explicit imposer/constrained_entity roles, got %s", decision2.Action)
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
			Class:         classOrganization,
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
			"sourceType":          string(classMarket),
			"targetType":          string(classMarket),
			"sourceCanonicalPath": "/market/global/materials",
			"targetCanonicalPath": "/market/global/materials/raw-materials",
			"relationType":        string(relParticipatesIn),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionQuarantineSoft {
		t.Fatalf("expected quarantine_soft for MARKET->MARKET PARTICIPATES_IN, got %s", decision.Action)
	}
}

func TestValidationPolicy_DepositToOrgSuppliesToGetsPenaltyInsteadOfQuarantine(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind": "edge",
			"sourceType":     string(classDeposit),
			"targetType":     string(classOrganization),
			"relationType":   string(relSuppliesTo),
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if decision.Action != ActionAllowWithPenalty {
		t.Fatalf("expected %s for deposit->ORG SUPPLIES_TO shortcut, got %s", ActionAllowWithPenalty, decision.Action)
	}
	if got, _ := decision.Params["edgeModality"].(string); got != "potential" {
		t.Fatalf("expected edgeModality=potential, got %q", got)
	}
}

func TestValidationPolicy_GovRegulatesMarketAllowed(t *testing.T) {
	p := &ValidationPolicy{rulesRepo: testValidationRulesRepo{}}
	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceType":          string(classOrganization),
			"targetType":          string(classMarket),
			"sourceCanonicalPath": "/gov/us/federal-reserve",
			"targetCanonicalPath": "/market/us/finance/banking",
			"relationType":        string(relRegulates),
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
			"sourceType":          string(classOrganization),
			"targetType":          string(classMarket),
			"sourceCanonicalPath": "/org/us/jpmorgan-chase",
			"targetCanonicalPath": "/market/us/finance/banking",
			"relationType":        string(relRegulates),
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
		fragmentType ah.FragmentClass
		path         string
		wantDecision string
	}{
		{
			name:         "legacy org path",
			fragmentType: classOrganization,
			path:         "/org/acme",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy market path",
			fragmentType: classMarket,
			path:         "/market/semiconductors",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy program path",
			fragmentType: classProgram,
			path:         "/program/alem-ai",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy community path",
			fragmentType: classCommunity,
			path:         "/community/ae/hawala-network",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
		{
			name:         "legacy deposit path",
			fragmentType: classDeposit,
			path:         "/deposit/permian-basin",
			wantDecision: "rule:hardcoded:invalid_canonical_path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := p.Decide(context.Background(), PolicyInput{
				Kind: "fragment",
				Fragment: &horizonmodels.HorizonFragment{
					Class:         tt.fragmentType,
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
