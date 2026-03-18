package horizon

import (
	"fmt"
	"math"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type scoredMechanism struct {
	Key           string
	Score         float64
	EvidenceCount int
}

type scoredProduct struct {
	Ref       string
	Score     float64
	PathCount int
	LinkCount int
}

type scoredNode struct {
	Ref       string
	Score     float64
	PathCount int
	Role      string
}

type scoredMetric struct {
	Key        string
	Value      float64
	Unit       string
	Scope      string
	Importance float64
}

func annotateSupplyChainArtifactRiskOverlays(artifacts []*horizonmodels.MaterializedArtifact) {
	for _, artifact := range artifacts {
		annotateSupplyChainArtifactRiskOverlay(artifact)
	}
}

func annotateSupplyChainArtifactRiskOverlay(artifact *horizonmodels.MaterializedArtifact) {
	if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return
	}
	if artifact.RiskOverlay != nil {
		return
	}
	artifact.RiskOverlay = buildSupplyChainPathRiskOverlay(artifact)
}

func buildSupplyChainPathRiskOverlay(artifact *horizonmodels.MaterializedArtifact) *horizonmodels.SupplyChainPathRiskOverlay {
	if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return nil
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return nil
	}

	segmentEdges := make(map[int][]horizonmodels.SupplyChainPathEdge)
	for _, edge := range parsed.Edges {
		if edge.SegmentIndex == nil {
			continue
		}
		idx := *edge.SegmentIndex
		segmentEdges[idx] = append(segmentEdges[idx], edge)
	}

	mechanisms := map[string]*scoredMechanism{}
	products := map[string]*scoredProduct{}
	bottlenecks := map[string]*scoredNode{}
	metrics := map[string]*scoredMetric{}
	flags := map[string]struct{}{}
	legs := make([]horizonmodels.SupplyChainLegRiskOverlay, 0, len(parsed.Legs))
	maxCriticality := 0.0
	topProductRef := ""

	addMechanism := func(key string, score float64) {
		key = strings.TrimSpace(key)
		if key == "" || score <= 0 {
			return
		}
		entry := mechanisms[key]
		if entry == nil {
			entry = &scoredMechanism{Key: key}
			mechanisms[key] = entry
		}
		entry.Score += score
		entry.EvidenceCount++
	}
	addProduct := func(ref string, score float64) {
		ref = strings.TrimSpace(ref)
		if ref == "" || score <= 0 {
			return
		}
		entry := products[ref]
		if entry == nil {
			entry = &scoredProduct{Ref: ref}
			products[ref] = entry
		}
		entry.Score += score
		entry.PathCount++
		entry.LinkCount++
		if entry.Score > maxCriticality {
			maxCriticality = entry.Score
			topProductRef = ref
		}
	}
	addNode := func(ref string, role string, score float64) {
		ref = strings.TrimSpace(ref)
		if ref == "" || score <= 0 {
			return
		}
		entry := bottlenecks[ref]
		if entry == nil {
			entry = &scoredNode{Ref: ref, Role: role}
			bottlenecks[ref] = entry
		}
		entry.Score += score
		entry.PathCount++
	}
	addMetric := func(key string, value float64, unit string, scope string, importance float64) {
		key = strings.TrimSpace(key)
		if key == "" || importance <= 0 || !isFinite(value) {
			return
		}
		if existing := metrics[key]; existing != nil {
			if importance > existing.Importance {
				existing.Value = value
				existing.Unit = unit
				existing.Scope = scope
				existing.Importance = importance
			}
			return
		}
		metrics[key] = &scoredMetric{
			Key:        key,
			Value:      value,
			Unit:       unit,
			Scope:      scope,
			Importance: importance,
		}
	}

	pathStressScore := sanitizeUnitInterval(artifact.PathStressScore)
	if pathStressScore == 0 && parsed.Stress != nil {
		pathStressScore = sanitizeUnitInterval(parsed.Stress.PathScore)
	}
	if pathStressScore > 0 {
		addMetric("path_stress_score", pathStressScore, "", "path", pathStressScore)
	}

	for _, leg := range parsed.Legs {
		legEdges := segmentEdges[leg.Index]
		legMechanisms := map[string]*scoredMechanism{}
		legMetrics := map[string]*scoredMetric{}
		legCriticality := legCriticalityScore(leg, legEdges)
		if legCriticality > maxCriticality {
			maxCriticality = legCriticality
		}
		productRef := strings.TrimSpace(leg.ProductRef)
		if productRef == "" {
			productRef = firstNonEmptyPathProductRef(legEdges)
		}
		addProduct(productRef, legCriticality)

		legFlags := collectLegFlags(leg, legEdges)
		for _, flag := range legFlags {
			flags[flag] = struct{}{}
			mechanismKey := flagToPrimaryMechanism(flag)
			weight := flagToMechanismWeight(flag)
			addMechanism(mechanismKey, weight)
			addMechanismScore(legMechanisms, mechanismKey, weight)
		}
		for _, edge := range legEdges {
			primaryMechanism := strings.TrimSpace(stringQualifier(edge.Qualifiers, "primaryRiskMechanism"))
			if primaryMechanism != "" {
				score := 0.85 + overlayMetricValue(edge.Metrics, "supply_chain_criticality")*0.2
				addMechanism(primaryMechanism, score)
				addMechanismScore(legMechanisms, primaryMechanism, score)
			}
			applyMetricMechanismSignals(mechanisms, metrics, edge.Metrics, "link")
			applyMetricMechanismSignals(legMechanisms, legMetrics, edge.Metrics, "link")
		}
		applyLegStressMechanismSignals(mechanisms, metrics, &leg, legCriticality)
		applyLegStressMechanismSignals(legMechanisms, legMetrics, &leg, legCriticality)

		if isLegBottleneck(parsed.Stress, &leg) {
			for _, ref := range leg.TransportViaRefs {
				addNode(ref, "transport", legCriticality+0.2)
			}
			if len(leg.TransportViaRefs) == 0 {
				for _, ref := range leg.ZoneRefs {
					addNode(ref, "zone", legCriticality+0.1)
				}
			}
		}

		topLegMechanisms := topOverlayMechanisms(legMechanisms, 3)
		topLegMetrics := topOverlayMetrics(legMetrics, 4)
		primaryMechanism := ""
		if len(topLegMechanisms) > 0 {
			primaryMechanism = topLegMechanisms[0].Key
		}
		legs = append(legs, horizonmodels.SupplyChainLegRiskOverlay{
			SegmentIndex:     leg.Index,
			Summary:          buildSupplyChainLegSummary(productRef, primaryMechanism, legCriticality, isLegBottleneck(parsed.Stress, &leg), leg.TransportViaRefs),
			PrimaryMechanism: primaryMechanism,
			Criticality:      legCriticality,
			StressScore:      sanitizeUnitInterval(legStressScore(&leg)),
			Bottleneck:       isLegBottleneck(parsed.Stress, &leg),
			ProductRef:       productRef,
			ProductLabel:     overlayLabelForRef(productRef),
			SupplierRef:      strings.TrimSpace(leg.SupplierRef),
			SupplierLabel:    overlayLabelForRef(leg.SupplierRef),
			CustomerRef:      strings.TrimSpace(leg.CustomerRef),
			CustomerLabel:    overlayLabelForRef(leg.CustomerRef),
			TransportViaRefs: append([]string(nil), leg.TransportViaRefs...),
			Flags:            legFlags,
			Mechanisms:       topLegMechanisms,
			DriverMetrics:    topLegMetrics,
		})
	}

	topMechanisms := topOverlayMechanisms(mechanisms, 5)
	topProducts := topOverlayProducts(products, 4)
	topNodes := topOverlayNodes(bottlenecks, 4)
	topMetrics := topOverlayMetrics(metrics, 5)
	primaryMechanism := ""
	if len(topMechanisms) > 0 {
		primaryMechanism = topMechanisms[0].Key
	}
	stressScore := pathStressScore
	if stressScore == 0 {
		stressScore = maxLegStress(parsed.Legs)
	}
	criticality := math.Max(stressScore, clampUnitInterval(maxCriticality))
	if criticality == 0 && len(topMechanisms) > 0 {
		criticality = clampUnitInterval(topMechanisms[0].Score)
	}

	return &horizonmodels.SupplyChainPathRiskOverlay{
		Scope:              "path",
		Summary:            buildSupplyChainPathSummary(topProductRef, primaryMechanism, criticality, parsed.Stress, topNodes),
		PrimaryMechanism:   primaryMechanism,
		Criticality:        criticality,
		StressScore:        stressScore,
		BottleneckLegIndex: bottleneckLegIndex(parsed.Stress),
		Flags:              sortedStringKeys(flags),
		Mechanisms:         topMechanisms,
		Products:           topProducts,
		Bottlenecks:        topNodes,
		DriverMetrics:      topMetrics,
		Legs:               legs,
	}
}

