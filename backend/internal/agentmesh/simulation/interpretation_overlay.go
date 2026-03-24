package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func buildScenarioMetadata(run *horizonmodels.SimulationRun) map[string]any {
	dsl, name, ok := scenarioDSLByRun(run)
	if !ok || dsl == nil {
		return map[string]any{
			"name":  name,
			"axis":  runAxis(run),
			"mode":  strings.TrimSpace(run.Request.Mode),
			"scope": compactStrings(run.BaselineRefs.FragmentPaths),
		}
	}
	return map[string]any{
		"name":               name,
		"id":                 strings.TrimSpace(dsl.Metadata.ID),
		"title":              strings.TrimSpace(dsl.Metadata.Title),
		"version":            strings.TrimSpace(dsl.Metadata.Version),
		"description":        strings.TrimSpace(dsl.Metadata.Description),
		"actionOntology":     strings.TrimSpace(dsl.Metadata.ActionOntology),
		"overlay":            strings.TrimSpace(dsl.Interpretation.Overlay),
		"executiveAudience":  strings.TrimSpace(dsl.Interpretation.ExecutiveAudience),
		"priorityDimensions": compactStrings(dsl.Interpretation.PriorityDimensions),
		"axis":               runAxis(run),
	}
}

func buildScenarioGraphValidation(run *horizonmodels.SimulationRun, worldAgents map[string][]*horizonmodels.SimulationAgentSpec) map[string]any {
	dsl, _, ok := scenarioDSLByRun(run)
	if !ok || dsl == nil {
		return map[string]any{
			"status": "not_applicable",
		}
	}
	pathSet := map[string]struct{}{}
	for _, path := range compactStrings(append(append(append([]string{}, run.Request.SeedPaths...), run.BaselineRefs.ScopePaths...), run.BaselineRefs.FragmentPaths...)) {
		pathSet[path] = struct{}{}
	}
	totalRequirements := 0
	satisfied := 0
	missingPaths := make([]string, 0)
	for _, path := range dsl.GraphRequirements.RequiredPaths {
		totalRequirements++
		if _, ok := pathSet[path]; ok {
			satisfied++
			continue
		}
		missingPaths = append(missingPaths, path)
	}

	classOrigins := map[string]map[string]struct{}{}
	classMaxOrigins := map[string]int{}
	for _, agents := range worldAgents {
		for _, agent := range agents {
			if agent == nil {
				continue
			}
			cls := strings.TrimSpace(agent.AgentClass)
			if cls == "" {
				continue
			}
			if classOrigins[cls] == nil {
				classOrigins[cls] = map[string]struct{}{}
			}
			origins := compactStrings(agent.OriginRefs)
			for _, origin := range origins {
				classOrigins[cls][origin] = struct{}{}
			}
			if len(origins) > classMaxOrigins[cls] {
				classMaxOrigins[cls] = len(origins)
			}
		}
	}
	missingBindings := make([]string, 0)
	for _, binding := range dsl.GraphRequirements.RequiredBindings {
		totalRequirements++
		origins := classOrigins[binding.AgentClass]
		if len(origins) == 0 || classMaxOrigins[binding.AgentClass] < binding.MinOriginRefs {
			missingBindings = append(missingBindings, fmt.Sprintf("%s:minOrigins=%d", binding.AgentClass, binding.MinOriginRefs))
			continue
		}
		ok := true
		for _, origin := range binding.RequiredOrigins {
			if _, exists := origins[origin]; !exists {
				ok = false
				break
			}
		}
		if ok {
			satisfied++
			continue
		}
		missingBindings = append(missingBindings, fmt.Sprintf("%s:%s", binding.AgentClass, strings.Join(binding.RequiredOrigins, ",")))
	}
	coverage := 1.0
	if totalRequirements > 0 {
		coverage = round3(float64(satisfied) / float64(totalRequirements))
	}
	status := "grounded"
	if len(missingPaths) > 0 || len(missingBindings) > 0 {
		status = "partial"
	}
	return map[string]any{
		"status":            status,
		"coverage":          coverage,
		"requiredPaths":     len(dsl.GraphRequirements.RequiredPaths),
		"requiredBindings":  len(dsl.GraphRequirements.RequiredBindings),
		"satisfied":         satisfied,
		"missingPaths":      compactStrings(missingPaths),
		"missingBindings":   compactStrings(missingBindings),
		"scopeAnchors":      compactStrings(dsl.GraphRequirements.ScopeAnchors),
		"requiredEdgeTypes": compactStrings(dsl.GraphRequirements.RequiredEdgeTypes),
	}
}

