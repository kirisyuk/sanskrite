package pathbuilder

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	branchClassPrimaryGoodsFlow      = "primary_goods_flow"
	branchClassComponentChain        = "component_chain"
	branchClassMaterialInputChain    = "material_input_chain"
	branchClassDistributionRetail    = "distribution_retail_branch"
	branchClassRouteLogisticsSupport = "route_logistics_support"
	branchClassInfraEnergySupport    = "infra_energy_support"
	branchClassPackagingSupport      = "packaging_support"
	branchClassBackgroundNoise       = "background_noise"

	branchTierPrimary    = "primary"
	branchTierSecondary  = "secondary"
	branchTierSupporting = "supporting"
	branchTierBackground = "background"
)

type assemblyBranchCandidate struct {
	Summary                  models.SupplyChainAssemblyBranchSummary
	BusinessRelevanceScore   float64
	AnchorProximityScore     float64
	RouteEvidenceScore       float64
	EvidenceStrengthScore    float64
	NoisePenaltyScore        float64
	DecisionValueScore       float64
	DistributionSignificance float64
}

type supplyChainQualityOverlay struct {
	ProductNodes              []models.SupplyChainAssemblyNode
	ActorNodes                []models.SupplyChainAssemblyNode
	SupplyEdges               []models.SupplyChainAssemblySupplyEdge
	TransformEdges            []models.SupplyChainAssemblyTransformEdge
	AssemblyEdges             []models.SupplyChainAssemblyTransformEdge
	Coverage                  *models.SupplyChainAssemblyCoverage
	PrimaryBranches           []models.SupplyChainAssemblyBranchSummary
	SecondaryBranches         []models.SupplyChainAssemblyBranchSummary
	TopRoutes                 []string
	TopDistributionNodes      []string
	TopSuppliers              []string
	TopProductFamilies        []string
	SuppressedSupportBranches []models.SupplyChainAssemblyBranchSummary
}

