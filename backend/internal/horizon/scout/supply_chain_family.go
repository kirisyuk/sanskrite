package scout

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type supplyChainFamilyCandidate struct {
	canonicalKey string
	path         *pathbuilder.SupplyChainPath
	sufficiency  *models.SupplyChainPathSufficiency
}

type supplyChainFamilyBatch struct {
	ID                 string
	AnchorSupplier     string
	AnchorCustomer     string
	ProductAxis        string
	PathCount          int
	ValidatedPathCount int
	HeuristicPathCount int
	GapPathCount       int
	OpenGaps           []string
	CanonicalKeys      []string
	NodeRefs           []string
}

func buildSupplyChainFamilyKey(tenantID string, segments []pathbuilder.PathSegment) string {
	if len(segments) == 0 {
		return ""
	}
	last := segments[len(segments)-1]
	customer := supplyChainFamilySlug(last.CustomerRef)
	product := supplyChainFamilySlug(last.ProductRef)
	if customer != "" && product != "" {
		return fmt.Sprintf("/supply-chain-family/%s/customer_product/%s/%s", strings.TrimSpace(tenantID), customer, product)
	}
	supplier := supplyChainFamilySlug(segments[0].SupplierRef)
	if supplier != "" && product != "" {
		return fmt.Sprintf("/supply-chain-family/%s/supplier_product/%s/%s", strings.TrimSpace(tenantID), supplier, product)
	}
	return ""
}

func supplyChainFamilySlug(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	return strings.Trim(strings.ReplaceAll(ref, "/", "_"), "_")
}

func firstNonEmptyFamilyValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func collectFamilyNodeRefs(segments []pathbuilder.PathSegment) []string {
	refs := make([]string, 0, len(segments)*4)
	for _, segment := range segments {
		for _, ref := range []string{segment.SupplierRef, segment.ProductRef, segment.CustomerRef} {
			ref = strings.TrimSpace(ref)
			if ref != "" {
				refs = append(refs, ref)
			}
		}
		if segment.SupplierFacilityRef != nil && strings.TrimSpace(*segment.SupplierFacilityRef) != "" {
			refs = append(refs, strings.TrimSpace(*segment.SupplierFacilityRef))
		}
		if segment.CustomerFacilityRef != nil && strings.TrimSpace(*segment.CustomerFacilityRef) != "" {
			refs = append(refs, strings.TrimSpace(*segment.CustomerFacilityRef))
		}
		for _, via := range segment.TransportViaRefs {
			via = strings.TrimSpace(via)
			if via != "" {
				refs = append(refs, via)
			}
		}
	}
	return uniqueSortedStrings(refs)
}

func buildSupplyChainFamilyBatches(tenantID string, round int, candidates []supplyChainFamilyCandidate) map[string]*supplyChainFamilyBatch {
	families := make(map[string]*supplyChainFamilyBatch)
	for _, candidate := range candidates {
		if candidate.path == nil || len(candidate.path.Segments) == 0 {
			continue
		}
		familyID := buildSupplyChainFamilyKey(tenantID, candidate.path.Segments)
		if familyID == "" {
			continue
		}
		batch := families[familyID]
		if batch == nil {
			last := candidate.path.Segments[len(candidate.path.Segments)-1]
			first := candidate.path.Segments[0]
			batch = &supplyChainFamilyBatch{
				ID:             familyID,
				AnchorSupplier: strings.TrimSpace(first.SupplierRef),
				AnchorCustomer: strings.TrimSpace(last.CustomerRef),
				ProductAxis:    strings.TrimSpace(last.ProductRef),
			}
			families[familyID] = batch
		}
		batch.PathCount++
		if strings.TrimSpace(candidate.canonicalKey) != "" {
			batch.CanonicalKeys = append(batch.CanonicalKeys, strings.TrimSpace(candidate.canonicalKey))
		}
		batch.NodeRefs = mergeUniqueStrings(batch.NodeRefs, collectFamilyNodeRefs(candidate.path.Segments))
		if candidate.sufficiency != nil {
			switch strings.TrimSpace(candidate.sufficiency.Status) {
			case models.SupplyChainSufficiencyValidated:
				batch.ValidatedPathCount++
			case models.SupplyChainSufficiencyForensicGap:
				batch.GapPathCount++
			default:
				batch.HeuristicPathCount++
			}
			batch.OpenGaps = mergeUniqueStrings(batch.OpenGaps, candidate.sufficiency.OpenGaps)
			batch.OpenGaps = mergeUniqueStrings(batch.OpenGaps, candidate.sufficiency.HardFailures)
		}
	}
	for _, batch := range families {
		batch.CanonicalKeys = uniqueSortedStrings(batch.CanonicalKeys)
		batch.OpenGaps = uniqueSortedStrings(batch.OpenGaps)
		batch.NodeRefs = uniqueSortedStrings(batch.NodeRefs)
	}
	for idx := range candidates {
		if candidates[idx].path == nil {
			continue
		}
		familyID := buildSupplyChainFamilyKey(tenantID, candidates[idx].path.Segments)
		batch := families[familyID]
		if batch == nil {
			continue
		}
		candidates[idx].path.Family = &models.SupplyChainPathFamily{
			ID:                 batch.ID,
			AnchorSupplier:     batch.AnchorSupplier,
			AnchorCustomer:     batch.AnchorCustomer,
			ProductAxis:        batch.ProductAxis,
			PathCount:          batch.PathCount,
			ValidatedPathCount: batch.ValidatedPathCount,
			HeuristicPathCount: batch.HeuristicPathCount,
			GapPathCount:       batch.GapPathCount,
			OpenGaps:           append([]string(nil), batch.OpenGaps...),
			CoResearchRound:    round,
		}
	}
	return families
}

