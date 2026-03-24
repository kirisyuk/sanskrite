package simulation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scenarios/teslabattery"
)

func TestApplyActionImpactDifferentiatesAxesAndDomains(t *testing.T) {
	base := metricState{risk: 0.2, price: 0.2, narrative: 0.2, policy: 0.2, supply: 0.2}
	regulator := &horizonmodels.SimulationAgentSpec{
		AgentClass:       "regulator",
		PopulationWeight: 1.4,
		Traits:           map[string]any{"attention": 0.8, "riskBias": 0.7},
		BeliefState:      map[string]float64{"stress": 0.4},
		ResourceState:    map[string]float64{"budget": 0.7},
	}
	importer := &horizonmodels.SimulationAgentSpec{
		AgentClass:       "importer",
		PopulationWeight: 1.4,
		Traits:           map[string]any{"attention": 0.7, "riskBias": 0.6},
		BeliefState:      map[string]float64{"stress": 0.5},
		ResourceState:    map[string]float64{"budget": 0.5},
	}

	legal := applyActionImpact(base, horizonmodels.SimulationAction{
		Type:    string(horizonmodels.SimulationActionIssueStatement),
		Payload: map[string]any{"delta": 0.05},
	}, regulator, horizonmodels.AxisLegalCompliance, 1, 1)
	financial := applyActionImpact(base, horizonmodels.SimulationAction{
		Type:    string(horizonmodels.SimulationActionAdjustBudget),
		Payload: map[string]any{"delta": 0.05},
	}, importer, horizonmodels.AxisFinancialSettlement, 2, 1)

	if legal.policy <= legal.price {
		t.Fatalf("expected legal regulator action to stress policy more than price: %+v", legal)
	}
	if financial.price <= financial.policy {
		t.Fatalf("expected financial importer action to stress price more than policy: %+v", financial)
	}
}

func TestApplyMetricDeltaAvoidsInstantSaturation(t *testing.T) {
	value := 0.25
	for i := 0; i < 6; i++ {
		value = applyMetricDelta(value, 0.09)
	}
	if value >= 1 {
		t.Fatalf("expected damping below hard saturation, got %.3f", value)
	}
	if value <= 0.45 {
		t.Fatalf("expected metric to still move materially, got %.3f", value)
	}
}

func TestAxisActionRelevanceSuppressesOffAxisNoise(t *testing.T) {
	demandPolicy := axisActionRelevance(horizonmodels.AxisPhysicalFlowDemand, "state", string(horizonmodels.SimulationActionIssueStatement), 1, 0)
	demandCommercial := axisActionRelevance(horizonmodels.AxisPhysicalFlowDemand, "consumer", string(horizonmodels.SimulationActionBuy), 1, 0)
	if demandCommercial <= demandPolicy {
		t.Fatalf("expected demand commercial relevance %.2f to exceed policy relevance %.2f", demandCommercial, demandPolicy)
	}

	infoNarrative := axisActionRelevance(horizonmodels.AxisInfoUncertainty, "info", string(horizonmodels.SimulationActionBroadcastMessage), 1, 0)
	infoSell := axisActionRelevance(horizonmodels.AxisInfoUncertainty, "market", string(horizonmodels.SimulationActionSell), 1, 0)
	if infoNarrative <= infoSell {
		t.Fatalf("expected info narrative relevance %.2f to exceed sell relevance %.2f", infoNarrative, infoSell)
	}
}

func TestBuildActionProfileGroupsByCategory(t *testing.T) {
	events := map[string][]*horizonmodels.SimulationEvent{
		"world-1": {
			{
				Payload: map[string]any{
					"actions": []any{
						map[string]any{"type": string(horizonmodels.SimulationActionRerouteShipment), "actorId": "a1", "targetRefs": []any{"/market/us/transportation/freight-logistics"}},
						map[string]any{"type": string(horizonmodels.SimulationActionAdjustGuidance), "actorId": "a2", "targetRefs": []any{"/market/us/capital/industrial-investor-confidence"}},
					},
				},
			},
		},
	}
	profile := buildActionProfile(events)
	categories, ok := profile["topActionCategories"].([]string)
	if !ok || len(categories) == 0 {
		t.Fatalf("expected top action categories, got %#v", profile)
	}
}