func buildSupplyChainObjectRiskOverlay(centerFrag *horizonmodels.HorizonFragment, chains []*horizonmodels.MaterializedArtifact, fragmentMap map[string]*horizonmodels.HorizonFragment) *horizonmodels.SupplyChainObjectRiskOverlay {
	if centerFrag == nil || len(chains) == 0 {
		return nil
	}
	products := map[string]*scoredProduct{}
	mechanisms := map[string]*scoredMechanism{}
	bottlenecks := map[string]*scoredNode{}
	metrics := map[string]*scoredMetric{}
	topPaths := make([]horizonmodels.SupplyChainOverlayPathRef, 0, len(chains))
	stressedCount := 0
	maxCriticality := 0.0

	for _, chain := range chains {
		if chain == nil {
			continue
		}
		annotateSupplyChainArtifactRiskOverlay(chain)
		overlay := chain.RiskOverlay
		if overlay == nil {
			continue
		}
		weight := math.Max(overlay.Criticality, overlay.StressScore)
		if weight >= 0.58 {
			stressedCount++
		}
		if weight > maxCriticality {
			maxCriticality = weight
		}
		for _, product := range overlay.Products {
			entry := products[product.Ref]
			if entry == nil {
				entry = &scoredProduct{Ref: product.Ref}
				products[product.Ref] = entry
			}
			entry.Score += weight + product.Score*0.2
			entry.PathCount++
			entry.LinkCount += maxInt(1, product.LinkCount)
		}
		for _, mechanism := range overlay.Mechanisms {
			entry := mechanisms[mechanism.Key]
			if entry == nil {
				entry = &scoredMechanism{Key: mechanism.Key}
				mechanisms[mechanism.Key] = entry
			}
			entry.Score += weight + mechanism.Score*0.25
			entry.EvidenceCount += maxInt(1, mechanism.EvidenceCount)
		}
		if len(overlay.Mechanisms) == 0 && strings.TrimSpace(overlay.PrimaryMechanism) != "" {
			entry := mechanisms[strings.TrimSpace(overlay.PrimaryMechanism)]
			if entry == nil {
				entry = &scoredMechanism{Key: strings.TrimSpace(overlay.PrimaryMechanism)}
				mechanisms[entry.Key] = entry
			}
			entry.Score += weight
			entry.EvidenceCount++
		}
		for _, node := range overlay.Bottlenecks {
			entry := bottlenecks[node.Ref]
			if entry == nil {
				entry = &scoredNode{Ref: node.Ref, Role: node.Role}
				bottlenecks[node.Ref] = entry
			}
			entry.Score += weight + node.Score*0.25
			entry.PathCount++
		}
		for _, metric := range overlay.DriverMetrics {
			addOverlayMetricAggregate(metrics, metric, weight)
		}
		topPaths = append(topPaths, buildOverlayPathRef(chain, fragmentMap))
	}

	sort.SliceStable(topPaths, func(i, j int) bool {
		if topPaths[i].Criticality == topPaths[j].Criticality {
			return topPaths[i].StressScore > topPaths[j].StressScore
		}
		return topPaths[i].Criticality > topPaths[j].Criticality
	})
	if len(topPaths) > 6 {
		topPaths = topPaths[:6]
	}

	topMechanisms := topOverlayMechanisms(mechanisms, 5)
	topProducts := topOverlayProducts(products, 4)
	topBottlenecks := topOverlayNodes(bottlenecks, 4)
	topMetrics := topOverlayMetrics(metrics, 5)
	primaryMechanism := ""
	if len(topMechanisms) > 0 {
		primaryMechanism = topMechanisms[0].Key
	}
	return &horizonmodels.SupplyChainObjectRiskOverlay{
		Scope:             "object",
		FragmentID:        strings.TrimSpace(centerFrag.ID),
		FragmentRef:       strings.TrimSpace(centerFrag.CanonicalPath),
		FragmentLabel:     fragmentDisplayName(centerFrag),
		Summary:           buildSupplyChainObjectSummary(fragmentDisplayName(centerFrag), primaryMechanism, maxCriticality, topProducts, topBottlenecks),
		PrimaryMechanism:  primaryMechanism,
		Criticality:       clampUnitInterval(maxCriticality),
		IncidentPathCount: len(topPaths),
		StressedPathCount: stressedCount,
		TopProducts:       topProducts,
		TopMechanisms:     topMechanisms,
		TopBottlenecks:    topBottlenecks,
		DriverMetrics:     topMetrics,
		TopPaths:          topPaths,
	}
}

