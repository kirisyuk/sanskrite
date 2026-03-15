package scout

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	supplyChainContinuationModeRefineExisting = "refine_existing_assembly"
	supplyChainRefineSkipNoAnchor             = "no_final_product_anchor"
)

type supplyChainAssemblyInputs struct {
	AnchorSet     []string
	SourceAnchors []string
	GeoFilters    []string
}

func (e *ScoutEngine) runSupplyChainFastAssembly(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	domainCP string,
	supplyChainPlan *agents.SupplyChainExecutionPlan,
) error {
	return e.runSupplyChainAssemblyStage(ctx, job, sourceFragment, domainCP, supplyChainPlan, stageEntitySupplyChainFastAssembly, false)
}

func (e *ScoutEngine) runSupplyChainAssemblyRefine(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	domainCP string,
	supplyChainPlan *agents.SupplyChainExecutionPlan,
) error {
	return e.runSupplyChainAssemblyStage(ctx, job, sourceFragment, domainCP, supplyChainPlan, stageEntitySupplyChainRefine, true)
}

func (e *ScoutEngine) runSupplyChainAssemblyStage(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	domainCP string,
	supplyChainPlan *agents.SupplyChainExecutionPlan,
	stage string,
	refineMode bool,
) error {
	if e == nil || job == nil {
		return nil
	}
	pipelineName := pipelineRuntimeDomainBase
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	e.pipelineStageBegin(ctx, job, pipelineName, stage, true, stage)

	if e.supplyChainPathBuilder == nil || e.fragmentsRepo == nil || e.edgesRepo == nil {
		summary := buildSupplyChainAssemblyState(nil, "empty", "no_materialized_supply_links", stage)
		job.State["supplyChainAssembly"] = summary
		e.pipelineStageSkip(ctx, job, pipelineName, stage, true, "no_materialized_supply_links")
		e.persistSupplyChainAssemblyState(ctx, job, summary)
		return nil
	}

	inputs := collectSupplyChainAssemblyInputs(job, domainCP, supplyChainPlan)
	if len(inputs.AnchorSet) == 0 {
		summary := buildSupplyChainAssemblyState(nil, "empty", supplyChainRefineSkipNoAnchor, stage)
		job.State["supplyChainAssembly"] = summary
		e.pipelineStageSkip(ctx, job, pipelineName, stage, true, supplyChainRefineSkipNoAnchor)
		e.persistSupplyChainAssemblyState(ctx, job, summary)
		return nil
	}

	rawNodeRefs, rawNodeLabels, rawEdges, frontiers := collectSupplyChainAssemblyRawGraph(job)
	builder := pathbuilder.NewSupplyChainAssemblyBuilder(e.fragmentsRepo, e.edgesRepo, e.supplyChainPathBuilder)
	result, err := builder.BuildAndMaterialize(ctx, pathbuilder.SupplyChainAssemblyOptions{
		TenantID:      strings.TrimSpace(firstNonEmpty(job.TenantID, "global")),
		ProjectID:     strings.TrimSpace(job.ProjectID),
		LensID:        strings.TrimSpace(job.LensID),
		AnchorSet:     inputs.AnchorSet,
		SourceAnchors: inputs.SourceAnchors,
		RawNodeRefs:   rawNodeRefs,
		RawNodeLabels: rawNodeLabels,
		RawEdges:      rawEdges,
		GeoFilters:    inputs.GeoFilters,
	})
	if err != nil {
		e.pipelineStageFail(ctx, job, pipelineName, stage, true, stage+"_failed", true, err)
		return err
	}

	if result == nil {
		summary := buildSupplyChainAssemblyState(nil, "empty", "assembly_no_reachable_graph", stage)
		job.State["supplyChainAssembly"] = summary
		e.pipelineStageSkip(ctx, job, pipelineName, stage, true, "assembly_no_reachable_graph")
		e.persistSupplyChainAssemblyState(ctx, job, summary)
		return nil
	}

	if strings.TrimSpace(result.SkipReason) != "" {
		summary := buildSupplyChainAssemblyState(result, "empty", result.SkipReason, stage)
		job.State["supplyChainAssembly"] = summary
		e.pipelineStageSkip(ctx, job, pipelineName, stage, true, result.SkipReason)
		e.persistSupplyChainAssemblyState(ctx, job, summary)
		return nil
	}

	if !refineMode && !isSupplyChainRefineContinuationJob(job) && result.Artifact != nil {
		continuationJobIDs, scheduled := e.scheduleSupplyChainAssemblyContinuation(ctx, job, result, frontiers)
		result.BackgroundContinuationScheduled = scheduled
		result.BackgroundContinuationJobIDs = continuationJobIDs
		if scheduled {
			if err := e.updateAssemblyArtifactContinuationMetadata(ctx, result); err != nil {
				recordPipelineIssue(job, "supply_chain_continuation_metadata", err.Error())
			}
		}
	}
	if refineMode && isSupplyChainRefineContinuationJob(job) {
		e.runSupplyChainContinuationEnrichment(ctx, job, sourceFragment)
	}

	summary := buildSupplyChainAssemblyState(result, result.Status, "", stage)
	job.State["supplyChainAssembly"] = summary
	e.persistSupplyChainAssemblyState(ctx, job, summary)

	if result.Artifact != nil {
		if lensID := strings.TrimSpace(job.LensID); lensID != "" && e.lensesRepo != nil {
			_ = addArtifactToLensScope(ctx, e.lensesRepo, lensID, result.Artifact)
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainAssembly] stage=%s assembly=%s anchors=%d products=%d actors=%d derivedPaths=%d provisional=%t",
			strings.TrimSpace(stage),
			result.Artifact.CanonicalKey,
			len(result.ResolvedAnchors),
			result.Coverage.ProductsN,
			result.Coverage.ActorsN,
			len(result.DerivedPathKeys),
			result.Provisional,
		))
	} else {
		e.logLine(ctx, job, "info", fmt.Sprintf("[SupplyChainAssembly] stage=%s anchors=%d derivedPaths=%d status=%s",
			strings.TrimSpace(stage), len(result.ResolvedAnchors), len(result.DerivedPathKeys), result.Status))
	}

	e.pipelineStageComplete(ctx, job, pipelineName, stage, true, stage+"_done")
	return nil
}

