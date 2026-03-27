package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type disciplineSpec struct {
	ID            string
	Title         string
	Functions     []string
	MetricWeights map[string]float64
	Categories    []string
	ClassPatterns []string
}

type functionSpec struct {
	ID            string
	Disciplines   []string
	ClassPatterns []string
}

type decisionDemandSpec struct {
	ID          string
	Title       string
	Disciplines []string
	Functions   []string
	ActionTypes []string
	Threshold   float64
	Rationale   string
	Recommended []string
}

type roleSpec struct {
	ID          string
	Title       string
	Functions   []string
	Disciplines []string
	PrimaryLens string
}

func buildDisciplinedSignals(run *horizonmodels.SimulationRun, outcomeDistribution map[string]float64, actorReactions []map[string]any, actionProfile map[string]any) []map[string]any {
	specs := disciplineSpecs()
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		score := disciplineScore(spec, outcomeDistribution, actorReactions, actionProfile)
		if score <= 0.18 {
			continue
		}
		out = append(out, map[string]any{
			"id":                   spec.ID,
			"title":                spec.Title,
			"score":                round3(score),
			"functions":            spec.Functions,
			"primaryMetrics":       disciplinePrimaryMetrics(spec, outcomeDistribution),
			"primaryCategories":    presentCategories(spec.Categories, actionProfile),
			"representativeActors": representativeActorsForPatterns(actorReactions, spec.ClassPatterns, 5),
			"dominantActions":      dominantActionsForPatterns(actorReactions, spec.ClassPatterns, 5),
			"summary":              disciplineSummary(spec, score, outcomeDistribution, actorReactions, actionProfile),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "id") < stringValue(out[j], "id")
		}
		return li > lj
	})
	return out
}

func buildFunctionExposures(disciplinedSignals []map[string]any, actorReactions []map[string]any, outcomeDistribution map[string]float64) []map[string]any {
	specs := functionSpecs()
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		score := functionExposureScore(spec, disciplinedSignals, actorReactions)
		if score <= 0.16 {
			continue
		}
		relatedDisciplines := intersectDisciplines(spec.Disciplines, disciplinedSignals)
		out = append(out, map[string]any{
			"function":             spec.ID,
			"score":                round3(score),
			"relatedDisciplines":   relatedDisciplines,
			"representativeActors": representativeActorsForPatterns(actorReactions, spec.ClassPatterns, 5),
			"dominantActions":      dominantActionsForPatterns(actorReactions, spec.ClassPatterns, 5),
			"primaryMetrics":       disciplineMetricsFromIDs(relatedDisciplines, disciplinedSignals, outcomeDistribution),
			"summary":              functionExposureSummary(spec, score, relatedDisciplines),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "function") < stringValue(out[j], "function")
		}
		return li > lj
	})
	return out
}

func buildDecisionDemands(run *horizonmodels.SimulationRun, disciplinedSignals, functionExposures []map[string]any, actionProfile map[string]any) []map[string]any {
	specs := decisionDemandSpecs()
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		score := decisionDemandScore(spec, disciplinedSignals, functionExposures, actionProfile)
		if score < spec.Threshold {
			continue
		}
		out = append(out, map[string]any{
			"id":                 spec.ID,
			"title":              spec.Title,
			"priority":           priorityLabel(score),
			"score":              round3(score),
			"functions":          spec.Functions,
			"disciplines":        spec.Disciplines,
			"triggeringActions":  presentActionTypes(spec.ActionTypes, actionProfile),
			"rationale":          spec.Rationale,
			"recommendedActions": compactNarrativeLines(spec.Recommended),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "id") < stringValue(out[j], "id")
		}
		return li > lj
	})
	return out
}

func buildMonitoringSignals(run *horizonmodels.SimulationRun, disciplinedSignals, functionExposures []map[string]any, outcomeDistribution, sensitivity map[string]float64) []map[string]any {
	out := make([]map[string]any, 0, 12)
	for _, dim := range rankOutcomeDimensions(outcomeDistribution)[:min(len(rankOutcomeDimensions(outcomeDistribution)), 4)] {
		out = append(out, map[string]any{
			"id":          dim.key,
			"title":       readableMetric(dim.key),
			"cadence":     "weekly",
			"kind":        "simulation_dimension",
			"score":       round3(dim.value),
			"why":         fmt.Sprintf("This dimension is one of the strongest active pressures in the current ensemble."),
			"disciplines": relatedDisciplineIDsForMetric(dim.key, disciplinedSignals),
			"functions":   relatedFunctionsForMetric(dim.key, functionExposures),
		})
	}
	for _, dim := range rankOutcomeDimensions(sensitivity)[:min(len(rankOutcomeDimensions(sensitivity)), 2)] {
		out = append(out, map[string]any{
			"id":          dim.key + "_variance",
			"title":       readableMetric(dim.key) + " variance",
			"cadence":     "weekly",
			"kind":        "scenario_variance",
			"score":       round3(dim.value),
			"why":         "This dimension still drives world-to-world divergence and should be challenged continuously.",
			"disciplines": relatedDisciplineIDsForMetric(dim.key, disciplinedSignals),
			"functions":   relatedFunctionsForMetric(dim.key, functionExposures),
		})
	}
	out = append(out, scopeDerivedMonitoringOverlay(run)...)
	out = append(out, operationalMonitoringOverlay(run)...)
	out = append(out, scenarioMonitoringOverlay(run)...)
	return dedupeMonitoringSignals(out)
}

func buildRoleReadouts(run *horizonmodels.SimulationRun, disciplinedSignals, functionExposures, decisionDemands, monitoringSignals []map[string]any) map[string]any {
	specs := roleSpecs()
	out := map[string]any{}
	for _, spec := range specs {
		readout := buildRoleReadout(spec, disciplinedSignals, functionExposures, decisionDemands, monitoringSignals)
		if len(readout) == 0 {
			continue
		}
		out[spec.ID] = readout
	}
	applyOperationalRoleOverlay(run, out)
	applyScenarioRoleOverlay(run, out)
	return out
}

