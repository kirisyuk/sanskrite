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
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

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
	propagationTrigger func(ctx context.Context, fragmentID string) error
}

// WithPropagationTrigger устанавливает триггер для запуска распространения изменений
func (e *ScoutEngine) WithPropagationTrigger(trigger func(ctx context.Context, fragmentID string) error) *ScoutEngine {
	e.propagationTrigger = trigger
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
	}
}

// ExecuteScoutingJob выполняет полный цикл ScoutingJob
func (e *ScoutEngine) ExecuteScoutingJob(ctx context.Context, job *models.ScoutingJob) error {
	tenantID := job.TenantID
	projectID := job.ProjectID

	// FRESHNESS CHECK: Проверяем, есть ли свежие данные
	// Это предотвращает повторные дорогие поиски, если данные недавно обновлялись (волны/затухание)
	if existingFragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, job.TargetCanonicalPath); err == nil && existingFragment != nil {
		freshnessThreshold := 1 * time.Hour // Для теста. В проде может быть конфигурируемо.
		if time.Since(existingFragment.Metadata.LastVerifiedAt) < freshnessThreshold {
			fmt.Printf("[ScoutEngine] Data for %s is fresh (verified %v ago). Skipping active scouting.\n",
				job.TargetCanonicalPath, time.Since(existingFragment.Metadata.LastVerifiedAt))

			// Сразу завершаем задачу
			_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateVerified)
			return nil
		}
	}

	// PLANNING: Планирование источников
	if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStatePlanning); err != nil {
		return fmt.Errorf("failed to transition to PLANNING: %w", err)
	}

	sourcePlan, err := e.planner.PlanSources(ctx, tenantID, projectID, job.TargetCanonicalPath, models.ScoutingStrategy(job.ScoutingStrategy))
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
		fmt.Printf("[ScoutEngine] Starting research for job %s, missing info: %v\n", job.ID, validationResult.MissingInfo)
		enrichedFragment, newRelations, err := e.researchAgent.EnrichFragment(ctx, extractedFragment, validationResult.MissingInfo)
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