func (e *ScoutEngine) runSupplyChainContinuationEnrichment(ctx context.Context, job *models.ScoutingJob, sourceFragment *models.HorizonFragment) {
	if e == nil || job == nil || job.State == nil {
		return
	}
	artifact, parsed, err := e.loadSupplyChainAssemblyArtifact(ctx, job)
	if err != nil || artifact == nil || parsed == nil {
		return
	}
	scopeRefs := buildSupplyChainAssemblyScopeRefs(parsed, parseStateStringSlice(job.State["frontiersToExpand"]))
	if len(scopeRefs) > 0 {
		e.runScopedIntraGraphInference(ctx, strings.TrimSpace(firstNonEmpty(job.TenantID, "global")), strings.TrimSpace(job.ProjectID), strings.TrimSpace(job.LensID), scopeRefs)
	}
	if families := e.loadSupplyChainContinuationFamilyBatches(ctx, job, artifact, parsed); len(families) > 0 {
		prevSkip := parseStateBool(job.State["supplyChainSkipFamilyCoResearch"], false)
		job.State["supplyChainSkipFamilyCoResearch"] = false
		e.runSupplyChainFamilyCoResearch(ctx, job, sourceFragment, families)
		job.State["supplyChainSkipFamilyCoResearch"] = prevSkip
	}
	if scopedContext := buildScopedContinuationChainContext(job, parsed, scopeRefs); scopedContext != nil {
		prevSkip := parseStateBool(job.State["supplyChainSkipChildScheduling"], false)
		job.State["supplyChainSkipChildScheduling"] = false
		_ = e.scheduleSupplyChainChildJobs(ctx, job, sourceFragment, nil, scopedContext)
		job.State["supplyChainSkipChildScheduling"] = prevSkip
	}
}