func buildRoleReadout(spec roleSpec, disciplinedSignals, functionExposures, decisionDemands, monitoringSignals []map[string]any) map[string]any {
	relevantFunctions := filterFunctionExposures(functionExposures, spec.Functions)
	relevantDisciplines := filterDisciplines(disciplinedSignals, spec.Disciplines)
	relevantDemands := filterDecisionDemands(decisionDemands, spec.Functions, spec.Disciplines)
	relevantMonitoring := filterMonitoringSignals(monitoringSignals, spec.Functions, spec.Disciplines)
	if len(relevantFunctions) == 0 && len(relevantDisciplines) == 0 {
		return nil
	}
	summary := []string{}
	if len(relevantFunctions) > 0 {
		topFn := relevantFunctions[0]
		summary = append(summary, fmt.Sprintf("%s is primarily exposed through %s.", spec.Title, stringValue(topFn, "function")))
	}
	if len(relevantDisciplines) > 0 {
		topDisc := relevantDisciplines[0]
		summary = append(summary, fmt.Sprintf("The strongest discipline for this role is %s.", strings.ReplaceAll(stringValue(topDisc, "title"), "_", " ")))
	}
	return map[string]any{
		"title":               spec.Title,
		"primaryLens":         spec.PrimaryLens,
		"priorityFunctions":   topStringField(relevantFunctions, "function", 4),
		"priorityDisciplines": topStringField(relevantDisciplines, "id", 4),
		"summary":             compactNarrativeLines(summary),
		"immediateActions":    flattenDemandActions(relevantDemands, 5),
		"weeklyMonitoring":    flattenMonitoringTitles(relevantMonitoring, 6),
		"decisionDemands":     topStringField(relevantDemands, "title", 5),
	}
}

