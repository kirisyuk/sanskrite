package policy

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type canonRulesRepoStub struct {
	rules []*CanonizationRule
	err   error
}

func (s canonRulesRepoStub) GetRules(context.Context) ([]*CanonizationRule, error) {
	return s.rules, s.err
}

func TestCanonizationPolicy_NormalizeFragmentType_MatrixCoverage(t *testing.T) {
	p := NewCanonizationPolicy(canonRulesRepoStub{})

	tests := []struct {
		in   string
		want horizonmodels.FragmentType
	}{
		{in: "ORGANIZATION", want: horizonmodels.FragmentTypeOrganization},
		{in: "ORG", want: horizonmodels.FragmentTypeOrganization},
		{in: "GOV", want: horizonmodels.FragmentTypeOrganization},
		{in: "GOVERNMENT", want: horizonmodels.FragmentTypeOrganization},
		{in: "FACILITY", want: horizonmodels.FragmentTypeFacility},
		{in: "FAC", want: horizonmodels.FragmentTypeFacility},
		{in: "GEO_REGION", want: horizonmodels.FragmentTypeGeoRegion},
		{in: "region", want: horizonmodels.FragmentTypeGeoRegion},
		{in: "LOCATION", want: horizonmodels.FragmentTypeGeoRegion},
		{in: "PRODUCT", want: horizonmodels.FragmentTypeProduct},
		{in: "PROGRAM", want: horizonmodels.FragmentTypeProgram},
		{in: "MARKET", want: horizonmodels.FragmentTypeMarket},
		{in: "EVENT", want: horizonmodels.FragmentTypeEvent},
		{in: "PERSON", want: horizonmodels.FragmentTypePerson},
		{in: "INDIVIDUAL", want: horizonmodels.FragmentTypePerson},
		{in: "MEDIA_CHANNEL", want: horizonmodels.FragmentTypeMediaChannel},
		{in: "MEDIA", want: horizonmodels.FragmentTypeMediaChannel},
		{in: "DEPOSIT", want: horizonmodels.FragmentTypeDeposit},
		{in: "MINE", want: horizonmodels.FragmentTypeDeposit},
		{in: "DOMAIN", want: horizonmodels.FragmentTypeDomain},
		{in: "SPHERE", want: horizonmodels.FragmentTypeSphere},
		{in: "DECISION_POINT", want: horizonmodels.FragmentTypeDecisionPoint},
		{in: "DECISION", want: horizonmodels.FragmentTypeDecisionPoint},
		{in: "MECHANISM", want: horizonmodels.FragmentTypeMechanism},
		{in: "SUPPLY_LINK", want: horizonmodels.FragmentTypeSupplyLink},
		{in: "SUPPLYLINK", want: horizonmodels.FragmentTypeSupplyLink},
		{in: "COMMUNITY", want: horizonmodels.FragmentTypeCommunity},
		{in: "NETWORK", want: horizonmodels.FragmentTypeCommunity},
	}

	for _, tt := range tests {
		got := p.normalizeFragmentType(tt.in)
		if got != tt.want {
			t.Fatalf("normalizeFragmentType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCanonizationPolicy_NormalizeFragmentType_PathInference(t *testing.T) {
	p := NewCanonizationPolicy(canonRulesRepoStub{})

	if got := p.normalizeFragmentType("/gov/na/eag"); got != horizonmodels.FragmentTypeOrganization {
		t.Fatalf("gov path should map to ORGANIZATION, got %q", got)
	}
	if got := p.normalizeFragmentType("/mechanism/regulatory/export-control"); got != horizonmodels.FragmentTypeMechanism {
		t.Fatalf("mechanism path should map to MECHANISM, got %q", got)
	}
}

func TestCanonizationPolicy_NormalizeFragmentType_UnknownFallsBackToOrganization(t *testing.T) {
	p := NewCanonizationPolicy(canonRulesRepoStub{})
	got := p.normalizeFragmentType("SOMETHING_ENTIRELY_UNKNOWN")
	if got == "" {
		t.Fatalf("normalizeFragmentType must never return empty")
	}
	if got != horizonmodels.FragmentTypeOrganization {
		t.Fatalf("unknown type should fall back to ORGANIZATION, got %q", got)
	}
}

func TestCanonizationPolicy_Decide_PreventsEmptyCanonicalPathOutput(t *testing.T) {
	p := NewCanonizationPolicy(canonRulesRepoStub{})

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "canonization",
		Context: map[string]interface{}{
			"inputType":  "canonical_path",
			"inputValue": "/region/na",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	out, _ := decision.Params["output"].(string)
	if out == "" {
		t.Fatalf("canonization output must never be empty")
	}
	if out != "/region/na" {
		t.Fatalf("expected fallback to original canonical path, got %q", out)
	}
}

func TestCanonizationPolicy_Decide_SkipsEmptyDbRuleOutput(t *testing.T) {
	p := NewCanonizationPolicy(canonRulesRepoStub{
		rules: []*CanonizationRule{
			{
				ID:           "bad-empty",
				InputType:    "fragment_type",
				InputPattern: "MECHANISM",
				Output:       "",
			},
		},
	})

	decision, err := p.Decide(context.Background(), PolicyInput{
		Kind: "canonization",
		Context: map[string]interface{}{
			"inputType":  "fragment_type",
			"inputValue": "MECHANISM",
		},
	})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	out, _ := decision.Params["output"].(string)
	if out != string(horizonmodels.FragmentTypeMechanism) {
		t.Fatalf("expected hardcoded fallback output %q, got %q", horizonmodels.FragmentTypeMechanism, out)
	}
}
