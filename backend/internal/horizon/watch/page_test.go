package watch

import (
	"context"
	"strings"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestNormalizedWatchInterestUsesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	title, query, overrides := normalizedWatchInterest(
		"Red Sea corridor",
		"/zone/red_sea",
		"",
		"",
		[]string{"shipping insurance", "Red Sea corridor"},
	)

	if title != "Red Sea corridor risk picture" {
		t.Fatalf("expected default title, got %q", title)
	}
	if query == "" {
		t.Fatalf("expected default query to be populated")
	}
	if len(overrides) < 3 {
		t.Fatalf("expected merged overrides, got %#v", overrides)
	}
	if overrides[0] != query {
		t.Fatalf("expected query to be first override, got %#v", overrides)
	}
}

func TestBuildWatchPageDashboardMetricsComputesDeltaFromPreviousSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	prev := &horizonmodels.WatchPageSnapshot{
		Dashboards: []horizonmodels.WatchPageDashboardMetric{
			{ID: "tracked-risks", Value: "12"},
			{ID: "stable-metric", Value: "4"},
		},
	}

	metrics := buildWatchPageDashboardMetrics([]DashboardWidget{
		{ID: "tracked-risks", Title: "Tracked risks", Value: "15"},
		{ID: "stable-metric", Title: "Stable metric", Value: "4"},
		{ID: "unparsed", Title: "Unparsed", Value: "n/a"},
	}, prev, now)

	if got := len(metrics); got != 3 {
		t.Fatalf("expected 3 metrics, got %d", got)
	}
	if metrics[0].DeltaValue != "+3" || metrics[0].DeltaDirection != "up" {
		t.Fatalf("expected positive delta, got %#v", metrics[0])
	}
	if metrics[1].DeltaValue != "0" || metrics[1].DeltaDirection != "flat" {
		t.Fatalf("expected flat delta, got %#v", metrics[1])
	}
	if metrics[2].DeltaValue != "" || metrics[2].DeltaDirection != "" {
		t.Fatalf("expected unparsed metric to skip deltas, got %#v", metrics[2])
	}
}

func TestBuildWatchPageAffectedItemsRanksAffectedPathsDeterministically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	briefs := []*horizonmodels.WatchBrief{
		{
			ID:                 "brief-1",
			TargetID:           "target-1",
			SubjectID:          "subject-1",
			CanonicalPath:      "/org/acme",
			Headline:           "Port congestion is rising",
			Summary:            "Primary summary",
			Hotness:            0.9,
			Confidence:         0.8,
			TopAffectedTargets: []string{"/corridor/red-sea", "/market/freight"},
			TopEvidence:        []string{"doc-1"},
		},
		{
			ID:                 "brief-2",
			TargetID:           "target-2",
			SubjectID:          "subject-2",
			CanonicalPath:      "/corridor/red-sea",
			Headline:           "Insurance costs are increasing",
			Summary:            "Secondary summary",
			Hotness:            0.7,
			Confidence:         0.6,
			TopAffectedTargets: []string{"/market/freight"},
			TopEvidence:        []string{"doc-2"},
		},
	}

	items := buildWatchPageAffectedItems(briefs, now)

	if len(items) < 2 {
		t.Fatalf("expected affected items, got %#v", items)
	}
	if items[0].CanonicalPath != "/market/freight" {
		t.Fatalf("expected freight market to rank first, got %#v", items[0])
	}
	if items[0].Rank != 1 {
		t.Fatalf("expected first item rank 1, got %d", items[0].Rank)
	}
	if len(items[0].BackingIDs) == 0 || len(items[0].SourceRefs) == 0 {
		t.Fatalf("expected backing ids and source refs to propagate, got %#v", items[0])
	}
}

