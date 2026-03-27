package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func buildResultOverlays(
	run *horizonmodels.SimulationRun,
	actorReactions []map[string]any,
	protocolSummary []map[string]any,
	transactionSummary []map[string]any,
	monitoringSignals []map[string]any,
	roleReadouts map[string]any,
) map[string]any {
	shoulders := buildShoulderResultOverlays(run, actorReactions, protocolSummary, transactionSummary, monitoringSignals)
	products := buildProductResultOverlays(run, actorReactions, protocolSummary, transactionSummary, monitoringSignals)
	pathChains := buildPathChainResultOverlays(run, shoulders, products, protocolSummary, transactionSummary)
	cascades := buildCascadeResultOverlays(shoulders, products, protocolSummary, transactionSummary)
	roles := buildRoleResultOverlays(roleReadouts, shoulders, products, pathChains, cascades)
	if len(shoulders) == 0 && len(products) == 0 && len(pathChains) == 0 && len(cascades) == 0 && len(roles) == 0 {
		return nil
	}
	return map[string]any{
		"shoulders":  shoulders,
		"products":   products,
		"pathChains": pathChains,
		"cascades":   cascades,
		"roles":      roles,
	}
}

func buildShoulderResultOverlays(
	run *horizonmodels.SimulationRun,
	actorReactions []map[string]any,
	protocolSummary []map[string]any,
	transactionSummary []map[string]any,
	monitoringSignals []map[string]any,
) []map[string]any {
	reactionByClass := actorReactionIndex(actorReactions)
	monitoringByID := monitoringSignalIndex(monitoringSignals)
	overlays := make([]map[string]any, 0, 6)
	for _, shoulder := range scopeDerivedShoulders(run) {
		monitoringID := "shoulder_load_" + shoulder.CountryCode
		reaction := reactionByClass[shoulder.AgentClass]
		actions := overlayDominantActions(reaction)
		functions := []string{"logistics", "procurement", "operations"}
		disciplines := []string{"supply_operations", "trade_compliance"}
		category := overlayRiskCategory(actions, functions, disciplines)
		executionScore := overlayExecutionPressure(actions, protocolSummary, transactionSummary, disciplines)
		overlays = append(overlays, map[string]any{
			"id":                  monitoringID,
			"title":               shoulder.CountryName + " shoulder",
			"kind":                "regional_shoulder",
			"targetRefs":          compactStrings(shoulder.OriginRefs),
			"targetCountryCode":   shoulder.CountryCode,
			"riskCategory":        category,
			"dominantMechanisms":  overlayMechanisms(actions, protocolSummary, transactionSummary, functions, disciplines),
			"recommendedActions":  overlayRecommendedActions(actions),
			"functions":           functions,
			"disciplines":         disciplines,
			"pathCount":           shoulder.PathCount,
			"supportingPathCount": shoulder.PathCount,
			"maxStress":           round3(shoulder.MaxStress),
			"stressScore":         round3(clamp01(shoulder.MaxStress)),
			"executionScore":      round3(executionScore),
			"severityRaw":         round3(shoulder.Severity),
			"signalRefs":          compactStrings([]string{monitoringID}),
			"explanation":         shoulderOverlayExplanation(shoulder, actions, monitoringByID[monitoringID]),
			"attentionFocus":      shoulderAttentionFocus(shoulder, actions),
		})
	}
	applyOverlayPriorityScores(overlays)
	sortOverlayItems(overlays)
	return overlays
}

