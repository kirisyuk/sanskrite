package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

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

	edgeID, err := h.service.Edges().Create(r.Context(), &edge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	edge.ID = edgeID
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

	response := map[string]interface{}{
		"edges": edges,
		"total": len(edges),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