func TestBuildExecutiveMemoIncludesSystemicSectionsAndPRResponse(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Tesla battery logistics under trade restrictions and logistics delays.",
			SeedPaths: teslabattery.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisInfoUncertainty,
				SeedPaths: teslabattery.SeedPaths(),
			},
		},
		BaselineRefs: horizonmodels.SimulationBaselineRefs{
			SnapshotDigest: "digest-1",
			ScopePaths:     teslabattery.SeedPaths(),
			FragmentPaths:  teslabattery.SeedPaths(),
		},
	}
	worlds := []*horizonmodels.SimulationWorld{{WorldIndex: 0}}
	outcomes := map[string]float64{
		"risk_score":           0.88,
		"price_pressure":       0.74,
		"narrative_volatility": 0.92,
		"policy_pressure":      0.68,
		"supply_stress":        0.41,
	}
	sensitivity := map[string]float64{
		"risk_score":           0.12,
		"price_pressure":       0.18,
		"narrative_volatility": 0.31,
		"policy_pressure":      0.22,
	}
	actorReactions := []map[string]any{
		{"actorClass": "pr_agent", "dominantActions": []string{"adjust_guidance", "issue_statement"}},
		{"actorClass": "enterprise_treasury", "dominantActions": []string{"hedge_cost", "adjust_budget"}},
		{"actorClass": "facility_operator_texas", "dominantActions": []string{"allocate_capacity", "reprioritize_flow"}},
		{"actorClass": "facility_operator_nevada", "dominantActions": []string{"allocate_capacity", "reserve_inventory"}},
	}
	mechanismChains := []map[string]any{
		{"chainSummary": "trade_official -> customs_official -> ocean_logistics_operator -> facility_operator_texas"},
	}
	graphValidation := map[string]any{"coverage": 1.0, "status": "grounded"}
	actionProfile := map[string]any{
		"topActionCategories": []string{"policy", "logistics", "finance"},
		"topActionTypes":      []string{"request_exemption", "reroute_shipment", "adjust_guidance"},
	}

	memo := buildExecutiveMemo(run, worlds, outcomes, sensitivity, actorReactions, mechanismChains, graphValidation, actionProfile)
	for _, key := range []string{"whatIsHappening", "whatIsAtRisk", "actions30Days", "weeklyMonitoring"} {
		if lines := stringSliceAny(memo[key]); len(lines) == 0 {
			t.Fatalf("expected non-empty memo section %s, got %#v", key, memo[key])
		}
	}
	pr := mapValue(memo["prResponse"])
	if len(stringSliceAny(pr["expectedHeadwinds"])) == 0 {
		t.Fatalf("expected PR expectedHeadwinds in memo, got %#v", pr)
	}
	if len(stringSliceAny(pr["recommendedLines"])) == 0 {
		t.Fatalf("expected PR recommendedLines in memo, got %#v", pr)
	}
}