func buildProductResultOverlays(
	run *horizonmodels.SimulationRun,
	actorReactions []map[string]any,
	protocolSummary []map[string]any,
	transactionSummary []map[string]any,
	monitoringSignals []map[string]any,
) []map[string]any {
	reactionByClass := actorReactionIndex(actorReactions)
	monitoringByID := monitoringSignalIndex(monitoringSignals)
	overlays := make([]map[string]any, 0, 6)
	for _, product := range scopeDerivedProducts(run) {
		productID := "product_availability_" + scopeSafeSlug(scopeLastPathSegment(product.ProductPath))
		reaction := reactionByClass[product.AgentClass]
		actions := overlayDominantActions(reaction)
		functions := []string{"product", "operations", "procurement"}
		disciplines := []string{"product_program", "supply_operations"}
		category := overlayRiskCategory(actions, functions, disciplines)
		executionScore := overlayExecutionPressure(actions, protocolSummary, transactionSummary, disciplines)
		overlays = append(overlays, map[string]any{
			"id":                  productID,
			"title":               titleWords(product.ProductName),
			"kind":                "product_family",
			"productPath":         product.ProductPath,
			"targetRefs":          compactStrings(product.OriginRefs),
			"riskCategory":        category,
			"dominantMechanisms":  overlayMechanisms(actions, protocolSummary, transactionSummary, functions, disciplines),
			"recommendedActions":  overlayRecommendedActions(actions),
			"functions":           functions,
			"disciplines":         disciplines,
			"pathCount":           product.PathCount,
			"supportingPathCount": product.PathCount,
			"maxStress":           round3(product.MaxStress),
			"stressScore":         round3(clamp01(product.MaxStress)),
			"executionScore":      round3(executionScore),
			"severityRaw":         round3(product.Severity),
			"signalRefs":          compactStrings([]string{productID}),
			"explanation":         productOverlayExplanation(product, actions, monitoringByID[productID]),
			"attentionFocus":      productAttentionFocus(product, actions),
		})
	}
	applyOverlayPriorityScores(overlays)
	sortOverlayItems(overlays)
	return overlays
}

func buildCascadeResultOverlays(
	shoulders []map[string]any,
	products []map[string]any,
	protocolSummary []map[string]any,
	transactionSummary []map[string]any,
) []map[string]any {
	if len(shoulders) == 0 || len(products) == 0 {
		return nil
	}
	protocols := topProtocolTypes(protocolSummary, 2)
	transactions := topTransactionTypes(transactionSummary, 2)
	out := make([]map[string]any, 0, min(len(shoulders)*len(products), 6))
	for _, shoulder := range shoulders[:min(len(shoulders), 3)] {
		for _, product := range products[:min(len(products), 2)] {
			supportingPathCount := min(
				int(floatValue(shoulder, "supportingPathCount", floatValue(shoulder, "pathCount", 0))),
				int(floatValue(product, "supportingPathCount", floatValue(product, "pathCount", 0))),
			)
			score := round3(clamp01((floatValue(shoulder, "score", 0) + floatValue(product, "score", 0)) / 2))
			if score < 0.4 {
				continue
			}
			affectedRoles := []string{"coo", "cfo", "compliance_lead"}
			if hasAnyFold(stringSliceAny(shoulder["disciplines"]), []string{"trade_compliance"}) {
				affectedRoles = append(affectedRoles, "pr_lead")
			}
			out = append(out, map[string]any{
				"id":                  stringValue(shoulder, "id") + "__" + stringValue(product, "id"),
				"kind":                "derived_cascade",
				"sourceId":            stringValue(shoulder, "id"),
				"sourceTitle":         stringValue(shoulder, "title"),
				"targetId":            stringValue(product, "id"),
				"targetTitle":         stringValue(product, "title"),
				"score":               score,
				"riskCategory":        "shoulder_to_product_cascade",
				"affectedRoles":       compactStrings(affectedRoles),
				"supportingPathCount": supportingPathCount,
				"explanation": fmt.Sprintf(
					"Pressure on %s is likely to cascade into %s through %s and %s.",
					stringValue(shoulder, "title"),
					stringValue(product, "title"),
					strings.Join(protocols, ", "),
					strings.Join(transactions, ", "),
				),
				"attentionFocus": fmt.Sprintf(
					"Watch whether %s starts forcing priority changes or customer slippage in %s.",
					stringValue(shoulder, "title"),
					stringValue(product, "title"),
				),
			})
		}
	}
	sortOverlayItems(out)
	return out[:min(len(out), 6)]
}

