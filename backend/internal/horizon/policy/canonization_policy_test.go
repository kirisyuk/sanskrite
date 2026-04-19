package policy

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
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
		want ah.FragmentClass
	}{
		{in: "ORGANIZATION", want: classOrganization},
		{in: "ORG", want: classOrganization},
		{in: "GOV", want: classOrganization},
		{in: "GOVERNMENT", want: classOrganization},
		{in: "FACILITY", want: classFacility},
		{in: "FAC", want: classFacility},
		{in: "GEO_REGION", want: classGeoRegion},
		{in: "region", want: classGeoRegion},
		{in: "LOCATION", want: classGeoRegion},
		{in: "PRODUCT", want: classProduct},
		{in: "PROGRAM", want: classProgram},
		{in: "MARKET", want: classMarket},
		{in: "EVENT", want: classEvent},
		{in: "PERSON", want: classPerson},
		{in: "INDIVIDUAL", want: classPerson},
		{in: "MEDIA_CHANNEL", want: classMediaChannel},
		{in: "MEDIA", want: classMediaChannel},
		{in: "DEPOSIT", want: classDeposit},
		{in: "MINE", want: classDeposit},
		{in: "DOMAIN", want: classDomain},
		{in: "SPHERE", want: classSphere},
		{in: "DECISION_POINT", want: classDecisionPoint},
		{in: "DECISION", want: classDecisionPoint},
		{in: "MECHANISM", want: classMechanism},
		{in: "SUPPLY_LINK", want: classSupplyLink},
		{in: "SUPPLYLINK", want: classSupplyLink},
		{in: "COMMUNITY", want: classCommunity},
		{in: "NETWORK", want: classCommunity},
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

	if got := p.normalizeFragmentType("/gov/na/eag"); got != classOrganization {
		t.Fatalf("gov path should map to ORGANIZATION, got %q", got)
	}
	if got := p.normalizeFragmentType("/mechanism/regulatory/export-control"); got != classMechanism {
		t.Fatalf("mechanism path should map to MECHANISM, got %q", got)
	}
}

func TestCanonizationPolicy_NormalizeFragmentType_UnknownFallsBackToOrganization(t *testing.T) {
	p := NewCanonizationPolicy(canonRulesRepoStub{})
	got := p.normalizeFragmentType("SOMETHING_ENTIRELY_UNKNOWN")
	if got == "" {
		t.Fatalf("normalizeFragmentType must never return empty")
	}
	if got != classOrganization {
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
	if out != string(classMechanism) {
		t.Fatalf("expected hardcoded fallback output %q, got %q", classMechanism, out)
	}
}