func TestBuildDisciplineBundlePartsProduceRoleAddressableOutput(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question:  "Tesla battery logistics under trade restrictions and logistics delays.",
			SeedPaths: teslabattery.SeedPaths(),
			Signal: &horizonmodels.SimulationShock{
				Axis:      horizonmodels.AxisFinancialSettlement,
				SeedPaths: teslabattery.SeedPaths(),
			},
		},
	}
	outcomes := map[string]float64{
		"risk_score":           0.72,
		"price_pressure":       0.81,
		"narrative_volatility": 0.58,
		"policy_pressure":      0.47,
		"supply_stress":        0.33,
	}
	sensitivity := map[string]float64{
		"price_pressure":       0.22,
		"risk_score":           0.18,
		"narrative_volatility": 0.14,
		"supply_stress":        0.11,
	}
	actorReactions := []map[string]any{
		{"actorClass": "enterprise_treasury", "actionVolume": 18, "dominantActions": []string{"hedge_cost", "adjust_guidance"}},
		{"actorClass": "ocean_logistics_operator", "actionVolume": 15, "dominantActions": []string{"reroute_shipment", "expedite_shipment"}},
		{"actorClass": "facility_operator_texas", "actionVolume": 13, "dominantActions": []string{"allocate_capacity", "reprioritize_flow"}},
		{"actorClass": "pr_agent", "actionVolume": 11, "dominantActions": []string{"issue_statement", "adjust_guidance"}},
	}
	actionProfile := map[string]any{
		"categoryCounts": map[string]any{
			"finance":    10,
			"logistics":  8,
			"operations": 7,
			"investor":   6,
			"policy":     4,
		},
		"typeCounts": map[string]any{
			"hedge_cost":        6,
			"adjust_guidance":   7,
			"reroute_shipment":  5,
			"allocate_capacity": 4,
			"request_exemption": 2,
		},
	}
	disciplines := buildDisciplinedSignals(run, outcomes, actorReactions, actionProfile)
	if len(disciplines) == 0 {
		t.Fatal("expected disciplined signals")
	}
	functions := buildFunctionExposures(disciplines, actorReactions, outcomes)
	if len(functions) == 0 {
		t.Fatal("expected function exposures")
	}
	decisions := buildDecisionDemands(run, disciplines, functions, actionProfile)
	if len(decisions) == 0 {
		t.Fatal("expected decision demands")
	}
	monitoring := buildMonitoringSignals(run, disciplines, functions, outcomes, sensitivity)
	if len(monitoring) == 0 {
		t.Fatal("expected monitoring signals")
	}
	roles := buildRoleReadouts(run, disciplines, functions, decisions, monitoring)
	cfo := mapValue(roles["cfo"])
	prLead := mapValue(roles["pr_lead"])
	if len(stringSliceAny(cfo["summary"])) == 0 || len(stringSliceAny(cfo["immediateActions"])) == 0 {
		t.Fatalf("expected CFO readout with summary and actions, got %#v", cfo)
	}
	if len(stringSliceAny(prLead["summary"])) == 0 || len(stringSliceAny(prLead["weeklyMonitoring"])) == 0 {
		t.Fatalf("expected PR readout with summary and monitoring, got %#v", prLead)
	}
}

func TestGenericActionPlanUsesPathMetricsForDerivedActors(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		Request: horizonmodels.SimulationRunRequest{
			Question: "Sanctions pressure on cross-border oilfield-services routing.",
			Signal:   &horizonmodels.SimulationShock{Axis: horizonmodels.AxisPhysicalFlowSupply},
		},
	}
	world := &horizonmodels.SimulationWorld{
		MarketState: map[string]any{
			"pricePressure":        0.71,
			"supplyStress":         0.76,
			"pathStressScore":      0.82,
			"bindingConfidence":    0.41,
			"recoveryLatencyHours": 96.0,
			"productRiskScore":     0.64,
			"shoulderLoadScore":    0.58,
		},
		InstitutionState: map[string]any{"policyPressure": 0.33},
	}
	shoulder := &horizonmodels.SimulationAgentSpec{
		AgentClass:  "regional_supply_shoulder_cn",
		BeliefState: map[string]float64{"stress": 0.55},
		OriginRefs:  []string{"/fac/cn/jereh-yantai-manufacturing-center"},
	}
	actionType, _, _ := genericActionPlan(run, world, shoulder, 1, 0)
	if actionType != string(horizonmodels.SimulationActionRerouteShipment) && actionType != string(horizonmodels.SimulationActionReserveInventory) {
		t.Fatalf("expected derived shoulder to react to path metrics, got %s", actionType)
	}

	product := &horizonmodels.SimulationAgentSpec{
		AgentClass:  "product_risk_manager_telemetry-tools",
		BeliefState: map[string]float64{"stress": 0.58},
		OriginRefs:  []string{"/product/finished/electronic/manufacturing/telemetry-tools"},
	}
	actionType, _, _ = genericActionPlan(run, world, product, 2, 0)
	if actionType != string(horizonmodels.SimulationActionReprioritizeFlow) && actionType != string(horizonmodels.SimulationActionReserveInventory) {
		t.Fatalf("expected derived product risk manager to react to path metrics, got %s", actionType)
	}
}

func TestRunAxisFallsBackToRecognition(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisLogisticsCapacity,
		},
	}
	if got := runAxis(run); got != horizonmodels.AxisLogisticsCapacity {
		t.Fatalf("expected recognition primary axis, got %q", got)
	}
}

