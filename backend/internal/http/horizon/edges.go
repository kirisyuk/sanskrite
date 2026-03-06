package horizon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpolicy "github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func getLensIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	// Query param takes precedence to make this easy to test/invoke.
	if v := strings.TrimSpace(r.URL.Query().Get("lensId")); v != "" {
		return v
	}
	// Headers: support both common casings used across the codebase.
	if v := strings.TrimSpace(r.Header.Get("X-Lens-Id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Lens-ID")); v != "" {
		return v
	}
	return ""
}

func extractStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if x == nil {
				continue
			}
			if s, ok := x.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func indexPathMatchesFilter(filterPath, indexPath string) bool {
	filterPath = strings.TrimSpace(filterPath)
	indexPath = strings.TrimSpace(indexPath)
	if filterPath == "" || indexPath == "" {
		return false
	}
	// Exact match is the primary mode (indexPaths include ancestors, so this is powerful).
	if filterPath == indexPath {
		return true
	}
	// Optional wildcard support for future flexibility:
	// - "/Foo/Bar/**" => prefix match
	// - "/Foo/Bar*"   => prefix match
	if strings.Contains(filterPath, "*") {
		prefix := strings.Split(filterPath, "*")[0]
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return false
		}
		return strings.HasPrefix(indexPath, prefix)
	}
	return false
}

func indexPathsIntersectFilter(indexPaths []string, filterPaths []string) bool {
	if len(indexPaths) == 0 || len(filterPaths) == 0 {
		return false
	}
	for _, fp := range filterPaths {
		fp = strings.TrimSpace(fp)
		if fp == "" {
			continue
		}
		for _, ip := range indexPaths {
			if indexPathMatchesFilter(fp, ip) {
				return true
			}
		}
	}
	return false
}

func (h *HorizonHandlers) normalizeEdgeForCreate(ctx context.Context, edge *models.DependencyEdge) error {
	if h == nil || h.service == nil {
		return fmt.Errorf("service not initialized")
	}
	if edge == nil {
		return fmt.Errorf("edge is nil")
	}
	if strings.TrimSpace(edge.SourceFragmentID) == "" || strings.TrimSpace(edge.TargetFragmentID) == "" {
		return fmt.Errorf("sourceFragmentId and targetFragmentId are required")
	}
	if strings.TrimSpace(edge.SourceFragmentID) == strings.TrimSpace(edge.TargetFragmentID) {
		return fmt.Errorf("sourceFragmentId and targetFragmentId must be different")
	}

	// Normalize relation type via code-defined taxonomy contracts.
	rawType := strings.TrimSpace(string(edge.Type))
	normType := strings.TrimSpace(taxonomy.NormalizeRelationType(rawType))
	edge.Type = models.DependencyType(normType)

	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	if rawType != "" && normType != "" && rawType != normType {
		edge.Qualifiers["rawRelationType"] = rawType
		edge.Qualifiers["normalizedRelationType"] = normType
	}

	// Canonical representation: SUBSIDIARY_OF is normalized into OWNS (parent -> subsidiary).
	if edge.Type == models.DependencyTypeSubsidiaryOf {
		edge.Type = models.DependencyTypeOwns
		edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
		edge.Qualifiers["normalizedDirection"] = true
		edge.Qualifiers["normalizedEdgeType"] = string(edge.Type)
	}

	// Symmetric edge normalization (avoid A->B and B->A duplicates).
	edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)

	// Resolve endpoint types for direction validation.
	frRepo := h.service.Fragments()
	if frRepo == nil {
		// Best-effort: if we can't validate types, still allow creation (legacy behavior).
		return nil
	}
	srcFrag, srcErr := frRepo.Get(ctx, edge.SourceFragmentID)
	dstFrag, dstErr := frRepo.Get(ctx, edge.TargetFragmentID)
	if srcErr != nil || dstErr != nil || srcFrag == nil || dstFrag == nil {
		return fmt.Errorf("failed to resolve edge endpoints: srcErr=%v dstErr=%v", srcErr, dstErr)
	}

	// Canonicalize certain mis-modeled edges where reversing is not sufficient.
	// Example: PRODUCT OWNS ORGANIZATION should become ORGANIZATION PRODUCES PRODUCT (not ORG OWNS PRODUCT).
	// Use path-aware canonicalization to handle community subtypes (dynasty/tribe).
	if newType, shouldSwap, changed, reason := scout.CanonicalizeEdgeWithPaths(edge.Type, srcFrag.Type, dstFrag.Type, srcFrag.CanonicalPath, dstFrag.CanonicalPath); changed {
		oldType := edge.Type
		edge.Type = newType
		edge.Qualifiers["canonicalized"] = true
		edge.Qualifiers["canonicalizedFromEdgeType"] = string(oldType)
		edge.Qualifiers["canonicalizationReason"] = reason
		edge.Qualifiers["normalizedEdgeType"] = string(edge.Type)
		if shouldSwap {
			edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
			edge.Qualifiers["normalizedDirection"] = true
			// Keep local type vars consistent for subsequent direction validation.
			srcFrag, dstFrag = dstFrag, srcFrag
		}
		// Re-apply symmetric normalization after any swap/type-change (noop for non-symmetric).
		edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
	}

	// Enforce direction policies (FOUNDED, EMPLOYED_BY, MANAGES, KEY_PERSON, etc.).
	if valid, shouldReverse, rule := scout.ValidateEdgeDirection(edge.Type, srcFrag.Type, dstFrag.Type); !valid {
		if shouldReverse {
			edge.SourceFragmentID, edge.TargetFragmentID = edge.TargetFragmentID, edge.SourceFragmentID
			edge.Qualifiers["normalizedDirection"] = true
			// Re-apply symmetric normalization after swap (noop for non-symmetric).
			edge.SourceFragmentID, edge.TargetFragmentID, edge.Type, _ = scout.NormalizeSymmetricEdge(edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
		} else if rule != nil {
			return fmt.Errorf("invalid edge direction for %s: %s", edge.Type, rule.Description)
		}
	}

	return nil
}

