package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// AgentOrchestrator управляет выполнением AgentJob с шагами на основе capabilities
type AgentOrchestrator struct {
	jobsRepo           *repo.AgentJobsRepository
	capabilityRegistry *CapabilityRegistry
	publisher          *realtime.Publisher
	logger             *log.Logger

	// Доступ к репозиториям для capabilities
	coreDocs     *repo.CoreDocsRepo
	documents    *repo.DocumentsRepository
	entities     *repo.EntityRepository
	events       *repo.EventRepository
	interactions *repo.InteractionsRepository

	// Tavily для web_search capability
	tavilyClient TavilySearchClient // Интерфейс для избежания циклического импорта

	// BigQuery для bigquery_query capability
	bigqueryClient interface{} // *gcp.BigQueryClient (используем interface{} для избежания циклического импорта)

	// GenAI для llm_generate, llm_analyze capabilities
	genaiClient interface{} // *gen_combined.Client (используем interface{} для избежания циклического импорта)

	// Активные jobs (для отслеживания)
	activeJobs map[string]*activeJobContext
	mu         sync.RWMutex
}

// activeJobContext контекст активного job
type activeJobContext struct {
	job    *models.AgentJob
	cancel context.CancelFunc
	done   chan struct{}
}

// NewAgentOrchestrator создает новый оркестратор
func NewAgentOrchestrator(
	jobsRepo *repo.AgentJobsRepository,
	capabilityRegistry *CapabilityRegistry,
	publisher *realtime.Publisher,
	logger *log.Logger,
) *AgentOrchestrator {
	return &AgentOrchestrator{
		jobsRepo:           jobsRepo,
		capabilityRegistry: capabilityRegistry,
		publisher:          publisher,
		logger:             logger,
		activeJobs:         make(map[string]*activeJobContext),
	}
}

// WithRepositories настраивает доступ к репозиториям
func (o *AgentOrchestrator) WithRepositories(
	coreDocs *repo.CoreDocsRepo,
	documents *repo.DocumentsRepository,
	entities *repo.EntityRepository,
	events *repo.EventRepository,
	interactions *repo.InteractionsRepository,
) *AgentOrchestrator {
	o.coreDocs = coreDocs
	o.documents = documents
	o.entities = entities
	o.events = events
	o.interactions = interactions
	return o
}

// WithTavily настраивает Tavily клиент для web_search
func (o *AgentOrchestrator) WithTavily(client TavilySearchClient) *AgentOrchestrator {
	o.tavilyClient = client
	return o
}

// WithBigQuery настраивает BigQuery клиент для bigquery_query
func (o *AgentOrchestrator) WithBigQuery(client interface{}) *AgentOrchestrator {
	o.bigqueryClient = client
	return o
}

// WithGenAI настраивает GenAI клиент для llm_generate, llm_analyze
func (o *AgentOrchestrator) WithGenAI(client interface{}) *AgentOrchestrator {
	o.genaiClient = client
	return o
}

// CreateJob создает новый AgentJob
func (o *AgentOrchestrator) CreateJob(
	ctx context.Context,
	tenantID, projectID, userID, agentID string,
	task map[string]interface{},
	autoMode bool,
	title string,
) (*models.AgentJob, error) {
	// Извлечь userQuery и planMode из task
	userQuery := ""
	if uq, ok := task["userQuery"].(string); ok {
		userQuery = uq
	}

	planMode := models.PlanModeManual
	if pm, ok := task["planMode"].(string); ok {
		planMode = models.PlanMode(pm)
	} else if autoMode {
		// Если autoMode включен, но planMode не указан, используем auto
		planMode = models.PlanModeAuto
	}

	job := &models.AgentJob{
		ID:        "",
		TenantID:  tenantID,
		ProjectID: projectID,
		UserID:    userID,
		AgentID:   agentID,
		Title:     title,
		Status:    models.AgentJobStatusPending,
		Progress:  0.0,
		Task:      task,
		Steps:     []models.AgentStep{},
		AutoMode:  autoMode,
		UserQuery: userQuery,
		PlanMode:  planMode,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Если есть явно заданные шаги в task, использовать их
	if stepsRaw, ok := task["steps"].([]interface{}); ok && len(stepsRaw) > 0 {
		steps := make([]models.AgentStep, 0, len(stepsRaw))
		for _, stepRaw := range stepsRaw {
			if stepMap, ok := stepRaw.(map[string]interface{}); ok {
				step := o.parseStepFromMap(stepMap)
				steps = append(steps, step)
			}
		}
		job.Steps = steps
		job.PlanMode = models.PlanModeManual // Если шаги заданы явно, используем manual режим
	}

	jobID, err := o.jobsRepo.Create(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}
	job.ID = jobID

	if o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Created job %s: agent=%s, project=%s, planMode=%s, steps=%d",
			jobID, agentID, projectID, planMode, len(job.Steps))
	}

	return job, nil
}

// parseStepFromMap парсит шаг из map
func (o *AgentOrchestrator) parseStepFromMap(stepMap map[string]interface{}) models.AgentStep {
	step := models.AgentStep{
		Status:     models.AgentStepStatusPending,
		IsCritical: false,
	}

	if id, ok := stepMap["id"].(string); ok {
		step.ID = id
	}
	if capability, ok := stepMap["capability"].(string); ok {
		step.Capability = capability
	}
	if input, ok := stepMap["input"].(map[string]interface{}); ok {
		step.Input = input
	}
	if dependsOn, ok := stepMap["dependsOn"].([]interface{}); ok {
		step.DependsOn = make([]string, 0, len(dependsOn))
		for _, dep := range dependsOn {
			if depStr, ok := dep.(string); ok {
				step.DependsOn = append(step.DependsOn, depStr)
			}
		}
	}
	if isCritical, ok := stepMap["isCritical"].(bool); ok {
		step.IsCritical = isCritical
	}

	// LLM capabilities не должны быть критичными - они могут fallback на другие методы
	if step.Capability == "llm_analyze" || step.Capability == "llm_generate" {
		step.IsCritical = false
	}

	return step
}

