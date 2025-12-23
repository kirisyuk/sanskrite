package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetScoutingJob получает статус ScoutingJob
func (h *HorizonHandlers) GetScoutingJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	jobID := extractIDFromPath(r, "/api/horizon/scout/jobs/")
	if jobID == "" {
		http.Error(w, "Job ID required in path", http.StatusBadRequest)
		return
	}

	job, err := h.service.ScoutingJobs().Get(r.Context(), jobID)
	if err != nil {
		http.Error(w, "ScoutingJob not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if job.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Формируем ответ согласно документации
	response := map[string]interface{}{
		"id":                  job.ID,
		"status":              job.CurrentState,
		"targetCanonicalPath": job.TargetCanonicalPath,
		"progress":            0.0, // TODO: Вычислить прогресс на основе состояния
		"currentStep":         string(job.CurrentState),
		"steps":               []map[string]interface{}{}, // TODO: Извлечь steps из AgentJob
		"createdAt":           job.CreatedAt,
		"estimatedCompletion": nil, // TODO: Вычислить на основе стратегии
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ListScoutingJobs получает список ScoutingJob
func (h *HorizonHandlers) ListScoutingJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lensID := r.URL.Query().Get("lensId")
	status := r.URL.Query().Get("status")
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}

	var jobs []*models.ScoutingJob
	var err error

	// Prefer project-scoped listing (more reliable Firestore index) when lensId is provided.
	if lensID != "" {
		lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
		if lerr != nil {
			http.Error(w, "Lens not found", http.StatusNotFound)
			return
		}
		if lens.TenantID != authInfo.TenantID {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
		jobs, err = h.service.ScoutingJobs().ListByProjectAndLens(r.Context(), authInfo.TenantID, lens.ProjectID, lensID, limit*2)
	} else {
		// Fallback: tenant-wide listing (may depend on composite indexes).
		jobs, err = h.service.ScoutingJobs().ListByLens(r.Context(), authInfo.TenantID, lensID, limit*2) // Увеличиваем лимит для фильтрации по status
	}

	// Фильтруем по статусу если указан
	if err == nil && status != "" {
		var filteredJobs []*models.ScoutingJob
		for _, job := range jobs {
			if string(job.CurrentState) == status {
				filteredJobs = append(filteredJobs, job)
				if len(filteredJobs) >= limit {
					break
				}
			}
		}
		jobs = filteredJobs
	} else if err == nil && len(jobs) > limit {
		// Ограничиваем лимит если не было фильтрации по статусу
		jobs = jobs[:limit]
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ответ согласно документации
	jobResponses := make([]map[string]interface{}, 0, len(jobs))
	for _, job := range jobs {
		jobResponses = append(jobResponses, map[string]interface{}{
			"id":                  job.ID,
			"status":              job.CurrentState,
			"targetCanonicalPath": job.TargetCanonicalPath,
			"createdAt":           job.CreatedAt,
			"completedAt":         nil, // TODO: Добавить поле completedAt в ScoutingJob
		})
	}

	response := map[string]interface{}{
		"jobs":  jobResponses,
		"total": len(jobResponses),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