func buildActionProfile(worldEvents map[string][]*horizonmodels.SimulationEvent) map[string]any {
	typeCounts := map[string]int{}
	categoryCounts := map[string]int{}
	for _, events := range worldEvents {
		for _, event := range events {
			for _, action := range eventActions(event) {
				actionType := strings.TrimSpace(action.Type)
				if actionType == "" {
					continue
				}
				typeCounts[actionType]++
				category := "other"
				if descriptor, ok := horizonmodels.SimulationActionCatalog()[actionType]; ok {
					category = descriptor.Category
				}
				categoryCounts[category]++
			}
		}
	}
	topTypes := topMechanisms(typeCounts)
	topCategories := topMechanisms(categoryCounts)
	return map[string]any{
		"topActionTypes":      topTypes,
		"topActionCategories": topCategories,
		"typeCounts":          intMapAny(typeCounts),
		"categoryCounts":      intMapAny(categoryCounts),
	}
}

func buildExecutiveNarrative(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, outcomeDistribution, sensitivity map[string]float64, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) []string {
	lines := buildGenericExecutiveNarrative(run, outcomeDistribution, sensitivity, actorReactions, mechanismChains)
	lines = append(lines, scenarioExecutiveOverlay(run, worlds, outcomeDistribution, actorReactions, mechanismChains, graphValidation, actionProfile)...)
	return compactNarrativeLines(lines)
}

func buildTechnicalNarrative(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) []string {
	lines := buildGenericTechnicalNarrative(run, worlds, actorReactions, mechanismChains)
	if coverage, ok := graphValidation["coverage"].(float64); ok {
		lines = append(lines, fmt.Sprintf("Scenario graph grounding coverage evaluated at %.3f against required paths and agent bindings.", coverage))
	}
	if categories, ok := actionProfile["topActionCategories"].([]string); ok && len(categories) > 0 {
		lines = append(lines, fmt.Sprintf("Dominant action categories were %s.", strings.Join(categories[:min(len(categories), 3)], ", ")))
	}
	lines = append(lines, scenarioTechnicalOverlay(run, worlds, actorReactions, mechanismChains, graphValidation, actionProfile)...)
	return compactNarrativeLines(lines)
}

func buildExecutiveMemo(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, outcomeDistribution, sensitivity map[string]float64, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) map[string]any {
	base := map[string]any{
		"version":          "sim.executive_memo.v1",
		"audience":         stringValue(buildScenarioMetadata(run), "executiveAudience"),
		"whatIsHappening":  buildGenericWhatIsHappening(run, outcomeDistribution, actorReactions, mechanismChains, graphValidation, actionProfile),
		"whatIsAtRisk":     buildGenericWhatIsAtRisk(outcomeDistribution, sensitivity, actorReactions, mechanismChains),
		"actions30Days":    buildGenericActions30Days(run, outcomeDistribution, actorReactions, mechanismChains, actionProfile),
		"weeklyMonitoring": buildGenericWeeklyMonitoring(outcomeDistribution, sensitivity, graphValidation, actionProfile),
		"prResponse":       buildGenericPRResponse(run, outcomeDistribution, actorReactions, actionProfile),
		"graphGrounding":   graphValidation,
		"topActionProfile": actionProfile,
		"scenarioMetadata": buildScenarioMetadata(run),
	}
	return mergeExecutiveMemo(base, scenarioExecutiveMemoOverlay(run, worlds, outcomeDistribution, sensitivity, actorReactions, mechanismChains, graphValidation, actionProfile))
}

