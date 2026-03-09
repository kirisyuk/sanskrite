package horizon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
)

type fragmentTraceGraphEdge struct {
	ID                string  `json:"id,omitempty"`
	SourceFragmentID  string  `json:"sourceFragmentId"`
	TargetFragmentID  string  `json:"targetFragmentId"`
	Type              string  `json:"type"`
	Family            string  `json:"family,omitempty"`
	SemanticDirection string  `json:"semanticDirection,omitempty"`
	BusinessMeaning   string  `json:"businessMeaning,omitempty"`
	Priority          int     `json:"priority,omitempty"`
	Criticality       float64 `json:"criticality,omitempty"`
}

type fragmentTraceGraph struct {
	Edges       []fragmentTraceGraphEdge              `json:"edges"`
	Total       int                                   `json:"total"`
	Fragments   []interface{}                         `json:"fragments,omitempty"`
	Counts      interface{}                           `json:"counts,omitempty"`
	Coordinates map[string]*horizonmodels.Coordinates `json:"coordinates,omitempty"`
}

type fragmentTraceSupplyChains struct {
	Chains  []interface{}                 `json:"chains"`
	Total   int                           `json:"total"`
	Summary fragmentTraceSupplyChainStats `json:"summary,omitempty"`
}

type fragmentTraceSupplyChainStats struct {
	PinnedCount      int `json:"pinnedCount"`
	DiscoveredCount  int `json:"discoveredCount"`
	DirectCount      int `json:"directCount"`
	RouteCount       int `json:"routeCount"`
	ZoneCount        int `json:"zoneCount"`
	AffectedCount    int `json:"affectedCount"`
	HighStressCount  int `json:"highStressCount"`
	BottleneckCount  int `json:"bottleneckCount"`
	StrongBindingCnt int `json:"strongBindingCount"`
}

type fragmentTraceResponse struct {
	FragmentID   string                              `json:"fragmentId"`
	Graph        fragmentTraceGraph                  `json:"graph"`
	SupplyChains fragmentTraceSupplyChains           `json:"supplyChains"`
	Propagation  *propagation.PropagationTraceResult `json:"propagation,omitempty"`
}

