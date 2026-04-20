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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathquery"
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
	Chains      []interface{}                               `json:"chains"`
	Total       int                                         `json:"total"`
	Summary     fragmentTraceSupplyChainStats               `json:"summary,omitempty"`
	RiskOverlay *horizonmodels.SupplyChainObjectRiskOverlay `json:"riskOverlay,omitempty"`
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

type fragmentTraceWhyItMattersItem struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fragmentTraceRelation struct {
	EdgeID             string                     `json:"edgeId,omitempty"`
	SourceFragmentID   string                     `json:"sourceFragmentId"`
	TargetFragmentID   string                     `json:"targetFragmentId"`
	RelatedFragmentID  string                     `json:"relatedFragmentId"`
	RelatedLabel       string                     `json:"relatedLabel"`
	RelatedType        string                     `json:"relatedType,omitempty"`
	RelatedCanonical   string                     `json:"relatedCanonicalPath,omitempty"`
	RelatedCoordinates *horizonmodels.Coordinates `json:"relatedCoordinates,omitempty"`
	Type               string                     `json:"type"`
	Family             string                     `json:"family,omitempty"`
	SemanticDirection  string                     `json:"semanticDirection,omitempty"`
	BusinessMeaning    string                     `json:"businessMeaning,omitempty"`
	WhyRelevant        string                     `json:"whyRelevant,omitempty"`
	Priority           int                        `json:"priority,omitempty"`
	Criticality        float64                    `json:"criticality,omitempty"`
}

type fragmentTraceExplain struct {
	Summary          string                          `json:"summary,omitempty"`
	WhyItMatters     []fragmentTraceWhyItMattersItem `json:"whyItMatters,omitempty"`
	DirectRelations  []fragmentTraceRelation         `json:"directRelations,omitempty"`
	ContextRelations []fragmentTraceRelation         `json:"contextRelations,omitempty"`
}

