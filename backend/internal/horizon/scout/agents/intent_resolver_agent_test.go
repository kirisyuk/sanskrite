package agents

import "testing"

func TestApplyIntentResolutionQualityRules_SynthesizesSupplyPlan(t *testing.T) {
	out := IntentResolution{
		Domains: []DomainResolution{{
			CanonicalPath: "/domain/v1/supply-chain/geo/qa/geo/eu/product/lng",
			ContractType:  "supply-chain",
			Facets:        map[string][]string{"geo": {"qa", "eu"}, "product": {"lng"}},
		}},
		Anchors: []struct {
			CanonicalPath string  `json:"canonicalPath"`
			Type          string  `json:"type"`
			Name          string  `json:"name,omitempty"`
			Confidence    float64 `json:"confidence"`
			Reason        string  `json:"reason"`
		}{
			{CanonicalPath: "/org/qa/qatarenergy", Type: "ORGANIZATION", Confidence: 0.9},
			{CanonicalPath: "/deposit/qa/north-field", Type: "DEPOSIT", Confidence: 0.8},
		},
	}
	applyIntentResolutionQualityRules("Map the Qatari LNG supply chain through the Strait of Hormuz to European industrial consumers", &out)
	if out.Domains[0].SupplyChainExecutionPlan == nil || len(out.Domains[0].SupplyChainExecutionPlan.Queries) == 0 {
		t.Fatalf("expected synthesized supply chain plan")
	}
	if got := out.Domains[0].SupplyChainExecutionPlan.GraphBuildMode; got != SupplyChainGraphBuildModeHotPathPlusGraph {
		t.Fatalf("expected default graphBuildMode %q, got %q", SupplyChainGraphBuildModeHotPathPlusGraph, got)
	}
	if got := out.Domains[0].SupplyChainExecutionPlan.Queries[0].Direction; got != "both" {
		t.Fatalf("expected direction both, got %q", got)
	}
	if len(out.Domains[0].SupplyChainExecutionPlan.OverlayQueries) == 0 {
		t.Fatalf("expected route-sensitive overlay query")
	}
}

func TestApplyIntentResolutionQualityRules_SynthesizesRegulatoryPlan(t *testing.T) {
	out := IntentResolution{
		Domains: []DomainResolution{{
			CanonicalPath: "/domain/v1/policy-impact/geo/eu/industry/steel",
			ContractType:  "policy-impact",
			Facets:        map[string][]string{"geo": {"eu"}, "industry": {"steel"}},
		}},
	}
	applyIntentResolutionQualityRules("What happens if the EU increases CBAM costs on imported steel?", &out)
	plan := out.Domains[0].RegulatoryImpactExecutionPlan
	if plan == nil || len(plan.Queries) == 0 {
		t.Fatalf("expected synthesized regulatory impact plan")
	}
	if got := plan.Queries[0].Instrument; got != "/mechanism/regulatory/carbon-border-adjustment" {
		t.Fatalf("unexpected mechanism: %q", got)
	}
}

func TestInferIntentScenarioProfile_ExpandsCapsForTransmissionScenarios(t *testing.T) {
	p := inferIntentScenarioProfile("What happens if military escalation around the Strait of Hormuz disrupts LNG shipments from Qatar for 90 days?", nil)
	if !p.routeSensitive || !p.affectedZone {
		t.Fatalf("expected route-sensitive affected-zone profile: %+v", p)
	}
	if p.anchorCap() < 8 {
		t.Fatalf("expected expanded anchor cap, got %d", p.anchorCap())
	}
	if p.domainCap() < 5 {
		t.Fatalf("expected expanded domain cap, got %d", p.domainCap())
	}
}

func TestRepairSupplyChainDomainNamespacePath_RewritesOrgNamespaceIntoFacets(t *testing.T) {
	got := repairSupplyChainDomainNamespacePath("/domain/v1/supply-chain/org/us/apple-inc/product/macbook")
	want := "/domain/v1/supply-chain/geo/us/subject/apple-inc/product/macbook"
	if got != want {
		t.Fatalf("repairSupplyChainDomainNamespacePath() = %q, want %q", got, want)
	}
}

func TestRepairSupplyChainDomainNamespacePath_LeavesNonSupplyChainDomainsUntouched(t *testing.T) {
	in := "/domain/v1/policy-impact/org/us/apple-inc"
	if got := repairSupplyChainDomainNamespacePath(in); got != in {
		t.Fatalf("repairSupplyChainDomainNamespacePath() = %q, want %q", got, in)
	}
}

func TestNormalizeSupplyChainGraphBuildMode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults", in: "", want: SupplyChainGraphBuildModeHotPathPlusGraph},
		{name: "hot path only", in: "hot_path_only", want: SupplyChainGraphBuildModeHotPathOnly},
		{name: "upper case normalizes", in: "HOT_PATH_PLUS_GRAPH", want: SupplyChainGraphBuildModeHotPathPlusGraph},
		{name: "unknown defaults", in: "graph_only", want: SupplyChainGraphBuildModeHotPathPlusGraph},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSupplyChainGraphBuildMode(tt.in); got != tt.want {
				t.Fatalf("normalizeSupplyChainGraphBuildMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
