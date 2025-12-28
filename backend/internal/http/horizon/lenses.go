package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
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
	if strings.TrimSpace(lens.ProjectID) == "" {
		http.Error(w, "projectId is required", http.StatusBadRequest)
		return
	}

	// Correlation/session IDs (optional, but recommended for realtime UX).
	// Frontend can pass these via headers; backend will generate correlationId if missing.
	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
	if correlationID == "" {
		correlationID = strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	}
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-ID"))
	}

	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     lens.ProjectID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	trace.Log(ctx, "lens_create", "lens.create.request", map[string]any{
		"tenantId":  authInfo.TenantID,
		"projectId": lens.ProjectID,
		"name":      trace.Trunc(lens.Name, 120),
		"status":    strings.TrimSpace(lens.Status),
		"scopeN":    len(lens.Scope),
		"scopeTop":  trace.StrSlice(lens.Scope, 12),
	})

	lensID, err := h.service.Lens().CreateLens(ctx, &lens)
	if err != nil {
		trace.Log(ctx, "lens_create", "lens.create.error", map[string]any{
			"tenantId":  authInfo.TenantID,
			"projectId": lens.ProjectID,
			"error":     err.Error(),
		})
		// Best-effort: publish assembly failed for UI visibility.
		if pub := h.service.RealtimePublisher(); pub != nil && strings.TrimSpace(lens.ProjectID) != "" {
			_ = pub.PublishJSON(ctx, realtime.EventType("horizon.assembly.failed"), authInfo.TenantID, map[string]interface{}{
				"tenantId":      authInfo.TenantID,
				"projectId":     lens.ProjectID,
				"phase":         "decomposition",
				"error":         err.Error(),
				"correlationId": correlationID,
				"sessionId":     sessionID,
			})
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lens.ID = lensID
	trace.Log(ctx, "lens_create", "lens.create.ok", map[string]any{
		"tenantId":  authInfo.TenantID,
		"projectId": lens.ProjectID,
		"lensId":    lensID,
		"scopeN":    len(lens.Scope),
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
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
		"jobId":               lensID,
		"status":              "completed",
		"estimatedCompletion": nil,
		"metrics":             metrics,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}