func disciplineSpecs() []disciplineSpec {
	return []disciplineSpec{
		{
			ID:            "policy_compliance",
			Title:         "Policy and compliance",
			Functions:     []string{"regulatory", "communications"},
			MetricWeights: map[string]float64{"policy_pressure": 0.6, "risk_score": 0.25, "narrative_volatility": 0.15},
			Categories:    []string{"policy"},
			ClassPatterns: []string{"trade_official", "customs_official", "regulator", "budget_revenue_officer", "investment_climate_officer", "export_control_officer", "transshipment_compliance_officer"},
		},
		{
			ID:            "trade_compliance",
			Title:         "Trade controls and sanctions compliance",
			Functions:     []string{"compliance", "regulatory", "procurement"},
			MetricWeights: map[string]float64{"policy_pressure": 0.42, "risk_score": 0.28, "supply_stress": 0.20, "narrative_volatility": 0.10},
			Categories:    []string{"policy", "sourcing"},
			ClassPatterns: []string{"export_control_officer", "sanctions_compliance_officer", "launch_regulator", "procurement_compliance_lead", "customs_official", "trade_official", "transshipment_compliance_officer", "slb_russia_procurement_lead"},
		},
		{
			ID:            "supply_operations",
			Title:         "Supply and operations",
			Functions:     []string{"operations", "logistics", "procurement"},
			MetricWeights: map[string]float64{"price_pressure": 0.35, "supply_stress": 0.4, "risk_score": 0.25},
			Categories:    []string{"operations", "logistics", "inventory", "sourcing"},
			ClassPatterns: []string{"facility_operator_*", "production_planner", "warehouse_operator", "ocean_logistics_operator", "ground_logistics_operator", "rail_carrier", "port_operator", "procurement_director", "launch_manifest_planner", "cross_border_logistics_operator", "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer", "jebel_ali_hub_operator", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator", "parallel_import_broker", "regional_supply_shoulder_*", "product_risk_manager_*"},
		},
		{
			ID:            "treasury_finance",
			Title:         "Treasury and finance",
			Functions:     []string{"finance", "product"},
			MetricWeights: map[string]float64{"price_pressure": 0.45, "risk_score": 0.25, "narrative_volatility": 0.15, "policy_pressure": 0.15},
			Categories:    []string{"finance", "investor"},
			ClassPatterns: []string{"enterprise_treasury", "bank", "equity_analyst", "analyst", "ofs_settlement_bank", "trade_credit_insurer"},
		},
		{
			ID:            "investor_reputation",
			Title:         "Investor and reputation",
			Functions:     []string{"communications", "finance"},
			MetricWeights: map[string]float64{"narrative_volatility": 0.5, "risk_score": 0.25, "policy_pressure": 0.15, "price_pressure": 0.10},
			Categories:    []string{"investor", "narrative"},
			ClassPatterns: []string{"pr_agent", "investor_relations", "equity_analyst", "media", "influencer", "market_intelligence_analyst", "trade_credit_insurer"},
		},
		{
			ID:            "market_demand",
			Title:         "Market and demand",
			Functions:     []string{"commercial", "product"},
			MetricWeights: map[string]float64{"price_pressure": 0.45, "narrative_volatility": 0.20, "risk_score": 0.20, "supply_stress": 0.15},
			Categories:    []string{"commercial"},
			ClassPatterns: []string{"consumer_*", "large_energy_customer", "competitor_demand_sink", "marketplace_seller", "ecommerce_platform", "satellite_services_customer", "uae_space_procurement", "saudi_space_procurement", "russian_oilfield_customer"},
		},
		{
			ID:            "product_program",
			Title:         "Product and program mix",
			Functions:     []string{"product", "operations"},
			MetricWeights: map[string]float64{"price_pressure": 0.35, "risk_score": 0.30, "supply_stress": 0.20, "narrative_volatility": 0.15},
			Categories:    []string{"operations", "commercial"},
			ClassPatterns: []string{"vehicle_program_manager", "energy_storage_program_manager", "production_planner", "facility_operator_*", "launch_vehicle_manufacturer", "satellite_manufacturer", "launch_manifest_planner", "roscosmos_program_office", "isro_program_office", "slb_russia_procurement_lead", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator", "product_risk_manager_*"},
		},
	}
}

func functionSpecs() []functionSpec {
	return []functionSpec{
		{ID: "finance", Disciplines: []string{"treasury_finance", "investor_reputation"}, ClassPatterns: []string{"enterprise_treasury", "bank", "equity_analyst", "analyst", "ofs_settlement_bank", "trade_credit_insurer", "market_intelligence_analyst"}},
		{ID: "operations", Disciplines: []string{"supply_operations", "product_program"}, ClassPatterns: []string{"facility_operator_*", "production_planner", "warehouse_operator", "launch_vehicle_manufacturer", "rocket_engine_supplier", "satellite_manufacturer", "launch_manifest_planner", "roscosmos_program_office", "isro_program_office", "jebel_ali_hub_operator", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator", "regional_supply_shoulder_*", "product_risk_manager_*"}},
		{ID: "logistics", Disciplines: []string{"supply_operations"}, ClassPatterns: []string{"ocean_logistics_operator", "ground_logistics_operator", "rail_carrier", "port_operator", "cross_border_logistics_operator", "launch_manifest_planner", "jebel_ali_hub_operator", "parallel_import_broker", "regional_supply_shoulder_*"}},
		{ID: "procurement", Disciplines: []string{"supply_operations", "policy_compliance", "trade_compliance"}, ClassPatterns: []string{"procurement_director", "catl_export_desk", "panasonic_line_operator", "material_supplier_lithium", "lithium_refinery_operator", "procurement_compliance_lead", "uae_space_procurement", "saudi_space_procurement", "slb_russia_procurement_lead", "russian_oilfield_customer", "product_risk_manager_*"}},
		{ID: "regulatory", Disciplines: []string{"policy_compliance", "trade_compliance"}, ClassPatterns: []string{"trade_official", "customs_official", "regulator", "tax_advisor", "export_control_officer", "launch_regulator", "sanctions_compliance_officer", "transshipment_compliance_officer"}},
		{ID: "compliance", Disciplines: []string{"trade_compliance", "policy_compliance"}, ClassPatterns: []string{"export_control_officer", "sanctions_compliance_officer", "procurement_compliance_lead", "launch_regulator", "transshipment_compliance_officer", "slb_russia_procurement_lead"}},
		{ID: "communications", Disciplines: []string{"investor_reputation", "policy_compliance"}, ClassPatterns: []string{"pr_agent", "investor_relations", "media", "influencer", "market_intelligence_analyst"}},
		{ID: "product", Disciplines: []string{"product_program", "market_demand"}, ClassPatterns: []string{"vehicle_program_manager", "energy_storage_program_manager", "large_energy_customer", "competitor_demand_sink", "launch_vehicle_manufacturer", "satellite_manufacturer", "satellite_services_customer", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator", "product_risk_manager_*"}},
		{ID: "commercial", Disciplines: []string{"market_demand", "treasury_finance"}, ClassPatterns: []string{"large_energy_customer", "consumer_*", "competitor_demand_sink", "retailer", "importer", "satellite_services_customer", "uae_space_procurement", "saudi_space_procurement", "russian_oilfield_customer", "parallel_import_broker"}},
	}
}

func decisionDemandSpecs() []decisionDemandSpec {
	return []decisionDemandSpec{
		{
			ID:          "regulatory_pathway_management",
			Title:       "Regulatory pathway management",
			Disciplines: []string{"policy_compliance", "trade_compliance"},
			Functions:   []string{"regulatory", "communications", "procurement"},
			ActionTypes: []string{"request_exemption", "enforce_rule", "issue_statement", "lobby"},
			Threshold:   0.35,
			Rationale:   "The swarm is spending real bandwidth on policy gating and institutional response, so room-to-operate must be managed explicitly.",
			Recommended: []string{"Identify decision owners, exemption paths, and counterparties needed to reduce friction.", "Align policy handling with external communications so the public line follows the real regulatory path."},
		},
		{
			ID:          "capacity_and_routing_control",
			Title:       "Capacity and routing control",
			Disciplines: []string{"supply_operations", "product_program"},
			Functions:   []string{"operations", "logistics", "procurement", "product"},
			ActionTypes: []string{"reroute_shipment", "expedite_shipment", "allocate_capacity", "reserve_inventory", "switch_supplier", "reprioritize_flow"},
			Threshold:   0.35,
			Rationale:   "The swarm is reallocating scarce nodes rather than waiting for a binary failure, so capacity and flow control must be explicit.",
			Recommended: []string{"Make reroute, reserve, and allocation rules explicit for priority programs.", "Name which facilities, lanes, and counterparties are protected first and which ones absorb delay."},
		},
		{
			ID:          "treasury_margin_and_guidance_control",
			Title:       "Treasury, margin, and guidance control",
			Disciplines: []string{"treasury_finance", "investor_reputation"},
			Functions:   []string{"finance", "communications"},
			ActionTypes: []string{"adjust_budget", "hedge_cost", "adjust_guidance", "sell"},
			Threshold:   0.32,
			Rationale:   "The swarm is treating cost, liquidity, and external guidance as one coupled control problem.",
			Recommended: []string{"Run treasury discipline and guidance discipline together rather than as separate workstreams.", "Escalate early if resilience is being bought at the expense of margin or credibility."},
		},
		{
			ID:          "stakeholder_narrative_alignment",
			Title:       "Stakeholder narrative alignment",
			Disciplines: []string{"investor_reputation", "policy_compliance", "trade_compliance"},
			Functions:   []string{"communications", "finance", "regulatory"},
			ActionTypes: []string{"broadcast_message", "issue_statement", "change_belief", "adjust_guidance"},
			Threshold:   0.30,
			Rationale:   "The swarm shows that external interpretation feeds back into operating pressure, so communications must stay inside the control loop.",
			Recommended: []string{"Base public lines on actual operating actions already visible in the swarm traces.", "Avoid timing promises that the constrained graph nodes cannot yet support."},
		},
		{
			ID:          "counterparty_screening_and_license_triage",
			Title:       "Counterparty screening and license triage",
			Disciplines: []string{"trade_compliance"},
			Functions:   []string{"compliance", "regulatory", "procurement", "operations"},
			ActionTypes: []string{"request_exemption", "enforce_rule", "reprioritize_flow", "issue_statement"},
			Threshold:   0.30,
			Rationale:   "The swarm is converting sanctions and export-control friction into a live license-and-counterparty queue, so compliance triage becomes an operating control point.",
			Recommended: []string{"Stand up a single decision queue for counterparty screening, licensing, and exemptions.", "Tie shipment, manifest, and procurement decisions to compliance status instead of handling them in separate teams."},
		},
	}
}

func roleSpecs() []roleSpec {
	return []roleSpec{
		{ID: "cfo", Title: "CFO", Functions: []string{"finance", "procurement", "communications"}, Disciplines: []string{"treasury_finance", "investor_reputation", "policy_compliance"}, PrimaryLens: "liquidity, cost, guidance"},
		{ID: "coo", Title: "COO", Functions: []string{"operations", "logistics", "procurement", "product"}, Disciplines: []string{"supply_operations", "product_program", "market_demand"}, PrimaryLens: "continuity, capacity, flow"},
		{ID: "pr_lead", Title: "PR Lead", Functions: []string{"communications", "regulatory", "finance"}, Disciplines: []string{"investor_reputation", "policy_compliance"}, PrimaryLens: "narrative control, market interpretation"},
		{ID: "compliance_lead", Title: "Compliance Lead", Functions: []string{"compliance", "regulatory", "procurement"}, Disciplines: []string{"trade_compliance", "policy_compliance"}, PrimaryLens: "screening, licensing, sanctions exposure"},
	}
}

func disciplineScore(spec disciplineSpec, outcomeDistribution map[string]float64, actorReactions []map[string]any, actionProfile map[string]any) float64 {
	totalWeight := 0.0
	metricScore := 0.0
	for key, weight := range spec.MetricWeights {
		totalWeight += weight
		metricScore += outcomeDistribution[key] * weight
	}
	if totalWeight > 0 {
		metricScore /= totalWeight
	}
	categoryScore := normalizedCategoryScore(spec.Categories, actionProfile)
	actorScore := matchingActorScore(spec.ClassPatterns, actorReactions)
	return clamp01(metricScore*0.68 + categoryScore*0.18 + actorScore*0.14)
}

func functionExposureScore(spec functionSpec, disciplinedSignals, actorReactions []map[string]any) float64 {
	disciplineScore := 0.0
	for _, discipline := range disciplinedSignals {
		if containsFold(spec.Disciplines, stringValue(discipline, "id")) {
			disciplineScore = maxFloat(disciplineScore, floatValue(discipline, "score", 0))
		}
	}
	actorScore := matchingActorScore(spec.ClassPatterns, actorReactions)
	return clamp01(disciplineScore*0.72 + actorScore*0.28)
}

func decisionDemandScore(spec decisionDemandSpec, disciplinedSignals, functionExposures []map[string]any, actionProfile map[string]any) float64 {
	disciplineScore := 0.0
	for _, discipline := range disciplinedSignals {
		if containsFold(spec.Disciplines, stringValue(discipline, "id")) {
			disciplineScore = maxFloat(disciplineScore, floatValue(discipline, "score", 0))
		}
	}
	functionScore := 0.0
	for _, exposure := range functionExposures {
		if containsFold(spec.Functions, stringValue(exposure, "function")) {
			functionScore = maxFloat(functionScore, floatValue(exposure, "score", 0))
		}
	}
	actionScore := normalizedActionTypeScore(spec.ActionTypes, actionProfile)
	return clamp01(disciplineScore*0.5 + functionScore*0.3 + actionScore*0.2)
}

func disciplinePrimaryMetrics(spec disciplineSpec, outcomeDistribution map[string]float64) []string {
	type scored struct {
		key   string
		value float64
	}
	items := make([]scored, 0, len(spec.MetricWeights))
	for key := range spec.MetricWeights {
		items = append(items, scored{key: key, value: outcomeDistribution[key]})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].value == items[j].value {
			return items[i].key < items[j].key
		}
		return items[i].value > items[j].value
	})
	out := make([]string, 0, min(len(items), 3))
	for _, item := range items[:min(len(items), 3)] {
		out = append(out, item.key)
	}
	return out
}

func presentCategories(categories []string, actionProfile map[string]any) []string {
	counts := mapValue(actionProfile["categoryCounts"])
	out := make([]string, 0, len(categories))
	for _, category := range categories {
		if floatValue(counts, category, 0) > 0 {
			out = append(out, category)
		}
	}
	return compactStrings(out)
}

func presentActionTypes(actionTypes []string, actionProfile map[string]any) []string {
	counts := mapValue(actionProfile["typeCounts"])
	out := make([]string, 0, len(actionTypes))
	for _, actionType := range actionTypes {
		if floatValue(counts, actionType, 0) > 0 {
			out = append(out, actionType)
		}
	}
	return compactStrings(out)
}

func representativeActorsForPatterns(actorReactions []map[string]any, patterns []string, limit int) []string {
	items := make([]map[string]any, 0)
	for _, reaction := range actorReactions {
		if matchesAnyPattern(stringValue(reaction, "actorClass"), patterns) {
			items = append(items, reaction)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		li := floatValue(items[i], "actionVolume", 0)
		lj := floatValue(items[j], "actionVolume", 0)
		if li == lj {
			return stringValue(items[i], "actorClass") < stringValue(items[j], "actorClass")
		}
		return li > lj
	})
	out := make([]string, 0, min(len(items), limit))
	for _, item := range items[:min(len(items), limit)] {
		out = append(out, stringValue(item, "actorClass"))
	}
	return out
}

func dominantActionsForPatterns(actorReactions []map[string]any, patterns []string, limit int) []string {
	counts := map[string]int{}
	for _, reaction := range actorReactions {
		if !matchesAnyPattern(stringValue(reaction, "actorClass"), patterns) {
			continue
		}
		for _, action := range stringSliceAny(reaction["dominantActions"]) {
			counts[action]++
		}
	}
	return topMechanisms(counts)[:min(len(topMechanisms(counts)), limit)]
}

func disciplineSummary(spec disciplineSpec, score float64, outcomeDistribution map[string]float64, actorReactions []map[string]any, actionProfile map[string]any) string {
	metrics := disciplinePrimaryMetrics(spec, outcomeDistribution)
	actors := representativeActorsForPatterns(actorReactions, spec.ClassPatterns, 3)
	categories := presentCategories(spec.Categories, actionProfile)
	parts := []string{fmt.Sprintf("%s is materially active", spec.Title)}
	if len(metrics) > 0 {
		readable := make([]string, 0, len(metrics))
		for _, metric := range metrics {
			readable = append(readable, readableMetric(metric))
		}
		parts = append(parts, "through "+strings.Join(readable, ", "))
	}
	if len(actors) > 0 {
		parts = append(parts, "with visible pressure on "+strings.Join(actors, ", "))
	}
	if len(categories) > 0 {
		parts = append(parts, "and dominant "+strings.Join(categories, ", ")+" actions")
	}
	return strings.Join(parts, " ") + fmt.Sprintf(" (score %.3f).", round3(score))
}

func functionExposureSummary(spec functionSpec, score float64, disciplines []string) string {
	if len(disciplines) == 0 {
		return fmt.Sprintf("%s exposure is active (score %.3f).", spec.ID, round3(score))
	}
	return fmt.Sprintf("%s exposure is active through %s (score %.3f).", spec.ID, strings.Join(disciplines[:min(len(disciplines), 3)], ", "), round3(score))
}

func intersectDisciplines(expected []string, disciplinedSignals []map[string]any) []string {
	out := make([]string, 0, len(expected))
	for _, discipline := range disciplinedSignals {
		id := stringValue(discipline, "id")
		if containsFold(expected, id) {
			out = append(out, id)
		}
	}
	return compactStrings(out)
}

func disciplineMetricsFromIDs(disciplineIDs []string, disciplinedSignals []map[string]any, outcomeDistribution map[string]float64) []string {
	set := map[string]struct{}{}
	for _, discipline := range disciplinedSignals {
		if !containsFold(disciplineIDs, stringValue(discipline, "id")) {
			continue
		}
		for _, metric := range stringSliceAny(discipline["primaryMetrics"]) {
			set[metric] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for metric := range set {
		out = append(out, metric)
	}
	sort.Slice(out, func(i, j int) bool {
		if outcomeDistribution[out[i]] == outcomeDistribution[out[j]] {
			return out[i] < out[j]
		}
		return outcomeDistribution[out[i]] > outcomeDistribution[out[j]]
	})
	return out[:min(len(out), 4)]
}

func normalizedCategoryScore(categories []string, actionProfile map[string]any) float64 {
	counts := mapValue(actionProfile["categoryCounts"])
	total := 0.0
	matched := 0.0
	for _, value := range counts {
		switch typed := value.(type) {
		case int:
			total += float64(typed)
		case float64:
			total += typed
		}
	}
	for _, category := range categories {
		matched += floatValue(counts, category, 0)
	}
	if total <= 0 {
		return 0
	}
	return clamp01(matched / total)
}

func normalizedActionTypeScore(actionTypes []string, actionProfile map[string]any) float64 {
	counts := mapValue(actionProfile["typeCounts"])
	total := 0.0
	matched := 0.0
	for _, value := range counts {
		switch typed := value.(type) {
		case int:
			total += float64(typed)
		case float64:
			total += typed
		}
	}
	for _, actionType := range actionTypes {
		matched += floatValue(counts, actionType, 0)
	}
	if total <= 0 {
		return 0
	}
	return clamp01(matched / total)
}

func matchingActorScore(patterns []string, actorReactions []map[string]any) float64 {
	total := 0.0
	matched := 0.0
	for _, reaction := range actorReactions {
		volume := floatValue(reaction, "actionVolume", 0)
		total += volume
		if matchesAnyPattern(stringValue(reaction, "actorClass"), patterns) {
			matched += volume
		}
	}
	if total <= 0 {
		return 0
	}
	return clamp01(matched / total)
}

func matchesAnyPattern(value string, patterns []string) bool {
	value = strings.TrimSpace(value)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(value, strings.TrimSuffix(pattern, "*")) {
				return true
			}
			continue
		}
		if strings.EqualFold(value, pattern) {
			return true
		}
	}
	return false
}

func relatedDisciplineIDsForMetric(metric string, disciplinedSignals []map[string]any) []string {
	out := []string{}
	for _, discipline := range disciplinedSignals {
		if containsFold(stringSliceAny(discipline["primaryMetrics"]), metric) {
			out = append(out, stringValue(discipline, "id"))
		}
	}
	return compactStrings(out)
}

func relatedFunctionsForMetric(metric string, functionExposures []map[string]any) []string {
	out := []string{}
	for _, exposure := range functionExposures {
		if containsFold(stringSliceAny(exposure["primaryMetrics"]), metric) {
			out = append(out, stringValue(exposure, "function"))
		}
	}
	return compactStrings(out)
}

func scenarioMonitoringOverlay(run *horizonmodels.SimulationRun) []map[string]any {
	switch {
	case isTeslaBatteryHotPath(run):
		return []map[string]any{
			{"id": "customs_clearance_time", "title": "Customs clearance time", "cadence": "weekly", "kind": "external_kpi", "disciplines": []string{"policy_compliance", "supply_operations"}, "functions": []string{"regulatory", "logistics"}, "why": "Customs friction is part of the dominant bottleneck chain."},
			{"id": "freight_premium", "title": "Freight premium", "cadence": "weekly", "kind": "external_kpi", "disciplines": []string{"supply_operations", "treasury_finance"}, "functions": []string{"logistics", "finance"}, "why": "The swarm is paying for resilience before facing outright shortage."},
			{"id": "ev_storage_allocation_split", "title": "EV vs storage allocation split", "cadence": "weekly", "kind": "allocation_kpi", "disciplines": []string{"product_program", "supply_operations"}, "functions": []string{"product", "operations"}, "why": "Program competition for the same constrained inputs is central to the scenario."},
			{"id": "guidance_credibility_shift", "title": "Guidance credibility shift", "cadence": "weekly", "kind": "reputation_kpi", "disciplines": []string{"investor_reputation"}, "functions": []string{"communications", "finance"}, "why": "Narrative drift can turn an operating issue into a confidence issue."},
		}
	case isKZVATHotPath(run):
		return []map[string]any{
			{"id": "formal_to_informal_channel_shift", "title": "Formal-to-informal channel shift", "cadence": "weekly", "kind": "market_kpi", "disciplines": []string{"market_demand"}, "functions": []string{"commercial", "regulatory"}, "why": "Channel migration is part of the transition-risk profile."},
		}
	case isSLBRussiaHotPath(run):
		out := []map[string]any{
			{"id": "counterparty_screening_queue", "title": "Counterparty screening queue", "cadence": "weekly", "kind": "compliance_kpi", "disciplines": []string{"trade_compliance"}, "functions": []string{"compliance", "procurement"}, "why": "Screening and exemptions are becoming a live gating queue for Russia-linked flows."},
			{"id": "jebel_ali_reexport_delay", "title": "Jebel Ali re-export delay", "cadence": "weekly", "kind": "logistics_kpi", "disciplines": []string{"supply_operations", "trade_compliance"}, "functions": []string{"logistics", "compliance"}, "why": "The swarm is treating the UAE hub as a bottleneck where transshipment and compliance meet."},
			{"id": "localization_fill_rate", "title": "Localization fill rate", "cadence": "weekly", "kind": "operations_kpi", "disciplines": []string{"product_program", "supply_operations"}, "functions": []string{"operations", "product"}, "why": "Tyumen assembly and specialty-chemicals nodes are absorbing pressure from transshipment friction."},
			{"id": "russian_customer_slippage", "title": "Russian customer booking slippage", "cadence": "weekly", "kind": "commercial_kpi", "disciplines": []string{"market_demand"}, "functions": []string{"commercial", "procurement"}, "why": "Russian oilfield customers respond through booking hesitation, substitutions, and delay."},
			{"id": "trade_credit_cover_shift", "title": "Trade-credit cover shift", "cadence": "weekly", "kind": "finance_kpi", "disciplines": []string{"treasury_finance", "trade_compliance"}, "functions": []string{"finance", "compliance"}, "why": "Insurance and settlement repricing are early signals that sanctions pressure is moving into financing."},
		}
		for _, shoulder := range slbRussiaDerivedShoulders(run) {
			out = append(out, map[string]any{
				"id":          "shoulder_load_" + shoulder.CountryCode,
				"title":       shoulder.CountryName + " shoulder load",
				"cadence":     "weekly",
				"kind":        "regional_shoulder_kpi",
				"disciplines": []string{"supply_operations", "trade_compliance"},
				"functions":   []string{"logistics", "procurement", "operations"},
				"why":         fmt.Sprintf("%s appears in the graph as an active external shoulder feeding the Russia-linked chain.", shoulder.CountryName),
			})
		}
		for _, product := range slbRussiaDerivedProducts(run) {
			out = append(out, map[string]any{
				"id":          "product_availability_" + slbSafeSlug(lastPathSegment(product.ProductPath)),
				"title":       titleWords(product.ProductName) + " availability",
				"cadence":     "weekly",
				"kind":        "product_kpi",
				"disciplines": []string{"product_program", "supply_operations"},
				"functions":   []string{"product", "operations", "procurement"},
				"why":         fmt.Sprintf("%s is present in active supply paths and should be monitored as a specific stress-bearing product family.", product.ProductName),
			})
		}
		return out
	case isSpaceSatelliteHotPath(run):
		return []map[string]any{
			{"id": "counterparty_screening_queue", "title": "Counterparty screening queue", "cadence": "weekly", "kind": "compliance_kpi", "disciplines": []string{"trade_compliance"}, "functions": []string{"compliance", "procurement"}, "why": "Sanctions and export-control checks are becoming an operating bottleneck."},
			{"id": "launch_license_delay", "title": "Launch license delay", "cadence": "weekly", "kind": "regulatory_kpi", "disciplines": []string{"trade_compliance", "policy_compliance"}, "functions": []string{"regulatory", "operations"}, "why": "License and launch authorization friction drives reprioritization in the launch manifest."},
			{"id": "manifest_slip_rate", "title": "Manifest slip rate", "cadence": "weekly", "kind": "operations_kpi", "disciplines": []string{"supply_operations", "product_program"}, "functions": []string{"operations", "logistics"}, "why": "Launch and delivery commitments are being reprioritized rather than fulfilled linearly."},
			{"id": "insured_launch_premium", "title": "Insured launch premium", "cadence": "weekly", "kind": "finance_kpi", "disciplines": []string{"treasury_finance", "trade_compliance"}, "functions": []string{"finance", "compliance"}, "why": "Insurance repricing is an early signal that compliance risk is becoming a financing problem."},
			{"id": "customer_booking_slippage", "title": "Customer booking slippage", "cadence": "weekly", "kind": "commercial_kpi", "disciplines": []string{"market_demand"}, "functions": []string{"commercial", "product"}, "why": "Customer reactions appear first through booking hesitation and launch manifest reshuffling."},
		}
	default:
		return nil
	}
}

func operationalMonitoringOverlay(run *horizonmodels.SimulationRun) []map[string]any {
	if run == nil {
		return nil
	}
	out := make([]map[string]any, 0, 6)
	safety := metricFloat(run.BaselinePreview, "operational_product_safety_enforcement_max")
	counterfeit := metricFloat(run.BaselinePreview, "operational_counterfeit_seizure_pressure_max")
	recall := metricFloat(run.BaselinePreview, "operational_recall_exposure_max")
	leakage := metricFloat(run.BaselinePreview, "operational_informal_channel_leakage_max")
	trustGap := 0.0
	if v := metricFloat(run.BaselinePreview, "operational_consumer_trust_min"); v > 0 {
		trustGap = clamp01(1 - v)
	}
	screening := metricFloat(run.BaselinePreview, "operational_screening_intensity_max")
	customsDelay := clamp01(metricFloat(run.BaselinePreview, "operational_customs_delay_max") / 96.0)

	if safety >= 0.35 || counterfeit >= 0.35 || recall >= 0.25 {
		out = append(out, map[string]any{
			"id":          "product_safety_enforcement",
			"title":       "Product safety enforcement",
			"cadence":     "weekly",
			"kind":        "compliance_kpi",
			"score":       round3(maxFloat(safety, maxFloat(counterfeit, recall))),
			"disciplines": []string{"trade_compliance", "policy_compliance", "product_program"},
			"functions":   []string{"compliance", "regulatory", "product"},
			"why":         "Product-safety enforcement, seizure pressure, or recall exposure is materially affecting continuity and assortment risk.",
		})
	}
	if leakage >= 0.35 {
		out = append(out, map[string]any{
			"id":          "informal_channel_leakage",
			"title":       "Informal channel leakage",
			"cadence":     "weekly",
			"kind":        "market_kpi",
			"score":       round3(leakage),
			"disciplines": []string{"market_demand", "trade_compliance"},
			"functions":   []string{"commercial", "regulatory", "compliance"},
			"why":         "Grey-market absorption is large enough to distort effective availability, brand control, and enforcement load.",
		})
	}
	if trustGap >= 0.25 {
		out = append(out, map[string]any{
			"id":          "consumer_trust_erosion",
			"title":       "Consumer trust erosion",
			"cadence":     "weekly",
			"kind":        "market_kpi",
			"score":       round3(trustGap),
			"disciplines": []string{"market_demand", "investor_reputation"},
			"functions":   []string{"commercial", "communications", "product"},
			"why":         "Consumer trust is soft enough that inspection, seizure, or safety signals can change demand before core supply collapses.",
		})
	}
	if screening >= 0.45 || customsDelay >= 0.45 {
		out = append(out, map[string]any{
			"id":          "customs_screening_burden",
			"title":       "Customs screening burden",
			"cadence":     "weekly",
			"kind":        "logistics_kpi",
			"score":       round3(maxFloat(screening, customsDelay)),
			"disciplines": []string{"trade_compliance", "supply_operations"},
			"functions":   []string{"compliance", "logistics", "procurement"},
			"why":         "Screening intensity and customs delay are part of the operating path, not just paperwork around it.",
		})
	}
	return out
}

func dedupeMonitoringSignals(items []map[string]any) []map[string]any {
	seen := map[string]struct{}{}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := stringValue(item, "id")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, item)
	}
	return out
}

func titleWords(raw string) string {
	parts := strings.Fields(strings.TrimSpace(raw))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func filterFunctionExposures(items []map[string]any, functions []string) []map[string]any {
	out := []map[string]any{}
	for _, item := range items {
		if containsFold(functions, stringValue(item, "function")) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "function") < stringValue(out[j], "function")
		}
		return li > lj
	})
	return out
}