func TestBuildWatchPageProductsCapturesConsumersAndTransforms(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	artifact := &horizonmodels.MaterializedArtifact{ID: "assembly-1"}
	assemblies := []lensAssemblyArtifact{{
		Artifact: artifact,
		Parsed: &horizonmodels.SupplyChainAssemblyGraphPayload{
			ProductNodes: []horizonmodels.SupplyChainAssemblyNode{
				{Ref: "/product/raw-sugar", Name: "Raw Sugar"},
				{Ref: "/product/refined-sugar", Name: "Refined Sugar"},
			},
			ActorNodes: []horizonmodels.SupplyChainAssemblyNode{
				{Ref: "/org/sa/refinery", Name: "Jeddah Refinery"},
				{Ref: "/fac/ye/hodeidah-plant", Name: "Hodeidah Plant"},
				{Ref: "/org/ye/retail-network", Name: "Yemen Retail Network"},
			},
			SupplyEdges: []horizonmodels.SupplyChainAssemblySupplyEdge{
				{SupplierRef: "/org/sa/refinery", ProductRef: "/product/raw-sugar", CustomerRef: "/fac/ye/hodeidah-plant"},
				{SupplierRef: "/fac/ye/hodeidah-plant", ProductRef: "/product/refined-sugar", CustomerRef: "/org/ye/retail-network"},
			},
			TransformEdges: []horizonmodels.SupplyChainAssemblyTransformEdge{
				{FromProductRef: "/product/raw-sugar", ToProductRef: "/product/refined-sugar"},
			},
		},
	}}

	items := buildWatchPageProducts(assemblies, nil, now)

	if len(items) != 2 {
		t.Fatalf("expected 2 products, got %#v", items)
	}
	if items[0].Label != "Raw Sugar" {
		t.Fatalf("expected raw sugar first, got %#v", items[0])
	}
	if items[0].Summary == "" || items[1].Summary == "" {
		t.Fatalf("expected product summaries with consumers/transforms, got %#v", items)
	}
	if want := "Transforms into Refined Sugar"; !contains(items[0].Summary, want) {
		t.Fatalf("expected transform chain in raw sugar summary, got %q", items[0].Summary)
	}
	if want := "Consumed by Yemen Retail Network"; !contains(items[1].Summary, want) {
		t.Fatalf("expected downstream consumer in refined sugar summary, got %q", items[1].Summary)
	}
}

func TestBuildWatchPageSignalsFallsBackToMonitoringSnapshots(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	signals := buildWatchPageSignals(&DashboardSnapshot{
		Monitoring: []*MonitoringDeltaSnapshot{
			{
				ID:                 "monitor-1",
				Label:              "Hodeidah sugar refinery",
				CanonicalPath:      "/fac/ye/hodeidah-sugar-refinery",
				Headline:           "Refinery throughput is under pressure",
				Summary:            "Binding confidence and path stress indicate sustained delay risk.",
				Status:             "PATH_SUBSTRATE",
				UpdatedAt:          now.Add(-2 * time.Hour),
				TopAffectedTargets: []string{"/geo/ye", "/product/refined-sugar"},
			},
		},
	}, nil, now)

	if len(signals) != 1 {
		t.Fatalf("expected monitoring snapshot to become a signal, got %#v", signals)
	}
	if signals[0].SourceLabel != "Lens monitoring" {
		t.Fatalf("expected monitoring source label, got %#v", signals[0])
	}
	if signals[0].Title != "Refinery throughput is under pressure" {
		t.Fatalf("unexpected signal title %#v", signals[0])
	}
}

