package horizon

import (
	"fmt"
	"net/http"
	"strings"
)

func (h *HorizonHandlers) GetLensAttention(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.watchRuntime == nil {
		http.Error(w, "watch runtime not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) != "attention" {
		http.Error(w, "invalid attention path", http.StatusBadRequest)
		return
	}
	lensID := strings.TrimSpace(parts[0])
	if strings.TrimSpace(lensID) == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lens, lerr := h.service.Lens().GetLens(r.Context(), lensID)
	if lerr != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}
	view, err := h.watchRuntime.LensAttention(r.Context(), authInfo.TenantID, lensID, 40)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build lens attention: %v", err), http.StatusInternalServerError)
		return
	}
	if len(parts) > 2 {
		switch strings.TrimSpace(parts[2]) {
		case "subjects":
			writeJSON(w, http.StatusOK, map[string]any{
				"lensId":      view.LensID,
				"lensName":    view.LensName,
				"subjects":    view.Subjects,
				"generatedAt": view.GeneratedAt,
			})
			return
		case "feed":
			writeJSON(w, http.StatusOK, map[string]any{
				"lensId":      view.LensID,
				"lensName":    view.LensName,
				"feed":        view.Feed,
				"generatedAt": view.GeneratedAt,
			})
			return
		default:
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}
	writeJSON(w, http.StatusOK, view)
}
