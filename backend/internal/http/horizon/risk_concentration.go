package horizon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// GetRiskConcentration returns concentration hotspots for external risk/opportunity.
// GET /api/horizon/risk/concentration?polarity=risk&limit=10
func (h *HorizonHandlers) GetRiskConcentration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	auth := httputil.GetAuthInfo(r.Context())
	if auth == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	polarity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("polarity")))
	if polarity == "" {
		polarity = "risk"
	}
	if polarity != "risk" && polarity != "opportunity" {
		http.Error(w, "invalid polarity", http.StatusBadRequest)
		return
	}
	limit := 10
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	snapshot, err := h.service.GetConcentrationSnapshot(r.Context(), strings.TrimSpace(auth.TenantID), polarity, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}

