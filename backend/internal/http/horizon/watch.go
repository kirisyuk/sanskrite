package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	watch "github.com/igorkirisyuk/sanskrite/internal/horizon/watch"
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

type compareWatchScenariosRequest struct {
	Mode        string   `json:"mode,omitempty"`
	LensID      string   `json:"lensId,omitempty"`
	TimeHorizon string   `json:"timeHorizon,omitempty"`
	ContractIDs []string `json:"contractIds,omitempty"`
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
	dashboard, err := h.watchService.GetDashboardSnapshot(r.Context(), watch.DashboardRequest{
		TenantID:     authInfo.TenantID,
		UserID:       authInfo.UID,
		Mode:         mode,
		LensID:       strings.TrimSpace(r.URL.Query().Get("lensId")),
		ServiceLine:  strings.TrimSpace(r.URL.Query().Get("serviceLine")),
		TimeHorizon:  strings.TrimSpace(r.URL.Query().Get("timeHorizon")),
		QuickFilters: splitCSV(r.URL.Query().Get("filters")),
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build dashboard: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, dashboard)
}

func (h *HorizonHandlers) GetWatchScenarioWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	workspace, err := h.watchService.GetScenarioWorkspace(r.Context(), watch.DashboardRequest{
		TenantID:    authInfo.TenantID,
		UserID:      authInfo.UID,
		Mode:        strings.TrimSpace(r.URL.Query().Get("mode")),
		LensID:      strings.TrimSpace(r.URL.Query().Get("lensId")),
		ServiceLine: watch.ServiceLineScenario,
		TimeHorizon: strings.TrimSpace(r.URL.Query().Get("timeHorizon")),
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build scenario workspace: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (h *HorizonHandlers) GetWatchScenarioDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
	presetID := extractIDFromPath(r, "/api/horizon/watch/scenarios/contracts/")
	if presetID == "" {
		http.Error(w, "scenario contract id required", http.StatusBadRequest)
		return
	}
	workspace, err := h.watchService.GetScenarioWorkspace(r.Context(), watch.DashboardRequest{
		TenantID:    authInfo.TenantID,
		UserID:      authInfo.UID,
		Mode:        strings.TrimSpace(r.URL.Query().Get("mode")),
		LensID:      strings.TrimSpace(r.URL.Query().Get("lensId")),
		ServiceLine: watch.ServiceLineScenario,
		TimeHorizon: strings.TrimSpace(r.URL.Query().Get("timeHorizon")),
	})
	if err != nil || workspace == nil {
		http.Error(w, fmt.Sprintf("failed to load scenario detail: %v", err), http.StatusInternalServerError)
		return
	}
	for _, preset := range workspace.Contracts {
		if strings.EqualFold(strings.TrimSpace(preset.ID), strings.TrimSpace(presetID)) {
			writeJSON(w, http.StatusOK, map[string]any{
				"workspace": workspace,
				"contract":  preset,
			})
			return
		}
	}
	http.Error(w, "scenario contract not found", http.StatusNotFound)
}

func (h *HorizonHandlers) CompareWatchScenarios(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchService == nil || h.service == nil || h.service.Propagation() == nil || h.service.Fragments() == nil || h.propagationProfileLookup == nil {
		http.Error(w, "watch scenario dependencies not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var req compareWatchScenariosRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	workspace, err := h.watchService.GetScenarioWorkspace(r.Context(), watch.DashboardRequest{
		TenantID:    authInfo.TenantID,
		UserID:      authInfo.UID,
		Mode:        strings.TrimSpace(req.Mode),
		LensID:      strings.TrimSpace(req.LensID),
		ServiceLine: watch.ServiceLineScenario,
		TimeHorizon: strings.TrimSpace(req.TimeHorizon),
	})
	if err != nil || workspace == nil {
		http.Error(w, fmt.Sprintf("failed to build scenario workspace: %v", err), http.StatusInternalServerError)
		return
	}
	selected := map[string]struct{}{}
	for _, id := range req.ContractIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = struct{}{}
		}
	}
	branches := make([]map[string]any, 0, len(workspace.Contracts))
	for _, preset := range workspace.Contracts {
		if len(selected) > 0 {
			if _, ok := selected[strings.TrimSpace(preset.ID)]; !ok {
				continue
			}
		}
		result, runErr := propagation.RunScenario(
			r.Context(),
			h.service.Propagation(),
			h.propagationProfileLookup,
			h.service.Fragments(),
			propagation.ScenarioInput{
				ScenarioID:      strings.TrimSpace(preset.ID),
				ContractType:    strings.TrimSpace(preset.ContractType),
				DomainModes:     append([]string(nil), preset.DomainModes...),
				Mode:            propagation.ScenarioModePreview,
				Parameters:      preset.Parameters,
				SeedFragmentIDs: append([]string(nil), preset.SeedFragmentIDs...),
				TargetPaths:     append([]string(nil), preset.TargetPaths...),
				MaxTraceSteps:   60,
			},
		)
		branch := map[string]any{
			"contract": preset,
		}
		if runErr != nil {
			branch["error"] = runErr.Error()
		} else {
			branch["result"] = result
		}
		branches = append(branches, branch)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace": workspace,
		"branches":  branches,
	})
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

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
