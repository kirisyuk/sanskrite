package watch

import (
	"context"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
)

func TestBuildDashboardWidgetsByServiceLine(t *testing.T) {
	svc := &Service{}
	now := time.Now().UTC()
	globalBriefs := []*horizonmodels.WatchBrief{
		{
			ID:                 "g1",
			Label:              "Gulf",
			CanonicalPath:      "/region/gulf",
			Hotness:            0.82,
			Confidence:         0.74,
			RiskPosture:        "elevated",
			TopAffectedTargets: []string{"/market/energy", "/corridor/red-sea"},
		},
	}
	myBriefs := []*horizonmodels.WatchBrief{
		{
			ID:                 "m1",
			Label:              "Saudi",
			CanonicalPath:      "/loc/saudi-arabia",
			Hotness:            0.67,
			Confidence:         0.71,
			RiskPosture:        "elevated",
			TopAffectedTargets: []string{"/market/defense"},
		},
	}
	feed := []*horizonmodels.WatchFeedEntry{
		{ID: "f1", Title: "Signal", CreatedAt: now},
		{ID: "f2", Title: "Trigger", CreatedAt: now.Add(-time.Minute)},
	}
	attention := &horizonwatchruntime.LensAttentionView{
		LensID:   "lens-1",
		LensName: "MENA",
		Subjects: []*horizonwatchruntime.LensAttentionSubject{
			{
				SubjectID:           "sub-1",
				DisplayLabel:        "Saudi procurement",
				AnchorCanonicalPath: "/loc/saudi-arabia",
				Status:              "active",
				UniversePaths:       []string{"/market/defense"},
				CoverageGaps:        []string{"insurance exposure"},
			},
		},
	}

	geoWidgets := svc.buildDashboardWidgets(DashboardRequest{Mode: string(horizonmodels.WatchModeCombined), ServiceLine: ServiceLineGeo}, globalBriefs, myBriefs, feed, attention, nil, nil)
	monitoringWidgets := svc.buildDashboardWidgets(DashboardRequest{Mode: string(horizonmodels.WatchModeCombined), ServiceLine: ServiceLineMonitoring}, globalBriefs, myBriefs, feed, attention, nil, nil)
	scenarioWidgets := svc.buildDashboardWidgets(DashboardRequest{Mode: string(horizonmodels.WatchModeCombined), ServiceLine: ServiceLineScenario}, globalBriefs, myBriefs, feed, attention, nil, nil)

	if len(geoWidgets) != 4 {
		t.Fatalf("expected 4 geo widgets, got %d", len(geoWidgets))
	}
	if geoWidgets[0].ID != "pressure-index" {
		t.Fatalf("expected geo widget to start with pressure-index, got %q", geoWidgets[0].ID)
	}
	if len(monitoringWidgets) != 4 {
		t.Fatalf("expected 4 monitoring widgets, got %d", len(monitoringWidgets))
	}
	if monitoringWidgets[0].ID != "monitoring-coverage" {
		t.Fatalf("expected monitoring widget to start with monitoring-coverage, got %q", monitoringWidgets[0].ID)
	}
	if len(scenarioWidgets) != 4 {
		t.Fatalf("expected 4 scenario widgets, got %d", len(scenarioWidgets))
	}
	if scenarioWidgets[1].ID != "scenario-contracts" {
		t.Fatalf("expected scenario widgets to include scenario-contracts, got %q", scenarioWidgets[1].ID)
	}
}

