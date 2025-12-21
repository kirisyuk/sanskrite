package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	ontologyModels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

func (e *ScoutEngine) applyDimensionWavesFromAppliedFacts(
	ctx context.Context,
	target *models.HorizonFragment,
	facts []*ontologyModels.OntologyFact,
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

	applyMetricUpdates := func(fragment *models.HorizonFragment, fact *ontologyModels.OntologyFact) bool {
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
		dedupHorizonRefsLocal := func(in []ontologyModels.SourceRef) []models.SourceRef {
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
		if len(f.MetricUpdates) == 0 {
			continue // wave dimensions are currently metric-driven
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
					continue
				}
				ensureInfluenceEdge(sfrag.ID, target.ID, f.Confidence, map[string]interface{}{"dimension": "sphere", "path": sp, "factId": f.ID})
				if applyMetricUpdates(sfrag, f) {
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
					if applyMetricUpdates(gfrag, f) {
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
		var facts []*ontologyModels.OntologyFact
		if e.planner != nil && e.planner.factsRepo != nil && sinceFacts != nil {
			facts, err = e.planner.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, tenantID, job.TargetCanonicalPath, sinceFacts.UTC(), 200)
			if err == nil && len(facts) > 0 {
				applier := &FactApplier{vertex: e.planner.vertexSearchClient}
				// Thresholds are conservative; can be tuned later or moved into policy doc.
				applyRes, err := applier.TryApplyFactsFastPath(ctx, e.fragmentsRepo, existingFragment, facts, 0.70 /*relevance*/, 0.80 /*fact confidence*/)
				if err == nil && applyRes != nil && applyRes.Applied {
					fmt.Printf("[ScoutEngine] Fast-path applied facts for %s: appliedFacts=%d maxRelevance=%.3f\n",
						job.TargetCanonicalPath, applyRes.AppliedFacts, applyRes.MaxRelevance)
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
					_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateVerified)
					return nil
				}
			}
		}

				// If fragment is fresh AND there are no new facts since watermark, skip expensive scouting.
				if isFresh && len(facts) == 0 {
			fmt.Printf("[ScoutEngine] Data for %s is fresh (verified %v ago) and no new facts. Skipping active scouting.\n",
				job.TargetCanonicalPath, time.Since(existingFragment.Metadata.LastVerifiedAt))
			_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateVerified)
			return nil
		}
	}

	// PLANNING: Планирование источников
	if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStatePlanning); err != nil {
		return fmt.Errorf("failed to transition to PLANNING: %w", err)
	}

	sourcePlan, err := e.planner.PlanSources(ctx, tenantID, projectID, job.TargetCanonicalPath, models.ScoutingStrategy(job.ScoutingStrategy), sinceFacts)
	if err != nil {
		_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
		return fmt.Errorf("failed to plan sources: %w", err)
	}

	// EXECUTION: Выполнение запросов к источникам
	if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateExecution); err != nil {
		return fmt.Errorf("failed to transition to EXECUTION: %w", err)
	}

	var aggregatedContent string
	var allSourceIDs []string
	var allDocumentIDs []string
	var sourceResults []*ExecuteDataSourceResult

	// Получаем источники из плана
	for _, source := range sourcePlan.Sources {
		result, err := e.executor.ExecuteDataSource(ctx, source)
		if err != nil {
			// Логируем ошибку, но продолжаем с другими источниками
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
		if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateExtraction); err != nil {
			return fmt.Errorf("failed to transition to EXTRACTION: %w", err)
		}

		// Определяем entityPath и sectorPath из targetCanonicalPath через LLM
		entityPath, sectorPath, err := e.taxonomyClassifier.ClassifyTaxonomy(ctx, job.TargetCanonicalPath)
		if err != nil {
			_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
			return fmt.Errorf("failed to classify taxonomy: %w", err)
		}

		scoutResult, err = e.scoutAgent.ExtractFragmentData(ctx, aggregatedContent, job.TargetCanonicalPath, entityPath, sectorPath)
		if err != nil {
			_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
			return fmt.Errorf("failed to extract fragment data: %w", err)
		}
		extractedFragment = &scoutResult.Fragment

		// VALIDATION: Валидация через Critic Agent
		if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateValidation); err != nil {
			return fmt.Errorf("failed to transition to VALIDATION: %w", err)
		}

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
		researchCtx := ai.WithUsageContext(ctx, tenantID, "SYSTEM", projectID, "horizon.research", lensID, cascadeID)
		enrichedFragment, newRelations, err := e.researchAgent.EnrichFragment(researchCtx, extractedFragment, validationResult.MissingInfo)
		if err == nil {
			extractedFragment = enrichedFragment
			if len(newRelations) > 0 {
				scoutResult.Relations = append(scoutResult.Relations, newRelations...)
				fmt.Printf("[ScoutEngine] Research found %d new relations.\n", len(newRelations))
			}
			// Обновляем LastVerifiedAt
			extractedFragment.Metadata.LastVerifiedAt = time.Now()
		} else {
			fmt.Printf("[ScoutEngine] Research failed: %v\n", err)
		}
	}
SKIP_RESEARCH:

	// RESOLUTION: Разрешение конфликтов если необходимо
	if validationResult.HasConflicts {
		if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateResolution); err != nil {
			return fmt.Errorf("failed to transition to RESOLUTION: %w", err)
		}

		resolutionResult, err := e.resolverAgent.Resolve(ctx, agents.ResolutionRequest{
			ScoutData:       extractedFragment,
			CriticArguments: validationResult.Conflicts,
			SourceContent:   aggregatedContent,
		})
		if err != nil {
			_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
			return fmt.Errorf("failed to resolve conflicts: %w", err)
		}

		switch resolutionResult.Verdict {
		case "Rejected":
			_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateRejected)
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
	if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateVerified); err != nil {
		return fmt.Errorf("failed to transition to VERIFIED: %w", err)
	}

	// Извлекаем Evidence из результатов источников через LLM
	evidenceExtractor := NewEvidenceExtractorLLM(e.aiExecutor)
	evidence, err := ExtractEvidence(ctx, evidenceExtractor, sourceResults, extractedFragment.State)
	if err != nil {
		// Логируем ошибку, но продолжаем без Evidence
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

	// Публикуем событие завершения
	if e.realtime != nil {
		_ = e.realtime.PublishJSON(ctx, realtime.EventType("scout.completed"), tenantID, map[string]interface{}{
			"jobId":         job.ID,
			"fragmentId":    fragmentID,
			"canonicalPath": job.TargetCanonicalPath,
			"timestamp":     time.Now().UTC(),
		})
	}

	// DECOMPOSITION: Обработка найденных связей (рекурсивный скаутинг)
	if len(scoutResult.Relations) > 0 {
		// Используем existingFragment, так как он содержит актуальный ID после сохранения
		if err := e.processDiscoveredRelations(ctx, job, existingFragment, scoutResult.Relations); err != nil {
			fmt.Printf("Decomposition failed: %v\n", err)
		}
	}

	// Запускаем распространение изменений в графе
	if e.propagationTrigger != nil {
		// Запускаем в отдельной горутине, чтобы не блокировать завершение задачи
		go func(fid string) {
			// Используем фоновый контекст с таймаутом
			propCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := e.propagationTrigger(propCtx, fid); err != nil {
				// Логируем ошибку (в реальном коде нужен logger)
				fmt.Printf("Propagation trigger failed for fragment %s: %v\n", fid, err)
			}
		}(fragmentID)
	}

	return nil
}