func buildPathChainResultOverlays(
	run *horizonmodels.SimulationRun,
	shoulders []map[string]any,
	products []map[string]any,
	protocolSummary []map[string]any,
	transactionSummary []map[string]any,
) []map[string]any {
	if len(shoulders) == 0 || len(products) == 0 {
		return nil
	}
	artifactChains := artifactChainIndex(run)
	protocols := topProtocolTypes(protocolSummary, 3)
	transactions := topTransactionTypes(transactionSummary, 3)
	out := make([]map[string]any, 0, min(len(shoulders)*len(products), 6))
	for _, shoulder := range shoulders[:min(len(shoulders), 3)] {
		for _, product := range products[:min(len(products), 2)] {
			chainSignal, hasChainSignal := artifactChains[pathChainKey(stringValue(shoulder, "targetCountryCode"), stringValue(product, "productPath"))]
			if len(artifactChains) > 0 && !hasChainSignal {
				continue
			}
			supportingPathCount := min(
				int(floatValue(shoulder, "supportingPathCount", floatValue(shoulder, "pathCount", 0))),
				int(floatValue(product, "supportingPathCount", floatValue(product, "pathCount", 0))),
			)
			targetRefs := compactStrings(append(stringSliceAny(shoulder["targetRefs"]), stringSliceAny(product["targetRefs"])...))
			stressScore := round3(maxFloat(floatValue(shoulder, "stressScore", 0), floatValue(product, "stressScore", 0)))
			coverageScore := 0.0
			if hasChainSignal {
				if chainSignal.PathCount > 0 {
					supportingPathCount = chainSignal.PathCount
				}
				if len(chainSignal.Refs) > 0 {
					targetRefs = compactStrings(chainSignal.Refs)
				}
				stressScore = round3(maxFloat(stressScore, chainSignal.MaxStress))
				coverageScore = chainSignal.Score
			}
			executionScore := round3(maxFloat(floatValue(shoulder, "executionScore", 0), floatValue(product, "executionScore", 0)))
			score := round3(clamp01((floatValue(shoulder, "score", 0) + floatValue(product, "score", 0)) / 2))
			if score < 0.4 && !hasChainSignal {
				continue
			}
			out = append(out, map[string]any{
				"id":                  "path_chain_" + stringValue(shoulder, "id") + "__" + stringValue(product, "id"),
				"title":               stringValue(shoulder, "title") + " -> " + stringValue(product, "title"),
				"kind":                "path_chain",
				"score":               score,
				"riskCategory":        "path_chain_stress",
				"functions":           compactStrings(append(stringSliceAny(shoulder["functions"]), stringSliceAny(product["functions"])...)),
				"disciplines":         compactStrings(append(stringSliceAny(shoulder["disciplines"]), stringSliceAny(product["disciplines"])...)),
				"targetRefs":          targetRefs,
				"supportingPathCount": supportingPathCount,
				"stressScore":         stressScore,
				"severityRaw":         round3(coverageScore),
				"executionScore":      executionScore,
				"sourcePriorityScore": round3(floatValue(shoulder, "priorityScore", floatValue(shoulder, "score", 0))),
				"targetPriorityScore": round3(floatValue(product, "priorityScore", floatValue(product, "score", 0))),
				"dominantMechanisms":  compactStrings(append(append(protocols, transactions...), append(stringSliceAny(shoulder["dominantMechanisms"]), stringSliceAny(product["dominantMechanisms"])...)...)),
				"recommendedActions":  compactNarrativeLines(append(stringSliceAny(shoulder["recommendedActions"]), stringSliceAny(product["recommendedActions"])...)),
				"sourceId":            stringValue(shoulder, "id"),
				"targetId":            stringValue(product, "id"),
				"explanation": fmt.Sprintf(
					"%s and %s form an active chain under pressure through %s and %s.",
					stringValue(shoulder, "title"),
					stringValue(product, "title"),
					strings.Join(protocols, ", "),
					strings.Join(transactions, ", "),
				),
				"attentionFocus": fmt.Sprintf(
					"Watch whether %s can keep %s supplied without routing, allocation, and inventory drifting apart.",
					stringValue(shoulder, "title"),
					stringValue(product, "title"),
				),
			})
		}
	}
	applyPathChainPriorityScores(out)
	sortOverlayItems(out)
	return out[:min(len(out), 6)]
}

func buildRoleResultOverlays(roleReadouts map[string]any, shoulders, products, pathChains, cascades []map[string]any) []map[string]any {
	roleSpecs := roleSpecs()
	roleIndex := map[string]roleSpec{}
	for _, spec := range roleSpecs {
		roleIndex[spec.ID] = spec
	}
	out := make([]map[string]any, 0, len(roleReadouts))
	for roleID, raw := range roleReadouts {
		readout := mapValue(raw)
		if len(readout) == 0 {
			continue
		}
		spec, ok := roleIndex[roleID]
		if !ok {
			continue
		}
		relevantShoulders := filterOverlayTargetsForRole(shoulders, spec.Functions, spec.Disciplines)
		relevantProducts := filterOverlayTargetsForRole(products, spec.Functions, spec.Disciplines)
		relevantPathChains := filterOverlayTargetsForRole(pathChains, spec.Functions, spec.Disciplines)
		relevantCascades := filterCascadeTargetsForRole(cascades, roleID)
		out = append(out, map[string]any{
			"role":                roleID,
			"title":               firstString(readout, "title"),
			"primaryLens":         spec.PrimaryLens,
			"priorityFunctions":   stringSliceAny(readout["priorityFunctions"]),
			"priorityDisciplines": stringSliceAny(readout["priorityDisciplines"]),
			"summary":             stringSliceAny(readout["summary"]),
			"immediateActions":    stringSliceAny(readout["immediateActions"]),
			"weeklyMonitoring":    stringSliceAny(readout["weeklyMonitoring"]),
			"decisionDemands":     stringSliceAny(readout["decisionDemands"]),
			"targets": map[string]any{
				"shoulders":  relevantShoulders,
				"products":   relevantProducts,
				"pathChains": relevantPathChains,
				"cascades":   relevantCascades,
			},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return stringValue(out[i], "role") < stringValue(out[j], "role")
	})
	return out
}

func actorReactionIndex(actorReactions []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(actorReactions))
	for _, reaction := range actorReactions {
		cls := strings.TrimSpace(stringValue(reaction, "actorClass"))
		if cls == "" {
			continue
		}
		out[cls] = reaction
	}
	return out
}

