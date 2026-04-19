package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonproductdict "github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

type productStreamRequest struct {
	Query       string `json:"query,omitempty"`
	ProductRef  string `json:"productRef,omitempty"`
	Direction   string `json:"direction,omitempty"`
	Hops        int    `json:"hops,omitempty"`
	LimitPerHop int    `json:"limitPerHop,omitempty"`
}

type productStreamNodeResponse struct {
	CanonicalPath string `json:"canonicalPath"`
	Label         string `json:"label,omitempty"`
	Hop           int    `json:"hop"`
}

type productStreamEdgeResponse struct {
	FromPath     string `json:"fromPath"`
	ToPath       string `json:"toPath"`
	RelationKind string `json:"relationKind"`
}

type productStreamResponse struct {
	Query         string                      `json:"query"`
	ResolvedPath  string                      `json:"resolvedPath"`
	ResolvedLabel string                      `json:"resolvedLabel,omitempty"`
	Direction     string                      `json:"direction"`
	Hops          int                         `json:"hops"`
	LimitPerHop   int                         `json:"limitPerHop"`
	Truncated     bool                        `json:"truncated"`
	Cache         map[string]bool             `json:"cache"`
	Nodes         []productStreamNodeResponse `json:"nodes"`
	Edges         []productStreamEdgeResponse `json:"edges"`
}

