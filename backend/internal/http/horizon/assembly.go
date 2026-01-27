package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
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
	// Prefer project-scoped listing (more reliable Firestore index).
	jobs, err := scoutingJobsRepo.ListByProjectAndLens(r.Context(), authInfo.TenantID, lens.ProjectID, lensID, 1000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var totalJobs int
	var completedJobs int
	var failedJobs int
	var pendingJobs int
	var runningJobs int
	for _, job := range jobs {
		if job == nil {
			continue
		}
		totalJobs++
		// Prefer AgentJob.Status, fall back to CurrentState (best-effort).
		switch strings.ToLower(string(job.Status)) {
		case "completed", "partial":
			completedJobs++
			continue
		case "failed":
			failedJobs++
			continue
		case "pending":
			pendingJobs++
			continue
		case "running":
			runningJobs++
			continue
		}
		// Fallback: infer from current state machine.
		switch job.CurrentState {
		case models.ScoutingJobStateVerified:
			completedJobs++
		case models.ScoutingJobStateFailed, models.ScoutingJobStateRejected:
			failedJobs++
		case models.ScoutingJobStatePending:
			pendingJobs++
		default:
			// planning/execution/extraction/validation/resolution
			runningJobs++
		}
	}

	// Определяем общий статус сборки
	status := "ready"
	stage := "ready"
	progress := 1.0

	if totalJobs > 0 && (pendingJobs > 0 || runningJobs > 0) {
		status = "scouting"
		stage = "scouting"
	}
	if totalJobs > 0 {
		progress = float64(completedJobs+failedJobs) / float64(totalJobs)
	}
	// If all jobs are terminal (completed/failed), we are ready.
	if totalJobs > 0 && pendingJobs == 0 && runningJobs == 0 {
		status = "ready"
		stage = "ready"
	}

	// Формируем ответ согласно документации
	scoutingPhaseStatus := "completed"
	if totalJobs > 0 && (pendingJobs > 0 || runningJobs > 0) {
		scoutingPhaseStatus = "scouting"
	}
	readyPhaseStatus := "pending"
	if stage == "ready" {
		readyPhaseStatus = "ready"
	}
	stageMap := map[string]interface{}{
		"decomposition": map[string]interface{}{
			"status": "completed",
			"nodes":  len(lens.Scope),
		},
		"cacheHit": map[string]interface{}{
			"status": "completed",
			"nodes":  0, // TODO: Подсчитать узлы из кэша
		},
		"scouting": map[string]interface{}{
			"status":    scoutingPhaseStatus,
			"completed": completedJobs + failedJobs,
			"total":     totalJobs,
			"logs":      []string{}, // TODO: Получить логи из ScoutingJob
		},
		"ready": map[string]interface{}{
			"status": readyPhaseStatus,
		},
	}
	response := map[string]interface{}{
		"status":   status,
		"stage":    stage,
		"progress": progress,
		"stages":   stageMap,
		// Backward compatibility: keep old field names for a deprecation window.
		"phase":  stage,
		"phases": stageMap,
		"graph": map[string]interface{}{
			"nodes": []interface{}{}, // TODO: Получить узлы графа
			"edges": []interface{}{}, // TODO: Получить рёбра графа
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