func (h *HorizonHandlers) GetFragmentTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	fragmentID := fragmentIDFromFragmentSubroute(r.URL.Path, "/trace")
	if fragmentID == "" {
		http.Error(w, "Fragment ID required in path", http.StatusBadRequest)
		return
	}

	graphPayload, statusCode, err := h.getFragmentDependenciesPayload(r, fragmentID)
	if err != nil {
		http.Error(w, err.Error(), statusCode)
		return
	}

	resp := fragmentTraceResponse{
		FragmentID: fragmentID,
		Graph:      buildFragmentTraceGraph(graphPayload, fragmentID),
		SupplyChains: fragmentTraceSupplyChains{
			Chains: make([]interface{}, 0),
			Total:  0,
		},
	}

	centerFrag, _ := h.service.Fragments().Get(r.Context(), fragmentID)
	relatedChains, err := h.collectFragmentTraceSupplyChains(r.Context(), centerFrag, strings.TrimSpace(r.URL.Query().Get("lensId")))
	if err == nil && len(relatedChains) > 0 {
		resp.SupplyChains.Chains = make([]interface{}, 0, len(relatedChains))
		for _, chain := range relatedChains {
			resp.SupplyChains.Chains = append(resp.SupplyChains.Chains, chain)
		}
		resp.SupplyChains.Total = len(relatedChains)
		resp.SupplyChains.Summary = buildFragmentTraceSupplyChainStats(relatedChains)
	}

	if propagationEngine := h.service.Propagation(); propagationEngine != nil {
		maxHops := 6
		if hops := strings.TrimSpace(r.URL.Query().Get("hops")); hops != "" {
			if parsed, err := parseInt(hops); err == nil && parsed > 0 {
				maxHops = parsed + 1
			}
		}
		trace, err := propagationEngine.TracePropagationPaths(r.Context(), fragmentID, "map_trace", 0.1, maxHops)
		if err == nil {
			resp.Propagation = trace
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func buildFragmentTraceSupplyChainStats(chains []*horizonmodels.MaterializedArtifact) fragmentTraceSupplyChainStats {
	stats := fragmentTraceSupplyChainStats{}
	for _, chain := range chains {
		if chain == nil {
			continue
		}
		if chain.IsPinned {
			stats.PinnedCount++
		} else {
			stats.DiscoveredCount++
		}
		switch chain.TraceRole {
		case "direct_anchor":
			stats.DirectCount++
		case "route_overlap", "route_context":
			stats.RouteCount++
		case "zone_overlap", "zone_context":
			stats.ZoneCount++
		case "affected_zone_overlap", "affected_context":
			stats.AffectedCount++
		}
		if chain.PathStressScore >= 0.58 {
			stats.HighStressCount++
		}
		if chain.BottleneckLeg >= 0 {
			stats.BottleneckCount++
		}
		if chain.BindingScore >= 0.62 {
			stats.StrongBindingCnt++
		}
	}
	return stats
}

func fragmentIDFromFragmentSubroute(path string, suffix string) string {
	path = strings.TrimPrefix(path, "/api/horizon/fragments/")
	if strings.HasSuffix(path, suffix) {
		path = strings.TrimSuffix(path, suffix)
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func (h *HorizonHandlers) getFragmentDependenciesPayload(r *http.Request, fragmentID string) (map[string]interface{}, int, error) {
	if fragmentID == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("fragment id is required")
	}
	req := r.Clone(r.Context())
	q := req.URL.Query()
	if strings.TrimSpace(q.Get("includeFragments")) == "" {
		q.Set("includeFragments", "1")
	}
	if strings.TrimSpace(q.Get("includeCoordinates")) == "" {
		q.Set("includeCoordinates", "1")
	}
	if strings.TrimSpace(q.Get("includeCounts")) == "" {
		q.Set("includeCounts", "1")
	}
	req.URL.RawQuery = q.Encode()
	req.URL.Path = "/api/horizon/fragments/" + fragmentID + "/dependencies"

	rec := httptest.NewRecorder()
	h.GetFragmentDependencies(rec, req)
	if rec.Code >= 400 {
		return nil, rec.Code, errors.New(strings.TrimSpace(rec.Body.String()))
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("decode dependencies payload: %w", err)
	}
	return payload, http.StatusOK, nil
}

func buildFragmentTraceGraph(payload map[string]interface{}, centerFragmentID string) fragmentTraceGraph {
	graph := fragmentTraceGraph{
		Edges: make([]fragmentTraceGraphEdge, 0),
	}
	if payload == nil {
		return graph
	}
	if total, ok := payload["total"].(float64); ok {
		graph.Total = int(total)
	}
	if fragments, ok := payload["fragments"].([]interface{}); ok {
		graph.Fragments = fragments
	}
	if counts, ok := payload["counts"]; ok {
		graph.Counts = counts
	}
	if rawCoords, ok := payload["coordinates"].(map[string]interface{}); ok {
		graph.Coordinates = make(map[string]*horizonmodels.Coordinates, len(rawCoords))
		for id, raw := range rawCoords {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			lat, latOK := entry["lat"].(float64)
			lon, lonOK := entry["lon"].(float64)
			if !latOK || !lonOK {
				continue
			}
			graph.Coordinates[id] = &horizonmodels.Coordinates{Lat: lat, Lon: lon}
		}
	}
	rawEdges, _ := payload["edges"].([]interface{})
	for _, raw := range rawEdges {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sourceID := stringField(entry, "sourceFragmentId", "sourceFragmentID")
		targetID := stringField(entry, "targetFragmentId", "targetFragmentID")
		edgeType := strings.ToUpper(strings.TrimSpace(stringField(entry, "type")))
		if sourceID == "" || targetID == "" || edgeType == "" {
			continue
		}
		criticality := 0.0
		if props, ok := entry["properties"].(map[string]interface{}); ok {
			if v, ok := props["criticality"].(float64); ok {
				criticality = v
			}
		}
		graph.Edges = append(graph.Edges, fragmentTraceGraphEdge{
			ID:                stringField(entry, "id", "ID"),
			SourceFragmentID:  sourceID,
			TargetFragmentID:  targetID,
			Type:              edgeType,
			Family:            traceEdgeFamily(edgeType),
			SemanticDirection: traceEdgeSemanticDirection(centerFragmentID, sourceID, targetID, edgeType),
			BusinessMeaning:   traceBusinessMeaning(edgeType),
			Priority:          traceEdgePriority(edgeType),
			Criticality:       criticality,
		})
	}
	if graph.Total == 0 {
		graph.Total = len(graph.Edges)
	}
	return graph
}

func (h *HorizonHandlers) collectFragmentTraceSupplyChains(ctx context.Context, frag *horizonmodels.HorizonFragment, lensID string) ([]*horizonmodels.MaterializedArtifact, error) {
	if h.materializedArtifactsRepo == nil || frag == nil {
		return nil, nil
	}
	tenantID := strings.TrimSpace(frag.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	cp := strings.TrimSpace(frag.CanonicalPath)
	selected := make(map[string]*horizonmodels.MaterializedArtifact)
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
		annotateSupplyChainTraceArtifact(&copyArtifact, cp)
		selected[copyArtifact.ID] = &copyArtifact
	}
	if cp != "" {
		if items, err := h.materializedArtifactsRepo.FindByNodeRef(ctx, tenantID, cp, horizonmodels.MaterializedKindSupplyChainPath); err == nil {
			for _, artifact := range items {
				appendArtifact(artifact, false, "node_overlap", []string{cp}, 1.0)
			}
		}
		if frag.Type == horizonmodels.FragmentTypeAffectedZone {
			if items, err := h.materializedArtifactsRepo.FindByAffectedZoneRefsAny(ctx, tenantID, []string{cp}, horizonmodels.MaterializedKindSupplyChainPath, 50); err == nil {
				for _, artifact := range items {
					appendArtifact(artifact, false, "affected_zone_overlap", []string{cp}, 0.9)
				}
			}
		}
	}
	if lensID != "" && h.lensItemsHandler != nil && h.lensItemsHandler.lensesRepo != nil {
		if lens, err := h.lensItemsHandler.lensesRepo.Get(ctx, lensID); err == nil && lens != nil {
			scoped, discovered, err := h.collectSupplyChainsForLens(&http.Request{Method: http.MethodGet}, lens, 100)
			if err == nil {
				for _, artifact := range append(scoped, discovered...) {
					if artifact == nil {
						continue
					}
					horizonmodels.PopulateSupplyChainPathIndexes(artifact)
					if cp != "" {
						if traceContainsString(artifact.NodeRefs, cp) || traceContainsString(artifact.AffectedZoneRefs, cp) || traceContainsString(artifact.ZoneRefs, cp) {
							appendArtifact(artifact, artifact.IsPinned, artifact.DiscoverySource, artifact.MatchedRefs, artifact.OverlapScore)
						}
					}
				}
			}
		}
	}
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(selected))
	for _, artifact := range selected {
		out = append(out, artifact)
	}
	sortSupplyChainArtifacts(out)
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i]
		right := out[j]
		if left.TracePriority != right.TracePriority {
			return left.TracePriority > right.TracePriority
		}
		return left.UpdatedAt.After(right.UpdatedAt)
	})
	return out, nil
}