func buildGenericWhatIsHappening(run *horizonmodels.SimulationRun, outcomeDistribution map[string]float64, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) []string {
	leading := rankOutcomeDimensions(outcomeDistribution)
	lines := []string{}
	if len(leading) > 0 {
		line := fmt.Sprintf("The dominant operating condition is %s", readableMetric(leading[0].key))
		if len(leading) > 1 {
			line += fmt.Sprintf(" with secondary pressure in %s", readableMetric(leading[1].key))
		}
		lines = append(lines, line+".")
	}
	if len(actorReactions) > 0 {
		lines = append(lines, fmt.Sprintf("The most active representative groups are %s.", joinActorClasses(actorReactions, 4)))
	}
	if len(mechanismChains) > 0 {
		if summary := strings.TrimSpace(stringValue(mechanismChains[0], "chainSummary")); summary != "" {
			lines = append(lines, "The prevailing mechanism chain is "+summary+".")
		}
	}
	if coverage, ok := graphValidation["coverage"].(float64); ok && coverage > 0 {
		lines = append(lines, fmt.Sprintf("Graph grounding coverage is %.3f, so the memo is anchored to explicit scope paths and agent bindings rather than free-form narration.", coverage))
	}
	if categories, ok := actionProfile["topActionCategories"].([]string); ok && len(categories) > 0 {
		lines = append(lines, fmt.Sprintf("Execution is currently being driven by %s actions.", strings.Join(categories[:min(len(categories), 3)], ", ")))
	}
	return compactNarrativeLines(lines)
}

func buildGenericWhatIsAtRisk(outcomeDistribution, sensitivity map[string]float64, actorReactions, mechanismChains []map[string]any) []string {
	lines := []string{}
	for _, dimension := range rankOutcomeDimensions(outcomeDistribution)[:min(len(rankOutcomeDimensions(outcomeDistribution)), 3)] {
		switch dimension.key {
		case "price_pressure":
			lines = append(lines, "Margins and procurement economics are exposed because price pressure is arriving early and broadly.")
		case "policy_pressure":
			lines = append(lines, "Management freedom is exposed because regulatory or institutional actors are becoming part of the operating loop.")
		case "narrative_volatility":
			lines = append(lines, "Confidence and external interpretation are exposed because the issue can spill beyond operations into credibility.")
		case "supply_stress":
			lines = append(lines, "Continuity of supply is exposed because constrained inputs and logistics paths are accumulating stress.")
		case "risk_score":
			lines = append(lines, "Overall execution resilience is exposed because multiple dimensions are tightening at the same time.")
		}
	}
	if len(actorReactions) > 0 {
		lines = append(lines, fmt.Sprintf("The exposed layers are concentrated in %s.", joinActorClasses(actorReactions, 5)))
	}
	uncertain := rankOutcomeDimensions(sensitivity)
	if len(uncertain) > 0 {
		lines = append(lines, fmt.Sprintf("The largest uncertainty remains in %s, so this is the dimension to challenge and stress-test first.", readableMetric(uncertain[0].key)))
	}
	if len(mechanismChains) > 0 {
		lines = append(lines, "Risk is not isolated to one node; it propagates through interacting actor classes and graph-bound dependencies.")
	}
	return compactNarrativeLines(lines)
}

func buildGenericActions30Days(run *horizonmodels.SimulationRun, outcomeDistribution map[string]float64, actorReactions, mechanismChains []map[string]any, actionProfile map[string]any) []string {
	lines := []string{
		"Stand up a weekly control room that ties together operations, finance, policy, and communications around the same simulation dimensions.",
		"Name the top constrained nodes, counterparties, and graph paths explicitly and assign owners for each one.",
	}
	if categories, ok := actionProfile["topActionCategories"].([]string); ok && len(categories) > 0 {
		if containsFold(categories, "policy") || outcomeDistribution["policy_pressure"] > 0.5 {
			lines = append(lines, "Prepare a concrete policy-response path now, including which institutional actors need direct engagement and what exceptions or clarifications are required.")
		}
		if containsFold(categories, "finance") || outcomeDistribution["price_pressure"] > 0.55 {
			lines = append(lines, "Run a treasury and pricing workstream in parallel so that cost defense and liquidity decisions happen before physical disruption forces them.")
		}
		if containsFold(categories, "logistics") || containsFold(categories, "operations") || outcomeDistribution["supply_stress"] > 0.45 {
			lines = append(lines, "Protect continuity with explicit contingency routing, capacity allocation, and inventory reservation decisions rather than ad hoc firefighting.")
		}
	}
	if outcomeDistribution["narrative_volatility"] > 0.55 {
		lines = append(lines, "Treat communications as an operating workstream, not a post-factum wrapper, because narrative spillover can change stakeholder behavior.")
	}
	return compactNarrativeLines(lines)
}