func (h *HorizonHandlers) GetProductStream(w http.ResponseWriter, r *http.Request) {
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

	req := productStreamRequest{
		Query:       strings.TrimSpace(r.URL.Query().Get("query")),
		ProductRef:  strings.TrimSpace(r.URL.Query().Get("productRef")),
		Direction:   strings.TrimSpace(r.URL.Query().Get("direction")),
		Hops:        parseIntOrDefault(strings.TrimSpace(r.URL.Query().Get("hops")), 1),
		LimitPerHop: parseIntOrDefault(strings.TrimSpace(r.URL.Query().Get("limitPerHop")), 25),
	}
	if r.Method == http.MethodPost {
		var body productStreamRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if strings.TrimSpace(body.Query) != "" {
				req.Query = strings.TrimSpace(body.Query)
			}
			if strings.TrimSpace(body.ProductRef) != "" {
				req.ProductRef = strings.TrimSpace(body.ProductRef)
			}
			if strings.TrimSpace(body.Direction) != "" {
				req.Direction = strings.TrimSpace(body.Direction)
			}
			if body.Hops > 0 {
				req.Hops = body.Hops
			}
			if body.LimitPerHop > 0 {
				req.LimitPerHop = body.LimitPerHop
			}
		}
	}
	if req.Hops <= 0 {
		req.Hops = 1
	}
	if req.Hops > 3 {
		req.Hops = 3
	}
	if req.LimitPerHop <= 0 {
		req.LimitPerHop = 25
	}
	if req.LimitPerHop > 200 {
		req.LimitPerHop = 200
	}

	seed, err := h.resolveProductStreamSeed(r, authInfo.TenantID, firstNonEmpty(req.ProductRef, req.Query))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	expansion, err := h.productDictionary.ExpandProductStreamForTenant(r.Context(), authInfo.TenantID, seed.CanonicalPath, req.Direction, req.Hops, req.LimitPerHop)
	if err != nil {
		http.Error(w, "Failed to expand product stream: "+strings.TrimSpace(err.Error()), http.StatusInternalServerError)
		return
	}

	pathSet := make([]string, 0, len(expansion.Nodes))
	for _, node := range expansion.Nodes {
		pathSet = append(pathSet, node.CanonicalPath)
	}
	fragByPath, _ := h.service.Fragments().BatchGetByCanonicalPathsForTenant(r.Context(), authInfo.TenantID, pathSet)
	nodes := make([]productStreamNodeResponse, 0, len(expansion.Nodes))
	for _, node := range expansion.Nodes {
		label := ""
		if frag := fragByPath[strings.TrimSpace(node.CanonicalPath)]; frag != nil {
			label = fragmentDisplayName(frag)
		}
		nodes = append(nodes, productStreamNodeResponse{
			CanonicalPath: node.CanonicalPath,
			Label:         label,
			Hop:           node.Hop,
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Hop != nodes[j].Hop {
			return nodes[i].Hop < nodes[j].Hop
		}
		return nodes[i].CanonicalPath < nodes[j].CanonicalPath
	})

	edges := make([]productStreamEdgeResponse, 0, len(expansion.Edges))
	for _, edge := range expansion.Edges {
		edges = append(edges, productStreamEdgeResponse{
			FromPath:     edge.FromPath,
			ToPath:       edge.ToPath,
			RelationKind: edge.RelationKind,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(productStreamResponse{
		Query:         firstNonEmpty(req.ProductRef, req.Query, seed.CanonicalPath),
		ResolvedPath:  seed.CanonicalPath,
		ResolvedLabel: fragmentDisplayName(seed),
		Direction:     expansion.Direction,
		Hops:          expansion.MaxHops,
		LimitPerHop:   req.LimitPerHop,
		Truncated:     expansion.Truncated,
		Cache: map[string]bool{
			"redis":     expansion.UsedRedis,
			"firestore": expansion.UsedFs,
		},
		Nodes: nodes,
		Edges: edges,
	})
}

func (h *HorizonHandlers) resolveProductStreamSeed(r *http.Request, tenantID string, query string) (*horizonmodels.HorizonFragment, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query or productRef is required")
	}
	ctx := r.Context()

	if strings.HasPrefix(query, "/product/") {
		canonicalPath, _, err := h.productDictionary.CanonicalizeProductPathForTenant(ctx, tenantID, query)
		if err != nil {
			return nil, err
		}
		frag, err := h.service.Fragments().GetByCanonicalPathForTenant(ctx, tenantID, canonicalPath)
		if err == nil && frag != nil && frag.Class == FragmentTypeProduct {
			return frag, nil
		}
	}

	exact, _ := h.service.Fragments().FindByLabelForTenant(ctx, tenantID, "en", query, FragmentTypeProduct)
	if seed := bestProductSeedCandidate(ctx, tenantID, query, exact, h.productDictionary); seed != nil {
		return seed, nil
	}

	prefix, _ := h.service.Fragments().SearchPrefixForTenant(ctx, tenantID, query, 20)
	if seed := bestProductSeedCandidate(ctx, tenantID, query, filterProductFragments(prefix), h.productDictionary); seed != nil {
		return seed, nil
	}

	semantic, _ := h.service.SearchFragments(ctx, query, 20)
	if seed := bestProductSeedCandidate(ctx, tenantID, query, filterProductFragments(semantic), h.productDictionary); seed != nil {
		return seed, nil
	}
	return nil, fmt.Errorf("product not found")
}

func bestProductSeedCandidate(ctx context.Context, tenantID string, query string, fragments []*horizonmodels.HorizonFragment, dict *horizonproductdict.Runtime) *horizonmodels.HorizonFragment {
	queryNorm := strings.ToLower(strings.TrimSpace(query))
	var best *horizonmodels.HorizonFragment
	bestScore := -1
	for _, frag := range fragments {
		if frag == nil || frag.Class != FragmentTypeProduct {
			continue
		}
		score := 0
		label := strings.ToLower(strings.TrimSpace(fragmentDisplayName(frag)))
		cp := strings.ToLower(strings.TrimSpace(frag.CanonicalPath))
		if label == queryNorm {
			score += 100
		}
		if cp == queryNorm {
			score += 95
		}
		if strings.HasPrefix(label, queryNorm) {
			score += 20
		}
		if strings.Contains(label, queryNorm) {
			score += 10
		}
		if strings.HasPrefix(cp, "/product/") {
			score += 5
		}
		if dict != nil {
			if canonical, matched, err := dict.CanonicalizeProductPathForTenant(ctx, tenantID, frag.CanonicalPath); err == nil && matched && canonical == frag.CanonicalPath {
				score += 1
			}
		}
		if score > bestScore {
			best = frag
			bestScore = score
		}
	}
	return best
}

func filterProductFragments(in []*horizonmodels.HorizonFragment) []*horizonmodels.HorizonFragment {
	out := make([]*horizonmodels.HorizonFragment, 0, len(in))
	seen := map[string]struct{}{}
	for _, frag := range in {
		if frag == nil || frag.Class != FragmentTypeProduct {
			continue
		}
		key := strings.TrimSpace(frag.CanonicalPath)
		if key == "" {
			key = strings.TrimSpace(frag.ID)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, frag)
	}
	return out
}

func parseIntOrDefault(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return fallback
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