func buildSupplyChainQualityOverlay(
	anchors []string,
	productNodes []models.SupplyChainAssemblyNode,
	actorNodes []models.SupplyChainAssemblyNode,
	supplyEdges []models.SupplyChainAssemblySupplyEdge,
	transformEdges []models.SupplyChainAssemblyTransformEdge,
	assemblyEdges []models.SupplyChainAssemblyTransformEdge,
	routeBindings []models.SupplyChainAssemblyRouteBinding,
	connectivity map[string]map[string]struct{},
	coverage *models.SupplyChainAssemblyCoverage,
) supplyChainQualityOverlay {
	overlay := supplyChainQualityOverlay{
		ProductNodes:   append([]models.SupplyChainAssemblyNode(nil), productNodes...),
		ActorNodes:     append([]models.SupplyChainAssemblyNode(nil), actorNodes...),
		SupplyEdges:    append([]models.SupplyChainAssemblySupplyEdge(nil), supplyEdges...),
		TransformEdges: append([]models.SupplyChainAssemblyTransformEdge(nil), transformEdges...),
		AssemblyEdges:  append([]models.SupplyChainAssemblyTransformEdge(nil), assemblyEdges...),
		Coverage:       coverage,
	}
	if coverage == nil {
		overlay.Coverage = &models.SupplyChainAssemblyCoverage{}
	}

	distances := buildAnchorDistances(anchors, connectivity)
	actorNodeMap := make(map[string]models.SupplyChainAssemblyNode, len(actorNodes))
	for _, node := range actorNodes {
		actorNodeMap[strings.TrimSpace(node.Ref)] = node
	}
	productNodeMap := make(map[string]models.SupplyChainAssemblyNode, len(productNodes))
	for _, node := range productNodes {
		productNodeMap[strings.TrimSpace(node.Ref)] = node
	}

	supplyByProduct := map[string][]models.SupplyChainAssemblySupplyEdge{}
	transformTo := map[string][]models.SupplyChainAssemblyTransformEdge{}
	transformFrom := map[string][]models.SupplyChainAssemblyTransformEdge{}
	routeByProduct := map[string][]models.SupplyChainAssemblyRouteBinding{}
	for _, edge := range supplyEdges {
		ref := strings.TrimSpace(edge.ProductRef)
		if ref != "" {
			supplyByProduct[ref] = append(supplyByProduct[ref], edge)
		}
	}
	for _, edge := range transformEdges {
		if ref := strings.TrimSpace(edge.ToProductRef); ref != "" {
			transformTo[ref] = append(transformTo[ref], edge)
		}
		if ref := strings.TrimSpace(edge.FromProductRef); ref != "" {
			transformFrom[ref] = append(transformFrom[ref], edge)
		}
	}
	for _, edge := range assemblyEdges {
		if ref := strings.TrimSpace(edge.ToProductRef); ref != "" {
			transformTo[ref] = append(transformTo[ref], edge)
		}
		if ref := strings.TrimSpace(edge.FromProductRef); ref != "" {
			transformFrom[ref] = append(transformFrom[ref], edge)
		}
	}
	for _, binding := range routeBindings {
		if ref := strings.TrimSpace(binding.ProductRef); ref != "" {
			routeByProduct[ref] = append(routeByProduct[ref], binding)
		}
	}

	candidates := make([]assemblyBranchCandidate, 0, len(productNodes)+len(actorNodes))
	for _, productNode := range productNodes {
		productRef := strings.TrimSpace(productNode.Ref)
		if productRef == "" {
			continue
		}
		candidate := buildProductBranchCandidate(productRef, anchors, distances, supplyByProduct[productRef], transformTo[productRef], transformFrom[productRef], routeByProduct[productRef], connectivity, actorNodeMap)
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		for _, actorNode := range actorNodes {
			if candidate, ok := buildActorOnlyBranchCandidate(actorNode, anchors, distances); ok {
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].DecisionValueScore == candidates[j].DecisionValueScore {
			return candidates[i].Summary.ID < candidates[j].Summary.ID
		}
		return candidates[i].DecisionValueScore > candidates[j].DecisionValueScore
	})
	assignBranchTiers(candidates)

	branchByID := make(map[string]assemblyBranchCandidate, len(candidates))
	productBranch := make(map[string]assemblyBranchCandidate, len(candidates))
	actorBranch := map[string]assemblyBranchCandidate{}
	for _, candidate := range candidates {
		branchByID[candidate.Summary.ID] = candidate
		for _, productRef := range candidate.Summary.ProductRefs {
			if existing, ok := productBranch[productRef]; !ok || existing.DecisionValueScore < candidate.DecisionValueScore {
				productBranch[productRef] = candidate
			}
		}
		for _, actorRef := range candidate.Summary.ActorRefs {
			if existing, ok := actorBranch[actorRef]; !ok || existing.DecisionValueScore < candidate.DecisionValueScore {
				actorBranch[actorRef] = candidate
			}
		}
	}

	overlay.PrimaryBranches, overlay.SecondaryBranches, overlay.SuppressedSupportBranches = splitAssemblyBranches(candidates)
	overlay.TopRoutes = summarizeBranchRefs(overlay.PrimaryBranches, overlay.SecondaryBranches, func(branch models.SupplyChainAssemblyBranchSummary) []string {
		return branch.RouteRefs
	}, 8)
	overlay.TopDistributionNodes = summarizeFilteredBranchRefs(overlay.PrimaryBranches, overlay.SecondaryBranches, func(branch models.SupplyChainAssemblyBranchSummary) []string {
		return branch.ActorRefs
	}, isDistributionLikeRef, 8)
	overlay.TopSuppliers = summarizeFilteredBranchRefs(overlay.PrimaryBranches, nil, func(branch models.SupplyChainAssemblyBranchSummary) []string {
		return branch.EntryNodes
	}, isSupplierLikeRef, 8)
	overlay.TopProductFamilies = summarizeBranchLabels(overlay.PrimaryBranches, overlay.SecondaryBranches, 8)

	for idx := range overlay.ProductNodes {
		ref := strings.TrimSpace(overlay.ProductNodes[idx].Ref)
		if candidate, ok := productBranch[ref]; ok {
			applyBranchToAssemblyNode(&overlay.ProductNodes[idx], candidate, distances[ref], false)
		}
	}
	for idx := range overlay.ActorNodes {
		ref := strings.TrimSpace(overlay.ActorNodes[idx].Ref)
		if candidate, ok := actorBranch[ref]; ok {
			applyBranchToAssemblyNode(&overlay.ActorNodes[idx], candidate, distances[ref], isDistributionLikeRef(ref))
			continue
		}
		if candidate, ok := buildActorOnlyBranchCandidate(overlay.ActorNodes[idx], anchors, distances); ok {
			applyBranchToAssemblyNode(&overlay.ActorNodes[idx], candidate, distances[ref], isDistributionLikeRef(ref))
		}
	}

	noiseEdges := 0
	routeBoundPrimaryEdges := 0
	for idx := range overlay.SupplyEdges {
		branch := pickBranchForSupplyEdge(overlay.SupplyEdges[idx], productBranch, actorBranch)
		applyBranchToSupplyEdge(&overlay.SupplyEdges[idx], branch)
		if overlay.SupplyEdges[idx].PriorityTier == branchTierPrimary && len(overlay.SupplyEdges[idx].TransportViaRefs) > 0 {
			routeBoundPrimaryEdges++
		}
		if isSupportTier(overlay.SupplyEdges[idx].PriorityTier) {
			noiseEdges++
		}
	}
	for idx := range overlay.TransformEdges {
		branch := pickBranchForTransformEdge(overlay.TransformEdges[idx], productBranch)
		applyBranchToTransformEdge(&overlay.TransformEdges[idx], branch)
		if isSupportTier(overlay.TransformEdges[idx].PriorityTier) {
			noiseEdges++
		}
	}
	for idx := range overlay.AssemblyEdges {
		branch := pickBranchForTransformEdge(overlay.AssemblyEdges[idx], productBranch)
		applyBranchToTransformEdge(&overlay.AssemblyEdges[idx], branch)
		if isSupportTier(overlay.AssemblyEdges[idx].PriorityTier) {
			noiseEdges++
		}
	}

	overlay.Coverage.PrimaryBranchesN = len(overlay.PrimaryBranches)
	overlay.Coverage.SecondaryBranchesN = len(overlay.SecondaryBranches)
	overlay.Coverage.SupportBranchesN = len(overlay.SuppressedSupportBranches)
	overlay.Coverage.PrimaryProductsCoveredN = countPrimaryProductsCovered(overlay.PrimaryBranches)
	overlay.Coverage.RouteBoundPrimaryEdgesN = routeBoundPrimaryEdges
	overlay.Coverage.NoiseEdgesN = noiseEdges
	return overlay
}

