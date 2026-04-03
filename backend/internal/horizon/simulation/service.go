package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/metriccore"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	defaultRunTTL        = 72 * time.Hour
	defaultSimulationTTL = 72 * time.Hour
)

type AgentMeshTaskSubmitter interface {
	Submit(ctx context.Context, tenantID, capability string, input map[string]any, dedupKey string) (string, error)
}

type Publisher func(ctx context.Context, eventType string, tenantID string, payload any) error

type Service struct {
	runs         *horizonrepo.SimulationRunsRepository
	worlds       *horizonrepo.SimulationWorldsRepository
	agents       *horizonrepo.SimulationAgentsRepository
	events       *horizonrepo.SimulationEventsRepository
	artifacts    *horizonrepo.SimulationArtifactsRepository
	aggregates   *horizonrepo.AnalyticalAggregatesRepository
	fragments    *horizonrepo.HorizonFragmentsRepository
	edges        *horizonrepo.DependencyEdgesRepository
	lenses       *horizonrepo.LensesRepository
	materialized *horizonrepo.MaterializedArtifactsRepository
	facts        *corerepo.OntologyFactsRepository
	propagation  *horizonprop.PropagationEngine
	profiles     *horizonprop.ProfileLookup
	submitter    AgentMeshTaskSubmitter
	operational  *metriccore.OperationalHydrator
	publisher    Publisher
	logger       *log.Logger
}

type Dependencies struct {
	RunsRepo                 *horizonrepo.SimulationRunsRepository
	WorldsRepo               *horizonrepo.SimulationWorldsRepository
	AgentsRepo               *horizonrepo.SimulationAgentsRepository
	EventsRepo               *horizonrepo.SimulationEventsRepository
	ArtifactsRepo            *horizonrepo.SimulationArtifactsRepository
	AnalyticalAggregatesRepo *horizonrepo.AnalyticalAggregatesRepository
	FragmentsRepo            *horizonrepo.HorizonFragmentsRepository
	EdgesRepo                *horizonrepo.DependencyEdgesRepository
	LensesRepo               *horizonrepo.LensesRepository
	MaterializedRepo         *horizonrepo.MaterializedArtifactsRepository
	OntologyFactsRepo        *corerepo.OntologyFactsRepository
	PropagationEngine        *horizonprop.PropagationEngine
	PropagationProfileLookup *horizonprop.ProfileLookup
	Submitter                AgentMeshTaskSubmitter
	OperationalHydrator      *metriccore.OperationalHydrator
	Publisher                Publisher
	Logger                   *log.Logger
}

func NewService(deps Dependencies) (*Service, error) {
	if deps.RunsRepo == nil || deps.WorldsRepo == nil || deps.AgentsRepo == nil || deps.EventsRepo == nil || deps.ArtifactsRepo == nil {
		return nil, fmt.Errorf("simulation repositories are required")
	}
	if deps.FragmentsRepo == nil || deps.EdgesRepo == nil || deps.LensesRepo == nil || deps.MaterializedRepo == nil || deps.OntologyFactsRepo == nil {
		return nil, fmt.Errorf("simulation baseline repositories are required")
	}
	if deps.PropagationEngine == nil || deps.PropagationProfileLookup == nil {
		return nil, fmt.Errorf("propagation engine and profile lookup are required")
	}
	if deps.Logger == nil {
		deps.Logger = log.Default()
	}
	return &Service{
		runs:         deps.RunsRepo,
		worlds:       deps.WorldsRepo,
		agents:       deps.AgentsRepo,
		events:       deps.EventsRepo,
		artifacts:    deps.ArtifactsRepo,
		aggregates:   deps.AnalyticalAggregatesRepo,
		fragments:    deps.FragmentsRepo,
		edges:        deps.EdgesRepo,
		lenses:       deps.LensesRepo,
		materialized: deps.MaterializedRepo,
		facts:        deps.OntologyFactsRepo,
		propagation:  deps.PropagationEngine,
		profiles:     deps.PropagationProfileLookup,
		submitter:    deps.Submitter,
		operational:  deps.OperationalHydrator,
		publisher:    deps.Publisher,
		logger:       deps.Logger,
	}, nil
}

