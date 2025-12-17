package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// CoPilotChat обрабатывает запросы к Co-Pilot чату
func (h *HorizonHandlers) CoPilotChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		LensID  string `json:"lensId"`
		Message string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.LensID == "" {
		http.Error(w, "lensId is required", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	// Получаем Lens
	lens, err := h.service.Lens().GetLens(r.Context(), req.LensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем данные Lens для контекста
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), lens.Scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем контекст для Co-Pilot
	contextPrompt := h.buildCoPilotContext(lens, fragments)

	// Используем AIExecutor для генерации ответа
	aiExecutor := h.service.AIExecutor()
	if aiExecutor == nil {
		http.Error(w, "AI executor not available", http.StatusInternalServerError)
		return
	}

	// Формируем промпт для Co-Pilot
	fullPrompt := contextPrompt + "\n\nUser: " + req.Message + "\n\nAssistant:"

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: fullPrompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         8000,
			MaxOutputSize:            2048,
		},
	}

	result, err := aiExecutor.ExecuteTask(r.Context(), task)
	if err != nil {
		http.Error(w, "Failed to generate response", http.StatusInternalServerError)
		return
	}

	// Формируем ответ
	response := map[string]interface{}{
		"lensId":    req.LensID,
		"message":   req.Message,
		"response":  result,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// buildCoPilotContext строит контекст для Co-Pilot на основе Lens и фрагментов
func (h *HorizonHandlers) buildCoPilotContext(lens *models.Lens, fragments []*models.HorizonFragment) string {
	context := "You are a Co-Pilot assistant for a risk analysis system (Horizon).\n\n"
	context += "Current Lens: " + lens.Name + "\n"
	// Description field doesn't exist in Lens model, using Name only
	context += "Scope: " + strings.Join(lens.Scope, ", ") + "\n\n"
	context += "Fragments in scope:\n"

	for i, fragment := range fragments {
		if i >= 10 { // Ограничиваем количество фрагментов для контекста
			context += "... and " + strconv.Itoa(len(fragments)-10) + " more fragments\n"
			break
		}
		context += "- " + fragment.CanonicalPath + " (" + string(fragment.Type) + ")\n"
		if fragment.Description != "" {
			context += "  " + fragment.Description + "\n"
		}
	}

	context += "\nYou can help users:\n"
	context += "- Understand the risk landscape\n"
	context += "- Analyze specific fragments or metrics\n"
	context += "- Suggest actions based on current state\n"
	context += "- Answer questions about the graph structure\n"

	return context
}