func buildProductBranchCandidate(
	productRef string,
	anchors []string,
	distances map[string]int,
	supplyEdges []models.SupplyChainAssemblySupplyEdge,
	incoming []models.SupplyChainAssemblyTransformEdge,
	outgoing []models.SupplyChainAssemblyTransformEdge,
	routeBindings []models.SupplyChainAssemblyRouteBinding,
	connectivity map[string]map[string]struct{},
	actorNodeMap map[string]models.SupplyChainAssemblyNode,
) assemblyBranchCandidate {
	actorRefs := make([]string, 0, len(supplyEdges)*2)
	routeRefs := make([]string, 0, len(routeBindings)*2)
	entryNodes := make([]string, 0, len(supplyEdges)+len(incoming))
	exitNodes := make([]string, 0, len(supplyEdges)+len(outgoing))
	confidenceTotal := 0.0
	confidenceN := 0
	for _, edge := range supplyEdges {
		actorRefs = append(actorRefs, strings.TrimSpace(edge.SupplierRef), strings.TrimSpace(edge.CustomerRef))
		entryNodes = append(entryNodes, strings.TrimSpace(edge.SupplierRef))
		exitNodes = append(exitNodes, strings.TrimSpace(edge.CustomerRef))
		routeRefs = append(routeRefs, edge.TransportViaRefs...)
		if edge.Confidence > 0 {
			confidenceTotal += edge.Confidence
			confidenceN++
		}
	}
	for _, edge := range incoming {
		entryNodes = append(entryNodes, strings.TrimSpace(edge.FromProductRef))
	}
	for _, edge := range outgoing {
		exitNodes = append(exitNodes, strings.TrimSpace(edge.ToProductRef))
	}
	for _, binding := range routeBindings {
		routeRefs = append(routeRefs, binding.ViaRefs...)
	}
	for neighbor := range connectivity[productRef] {
		if node, ok := actorNodeMap[strings.TrimSpace(neighbor)]; ok {
			actorRefs = append(actorRefs, strings.TrimSpace(node.Ref))
		}
	}
	actorRefs = uniqueSortedStrings(actorRefs)
	routeRefs = uniqueSortedStrings(routeRefs)
	entryNodes = uniqueSortedStrings(entryNodes)
	exitNodes = uniqueSortedStrings(exitNodes)

	branchClass := classifyAssemblyBranch(productRef, actorRefs, routeRefs, anchors)
	businessScore := branchBusinessScore(branchClass)
	anchorScore := scoreFromDistance(minDistanceForRefs(distances, append([]string{productRef}, actorRefs...)))
	routeScore := clamp01(float64(len(routeRefs))*0.2 + boolScore(len(routeRefs) > 0, 0.15))
	evidenceScore := clamp01(float64(len(supplyEdges))*0.14 + float64(len(incoming)+len(outgoing))*0.08 + confidenceAverage(confidenceTotal, confidenceN)*0.35 + namedSpecificityScore(actorRefs, actorNodeMap)*0.22)
	distributionScore := distributionSignificanceScore(actorRefs)
	noiseScore := branchNoisePenalty(branchClass, actorRefs, routeRefs, supplyEdges == nil)
	decisionValue := clamp01((businessScore * 0.32) + (anchorScore * 0.24) + (routeScore * 0.12) + (evidenceScore * 0.2) + (distributionScore * 0.12) - (noiseScore * 0.32))

	title := fmt.Sprintf("%s branch", familyLabelFromRef(productRef))
	reason := buildBranchReason(branchClass, productRef, actorRefs, routeRefs)
	id := buildAssemblyBranchID(productRef, branchClass)
	summary := models.SupplyChainAssemblyBranchSummary{
		ID:                     id,
		BranchClass:            branchClass,
		Title:                  title,
		AnchorRefs:             uniqueSortedStrings(anchorsNearRefs(anchors, distances, append([]string{productRef}, actorRefs...))),
		ProductRefs:            []string{productRef},
		ActorRefs:              actorRefs,
		RouteRefs:              routeRefs,
		EntryNodes:             entryNodes,
		ExitNodes:              exitNodes,
		DecisionValueScore:     roundScore(decisionValue),
		BusinessRelevanceScore: roundScore(businessScore),
		AnchorProximityScore:   roundScore(anchorScore),
		RouteEvidenceScore:     roundScore(routeScore),
		EvidenceStrengthScore:  roundScore(evidenceScore),
		NoisePenaltyScore:      roundScore(noiseScore),
		Reason:                 reason,
	}
	return assemblyBranchCandidate{
		Summary:                  summary,
		BusinessRelevanceScore:   businessScore,
		AnchorProximityScore:     anchorScore,
		RouteEvidenceScore:       routeScore,
		EvidenceStrengthScore:    evidenceScore,
		NoisePenaltyScore:        noiseScore,
		DecisionValueScore:       decisionValue,
		DistributionSignificance: distributionScore,
	}
}

