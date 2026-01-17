package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func isLegacyOrgPath(cp string) bool {
	cp = strings.TrimSpace(cp)
	if !strings.HasPrefix(cp, "/org/") {
		return false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	// Legacy: /org/{slug} (2 segments total)
	return len(parts) == 2 && parts[0] == "org" && strings.TrimSpace(parts[1]) != ""
}

func (e *ScoutEngine) applyDimensionWavesFromAppliedFacts(
	ctx context.Context,
	target *models.HorizonFragment,
	facts []*coreModels.OntologyFact,
	appliedFactIDs []string,
) error {
	if e == nil || target == nil || target.ID == "" || e.fragmentsRepo == nil || e.edgesRepo == nil {
		return nil
	}
	if len(facts) == 0 || len(appliedFactIDs) == 0 {
		return nil
	}

	applied := map[string]struct{}{}
	for _, id := range appliedFactIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			applied[id] = struct{}{}
		}
	}
	if len(applied) == 0 {
		return nil
	}

	now := time.Now().UTC()
	dimensionFragmentIDsToPropagate := map[string]struct{}{}

	ensureFragment := func(canonicalPath string, ftype models.FragmentType, label string, entityPath string, sectorPath string) (*models.HorizonFragment, error) {
		canonicalPath = strings.TrimSpace(canonicalPath)
		if canonicalPath == "" || canonicalPath == "/" {
			return nil, fmt.Errorf("empty canonicalPath")
		}
		// Best-effort: normalize/strictify canonicalPath early so we don't create duplicates like /loc/uae vs /loc/ae/na/na.
		if e.canonicalPathResolver != nil && ftype != "" {
			if resolved, rerr := e.canonicalPathResolver.Resolve(ctx, canonicalPath, ftype); rerr == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
				canonicalPath = strings.TrimSpace(resolved.CanonicalPath)
				if resolved.ExistingFragment != nil && strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
					return resolved.ExistingFragment, nil
				}
			}
		}
		ex, err := e.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
		if err == nil && ex != nil && ex.ID != "" {
			return ex, nil
		}
		frag := &models.HorizonFragment{
			TenantID:      "global",
			Version:       1,
			CreatedAt:     now,
			UpdatedAt:     now,
			CreatedBy:     "system",
			UpdatedBy:     "system",
			Labels:        map[string]string{"en": strings.TrimSpace(label)},
			Status:        "active",
			CanonicalPath: canonicalPath,
			Type:          ftype,
			EntityPath:    entityPath,
			SectorPath:    sectorPath,
			State:         map[string]models.MetricValue{},
			Provenance:    models.Provenance{Method: "taxonomy_dimension_seed", SourceIDs: []string{}, SourceRefs: []models.SourceRef{}},
			Metadata:      models.FragmentMetadata{CreatedAt: now, LastUpdatedAt: now, LastVerifiedAt: now, LastFactsSeenAt: now, LastFactsAppliedAt: now, EpistemicStatus: models.EpistemicStatusVerified},
			Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
		}
		id, err := e.fragmentsRepo.Create(ctx, frag)
		if err != nil {
			return nil, err
		}
		frag.ID = id
		return frag, nil
	}

	applyMetricUpdates := func(fragment *models.HorizonFragment, fact *coreModels.OntologyFact) bool {
		if fragment == nil || fact == nil || len(fact.MetricUpdates) == 0 {
			return false
		}
		if fragment.State == nil {
			fragment.State = map[string]models.MetricValue{}
		}
		dedupStringsLocal := func(in []string) []string {
			seen := map[string]struct{}{}
			out := make([]string, 0, len(in))
			for _, s := range in {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if _, ok := seen[s]; ok {
					continue
				}
				seen[s] = struct{}{}
				out = append(out, s)
			}
			return out
		}
		dedupHorizonRefsLocal := func(in []coreModels.SourceRef) []models.SourceRef {
			seen := map[string]struct{}{}
			out := make([]models.SourceRef, 0, len(in))
			for _, r := range in {
				sid := strings.TrimSpace(r.SourceID)
				st := strings.TrimSpace(r.SourceType)
				if sid == "" && st == "" {
					continue
				}
				key := st + "|" + sid
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, models.SourceRef{SourceID: sid, SourceType: st})
			}
			return out
		}
		changed := false
		for _, mu := range fact.MetricUpdates {
			name := strings.TrimSpace(mu.MetricName)
			if name == "" {
				continue
			}
			obs := now
			if mu.ObservedAt != nil {
				obs = mu.ObservedAt.UTC()
			}
			conf := mu.Confidence
			if conf <= 0 {
				conf = fact.Confidence
			}
			prev, ok := fragment.State[name]
			next := models.MetricValue{
				Value:      mu.Value,
				Unit:       mu.Unit,
				Confidence: conf,
				ObservedAt: obs,
				Provenance: models.Provenance{
					SourceIDs:  dedupStringsLocal(fact.SourceIDs),
					SourceRefs: dedupHorizonRefsLocal(fact.SourceRefs),
					ObservedAt: &obs,
					Method:     "dimension_fact_fastpath",
				},
			}
			if !ok || prev.Value != next.Value || (prev.Unit == nil) != (next.Unit == nil) {
				fragment.State[name] = next
				changed = true
			}
		}
		return changed
	}

	ensureInfluenceEdge := func(sourceID, targetID string, confidence float64, qualifier map[string]interface{}) {
		if sourceID == "" || targetID == "" || sourceID == targetID {
			return
		}
		edgeType := models.DependencyTypeInfluences
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, edgeType); err == nil {
			return
		}
		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sourceID,
			TargetFragmentID: targetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality: func(x float64) float64 {
					if x < 0 {
						return 0
					}
					if x > 1 {
						return 1
					}
					return x
				}(confidence),
				Amplification: 1.0,
				LatencyHours:  1,
			},
			Qualifiers: qualifier,
			Provenance: models.Provenance{
				Method: "dimension_edge_from_fact",
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     now,
				LastUpdatedAt: now,
			},
		}
		if _, err := e.edgesRepo.Create(ctx, edge); err != nil {
			// Fail-open: dimension edges are not critical for core graph materialization, but must not be silent.
			fmt.Printf("[ScoutEngine] Failed to create dimension edge: src=%s dst=%s type=%s err=%v\n",
				sourceID, targetID, string(edgeType), err)
		}
	}

	for _, f := range facts {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		if _, ok := applied[strings.TrimSpace(f.ID)]; !ok {
			continue
		}

		// 1) sphere dimensions
		if len(f.SpherePaths) > 0 {
			for _, sp := range f.SpherePaths {
				sp = strings.TrimSpace(sp)
				if sp == "" {
					continue
				}
				sfrag, err := ensureFragment(sp, models.FragmentTypeSphere, sp, "/Influence/Sphere", "/Influence")
				if err != nil || sfrag == nil {
					if err != nil {
						fmt.Printf("[ScoutEngine] Failed to ensure sphere fragment %s: %v\n", sp, err)
					}
					continue
				}
				ensureInfluenceEdge(sfrag.ID, target.ID, f.Confidence, map[string]interface{}{"dimension": "sphere", "path": sp, "factId": f.ID})
				// Metric updates are optional; sphere linkage should exist even without metrics.
				if len(f.MetricUpdates) > 0 && applyMetricUpdates(sfrag, f) {
					if err := e.fragmentsRepo.Update(ctx, sfrag); err != nil {
						fmt.Printf("[ScoutEngine] Failed to update sphere dimension fragment after metric updates: id=%s path=%s err=%v\n",
							strings.TrimSpace(sfrag.ID), strings.TrimSpace(sfrag.CanonicalPath), err)
					}
					dimensionFragmentIDsToPropagate[sfrag.ID] = struct{}{}
				}
			}
		}

		// 2) geo dimension (locationPath)
		if f.LocationPath != nil {
			lp := strings.TrimSpace(*f.LocationPath)
			if lp != "" {
				gfrag, err := ensureFragment(lp, models.FragmentTypeGeoRegion, lp, "/Geo/Region", "/Geography")
				if err == nil && gfrag != nil {
					ensureInfluenceEdge(gfrag.ID, target.ID, f.Confidence, map[string]interface{}{"dimension": "geo", "path": lp, "factId": f.ID})
					// Metric updates are optional; geo linkage should exist even without metrics.
					if len(f.MetricUpdates) > 0 && applyMetricUpdates(gfrag, f) {
						if err := e.fragmentsRepo.Update(ctx, gfrag); err != nil {
							fmt.Printf("[ScoutEngine] Failed to update geo dimension fragment after metric updates: id=%s path=%s err=%v\n",
								strings.TrimSpace(gfrag.ID), strings.TrimSpace(gfrag.CanonicalPath), err)
						}
						dimensionFragmentIDsToPropagate[gfrag.ID] = struct{}{}
					}
				}
			}
		}
	}

	// Trigger propagation for dimension fragments (non-blocking).
	if e.propagationTrigger != nil {
		for fid := range dimensionFragmentIDsToPropagate {
			fid := fid
			go func() {
				propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				if err := e.propagationTrigger(propCtx, fid); err != nil {
					fmt.Printf("[ScoutEngine] propagationTrigger failed for dimension fragment: fragmentId=%s err=%v\n", fid, err)
				}
			}()
		}
	}

	return nil
}

// ScoutEngine основной движок Scout для сбора и верификации данных
type ScoutEngine struct {
	planner                 *ScoutPlanner
	executor                *SourceExecutor
	scoutAgent              *agents.ScoutAgent
	criticAgent             *agents.CriticAgent
	resolverAgent           *agents.ResolverAgent
	researchAgent           *agents.ResearchAgent // New
	frameBuilder            *FrameBuilder
	workflow                *ScoutWorkflow
	fragmentsRepo           *repo.HorizonFragmentsRepository
	framesRepo              *repo.FramesRepository
	edgesRepo               *repo.DependencyEdgesRepository
	scoutingJobsRepo        *repo.ScoutingJobsRepository
	cascadeVisitedRepo      *repo.CascadeVisitedRepository
	lensesRepo              *repo.LensesRepository // Optional: for updating lens ViewConfig on assembly completion
	realtime                *realtime.Publisher
	taxonomyClassifier      *TaxonomyClassifier
	aiExecutor              *ai.AITaskExecutor
	wikidataClient          coreontology.WikidataClientInterface // Optional: shared for enrichment/dedup (no search by default)
	geonamesClient          coreontology.GeoNamesClientInterface // Optional: deterministic geo normalization/enrichment
	spendLedger             *repo.HorizonSpendLedger
	budgetPolicy            BudgetPolicy
	propagationTrigger      func(ctx context.Context, fragmentID string) error
	signalSynthesizer       *signals.Synthesizer              // Optional: for signal synthesis from facts
	factsRepo               *corerepo.OntologyFactsRepository // Optional: for saving synthesized signals
	factEmitter             *coreontology.FactEmitter         // Optional: unified fact emission (write + feed)
	geoHierarchyBuilder     *GeoHierarchyBuilder              // Auto-creates geo hierarchy when cities are created
	orgDeduplicator         *OrgDeduplicator                  // Deduplicates organizations by Wikidata/shortName
	personDeduplicator      *PersonDeduplicator               // Deduplicates persons by name/phonetic match
	preCreateValidator      *PreCreateValidator               // Validates fragments before creation
	orgHierarchyBuilder     *OrgHierarchyBuilder              // Auto-creates org parent hierarchy
	marketHierarchyBuilder  *MarketHierarchyBuilder           // Auto-creates market hierarchy (segment-first when possible)
	govHierarchyBuilder     *GovHierarchyBuilder              // Best-effort gov type inference + admin hierarchy helpers
	productDeduplicator     *ProductDeduplicator              // Deduplicates products by Wikidata/CAS/formula
	productHierarchyBuilder *ProductHierarchyBuilder          // Auto-creates product value chain hierarchy
	validationPolicy        *policy.PolicyExecutor            // Optional: unified validation policy for edges
	canonizationPolicy      *policy.PolicyExecutor            // Optional: unified canonization policy for paths/types
	canonicalPathResolver   *CanonicalPathResolver            // Unified canonicalization and deduplication service
	missingInfoStatusRepo   *repo.MissingInfoStatusRepository // Tracks missingInfo attempts to prevent infinite loops
}

// bestEffortRewriteLensScope replaces alias canonical paths in lens.Scope with their canonical equivalents.
// This helps the UI/graph loader, which relies on lens.Scope, not on transient job state.
func (e *ScoutEngine) bestEffortRewriteLensScope(ctx context.Context, tenantID, lensID, fromPath, toPath string) {
	if e == nil || e.lensesRepo == nil {
		return
	}
	lensID = strings.TrimSpace(lensID)
	tenantID = strings.TrimSpace(tenantID)
	fromPath = strings.TrimSpace(fromPath)
	toPath = strings.TrimSpace(toPath)
	if lensID == "" || fromPath == "" || toPath == "" || fromPath == toPath {
		return
	}
	lens, err := e.lensesRepo.Get(ctx, lensID)
	if err != nil || lens == nil {
		return
	}
	// Tenant safety: never cross-write.
	if tenantID != "" && strings.TrimSpace(lens.TenantID) != "" && strings.TrimSpace(lens.TenantID) != tenantID {
		return
	}

	changed := false
	for i, s := range lens.Scope {
		if strings.TrimSpace(s) == fromPath {
			lens.Scope[i] = toPath
			changed = true
		}
	}
	if !changed {
		return
	}

	// Best-effort: keep cache consistent.
	if lens.ScopeFragments != nil {
		if fid, ok := lens.ScopeFragments[fromPath]; ok {
			delete(lens.ScopeFragments, fromPath)
			// Only set if not already present.
			if _, ok2 := lens.ScopeFragments[toPath]; !ok2 && strings.TrimSpace(fid) != "" {
				lens.ScopeFragments[toPath] = fid
			}
		}
	}

	// De-dupe scope after rewrite.
	seen := map[string]struct{}{}
	out := make([]string, 0, len(lens.Scope))
	for _, s := range lens.Scope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	lens.Scope = out

	_ = e.lensesRepo.Update(ctx, lens)
}

// WithPropagationTrigger устанавливает триггер для запуска распространения изменений
func (e *ScoutEngine) WithPropagationTrigger(trigger func(ctx context.Context, fragmentID string) error) *ScoutEngine {
	e.propagationTrigger = trigger
	return e
}

// WithSpendLedger wires spend attribution ledger used for budget enforcement (optional).
func (e *ScoutEngine) WithSpendLedger(ledger *repo.HorizonSpendLedger) *ScoutEngine {
	e.spendLedger = ledger
	return e
}

// WithCascadeVisitedRepo wires cascade-level visited set storage (optional).
// Used to prevent cycles / avalanche fanout inside a single cascade (especially supply_chain mode).
func (e *ScoutEngine) WithCascadeVisitedRepo(r *repo.CascadeVisitedRepository) *ScoutEngine {
	e.cascadeVisitedRepo = r
	return e
}

func (e *ScoutEngine) WithBudgetPolicy(policy BudgetPolicy) *ScoutEngine {
	e.budgetPolicy = policy
	return e
}

// WithFactEmitter wires a shared fact emitter for producing ontology facts from Scout results.
func (e *ScoutEngine) WithFactEmitter(emitter *coreontology.FactEmitter) *ScoutEngine {
	e.factEmitter = emitter
	return e
}

// WithValidationPolicy sets the validation policy executor for edge validation.
func (e *ScoutEngine) WithValidationPolicy(executor *policy.PolicyExecutor) *ScoutEngine {
	e.validationPolicy = executor
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithValidationPolicy(executor)
	}
	return e
}

// WithCanonizationPolicy sets the canonization policy executor for path/type normalization.
func (e *ScoutEngine) WithCanonizationPolicy(executor *policy.PolicyExecutor) *ScoutEngine {
	e.canonizationPolicy = executor
	return e
}