func collectSupplyChainAssemblyInputs(
	job *models.ScoutingJob,
	domainCP string,
	supplyChainPlan *agents.SupplyChainExecutionPlan,
) supplyChainAssemblyInputs {
	anchors := make([]string, 0, 24)
	sourceAnchors := make([]string, 0, 24)
	geo := make([]string, 0, 8)
	addAnchor := func(dst *[]string, values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				*dst = append(*dst, value)
			}
		}
	}
	addGeo := func(values ...string) {
		for _, value := range values {
			value = strings.TrimSpace(strings.ToLower(value))
			if value != "" {
				geo = append(geo, value)
			}
		}
	}

	if supplyChainPlan != nil {
		for _, query := range supplyChainPlan.Queries {
			addAnchor(&anchors, query.Anchor)
			addAnchor(&anchors, query.Anchors...)
			addAnchor(&anchors, query.ProductFilter...)
			addAnchor(&sourceAnchors, query.Anchor)
			addAnchor(&sourceAnchors, query.Anchors...)
			addGeo(query.GeoFilter...)
		}
	}
	if _, facets := parseDomainPathFacets(strings.TrimSpace(domainCP)); facets != nil {
		for _, key := range []string{
			"product", "material", "commodity", "goods", "item", "subject", "application",
			"org", "organization", "company", "facility", "plant", "deposit", "warehouse",
			"market", "market_segment", "sector",
		} {
			addAnchor(&anchors, facets[key]...)
			addAnchor(&sourceAnchors, facets[key]...)
		}
		addGeo(facets["geo"]...)
	}
	if job != nil && job.State != nil {
		if cc, _ := ParseChainContext(job.State["chainContext"]); cc != nil {
			for _, ref := range cc.DiscoveredNodes {
				ref = strings.TrimSpace(ref)
				switch {
				case strings.HasPrefix(ref, "/product/"),
					strings.HasPrefix(ref, "/org/"),
					strings.HasPrefix(ref, "/fac/"),
					strings.HasPrefix(ref, "/deposit/"):
					anchors = append(anchors, ref)
				}
			}
		}
	}
	return supplyChainAssemblyInputs{
		AnchorSet:     dedupStrings(anchors),
		SourceAnchors: dedupStrings(firstNonEmptyStringSlice(sourceAnchors, anchors)),
		GeoFilters:    dedupStrings(geo),
	}
}

func collectSupplyChainAssemblyRawGraph(job *models.ScoutingJob) ([]string, map[string]string, []pathbuilder.SupplyChainAssemblyRawEdge, []string) {
	if job == nil || job.State == nil {
		return nil, nil, nil, nil
	}
	cc, _ := ParseChainContext(job.State["chainContext"])
	if cc == nil {
		return nil, nil, nil, nil
	}
	rawEdges := make([]pathbuilder.SupplyChainAssemblyRawEdge, 0, len(cc.DiscoveredEdges))
	for _, edge := range cc.DiscoveredEdges {
		rawEdges = append(rawEdges, pathbuilder.SupplyChainAssemblyRawEdge{
			Source:   strings.TrimSpace(edge.Source),
			Target:   strings.TrimSpace(edge.Target),
			EdgeType: strings.TrimSpace(edge.EdgeType),
		})
	}
	return dedupStrings(cc.DiscoveredNodes), cloneStringMap(cc.NodeLabels), rawEdges, buildSupplyChainFrontiers(cc)
}

