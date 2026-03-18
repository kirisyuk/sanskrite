package scout

import (
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestBuildDirectedSupplyChainQueriesUsesDirectedState(t *testing.T) {
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"maxRecursionDepth":     2,
				"hotPathCanonicalPaths": []string{"/org/hot-a", "/org/hot-a", "/fac/site-b", "/person/nope"},
				"directedExploration": horizonmodels.DirectedExplorationConfig{
					DomainMode:      "supply_chain",
					Direction:       "upstream",
					CustomQuery:     "focus on export controls",
					IgnoreFreshness: true,
				},
			},
		},
		TargetCanonicalPath: "/org/anchor-co",
	}

	queries := buildDirectedSupplyChainQueries(job)
	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(queries))
	}

	query := queries[0]
	if query.Direction != "upstream" {
		t.Fatalf("expected upstream direction, got %q", query.Direction)
	}
	if query.Depth != 4 {
		t.Fatalf("expected derived depth 4, got %d", query.Depth)
	}
	if query.Anchor != "" {
		t.Fatalf("expected anchor to be cleared when hot path anchors exist for non-product target, got %q", query.Anchor)
	}
	if query.AnchorType != "org" {
		t.Fatalf("expected org anchor type, got %q", query.AnchorType)
	}
	if len(query.Anchors) != 2 || query.Anchors[0] != "/org/hot-a" || query.Anchors[1] != "/fac/site-b" {
		t.Fatalf("unexpected anchors: %#v", query.Anchors)
	}
	if !strings.Contains(query.Reason, "focus on export controls") {
		t.Fatalf("expected custom query in reason, got %q", query.Reason)
	}
	if !directedExplorationShouldIgnoreFreshness(job) {
		t.Fatal("expected directed exploration to ignore freshness")
	}
}

func TestDirectedExplorationAnchorPathHelpers(t *testing.T) {
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"directedExploration": map[string]any{
					"domainMode":      "supply_chain",
					"direction":       "downstream",
					"renderTarget":    "map",
					"hotPathOnly":     true,
					"customQuery":     "priority to chokepoints",
					"ignoreFreshness": false,
				},
				"ignoreFreshnessForUserRequest": true,
			},
		},
		TargetCanonicalPath: "/product/widget-core",
	}

	if !isDirectedSupplyChainExplorationJob(job) {
		t.Fatal("expected job to be recognized as directed supply-chain exploration")
	}
	if directedExplorationRenderTarget(job) != "map" {
		t.Fatalf("expected map render target, got %q", directedExplorationRenderTarget(job))
	}
	if !directedExplorationHotPathOnly(job) {
		t.Fatal("expected hot-path-only flag")
	}
	if directedExplorationDirection(job) != "downstream" {
		t.Fatalf("expected downstream direction, got %q", directedExplorationDirection(job))
	}
	if !directedExplorationShouldIgnoreFreshness(job) {
		t.Fatal("expected legacy ignoreFreshnessForUserRequest fallback to apply")
	}
	if supplyChainAnchorTypeForPath("/deposit/lithium-basin") != "deposit" {
		t.Fatalf("expected deposit anchor type, got %q", supplyChainAnchorTypeForPath("/deposit/lithium-basin"))
	}
	if supplyChainAnchorTypeForPath("/gov/customs-ministry") != "org" {
		t.Fatalf("expected gov anchors to collapse to org execution type, got %q", supplyChainAnchorTypeForPath("/gov/customs-ministry"))
	}
}

func TestDirectedSupplyChainExplorationToolsSelection(t *testing.T) {
	upstream := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"directedExploration": horizonmodels.DirectedExplorationConfig{
					DomainMode: "supply_chain",
					Direction:  "upstream",
				},
			},
		},
	}
	tools := directedSupplyChainExplorationTools(upstream)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools for upstream run, got %d", len(tools))
	}
	if tools[0].Name() != "supply_chain.hot_path" || tools[1].Name() != "supply_chain.upstream_unwind" {
		t.Fatalf("unexpected upstream tool chain: %s -> %s", tools[0].Name(), tools[1].Name())
	}

	hotPathOnly := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{
				"directedExploration": horizonmodels.DirectedExplorationConfig{
					DomainMode:  "supply_chain",
					Direction:   "downstream",
					HotPathOnly: true,
				},
			},
		},
	}
	tools = directedSupplyChainExplorationTools(hotPathOnly)
	if len(tools) != 1 || tools[0].Name() != "supply_chain.hot_path" {
		t.Fatalf("expected hot-path-only chain, got %#v", tools)
	}
}

func TestAppendDirectedExplorationToolRun(t *testing.T) {
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			State: map[string]interface{}{},
		},
	}
	startedAt := time.Date(2026, 3, 18, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(2 * time.Second)
	appendDirectedExplorationToolRun(job, "supply_chain.hot_path", directedSupplyChainToolRun{
		Status: "completed",
		Metrics: map[string]any{
			"hotPathChainsN": 3,
		},
	}, startedAt, finishedAt)

	raw := parseStateSliceMap(job.State["directedExplorationToolRuns"])
	if len(raw) != 1 {
		t.Fatalf("expected one tool run, got %d", len(raw))
	}
	if parseStateString(raw[0]["name"]) != "supply_chain.hot_path" {
		t.Fatalf("unexpected tool name: %#v", raw[0]["name"])
	}
	metrics, ok := raw[0]["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics map, got %#v", raw[0]["metrics"])
	}
	if parseStateInt(metrics["hotPathChainsN"], 0) != 3 {
		t.Fatalf("unexpected hotPathChainsN: %#v", metrics["hotPathChainsN"])
	}
}