func buildActorOnlyBranchCandidate(node models.SupplyChainAssemblyNode, anchors []string, distances map[string]int) (assemblyBranchCandidate, bool) {
	ref := strings.TrimSpace(node.Ref)
	if ref == "" {
		return assemblyBranchCandidate{}, false
	}
	branchClass := branchClassBackgroundNoise
	if isDistributionLikeRef(ref) {
		branchClass = branchClassDistributionRetail
	} else if isRouteLikeRef(ref) {
		branchClass = branchClassRouteLogisticsSupport
	} else if isInfraLikeRef(ref) {
		branchClass = branchClassInfraEnergySupport
	}
	businessScore := branchBusinessScore(branchClass)
	anchorScore := scoreFromDistance(minDistanceForRefs(distances, []string{ref}))
	evidenceScore := clamp01(0.25 + namedRefScore(ref)*0.35)
	noiseScore := branchNoisePenalty(branchClass, []string{ref}, nil, true)
	decisionValue := clamp01((businessScore * 0.34) + (anchorScore * 0.3) + (evidenceScore * 0.18) - (noiseScore * 0.34))
	if decisionValue < 0.2 {
		return assemblyBranchCandidate{}, false
	}
	summary := models.SupplyChainAssemblyBranchSummary{
		ID:                     buildAssemblyBranchID(ref, branchClass),
		BranchClass:            branchClass,
		Title:                  fmt.Sprintf("%s branch", familyLabelFromRef(ref)),
		AnchorRefs:             uniqueSortedStrings(anchorsNearRefs(anchors, distances, []string{ref})),
		ActorRefs:              []string{ref},
		EntryNodes:             []string{ref},
		ExitNodes:              []string{ref},
		DecisionValueScore:     roundScore(decisionValue),
		BusinessRelevanceScore: roundScore(businessScore),
		AnchorProximityScore:   roundScore(anchorScore),
		EvidenceStrengthScore:  roundScore(evidenceScore),
		NoisePenaltyScore:      roundScore(noiseScore),
		Reason:                 buildBranchReason(branchClass, ref, []string{ref}, nil),
	}
	return assemblyBranchCandidate{
		Summary:                summary,
		BusinessRelevanceScore: businessScore,
		AnchorProximityScore:   anchorScore,
		EvidenceStrengthScore:  evidenceScore,
		NoisePenaltyScore:      noiseScore,
		DecisionValueScore:     decisionValue,
	}, true
}