// WithCanonicalPathResolver sets the canonical path resolver for early canonicalization and deduplication.
func (e *ScoutEngine) WithCanonicalPathResolver(resolver *CanonicalPathResolver) *ScoutEngine {
	e.canonicalPathResolver = resolver
	return e
}

// CanonicalPathResolver returns the resolver wired into this engine (may be nil).
func (e *ScoutEngine) CanonicalPathResolver() *CanonicalPathResolver {
	if e == nil {
		return nil
	}
	return e.canonicalPathResolver
}

// WithLensesRepository sets the lenses repository for updating lens ViewConfig on assembly completion.
func (e *ScoutEngine) WithLensesRepository(lensesRepo *repo.LensesRepository) *ScoutEngine {
	e.lensesRepo = lensesRepo
	return e
}

// OrgDeduplicator returns the OrgDeduplicator instance used by this engine.
// This allows other subsystems (e.g. CanonicalPathResolver wired in main) to reuse the same dedup logic.
func (e *ScoutEngine) OrgDeduplicator() *OrgDeduplicator {
	if e == nil {
		return nil
	}
	return e.orgDeduplicator
}

// NewScoutEngine создает новый Scout Engine
func NewScoutEngine(
	planner *ScoutPlanner,
	executor *SourceExecutor,
	scoutAgent *agents.ScoutAgent,
	criticAgent *agents.CriticAgent,
	resolverAgent *agents.ResolverAgent,
	researchAgent *agents.ResearchAgent, // New
	frameBuilder *FrameBuilder,
	workflow *ScoutWorkflow,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	framesRepo *repo.FramesRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	scoutingJobsRepo *repo.ScoutingJobsRepository,
	realtime *realtime.Publisher,
	aiExecutor *ai.AITaskExecutor,
) *ScoutEngine {
	geoBuilder := NewGeoHierarchyBuilder(fragmentsRepo, edgesRepo)
	orgDedup := NewOrgDeduplicator(fragmentsRepo, nil)       // WikidataClient set via WithWikidataClient
	personDedup := NewPersonDeduplicator(fragmentsRepo, nil) // WikidataClient set via WithWikidataClient
	orgHierarchy := NewOrgHierarchyBuilder(fragmentsRepo, edgesRepo)
	marketHierarchy := NewMarketHierarchyBuilder(fragmentsRepo, edgesRepo)
	govHierarchy := NewGovHierarchyBuilder(fragmentsRepo, edgesRepo)
	productDedup := NewProductDeduplicator(fragmentsRepo, nil)                    // WikidataClient set via WithWikidataClient
	productHierarchy := NewProductHierarchyBuilder(fragmentsRepo, edgesRepo, nil) // WikidataClient set via WithWikidataClient

	engine := &ScoutEngine{
		planner:                 planner,
		executor:                executor,
		scoutAgent:              scoutAgent,
		criticAgent:             criticAgent,
		resolverAgent:           resolverAgent,
		researchAgent:           researchAgent,
		frameBuilder:            frameBuilder,
		workflow:                workflow,
		fragmentsRepo:           fragmentsRepo,
		framesRepo:              framesRepo,
		edgesRepo:               edgesRepo,
		scoutingJobsRepo:        scoutingJobsRepo,
		cascadeVisitedRepo:      nil,
		realtime:                realtime,
		taxonomyClassifier:      NewTaxonomyClassifier(aiExecutor),
		aiExecutor:              aiExecutor,
		spendLedger:             nil,
		budgetPolicy:            BudgetPolicyFromEnv(),
		geoHierarchyBuilder:     geoBuilder,
		orgDeduplicator:         orgDedup,
		personDeduplicator:      personDedup,
		orgHierarchyBuilder:     orgHierarchy,
		marketHierarchyBuilder:  marketHierarchy,
		govHierarchyBuilder:     govHierarchy,
		productDeduplicator:     productDedup,
		productHierarchyBuilder: productHierarchy,
	}
	engine.preCreateValidator = NewPreCreateValidator(geoBuilder, orgDedup, productDedup).WithPersonDedup(personDedup)
	return engine
}

// WithWikidataClient sets the Wikidata client for organization, person, and product deduplication
func (e *ScoutEngine) WithWikidataClient(client coreontology.WikidataClientInterface) *ScoutEngine {
	e.wikidataClient = client
	if e.orgDeduplicator != nil {
		e.orgDeduplicator.wikidataClient = client
	} else {
		e.orgDeduplicator = NewOrgDeduplicator(e.fragmentsRepo, client)
	}
	if e.personDeduplicator != nil {
		e.personDeduplicator.wikidataClient = client
	} else {
		e.personDeduplicator = NewPersonDeduplicator(e.fragmentsRepo, client)
	}
	if e.productDeduplicator != nil {
		e.productDeduplicator.wikidataClient = client
		if e.productDeduplicator.extractor != nil {
			e.productDeduplicator.extractor.wikidataClient = client
		}
	} else {
		e.productDeduplicator = NewProductDeduplicator(e.fragmentsRepo, client)
	}
	if e.productHierarchyBuilder != nil {
		e.productHierarchyBuilder.wikidataClient = client
		if e.productHierarchyBuilder.extractor != nil {
			e.productHierarchyBuilder.extractor.wikidataClient = client
		}
		// Keep producer/org dedup in sync as well (used for Wikidata manufacturer enrichment).
		if e.productHierarchyBuilder.orgDeduplicator != nil {
			e.productHierarchyBuilder.orgDeduplicator.wikidataClient = client
		}
	} else {
		e.productHierarchyBuilder = NewProductHierarchyBuilder(e.fragmentsRepo, e.edgesRepo, client)
	}
	// Also update canonicalPathResolver if it exists
	if e.canonicalPathResolver != nil && e.canonicalPathResolver.orgDeduplicator != nil {
		e.canonicalPathResolver.orgDeduplicator.wikidataClient = client
	}
	// Also update PreCreateValidator for conservative enrichment (QID-known only).
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithWikidataClient(client)
	}
	return e
}

// WithGeoNamesClient sets the GeoNames client for deterministic geo operations.
func (e *ScoutEngine) WithGeoNamesClient(client coreontology.GeoNamesClientInterface) *ScoutEngine {
	e.geonamesClient = client
	if e.preCreateValidator != nil {
		e.preCreateValidator.WithGeoNamesClient(client)
	}
	return e
}

