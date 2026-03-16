package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type syncWatchRuntimeRequest struct {
	TenantID      string `json:"tenantId,omitempty"`
	IncludeGlobal bool   `json:"includeGlobal,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	LensID        string `json:"lensId,omitempty"`
	FragmentID    string `json:"fragmentId,omitempty"`
}

func (h *HorizonHandlers) HandleWatchSubjectsRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchRuntime == nil {
		http.Error(w, "watch runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	tenantID := strings.TrimSpace(authInfo.TenantID)
	if h.isAdminRequest(r, authInfo) {
		if requested := strings.TrimSpace(r.URL.Query().Get("tenantId")); requested != "" {
			tenantID = requested
		}
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	subjects, err := h.watchRuntime.ListSubjects(r.Context(), tenantID, r.URL.Query().Get("lensId"), r.URL.Query().Get("anchorCanonicalPath"), limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list watch subjects: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subjects": subjects})
}

func (h *HorizonHandlers) ListWatchTriggers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchRuntime == nil {
		http.Error(w, "watch runtime not configured", http.StatusServiceUnavailable)
		return
	}
	if _, err := requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	subjectID := strings.TrimSpace(r.URL.Query().Get("subjectId"))
	if subjectID == "" {
		http.Error(w, "subjectId is required", http.StatusBadRequest)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	triggers, err := h.watchRuntime.ListTriggers(r.Context(), subjectID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list watch triggers: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": triggers})
}

func (h *HorizonHandlers) SyncWatchRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchRuntime == nil || h.watchService == nil {
		http.Error(w, "watch runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	var req syncWatchRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	tenantID := strings.TrimSpace(authInfo.TenantID)
	if h.isAdminRequest(r, authInfo) && strings.TrimSpace(req.TenantID) != "" {
		tenantID = strings.TrimSpace(req.TenantID)
	}
	if strings.TrimSpace(req.LensID) != "" {
		if err := h.watchRuntime.SyncWatchSubjectsForLens(r.Context(), strings.TrimSpace(req.LensID)); err != nil {
			http.Error(w, fmt.Sprintf("failed to sync lens watch runtime: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"synced": true, "lensId": strings.TrimSpace(req.LensID)})
		return
	}
	if strings.TrimSpace(req.FragmentID) != "" {
		if err := h.watchRuntime.SyncSubjectsForFragment(r.Context(), strings.TrimSpace(req.FragmentID)); err != nil {
			http.Error(w, fmt.Sprintf("failed to sync fragment watch runtime: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"synced": true, "fragmentId": strings.TrimSpace(req.FragmentID)})
		return
	}
	report, err := h.watchRuntime.SyncTenant(r.Context(), tenantID, req.IncludeGlobal && h.isAdminRequest(r, authInfo), req.Limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to sync watch runtime: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