func assignBranchTiers(candidates []assemblyBranchCandidate) {
	primaryBudget := 8
	secondaryBudget := 12
	primaryCount := 0
	secondaryCount := 0
	for idx := range candidates {
		class := candidates[idx].Summary.BranchClass
		score := candidates[idx].DecisionValueScore
		tier := branchTierBackground
		switch {
		case isPrimaryEligibleClass(class) && score >= 0.58 && primaryCount < primaryBudget:
			tier = branchTierPrimary
			primaryCount++
		case !isSupportClass(class) && score >= 0.42 && secondaryCount < secondaryBudget:
			tier = branchTierSecondary
			secondaryCount++
		case isSupportClass(class) && score >= 0.34:
			tier = branchTierSupporting
		case score >= 0.28:
			tier = branchTierSupporting
		}
		candidates[idx].Summary.PriorityTier = tier
	}
	if primaryCount == 0 {
		for idx := range candidates {
			if isSupportClass(candidates[idx].Summary.BranchClass) {
				continue
			}
			candidates[idx].Summary.PriorityTier = branchTierPrimary
			primaryCount++
			break
		}
	}
}

func splitAssemblyBranches(candidates []assemblyBranchCandidate) ([]models.SupplyChainAssemblyBranchSummary, []models.SupplyChainAssemblyBranchSummary, []models.SupplyChainAssemblyBranchSummary) {
	primary := make([]models.SupplyChainAssemblyBranchSummary, 0, len(candidates))
	secondary := make([]models.SupplyChainAssemblyBranchSummary, 0, len(candidates))
	suppressed := make([]models.SupplyChainAssemblyBranchSummary, 0, len(candidates))
	primaryIDs := map[string]struct{}{}
	for _, candidate := range candidates {
		switch candidate.Summary.PriorityTier {
		case branchTierPrimary:
			primary = append(primary, candidate.Summary)
			primaryIDs[candidate.Summary.ID] = struct{}{}
		case branchTierSecondary:
			secondary = append(secondary, candidate.Summary)
		default:
			suppressed = append(suppressed, candidate.Summary)
		}
	}
	if len(primary) > 0 && len(suppressed) > 0 {
		for i := range primary {
			primary[i].CollapsedSupportingBranchIDs = collectSuppressedSupportIDs(suppressed, primary[i].BranchClass)
		}
	}
	return primary, secondary, suppressed
}