func (s *Service) CreateRun(ctx context.Context, tenantID, projectID string, req *horizonmodels.SimulationRunRequest) (*horizonmodels.SimulationRun, error) {
	tenantID = strings.TrimSpace(tenantID)
	projectID = strings.TrimSpace(projectID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenantID is required")
	}
	if req == nil {
		return nil, fmt.Errorf("simulation request is nil")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	baselineRefs, err := s.buildBaselineRefs(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	var lens *horizonmodels.Lens
	if strings.TrimSpace(req.LensID) != "" {
		lens, _ = s.lenses.Get(ctx, strings.TrimSpace(req.LensID))
	}
	recognition := sanitizeSimulationDocumentMap(BuildInputRecognition(req, lens, baselineRefs))
	_, signal, preview := s.BuildSimulationBaseline(ctx, tenantID, projectID, req, baselineRefs, recognition)
	preview = sanitizeSimulationDocumentMap(preview)
	if s.logger != nil {
		_, hasHydration := preview["operationalHydration"]
		s.logger.Printf("[horizon.simulation] create_run_preview lens=%s run_question=%q has_operational=%t preview_keys=%d", strings.TrimSpace(req.LensID), strings.TrimSpace(req.Question), hasHydration, len(preview))
	}
	now := time.Now().UTC()
	run := &horizonmodels.SimulationRun{
		TenantID:        tenantID,
		ProjectID:       firstNonEmpty(projectID, req.ProjectID),
		Status:          string(horizonmodels.SimulationRunStatusQueued),
		Request:         *req,
		Execution:       executionFromRequest(req),
		Recognition:     recognition,
		BaselineRefs:    baselineRefs,
		BaselineSignal:  signal,
		BaselinePreview: preview,
		Progress: horizonmodels.SimulationProgress{
			TotalWorlds: req.WorldCount,
			TotalTicks:  req.WorldCount * req.TickCount,
		},
		ExpiresAt: now.Add(defaultRunTTL),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := run.Validate(); err != nil {
		return nil, err
	}
	runID, err := s.runs.Create(ctx, run)
	if err != nil {
		return nil, err
	}
	run.ID = runID

	if s.submitter == nil {
		return run, nil
	}
	taskInput := map[string]any{
		"runId":    run.ID,
		"tenantId": run.TenantID,
		"question": run.Request.Question,
	}
	taskID, err := s.submitter.Submit(ctx, run.TenantID, "sim.plan", taskInput, "sim.plan:"+run.ID)
	if err != nil {
		run.Status = string(horizonmodels.SimulationRunStatusFailed)
		run.Failure = &horizonmodels.SimulationFailureInfo{Message: err.Error(), RetryClass: "transient"}
		_ = s.runs.Update(ctx, run)
		return nil, err
	}
	run.AgentMeshTaskIDs = append(run.AgentMeshTaskIDs, taskID)
	if err := s.runs.Update(ctx, run); err != nil {
		return nil, err
	}
	s.publish(ctx, "horizon.simulation.run.created", run.TenantID, map[string]any{
		"runId":           run.ID,
		"agentmeshTaskId": taskID,
		"status":          run.Status,
	})
	return run, nil
}

func (s *Service) GetRun(ctx context.Context, runID string) (*horizonmodels.SimulationRun, error) {
	return s.runs.Get(ctx, runID)
}

func (s *Service) ListRecentByTenant(ctx context.Context, tenantID string, limit int) ([]*horizonmodels.SimulationRun, error) {
	return s.runs.ListRecentByTenant(ctx, tenantID, limit)
}

func (s *Service) GetLatestRunByLens(ctx context.Context, tenantID, lensID string) (*horizonmodels.SimulationRun, error) {
	return s.runs.GetLatestByTenantAndLens(ctx, tenantID, lensID)
}

func (s *Service) ListRunsByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*horizonmodels.SimulationRun, error) {
	return s.runs.ListByTenantAndLens(ctx, tenantID, lensID, limit)
}

func (s *Service) ListWorlds(ctx context.Context, tenantID, runID string) ([]*horizonmodels.SimulationWorld, error) {
	return s.worlds.ListByRun(ctx, tenantID, runID, 256)
}

func (s *Service) GetBundle(ctx context.Context, tenantID, runID string) (*horizonmodels.SimulationArtifact, error) {
	artifacts, err := s.artifacts.ListByRun(ctx, tenantID, runID, 64)
	if err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		if strings.EqualFold(strings.TrimSpace(artifact.Kind), string(horizonmodels.SimulationArtifactKindBundle)) {
			return artifact, nil
		}
	}
	return nil, fmt.Errorf("bundle artifact not found")
}

func (s *Service) GetBundleView(ctx context.Context, tenantID, runID string) (*horizonmodels.SimulationBundle, *horizonmodels.SimulationArtifact, error) {
	artifact, err := s.GetBundle(ctx, tenantID, runID)
	if err != nil {
		return nil, nil, err
	}
	bundle, err := DecodeSimulationBundleArtifact(artifact)
	if err != nil {
		return nil, artifact, err
	}
	return bundle, artifact, nil
}

func (s *Service) EnsureScenarioAggregate(ctx context.Context, tenantID, runID string) (*horizonmodels.AnalyticalAggregate, error) {
	if s == nil || s.aggregates == nil {
		return nil, nil
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil || run == nil {
		return nil, err
	}
	if tenantID == "" {
		tenantID = run.TenantID
	}
	existing, err := s.aggregates.ListByScopeRefs(ctx, strings.TrimSpace(tenantID), []string{strings.TrimSpace(runID)}, 8)
	if err == nil {
		for _, item := range existing {
			if item == nil || !strings.EqualFold(strings.TrimSpace(item.AggregateType), "disruption_impact_summary") {
				continue
			}
			if item.StaleAt == nil || item.StaleAt.After(time.Now().UTC()) {
				return item, nil
			}
		}
	}
	bundle, _, err := s.GetBundleView(ctx, strings.TrimSpace(tenantID), runID)
	if err != nil {
		bundle = nil
	}
	return analytical.PersistSimulationRunAggregate(ctx, s.aggregates, run, bundle, time.Now().UTC())
}

func (s *Service) GetTrace(ctx context.Context, tenantID, runID string) (map[string]any, error) {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	worlds, err := s.worlds.ListByRun(ctx, tenantID, runID, 256)
	if err != nil {
		return nil, err
	}
	events, err := s.events.ListByWorld(ctx, tenantID, runID, "", 512)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run":    run,
		"worlds": worlds,
		"events": events,
	}, nil
}

func DecodeSimulationBundleArtifact(artifact *horizonmodels.SimulationArtifact) (*horizonmodels.SimulationBundle, error) {
	if artifact == nil || artifact.Payload == nil {
		return nil, fmt.Errorf("bundle artifact is empty")
	}
	rawBundle, ok := artifact.Payload["bundle"]
	if !ok {
		return nil, fmt.Errorf("bundle payload is missing")
	}
	blob, err := json.Marshal(rawBundle)
	if err != nil {
		return nil, err
	}
	var bundle horizonmodels.SimulationBundle
	if err := json.Unmarshal(blob, &bundle); err != nil {
		return nil, err
	}
	return &bundle, nil
}

func (s *Service) buildBaselineRefs(ctx context.Context, tenantID string, req *horizonmodels.SimulationRunRequest) (horizonmodels.SimulationBaselineRefs, error) {
	var refs horizonmodels.SimulationBaselineRefs
	refs.LensID = strings.TrimSpace(req.LensID)
	scopePaths := make([]string, 0, len(req.SeedPaths)+32)
	scopePaths = append(scopePaths, req.SeedPaths...)
	artifactKeys := make([]string, 0, len(req.ArtifactKeys)+32)
	artifactKeys = append(artifactKeys, req.ArtifactKeys...)
	if refs.LensID != "" {
		lens, err := s.lenses.Get(ctx, refs.LensID)
		if err != nil {
			return refs, err
		}
		scopePaths = append(scopePaths, workscope.LensWorkbenchScopePaths(lens)...)
		for key, ref := range lens.ScopeArtifacts {
			artifactKeys = append(artifactKeys, key)
			scopePaths = append(scopePaths,
				strings.TrimSpace(ref.AnchorSupplier),
				strings.TrimSpace(ref.AnchorCustomer),
				strings.TrimSpace(ref.ProductAxis),
			)
			scopePaths = append(scopePaths, ref.NodeRefs...)
		}
		if arts, err := s.materialized.ListByLens(ctx, refs.LensID, workscope.MaterializedListByLensLimitSimulation); err == nil {
			for _, art := range arts {
				artifactKeys = append(artifactKeys, art.CanonicalKey)
				scopePaths = append(scopePaths,
					strings.TrimSpace(art.AnchorSupplier),
					strings.TrimSpace(art.AnchorCustomer),
					strings.TrimSpace(art.ProductAxis),
				)
				scopePaths = append(scopePaths, art.NodeRefs...)
				refs.ArtifactIDs = append(refs.ArtifactIDs, art.ID)
			}
		}
	}
	scopePaths = compactStringsPreserveOrder(scopePaths)
	artifactKeys = compactStrings(artifactKeys)
	refs.ScopePaths = scopePaths
	refs.ArtifactKeys = artifactKeys

	fragments, err := s.fragments.ListByCanonicalPaths(ctx, scopePaths)
	if err != nil {
		return refs, err
	}
	for _, frag := range fragments {
		if frag == nil {
			continue
		}
		refs.FragmentIDs = append(refs.FragmentIDs, strings.TrimSpace(frag.ID))
		refs.FragmentPaths = append(refs.FragmentPaths, strings.TrimSpace(frag.CanonicalPath))
	}
	for _, key := range artifactKeys {
		art, err := s.materialized.GetByCanonicalKeyAnyKind(ctx, tenantID, key)
		if err == nil && art != nil {
			refs.ArtifactIDs = append(refs.ArtifactIDs, strings.TrimSpace(art.ID))
			scopePaths = append(scopePaths, art.NodeRefs...)
		}
	}
	refs.ScopePaths = compactStrings(scopePaths)
	refs.FragmentIDs = compactStrings(refs.FragmentIDs)
	refs.FragmentPaths = compactStrings(refs.FragmentPaths)
	refs.ArtifactIDs = compactStrings(refs.ArtifactIDs)

	edgeIDs := make([]string, 0, len(refs.FragmentIDs)*2)
	for _, fragmentID := range refs.FragmentIDs {
		sourceEdges, _ := s.edges.ListBySource(ctx, tenantID, fragmentID, nil, 64)
		targetEdges, _ := s.edges.ListByTarget(ctx, tenantID, fragmentID, nil, 64)
		for _, edge := range append(sourceEdges, targetEdges...) {
			if edge != nil {
				edgeIDs = append(edgeIDs, strings.TrimSpace(edge.ID))
			}
		}
	}
	refs.EdgeIDs = compactStrings(edgeIDs)

	factIDs := make([]string, 0, len(refs.ScopePaths)*4)
	for _, path := range refs.ScopePaths {
		facts, err := s.facts.ListByCanonicalPath(ctx, tenantID, path, "", 8)
		if err != nil {
			continue
		}
		for _, fact := range facts {
			if fact != nil {
				factIDs = append(factIDs, strings.TrimSpace(fact.ID))
			}
		}
	}
	refs.FactIDs = compactStrings(factIDs)
	refs.SnapshotDigest = horizonmodels.SimulationSnapshotDigest(
		refs.LensID,
		refs.ScopePaths,
		refs.FragmentPaths,
		refs.EdgeIDs,
		refs.FactIDs,
		refs.ArtifactKeys,
	)
	return refs, nil
}

func (s *Service) buildBaselineSignalAndPreview(ctx context.Context, tenantID string, req *horizonmodels.SimulationRunRequest, refs horizonmodels.SimulationBaselineRefs, recognition map[string]any, overlay map[string]any) (*horizonmodels.SignalDescriptor, map[string]any) {
	signal := signalFromRequest(req, refs.ScopePaths, recognition)
	artifactPreview := mergeSimulationPreview(s.buildArtifactPathPreview(ctx, refs, recognition), overlay)
	if signal == nil {
		return nil, artifactPreview
	}
	seedIDs := s.resolveSeedFragmentIDs(ctx, tenantID, signal)
	if len(seedIDs) == 0 {
		return signal, mergeSimulationPreview(map[string]any{
			"seedFragmentIds": []string{},
			"status":          "no_seeds",
		}, artifactPreview)
	}
	result, err := s.propagation.SimulateSignalFromSeeds(ctx, signal, s.profiles, seedIDs)
	if err != nil {
		s.logger.Printf("[horizon.simulation] baseline preview failed: %v", err)
		return signal, mergeSimulationPreview(map[string]any{
			"seedFragmentIds": seedIDs,
			"status":          "preview_failed",
			"error":           err.Error(),
		}, artifactPreview)
	}
	return signal, mergeSimulationPreview(map[string]any{
		"seedFragmentIds":    seedIDs,
		"affectedFragments":  result.AffectedFragments,
		"propagationPaths":   result.PropagationPaths,
		"metrics":            result.Metrics,
		"trace":              result.Trace,
		"traceDropped":       result.TraceDropped,
		"propagationTimeMs":  result.PropagationTime.Milliseconds(),
		"baselineDigest":     refs.SnapshotDigest,
		"recognition":        recognition,
		"previewGeneratedAt": time.Now().UTC(),
	}, artifactPreview)
}

func (s *Service) buildOperationalPreview(ctx context.Context, tenantID string, req *horizonmodels.SimulationRunRequest, refs horizonmodels.SimulationBaselineRefs, recognition map[string]any) map[string]any {
	if req == nil {
		return map[string]any{
			"operationalHydration": map[string]any{
				"status": "skipped",
				"reason": "request_nil",
			},
		}
	}
	if s.operational == nil {
		if s.logger != nil {
			s.logger.Printf("[horizon.simulation] operational hydration unavailable lens=%s reason=hydrator_nil", strings.TrimSpace(req.LensID))
		}
		return map[string]any{
			"operationalHydration": map[string]any{
				"status": "unavailable",
				"reason": "hydrator_nil",
			},
		}
	}
	snapshot, err := s.operational.Hydrate(ctx, metriccore.OperationalHydrationRequest{
		TenantID:     tenantID,
		LensID:       strings.TrimSpace(req.LensID),
		Question:     strings.TrimSpace(req.Question),
		Recognition:  recognition,
		DomainModes:  append([]string(nil), req.DomainModes...),
		BaselineRefs: refs,
	})
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("[horizon.simulation] operational hydration failed lens=%s err=%v", strings.TrimSpace(req.LensID), err)
		}
		return map[string]any{
			"operationalHydration": map[string]any{
				"status": "failed",
				"error":  err.Error(),
			},
		}
	}
	if snapshot == nil {
		if s.logger != nil {
			s.logger.Printf("[horizon.simulation] operational hydration empty lens=%s", strings.TrimSpace(req.LensID))
		}
		return map[string]any{
			"operationalHydration": map[string]any{
				"status": "empty",
			},
		}
	}
	preview := metriccore.SnapshotPreviewOverlay(snapshot)
	preview["operationalHydration"] = map[string]any{
		"status":               "ready",
		"targetCount":          snapshot.Summary.TargetCount,
		"refreshedTargetCount": snapshot.Summary.RefreshedTargetCount,
		"generatedAt":          snapshot.GeneratedAt,
	}
	if s.logger != nil {
		s.logger.Printf("[horizon.simulation] operational hydration ready lens=%s targets=%d refreshed=%d missing=%d blockage=%.3f screening=%.3f", strings.TrimSpace(req.LensID), snapshot.Summary.TargetCount, snapshot.Summary.RefreshedTargetCount, snapshot.Summary.MissingMetricCount, snapshot.Summary.BlockageMax, snapshot.Summary.ScreeningIntensityMax)
	}
	return preview
}

