package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// CreateLens создает новый Lens
func (h *HorizonHandlers) CreateLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var lens models.Lens
	if err := json.NewDecoder(r.Body).Decode(&lens); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lens.TenantID = authInfo.TenantID

	lensID, err := h.service.Lens().CreateLens(r.Context(), &lens)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lens.ID = lensID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lens)
}

// GetLens получает Lens по ID
func (h *HorizonHandlers) GetLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lens)
}

// UpdateLens обновляет Lens
func (h *HorizonHandlers) UpdateLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var lens models.Lens
	if err := json.NewDecoder(r.Body).Decode(&lens); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	lens.ID = lensID
	if err := h.service.Lens().UpdateLens(r.Context(), &lens); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lens)
}

// DeleteLens удаляет Lens
func (h *HorizonHandlers) DeleteLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if err := h.service.Lens().DeleteLens(r.Context(), lensID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListLenses получает список Lens для проекта
func (h *HorizonHandlers) ListLenses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	projectID := r.URL.Query().Get("projectId")
	if projectID == "" {
		http.Error(w, "projectId query parameter is required", http.StatusBadRequest)
		return
	}

	status := r.URL.Query().Get("status")
	var statusPtr *string
	if status != "" {
		statusPtr = &status
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}

	lenses, err := h.service.Lens().ListLensesByProject(r.Context(), authInfo.TenantID, projectID, statusPtr, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"lenses": lenses,
		"total":  len(lenses),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RecalculateLens выполняет принудительный пересчет метрик для Lens
func (h *HorizonHandlers) RecalculateLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Выполняем пересчет метрик
	metrics, err := h.service.Lens().CalculateMetrics(r.Context(), lensID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Возвращаем ответ с jobId (в данном случае синхронный пересчет)
	response := map[string]interface{}{
		"jobId":                lensID,
		"status":               "completed",
		"estimatedCompletion":  nil,
		"metrics":              metrics,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