func buildSupplyChainAssemblyState(result *pathbuilder.SupplyChainAssemblyResult, status string, skipReason string, stage string) map[string]interface{} {
	summary := map[string]interface{}{
		"status":     strings.TrimSpace(firstNonEmpty(status, "empty")),
		"skipReason": strings.TrimSpace(skipReason),
		"stage":      normalizeSupplyChainAssemblyStage(stage),
	}
	if result == nil {
		summary["anchorSet"] = []string{}
		summary["resolvedAnchors"] = []string{}
		summary["unresolvedAnchors"] = []string{}
		summary["openGaps"] = []string{}
		summary["derivedPathKeys"] = []string{}
		return summary
	}
	summary["stage"] = strings.TrimSpace(firstNonEmpty(result.Stage, normalizeSupplyChainAssemblyStage(stage)))
	summary["provisional"] = result.Provisional
	summary["sourceMode"] = strings.TrimSpace(result.SourceMode)
	summary["anchorSet"] = append([]string(nil), result.AnchorSet...)
	summary["resolvedAnchors"] = append([]string(nil), result.ResolvedAnchors...)
	summary["unresolvedAnchors"] = append([]string(nil), result.UnresolvedAnchors...)
	summary["anchorProducts"] = append([]string(nil), result.AnchorProducts...)
	summary["anchorComponentsN"] = float64(countAssemblyAnchorComponents(result.Artifact))
	summary["backgroundContinuationScheduled"] = result.BackgroundContinuationScheduled
	if len(result.BackgroundContinuationJobIDs) > 0 {
		summary["backgroundContinuationJobIds"] = append([]string(nil), result.BackgroundContinuationJobIDs...)
	}
	summary["openGaps"] = append([]string(nil), result.OpenGaps...)
	summary["derivedPathKeys"] = append([]string(nil), result.DerivedPathKeys...)
	summary["openGapsN"] = float64(len(result.OpenGaps))
	summary["derivedPathsN"] = float64(len(result.DerivedPathKeys))
	summary["tracedPathsN"] = float64(result.TracedPathsCount)
	if result.Coverage != nil {
		summary["productsN"] = float64(result.Coverage.ProductsN)
		summary["actorsN"] = float64(result.Coverage.ActorsN)
		summary["transformsN"] = float64(result.Coverage.TransformationsN)
		summary["suppliersN"] = float64(result.Coverage.SuppliersN)
		summary["unresolvedLeavesN"] = float64(result.Coverage.UnresolvedLeaves)
		summary["truncated"] = result.Coverage.Truncated
	}
	if result.Artifact != nil {
		summary["artifactKey"] = strings.TrimSpace(result.Artifact.CanonicalKey)
		summary["artifactId"] = strings.TrimSpace(result.Artifact.ID)
	}
	return summary
}