type artifactShoulderAggregate struct {
	code      string
	refs      []string
	pathCount int
	score     float64
	maxStress float64
}

type artifactProductAggregate struct {
	path      string
	pathCount int
	score     float64
	maxStress float64
}

type artifactChainAggregate struct {
	code      string
	path      string
	refs      []string
	pathCount int
	score     float64
	maxStress float64
}

func (s *Service) buildArtifactPathPreview(ctx context.Context, refs horizonmodels.SimulationBaselineRefs, recognition map[string]any) map[string]any {
	profile := simscope.BuildProfile(stringValue(recognition, "query"), nil, refs, recognition)
	artifacts := s.collectBaselineArtifacts(ctx, refs, profile)
	if len(artifacts) == 0 {
		return nil
	}

	primaryGeo := recognitionPrimaryGeoCode(recognition)
	shoulders := map[string]*artifactShoulderAggregate{}
	products := map[string]*artifactProductAggregate{}
	chains := map[string]*artifactChainAggregate{}
	pathCount := 0
	stressedCount := 0
	legCountTotal := 0
	stressTotal := 0.0
	bindingTotal := 0.0
	bindingCount := 0
	stressMax := 0.0
	recoveryMax := 0.0
	productRiskMax := 0.0
	shoulderRiskMax := 0.0

	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		pathCount++
		stressScore := artifact.PathStressScore
		if stressScore > 0 {
			stressTotal += stressScore
			if stressScore > stressMax {
				stressMax = stressScore
			}
		}
		if stressScore >= 0.45 {
			stressedCount++
		}
		if artifact.BindingScore > 0 {
			bindingTotal += artifact.BindingScore
			bindingCount++
		}
		if artifact.RecoveryLatencyHrs > recoveryMax {
			recoveryMax = artifact.RecoveryLatencyHrs
		}
		legCountTotal += artifact.LegCount

		pathRiskScore := artifactRiskScore(artifact)
		productAxis := strings.TrimSpace(artifact.ProductAxis)
		if productAxis != "" {
			entry := products[productAxis]
			if entry == nil {
				entry = &artifactProductAggregate{path: productAxis}
				products[productAxis] = entry
			}
			entry.pathCount++
			entry.score += pathRiskScore
			if stressScore > entry.maxStress {
				entry.maxStress = stressScore
			}
			if entry.score > productRiskMax {
				productRiskMax = entry.score
			}
		}

		shoulderRefs := artifactShoulderRefs(artifact)
		for _, code := range artifactShoulderCountries(artifact, primaryGeo) {
			entry := shoulders[code]
			if entry == nil {
				entry = &artifactShoulderAggregate{code: code}
				shoulders[code] = entry
			}
			entry.pathCount++
			entry.score += pathRiskScore
			entry.maxStress = maxFloat(entry.maxStress, stressScore)
			entry.refs = append(entry.refs, shoulderRefs...)
			if entry.score > shoulderRiskMax {
				shoulderRiskMax = entry.score
			}
			if productAxis != "" {
				chainKey := code + "::" + productAxis
				chain := chains[chainKey]
				if chain == nil {
					chain = &artifactChainAggregate{code: code, path: productAxis}
					chains[chainKey] = chain
				}
				chain.pathCount++
				chain.score += pathRiskScore
				chain.maxStress = maxFloat(chain.maxStress, stressScore)
				chain.refs = append(chain.refs, shoulderRefs...)
				chain.refs = append(chain.refs, productAxis)
			}
		}
	}
	if pathCount == 0 {
		return nil
	}
	metrics := map[string]any{
		"artifact_path_count":           pathCount,
		"artifact_path_stress_avg":      round3(stressTotal / float64(pathCount)),
		"artifact_path_stress_max":      round3(stressMax),
		"artifact_binding_avg":          round3(safeDiv(bindingTotal, float64(bindingCount))),
		"artifact_recovery_latency_max": round3(recoveryMax),
		"artifact_leg_count_total":      legCountTotal,
		"artifact_stressed_paths":       stressedCount,
		"artifact_product_risk_max":     round3(productRiskMax),
		"artifact_shoulder_risk_max":    round3(shoulderRiskMax),
	}
	shoulderPreview := buildArtifactShoulderPreview(shoulders)
	allowedShoulders := map[string]struct{}{}
	for _, item := range shoulderPreview {
		code := strings.ToLower(strings.TrimSpace(stringValue(item, "code")))
		if code == "" {
			continue
		}
		allowedShoulders[code] = struct{}{}
	}

	return map[string]any{
		"metrics":              metrics,
		"artifactTopShoulders": shoulderPreview,
		"artifactTopProducts":  buildArtifactProductPreview(products),
		"artifactChains":       buildArtifactChainPreview(chains, allowedShoulders),
	}
}

