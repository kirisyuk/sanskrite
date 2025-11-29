package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// AgentJobsHandler обрабатывает HTTP запросы для AgentJobs (новый orchestrator-based подход)
type AgentJobsHandler struct {
	orchestrator *agents.AgentOrchestrator
	jobsRepo     *repo.AgentJobsRepository
	publisher    *realtime.Publisher
	logger       *log.Logger
}

// NewAgentJobsHandler создает новый AgentJobsHandler
func NewAgentJobsHandler(
	orchestrator *agents.AgentOrchestrator,
	jobsRepo *repo.AgentJobsRepository,
	publisher *realtime.Publisher,
	logger *log.Logger,
) *AgentJobsHandler {
	return &AgentJobsHandler{
		orchestrator: orchestrator,
		jobsRepo:     jobsRepo,
		publisher:    publisher,
		logger:       logger,
	}
}

// CreateAgentJob обрабатывает POST /api/v1/projects/{projectId}/agent-jobs
// Создает новый AgentJob и планирует шаги через агента
func (h *AgentJobsHandler) CreateAgentJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Извлекаем projectId из пути
	projectID := extractProjectID(r.URL.Path)
	if projectID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Project ID required")
		return
	}

	var req struct {
		AgentID string                 `json:"agentId"` // ID агента (например, "document_preparation")
		Task    map[string]interface{} `json:"task"`    // Задача для агента
		AutoMode bool                  `json:"autoMode,omitempty"` // Автоматический режим (default: true)
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	if req.AgentID == "" {
		WriteBadRequest(w, "MISSING_FIELD", "agentId is required")
		return
	}

	if req.Task == nil {
		req.Task = make(map[string]interface{})
	}

	autoMode := true
	if req.AutoMode == false {
		autoMode = false
	}

	// Создать job (используем UID как UserID)
	userID := auth.UID
	job, err := h.orchestrator.CreateJob(r.Context(), auth.TenantID, projectID, userID, req.AgentID, req.Task, autoMode)
	if err != nil {
		h.logger.Printf("[AgentJobsHandler] Failed to create job: %v", err)
		WriteInternalError(w, "Failed to create agent job")
		return
	}

	// TODO: Планирование шагов через агента (пока шаги должны быть добавлены отдельно или через планировщик)
	// Для начала возвращаем job без шагов - шаги можно добавить через UpdateAgentJob

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"job": job,
	})
}

// ListAgentJobs обрабатывает GET /api/v1/projects/{projectId}/agent-jobs
// Возвращает список AgentJobs для проекта
func (h *AgentJobsHandler) ListAgentJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	projectID := extractProjectID(r.URL.Path)
	if projectID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Project ID required")
		return
	}

	// Опциональные query параметры
	status := r.URL.Query().Get("status")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsedLimit, err := parseInt(l); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	var jobs []*models.AgentJob
	var err error

	if status != "" {
		statusEnum := models.AgentJobStatus(status)
		jobs, err = h.jobsRepo.ListByProjectAndStatus(r.Context(), auth.TenantID, projectID, statusEnum, limit)
	} else {
		jobs, err = h.jobsRepo.ListByProject(r.Context(), auth.TenantID, projectID, limit)
	}

	if err != nil {
		h.logger.Printf("[AgentJobsHandler] Failed to list jobs: %v", err)
		WriteInternalError(w, "Failed to list agent jobs")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": jobs,
		"count": len(jobs),
	})
}

// GetAgentJob обрабатывает GET /api/v1/projects/{projectId}/agent-jobs/{jobId}
// Возвращает детали конкретного AgentJob
func (h *AgentJobsHandler) GetAgentJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	jobID := extractJobID(r.URL.Path)
	if jobID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Job ID required")
		return
	}

	job, err := h.jobsRepo.Get(r.Context(), jobID)
	if err != nil {
		WriteNotFound(w, "Agent job not found")
		return
	}

	// Проверка доступа
	if job.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"job": job,
	})
}