func buildSupplyChainLensRiskOverlay(chains []*horizonmodels.MaterializedArtifact) *horizonmodels.SupplyChainLensRiskOverlay {
	if len(chains) == 0 {
		return nil
	}
	products := map[string]*scoredProduct{}
	mechanisms := map[string]*scoredMechanism{}
	bottlenecks := map[string]*scoredNode{}
	nodes := map[string]*scoredNode{}
	metrics := map[string]*scoredMetric{}
	topPaths := make([]horizonmodels.SupplyChainOverlayPathRef, 0, len(chains))
	stressed := 0
	maxCriticality := 0.0

	for _, chain := range chains {
		if chain == nil {
			continue
		}
		annotateSupplyChainArtifactRiskOverlay(chain)
		overlay := chain.RiskOverlay
		if overlay == nil {
			continue
		}
		weight := math.Max(overlay.Criticality, overlay.StressScore)
		if weight >= 0.58 {
			stressed++
		}
		if weight > maxCriticality {
			maxCriticality = weight
		}
		for _, product := range overlay.Products {
			entry := products[product.Ref]
			if entry == nil {
				entry = &scoredProduct{Ref: product.Ref}
				products[product.Ref] = entry
			}
			entry.Score += weight + product.Score*0.2
			entry.PathCount++
			entry.LinkCount += maxInt(1, product.LinkCount)
		}
		for _, mechanism := range overlay.Mechanisms {
			entry := mechanisms[mechanism.Key]
			if entry == nil {
				entry = &scoredMechanism{Key: mechanism.Key}
				mechanisms[mechanism.Key] = entry
			}
			entry.Score += weight + mechanism.Score*0.25
			entry.EvidenceCount += maxInt(1, mechanism.EvidenceCount)
		}
		if len(overlay.Mechanisms) == 0 && strings.TrimSpace(overlay.PrimaryMechanism) != "" {
			entry := mechanisms[strings.TrimSpace(overlay.PrimaryMechanism)]
			if entry == nil {
				entry = &scoredMechanism{Key: strings.TrimSpace(overlay.PrimaryMechanism)}
				mechanisms[entry.Key] = entry
			}
			entry.Score += weight
			entry.EvidenceCount++
		}
		for _, bottleneck := range overlay.Bottlenecks {
			entry := bottlenecks[bottleneck.Ref]
			if entry == nil {
				entry = &scoredNode{Ref: bottleneck.Ref, Role: bottleneck.Role}
				bottlenecks[bottleneck.Ref] = entry
			}
			entry.Score += weight + bottleneck.Score*0.25
			entry.PathCount++
		}
		for _, leg := range overlay.Legs {
			for _, ref := range []struct {
				ref  string
				role string
			}{
				{ref: leg.SupplierRef, role: "supplier"},
				{ref: leg.CustomerRef, role: "customer"},
			} {
				cleanRef := strings.TrimSpace(ref.ref)
				if cleanRef == "" {
					continue
				}
				entry := nodes[cleanRef]
				if entry == nil {
					entry = &scoredNode{Ref: cleanRef, Role: ref.role}
					nodes[cleanRef] = entry
				}
				entry.Score += leg.Criticality
				entry.PathCount++
			}
		}
		for _, metric := range overlay.DriverMetrics {
			addOverlayMetricAggregate(metrics, metric, weight)
		}
		topPaths = append(topPaths, buildOverlayPathRef(chain, nil))
	}

	sort.SliceStable(topPaths, func(i, j int) bool {
		if topPaths[i].Criticality == topPaths[j].Criticality {
			return topPaths[i].StressScore > topPaths[j].StressScore
		}
		return topPaths[i].Criticality > topPaths[j].Criticality
	})
	if len(topPaths) > 8 {
		topPaths = topPaths[:8]
	}

	topMechanisms := topOverlayMechanisms(mechanisms, 6)
	topProducts := topOverlayProducts(products, 6)
	topBottlenecks := topOverlayNodes(bottlenecks, 6)
	topNodes := topOverlayNodes(nodes, 6)
	topMetrics := topOverlayMetrics(metrics, 6)
	primaryMechanism := ""
	if len(topMechanisms) > 0 {
		primaryMechanism = topMechanisms[0].Key
	}
	return &horizonmodels.SupplyChainLensRiskOverlay{
		Scope:            "lens",
		Summary:          buildSupplyChainLensSummary(primaryMechanism, maxCriticality, topProducts, topBottlenecks),
		PrimaryMechanism: primaryMechanism,
		Criticality:      clampUnitInterval(maxCriticality),
		TotalPaths:       len(chains),
		StressedPaths:    stressed,
		TopProducts:      topProducts,
		TopMechanisms:    topMechanisms,
		TopBottlenecks:   topBottlenecks,
		TopNodes:         topNodes,
		DriverMetrics:    topMetrics,
		TopPaths:         topPaths,
	}
}