// ExecuteJob выполняет job с управлением шагами
func (o *AgentOrchestrator) ExecuteJob(
	ctx context.Context,
	jobID string,
	onUpdate func(Update),
) error {
	// Загрузить job
	job, err := o.jobsRepo.Get(ctx, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if job.Status != models.AgentJobStatusPending && job.Status != models.AgentJobStatusPaused {
		return fmt.Errorf("job %s is not in pending/paused status: %s", jobID, job.Status)
	}

	// Создать контекст с отменой
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Зарегистрировать активный job
	o.mu.Lock()
	o.activeJobs[jobID] = &activeJobContext{
		job:    job,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		activeJob, ok := o.activeJobs[jobID]
		if ok {
			delete(o.activeJobs, jobID)
			close(activeJob.done)
		}
		o.mu.Unlock()
	}()

	// Обновить статус на running
	if err := o.jobsRepo.UpdateStatus(ctx, jobID, models.AgentJobStatusRunning, 0.0); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	o.sendUpdate(ctx, onUpdate, jobID, job.AgentID, job.TenantID, job.ProjectID, "starting", 0.0, "Job started", nil)

	// Создать interaction для отображения выполнения в чате
	interactionID, err := o.createAgentExecutionInteraction(ctx, job)
	if err != nil && o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Failed to create interaction for job %s: %v", jobID, err)
		// Не прерываем выполнение, если не удалось создать interaction
	} else if interactionID != "" {
		// Сохранить interactionID в job
		job.InteractionID = interactionID
		_ = o.jobsRepo.Update(ctx, job)
	}

	// Если режим автономного планирования и план не создан
	if job.PlanMode == models.PlanModeAuto && len(job.Steps) == 0 {
		if err := o.generatePlan(ctx, job); err != nil {
			return fmt.Errorf("failed to generate plan: %w", err)
		}
		// Перезагрузить job после генерации плана
		job, err = o.jobsRepo.Get(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to reload job after plan generation: %w", err)
		}
	}

	// Выполнить шаги с поддержкой репланирования
	err = o.executeWithReplanning(jobCtx, job, onUpdate)

	// Перезагрузить job для получения актуальных шагов
	job, _ = o.jobsRepo.Get(ctx, jobID)

	// Обновить финальный статус
	if err != nil {
		if o.logger != nil {
			o.logger.Printf("[AgentOrchestrator] Job %s failed: %v", jobID, err)
		}
		_ = o.jobsRepo.UpdateStatus(ctx, jobID, models.AgentJobStatusFailed, job.Progress)
		errMsg := err.Error()
		o.sendUpdate(ctx, onUpdate, jobID, job.AgentID, job.TenantID, job.ProjectID, "error", job.Progress, fmt.Sprintf("Job failed: %v", err), &errMsg)

		// Обновить interaction с финальным статусом
		if job.InteractionID != "" {
			_ = o.updateAgentExecutionInteraction(ctx, job.InteractionID, job, &errMsg)
		}
		return err
	}

	// Определить финальный статус на основе результатов шагов
	finalStatus := o.determineFinalStatus(job)
	if err := o.jobsRepo.UpdateStatus(ctx, jobID, finalStatus, 1.0); err != nil {
		return fmt.Errorf("failed to update final status: %w", err)
	}

	job.Progress = 1.0
	job.Status = finalStatus
	o.sendUpdate(ctx, onUpdate, jobID, job.AgentID, job.TenantID, job.ProjectID, "completed", 1.0, "Job completed", nil)

	// Обновить interaction с финальным статусом
	if job.InteractionID != "" {
		_ = o.updateAgentExecutionInteraction(ctx, job.InteractionID, job, nil)
	}

	if o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Job %s completed with status: %s", jobID, finalStatus)
	}

	return nil
}

// executeSteps выполняет шаги job с учетом зависимостей
func (o *AgentOrchestrator) executeSteps(
	ctx context.Context,
	job *models.AgentJob,
	onUpdate func(Update),
) error {
	// Построить граф зависимостей
	stepMap := make(map[string]*models.AgentStep)
	for i := range job.Steps {
		stepMap[job.Steps[i].ID] = &job.Steps[i]
	}

	// Выполнять шаги пока есть готовые к выполнению
	iterationCount := 0
	maxIterations := 100 // Защита от бесконечного цикла
	for {
		iterationCount++
		if iterationCount > maxIterations {
			return fmt.Errorf("deadlock: exceeded max iterations (%d) in executeSteps", maxIterations)
		}

		// Перезагрузить job из базы, чтобы получить актуальные Output от завершенных шагов
		reloadedJob, reloadErr := o.jobsRepo.Get(ctx, job.ID)
		if reloadErr == nil {
			// Обновить stepMap с актуальными данными из базы
			for i := range reloadedJob.Steps {
				if existingStep, ok := stepMap[reloadedJob.Steps[i].ID]; ok {
					// Обновить Output, Status и Error из базы
					existingStep.Output = reloadedJob.Steps[i].Output
					existingStep.Status = reloadedJob.Steps[i].Status
					existingStep.Error = reloadedJob.Steps[i].Error
					existingStep.CompletedAt = reloadedJob.Steps[i].CompletedAt
				} else {
					// Если шаг не найден в stepMap, добавить его
					stepMap[reloadedJob.Steps[i].ID] = &reloadedJob.Steps[i]
				}
			}
			job = reloadedJob
		} else if o.logger != nil {
			o.logger.Printf("[AgentOrchestrator] WARNING: Failed to reload job from DB: %v", reloadErr)
		}

		// Найти шаги, готовые к выполнению (все зависимости выполнены)
		readySteps := o.findReadySteps(job.Steps, stepMap)

		if len(readySteps) == 0 {
			// Проверить, все ли шаги завершены
			allDone := true
			for _, step := range job.Steps {
				if step.Status != models.AgentStepStatusCompleted &&
					step.Status != models.AgentStepStatusFailed &&
					step.Status != models.AgentStepStatusSkipped {
					allDone = false
					break
				}
			}
			if allDone {
				break
			}
			// Если есть pending шаги, но они не готовы - ошибка планирования
			return fmt.Errorf("deadlock: no ready steps but not all steps completed")
		}

		// Выполнить готовые шаги (можно параллельно, но для простоты последовательно)
		for _, stepID := range readySteps {
			step := stepMap[stepID]
			if step.Status != models.AgentStepStatusPending {
				continue
			}

			// Выполнить шаг
			if err := o.executeStep(ctx, job, step, stepMap, onUpdate); err != nil {
				// Если критичный шаг упал - остановить job
				if step.IsCritical {
					return fmt.Errorf("critical step %s failed: %w", stepID, err)
				}
				// Некритичный шаг - пометить как skipped, чтобы зависимые шаги могли выполниться
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] Non-critical step %s failed, marking as skipped: %v", stepID, err)
				}
				now := time.Now().UTC()
				step.Status = models.AgentStepStatusSkipped
				errorMsg := err.Error()
				step.Error = &errorMsg
				step.CompletedAt = &now
				step.Message = fmt.Sprintf("Skipped due to failure: %v", err)
				_ = o.jobsRepo.UpdateStep(ctx, job.ID, step.ID, *step)
				o.sendStepUpdate(ctx, onUpdate, job.ID, job.AgentID, job.TenantID, job.ProjectID, step, "skipped")
			}
		}

		// Обновить прогресс
		progress := o.calculateProgress(job.Steps)
		job.Progress = progress
		if err := o.jobsRepo.UpdateStatus(ctx, job.ID, models.AgentJobStatusRunning, progress); err != nil {
			if o.logger != nil {
				o.logger.Printf("[AgentOrchestrator] Failed to update progress: %v", err)
			}
		}
	}

	return nil
}

