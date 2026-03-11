package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	appcache "github.com/igorkirisyuk/sanskrite/internal/cache"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	redis "github.com/redis/go-redis/v9"
)

var (
	supplyChainSearchRedisOnce sync.Once
	supplyChainSearchRedis     *redis.Client
)

type supplyChainSearchRequest struct {
	Anchors []string `json:"anchors"`
	MaxLegs int      `json:"maxLegs,omitempty"`
	Limit   int      `json:"limit,omitempty"`
	LensID  string   `json:"lensId,omitempty"`
}

type supplyChainSearchResult struct {
	Artifact           *horizonmodels.MaterializedArtifact `json:"artifact"`
	PreviewPayload     map[string]interface{}              `json:"previewPayload,omitempty"`
	Match              map[string]interface{}              `json:"match,omitempty"`
	Score              float64                             `json:"score"`
	SupplyLinkCoverage map[string]interface{}              `json:"supplyLinkCoverage,omitempty"`
}

type supplyChainSearchAnchor struct {
	CanonicalPath string `json:"canonicalPath"`
	Name          string `json:"name"`
	Type          string `json:"type,omitempty"`
}

func (h *HorizonHandlers) SearchSupplyChains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if h.materializedArtifactsRepo == nil {
		http.Error(w, "Supply chains not available", http.StatusServiceUnavailable)
		return
	}

	var req supplyChainSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Anchors = normalizeSupplyChainAnchors(req.Anchors)
	if len(req.Anchors) == 0 || len(req.Anchors) > 2 {
		http.Error(w, "anchors must contain one or two canonical paths", http.StatusBadRequest)
		return
	}
	if req.MaxLegs <= 0 {
		req.MaxLegs = 6
	}
	if req.MaxLegs > 12 {
		req.MaxLegs = 12
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 50 {
		req.Limit = 50
	}
	tenantID := strings.TrimSpace(authInfo.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	cacheKey := fmt.Sprintf("h:supply-chain-search:v2:%s:%s:%s:%s:%d:%d",
		tenantID,
		strings.TrimSpace(req.LensID),
		req.Anchors[0],
		secondAnchor(req.Anchors),
		req.MaxLegs,
		req.Limit,
	)
	if payload, ok := getSupplyChainSearchCache(r.Context(), cacheKey); ok {
		var cached map[string]interface{}
		if err := json.Unmarshal(payload, &cached); err == nil {
			if cacheMeta, ok := cached["cache"].(map[string]interface{}); ok {
				cacheMeta["hit"] = true
			} else {
				cached["cache"] = map[string]interface{}{"hit": true, "ttlSec": 600}
			}
			if patched, merr := json.Marshal(cached); merr == nil {
				payload = patched
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
		return
	}

	candidates, err := h.searchSupplyChainCandidates(r, tenantID, req)
	if err != nil {
		http.Error(w, "Failed to search supply chains", http.StatusInternalServerError)
		return
	}

	results := make([]supplyChainSearchResult, 0, len(candidates))
	linkageSummary := map[string]int{
		"fullyLinked":        0,
		"repairedAtReadTime": 0,
		"unresolved":         0,
	}
	pinnedCount := 0
	directedCount := 0
	clippedCount := 0
	for _, artifact := range candidates {
		artifactCopy := *artifact
		summary, err := horizonmodels.RepairSupplyChainPathLinkage(&artifactCopy, nil)
		if err != nil || !summary.FullyLinked {
			linkageSummary["unresolved"]++
			continue
		}
		if summary.Changed {
			linkageSummary["repairedAtReadTime"]++
			_ = h.materializedArtifactsRepo.Update(r.Context(), &artifactCopy)
		} else {
			linkageSummary["fullyLinked"]++
		}
		score, match, previewPayload := buildSupplyChainSearchProjection(&artifactCopy, req.Anchors, req.MaxLegs)
		if artifactCopy.IsPinned {
			pinnedCount++
		}
		if directed, _ := match["directedMatch"].(bool); directed {
			directedCount++
		}
		if clipped, _ := match["clipped"].(bool); clipped {
			clippedCount++
		}
		results = append(results, supplyChainSearchResult{
			Artifact:       &artifactCopy,
			PreviewPayload: previewPayload,
			Match:          match,
			Score:          score,
			SupplyLinkCoverage: map[string]interface{}{
				"linked":      summary.Linked,
				"resolvedIds": summary.ResolvedIDs,
				"unresolved":  summary.Unresolved,
			},
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Artifact.UpdatedAt.After(results[j].Artifact.UpdatedAt)
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	response := map[string]interface{}{
		"mode": func() string {
			if len(req.Anchors) == 2 {
				return "between_anchors"
			}
			return "single_anchor"
		}(),
		"anchors": buildSupplyChainSearchAnchors(req.Anchors),
		"results": results,
		"summary": map[string]interface{}{
			"total":       len(results),
			"pinned":      pinnedCount,
			"direct":      directedCount,
			"directional": directedCount,
			"clipped":     clippedCount,
		},
		"linkageSummary": linkageSummary,
		"cache": map[string]interface{}{
			"hit":    false,
			"key":    cacheKey,
			"ttlSec": 600,
		},
	}
	raw, _ := json.Marshal(response)
	setSupplyChainSearchCache(context.Background(), cacheKey, raw, 10*time.Minute)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// GetSupplyChainsByNode returns materialized supply chain artifacts (SUPPLY_CHAIN_PATH)
// where nodeRefs contains the given canonical path.
// GET /api/horizon/supply-chains/by-node?path=/org/catl
// Query uses FindByNodeRef(tenantId, path, SUPPLY_CHAIN_PATH).
func (h *HorizonHandlers) GetSupplyChainsByNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	canonicalPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if canonicalPath == "" {
		http.Error(w, "path query parameter is required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(canonicalPath, "/") {
		http.Error(w, "path must be a canonical path (start with /)", http.StatusBadRequest)
		return
	}

	if h.materializedArtifactsRepo == nil {
		http.Error(w, "Supply chains not available", http.StatusServiceUnavailable)
		return
	}

	tenantID := strings.TrimSpace(authInfo.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}

	artifacts, err := h.materializedArtifactsRepo.FindByNodeRef(
		r.Context(),
		tenantID,
		canonicalPath,
		horizonmodels.MaterializedKindSupplyChainPath,
	)
	if err != nil {
		http.Error(w, "Failed to fetch supply chains", http.StatusInternalServerError)
		return
	}

	// Also try "global" tenant if tenant-specific returned nothing
	if len(artifacts) == 0 && tenantID != "global" {
		artifacts, err = h.materializedArtifactsRepo.FindByNodeRef(
			r.Context(),
			"global",
			canonicalPath,
			horizonmodels.MaterializedKindSupplyChainPath,
		)
		if err != nil {
			http.Error(w, "Failed to fetch supply chains", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"path":     canonicalPath,
		"tenantId": tenantID,
		"chains":   artifacts,
		"total":    len(artifacts),
	})
}

// GetSupplyChainsByLens returns materialized supply chain artifacts (SUPPLY_CHAIN_PATH) for a lens.
// GET /api/horizon/supply-chains/by-lens?lensId=...
func (h *HorizonHandlers) GetSupplyChainsByLens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	lensID := strings.TrimSpace(r.URL.Query().Get("lensId"))
	if lensID == "" {
		http.Error(w, "lensId query parameter is required", http.StatusBadRequest)
		return
	}

	if h.materializedArtifactsRepo == nil {
		http.Error(w, "Supply chains not available", http.StatusServiceUnavailable)
		return
	}
	if h.lensItemsHandler == nil || h.lensItemsHandler.lensesRepo == nil {
		http.Error(w, "Lens repository not available", http.StatusServiceUnavailable)
		return
	}

	lens, err := h.lensItemsHandler.lensesRepo.Get(r.Context(), lensID)
	if err != nil || lens == nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	const limit = 100
	scopedChains, discoveredChains, err := h.collectSupplyChainsForLens(r, lens, limit)
	if err != nil {
		http.Error(w, "Failed to fetch supply chains", http.StatusInternalServerError)
		return
	}
	chains := append(scopedChains, discoveredChains...)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"lensId":          lensID,
		"chains":          chains,
		"total":           len(chains),
		"scopedTotal":     len(scopedChains),
		"discoveredTotal": len(discoveredChains),
	})
}

type supplyChainLensContext struct {
	NodeRefs         []string
	FlowRefs         []string
	ZoneRefs         []string
	AffectedZoneRefs []string
	ArtifactKeys     []string
}

func (h *HorizonHandlers) collectSupplyChainsForLens(r *http.Request, lens *horizonmodels.Lens, limit int) ([]*horizonmodels.MaterializedArtifact, []*horizonmodels.MaterializedArtifact, error) {
	ctx := r.Context()
	tenantID := strings.TrimSpace(lens.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	contextRefs := buildSupplyChainLensContext(lens)
	selected := make(map[string]*horizonmodels.MaterializedArtifact, limit*2)
	scopedIDs := make(map[string]struct{}, limit)

	appendArtifact := func(artifact *horizonmodels.MaterializedArtifact, pinned bool, source string, matched []string, overlapScore float64) {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			return
		}
		horizonmodels.PopulateSupplyChainPathIndexes(artifact)
		if existing := selected[artifact.ID]; existing != nil {
			if pinned && !existing.IsPinned {
				existing.IsPinned = true
				existing.DiscoverySource = "scoped"
				existing.DiscoveryReason = "attached to lens scope"
			}
			if overlapScore > existing.OverlapScore {
				existing.OverlapScore = overlapScore
				existing.MatchedRefs = append([]string(nil), matched...)
				if !existing.IsPinned {
					existing.DiscoverySource = source
					existing.DiscoveryReason = describeSupplyChainDiscovery(source, matched)
				}
			}
			return
		}
		copyArtifact := *artifact
		copyArtifact.IsPinned = pinned
		copyArtifact.MatchedRefs = append([]string(nil), matched...)
		copyArtifact.OverlapScore = overlapScore
		if pinned {
			copyArtifact.DiscoverySource = "scoped"
			copyArtifact.DiscoveryReason = "attached to lens scope"
		} else {
			copyArtifact.DiscoverySource = source
			copyArtifact.DiscoveryReason = describeSupplyChainDiscovery(source, matched)
		}
		annotateSupplyChainTraceArtifact(&copyArtifact, "")
		selected[copyArtifact.ID] = &copyArtifact
		if pinned {
			scopedIDs[copyArtifact.ID] = struct{}{}
		}
	}

	legacyScoped, err := h.materializedArtifactsRepo.ListByLens(ctx, lens.ID, limit)
	if err != nil {
		return nil, nil, err
	}
	for _, artifact := range legacyScoped {
		appendArtifact(artifact, true, "scoped", nil, 1)
	}
	for _, artKey := range contextRefs.ArtifactKeys {
		artifact, err := h.materializedArtifactsRepo.GetByCanonicalKeyAnyKind(ctx, tenantID, artKey)
		if err == nil && artifact != nil {
			appendArtifact(artifact, true, "scoped", nil, 1)
		}
	}

	discoveredLimit := limit * 2
	discoveredQueries := []struct {
		refs   []string
		source string
		find   func([]string) ([]*horizonmodels.MaterializedArtifact, error)
	}{
		{
			refs:   contextRefs.NodeRefs,
			source: "node_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return h.materializedArtifactsRepo.FindByNodeRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.FlowRefs,
			source: "flow_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return h.materializedArtifactsRepo.FindByFlowRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.ZoneRefs,
			source: "zone_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return h.materializedArtifactsRepo.FindByZoneRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
		{
			refs:   contextRefs.AffectedZoneRefs,
			source: "affected_zone_overlap",
			find: func(refs []string) ([]*horizonmodels.MaterializedArtifact, error) {
				return h.materializedArtifactsRepo.FindByAffectedZoneRefsAny(ctx, tenantID, refs, horizonmodels.MaterializedKindSupplyChainPath, discoveredLimit)
			},
		},
	}

	for _, query := range discoveredQueries {
		if len(query.refs) == 0 {
			continue
		}
		artifacts, err := query.find(query.refs)
		if err != nil {
			return nil, nil, err
		}
		for _, artifact := range artifacts {
			score, matched := scoreSupplyChainOverlap(artifact, contextRefs)
			if score <= 0 {
				continue
			}
			appendArtifact(artifact, false, query.source, matched, score)
		}
	}

	scoped := make([]*horizonmodels.MaterializedArtifact, 0, len(scopedIDs))
	discovered := make([]*horizonmodels.MaterializedArtifact, 0, len(selected))
	for _, artifact := range selected {
		if _, ok := scopedIDs[artifact.ID]; ok {
			scoped = append(scoped, artifact)
			continue
		}
		discovered = append(discovered, artifact)
	}
	sortSupplyChainArtifacts(scoped)
	sortSupplyChainArtifacts(discovered)
	if len(scoped) > limit {
		scoped = scoped[:limit]
	}
	remaining := limit - len(scoped)
	if remaining < 0 {
		remaining = 0
	}
	if len(discovered) > remaining {
		discovered = discovered[:remaining]
	}
	return scoped, discovered, nil
}

func (h *HorizonHandlers) searchSupplyChainCandidates(r *http.Request, tenantID string, req supplyChainSearchRequest) ([]*horizonmodels.MaterializedArtifact, error) {
	if strings.TrimSpace(req.LensID) != "" && h.lensItemsHandler != nil && h.lensItemsHandler.lensesRepo != nil {
		lens, err := h.lensItemsHandler.lensesRepo.Get(r.Context(), strings.TrimSpace(req.LensID))
		if err == nil && lens != nil {
			scoped, discovered, cerr := h.collectSupplyChainsForLens(r, lens, max(req.Limit*3, 50))
			if cerr == nil {
				return filterSupplyChainCandidates(append(scoped, discovered...), req.Anchors), nil
			}
		}
	}
	artifacts, err := h.materializedArtifactsRepo.FindByNodeRef(r.Context(), tenantID, req.Anchors[0], horizonmodels.MaterializedKindSupplyChainPath)
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 && tenantID != "global" {
		artifacts, err = h.materializedArtifactsRepo.FindByNodeRef(r.Context(), "global", req.Anchors[0], horizonmodels.MaterializedKindSupplyChainPath)
		if err != nil {
			return nil, err
		}
	}
	return filterSupplyChainCandidates(artifacts, req.Anchors), nil
}

func filterSupplyChainCandidates(artifacts []*horizonmodels.MaterializedArtifact, anchors []string) []*horizonmodels.MaterializedArtifact {
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if _, ok := seen[artifact.ID]; ok {
			continue
		}
		if !artifactContainsAllAnchors(artifact, anchors) {
			continue
		}
		seen[artifact.ID] = struct{}{}
		out = append(out, artifact)
	}
	return out
}

func artifactContainsAllAnchors(artifact *horizonmodels.MaterializedArtifact, anchors []string) bool {
	if artifact == nil {
		return false
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	have := make(map[string]struct{}, len(artifact.NodeRefs))
	for _, ref := range artifact.NodeRefs {
		have[strings.TrimSpace(ref)] = struct{}{}
	}
	for _, anchor := range anchors {
		if _, ok := have[strings.TrimSpace(anchor)]; !ok {
			return false
		}
	}
	return true
}

func buildSupplyChainSearchProjection(artifact *horizonmodels.MaterializedArtifact, anchors []string, maxLegs int) (float64, map[string]interface{}, map[string]interface{}) {
	match := map[string]interface{}{}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return 0, match, artifact.Payload
	}
	matchedLegs := findMatchedLegIndexes(parsed, anchors)
	match["anchorIndexes"] = matchedLegs
	match["legSpan"] = len(matchedLegs)
	score := artifact.PathStressScore + artifact.BindingScore
	if artifact.IsPinned {
		score += 5
	}
	if len(anchors) == 2 && isDirectedAnchorMatch(parsed, anchors[0], anchors[1]) {
		match["directedMatch"] = true
		score += 3
	}
	previewPayload, clipped := clipSupplyChainPayload(parsed, matchedLegs, maxLegs)
	match["clipped"] = clipped
	if !clipped {
		score += 1
	}
	if match["legSpan"].(int) > 0 {
		score += 1 / float64(match["legSpan"].(int))
	}
	return score, match, previewPayload
}

func findMatchedLegIndexes(parsed *horizonmodels.SupplyChainPathPayload, anchors []string) []int {
	out := make([]int, 0, len(parsed.Legs))
	for _, leg := range parsed.Legs {
		for _, anchor := range anchors {
			anchor = strings.TrimSpace(anchor)
			if anchor == "" {
				continue
			}
			if leg.SupplierRef == anchor || leg.CustomerRef == anchor || leg.ProductRef == anchor {
				out = append(out, leg.Index)
				break
			}
		}
	}
	if len(out) == 0 && len(parsed.Legs) > 0 {
		out = append(out, 0)
	}
	return out
}

func isDirectedAnchorMatch(parsed *horizonmodels.SupplyChainPathPayload, anchorA, anchorB string) bool {
	firstA := -1
	firstB := -1
	for _, leg := range parsed.Legs {
		if firstA == -1 && (leg.SupplierRef == anchorA || leg.CustomerRef == anchorA || leg.ProductRef == anchorA) {
			firstA = leg.Index
		}
		if firstB == -1 && (leg.SupplierRef == anchorB || leg.CustomerRef == anchorB || leg.ProductRef == anchorB) {
			firstB = leg.Index
		}
	}
	return firstA >= 0 && firstB >= 0 && firstA <= firstB
}

func clipSupplyChainPayload(parsed *horizonmodels.SupplyChainPathPayload, matchedLegs []int, maxLegs int) (map[string]interface{}, bool) {
	if parsed == nil || len(parsed.Legs) == 0 || maxLegs <= 0 || len(parsed.Legs) <= maxLegs {
		return horizonmodels.BuildSupplyChainPathPayload(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Notes), false
	}
	start := matchedLegs[0]
	end := matchedLegs[len(matchedLegs)-1]
	if end-start+1 > maxLegs {
		end = start + maxLegs - 1
	}
	for end-start+1 < maxLegs {
		if start > 0 {
			start--
		}
		if end-start+1 >= maxLegs {
			break
		}
		if end < len(parsed.Legs)-1 {
			end++
		}
		if start == 0 && end == len(parsed.Legs)-1 {
			break
		}
	}
	keepLegs := parsed.Legs[start : end+1]
	keepSegments := make(map[int]struct{}, len(keepLegs))
	nodeRefs := make(map[string]horizonmodels.SupplyChainPathNode, len(parsed.Nodes))
	for _, leg := range keepLegs {
		keepSegments[leg.Index] = struct{}{}
		for _, ref := range []string{leg.SupplierRef, leg.ProductRef, leg.CustomerRef} {
			for _, node := range parsed.Nodes {
				if node.Ref == ref {
					nodeRefs[ref] = node
				}
			}
		}
		for _, ref := range leg.TransportViaRefs {
			for _, node := range parsed.Nodes {
				if node.Ref == ref {
					nodeRefs[ref] = node
				}
			}
		}
	}
	nodes := make([]horizonmodels.SupplyChainPathNode, 0, len(nodeRefs))
	for _, node := range nodeRefs {
		nodes = append(nodes, node)
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Ref < nodes[j].Ref })
	keptRefs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		keptRefs[node.Ref] = struct{}{}
	}
	edges := make([]horizonmodels.SupplyChainPathEdge, 0, len(parsed.Edges))
	for _, edge := range parsed.Edges {
		if edge.SegmentIndex == nil {
			continue
		}
		if _, ok := keepSegments[*edge.SegmentIndex]; !ok {
			continue
		}
		if edge.From >= 0 && edge.From < len(parsed.Nodes) {
			if _, ok := keptRefs[parsed.Nodes[edge.From].Ref]; !ok {
				continue
			}
		}
		if edge.To >= 0 && edge.To < len(parsed.Nodes) {
			if _, ok := keptRefs[parsed.Nodes[edge.To].Ref]; !ok {
				continue
			}
		}
		edges = append(edges, edge)
	}
	return horizonmodels.BuildSupplyChainPathPayload(nodes, edges, keepLegs, parsed.Stress, parsed.Notes), true
}

func normalizeSupplyChainAnchors(anchors []string) []string {
	out := make([]string, 0, len(anchors))
	seen := make(map[string]struct{}, len(anchors))
	for _, anchor := range anchors {
		anchor = strings.TrimSpace(anchor)
		if anchor == "" || !strings.HasPrefix(anchor, "/") {
			continue
		}
		if _, ok := seen[anchor]; ok {
			continue
		}
		seen[anchor] = struct{}{}
		out = append(out, anchor)
	}
	return out
}

func secondAnchor(anchors []string) string {
	if len(anchors) < 2 {
		return "_"
	}
	return anchors[1]
}

func buildSupplyChainSearchAnchors(anchors []string) []supplyChainSearchAnchor {
	out := make([]supplyChainSearchAnchor, 0, len(anchors))
	for _, anchor := range anchors {
		anchor = strings.TrimSpace(anchor)
		if anchor == "" {
			continue
		}
		out = append(out, supplyChainSearchAnchor{
			CanonicalPath: anchor,
			Name:          displayNameFromCanonicalPath(anchor),
			Type:          inferCanonicalPathType(anchor),
		})
	}
	return out
}

func inferCanonicalPathType(path string) string {
	switch {
	case strings.HasPrefix(path, "/org/"):
		return "ORGANIZATION"
	case strings.HasPrefix(path, "/fac/"):
		return "FACILITY"
	case strings.HasPrefix(path, "/product/"):
		return "PRODUCT"
	case strings.HasPrefix(path, "/market/"):
		return "MARKET"
	case strings.HasPrefix(path, "/zone/"):
		return "AFFECTED_ZONE"
	case strings.HasPrefix(path, "/loc/"), strings.HasPrefix(path, "/region/"):
		return "GEO_REGION"
	default:
		return ""
	}
}

func displayNameFromCanonicalPath(path string) string {
	segments := strings.Split(strings.TrimSpace(path), "/")
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		segment = strings.ReplaceAll(segment, "-", " ")
		segment = strings.ReplaceAll(segment, "_", " ")
		if segment != "" {
			out = append(out, segment)
		}
	}
	if len(out) == 0 {
		return path
	}
	return out[len(out)-1]
}

func getSupplyChainSearchCache(ctx context.Context, key string) ([]byte, bool) {
	client := getSupplyChainSearchRedis()
	if client == nil || strings.TrimSpace(key) == "" {
		return nil, false
	}
	rctx, cancel := context.WithTimeout(ctx, 70*time.Millisecond)
	defer cancel()
	raw, err := client.Get(rctx, key).Bytes()
	return raw, err == nil && len(raw) > 0
}

func setSupplyChainSearchCache(ctx context.Context, key string, payload []byte, ttl time.Duration) {
	client := getSupplyChainSearchRedis()
	if client == nil || strings.TrimSpace(key) == "" || len(payload) == 0 {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 70*time.Millisecond)
	defer cancel()
	_ = client.Set(rctx, key, payload, ttl).Err()
}

func getSupplyChainSearchRedis() *redis.Client {
	supplyChainSearchRedisOnce.Do(func() {
		supplyChainSearchRedis = appcache.NewRedisClient()
	})
	return supplyChainSearchRedis
}

func buildSupplyChainLensContext(lens *horizonmodels.Lens) supplyChainLensContext {
	out := supplyChainLensContext{}
	if lens == nil {
		return out
	}
	out.NodeRefs = mergeUniqueStrings(out.NodeRefs, collectLensScopeNodeRefs(lens))
	for artKey, ref := range lens.ScopeArtifacts {
		if strings.TrimSpace(artKey) != "" {
			out.ArtifactKeys = append(out.ArtifactKeys, artKey)
		}
		out.NodeRefs = mergeUniqueStrings(out.NodeRefs, ref.NodeRefs)
		out.FlowRefs = mergeUniqueStrings(out.FlowRefs, ref.FlowRefs)
		out.ZoneRefs = mergeUniqueStrings(out.ZoneRefs, ref.ZoneRefs)
		out.AffectedZoneRefs = mergeUniqueStrings(out.AffectedZoneRefs, ref.AffectedZoneRefs)
	}
	sort.Strings(out.ArtifactKeys)
	return out
}

func collectLensScopeNodeRefs(lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	refs := make([]string, 0, len(lens.Scope)+len(lens.ScopeFragments))
	for _, entry := range lens.Scope {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		switch {
		case strings.HasPrefix(entry, "/query/"):
			if lens.ResolvedQueries != nil {
				refs = mergeUniqueStrings(refs, lens.ResolvedQueries[entry])
			}
		case strings.HasPrefix(entry, horizonlens.ArtifactScopePrefix):
			continue
		case strings.HasPrefix(entry, "/domain/"):
			continue
		case strings.HasPrefix(entry, "/"):
			refs = mergeUniqueStrings(refs, []string{entry})
		}
	}
	for canonicalPath := range lens.ScopeFragments {
		refs = mergeUniqueStrings(refs, []string{canonicalPath})
	}
	if lens.ResolvedQueries != nil {
		for _, paths := range lens.ResolvedQueries {
			refs = mergeUniqueStrings(refs, paths)
		}
	}
	return refs
}

func scoreSupplyChainOverlap(artifact *horizonmodels.MaterializedArtifact, ctx supplyChainLensContext) (float64, []string) {
	if artifact == nil {
		return 0, nil
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	nodeMatches := intersectCount(artifact.NodeRefs, ctx.NodeRefs)
	flowMatches := intersectCount(artifact.FlowRefs, ctx.FlowRefs)
	zoneMatches := intersectCount(artifact.ZoneRefs, ctx.ZoneRefs)
	affectedMatches := intersectCount(artifact.AffectedZoneRefs, ctx.AffectedZoneRefs)
	score := float64(nodeMatches)*1.0 + float64(flowMatches)*0.75 + float64(zoneMatches)*0.6 + float64(affectedMatches)*0.9
	if score <= 0 {
		return 0, nil
	}
	matched := make([]string, 0, 8)
	matched = append(matched, firstIntersect(artifact.NodeRefs, ctx.NodeRefs, 4)...)
	matched = append(matched, firstIntersect(artifact.FlowRefs, ctx.FlowRefs, 2)...)
	matched = append(matched, firstIntersect(artifact.ZoneRefs, ctx.ZoneRefs, 2)...)
	matched = append(matched, firstIntersect(artifact.AffectedZoneRefs, ctx.AffectedZoneRefs, 2)...)
	return score, mergeUniqueStrings(nil, matched)
}

func describeSupplyChainDiscovery(source string, matched []string) string {
	switch source {
	case "node_overlap":
		return "discovered from overlapping graph entities"
	case "flow_overlap":
		return "discovered from overlapping route bindings"
	case "zone_overlap":
		return "discovered from overlapping zone bindings"
	case "affected_zone_overlap":
		return "discovered from overlapping affected-zone bindings"
	default:
		if len(matched) > 0 {
			return "discovered from lens context overlap"
		}
		return ""
	}
}

func sortSupplyChainArtifacts(artifacts []*horizonmodels.MaterializedArtifact) {
	sort.SliceStable(artifacts, func(i, j int) bool {
		left := artifacts[i]
		right := artifacts[j]
		if left.TracePriority != right.TracePriority {
			return left.TracePriority > right.TracePriority
		}
		if left.IsPinned != right.IsPinned {
			return left.IsPinned
		}
		if left.OverlapScore != right.OverlapScore {
			return left.OverlapScore > right.OverlapScore
		}
		leftStress := artifactPathStressScore(left)
		rightStress := artifactPathStressScore(right)
		if leftStress != rightStress {
			return leftStress > rightStress
		}
		return left.UpdatedAt.After(right.UpdatedAt)
	})
}

func annotateSupplyChainTraceArtifact(artifact *horizonmodels.MaterializedArtifact, centerCanonicalPath string) {
	if artifact == nil {
		return
	}
	horizonmodels.PopulateSupplyChainPathIndexes(artifact)
	artifact.LegCount = 0
	artifact.BindingScore = 0
	artifact.PathStressScore = 0
	artifact.BottleneckLeg = -1
	artifact.TraceRole = classifySupplyChainTraceRole(artifact, centerCanonicalPath)

	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err == nil && parsed != nil {
		artifact.LegCount = len(parsed.Legs)
		artifact.BindingScore = averageLegBindingConfidence(parsed.Legs)
		if parsed.Stress != nil {
			artifact.PathStressScore = parsed.Stress.PathScore
			artifact.BottleneckLeg = parsed.Stress.BottleneckLegIndex
		}
	}

	artifact.TracePriority = scoreSupplyChainTracePriority(artifact)
	artifact.TraceReason = buildSupplyChainTraceReason(artifact, centerCanonicalPath)
}

func classifySupplyChainTraceRole(artifact *horizonmodels.MaterializedArtifact, centerCanonicalPath string) string {
	centerCanonicalPath = strings.TrimSpace(centerCanonicalPath)
	switch {
	case centerCanonicalPath != "" && traceContainsString(artifact.NodeRefs, centerCanonicalPath):
		return "direct_anchor"
	case centerCanonicalPath != "" && traceContainsString(artifact.FlowRefs, centerCanonicalPath):
		return "route_overlap"
	case centerCanonicalPath != "" && traceContainsString(artifact.ZoneRefs, centerCanonicalPath):
		return "zone_overlap"
	case centerCanonicalPath != "" && traceContainsString(artifact.AffectedZoneRefs, centerCanonicalPath):
		return "affected_zone_overlap"
	case artifact.IsPinned:
		return "pinned_context"
	case artifact.DiscoverySource == "node_overlap":
		return "graph_context"
	case artifact.DiscoverySource == "flow_overlap":
		return "route_context"
	case artifact.DiscoverySource == "zone_overlap":
		return "zone_context"
	case artifact.DiscoverySource == "affected_zone_overlap":
		return "affected_context"
	default:
		return "context_overlap"
	}
}

func averageLegBindingConfidence(legs []horizonmodels.SupplyChainPathLeg) float64 {
	if len(legs) == 0 {
		return 0
	}
	total := 0.0
	count := 0
	for _, leg := range legs {
		if leg.BindingConfidence <= 0 {
			continue
		}
		total += leg.BindingConfidence
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func scoreSupplyChainTracePriority(artifact *horizonmodels.MaterializedArtifact) int {
	if artifact == nil {
		return 0
	}
	score := 0
	if artifact.IsPinned {
		score += 180
	}
	score += int(artifact.OverlapScore * 100)
	score += int(artifact.PathStressScore * 70)
	score += int(artifact.BindingScore * 35)
	if artifact.LegCount > 0 {
		if artifact.LegCount > 9 {
			score += 18
		} else {
			score += artifact.LegCount * 2
		}
	}
	switch artifact.TraceRole {
	case "direct_anchor":
		score += 50
	case "affected_zone_overlap":
		score += 42
	case "route_overlap", "route_context":
		score += 38
	case "zone_overlap", "zone_context":
		score += 30
	case "graph_context":
		score += 22
	}
	return score
}

func buildSupplyChainTraceReason(artifact *horizonmodels.MaterializedArtifact, centerCanonicalPath string) string {
	if artifact == nil {
		return ""
	}
	bits := make([]string, 0, 4)
	switch artifact.TraceRole {
	case "direct_anchor":
		bits = append(bits, "contains the selected object")
	case "route_overlap", "route_context":
		bits = append(bits, "shares route bindings with the current map context")
	case "zone_overlap", "zone_context":
		bits = append(bits, "intersects the same static zone context")
	case "affected_zone_overlap", "affected_context":
		bits = append(bits, "is exposed to the same affected zone")
	case "graph_context":
		bits = append(bits, "overlaps graph entities already in scope")
	case "pinned_context":
		bits = append(bits, "pinned to the current lens scope")
	}
	if artifact.PathStressScore >= 0.7 {
		bits = append(bits, "path stress is high")
	} else if artifact.PathStressScore >= 0.45 {
		bits = append(bits, "path stress is elevated")
	}
	if artifact.BottleneckLeg >= 0 {
		bits = append(bits, "has a mapped bottleneck leg")
	}
	if artifact.BindingScore >= 0.65 {
		bits = append(bits, "route binding confidence is strong")
	}
	if len(bits) == 0 && strings.TrimSpace(artifact.DiscoveryReason) != "" {
		bits = append(bits, strings.TrimSpace(artifact.DiscoveryReason))
	}
	if len(bits) == 0 && strings.TrimSpace(centerCanonicalPath) != "" {
		bits = append(bits, "related to the selected fragment context")
	}
	return strings.Join(bits, ". ")
}

func artifactPathStressScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil && parsed.Stress != nil {
		return parsed.Stress.PathScore
	}
	return 0
}

func intersectCount(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	count := 0
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := set[value]; ok {
			count++
		}
	}
	return count
}

func firstIntersect(left, right []string, limit int) []string {
	if len(left) == 0 || len(right) == 0 || limit <= 0 {
		return nil
	}
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, limit)
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := set[value]; ok {
			out = append(out, value)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func mergeUniqueStrings(dst []string, values []string) []string {
	if len(values) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst)+len(values))
	out := make([]string, 0, len(dst)+len(values))
	for _, value := range dst {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