func (e *ScoutEngine) publish(ctx context.Context, et realtime.EventType, data map[string]interface{}) {
	if e == nil || e.realtime == nil {
		return
	}
	rc, ok := rtctx.Get(ctx)
	tenantID := ""
	if ok {
		tenantID = strings.TrimSpace(rc.TenantID)
		if v := strings.TrimSpace(rc.ProjectID); v != "" {
			data["projectId"] = v
		}
		if v := strings.TrimSpace(rc.LensID); v != "" {
			data["lensId"] = v
		}
		if v := strings.TrimSpace(rc.CorrelationID); v != "" {
			data["correlationId"] = v
		}
		if v := strings.TrimSpace(rc.SessionID); v != "" {
			data["sessionId"] = v
		}
	}
	if tenantID == "" {
		if v, ok2 := data["tenantId"].(string); ok2 {
			tenantID = strings.TrimSpace(v)
		}
	}
	if tenantID == "" {
		return
	}
	data["tenantId"] = tenantID
	data["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	_ = e.realtime.PublishJSON(ctx, et, tenantID, data)
}

func (e *ScoutEngine) attachRealtimeContext(ctx context.Context, job *models.ScoutingJob, tenantID, projectID, lensID string) context.Context {
	correlationID := ""
	sessionID := ""
	if job != nil && job.State != nil {
		if v, ok := job.State["correlationId"].(string); ok {
			correlationID = strings.TrimSpace(v)
		}
		if v, ok := job.State["sessionId"].(string); ok {
			sessionID = strings.TrimSpace(v)
		}
	}
	return rtctx.With(ctx, rtctx.Context{
		TenantID:      strings.TrimSpace(tenantID),
		ProjectID:     strings.TrimSpace(projectID),
		LensID:        strings.TrimSpace(lensID),
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})
}

func (e *ScoutEngine) transition(ctx context.Context, job *models.ScoutingJob, newState models.ScoutingJobState, progress float64, hint string) error {
	if e == nil || e.workflow == nil || job == nil {
		return nil
	}
	if err := e.workflow.TransitionState(ctx, job.ID, newState); err != nil {
		return err
	}
	// Update local job snapshot (best-effort)
	job.CurrentState = newState
	// IMPORTANT: keep AgentJob.Status in sync with the transitioned state.
	// Otherwise subsequent best-effort Update() calls from ScoutEngine (which persist job.State, attempts, etc.)
	// can overwrite the terminal status back to "pending"/"running", causing CreateDedup() to incorrectly
	// treat old jobs as active forever and to reuse jobId (created=false).
	switch newState {
	case models.ScoutingJobStateVerified:
		job.Status = coreModels.AgentJobStatusCompleted
	case models.ScoutingJobStateRejected, models.ScoutingJobStateFailed:
		job.Status = coreModels.AgentJobStatusFailed
	default:
		// Any non-terminal scout state counts as running from AgentJob perspective.
		// (pending is only used at creation time).
		job.Status = coreModels.AgentJobStatusRunning
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State["currentState"] = string(newState)
	evt := map[string]interface{}{
		"tenantId":  job.TenantID,
		"projectId": job.ProjectID,
		"lensId":    strings.TrimSpace(job.LensID),
		"jobId":     job.ID,
		"state":     string(newState),
		"progress":  progress,
	}
	if hint != "" {
		evt["hint"] = hint
	}
	if strings.TrimSpace(job.TargetCanonicalPath) != "" {
		evt["targetCanonicalPath"] = strings.TrimSpace(job.TargetCanonicalPath)
	}
	e.publish(ctx, realtime.EventType("horizon.scout.job.state"), evt)
	return nil
}

func (e *ScoutEngine) logLine(ctx context.Context, job *models.ScoutingJob, level string, line string) {
	if e == nil || job == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if level == "" {
		level = "info"
	}
	e.publish(ctx, realtime.EventType("horizon.scout.job.log"), map[string]interface{}{
		"tenantId":            job.TenantID,
		"projectId":           job.ProjectID,
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               job.ID,
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"level":               level,
		"line":                line,
	})
}

// ExecuteScoutingJob выполняет полный цикл ScoutingJob
func (e *ScoutEngine) ExecuteScoutingJob(ctx context.Context, job *models.ScoutingJob) error {
	if job == nil {
		return fmt.Errorf("job is nil")
	}
	trace.Log(ctx, "scout_exec", "scout.job.start", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"status":              strings.TrimSpace(string(job.Status)),
		"currentState":        string(job.CurrentState),
		"sourceIdsN":          len(job.SourceIDs),
	})

	// IDEMPOTENCY CHECK: Skip already-completed jobs (handles Cloud Tasks retries gracefully).
	// This prevents duplicate work when Cloud Tasks retries due to timeout/transient errors.
	if job.CurrentState == models.ScoutingJobStateVerified ||
		job.CurrentState == models.ScoutingJobStateFailed ||
		job.Status == coreModels.AgentJobStatusCompleted ||
		job.Status == coreModels.AgentJobStatusFailed {
		trace.Log(ctx, "scout_exec", "scout.job.idempotent_skip", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"status":              strings.TrimSpace(string(job.Status)),
			"currentState":        string(job.CurrentState),
		})
		fmt.Printf("[ScoutEngine] Job %s already terminal (CurrentState=%s, Status=%s). Skipping (idempotent).\n",
			job.ID, job.CurrentState, job.Status)
		return nil // Success: already processed
	}

	// CIRCUIT BREAKER: prevent infinite retarget/alias loops within a single job.
	// If the same canonicalPath is processed repeatedly (e.g., due to alias/dedup oscillation),
	// hard-stop this branch to avoid runaway Cloud Tasks retries and repeated research.
	{
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		cp := strings.TrimSpace(job.TargetCanonicalPath)
		if cp != "" {
			// Keep per-path attempt counters inside job.State to survive Cloud Tasks retries.
			// Shape: state["canonicalPathAttempts"] = map[string]int
			attempts := map[string]int{}
			if raw, ok := job.State["canonicalPathAttempts"]; ok {
				switch t := raw.(type) {
				case map[string]int:
					attempts = t
				case map[string]interface{}:
					for k, v := range t {
						k = strings.TrimSpace(k)
						if k == "" {
							continue
						}
						// Firestore may decode ints as float64/int64/string.
						n := 0
						switch vv := v.(type) {
						case int:
							n = vv
						case int32:
							n = int(vv)
						case int64:
							n = int(vv)
						case float64:
							n = int(vv)
						case float32:
							n = int(vv)
						case string:
							n = parseStateInt(vv, 0)
						default:
							n = parseStateInt(fmt.Sprintf("%v", vv), 0)
						}
						attempts[k] = n
					}
				}
			}
			attempts[cp] = attempts[cp] + 1
			job.State["canonicalPathAttempts"] = attempts
			if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
				fmt.Printf("[ScoutEngine] Failed to persist circuit breaker state (best-effort): jobId=%s target=%s err=%v\n",
					strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			}
			trace.Log(ctx, "scout_exec", "scout.job.circuit_breaker.tick", map[string]any{
				"tenantId":      strings.TrimSpace(job.TenantID),
				"projectId":     strings.TrimSpace(job.ProjectID),
				"lensId":        strings.TrimSpace(job.LensID),
				"jobId":         strings.TrimSpace(job.ID),
				"canonicalPath": cp,
				"attempts":      attempts[cp],
			})

			if attempts[cp] >= 5 {
				trace.Log(ctx, "scout_exec", "scout.job.circuit_breaker.trip", map[string]any{
					"tenantId":      strings.TrimSpace(job.TenantID),
					"projectId":     strings.TrimSpace(job.ProjectID),
					"lensId":        strings.TrimSpace(job.LensID),
					"jobId":         strings.TrimSpace(job.ID),
					"canonicalPath": cp,
					"attempts":      attempts[cp],
				})
				fmt.Printf("[ScoutEngine] Circuit breaker tripped: job=%s canonicalPath=%s attempts=%d. Hard-stopping.\n",
					strings.TrimSpace(job.ID), cp, attempts[cp])
				if err := e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "circuit_breaker"); err != nil {
					fmt.Printf("[ScoutEngine] Failed to transition job to FAILED on circuit breaker (best-effort): jobId=%s target=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
				}
				e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
					"tenantId":      job.TenantID,
					"projectId":     job.ProjectID,
					"lensId":        strings.TrimSpace(job.LensID),
					"jobId":         job.ID,
					"canonicalPath": cp,
					"hint":          "circuit_breaker",
				})
				// Check if all jobs are completed
				e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, job.ProjectID, strings.TrimSpace(job.LensID))
				return nil // ACK: stop Cloud Tasks retries for this branch
			}
		}
	}

	tenantID := job.TenantID
	projectID := job.ProjectID

	// Optional task focus modules (specialties) requested by UI/lens.
	// These are stored in job.State to keep schemas stable.
	specialties := []string(nil)
	if job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			specialties = taxonomy.NormalizeSpecialties(parseStateStringSlice(v))
		}
	}

	// Supply chain context (optional): when supply_chain specialty is enabled, we pass
	// already-discovered chain nodes/edges + gaps to the LLM to keep it focused and incremental.
	chainContextForPrompt := ""
	if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtySupplyChain) && job.State != nil {
		if v, ok := job.State["chainContext"]; ok {
			if cc, _ := ParseChainContext(v); cc != nil {
				chainContextForPrompt = cc.ToPromptContext()
			}
		}
	}

	// Attach telemetry context so LLM usage metrics get attributed even if individual agents
	// don't fill task.TenantID/UserID fields.
	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" && job.State != nil {
		if v, ok := job.State["lensId"].(string); ok {
			lensID = strings.TrimSpace(v)
		}
	}
	cascadeID := ""
	if job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); ok {
			cascadeID = strings.TrimSpace(v)
		}
	}
	if cascadeID == "" {
		cascadeID = strings.TrimSpace(job.ID)
	}
	// Best-effort: persist cascadeRootJobId back into job state once.
	if job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); !ok || strings.TrimSpace(v) == "" {
			job.State["cascadeRootJobId"] = cascadeID
			_ = e.scoutingJobsRepo.Update(ctx, job)
		}
	}
	ctx = ai.WithUsageContext(ctx, tenantID, "SYSTEM", projectID, "horizon.scout", lensID, cascadeID)
	ctx = e.attachRealtimeContext(ctx, job, tenantID, projectID, lensID)
	trace.Log(ctx, "scout_exec", "scout.job.context", map[string]any{
		"tenantId":            strings.TrimSpace(tenantID),
		"projectId":           strings.TrimSpace(projectID),
		"lensId":              strings.TrimSpace(lensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"cascadeRootJobId":    cascadeID,
	})

	// EARLY CANONICALIZATION AND DEDUPLICATION: Resolve canonical path before any processing
	// This ensures we operate on canonicalized and deduplicated paths throughout the pipeline
	var resolvedPath *ResolveResult
	var effectiveCanonicalPath string
	if e.canonicalPathResolver != nil {
		fragmentType := InferFragmentTypeFromCanonicalPath(job.TargetCanonicalPath)

		resolved, err := e.canonicalPathResolver.Resolve(ctx, job.TargetCanonicalPath, fragmentType)
		if err == nil && resolved != nil {
			resolvedPath = resolved
			effectiveCanonicalPath = resolved.CanonicalPath

			// If path was changed (canonicalized or deduplicated), update job
			if effectiveCanonicalPath != strings.TrimSpace(job.TargetCanonicalPath) {
				oldPath := strings.TrimSpace(job.TargetCanonicalPath)
				fmt.Printf("[ScoutEngine] Early canonicalization/dedup: %s -> %s\n", oldPath, effectiveCanonicalPath)
				trace.Log(ctx, "scout_exec", "scout.early_canonicalization", map[string]any{
					"tenantId":           strings.TrimSpace(job.TenantID),
					"projectId":          strings.TrimSpace(job.ProjectID),
					"lensId":             strings.TrimSpace(lensID),
					"jobId":              strings.TrimSpace(job.ID),
					"fromCanonicalPath":  oldPath,
					"toCanonicalPath":    effectiveCanonicalPath,
					"foundExisting":      resolved.ExistingFragment != nil,
					"normalizationSteps": resolved.NormalizationSteps,
				})

				// Update job with canonicalized path
				job.TargetCanonicalPath = effectiveCanonicalPath
				if job.State == nil {
					job.State = make(map[string]interface{})
				}
				if _, ok := job.State["aliasCanonicalPath"]; !ok && oldPath != "" {
					job.State["aliasCanonicalPath"] = oldPath
				}
				job.State["targetCanonicalPath"] = effectiveCanonicalPath
				_ = e.scoutingJobsRepo.Update(ctx, job)
				// Best-effort: update lens scope so UI/graph doesn't keep alias paths.
				e.bestEffortRewriteLensScope(ctx, tenantID, lensID, oldPath, effectiveCanonicalPath)
			}
		} else {
			// Fallback to original path if resolution fails
			effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
		}
	} else {
		effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
	}

	// FRESHNESS CHECK: Проверяем, есть ли свежие данные
	// Используем уже канонизированный и дедуплицированный путь
	// Это предотвращает повторные дорогие поиски, если данные недавно обновлялись (волны/затухание)
	var sinceFacts *time.Time
	var existingFragment *models.HorizonFragment

	// If resolver found an existing fragment, use it directly
	if resolvedPath != nil && resolvedPath.ExistingFragment != nil {
		existingFragment = resolvedPath.ExistingFragment
	} else {
		// Otherwise check by canonical path
		if existing, err := e.fragmentsRepo.GetByCanonicalPath(ctx, effectiveCanonicalPath); err == nil && existing != nil {
			existingFragment = existing
		}
	}

	if existingFragment != nil {
		freshnessThreshold := 1 * time.Hour // Для теста. В проде может быть конфигурируемо.
		isFresh := !existingFragment.Metadata.LastVerifiedAt.IsZero() && time.Since(existingFragment.Metadata.LastVerifiedAt) < freshnessThreshold
		// Watermark for facts: prefer dedicated LastFactsSeenAt, fall back to LastVerifiedAt.
		if !existingFragment.Metadata.LastFactsSeenAt.IsZero() {
			sinceFacts = &existingFragment.Metadata.LastFactsSeenAt
		} else if !existingFragment.Metadata.LastVerifiedAt.IsZero() {
			sinceFacts = &existingFragment.Metadata.LastVerifiedAt
		}
		trace.Log(ctx, "scout_exec", "scout.freshness.checked", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"fragmentId":          strings.TrimSpace(existingFragment.ID),
			"isFresh":             isFresh,
			"epistemicStatus":     string(existingFragment.Metadata.EpistemicStatus),
			"lastVerifiedAt": func() string {
				if existingFragment.Metadata.LastVerifiedAt.IsZero() {
					return ""
				}
				return existingFragment.Metadata.LastVerifiedAt.UTC().Format(time.RFC3339Nano)
			}(),
			"lastFactsSeenAt": func() string {
				if existingFragment.Metadata.LastFactsSeenAt.IsZero() {
					return ""
				}
				return existingFragment.Metadata.LastFactsSeenAt.UTC().Format(time.RFC3339Nano)
			}(),
		})

		// FAST PATH: if we have fresh facts and they strongly match the fragment, apply deterministically without LLM.
		var facts []*coreModels.OntologyFact
		if e.planner != nil && e.planner.factsRepo != nil && sinceFacts != nil {
			var err error
			facts, err = e.planner.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, effectiveCanonicalPath, sinceFacts.UTC(), 200)
			if err == nil && len(facts) > 0 {
				trace.Log(ctx, "scout_exec", "scout.fast_path.facts_found", map[string]any{
					"tenantId":            strings.TrimSpace(job.TenantID),
					"projectId":           strings.TrimSpace(job.ProjectID),
					"lensId":              strings.TrimSpace(lensID),
					"jobId":               strings.TrimSpace(job.ID),
					"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
					"factsN":              len(facts),
				})
				applier := &FactApplier{vertex: e.planner.vertexSearchClient}
				// Thresholds are conservative; can be tuned later or moved into policy doc.
				applyRes, err := applier.TryApplyFactsFastPath(ctx, e.fragmentsRepo, existingFragment, facts, 0.70 /*relevance*/, 0.80 /*fact confidence*/)
				if err == nil && applyRes != nil && applyRes.Applied {
					trace.Log(ctx, "scout_exec", "scout.fast_path.applied", map[string]any{
						"tenantId":            strings.TrimSpace(job.TenantID),
						"projectId":           strings.TrimSpace(job.ProjectID),
						"lensId":              strings.TrimSpace(lensID),
						"jobId":               strings.TrimSpace(job.ID),
						"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
						"appliedFacts":        applyRes.AppliedFacts,
						"maxRelevance":        applyRes.MaxRelevance,
					})
					fmt.Printf("[ScoutEngine] Fast-path applied facts for %s: appliedFacts=%d maxRelevance=%.3f\n",
						job.TargetCanonicalPath, applyRes.AppliedFacts, applyRes.MaxRelevance)

					// Signal synthesis: synthesize signals from applied facts
					if e.signalSynthesizer != nil && e.factsRepo != nil {
						for _, fact := range facts {
							if fact == nil {
								continue
							}
							synthesizedSignals, err := e.signalSynthesizer.Synthesize(ctx, fact)
							if err == nil && len(synthesizedSignals) > 0 {
								for _, signal := range synthesizedSignals {
									// Save signal as OntologyFact
									if signalID, err := e.factsRepo.Create(ctx, signal); err == nil {
										fmt.Printf("[ScoutEngine] Synthesized signal %s from fact %s\n", signalID, fact.ID)
										// Apply signal's MetricUpdates to target fragments
										for _, path := range signal.CanonicalPaths {
											frag, _ := e.fragmentsRepo.GetByCanonicalPath(ctx, path)
											if frag != nil && len(signal.MetricUpdates) > 0 {
												// Apply metric updates
												for _, mu := range signal.MetricUpdates {
													if frag.State == nil {
														frag.State = make(map[string]models.MetricValue)
													}
													obs := time.Now()
													if mu.ObservedAt != nil {
														obs = *mu.ObservedAt
													}
													frag.State[mu.MetricName] = models.MetricValue{
														Value:      mu.Value,
														Unit:       mu.Unit,
														Confidence: mu.Confidence,
														ObservedAt: obs,
														Provenance: models.Provenance{
															Method:    "signal_synthesis",
															SourceIDs: []string{signalID},
														},
													}
												}
												if err := e.fragmentsRepo.Update(ctx, frag); err != nil {
													fmt.Printf("[ScoutEngine] Failed to persist signal-derived metric updates (best-effort): signalId=%s fragmentId=%s path=%s err=%v\n",
														strings.TrimSpace(signalID), strings.TrimSpace(frag.ID), strings.TrimSpace(path), err)
												}
												// Trigger propagation for signal-derived metrics
												if e.propagationTrigger != nil {
													go func(fid string) {
														propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
														defer cancel()
														if err := e.propagationTrigger(propCtx, fid); err != nil {
															fmt.Printf("[ScoutEngine] propagationTrigger failed for signal-derived metric updates: fragmentId=%s signalId=%s err=%v\n",
																strings.TrimSpace(fid), strings.TrimSpace(signalID), err)
														}
													}(frag.ID)
												}
											}
										}
									}
								}
							}
						}
					}

					// Dimension propagation: for applied facts, map sphere/location to dimension fragments and trigger waves.
					if err := e.applyDimensionWavesFromAppliedFacts(ctx, existingFragment, facts, applyRes.AppliedFactIDs); err != nil {
						fmt.Printf("[ScoutEngine] applyDimensionWavesFromAppliedFacts failed (best-effort): jobId=%s fragmentId=%s target=%s err=%v\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(existingFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
					}
					// Trigger propagation for deterministic updates as well (non-blocking).
					if e.propagationTrigger != nil {
						go func(fid string) {
							propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
							defer cancel()
							if err := e.propagationTrigger(propCtx, fid); err != nil {
								fmt.Printf("Propagation trigger failed for fragment %s: %v\n", fid, err)
							}
						}(existingFragment.ID)
					}
					if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "fast_path_applied_facts"); err != nil {
						fmt.Printf("[ScoutEngine] Failed to transition job to VERIFIED on fast-path (best-effort): jobId=%s target=%s err=%v\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
					}
					// Emit completion + node upsert for UI.
					e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
						"tenantId":        job.TenantID,
						"projectId":       projectID,
						"lensId":          lensID,
						"jobId":           job.ID,
						"fragmentId":      existingFragment.ID,
						"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
						"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
						"confidence":      1.0,
					})
					// Check if all jobs are completed
					e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, projectID, lensID)
					e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
						"tenantId":        job.TenantID,
						"projectId":       projectID,
						"lensId":          lensID,
						"fragmentId":      existingFragment.ID,
						"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
						"status":          "verified",
						"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
						"version":         existingFragment.Version,
					})
					return nil
				}
			}
		}

		// If fragment is fresh AND there are no new facts since watermark, skip expensive scouting.
		// IMPORTANT: do NOT skip Watchdog-driven jobs (they carry explicit SourceIDs and are triggered by content changes).
		if isFresh && len(facts) == 0 && len(job.SourceIDs) == 0 {
			fmt.Printf("[ScoutEngine] Data for %s is fresh (verified %v ago) and no new facts. Skipping active scouting.\n",
				job.TargetCanonicalPath, time.Since(existingFragment.Metadata.LastVerifiedAt))
			_ = e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "fresh_no_new_facts")
			e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
				"tenantId":        job.TenantID,
				"projectId":       projectID,
				"lensId":          lensID,
				"jobId":           job.ID,
				"fragmentId":      existingFragment.ID,
				"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
				"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
				"confidence":      1.0,
			})
			// Check if all jobs are completed
			e.checkAndPublishAssemblyCompletion(ctx, job.TenantID, projectID, lensID)
			return nil
		}
	}

	// PLANNING: Планирование источников
	if err := e.transition(ctx, job, models.ScoutingJobStatePlanning, 0.1, "planning"); err != nil {
		return fmt.Errorf("failed to transition to PLANNING: %w", err)
	}
	e.logLine(ctx, job, "info", fmt.Sprintf("> planning sources for %s", strings.TrimSpace(job.TargetCanonicalPath)))

	var sourcePlan *SourcePlan

	// ОПТИМИЗАЦИЯ: Если job имеет конкретные sourceIDs (от Watchdog), используем их напрямую.
	// Это избегает дорогого PLANNING (Vertex AI Search + Firestore queries) для Watchdog-driven jobs.
	// Watchdog уже обнаружил изменения в конкретных источниках, нет смысла переопределять их.
	if len(job.SourceIDs) > 0 {
		fmt.Printf("[ScoutEngine] Using explicit sourceIDs from job (Watchdog-driven): %d sources\n", len(job.SourceIDs))
		e.logLine(ctx, job, "info", fmt.Sprintf("> using explicit sourceIDs (%d) (watchdog-driven)", len(job.SourceIDs)))
		sources := make([]*coreModels.DataSource, 0, len(job.SourceIDs))
		for _, sourceID := range job.SourceIDs {
			if e.planner != nil && e.planner.dataSourcesRepo != nil {
				if source, err := e.planner.dataSourcesRepo.Get(ctx, sourceID); err == nil && source != nil {
					sources = append(sources, source)
				}
			}
		}
		sourcePlan = &SourcePlan{
			DataSourceIDs: job.SourceIDs,
			Strategy:      job.ScoutingStrategy,
			Sources:       sources,
		}
	} else {
		// Полное планирование только если sourceIDs не указаны (Lens-driven jobs)
		fmt.Printf("[ScoutEngine] Running full planning for %s (Lens-driven)\n", job.TargetCanonicalPath)
		e.logLine(ctx, job, "info", "> running full planning (lens-driven)")
		var err error
		// Optional focus query from Lens/ViewConfig (stored in job.State).
		searchQuery := ""
		if job.State != nil {
			if v, ok := job.State["focus"].(string); ok && strings.TrimSpace(v) != "" {
				searchQuery = strings.TrimSpace(v)
			} else if v, ok := job.State["searchQuery"].(string); ok && strings.TrimSpace(v) != "" {
				// Legacy/experimental alias.
				searchQuery = strings.TrimSpace(v)
			}
			// Optional free-form keywords (not necessarily canonical paths).
			if v, ok := job.State["keywords"]; ok {
				switch t := v.(type) {
				case []string:
					for _, s := range t {
						s = strings.TrimSpace(s)
						if s == "" {
							continue
						}
						if searchQuery != "" {
							searchQuery += " "
						}
						searchQuery += s
					}
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							s = strings.TrimSpace(s)
							if s == "" {
								continue
							}
							if searchQuery != "" {
								searchQuery += " "
							}
							searchQuery += s
						}
					}
				case string:
					s := strings.TrimSpace(t)
					if s != "" {
						if searchQuery != "" {
							searchQuery += " "
						}
						searchQuery += s
					}
				}
			}
			// If caller also provided spherePaths, treat them as additional query tokens.
			if v, ok := job.State["spherePaths"]; ok {
				switch t := v.(type) {
				case []string:
					for _, s := range t {
						s = strings.TrimSpace(s)
						if s != "" {
							if searchQuery != "" {
								searchQuery += " "
							}
							searchQuery += s
						}
					}
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							s = strings.TrimSpace(s)
							if s != "" {
								if searchQuery != "" {
									searchQuery += " "
								}
								searchQuery += s
							}
						}
					}
				case string:
					s := strings.TrimSpace(t)
					if s != "" {
						if searchQuery != "" {
							searchQuery += " "
						}
						searchQuery += s
					}
				}
			}
		}
		sourcePlan, err = e.planner.PlanSources(ctx, tenantID, projectID, job.TargetCanonicalPath, models.ScoutingStrategy(job.ScoutingStrategy), sinceFacts, searchQuery)
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "planning_failed")
			return fmt.Errorf("failed to plan sources: %w", err)
		}
	}

	// EXECUTION: Выполнение запросов к источникам
	if err := e.transition(ctx, job, models.ScoutingJobStateExecution, 0.25, "execution"); err != nil {
		return fmt.Errorf("failed to transition to EXECUTION: %w", err)
	}
	e.logLine(ctx, job, "info", "> executing sources")
	trace.Log(ctx, "scout_exec", "scout.execution.start", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(lensID),
		"jobId":               strings.TrimSpace(job.ID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"plannedSourcesN": func() int {
			if sourcePlan == nil {
				return 0
			}
			return len(sourcePlan.Sources)
		}(),
		"strategy": strings.TrimSpace(job.ScoutingStrategy),
	})

	var aggregatedContent string
	var allSourceIDs []string
	var allDocumentIDs []string
	var sourceResults []*ExecuteDataSourceResult

	// Получаем источники из плана
	for _, source := range sourcePlan.Sources {
		if source != nil {
			e.logLine(ctx, job, "info", fmt.Sprintf("> fetching source %s (%s)", strings.TrimSpace(source.ID), strings.TrimSpace(source.Kind)))
		}
		result, err := e.executor.ExecuteDataSource(ctx, source)
		if err != nil {
			// Логируем ошибку, но продолжаем с другими источниками
			fmt.Printf("[ScoutEngine] Failed to execute data source (id=%s, kind=%s): %v\n", source.ID, source.Kind, err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> source failed %s (%s): %v", strings.TrimSpace(source.ID), strings.TrimSpace(source.Kind), err))
			continue
		}
		if result.Content != "" {
			aggregatedContent += result.Content + "\n\n"
			sourceResults = append(sourceResults, result)
		}
		allSourceIDs = append(allSourceIDs, result.SourceIDs...)
		allDocumentIDs = append(allDocumentIDs, result.DocumentIDs...)
	}
	trace.Log(ctx, "scout_exec", "scout.execution.done", map[string]any{
		"tenantId":              strings.TrimSpace(job.TenantID),
		"projectId":             strings.TrimSpace(job.ProjectID),
		"lensId":                strings.TrimSpace(lensID),
		"jobId":                 strings.TrimSpace(job.ID),
		"targetCanonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
		"aggregatedContentLen":  len(aggregatedContent),
		"aggregatedContentSha1": trace.HashText(aggregatedContent),
		"sourceResultsN":        len(sourceResults),
		"sourceIdsN":            len(allSourceIDs),
		"documentIdsN":          len(allDocumentIDs),
	})

	var scoutResult *agents.ScoutResult
	var extractedFragment *models.HorizonFragment
	var validationResult *agents.ValidationResult

	if aggregatedContent == "" {
		fmt.Printf("[ScoutEngine] No internal content for %s. Initiating full research.\n", job.TargetCanonicalPath)
		e.logLine(ctx, job, "info", "> no internal content; initiating full research")
		trace.Log(ctx, "scout_research", "scout.no_internal_content", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"plannedSourcesN": func() int {
				if sourcePlan == nil {
					return 0
				}
				return len(sourcePlan.Sources)
			}(),
		})

		// Choose a sensible default type from canonicalPath prefix (no LLM needed for this deterministic mapping).
		defaultType := models.FragmentTypeOrganization
		switch {
		case strings.HasPrefix(job.TargetCanonicalPath, "/product/"):
			defaultType = models.FragmentTypeProduct
		case strings.HasPrefix(job.TargetCanonicalPath, "/program/"):
			defaultType = models.FragmentTypeProgram
		case strings.HasPrefix(job.TargetCanonicalPath, "/market/"):
			defaultType = models.FragmentTypeMarket
		case strings.HasPrefix(job.TargetCanonicalPath, "/fac/"):
			defaultType = models.FragmentTypeFacility
		case strings.HasPrefix(job.TargetCanonicalPath, "/loc/") || strings.HasPrefix(job.TargetCanonicalPath, "/region/"):
			defaultType = models.FragmentTypeGeoRegion
		case strings.HasPrefix(job.TargetCanonicalPath, "/event/"):
			defaultType = models.FragmentTypeEvent
		case strings.HasPrefix(job.TargetCanonicalPath, "/gov/"):
			// Government bodies are modeled as ORGANIZATION fragments, but use /gov/{iso2}/{slug} paths to avoid collisions.
			defaultType = models.FragmentTypeOrganization
		case strings.HasPrefix(job.TargetCanonicalPath, "/org/"):
			defaultType = models.FragmentTypeOrganization
		case strings.HasPrefix(job.TargetCanonicalPath, "/person/"):
			defaultType = models.FragmentTypePerson
		case strings.HasPrefix(job.TargetCanonicalPath, "/media/"):
			defaultType = models.FragmentTypeMediaChannel
		}

		// Создаем базовый фрагмент
		extractedFragment = &models.HorizonFragment{
			CanonicalPath: job.TargetCanonicalPath,
			TenantID:      tenantID,
			Type:          defaultType,
			State:         make(map[string]models.MetricValue),
			Governance: models.Governance{
				Owner:  "SYSTEM",
				Status: "ACTIVE",
			},
			Metadata: models.FragmentMetadata{
				CreatedAt:       time.Now(),
				LastUpdatedAt:   time.Now(),
				EpistemicStatus: models.EpistemicStatusLowConfidence,
			},
		}

		scoutResult = &agents.ScoutResult{
			Fragment:  *extractedFragment,
			Relations: []agents.DiscoveredRelation{},
		}

		// Пытаемся определить таксономию даже без контента
		entityPath, sectorPath, err := e.taxonomyClassifier.ClassifyTaxonomy(ctx, job.TargetCanonicalPath)
		if err == nil {
			extractedFragment.EntityPath = entityPath
			extractedFragment.SectorPath = sectorPath
		}

		// Формируем результат валидации, требующий полного исследования
		validationResult = &agents.ValidationResult{
			IsValid:    true,
			Confidence: 0.1,
			MissingInfo: []string{
				fmt.Sprintf("Comprehensive information about entity '%s' is missing. Identify its type, location, key metrics, description, and relationships.", job.TargetCanonicalPath),
			},
		}
		// MARKET: When internal sources are empty, we must explicitly seed market sizing/concentration questions.
		// Critic is not run in this branch, so without these prompts market metrics are often never populated.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/market/") || defaultType == models.FragmentTypeMarket {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("What is the market domain and vertical for %s? Return marketDomain (finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate) and marketVertical (specific vertical) with sources.", job.TargetCanonicalPath),
				fmt.Sprintf("What is the TAM (total addressable market) in USD for %s (latest available year)? Provide value (number), year, and sources.", job.TargetCanonicalPath),
				fmt.Sprintf("What is the market CAGR percent for %s (provide period, e.g. 2024-2029)? Provide percent and sources.", job.TargetCanonicalPath),
				fmt.Sprintf("What is the market concentration for %s? Provide CR4 (top-4 share percent) and/or HHI (0..10000) with sources.", job.TargetCanonicalPath),
				fmt.Sprintf("Who are the key players in %s and who dominates? Provide top players and (if available) market shares.", job.TargetCanonicalPath),
			)
		}
		// Always request ORG/FACILITY classifiers and governance facts when internal sources are empty.
		// Critic is not run in this branch, so we seed missingInfo explicitly for grounded research.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/org/") ||
			strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/") ||
			strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/fac/") {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Who is the external auditor of %s (as of the latest fiscal year)? Provide audit firm legal name and source. Output as relation AUDITED_BY.", job.TargetCanonicalPath),
				fmt.Sprintf("[HIGH PRIORITY] What industry/sector does %s operate in? Return marketSegments as array of paths /segment/{domain}/{vertical}/{geo}. Domains: finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate|public-sector|professional-services. For government use public-sector domain. Geo: ISO2 lowercase or regional bucket (global, mena, emea, apac, latam, na, gcc, eu, asean, nordics, benelux, anz, cis, ssa, usmca, nafta). Example: /segment/technology/semiconductors/tw", job.TargetCanonicalPath),
				fmt.Sprintf("[HIGH PRIORITY] What is the size of %s? Find employee count or revenue to determine orgSize: sme (<500 emp or <$100M rev), le (500-5000 emp or $100M-$1B), enterprise (5000-50000 emp or $1B-$10B), mega (>50000 emp or >$10B). For government: federal G20 ministries=enterprise/mega, regional=le/enterprise. Use 'na' only if truly unknown.", job.TargetCanonicalPath),
			)
		}
		// If a gov path uses /gov/na/... we MUST resolve jurisdiction to avoid cross-country collisions.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/na/") {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Which country/jurisdiction does %s belong to? Replace /gov/na/... with /gov/{iso2}/... based on evidence.", job.TargetCanonicalPath),
			)
		}
		// If an org path is legacy (/org/{slug}) or uses /org/na/... we should try to resolve the country of origin.
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/org/na/") || isLegacyOrgPath(strings.TrimSpace(job.TargetCanonicalPath)) {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("What is the country of origin / primary jurisdiction for %s? Prefer /org/{iso2}/{slug} canonical path. If unknown, keep /org/na/...", job.TargetCanonicalPath),
			)
		}
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/person/") {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Who are the direct public relatives (spouse/children/parents/siblings) of %s? Output RELATIVE_OF links with sources.", job.TargetCanonicalPath),
				fmt.Sprintf("Who are the publicly known close associates of %s (business partners/friends/classmates)? Output RELATED_TO links with evidence and sources.", job.TargetCanonicalPath),
			)
		}
		// Add specialty-specific research prompts when internal sources are empty (Critic is not run in this branch).
		if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyManagement) {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("Who are the executive leaders (CEO, CFO, COO, CTO/CIO, CMO, CHRO, President/Managing Director, Chair) of %s? Provide names and roles with sources.", job.TargetCanonicalPath),
			)
		}
		if taxonomy.HasSpecialty(specialties, taxonomy.SpecialtyProductsAndServices) {
			validationResult.MissingInfo = append(validationResult.MissingInfo,
				fmt.Sprintf("What products and services does %s produce/offer? Provide a canonical list of outputs suitable for /product/... paths.", job.TargetCanonicalPath),
				fmt.Sprintf("What key inputs/materials/components does %s consume? Provide a canonical list of inputs suitable for /product/... paths.", job.TargetCanonicalPath),
			)
			// For product nodes, also request upstream/downstream transformations.
			if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/product/") {
				validationResult.MissingInfo = append(validationResult.MissingInfo,
					fmt.Sprintf("What are the upstream materials and downstream derived products for %s (produced from / used to produce)? Return TRANSFORMS_INTO relationships between products.", job.TargetCanonicalPath),
				)
			}
		}
	} else {
		// EXTRACTION: Извлечение данных через Scout Agent
		if err := e.transition(ctx, job, models.ScoutingJobStateExtraction, 0.5, "extraction"); err != nil {
			return fmt.Errorf("failed to transition to EXTRACTION: %w", err)
		}
		e.logLine(ctx, job, "info", "> extracting structured data")

		// Определяем entityPath и sectorPath из targetCanonicalPath через LLM
		entityPath, sectorPath, err := e.taxonomyClassifier.ClassifyTaxonomy(ctx, job.TargetCanonicalPath)
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "taxonomy_failed")
			return fmt.Errorf("failed to classify taxonomy: %w", err)
		}
		e.logLine(ctx, job, "info", fmt.Sprintf("> taxonomy classified: entityPath=%s sectorPath=%s", entityPath, sectorPath))

		scoutResult, err = e.scoutAgent.ExtractFragmentData(ctx, aggregatedContent, job.TargetCanonicalPath, entityPath, sectorPath, specialties, chainContextForPrompt)
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "extraction_failed")
			return fmt.Errorf("failed to extract fragment data: %w", err)
		}
		extractedFragment = &scoutResult.Fragment

		// VALIDATION: Валидация через Critic Agent
		if err := e.transition(ctx, job, models.ScoutingJobStateValidation, 0.7, "validation"); err != nil {
			return fmt.Errorf("failed to transition to VALIDATION: %w", err)
		}
		e.logLine(ctx, job, "info", "> critic validating extracted data")

		// Provide Critic with a compact view of the existing hierarchy skeleton for MARKET/GOV targets,
		// so it can explicitly request "build up/down" actions as missingInfo without blocking the pipeline.
		graphContext := ""
		if strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/market/") || strings.HasPrefix(strings.TrimSpace(job.TargetCanonicalPath), "/gov/") {
			graphContext = e.buildGraphSkeletonContext(ctx, job.TargetCanonicalPath)
		}
		validationResult, err = e.criticAgent.ValidateWithGraphContext(ctx, extractedFragment, scoutResult.Relations, scoutResult.SupplyChains, aggregatedContent, specialties, graphContext)
		if err != nil {
			// Логируем ошибку, но не останавливаем процесс (fail-open с низким confidence)
			fmt.Printf("[ScoutEngine] critic validation error (fail-open): jobId=%s target=%s err=%v\n",
				strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> critic validation error (fail-open): %v", err))
			validationResult = &agents.ValidationResult{
				IsValid:    true,
				Confidence: 0.5,
			}
		}
	}

	// MARKET METRICS NORMALIZATION (best-effort):
	// Research currently may emit legacy keys like "market_tam_usd". We canonicalize them into /metric/market/... so:
	// - Critic can reliably require/check them
	// - later systems can aggregate/query them consistently
	// - they are not "lost" due to inconsistent key naming.
	if extractedFragment != nil && (extractedFragment.Type == models.FragmentTypeMarket || strings.HasPrefix(strings.TrimSpace(extractedFragment.CanonicalPath), "/market/")) {
		normalizeMarketMetricKeys(extractedFragment)
	}

	// RESEARCH: Дообогащение данных через Research Agent
	if len(validationResult.MissingInfo) > 0 {
		// Filter out missingInfo items that are exhausted or in cooldown to prevent infinite loops.
		// This uses the horizon_missing_info_status collection to track attempts per question.
		if e.missingInfoStatusRepo != nil {
			searchable, filteredCount, err := e.missingInfoStatusRepo.FilterSearchable(ctx, extractedFragment.ID, validationResult.MissingInfo)
			if err != nil {
				// Fail-open: keep missingInfo list, but log loudly so issues are visible.
				fmt.Printf("[ScoutEngine] missingInfo filter error (fail-open): jobId=%s fragmentId=%s target=%s err=%v\n",
					strings.TrimSpace(job.ID), strings.TrimSpace(extractedFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
				e.logLine(ctx, job, "warn", fmt.Sprintf("> missingInfo filter error (fail-open): %v", err))
			} else {
				if filteredCount > 0 {
					fmt.Printf("[ScoutEngine] Filtered %d exhausted/cooldown missingInfo items for fragment %s\n", filteredCount, extractedFragment.ID)
					e.logLine(ctx, job, "info", fmt.Sprintf("> filtered %d exhausted/cooldown missingInfo items", filteredCount))
				}
				validationResult.MissingInfo = searchable
			}
		}
		// Check if there are still items to research after filtering
		if len(validationResult.MissingInfo) == 0 {
			goto SKIP_RESEARCH
		}
		// Daily guardrails (USD + token caps) to keep costs bounded during MVP.
		if e.budgetPolicy.Enabled && e.spendLedger != nil && lensID != "" && cascadeID != "" {
			day := time.Now().UTC().Format("2006-01-02")
			if cSpend, _, cOut, err := e.spendLedger.GetCascadeSpendUSD(ctx, tenantID, cascadeID, day); err == nil {
				if e.budgetPolicy.CascadeDailyMaxUSD > 0 && cSpend >= e.budgetPolicy.CascadeDailyMaxUSD {
					fmt.Printf("[ScoutEngine] Skipping research: cascade daily USD budget exceeded: cascade=%s spend=%.4f\n", cascadeID, cSpend)
					goto SKIP_RESEARCH
				}
				if e.budgetPolicy.CascadeDailyMaxOutputTokens > 0 && cOut >= e.budgetPolicy.CascadeDailyMaxOutputTokens {
					fmt.Printf("[ScoutEngine] Skipping research: cascade daily token cap exceeded: cascade=%s outTokens=%d\n", cascadeID, cOut)
					goto SKIP_RESEARCH
				}
			}
			if dSpend, _, dOut, err := e.spendLedger.GetLensDaySpendUSD(ctx, tenantID, lensID, day); err == nil {
				if e.budgetPolicy.LensDailyMaxUSD > 0 && dSpend >= e.budgetPolicy.LensDailyMaxUSD {
					fmt.Printf("[ScoutEngine] Skipping research: lens daily USD budget exceeded: lens=%s day=%s spend=%.4f\n", lensID, day, dSpend)
					goto SKIP_RESEARCH
				}
				if e.budgetPolicy.LensDailyMaxOutputTokens > 0 && dOut >= e.budgetPolicy.LensDailyMaxOutputTokens {
					fmt.Printf("[ScoutEngine] Skipping research: lens daily token cap exceeded: lens=%s day=%s outTokens=%d\n", lensID, day, dOut)
					goto SKIP_RESEARCH
				}
			}
		}
		fmt.Printf("[ScoutEngine] Starting research for job %s, missing info: %v\n", job.ID, validationResult.MissingInfo)
		e.logLine(ctx, job, "info", fmt.Sprintf("> research started (missing=%d)", len(validationResult.MissingInfo)))
		researchCtx := ai.WithUsageContext(ctx, tenantID, "SYSTEM", projectID, "horizon.research", lensID, cascadeID)
		// Pass focus signals to grounded research (best-effort).
		focus := ""
		var spherePaths []string
		var keywords []string
		if job.State != nil {
			if v, ok := job.State["focus"].(string); ok {
				focus = strings.TrimSpace(v)
			}
			if v, ok := job.State["spherePaths"]; ok {
				switch t := v.(type) {
				case []string:
					spherePaths = append(spherePaths, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							spherePaths = append(spherePaths, s)
						}
					}
				case string:
					parts := strings.Split(t, ",")
					for _, p := range parts {
						spherePaths = append(spherePaths, p)
					}
				}
			}
			if v, ok := job.State["keywords"]; ok {
				switch t := v.(type) {
				case []string:
					keywords = append(keywords, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok2 := it.(string); ok2 {
							keywords = append(keywords, s)
						}
					}
				case string:
					parts := strings.Split(t, ",")
					for _, p := range parts {
						keywords = append(keywords, p)
					}
				}
			}
		}
		// Extract originalQuery and decompositionHints for MARKET-aware research
		originalQuery := ""
		var decompositionHints []agents.DecompositionHint
		if job.State != nil {
			if v, ok := job.State["originalQuery"].(string); ok {
				originalQuery = strings.TrimSpace(v)
			}
			if v, ok := job.State["decompositionHints"]; ok && v != nil {
				// Parse decompositionHints (may come as []interface{} from JSON)
				if dhBytes, err := json.Marshal(v); err == nil {
					if err := json.Unmarshal(dhBytes, &decompositionHints); err != nil {
						fmt.Printf("[ScoutEngine] Failed to parse decompositionHints (best-effort): jobId=%s target=%s err=%v\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
					}
				}
			}
		}
		trace.Log(ctx, "scout_research", "scout.research.start", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"missingN":            len(validationResult.MissingInfo),
			"missingTop":          trace.StrSlice(validationResult.MissingInfo, 6),
			"focus":               trace.Trunc(focus, 140),
			"spherePathsTop":      trace.StrSlice(spherePaths, 10),
			"keywordsTop":         trace.StrSlice(keywords, 10),
			"originalQueryLen":    len(originalQuery),
			"decompositionHintsN": len(decompositionHints),
		})
		enrichedFragment, newRelations, newSupplyChains, err := e.researchAgent.EnrichFragment(researchCtx, extractedFragment, validationResult.MissingInfo, focus, spherePaths, keywords, specialties, chainContextForPrompt, originalQuery, decompositionHints)
		if err == nil {
			// Capture canonicalPath drift introduced by research (aliasing).
			oldCP := ""
			if extractedFragment != nil {
				oldCP = strings.TrimSpace(extractedFragment.CanonicalPath)
			}
			newCP := ""
			if enrichedFragment != nil {
				newCP = strings.TrimSpace(enrichedFragment.CanonicalPath)
			}
			extractedFragment = enrichedFragment
			if len(newRelations) > 0 {
				scoutResult.Relations = append(scoutResult.Relations, newRelations...)
				fmt.Printf("[ScoutEngine] Research found %d new relations.\n", len(newRelations))
				e.logLine(ctx, job, "info", fmt.Sprintf("> research found %d new relations", len(newRelations)))
			}
			// Append supply chains from research (if supply_chain mode enabled)
			if len(newSupplyChains) > 0 {
				scoutResult.SupplyChains = append(scoutResult.SupplyChains, newSupplyChains...)
				fmt.Printf("[ScoutEngine] Research found %d new supply chains.\n", len(newSupplyChains))
				e.logLine(ctx, job, "info", fmt.Sprintf("> research found %d new supply chains", len(newSupplyChains)))
			}
			// Обновляем LastVerifiedAt
			extractedFragment.Metadata.LastVerifiedAt = time.Now()
			trace.Log(ctx, "scout_research", "scout.research.ok", map[string]any{
				"tenantId":                    strings.TrimSpace(job.TenantID),
				"projectId":                   strings.TrimSpace(job.ProjectID),
				"lensId":                      strings.TrimSpace(lensID),
				"jobId":                       strings.TrimSpace(job.ID),
				"targetCanonicalPath":         strings.TrimSpace(job.TargetCanonicalPath),
				"fragmentCanonicalPathBefore": oldCP,
				"fragmentCanonicalPathAfter":  newCP,
				"newRelationsN":               len(newRelations),
				"newSupplyChainsN":            len(newSupplyChains),
				"relationsTopCanonicalPaths": func() []string {
					out := make([]string, 0, len(newRelations))
					for _, r := range newRelations {
						out = append(out, strings.TrimSpace(r.TargetCanonicalPath))
					}
					return trace.StrSlice(out, 12)
				}(),
			})
			// Record successful research attempts for resolved questions.
			// A question is considered resolved if new relations/data were found.
			if e.missingInfoStatusRepo != nil {
				foundSomething := len(newRelations) > 0 || len(newSupplyChains) > 0
				for _, q := range validationResult.MissingInfo {
					if recErr := e.missingInfoStatusRepo.RecordAttempt(ctx, extractedFragment.ID, q, foundSomething, 0); recErr != nil {
						fmt.Printf("[ScoutEngine] missingInfo RecordAttempt failed: jobId=%s fragmentId=%s target=%s found=%v err=%v q=%q\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(extractedFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), foundSomething, recErr, trace.Trunc(q, 160))
						e.logLine(ctx, job, "warn", fmt.Sprintf("> missingInfo RecordAttempt failed: %v", recErr))
					}
				}
			}
		} else {
			fmt.Printf("[ScoutEngine] Research failed: %v\n", err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> research failed: %v", err))
			trace.Log(ctx, "scout_research", "scout.research.error", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(lensID),
				"jobId":               strings.TrimSpace(job.ID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"error":               err.Error(),
			})
			// Record failed research attempts (increment counter, apply cooldown).
			if e.missingInfoStatusRepo != nil {
				for _, q := range validationResult.MissingInfo {
					if recErr := e.missingInfoStatusRepo.RecordAttempt(ctx, extractedFragment.ID, q, false, 0); recErr != nil {
						fmt.Printf("[ScoutEngine] missingInfo RecordAttempt failed: jobId=%s fragmentId=%s target=%s found=%v err=%v q=%q\n",
							strings.TrimSpace(job.ID), strings.TrimSpace(extractedFragment.ID), strings.TrimSpace(job.TargetCanonicalPath), false, recErr, trace.Trunc(q, 160))
						e.logLine(ctx, job, "warn", fmt.Sprintf("> missingInfo RecordAttempt failed: %v", recErr))
					}
				}
			}
		}
	}
SKIP_RESEARCH:

	// RESOLUTION: Разрешение конфликтов если необходимо
	if validationResult.HasConflicts {
		if err := e.transition(ctx, job, models.ScoutingJobStateResolution, 0.85, "resolution"); err != nil {
			return fmt.Errorf("failed to transition to RESOLUTION: %w", err)
		}

		resolutionResult, err := e.resolverAgent.Resolve(ctx, agents.ResolutionRequest{
			ScoutData:       extractedFragment,
			CriticArguments: validationResult.Conflicts,
			SourceContent:   aggregatedContent,
		})
		if err != nil {
			_ = e.transition(ctx, job, models.ScoutingJobStateFailed, 1.0, "resolution_failed")
			return fmt.Errorf("failed to resolve conflicts: %w", err)
		}

		switch resolutionResult.Verdict {
		case "Rejected":
			_ = e.transition(ctx, job, models.ScoutingJobStateRejected, 1.0, "rejected")
			return fmt.Errorf("data rejected by resolver: %s", resolutionResult.Reasoning)
		case "Low_Confidence":
			// Продолжаем с низкой уверенностью
			if resolutionResult.ResolvedData != nil {
				// Обновляем extractedFragment с исправленными данными из Resolver
				if resolvedJSON, ok := resolutionResult.ResolvedData.(map[string]interface{}); ok {
					// Извлекаем state из resolvedJSON если он есть
					if resolvedState, ok := resolvedJSON["state"].(map[string]interface{}); ok {
						// Обновляем метрики в extractedFragment.State
						for metricName, metricValue := range extractedFragment.State {
							if resolvedMetricValue, ok := resolvedState[metricName].(map[string]interface{}); ok {
								if value, ok := resolvedMetricValue["value"].(float64); ok {
									metricValue.Value = value
									extractedFragment.State[metricName] = metricValue
								}
							}
						}
					}
				}
			}
		case "Verified":
			// Данные подтверждены, продолжаем
		}
	}

	// METRICS: Ensure standardized risk metrics are present in fragment.State.
	// Upstream extraction/research can yield facts but may omit normalized risk scores,
	// which would make Lens metrics aggregation and propagation ineffective.
	if extractedFragment.State == nil {
		extractedFragment.State = make(map[string]models.MetricValue)
	}
	{
		sourceURLs := make([]string, 0, len(sourceResults))
		for _, r := range sourceResults {
			if r != nil && strings.TrimSpace(r.SourceURL) != "" {
				sourceURLs = append(sourceURLs, strings.TrimSpace(r.SourceURL))
			}
		}
		// Optional dynamic metrics explicitly requested by caller (job.State["requestedMetrics"]).
		// Can be either []string or []any of strings; each entry can be:
		// - canonical metric path: "/metric/risk/..."
		// - domain path: "risk/..." or "/risk/..."
		// - bare name: "battery_supplier_concentration" (will be canonicalized)
		var requested []string
		if job.State != nil {
			if v, ok := job.State["requestedMetrics"]; ok {
				switch t := v.(type) {
				case []string:
					requested = append(requested, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
							requested = append(requested, s)
						}
					}
				case string:
					// Allow comma-separated convenience
					parts := strings.Split(t, ",")
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							requested = append(requested, p)
						}
					}
				}
			}
		}

		metricsAgent := agents.NewMetricsAgent(e.aiExecutor)
		state, err := metricsAgent.ComputeState(ctx, extractedFragment, sourceURLs, aggregatedContent, scoutResult.Relations, requested)
		if err != nil {
			fmt.Printf("[ScoutEngine] MetricsAgent failed (keeping existing state, len=%d): %v\n", len(extractedFragment.State), err)
		} else if len(state) > 0 {
			// IMPORTANT: Do NOT replace the entire state map.
			// Research (especially for MARKET) can populate absolute market metrics (TAM/CAGR/HHI/CR4) which must be preserved.
			for k, v := range state {
				extractedFragment.State[k] = v
			}
		}
	}

	// CANONICAL RETARGET:
	// If Research (or any upstream step) produced a different canonicalPath than the job started with,
	// we must continue work on THAT entity. Otherwise we end up verifying/storing data under one path
	// while repeatedly scouting another alias (infinite "no internal content" loop).
	// Note: effectiveCanonicalPath is already set from early canonicalization above
	if effectiveCanonicalPath == "" {
		effectiveCanonicalPath = strings.TrimSpace(job.TargetCanonicalPath)
	}
	if extractedFragment != nil {
		if cp := strings.TrimSpace(extractedFragment.CanonicalPath); cp != "" {
			effectiveCanonicalPath = cp
		}
	}
	if effectiveCanonicalPath != "" && strings.TrimSpace(job.TargetCanonicalPath) != effectiveCanonicalPath {
		old := strings.TrimSpace(job.TargetCanonicalPath)
		trace.Log(ctx, "scout_persist", "scout.retarget.start", map[string]any{
			"tenantId":          strings.TrimSpace(job.TenantID),
			"projectId":         strings.TrimSpace(job.ProjectID),
			"lensId":            strings.TrimSpace(lensID),
			"jobId":             strings.TrimSpace(job.ID),
			"fromCanonicalPath": old,
			"toCanonicalPath":   effectiveCanonicalPath,
			"hint":              "using_canonicalPath_from_research_or_dedup",
		})
		fmt.Printf("[ScoutEngine] Retargeting job %s: %s -> %s (using canonicalPath from research/dedup)\n",
			strings.TrimSpace(job.ID), old, effectiveCanonicalPath)
		// Persist into job + state so Cloud Tasks retries execute the correct target as well.
		job.TargetCanonicalPath = effectiveCanonicalPath
		if job.State == nil {
			job.State = map[string]interface{}{}
		}
		job.State["targetCanonicalPath"] = effectiveCanonicalPath
		// Keep original path for later anti-dedup / alias cleanup (best-effort).
		if _, ok := job.State["aliasCanonicalPath"]; !ok && old != "" {
			job.State["aliasCanonicalPath"] = old
		}
		if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
			fmt.Printf("[ScoutEngine] Failed to persist job retarget (best-effort): jobId=%s from=%s to=%s err=%v\n",
				strings.TrimSpace(job.ID), old, effectiveCanonicalPath, err)
		}
		// Best-effort: update lens scope so UI/graph doesn't keep alias paths.
		e.bestEffortRewriteLensScope(ctx, tenantID, lensID, old, effectiveCanonicalPath)
		trace.Log(ctx, "scout_persist", "scout.retarget.done", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(lensID),
			"jobId":               strings.TrimSpace(job.ID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		})
	}

	// VERIFIED: Создание Frame и обновление HorizonFragment
	if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "verified"); err != nil {
		return fmt.Errorf("failed to transition to VERIFIED: %w", err)
	}
	trace.Log(ctx, "scout_persist", "scout.persist.start", map[string]any{
		"tenantId":               strings.TrimSpace(job.TenantID),
		"projectId":              strings.TrimSpace(job.ProjectID),
		"lensId":                 strings.TrimSpace(lensID),
		"jobId":                  strings.TrimSpace(job.ID),
		"effectiveCanonicalPath": effectiveCanonicalPath,
		"confidence":             validationResult.Confidence,
		"relationsN": func() int {
			if scoutResult == nil {
				return 0
			}
			return len(scoutResult.Relations)
		}(),
	})

	// Извлекаем Evidence из результатов источников через LLM
	evidenceExtractor := NewEvidenceExtractorLLM(e.aiExecutor)
	evidence, err := ExtractEvidence(ctx, evidenceExtractor, sourceResults, extractedFragment.State)
	if err != nil {
		// Логируем ошибку, но продолжаем без Evidence
		fmt.Printf("[ScoutEngine] Failed to extract evidence (jobID=%s, fragmentPath=%s): %v\n", job.ID, job.TargetCanonicalPath, err)
		evidence = []models.Evidence{}
	}

	// Создаем Frame
	frame := e.frameBuilder.BuildFrame(
		tenantID,
		projectID,
		job.ID,
		effectiveCanonicalPath,
		extractedFragment.State,
		evidence,
		models.ClaimProvenance{
			SourceIDs:     allSourceIDs,
			DataSourceIDs: allSourceIDs,
			DocumentIDs:   allDocumentIDs,
			Method:        models.ExtractionMethodNLP,
			ExtractedAt:   time.Now(),
		},
		extractedFragment.KBRefs,
		validationResult.Confidence,
		allSourceIDs,
		allDocumentIDs,
	)

	frameID, err := e.framesRepo.Create(ctx, frame)
	if err != nil {
		return fmt.Errorf("failed to create frame: %w", err)
	}
	frame.ID = frameID

	// Обновляем или создаем HorizonFragment
	// Note: existingFragment may already be set from early canonicalization above
	if existingFragment == nil {
		var getErr error
		existingFragment, getErr = e.fragmentsRepo.GetByCanonicalPath(ctx, effectiveCanonicalPath)
		if getErr != nil {
			err = getErr
		}
	}
	var fragmentID string

	if err != nil || existingFragment == nil {
		// Создаем новый фрагмент
		extractedFragment.CanonicalPath = effectiveCanonicalPath
		extractedFragment.TenantID = "global"
		// Ensure we have a human-readable label even when upstream LLM produced slug-like lowercase or left labels empty.
		// This only affects display fields and does not change canonicalPath/type.
		if extractedFragment.Labels == nil {
			extractedFragment.Labels = map[string]string{}
		}
		if strings.TrimSpace(extractedFragment.Labels["en"]) == "" {
			seed := strings.TrimSpace(extractedFragment.Identifiers["legalName"])
			if seed == "" {
				seed = strings.TrimSpace(extractedFragment.Identifiers["shortName"])
			}
			extractedFragment.Labels["en"] = bestEffortDisplayLabel(seed, effectiveCanonicalPath)
		} else {
			extractedFragment.Labels["en"] = bestEffortDisplayLabel(extractedFragment.Labels["en"], effectiveCanonicalPath)
		}
		extractedFragment.Metadata.SourceFrameID = frameID
		extractedFragment.Metadata.EpistemicStatus = models.EpistemicStatusVerified
		if validationResult.Confidence < 0.7 {
			extractedFragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}

		// Pre-create validation: check policies and deduplication
		usedExistingDuplicate := false
		if e.preCreateValidator != nil {
			preValidation, valErr := e.preCreateValidator.ValidateAndProcess(ctx, extractedFragment)
			if valErr != nil {
				fmt.Printf("[ScoutEngine] Warning: pre-create validation error: %v\n", valErr)
			} else if preValidation != nil {
				// Log warnings
				for _, w := range preValidation.Warnings {
					fmt.Printf("[ScoutEngine] Pre-create warning: %s\n", w)
				}
				// If pre-create validation normalized the canonicalPath (e.g., /loc/uae -> /loc/ae/na/na),
				// retarget the job so all subsequent steps operate on the canonical path.
				if strings.TrimSpace(extractedFragment.CanonicalPath) != "" && extractedFragment.CanonicalPath != effectiveCanonicalPath {
					old := effectiveCanonicalPath
					effectiveCanonicalPath = strings.TrimSpace(extractedFragment.CanonicalPath)
					job.TargetCanonicalPath = effectiveCanonicalPath
					if job.State == nil {
						job.State = map[string]interface{}{}
					}
					job.State["targetCanonicalPath"] = effectiveCanonicalPath
					if _, ok := job.State["aliasCanonicalPath"]; !ok && strings.TrimSpace(old) != "" {
						job.State["aliasCanonicalPath"] = strings.TrimSpace(old)
					}
					fmt.Printf("[ScoutEngine] Retargeting job %s to normalized canonicalPath: %s -> %s\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(old), effectiveCanonicalPath)
					_ = e.scoutingJobsRepo.Update(ctx, job)
					// Best-effort: update lens scope so UI/graph doesn't keep alias paths.
					e.bestEffortRewriteLensScope(ctx, tenantID, lensID, old, effectiveCanonicalPath)
				}
				// Only block on critical issues (empty path/type, /discovered/ paths)
				// Warnings don't block creation - they're logged for post-factum fixes
				if !preValidation.IsValid && preValidation.BlockReason != "" {
					// Only block on critical cases that cannot be fixed post-factum
					if strings.Contains(preValidation.BlockReason, "/discovered/") ||
						strings.Contains(preValidation.BlockReason, "canonical path is required") ||
						strings.Contains(preValidation.BlockReason, "fragment type is required") {
						return fmt.Errorf("fragment creation blocked: %s", preValidation.BlockReason)
					}
					// For other cases, log warning but continue
					fmt.Printf("[ScoutEngine] WARNING (non-blocking): %s\n", preValidation.BlockReason)
				}
				// Check if we should use an existing fragment instead (deduplication)
				if preValidation.ReplacementFrag != nil && preValidation.ReplacementFrag.ID != "" {
					fmt.Printf("[ScoutEngine] Using existing fragment %s instead of creating new\n", preValidation.ReplacementFrag.ID)
					existingFragment = preValidation.ReplacementFrag
					fragmentID = existingFragment.ID
					usedExistingDuplicate = true

					// Merge OrgSize/MarketSegments from extracted into existing (fill-only)
					if strings.TrimSpace(extractedFragment.OrgSize) != "" {
						cur := strings.TrimSpace(strings.ToLower(existingFragment.OrgSize))
						in := strings.TrimSpace(extractedFragment.OrgSize)
						if cur == "" || cur == "na" {
							existingFragment.OrgSize = in
						}
					}
					if len(extractedFragment.MarketSegments) > 0 {
						if existingFragment.MarketSegments == nil {
							existingFragment.MarketSegments = make([]string, 0, len(extractedFragment.MarketSegments))
						}
						seen := make(map[string]bool)
						for _, s := range existingFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s != "" {
								seen[s] = true
							}
						}
						for _, s := range extractedFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s == "" {
								continue
							}
							if seen[s] {
								continue
							}
							seen[s] = true
							existingFragment.MarketSegments = append(existingFragment.MarketSegments, s)
						}
					}

					// Also merge State/Metadata from extracted fragment so new scouting is effective
					if len(extractedFragment.State) > 0 {
						if existingFragment.State == nil {
							existingFragment.State = make(map[string]models.MetricValue)
						}
						for k, v := range extractedFragment.State {
							existingFragment.State[k] = v
						}
					}
					existingFragment.Metadata.LastUpdatedAt = time.Now()
					existingFragment.Metadata.LastVerifiedAt = time.Now()
					existingFragment.Metadata.SourceFrameID = frameID

					// Best-effort: save the merged OrgSize/MarketSegments back to Firestore
					if uerr := e.fragmentsRepo.Update(ctx, existingFragment); uerr != nil {
						fmt.Printf("[ScoutEngine] Warning: failed to update fragment after dedup merge: %v\n", uerr)
					}

					// If dedup selected a replacement fragment, we must retarget the job to it as well.
					// Otherwise subsequent steps (events, decomposition, retries) will keep using the alias.
					if existingFragment != nil {
						if cp := strings.TrimSpace(existingFragment.CanonicalPath); cp != "" && cp != effectiveCanonicalPath {
							old := effectiveCanonicalPath
							effectiveCanonicalPath = cp
							fmt.Printf("[ScoutEngine] Retargeting job %s to dedup replacement canonicalPath: %s -> %s\n",
								strings.TrimSpace(job.ID), old, effectiveCanonicalPath)
							job.TargetCanonicalPath = effectiveCanonicalPath
							if job.State == nil {
								job.State = map[string]interface{}{}
							}
							job.State["targetCanonicalPath"] = effectiveCanonicalPath
							if _, ok := job.State["aliasCanonicalPath"]; !ok && strings.TrimSpace(old) != "" {
								job.State["aliasCanonicalPath"] = strings.TrimSpace(old)
							}
							_ = e.scoutingJobsRepo.Update(ctx, job)
							// Best-effort: update lens scope so UI/graph doesn't keep alias paths.
							e.bestEffortRewriteLensScope(ctx, tenantID, lensID, strings.TrimSpace(old), effectiveCanonicalPath)
						}
					}
				}
			}
		}

		// Only create if we didn't find a duplicate
		if !usedExistingDuplicate {
			// Race guard: another worker/job might have created this fragment after our first GetByCanonicalPath.
			// Firestore doesn't enforce uniqueness on canonicalPath, so re-check right before Create.
			if cp := strings.TrimSpace(extractedFragment.CanonicalPath); cp != "" {
				if reread, rerr := e.fragmentsRepo.GetByCanonicalPath(ctx, cp); rerr == nil && reread != nil && strings.TrimSpace(reread.ID) != "" {
					usedExistingDuplicate = true
					existingFragment = reread
					fragmentID = reread.ID

					// Best-effort: apply the newly extracted state onto the existing fragment.
					// This keeps the current scouting run effective even when creation raced.
					existingFragment.State = extractedFragment.State
					existingFragment.Metadata.LastUpdatedAt = time.Now()
					existingFragment.Metadata.LastVerifiedAt = time.Now()
					existingFragment.Metadata.SourceFrameID = frameID
					existingFragment.Metadata.EpistemicStatus = extractedFragment.Metadata.EpistemicStatus

					// Merge OrgSize: fill if existing is empty/unknown ("na")
					if strings.TrimSpace(extractedFragment.OrgSize) != "" {
						cur := strings.TrimSpace(strings.ToLower(existingFragment.OrgSize))
						in := strings.TrimSpace(extractedFragment.OrgSize)
						if cur == "" || cur == "na" {
							existingFragment.OrgSize = in
						}
					}

					// Merge MarketSegments: union + dedup
					if len(extractedFragment.MarketSegments) > 0 {
						if existingFragment.MarketSegments == nil {
							existingFragment.MarketSegments = make([]string, 0, len(extractedFragment.MarketSegments))
						}
						seen := make(map[string]bool)
						for _, s := range existingFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s != "" {
								seen[s] = true
							}
						}
						for _, s := range extractedFragment.MarketSegments {
							s = strings.TrimSpace(s)
							if s == "" {
								continue
							}
							if seen[s] {
								continue
							}
							seen[s] = true
							existingFragment.MarketSegments = append(existingFragment.MarketSegments, s)
						}
					}

					// Normalize display label again (safe; no canonical changes).
					if existingFragment.Labels == nil {
						existingFragment.Labels = map[string]string{}
					}
					if strings.TrimSpace(existingFragment.Labels["en"]) == "" {
						seed := strings.TrimSpace(extractedFragment.Labels["en"])
						if seed == "" {
							seed = strings.TrimSpace(extractedFragment.Identifiers["legalName"])
						}
						if seed == "" {
							seed = strings.TrimSpace(extractedFragment.Identifiers["shortName"])
						}
						existingFragment.Labels["en"] = bestEffortDisplayLabel(seed, cp)
					} else {
						existingFragment.Labels["en"] = bestEffortDisplayLabel(existingFragment.Labels["en"], cp)
					}

					// Conservative Wikidata enrichment (QID-known only) to keep names/industryPaths filled when possible.
					if e.wikidataClient != nil {
						_, _ = henrich.EnrichFragmentFromWikidataIfPresent(ctx, e.wikidataClient, existingFragment, henrich.ConservativeWikidataOptions{
							Force:     false,
							Overwrite: true,
						})
					}

					if uerr := e.fragmentsRepo.Update(ctx, existingFragment); uerr != nil {
						return fmt.Errorf("failed to update fragment after race-guard hit: %w", uerr)
					}
				}
			}
			if usedExistingDuplicate {
				// Skip Create path; we've adopted an existing fragment.
			} else {
				fragmentID, err = e.fragmentsRepo.Create(ctx, extractedFragment)
				if err != nil {
					return fmt.Errorf("failed to create fragment: %w", err)
				}
				extractedFragment.ID = fragmentID
				existingFragment = extractedFragment

				// Segment-first linking: materialize derived market nodes from org/fac marketSegments (best-effort).
				e.ensureSegmentDerivedMarketLinks(ctx, existingFragment, "scout_job", job.ID)
			}
			// Segment-first linking also applies when we adopted an existing fragment (dedup path).
			if usedExistingDuplicate {
				e.ensureSegmentDerivedMarketLinks(ctx, existingFragment, "scout_job", job.ID)
			}
		}
	} else {
		// Обновляем существующий фрагмент
		existingFragment.State = extractedFragment.State

		// Merge OrgSize: fill if existing is empty/unknown ("na")
		if strings.TrimSpace(extractedFragment.OrgSize) != "" {
			cur := strings.TrimSpace(strings.ToLower(existingFragment.OrgSize))
			in := strings.TrimSpace(extractedFragment.OrgSize)
			if cur == "" || cur == "na" {
				existingFragment.OrgSize = in
			}
		}

		// Merge MarketSegments: union + dedup
		if len(extractedFragment.MarketSegments) > 0 {
			if existingFragment.MarketSegments == nil {
				existingFragment.MarketSegments = make([]string, 0, len(extractedFragment.MarketSegments))
			}
			seen := make(map[string]bool)
			for _, s := range existingFragment.MarketSegments {
				s = strings.TrimSpace(s)
				if s != "" {
					seen[s] = true
				}
			}
			for _, s := range extractedFragment.MarketSegments {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				if seen[s] {
					continue
				}
				seen[s] = true
				existingFragment.MarketSegments = append(existingFragment.MarketSegments, s)
			}
		}

		// Best-effort: normalize display label on update as well (helps fix older lowercase-only stubs over time).
		if existingFragment.Labels == nil {
			existingFragment.Labels = map[string]string{}
		}
		if strings.TrimSpace(existingFragment.Labels["en"]) == "" {
			seed := strings.TrimSpace(extractedFragment.Labels["en"])
			if seed == "" {
				seed = strings.TrimSpace(extractedFragment.Identifiers["legalName"])
			}
			if seed == "" {
				seed = strings.TrimSpace(extractedFragment.Identifiers["shortName"])
			}
			existingFragment.Labels["en"] = bestEffortDisplayLabel(seed, effectiveCanonicalPath)
		} else {
			existingFragment.Labels["en"] = bestEffortDisplayLabel(existingFragment.Labels["en"], effectiveCanonicalPath)
		}
		existingFragment.Metadata.LastUpdatedAt = time.Now()
		existingFragment.Metadata.LastVerifiedAt = time.Now()
		existingFragment.Metadata.SourceFrameID = frameID
		existingFragment.Metadata.EpistemicStatus = models.EpistemicStatusVerified
		if validationResult.Confidence < 0.7 {
			existingFragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}

		// IMPORTANT: Re-scout of an existing fragment should also (re)apply conservative Wikidata enrichment
		// when a QID is already present, so industryPaths and canonical names stay filled.
		// This is deterministic and does not perform Wikidata search.
		if e.wikidataClient != nil {
			_, _ = henrich.EnrichFragmentFromWikidataIfPresent(ctx, e.wikidataClient, existingFragment, henrich.ConservativeWikidataOptions{
				Force:     false,
				Overwrite: true,
			})
		}

		if err := e.fragmentsRepo.Update(ctx, existingFragment); err != nil {
			return fmt.Errorf("failed to update fragment: %w", err)
		}
		fragmentID = existingFragment.ID

		// Segment-first linking: materialize derived market nodes from org/fac marketSegments (best-effort).
		e.ensureSegmentDerivedMarketLinks(ctx, existingFragment, "scout_job", job.ID)
	}

	// Auto-enrich fragment with external hierarchy (Wikidata/Geo/Org)
	// This runs on both create and update to ensure best-effort enrichment even for existing fragments.
	if existingFragment != nil {
		e.enrichFragmentHierarchy(ctx, existingFragment, job.ID)
	}

	// Emit minimal ontology facts from Scout's LLM output/state (best-effort).
	// This is intentionally small-volume: 1 fact per root job, focusing on risk metrics.
	_ = e.emitMinimalFactsFromScoutRun(ctx, job, existingFragment, frameID)

	// Intent-based sphere linking: ensure sphere fragments and INFLUENCES edges exist for this target
	// when the Lens requested spherePaths. This is intentionally independent of ontology facts/metrics
	// so the UI can group/cluster entities by sphere even in early/partial graphs.
	if err := e.applyIntentSphereLinks(ctx, job, existingFragment); err != nil {
		// Best effort: do not fail job completion for linkage issues.
		fmt.Printf("[ScoutEngine] Failed to apply intent sphere links for %s: %v\n", job.TargetCanonicalPath, err)
	}

	// Ontology-first intent decomposition: propose reusable relations/facts even when local corpus is empty.
	// This seeds exploration for the requested spheres and makes future similar queries faster.
	if err := e.seedOntologyFromIntent(ctx, job, existingFragment); err != nil {
		fmt.Printf("[ScoutEngine] Intent decomposition failed for %s: %v\n", job.TargetCanonicalPath, err)
	}

	// Realtime: job completed + node materialized/updated.
	e.publish(ctx, realtime.EventType("horizon.scout.job.completed"), map[string]interface{}{
		"tenantId":        tenantID,
		"projectId":       projectID,
		"lensId":          lensID,
		"jobId":           job.ID,
		"fragmentId":      fragmentID,
		"canonicalPath":   strings.TrimSpace(job.TargetCanonicalPath),
		"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
		"confidence":      validationResult.Confidence,
	})

	// Check if all scout jobs for this lens are completed and publish final assembly.ready event
	e.checkAndPublishAssemblyCompletion(ctx, tenantID, projectID, lensID)

	e.publish(ctx, realtime.EventType("horizon.graph.node.upserted"), map[string]interface{}{
		"tenantId":      tenantID,
		"projectId":     projectID,
		"lensId":        lensID,
		"fragmentId":    fragmentID,
		"canonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"status": func() string {
			if existingFragment.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence {
				return "low_confidence"
			}
			return "verified"
		}(),
		"epistemicStatus": string(existingFragment.Metadata.EpistemicStatus),
		"version":         existingFragment.Version,
	})

	// DECOMPOSITION: Обработка найденных связей (рекурсивный скаутинг)
	if len(scoutResult.Relations) > 0 {
		// Используем existingFragment, так как он содержит актуальный ID после сохранения
		if err := e.processDiscoveredRelations(ctx, job, existingFragment, scoutResult.Relations); err != nil {
			fmt.Printf("Decomposition failed: %v\n", err)
		}
	}

	// SUPPLY CHAIN: Parse and persist supply chain fragments and edges
	if len(scoutResult.SupplyChains) > 0 {
		if err := e.processSupplyChains(ctx, job, existingFragment, scoutResult.SupplyChains); err != nil {
			fmt.Printf("[ScoutEngine] Supply chain processing failed: %v\n", err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> supply chain processing failed: %v", err))
		}
	}

	// Запускаем распространение изменений в графе
	if e.propagationTrigger != nil {
		rc, _ := rtctx.Get(ctx)
		// Запускаем в отдельной горутине, чтобы не блокировать завершение задачи
		go func(fid string) {
			// Используем фоновый контекст с таймаутом
			propBase := context.Background()
			if strings.TrimSpace(rc.TenantID) != "" || strings.TrimSpace(rc.ProjectID) != "" || strings.TrimSpace(rc.LensID) != "" || strings.TrimSpace(rc.CorrelationID) != "" || strings.TrimSpace(rc.SessionID) != "" {
				propBase = rtctx.With(propBase, rc)
			}
			propCtx, cancel := context.WithTimeout(propBase, 5*time.Minute)
			defer cancel()
			if err := e.propagationTrigger(propCtx, fid); err != nil {
				// Логируем ошибку (в реальном коде нужен logger)
				fmt.Printf("Propagation trigger failed for fragment %s: %v\n", fid, err)
			}
		}(fragmentID)
	}

	return nil
}