// findReadySteps находит шаги, готовые к выполнению (все зависимости выполнены)
func (o *AgentOrchestrator) findReadySteps(
	steps []models.AgentStep,
	stepMap map[string]*models.AgentStep,
) []string {
	ready := []string{}

	for _, step := range steps {
		if step.Status != models.AgentStepStatusPending {
			continue
		}

		// Проверить зависимости
		allDepsReady := true
		for _, depID := range step.DependsOn {
			dep, ok := stepMap[depID]
			if !ok {
				allDepsReady = false
				break
			}
			if dep.Status != models.AgentStepStatusCompleted && dep.Status != models.AgentStepStatusSkipped {
				allDepsReady = false
				break
			}
		}

		if allDepsReady {
			ready = append(ready, step.ID)
		}
	}

	return ready
}

// executeStep выполняет один шаг
func (o *AgentOrchestrator) executeStep(
	ctx context.Context,
	job *models.AgentJob,
	step *models.AgentStep,
	stepMap map[string]*models.AgentStep,
	onUpdate func(Update),
) error {
	// Получить исполнителя capability
	executor, err := o.capabilityRegistry.Get(Capability(step.Capability))
	if err != nil {
		return fmt.Errorf("capability executor not found: %w", err)
	}

	// Подготовить контекст шага
	previousOutputs := make(map[string]map[string]interface{})
	for _, depID := range step.DependsOn {
		if dep, ok := stepMap[depID]; ok && dep.Output != nil {
			previousOutputs[depID] = dep.Output
			if o.logger != nil {
				outputKeys := make([]string, 0, len(dep.Output))
				for k := range dep.Output {
					outputKeys = append(outputKeys, k)
				}
				o.logger.Printf("[AgentOrchestrator] Added dependency %s to previousOutputs with keys: %v", depID, outputKeys)
			}
		} else {
			if o.logger != nil {
				if dep, ok := stepMap[depID]; ok {
					o.logger.Printf("[AgentOrchestrator] Dependency %s found in stepMap but Output is nil (Status: %s)", depID, dep.Status)
				} else {
					o.logger.Printf("[AgentOrchestrator] Dependency %s not found in stepMap (available steps: %v)", depID, o.getMapKeysString(stepMap))
				}
			}
		}
	}

	if o.logger != nil {
		prevKeys := make([]string, 0, len(previousOutputs))
		for k := range previousOutputs {
			prevKeys = append(prevKeys, k)
		}
		o.logger.Printf("[AgentOrchestrator] Resolving input for step %s: %v, previousOutputs keys: %v", step.ID, step.Input, prevKeys)
	}

	// Разрешить динамические ссылки в input перед валидацией
	resolvedInput := o.resolveInputReferences(step.Input, previousOutputs, stepMap)

	// Валидировать входные данные с разрешенными значениями
	if err := executor.ValidateInput(resolvedInput); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}

	stepCtx := &AgentStepContext{
		JobID:           job.ID,
		StepID:          step.ID,
		TenantID:        job.TenantID,
		ProjectID:       job.ProjectID,
		UserID:          job.UserID,
		Input:           resolvedInput, // Используем разрешенные значения
		DependsOn:       step.DependsOn,
		PreviousOutputs: previousOutputs,
	}

	// Обновить статус шага на running
	now := time.Now().UTC()
	step.Status = models.AgentStepStatusRunning
	step.StartedAt = &now
	step.Message = fmt.Sprintf("Executing %s", step.Capability)
	if err := o.jobsRepo.UpdateStep(ctx, job.ID, step.ID, *step); err != nil {
		return fmt.Errorf("failed to update step status: %w", err)
	}

	o.sendStepUpdate(ctx, onUpdate, job.ID, job.AgentID, job.TenantID, job.ProjectID, step, "running")

	// Выполнить capability
	result, err := executor.Execute(ctx, stepCtx)
	if err != nil {
		// Ошибка выполнения
		errorMsg := err.Error()
		step.Status = models.AgentStepStatusFailed
		step.Error = &errorMsg
		step.CompletedAt = &now
		_ = o.jobsRepo.UpdateStep(ctx, job.ID, step.ID, *step)
		o.sendStepUpdate(ctx, onUpdate, job.ID, job.AgentID, job.TenantID, job.ProjectID, step, "failed")
		return err
	}

	// Обновить результат шага
	step.Status = models.AgentStepStatusCompleted
	step.Output = result.Output
	step.Message = result.Message
	step.CompletedAt = &now
	if result.NextSteps != nil {
		step.NextSteps = result.NextSteps
	}
	if err := o.jobsRepo.UpdateStep(ctx, job.ID, step.ID, *step); err != nil {
		return fmt.Errorf("failed to update step result: %w", err)
	}

	o.sendStepUpdate(ctx, onUpdate, job.ID, job.AgentID, job.TenantID, job.ProjectID, step, "completed")

	if o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Step %s completed successfully", step.ID)
	}

	return nil
}

// calculateProgress вычисляет прогресс job на основе статусов шагов
func (o *AgentOrchestrator) calculateProgress(steps []models.AgentStep) float64 {
	if len(steps) == 0 {
		return 0.0
	}
	completed := 0
	for _, step := range steps {
		if step.Status == models.AgentStepStatusCompleted {
			completed++
		}
	}
	return float64(completed) / float64(len(steps))
}

// determineFinalStatus определяет финальный статус job на основе результатов шагов
func (o *AgentOrchestrator) determineFinalStatus(job *models.AgentJob) models.AgentJobStatus {
	hasFailedCritical := false
	hasFailedNonCritical := false
	allCompleted := true

	for _, step := range job.Steps {
		if step.Status == models.AgentStepStatusFailed {
			if step.IsCritical {
				hasFailedCritical = true
			} else {
				hasFailedNonCritical = true
			}
		}
		if step.Status != models.AgentStepStatusCompleted &&
			step.Status != models.AgentStepStatusFailed &&
			step.Status != models.AgentStepStatusSkipped {
			allCompleted = false
		}
	}

	if hasFailedCritical {
		return models.AgentJobStatusFailed
	}
	if hasFailedNonCritical && allCompleted {
		return models.AgentJobStatusPartial
	}
	if allCompleted {
		return models.AgentJobStatusCompleted
	}
	return models.AgentJobStatusRunning
}

// sendUpdate отправляет обновление через callback и WebSocket
func (o *AgentOrchestrator) sendUpdate(
	ctx context.Context,
	onUpdate func(Update),
	jobID, agentID, tenantID, projectID, stage string,
	progress float64,
	message string,
	err *string,
) {
	update := Update{
		TaskID:    jobID,
		AgentID:   agentID,
		AgentName: agentID, // Можно улучшить, получая имя из registry
		Stage:     stage,
		Progress:  progress,
		Message:   message,
		Timestamp: time.Now(),
		Error:     err,
	}

	if onUpdate != nil {
		onUpdate(update)
	}

	// Отправить через WebSocket
	if o.publisher != nil {
		_ = o.publisher.PublishJSON(ctx, realtime.EventAgentUpdate, tenantID, map[string]interface{}{
			"taskId":    update.TaskID,
			"agentId":   update.AgentID,
			"agentName": update.AgentName,
			"projectId": projectID,
			"stage":     update.Stage,
			"progress":  update.Progress,
			"message":   update.Message,
			"timestamp": update.Timestamp.UnixMilli(),
			"error":     update.Error,
		})
	}
}

