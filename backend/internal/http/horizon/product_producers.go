package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type productProducersRequest struct {
	Query      string `json:"query,omitempty"`
	ProductRef string `json:"productRef,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type productProducerResponse struct {
	FragmentID    string `json:"fragmentId"`
	CanonicalPath string `json:"canonicalPath,omitempty"`
	Label         string `json:"label,omitempty"`
	FragmentType  string `json:"fragmentType,omitempty"`
	GeoSlug       string `json:"geoSlug,omitempty"`
	SourceScope   string `json:"sourceScope,omitempty"`
	EdgeCount     int    `json:"edgeCount,omitempty"`
}

type productProducersResponse struct {
	Query         string                    `json:"query"`
	ResolvedPath  string                    `json:"resolvedPath"`
	ResolvedLabel string                    `json:"resolvedLabel,omitempty"`
	Limit         int                       `json:"limit"`
	Producers     []productProducerResponse `json:"producers"`
}

type productProducerCandidate struct {
	fragment    *horizonmodels.HorizonFragment
	sourceScope string
	edgeCount   int
}

func (h *HorizonHandlers) GetProductProducers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.productDictionary == nil || h.service == nil || h.service.Fragments() == nil || h.service.Edges() == nil {
		http.Error(w, "Product producer lookup unavailable", http.StatusServiceUnavailable)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	req := productProducersRequest{
		Query:      strings.TrimSpace(r.URL.Query().Get("query")),
		ProductRef: strings.TrimSpace(r.URL.Query().Get("productRef")),
		Limit:      parseIntOrDefault(strings.TrimSpace(r.URL.Query().Get("limit")), 8),
	}
	if r.Method == http.MethodPost {
		var body productProducersRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if strings.TrimSpace(body.Query) != "" {
				req.Query = strings.TrimSpace(body.Query)
			}
			if strings.TrimSpace(body.ProductRef) != "" {
				req.ProductRef = strings.TrimSpace(body.ProductRef)
			}
			if body.Limit > 0 {
				req.Limit = body.Limit
			}
		}
	}
	if req.Limit <= 0 {
		req.Limit = 8
	}
	if req.Limit > 24 {
		req.Limit = 24
	}

	seed, err := h.resolveProductStreamSeed(r, authInfo.TenantID, firstNonEmpty(req.ProductRef, req.Query))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	producers, err := h.lookupProductProducers(r, authInfo.TenantID, seed.CanonicalPath, req.Limit)
	if err != nil {
		http.Error(w, "Failed to load product producers: "+strings.TrimSpace(err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(productProducersResponse{
		Query:         firstNonEmpty(req.ProductRef, req.Query, seed.CanonicalPath),
		ResolvedPath:  seed.CanonicalPath,
		ResolvedLabel: fragmentDisplayName(seed),
		Limit:         req.Limit,
		Producers:     producers,
	})
}

func (h *HorizonHandlers) lookupProductProducers(r *http.Request, tenantID string, canonicalPath string, limit int) ([]productProducerResponse, error) {
	ctx := r.Context()
	targets := h.collectProducerTargets(ctx, tenantID, canonicalPath)
	edgeType := horizonmodels.DependencyTypeProduces
	candidates := map[string]*productProducerCandidate{}

	for _, target := range targets {
		if strings.TrimSpace(target.fragmentID) == "" || strings.TrimSpace(target.scope) == "" {
			continue
		}
		edges, err := h.service.Edges().ListByTarget(ctx, target.scope, target.fragmentID, &edgeType, max(limit*12, 96))
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edge == nil || strings.TrimSpace(edge.SourceFragmentID) == "" {
				continue
			}
			sourceFrag, err := h.service.Fragments().Get(ctx, edge.SourceFragmentID)
			if err != nil || sourceFrag == nil || !isProducerFragmentType(sourceFrag.Type) {
				continue
			}
			mergeProductProducerCandidate(candidates, sourceFrag, target.scope)
		}
	}

	out := make([]productProducerResponse, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.fragment == nil {
			continue
		}
		out = append(out, productProducerResponse{
			FragmentID:    strings.TrimSpace(candidate.fragment.ID),
			CanonicalPath: strings.TrimSpace(candidate.fragment.CanonicalPath),
			Label:         fragmentDisplayName(candidate.fragment),
			FragmentType:  string(candidate.fragment.Type),
			GeoSlug:       strings.TrimSpace(candidate.fragment.GeoSlug),
			SourceScope:   strings.TrimSpace(candidate.sourceScope),
			EdgeCount:     candidate.edgeCount,
		})
	}
	sortProductProducers(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type producerTarget struct {
	scope      string
	fragmentID string
}

func (h *HorizonHandlers) collectProducerTargets(ctx context.Context, tenantID string, canonicalPath string) []producerTarget {
	targets := make([]producerTarget, 0, 2)
	seen := map[string]struct{}{}

	addTarget := func(scope string) {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return
		}
		frag, err := h.service.Fragments().GetByCanonicalPathForTenant(ctx, scope, canonicalPath)
		if err != nil || frag == nil {
			return
		}
		key := strings.TrimSpace(scope) + "|" + strings.TrimSpace(frag.ID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, producerTarget{
			scope:      scope,
			fragmentID: strings.TrimSpace(frag.ID),
		})
	}

	addTarget(strings.TrimSpace(tenantID))
	if strings.TrimSpace(tenantID) != "global" {
		addTarget("global")
	}
	return targets
}

func isProducerFragmentType(fragmentType horizonmodels.FragmentType) bool {
	switch fragmentType {
	case horizonmodels.FragmentTypeOrganization, horizonmodels.FragmentTypeFacility, horizonmodels.FragmentTypeDeposit, horizonmodels.FragmentTypeProgram:
		return true
	default:
		return false
	}
}

func mergeProductProducerCandidate(dst map[string]*productProducerCandidate, frag *horizonmodels.HorizonFragment, sourceScope string) {
	if dst == nil || frag == nil {
		return
	}
	key := strings.TrimSpace(frag.CanonicalPath)
	if key == "" {
		key = strings.TrimSpace(frag.ID)
	}
	if key == "" {
		return
	}
	if existing, ok := dst[key]; ok && existing != nil {
		existing.edgeCount++
		if producerScopeRank(strings.TrimSpace(sourceScope)) > producerScopeRank(strings.TrimSpace(existing.sourceScope)) {
			existing.fragment = frag
			existing.sourceScope = sourceScope
		}
		return
	}
	dst[key] = &productProducerCandidate{
		fragment:    frag,
		sourceScope: sourceScope,
		edgeCount:   1,
	}
}

func producerScopeRank(scope string) int {
	if strings.TrimSpace(scope) == "global" {
		return 1
	}
	if strings.TrimSpace(scope) == "" {
		return 0
	}
	return 2
}

func producerTypeRank(fragmentType string) int {
	switch strings.TrimSpace(fragmentType) {
	case string(horizonmodels.FragmentTypeFacility):
		return 0
	case string(horizonmodels.FragmentTypeOrganization):
		return 1
	case string(horizonmodels.FragmentTypeDeposit):
		return 2
	case string(horizonmodels.FragmentTypeProgram):
		return 3
	default:
		return 4
	}
}

func sortProductProducers(producers []productProducerResponse) {
	sort.Slice(producers, func(i, j int) bool {
		if producerTypeRank(producers[i].FragmentType) != producerTypeRank(producers[j].FragmentType) {
			return producerTypeRank(producers[i].FragmentType) < producerTypeRank(producers[j].FragmentType)
		}
		if producers[i].EdgeCount != producers[j].EdgeCount {
			return producers[i].EdgeCount > producers[j].EdgeCount
		}
		return strings.ToLower(strings.TrimSpace(producers[i].Label)) < strings.ToLower(strings.TrimSpace(producers[j].Label))
	})
}