func (s *Service) collectBaselineArtifacts(ctx context.Context, refs horizonmodels.SimulationBaselineRefs, profile simscope.Profile) []*horizonmodels.MaterializedArtifact {
	if s.materialized == nil {
		return nil
	}
	ws := workscope.FromBaselineRefs("", "", refs)
	resolved, _ := workscope.NewResolver(s.materialized).Resolve(ctx, ws)
	return workscope.CollectMaterializedArtifacts(ctx, s.materialized, resolved, profile)
}

func buildArtifactShoulderPreview(input map[string]*artifactShoulderAggregate) []map[string]any {
	items := make([]*artifactShoulderAggregate, 0, len(input))
	for _, entry := range input {
		if entry == nil || entry.pathCount == 0 {
			continue
		}
		entry.refs = compactStringsPreserveOrder(entry.refs)
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].code < items[j].code
		}
		return items[i].score > items[j].score
	})
	items = filterManagementRelevantShoulders(items)
	out := make([]map[string]any, 0, min(len(items), 6))
	for _, entry := range items[:min(len(items), 6)] {
		out = append(out, map[string]any{
			"code":      entry.code,
			"name":      countryNameFromCode(entry.code),
			"pathCount": entry.pathCount,
			"riskScore": round3(entry.score),
			"maxStress": round3(entry.maxStress),
			"refs":      traceSlice(entry.refs, 4),
		})
	}
	return out
}

