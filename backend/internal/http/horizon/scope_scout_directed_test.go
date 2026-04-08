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