func buildGenericWeeklyMonitoring(outcomeDistribution, sensitivity map[string]float64, graphValidation, actionProfile map[string]any) []string {
	lines := []string{}
	for _, dimension := range rankOutcomeDimensions(outcomeDistribution)[:min(len(rankOutcomeDimensions(outcomeDistribution)), 4)] {
		lines = append(lines, fmt.Sprintf("Track %s weekly and investigate any move that exceeds the scenario baseline.", readableMetric(dimension.key)))
	}
	for _, dimension := range rankOutcomeDimensions(sensitivity)[:min(len(rankOutcomeDimensions(sensitivity)), 2)] {
		lines = append(lines, fmt.Sprintf("Re-run sensitivity on %s weekly because that is where scenario divergence is still highest.", readableMetric(dimension.key)))
	}
	if coverage, ok := graphValidation["coverage"].(float64); ok && coverage < 1 {
		lines = append(lines, "Watch graph coverage drift weekly and close missing bindings before relying on the scenario as a decision instrument.")
	}
	return compactNarrativeLines(lines)
}

func buildGenericPRResponse(run *horizonmodels.SimulationRun, outcomeDistribution map[string]float64, actorReactions []map[string]any, actionProfile map[string]any) map[string]any {
	headwinds := []string{}
	lines := []string{}
	watchouts := []string{}
	if outcomeDistribution["narrative_volatility"] > 0.45 {
		headwinds = append(headwinds, "Stakeholders are likely to ask whether management still controls the situation or is reacting after the fact.")
	}
	if outcomeDistribution["price_pressure"] > 0.45 {
		headwinds = append(headwinds, "External audiences will connect operational pressure to cost, margin, or affordability questions.")
	}
	if outcomeDistribution["policy_pressure"] > 0.45 {
		headwinds = append(headwinds, "There will be questions about dealings with regulators, authorities, or public institutions.")
	}
	lines = append(lines,
		"Anchor every message in concrete control actions already being taken by the operating system, not in generic reassurance.",
		"Use the graph-grounded story: what changed, which operating nodes are affected, what is being protected first, and what remains stable.",
	)
	if actorHasClass(actorReactions, "pr_agent") || actorHasClass(actorReactions, "investor_relations") || actorHasClass(actorReactions, "media") {
		lines = append(lines, "Keep PR and investor communications in the same operating loop as finance and operations so the message reflects real decisions.")
	}
	watchouts = append(watchouts,
		"Do not overpromise on recovery timing before the constrained nodes and interaction chains actually stabilize.",
		"Do not let the public story drift away from the actions the swarm is actually taking, or credibility will deteriorate further.",
	)
	return map[string]any{
		"expectedHeadwinds": compactNarrativeLines(headwinds),
		"recommendedLines":  compactNarrativeLines(lines),
		"watchouts":         compactNarrativeLines(watchouts),
	}
}

func scenarioExecutiveOverlay(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, outcomeDistribution map[string]float64, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) []string {
	switch {
	case isTeslaBatteryHotPath(run):
		return teslaExecutiveOverlay(outcomeDistribution, actorReactions, mechanismChains, actionProfile)
	case isKZVATHotPath(run):
		return kzvatExecutiveOverlay(outcomeDistribution, actorReactions, actionProfile)
	default:
		return nil
	}
}

func scenarioTechnicalOverlay(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) []string {
	switch {
	case isTeslaBatteryHotPath(run):
		return []string{
			"Tesla overlay separated execution bottlenecks from confidence spillovers using scenario-specific actor bindings.",
		}
	case isKZVATHotPath(run):
		return []string{
			"KZ VAT overlay separated fiscal, consumer, and narrative spillovers using scenario-specific actor bindings.",
		}
	default:
		return nil
	}
}

func scenarioExecutiveMemoOverlay(run *horizonmodels.SimulationRun, worlds []*horizonmodels.SimulationWorld, outcomeDistribution, sensitivity map[string]float64, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) map[string]any {
	switch {
	case isTeslaBatteryHotPath(run):
		return teslaExecutiveMemoOverlay(worlds, outcomeDistribution, sensitivity, actorReactions, mechanismChains, graphValidation, actionProfile)
	case isKZVATHotPath(run):
		return kzvatExecutiveMemoOverlay(outcomeDistribution, actorReactions)
	default:
		return nil
	}
}