func (e *ScoutEngine) updateAssemblyArtifactContinuationMetadata(ctx context.Context, result *pathbuilder.SupplyChainAssemblyResult) error {
	if e == nil || result == nil || result.Artifact == nil || e.planner == nil || e.planner.materializedRepo == nil {
		return nil
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(result.Artifact.Payload)
	if err != nil || parsed == nil {
		return err
	}
	parsed.BackgroundContinuationScheduled = result.BackgroundContinuationScheduled
	parsed.BackgroundContinuationJobIDs = append([]string(nil), result.BackgroundContinuationJobIDs...)
	result.Artifact.Payload = models.BuildSupplyChainAssemblyGraphPayloadDetailed(models.SupplyChainAssemblyGraphPayloadOptions{
		AnchorSet:                       parsed.AnchorSet,
		SourceAnchors:                   parsed.SourceAnchors,
		ResolvedAnchors:                 parsed.ResolvedAnchors,
		UnresolvedAnchors:               parsed.UnresolvedAnchors,
		AnchorProducts:                  parsed.AnchorProducts,
		AnchorComponents:                parsed.AnchorComponents,
		ProductNodes:                    parsed.ProductNodes,
		ActorNodes:                      parsed.ActorNodes,
		SupplyEdges:                     parsed.SupplyEdges,
		TransformEdges:                  parsed.TransformEdges,
		AssemblyEdges:                   parsed.AssemblyEdges,
		RouteBindings:                   parsed.RouteBindings,
		Coverage:                        parsed.Coverage,
		OpenGaps:                        parsed.OpenGaps,
		DerivedPathKeys:                 parsed.DerivedPathKeys,
		Stage:                           parsed.Stage,
		Provisional:                     parsed.Provisional,
		SourceMode:                      parsed.SourceMode,
		BackgroundContinuationScheduled: parsed.BackgroundContinuationScheduled,
		BackgroundContinuationJobIDs:    parsed.BackgroundContinuationJobIDs,
		Notes:                           parsed.Notes,
	})
	result.Artifact.UpdatedAt = time.Now().UTC()
	return e.planner.materializedRepo.Upsert(ctx, result.Artifact)
}

func (e *ScoutEngine) scheduleSupplyChainAssemblyContinuation(
	ctx context.Context,
	job *models.ScoutingJob,
	result *pathbuilder.SupplyChainAssemblyResult,
	frontiers []string,
) ([]string, bool) {
	if e == nil || job == nil || result == nil || result.Artifact == nil || e.scoutingJobsRepo == nil {
		return nil, false
	}
	if isSupplyChainRefineContinuationJob(job) {
		return nil, false
	}
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" {
		lensID = parseStateString(job.State["lensId"])
	}
	if lensID != "" {
		if jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, job.TenantID, job.ProjectID, lensID, 200); err == nil {
			for _, existing := range jobs {
				if existing == nil || existing.Status == agentmodels.AgentJobStatusCompleted || existing.Status == agentmodels.AgentJobStatusFailed {
					continue
				}
				if strings.TrimSpace(parseStateString(existing.State["mode"])) != supplyChainContinuationModeRefineExisting {
					continue
				}
				if strings.TrimSpace(parseStateString(existing.State["parentJobId"])) == strings.TrimSpace(job.ID) &&
					strings.TrimSpace(parseStateString(existing.State["assemblyArtifactKey"])) == strings.TrimSpace(result.Artifact.CanonicalKey) {
					return []string{strings.TrimSpace(existing.ID)}, true
				}
			}
		}
	}

	now := time.Now().UTC()
	state := map[string]interface{}{
		"mode":                    supplyChainContinuationModeRefineExisting,
		"skipCoreAssembly":        true,
		"parentJobId":             job.ID,
		"assemblyArtifactKey":     result.Artifact.CanonicalKey,
		"resolvedAnchorsSnapshot": append([]string(nil), result.ResolvedAnchors...),
		"frontiersToExpand":       append([]string(nil), frontiers...),
		"openGapsToResearch":      append([]string(nil), result.OpenGaps...),
		"baselineAssemblyHash":    computeAssemblyArtifactHash(result.Artifact),
		"pipelinePass":            string(pipeline.PassBaseEnrichment),
	}
	for _, key := range []string{
		"targetCanonicalPath", "currentState", "scoutingStrategy", "recursionDepth", "maxRecursionDepth",
		"lensId", "maxChildrenPerJob", "cascadeRootJobId", "requestedMetrics", "specialties",
		"chainContext", "focus", "keywords", "spherePaths", "originalQuery", "decompositionHints",
		"executionPlan", "domainModes",
	} {
		if value, ok := job.State[key]; ok {
			state[key] = value
		}
	}
	copyDomainModeStateToChild(job.State, state)

	continuationJob := &models.ScoutingJob{
		AgentJob: agentmodels.AgentJob{
			TenantID:  job.TenantID,
			ProjectID: job.ProjectID,
			Status:    agentmodels.AgentJobStatusPending,
			AgentID:   firstNonEmpty(job.AgentID, "scout"),
			CreatedAt: now,
			UpdatedAt: now,
			State:     state,
		},
		LensID:              lensID,
		TargetCanonicalPath: strings.TrimSpace(job.TargetCanonicalPath),
		ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
		BillingContext: models.BillingContext{
			ChargeModel: string(models.ChargeModelBounty),
			ProjectID:   job.ProjectID,
		},
		CurrentState: models.ScoutingJobStatePending,
	}
	jobID, err := e.scoutingJobsRepo.Create(ctx, continuationJob)
	if err != nil {
		return nil, false
	}
	return []string{strings.TrimSpace(jobID)}, true
}

func (e *ScoutEngine) persistSupplyChainAssemblyState(ctx context.Context, job *models.ScoutingJob, summary map[string]interface{}) {
	if e == nil || job == nil || e.scoutingJobsRepo == nil {
		return
	}
	_ = e.scoutingJobsRepo.UpdateState(ctx, job.ID, map[string]interface{}{
		"supplyChainAssembly": summary,
	})
}

func hasSupplyChainAssemblySummary(job *models.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	raw, ok := job.State["supplyChainAssembly"].(map[string]interface{})
	if !ok || raw == nil {
		return false
	}
	status := strings.TrimSpace(parseStateString(raw["status"]))
	return status == "complete" || status == "partial" || status == "empty"
}

func isSupplyChainRefineContinuationJob(job *models.ScoutingJob) bool {
	if job == nil || job.State == nil {
		return false
	}
	return strings.TrimSpace(parseStateString(job.State["mode"])) == supplyChainContinuationModeRefineExisting ||
		parseStateBool(job.State["skipCoreAssembly"], false)
}

func isSupplyChainDomainJob(job *models.ScoutingJob) bool {
	return taxonomy.HasDomainMode(getJobDomainModes(job), taxonomy.DomainModeSupplyChain)
}

