package horizon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

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
		"tenantId":         strings.TrimSpace(authTenant),
		"projectId":        strings.TrimSpace(projectID),
		"lensId":           strings.TrimSpace(lensID),
		"edgeId":           strings.TrimSpace(edge.ID),
		"sourceFragmentId": strings.TrimSpace(edge.SourceFragmentID),
		"targetFragmentId": strings.TrimSpace(edge.TargetFragmentID),
		"type":             string(edge.Type),
		"properties":       edge.Properties,
		"qualifiers":       edge.Qualifiers,
		"ts":               time.Now().UTC().Format(time.RFC3339Nano),
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
		http.Error(w, "Access denied", http.StatusForbidden)
		return
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

	var edges []*models.DependencyEdge
	var err error

	tenantID := "global" // HorizonFragment всегда глобальные

	switch direction {
	case "outgoing":
		edges, err = h.service.Edges().ListBySource(r.Context(), tenantID, fragmentID, edgeType, 100)
	case "incoming":
		edges, err = h.service.Edges().ListByTarget(r.Context(), tenantID, fragmentID, edgeType, 100)
	case "both":
		outgoing, err1 := h.service.Edges().ListBySource(r.Context(), tenantID, fragmentID, edgeType, 100)
		incoming, err2 := h.service.Edges().ListByTarget(r.Context(), tenantID, fragmentID, edgeType, 100)
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

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter out edges pointing to unindexed neighbor fragments (default), to avoid exploding graphs.
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

		neighborMap, ferr := h.service.Fragments().GetMany(r.Context(), neighborIDs)
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
				n := neighborMap[neighborID]
				if n != nil && len(n.IndexPaths) > 0 {
					filtered = append(filtered, e)
				}
			}
			edges = filtered
		}
	}

	response := map[string]interface{}{
		"edges": edges,
		"total": len(edges),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