func shouldRunSupplyChainFamilyCoResearch(job *models.ScoutingJob) bool {
	if job == nil {
		return false
	}
	if isDirectedSupplyChainExplorationJob(job) {
		return false
	}
	if job.State == nil {
		return true
	}
	return parseStateBool(job.State["supplyChainFamilyCoResearchEnabled"], true)
}

func buildSupplyChainFamilyCoResearchMissingInfo(batch *supplyChainFamilyBatch) []string {
	if batch == nil || len(batch.OpenGaps) == 0 {
		return nil
	}
	gaps := batch.OpenGaps
	if len(gaps) > 6 {
		gaps = gaps[:6]
	}
	lines := make([]string, 0, len(gaps)+4)
	lines = append(lines, fmt.Sprintf("Resolve related supply-chain gaps for family %s.", batch.ID))
	if strings.TrimSpace(batch.AnchorCustomer) != "" {
		lines = append(lines, fmt.Sprintf("Terminal customer: %s", batch.AnchorCustomer))
	}
	if strings.TrimSpace(batch.ProductAxis) != "" {
		lines = append(lines, fmt.Sprintf("Product axis: %s", batch.ProductAxis))
	}
	lines = append(lines, "Open gaps:")
	for _, gap := range gaps {
		lines = append(lines, "- "+strings.TrimSpace(gap))
	}
	lines = append(lines, "Goal: close these gaps across the related chain set. Prefer reusing known entities; only add new suppliers/facilities/products when grounded evidence is explicit.")
	return []string{strings.Join(lines, "\n")}
}

func buildSupplyChainFamilyPromptContext(batch *supplyChainFamilyBatch) string {
	if batch == nil {
		return ""
	}
	lines := []string{
		"SUPPLY CHAIN FAMILY CONTEXT:",
		fmt.Sprintf("- familyId: %s", batch.ID),
		fmt.Sprintf("- memberPaths: %d", batch.PathCount),
	}
	if strings.TrimSpace(batch.AnchorSupplier) != "" {
		lines = append(lines, fmt.Sprintf("- anchorSupplier: %s", batch.AnchorSupplier))
	}
	if strings.TrimSpace(batch.AnchorCustomer) != "" {
		lines = append(lines, fmt.Sprintf("- anchorCustomer: %s", batch.AnchorCustomer))
	}
	if strings.TrimSpace(batch.ProductAxis) != "" {
		lines = append(lines, fmt.Sprintf("- productAxis: %s", batch.ProductAxis))
	}
	if len(batch.NodeRefs) > 0 {
		nodeRefs := batch.NodeRefs
		if len(nodeRefs) > 20 {
			nodeRefs = nodeRefs[:20]
		}
		lines = append(lines, "- knownFamilyRefs:")
		for _, ref := range nodeRefs {
			lines = append(lines, "  - "+ref)
		}
	}
	if len(batch.OpenGaps) > 0 {
		lines = append(lines, "- unresolvedGaps:")
		for _, gap := range batch.OpenGaps {
			lines = append(lines, "  - "+gap)
		}
	}
	return strings.Join(lines, "\n")
}