func (e *ScoutEngine) emitMinimalFactsFromScoutRun(ctx context.Context, job *models.ScoutingJob, fragment *models.HorizonFragment, frameID string) error {
	if e == nil || job == nil || fragment == nil {
		return nil
	}
	// Avoid exponential growth: only root jobs.
	if job.State != nil {
		if v, ok := job.State["recursionDepth"].(float64); ok && int(v) > 0 {
			return nil
		}
		if v, ok := job.State["scoutFactsEmitted"].(bool); ok && v {
			return nil
		}
	}
	if e.factEmitter == nil && (e.planner == nil || e.planner.factsRepo == nil) {
		return nil
	}

	// Collect only risk metrics to keep it minimal.
	type pair struct {
		name string
		mv   models.MetricValue
	}
	riskMetrics := make([]pair, 0)
	for k, mv := range fragment.State {
		k2 := strings.TrimSpace(k)
		if strings.HasPrefix(k2, "/metric/risk/") {
			riskMetrics = append(riskMetrics, pair{name: k2, mv: mv})
		}
	}
	if len(riskMetrics) == 0 {
		return nil
	}
	if len(riskMetrics) > 12 {
		riskMetrics = riskMetrics[:12]
	}

	now := time.Now().UTC()
	title := fmt.Sprintf("Scout inferred risk metrics for %s", strings.TrimSpace(fragment.CanonicalPath))
	f := &coreModels.OntologyFact{
		BaseOntologyFields: coreModels.BaseOntologyFields{
			TenantID:       strings.TrimSpace(job.TenantID),
			Version:        1,
			CreatedAt:      now,
			UpdatedAt:      now,
			CreatedBy:      "system",
			UpdatedBy:      "system",
			Labels:         map[string]string{"en": title},
			Status:         "active",
			Tags:           []string{"scout", "llm", "metric", "risk"},
			CanonicalPaths: []string{strings.TrimSpace(fragment.CanonicalPath)},
			ActorPaths:     []string{strings.TrimSpace(fragment.CanonicalPath)},
			SpherePaths: func() []string {
				if job.State == nil {
					return nil
				}
				raw, ok := job.State["spherePaths"]
				if !ok {
					return nil
				}
				paths := []string{}
				switch t := raw.(type) {
				case []string:
					paths = append(paths, t...)
				case []interface{}:
					for _, it := range t {
						if s, ok := it.(string); ok {
							paths = append(paths, s)
						}
					}
				case string:
					for _, p := range strings.Split(t, ",") {
						paths = append(paths, p)
					}
				}
				return taxonomy.NormalizeSpherePaths(paths)
			}(),
		},
		Kind:       "metric",
		Type:       "scout_inferred_risk_metrics",
		Title:      title,
		Summary:    "Minimal metric patch emitted by Scout after LLM research; used to seed ontology_facts when sources pipeline is paused.",
		ObservedAt: &now,
		SourceRefs: []coreModels.SourceRef{
			{SourceID: strings.TrimSpace(job.ID), SourceType: coreModels.SourceTypeAgentJob},
			{SourceID: strings.TrimSpace(frameID), SourceType: coreModels.SourceTypeHorizonFrame},
		},
		Confidence: 0.6,
	}

	for _, p := range riskMetrics {
		mv := p.mv
		observed := mv.ObservedAt
		f.MetricUpdates = append(f.MetricUpdates, coreModels.FactMetricUpdate{
			MetricName: p.name,
			Value:      mv.Value,
			Unit:       mv.Unit,
			Confidence: mv.Confidence,
			ObservedAt: &observed,
		})
		if mv.Confidence > f.Confidence {
			f.Confidence = mv.Confidence
		}
	}
	f.VectorText = f.GenerateVectorText()

	rc, _ := rtctx.Get(ctx)
	// Add correlation/trace link when available (so facts can be traced back to LLM/tool telemetry).
	if v := strings.TrimSpace(rc.CorrelationID); v != "" {
		f.SourceRefs = append(f.SourceRefs, coreModels.SourceRef{SourceID: v, SourceType: coreModels.SourceTypeAITrace})
	}
	emitOpts := coreontology.EmitFactOptions{
		TenantID:         strings.TrimSpace(job.TenantID),
		ProjectID:        strings.TrimSpace(job.ProjectID),
		LensID:           strings.TrimSpace(job.LensID),
		SessionID:        strings.TrimSpace(rc.SessionID),
		CorrelationID:    strings.TrimSpace(rc.CorrelationID),
		PublishFeedEvent: true,
		Actor:            "system",
	}

	metricFactID := ""
	if e.factEmitter != nil {
		if id, err := e.factEmitter.Emit(ctx, f, emitOpts); err == nil {
			metricFactID = id
		}
	} else if e.planner != nil && e.planner.factsRepo != nil {
		if id, err := e.planner.factsRepo.Create(ctx, f); err == nil {
			metricFactID = id
		}
	}

	// Also emit a minimal signal snapshot so the system can answer "risks" queries even when source ingestion is paused.
	// This is intentionally one signal per root job to avoid feed spam.
	if job.State != nil {
		if v, ok := job.State["scoutSignalsEmitted"].(bool); ok && v {
			// nothing
		} else {
			sTitle := fmt.Sprintf("Risk snapshot for %s", strings.TrimSpace(fragment.CanonicalPath))
			parts := make([]string, 0, len(riskMetrics))
			for _, p := range riskMetrics {
				parts = append(parts, fmt.Sprintf("%s=%.2f", p.name, p.mv.Value))
			}
			sort.Strings(parts)
			sSummary := "Risk metrics snapshot synthesized by Scout from LLM state. " + strings.Join(parts, "; ")

			s := &coreModels.OntologyFact{
				BaseOntologyFields: coreModels.BaseOntologyFields{
					TenantID:       strings.TrimSpace(job.TenantID),
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
					CreatedBy:      "system",
					UpdatedBy:      "system",
					Labels:         map[string]string{"en": sTitle},
					Status:         "active",
					Tags:           []string{"scout", "llm", "signal", "risk"},
					CanonicalPaths: []string{strings.TrimSpace(fragment.CanonicalPath)},
					ActorPaths:     []string{strings.TrimSpace(fragment.CanonicalPath)},
					SpherePaths:    f.SpherePaths,
				},
				Kind:       "signal",
				Type:       "risk_snapshot",
				Title:      sTitle,
				Summary:    sSummary,
				ObservedAt: &now,
				SourceRefs: []coreModels.SourceRef{
					{SourceID: strings.TrimSpace(job.ID), SourceType: coreModels.SourceTypeAgentJob},
					{SourceID: strings.TrimSpace(frameID), SourceType: coreModels.SourceTypeHorizonFrame},
				},
				Confidence: f.Confidence,
			}
			if strings.TrimSpace(metricFactID) != "" {
				s.SourceRefs = append(s.SourceRefs, coreModels.SourceRef{SourceID: strings.TrimSpace(metricFactID), SourceType: coreModels.SourceTypeOntologyFact})
			}
			if v := strings.TrimSpace(rc.CorrelationID); v != "" {
				s.SourceRefs = append(s.SourceRefs, coreModels.SourceRef{SourceID: v, SourceType: coreModels.SourceTypeAITrace})
			}
			s.VectorText = s.GenerateVectorText()

			if e.factEmitter != nil {
				if _, err := e.factEmitter.Emit(ctx, s, emitOpts); err != nil {
					fmt.Printf("[ScoutEngine] Failed to emit signal snapshot fact (best-effort): jobId=%s target=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
				}
			} else if e.planner != nil && e.planner.factsRepo != nil {
				if _, err := e.planner.factsRepo.Create(ctx, s); err != nil {
					fmt.Printf("[ScoutEngine] Failed to persist signal snapshot fact (best-effort): jobId=%s target=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
				}
			}
			job.State["scoutSignalsEmitted"] = true
		}
	}

	if job.State != nil {
		job.State["scoutFactsEmitted"] = true
		if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
			fmt.Printf("[ScoutEngine] Failed to persist job state scoutFactsEmitted (best-effort): jobId=%s target=%s err=%v\n",
				strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
		}
	}
	return nil
}

func (e *ScoutEngine) seedOntologyFromIntent(ctx context.Context, job *models.ScoutingJob, sourceFragment *models.HorizonFragment) error {
	if e == nil || job == nil || sourceFragment == nil || strings.TrimSpace(sourceFragment.ID) == "" {
		return nil
	}
	if e.aiExecutor == nil {
		return nil
	}
	if job.State == nil {
		return nil
	}
	// Only run on root jobs to avoid exponential growth; recursion inherits intent for grounded scouting.
	if v, ok := job.State["recursionDepth"].(float64); ok && int(v) > 0 {
		return nil
	}
	if v, ok := job.State["intentDecompositionDone"].(bool); ok && v {
		return nil
	}

	focus := ""
	if v, ok := job.State["focus"].(string); ok {
		focus = strings.TrimSpace(v)
	}
	// keywords (optional)
	keywords := []string{}
	if v, ok := job.State["keywords"]; ok {
		switch t := v.(type) {
		case []string:
			keywords = append(keywords, t...)
		case []interface{}:
			for _, it := range t {
				if s, ok2 := it.(string); ok2 {
					keywords = append(keywords, s)
				}
			}
		case string:
			for _, p := range strings.Split(t, ",") {
				keywords = append(keywords, p)
			}
		}
	}
	// spherePaths (optional)
	spherePaths := []string{}
	if v, ok := job.State["spherePaths"]; ok {
		switch t := v.(type) {
		case []string:
			spherePaths = append(spherePaths, t...)
		case []interface{}:
			for _, it := range t {
				if s, ok2 := it.(string); ok2 {
					spherePaths = append(spherePaths, s)
				}
			}
		case string:
			for _, p := range strings.Split(t, ",") {
				spherePaths = append(spherePaths, p)
			}
		}
	}
	spherePaths = taxonomy.NormalizeSpherePaths(spherePaths)

	// Extract originalQuery and decompositionHints for MARKET-aware decomposition
	originalQuery := ""
	var decompositionHints []agents.DecompositionHint
	if v, ok := job.State["originalQuery"].(string); ok {
		originalQuery = strings.TrimSpace(v)
	}
	if v, ok := job.State["decompositionHints"]; ok && v != nil {
		if dhBytes, err := json.Marshal(v); err == nil {
			if err := json.Unmarshal(dhBytes, &decompositionHints); err != nil {
				fmt.Printf("[ScoutEngine] Failed to parse decompositionHints for intent seeding (best-effort): jobId=%s target=%s err=%v\n",
					strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
			}
		}
	}

	// Check if this is an abstract anchor (MARKET/PROGRAM)
	isAbstract := strings.HasPrefix(job.TargetCanonicalPath, "/market/") ||
		strings.HasPrefix(job.TargetCanonicalPath, "/program/")

	// Guardrail: do NOT run intent decomposition unless we have any intent signal.
	// This is important to avoid changing behavior for ad-hoc "Scout it" / RefreshFragment calls
	// that may target MARKET/PROGRAM nodes without any user intent context.
	hasIntent := focus != "" || len(keywords) > 0 || len(spherePaths) > 0 || originalQuery != "" || len(decompositionHints) > 0
	if !hasIntent {
		return nil
	}

	agent := agents.NewIntentDecomposerAgent(e.aiExecutor)
	opts := agents.DecomposeOptions{
		IsAbstractAnchor:   isAbstract,
		DecompositionHints: decompositionHints,
		OriginalQuery:      originalQuery,
	}
	rels, err := agent.DecomposeWithOptions(ctx, strings.TrimSpace(job.TargetCanonicalPath), focus, spherePaths, keywords, opts)
	if err != nil {
		return err
	}
	if len(rels) == 0 {
		job.State["intentDecompositionDone"] = true
		if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
			fmt.Printf("[ScoutEngine] Failed to persist intentDecompositionDone (best-effort): jobId=%s target=%s err=%v\n",
				strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
		}
		return nil
	}

	// Persist reusable ontology facts (kind=relation) so future queries can reuse them (and Vertex can index them).
	if e.factEmitter != nil || (e.planner != nil && e.planner.factsRepo != nil) {
		now := time.Now().UTC()
		for _, r := range rels {
			targetCP := strings.TrimSpace(r.TargetCanonicalPath)
			if targetCP == "" {
				continue
			}
			rt := strings.TrimSpace(taxonomy.NormalizeRelationType(r.RelationType))
			title := fmt.Sprintf("%s %s %s", strings.TrimSpace(job.TargetCanonicalPath), rt, targetCP)
			f := &coreModels.OntologyFact{
				BaseOntologyFields: coreModels.BaseOntologyFields{
					TenantID:       strings.TrimSpace(job.TenantID),
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
					CreatedBy:      "system",
					UpdatedBy:      "system",
					Labels:         map[string]string{"en": title},
					Status:         "active",
					Tags:           []string{"hypothesis", "intent", "relation"},
					CanonicalPaths: []string{strings.TrimSpace(job.TargetCanonicalPath), targetCP},
					ActorPaths:     []string{strings.TrimSpace(job.TargetCanonicalPath), targetCP},
					SpherePaths:    spherePaths,
					VectorText:     "",
					LocationPath:   nil,
					Description:    "",
				},
				Kind:       "relation",
				Type:       rt,
				Title:      title,
				Summary:    strings.TrimSpace(r.Reason),
				ObservedAt: &now,
				SourceRefs: []coreModels.SourceRef{{SourceID: strings.TrimSpace(job.ID), SourceType: coreModels.SourceTypeAgentJob}},
				Confidence: func() float64 {
					c := r.Confidence
					if c <= 0 || c > 0.65 {
						return 0.35
					}
					return c
				}(),
			}
			// Add correlation/trace link when available (so facts can be traced back to LLM/tool telemetry).
			if rc, ok := rtctx.Get(ctx); ok {
				if v := strings.TrimSpace(rc.CorrelationID); v != "" {
					f.SourceRefs = append(f.SourceRefs, coreModels.SourceRef{SourceID: v, SourceType: coreModels.SourceTypeAITrace})
				}
			}
			f.VectorText = f.GenerateVectorText()
			if e.factEmitter != nil {
				rc, _ := rtctx.Get(ctx)
				if _, err := e.factEmitter.Emit(ctx, f, coreontology.EmitFactOptions{
					TenantID:         strings.TrimSpace(job.TenantID),
					ProjectID:        strings.TrimSpace(job.ProjectID),
					LensID:           strings.TrimSpace(job.LensID),
					SessionID:        strings.TrimSpace(rc.SessionID),
					CorrelationID:    strings.TrimSpace(rc.CorrelationID),
					PublishFeedEvent: true,
					Actor:            "system",
				}); err != nil { // best effort
					fmt.Printf("[ScoutEngine] Failed to emit intent relation fact (best-effort): jobId=%s target=%s type=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), strings.TrimSpace(r.RelationType), err)
				}
			} else if e.planner != nil && e.planner.factsRepo != nil {
				if _, err := e.planner.factsRepo.Create(ctx, f); err != nil { // best effort
					fmt.Printf("[ScoutEngine] Failed to persist intent relation fact (best-effort): jobId=%s target=%s type=%s err=%v\n",
						strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), strings.TrimSpace(r.RelationType), err)
				}
			}
		}
	}

	// Materialize hypothesis edges/fragments and schedule recursive scouting.
	if err := e.processHypothesisRelations(ctx, job, sourceFragment, rels); err != nil {
		return err
	}

	job.State["intentDecompositionDone"] = true
	if err := e.scoutingJobsRepo.Update(ctx, job); err != nil {
		fmt.Printf("[ScoutEngine] Failed to persist intentDecompositionDone (best-effort): jobId=%s target=%s err=%v\n",
			strings.TrimSpace(job.ID), strings.TrimSpace(job.TargetCanonicalPath), err)
	}
	return nil
}

// applyIntentSphereLinks ensures the graph contains sphere dimension nodes and edges requested by Lens/job intent.
// This is a lightweight linkage (not an extracted fact) and should be treated as a UI/analysis hint.
func (e *ScoutEngine) applyIntentSphereLinks(ctx context.Context, job *models.ScoutingJob, target *models.HorizonFragment) error {
	if e == nil || job == nil || target == nil || target.ID == "" {
		return nil
	}
	if job.State == nil {
		return nil
	}
	raw, ok := job.State["spherePaths"]
	if !ok {
		return nil
	}

	// Parse to []string (accept []string, []any, comma-separated string).
	paths := []string{}
	switch t := raw.(type) {
	case []string:
		paths = append(paths, t...)
	case []interface{}:
		for _, it := range t {
			if s, ok := it.(string); ok {
				paths = append(paths, s)
			}
		}
	case string:
		for _, p := range strings.Split(t, ",") {
			paths = append(paths, p)
		}
	}

	now := time.Now().UTC()
	for _, sp := range paths {
		sp = strings.TrimSpace(sp)
		if sp == "" {
			continue
		}

		// Ensure sphere fragment exists.
		sfrag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, sp)
		if err != nil || sfrag == nil {
			sfrag = &models.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: sp,
				Type:          models.FragmentTypeSphere,
				Status:        "active",
				Labels:        map[string]string{"en": sp},
				EntityPath:    "/Influence/Sphere",
				SectorPath:    "/Influence",
				State:         make(map[string]models.MetricValue),
				Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
				Provenance: models.Provenance{
					Method:    "lens_intent_sphere_link",
					SourceIDs: []string{strings.TrimSpace(job.ID)},
				},
				Metadata: models.FragmentMetadata{
					CreatedAt:     now,
					LastUpdatedAt: now,
				},
			}
			id, cerr := e.fragmentsRepo.Create(ctx, sfrag)
			if cerr != nil {
				// If create races or fails for other reasons, re-read once.
				if reread, rerr := e.fragmentsRepo.GetByCanonicalPath(ctx, sp); rerr == nil && reread != nil {
					sfrag = reread
				} else {
					return cerr
				}
			} else {
				sfrag.ID = id
			}
		}

		if sfrag.ID == "" || sfrag.ID == target.ID {
			continue
		}

		// Ensure target fragment carries this sphere coordinate in its indexPaths (best-effort).
		// This enables deterministic "map by sphere" without requiring ontology facts or LLM.
		hasSphere := false
		for _, ip := range target.IndexPaths {
			if ip == sp {
				hasSphere = true
				break
			}
		}
		if !hasSphere && e.fragmentsRepo != nil {
			if err := e.fragmentsRepo.UpdateIndexPaths(ctx, target.ID, []string{sp}); err != nil { // best effort
				fmt.Printf("[ScoutEngine] Failed to UpdateIndexPaths (best-effort): fragmentId=%s target=%s indexPath=%s err=%v\n",
					strings.TrimSpace(target.ID), strings.TrimSpace(target.CanonicalPath), sp, err)
			}
			// Keep local copy consistent for subsequent iterations in this job.
			target.IndexPaths = models.MergeIndexPaths(target, []string{sp})
		}

		edgeType := models.DependencyTypeInfluences
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sfrag.ID, target.ID, edgeType); err == nil {
			continue
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sfrag.ID,
			TargetFragmentID: target.ID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   0.6,
				Amplification: 1.0,
				LatencyHours:  1,
			},
			Qualifiers: map[string]interface{}{
				"dimension": "sphere",
				"path":      sp,
				"source":    "lens_intent",
				"lensId":    strings.TrimSpace(job.LensID),
				"cascadeRootJobId": func() string {
					if v, ok := job.State["cascadeRootJobId"].(string); ok {
						return strings.TrimSpace(v)
					}
					return ""
				}(),
			},
			Provenance: models.Provenance{
				Method:    "lens_intent_sphere_link",
				SourceIDs: []string{strings.TrimSpace(job.ID)},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     now,
				LastUpdatedAt: now,
			},
		}
		if id, err := e.edgesRepo.Create(ctx, edge); err == nil {
			edge.ID = id
			// Realtime: edge materialized/updated for UI graph.
			e.publish(ctx, realtime.EventType("horizon.graph.edge.upserted"), map[string]interface{}{
				"tenantId":         job.TenantID,
				"projectId":        job.ProjectID,
				"lensId":           strings.TrimSpace(job.LensID),
				"edgeId":           edge.ID,
				"sourceFragmentId": edge.SourceFragmentID,
				"targetFragmentId": edge.TargetFragmentID,
				"type":             string(edge.Type),
				"properties":       edge.Properties,
				"qualifiers":       edge.Qualifiers,
			})
		}
	}

	return nil
}

