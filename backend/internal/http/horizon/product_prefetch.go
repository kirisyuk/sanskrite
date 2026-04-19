package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"unicode"

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
	pairBackedSuggestions, seenCanonicals, err := h.findPairBackedProductPrefetchSuggestions(ctx, tenantID, query, limit)
	if err != nil {
		return nil, err
	}
	if len(pairBackedSuggestions) >= limit {
		return pairBackedSuggestions[:limit], nil
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

	for _, variant := range productPrefetchQueryVariants(query) {
		exact, err := h.service.Fragments().FindByLabelForTenant(ctx, tenantID, "en", variant, FragmentTypeProduct)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, filterProductFragments(exact)...)

		prefix, err := h.service.Fragments().SearchPrefixForTenant(ctx, tenantID, variant, rawLimit)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, filterProductFragments(prefix)...)
	}

	fallbackSuggestions, err := rankProductPrefetchSuggestions(ctx, tenantID, query, candidates, h, limit)
	if err != nil {
		return nil, err
	}
	out := make([]productPrefetchSuggestionResponse, 0, limit)
	for _, item := range pairBackedSuggestions {
		out = append(out, item)
	}
	for _, item := range fallbackSuggestions {
		cp := strings.TrimSpace(item.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seenCanonicals[cp]; ok {
			continue
		}
		out = append(out, item)
		seenCanonicals[cp] = struct{}{}
		if len(out) >= limit {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func productPrefetchQueryVariants(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	appendVariant := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	appendVariant(query)
	appendVariant(strings.ToLower(query))
	appendVariant(titleCaseQuery(query))
	return out
}

func titleCaseQuery(query string) string {
	parts := strings.Fields(strings.TrimSpace(query))
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		runes := []rune(strings.ToLower(part))
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func (h *HorizonHandlers) findPairBackedProductPrefetchSuggestions(ctx context.Context, tenantID string, query string, limit int) ([]productPrefetchSuggestionResponse, map[string]struct{}, error) {
	if h == nil || h.productDictionary == nil || h.service == nil || h.service.Fragments() == nil {
		return []productPrefetchSuggestionResponse{}, map[string]struct{}{}, nil
	}
	paths, err := h.productDictionary.SearchCanonicalProductPathsForTenant(ctx, tenantID, query, max(limit*3, 12))
	if err != nil {
		return nil, nil, err
	}
	if len(paths) == 0 {
		return []productPrefetchSuggestionResponse{}, map[string]struct{}{}, nil
	}
	frags, err := h.service.Fragments().BatchGetByCanonicalPathsForTenant(ctx, tenantID, paths)
	if err != nil {
		return nil, nil, err
	}
	queryNorm := normalizeProductPrefetchText(query)
	out := make([]productPrefetchSuggestionResponse, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		seen[path] = struct{}{}
		frag := frags[path]
		label := fragmentDisplayName(frag)
		if label == "" {
			label = humanizeProductPath(path)
		}
		out = append(out, productPrefetchSuggestionResponse{
			CanonicalPath: path,
			Label:         label,
			MatchedLabel:  label,
			MatchedPath:   path,
			SourceScope:   sourceScopeForFragment(tenantID, frag),
			Score:         500 + scoreProductPrefetchPath(queryNorm, path, label),
		})
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
	if len(out) > limit {
		out = out[:limit]
	}
	return out, seen, nil
}

func rankProductPrefetchSuggestions(ctx context.Context, tenantID string, query string, candidates []*horizonmodels.HorizonFragment, h *HorizonHandlers, limit int) ([]productPrefetchSuggestionResponse, error) {
	queryNorm := normalizeProductPrefetchText(query)
	if queryNorm == "" || h == nil || h.productDictionary == nil || h.service == nil || h.service.Fragments() == nil {
		return []productPrefetchSuggestionResponse{}, nil
	}

	canonicalPaths := make([]string, 0, len(candidates))
	canonicalSeen := map[string]struct{}{}
	for _, frag := range candidates {
		if frag == nil || frag.Class != FragmentTypeProduct {
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
		if frag == nil || frag.Class != FragmentTypeProduct {
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
			suggestion.SourceScope = sourceScopeForFragment(tenantID, frag)

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
	label := normalizeProductPrefetchText(fragmentDisplayName(frag))
	canonicalLabelNorm := normalizeProductPrefetchText(canonicalLabel)
	path := normalizeProductPrefetchText(frag.CanonicalPath)
	canonicalPathNorm := normalizeProductPrefetchText(canonicalPath)
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

func scoreProductPrefetchPath(queryNorm string, canonicalPath string, label string) int {
	labelNorm := normalizeProductPrefetchText(label)
	pathNorm := normalizeProductPrefetchText(canonicalPath)
	score := 0
	if labelNorm == queryNorm {
		score += 120
	}
	if pathNorm == queryNorm {
		score += 110
	}
	if strings.HasPrefix(labelNorm, queryNorm) {
		score += 40
	}
	if strings.HasPrefix(pathNorm, queryNorm) {
		score += 30
	}
	if strings.Contains(labelNorm, queryNorm) {
		score += 10
	}
	if strings.Contains(pathNorm, queryNorm) {
		score += 6
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

func normalizeProductPrefetchText(value string) string {
	replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", "(", " ", ")", " ")
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(replacer.Replace(value)))), " ")
}

func humanizeProductPath(path string) string {
	segment := lastNonEmptyPathSegment(path)
	if segment == "" {
		return path
	}
	parts := strings.Fields(strings.ReplaceAll(segment, "-", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func lastNonEmptyPathSegment(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func sourceScopeForFragment(tenantID string, frag *horizonmodels.HorizonFragment) string {
	if frag == nil {
		return "global"
	}
	if strings.TrimSpace(frag.TenantID) != "" && strings.TrimSpace(frag.TenantID) == strings.TrimSpace(tenantID) && strings.TrimSpace(tenantID) != "global" {
		return "tenant"
	}
	return "global"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