// sendStepUpdate отправляет обновление для конкретного шага
func (o *AgentOrchestrator) sendStepUpdate(
	ctx context.Context,
	onUpdate func(Update),
	jobID, agentID, tenantID, projectID string,
	step *models.AgentStep,
	stage string,
) {
	message := step.Message
	if message == "" {
		message = fmt.Sprintf("Step %s: %s", step.ID, step.Capability)
	}

	// Отправляем общее обновление через callback
	o.sendUpdate(ctx, onUpdate, jobID, agentID, tenantID, projectID, stage, 0.0, message, step.Error)

	// Отправляем детальное обновление шага через WebSocket
	if o.publisher != nil {
		stepUpdate := map[string]interface{}{
			"jobId":      jobID,
			"stepId":     step.ID,
			"agentId":    agentID,
			"projectId":  projectID,
			"capability": step.Capability,
			"status":     string(step.Status),
			"stage":      stage,
			"message":    message,
			"timestamp":  time.Now().UTC().UnixMilli(),
		}

		// Добавляем опциональные поля
		if len(step.Output) > 0 {
			stepUpdate["output"] = step.Output
		}
		if step.Error != nil {
			stepUpdate["error"] = *step.Error
		}
		if step.ExecutionStatus != "" {
			stepUpdate["executionStatus"] = string(step.ExecutionStatus)
		}
		if step.StartedAt != nil {
			stepUpdate["startedAt"] = step.StartedAt.UnixMilli()
		}
		if step.CompletedAt != nil {
			stepUpdate["completedAt"] = step.CompletedAt.UnixMilli()
		}
		if len(step.DependsOn) > 0 {
			stepUpdate["dependsOn"] = step.DependsOn
		}
		if step.RetryCount > 0 {
			stepUpdate["retryCount"] = step.RetryCount
		}

		_ = o.publisher.PublishJSON(ctx, realtime.EventAgentStepUpdate, tenantID, stepUpdate)
	}

	// Обновить interaction с деталями шага
	if o.interactions != nil {
		// Найти interaction для этого job
		// TODO: можно хранить interactionID в job или искать по jobID в metadata
		_ = o.updateAgentExecutionInteractionStep(ctx, jobID, step)
	}
}

// createAgentExecutionInteraction создает interaction для отображения выполнения agent job в чате
func (o *AgentOrchestrator) createAgentExecutionInteraction(ctx context.Context, job *models.AgentJob) (string, error) {
	if o.interactions == nil {
		return "", fmt.Errorf("interactions repository not configured")
	}

	// Формируем content с деталями job
	content := map[string]interface{}{
		"jobId":     job.ID,
		"agentId":   job.AgentID,
		"title":     job.Title,
		"userQuery": job.UserQuery,
		"status":    string(job.Status),
		"progress":  job.Progress,
		"steps":     []interface{}{},
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("failed to marshal content: %w", err)
	}

	agentIDPtr := &job.AgentID
	interaction := &repo.Interaction{
		ID:        "",
		TenantID:  job.TenantID,
		ProjectID: job.ProjectID,
		Kind:      "agent.execution",
		Subkind:   agentIDPtr,
		Content:   string(contentJSON),
		Author: &repo.ParticipantRef{
			ParticipantID: "system",
			Kind:          "agent",
			DisplayName:   agentIDPtr,
		},
		Metadata: &repo.InteractionMetadata{},
	}

	interactionID, err := o.interactions.Create(ctx, interaction)
	if err != nil {
		return "", fmt.Errorf("failed to create interaction: %w", err)
	}

	// Публикуем событие создания interaction
	if o.publisher != nil {
		_ = o.publisher.PublishJSON(ctx, realtime.EventInteractionCreated, job.TenantID, map[string]interface{}{
			"id":        interactionID,
			"projectId": job.ProjectID,
			"kind":      "agent.execution",
			"jobId":     job.ID,
		})
	}

	return interactionID, nil
}

// updateAgentExecutionInteraction обновляет interaction при изменении статуса job
func (o *AgentOrchestrator) updateAgentExecutionInteraction(ctx context.Context, interactionID string, job *models.AgentJob, errorMsg *string) error {
	if o.interactions == nil {
		return fmt.Errorf("interactions repository not configured")
	}

	// Загрузить job с актуальными шагами
	fullJob, err := o.jobsRepo.Get(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	// Формируем content с деталями job и шагов
	stepsData := make([]map[string]interface{}, 0, len(fullJob.Steps))
	for _, step := range fullJob.Steps {
		stepData := map[string]interface{}{
			"id":         step.ID,
			"capability": step.Capability,
			"status":     string(step.Status),
			"message":    step.Message,
		}
		if step.Error != nil {
			stepData["error"] = *step.Error
		}
		if len(step.Output) > 0 {
			stepData["output"] = step.Output
		}
		if step.StartedAt != nil {
			stepData["startedAt"] = step.StartedAt.UnixMilli()
		}
		if step.CompletedAt != nil {
			stepData["completedAt"] = step.CompletedAt.UnixMilli()
		}
		stepsData = append(stepsData, stepData)
	}

	content := map[string]interface{}{
		"jobId":     fullJob.ID,
		"agentId":   fullJob.AgentID,
		"title":     fullJob.Title,
		"userQuery": fullJob.UserQuery,
		"status":    string(fullJob.Status),
		"progress":  fullJob.Progress,
		"steps":     stepsData,
	}

	if errorMsg != nil {
		content["error"] = *errorMsg
	}

	if len(fullJob.Result) > 0 {
		content["result"] = fullJob.Result
	}

	contentJSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("failed to marshal content: %w", err)
	}

	updates := map[string]interface{}{
		"content": string(contentJSON),
	}

	if err := o.interactions.Update(ctx, interactionID, updates); err != nil {
		return fmt.Errorf("failed to update interaction: %w", err)
	}

	// Публикуем событие обновления interaction
	if o.publisher != nil {
		_ = o.publisher.PublishJSON(ctx, realtime.EventInteractionUpdated, fullJob.TenantID, map[string]interface{}{
			"id":        interactionID,
			"projectId": fullJob.ProjectID,
			"kind":      "agent.execution",
			"jobId":     fullJob.ID,
		})
	}

	return nil
}

