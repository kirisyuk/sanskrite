package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	hlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

// normalizeViewConfigForClient fixes PascalCase keys in viewConfig.domains
// that result from storing DomainResolution structs without firestore tags.
// Existing Firestore documents may have PascalCase field names (CanonicalPath,
// ContractType, etc.); this normalizes them to camelCase for the frontend.
func normalizeViewConfigForClient(lens *models.Lens) {
	if lens == nil || lens.ViewConfig == nil {
		return
	}
	raw, ok := lens.ViewConfig["domains"]
	if !ok || raw == nil {
		return
	}
	items, ok := toSliceOfMaps(raw)
	if !ok || len(items) == 0 {
		return
	}
	normalized := make([]map[string]interface{}, 0, len(items))
	for _, m := range items {
		nm := normalizeDomainMapKeys(m)
		if len(nm) > 0 {
			normalized = append(normalized, nm)
		}
	}
	lens.ViewConfig["domains"] = normalized
}

func toSliceOfMaps(v interface{}) ([]map[string]interface{}, bool) {
	switch t := v.(type) {
	case []map[string]interface{}:
		return t, true
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func normalizeDomainMapKeys(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	pick := func(camel, pascal string) (interface{}, bool) {
		if v, ok := m[camel]; ok {
			return v, true
		}
		if v, ok := m[pascal]; ok {
			return v, true
		}
		return nil, false
	}
	out := make(map[string]interface{}, len(m))
	mapping := [][2]string{
		{"canonicalPath", "CanonicalPath"},
		{"contractType", "ContractType"},
		{"title", "Title"},
		{"description", "Description"},
		{"facets", "Facets"},
		{"projections", "Projections"},
		{"relatedDomains", "RelatedDomains"},
		{"affectedDomains", "AffectedDomains"},
		{"firstOrderEntities", "FirstOrderEntities"},
		{"confidence", "Confidence"},
		{"reason", "Reason"},
		{"supplyChainExecutionPlan", "SupplyChainExecutionPlan"},
		{"regulatoryImpactExecutionPlan", "RegulatoryImpactExecutionPlan"},
		{"governanceExecutionPlan", "GovernanceExecutionPlan"},
		{"ecosystemExecutionPlan", "EcosystemExecutionPlan"},
	}
	for _, pair := range mapping {
		if v, ok := pick(pair[0], pair[1]); ok && v != nil {
			out[pair[0]] = v
		}
	}
	return out
}

func lensValidationStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "validation error") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

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
	if strings.TrimSpace(lens.Status) == "" {
		lens.Status = string(models.LensStatusActive)
	}
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

	// Optional header: suppress scouting for scope-only creates (deterministic UI-selected objects).
	skipScouting := false
	if v := strings.TrimSpace(r.Header.Get("X-Horizon-Skip-Scouting")); v != "" {
		v = strings.ToLower(v)
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			ctx = hlens.WithSkipScouting(ctx)
			skipScouting = true
		}
	}

	trace.Log(ctx, "lens_create", "lens.create.request", map[string]any{
		"tenantId":  authInfo.TenantID,
		"projectId": lens.ProjectID,
		"name":      trace.Trunc(lens.Name, 120),
		"status":    strings.TrimSpace(lens.Status),
		"scopeN":    len(lens.Scope),
		"scopeTop":  trace.StrSlice(lens.Scope, 12),
	})

	if !skipScouting {
		ready, reason, message := h.scoutExecutionAvailability()
		if !ready {
			trace.Log(ctx, "lens_create", "lens.create.blocked", map[string]any{
				"tenantId":         authInfo.TenantID,
				"projectId":        lens.ProjectID,
				"scopeN":           len(lens.Scope),
				"scoutReady":       false,
				"scoutReadyReason": reason,
			})
			h.publishAssemblyFatal(
				ctx,
				authInfo.TenantID,
				lens.ProjectID,
				"",
				"scouting",
				correlationID,
				sessionID,
				reason,
				message,
			)
			writeFatalPipelineError(w, http.StatusServiceUnavailable, reason, message, "scouting", map[string]interface{}{
				"mode": "create_lens",
			})
			return
		}
	}

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
		http.Error(w, err.Error(), lensValidationStatus(err))
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

	normalizeViewConfigForClient(lens)

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

	// Enforce tenant (do not trust client-sent tenantId).
	lens.TenantID = authInfo.TenantID

	// Best-effort: validate access and freeze projectId to existing lens (do not allow cross-project moves here).
	if old, err := h.service.Lens().GetLens(r.Context(), lensID); err == nil && old != nil {
		if old.TenantID != authInfo.TenantID {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
		if strings.TrimSpace(old.ProjectID) != "" {
			lens.ProjectID = old.ProjectID
		}
	}

	ctx := r.Context()
	// Optional header: suppress scouting for scope-only updates (e.g., pin/unpin).
	if v := strings.TrimSpace(r.Header.Get("X-Horizon-Skip-Scouting")); v != "" {
		v = strings.ToLower(v)
		if v == "1" || v == "true" || v == "yes" || v == "on" {
			ctx = hlens.WithSkipScouting(ctx)
		}
	}

	if err := h.service.Lens().UpdateLens(ctx, &lens); err != nil {
		http.Error(w, err.Error(), lensValidationStatus(err))
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

	for _, l := range lenses {
		normalizeViewConfigForClient(l)
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

// TransferLens moves a lens from one project to another
func (h *HorizonHandlers) TransferLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lensID := extractIDFromPath(r, "/api/horizon/lenses/")
	if lensID == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}

	// Remove /transfer suffix from lensID if present
	lensID = strings.TrimSuffix(lensID, "/transfer")

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		TargetProjectID string `json:"targetProjectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.TargetProjectID) == "" {
		http.Error(w, "targetProjectId is required", http.StatusBadRequest)
		return
	}

	// Get the lens and verify access
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Transfer the lens
	updatedLens, err := h.service.Lens().TransferLens(r.Context(), lensID, req.TargetProjectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedLens)
}