func filterManagementRelevantShoulders(items []*artifactShoulderAggregate) []*artifactShoulderAggregate {
	if len(items) <= 1 {
		return items
	}
	leaderPathCount := max(items[0].pathCount, 1)
	leaderScore := maxFloat(items[0].score, 1)
	const minRelativeShare = 0.08
	filtered := make([]*artifactShoulderAggregate, 0, len(items))
	filtered = append(filtered, items[0])
	for _, item := range items[1:] {
		if item == nil || item.pathCount < 2 {
			continue
		}
		coverageShare := float64(item.pathCount) / float64(leaderPathCount)
		severityShare := item.score / leaderScore
		if coverageShare >= minRelativeShare || severityShare >= minRelativeShare || item.maxStress >= 0.58 {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func buildArtifactProductPreview(input map[string]*artifactProductAggregate) []map[string]any {
	items := make([]*artifactProductAggregate, 0, len(input))
	for _, entry := range input {
		if entry == nil || entry.pathCount == 0 {
			continue
		}
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].path < items[j].path
		}
		return items[i].score > items[j].score
	})
	out := make([]map[string]any, 0, min(len(items), 6))
	for _, entry := range items[:min(len(items), 6)] {
		out = append(out, map[string]any{
			"path":      entry.path,
			"name":      strings.ReplaceAll(lastPathSegment(entry.path), "-", " "),
			"pathCount": entry.pathCount,
			"riskScore": round3(entry.score),
			"maxStress": round3(entry.maxStress),
		})
	}
	return out
}