func collectSuppressedSupportIDs(branches []models.SupplyChainAssemblyBranchSummary, primaryClass string) []string {
	out := make([]string, 0, len(branches))
	for _, branch := range branches {
		if !isSupportClass(branch.BranchClass) && branch.BranchClass == primaryClass {
			continue
		}
		if strings.TrimSpace(branch.ID) != "" {
			out = append(out, strings.TrimSpace(branch.ID))
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func pickBranchForSupplyEdge(edge models.SupplyChainAssemblySupplyEdge, productBranch map[string]assemblyBranchCandidate, actorBranch map[string]assemblyBranchCandidate) assemblyBranchCandidate {
	if candidate, ok := productBranch[strings.TrimSpace(edge.ProductRef)]; ok {
		return candidate
	}
	if candidate, ok := actorBranch[strings.TrimSpace(edge.CustomerRef)]; ok {
		return candidate
	}
	return actorBranch[strings.TrimSpace(edge.SupplierRef)]
}

func pickBranchForTransformEdge(edge models.SupplyChainAssemblyTransformEdge, productBranch map[string]assemblyBranchCandidate) assemblyBranchCandidate {
	if candidate, ok := productBranch[strings.TrimSpace(edge.ToProductRef)]; ok {
		return candidate
	}
	return productBranch[strings.TrimSpace(edge.FromProductRef)]
}

func applyBranchToAssemblyNode(node *models.SupplyChainAssemblyNode, branch assemblyBranchCandidate, anchorDistance int, terminalDistribution bool) {
	if node == nil || strings.TrimSpace(branch.Summary.ID) == "" {
		return
	}
	node.BranchClass = branch.Summary.BranchClass
	node.PriorityTier = branch.Summary.PriorityTier
	node.PrimaryBranchID = branch.Summary.ID
	node.DecisionValueScore = roundScore(branch.DecisionValueScore)
	node.AnchorDistance = anchorDistance
	node.EvidenceStrengthScore = roundScore(branch.EvidenceStrengthScore)
	node.IsPrimaryFocus = branch.Summary.PriorityTier == branchTierPrimary
	node.IsTerminalDistributionNode = terminalDistribution
	node.IsSupportNode = isSupportClass(branch.Summary.BranchClass) || isSupportTier(branch.Summary.PriorityTier)
}

func applyBranchToSupplyEdge(edge *models.SupplyChainAssemblySupplyEdge, branch assemblyBranchCandidate) {
	if edge == nil || strings.TrimSpace(branch.Summary.ID) == "" {
		return
	}
	edge.BranchClass = branch.Summary.BranchClass
	edge.PriorityTier = branch.Summary.PriorityTier
	edge.DecisionValueScore = roundScore(branch.DecisionValueScore)
	edge.RouteCriticality = roundScore(maxFloat(branch.RouteEvidenceScore, boolScore(len(edge.TransportViaRefs) > 0, 0.7)))
	edge.SupportsPrimaryBranch = branch.Summary.PriorityTier == branchTierPrimary
	edge.PrimaryBranchID = branch.Summary.ID
	edge.EvidenceStrengthScore = roundScore(maxFloat(branch.EvidenceStrengthScore, edge.Confidence))
	edge.NoisePenaltyScore = roundScore(branch.NoisePenaltyScore)
}

func applyBranchToTransformEdge(edge *models.SupplyChainAssemblyTransformEdge, branch assemblyBranchCandidate) {
	if edge == nil || strings.TrimSpace(branch.Summary.ID) == "" {
		return
	}
	edge.BranchClass = branch.Summary.BranchClass
	edge.PriorityTier = branch.Summary.PriorityTier
	edge.DecisionValueScore = roundScore(branch.DecisionValueScore)
	edge.PrimaryBranchID = branch.Summary.ID
}

func buildAnchorDistances(anchors []string, connectivity map[string]map[string]struct{}) map[string]int {
	distances := map[string]int{}
	queue := make([]string, 0, len(anchors))
	for _, anchor := range uniqueSortedStrings(anchors) {
		anchor = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(anchor))
		if anchor == "" {
			continue
		}
		distances[anchor] = 0
		queue = append(queue, anchor)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for next := range connectivity[current] {
			if _, ok := distances[next]; ok {
				continue
			}
			distances[next] = distances[current] + 1
			queue = append(queue, next)
		}
	}
	return distances
}

func classifyAssemblyBranch(productRef string, actorRefs []string, routeRefs []string, anchors []string) string {
	text := strings.ToLower(strings.Join(append([]string{productRef}, actorRefs...), "|"))
	anchorText := strings.ToLower(strings.Join(anchors, "|"))
	switch {
	case strings.Contains(text, "packag") || strings.Contains(text, "carton") || strings.Contains(text, "paper") || strings.Contains(text, "box"):
		return branchClassPackagingSupport
	case strings.Contains(text, "/product/component/") || strings.Contains(text, "component") || strings.Contains(text, "module") || strings.Contains(text, "battery") || strings.Contains(text, "chip") || strings.Contains(text, "processor") || strings.Contains(text, "display"):
		return branchClassComponentChain
	case strings.Contains(text, "/product/material/") || strings.Contains(text, "material") || strings.Contains(text, "ore") || strings.Contains(text, "metal") || strings.Contains(text, "chemical") || strings.Contains(text, "cathode") || strings.Contains(text, "anode"):
		return branchClassMaterialInputChain
	case containsRetailSignal(text) || distributionSignificanceScore(actorRefs) >= 0.45 || containsRetailSignal(anchorText):
		return branchClassDistributionRetail
	case len(routeRefs) > 0 || strings.Contains(text, "customs") || strings.Contains(text, "port") || strings.Contains(text, "terminal"):
		return branchClassRouteLogisticsSupport
	case isInfraLikeRef(text) || isInfraLikeRef(anchorText):
		return branchClassInfraEnergySupport
	default:
		return branchClassPrimaryGoodsFlow
	}
}

func branchBusinessScore(class string) float64 {
	switch strings.TrimSpace(class) {
	case branchClassDistributionRetail:
		return 0.9
	case branchClassPrimaryGoodsFlow:
		return 0.86
	case branchClassComponentChain:
		return 0.78
	case branchClassMaterialInputChain:
		return 0.72
	case branchClassRouteLogisticsSupport:
		return 0.54
	case branchClassInfraEnergySupport:
		return 0.32
	case branchClassPackagingSupport:
		return 0.22
	default:
		return 0.18
	}
}

func branchNoisePenalty(class string, actorRefs []string, routeRefs []string, actorOnly bool) float64 {
	penalty := 0.0
	switch strings.TrimSpace(class) {
	case branchClassPackagingSupport:
		penalty += 0.62
	case branchClassInfraEnergySupport:
		penalty += 0.48
	case branchClassRouteLogisticsSupport:
		penalty += 0.32
	case branchClassBackgroundNoise:
		penalty += 0.72
	}
	if actorOnly {
		penalty += 0.18
	}
	if len(routeRefs) == 0 && len(actorRefs) <= 1 {
		penalty += 0.12
	}
	return clamp01(penalty)
}

func buildBranchReason(branchClass string, rootRef string, actorRefs []string, routeRefs []string) string {
	switch branchClass {
	case branchClassDistributionRetail:
		return fmt.Sprintf("promoted for anchor-near distribution coverage around %s with %d operational nodes", familyLabelFromRef(rootRef), len(actorRefs))
	case branchClassComponentChain:
		return fmt.Sprintf("promoted as component branch for %s", familyLabelFromRef(rootRef))
	case branchClassMaterialInputChain:
		return fmt.Sprintf("promoted as material-input branch for %s", familyLabelFromRef(rootRef))
	case branchClassRouteLogisticsSupport:
		return fmt.Sprintf("kept as logistics-support branch with %d route bindings", len(routeRefs))
	case branchClassPackagingSupport:
		return fmt.Sprintf("demoted as packaging/support branch for %s", familyLabelFromRef(rootRef))
	case branchClassInfraEnergySupport:
		return fmt.Sprintf("demoted as infra/energy branch for %s", familyLabelFromRef(rootRef))
	default:
		return fmt.Sprintf("promoted as primary goods flow for %s", familyLabelFromRef(rootRef))
	}
}

func buildAssemblyBranchID(ref string, branchClass string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(branchClass) + "|" + strings.TrimSpace(ref)))
	return fmt.Sprintf("branch_%x", sum[:6])
}

