package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

type createLensFromQueryRequest struct {
	Name             string   `json:"name"`
	ProjectID         string   `json:"projectId"`
	Query            string   `json:"query"`
	MaxRecursionDepth *int     `json:"maxRecursionDepth,omitempty"`
	RequestedMetrics  []string `json:"requestedMetrics,omitempty"`
	Status           string   `json:"status,omitempty"` // ACTIVE|DORMANT
}

type createLensFromQueryResponse struct {
	Intent *agents.IntentResolution `json:"intent"`
	Lens   *models.Lens            `json:"lens"`
}

// CreateLensFromQuery resolves a natural language query into intent and creates a Lens with:
// - scope = anchors.canonicalPath
// - viewConfig = focus + spherePaths + keywords (+ optional knobs)
//
// POST /api/horizon/lenses/from-query
func (h *HorizonHandlers) CreateLensFromQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req createLensFromQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Query = strings.TrimSpace(req.Query)
	req.Name = strings.TrimSpace(req.Name)
	req.Status = strings.TrimSpace(req.Status)

	if req.ProjectID == "" {
		http.Error(w, "projectId is required", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	// Correlation/session IDs (optional, but recommended for realtime UX).
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
		ProjectID:     req.ProjectID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	trace.Log(ctx, "lens_create", "lens.from_query.request", map[string]any{
		"tenantId":          authInfo.TenantID,
		"projectId":         req.ProjectID,
		"queryLen":          len(req.Query),
		"querySha1":         trace.HashText(req.Query),
		"nameProvided":      req.Name != "",
		"statusProvided":    req.Status != "",
		"maxRecursionDepth": req.MaxRecursionDepth,
		"requestedMetricsN": len(req.RequestedMetrics),
	})

	// 1) Resolve intent (precheck).
	intentAgent := agents.NewIntentResolverAgent(h.service.AIExecutor())
	trace.Log(ctx, "intent", "intent.resolve.start", map[string]any{
		"projectId": req.ProjectID,
		"queryLen":  len(req.Query),
	})
	intent, err := intentAgent.Resolve(ctx, req.Query)
	if err != nil {
		trace.Log(ctx, "intent", "intent.resolve.error", map[string]any{
			"projectId": req.ProjectID,
			"error":     err.Error(),
		})
		// Best-effort: publish assembly failed for UI visibility.
		if pub := h.service.RealtimePublisher(); pub != nil {
			_ = pub.PublishJSON(ctx, realtime.EventType("horizon.assembly.failed"), authInfo.TenantID, map[string]interface{}{
				"tenantId":      authInfo.TenantID,
				"projectId":     req.ProjectID,
				"phase":         "intent",
				"error":         err.Error(),
				"correlationId": correlationID,
				"sessionId":     sessionID,
			})
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	anchorPaths := make([]string, 0, len(intent.Anchors))
	for _, a := range intent.Anchors {
		anchorPaths = append(anchorPaths, strings.TrimSpace(a.CanonicalPath))
	}
	trace.Log(ctx, "intent", "intent.resolve.ok", map[string]any{
		"projectId":      req.ProjectID,
		"focus":          trace.Trunc(intent.Focus, 140),
		"spherePathsN":   len(intent.SpherePaths),
		"keywordsN":      len(intent.Keywords),
		"anchorsN":       len(intent.Anchors),
		"anchorPaths":    trace.StrSlice(anchorPaths, 10),
		"spherePathsTop": trace.StrSlice(intent.SpherePaths, 10),
	})

	// 2) Build Lens.
	scope := make([]string, 0, len(intent.Anchors))
	seenScope := make(map[string]struct{}, len(intent.Anchors))
	for _, a := range intent.Anchors {
		cp := strings.TrimSpace(a.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seenScope[cp]; ok {
			continue
		}
		seenScope[cp] = struct{}{}
		scope = append(scope, cp)
	}
	if len(scope) == 0 {
		trace.Log(ctx, "scope", "scope.empty", map[string]any{
			"projectId": req.ProjectID,
			"anchorsN":  len(intent.Anchors),
		})
		http.Error(w, "no anchors resolved", http.StatusBadRequest)
		return
	}

	name := req.Name
	if name == "" {
		name = intent.Focus
	}
	status := strings.ToUpper(req.Status)
	if status == "" {
		status = "ACTIVE"
	}

	viewConfig := map[string]interface{}{
		"focus":      intent.Focus,
		"spherePaths": intent.SpherePaths,
		"keywords":   intent.Keywords,
	}
	if req.MaxRecursionDepth != nil {
		viewConfig["maxRecursionDepth"] = *req.MaxRecursionDepth
	}
	if len(req.RequestedMetrics) > 0 {
		viewConfig["requestedMetrics"] = req.RequestedMetrics
	}

	lens := &models.Lens{
		TenantID:  authInfo.TenantID,
		ProjectID: req.ProjectID,
		Name:      name,
		Scope:     scope,
		ViewConfig: viewConfig,
		Status:    status,
	}

	trace.Log(ctx, "lens_create", "lens.built", map[string]any{
		"tenantId":      lens.TenantID,
		"projectId":     lens.ProjectID,
		"name":          trace.Trunc(lens.Name, 120),
		"status":        lens.Status,
		"scopeN":        len(lens.Scope),
		"scopeTop":      trace.StrSlice(lens.Scope, 12),
		"viewConfigKeys": func() []string {
			keys := make([]string, 0, len(lens.ViewConfig))
			for k := range lens.ViewConfig {
				keys = append(keys, k)
			}
			return trace.StrSlice(keys, 20)
		}(),
	})

	lensID, err := h.service.Lens().CreateLens(ctx, lens)
	if err != nil {
		trace.Log(ctx, "lens_create", "lens.create.error", map[string]any{
			"tenantId":  lens.TenantID,
			"projectId": lens.ProjectID,
			"error":     err.Error(),
		})
		if pub := h.service.RealtimePublisher(); pub != nil {
			_ = pub.PublishJSON(ctx, realtime.EventType("horizon.assembly.failed"), authInfo.TenantID, map[string]interface{}{
				"tenantId":      authInfo.TenantID,
				"projectId":     req.ProjectID,
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
		"tenantId":  lens.TenantID,
		"projectId": lens.ProjectID,
		"lensId":    lensID,
		"scopeN":    len(lens.Scope),
	})

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-Id", correlationID)
	_ = json.NewEncoder(w).Encode(createLensFromQueryResponse{
		Intent: intent,
		Lens:   lens,
	})
}