func buildArtifactChainPreview(input map[string]*artifactChainAggregate, allowedCountries map[string]struct{}) []map[string]any {
	items := make([]*artifactChainAggregate, 0, len(input))
	for _, entry := range input {
		if entry == nil || entry.pathCount == 0 {
			continue
		}
		if len(allowedCountries) > 0 {
			if _, ok := allowedCountries[strings.ToLower(strings.TrimSpace(entry.code))]; !ok {
				continue
			}
		}
		entry.refs = compactStringsPreserveOrder(entry.refs)
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			if items[i].code == items[j].code {
				return items[i].path < items[j].path
			}
			return items[i].code < items[j].code
		}
		return items[i].score > items[j].score
	})
	out := make([]map[string]any, 0, min(len(items), 12))
	for _, entry := range items[:min(len(items), 12)] {
		out = append(out, map[string]any{
			"countryCode": entry.code,
			"countryName": countryNameFromCode(entry.code),
			"path":        entry.path,
			"name":        strings.ReplaceAll(lastPathSegment(entry.path), "-", " "),
			"pathCount":   entry.pathCount,
			"riskScore":   round3(entry.score),
			"maxStress":   round3(entry.maxStress),
			"refs":        traceSlice(entry.refs, 6),
		})
	}
	return out
}

func artifactRiskScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	bindingDeficit := 1 - minFloat(maxFloat(artifact.BindingScore, 0), 1)
	recoveryComponent := minFloat(maxFloat(artifact.RecoveryLatencyHrs/96.0, 0), 1)
	stressComponent := minFloat(maxFloat(artifact.PathStressScore, 0), 1)
	return round3(stressComponent*1.4 + bindingDeficit*0.7 + recoveryComponent*0.5)
}

func artifactShoulderCountries(artifact *horizonmodels.MaterializedArtifact, primaryGeo string) []string {
	if artifact == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, ref := range artifactShoulderRefs(artifact) {
		code := countryCodeFromPath(ref)
		if code == "" || code == primaryGeo {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func artifactShoulderRefs(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	refs := make([]string, 0, 12)
	refs = append(refs,
		strings.TrimSpace(artifact.AnchorSupplier),
		strings.TrimSpace(artifact.AnchorCustomer),
	)
	if legs, err := horizonmodels.GetSupplyChainPathLegs(artifact); err == nil && len(legs) > 0 {
		for _, leg := range legs {
			refs = append(refs,
				strings.TrimSpace(leg.SupplierRef),
				strings.TrimSpace(leg.CustomerRef),
			)
		}
	}
	if len(compactStringsPreserveOrder(refs)) == 0 {
		refs = append(refs, artifact.NodeRefs...)
	}
	return compactStringsPreserveOrder(refs)
}

func recognitionPrimaryGeoCode(recognition map[string]any) string {
	for _, raw := range mapSlice(recognition["objectCandidates"]) {
		path := strings.TrimSpace(stringValue(raw, "path"))
		if code := countryCodeFromPath(path); code != "" {
			return code
		}
	}
	return ""
}

func countryCodeFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "loc", "fac", "org", "market", "gov":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	case "domain":
		for i := 1; i < len(parts)-1; i++ {
			if parts[i] == "geo" && len(strings.TrimSpace(parts[i+1])) == 2 {
				return strings.ToLower(strings.TrimSpace(parts[i+1]))
			}
		}
		return ""
	default:
		return ""
	}
}

func countryNameFromCode(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ae":
		return "UAE"
	case "cn":
		return "China"
	case "in":
		return "India"
	case "kz":
		return "Kazakhstan"
	case "tr":
		return "Turkey"
	case "be":
		return "Belgium"
	case "nl":
		return "Netherlands"
	case "eg":
		return "Egypt"
	case "qa":
		return "Qatar"
	case "ru":
		return "Russia"
	default:
		return strings.ToUpper(code)
	}
}