func monitoringSignalIndex(items []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(items))
	for _, item := range items {
		id := strings.TrimSpace(stringValue(item, "id"))
		if id == "" {
			continue
		}
		out[id] = item
	}
	return out
}

func overlayDominantActions(reaction map[string]any) []string {
	if len(reaction) == 0 {
		return nil
	}
	return stringSliceAny(reaction["dominantActions"])
}

func overlayExecutionPressure(actions []string, protocolSummary, transactionSummary []map[string]any, disciplines []string) float64 {
	score := 0.0
	if hasAnyFold(actions, []string{
		string(horizonmodels.SimulationActionAllocateCapacity),
		string(horizonmodels.SimulationActionReserveInventory),
		string(horizonmodels.SimulationActionReprioritizeFlow),
	}) {
		score += 0.38
	}
	if hasAnyFold(actions, []string{
		string(horizonmodels.SimulationActionRerouteShipment),
		string(horizonmodels.SimulationActionSwitchSupplier),
		string(horizonmodels.SimulationActionExpediteShipment),
	}) {
		score += 0.34
	}
	if hasAnyFold(actions, []string{
		string(horizonmodels.SimulationActionRequestExemption),
		string(horizonmodels.SimulationActionIssueStatement),
	}) {
		score += 0.18
	}
	if hasAnyFold(disciplines, []string{"trade_compliance", "policy_compliance"}) {
		score += 0.12
	}
	score += 0.05 * float64(min(len(topProtocolTypes(protocolSummary, 3)), 3))
	score += 0.04 * float64(min(len(topTransactionTypes(transactionSummary, 3)), 3))
	return clamp01(score)
}

func overlayRiskCategory(actions, functions, disciplines []string) string {
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionIssueStatement)}) ||
		hasAnyFold(disciplines, []string{"trade_compliance", "policy_compliance"}) {
		return "compliance_overhead"
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionReprioritizeFlow)}) {
		return "capacity_pressure"
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionSwitchSupplier)}) ||
		hasAnyFold(functions, []string{"logistics"}) {
		return "routing_overhead"
	}
	return "operating_risk"
}

func overlayMechanisms(actions []string, protocolSummary, transactionSummary []map[string]any, functions, disciplines []string) []string {
	out := make([]string, 0, 8)
	out = append(out, actions...)
	out = append(out, topProtocolTypes(protocolSummary, 2)...)
	out = append(out, topTransactionTypes(transactionSummary, 2)...)
	if hasAnyFold(disciplines, []string{"trade_compliance"}) {
		out = append(out, "review_queue")
	}
	return compactStrings(out)
}

func topProtocolTypes(protocolSummary []map[string]any, limit int) []string {
	out := make([]string, 0, min(len(protocolSummary), limit))
	for _, item := range protocolSummary[:min(len(protocolSummary), limit)] {
		out = append(out, stringValue(item, "protocolType"))
	}
	return compactStrings(out)
}

func topTransactionTypes(transactionSummary []map[string]any, limit int) []string {
	out := make([]string, 0, min(len(transactionSummary), limit))
	for _, item := range transactionSummary[:min(len(transactionSummary), limit)] {
		out = append(out, stringValue(item, "transactionType"))
	}
	return compactStrings(out)
}

