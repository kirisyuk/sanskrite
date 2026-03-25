package horizon

import (
	"net/http"
	"strconv"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

type lensSimulationRouteKind string

const (
	lensSimulationRouteInvalid     lensSimulationRouteKind = ""
	lensSimulationRouteList        lensSimulationRouteKind = "list"
	lensSimulationRouteLatest      lensSimulationRouteKind = "latest"
	lensSimulationRouteBundleView  lensSimulationRouteKind = "bundle_view"
	lensSimulationRouteOverlays    lensSimulationRouteKind = "overlays"
)

func parseLensSimulationRoute(path string) (lensID string, kind lensSimulationRouteKind) {
	trimmed := strings.TrimPrefix(path, "/api/horizon/lenses/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) < 2 {
		return "", lensSimulationRouteInvalid
	}
	lensID = strings.TrimSpace(parts[0])
	if lensID == "" || strings.TrimSpace(parts[1]) != "simulations" {
		return "", lensSimulationRouteInvalid
	}
	if len(parts) == 2 {
		return lensID, lensSimulationRouteList
	}
	if strings.TrimSpace(parts[2]) != "latest" {
		return "", lensSimulationRouteInvalid
	}
	if len(parts) == 3 {
		return lensID, lensSimulationRouteLatest
	}
	if len(parts) == 4 {
		switch strings.TrimSpace(parts[3]) {
		case "bundle-view":
			return lensID, lensSimulationRouteBundleView
		case "overlays":
			return lensID, lensSimulationRouteOverlays
		}
	}
	return "", lensSimulationRouteInvalid
}

func (h *HorizonHandlers) HandleLensSimulationRoutes(w http.ResponseWriter, r *http.Request) {
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if h.service == nil || h.service.Simulation() == nil {
		http.Error(w, "simulation service is not configured", http.StatusServiceUnavailable)
		return
	}

	lensID, routeKind := parseLensSimulationRoute(r.URL.Path)
	if routeKind == lensSimulationRouteInvalid {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if routeKind == lensSimulationRouteList {
		limit := 25
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		runs, err := h.service.Simulation().ListRunsByLens(r.Context(), strings.TrimSpace(authInfo.TenantID), lensID, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"lensId": lensID,
			"runs":   runs,
		})
		return
	}

	run, err := h.service.Simulation().GetLatestRunByLens(r.Context(), strings.TrimSpace(authInfo.TenantID), lensID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !h.simulationAccessibleToTenant(r.Context(), run, authInfo.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if routeKind == lensSimulationRouteLatest {
		writeJSON(w, http.StatusOK, run)
		return
	}

	switch routeKind {
	case lensSimulationRouteBundleView:
		bundle, artifact, err := h.service.Simulation().GetBundleView(r.Context(), strings.TrimSpace(run.TenantID), run.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"runId":      run.ID,
			"artifactId": strings.TrimSpace(artifact.ID),
			"kind":       "bundle_view",
			"bundle":     bundle,
		})
	case lensSimulationRouteOverlays:
		bundle, artifact, err := h.service.Simulation().GetBundleView(r.Context(), strings.TrimSpace(run.TenantID), run.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		filters := parseSimulationOverlayFilters(r)
		overlays := filterSimulationResultOverlays(bundle.ResultOverlays, filters)
		writeJSON(w, http.StatusOK, map[string]any{
			"runId":      run.ID,
			"artifactId": strings.TrimSpace(artifact.ID),
			"kind":       "simulation_result_overlays",
			"filters":    filters.toMap(),
			"overlays":   overlays,
			"lensId":     lensID,
		})
	default:
		http.NotFound(w, r)
	}
}