func TestApplyServiceLineShaping(t *testing.T) {
	svc := &Service{}
	baseScenario := &ScenarioWorkspaceSnapshot{
		Scope: "tenant",
		Contracts: []ScenarioContractPreset{
			{ID: "supply-chain", ContractType: "policy-impact"},
			{ID: "governance", ContractType: "policy-impact"},
			{ID: "regulatory", ContractType: "policy-impact"},
		},
	}

	makeSnapshot := func() *DashboardSnapshot {
		return &DashboardSnapshot{
			GlobalHotSlices: []*horizonmodels.WatchBrief{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}, {ID: "b4"}, {ID: "b5"}},
			MyWatch:         []*horizonmodels.WatchBrief{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}, {ID: "m4"}, {ID: "m5"}, {ID: "m6"}, {ID: "m7"}, {ID: "m8"}, {ID: "m9"}},
			Feed:            []*horizonmodels.WatchFeedEntry{{ID: "f1"}, {ID: "f2"}, {ID: "f3"}, {ID: "f4"}, {ID: "f5"}, {ID: "f6"}, {ID: "f7"}},
			MapFragments:    []MapFragmentReadModel{{ID: "mf1"}, {ID: "mf2"}, {ID: "mf3"}},
			Monitoring:      []*MonitoringDeltaSnapshot{{ID: "d1"}, {ID: "d2"}, {ID: "d3"}, {ID: "d4"}},
			ScenarioWorkspace: &ScenarioWorkspaceSnapshot{
				Scope:     baseScenario.Scope,
				Contracts: append([]ScenarioContractPreset(nil), baseScenario.Contracts...),
			},
		}
	}

	geoSnapshot := makeSnapshot()
	svc.applyServiceLineShaping(geoSnapshot, DashboardRequest{ServiceLine: ServiceLineGeo})
	if geoSnapshot.ScenarioWorkspace != nil {
		t.Fatalf("expected geo snapshot to omit scenario workspace")
	}
	if len(geoSnapshot.Monitoring) != 3 {
		t.Fatalf("expected geo snapshot monitoring support rail to be truncated to 3, got %d", len(geoSnapshot.Monitoring))
	}
	if len(geoSnapshot.Feed) != 6 {
		t.Fatalf("expected geo feed to be truncated to 6, got %d", len(geoSnapshot.Feed))
	}

	monitoringSnapshot := makeSnapshot()
	svc.applyServiceLineShaping(monitoringSnapshot, DashboardRequest{ServiceLine: ServiceLineMonitoring})
	if monitoringSnapshot.ScenarioWorkspace != nil {
		t.Fatalf("expected monitoring snapshot to omit scenario workspace")
	}
	if monitoringSnapshot.MapFragments != nil {
		t.Fatalf("expected monitoring snapshot map fragments to be nil")
	}
	if len(monitoringSnapshot.MyWatch) != 9 {
		t.Fatalf("expected monitoring snapshot to preserve my watch briefs, got %d", len(monitoringSnapshot.MyWatch))
	}

	scenarioSnapshot := makeSnapshot()
	svc.applyServiceLineShaping(scenarioSnapshot, DashboardRequest{ServiceLine: ServiceLineScenario})
	if scenarioSnapshot.ScenarioWorkspace == nil {
		t.Fatalf("expected scenario snapshot to keep scenario workspace")
	}
	if scenarioSnapshot.Monitoring != nil {
		t.Fatalf("expected scenario snapshot to omit monitoring list")
	}
	if len(scenarioSnapshot.MapFragments) != 2 {
		t.Fatalf("expected scenario snapshot map fragments truncated to 2, got %d", len(scenarioSnapshot.MapFragments))
	}
	if len(scenarioSnapshot.Feed) != 4 {
		t.Fatalf("expected scenario snapshot feed truncated to 4, got %d", len(scenarioSnapshot.Feed))
	}
}

func TestBuildMonitoringSnapshotsPrefersLensAttention(t *testing.T) {
	svc := &Service{}
	now := time.Now().UTC()
	attention := &horizonwatchruntime.LensAttentionView{
		LensID: "lens-1",
		Subjects: []*horizonwatchruntime.LensAttentionSubject{
			{
				SubjectID:           "subject-1",
				DisplayLabel:        "Hormuz route",
				SubjectKind:         "corridor",
				AnchorCanonicalPath: "/corridor/hormuz",
				Status:              "active",
				DomainModes:         []string{"supply_chain", "policy_impact"},
				UniversePaths:       []string{"/market/shipping"},
				CoverageGaps:        []string{"port insurance data"},
				LastRun:             &horizonmodels.ScopeAgentRun{UpdatedAt: now},
			},
		},
	}

	out := svc.buildMonitoringSnapshots(context.Background(), DashboardRequest{ServiceLine: ServiceLineMonitoring}, nil, nil, attention, nil, nil)
	if len(out) != 1 {
		t.Fatalf("expected one monitoring snapshot from lens attention, got %d", len(out))
	}
	if out[0].CanonicalPath != "/corridor/hormuz" {
		t.Fatalf("expected anchor canonical path to be preserved, got %q", out[0].CanonicalPath)
	}
	if out[0].Summary == "" {
		t.Fatalf("expected monitoring summary to be populated from coverage gaps or domain modes")
	}
}

