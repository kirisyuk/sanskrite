package agents

import "testing"

func TestSynthesizeGovernanceExecutionPlan_CommunityDynastyAnchor(t *testing.T) {
	d := &DomainResolution{
		ContractType: "ownership-structure",
		Facets: map[string][]string{
			"geo": {"om"},
		},
	}
	anchors := []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}{
		{
			CanonicalPath: "/community/om/dynasty/al-said",
			Type:          "COMMUNITY",
			Name:          "Al Said",
			Confidence:    0.92,
			Reason:        "family dynasty anchor",
		},
	}

	plan := synthesizeGovernanceExecutionPlan("Oman families ownership structure by sector", d, anchors)
	if plan == nil || len(plan.Queries) != 1 {
		t.Fatalf("expected 1 governance query, got %#v", plan)
	}
	got := plan.Queries[0]
	if got.Target != "/community/om/dynasty/al-said" {
		t.Fatalf("unexpected target: %q", got.Target)
	}
	if got.TargetType != "community" {
		t.Fatalf("expected community targetType, got %q", got.TargetType)
	}
	if got.Direction != "downward" {
		t.Fatalf("expected downward direction for dynasty ownership map, got %q", got.Direction)
	}
}

func TestSynthesizeGovernanceExecutionPlan_ControlMapDefaultsToBothWithBoard(t *testing.T) {
	d := &DomainResolution{
		ContractType: "control-map",
		Facets: map[string][]string{
			"geo": {"sa"},
		},
	}
	anchors := []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}{
		{
			CanonicalPath: "/org/sa/aramco",
			Type:          "ORGANIZATION",
			Name:          "Aramco",
			Confidence:    0.95,
			Reason:        "organization anchor",
		},
	}

	plan := synthesizeGovernanceExecutionPlan("Saudi Aramco control map", d, anchors)
	if plan == nil || len(plan.Queries) != 1 {
		t.Fatalf("expected 1 governance query, got %#v", plan)
	}
	got := plan.Queries[0]
	if got.Direction != "both" {
		t.Fatalf("expected both direction for control map, got %q", got.Direction)
	}
	if !got.IncludeBoard {
		t.Fatal("expected control-map query to include board layer by default")
	}
}

func TestSynthesizeEcosystemExecutionPlan_CommunityFocusIncludesCommunityActors(t *testing.T) {
	d := &DomainResolution{
		Title: "Oman Family Influence",
		Facets: map[string][]string{
			"geo":    {"om"},
			"sector": {"energy"},
		},
	}
	anchors := []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}{
		{
			CanonicalPath: "/community/om/dynasty/al-said",
			Type:          "COMMUNITY",
			Name:          "Al Said",
			Confidence:    0.91,
			Reason:        "dynasty anchor",
		},
	}

	plan := synthesizeEcosystemExecutionPlan("Oman family influence abroad", d, anchors, intentScenarioProfile{ecosystem: true, investment: true})
	if plan == nil || len(plan.Queries) != 1 {
		t.Fatalf("expected 1 ecosystem query, got %#v", plan)
	}
	got := plan.Queries[0]
	if got.Focus != "/community/om/dynasty/al-said" {
		t.Fatalf("unexpected focus: %q", got.Focus)
	}
	if got.FocusType != "community" {
		t.Fatalf("expected community focusType, got %q", got.FocusType)
	}
	if !filterContains(got.ActorTypes, "COMMUNITY") {
		t.Fatalf("expected COMMUNITY actor type in %+v", got.ActorTypes)
	}
	if !filterContains(got.ActorTypes, "PERSON") {
		t.Fatalf("expected PERSON actor type in %+v", got.ActorTypes)
	}
}
