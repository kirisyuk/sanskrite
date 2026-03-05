package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/merge"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetFactsByCanonicalPath handles GET /api/horizon/ontology-facts/by-canonical-path.
// Returns ontology facts linked to the given canonical path. Facts can relate to multiple
// canonical paths (canonicalPaths array); the response includes the full array for each fact.
//
// Query params:
//   - canonicalPath (required): e.g. /org/emirates, /loc/us
//   - kind (optional): filter by fact kind (event, claim, signal, metric, relation, other)
//   - limit (optional): max results, default 100
func (h *HorizonHandlers) GetFactsByCanonicalPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.factsRepo == nil {
		http.Error(w, "Ontology facts not configured", http.StatusServiceUnavailable)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	tenantID := strings.TrimSpace(authInfo.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	canonicalPath := strings.TrimSpace(r.URL.Query().Get("canonicalPath"))
	if canonicalPath == "" {
		http.Error(w, "canonicalPath is required", http.StatusBadRequest)
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	limit := 100
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}

	facts, err := h.factsRepo.ListByCanonicalPath(r.Context(), tenantID, canonicalPath, kind, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Resolve current canonical name (merge-aware): names can drift after abbreviation/merge.
	canonicalPathCurrentName := ""
	if h.service != nil && h.service.Fragments() != nil {
		if frag, ferr := h.service.Fragments().GetByCanonicalPath(r.Context(), canonicalPath); ferr == nil && frag != nil {
			canonicalPathCurrentName = merge.FragmentLabel(frag)
		}
	}
	if canonicalPathCurrentName == "" {
		canonicalPathCurrentName = merge.LabelFromPath(canonicalPath)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"canonicalPath":             canonicalPath,
		"canonicalPathCurrentName":  canonicalPathCurrentName,
		"facts":                     facts,
		"count":                     len(facts),
	})
}
