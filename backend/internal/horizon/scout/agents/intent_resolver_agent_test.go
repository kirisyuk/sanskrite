package agents

import (
	"strings"
	"testing"
)

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

func TestSynthesizeSupplyChainExecutionPlan_IncludesFacilityAnchors(t *testing.T) {
	d := &DomainResolution{
		ContractType: "supply-chain",
		Facets: map[string][]string{
			"geo":     {"ru"},
			"product": {"oilfield equipment"},
		},
	}
	anchors := []struct {
		CanonicalPath string  `json:"canonicalPath"`
		Type          string  `json:"type"`
		Name          string  `json:"name,omitempty"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}{
		{CanonicalPath: "/fac/ae/jebel-ali-terminal", Type: "FACILITY", Confidence: 0.91, Reason: "relay terminal anchor"},
		{CanonicalPath: "/org/us/slb", Type: "ORGANIZATION", Confidence: 0.94, Reason: "subject anchor"},
	}

	plan := synthesizeSupplyChainExecutionPlan("SLB equipment routed via Jebel Ali into Russia", d, anchors, intentScenarioProfile{routeSensitive: true})
	if plan == nil || len(plan.Queries) == 0 {
		t.Fatalf("expected synthesized supply chain plan, got %#v", plan)
	}
	foundFacility := false
	for _, q := range plan.Queries {
		if q.Anchor == "/fac/ae/jebel-ali-terminal" {
			foundFacility = true
			if q.AnchorType != "fac" {
				t.Fatalf("expected facility anchorType, got %q", q.AnchorType)
			}
		}
	}
	if !foundFacility {
		t.Fatalf("expected facility anchor query in %#v", plan.Queries)
	}
}

func TestApplyIntentResolutionQualityRules_SynthesizesServicePlan(t *testing.T) {
	out := IntentResolution{
		Domains: []DomainResolution{{
			CanonicalPath: "/domain/v1/service-chain/geo/us/subject/google-cloud",
			ContractType:  "service-chain",
			Facets:        map[string][]string{"geo": {"us"}, "subject": {"google-cloud"}, "product": {"cloud-service"}},
		}},
		Anchors: []struct {
			CanonicalPath string  `json:"canonicalPath"`
			Type          string  `json:"type"`
			Name          string  `json:"name,omitempty"`
			Confidence    float64 `json:"confidence"`
			Reason        string  `json:"reason"`
		}{
			{CanonicalPath: "/org/us/google-cloud", Type: "ORGANIZATION", Confidence: 0.9},
			{CanonicalPath: "/fac/us/google-cloud-us-central1", Type: "FACILITY", Confidence: 0.8},
			{CanonicalPath: "/deposit/us/random-fuel-source", Type: "DEPOSIT", Confidence: 0.7},
		},
	}
	applyIntentResolutionQualityRules("Map the Google Cloud service chain for us-central1 including dependencies and downstream recipients", &out)
	if out.Domains[0].ServiceChainExecutionPlan == nil || len(out.Domains[0].ServiceChainExecutionPlan.Queries) == 0 {
		t.Fatalf("expected synthesized service chain plan")
	}
	if out.Domains[0].SupplyChainExecutionPlan != nil {
		t.Fatalf("service-chain domain must not receive supplyChainExecutionPlan")
	}
	for _, q := range out.Domains[0].ServiceChainExecutionPlan.Queries {
		if q.AnchorType == "deposit" {
			t.Fatalf("service-chain plan must not include deposit anchors: %#v", out.Domains[0].ServiceChainExecutionPlan.Queries)
		}
	}
	for _, entity := range out.Domains[0].FirstOrderEntities {
		if strings.HasPrefix(entity, "/deposit/") {
			t.Fatalf("service-chain firstOrderEntities must not include deposit anchors: %#v", out.Domains[0].FirstOrderEntities)
		}
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

func TestInferIntentScenarioProfile_DistinguishesServiceAndValueSignals(t *testing.T) {
	serviceProfile := inferIntentScenarioProfile("Map the service chain for Google Cloud data center services in the Gulf, including control gates and downstream recipients", nil)
	if !serviceProfile.service {
		t.Fatalf("expected service profile, got %+v", serviceProfile)
	}
	if serviceProfile.value {
		t.Fatalf("did not expect value signal in service query: %+v", serviceProfile)
	}

	valueProfile := inferIntentScenarioProfile("Map the cloud infrastructure value chain, profit pools, and bargaining power", nil)
	if !valueProfile.value {
		t.Fatalf("expected value profile, got %+v", valueProfile)
	}
	if valueProfile.service {
		t.Fatalf("did not expect service signal in value query: %+v", valueProfile)
	}
}

func TestPrioritizeIntentDomains_PrefersServiceChainForServiceSignals(t *testing.T) {
	profile := inferIntentScenarioProfile("Map the service chain for Google Cloud data center services in the Gulf, including control gates and downstream recipients", nil)
	domains := prioritizeIntentDomains([]DomainResolution{
		{ContractType: "value-chain", CanonicalPath: "/domain/v1/value-chain/subject/google-cloud"},
		{ContractType: "supply-chain", CanonicalPath: "/domain/v1/supply-chain/subject/google-cloud"},
		{ContractType: "service-chain", CanonicalPath: "/domain/v1/service-chain/subject/google-cloud"},
	}, profile)
	if got := normalizeContractTypeAlias(domains[0].ContractType); got != "service-chain" {
		t.Fatalf("expected service-chain to rank first, got %q", got)
	}
}

func TestPrioritizeIntentDomains_PrefersValueChainForValueSignals(t *testing.T) {
	profile := inferIntentScenarioProfile("Analyze the cloud value chain, profit pools, and value capture across providers and channel partners", nil)
	domains := prioritizeIntentDomains([]DomainResolution{
		{ContractType: "service-chain", CanonicalPath: "/domain/v1/service-chain/subject/google-cloud"},
		{ContractType: "supply-chain", CanonicalPath: "/domain/v1/supply-chain/subject/google-cloud"},
		{ContractType: "value-chain", CanonicalPath: "/domain/v1/value-chain/subject/google-cloud"},
	}, profile)
	if got := normalizeContractTypeAlias(domains[0].ContractType); got != "value-chain" {
		t.Fatalf("expected value-chain to rank first, got %q", got)
	}
}

func TestIntentResolverPrompts_IncludeServiceChainContract(t *testing.T) {
	unified := buildUnifiedIntentPrompt("Map the Google Cloud service chain in the Gulf", nil)
	if !strings.Contains(unified, "service-chain") || !strings.Contains(unified, "serviceChainExecutionPlan") {
		t.Fatalf("unified prompt must teach service-chain explicitly")
	}

	agentPrompt := (&IntentResolverAgent{}).buildPrompt("Map the Google Cloud service chain in the Gulf", nil)
	if !strings.Contains(agentPrompt, "service-chain") || !strings.Contains(agentPrompt, "serviceChainExecutionPlan") {
		t.Fatalf("agent prompt must teach service-chain explicitly")
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

func TestRepairSupplyChainDomainNamespacePath_RepairsServiceChainDomains(t *testing.T) {
	got := repairSupplyChainDomainNamespacePath("/domain/v1/service-chain/org/us/google-cloud/product/cloud-service")
	want := "/domain/v1/service-chain/geo/us/subject/google-cloud/product/cloud-service"
	if got != want {
		t.Fatalf("repairSupplyChainDomainNamespacePath() = %q, want %q", got, want)
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

func TestBuildLensTitleForIntent_PrefersExistingIntentTitle(t *testing.T) {
	got := buildLensTitleForIntent("  Gulf LNG Exposure  ", "ignored focus", "ignored query", []DomainResolution{{
		Title: "Ignored domain title",
	}})
	if got != "Gulf LNG Exposure" {
		t.Fatalf("buildLensTitleForIntent(existing) = %q, want %q", got, "Gulf LNG Exposure")
	}
}

func TestBuildLensTitleForIntent_FallsBackToDomainTitleThenFocus(t *testing.T) {
	got := buildLensTitleForIntent("", "qatar lng exposure through hormuz", "fallback query", []DomainResolution{{
		Title: "Qatar LNG Supply Chain",
	}})
	if got != "Qatar LNG Supply Chain" {
		t.Fatalf("buildLensTitleForIntent(domain) = %q, want %q", got, "Qatar LNG Supply Chain")
	}

	got = buildLensTitleForIntent("", "qatar lng exposure through hormuz", "fallback query", nil)
	if got != "Qatar lng exposure through hormuz" {
		t.Fatalf("buildLensTitleForIntent(focus) = %q, want %q", got, "Qatar lng exposure through hormuz")
	}
}