func isFastFirstSupplyChainJob(job *models.ScoutingJob) bool {
	if !isSupplyChainDomainJob(job) || job == nil || job.State == nil {
		return false
	}
	contractTypes := contractTypesFromDomainsRaw(job.State["domains"])
	if len(contractTypes) == 0 {
		targetCT, _ := parseDomainPathFacets(strings.TrimSpace(job.TargetCanonicalPath))
		if agents.IsSupplyChainContractType(targetCT) {
			return true
		}
		return isSupplyChainDomainJob(job)
	}
	hasSupplyChain := false
	for _, contractType := range contractTypes {
		if agents.IsSupplyChainContractType(contractType) {
			hasSupplyChain = true
			continue
		}
		return false
	}
	return hasSupplyChain
}

func buildSupplyChainFrontiers(cc *ChainContext) []string {
	if cc == nil {
		return nil
	}
	incoming := map[string]int{}
	outgoing := map[string]int{}
	for _, edge := range cc.DiscoveredEdges {
		incoming[strings.TrimSpace(edge.Target)]++
		outgoing[strings.TrimSpace(edge.Source)]++
	}
	out := make([]string, 0)
	for _, ref := range cc.DiscoveredNodes {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if incoming[ref] == 0 || outgoing[ref] == 0 {
			out = append(out, ref)
		}
	}
	return dedupStrings(out)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func firstNonEmptyStringSlice(primary []string, fallback []string) []string {
	if len(primary) > 0 {
		return dedupStrings(primary)
	}
	return dedupStrings(fallback)
}

func normalizeSupplyChainAssemblyStage(stage string) string {
	switch strings.TrimSpace(stage) {
	case stageEntitySupplyChainFastAssembly:
		return "fast"
	case stageEntitySupplyChainRefine:
		return "refined"
	default:
		return strings.TrimSpace(stage)
	}
}

func countAssemblyAnchorComponents(artifact *models.MaterializedArtifact) int {
	if artifact == nil {
		return 0
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return 0
	}
	return len(parsed.AnchorComponents)
}

func computeAssemblyArtifactHash(artifact *models.MaterializedArtifact) string {
	if artifact == nil {
		return ""
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(artifact.CanonicalKey) + "|" + fmt.Sprint(artifact.Payload)))
	return fmt.Sprintf("%x", h[:12])
}

func (e *ScoutEngine) loadSupplyChainAssemblyArtifact(ctx context.Context, job *models.ScoutingJob) (*models.MaterializedArtifact, *models.SupplyChainAssemblyGraphPayload, error) {
	if e == nil || job == nil || job.State == nil || e.planner == nil || e.planner.materializedRepo == nil {
		return nil, nil, nil
	}
	artifactKey := strings.TrimSpace(parseStateString(job.State["assemblyArtifactKey"]))
	if artifactKey == "" {
		if summary, ok := job.State["supplyChainAssembly"].(map[string]interface{}); ok && summary != nil {
			artifactKey = strings.TrimSpace(parseStateString(summary["artifactKey"]))
		}
	}
	if artifactKey == "" {
		return nil, nil, nil
	}
	tenantID := strings.TrimSpace(firstNonEmpty(job.TenantID, "global"))
	artifact, err := e.planner.materializedRepo.GetByCanonicalKey(ctx, tenantID, models.MaterializedKindSupplyChainAssemblyGraph, artifactKey)
	if err != nil || artifact == nil {
		if tenantID != "global" {
			artifact, err = e.planner.materializedRepo.GetByCanonicalKey(ctx, "global", models.MaterializedKindSupplyChainAssemblyGraph, artifactKey)
		}
		if err != nil || artifact == nil {
			return nil, nil, err
		}
	}
	parsed, err := models.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
	if err != nil {
		return artifact, nil, err
	}
	return artifact, parsed, nil
}

func buildSupplyChainAssemblyScopeRefs(parsed *models.SupplyChainAssemblyGraphPayload, frontiers []string) []string {
	if parsed == nil {
		return uniqueSortedStrings(frontiers)
	}
	refs := make([]string, 0, len(parsed.ProductNodes)+len(parsed.ActorNodes)+len(frontiers))
	for _, node := range parsed.ProductNodes {
		refs = append(refs, strings.TrimSpace(node.Ref))
	}
	for _, node := range parsed.ActorNodes {
		refs = append(refs, strings.TrimSpace(node.Ref))
	}
	refs = append(refs, frontiers...)
	return uniqueSortedStrings(refs)
}

