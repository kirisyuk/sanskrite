package horizon

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (h *HorizonHandlers) HandleWatchDigestsRoot(w http.ResponseWriter, r *http.Request) {
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
	scopeType := strings.TrimSpace(r.URL.Query().Get("scopeType"))
	scopeID := strings.TrimSpace(r.URL.Query().Get("scopeId"))
	if scopeType == "" || scopeID == "" {
		http.Error(w, "scopeType and scopeId are required", http.StatusBadRequest)
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, convErr := strconv.Atoi(raw); convErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	digests, derr := h.watchRuntime.ListRecentDigestsByScope(r.Context(), authInfo.TenantID, scopeType, scopeID, limit)
	if derr != nil {
		http.Error(w, fmt.Sprintf("failed to list digests: %v", derr), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"digests": digests})
}

func (h *HorizonHandlers) HandleWatchDigestDetail(w http.ResponseWriter, r *http.Request) {
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
	digestID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/horizon/watch/digests/"))
	if digestID == "" {
		http.Error(w, "digest id is required", http.StatusBadRequest)
		return
	}
	digest, derr := h.watchRuntime.GetMonitoringDigest(r.Context(), digestID)
	if derr != nil {
		http.Error(w, fmt.Sprintf("failed to load digest: %v", derr), http.StatusInternalServerError)
		return
	}
	if digest == nil || strings.TrimSpace(digest.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "digest not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, digest)
}