func stringField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func traceEdgePriority(edgeType string) int {
	switch edgeType {
	case "SUPPLIES_TO":
		return 100
	case "DEPENDS_ON":
		return 99
	case "PRODUCES":
		return 97
	case "EXTRACTS_FROM":
		return 96
	case "REQUIRES":
		return 95
	case "USES":
		return 94
	case "CONTROLS":
		return 92
	case "MANAGES":
		return 90
	case "OWNS":
		return 88
	case "INVESTS_IN":
		return 86
	case "REGULATES":
		return 84
	case "ASSURES":
		return 82
	case "AUDITED_BY":
		return 80
	case "OPERATES_IN":
		return 76
	case "LOCATED_IN":
		return 74
	case "TRADED_IN":
		return 73
	case "PARTICIPATES_IN":
		return 72
	case "SUPPORTS":
		return 70
	case "PARTNERS_WITH":
		return 68
	default:
		return 40
	}
}

func traceEdgeFamily(edgeType string) string {
	switch edgeType {
	case "SUPPLIES_TO", "DEPENDS_ON", "PRODUCES", "EXTRACTS_FROM", "REQUIRES", "USES", "SUPPORTS":
		return "supply"
	case "CONTROLS", "MANAGES", "OWNS", "INVESTS_IN", "BOARD_MEMBER_OF", "EMPLOYED_BY", "FOUNDED":
		return "control"
	case "REGULATES", "ASSURES", "AUDITED_BY", "INFLUENCES", "AFFECTS", "IMPACTS":
		return "influence"
	case "LOCATED_IN", "OPERATES_IN", "BASED_IN", "HEADQUARTERED_IN":
		return "location"
	case "PARTICIPATES_IN", "TRADED_IN", "HAS_MARKET_SEGMENT", "HAS_SUBMARKET":
		return "market"
	case "COMPETES_WITH", "CONFLICTS_WITH", "OPPOSES":
		return "risk"
	case "RELATED_TO", "RELATIVE_OF", "PARTNERS_WITH", "COOPERATES_WITH":
		return "social"
	default:
		return "other"
	}
}