func TestBuildWatchPageSignalsKeepsGraphSignalsLensSpecificAndSupplemental(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	graph := &lensDiagnosticGraph{
		Nodes: map[string]*horizonmodels.HorizonFragment{
			"product":  {ID: "product", CanonicalPath: "/product/crude-palm-oil", Labels: map[string]string{"en": "Crude Palm Oil"}},
			"producer": {ID: "producer", CanonicalPath: "/org/id/golden-agri", Labels: map[string]string{"en": "Golden Agri"}},
			"consumer": {ID: "consumer", CanonicalPath: "/org/ye/ycgsi", Labels: map[string]string{"en": "YCGSI"}},
		},
		DirectRelations: []lensDiagnosticRelation{
			{
				EdgeID:               "edge-produces",
				SourceFragmentID:     "producer",
				TargetFragmentID:     "product",
				RelatedFragmentID:    "product",
				RelatedCanonicalPath: "/product/crude-palm-oil",
				RelatedLabel:         "Crude Palm Oil",
				Type:                 string(ah.RelationNameProduces),
				WhyRelevant:          "Golden Agri produces one of the products carrying current exposure.",
			},
			{
				EdgeID:               "edge-supplies",
				RelatedFragmentID:    "refinery",
				RelatedCanonicalPath: "/fac/ye/aden-refinery",
				RelatedLabel:         "Aden Refinery",
				Type:                 string(ah.RelationNameSupplies),
				WhyRelevant:          "Aden Refinery supplies into the current lens graph, so downstream availability depends on that link.",
			},
		},
	}

	signals := buildWatchPageSignals(&DashboardSnapshot{
		Monitoring: []*MonitoringDeltaSnapshot{
			{
				ID:            "monitor-1",
				CanonicalPath: "/route/red-sea",
				Headline:      "Red Sea route stress",
				Summary:       "Transit delays are rising.",
				UpdatedAt:     now.Add(-time.Hour),
			},
		},
	}, graph, now)

	if len(signals) < 2 {
		t.Fatalf("expected graph signal to supplement base monitoring, got %#v", signals)
	}
	titles := strings.Join(titlesFromSignals(signals), " | ")
	if !contains(titles, "Crude Palm Oil downstream exposure") {
		t.Fatalf("expected product-chain graph signal, got %#v", titles)
	}
	if contains(titles, "Aden Refinery supply dependency") {
		t.Fatalf("expected generic upstream supplier signal to be filtered out, got %#v", titles)
	}
}

func TestAssembleLensPageSnapshotBuildsLensSurfaceFromInterestFraming(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC)
	prevCreatedAt := now.Add(-48 * time.Hour)
	svc := &Service{}
	lens := &horizonmodels.Lens{
		ID:        "lens-1",
		TenantID:  "tenant-1",
		ProjectID: "project-1",
		Name:      "Semiconductor chokepoints",
	}
	brief := &horizonmodels.WatchBrief{
		ID:                 "brief-1",
		TargetID:           "target-1",
		SubjectID:          "subject-1",
		Label:              "TSMC",
		CanonicalPath:      "/org/tw/tsmc",
		InterestTitle:      "Taiwan semiconductor exposure",
		InterestQuery:      "Current chokepoints, affected fabs, and escalation paths around Taiwan semiconductor supply",
		Headline:           "Taiwan semiconductor exposure is tightening",
		Summary:            "Export control pressure and maritime disruption are converging around upstream fabs.",
		Explanation:        "Accepted proposals and recent facts show pressure building around Taiwanese production and outbound logistics.",
		RiskPosture:        "elevated",
		Hotness:            0.91,
		Confidence:         0.83,
		DominantThemes:     []string{"export_controls", "shipping"},
		TopAffectedTargets: []string{"/org/tw/tsmc", "/route/tw-east-asia"},
		TopEvidence:        []string{"proposal-1", "fact-1"},
	}
	base := &DashboardSnapshot{
		Scope:        "tenant-1",
		LensID:       lens.ID,
		LensName:     lens.Name,
		TimeHorizon:  "7d",
		MyWatch:      []*horizonmodels.WatchBrief{brief},
		Dashboards:   []DashboardWidget{{ID: "tracked-risks", Title: "Tracked risks", Value: "8", Subtitle: "Across the lens"}},
		MapFragments: []MapFragmentReadModel{{ID: "frag-1", Title: "Taiwan Strait", Caption: "Pressure is clustering around key shipping lanes.", CanonicalPaths: []string{"/zone/tw-strait"}, LayerHints: []string{"shipping", "exposure"}, ScopeType: "geo", BriefID: brief.ID}},
		Monitoring:   []*MonitoringDeltaSnapshot{{ID: "monitor-1", Headline: "Taiwan route stress", Summary: "Shipping disruption is rising across the lens.", CanonicalPath: "/route/tw-east-asia", UpdatedAt: now}},
		ScenarioWorkspace: &ScenarioWorkspaceSnapshot{
			Contracts: []ScenarioContractPreset{{
				ID:          "scenario-1",
				Title:       "Maritime chokepoint escalation",
				Description: "Test whether export control pressure spills into shipping and insurance.",
				TargetPaths: []string{"/route/tw-east-asia"},
			}},
		},
		GeneratedAt: now,
	}
	prev := &horizonmodels.WatchPageSnapshot{
		LensID:    lens.ID,
		CreatedAt: prevCreatedAt,
		Dashboards: []horizonmodels.WatchPageDashboardMetric{
			{ID: "tracked-risks", Value: "6"},
		},
	}

	snapshot := svc.assembleLensPageSnapshot(context.Background(), lens, base, prev)

	if snapshot.BuildStatus != horizonmodels.WatchPageBuildStatusReady {
		t.Fatalf("expected ready snapshot, got %s", snapshot.BuildStatus)
	}
	if snapshot.InterestTitle != brief.InterestTitle {
		t.Fatalf("expected interest title from brief, got %q", snapshot.InterestTitle)
	}
	if snapshot.InterestQuery != brief.InterestQuery {
		t.Fatalf("expected interest query from brief, got %q", snapshot.InterestQuery)
	}
	if snapshot.Hero.Title != brief.InterestTitle {
		t.Fatalf("expected hero title from interest framing, got %q", snapshot.Hero.Title)
	}
	if len(snapshot.WhatIsAtRisk) == 0 || len(snapshot.Affected) == 0 || len(snapshot.MapFragments) == 0 || len(snapshot.Evidence) == 0 {
		t.Fatalf("expected populated sections, got %#v", snapshot)
	}
	if snapshot.SectionEnrichments["scope"].Content == "" || snapshot.SectionEnrichments["signals"].Content == "" || snapshot.SectionEnrichments["scenarios"].Content == "" {
		t.Fatalf("expected deterministic section summaries, got %#v", snapshot.SectionEnrichments)
	}
	if got := snapshot.Dashboards[0].DeltaValue; got != "+2" {
		t.Fatalf("expected dashboard delta +2, got %q", got)
	}
	if snapshot.CreatedAt != prevCreatedAt {
		t.Fatalf("expected createdAt to persist across rebuilds, got %s", snapshot.CreatedAt)
	}
	if snapshot.PageFraming["interestTitle"] != brief.InterestTitle {
		t.Fatalf("expected page framing to retain interest title, got %#v", snapshot.PageFraming)
	}
}