func overlayRecommendedActions(actions []string) []string {
	if len(actions) == 0 {
		return []string{"Hold current routing and inventory priorities explicit."}
	}
	lines := make([]string, 0, min(len(actions), 3))
	for _, action := range actions[:min(len(actions), 3)] {
		switch action {
		case string(horizonmodels.SimulationActionRerouteShipment):
			lines = append(lines, "Keep reroute rules explicit and tied to priority lanes.")
		case string(horizonmodels.SimulationActionReserveInventory):
			lines = append(lines, "Protect scarce inventory before downstream commitments drift.")
		case string(horizonmodels.SimulationActionAllocateCapacity):
			lines = append(lines, "Lock capacity allocation rules around priority programs.")
		case string(horizonmodels.SimulationActionReprioritizeFlow):
			lines = append(lines, "Reprioritize flows before customer promises and internal schedules diverge.")
		case string(horizonmodels.SimulationActionRequestExemption):
			lines = append(lines, "Treat exemptions and reviews as part of the execution path, not a side task.")
		case string(horizonmodels.SimulationActionAdjustBudget):
			lines = append(lines, "Reflect resilience cost explicitly in budget and margin control.")
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "Keep routing, inventory, and execution rules synchronized around the active risk.")
	}
	return compactNarrativeLines(lines)
}

func shoulderOverlayExplanation(shoulder scopeDerivedShoulder, actions []string, monitoring map[string]any) string {
	actionText := "active routing and reservation actions"
	if len(actions) > 0 {
		actionText = strings.Join(actions[:min(len(actions), 3)], ", ")
	}
	why := strings.TrimSpace(stringValue(monitoring, "why"))
	if why == "" {
		why = fmt.Sprintf("%s is carrying live external shoulder pressure in the active chain.", shoulder.CountryName)
	}
	return fmt.Sprintf(
		"%s supports %d materialized paths with max stress %.3f and is showing %s. %s",
		shoulder.CountryName,
		shoulder.PathCount,
		shoulder.MaxStress,
		actionText,
		why,
	)
}

func productOverlayExplanation(product scopeDerivedProduct, actions []string, monitoring map[string]any) string {
	actionText := "reprioritization and inventory control"
	if len(actions) > 0 {
		actionText = strings.Join(actions[:min(len(actions), 3)], ", ")
	}
	why := strings.TrimSpace(stringValue(monitoring, "why"))
	if why == "" {
		why = fmt.Sprintf("%s is carrying live product-family risk in active scope paths.", product.ProductName)
	}
	return fmt.Sprintf(
		"%s appears in %d stressed paths with max stress %.3f and is already pulling %s. %s",
		titleWords(product.ProductName),
		product.PathCount,
		product.MaxStress,
		actionText,
		why,
	)
}

func shoulderAttentionFocus(shoulder scopeDerivedShoulder, actions []string) string {
	focus := []string{
		fmt.Sprintf("Watch how many active paths still depend on %s.", shoulder.CountryName),
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionSwitchSupplier)}) {
		focus = append(focus, "Reroute volume is already a live signal here.")
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionIssueStatement)}) {
		focus = append(focus, "Review and exception handling are part of the execution path.")
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionReserveInventory)}) {
		focus = append(focus, "Capacity and inventory are already being protected around this shoulder.")
	}
	return strings.Join(compactNarrativeLines(focus), " ")
}

func productAttentionFocus(product scopeDerivedProduct, actions []string) string {
	focus := []string{
		fmt.Sprintf("Watch whether %s starts forcing promise or schedule changes.", titleWords(product.ProductName)),
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionReserveInventory)}) {
		focus = append(focus, "Reservation pressure is already visible.")
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionReprioritizeFlow)}) {
		focus = append(focus, "This family is already being reprioritized against other demand.")
	}
	if hasAnyFold(actions, []string{string(horizonmodels.SimulationActionAllocateCapacity)}) {
		focus = append(focus, "Capacity protection is part of the active response.")
	}
	return strings.Join(compactNarrativeLines(focus), " ")
}

func filterOverlayTargetsForRole(items []map[string]any, functions, disciplines []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if hasAnyFold(stringSliceAny(item["functions"]), functions) || hasAnyFold(stringSliceAny(item["disciplines"]), disciplines) {
			out = append(out, stringValue(item, "id"))
		}
	}
	return compactStrings(out)
}

func filterCascadeTargetsForRole(items []map[string]any, roleID string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if containsFold(stringSliceAny(item["affectedRoles"]), roleID) {
			out = append(out, stringValue(item, "id"))
		}
	}
	return compactStrings(out)
}

type artifactChainSignal struct {
	CountryCode string
	ProductPath string
	PathCount   int
	Score       float64
	MaxStress   float64
	Refs        []string
}