// updateAgentExecutionInteractionStep обновляет interaction при выполнении шага
func (o *AgentOrchestrator) updateAgentExecutionInteractionStep(ctx context.Context, jobID string, _ *models.AgentStep) error {
	if o.interactions == nil {
		return nil // Не критично, если репозиторий не настроен
	}

	// Загрузить job для получения interactionID
	job, err := o.jobsRepo.Get(ctx, jobID)
	if err != nil {
		return err
	}

	// Если interactionID не сохранен, попытаться найти interaction
	interactionID := job.InteractionID
	if interactionID == "" {
		// Поиск interaction по projectID и kind
		interactions, err := o.interactions.ListByProject(ctx, job.ProjectID)
		if err != nil {
			return err
		}
		for _, it := range interactions {
			if it.Kind == "agent.execution" {
				// Проверить, что это interaction для этого job
				var content map[string]interface{}
				if err := json.Unmarshal([]byte(it.Content), &content); err == nil {
					if jobIDFromContent, ok := content["jobId"].(string); ok && jobIDFromContent == jobID {
						interactionID = it.ID
						// Сохранить interactionID в job
						job.InteractionID = interactionID
						_ = o.jobsRepo.Update(ctx, job)
						break
					}
				}
			}
		}
	}

	if interactionID == "" {
		return nil // Interaction не найден, пропускаем обновление
	}

	return o.updateAgentExecutionInteraction(ctx, interactionID, job, nil)
}

// PauseJob приостанавливает выполнение job
func (o *AgentOrchestrator) PauseJob(ctx context.Context, jobID string) error {
	o.mu.Lock()
	activeJob, ok := o.activeJobs[jobID]
	o.mu.Unlock()

	if ok && activeJob != nil {
		activeJob.cancel()
	}

	return o.jobsRepo.UpdateStatus(ctx, jobID, models.AgentJobStatusPaused, 0.0)
}

// ResumeJob возобновляет выполнение job
func (o *AgentOrchestrator) ResumeJob(ctx context.Context, jobID string, onUpdate func(Update)) error {
	job, err := o.jobsRepo.Get(ctx, jobID)
	if err != nil {
		return err
	}

	if job.Status != models.AgentJobStatusPaused {
		return fmt.Errorf("job %s is not paused", jobID)
	}

	// Запустить выполнение в горутине
	go func() {
		if err := o.ExecuteJob(context.Background(), jobID, onUpdate); err != nil {
			if o.logger != nil {
				o.logger.Printf("[AgentOrchestrator] Failed to resume job %s: %v", jobID, err)
			}
		}
	}()

	return nil
}

// generatePlan генерирует план выполнения для job
func (o *AgentOrchestrator) generatePlan(ctx context.Context, job *models.AgentJob) error {
	if o.genaiClient == nil {
		return fmt.Errorf("genai client not configured")
	}

	genaiClient, ok := o.genaiClient.(*gen_combined.Client)
	if !ok {
		return fmt.Errorf("invalid genai client type")
	}

	executor := NewPlanGenerateExecutor(genaiClient, o.capabilityRegistry, o.logger)

	// Подготовить контекст для планирования
	contextInfo := map[string]interface{}{
		"tenantID":  job.TenantID,
		"projectID": job.ProjectID,
		"agentID":   job.AgentID,
	}

	stepCtx := &AgentStepContext{
		JobID:     job.ID,
		StepID:    "plan_generation",
		TenantID:  job.TenantID,
		ProjectID: job.ProjectID,
		UserID:    job.UserID,
		Input: map[string]interface{}{
			"userQuery": job.UserQuery,
			"context":   contextInfo,
		},
		PreviousOutputs: make(map[string]map[string]interface{}),
	}

	result, err := executor.Execute(ctx, stepCtx)
	if err != nil {
		return fmt.Errorf("plan generation failed: %w", err)
	}

	// Извлечь intent, goal и steps из результата
	intent, _ := result.Output["intent"].(map[string]interface{})
	goal, _ := result.Output["goal"].(map[string]interface{})
	stepsRaw, ok := result.Output["steps"].([]interface{})
	if !ok {
		if o.logger != nil {
			o.logger.Printf("[AgentOrchestrator] WARNING: steps is not []interface{}, type: %T, value: %+v", result.Output["steps"], result.Output["steps"])
		}
		stepsRaw = []interface{}{}
	}

	if o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Extracted from plan: intent=%+v, goal=%+v, stepsRaw count=%d", intent != nil, goal != nil, len(stepsRaw))
	}

	// Конвертировать steps в AgentStep
	steps := make([]models.AgentStep, 0, len(stepsRaw))
	for idx, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]interface{})
		if !ok {
			if o.logger != nil {
				o.logger.Printf("[AgentOrchestrator] WARNING: step %d is not map[string]interface{}, type: %T", idx+1, stepRaw)
			}
			continue
		}

		step := o.parseStepFromMap(stepMap)

		// Если LLM не указал dependsOn и это не первый шаг, добавляем зависимость от предыдущего шага
		if len(step.DependsOn) == 0 && idx > 0 {
			prevStepID := steps[idx-1].ID
			if prevStepID != "" {
				step.DependsOn = []string{prevStepID}
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] Auto-added dependsOn for step %s -> %s", step.ID, prevStepID)
				}
			}
		}

		if o.logger != nil {
			o.logger.Printf("[AgentOrchestrator] Parsed step %d: id=%s, capability=%s, dependsOn=%v", idx+1, step.ID, step.Capability, step.DependsOn)
		}

		steps = append(steps, step)
	}

	// Проверить ограничение на количество шагов
	if len(steps) > 15 {
		return fmt.Errorf("generated plan exceeds max steps limit: %d steps (limit: 15)", len(steps))
	}

	// Обновить job
	job.Intent = intent
	job.Goal = goal
	job.Steps = steps
	job.UpdatedAt = time.Now().UTC()

	if err := o.jobsRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job with plan: %w", err)
	}

	if o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Generated plan for job %s: %d steps", job.ID, len(steps))
	}

	return nil
}