func TestGenericFallbackActionsAreGroundedAndNotOnlyDoNothing(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		TenantID: "system",
		Request: horizonmodels.SimulationRunRequest{
			Question: "Disruption in LNG routing and port congestion affecting QatarEnergy supply flows.",
			Signal: &horizonmodels.SimulationShock{
				Axis: horizonmodels.AxisLogisticsCapacity,
			},
		},
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisLogisticsCapacity,
			"objectCandidates": []map[string]any{
				{"path": "/org/qa/qatarenergy", "kind": "org", "score": 1.0},
				{"path": "/market/global/energy/lng", "kind": "market", "score": 0.9},
				{"path": "/fac/qa/ras-laffan-port", "kind": "fac", "score": 0.85},
				{"path": "/domain/v1/supply-chain/geo/qa-eu/product/lng", "kind": "domain", "score": 0.8},
				{"path": "/loc/qa", "kind": "loc", "score": 0.75},
			},
		},
	}
	world := &horizonmodels.SimulationWorld{
		WorldIndex: 1,
		MarketState: map[string]any{
			"pricePressure": 0.58,
			"supplyStress":  0.72,
		},
		InstitutionState: map[string]any{
			"policyPressure": 0.21,
		},
	}
	raw := fallbackPopulation(run, world)
	if len(raw) == 0 {
		t.Fatal("expected generic fallback population")
	}
	agents := make([]*horizonmodels.SimulationAgentSpec, 0, len(raw))
	for idx, spec := range raw[:8] {
		agent := &horizonmodels.SimulationAgentSpec{
			ID:             "agent-" + string(rune('a'+idx)),
			AgentClass:     firstString(spec, "agentClass"),
			OriginRefs:     compactStrings(stringSliceValue(spec["originRefs"])),
			AllowedActions: normalizeAllowedActions(stringSliceValue(spec["allowedActions"])),
			Traits:         mapValue(spec["traits"]),
			BeliefState:    floatMapValue(spec["beliefState"]),
			ResourceState:  floatMapValue(spec["resourceState"]),
		}
		agents = append(agents, agent)
	}
	actions := fallbackActions(run, world, agents, 1)
	if len(actions) == 0 {
		t.Fatal("expected grounded generic fallback actions")
	}
	nonIdle := 0
	for _, action := range actions {
		if err := action.Validate(); err != nil {
			t.Fatalf("expected valid action, got %v for %#v", err, action)
		}
		if action.Type != string(horizonmodels.SimulationActionDoNothing) {
			nonIdle++
		}
	}
	if nonIdle == 0 {
		t.Fatalf("expected at least one non-idle action, got %#v", actions)
	}
}

func TestBuildProtocolSummarySkipsUntypedProtocol(t *testing.T) {
	worlds := []*horizonmodels.SimulationWorld{
		{
			ProtocolTraces: []map[string]any{
				{"protocolType": "routing_protocol", "messageType": "routing_request", "fromClass": "logistics_operator", "toClass": "facility_operator"},
				{"kind": "transaction_status", "messageType": "", "fromClass": "logistics_operator", "toClass": "facility_operator"},
			},
		},
	}
	summary := buildProtocolSummary(worlds)
	for _, item := range summary {
		if stringValue(item, "protocolType") == "untyped_protocol" {
			t.Fatalf("expected untyped protocol to be filtered out: %#v", summary)
		}
	}
	if len(summary) == 0 || stringValue(summary[0], "protocolType") != "routing_protocol" {
		t.Fatalf("expected routing_protocol summary, got %#v", summary)
	}
}