func mergeSimulationPreview(base, overlay map[string]any) map[string]any {
	if len(overlay) == 0 {
		return base
	}
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range overlay {
		if key == "metrics" {
			baseMetrics, _ := base["metrics"].(map[string]any)
			if baseMetrics == nil {
				baseMetrics = map[string]any{}
			}
			if overlayMetrics, ok := value.(map[string]any); ok {
				for mk, mv := range overlayMetrics {
					baseMetrics[mk] = mv
				}
			}
			base["metrics"] = baseMetrics
			continue
		}
		if _, exists := base[key]; !exists {
			base[key] = value
		}
	}
	return base
}

func traceSlice(values []string, limit int) []string {
	if len(values) == 0 || limit <= 0 {
		return nil
	}
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func lastPathSegment(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func (s *Service) resolveSeedFragmentIDs(ctx context.Context, tenantID string, signal *horizonmodels.SignalDescriptor) []string {
	if signal == nil {
		return nil
	}
	ids := make([]string, 0, len(signal.SeedPaths)+4)
	for _, path := range compactStringsPreserveOrder(signal.SeedPaths) {
		frag, err := s.fragments.GetByCanonicalPathForTenant(ctx, tenantID, path)
		if err == nil && frag != nil {
			ids = append(ids, frag.ID)
		}
	}
	if len(ids) == 0 && strings.TrimSpace(signal.LocationPath) != "" {
		frag, err := s.fragments.GetByCanonicalPathForTenant(ctx, tenantID, signal.LocationPath)
		if err == nil && frag != nil {
			ids = append(ids, frag.ID)
		}
	}
	return compactStrings(ids)
}

func (s *Service) publish(ctx context.Context, eventType, tenantID string, payload any) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher(ctx, eventType, tenantID, payload); err != nil {
		s.logger.Printf("[horizon.simulation] publish failed: %v", err)
	}
}

func executionFromRequest(req *horizonmodels.SimulationRunRequest) horizonmodels.SimulationExecutionConfig {
	return horizonmodels.SimulationExecutionConfig{
		WorldCount:  req.WorldCount,
		TickCount:   req.TickCount,
		TimeHorizon: req.TimeHorizon,
		Audience:    req.Audience,
		Mode:        req.Mode,
		DomainModes: req.DomainModes,
	}
}

func signalFromRequest(req *horizonmodels.SimulationRunRequest, fallbackScope []string, recognition map[string]any) *horizonmodels.SignalDescriptor {
	if req == nil {
		return nil
	}
	shock := horizonmodels.SimulationShock{}
	if req.Signal != nil {
		shock = *req.Signal
	}
	seedPaths := compactStringsPreserveOrder(append(append([]string{}, shock.SeedPaths...), req.SeedPaths...))
	if len(seedPaths) == 0 {
		for _, object := range mapSlice(recognition["objectCandidates"]) {
			path := stringValue(object, "path")
			switch objectKindFromPath(path) {
			case "org", "fac", "market", "product", "domain", "geo", "gov":
				seedPaths = append(seedPaths, path)
			}
			if len(seedPaths) >= 8 {
				break
			}
		}
	}
	if len(seedPaths) == 0 && len(fallbackScope) > 0 {
		seedPaths = compactStringsPreserveOrder(fallbackScope[:min(len(fallbackScope), 12)])
	}
	axis := strings.TrimSpace(shock.Axis)
	if axis == "" {
		axis = axisFromShockType(shock.Type)
	}
	if axis == "" {
		axis = strings.TrimSpace(stringValue(recognition, "primaryAxis"))
	}
	if axis == "" {
		axis = horizonmodels.AxisInfoUncertainty
	}
	polarity := firstNonEmpty(shock.Polarity, "risk")
	locationPath := strings.TrimSpace(shock.LocationPath)
	if locationPath == "" {
		for _, object := range mapSlice(recognition["objectCandidates"]) {
			path := stringValue(object, "path")
			if objectKindFromPath(path) == "geo" {
				locationPath = path
				break
			}
		}
	}
	actorPaths := compactStrings(append([]string{}, shock.ActorPaths...))
	if len(actorPaths) == 0 {
		for _, object := range mapSlice(recognition["objectCandidates"]) {
			path := stringValue(object, "path")
			switch objectKindFromPath(path) {
			case "org", "gov":
				actorPaths = append(actorPaths, path)
			}
			if len(actorPaths) >= 4 {
				break
			}
		}
	}
	now := time.Now().UTC()
	idMaterial := strings.Join([]string{
		axis,
		polarity,
		strings.TrimSpace(shock.Text),
		strings.TrimSpace(shock.Type),
		strings.Join(seedPaths, ","),
	}, "|")
	sum := sha256.Sum256([]byte(idMaterial))
	signal := &horizonmodels.SignalDescriptor{
		ID:           "sim-" + hex.EncodeToString(sum[:8]),
		CreatedAt:    now,
		Axis:         axis,
		Amplitude:    maxFloat(0.05, minFloat(shock.Magnitude, 1.0)),
		Polarity:     polarity,
		ActorPaths:   compactStrings(actorPaths),
		SeedPaths:    seedPaths,
		LocationPath: locationPath,
		Constraints: horizonmodels.SignalConstraints{
			TimeHorizon: firstNonEmpty(req.TimeHorizon, horizonmodels.SignalTimeHorizonShortTerm),
			Timeframe:   firstNonEmpty(req.TimeHorizon, horizonmodels.SignalTimeHorizonShortTerm),
			Modality:    horizonmodels.SignalModalityProjected,
			Scope:       "primary",
			Likelihood:  0.55,
			Impact:      maxFloat(0.05, minFloat(shock.Magnitude, 1.0)),
		},
		Confidence: 0.55,
		ObservedAt: now,
	}
	if signal.Amplitude == 0 {
		signal.Amplitude = 0.35
	}
	if signal.LocationPath == "" && len(seedPaths) == 0 {
		return nil
	}
	if err := signal.Validate(); err != nil {
		return nil
	}
	return signal
}

func axisFromShockType(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "price_shock", "price", "inflation", "cost":
		return horizonmodels.AxisFinancialSettlement
	case "policy", "regulatory", "sanction", "sanctions":
		return horizonmodels.AxisLegalCompliance
	case "supply_shock", "supply", "shortage":
		return horizonmodels.AxisPhysicalFlowSupply
	case "demand_shock", "demand":
		return horizonmodels.AxisPhysicalFlowDemand
	case "logistics", "route", "shipment":
		return horizonmodels.AxisLogisticsCapacity
	case "reputation", "brand", "trust":
		return horizonmodels.AxisInfoReputation
	case "war", "conflict", "crisis":
		return horizonmodels.AxisGeoConflict
	default:
		return ""
	}
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

type WorldRuntimeState struct {
	Seed               int64                           `json:"seed"`
	BaselineSignal     *horizonmodels.SignalDescriptor `json:"baselineSignal,omitempty"`
	BaselinePreview    map[string]any                  `json:"baselinePreview,omitempty"`
	ScopeDigest        string                          `json:"scopeDigest,omitempty"`
	PopulationDigest   string                          `json:"populationDigest,omitempty"`
	WorldBootstrapHash string                          `json:"worldBootstrapHash,omitempty"`
}

func DeterministicSeed(runID string, worldIndex int) int64 {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", strings.TrimSpace(runID), worldIndex)))
	return int64(binarySeed(sum[:8]))
}

func binarySeed(raw []byte) uint64 {
	var out uint64
	for _, b := range raw {
		out = (out << 8) | uint64(b)
	}
	return out
}

func SeededRand(runID string, worldIndex int) *rand.Rand {
	return rand.New(rand.NewSource(DeterministicSeed(runID, worldIndex)))
}

func MarshalDigest(parts ...any) string {
	hashParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			continue
		}
		raw, err := json.Marshal(part)
		if err != nil {
			continue
		}
		hashParts = append(hashParts, string(raw))
	}
	sort.Strings(hashParts)
	sum := sha256.Sum256([]byte(strings.Join(hashParts, "\n")))
	return hex.EncodeToString(sum[:])
}

