package scout

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/signals"
	coreModels "github.com/igorkirisyuk/sanskrite/internal/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

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
		_, _ = e.edgesRepo.Create(ctx, edge) // best effort
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
					_ = e.fragmentsRepo.Update(ctx, sfrag)
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
						_ = e.fragmentsRepo.Update(ctx, gfrag)
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
				_ = e.propagationTrigger(propCtx, fid)
			}()
		}
	}

	return nil
}

// ScoutEngine основной движок Scout для сбора и верификации данных
type ScoutEngine struct {
	planner            *ScoutPlanner
	executor           *SourceExecutor
	scoutAgent         *agents.ScoutAgent
	criticAgent        *agents.CriticAgent
	resolverAgent      *agents.ResolverAgent
	researchAgent      *agents.ResearchAgent // New
	frameBuilder       *FrameBuilder
	workflow           *ScoutWorkflow
	fragmentsRepo      *repo.HorizonFragmentsRepository
	framesRepo         *repo.FramesRepository
	edgesRepo          *repo.DependencyEdgesRepository
	scoutingJobsRepo   *repo.ScoutingJobsRepository
	realtime           *realtime.Publisher
	taxonomyClassifier *TaxonomyClassifier
	aiExecutor         *ai.AITaskExecutor
	spendLedger        *repo.HorizonSpendLedger
	budgetPolicy       BudgetPolicy
	propagationTrigger func(ctx context.Context, fragmentID string) error
	signalSynthesizer  *signals.Synthesizer              // Optional: for signal synthesis from facts
	factsRepo          *corerepo.OntologyFactsRepository // Optional: for saving synthesized signals
	factEmitter        *coreontology.FactEmitter         // Optional: unified fact emission (write + feed)
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

func (e *ScoutEngine) WithBudgetPolicy(policy BudgetPolicy) *ScoutEngine {
	e.budgetPolicy = policy
	return e
}

// WithFactEmitter wires a shared fact emitter for producing ontology facts from Scout results.
func (e *ScoutEngine) WithFactEmitter(emitter *coreontology.FactEmitter) *ScoutEngine {
	e.factEmitter = emitter
	return e
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
	return &ScoutEngine{
		planner:            planner,
		executor:           executor,
		scoutAgent:         scoutAgent,
		criticAgent:        criticAgent,
		resolverAgent:      resolverAgent,
		researchAgent:      researchAgent,
		frameBuilder:       frameBuilder,
		workflow:           workflow,
		fragmentsRepo:      fragmentsRepo,
		framesRepo:         framesRepo,
		edgesRepo:          edgesRepo,
		scoutingJobsRepo:   scoutingJobsRepo,
		realtime:           realtime,
		taxonomyClassifier: NewTaxonomyClassifier(aiExecutor),
		aiExecutor:         aiExecutor,
		spendLedger:        nil,
		budgetPolicy:       BudgetPolicyFromEnv(),
	}
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
	tenantID := job.TenantID
	projectID := job.ProjectID

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

	// FRESHNESS CHECK: Проверяем, есть ли свежие данные
	// Это предотвращает повторные дорогие поиски, если данные недавно обновлялись (волны/затухание)
	var sinceFacts *time.Time
	if existingFragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, job.TargetCanonicalPath); err == nil && existingFragment != nil {
		freshnessThreshold := 1 * time.Hour // Для теста. В проде может быть конфигурируемо.
		isFresh := !existingFragment.Metadata.LastVerifiedAt.IsZero() && time.Since(existingFragment.Metadata.LastVerifiedAt) < freshnessThreshold
		// Watermark for facts: prefer dedicated LastFactsSeenAt, fall back to LastVerifiedAt.
		if !existingFragment.Metadata.LastFactsSeenAt.IsZero() {
			sinceFacts = &existingFragment.Metadata.LastFactsSeenAt
		} else if !existingFragment.Metadata.LastVerifiedAt.IsZero() {
			sinceFacts = &existingFragment.Metadata.LastVerifiedAt
		}

		// FAST PATH: if we have fresh facts and they strongly match the fragment, apply deterministically without LLM.
		var facts []*coreModels.OntologyFact
		if e.planner != nil && e.planner.factsRepo != nil && sinceFacts != nil {
			facts, err = e.planner.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, job.TargetCanonicalPath, sinceFacts.UTC(), 200)
			if err == nil && len(facts) > 0 {
				applier := &FactApplier{vertex: e.planner.vertexSearchClient}
				// Thresholds are conservative; can be tuned later or moved into policy doc.
				applyRes, err := applier.TryApplyFactsFastPath(ctx, e.fragmentsRepo, existingFragment, facts, 0.70 /*relevance*/, 0.80 /*fact confidence*/)
				if err == nil && applyRes != nil && applyRes.Applied {
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
												_ = e.fragmentsRepo.Update(ctx, frag)
												// Trigger propagation for signal-derived metrics
												if e.propagationTrigger != nil {
													go func(fid string) {
														propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
														defer cancel()
														_ = e.propagationTrigger(propCtx, fid)
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
					_ = e.applyDimensionWavesFromAppliedFacts(ctx, existingFragment, facts, applyRes.AppliedFactIDs)
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
					_ = e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "fast_path_applied_facts")
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
		if isFresh && len(facts) == 0 {
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
		if job != nil && job.State != nil {
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

	var scoutResult *agents.ScoutResult
	var extractedFragment *models.HorizonFragment
	var validationResult *agents.ValidationResult

	if aggregatedContent == "" {
		fmt.Printf("[ScoutEngine] No internal content for %s. Initiating full research.\n", job.TargetCanonicalPath)
		e.logLine(ctx, job, "info", "> no internal content; initiating full research")

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
		case strings.HasPrefix(job.TargetCanonicalPath, "/org/"):
			defaultType = models.FragmentTypeOrganization
		case strings.HasPrefix(job.TargetCanonicalPath, "/person/"):
			defaultType = models.FragmentTypePerson
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

		scoutResult, err = e.scoutAgent.ExtractFragmentData(ctx, aggregatedContent, job.TargetCanonicalPath, entityPath, sectorPath)
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

		validationResult, err = e.criticAgent.Validate(ctx, extractedFragment, scoutResult.Relations, aggregatedContent)
		if err != nil {
			// Логируем ошибку, но не останавливаем процесс (fail-open с низким confidence)
			fmt.Printf("Validation error: %v\n", err)
			validationResult = &agents.ValidationResult{
				IsValid:    true,
				Confidence: 0.5,
			}
		}
	}

	// RESEARCH: Дообогащение данных через Research Agent
	if len(validationResult.MissingInfo) > 0 {
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
		if job != nil && job.State != nil {
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
		enrichedFragment, newRelations, err := e.researchAgent.EnrichFragment(researchCtx, extractedFragment, validationResult.MissingInfo, focus, spherePaths, keywords)
		if err == nil {
			extractedFragment = enrichedFragment
			if len(newRelations) > 0 {
				scoutResult.Relations = append(scoutResult.Relations, newRelations...)
				fmt.Printf("[ScoutEngine] Research found %d new relations.\n", len(newRelations))
				e.logLine(ctx, job, "info", fmt.Sprintf("> research found %d new relations", len(newRelations)))
			}
			// Обновляем LastVerifiedAt
			extractedFragment.Metadata.LastVerifiedAt = time.Now()
		} else {
			fmt.Printf("[ScoutEngine] Research failed: %v\n", err)
			e.logLine(ctx, job, "warn", fmt.Sprintf("> research failed: %v", err))
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
		if job != nil && job.State != nil {
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
			extractedFragment.State = state
		}
	}

	// VERIFIED: Создание Frame и обновление HorizonFragment
	if err := e.transition(ctx, job, models.ScoutingJobStateVerified, 1.0, "verified"); err != nil {
		return fmt.Errorf("failed to transition to VERIFIED: %w", err)
	}

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
		job.TargetCanonicalPath,
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
	existingFragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, job.TargetCanonicalPath)
	var fragmentID string
	if err != nil {
		// Создаем новый фрагмент
		extractedFragment.CanonicalPath = job.TargetCanonicalPath
		extractedFragment.TenantID = "global"
		extractedFragment.Metadata.SourceFrameID = frameID
		extractedFragment.Metadata.EpistemicStatus = models.EpistemicStatusVerified
		if validationResult.Confidence < 0.7 {
			extractedFragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}

		fragmentID, err = e.fragmentsRepo.Create(ctx, extractedFragment)
		if err != nil {
			return fmt.Errorf("failed to create fragment: %w", err)
		}
		extractedFragment.ID = fragmentID
		existingFragment = extractedFragment
	} else {
		// Обновляем существующий фрагмент
		existingFragment.State = extractedFragment.State
		existingFragment.Metadata.LastUpdatedAt = time.Now()
		existingFragment.Metadata.LastVerifiedAt = time.Now()
		existingFragment.Metadata.SourceFrameID = frameID
		existingFragment.Metadata.EpistemicStatus = models.EpistemicStatusVerified
		if validationResult.Confidence < 0.7 {
			existingFragment.Metadata.EpistemicStatus = models.EpistemicStatusLowConfidence
		}

		if err := e.fragmentsRepo.Update(ctx, existingFragment); err != nil {
			return fmt.Errorf("failed to update fragment: %w", err)
		}
		fragmentID = existingFragment.ID
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
				_, _ = e.factEmitter.Emit(ctx, s, emitOpts) // best effort
			} else if e.planner != nil && e.planner.factsRepo != nil {
				_, _ = e.planner.factsRepo.Create(ctx, s) // best effort
			}
			job.State["scoutSignalsEmitted"] = true
		}
	}

	if job.State != nil {
		job.State["scoutFactsEmitted"] = true
		_ = e.scoutingJobsRepo.Update(ctx, job)
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

	// If there's no intent signal, do nothing.
	if focus == "" && len(keywords) == 0 && len(spherePaths) == 0 {
		return nil
	}

	agent := agents.NewIntentDecomposerAgent(e.aiExecutor)
	rels, err := agent.Decompose(ctx, strings.TrimSpace(job.TargetCanonicalPath), focus, spherePaths, keywords)
	if err != nil {
		return err
	}
	if len(rels) == 0 {
		job.State["intentDecompositionDone"] = true
		_ = e.scoutingJobsRepo.Update(ctx, job)
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
				_, _ = e.factEmitter.Emit(ctx, f, coreontology.EmitFactOptions{
					TenantID:         strings.TrimSpace(job.TenantID),
					ProjectID:        strings.TrimSpace(job.ProjectID),
					LensID:           strings.TrimSpace(job.LensID),
					SessionID:        strings.TrimSpace(rc.SessionID),
					CorrelationID:    strings.TrimSpace(rc.CorrelationID),
					PublishFeedEvent: true,
					Actor:            "system",
				}) // best effort
			} else if e.planner != nil && e.planner.factsRepo != nil {
				_, _ = e.planner.factsRepo.Create(ctx, f) // best effort
			}
		}
	}

	// Materialize hypothesis edges/fragments and schedule recursive scouting.
	if err := e.processHypothesisRelations(ctx, job, sourceFragment, rels); err != nil {
		return err
	}

	job.State["intentDecompositionDone"] = true
	_ = e.scoutingJobsRepo.Update(ctx, job)
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