func familyLabelFromRef(ref string) string {
	ref = canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(ref))
	if ref == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) == 0 {
		return ref
	}
	if len(parts) >= 2 && parts[0] == "product" {
		parts = parts[1:]
	}
	label := parts[len(parts)-1]
	if len(parts) >= 2 && (parts[0] == "material" || parts[0] == "component" || parts[0] == "device" || parts[0] == "goods") {
		label = parts[len(parts)-1]
	}
	return strings.ReplaceAll(label, "-", " ")
}

func namedSpecificityScore(actorRefs []string, actorNodeMap map[string]models.SupplyChainAssemblyNode) float64 {
	if len(actorRefs) == 0 {
		return 0
	}
	total := 0.0
	for _, ref := range actorRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		score := namedRefScore(ref)
		if node, ok := actorNodeMap[ref]; ok && strings.TrimSpace(node.Name) != "" {
			score = maxFloat(score, 0.85)
		}
		total += score
	}
	return clamp01(total / float64(len(actorRefs)))
}

func namedRefScore(ref string) float64 {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0
	}
	if strings.Count(ref, "-") >= 1 {
		return 0.8
	}
	return 0.55
}

func minDistanceForRefs(distances map[string]int, refs []string) int {
	best := -1
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		dist, ok := distances[ref]
		if !ok {
			continue
		}
		if best == -1 || dist < best {
			best = dist
		}
	}
	return best
}

func anchorsNearRefs(anchors []string, distances map[string]int, refs []string) []string {
	out := make([]string, 0, len(anchors))
	limit := minDistanceForRefs(distances, refs)
	if limit < 0 {
		return nil
	}
	for _, anchor := range anchors {
		anchor = strings.TrimSpace(anchor)
		if anchor == "" {
			continue
		}
		if dist, ok := distances[anchor]; ok && dist <= limit+1 {
			out = append(out, anchor)
			continue
		}
		if containsAssemblyString(refs, anchor) {
			out = append(out, anchor)
		}
	}
	return uniqueSortedStrings(out)
}

func scoreFromDistance(distance int) float64 {
	switch {
	case distance < 0:
		return 0.25
	case distance == 0:
		return 1
	case distance == 1:
		return 0.82
	case distance == 2:
		return 0.64
	case distance == 3:
		return 0.48
	default:
		return 0.32
	}
}

func distributionSignificanceScore(actorRefs []string) float64 {
	if len(actorRefs) == 0 {
		return 0
	}
	score := 0.0
	for _, ref := range actorRefs {
		switch {
		case isDistributionLikeRef(ref):
			score += 0.35
		case isStoreLikeRef(ref):
			score += 0.25
		case isRouteLikeRef(ref):
			score += 0.15
		}
	}
	return clamp01(score)
}

