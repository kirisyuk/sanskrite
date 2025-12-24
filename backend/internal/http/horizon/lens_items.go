package horizon

import (
	"encoding/json"
	"net/http"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// LensItemsHandler handles LensItems HTTP requests
type LensItemsHandler struct {
	lensItemsRepo *horizonrepo.LensItemsRepository
	lensesRepo    *horizonrepo.LensesRepository
	projectsRepo  *repo.ProjectsRepository
}

// NewLensItemsHandler creates a new LensItemsHandler
func NewLensItemsHandler(
	lensItemsRepo *horizonrepo.LensItemsRepository,
	lensesRepo *horizonrepo.LensesRepository,
	projectsRepo *repo.ProjectsRepository,
) *LensItemsHandler {
	return &LensItemsHandler{
		lensItemsRepo: lensItemsRepo,
		lensesRepo:    lensesRepo,
		projectsRepo:  projectsRepo,
	}
}

// ListLensItems handles GET /api/horizon/lenses/{lensId}/items
func (h *LensItemsHandler) ListLensItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		httputil.WriteUnauthorized(w, "Authentication required")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 {
		httputil.WriteBadRequest(w, "INVALID_PATH", "Lens ID is required")
		return
	}
	lensID := pathParts[3]

	// Verify lens exists and belongs to tenant
	lens, err := h.lensesRepo.Get(r.Context(), lensID)
	if err != nil {
		httputil.WriteNotFound(w, "Lens not found")
		return
	}
	if lens.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Verify project access
	project, err := h.projectsRepo.Get(r.Context(), lens.ProjectID)
	if err != nil {
		httputil.WriteNotFound(w, "Project not found")
		return
	}
	if project.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	if h.lensItemsRepo == nil {
		httputil.WriteInternalError(w, "LensItems not configured")
		return
	}

	items, err := h.lensItemsRepo.ListByLens(r.Context(), lensID)
	if err != nil {
		// If Firestore index not ready or other transient failures, return empty list instead of 500
		em := strings.ToLower(err.Error())
		if strings.Contains(em, "failedprecondition") || strings.Contains(em, "index") || strings.Contains(em, "unavailable") {
			httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}
		httputil.WriteInternalError(w, "Failed to list lens items")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateLensItem handles POST /api/horizon/lenses/{lensId}/items
func (h *LensItemsHandler) CreateLensItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		httputil.WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.lensItemsRepo == nil {
		httputil.WriteInternalError(w, "LensItems repo not configured")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 {
		httputil.WriteBadRequest(w, "INVALID_PATH", "Lens ID is required")
		return
	}
	lensID := pathParts[3]

	// Verify lens exists and belongs to tenant
	lens, err := h.lensesRepo.Get(r.Context(), lensID)
	if err != nil {
		httputil.WriteNotFound(w, "Lens not found")
		return
	}
	if lens.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Verify project access
	project, err := h.projectsRepo.Get(r.Context(), lens.ProjectID)
	if err != nil || project.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var body horizonmodels.LensItem
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}

	// Set required fields from context
	body.TenantID = authInfo.TenantID
	body.ProjectID = lens.ProjectID
	body.LensID = lensID

	// Validate
	if err := body.Validate(); err != nil {
		httputil.WriteBadRequest(w, "VALIDATION_ERROR", err.Error())
		return
	}

	id, err := h.lensItemsRepo.Create(r.Context(), &body)
	if err != nil {
		httputil.WriteInternalError(w, "Failed to create lens item")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// UpdateLensItem handles PATCH /api/horizon/lenses/{lensId}/items/{itemId}
func (h *LensItemsHandler) UpdateLensItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH is allowed", nil)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		httputil.WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.lensItemsRepo == nil {
		httputil.WriteInternalError(w, "LensItems repo not configured")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 {
		httputil.WriteBadRequest(w, "INVALID_PATH", "Lens and Item IDs are required")
		return
	}
	lensID := pathParts[3]
	itemID := pathParts[5]

	// Verify lens exists and belongs to tenant
	lens, err := h.lensesRepo.Get(r.Context(), lensID)
	if err != nil {
		httputil.WriteNotFound(w, "Lens not found")
		return
	}
	if lens.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Verify project access
	project, err := h.projectsRepo.Get(r.Context(), lens.ProjectID)
	if err != nil || project.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Verify item belongs to lens and tenant
	item, err := h.lensItemsRepo.Get(r.Context(), itemID)
	if err != nil || item == nil || item.LensID != lensID || item.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		httputil.WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	if len(payload) == 0 {
		httputil.WriteBadRequest(w, "VALIDATION_ERROR", "No fields to update")
		return
	}

	// Whitelist fields allowed for update
	allowed := map[string]struct{}{
		"kind": {}, "subkind": {}, "fragmentId": {}, "canonicalPath": {}, "label": {}, "type": {},
		"state": {}, "status": {}, "expanded": {}, "collapsed": {}, "critical": {}, "position": {},
		"sourceFragmentId": {}, "targetFragmentId": {}, "edgeType": {}, "criticality": {},
		"amplification": {}, "latencyHours": {}, "direction": {}, "viewportConfig": {},
		"layoutData": {}, "metadata": {},
	}
	updates := map[string]interface{}{}
	for k, v := range payload {
		if _, ok := allowed[k]; ok {
			updates[k] = v
		}
	}
	if len(updates) == 0 {
		httputil.WriteBadRequest(w, "VALIDATION_ERROR", "No updatable fields provided")
		return
	}

	if err := h.lensItemsRepo.Update(r.Context(), itemID, updates); err != nil {
		httputil.WriteInternalError(w, "Failed to update lens item")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// DeleteLensItem handles DELETE /api/horizon/lenses/{lensId}/items/{itemId}
func (h *LensItemsHandler) DeleteLensItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		httputil.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		httputil.WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.lensItemsRepo == nil {
		httputil.WriteInternalError(w, "LensItems repo not configured")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 {
		httputil.WriteBadRequest(w, "INVALID_PATH", "Lens and Item IDs are required")
		return
	}
	lensID := pathParts[3]
	itemID := pathParts[5]

	// Verify lens exists and belongs to tenant
	lens, err := h.lensesRepo.Get(r.Context(), lensID)
	if err != nil {
		httputil.WriteNotFound(w, "Lens not found")
		return
	}
	if lens.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Verify project access
	project, err := h.projectsRepo.Get(r.Context(), lens.ProjectID)
	if err != nil || project.TenantID != authInfo.TenantID {
		httputil.WriteForbidden(w, "Access denied")
		return
	}

	// Verify item belongs to lens and tenant
	item, err := h.lensItemsRepo.Get(r.Context(), itemID)
	if err == nil {
		if item.LensID != lensID || item.TenantID != authInfo.TenantID {
			httputil.WriteForbidden(w, "Access denied")
			return
		}
	}

	if err := h.lensItemsRepo.DeleteByID(r.Context(), itemID); err != nil {
		httputil.WriteInternalError(w, "Failed to delete lens item")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