func (e *ScoutEngine) representativeFragmentForSupplyChainFamily(ctx context.Context, sourceFragment *models.HorizonFragment, batch *supplyChainFamilyBatch) *models.HorizonFragment {
	if batch == nil {
		return sourceFragment
	}
	candidates := []string{batch.AnchorCustomer, batch.ProductAxis, batch.AnchorSupplier}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if sourceFragment != nil && strings.TrimSpace(sourceFragment.CanonicalPath) == candidate {
			return sourceFragment
		}
		if e.fragmentsRepo != nil {
			if fragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, candidate); err == nil && fragment != nil {
				return fragment
			}
		}
		return &models.HorizonFragment{
			CanonicalPath: candidate,
			Class:          InferFragmentTypeFromCanonicalPath(candidate),
			TenantID:      "global",
		}
	}
	return sourceFragment
}

func (e *ScoutEngine) runSupplyChainFamilyCoResearch(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	families map[string]*supplyChainFamilyBatch,
) {
	if e == nil || e.researchAgent == nil || len(families) == 0 || !shouldRunSupplyChainFamilyCoResearch(job) {
		return
	}
	if job.State == nil {
		job.State = make(map[string]interface{})
	}
	currentRound := parseStateInt(job.State["supplyChainFamilyCoResearchRound"], 0)
	maxRounds := parseStateInt(job.State["supplyChainFamilyCoResearchMaxRounds"], 1)
	if maxRounds <= 0 {
		maxRounds = 1
	}
	if currentRound >= maxRounds {
		return
	}
	maxFamilies := parseStateInt(job.State["supplyChainFamilyCoResearchMaxFamilies"], 3)
	if maxFamilies <= 0 {
		maxFamilies = 3
	}
	var ordered []*supplyChainFamilyBatch
	for _, batch := range families {
		if batch != nil && len(batch.OpenGaps) > 0 {
			ordered = append(ordered, batch)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if len(ordered[i].OpenGaps) == len(ordered[j].OpenGaps) {
			return ordered[i].PathCount > ordered[j].PathCount
		}
		return len(ordered[i].OpenGaps) > len(ordered[j].OpenGaps)
	})
	if len(ordered) > maxFamilies {
		ordered = ordered[:maxFamilies]
	}
	if len(ordered) == 0 {
		return
	}
	chainContext, _ := ParseChainContext(job.State["chainContext"])
	chainContextStr := ""
	existingEntities := make([]string, 0)
	if chainContext != nil {
		chainContextStr = chainContext.ToPromptContext()
		existingEntities = append(existingEntities, chainContext.DiscoveredNodes...)
	}
	var allChains []agents.SupplyChain
	for _, batch := range ordered {
		fragment := e.representativeFragmentForSupplyChainFamily(ctx, sourceFragment, batch)
		missingInfo := buildSupplyChainFamilyCoResearchMissingInfo(batch)
		if len(missingInfo) == 0 {
			continue
		}
		familyContext := buildSupplyChainFamilyPromptContext(batch)
		input := agents.ResearchStrategyInput{
			Fragment:         fragment,
			MissingInfo:      missingInfo,
			Focus:            firstNonEmptyFamilyValue(batch.AnchorCustomer, batch.ProductAxis, batch.AnchorSupplier),
			ChainContext:     strings.TrimSpace(strings.Join([]string{chainContextStr, familyContext}, "\n\n")),
			ExistingEntities: mergeUniqueStrings(existingEntities, batch.NodeRefs),
			Keywords:         append([]string(nil), batch.OpenGaps...),
		}
		_, _, newChains, _, _, err := e.researchAgent.ResearchPipeline().Enrich(ctx, input, []string{taxonomy.DomainModeSupplyChain})
		if err != nil {
			e.logLine(ctx, job, "warn", fmt.Sprintf("[SupplyChainFamilyCoResearch] family=%s err=%v", batch.ID, err))
			continue
		}
		if len(newChains) > 0 {
			e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainFamilyCoResearch] family=%s newChains=%d", batch.ID, len(newChains)))
			allChains = append(allChains, newChains...)
		}
	}
	if len(allChains) == 0 {
		return
	}
	job.State["supplyChainFamilyCoResearchRound"] = float64(currentRound + 1)
	job.State["supplyChainFamilyCoResearchLastChains"] = float64(len(allChains))
	prevPhase := parseStateString(job.State["supplyChainPhase"])
	job.State["supplyChainPhase"] = "family_targeted_enrich"
	defer func() {
		if prevPhase == "" {
			job.State["supplyChainPhase"] = "chain_core"
		} else {
			job.State["supplyChainPhase"] = prevPhase
		}
	}()
	_ = e.processSupplyChains(ctx, job, sourceFragment, allChains)
}
