package horizon

import (
	"context"
	"encoding/json"
	"fmt"
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

// TracePropagationPaths returns how propagation would flow from a node (preview, no persist).
// GET /api/horizon/propagation/trace?fragmentId=...&metricName=...&delta=...&maxHops=...
// Response: { sourceFragmentId, nodeIds, edges, paths } for frontend red glow visualization.
func (h *HorizonHandlers) TracePropagationPaths(w http.ResponseWriter, r *http.Request) {
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

	metricName := r.URL.Query().Get("metricName")
	if metricName == "" {
		metricName = "propagation_trace"
	}
	delta := 0.1
	if d := r.URL.Query().Get("delta"); d != "" {
		if parsed, err := parseFloat(d); err == nil {
			delta = parsed
		}
	}
	maxHops := 32
	if m := r.URL.Query().Get("maxHops"); m != "" {
		if parsed, err := parseInt(m); err == nil && parsed > 0 {
			maxHops = parsed
		}
	}

	result, err := propagationEngine.TracePropagationPaths(r.Context(), fragmentID, metricName, delta, maxHops)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func parseInt(s string) (int, error) {
	var i int
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