func TestBuildScenarioWorkspaceUsesTargetPathsAndSeedFragments(t *testing.T) {
	svc := &Service{}
	briefs := []*horizonmodels.WatchBrief{
		{
			ID:                 "brief-1",
			CanonicalPath:      "/loc/saudi-arabia",
			TopAffectedTargets: []string{"/market/defense", "/corridor/red-sea"},
		},
	}
	attention := &horizonwatchruntime.LensAttentionView{
		LensID:   "lens-1",
		LensName: "Gulf lens",
		Subjects: []*horizonwatchruntime.LensAttentionSubject{
			{
				SubjectID:           "sub-1",
				DisplayLabel:        "Red Sea route",
				AnchorCanonicalPath: "/corridor/red-sea",
				AnchorFragmentID:    "frag-red-sea",
				UniversePaths:       []string{"/market/shipping"},
			},
		},
	}

	workspace := svc.buildScenarioWorkspace(DashboardRequest{ServiceLine: ServiceLineScenario, LensID: "lens-1"}, briefs, attention, nil, nil)
	if workspace == nil {
		t.Fatalf("expected scenario workspace")
	}
	if workspace.Scope != "lens" {
		t.Fatalf("expected lens scope, got %q", workspace.Scope)
	}
	if workspace.LensName != "Gulf lens" {
		t.Fatalf("expected lens name to be carried into workspace, got %q", workspace.LensName)
	}
	if len(workspace.Contracts) != 3 {
		t.Fatalf("expected 3 scenario contracts, got %d", len(workspace.Contracts))
	}
	for _, contract := range workspace.Contracts {
		if contract.ContractType != "policy-impact" {
			t.Fatalf("expected policy-impact contract type, got %q", contract.ContractType)
		}
		if len(contract.TargetPaths) == 0 {
			t.Fatalf("expected contract %q to have target paths", contract.ID)
		}
		if len(contract.SeedFragmentIDs) == 0 {
			t.Fatalf("expected contract %q to have seed fragment ids", contract.ID)
		}
		if got := contract.Parameters["location_path"]; got != "/loc/saudi-arabia" {
			t.Fatalf("expected contract %q to resolve location path, got %#v", contract.ID, got)
		}
		if _, ok := contract.Parameters["vat_rate_from"]; !ok {
			t.Fatalf("expected contract %q to define vat_rate_from", contract.ID)
		}
		if _, ok := contract.Parameters["vat_rate_to"]; !ok {
			t.Fatalf("expected contract %q to define vat_rate_to", contract.ID)
		}
		if _, ok := contract.Parameters["engine_id"]; ok {
			t.Fatalf("expected contract %q not to pin a fake engine_id", contract.ID)
		}
	}
}

func TestScenarioWorkspaceContractsEncodeWithPolicyImpactAdapter(t *testing.T) {
	svc := &Service{}
	workspace := svc.buildScenarioWorkspace(DashboardRequest{ServiceLine: ServiceLineScenario}, []*horizonmodels.WatchBrief{
		{
			ID:                 "brief-1",
			CanonicalPath:      "/loc/kz",
			TopAffectedTargets: []string{"/market/kz/technology/consumer-electronics", "/org/kr/samsung-electronics"},
		},
	}, &horizonwatchruntime.LensAttentionView{
		LensID:   "lens-1",
		LensName: "Samsung CE Central Eurasia",
		Subjects: []*horizonwatchruntime.LensAttentionSubject{
			{
				SubjectID:           "sub-1",
				AnchorCanonicalPath: "/loc/kz/almaty",
				AnchorFragmentID:    "frag-kz",
				UniversePaths:       []string{"/loc/central-asia"},
			},
		},
	}, nil, nil)
	if workspace == nil {
		t.Fatalf("expected workspace")
	}

	adapter := propagation.NewPolicyImpactAdapter()
	for _, contract := range workspace.Contracts {
		signals, err := adapter.Encode(context.Background(), propagation.ScenarioInput{
			ScenarioID:   contract.ID,
			ContractType: contract.ContractType,
			DomainModes:  append([]string(nil), contract.DomainModes...),
			Parameters:   contract.Parameters,
			TargetPaths:  append([]string(nil), contract.TargetPaths...),
		})
		if err != nil {
			t.Fatalf("expected contract %q to encode successfully, got error: %v", contract.ID, err)
		}
		if len(signals) == 0 {
			t.Fatalf("expected contract %q to produce encoded signals", contract.ID)
		}
	}
}