func artifactChainIndex(run *horizonmodels.SimulationRun) map[string]artifactChainSignal {
	out := map[string]artifactChainSignal{}
	if run == nil {
		return out
	}
	for _, raw := range mapSlice(scopeAnySlice(run.BaselinePreview["artifactChains"])) {
		code := strings.ToLower(strings.TrimSpace(firstString(raw, "countryCode")))
		path := strings.TrimSpace(firstString(raw, "path"))
		if code == "" || path == "" {
			continue
		}
		out[pathChainKey(code, path)] = artifactChainSignal{
			CountryCode: code,
			ProductPath: path,
			PathCount:   int(floatValue(raw, "pathCount", 0)),
			Score:       floatValue(raw, "riskScore", 0),
			MaxStress:   floatValue(raw, "maxStress", 0),
			Refs:        stringSliceAny(raw["refs"]),
		}
	}
	return out
}

func pathChainKey(countryCode, productPath string) string {
	return strings.ToLower(strings.TrimSpace(countryCode)) + "::" + strings.TrimSpace(productPath)
}

func sortOverlayItems(items []map[string]any) {
	sort.Slice(items, func(i, j int) bool {
		li := floatValue(items[i], "score", 0)
		lj := floatValue(items[j], "score", 0)
		if li == lj {
			return stringValue(items[i], "id") < stringValue(items[j], "id")
		}
		return li > lj
	})
}

func applyOverlayPriorityScores(items []map[string]any) {
	if len(items) == 0 {
		return
	}
	maxSeverity := 0.0
	maxPathCount := 0.0
	for _, item := range items {
		maxSeverity = maxFloat(maxSeverity, floatValue(item, "severityRaw", 0))
		maxPathCount = maxFloat(maxPathCount, floatValue(item, "supportingPathCount", floatValue(item, "pathCount", 0)))
	}
	if maxSeverity <= 0 {
		maxSeverity = 1
	}
	if maxPathCount <= 0 {
		maxPathCount = 1
	}
	for _, item := range items {
		stressScore := clamp01(floatValue(item, "stressScore", floatValue(item, "maxStress", 0)))
		coverageScore := clamp01(floatValue(item, "supportingPathCount", floatValue(item, "pathCount", 0)) / maxPathCount)
		executionScore := clamp01(floatValue(item, "executionScore", 0))
		severityScore := clamp01(floatValue(item, "severityRaw", 0) / maxSeverity)
		priorityScore := round3(clamp01(stressScore*0.36 + coverageScore*0.28 + executionScore*0.18 + severityScore*0.18))
		item["stressScore"] = round3(stressScore)
		item["coverageScore"] = round3(coverageScore)
		item["severityScore"] = round3(severityScore)
		item["priorityScore"] = priorityScore
		item["score"] = priorityScore
		item["priorityLabel"] = overlayPriorityLabel(priorityScore)
	}
}

func applyPathChainPriorityScores(items []map[string]any) {
	if len(items) == 0 {
		return
	}
	maxPathCount := 0.0
	for _, item := range items {
		maxPathCount = maxFloat(maxPathCount, floatValue(item, "supportingPathCount", floatValue(item, "pathCount", 0)))
	}
	if maxPathCount <= 0 {
		maxPathCount = 1
	}
	for _, item := range items {
		stressScore := clamp01(floatValue(item, "stressScore", floatValue(item, "maxStress", 0)))
		coverageScore := clamp01(floatValue(item, "supportingPathCount", floatValue(item, "pathCount", 0)) / maxPathCount)
		executionScore := clamp01(floatValue(item, "executionScore", 0))
		sourceScore := clamp01(floatValue(item, "sourcePriorityScore", 0))
		targetScore := clamp01(floatValue(item, "targetPriorityScore", 0))
		priorityScore := round3(clamp01(stressScore*0.28 + coverageScore*0.20 + executionScore*0.18 + sourceScore*0.18 + targetScore*0.16))
		item["stressScore"] = round3(stressScore)
		item["coverageScore"] = round3(coverageScore)
		item["priorityScore"] = priorityScore
		item["score"] = priorityScore
		item["priorityLabel"] = overlayPriorityLabel(priorityScore)
	}
}

func overlayPriorityLabel(score float64) string {
	switch {
	case score >= 0.82:
		return "critical"
	case score >= 0.64:
		return "high"
	case score >= 0.42:
		return "medium"
	default:
		return "watch"
	}
}