func WorldSummary(world *horizonmodels.SimulationWorld) map[string]any {
	if world == nil {
		return nil
	}
	messageCount := len(world.MessageBus)
	protocolCount := len(world.ProtocolTraces)
	transactionCount := len(world.Transactions)
	if messageCount == 0 || protocolCount == 0 || transactionCount == 0 {
		for _, metric := range world.StepMetrics {
			messageCount += worldMetricInt(metric, "messages")
			protocolCount += worldMetricInt(metric, "protocolEvents")
			transactionCount += worldMetricInt(metric, "transactionChanges")
		}
	}
	return map[string]any{
		"id":                 world.ID,
		"worldIndex":         world.WorldIndex,
		"status":             world.Status,
		"randomSeed":         world.RandomSeed,
		"stepCount":          world.StepCount,
		"bootstrapDigest":    world.BootstrapDigest,
		"populationDigest":   world.PopulationDigest,
		"outcomeVector":      world.OutcomeVector,
		"dominantMechanisms": world.DominantMechanisms,
		"uncertainty":        world.Uncertainty,
		"messageCount":       messageCount,
		"protocolCount":      protocolCount,
		"transactionCount":   transactionCount,
	}
}

func FactIDs(facts []*coremodels.OntologyFact) []string {
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		if fact != nil {
			ids = append(ids, strings.TrimSpace(fact.ID))
		}
	}
	return compactStrings(ids)
}

func sanitizeSimulationDocumentMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return input
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return map[string]any{
			"status": "preview_sanitization_failed",
			"error":  err.Error(),
		}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{
			"status": "preview_sanitization_failed",
			"error":  err.Error(),
		}
	}
	return sanitizeSimulationMap(out)
}

func sanitizeSimulationMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return input
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = sanitizeSimulationValue(value)
	}
	return out
}

func sanitizeSimulationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeSimulationMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			sanitized := sanitizeSimulationValue(item)
			if nested, ok := sanitized.([]any); ok {
				out = append(out, map[string]any{"items": nested})
				continue
			}
			out = append(out, sanitized)
		}
		return out
	default:
		return typed
	}
}

func worldMetricInt(metric map[string]any, key string) int {
	if metric == nil {
		return 0
	}
	switch typed := metric[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
