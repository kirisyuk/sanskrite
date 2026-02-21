package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

// RunScenario handles POST /api/horizon/scenarios/run
// Runs a domain scenario: Encode → Propagate → Decode.
// Behavior is controlled by input.mode: "preview" (no decode persistence) or "apply" (persist decode metrics).
func (h *HorizonHandlers) RunScenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if h.service == nil || h.service.Propagation() == nil || h.service.Fragments() == nil {
		http.Error(w, "Horizon service or propagation not available", http.StatusServiceUnavailable)
		return
	}
	if h.propagationProfileLookup == nil {
		http.Error(w, "Propagation profile lookup not configured", http.StatusServiceUnavailable)
		return
	}

	var req propagation.ScenarioInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.ContractType = strings.TrimSpace(req.ContractType)
	if req.ContractType == "" {
		req.ContractType = propagation.PolicyImpactContractTypeFamily
	}

	result, err := propagation.RunScenario(
		r.Context(),
		h.service.Propagation(),
		h.propagationProfileLookup,
		h.service.Fragments(),
		req,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