func addOverlayMetricAggregate(dst map[string]*scoredMetric, metric horizonmodels.SupplyChainOverlayMetric, weight float64) {
	if strings.TrimSpace(metric.Key) == "" || metric.Importance <= 0 {
		return
	}
	if existing := dst[metric.Key]; existing != nil {
		if metric.Importance*weight > existing.Importance {
			existing.Value = metric.Value
			existing.Unit = metric.Unit
			existing.Scope = metric.Scope
			existing.Importance = metric.Importance * weight
		}
		return
	}
	dst[metric.Key] = &scoredMetric{
		Key:        metric.Key,
		Value:      metric.Value,
		Unit:       metric.Unit,
		Scope:      metric.Scope,
		Importance: metric.Importance * weight,
	}
}

func collectLegFlags(leg horizonmodels.SupplyChainPathLeg, edges []horizonmodels.SupplyChainPathEdge) []string {
	flags := map[string]struct{}{}
	for _, edge := range edges {
		rawFlags := interfaceSliceQualifier(edge.Qualifiers, "riskFlags")
		for _, flag := range rawFlags {
			flag = strings.TrimSpace(flag)
			if flag == "" {
				continue
			}
			flags[flag] = struct{}{}
		}
		for _, key := range []string{
			"approved_vendor_only",
			"service_contract_required",
			"export_license_required",
			"sanctions_touchpoint",
			"dual_use_flag",
		} {
			if qualifierTruthy(edge.Qualifiers, key) || overlayMetricValue(edge.Metrics, key) > 0 {
				flags[key] = struct{}{}
			}
		}
	}
	if legStressScore(&leg) >= 0.58 {
		flags["route_fragility"] = struct{}{}
	}
	if isLegBottleneck(nil, &leg) {
		flags["route_bottleneck"] = struct{}{}
	}
	return sortedStringKeys(flags)
}

func applyMetricMechanismSignals(dst map[string]*scoredMechanism, metrics map[string]*scoredMetric, values map[string]horizonmodels.MetricValue, scope string) {
	lead := overlayMetricValue(values, "lead_time_days")
	if lead == 0 {
		lead = overlayMetricValue(values, "lead_time_days_baseline")
	}
	inventoryCover := overlayMetricValue(values, "inventory_cover_days_at_destination")
	if inventoryCover == 0 {
		inventoryCover = overlayMetricValue(values, "inventory_days_baseline")
	}
	if lead > 0 {
		addMechanismScore(dst, "inventory_vulnerability", clampUnitInterval(lead/30.0))
		addMetricValue(metrics, values, "lead_time_days", "lead_time_days_baseline", scope, clampUnitInterval(lead/30.0))
	}
	if inventoryCover > 0 {
		importance := 0.4
		if lead > 0 && inventoryCover < lead {
			importance = clampUnitInterval((lead-inventoryCover)/maxFloat(lead, 1))
			addMechanismScore(dst, "inventory_vulnerability", 0.45+importance*0.55)
		}
		addMetricValue(metrics, values, "inventory_cover_days_at_destination", "inventory_days_baseline", scope, importance)
	}
	customs := overlayMetricValue(values, "customs_clearance_days")
	if customs > 0 {
		score := clampUnitInterval(customs / 14.0)
		addMechanismScore(dst, "customs_friction", score)
		addMetricValue(metrics, values, "customs_clearance_days", "", scope, score)
	}
	regulatoryFit := overlayMetricValue(values, "regulatory_fit")
	if regulatoryFit > 0 {
		score := clampUnitInterval(1 - regulatoryFit)
		addMechanismScore(dst, "low_substitutability", score*0.7)
		addMechanismScore(dst, "control_exposure", score*0.45)
		addMetricValue(metrics, values, "regulatory_fit", "", scope, score)
	}
	routeFragility := overlayMetricValue(values, "route_fragility")
	if routeFragility > 0 {
		score := clampUnitInterval(routeFragility)
		addMechanismScore(dst, "route_fragility", score)
		addMetricValue(metrics, values, "route_fragility", "", scope, score)
	}
	rerouteability := overlayMetricValue(values, "route_rerouteability")
	if rerouteability > 0 {
		score := clampUnitInterval(1 - rerouteability)
		addMechanismScore(dst, "route_fragility", score*0.7)
		addMetricValue(metrics, values, "route_rerouteability", "", scope, score)
	}
	serviceCriticality := overlayMetricValue(values, "service_criticality")
	if serviceCriticality > 0 {
		score := clampUnitInterval(serviceCriticality)
		addMechanismScore(dst, "service_dependency", score)
		addMetricValue(metrics, values, "service_criticality", "", scope, score)
	}
	technicalSub := overlayMetricValue(values, "technical_substitutability")
	if technicalSub > 0 {
		score := clampUnitInterval(1 - technicalSub)
		addMechanismScore(dst, "low_substitutability", score)
		addMetricValue(metrics, values, "technical_substitutability", "", scope, score)
	}
	supplyCriticality := overlayMetricValue(values, "supply_chain_criticality")
	if supplyCriticality > 0 {
		addMetricValue(metrics, values, "supply_chain_criticality", "", scope, clampUnitInterval(supplyCriticality))
	}
}