func filterDisciplines(items []map[string]any, disciplines []string) []map[string]any {
	out := []map[string]any{}
	for _, item := range items {
		if containsFold(disciplines, stringValue(item, "id")) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "id") < stringValue(out[j], "id")
		}
		return li > lj
	})
	return out
}

func filterDecisionDemands(items []map[string]any, functions, disciplines []string) []map[string]any {
	out := []map[string]any{}
	for _, item := range items {
		if hasAnyFold(stringSliceAny(item["functions"]), functions) || hasAnyFold(stringSliceAny(item["disciplines"]), disciplines) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "id") < stringValue(out[j], "id")
		}
		return li > lj
	})
	return out
}

func filterMonitoringSignals(items []map[string]any, functions, disciplines []string) []map[string]any {
	out := []map[string]any{}
	for _, item := range items {
		if hasAnyFold(stringSliceAny(item["functions"]), functions) || hasAnyFold(stringSliceAny(item["disciplines"]), disciplines) {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		li := floatValue(out[i], "score", 0)
		lj := floatValue(out[j], "score", 0)
		if li == lj {
			return stringValue(out[i], "id") < stringValue(out[j], "id")
		}
		return li > lj
	})
	return out
}

func topStringField(items []map[string]any, key string, limit int) []string {
	out := make([]string, 0, min(len(items), limit))
	for _, item := range items[:min(len(items), limit)] {
		if value := stringValue(item, key); value != "" {
			out = append(out, value)
		}
	}
	return compactStrings(out)
}