// executeWithReplanning выполняет шаги с поддержкой репланирования
func (o *AgentOrchestrator) executeWithReplanning(
	ctx context.Context,
	job *models.AgentJob,
	onUpdate func(Update),
) error {
	maxReplans := 5 // Максимальное количество репланов
	replanCount := 0
	lastError := ""         // Для детекции повторяющихся ошибок
	repeatedErrorCount := 0 // Счетчик повторяющихся ошибок

	for {
		// Выполнить текущие шаги
		err := o.executeSteps(ctx, job, onUpdate)
		if err != nil && err.Error() != "deadlock: no ready steps but not all steps completed" {
			errMsg := err.Error()

			// Детекция повторяющихся ошибок
			if errMsg == lastError {
				repeatedErrorCount++
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] Repeated error detected (count: %d): %s", repeatedErrorCount, errMsg)
				}
				// Если та же ошибка повторяется 2 раза подряд - останавливаемся
				if repeatedErrorCount >= 2 {
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Stopping job %s due to repeated error after %d replans", job.ID, replanCount)
					}
					return fmt.Errorf("repeated error after %d replans: %s", replanCount, errMsg)
				}
			} else {
				repeatedErrorCount = 0
				lastError = errMsg
			}

			// Критичная ошибка - проверить возможность репланирования перед остановкой
			if job.PlanMode == models.PlanModeAuto && replanCount < maxReplans {
				// Перезагрузить job для получения актуального состояния
				job, _ = o.jobsRepo.Get(ctx, job.ID)
				if o.shouldReplan(job) || len(job.Steps) > 0 {
					// Попробовать репланирование
					if replanErr := o.replan(ctx, job); replanErr == nil {
						replanCount++
						job.ReplanCount = replanCount
						now := time.Now().UTC()
						job.LastReplanAt = &now
						job.ReplanTriggered = true
						job, _ = o.jobsRepo.Get(ctx, job.ID)
						continue // Попробовать выполнить новый план
					}
				}
			}
			// Если репланирование невозможно или не помогло - остановить выполнение
			return err
		} else {
			// Сброс счетчика повторяющихся ошибок при успешном выполнении
			repeatedErrorCount = 0
			lastError = ""
		}

		// Проверить, нужен ли репланинг
		if !o.shouldReplan(job) {
			// Проверить достижение цели
			if o.checkGoalAchieved(job) {
				break
			}

			// Если все шаги выполнены, но цель не достигнута - репланинг
			if o.allStepsCompleted(job) && job.PlanMode != models.PlanModeManual {
				if replanCount >= maxReplans {
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Max replans reached for job %s (limit: %d)", job.ID, maxReplans)
					}
					// Останавливаем выполнение после достижения лимита репланов
					return fmt.Errorf("max replans reached (%d) without achieving goal", maxReplans)
				}

				if err := o.replan(ctx, job); err != nil {
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Replanning failed: %v", err)
					}
					return fmt.Errorf("replanning failed: %w", err)
				}
				replanCount++
				job.ReplanCount = replanCount
				now := time.Now().UTC()
				job.LastReplanAt = &now
				job.ReplanTriggered = true

				// Перезагрузить job после реплана
				var reloadErr error
				job, reloadErr = o.jobsRepo.Get(ctx, job.ID)
				if reloadErr != nil {
					return fmt.Errorf("failed to reload job after replan: %w", reloadErr)
				}
				continue
			}
		} else {
			// Репланинг нужен из-за retry_needed или suggestsReplan
			if replanCount >= maxReplans {
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] Max replans reached for job %s (limit: %d)", job.ID, maxReplans)
				}
				// Останавливаем выполнение после достижения лимита репланов
				return fmt.Errorf("max replans reached (%d) - stopping to prevent infinite loop", maxReplans)
			}

			if err := o.replan(ctx, job); err != nil {
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] Replanning failed: %v", err)
				}
				return fmt.Errorf("replanning failed: %w", err)
			}
			replanCount++
			job.ReplanCount = replanCount
			now := time.Now().UTC()
			job.LastReplanAt = &now
			job.ReplanTriggered = true

			// Перезагрузить job после реплана
			var reloadErr error
			job, reloadErr = o.jobsRepo.Get(ctx, job.ID)
			if reloadErr != nil {
				return fmt.Errorf("failed to reload job after replan: %w", reloadErr)
			}
			continue
		}

		// Если дошли сюда и нет ошибок, но и нет прогресса - проверяем, не застряли ли мы
		if replanCount > 0 && o.allStepsCompleted(job) && !o.checkGoalAchieved(job) {
			if o.logger != nil {
				o.logger.Printf("[AgentOrchestrator] Job %s appears stuck: all steps completed but goal not achieved after %d replans", job.ID, replanCount)
			}
			break
		}

		break
	}

	return nil
}

// resolveInputReferences разрешает динамические ссылки в input на основе outputs предыдущих шагов
// Поддерживает простые ссылки типа "doc-id-from-step1" -> ищет в output step1 поле "documentIds" или "id"
func (o *AgentOrchestrator) resolveInputReferences(
	input map[string]interface{},
	previousOutputs map[string]map[string]interface{},
	stepMap map[string]*models.AgentStep,
) map[string]interface{} {
	resolved := make(map[string]interface{})

	for key, value := range input {
		resolved[key] = o.resolveValue(value, previousOutputs, stepMap)
	}

	return resolved
}

