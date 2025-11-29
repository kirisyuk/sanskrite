package agents

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// AgentOrchestrator управляет выполнением AgentJob с шагами на основе capabilities
type AgentOrchestrator struct {
	jobsRepo          *repo.AgentJobsRepository
	capabilityRegistry *CapabilityRegistry
	publisher         *realtime.Publisher
	logger            *log.Logger

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
	job      *models.AgentJob
	cancel   context.CancelFunc
	done     chan struct{}
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

	// Обновить финальный статус
	if err != nil {
		if o.logger != nil {
			o.logger.Printf("[AgentOrchestrator] Job %s failed: %v", jobID, err)
		}
		_ = o.jobsRepo.UpdateStatus(ctx, jobID, models.AgentJobStatusFailed, job.Progress)
		errMsg := err.Error()
		o.sendUpdate(ctx, onUpdate, jobID, job.AgentID, job.TenantID, job.ProjectID, "error", job.Progress, fmt.Sprintf("Job failed: %v", err), &errMsg)
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
	for {
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

	// Валидировать входные данные
	if err := executor.ValidateInput(step.Input); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}

	// Подготовить контекст шага
	previousOutputs := make(map[string]map[string]interface{})
	for _, depID := range step.DependsOn {
		if dep, ok := stepMap[depID]; ok && dep.Output != nil {
			previousOutputs[depID] = dep.Output
		}
	}

	stepCtx := &AgentStepContext{
		JobID:          job.ID,
		StepID:         step.ID,
		TenantID:       job.TenantID,
		ProjectID:      job.ProjectID,
		UserID:         job.UserID,
		Input:          step.Input,
		DependsOn:      step.DependsOn,
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
	o.sendUpdate(ctx, onUpdate, jobID, agentID, tenantID, projectID, stage, 0.0, message, step.Error)
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
	stepsRaw, _ := result.Output["steps"].([]interface{})

	// Конвертировать steps в AgentStep
	steps := make([]models.AgentStep, 0, len(stepsRaw))
	for _, stepRaw := range stepsRaw {
		if stepMap, ok := stepRaw.(map[string]interface{}); ok {
			step := o.parseStepFromMap(stepMap)
			steps = append(steps, step)
		}
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

	for {
		// Выполнить текущие шаги
		err := o.executeSteps(ctx, job, onUpdate)
		if err != nil && err.Error() != "deadlock: no ready steps but not all steps completed" {
			// Критичная ошибка - остановить выполнение
			return err
		}

		// Проверить, нужен ли репланинг
		if !o.shouldReplan(job) {
			// Проверить достижение цели
			if o.checkGoalAchieved(ctx, job) {
				break
			}
			
			// Если все шаги выполнены, но цель не достигнута - репланинг
			if o.allStepsCompleted(job) && job.PlanMode != models.PlanModeManual {
				if replanCount >= maxReplans {
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Max replans reached for job %s", job.ID)
					}
					break
				}
				
				if err := o.replan(ctx, job); err != nil {
					if o.logger != nil {
						o.logger.Printf("[AgentOrchestrator] Replanning failed: %v", err)
					}
					break
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
					o.logger.Printf("[AgentOrchestrator] Max replans reached for job %s", job.ID)
				}
				break
			}
			
			if err := o.replan(ctx, job); err != nil {
				if o.logger != nil {
					o.logger.Printf("[AgentOrchestrator] Replanning failed: %v", err)
				}
				break
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

		break
	}

	return nil
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
func (o *AgentOrchestrator) checkGoalAchieved(ctx context.Context, job *models.AgentJob) bool {
	if job.Goal == nil || len(job.Goal) == 0 {
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
		
		if step.Status == models.AgentStepStatusCompleted {
			completedSteps = append(completedSteps, stepMap)
		} else if step.Status == models.AgentStepStatusFailed || step.Status == models.AgentStepStatusSkipped {
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

	// Конвертировать в AgentStep
	newSteps := make([]models.AgentStep, 0, len(stepsRaw))
	for _, stepRaw := range stepsRaw {
		if stepMap, ok := stepRaw.(map[string]interface{}); ok {
			step := o.parseStepFromMap(stepMap)
			newSteps = append(newSteps, step)
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

// addSuggestedSteps добавляет предложенные шаги к job
func (o *AgentOrchestrator) addSuggestedSteps(
	ctx context.Context,
	job *models.AgentJob,
	suggestedSteps []SuggestedStep,
	stepMap map[string]*models.AgentStep,
) error {
	newSteps := make([]models.AgentStep, 0, len(suggestedSteps))
	
	for _, suggested := range suggestedSteps {
		step := models.AgentStep{
			ID:         suggested.Capability + "_" + fmt.Sprintf("%d", len(newSteps)+1), // Генерируем ID если не указан
			Capability: suggested.Capability,
			Status:     models.AgentStepStatusPending,
			IsCritical: false,
			Input:      suggested.Input,
		}
		
		newSteps = append(newSteps, step)
		stepMap[step.ID] = &newSteps[len(newSteps)-1]
	}
	
	job.Steps = append(job.Steps, newSteps...)
	job.UpdatedAt = time.Now().UTC()
	
	return o.jobsRepo.Update(ctx, job)
}

