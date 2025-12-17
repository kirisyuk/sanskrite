package scout

import (
	"context"
	"fmt"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// ScoutEngine основной движок Scout для сбора и верификации данных
type ScoutEngine struct {
	planner          *ScoutPlanner
	executor         *SourceExecutor
	scoutAgent       *agents.ScoutAgent
	criticAgent      *agents.CriticAgent
	resolverAgent    *agents.ResolverAgent
	researchAgent    *agents.ResearchAgent // New
	frameBuilder     *FrameBuilder
	workflow         *ScoutWorkflow
	fragmentsRepo    *repo.HorizonFragmentsRepository
	framesRepo       *repo.FramesRepository
	realtime            *realtime.Publisher
	taxonomyClassifier  *TaxonomyClassifier
	aiExecutor          *ai.AITaskExecutor
	propagationTrigger  func(ctx context.Context, fragmentID string) error
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
	realtime *realtime.Publisher,
	aiExecutor *ai.AITaskExecutor,
) *ScoutEngine {
	return &ScoutEngine{
		planner:            planner,
		executor:            executor,
		scoutAgent:          scoutAgent,
		criticAgent:         criticAgent,
		resolverAgent:       resolverAgent,
		researchAgent:       researchAgent,
		frameBuilder:        frameBuilder,
		workflow:            workflow,
		fragmentsRepo:       fragmentsRepo,
		framesRepo:          framesRepo,
		realtime:            realtime,
		taxonomyClassifier: NewTaxonomyClassifier(aiExecutor),
	}
}

// ExecuteScoutingJob выполняет полный цикл ScoutingJob
func (e *ScoutEngine) ExecuteScoutingJob(ctx context.Context, job *models.ScoutingJob) error {
	tenantID := job.TenantID
	projectID := job.ProjectID

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

	if aggregatedContent == "" {
		_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
		return fmt.Errorf("no content collected from sources")
	}

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

	extractedFragment, err := e.scoutAgent.ExtractFragmentData(ctx, aggregatedContent, job.TargetCanonicalPath, entityPath, sectorPath)
	if err != nil {
		_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
		return fmt.Errorf("failed to extract fragment data: %w", err)
	}

	// VALIDATION: Валидация через Critic Agent
	if err := e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateValidation); err != nil {
		return fmt.Errorf("failed to transition to VALIDATION: %w", err)
	}

	validationResult, err := e.criticAgent.Validate(ctx, extractedFragment, aggregatedContent)
	if err != nil {
		_ = e.workflow.TransitionState(ctx, job.ID, models.ScoutingJobStateFailed)
		return fmt.Errorf("failed to validate data: %w", err)
	}

	// RESEARCH: Дообогащение данных через Research Agent
	if len(validationResult.MissingInfo) > 0 {
		// Мы не переходим в отдельный стейт, это часть процесса валидации/обогащения
		enrichedFragment, err := e.researchAgent.EnrichFragment(ctx, extractedFragment, validationResult.MissingInfo)
		if err == nil {
			extractedFragment = enrichedFragment
			// После обогащения мы считаем данные более полными, но конфликты все еще могут требовать разрешения
		} else {
			// Логируем ошибку, но продолжаем с исходными данными
			// В будущем можно добавить event log
			_ = err
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
	_ = e.realtime.PublishJSON(ctx, realtime.EventType("scout.completed"), tenantID, map[string]interface{}{
		"jobId":         job.ID,
		"fragmentId":    fragmentID,
		"canonicalPath": job.TargetCanonicalPath,
		"timestamp":     time.Now().UTC(),
	})

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