func teslaExecutiveOverlay(outcomeDistribution map[string]float64, actorReactions, mechanismChains []map[string]any, actionProfile map[string]any) []string {
	lines := []string{
		"Scenario-specific readout: the bottleneck is concentrated in customs, freight, cell allocation, and factory planning rather than in a single factory shutdown.",
	}
	if actorHasClass(actorReactions, "facility_operator_nevada") && actorHasClass(actorReactions, "facility_operator_texas") {
		lines = append(lines, "Texas and Nevada both react through supplier switching and budget protection; Nevada is pulled toward cell continuity while Texas is pulled toward vehicle execution and product priority.")
	}
	if actorHasClass(actorReactions, "energy_storage_program_manager") || actorHasClass(actorReactions, "vehicle_program_manager") {
		lines = append(lines, "Program-level reactions indicate product-mix reprioritization rather than pure volume collapse, especially where energy storage and vehicle lines compete for the same constrained inputs.")
	}
	if categories, ok := actionProfile["topActionCategories"].([]string); ok && len(categories) > 0 {
		lines = append(lines, fmt.Sprintf("Operationally, the scenario is led by %s actions before it becomes a broader reputation issue.", strings.Join(categories[:min(len(categories), 2)], " and ")))
	}
	if outcomeDistribution["price_pressure"] > outcomeDistribution["supply_stress"] {
		lines = append(lines, "Cost and allocation pressures arrive faster than outright physical shortage, which means margin defense and prioritization become earlier management tasks than emergency continuity planning.")
	}
	return lines
}

func kzvatExecutiveOverlay(outcomeDistribution map[string]float64, actorReactions []map[string]any, actionProfile map[string]any) []string {
	lines := []string{
		"Scenario-specific readout: the dominant response is transition friction across prices, working capital, and confidence, not a single-step economic break.",
	}
	if actorHasClass(actorReactions, "budget_revenue_officer") {
		lines = append(lines, "Fiscal actors remain active enough to signal that revenue gains come with administrative and service-delivery trade-offs rather than as clean upside.")
	}
	if outcomeDistribution["policy_pressure"] > 0.4 {
		lines = append(lines, "The political-administrative contour remains material, so transition design matters almost as much as the tax rate itself.")
	}
	return lines
}

func teslaExecutiveMemoOverlay(worlds []*horizonmodels.SimulationWorld, outcomeDistribution, sensitivity map[string]float64, actorReactions, mechanismChains []map[string]any, graphValidation, actionProfile map[string]any) map[string]any {
	what := []string{
		"The shock is not being interpreted as one logistics failure. It is being interpreted as a chain running through trade restrictions, customs friction, freight pressure, cell allocation, factory reprioritization, and then investor confidence.",
		"Texas and Nevada are acting as prioritization nodes: the system is deciding what to protect first, not simply whether Tesla can operate at all.",
	}
	if actorHasClass(actorReactions, "pr_agent") {
		what = append(what, "The PR agent is active inside the operating loop, which means external explanation is already part of the response surface, not a separate afterthought.")
	}
	atRisk := []string{
		"The main risk is loss of flexibility. Tesla can keep operating, but each protected node becomes more expensive and harder to defend without sacrificing another one.",
		"Vehicle execution, storage continuity, treasury buffer, and execution credibility are all exposed at the same time.",
	}
	if outcomeDistribution["price_pressure"] > 0.7 {
		atRisk = append(atRisk, "Margin defense is at risk before outright physical collapse, because the chain is becoming more expensive before it becomes fully unavailable.")
	}
	actions := []string{
		"Within 30 days, lock a joint steering loop across procurement, treasury, logistics, factory planning, investor relations, and PR.",
		"Make Texas-versus-Nevada allocation rules explicit so product-priority decisions are made deliberately rather than by operational drift.",
		"Open the policy workstream immediately: identify exemption paths, customs bottlenecks, and the specific counterparties needed to reduce friction.",
	}
	if actorHasClass(actorReactions, "pr_agent") || actorHasClass(actorReactions, "investor_relations") {
		actions = append(actions, "Use PR and investor relations as a control surface: explain what is being rerouted, what capacity is being protected, and where guidance may change.")
	}
	monitoring := []string{
		"Track customs clearance time, freight premium, and reroute frequency every week.",
		"Track how much scarce input is being allocated to EV versus storage programs every week.",
		"Track whether the external narrative is shifting from temporary disruption to structural loss of control.",
	}
	if len(worlds) > 0 && len(sensitivity) > 0 {
		monitoring = append(monitoring, fmt.Sprintf("Re-run scenario sensitivity weekly with special attention to %s.", readableMetric(rankOutcomeDimensions(sensitivity)[0].key)))
	}
	prResponse := map[string]any{
		"expectedHeadwinds": compactNarrativeLines([]string{
			"Questions about whether Tesla still controls its supply chain under trade restrictions and customs friction.",
			"Questions about which product lines will lose priority and whether guidance will need to move.",
			"Questions about whether the company is paying materially more just to preserve continuity.",
		}),
		"recommendedLines": compactNarrativeLines([]string{
			"State clearly that Tesla is actively rerouting shipments, reallocating capacity, and protecting critical programs rather than waiting for the chain to fail.",
			"Frame guidance updates, if needed, as a consequence of deliberate prioritization and resilience management rather than loss of operational control.",
			"Keep the story grounded in real operating decisions already visible in the swarm traces: reroute, reserve, allocate, reprioritize, request exemption.",
		}),
		"watchouts": compactNarrativeLines([]string{
			"Do not imply that every facility or program is equally protected if the system is already prioritizing between them.",
			"Do not separate the PR line from the treasury and operations reality; markets will detect that mismatch quickly.",
		}),
	}
	return map[string]any{
		"whatIsHappening":  compactNarrativeLines(what),
		"whatIsAtRisk":     compactNarrativeLines(atRisk),
		"actions30Days":    compactNarrativeLines(actions),
		"weeklyMonitoring": compactNarrativeLines(monitoring),
		"prResponse":       prResponse,
	}
}

