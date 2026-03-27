package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	horizonsupplychain "github.com/igorkirisyuk/sanskrite/internal/horizon/supplychain"
)

type candidateChainCompletionRequest struct {
	MaxArtifactsPerChain int  `json:"maxArtifactsPerChain,omitempty"`
	MaxCandidates        int  `json:"maxCandidates,omitempty"`
	MinUnionCountries    int  `json:"minUnionCountries,omitempty"`
	DryRun               bool `json:"dryRun,omitempty"`
}

func (h *HorizonHandlers) HandleLensCandidateChainRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.candidateChainService == nil || h.service == nil || h.service.Lens() == nil {
		http.Error(w, "candidate chain completion is not configured", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	lensID, action := parseLensCandidateChainRoute(r.URL.Path)
	if lensID == "" || action != "complete" {
		http.NotFound(w, r)
		return
	}
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "lens not found", http.StatusNotFound)
		return
	}
	if !authInfo.IsSystem && strings.TrimSpace(lens.TenantID) != strings.TrimSpace(authInfo.TenantID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req candidateChainCompletionRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	result, err := h.candidateChainService.CompleteLens(r.Context(), candidateChainCompletionOptions(lensID, req))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lensId": lensID,
		"kind":   "candidate_chain_completion",
		"result": result,
	})
}

func parseLensCandidateChainRoute(path string) (lensID, action string) {
	trimmed := strings.TrimPrefix(path, "/api/horizon/lenses/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 3 {
		return "", ""
	}
	if strings.TrimSpace(parts[1]) != "candidate-chains" {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[2])
}

func candidateChainCompletionOptions(lensID string, req candidateChainCompletionRequest) horizonsupplychain.CandidateChainCompletionOptions {
	return horizonsupplychain.CandidateChainCompletionOptions{
		LensID:               strings.TrimSpace(lensID),
		MaxArtifactsPerChain: positiveOr(req.MaxArtifactsPerChain, 4),
		MaxCandidates:        positiveOr(req.MaxCandidates, 8),
		MinUnionCountries:    positiveOr(req.MinUnionCountries, 3),
		DryRun:               req.DryRun,
	}
}

func positiveOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