// resolveValue рекурсивно разрешает значение, заменяя placeholder-и на реальные данные
func (o *AgentOrchestrator) resolveValue(
	value interface{},
	previousOutputs map[string]map[string]interface{},
	stepMap map[string]*models.AgentStep,
) interface{} {
	switch v := value.(type) {
	case string:
		// Проверяем синтаксис шаблона ${stepId.field} или ${stepId.field[index]} или ${stepId.field[0].subfield}
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			// Извлекаем содержимое между ${ и }
			templateContent := strings.TrimPrefix(strings.TrimSuffix(v, "}"), "${")

			// Парсим шаблон: stepId.field[index].subfield или stepId.field
			stepID := ""

			// Структура для хранения пути с возможными индексами
			type pathElement struct {
				field      string
				arrayIndex int // -1 если нет индекса
			}
			var pathElements []pathElement

			// Разделяем на части по точке
			parts := strings.Split(templateContent, ".")
			if len(parts) >= 2 {
				stepID = parts[0]

				// Парсим каждую часть пути (кроме stepID)
				for _, part := range parts[1:] {
					elem := pathElement{field: part, arrayIndex: -1}

					// Проверяем, есть ли индекс массива [0], [1] и т.д.
					if idx := strings.Index(part, "["); idx != -1 {
						if idx2 := strings.Index(part, "]"); idx2 != -1 && idx2 > idx {
							elem.field = part[:idx]
							indexStr := part[idx+1 : idx2]
							var parsedIdx int
							if n, err := fmt.Sscanf(indexStr, "%d", &parsedIdx); err == nil && n == 1 && parsedIdx >= 0 {
								elem.arrayIndex = parsedIdx
							}
						}
					}
					pathElements = append(pathElements, elem)
				}
			}

			if stepID != "" && len(pathElements) > 0 {
				if output, ok := previousOutputs[stepID]; ok {
					if o.logger != nil {
						// Логируем структуру output для отладки
						outputKeys := make([]string, 0, len(output))
						for k := range output {
							outputKeys = append(outputKeys, k)
						}
						o.logger.Printf("[AgentOrchestrator] Resolving template %q: stepID=%s, pathElements=%v, output keys: %v", v, stepID, pathElements, outputKeys)
					}
					// Рекурсивно извлекаем значение по пути полей
					var currentValue interface{} = output

					// Если первый элемент пути - "output", пропускаем его (для совместимости с ${stepId.output.field})
					startIdx := 0
					if len(pathElements) > 1 && pathElements[0].field == "output" {
						startIdx = 1
					}

					for i := startIdx; i < len(pathElements); i++ {
						elem := pathElements[i]
						field := elem.field
						arrIdx := elem.arrayIndex

						if m, ok := currentValue.(map[string]interface{}); ok {
							// Получаем значение поля
							fieldValue, exists := m[field]
							if !exists {
								if o.logger != nil {
									keys := make([]string, 0, len(m))
									for k := range m {
										keys = append(keys, k)
									}
									o.logger.Printf("[AgentOrchestrator] Field %s not found in map (available keys: %v)", field, keys)
								}
								currentValue = nil
								break
							}
							currentValue = fieldValue

							// Если у этого элемента есть индекс массива, применяем его
							if arrIdx >= 0 {
								if arr, ok := currentValue.([]interface{}); ok {
									if arrIdx < len(arr) {
										currentValue = arr[arrIdx]
										if o.logger != nil {
											o.logger.Printf("[AgentOrchestrator] Applied array index [%d] to field %s, got type: %T", arrIdx, field, currentValue)
										}
									} else {
										if o.logger != nil {
											o.logger.Printf("[AgentOrchestrator] Array index [%d] out of bounds for field %s (array length: %d)", arrIdx, field, len(arr))
										}
										currentValue = nil
										break
									}
								}
							}
						} else if arr, ok := currentValue.([]interface{}); ok {
							// Если currentValue - это массив
							if arrIdx >= 0 {
								if arrIdx < len(arr) {
									currentValue = arr[arrIdx]
								} else {
									currentValue = nil
									break
								}
							} else if len(arr) > 0 {
								// Берем первый элемент по умолчанию и ищем поле
								currentValue = arr[0]
								if m, ok := currentValue.(map[string]interface{}); ok {
									if fv, exists := m[field]; exists {
										currentValue = fv
									} else {
										currentValue = nil
										break
									}
								} else {
									currentValue = nil
									break
								}
							} else {
								currentValue = nil
								break
							}
						} else {
							currentValue = nil
							break
						}
					}

					if currentValue != nil {
						// Конвертируем в строку если нужно
						if strValue, ok := currentValue.(string); ok && strValue != "" {
							if o.logger != nil {
								o.logger.Printf("[AgentOrchestrator] Resolved template %q to %q from step %s", v, strValue, stepID)
							}
							return strValue
						}
						// Если не строка, конвертируем в строку
						strValue := fmt.Sprintf("%v", currentValue)
						if strValue != "" && strValue != "<nil>" {
							if o.logger != nil {
								o.logger.Printf("[AgentOrchestrator] Resolved template %q to %q from step %s (converted to string)", v, strValue, stepID)
							}
							return strValue
						}
					}
				}
				// Если не нашли - возвращаем как есть для валидации
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] WARNING: Could not resolve template %q - will fail validation", v)
				}
				return v
			}
		}

		// Проверяем, является ли это placeholder-ом
		placeholderPatterns := []string{
			"doc-id-placeholder", "placeholder", "summary-doc-id-placeholder",
			"doc-id-from-", "id-from-", "doc-id", "summary-doc-id",
		}
		isPlaceholder := false
		for _, pattern := range placeholderPatterns {
			if strings.Contains(strings.ToLower(v), strings.ToLower(pattern)) {
				isPlaceholder = true
				break
			}
		}

		if isPlaceholder {
			// Пытаемся найти реальный ID в предыдущих шагах
			// Сначала пробуем извлечь stepID из строки
			stepID := ""
			if strings.HasPrefix(v, "doc-id-from-") {
				stepID = strings.TrimPrefix(v, "doc-id-from-")
			} else if strings.HasPrefix(v, "id-from-") {
				stepID = strings.TrimPrefix(v, "id-from-")
			} else {
				// Если нет явного указания шага, ищем во всех зависимостях
				for depID, output := range previousOutputs {
					// Сначала проверяем documentIds массив (самый надежный способ)
					if docIds, ok := output["documentIds"].([]interface{}); ok && len(docIds) > 0 {
						if id, ok := docIds[0].(string); ok {
							if o.logger != nil {
								o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s documentIds", v, id, depID)
							}
							return id
						}
					}
					// Проверяем matches массив (из data_search)
					if matches, ok := output["matches"].([]interface{}); ok && len(matches) > 0 {
						if match, ok := matches[0].(map[string]interface{}); ok {
							// Проверяем coreDocId (предпочтительно)
							if coreDocID, ok := match["coreDocId"].(string); ok && coreDocID != "" {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s matches[0].coreDocId", v, coreDocID, depID)
								}
								return coreDocID
							}
							// Проверяем documentId (алиас)
							if docId, ok := match["documentId"].(string); ok && docId != "" {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s matches[0].documentId", v, docId, depID)
								}
								return docId
							}
						}
					}
					// Проверяем results (для web_search и других)
					if results, ok := output["results"].([]interface{}); ok && len(results) > 0 {
						if result, ok := results[0].(map[string]interface{}); ok {
							if id, ok := result["id"].(string); ok {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s results", v, id, depID)
								}
								return id
							}
							if docId, ok := result["documentId"].(string); ok {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s documentId", v, docId, depID)
								}
								return docId
							}
						}
					}
				}
				// Если не нашли - это ошибка, но возвращаем как есть для валидации
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] WARNING: Could not resolve placeholder %q - will fail validation", v)
				}
				return v
			}

			// Ищем в output конкретного шага
			if stepID != "" {
				if output, ok := previousOutputs[stepID]; ok {
					// Пробуем разные поля в порядке приоритета
					// 1. documentIds массив (самый надежный)
					if docIds, ok := output["documentIds"].([]interface{}); ok && len(docIds) > 0 {
						if id, ok := docIds[0].(string); ok {
							if o.logger != nil {
								o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s documentIds", v, id, stepID)
							}
							return id
						}
					}
					// 2. matches массив
					if matches, ok := output["matches"].([]interface{}); ok && len(matches) > 0 {
						if match, ok := matches[0].(map[string]interface{}); ok {
							if coreDocID, ok := match["coreDocId"].(string); ok && coreDocID != "" {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s matches", v, coreDocID, stepID)
								}
								return coreDocID
							}
							if docId, ok := match["documentId"].(string); ok && docId != "" {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s matches", v, docId, stepID)
								}
								return docId
							}
						}
					}
					// 3. results массив
					if results, ok := output["results"].([]interface{}); ok && len(results) > 0 {
						if result, ok := results[0].(map[string]interface{}); ok {
							if id, ok := result["id"].(string); ok {
								if o.logger != nil {
									o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s results", v, id, stepID)
								}
								return id
							}
						}
					}
					// 4. Прямое поле id или documentId
					if id, ok := output["id"].(string); ok && id != "" {
						if o.logger != nil {
							o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s id", v, id, stepID)
						}
						return id
					}
					if docId, ok := output["documentId"].(string); ok && docId != "" {
						if o.logger != nil {
							o.logger.Printf("[AgentOrchestrator] Resolved placeholder %q to %q from step %s documentId", v, docId, stepID)
						}
						return docId
					}
				}
			}
			// Если не нашли - возвращаем как есть для валидации
			if o.logger != nil {
				o.logger.Printf("[AgentOrchestrator] WARNING: Could not resolve placeholder %q - will fail validation", v)
			}
			return v
		}
	case []interface{}:
		// Рекурсивно обрабатываем элементы массива
		resolved := make([]interface{}, len(v))
		for i, item := range v {
			resolved[i] = o.resolveValue(item, previousOutputs, stepMap)
		}
		return resolved
	case map[string]interface{}:
		// Рекурсивно обрабатываем значения мапы
		resolved := make(map[string]interface{})
		for key, val := range v {
			resolved[key] = o.resolveValue(val, previousOutputs, stepMap)
		}
		return resolved
	}
	return value
}