func containsRetailSignal(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, token := range []string{"retail", "store", "warehouse", "distribution", "fulfillment", "ecom", "shop", "mall", "marwin", "meloman"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

func isDistributionLikeRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, token := range []string{"distribution", "warehouse", "fulfillment", "dc", "depot", "hub"} {
		if strings.Contains(ref, token) {
			return true
		}
	}
	return false
}

func isStoreLikeRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, token := range []string{"store", "retail", "shop", "marketplace"} {
		if strings.Contains(ref, token) {
			return true
		}
	}
	return false
}

func isRouteLikeRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, token := range []string{"port", "customs", "terminal", "rail", "corridor", "checkpoint"} {
		if strings.Contains(ref, token) {
			return true
		}
	}
	return false
}

func isSupplierLikeRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, token := range []string{"factory", "plant", "mill", "mine", "fab", "supplier", "maker", "manufacturer"} {
		if strings.Contains(ref, token) {
			return true
		}
	}
	return strings.HasPrefix(ref, "/org/") || strings.HasPrefix(ref, "/fac/")
}

func isInfraLikeRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, token := range []string{"power", "electric", "energy", "utility", "gas", "grid"} {
		if strings.Contains(ref, token) {
			return true
		}
	}
	return false
}

func isPrimaryEligibleClass(class string) bool {
	switch class {
	case branchClassPrimaryGoodsFlow, branchClassDistributionRetail, branchClassComponentChain, branchClassMaterialInputChain:
		return true
	default:
		return false
	}
}

func isSupportClass(class string) bool {
	switch class {
	case branchClassRouteLogisticsSupport, branchClassInfraEnergySupport, branchClassPackagingSupport, branchClassBackgroundNoise:
		return true
	default:
		return false
	}
}

func isSupportTier(tier string) bool {
	return tier == branchTierSupporting || tier == branchTierBackground
}

func summarizeBranchRefs(primary []models.SupplyChainAssemblyBranchSummary, secondary []models.SupplyChainAssemblyBranchSummary, extractor func(models.SupplyChainAssemblyBranchSummary) []string, limit int) []string {
	return summarizeFilteredBranchRefs(primary, secondary, extractor, nil, limit)
}

func summarizeBranchLabels(primary []models.SupplyChainAssemblyBranchSummary, secondary []models.SupplyChainAssemblyBranchSummary, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, limit)
	for _, groups := range [][]models.SupplyChainAssemblyBranchSummary{primary, secondary} {
		for _, branch := range groups {
			for _, ref := range branch.ProductRefs {
				label := strings.TrimSpace(familyLabelFromRef(ref))
				if label == "" {
					continue
				}
				if _, ok := seen[label]; ok {
					continue
				}
				seen[label] = struct{}{}
				out = append(out, label)
				if limit > 0 && len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

func summarizeFilteredBranchRefs(primary []models.SupplyChainAssemblyBranchSummary, secondary []models.SupplyChainAssemblyBranchSummary, extractor func(models.SupplyChainAssemblyBranchSummary) []string, keep func(string) bool, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, limit)
	for _, groups := range [][]models.SupplyChainAssemblyBranchSummary{primary, secondary} {
		for _, branch := range groups {
			for _, ref := range extractor(branch) {
				ref = strings.TrimSpace(ref)
				if ref == "" {
					continue
				}
				if keep != nil && !keep(ref) {
					continue
				}
				if _, ok := seen[ref]; ok {
					continue
				}
				seen[ref] = struct{}{}
				out = append(out, ref)
				if limit > 0 && len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

func countPrimaryProductsCovered(primary []models.SupplyChainAssemblyBranchSummary) int {
	seen := map[string]struct{}{}
	for _, branch := range primary {
		for _, ref := range branch.ProductRefs {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				seen[ref] = struct{}{}
			}
		}
	}
	return len(seen)
}

func confidenceAverage(total float64, count int) float64 {
	if count <= 0 {
		return 0.45
	}
	return clamp01(total / float64(count))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func roundScore(v float64) float64 {
	v = clamp01(v)
	return float64(int(v*100+0.5)) / 100
}

func boolScore(ok bool, score float64) float64 {
	if ok {
		return score
	}
	return 0
}

func maxFloat(values ...float64) float64 {
	best := 0.0
	for _, value := range values {
		if value > best {
			best = value
		}
	}
	return best
}