type fragmentTraceResponse struct {
	FragmentID   string                              `json:"fragmentId"`
	Graph        fragmentTraceGraph                  `json:"graph"`
	SupplyChains fragmentTraceSupplyChains           `json:"supplyChains"`
	Explain      fragmentTraceExplain                `json:"explain"`
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
		annotateSupplyChainArtifactRiskOverlays(relatedChains)
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

	fragmentMap := buildFragmentTraceFragmentMap(graphPayload)
	if centerFrag != nil {
		fragmentMap[strings.TrimSpace(centerFrag.ID)] = centerFrag
	}
	resp.Explain = buildFragmentTraceExplain(centerFrag, resp.Graph, fragmentMap, relatedChains, resp.Propagation)
	resp.SupplyChains.RiskOverlay = buildSupplyChainObjectRiskOverlay(centerFrag, relatedChains, fragmentMap)

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
	path = strings.TrimSuffix(path, suffix)
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

func buildFragmentTraceFragmentMap(payload map[string]interface{}) map[string]*horizonmodels.HorizonFragment {
	out := map[string]*horizonmodels.HorizonFragment{}
	if payload == nil {
		return out
	}
	rawFragments, _ := payload["fragments"].([]interface{})
	for _, raw := range rawFragments {
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var frag horizonmodels.HorizonFragment
		if err := json.Unmarshal(data, &frag); err != nil {
			continue
		}
		if id := strings.TrimSpace(frag.ID); id != "" {
			out[id] = &frag
		}
	}
	return out
}

func buildFragmentTraceExplain(
	centerFrag *horizonmodels.HorizonFragment,
	graph fragmentTraceGraph,
	fragments map[string]*horizonmodels.HorizonFragment,
	chains []*horizonmodels.MaterializedArtifact,
	propagationTrace *propagation.PropagationTraceResult,
) fragmentTraceExplain {
	explain := fragmentTraceExplain{
		WhyItMatters:     make([]fragmentTraceWhyItMattersItem, 0, 3),
		DirectRelations:  make([]fragmentTraceRelation, 0, 8),
		ContextRelations: make([]fragmentTraceRelation, 0, 6),
	}
	centerID := strings.TrimSpace("")
	centerLabel := ""
	if centerFrag != nil {
		centerID = strings.TrimSpace(centerFrag.ID)
		centerLabel = fragmentTraceDisplayName(centerFrag)
	}

	direct := make([]fragmentTraceRelation, 0, 12)
	context := make([]fragmentTraceRelation, 0, 12)
	for _, edge := range graph.Edges {
		if centerID == "" {
			break
		}
		if strings.TrimSpace(edge.SourceFragmentID) != centerID && strings.TrimSpace(edge.TargetFragmentID) != centerID {
			continue
		}
		relatedID := strings.TrimSpace(edge.TargetFragmentID)
		if relatedID == centerID {
			relatedID = strings.TrimSpace(edge.SourceFragmentID)
		}
		relatedFrag := fragments[relatedID]
		item := fragmentTraceRelation{
			EdgeID:            strings.TrimSpace(edge.ID),
			SourceFragmentID:  strings.TrimSpace(edge.SourceFragmentID),
			TargetFragmentID:  strings.TrimSpace(edge.TargetFragmentID),
			RelatedFragmentID: relatedID,
			RelatedLabel:      fragmentTraceDisplayName(relatedFrag),
			RelatedType:       strings.TrimSpace(string(relatedFragmentType(relatedFrag))),
			RelatedCanonical:  strings.TrimSpace(relatedCanonicalPath(relatedFrag)),
			Type:              strings.TrimSpace(edge.Type),
			Family:            strings.TrimSpace(edge.Family),
			SemanticDirection: strings.TrimSpace(edge.SemanticDirection),
			BusinessMeaning:   strings.TrimSpace(edge.BusinessMeaning),
			Priority:          edge.Priority,
			Criticality:       edge.Criticality,
		}
		if relatedFrag != nil && relatedFrag.Coordinates != nil {
			item.RelatedCoordinates = relatedFrag.Coordinates
		}
		item.WhyRelevant = describeFragmentTraceRelation(centerLabel, item)
		if isFragmentTraceContextRelation(item) {
			context = append(context, item)
		} else {
			direct = append(direct, item)
		}
	}

	sort.SliceStable(direct, func(i, j int) bool {
		if direct[i].Priority != direct[j].Priority {
			return direct[i].Priority > direct[j].Priority
		}
		if direct[i].Criticality != direct[j].Criticality {
			return direct[i].Criticality > direct[j].Criticality
		}
		return direct[i].RelatedLabel < direct[j].RelatedLabel
	})
	sort.SliceStable(context, func(i, j int) bool {
		if context[i].Priority != context[j].Priority {
			return context[i].Priority > context[j].Priority
		}
		if context[i].Criticality != context[j].Criticality {
			return context[i].Criticality > context[j].Criticality
		}
		return context[i].RelatedLabel < context[j].RelatedLabel
	})

	if len(direct) > 8 {
		direct = direct[:8]
	}
	if len(context) > 6 {
		context = context[:6]
	}
	explain.DirectRelations = direct
	explain.ContextRelations = context

	supplyCount := len(chains)
	stressedSupplyCount := 0
	for _, chain := range chains {
		if chain != nil && chain.PathStressScore >= 0.58 {
			stressedSupplyCount++
		}
	}
	propagationCount := 0
	if propagationTrace != nil {
		propagationCount = len(propagationTrace.Edges)
	}

	explain.Summary = buildFragmentTraceExplainSummary(centerLabel, len(direct), len(context), supplyCount, stressedSupplyCount, propagationCount)
	explain.WhyItMatters = buildFragmentTraceWhyItMatters(centerLabel, direct, context, supplyCount, stressedSupplyCount, propagationCount)
	return explain
}

func buildFragmentTraceExplainSummary(centerLabel string, directCount, contextCount, supplyCount, stressedSupplyCount, propagationCount int) string {
	subject := strings.TrimSpace(centerLabel)
	if subject == "" {
		subject = "This object"
	}
	parts := make([]string, 0, 3)
	if directCount > 0 {
		parts = append(parts, fmt.Sprintf("%d direct relation(s)", directCount))
	}
	if contextCount > 0 {
		parts = append(parts, fmt.Sprintf("%d context anchor(s)", contextCount))
	}
	if supplyCount > 0 {
		parts = append(parts, fmt.Sprintf("%d supply path(s)", supplyCount))
	}
	if len(parts) == 0 {
		return subject + " currently has no mapped direct relations or supply paths on this surface."
	}
	summary := fmt.Sprintf("%s matters here through %s.", subject, joinWithCommasAnd(parts))
	if stressedSupplyCount > 0 {
		summary += fmt.Sprintf(" %d supply path(s) are currently stressed.", stressedSupplyCount)
	}
	if propagationCount > 0 {
		summary += fmt.Sprintf(" Signal propagation continues across %d additional link(s).", propagationCount)
	}
	return summary
}

func buildFragmentTraceWhyItMatters(
	centerLabel string,
	direct []fragmentTraceRelation,
	context []fragmentTraceRelation,
	supplyCount int,
	stressedSupplyCount int,
	propagationCount int,
) []fragmentTraceWhyItMattersItem {
	items := make([]fragmentTraceWhyItMattersItem, 0, 3)
	if len(direct) > 0 {
		top := direct[0]
		items = append(items, fragmentTraceWhyItMattersItem{
			Title: "Direct role",
			Body:  fmt.Sprintf("%s is directly connected through %s with %s.", fallbackFragmentTraceSubject(centerLabel), strings.ToLower(humanizeTraceToken(top.Type)), fallbackFragmentTraceSubject(top.RelatedLabel)),
		})
	}
	if len(context) > 0 {
		top := context[0]
		items = append(items, fragmentTraceWhyItMattersItem{
			Title: "Operating context",
			Body:  fmt.Sprintf("%s sits inside %s shaped by %s.", fallbackFragmentTraceSubject(centerLabel), strings.ToLower(strings.TrimSpace(top.BusinessMeaning)), fallbackFragmentTraceSubject(top.RelatedLabel)),
		})
	}
	if supplyCount > 0 {
		body := fmt.Sprintf("%d supply path(s) are attached to this object.", supplyCount)
		if stressedSupplyCount > 0 {
			body = fmt.Sprintf("%s %d of them are already stressed.", body, stressedSupplyCount)
		}
		items = append(items, fragmentTraceWhyItMattersItem{
			Title: "Supply exposure",
			Body:  body,
		})
	} else if propagationCount > 0 {
		items = append(items, fragmentTraceWhyItMattersItem{
			Title: "Propagation surface",
			Body:  fmt.Sprintf("Signals from %s can still propagate across %d additional link(s).", fallbackFragmentTraceSubject(centerLabel), propagationCount),
		})
	}
	if len(items) > 3 {
		items = items[:3]
	}
	return items
}

func fragmentTraceDisplayName(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if value := strings.TrimSpace(fragment.Labels["en"]); value != "" {
		return value
	}
	for _, value := range fragment.Labels {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	if value := strings.TrimSpace(fragment.Identifiers["legalName"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(fragment.Identifiers["shortName"]); value != "" {
		return value
	}
	cp := strings.Trim(strings.TrimSpace(fragment.CanonicalPath), "/")
	if cp == "" {
		return ""
	}
	parts := strings.Split(cp, "/")
	return strings.TrimSpace(parts[len(parts)-1])
}

func relatedFragmentType(fragment *horizonmodels.HorizonFragment) FragmentType {
	if fragment == nil {
		return ""
	}
	return fragment.Class
}

func relatedCanonicalPath(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	return fragment.CanonicalPath
}

func isFragmentTraceContextRelation(item fragmentTraceRelation) bool {
	switch item.Family {
	case "location", "market":
		return true
	}
	switch item.Type {
	case "REGULATES", "ASSURES", "AUDITED_BY", "LOCATED_IN", "OPERATES_IN", "BASED_IN", "HEADQUARTERED_IN", "PARTICIPATES_IN", "TRADED_IN":
		return true
	default:
		return false
	}
}

func describeFragmentTraceRelation(centerLabel string, item fragmentTraceRelation) string {
	other := fallbackFragmentTraceSubject(item.RelatedLabel)
	switch item.Type {
	case "SUPPLIES_TO":
		return fmt.Sprintf("This object supplies into %s, so downstream availability depends on that link.", other)
	case "DEPENDS_ON", "REQUIRES", "USES":
		return fmt.Sprintf("This object depends on %s, making it an immediate operational dependency.", other)
	case "CONTROLS", "MANAGES", "OWNS", "INVESTS_IN":
		return fmt.Sprintf("%s sits in the control chain around this object.", other)
	case "REGULATES", "ASSURES", "AUDITED_BY":
		return fmt.Sprintf("%s shapes the operating conditions and oversight around this object.", other)
	case "LOCATED_IN", "OPERATES_IN", "BASED_IN", "HEADQUARTERED_IN":
		return fmt.Sprintf("This object is anchored in %s, which defines its geographic operating context.", other)
	case "PARTICIPATES_IN", "TRADED_IN":
		return fmt.Sprintf("%s places this object inside the market context where pressure is transmitted.", other)
	case "COMPETES_WITH", "CONFLICTS_WITH", "OPPOSES":
		return fmt.Sprintf("%s is part of the direct competitive or conflict pressure around this object.", other)
	default:
		if meaning := strings.TrimSpace(item.BusinessMeaning); meaning != "" {
			return fmt.Sprintf("%s is relevant because it contributes %s around %s.", other, strings.ToLower(meaning), fallbackFragmentTraceSubject(centerLabel))
		}
		return fmt.Sprintf("%s is part of the immediate relationship logic around this object.", other)
	}
}

func fallbackFragmentTraceSubject(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "this object"
}

func humanizeTraceToken(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToLower(part)
	}
	return strings.Join(parts, " ")
}

func joinWithCommasAnd(items []string) string {
	clean := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return clean[0]
	case 2:
		return clean[0] + " and " + clean[1]
	default:
		return strings.Join(clean[:len(clean)-1], ", ") + ", and " + clean[len(clean)-1]
	}
}

func (h *HorizonHandlers) collectFragmentTraceSupplyChains(ctx context.Context, frag *horizonmodels.HorizonFragment, lensID string) ([]*horizonmodels.MaterializedArtifact, error) {
	if h.materializedArtifactsRepo == nil || frag == nil {
		return nil, nil
	}
	cp := strings.TrimSpace(frag.CanonicalPath)
	var lenses pathquery.LensGetter
	if h.lensItemsHandler != nil {
		lenses = h.lensItemsHandler.lensesRepo
	}
	out, err := pathquery.CollectFragmentTraceSupplyChains(ctx, h.materializedArtifactsRepo, lenses, frag, lensID)
	if err != nil || len(out) == 0 {
		return out, err
	}
	resolvedFragments := loadSupplyChainTraceFragments(ctx, h.service.Fragments(), out)
	for _, artifact := range out {
		annotateSupplyChainTraceArtifact(artifact, cp)
		hydrateSupplyChainTraceEndpoints(artifact, cp, resolvedFragments)
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
	case "SUPPLIES_TO", "DEPENDS_ON", "PRODUCES", "TRANSFORMS_INTO", "ASSEMBLES_INTO", "USED_IN", "EXTRACTS_FROM", "REQUIRES", "USES", "SUPPORTS":
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
	case "SUPPLIES_TO", "PRODUCES", "TRANSFORMS_INTO", "ASSEMBLES_INTO", "USED_IN", "EXTRACTS_FROM":
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
