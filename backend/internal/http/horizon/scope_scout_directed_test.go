package horizon

import "testing"

func TestNormalizeDirectedExploration(t *testing.T) {
	cfg, err := normalizeDirectedExploration(&scopeScoutDirectedExplorationRequest{
		DomainMode:          "supply_chain",
		Direction:           "up",
		RenderTarget:        "map",
		HotPathOnly:         true,
		HotPathNodeIDs:      []string{"frag-1", "frag-1", "frag-2", " "},
		IncludeCapabilities: true,
		IncludeEdgeMetrics:  true,
		CustomQuery:         "  check sanctions exposure  ",
		IgnoreFreshness:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
	if cfg.Direction != "upstream" {
		t.Fatalf("expected upstream direction, got %q", cfg.Direction)
	}
	if cfg.RenderTarget != "map" {
		t.Fatalf("expected map render target, got %q", cfg.RenderTarget)
	}
	if len(cfg.HotPathNodeIDs) != 2 || cfg.HotPathNodeIDs[0] != "frag-1" || cfg.HotPathNodeIDs[1] != "frag-2" {
		t.Fatalf("unexpected hot path node ids: %#v", cfg.HotPathNodeIDs)
	}
	if cfg.CustomQuery != "check sanctions exposure" {
		t.Fatalf("unexpected custom query %q", cfg.CustomQuery)
	}
}

func TestNormalizeDirectedExplorationRejectsInvalidConfig(t *testing.T) {
	cases := []scopeScoutDirectedExplorationRequest{
		{DomainMode: "governance", Direction: "upstream", RenderTarget: "graph"},
		{DomainMode: "supply_chain", Direction: "sideways", RenderTarget: "graph"},
		{DomainMode: "supply_chain", Direction: "downstream", RenderTarget: "canvas"},
	}
	for _, tc := range cases {
		if cfg, err := normalizeDirectedExploration(&tc); err == nil || cfg != nil {
			t.Fatalf("expected invalid config to fail: %#v", tc)
		}
	}
}

func TestIsAllowedSupplyChainAnchorPath(t *testing.T) {
	valid := []string{"/org/acme", "/gov/customs", "/fac/facility-x", "/product/widget", "/deposit/lithium"}
	for _, cp := range valid {
		if !isAllowedSupplyChainAnchorPath(cp) {
			t.Fatalf("expected %q to be allowed", cp)
		}
	}
	if isAllowedSupplyChainAnchorPath("/person/jane-doe") {
		t.Fatal("expected person path to be rejected")
	}
}

func TestShouldRouteDirectedExplorationToOneHop(t *testing.T) {
	cfg, err := normalizeDirectedExploration(&scopeScoutDirectedExplorationRequest{
		DomainMode:   "supply_chain",
		Direction:    "downstream",
		RenderTarget: "graph",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldRouteDirectedExplorationToOneHop("/org/acme", cfg) {
		t.Fatal("full downstream exploration must stay on recursive runtime")
	}
	if shouldRouteDirectedExplorationToOneHop("/fac/acme-plant", cfg) {
		t.Fatal("facility downstream exploration without hotPathOnly must stay recursive")
	}
	if shouldRouteDirectedExplorationToOneHop("/product/alumina", cfg) {
		t.Fatal("product anchor must not reroute to one-hop")
	}
	hotPathOnly, err := normalizeDirectedExploration(&scopeScoutDirectedExplorationRequest{
		DomainMode:   "supply_chain",
		Direction:    "downstream",
		RenderTarget: "graph",
		HotPathOnly:  true,
	})
	if err != nil {
		t.Fatalf("unexpected hot-path-only error: %v", err)
	}
	if !shouldRouteDirectedExplorationToOneHop("/org/acme", hotPathOnly) {
		t.Fatal("hot-path-only downstream exploration should reroute to one-hop")
	}
	upstream, err := normalizeDirectedExploration(&scopeScoutDirectedExplorationRequest{
		DomainMode:   "supply_chain",
		Direction:    "upstream",
		RenderTarget: "graph",
	})
	if err != nil {
		t.Fatalf("unexpected upstream error: %v", err)
	}
	if shouldRouteDirectedExplorationToOneHop("/org/acme", upstream) {
		t.Fatal("upstream exploration must stay on legacy directed flow")
	}
}

func TestBuildOneHopOptionsForDirectedExploration(t *testing.T) {
	org := buildOneHopOptionsForDirectedExploration("/org/acme")
	if org.AnchorType != "ORG" {
		t.Fatalf("expected ORG anchor type, got %q", org.AnchorType)
	}
	if org.MaxBranchesPerProduct <= 0 {
		t.Fatalf("expected positive max branches, got %d", org.MaxBranchesPerProduct)
	}
	if !org.IncludeInputsContext || !org.AllowOrgFallback || !org.AllowLLMFallback {
		t.Fatal("expected one-hop defaults to be enabled")
	}

	fac := buildOneHopOptionsForDirectedExploration("/fac/acme-plant")
	if fac.AnchorType != "FAC" {
		t.Fatalf("expected FAC anchor type, got %q", fac.AnchorType)
	}
}
