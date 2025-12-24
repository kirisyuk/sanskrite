package horizon

import (
	"encoding/json"
	"net/http"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetFragmentRollup returns aggregated rollup for a fragment.
// GET /api/horizon/rollup/{fragmentID}?kind=risk
func (h *HorizonHandlers) GetFragmentRollup(w http.ResponseWriter, r *http.Request, fragmentID, metricKind string) {
	ctx := r.Context()

	if auth := httputil.GetAuthInfo(ctx); auth == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get rollup from service
	rollup, err := h.service.GetFragmentRollup(ctx, fragmentID, metricKind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rollup)
}

// GetFragmentExplain returns detailed explanation for a rollup.
// GET /api/horizon/rollup/{fragmentID}/explain?kind=risk
func (h *HorizonHandlers) GetFragmentExplain(w http.ResponseWriter, r *http.Request, fragmentID, metricKind string) {
	ctx := r.Context()

	if auth := httputil.GetAuthInfo(ctx); auth == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get rollup with full explain trace from service
	rollup, err := h.service.GetFragmentRollup(ctx, fragmentID, metricKind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return only the explain trace
	if rollup.ExplainTrace == nil {
		http.Error(w, "No explain trace available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rollup.ExplainTrace)
}

