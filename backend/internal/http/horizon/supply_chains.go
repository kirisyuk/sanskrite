package horizon

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

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