func TestBuildGraphImpactSetsUsesPropagationAndGraphHops(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	graph := &lensDiagnosticGraph{
		Nodes: map[string]*horizonmodels.HorizonFragment{
			"seed":   {ID: "seed", CanonicalPath: "/org/ye/hsa", Labels: map[string]string{"en": "HSA Group"}},
			"direct": {ID: "direct", CanonicalPath: "/fac/ye/taiz-plant", Labels: map[string]string{"en": "Taiz Plant"}},
			"ind":    {ID: "ind", CanonicalPath: "/org/ye/downstream-retailer", Labels: map[string]string{"en": "Downstream Retailer"}},
		},
		NodeMinHops: map[string]int{
			"seed":   0,
			"direct": 1,
			"ind":    2,
		},
		NodeReasons: map[string]string{
			"direct": "Immediate receiving or processing node on the exposed graph.",
			"ind":    "Reached through a downstream dependency chain.",
		},
		NodeBackings: map[string][]string{
			"direct": {"edge-1"},
			"ind":    {"edge-2"},
		},
	}

	direct, indirect := buildGraphImpactSets(graph, now, 6)

	if len(direct) == 0 || direct[0].CanonicalPath != "/fac/ye/taiz-plant" {
		t.Fatalf("expected direct blast radius to start with Taiz Plant, got %#v", direct)
	}
	if len(indirect) == 0 || indirect[0].CanonicalPath != "/org/ye/downstream-retailer" {
		t.Fatalf("expected indirect blast radius to include downstream retailer, got %#v", indirect)
	}
	if !contains(strings.ToLower(direct[0].Reason), "immediate") && !contains(strings.ToLower(direct[0].Reason), "dependency") {
		t.Fatalf("expected direct reason to mention immediate dependency logic, got %#v", direct[0])
	}
}

