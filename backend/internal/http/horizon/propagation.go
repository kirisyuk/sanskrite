package horizon

import (
	"context"
	"encoding/json"
	"net/http"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetPropagationStatus получает статус распространения изменений
func (h *HorizonHandlers) GetPropagationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := r.URL.Query().Get("fragmentId")
	if fragmentID == "" {
		http.Error(w, "fragmentId query parameter is required", http.StatusBadRequest)
		return
	}

	propagationEngine := h.service.Propagation()
	if propagationEngine == nil {
		http.Error(w, "Propagation Engine not available", http.StatusServiceUnavailable)
		return
	}

	// Получаем статус распространения
	status := propagationEngine.GetPropagationStatus(fragmentID)
	
	var lastPropagation map[string]interface{}
	if status.LastPropagation != nil {
		lastPropagation = map[string]interface{}{
			"startedAt":          status.LastPropagation.StartedAt,
			"completedAt":        status.LastPropagation.CompletedAt,
			"nodesUpdated":       status.LastPropagation.NodesUpdated,
			"lensesRecalculated": status.LastPropagation.LensesRecalculated,
		}
	} else {
		lastPropagation = map[string]interface{}{
			"startedAt":          nil,
			"completedAt":        nil,
			"nodesUpdated":       0,
			"lensesRecalculated": 0,
		}
	}

	response := map[string]interface{}{
		"fragmentId":     fragmentID,
		"isPropagating":  status.IsPropagating,
		"lastPropagation": lastPropagation,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// TriggerPropagation запускает принудительное распространение изменений
func (h *HorizonHandlers) TriggerPropagation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := r.URL.Query().Get("fragmentId")
	if fragmentID == "" {
		http.Error(w, "fragmentId query parameter is required", http.StatusBadRequest)
		return
	}

	propagationEngine := h.service.Propagation()
	if propagationEngine == nil {
		http.Error(w, "Propagation Engine not available", http.StatusServiceUnavailable)
		return
	}

	// Запускаем принудительное распространение асинхронно
	go func() {
		ctx := context.Background()
		_, err := propagationEngine.TriggerPropagation(ctx, fragmentID)
		if err != nil {
			// Логируем ошибку, но не блокируем ответ
			_ = err
		}
	}()

	response := map[string]interface{}{
		"propagationId":       fragmentID,
		"status":              "started",
		"estimatedCompletion": nil,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