func kzvatExecutiveMemoOverlay(outcomeDistribution map[string]float64, actorReactions []map[string]any) map[string]any {
	return map[string]any{
		"whatIsHappening": compactNarrativeLines([]string{
			"VAT increase is being interpreted as a transition regime that hits prices, working capital, household behavior, and public interpretation at the same time.",
		}),
		"whatIsAtRisk": compactNarrativeLines([]string{
			"Clean fiscal upside is at risk because part of the gain is offset by administrative friction, pressure on formal demand, and public-service trade-offs.",
		}),
		"actions30Days": compactNarrativeLines([]string{
			"Treat transition design and public explanation as core operating tasks, not as secondary communications.",
		}),
		"weeklyMonitoring": compactNarrativeLines([]string{
			"Track price pressure, working-capital stress, and channel migration weekly.",
		}),
		"prResponse": map[string]any{
			"expectedHeadwinds": compactNarrativeLines([]string{
				"Questions about fairness, inflation, and pressure on households and public services.",
			}),
			"recommendedLines": compactNarrativeLines([]string{
				"Explain the transition path, protections, and service continuity in concrete operational terms.",
			}),
			"watchouts": compactNarrativeLines([]string{
				"Do not present revenue upside as clean if the transition burden is visibly uneven across groups.",
			}),
		},
	}
}

func compactNarrativeLines(lines []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

func actorHasClass(actorReactions []map[string]any, class string) bool {
	for _, item := range actorReactions {
		if strings.EqualFold(strings.TrimSpace(stringValue(item, "actorClass")), strings.TrimSpace(class)) {
			return true
		}
	}
	return false
}

func intMapAny(values map[string]int) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		out[key] = values[key]
	}
	return out
}

func mergeExecutiveMemo(base, overlay map[string]any) map[string]any {
	if len(base) == 0 {
		base = map[string]any{}
	}
	if len(overlay) == 0 {
		return base
	}
	for _, key := range []string{"whatIsHappening", "whatIsAtRisk", "actions30Days", "weeklyMonitoring"} {
		base[key] = appendStringSliceValue(base[key], overlay[key])
	}
	basePR := mapValue(base["prResponse"])
	if basePR == nil {
		basePR = map[string]any{}
	}
	overlayPR := mapValue(overlay["prResponse"])
	for _, key := range []string{"expectedHeadwinds", "recommendedLines", "watchouts"} {
		basePR[key] = appendStringSliceValue(basePR[key], overlayPR[key])
	}
	base["prResponse"] = basePR
	for key, value := range overlay {
		if key == "prResponse" || key == "whatIsHappening" || key == "whatIsAtRisk" || key == "actions30Days" || key == "weeklyMonitoring" {
			continue
		}
		base[key] = value
	}
	return base
}

func appendStringSliceValue(base, extra any) []string {
	return compactNarrativeLines(append(stringSliceAny(base), stringSliceAny(extra)...))
}