func traceBusinessMeaning(edgeType string) string {
	switch edgeType {
	case "SUPPLIES_TO", "PRODUCES", "EXTRACTS_FROM":
		return "Operational supply path"
	case "DEPENDS_ON", "REQUIRES", "USES":
		return "Dependency exposure"
	case "CONTROLS", "MANAGES", "OWNS", "INVESTS_IN":
		return "Control or ownership chain"
	case "LOCATED_IN", "OPERATES_IN", "BASED_IN", "HEADQUARTERED_IN":
		return "Geographic operating context"
	case "PARTICIPATES_IN":
		return "Market participation context"
	case "TRADED_IN":
		return "Product trading and price-discovery context"
	case "HAS_MARKET_SEGMENT", "HAS_SUBMARKET":
		return "Market positioning context"
	case "REGULATES", "ASSURES", "AUDITED_BY", "INFLUENCES", "AFFECTS", "IMPACTS":
		return "External influence or oversight"
	case "COMPETES_WITH", "CONFLICTS_WITH", "OPPOSES":
		return "Competitive or risk pressure"
	default:
		return "Context relationship"
	}
}

func traceEdgeSemanticDirection(centerID, sourceID, targetID, edgeType string) string {
	rawDir := ""
	if sourceID == centerID {
		rawDir = "outgoing"
	} else if targetID == centerID {
		rawDir = "incoming"
	} else {
		return "context"
	}
	reverse := map[string]struct{}{
		"DEPENDS_ON": {}, "REQUIRES": {}, "NEEDS": {}, "USES": {}, "EXTRACTS_FROM": {},
		"LOCATED_IN": {}, "OPERATES_IN": {}, "BASED_IN": {}, "HEADQUARTERED_IN": {}, "AUDITED_BY": {},
	}
	symmetric := map[string]struct{}{
		"COMPETES_WITH": {}, "CONFLICTS_WITH": {}, "OPPOSES": {}, "PARTNERS_WITH": {}, "COOPERATES_WITH": {}, "RELATIVE_OF": {}, "RELATED_TO": {}, "SIMILAR_TO": {}, "LINKED_TO": {},
	}
	if _, ok := symmetric[edgeType]; ok {
		return "upstream"
	}
	if _, ok := reverse[edgeType]; ok {
		if rawDir == "outgoing" {
			return "upstream"
		}
		return "downstream"
	}
	if rawDir == "incoming" {
		return "upstream"
	}
	return "downstream"
}

func traceContainsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
