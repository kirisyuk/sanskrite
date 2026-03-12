package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type createWatchTargetRequest struct {
	Kind          string   `json:"kind"`
	Label         string   `json:"label"`
	CanonicalPath string   `json:"canonicalPath"`
	FragmentID    string   `json:"fragmentId,omitempty"`
	DomainModes   []string `json:"domainModes,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	Priority      int      `json:"priority,omitempty"`
}

type searchWatchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type updateWatchPreferencesRequest struct {
	Mode                string         `json:"mode,omitempty"`
	TimeHorizon         string         `json:"timeHorizon,omitempty"`
	ConfidenceFloor     float64        `json:"confidenceFloor,omitempty"`
	OnlyWithImpactPaths bool           `json:"onlyWithImpactPaths,omitempty"`
	OnlyTrustedSources  bool           `json:"onlyTrustedSources,omitempty"`
	Filters             map[string]any `json:"filters,omitempty"`
}

func (h *HorizonHandlers) HandleWatchRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.GetWatchDashboard(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) HandleWatchTargetsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.CreateWatchTarget(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) HandleWatchTargetRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		h.DeleteWatchTarget(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HorizonHandlers) GetWatchDashboard(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	dashboard, err := h.watchService.GetDashboard(r.Context(), authInfo.TenantID, authInfo.UID, mode)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build dashboard: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, dashboard)
}

func (h *HorizonHandlers) SearchWatchTargets(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	if _, err := requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var req searchWatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	results, err := h.watchService.Search(r.Context(), req.Query, req.Limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to search watch targets: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *HorizonHandlers) CreateWatchTarget(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var req createWatchTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.CanonicalPath) == "" {
		http.Error(w, "canonicalPath is required", http.StatusBadRequest)
		return
	}
	target := &horizonmodels.WatchTarget{
		TenantID:      authInfo.TenantID,
		UserID:        authInfo.UID,
		Kind:          strings.TrimSpace(req.Kind),
		Label:         strings.TrimSpace(req.Label),
		CanonicalPath: strings.TrimSpace(req.CanonicalPath),
		FragmentID:    strings.TrimSpace(req.FragmentID),
		DomainModes:   req.DomainModes,
		Roles:         req.Roles,
		Priority:      req.Priority,
		Source:        "api_manual",
		Status:        string(horizonmodels.WatchTargetStatusActive),
	}
	if target.Kind == "" {
		target.Kind = inferWatchKind(target.CanonicalPath)
	}
	created, err := h.watchService.CreateTarget(r.Context(), target)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create watch target: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *HorizonHandlers) DeleteWatchTarget(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	targetID := extractIDFromPath(r, "/api/horizon/watch/targets/")
	if targetID == "" {
		http.Error(w, "target id required", http.StatusBadRequest)
		return
	}
	if err := h.watchService.DeleteTarget(r.Context(), authInfo.TenantID, targetID); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete watch target: %v", err), http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": targetID})
}

func (h *HorizonHandlers) UpdateWatchPreferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var req updateWatchPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	prefs := &horizonmodels.WatchPreferences{
		TenantID:            authInfo.TenantID,
		UserID:              authInfo.UID,
		Mode:                req.Mode,
		TimeHorizon:         req.TimeHorizon,
		ConfidenceFloor:     req.ConfidenceFloor,
		OnlyWithImpactPaths: req.OnlyWithImpactPaths,
		OnlyTrustedSources:  req.OnlyTrustedSources,
		Filters:             req.Filters,
	}
	if err := h.watchService.UpdatePreferences(r.Context(), prefs); err != nil {
		http.Error(w, fmt.Sprintf("failed to update watch preferences: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (h *HorizonHandlers) GetWatchBrief(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	if _, err := requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	briefID := extractIDFromPath(r, "/api/horizon/watch/briefs/")
	if briefID == "" {
		http.Error(w, "brief id required", http.StatusBadRequest)
		return
	}
	brief, err := h.watchService.GetBrief(r.Context(), briefID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get brief: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, brief)
}

func (h *HorizonHandlers) HandleWatchBriefRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchService == nil {
		http.Error(w, "watch service not configured", http.StatusServiceUnavailable)
		return
	}
	if _, err := requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/watch/briefs/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.Error(w, "brief id required", http.StatusBadRequest)
		return
	}
	parts := strings.Split(path, "/")
	briefID := strings.TrimSpace(parts[0])
	if briefID == "" {
		http.Error(w, "brief id required", http.StatusBadRequest)
		return
	}
	if len(parts) == 1 {
		h.GetWatchBrief(w, r)
		return
	}
	var (
		payload map[string]any
		err     error
	)
	switch strings.TrimSpace(parts[1]) {
	case "evidence":
		payload, err = h.watchService.GetBriefEvidence(r.Context(), briefID)
	case "paths":
		payload, err = h.watchService.GetBriefPaths(r.Context(), briefID)
	case "sources":
		payload, err = h.watchService.GetBriefSources(r.Context(), briefID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get brief details: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func inferWatchKind(canonicalPath string) string {
	canonicalPath = strings.TrimSpace(canonicalPath)
	switch {
	case strings.HasPrefix(canonicalPath, "/loc/"):
		return "country"
	case strings.HasPrefix(canonicalPath, "/market/"):
		return "market"
	case strings.HasPrefix(canonicalPath, "/corridor/"):
		return "corridor"
	default:
		return "fragment"
	}
}