// checkAndPublishAssemblyCompletion checks if all scout jobs for a lens are completed
// and publishes horizon.assembly.ready event when all jobs are done.
func (e *ScoutEngine) checkAndPublishAssemblyCompletion(ctx context.Context, tenantID, projectID, lensID string) {
	if e == nil || e.scoutingJobsRepo == nil || lensID == "" {
		return
	}

	// Get all scout jobs for this lens
	jobs, err := e.scoutingJobsRepo.ListByProjectAndLens(ctx, tenantID, projectID, lensID, 1000)
	if err != nil {
		return // Best-effort: don't fail if we can't check
	}

	if len(jobs) == 0 {
		return // No jobs to check
	}

	// Count jobs by status
	var totalJobs, completedJobs, failedJobs, pendingJobs, runningJobs int
	for _, job := range jobs {
		if job == nil {
			continue
		}
		totalJobs++
		switch job.Status {
		case coreModels.AgentJobStatusCompleted:
			completedJobs++
		case coreModels.AgentJobStatusFailed:
			failedJobs++
		case coreModels.AgentJobStatusPending:
			pendingJobs++
		case coreModels.AgentJobStatusRunning:
			runningJobs++
		}
	}

	// If all jobs are completed or failed, publish assembly.ready
	if totalJobs > 0 && pendingJobs == 0 && runningJobs == 0 {
		progress := 1.0
		if totalJobs > 0 {
			progress = float64(completedJobs+failedJobs) / float64(totalJobs)
		}

		// Publish final progress update
		e.publish(ctx, realtime.EventType("horizon.assembly.progress"), map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"phase":     "ready",
			"progress":  progress,
			"counters": map[string]interface{}{
				"jobsTotal":   totalJobs,
				"jobsDone":    completedJobs,
				"jobsFailed":  failedJobs,
				"jobsRunning": 0,
			},
		})

		// Publish final assembly.ready event
		e.publish(ctx, realtime.EventType("horizon.assembly.ready"), map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"phase":     "ready",
			"progress":  progress,
			"counters": map[string]interface{}{
				"jobsTotal":  totalJobs,
				"jobsDone":   completedJobs,
				"jobsFailed": failedJobs,
			},
		})

		// Update lens ViewConfig with assembly completion status
		if e.lensesRepo != nil {
			lens, err := e.lensesRepo.Get(ctx, lensID)
			if err == nil && lens != nil {
				if lens.ViewConfig == nil {
					lens.ViewConfig = make(map[string]interface{})
				}
				lens.ViewConfig["assemblyStatus"] = map[string]interface{}{
					"phase":       "ready",
					"progress":    progress,
					"completedAt": time.Now().UTC().Format(time.RFC3339Nano),
					"counters": map[string]interface{}{
						"jobsTotal":  totalJobs,
						"jobsDone":   completedJobs,
						"jobsFailed": failedJobs,
					},
				}
				_ = e.lensesRepo.Update(ctx, lens) // Best-effort: don't fail if update fails
			}
		}
	} else if totalJobs > 0 {
		// Publish progress update (not final yet)
		progress := float64(completedJobs+failedJobs) / float64(totalJobs)
		e.publish(ctx, realtime.EventType("horizon.assembly.progress"), map[string]interface{}{
			"tenantId":  tenantID,
			"projectId": projectID,
			"lensId":    lensID,
			"phase":     "scouting",
			"progress":  progress,
			"counters": map[string]interface{}{
				"jobsTotal":   totalJobs,
				"jobsDone":    completedJobs,
				"jobsFailed":  failedJobs,
				"jobsRunning": runningJobs,
				"jobsPending": pendingJobs,
			},
		})
	}
}