func TestBuildScenarioScopeInputsIncludesLensScopeFallback(t *testing.T) {
	targetPaths, seedFragmentIDs := buildScenarioScopeInputs([]*horizonmodels.WatchBrief{
		{CanonicalPath: "/org/kr/samsung-electronics", TopAffectedTargets: []string{"/market/kz/technology/consumer-electronics"}},
	}, nil, &horizonmodels.Lens{
		ID:    "lens-1",
		Name:  "Samsung CE Central Eurasia",
		Scope: []string{"/loc/kz", "/loc/kz/almaty", "/query/sector:/Technology/**", "/domain/v1/supply-chain/geo/kz/subject/samsung-electronics"},
		ScopeFragments: map[string]string{
			"/loc/kz":        "frag-kz",
			"/loc/kz/almaty": "frag-almaty",
		},
	}, nil)
	if len(targetPaths) == 0 {
		t.Fatalf("expected scope inputs to include target paths")
	}
	if !containsString(targetPaths, "/loc/kz") {
		t.Fatalf("expected lens scope path to be present in target paths: %v", targetPaths)
	}
	if containsString(targetPaths, "/query/sector:/Technology/**") {
		t.Fatalf("expected query addresses to be excluded from target paths: %v", targetPaths)
	}
	if containsString(targetPaths, "/domain/v1/supply-chain/geo/kz/subject/samsung-electronics") {
		t.Fatalf("expected domain paths to be excluded from target paths: %v", targetPaths)
	}
	if !containsString(seedFragmentIDs, "frag-kz") || !containsString(seedFragmentIDs, "frag-almaty") {
		t.Fatalf("expected scope fragments to be used as scenario seeds, got %v", seedFragmentIDs)
	}
}

func TestBuildLensBootstrapMonitoringSnapshot(t *testing.T) {
	snapshot := buildLensBootstrapMonitoringSnapshot(&horizonmodels.Lens{
		ID:          "lens-1",
		Name:        "Samsung CE Central Eurasia",
		Scope:       []string{"/loc/kz", "/loc/kz/almaty", "/query/sector:/Technology/**", "/domain/v1/supply-chain/geo/kz/subject/samsung-electronics"},
		DomainModes: []string{"supply_chain", "policy_impact"},
	})
	if snapshot == nil {
		t.Fatalf("expected bootstrap monitoring snapshot")
	}
	if snapshot.CanonicalPath != "/lens/lens-1" {
		t.Fatalf("expected lens canonical path, got %q", snapshot.CanonicalPath)
	}
	if snapshot.RiskPosture != "bootstrap_required" {
		t.Fatalf("expected bootstrap risk posture, got %q", snapshot.RiskPosture)
	}
	if containsString(snapshot.TopAffectedTargets, "/query/sector:/Technology/**") {
		t.Fatalf("expected query addresses to be excluded from monitoring fallback, got %v", snapshot.TopAffectedTargets)
	}
	if containsString(snapshot.TopAffectedTargets, "/domain/v1/supply-chain/geo/kz/subject/samsung-electronics") {
		t.Fatalf("expected domain paths to be excluded from monitoring fallback, got %v", snapshot.TopAffectedTargets)
	}
}

func TestBriefTouchesLensScope(t *testing.T) {
	lens := &horizonmodels.Lens{Scope: []string{"/loc/kz", "/market/kz/technology/consumer-electronics"}}
	if !briefTouchesLensScope(&horizonmodels.WatchBrief{CanonicalPath: "/loc/kz"}, lens) {
		t.Fatalf("expected direct scope hit to match")
	}
	if !briefTouchesLensScope(&horizonmodels.WatchBrief{CanonicalPath: "/org/kr/samsung-electronics", TopAffectedTargets: []string{"/market/kz/technology/consumer-electronics"}}, lens) {
		t.Fatalf("expected affected target overlap to match")
	}
	if briefTouchesLensScope(&horizonmodels.WatchBrief{CanonicalPath: "/loc/ua"}, lens) {
		t.Fatalf("expected unrelated brief not to match lens scope")
	}
}