func applyLegStressMechanismSignals(dst map[string]*scoredMechanism, metrics map[string]*scoredMetric, leg *horizonmodels.SupplyChainPathLeg, legCriticality float64) {
	if leg == nil || leg.Stress == nil {
		return
	}
	if score := sanitizeUnitInterval(leg.Stress.Fragility); score > 0 {
		addMechanismScore(dst, "route_fragility", score)
		metrics["path_fragility"] = &scoredMetric{Key: "path_fragility", Value: score, Scope: "route_leg", Importance: score}
	}
	if score := sanitizeUnitInterval(1 - leg.Stress.Rerouteability); score > 0 {
		addMechanismScore(dst, "route_fragility", score*0.8)
		metrics["path_rerouteability"] = &scoredMetric{Key: "path_rerouteability", Value: sanitizeUnitInterval(leg.Stress.Rerouteability), Scope: "route_leg", Importance: score}
	}
	if score := sanitizeUnitInterval(leg.Stress.StressScore); score > 0 {
		addMechanismScore(dst, "route_fragility", score*0.6)
		metrics["path_stress_score"] = &scoredMetric{Key: "path_stress_score", Value: score, Scope: "route_leg", Importance: score}
	}
	if score := sanitizeUnitInterval(leg.Stress.ZonePressure); score > 0 {
		addMechanismScore(dst, "route_fragility", score*0.45)
		metrics["path_zone_pressure"] = &scoredMetric{Key: "path_zone_pressure", Value: score, Scope: "route_leg", Importance: score}
	}
	if score := sanitizeUnitInterval(leg.Stress.AffectedZonePressure); score > 0 {
		addMechanismScore(dst, "control_exposure", score*0.35)
		metrics["path_affected_pressure"] = &scoredMetric{Key: "path_affected_pressure", Value: score, Scope: "route_leg", Importance: score}
	}
	if latency := sanitizePositive(leg.Stress.RecoveryLatencyHrs); latency > 0 {
		score := clampUnitInterval(latency / 240.0)
		addMechanismScore(dst, "route_fragility", score*0.35)
		metrics["recovery_latency_hours"] = &scoredMetric{Key: "recovery_latency_hours", Value: latency, Scope: "route_leg", Importance: math.Max(score, legCriticality*0.4)}
	}
	for _, factor := range leg.Stress.DominantFactors {
		addMechanismScore(dst, strings.TrimSpace(factor), 0.3)
	}
}

func buildOverlayPathRef(artifact *horizonmodels.MaterializedArtifact, fragmentMap map[string]*horizonmodels.HorizonFragment) horizonmodels.SupplyChainOverlayPathRef {
	ref := horizonmodels.SupplyChainOverlayPathRef{
		ArtifactID:       strings.TrimSpace(artifact.ID),
		CanonicalKey:     strings.TrimSpace(artifact.CanonicalKey),
		DisplayName:      strings.TrimSpace(firstNonEmpty(artifact.DisplayName, overlayLabelForRef(artifact.ProductAxis), overlayLabelForRef(artifact.CanonicalKey))),
		PrimaryMechanism: "",
		Criticality:      0,
		StressScore:      artifact.PathStressScore,
		Bottleneck:       artifact.BottleneckLeg >= 0,
	}
	if overlay := artifact.RiskOverlay; overlay != nil {
		ref.PrimaryMechanism = overlay.PrimaryMechanism
		ref.Criticality = overlay.Criticality
		ref.StressScore = overlay.StressScore
		ref.Bottleneck = overlay.BottleneckLegIndex >= 0
		ref.CriticalSummary = overlay.Summary
		if len(overlay.Products) > 0 {
			ref.ProductRef = overlay.Products[0].Ref
			ref.ProductLabel = firstNonEmpty(overlay.Products[0].Label, overlayLabelForRef(overlay.Products[0].Ref))
		}
		if len(overlay.Bottlenecks) > 0 {
			ref.RouteSummary = firstNonEmpty(overlay.Bottlenecks[0].Label, overlayLabelForRef(overlay.Bottlenecks[0].Ref))
		}
		if len(overlay.Legs) > 0 {
			ref.SupplierRef = overlay.Legs[0].SupplierRef
			ref.SupplierLabel = firstNonEmpty(fragmentLabel(fragmentMap, overlay.Legs[0].SupplierRef), overlay.Legs[0].SupplierLabel)
			ref.CustomerRef = overlay.Legs[0].CustomerRef
			ref.CustomerLabel = firstNonEmpty(fragmentLabel(fragmentMap, overlay.Legs[0].CustomerRef), overlay.Legs[0].CustomerLabel)
			ref.TransportViaRefs = append([]string(nil), overlay.Legs[0].TransportViaRefs...)
		}
	}
	return ref
}