func buildScopedContinuationChainContext(job *models.ScoutingJob, parsed *models.SupplyChainAssemblyGraphPayload, scopeRefs []string) *ChainContext {
	if job == nil || job.State == nil {
		return nil
	}
	cc, _ := ParseChainContext(job.State["chainContext"])
	if cc == nil {
		return nil
	}
	scopeSet := map[string]struct{}{}
	for _, ref := range buildSupplyChainAssemblyScopeRefs(parsed, scopeRefs) {
		scopeSet[strings.TrimSpace(ref)] = struct{}{}
	}
	if len(scopeSet) == 0 {
		return cc
	}
	out := NewChainContext()
	for _, ref := range cc.DiscoveredNodes {
		ref = strings.TrimSpace(ref)
		if _, ok := scopeSet[ref]; ok {
			out.DiscoveredNodes = append(out.DiscoveredNodes, ref)
			if cc.NodeLabels != nil {
				if out.NodeLabels == nil {
					out.NodeLabels = map[string]string{}
				}
				if label := strings.TrimSpace(cc.NodeLabels[ref]); label != "" {
					out.NodeLabels[ref] = label
				}
			}
		}
	}
	for _, edge := range cc.DiscoveredEdges {
		source := strings.TrimSpace(edge.Source)
		target := strings.TrimSpace(edge.Target)
		if _, ok := scopeSet[source]; !ok {
			continue
		}
		if _, ok := scopeSet[target]; !ok {
			continue
		}
		out.DiscoveredEdges = append(out.DiscoveredEdges, edge)
	}
	out.GapHints = uniqueSortedStrings(parseStateStringSlice(job.State["openGapsToResearch"]))
	return out
}

func (e *ScoutEngine) loadSupplyChainContinuationFamilyBatches(
	ctx context.Context,
	job *models.ScoutingJob,
	artifact *models.MaterializedArtifact,
	parsed *models.SupplyChainAssemblyGraphPayload,
) map[string]*supplyChainFamilyBatch {
	if e == nil || job == nil || artifact == nil || parsed == nil || e.planner == nil || e.planner.materializedRepo == nil {
		return nil
	}
	tenantID := strings.TrimSpace(firstNonEmpty(artifact.TenantID, job.TenantID, "global"))
	candidates := make([]supplyChainFamilyCandidate, 0, len(parsed.DerivedPathKeys))
	for _, key := range parsed.DerivedPathKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		pathArtifact, err := e.planner.materializedRepo.GetByCanonicalKey(ctx, tenantID, models.MaterializedKindSupplyChainPath, key)
		if err != nil || pathArtifact == nil {
			if tenantID != "global" {
				pathArtifact, _ = e.planner.materializedRepo.GetByCanonicalKey(ctx, "global", models.MaterializedKindSupplyChainPath, key)
			}
		}
		if pathArtifact == nil {
			continue
		}
		payload, err := models.ParseSupplyChainPathPayload(pathArtifact.Payload)
		if err != nil || payload == nil {
			continue
		}
		segments := make([]pathbuilder.PathSegment, 0, len(payload.Legs))
		for _, leg := range payload.Legs {
			segments = append(segments, pathbuilder.PathSegment{
				SupplyLinkID:            strings.TrimSpace(leg.SupplyLinkID),
				SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				SupplierRef:             strings.TrimSpace(leg.SupplierRef),
				ProductRef:              strings.TrimSpace(leg.ProductRef),
				CustomerRef:             strings.TrimSpace(leg.CustomerRef),
				TransportViaRefs:        append([]string(nil), leg.TransportViaRefs...),
				Confidence:              leg.SegmentConfidence,
			})
		}
		if len(segments) == 0 {
			continue
		}
		candidates = append(candidates, supplyChainFamilyCandidate{
			canonicalKey: key,
			path: &pathbuilder.SupplyChainPath{
				Segments: segments,
				Family:   payload.Family,
			},
			sufficiency: payload.Sufficiency,
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	return buildSupplyChainFamilyBatches(tenantID, parseStateInt(job.State["supplyChainFamilyCoResearchRound"], 0)+1, candidates)
}