func TestBuildLensNarrativeBriefsFiltersOutUnrelatedGlobalWatch(t *testing.T) {
	lens := &horizonmodels.Lens{
		ID:    "lens-1",
		Scope: []string{"/org/ye/hayel-saeed-anam-group"},
	}
	substrate := []supplyChainSubstrate{
		{
			CanonicalKey:     "/supply-chain/global/yemen-grocery-1",
			DisplayName:      "Jeddah sugar to Hodeidah refinery",
			AnchorSupplier:   "/org/sa/jeddah-united-sugar-refinery",
			AnchorCustomer:   "/org/ye/hodeidah-sugar-refinery",
			NodeRefs:         []string{"/org/ye/hayel-saeed-anam-group", "/product/raw-cane-sugar"},
			AffectedZoneRefs: []string{"/zone/ye/port-pressure"},
		},
	}
	globalBriefs := []*horizonmodels.WatchBrief{
		{
			ID:            "tesla-brief",
			CanonicalPath: "/org/us/tesla-inc",
			Headline:      "Tesla risk picture",
			Hotness:       0.95,
			Confidence:    0.9,
		},
		{
			ID:                 "yemen-brief",
			CanonicalPath:      "/org/ye/hodeidah-sugar-refinery",
			Headline:           "Hodeidah refinery under pressure",
			TopAffectedTargets: []string{"/zone/ye/port-pressure"},
			Hotness:            0.72,
			Confidence:         0.81,
		},
	}

	got := buildLensNarrativeBriefs(lens, nil, substrate, globalBriefs, nil)
	if len(got) != 1 {
		t.Fatalf("expected only the lens-relevant brief, got %#v", got)
	}
	if got[0].ID != "yemen-brief" {
		t.Fatalf("expected yemen brief to survive filtering, got %#v", got[0])
	}
}

func TestBuildScenarioWorkspaceUsesSupplyChainSubstrateBaseline(t *testing.T) {
	svc := &Service{}
	lens := &horizonmodels.Lens{
		ID:          "lens-1",
		Name:        "Kazakhstan Consumer Electronics",
		DomainModes: []string{"supply_chain", "policy_impact"},
		ScopeFragments: map[string]string{
			"/org/kr/samsung-electronics": "frag-samsung",
			"/org/kz/technodom-operator":  "frag-technodom",
		},
	}
	substrate := []supplyChainSubstrate{
		{
			CanonicalKey:      "/supply-chain/global/samsung-kz",
			DisplayName:       "Samsung -> Almaty DC -> Technodom",
			AnchorSupplier:    "/org/kr/samsung-electronics",
			AnchorCustomer:    "/org/kz/technodom-operator",
			ProductAxis:       "consumer electronics",
			NodeRefs:          []string{"/org/kr/samsung-electronics", "/loc/kz/almaty", "/org/kz/technodom-operator"},
			GeoRefs:           []string{"/loc/kz", "/loc/kz/almaty"},
			FlowRefs:          []string{"/flow/supply/consumer-electronics"},
			AffectedZoneRefs:  []string{"/zone/kz/logistics"},
			LegCount:          3,
			BindingConfidence: 0.84,
			PathStressScore:   0.66,
		},
	}

	workspace := svc.buildScenarioWorkspace(DashboardRequest{ServiceLine: ServiceLineScenario, LensID: "lens-1"}, nil, nil, lens, substrate)
	if workspace == nil {
		t.Fatalf("expected scenario workspace")
	}
	if workspace.BaselineRef != "Samsung -> Almaty DC -> Technodom" {
		t.Fatalf("expected baselineRef from top substrate, got %q", workspace.BaselineRef)
	}
	if !containsString(workspace.TargetPaths, "/org/kr/samsung-electronics") || !containsString(workspace.TargetPaths, "/org/kz/technodom-operator") {
		t.Fatalf("expected substrate node refs in target paths, got %v", workspace.TargetPaths)
	}
	for _, contract := range workspace.Contracts {
		if got := contract.Parameters["baseline_artifact_key"]; got != "/supply-chain/global/samsung-kz" {
			t.Fatalf("expected baseline artifact key on contract %q, got %#v", contract.ID, got)
		}
		if got := contract.Parameters["product_axis"]; got != "consumer electronics" {
			t.Fatalf("expected product axis metadata on contract %q, got %#v", contract.ID, got)
		}
	}
}