// getMapKeysString возвращает ключи мапы строк для логирования
func (o *AgentOrchestrator) getMapKeysString(m map[string]*models.AgentStep) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// shouldReplan определяет, нужен ли репланинг
func (o *AgentOrchestrator) shouldReplan(job *models.AgentJob) bool {
	// Репланинг нужен если:
	// 1. Есть шаги с ExecutionStatusRetryNeeded
	// 2. Есть шаги с SuggestsReplan = true (проверяется через CapabilityResult)

	for _, step := range job.Steps {
		if step.ExecutionStatus == models.ExecutionStatusRetryNeeded {
			return true
		}
	}
	return false
}

// checkGoalAchieved проверяет достижение цели
func (o *AgentOrchestrator) checkGoalAchieved(job *models.AgentJob) bool {
	if len(job.Goal) == 0 {
		// Если цель не задана, считаем что достигнута при выполнении всех шагов
		return o.allStepsCompleted(job)
	}

	// Можно использовать ReflectExecutor для проверки
	// Пока упрощенная проверка: все шаги выполнены успешно
	return o.allStepsCompleted(job)
}

// allStepsCompleted проверяет, все ли шаги завершены
func (o *AgentOrchestrator) allStepsCompleted(job *models.AgentJob) bool {
	for _, step := range job.Steps {
		if step.Status != models.AgentStepStatusCompleted &&
			step.Status != models.AgentStepStatusFailed &&
			step.Status != models.AgentStepStatusSkipped {
			return false
		}
	}
	return len(job.Steps) > 0
}

// replan выполняет репланирование
func (o *AgentOrchestrator) replan(ctx context.Context, job *models.AgentJob) error {
	if o.genaiClient == nil {
		return fmt.Errorf("genai client not configured")
	}

	genaiClient, ok := o.genaiClient.(*gen_combined.Client)
	if !ok {
		return fmt.Errorf("invalid genai client type")
	}

	executor := NewReplanExecutor(genaiClient, o.capabilityRegistry, o.logger)

	// Подготовить данные для реплана
	completedSteps := make([]interface{}, 0)
	failedSteps := make([]interface{}, 0)
	currentPlan := map[string]interface{}{
		"steps": make([]interface{}, 0),
	}

	for _, step := range job.Steps {
		stepMap := map[string]interface{}{
			"id":         step.ID,
			"capability": step.Capability,
			"status":     string(step.Status),
			"output":     step.Output,
		}

		switch step.Status {
		case models.AgentStepStatusCompleted:
			completedSteps = append(completedSteps, stepMap)
		case models.AgentStepStatusFailed, models.AgentStepStatusSkipped:
			failedSteps = append(failedSteps, stepMap)
		}

		// Добавить в currentPlan
		planStepMap := map[string]interface{}{
			"id":         step.ID,
			"capability": step.Capability,
			"input":      step.Input,
			"dependsOn":  step.DependsOn,
		}
		if planSteps, ok := currentPlan["steps"].([]interface{}); ok {
			currentPlan["steps"] = append(planSteps, planStepMap)
		}
	}

	stepCtx := &AgentStepContext{
		JobID:     job.ID,
		StepID:    "replan",
		TenantID:  job.TenantID,
		ProjectID: job.ProjectID,
		UserID:    job.UserID,
		Input: map[string]interface{}{
			"currentPlan":    currentPlan,
			"completedSteps": completedSteps,
			"failedSteps":    failedSteps,
			"currentGoal":    job.Goal,
		},
		PreviousOutputs: make(map[string]map[string]interface{}),
	}

	result, err := executor.Execute(ctx, stepCtx)
	if err != nil {
		return fmt.Errorf("replanning failed: %w", err)
	}

	// Извлечь обновленный план
	updatedPlanRaw, _ := result.Output["updatedPlan"].(map[string]interface{})
	stepsRaw, _ := updatedPlanRaw["steps"].([]interface{})

	// Проверить общее количество шагов (старые + новые)
	totalSteps := len(job.Steps) + len(stepsRaw)
	if totalSteps > 15 {
		return fmt.Errorf("max steps limit exceeded: %d steps (limit: 15)", totalSteps)
	}

	// Конвертировать в AgentStep с автозависимостями
	newSteps := make([]models.AgentStep, 0, len(stepsRaw))
	for idx, stepRaw := range stepsRaw {
		if stepMap, ok := stepRaw.(map[string]interface{}); ok {
			step := o.parseStepFromMap(stepMap)

			// Если dependsOn не указан, добавляем зависимости:
			// 1. От последнего завершенного шага (если есть)
			// 2. От предыдущего нового шага (если это не первый новый шаг)
			if len(step.DependsOn) == 0 {
				// Найти последний завершенный шаг
				var lastCompletedStepID string
				for i := len(job.Steps) - 1; i >= 0; i-- {
					if job.Steps[i].Status == models.AgentStepStatusCompleted {
						lastCompletedStepID = job.Steps[i].ID
						break
					}
				}

				if idx > 0 {
					// Зависимость от предыдущего нового шага
					step.DependsOn = []string{newSteps[idx-1].ID}
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Auto-added dependsOn for new replan step %s -> %s (previous new step)", step.ID, newSteps[idx-1].ID)
					}
				} else if lastCompletedStepID != "" {
					// Зависимость от последнего завершенного шага
					step.DependsOn = []string{lastCompletedStepID}
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Auto-added dependsOn for new replan step %s -> %s (last completed)", step.ID, lastCompletedStepID)
					}
				}
			}

			newSteps = append(newSteps, step)
			if o.logger != nil {
				o.logger.Printf("[AgentOrchestrator] Parsed replan step %d: id=%s, capability=%s, dependsOn=%v", idx+1, step.ID, step.Capability, step.DependsOn)
			}
		}
	}

	// Добавить новые шаги к существующим
	job.Steps = append(job.Steps, newSteps...)
	job.UpdatedAt = time.Now().UTC()

	if err := o.jobsRepo.Update(ctx, job); err != nil {
		return fmt.Errorf("failed to update job with replan: %w", err)
	}

	if o.logger != nil {
		o.logger.Printf("[AgentOrchestrator] Replanned job %s: added %d new steps", job.ID, len(newSteps))
	}

	return nil
}
