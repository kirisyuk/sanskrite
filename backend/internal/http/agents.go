package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// AgentsHandler обрабатывает HTTP запросы для агентов
type AgentsHandler struct {
	registry  *agents.Registry
	publisher *realtime.Publisher
	logger    *log.Logger
}

// NewAgentsHandler создает новый AgentsHandler
func NewAgentsHandler(
	registry *agents.Registry,
	publisher *realtime.Publisher,
	logger *log.Logger,
) *AgentsHandler {
	return &AgentsHandler{
		registry:  registry,
		publisher: publisher,
		logger:    logger,
	}
}

// ListAgents обрабатывает GET /api/v1/agents - список доступных агентов
func (h *AgentsHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	
	list := h.registry.List()
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"agents": list,
	})
}

// ExecuteAgent обрабатывает POST /api/v1/projects/{projectId}/agents/{agentId}/execute
func (h *AgentsHandler) ExecuteAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	
	// Извлекаем projectId и agentId из пути
	// Формат: /api/v1/projects/{projectId}/agents/{agentId}/execute
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID and Agent ID required")
		return
	}
	
	var projectID, agentID string
	for i, part := range parts {
		if part == "projects" && i+1 < len(parts) {
			projectID = parts[i+1]
		}
		if part == "agents" && i+1 < len(parts) {
			agentID = parts[i+1]
		}
	}
	
	if projectID == "" || agentID == "" {
		WriteBadRequest(w, "INVALID_PATH", "Project ID and Agent ID required")
		return
	}
	
	// Проверяем существование проекта
	// TODO: добавить проверку через projectsRepo если нужно
	
	// Получаем агента
	agent, ok := h.registry.Get(agentID)
	if !ok {
		WriteNotFound(w, "Agent not found")
		return
	}
	
	// Парсим тело запроса
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}
	
	// Добавляем/обновляем контекст из аутентификации
	if body["context"] == nil {
		body["context"] = make(map[string]interface{})
	}
	contextMap := body["context"].(map[string]interface{})
	contextMap["tenantId"] = auth.TenantID
	contextMap["projectId"] = projectID // Обязательно устанавливаем projectId
	contextMap["userId"] = auth.UID
	
	// Валидация что projectId в контексте соответствует пути
	if ctxProjectID, ok := contextMap["projectId"].(string); !ok || ctxProjectID != projectID {
		WriteBadRequest(w, "INVALID_CONTEXT", "projectId in context must match path")
		return
	}
	
	// Проверяем что projectId не пустой
	if projectID == "" {
		WriteBadRequest(w, "INVALID_CONTEXT", "projectId is required")
		return
	}
	
	taskJSON, err := json.Marshal(body)
	if err != nil {
		WriteInternalError(w, "Failed to marshal task")
		return
	}
	
	// Выполняем агента со стримингом
	resultJSON, err := h.registry.ExecuteAgentWithStream(
		r.Context(),
		agentID,
		taskJSON,
		auth.TenantID,
		projectID,
	)
	if err != nil {
		h.logger.Printf("agent execution failed: %v", err)
		WriteInternalError(w, "Agent execution failed: "+err.Error())
		return
	}
	
	// Парсим результат для ответа
	var result map[string]interface{}
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		WriteInternalError(w, "Failed to parse agent result")
		return
	}
	
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"result":    result,
		"agentId":   agentID,
		"agentName": agent.DisplayName(),
	})
}

// Helper для получения аутентификации из контекста
// Удалено - не используется