func TestBuildScenarioScopeInputsPrioritizesSupplyChainSubstrate(t *testing.T) {
	lens := &horizonmodels.Lens{
		ID: "lens-1",
		ScopeFragments: map[string]string{
			"/org/kr/samsung-electronics": "frag-samsung",
			"/org/kz/technodom-operator":  "frag-technodom",
		},
	}
	targetPaths, seedFragmentIDs := buildScenarioScopeInputs(nil, nil, lens, []supplyChainSubstrate{
		{
			CanonicalKey:     "/supply-chain/global/samsung-kz",
			AnchorSupplier:   "/org/kr/samsung-electronics",
			AnchorCustomer:   "/org/kz/technodom-operator",
			NodeRefs:         []string{"/org/kr/samsung-electronics", "/org/kz/technodom-operator"},
			GeoRefs:          []string{"/loc/kz"},
			AffectedZoneRefs: []string{"/zone/kz/logistics"},
		},
	})
	if !containsString(targetPaths, "/org/kr/samsung-electronics") || !containsString(targetPaths, "/zone/kz/logistics") {
		t.Fatalf("expected supply-chain substrate paths in scenario inputs, got %v", targetPaths)
	}
	if !containsString(seedFragmentIDs, "frag-samsung") || !containsString(seedFragmentIDs, "frag-technodom") {
		t.Fatalf("expected substrate endpoints to resolve to scope fragment seeds, got %v", seedFragmentIDs)
	}
}

func TestBuildMonitoringSnapshotsUseSupplyChainSubstrateWhenRuntimeMissing(t *testing.T) {
	svc := &Service{}
	lens := &horizonmodels.Lens{
		ID:          "lens-1",
		Name:        "Kazakhstan Consumer Electronics",
		UpdatedAt:   time.Now().UTC(),
		DomainModes: []string{"supply_chain", "policy_impact"},
		Scope:       []string{"/org/kr/samsung-electronics", "/org/kz/technodom-operator"},
	}
	out := svc.buildMonitoringSnapshots(context.Background(), DashboardRequest{ServiceLine: ServiceLineMonitoring, LensID: "lens-1"}, nil, nil, nil, lens, []supplyChainSubstrate{
		{
			CanonicalKey:      "/supply-chain/global/samsung-kz",
			DisplayName:       "Samsung -> Technodom",
			AnchorSupplier:    "/org/kr/samsung-electronics",
			AnchorCustomer:    "/org/kz/technodom-operator",
			ProductAxis:       "consumer electronics",
			NodeRefs:          []string{"/org/kr/samsung-electronics", "/org/kz/technodom-operator"},
			LegCount:          2,
			BindingConfidence: 0.81,
			PathStressScore:   0.61,
		},
	})
	if len(out) != 1 {
		t.Fatalf("expected substrate-only monitoring snapshot when substrate exists, got %d", len(out))
	}
	if out[0].Status != "PATH_SUBSTRATE" {
		t.Fatalf("expected path substrate snapshot, got %+v", out[0])
	}
	if out[0].CanonicalPath != "/artifact/supply-chain/global/samsung-kz" {
		t.Fatalf("expected artifact canonical path, got %q", out[0].CanonicalPath)
	}
}

func TestBuildSubstrateBriefsProduceSyntheticPathOverlayBriefs(t *testing.T) {
	briefs := buildSubstrateBriefs(&horizonmodels.Lens{
		TenantID:  "tenant-a",
		ProjectID: "project-a",
	}, []supplyChainSubstrate{
		{
			CanonicalKey:      "/supply-chain/global/samsung-kz",
			DisplayName:       "Samsung -> Technodom",
			AnchorSupplier:    "/org/kr/samsung-electronics",
			AnchorCustomer:    "/org/kz/technodom-operator",
			ProductAxis:       "consumer electronics",
			NodeRefs:          []string{"/org/kr/samsung-electronics", "/org/kz/technodom-operator"},
			AffectedZoneRefs:  []string{"/zone/kz/logistics"},
			BindingConfidence: 0.82,
			PathStressScore:   0.64,
		},
	})
	if len(briefs) != 1 {
		t.Fatalf("expected one substrate brief, got %d", len(briefs))
	}
	brief := briefs[0]
	if brief.ScopeType != "path_overlay" {
		t.Fatalf("expected path_overlay scope type, got %q", brief.ScopeType)
	}
	if brief.CanonicalPath != "/artifact/supply-chain/global/samsung-kz" {
		t.Fatalf("expected artifact canonical path, got %q", brief.CanonicalPath)
	}
	if brief.Metadata["syntheticArtifact"] != true {
		t.Fatalf("expected synthetic artifact marker, got %#v", brief.Metadata["syntheticArtifact"])
	}
	if !containsString(brief.TopAffectedTargets, "/zone/kz/logistics") {
		t.Fatalf("expected substrate paths in top affected targets, got %v", brief.TopAffectedTargets)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
