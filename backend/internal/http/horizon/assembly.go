package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// GetAssemblyStatus получает статус сборки графа для Lens
func (h *HorizonHandlers) GetAssemblyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем lensID из пути: /api/horizon/lenses/{lensId}/assembly-status
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

	// Получаем активные ScoutingJob для Lens
	scoutingJobsRepo := h.service.ScoutingJobs()
	jobs, err := scoutingJobsRepo.ListByLens(r.Context(), authInfo.TenantID, lensID, 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var totalJobs int
	var completedJobs int
	for _, job := range jobs {
		totalJobs++
		if job.CurrentState == models.ScoutingJobStateVerified {
			completedJobs++
		}
	}

	// Определяем общий статус сборки
	status := "ready"
	phase := "ready"
	progress := 1.0

	if len(jobs) > 0 {
		status = "scouting"
		phase = "scouting"
		if totalJobs > 0 {
			progress = float64(completedJobs) / float64(totalJobs)
		}
	}

	// Формируем ответ согласно документации
	response := map[string]interface{}{
		"status":   status,
		"phase":    phase,
		"progress": progress,
		"phases": map[string]interface{}{
			"decomposition": map[string]interface{}{
				"status": "completed",
				"nodes":  len(lens.Scope),
			},
			"cacheHit": map[string]interface{}{
				"status": "completed",
				"nodes":  0, // TODO: Подсчитать узлы из кэша
			},
			"scouting": map[string]interface{}{
				"status":    status,
				"completed": completedJobs,
				"total":     totalJobs,
				"logs":      []string{}, // TODO: Получить логи из ScoutingJob
			},
			"ready": map[string]interface{}{
				"status": status,
			},
		},
		"graph": map[string]interface{}{
			"nodes": []interface{}{}, // TODO: Получить узлы графа
			"edges": []interface{}{}, // TODO: Получить рёбра графа
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