// enrichFragmentHierarchy best-effort builds external hierarchies (Geo, Org, Product/Wikidata) for a fragment.
// It is safe to call on existing fragments (idempotent / non-destructive).
func (e *ScoutEngine) enrichFragmentHierarchy(ctx context.Context, fragment *models.HorizonFragment, jobID string) {
	if fragment == nil || fragment.ID == "" {
		return
	}

	// Auto-create geo hierarchy for GEO_REGION fragments
	if fragment.Type == models.FragmentTypeGeoRegion && e.geoHierarchyBuilder != nil {
		if strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			if err := e.geoHierarchyBuilder.EnsureAndLinkGeoHierarchy(ctx, fragment.ID, fragment.CanonicalPath, "scout_job", jobID); err != nil {
				fmt.Printf("[ScoutEngine] Warning: failed to build geo hierarchy for %s: %v\n", fragment.CanonicalPath, err)
			}
		}
	}

	// Auto-create org hierarchy for ORGANIZATION fragments (infer parent from name)
	if fragment.Type == models.FragmentTypeOrganization && e.orgHierarchyBuilder != nil {
		if err := e.orgHierarchyBuilder.EnsureOrgHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build org hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Auto-create product hierarchy for PRODUCT fragments (build value chain from Wikidata)
	if fragment.Type == models.FragmentTypeProduct && e.productHierarchyBuilder != nil {
		if err := e.productHierarchyBuilder.EnsureProductHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build product hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Auto-create market hierarchy for MARKET fragments (segment-first when possible; heuristic fallback)
	if fragment.Type == models.FragmentTypeMarket && e.marketHierarchyBuilder != nil {
		if err := e.marketHierarchyBuilder.EnsureMarketHierarchy(ctx, fragment.ID, fragment, "scout_job", jobID); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to build market hierarchy for %s: %v\n", fragment.CanonicalPath, err)
		}
	}

	// Best-effort gov type inference for /gov/... fragments (stored as ORGANIZATION)
	if strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/gov/") && e.govHierarchyBuilder != nil {
		if err := e.govHierarchyBuilder.EnsureGovHierarchy(ctx, fragment.ID, fragment); err != nil {
			fmt.Printf("[ScoutEngine] Warning: failed to infer gov hierarchy fields for %s: %v\n", fragment.CanonicalPath, err)
		}
	}
}
