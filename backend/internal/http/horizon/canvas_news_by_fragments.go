package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// NewsByFragmentsRequest is the request body for the news-by-fragments endpoint.
type NewsByFragmentsRequest struct {
	// Fragments: list of visible nodes with canonical paths (and optional names).
	Fragments []struct {
		CanonicalPath string `json:"canonicalPath"`
		CanonicalName string `json:"canonicalName,omitempty"`
	} `json:"fragments"`
	// CanonicalPaths: legacy format (converted to Fragments with empty canonicalName).
	CanonicalPaths []string `json:"canonicalPaths,omitempty"`
	DaysBack       int      `json:"daysBack,omitempty"`
}

// NewsByFragmentsHandler handles POST /api/horizon/canvas/news-by-fragments.
// Calls Gemini with search grounding, creates ontology facts with CanonicalNames,
// runs 12-axis + propagation, returns result and publishes via WS.
func (h *HorizonHandlers) NewsByFragments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.newsByFragmentsCanvas == nil {
		http.Error(w, "News by fragments canvas not configured", http.StatusServiceUnavailable)
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

	var req NewsByFragmentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	var fragments []discovery.FragmentInput
	for _, f := range req.Fragments {
		cp := strings.TrimSpace(f.CanonicalPath)
		if cp != "" {
			fragments = append(fragments, discovery.FragmentInput{
				CanonicalPath: cp,
				CanonicalName: strings.TrimSpace(f.CanonicalName),
			})
		}
	}
	if len(fragments) == 0 {
		for _, p := range req.CanonicalPaths {
			p = strings.TrimSpace(p)
			if p != "" {
				fragments = append(fragments, discovery.FragmentInput{CanonicalPath: p})
			}
		}
	}
	if len(fragments) == 0 {
		http.Error(w, "fragments or canonicalPaths is required and must not be empty", http.StatusBadRequest)
		return
	}
	if req.DaysBack <= 0 {
		req.DaysBack = 7
	}

	result, err := h.newsByFragmentsCanvas.Run(r.Context(), tenantID, fragments, req.DaysBack, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(result)
}