func (h *HorizonHandlers) publishEdgeUpserted(ctx context.Context, authTenant string, projectID string, lensID string, correlationID string, sessionID string, edge *models.DependencyEdge) {
	if h == nil || h.service == nil || edge == nil {
		return
	}
	pub := h.service.RealtimePublisher()
	if pub == nil {
		return
	}
	if strings.TrimSpace(projectID) == "" {
		// Without projectId we cannot route to subscribed clients (skip).
		return
	}
	payload := map[string]interface{}{
		"tenantId":            strings.TrimSpace(authTenant),
		"projectId":           strings.TrimSpace(projectID),
		"lensId":              strings.TrimSpace(lensID),
		"edgeId":              strings.TrimSpace(edge.ID),
		"sourceFragmentId":    strings.TrimSpace(edge.SourceFragmentID),
		"targetFragmentId":    strings.TrimSpace(edge.TargetFragmentID),
		"sourceCanonicalPath": "",
		"targetCanonicalPath": "",
		"sourceLabel":         "",
		"targetLabel":         "",
		"type":                string(edge.Type),
		"properties":          edge.Properties,
		"qualifiers":          edge.Qualifiers,
		"ts":                  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if h.service != nil && h.service.Fragments() != nil {
		if frag, err := h.service.Fragments().Get(ctx, strings.TrimSpace(edge.SourceFragmentID)); err == nil && frag != nil {
			payload["sourceCanonicalPath"] = strings.TrimSpace(frag.CanonicalPath)
			payload["sourceLabel"] = strings.TrimSpace(frag.Labels["en"])
		}
		if frag, err := h.service.Fragments().Get(ctx, strings.TrimSpace(edge.TargetFragmentID)); err == nil && frag != nil {
			payload["targetCanonicalPath"] = strings.TrimSpace(frag.CanonicalPath)
			payload["targetLabel"] = strings.TrimSpace(frag.Labels["en"])
		}
	}
	if correlationID != "" {
		payload["correlationId"] = correlationID
	}
	if sessionID != "" {
		payload["sessionId"] = sessionID
	}
	_ = pub.PublishJSON(ctx, realtime.EventType("horizon.graph.edge.upserted"), strings.TrimSpace(authTenant), payload)
}

// GetEdge returns a DependencyEdge by ID.
// GET /api/horizon/edges/{id}
func (h *HorizonHandlers) GetEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	edgeID := extractIDFromPath(r, "/api/horizon/edges/")
	if edgeID == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	edge, err := h.service.Edges().Get(r.Context(), edgeID)
	if err != nil {
		http.Error(w, "Edge not found", http.StatusNotFound)
		return
	}

	if edge.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edge)
}

