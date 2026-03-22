package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type productPrefetchRequest struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type productPrefetchSuggestionResponse struct {
	CanonicalPath     string `json:"canonicalPath"`
	Label             string `json:"label"`
	MatchedLabel      string `json:"matchedLabel,omitempty"`
	MatchedPath       string `json:"matchedPath,omitempty"`
	CanonicalizedFrom string `json:"canonicalizedFrom,omitempty"`
	SourceScope       string `json:"sourceScope,omitempty"`
	Score             int    `json:"score"`
}

type productPrefetchResponse struct {
	Query       string                              `json:"query"`
	Limit       int                                 `json:"limit"`
	Suggestions []productPrefetchSuggestionResponse `json:"suggestions"`
}

type scoredProductPrefetchSuggestion struct {
	productPrefetchSuggestionResponse
	candidateTenant string
}

func (h *HorizonHandlers) GetProductPrefetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.productDictionary == nil || h.service == nil || h.service.Fragments() == nil {
		http.Error(w, "Product dictionary unavailable", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	req := productPrefetchRequest{
		Query: strings.TrimSpace(r.URL.Query().Get("query")),
		Limit: parseIntOrDefault(strings.TrimSpace(r.URL.Query().Get("limit")), 8),
	}
	if r.Method == http.MethodPost {
		var body productPrefetchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if strings.TrimSpace(body.Query) != "" {
				req.Query = strings.TrimSpace(body.Query)
			}
			if body.Limit > 0 {
				req.Limit = body.Limit
			}
		}
	}
	if req.Limit <= 0 {
		req.Limit = 8
	}
	if req.Limit > 20 {
		req.Limit = 20
	}

	suggestions, err := h.findProductPrefetchSuggestions(r.Context(), authInfo.TenantID, req.Query, req.Limit)
	if err != nil {
		http.Error(w, "Failed to prefetch products: "+strings.TrimSpace(err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(productPrefetchResponse{
		Query:       req.Query,
		Limit:       req.Limit,
		Suggestions: suggestions,
	})
}

func (h *HorizonHandlers) findProductPrefetchSuggestions(ctx context.Context, tenantID string, query string, limit int) ([]productPrefetchSuggestionResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []productPrefetchSuggestionResponse{}, nil
	}
	rawLimit := limit * 4
	if rawLimit < 12 {
		rawLimit = 12
	}
	if rawLimit > 50 {
		rawLimit = 50
	}

	candidates := make([]*horizonmodels.HorizonFragment, 0, rawLimit+4)
	if strings.HasPrefix(query, "/product/") {
		canonicalPath, _, err := h.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, query)
		if err == nil && strings.TrimSpace(canonicalPath) != "" {
			if frag, ferr := h.service.Fragments().GetByCanonicalPathForTenant(ctx, tenantID, canonicalPath); ferr == nil && frag != nil {
				candidates = append(candidates, frag)
			}
		}
	}

	exact, err := h.service.Fragments().FindByLabelForTenant(ctx, tenantID, "en", query, horizonmodels.FragmentTypeProduct)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, filterProductFragments(exact)...)

	prefix, err := h.service.Fragments().SearchPrefixForTenant(ctx, tenantID, query, rawLimit)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, filterProductFragments(prefix)...)

	return rankProductPrefetchSuggestions(ctx, tenantID, query, candidates, h, limit)
}