func TestBuildGraphScenariosPreferProductAndOperationalLabels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	out := buildGraphScenarios(
		&lensDiagnosticGraph{
			ProductPaths: []string{"/product/crude-palm-oil"},
			RoutePaths:   []string{"/route/red-sea"},
		},
		[]horizonmodels.WatchPageScenarioItem{
			{Title: "Supply Chain Disruption"},
			{Title: "Governance Stress"},
			{Title: "Regulatory Impact"},
		},
		[]horizonmodels.WatchPageScopeItem{
			{Label: "Crude Palm Oil", CanonicalPath: "/product/crude-palm-oil"},
		},
		[]horizonmodels.WatchPageSupplyPath{
			{Title: "Red Sea retail corridor", RouteRefs: []string{"/route/red-sea"}},
		},
		[]horizonmodels.WatchPageAffectedItem{
			{Label: "Astc Sanaa Distribution Hub", CanonicalPath: "/fac/ye/astc-sanaa"},
		},
		[]horizonmodels.WatchPageAffectedItem{
			{Label: "Al Hadi Distribution Center", CanonicalPath: "/org/ye/al-hadi"},
		},
		now,
	)

	if len(out) != 3 {
		t.Fatalf("expected scenario set, got %#v", out)
	}
	if out[0].Title != "Crude Palm Oil shortfall" {
		t.Fatalf("expected supply scenario to use product label, got %#v", out[0])
	}
	if out[1].Title != "Astc Sanaa Distribution Hub operational disruption" {
		t.Fatalf("expected operational scenario to use direct affected node, got %#v", out[1])
	}
	if out[2].Title != "Red Sea retail corridor route escalation" {
		t.Fatalf("expected route scenario to use stressed path label, got %#v", out[2])
	}
}

func TestMergeScopeItemsEnrichesDuplicateProductsFromGraph(t *testing.T) {
	t.Parallel()

	merged := mergeScopeItems(
		[]horizonmodels.WatchPageScopeItem{
			{
				ID:            "graph:product:wheat-flour",
				Label:         "Wheat Flour",
				CanonicalPath: "/product/wheat-flour",
				Kind:          "product",
				Summary:       "Consumed by Sanaa Distribution Hub • Transforms into Packaged Flour",
				BackingIDs:    []string{"graph-edge-1"},
			},
		},
		[]horizonmodels.WatchPageScopeItem{
			{
				ID:            "assembly:product:wheat-flour",
				Label:         "Wheat Flour",
				CanonicalPath: "/product/wheat-flour",
				Kind:          "product",
				BackingIDs:    []string{"assembly-1"},
			},
		},
		8,
	)

	if len(merged) != 1 {
		t.Fatalf("expected duplicate product paths to merge, got %#v", merged)
	}
	if merged[0].Summary != "Consumed by Sanaa Distribution Hub • Transforms into Packaged Flour" {
		t.Fatalf("expected richer graph summary to survive merge, got %#v", merged[0])
	}
	if !contains(strings.Join(merged[0].BackingIDs, ","), "assembly-1") || !contains(strings.Join(merged[0].BackingIDs, ","), "graph-edge-1") {
		t.Fatalf("expected backing ids from both sources to survive merge, got %#v", merged[0].BackingIDs)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func TestDerivePageInterestFallsBackToLensQueryModel(t *testing.T) {
	t.Parallel()

	lens := &horizonmodels.Lens{
		ID:            "lens-2",
		Name:          "Battery bottlenecks",
		OriginalQuery: "What are the current choke points for EV battery precursor supply into Europe?",
		DomainQueries: []horizonmodels.LensDomainQuery{
			{
				CanonicalPath:      "/domain/v1/supply-chain/subject/ev-batteries",
				Title:              "EV battery precursor supply",
				Description:        "Map precursor bottlenecks and downstream exposure in Europe",
				FirstOrderEntities: []string{"/product/lithium-carbonate"},
			},
		},
	}

	title, query, overrides := derivePageInterest(lens, nil)
	if title != "Battery bottlenecks risk picture" {
		t.Fatalf("expected default title from lens name, got %q", title)
	}
	if query != lens.OriginalQuery {
		t.Fatalf("expected originalQuery fallback, got %q", query)
	}
	if len(overrides) < 4 {
		t.Fatalf("expected domain query details in overrides, got %#v", overrides)
	}
	if snapshot := derivePageFraming(lens, nil); snapshot["originalQuery"] != lens.OriginalQuery {
		t.Fatalf("expected page framing to carry originalQuery, got %#v", snapshot)
	}
}