func flattenDemandActions(items []map[string]any, limit int) []string {
	out := []string{}
	for _, item := range items {
		out = append(out, stringSliceAny(item["recommendedActions"])...)
	}
	return compactNarrativeLines(out)[:min(len(compactNarrativeLines(out)), limit)]
}

func flattenMonitoringTitles(items []map[string]any, limit int) []string {
	out := []string{}
	for _, item := range items {
		if title := stringValue(item, "title"); title != "" {
			out = append(out, title)
		}
	}
	return compactNarrativeLines(out)[:min(len(compactNarrativeLines(out)), limit)]
}

func hasAnyFold(values, expected []string) bool {
	for _, value := range values {
		if containsFold(expected, value) {
			return true
		}
	}
	return false
}

func priorityLabel(score float64) string {
	switch {
	case score >= 0.75:
		return "critical"
	case score >= 0.5:
		return "high"
	default:
		return "medium"
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func applyScenarioRoleOverlay(run *horizonmodels.SimulationRun, roleReadouts map[string]any) {
	if len(roleReadouts) == 0 {
		return
	}
	switch {
	case isTeslaBatteryHotPath(run):
		appendRoleSummary(roleReadouts, "cfo", "Finance is coupled to guidance here, so resilience cost and market credibility have to be managed together.")
		appendRoleSummary(roleReadouts, "coo", "Operations has to treat Texas and Nevada as explicit priority nodes rather than equal recipients of constrained inputs.")
		appendRoleSummary(roleReadouts, "pr_lead", "PR cannot work as a wrapper here; it has to narrate real operating choices that the swarm is already making.")
	case isKZVATHotPath(run):
		appendRoleSummary(roleReadouts, "cfo", "Fiscal or cost effects should be read with transition friction, not as clean upside.")
		appendRoleSummary(roleReadouts, "pr_lead", "Public explanation has to stay tightly aligned with actual transition protections and service continuity.")
	case isSLBRussiaHotPath(run):
		appendRoleSummary(roleReadouts, "coo", "Operations should treat this as a re-export and localization control problem, not as one simple import disruption.")
		appendRoleSummary(roleReadouts, "pr_lead", "External communications have to follow actual screening, transshipment, and localization decisions or the story will outrun operating reality.")
		appendRoleSummary(roleReadouts, "compliance_lead", "Compliance is now part of the flow-control layer because exemptions and screening determine whether equipment and chemicals move at all.")
	case isSpaceSatelliteHotPath(run):
		appendRoleSummary(roleReadouts, "coo", "Operations should read this as a manifest-and-allocation problem, not just as a generic trade risk.")
		appendRoleSummary(roleReadouts, "pr_lead", "External communications have to follow actual license, screening, and manifest decisions or the story will outrun the operating reality.")
		appendRoleSummary(roleReadouts, "compliance_lead", "Compliance is now part of the operating critical path because screening, licensing, and exemptions directly change delivery sequence.")
	}
}

func applyOperationalRoleOverlay(run *horizonmodels.SimulationRun, roleReadouts map[string]any) {
	if len(roleReadouts) == 0 || run == nil {
		return
	}
	safety := metricFloat(run.BaselinePreview, "operational_product_safety_enforcement_max")
	counterfeit := metricFloat(run.BaselinePreview, "operational_counterfeit_seizure_pressure_max")
	recall := metricFloat(run.BaselinePreview, "operational_recall_exposure_max")
	leakage := metricFloat(run.BaselinePreview, "operational_informal_channel_leakage_max")
	trustGap := 0.0
	if v := metricFloat(run.BaselinePreview, "operational_consumer_trust_min"); v > 0 {
		trustGap = clamp01(1 - v)
	}
	screening := metricFloat(run.BaselinePreview, "operational_screening_intensity_max")
	customsDelay := clamp01(metricFloat(run.BaselinePreview, "operational_customs_delay_max") / 96.0)

	if safety >= 0.35 || counterfeit >= 0.35 || recall >= 0.25 {
		appendRoleSummary(roleReadouts, "compliance_lead", "Product-safety, seizure, and recall pressure are part of the operating path here, not a post-market afterthought.")
		appendRoleSummary(roleReadouts, "coo", "Safety enforcement changes effective availability because inspections and seizures can remove stock even when upstream supply still exists.")
	}
	if leakage >= 0.35 {
		appendRoleSummary(roleReadouts, "coo", "Grey-market leakage should be read as an effective supply drain and control failure, not just as side-channel demand.")
		appendRoleSummary(roleReadouts, "cfo", "Informal-channel leakage can erode mix quality and margin even if headline import flow stays alive.")
	}
	if trustGap >= 0.25 || recall >= 0.25 {
		appendRoleSummary(roleReadouts, "pr_lead", "Consumer trust can deteriorate before core supply collapses, so messaging has to follow real safety, inspection, and recall conditions.")
	}
	if screening >= 0.45 || customsDelay >= 0.45 {
		appendRoleSummary(roleReadouts, "compliance_lead", "Customs screening now acts like a live queue on the execution path, so document quality and triage speed directly change flow.")
	}
}

func appendRoleSummary(roleReadouts map[string]any, roleID, line string) {
	role := mapValue(roleReadouts[roleID])
	if len(role) == 0 {
		return
	}
	role["summary"] = appendStringSliceValue(role["summary"], []any{line})
	roleReadouts[roleID] = role
}