// CreateEdge создает новый DependencyEdge
func (h *HorizonHandlers) CreateEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var edge models.DependencyEdge
	if err := json.NewDecoder(r.Body).Decode(&edge); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	edge.TenantID = authInfo.TenantID

	// Optional routing context from headers (best-effort for realtime).
	projectID := strings.TrimSpace(r.Header.Get("X-Project-Id"))
	if projectID == "" {
		projectID = strings.TrimSpace(r.Header.Get("X-Project-ID"))
	}
	lensID := strings.TrimSpace(r.Header.Get("X-Lens-Id"))
	if lensID == "" {
		lensID = strings.TrimSpace(r.Header.Get("X-Lens-ID"))
	}
	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
	if correlationID == "" {
		correlationID = strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	}
	if correlationID == "" {
		correlationID = uuid.NewString()
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-ID"))
	}
	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     projectID,
		LensID:        lensID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})

	// Normalize edge type + direction at creation time (prevents inverted FOUNDED/EMPLOYED_BY/etc).
	if err := h.normalizeEdgeForCreate(ctx, &edge); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	edgeID, err := h.service.Edges().Create(ctx, &edge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	edge.ID = edgeID
	h.publishEdgeUpserted(ctx, authInfo.TenantID, projectID, lensID, correlationID, sessionID, &edge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(edge)
}

// UpdateEdge обновляет DependencyEdge
func (h *HorizonHandlers) UpdateEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	edgeID := extractIDFromPath(r, "/api/horizon/edges/")
	if edgeID == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var updateReq struct {
		Properties map[string]interface{} `json:"properties"`
		Qualifiers map[string]interface{} `json:"qualifiers,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&updateReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	edge, err := h.service.Edges().Get(r.Context(), edgeID)
	if err != nil {
		http.Error(w, "Edge not found", http.StatusNotFound)
		return
	}

	if edge.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Обновляем свойства
	if updateReq.Properties != nil {
		if criticality, ok := updateReq.Properties["criticality"].(float64); ok {
			edge.Properties.Criticality = criticality
		}
		if amplification, ok := updateReq.Properties["amplification"].(float64); ok {
			edge.Properties.Amplification = amplification
		}
		if latencyHours, ok := updateReq.Properties["latencyHours"].(float64); ok {
			edge.Properties.LatencyHours = int(latencyHours)
		}
	}

	if updateReq.Qualifiers != nil {
		edge.Qualifiers = updateReq.Qualifiers
	}

	if err := h.service.Edges().Update(r.Context(), edge); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Best-effort realtime edge upsert (needs projectId in headers for routing).
	projectID := strings.TrimSpace(r.Header.Get("X-Project-Id"))
	if projectID == "" {
		projectID = strings.TrimSpace(r.Header.Get("X-Project-ID"))
	}
	lensID := strings.TrimSpace(r.Header.Get("X-Lens-Id"))
	if lensID == "" {
		lensID = strings.TrimSpace(r.Header.Get("X-Lens-ID"))
	}
	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-Id"))
	if correlationID == "" {
		correlationID = strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-Id"))
	if sessionID == "" {
		sessionID = strings.TrimSpace(r.Header.Get("X-Session-ID"))
	}
	ctx := rtctx.With(r.Context(), rtctx.Context{
		TenantID:      authInfo.TenantID,
		ProjectID:     projectID,
		LensID:        lensID,
		CorrelationID: correlationID,
		SessionID:     sessionID,
	})
	h.publishEdgeUpserted(ctx, authInfo.TenantID, projectID, lensID, correlationID, sessionID, edge)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(edge)
}

// DeleteEdge удаляет DependencyEdge
func (h *HorizonHandlers) DeleteEdge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	edgeID := extractIDFromPath(r, "/api/horizon/edges/")
	if edgeID == "" {
		http.Error(w, "Edge ID required in path", http.StatusBadRequest)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	edge, err := h.service.Edges().Get(r.Context(), edgeID)
	if err != nil {
		http.Error(w, "Edge not found", http.StatusNotFound)
		return
	}

	if edge.TenantID != authInfo.TenantID {
		edgeTenant := strings.TrimSpace(edge.TenantID)
		// Horizon "world graph" edges are typically stored under tenantId=system (and sometimes tenantId=global).
		// Allow admins to curate/delete these shared edges even if the caller tenant differs.
		// This matches the HITL update endpoint policy.
		if strings.EqualFold(edgeTenant, "global") || strings.EqualFold(edgeTenant, "system") {
			if !authInfo.IsSystem {
				p := &horizonpolicy.HumanInTheLoopPolicy{UsersRepo: h.usersRepo}
				if !p.IsAdmin(r.Context(), authInfo.TenantID, authInfo.UID) {
					http.Error(w, "Admin access required", http.StatusForbidden)
					return
				}
			}
		} else {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}
	}

	if err := h.service.Edges().Delete(r.Context(), edgeID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetFragmentDependencies получает зависимости узла
func (h *HorizonHandlers) GetFragmentDependencies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем fragmentID из пути: /api/horizon/fragments/{id}/dependencies
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/fragments/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "Fragment ID required in path", http.StatusBadRequest)
		return
	}
	fragmentID := pathParts[0]

	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "both"
	}

	// Optional semantic filtering (does NOT change default behavior unless provided).
	// semantic=upstream|downstream|both
	// - upstream: "influences/determines the center" (owners, prerequisites, context, peers)
	// - downstream: "determined by the center" (subsidiaries, outputs, influenced targets)
	semantic := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("semantic")))
	if semantic == "" {
		semantic = "both"
	}
	if semantic != "both" && semantic != "upstream" && semantic != "downstream" {
		http.Error(w, "Invalid semantic parameter", http.StatusBadRequest)
		return
	}

	// Default behavior: only return dependencies to fragments that already have indexPaths populated.
	// This keeps UI expansion bounded on partially backfilled datasets.
	includeUnindexedStr := strings.TrimSpace(r.URL.Query().Get("includeUnindexed"))
	includeUnindexed := includeUnindexedStr == "1" || strings.EqualFold(includeUnindexedStr, "true")
	edgeTypeStr := r.URL.Query().Get("type")

	var edgeType *models.DependencyType
	if edgeTypeStr != "" {
		dt := models.DependencyType(edgeTypeStr)
		edgeType = &dt
	}

	// Optional multi-type filter. Supports:
	// - edgeTypes=OWNS,CONTROLS
	// - edgeType=OWNS&edgeType=CONTROLS
	// - edgeType[]=OWNS&edgeType[]=CONTROLS (axios default)
	edgeTypesRaw := strings.TrimSpace(r.URL.Query().Get("edgeTypes"))
	var edgeTypes []string
	if edgeTypesRaw != "" {
		for _, p := range strings.Split(edgeTypesRaw, ",") {
			t := strings.TrimSpace(p)
			if t != "" {
				edgeTypes = append(edgeTypes, strings.ToUpper(t))
			}
		}
	}
	if len(edgeTypes) == 0 {
		if vals, ok := r.URL.Query()["edgeType"]; ok && len(vals) > 0 {
			for _, v := range vals {
				t := strings.TrimSpace(v)
				if t != "" {
					edgeTypes = append(edgeTypes, strings.ToUpper(t))
				}
			}
		}
	}
	if len(edgeTypes) == 0 {
		if vals, ok := r.URL.Query()["edgeType[]"]; ok && len(vals) > 0 {
			for _, v := range vals {
				t := strings.TrimSpace(v)
				if t != "" {
					edgeTypes = append(edgeTypes, strings.ToUpper(t))
				}
			}
		}
	}

	includeFragmentsStr := strings.TrimSpace(r.URL.Query().Get("includeFragments"))
	includeFragments := includeFragmentsStr == "1" || strings.EqualFold(includeFragmentsStr, "true")

	includeCountsStr := strings.TrimSpace(r.URL.Query().Get("includeCounts"))
	includeCounts := includeCountsStr == "1" || strings.EqualFold(includeCountsStr, "true")

	// Optional limits (only applied when explicitly provided).
	// If omitted, behavior stays the same as legacy (no budget trimming).
	maxNeighbors := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("maxNeighbors")); raw != "" {
		if v, perr := strconv.Atoi(raw); perr == nil && v > 0 {
			maxNeighbors = v
		}
	}
	maxEdges := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("maxEdges")); raw != "" {
		if v, perr := strconv.Atoi(raw); perr == nil && v > 0 {
			maxEdges = v
		}
	}
	maxPerType := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("maxPerType")); raw != "" {
		if v, perr := strconv.Atoi(raw); perr == nil && v > 0 {
			maxPerType = v
		}
	}

	limit := 100
	if limStr := strings.TrimSpace(r.URL.Query().Get("limit")); limStr != "" {
		if limParsed, perr := strconv.Atoi(limStr); perr == nil && limParsed > 0 {
			limit = limParsed
		}
	}
	// When using semantic or multi-type filtering, fetch a larger candidate set by default
	// (unless caller explicitly provided a limit), to reduce bias from store limits.
	if strings.TrimSpace(r.URL.Query().Get("limit")) == "" && (semantic != "both" || len(edgeTypes) > 0 || maxNeighbors > 0 || maxEdges > 0 || maxPerType > 0) {
		limit = 500
	}

	// hops: N-hop neighborhood (1 = direct neighbors only). When > 1, BFS expands the subgraph.
	hops := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("hops")); raw != "" {
		if v, perr := strconv.Atoi(raw); perr == nil && v >= 1 && v <= 5 {
			hops = v
		}
	}

	var edges []*models.DependencyEdge
	var err error

	tenantID := "global" // HorizonFragment всегда глобальные

	// If semantic filtering is used, we need both raw directions to evaluate semantics (because some types invert).
	// Do not change default behavior when semantic is not specified (semantic defaults to "both").
	if semantic != "both" && direction != "both" {
		direction = "both"
	}

	if hops > 1 {
		// BFS: collect N-hop subgraph (edges + node IDs).
		const maxNodes = 500
		const maxEdgesTotal = 2000
		const perNodeLimit = 100
		visited := map[string]struct{}{fragmentID: {}}
		frontier := []string{fragmentID}
		allEdges := make([]*models.DependencyEdge, 0, 256)
		edgeIDs := make(map[string]struct{}, 256)

		for hop := 0; hop < hops; hop++ {
			nextFrontier := make([]string, 0, 64)
			for _, fid := range frontier {
				if len(visited) >= maxNodes || len(allEdges) >= maxEdgesTotal {
					break
				}
				outgoing, _ := h.service.Edges().ListBySource(r.Context(), tenantID, fid, edgeType, perNodeLimit)
				incoming, _ := h.service.Edges().ListByTarget(r.Context(), tenantID, fid, edgeType, perNodeLimit)
				for _, e := range outgoing {
					if e == nil {
						continue
					}
					if _, ok := edgeIDs[e.ID]; !ok {
						edgeIDs[e.ID] = struct{}{}
						allEdges = append(allEdges, e)
					}
					if tid := strings.TrimSpace(e.TargetFragmentID); tid != "" {
						if _, ok := visited[tid]; !ok {
							visited[tid] = struct{}{}
							nextFrontier = append(nextFrontier, tid)
						}
					}
				}
				for _, e := range incoming {
					if e == nil {
						continue
					}
					if _, ok := edgeIDs[e.ID]; !ok {
						edgeIDs[e.ID] = struct{}{}
						allEdges = append(allEdges, e)
					}
					if sid := strings.TrimSpace(e.SourceFragmentID); sid != "" {
						if _, ok := visited[sid]; !ok {
							visited[sid] = struct{}{}
							nextFrontier = append(nextFrontier, sid)
						}
					}
				}
			}
			frontier = nextFrontier
			if len(frontier) == 0 {
				break
			}
		}
		// Keep only edges where both endpoints are in the subgraph.
		filtered := make([]*models.DependencyEdge, 0, len(allEdges))
		for _, e := range allEdges {
			if e == nil {
				continue
			}
			sid := strings.TrimSpace(e.SourceFragmentID)
			tid := strings.TrimSpace(e.TargetFragmentID)
			if sid != "" && tid != "" {
				if _, okS := visited[sid]; okS {
					if _, okT := visited[tid]; okT {
						filtered = append(filtered, e)
					}
				}
			}
		}
		edges = filtered
	} else {
		switch direction {
		case "outgoing":
			edges, err = h.service.Edges().ListBySource(r.Context(), tenantID, fragmentID, edgeType, limit)
		case "incoming":
			edges, err = h.service.Edges().ListByTarget(r.Context(), tenantID, fragmentID, edgeType, limit)
		case "both":
			outgoing, err1 := h.service.Edges().ListBySource(r.Context(), tenantID, fragmentID, edgeType, limit)
			incoming, err2 := h.service.Edges().ListByTarget(r.Context(), tenantID, fragmentID, edgeType, limit)
			if err1 != nil {
				err = err1
			} else if err2 != nil {
				err = err2
			} else {
				edges = append(outgoing, incoming...)
			}
		default:
			http.Error(w, "Invalid direction parameter", http.StatusBadRequest)
			return
		}
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply optional multi-type filter (in-memory), while keeping legacy single-type behavior as-is.
	if len(edgeTypes) > 0 && len(edges) > 0 {
		allow := make(map[string]struct{}, len(edgeTypes))
		for _, t := range edgeTypes {
			allow[strings.ToUpper(strings.TrimSpace(t))] = struct{}{}
		}
		filtered := make([]*models.DependencyEdge, 0, len(edges))
		for _, e := range edges {
			if e == nil {
				continue
			}
			et := strings.ToUpper(strings.TrimSpace(string(e.Type)))
			if _, ok := allow[et]; ok {
				filtered = append(filtered, e)
			}
		}
		edges = filtered
	}

	// Apply semantic direction filter if requested.
	// Raw direction is relative to the requested fragmentID:
	// - outgoing: fragmentID -> neighbor
	// - incoming: neighbor -> fragmentID
	if semantic != "both" && len(edges) > 0 {
		// Types where semantic meaning is reversed relative to raw direction:
		// - "A depends on B" (A->B) => B is upstream relative to A
		// - "A operates in B" (A->B) => B is upstream context relative to A
		reverse := map[string]struct{}{
			"DEPENDS_ON":       {},
			"REQUIRES":         {},
			"NEEDS":            {},
			"USES":             {},
			"EXTRACTS_FROM":    {}, // Facility -> Deposit means Deposit is upstream prerequisite/context
			"LOCATED_IN":       {},
			"OPERATES_IN":      {},
			"BASED_IN":         {},
			"HEADQUARTERED_IN": {},
			// Governance/context edges: org/fac -> auditor is upstream context relative to the org.
			"AUDITED_BY": {},
		}
		// Symmetric relations: always treated as upstream environment by default.
		symmetric := map[string]struct{}{
			"COMPETES_WITH":   {},
			"CONFLICTS_WITH":  {},
			"OPPOSES":         {},
			"PARTNERS_WITH":   {},
			"COOPERATES_WITH": {},
			"RELATIVE_OF":     {},
			"RELATED_TO":      {},
			"SIMILAR_TO":      {},
			"LINKED_TO":       {},
		}

		filtered := make([]*models.DependencyEdge, 0, len(edges))
		for _, e := range edges {
			if e == nil {
				continue
			}
			et := strings.ToUpper(strings.TrimSpace(string(e.Type)))

			rawDir := "" // incoming|outgoing
			if strings.TrimSpace(e.SourceFragmentID) == fragmentID {
				rawDir = "outgoing"
			} else if strings.TrimSpace(e.TargetFragmentID) == fragmentID {
				rawDir = "incoming"
			} else {
				// Shouldn't happen, but keep edge (best-effort).
				filtered = append(filtered, e)
				continue
			}

			sem := "downstream"
			if _, ok := symmetric[et]; ok {
				sem = "upstream"
			} else if _, ok := reverse[et]; ok {
				// Reverse mapping
				if rawDir == "outgoing" {
					sem = "upstream"
				} else {
					sem = "downstream"
				}
			} else {
				// Normal mapping
				if rawDir == "incoming" {
					sem = "upstream"
				} else {
					sem = "downstream"
				}
			}

			if sem == semantic {
				filtered = append(filtered, e)
			}
		}
		edges = filtered
	}

	// Optional lens-aware filtering:
	// If a lensId is provided, and it contains viewConfig.filterPaths (or viewConfig.FilterPaths),
	// we filter neighbors by intersection with neighbor.indexPaths.
	// NOTE: We do NOT require the caller to pass lensId (frontend currently doesn't),
	// so behavior remains backward-compatible.
	filterPaths := []string(nil)
	if !includeUnindexed {
		if lensID := getLensIDFromRequest(r); lensID != "" && h != nil && h.service != nil && h.service.Lens() != nil {
			if lens, lerr := h.service.Lens().GetLens(r.Context(), lensID); lerr == nil && lens != nil && lens.TenantID == authInfo.TenantID {
				if lens.ViewConfig != nil {
					// Accept both keys to be resilient to casing changes.
					filterPaths = extractStringSlice(lens.ViewConfig["filterPaths"])
					if len(filterPaths) == 0 {
						filterPaths = extractStringSlice(lens.ViewConfig["FilterPaths"])
					}
				}
			}
		}
	}

	// Filter out edges pointing to irrelevant or unindexed neighbor fragments (default), to avoid exploding graphs.
	// - If filterPaths are provided by lens, keep only neighbors whose indexPaths intersect filterPaths.
	// - Else, keep only neighbors with non-empty indexPaths.
	var neighborMap map[string]*models.HorizonFragment
	if !includeUnindexed && len(edges) > 0 {
		neighborIDs := make([]string, 0, len(edges))
		for _, e := range edges {
			if e == nil {
				continue
			}
			// Neighbor = the other endpoint relative to fragmentID
			if e.SourceFragmentID == fragmentID {
				if strings.TrimSpace(e.TargetFragmentID) != "" {
					neighborIDs = append(neighborIDs, e.TargetFragmentID)
				}
			} else if e.TargetFragmentID == fragmentID {
				if strings.TrimSpace(e.SourceFragmentID) != "" {
					neighborIDs = append(neighborIDs, e.SourceFragmentID)
				}
			} else {
				// Fallback: include both endpoints if we can't determine (shouldn't happen)
				if strings.TrimSpace(e.SourceFragmentID) != "" {
					neighborIDs = append(neighborIDs, e.SourceFragmentID)
				}
				if strings.TrimSpace(e.TargetFragmentID) != "" {
					neighborIDs = append(neighborIDs, e.TargetFragmentID)
				}
			}
		}

		nm, ferr := h.service.Fragments().GetMany(r.Context(), neighborIDs)
		if ferr == nil {
			filtered := make([]*models.DependencyEdge, 0, len(edges))
			for _, e := range edges {
				if e == nil {
					continue
				}
				neighborID := ""
				if e.SourceFragmentID == fragmentID {
					neighborID = strings.TrimSpace(e.TargetFragmentID)
				} else if e.TargetFragmentID == fragmentID {
					neighborID = strings.TrimSpace(e.SourceFragmentID)
				}
				// If we can't determine neighbor, keep edge (best-effort).
				if neighborID == "" {
					filtered = append(filtered, e)
					continue
				}
				n := nm[neighborID]
				if n == nil {
					continue
				}
				if len(filterPaths) > 0 {
					// Lens-aware: require intersection (also implicitly requires indexPaths to be non-empty).
					if indexPathsIntersectFilter(n.IndexPaths, filterPaths) {
						filtered = append(filtered, e)
					}
				} else {
					// Legacy default: only require that the neighbor has any indexPaths at all.
					if len(n.IndexPaths) > 0 {
						filtered = append(filtered, e)
					}
				}
			}
			edges = filtered
			neighborMap = nm
		}
	}

	// Apply optional limits (only when explicitly provided).
	beforeTotal := len(edges)
	beforeByType := map[string]int{}
	if includeCounts && beforeTotal > 0 {
		for _, e := range edges {
			if e == nil {
				continue
			}
			et := strings.ToUpper(strings.TrimSpace(string(e.Type)))
			if et == "" {
				et = "OTHER"
			}
			beforeByType[et]++
		}
	}

	if (maxNeighbors > 0 || maxEdges > 0 || maxPerType > 0) && len(edges) > 0 {
		// Sort edges by "importance" so budgets keep the most meaningful edges.
		// Currently: higher criticality first, then newer edges.
		sort.SliceStable(edges, func(i, j int) bool {
			ei := edges[i]
			ej := edges[j]
			if ei == nil && ej == nil {
				return false
			}
			if ei == nil {
				return false
			}
			if ej == nil {
				return true
			}
			ci := ei.Properties.Criticality
			cj := ej.Properties.Criticality
			if ci != cj {
				return ci > cj
			}
			ti := ei.Metadata.LastUpdatedAt
			tj := ej.Metadata.LastUpdatedAt
			if !ti.Equal(tj) {
				return ti.After(tj)
			}
			return ei.ID < ej.ID
		})

		// 1) Neighbor budget (unique neighbors relative to fragmentID)
		if maxNeighbors > 0 {
			type nScore struct {
				id       string
				score    float64
				firstIdx int
			}
			best := map[string]nScore{}
			for idx, e := range edges {
				if e == nil {
					continue
				}
				neighborID := ""
				if strings.TrimSpace(e.SourceFragmentID) == fragmentID {
					neighborID = strings.TrimSpace(e.TargetFragmentID)
				} else if strings.TrimSpace(e.TargetFragmentID) == fragmentID {
					neighborID = strings.TrimSpace(e.SourceFragmentID)
				}
				if neighborID == "" {
					continue
				}
				score := e.Properties.Criticality
				if cur, ok := best[neighborID]; ok {
					if score > cur.score {
						cur.score = score
						best[neighborID] = cur
					}
					continue
				}
				best[neighborID] = nScore{id: neighborID, score: score, firstIdx: idx}
			}
			neighbors := make([]nScore, 0, len(best))
			for _, v := range best {
				neighbors = append(neighbors, v)
			}
			sort.SliceStable(neighbors, func(i, j int) bool {
				if neighbors[i].score != neighbors[j].score {
					return neighbors[i].score > neighbors[j].score
				}
				return neighbors[i].firstIdx < neighbors[j].firstIdx
			})
			if len(neighbors) > maxNeighbors {
				neighbors = neighbors[:maxNeighbors]
			}
			allowNeighbor := map[string]struct{}{}
			for _, n := range neighbors {
				allowNeighbor[n.id] = struct{}{}
			}
			filtered := make([]*models.DependencyEdge, 0, len(edges))
			for _, e := range edges {
				if e == nil {
					continue
				}
				neighborID := ""
				if strings.TrimSpace(e.SourceFragmentID) == fragmentID {
					neighborID = strings.TrimSpace(e.TargetFragmentID)
				} else if strings.TrimSpace(e.TargetFragmentID) == fragmentID {
					neighborID = strings.TrimSpace(e.SourceFragmentID)
				} else {
					// keep best-effort
					filtered = append(filtered, e)
					continue
				}
				if neighborID == "" {
					continue
				}
				if _, ok := allowNeighbor[neighborID]; ok {
					filtered = append(filtered, e)
				}
			}
			edges = filtered
		}

		// 2) Per-type edge budget
		if maxPerType > 0 && len(edges) > 0 {
			used := map[string]int{}
			filtered := make([]*models.DependencyEdge, 0, len(edges))
			for _, e := range edges {
				if e == nil {
					continue
				}
				et := strings.ToUpper(strings.TrimSpace(string(e.Type)))
				if et == "" {
					et = "OTHER"
				}
				if used[et] >= maxPerType {
					continue
				}
				used[et]++
				filtered = append(filtered, e)
			}
			edges = filtered
		}

		// 3) Total edge budget
		if maxEdges > 0 && len(edges) > maxEdges {
			edges = edges[:maxEdges]
		}
	}

	// includeFragments: fetch fragments for all nodes in the edge set (1-hop neighbors or N-hop subgraph).
	var fragments []interface{}
	if includeFragments && len(edges) > 0 {
		uniq := make(map[string]struct{}, len(edges)*2)
		for _, e := range edges {
			if e == nil {
				continue
			}
			if sid := strings.TrimSpace(e.SourceFragmentID); sid != "" {
				uniq[sid] = struct{}{}
			}
			if tid := strings.TrimSpace(e.TargetFragmentID); tid != "" {
				uniq[tid] = struct{}{}
			}
		}
		ids := make([]string, 0, len(uniq))
		for id := range uniq {
			ids = append(ids, id)
		}

		// Reuse neighborMap from indexPaths filtering when possible; otherwise fetch.
		if neighborMap == nil {
			if nm, ferr := h.service.Fragments().GetMany(r.Context(), ids); ferr == nil {
				neighborMap = nm
			}
		}
		if neighborMap != nil {
			fragments = make([]interface{}, 0, len(ids))
			for _, id := range ids {
				if n := neighborMap[id]; n != nil {
					fragments = append(fragments, n)
				}
			}
		}
	}

	// Optional counts payload (only when requested).
	var counts interface{} = nil
	if includeCounts {
		afterTotal := len(edges)
		afterByType := map[string]int{}
		for _, e := range edges {
			if e == nil {
				continue
			}
			et := strings.ToUpper(strings.TrimSpace(string(e.Type)))
			if et == "" {
				et = "OTHER"
			}
			afterByType[et]++
		}
		byType := map[string]interface{}{}
		seenTypes := map[string]struct{}{}
		for t := range beforeByType {
			seenTypes[t] = struct{}{}
		}
		for t := range afterByType {
			seenTypes[t] = struct{}{}
		}
		for t := range seenTypes {
			b := beforeByType[t]
			a := afterByType[t]
			byType[t] = map[string]interface{}{
				"before":   b,
				"returned": a,
				"hasMore":  b > a,
			}
		}
		counts = map[string]interface{}{
			"total": map[string]interface{}{
				"before":   beforeTotal,
				"returned": afterTotal,
				"hasMore":  beforeTotal > afterTotal,
			},
			"byType": byType,
		}
	}

	// includeCoordinates: resolve geo coordinates for all edge endpoints.
	includeCoordinatesStr := strings.TrimSpace(r.URL.Query().Get("includeCoordinates"))
	includeCoordinates := includeCoordinatesStr == "1" || strings.EqualFold(includeCoordinatesStr, "true")
	var coordsMap map[string]*models.Coordinates
	if includeCoordinates && len(edges) > 0 {
		uniqIDs := make(map[string]struct{}, len(edges)*2)
		for _, e := range edges {
			if e == nil {
				continue
			}
			if sid := strings.TrimSpace(e.SourceFragmentID); sid != "" {
				uniqIDs[sid] = struct{}{}
			}
			if tid := strings.TrimSpace(e.TargetFragmentID); tid != "" {
				uniqIDs[tid] = struct{}{}
			}
		}
		ids := make([]string, 0, len(uniqIDs))
		for id := range uniqIDs {
			ids = append(ids, id)
		}
		fetchMap := neighborMap
		if fetchMap == nil {
			if nm, ferr := h.service.Fragments().GetMany(r.Context(), ids); ferr == nil {
				fetchMap = nm
			}
		}
		if fetchMap != nil {
			coordsMap = make(map[string]*models.Coordinates, len(fetchMap))
			for id, f := range fetchMap {
				if f == nil {
					continue
				}
				if f.Coordinates != nil && models.IsValidCoordinates(f.Coordinates.Lat, f.Coordinates.Lon) {
					coordsMap[id] = f.Coordinates
				} else if lat, lon, ok := horizonrepo.ResolveFragmentCoordinates(f); ok && models.IsValidCoordinates(lat, lon) {
					coordsMap[id] = &models.Coordinates{Lat: lat, Lon: lon}
				}
			}
		}
	}

	response := map[string]interface{}{
		"edges": edges,
		"total": len(edges),
	}
	if includeFragments {
		response["fragments"] = fragments
	}
	if includeCounts {
		response["counts"] = counts
	}
	if coordsMap != nil {
		response["coordinates"] = coordsMap
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
