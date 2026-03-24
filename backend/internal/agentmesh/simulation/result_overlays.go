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
	pathChains := buildPathChainResultOverlays(shoulders, products, protocolSummary, transactionSummary)
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
		score := overlayNormalizedRisk(shoulder.Severity)
		actions := overlayDominantActions(reaction)
		functions := []string{"logistics", "procurement", "operations"}
		disciplines := []string{"supply_operations", "trade_compliance"}
		category := overlayRiskCategory(actions, functions, disciplines)
		overlays = append(overlays, map[string]any{
			"id":                 monitoringID,
			"title":              shoulder.CountryName + " shoulder",
			"kind":               "regional_shoulder",
			"targetRefs":         compactStrings(shoulder.OriginRefs),
			"targetCountryCode":  shoulder.CountryCode,
			"score":              round3(score),
			"riskCategory":       category,
			"dominantMechanisms": overlayMechanisms(actions, protocolSummary, transactionSummary, functions, disciplines),
			"recommendedActions": overlayRecommendedActions(actions),
			"functions":          functions,
			"disciplines":        disciplines,
			"pathCount":          shoulder.PathCount,
			"maxStress":          round3(shoulder.MaxStress),
			"signalRefs":         compactStrings([]string{monitoringID}),
			"explanation":        shoulderOverlayExplanation(shoulder, actions, monitoringByID[monitoringID]),
		})
	}
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
		score := overlayNormalizedRisk(product.Severity)
		actions := overlayDominantActions(reaction)
		functions := []string{"product", "operations", "procurement"}
		disciplines := []string{"product_program", "supply_operations"}
		category := overlayRiskCategory(actions, functions, disciplines)
		overlays = append(overlays, map[string]any{
			"id":                 productID,
			"title":              titleWords(product.ProductName),
			"kind":               "product_family",
			"productPath":        product.ProductPath,
			"targetRefs":         compactStrings(product.OriginRefs),
			"score":              round3(score),
			"riskCategory":       category,
			"dominantMechanisms": overlayMechanisms(actions, protocolSummary, transactionSummary, functions, disciplines),
			"recommendedActions": overlayRecommendedActions(actions),
			"functions":          functions,
			"disciplines":        disciplines,
			"pathCount":          product.PathCount,
			"maxStress":          round3(product.MaxStress),
			"signalRefs":         compactStrings([]string{productID}),
			"explanation":        productOverlayExplanation(product, actions, monitoringByID[productID]),
		})
	}
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
			score := round3(clamp01((floatValue(shoulder, "score", 0) + floatValue(product, "score", 0)) / 2))
			if score < 0.4 {
				continue
			}
			affectedRoles := []string{"coo", "cfo", "compliance_lead"}
			if hasAnyFold(stringSliceAny(shoulder["disciplines"]), []string{"trade_compliance"}) {
				affectedRoles = append(affectedRoles, "pr_lead")
			}
			out = append(out, map[string]any{
				"id":            stringValue(shoulder, "id") + "__" + stringValue(product, "id"),
				"kind":          "derived_cascade",
				"sourceId":      stringValue(shoulder, "id"),
				"sourceTitle":   stringValue(shoulder, "title"),
				"targetId":      stringValue(product, "id"),
				"targetTitle":   stringValue(product, "title"),
				"score":         score,
				"riskCategory":  "shoulder_to_product_cascade",
				"affectedRoles": compactStrings(affectedRoles),
				"explanation": fmt.Sprintf(
					"Pressure on %s is likely to cascade into %s through %s and %s.",
					stringValue(shoulder, "title"),
					stringValue(product, "title"),
					strings.Join(protocols, ", "),
					strings.Join(transactions, ", "),
				),
			})
		}
	}
	sortOverlayItems(out)
	return out[:min(len(out), 6)]
}

func buildPathChainResultOverlays(
	shoulders []map[string]any,
	products []map[string]any,
	protocolSummary []map[string]any,
	transactionSummary []map[string]any,
) []map[string]any {
	if len(shoulders) == 0 || len(products) == 0 {
		return nil
	}
	protocols := topProtocolTypes(protocolSummary, 3)
	transactions := topTransactionTypes(transactionSummary, 3)
	out := make([]map[string]any, 0, min(len(shoulders)*len(products), 6))
	for _, shoulder := range shoulders[:min(len(shoulders), 3)] {
		for _, product := range products[:min(len(products), 2)] {
			score := round3(clamp01((floatValue(shoulder, "score", 0) + floatValue(product, "score", 0)) / 2))
			if score < 0.4 {
				continue
			}
			targetRefs := compactStrings(append(stringSliceAny(shoulder["targetRefs"]), stringSliceAny(product["targetRefs"])...))
			out = append(out, map[string]any{
				"id":                 "path_chain_" + stringValue(shoulder, "id") + "__" + stringValue(product, "id"),
				"title":              stringValue(shoulder, "title") + " -> " + stringValue(product, "title"),
				"kind":               "path_chain",
				"score":              score,
				"riskCategory":       "path_chain_stress",
				"functions":          compactStrings(append(stringSliceAny(shoulder["functions"]), stringSliceAny(product["functions"])...)),
				"disciplines":        compactStrings(append(stringSliceAny(shoulder["disciplines"]), stringSliceAny(product["disciplines"])...)),
				"targetRefs":         targetRefs,
				"dominantMechanisms": compactStrings(append(append(protocols, transactions...), append(stringSliceAny(shoulder["dominantMechanisms"]), stringSliceAny(product["dominantMechanisms"])...)...)),
				"recommendedActions": compactNarrativeLines(append(stringSliceAny(shoulder["recommendedActions"]), stringSliceAny(product["recommendedActions"])...)),
				"sourceId":           stringValue(shoulder, "id"),
				"targetId":           stringValue(product, "id"),
				"explanation": fmt.Sprintf(
					"%s and %s form an active chain under pressure through %s and %s.",
					stringValue(shoulder, "title"),
					stringValue(product, "title"),
					strings.Join(protocols, ", "),
					strings.Join(transactions, ", "),
				),
			})
		}
	}
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

func overlayNormalizedRisk(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return clamp01(value / 2.0)
}

func overlayDominantActions(reaction map[string]any) []string {
	if len(reaction) == 0 {
		return nil
	}
	return stringSliceAny(reaction["dominantActions"])
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