// ExecuteAgentJob обрабатывает POST /api/v1/projects/{projectId}/agent-jobs/{jobId}/execute
// Запускает выполнение AgentJob
func (h *AgentJobsHandler) ExecuteAgentJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	jobID := extractJobID(r.URL.Path)
	if jobID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Job ID required")
		return
	}

	// Проверка доступа
	job, err := h.jobsRepo.Get(r.Context(), jobID)
	if err != nil {
		WriteNotFound(w, "Agent job not found")
		return
	}

	if job.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Callback для обновлений через WebSocket
	onUpdate := func(update agents.Update) {
		// Обновления уже отправляются через publisher в orchestrator
		// Здесь можно добавить дополнительную логику если нужно
		if h.logger != nil {
			h.logger.Printf("[AgentJobsHandler] Job %s update: stage=%s, progress=%.2f", jobID, update.Stage, update.Progress)
		}
	}

	// Запустить выполнение в горутине (не блокируем HTTP запрос)
	go func() {
		if err := h.orchestrator.ExecuteJob(r.Context(), jobID, onUpdate); err != nil {
			if h.logger != nil {
				h.logger.Printf("[AgentJobsHandler] Job %s execution failed: %v", jobID, err)
			}
		}
	}()

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":  jobID,
		"status": "started",
		"message": "Job execution started",
	})
}

// PauseAgentJob обрабатывает POST /api/v1/projects/{projectId}/agent-jobs/{jobId}/pause
// Приостанавливает выполнение AgentJob
func (h *AgentJobsHandler) PauseAgentJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	jobID := extractJobID(r.URL.Path)
	if jobID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Job ID required")
		return
	}

	// Проверка доступа
	job, err := h.jobsRepo.Get(r.Context(), jobID)
	if err != nil {
		WriteNotFound(w, "Agent job not found")
		return
	}

	if job.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	if err := h.orchestrator.PauseJob(r.Context(), jobID); err != nil {
		WriteInternalError(w, "Failed to pause job")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"jobId":  jobID,
		"status": "paused",
		"message": "Job paused successfully",
	})
}

// ResumeAgentJob обрабатывает POST /api/v1/projects/{projectId}/agent-jobs/{jobId}/resume
// Возобновляет выполнение AgentJob
func (h *AgentJobsHandler) ResumeAgentJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	jobID := extractJobID(r.URL.Path)
	if jobID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Job ID required")
		return
	}

	// Проверка доступа
	job, err := h.jobsRepo.Get(r.Context(), jobID)
	if err != nil {
		WriteNotFound(w, "Agent job not found")
		return
	}

	if job.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	onUpdate := func(update agents.Update) {
		// Обновления отправляются через publisher
		if h.logger != nil {
			h.logger.Printf("[AgentJobsHandler] Job %s update: stage=%s, progress=%.2f", jobID, update.Stage, update.Progress)
		}
	}

	// Запустить выполнение в горутине
	go func() {
		if err := h.orchestrator.ResumeJob(r.Context(), jobID, onUpdate); err != nil {
			if h.logger != nil {
				h.logger.Printf("[AgentJobsHandler] Job %s resume failed: %v", jobID, err)
			}
		}
	}()

	WriteJSON(w, http.StatusAccepted, map[string]interface{}{
		"jobId":  jobID,
		"status": "resumed",
		"message": "Job execution resumed",
	})
}

// UpdateAgentJobSteps обрабатывает PUT /api/v1/projects/{projectId}/agent-jobs/{jobId}/steps
// Обновляет шаги AgentJob (для ручного планирования или модификации)
func (h *AgentJobsHandler) UpdateAgentJobSteps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PUT is allowed", nil)
		return
	}

	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	jobID := extractJobID(r.URL.Path)
	if jobID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Job ID required")
		return
	}

	var req struct {
		Steps []models.AgentStep `json:"steps"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Проверка доступа
	job, err := h.jobsRepo.Get(r.Context(), jobID)
	if err != nil {
		WriteNotFound(w, "Agent job not found")
		return
	}

	if job.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Обновить шаги
	job.Steps = req.Steps
	if err := h.jobsRepo.Update(r.Context(), job); err != nil {
		WriteInternalError(w, "Failed to update job steps")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"job": job,
	})
}

// Helper functions

func extractProjectID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if part == "projects" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractJobID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if part == "agent-jobs" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