func buildSupplyChainLegSummary(productRef, primaryMechanism string, criticality float64, bottleneck bool, transportViaRefs []string) string {
	productLabel := overlayLabelForRef(productRef)
	mechanismLabel := overlayMechanismLabel(primaryMechanism)
	switch {
	case bottleneck && len(transportViaRefs) > 0:
		return fmt.Sprintf("%s is constrained by %s around %s.", productLabel, mechanismLabel, overlayLabelForRef(transportViaRefs[0]))
	case bottleneck:
		return fmt.Sprintf("%s is constrained by %s on a bottleneck leg.", productLabel, mechanismLabel)
	case criticality >= 0.7:
		return fmt.Sprintf("%s is highly exposed because %s is elevated on this leg.", productLabel, mechanismLabel)
	default:
		return fmt.Sprintf("%s shows moderate exposure, led by %s on this leg.", productLabel, mechanismLabel)
	}
}

func buildSupplyChainPathSummary(productRef, primaryMechanism string, criticality float64, stress *horizonmodels.SupplyChainPathStress, bottlenecks []horizonmodels.SupplyChainOverlayNode) string {
	productLabel := overlayLabelForRef(productRef)
	mechanismLabel := overlayMechanismLabel(primaryMechanism)
	if len(bottlenecks) > 0 {
		return fmt.Sprintf("%s is most exposed to %s, with bottleneck pressure centered on %s.", productLabel, mechanismLabel, bottlenecks[0].Label)
	}
	if stress != nil && stress.BottleneckLegIndex >= 0 {
		return fmt.Sprintf("%s is most exposed to %s, with a bottleneck on leg %d.", productLabel, mechanismLabel, stress.BottleneckLegIndex)
	}
	if criticality >= 0.75 {
		return fmt.Sprintf("%s is highly exposed because %s remains elevated across the full route.", productLabel, mechanismLabel)
	}
	return fmt.Sprintf("%s carries moderate route exposure led by %s.", productLabel, mechanismLabel)
}

func buildSupplyChainObjectSummary(label, primaryMechanism string, criticality float64, products []horizonmodels.SupplyChainOverlayProduct, bottlenecks []horizonmodels.SupplyChainOverlayNode) string {
	productLabel := "the most exposed product flow"
	if len(products) > 0 {
		productLabel = firstNonEmpty(products[0].Label, overlayLabelForRef(products[0].Ref))
	}
	mechanismLabel := overlayMechanismLabel(primaryMechanism)
	if len(bottlenecks) > 0 {
		return fmt.Sprintf("%s is exposed mainly through %s on %s, with corridor pressure concentrated on %s.", label, mechanismLabel, productLabel, bottlenecks[0].Label)
	}
	if criticality >= 0.75 {
		return fmt.Sprintf("%s is highly exposed mainly through %s on %s.", label, mechanismLabel, productLabel)
	}
	return fmt.Sprintf("%s has moderate supply-chain exposure, led by %s on %s.", label, mechanismLabel, productLabel)
}

func buildSupplyChainLensSummary(primaryMechanism string, criticality float64, products []horizonmodels.SupplyChainOverlayProduct, bottlenecks []horizonmodels.SupplyChainOverlayNode) string {
	productLabel := "the top product family"
	if len(products) > 0 {
		productLabel = firstNonEmpty(products[0].Label, overlayLabelForRef(products[0].Ref))
	}
	mechanismLabel := overlayMechanismLabel(primaryMechanism)
	if len(bottlenecks) > 0 {
		return fmt.Sprintf("Lens risk is concentrated in %s, driven mainly by %s and bottleneck pressure around %s.", productLabel, mechanismLabel, bottlenecks[0].Label)
	}
	if criticality >= 0.75 {
		return fmt.Sprintf("Lens risk is concentrated in %s, driven mainly by %s.", productLabel, mechanismLabel)
	}
	return fmt.Sprintf("Lens exposure is moderate overall, with %s leading risk on %s.", mechanismLabel, productLabel)
}