func TestBuildResultOverlaysProvidesFrontEndRenderableTargets(t *testing.T) {
	run := &horizonmodels.SimulationRun{
		Request: horizonmodels.SimulationRunRequest{
			Question: "Secondary sanctions tighten around oilfield-services routing into Russia.",
			Signal:   &horizonmodels.SimulationShock{Axis: horizonmodels.AxisPhysicalFlowSupply},
			SeedPaths: []string{
				"/loc/ru",
				"/fac/cn/jereh-yantai-manufacturing-center",
				"/fac/ae/jebel-ali-logistics-center",
				"/product/finished/electronic/manufacturing/telemetry-tools",
				"/product/finished/industrial/pump/centrifugal-pumps",
			},
		},
		Recognition: map[string]any{
			"primaryAxis": horizonmodels.AxisPhysicalFlowSupply,
			"objectCandidates": []map[string]any{
				{"path": "/loc/ru", "kind": "geo", "score": 1.0},
			},
		},
		BaselinePreview: map[string]any{
			"artifactTopShoulders": []map[string]any{
				{"code": "cn", "name": "China", "pathCount": 4, "riskScore": 1.3, "maxStress": 0.84, "refs": []string{"/fac/cn/jereh-yantai-manufacturing-center"}},
				{"code": "ae", "name": "UAE", "pathCount": 3, "riskScore": 1.1, "maxStress": 0.79, "refs": []string{"/fac/ae/jebel-ali-logistics-center"}},
			},
			"artifactTopProducts": []map[string]any{
				{"path": "/product/finished/electronic/manufacturing/telemetry-tools", "name": "telemetry tools", "pathCount": 3, "riskScore": 1.2, "maxStress": 0.88},
				{"path": "/product/finished/industrial/pump/centrifugal-pumps", "name": "centrifugal pumps", "pathCount": 2, "riskScore": 0.9, "maxStress": 0.81},
			},
		},
	}
	actorReactions := []map[string]any{
		{"actorClass": "regional_supply_shoulder_cn", "actionVolume": 12, "dominantActions": []string{"reroute_shipment", "reserve_inventory"}},
		{"actorClass": "product_risk_manager_telemetry-tools", "actionVolume": 9, "dominantActions": []string{"reprioritize_flow", "request_exemption"}},
	}
	protocolSummary := []map[string]any{
		{"protocolType": "routing_protocol", "messageCount": 18},
		{"protocolType": "allocation_protocol", "messageCount": 14},
	}
	transactionSummary := []map[string]any{
		{"transactionType": "shipment_reroute", "count": 16},
		{"transactionType": "inventory_reservation", "count": 11},
	}
	monitoring := buildMonitoringSignals(run, nil, nil, map[string]float64{"price_pressure": 0.8}, map[string]float64{"risk_score": 0.2})
	roles := map[string]any{
		"coo": map[string]any{
			"title":               "COO",
			"priorityFunctions":   []string{"operations", "logistics"},
			"priorityDisciplines": []string{"supply_operations", "product_program"},
			"summary":             []string{"Operations are exposed through reroute and allocation pressure."},
			"immediateActions":    []string{"Lock routing and capacity rules."},
			"weeklyMonitoring":    []string{"China shoulder load", "Telemetry Tools availability"},
			"decisionDemands":     []string{"Capacity and routing control"},
		},
	}

	overlays := buildResultOverlays(run, actorReactions, protocolSummary, transactionSummary, monitoring, roles)
	if overlays == nil {
		t.Fatal("expected non-nil result overlays")
	}
	shoulders := mapSlice(anySlice(overlays["shoulders"]))
	products := mapSlice(anySlice(overlays["products"]))
	pathChains := mapSlice(anySlice(overlays["pathChains"]))
	cascades := mapSlice(anySlice(overlays["cascades"]))
	roleOverlays := mapSlice(anySlice(overlays["roles"]))
	if len(shoulders) == 0 || stringValue(shoulders[0], "kind") != "regional_shoulder" {
		t.Fatalf("expected shoulder overlays, got %#v", overlays["shoulders"])
	}
	if len(products) == 0 || stringValue(products[0], "kind") != "product_family" {
		t.Fatalf("expected product overlays, got %#v", overlays["products"])
	}
	if len(pathChains) == 0 || stringValue(pathChains[0], "kind") != "path_chain" {
		t.Fatalf("expected path-chain overlays, got %#v", overlays["pathChains"])
	}
	if len(cascades) == 0 || stringValue(cascades[0], "kind") != "derived_cascade" {
		t.Fatalf("expected cascade overlays, got %#v", overlays["cascades"])
	}
	if len(roleOverlays) == 0 {
		t.Fatalf("expected role overlays, got %#v", overlays["roles"])
	}
	targets := mapValue(roleOverlays[0]["targets"])
	if len(stringSliceAny(targets["shoulders"])) == 0 || len(stringSliceAny(targets["products"])) == 0 || len(stringSliceAny(targets["pathChains"])) == 0 {
		t.Fatalf("expected role overlay to target shoulder and product overlays, got %#v", targets)
	}
}
