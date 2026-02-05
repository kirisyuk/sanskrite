package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetCockpitData получает данные для Cockpit UI (граф, метрики, узлы)
func (h *HorizonHandlers) GetCockpitData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем lensID из пути: /api/horizon/lenses/{lensId}/cockpit
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lensID := pathParts[0]

	// Получаем Lens
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем все HorizonFragment из scope
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), lens.Scope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Root nodes = scope fragments (filtered). No hop+1 expansion: only scope entities.
	rootFragments := make([]*models.HorizonFragment, 0, len(fragments))
	rootIDs := make(map[string]struct{}, len(fragments))
	for _, fragment := range fragments {
		if fragment == nil || isSphereFragment(fragment) {
			continue
		}
		rootFragments = append(rootFragments, fragment)
		if fragment.ID != "" {
			rootIDs[fragment.ID] = struct{}{}
		}
	}

	// Collect edges ONLY among scope nodes.
	edgesRepo := h.service.Edges()
	var allEdges []interface{}
	seenEdges := make(map[string]struct{}, 2048)

	addEdge := func(edge *models.DependencyEdge) {
		if edge == nil {
			return
		}
		if _, ok := rootIDs[edge.SourceFragmentID]; !ok {
			return
		}
		if _, ok := rootIDs[edge.TargetFragmentID]; !ok {
			return
		}
		k := edge.SourceFragmentID + "|" + edge.TargetFragmentID + "|" + string(edge.Type)
		if _, ok := seenEdges[k]; ok {
			return
		}
		seenEdges[k] = struct{}{}
		allEdges = append(allEdges, edge)
	}

	// Pull edges for each root, but only keep those that stay within scope.
	for _, fragment := range rootFragments {
		if fragment == nil || fragment.ID == "" {
			continue
		}
		outgoingEdges, err := edgesRepo.ListBySource(r.Context(), "global", fragment.ID, nil, 200)
		if err == nil {
			for _, edge := range outgoingEdges {
				addEdge(edge)
			}
		}
		incomingEdges, err := edgesRepo.ListByTarget(r.Context(), "global", fragment.ID, nil, 200)
		if err == nil {
			for _, edge := range incomingEdges {
				addEdge(edge)
			}
		}
	}

	// Вычисляем метрики Lens через MetricsCalculator
	// Получаем все фрагменты и вычисляем метрики
	metrics := make(map[string]interface{})
	if len(rootFragments) > 0 {
		// Простое вычисление метрик на основе фрагментов
		metricSums := make(map[string]float64)
		metricCounts := make(map[string]int)
		for _, fragment := range rootFragments {
			for metricName, metricValue := range fragment.State {
				metricSums[metricName] += metricValue.Value
				metricCounts[metricName]++
			}
		}
		for metricName, count := range metricCounts {
			if count > 0 {
				metrics[metricName] = metricSums[metricName] / float64(count)
			}
		}
		metrics["totalFragments"] = len(rootFragments)
	}

	// Формируем ответ для Cockpit UI
	response := map[string]interface{}{
		"lens": map[string]interface{}{
			"id":      lens.ID,
			"name":    lens.Name,
			"scope":   lens.Scope,
			"metrics": metrics,
		},
		"graph": map[string]interface{}{
			"nodes": rootFragments,
			"edges": allEdges,
		},
		"summary": map[string]interface{}{
			"totalFragments": len(rootFragments),
			"totalEdges":     len(allEdges),
			"metrics":        metrics,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func isSphereFragment(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	if fragment.Type == models.FragmentType("SPHERE") {
		return true
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	return strings.HasPrefix(cp, "/sphere/")
}

// SimulateWhatIf выполняет What-If симуляцию изменения метрик
func (h *HorizonHandlers) SimulateWhatIf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем lensID из пути: /api/horizon/lenses/{lensId}/simulate
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lensID := pathParts[0]

	var req struct {
		FragmentID string                 `json:"fragmentId"`
		MetricName string                 `json:"metricName"`
		NewValue   map[string]interface{} `json:"newValue"` // MetricValue структура
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Получаем Lens
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем Propagation Engine
	propagationEngine := h.service.Propagation()
	if propagationEngine == nil {
		http.Error(w, "Propagation engine not available", http.StatusInternalServerError)
		return
	}

	// Получаем текущий фрагмент
	fragment, err := h.service.Fragments().Get(r.Context(), req.FragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Получаем старое значение метрики
	oldMetricValue, exists := fragment.State[req.MetricName]
	if !exists {
		http.Error(w, "Metric not found", http.StatusBadRequest)
		return
	}

	// Создаем новое значение метрики из запроса
	// TODO: Преобразовать req.NewValue в models.MetricValue
	// Временная реализация: используем старое значение с новым value
	newMetricValue := oldMetricValue
	if value, ok := req.NewValue["value"].(float64); ok {
		newMetricValue.Value = value
	}

	// Выполняем симуляцию распространения
	propagationResult, err := propagationEngine.PropagateChange(r.Context(), req.FragmentID, req.MetricName, oldMetricValue, newMetricValue)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ответ
	response := map[string]interface{}{
		"simulationId":      "sim-" + req.FragmentID + "-" + req.MetricName,
		"affectedFragments": propagationResult.AffectedFragments,
		"affectedLenses":    propagationResult.AffectedLenses,
		"propagationTime":   propagationResult.PropagationTime.String(),
		"metrics":           propagationResult.Metrics,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