func topOverlayMechanisms(src map[string]*scoredMechanism, limit int) []horizonmodels.SupplyChainOverlayMechanism {
	rows := make([]*scoredMechanism, 0, len(src))
	for _, row := range src {
		if row == nil || row.Score <= 0 {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score == rows[j].Score {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Score > rows[j].Score
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]horizonmodels.SupplyChainOverlayMechanism, 0, len(rows))
	for _, row := range rows {
		out = append(out, horizonmodels.SupplyChainOverlayMechanism{
			Key:           row.Key,
			Label:         overlayMechanismLabel(row.Key),
			Score:         clampUnitInterval(row.Score),
			EvidenceCount: row.EvidenceCount,
		})
	}
	return out
}

func topOverlayProducts(src map[string]*scoredProduct, limit int) []horizonmodels.SupplyChainOverlayProduct {
	rows := make([]*scoredProduct, 0, len(src))
	for _, row := range src {
		if row == nil || row.Score <= 0 {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score == rows[j].Score {
			return rows[i].Ref < rows[j].Ref
		}
		return rows[i].Score > rows[j].Score
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]horizonmodels.SupplyChainOverlayProduct, 0, len(rows))
	for _, row := range rows {
		out = append(out, horizonmodels.SupplyChainOverlayProduct{
			Ref:       row.Ref,
			Label:     overlayLabelForRef(row.Ref),
			Score:     clampUnitInterval(row.Score),
			LinkCount: row.LinkCount,
			PathCount: row.PathCount,
		})
	}
	return out
}

func topOverlayNodes(src map[string]*scoredNode, limit int) []horizonmodels.SupplyChainOverlayNode {
	rows := make([]*scoredNode, 0, len(src))
	for _, row := range src {
		if row == nil || row.Score <= 0 {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Score == rows[j].Score {
			return rows[i].Ref < rows[j].Ref
		}
		return rows[i].Score > rows[j].Score
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]horizonmodels.SupplyChainOverlayNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, horizonmodels.SupplyChainOverlayNode{
			Ref:       row.Ref,
			Label:     overlayLabelForRef(row.Ref),
			Role:      row.Role,
			Score:     clampUnitInterval(row.Score),
			PathCount: row.PathCount,
		})
	}
	return out
}

func topOverlayMetrics(src map[string]*scoredMetric, limit int) []horizonmodels.SupplyChainOverlayMetric {
	rows := make([]*scoredMetric, 0, len(src))
	for _, row := range src {
		if row == nil || row.Importance <= 0 {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Importance == rows[j].Importance {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Importance > rows[j].Importance
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]horizonmodels.SupplyChainOverlayMetric, 0, len(rows))
	for _, row := range rows {
		out = append(out, horizonmodels.SupplyChainOverlayMetric{
			Key:        row.Key,
			Label:      overlayMetricLabel(row.Key),
			Value:      row.Value,
			Unit:       row.Unit,
			Scope:      row.Scope,
			Importance: clampUnitInterval(row.Importance),
		})
	}
	return out
}

func overlayMetricValue(values map[string]horizonmodels.MetricValue, key string) float64 {
	if values == nil {
		return 0
	}
	value, ok := values[key]
	if !ok {
		return 0
	}
	if !isFinite(value.Value) {
		return 0
	}
	return value.Value
}

func addMetricValue(dst map[string]*scoredMetric, values map[string]horizonmodels.MetricValue, primaryKey string, fallbackKey string, scope string, importance float64) {
	key := strings.TrimSpace(primaryKey)
	value, ok := values[key]
	if !ok && strings.TrimSpace(fallbackKey) != "" {
		key = strings.TrimSpace(fallbackKey)
		value, ok = values[key]
	}
	if !ok || !isFinite(value.Value) {
		return
	}
	dst[key] = &scoredMetric{
		Key:        key,
		Value:      value.Value,
		Unit:       derefString(value.Unit),
		Scope:      scope,
		Importance: importance,
	}
}

func addMechanismScore(dst map[string]*scoredMechanism, key string, score float64) {
	key = strings.TrimSpace(key)
	if key == "" || score <= 0 {
		return
	}
	entry := dst[key]
	if entry == nil {
		entry = &scoredMechanism{Key: key}
		dst[key] = entry
	}
	entry.Score += score
	entry.EvidenceCount++
}

func flagToPrimaryMechanism(flag string) string {
	flag = strings.TrimSpace(flag)
	switch flag {
	case "approved_vendor_only", "service_contract_required":
		return "service_dependency"
	case "export_license_required", "sanctions_touchpoint", "dual_use_flag":
		return "control_exposure"
	case "route_bottleneck":
		return "route_fragility"
	default:
		return flag
	}
}

func flagToMechanismWeight(flag string) float64 {
	switch strings.TrimSpace(flag) {
	case "inventory_vulnerability":
		return 0.9
	case "low_substitutability":
		return 0.85
	case "sole_source_lock_in":
		return 0.8
	case "route_fragility", "route_bottleneck":
		return 0.82
	case "service_dependency":
		return 0.76
	case "control_exposure":
		return 0.72
	case "approved_vendor_only", "service_contract_required":
		return 0.68
	case "export_license_required", "sanctions_touchpoint", "dual_use_flag":
		return 0.7
	default:
		return 0.55
	}
}

func overlayMechanismLabel(key string) string {
	switch strings.TrimSpace(key) {
	case "inventory_vulnerability":
		return "inventory vulnerability"
	case "low_substitutability":
		return "low substitutability"
	case "sole_source_lock_in":
		return "sole-source lock-in"
	case "route_fragility", "route_bottleneck":
		return "route fragility"
	case "service_dependency":
		return "service dependency"
	case "control_exposure":
		return "control exposure"
	case "customs_friction":
		return "customs friction"
	case "approved_vendor_only":
		return "approved-vendor lock-in"
	case "service_contract_required":
		return "service contract dependency"
	case "export_license_required":
		return "export-license dependency"
	case "sanctions_touchpoint":
		return "sanctions touchpoint"
	case "dual_use_flag":
		return "dual-use control exposure"
	default:
		return strings.ReplaceAll(strings.TrimSpace(key), "_", " ")
	}
}

func overlayMetricLabel(key string) string {
	switch strings.TrimSpace(key) {
	case "inventory_cover_days_at_destination":
		return "inventory cover at destination"
	case "inventory_days_baseline":
		return "inventory baseline"
	case "lead_time_days", "lead_time_days_baseline":
		return "lead time"
	case "customs_clearance_days":
		return "customs clearance"
	case "regulatory_fit":
		return "regulatory fit"
	case "technical_substitutability":
		return "technical substitutability"
	case "service_criticality":
		return "service criticality"
	case "route_fragility", "path_fragility":
		return "route fragility"
	case "route_rerouteability", "path_rerouteability":
		return "rerouteability"
	case "path_stress_score":
		return "path stress"
	case "path_zone_pressure":
		return "zone pressure"
	case "path_affected_pressure":
		return "affected-zone pressure"
	case "recovery_latency_hours":
		return "recovery latency"
	case "supply_chain_criticality":
		return "supply criticality"
	default:
		return strings.ReplaceAll(strings.TrimSpace(key), "_", " ")
	}
}

func overlayLabelForRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parts := strings.Split(ref, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}
		return strings.Title(strings.ReplaceAll(part, "-", " "))
	}
	return ref
}