func rankProductPrefetchSuggestions(ctx context.Context, tenantID string, query string, candidates []*horizonmodels.HorizonFragment, h *HorizonHandlers, limit int) ([]productPrefetchSuggestionResponse, error) {
	queryNorm := strings.ToLower(strings.TrimSpace(query))
	if queryNorm == "" || h == nil || h.productDictionary == nil || h.service == nil || h.service.Fragments() == nil {
		return []productPrefetchSuggestionResponse{}, nil
	}

	canonicalPaths := make([]string, 0, len(candidates))
	canonicalSeen := map[string]struct{}{}
	for _, frag := range candidates {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeProduct {
			continue
		}
		canonicalPath, _, err := h.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, frag.CanonicalPath)
		if err != nil {
			return nil, err
		}
		canonicalPath = firstNonEmpty(strings.TrimSpace(canonicalPath), strings.TrimSpace(frag.CanonicalPath))
		if canonicalPath == "" {
			continue
		}
		if _, ok := canonicalSeen[canonicalPath]; ok {
			continue
		}
		canonicalSeen[canonicalPath] = struct{}{}
		canonicalPaths = append(canonicalPaths, canonicalPath)
	}

	canonicalFrags, err := h.service.Fragments().BatchGetByCanonicalPathsForTenant(ctx, tenantID, canonicalPaths)
	if err != nil {
		return nil, err
	}

	bestByCanonical := map[string]scoredProductPrefetchSuggestion{}
	for _, frag := range candidates {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeProduct {
			continue
		}
		canonicalPath, _, err := h.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, frag.CanonicalPath)
		if err != nil {
			return nil, err
		}
		canonicalPath = firstNonEmpty(strings.TrimSpace(canonicalPath), strings.TrimSpace(frag.CanonicalPath))
		if canonicalPath == "" {
			continue
		}
		canonicalFrag := canonicalFrags[canonicalPath]
		label := fragmentDisplayName(canonicalFrag)
		if label == "" {
			label = fragmentDisplayName(frag)
		}
		matchedLabel := fragmentDisplayName(frag)
		score := scoreProductPrefetchCandidate(queryNorm, tenantID, frag, canonicalPath, label)
		suggestion := scoredProductPrefetchSuggestion{
			productPrefetchSuggestionResponse: productPrefetchSuggestionResponse{
				CanonicalPath: canonicalPath,
				Label:         label,
				MatchedLabel:  matchedLabel,
				MatchedPath:   strings.TrimSpace(frag.CanonicalPath),
				Score:         score,
			},
			candidateTenant: strings.TrimSpace(frag.TenantID),
		}
		if frag.CanonicalPath != canonicalPath {
			suggestion.CanonicalizedFrom = strings.TrimSpace(frag.CanonicalPath)
		}
		if strings.TrimSpace(frag.TenantID) != "" && strings.TrimSpace(frag.TenantID) == strings.TrimSpace(tenantID) && strings.TrimSpace(tenantID) != "global" {
			suggestion.SourceScope = "tenant"
		} else {
			suggestion.SourceScope = "global"
		}

		current, ok := bestByCanonical[canonicalPath]
		if !ok || productPrefetchSuggestionLess(current, suggestion, tenantID) {
			bestByCanonical[canonicalPath] = suggestion
		}
	}

	out := make([]productPrefetchSuggestionResponse, 0, len(bestByCanonical))
	for _, suggestion := range bestByCanonical {
		out = append(out, suggestion.productPrefetchSuggestionResponse)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		return out[i].CanonicalPath < out[j].CanonicalPath
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func scoreProductPrefetchCandidate(queryNorm string, tenantID string, frag *horizonmodels.HorizonFragment, canonicalPath string, canonicalLabel string) int {
	if frag == nil {
		return 0
	}
	label := strings.ToLower(strings.TrimSpace(fragmentDisplayName(frag)))
	canonicalLabelNorm := strings.ToLower(strings.TrimSpace(canonicalLabel))
	path := strings.ToLower(strings.TrimSpace(frag.CanonicalPath))
	canonicalPathNorm := strings.ToLower(strings.TrimSpace(canonicalPath))
	score := 0

	if label == queryNorm || canonicalLabelNorm == queryNorm {
		score += 120
	}
	if path == queryNorm || canonicalPathNorm == queryNorm {
		score += 110
	}
	if strings.HasPrefix(label, queryNorm) || strings.HasPrefix(canonicalLabelNorm, queryNorm) {
		score += 40
	}
	if strings.HasPrefix(path, queryNorm) || strings.HasPrefix(canonicalPathNorm, queryNorm) {
		score += 30
	}
	if strings.Contains(label, queryNorm) || strings.Contains(canonicalLabelNorm, queryNorm) {
		score += 10
	}
	if strings.TrimSpace(frag.CanonicalPath) == strings.TrimSpace(canonicalPath) {
		score += 8
	}
	if tenant := strings.TrimSpace(frag.TenantID); tenant != "" && tenant == strings.TrimSpace(tenantID) && tenant != "global" {
		score += 6
	}
	if strings.HasPrefix(canonicalPathNorm, "/product/") {
		score += 4
	}
	return score
}

func productPrefetchSuggestionLess(current scoredProductPrefetchSuggestion, candidate scoredProductPrefetchSuggestion, tenantID string) bool {
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	currentTenant := strings.TrimSpace(current.candidateTenant)
	candidateTenant := strings.TrimSpace(candidate.candidateTenant)
	effectiveTenant := strings.TrimSpace(tenantID)
	if candidateTenant == effectiveTenant && currentTenant != effectiveTenant {
		return true
	}
	if currentTenant == effectiveTenant && candidateTenant != effectiveTenant {
		return false
	}
	return candidate.Label < current.Label
}