func fragmentDisplayName(frag *horizonmodels.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	if label := strings.TrimSpace(frag.Labels["en"]); label != "" {
		return label
	}
	return overlayLabelForRef(frag.CanonicalPath)
}

func fragmentLabel(fragmentMap map[string]*horizonmodels.HorizonFragment, canonicalPath string) string {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if fragmentMap == nil || canonicalPath == "" {
		return ""
	}
	for _, frag := range fragmentMap {
		if frag != nil && strings.TrimSpace(frag.CanonicalPath) == canonicalPath {
			return fragmentDisplayName(frag)
		}
	}
	return ""
}

func stringQualifier(qualifiers map[string]interface{}, key string) string {
	if qualifiers == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(qualifiers[key]))
}

func interfaceSliceQualifier(qualifiers map[string]interface{}, key string) []string {
	if qualifiers == nil {
		return nil
	}
	raw := qualifiers[key]
	items, ok := raw.([]interface{})
	if !ok {
		if strings.TrimSpace(fmt.Sprint(raw)) == "" || raw == nil {
			return nil
		}
		return []string{strings.TrimSpace(fmt.Sprint(raw))}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s := strings.TrimSpace(fmt.Sprint(item))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func qualifierTruthy(qualifiers map[string]interface{}, key string) bool {
	if qualifiers == nil {
		return false
	}
	raw, ok := qualifiers[key]
	if !ok || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		v := strings.ToLower(strings.TrimSpace(value))
		return v == "true" || v == "yes" || v == "1"
	default:
		return strings.TrimSpace(fmt.Sprint(raw)) != "" && fmt.Sprint(raw) != "0"
	}
}

func firstNonEmptyPathProductRef(edges []horizonmodels.SupplyChainPathEdge) string {
	for _, edge := range edges {
		if ref := strings.TrimSpace(edge.ProductRef); ref != "" {
			return ref
		}
	}
	return ""
}

func legStressScore(leg *horizonmodels.SupplyChainPathLeg) float64 {
	if leg == nil || leg.Stress == nil {
		return 0
	}
	if score := sanitizeUnitInterval(leg.Stress.StressScore); score > 0 {
		return score
	}
	return sanitizeUnitInterval(leg.Stress.Fragility)
}

func legCriticalityScore(leg horizonmodels.SupplyChainPathLeg, edges []horizonmodels.SupplyChainPathEdge) float64 {
	best := legStressScore(&leg)
	for _, edge := range edges {
		if score := sanitizeUnitInterval(overlayMetricValue(edge.Metrics, "supply_chain_criticality")); score > best {
			best = score
		}
		if score := sanitizeUnitInterval(overlayMetricValue(edge.Metrics, "route_fragility")); score > best {
			best = score
		}
	}
	if best == 0 && len(edges) > 0 {
		best = 0.45
	}
	return best
}

func isLegBottleneck(pathStress *horizonmodels.SupplyChainPathStress, leg *horizonmodels.SupplyChainPathLeg) bool {
	if pathStress != nil && leg != nil && pathStress.BottleneckLegIndex >= 0 && pathStress.BottleneckLegIndex == leg.Index {
		return true
	}
	if leg == nil || leg.Stress == nil {
		return false
	}
	return sanitizeUnitInterval(leg.Stress.StressScore) >= 0.58
}

func bottleneckLegIndex(pathStress *horizonmodels.SupplyChainPathStress) int {
	if pathStress == nil {
		return -1
	}
	return pathStress.BottleneckLegIndex
}

func maxLegStress(legs []horizonmodels.SupplyChainPathLeg) float64 {
	best := 0.0
	for _, leg := range legs {
		if score := legStressScore(&leg); score > best {
			best = score
		}
	}
	return best
}

func sortedStringKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		key = strings.TrimSpace(key)
		if key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func clampUnitInterval(v float64) float64 {
	if !isFinite(v) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func sanitizeUnitInterval(v float64) float64 {
	return clampUnitInterval(v)
}

func sanitizePositive(v float64) float64 {
	if !isFinite(v) || v <= 0 {
		return 0
	}
	return v
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
