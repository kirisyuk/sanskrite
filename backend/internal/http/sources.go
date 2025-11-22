package http

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SourcesHandler manages project–source links and data sources
type SourcesHandler struct {
	sp     *repo.SourceProjectsRepository
	ds     *repo.DataSourcesRepository
	logger *log.Logger
}

func NewSourcesHandler(sp *repo.SourceProjectsRepository, ds *repo.DataSourcesRepository, logger *log.Logger) *SourcesHandler {
	return &SourcesHandler{sp: sp, ds: ds, logger: logger}
}

// AttachSource handles POST /api/v1/projects/{id}/sources
func (h *SourcesHandler) AttachSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	var body struct {
		SourceID string `json:"sourceId"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.SourceID) == "" {
		WriteBadRequest(w, "INVALID_JSON", "sourceId is required")
		return
	}

	now := time.Now().UTC()
	sp := &models.SourceProject{
		TenantID:  auth.TenantID,
		SourceID:  body.SourceID,
		ProjectID: projectID,
		Status:    firstNonEmpty(body.Status, "active"),
		CreatedAt: now,
		UpdatedAt: now,
	}
	id, err := h.sp.Create(r.Context(), sp)
	if err != nil {
		WriteInternalError(w, "Failed to attach source")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// GetSourceProjectStatus handles GET /api/v1/projects/{id}/sources/{sourceId}/status
// Returns the status and details of a source-project link
func (h *SourcesHandler) GetSourceProjectStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 || parts[6] != "status" {
		WriteBadRequest(w, "INVALID_PATH", "Project ID, Source ID and /status are required")
		return
	}
	projectID := parts[3]
	sourceID := parts[5]

	// Get source-project link
	link, err := h.sp.GetByProjectAndSource(r.Context(), auth.TenantID, projectID, sourceID)
	if err != nil {
		h.logger.Printf("Failed to get source-project link: %v", err)
		WriteInternalError(w, "Failed to get source-project link")
		return
	}

	if link == nil {
		WriteNotFound(w, "Source-project link not found")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"id":         link.ID,
		"sourceId":   link.SourceID,
		"projectId":  link.ProjectID,
		"status":     link.Status,
		"weights":    link.Weights,
		"syncConfig": link.SyncConfig,
		"createdAt":  link.CreatedAt,
		"updatedAt":  link.UpdatedAt,
	})
}

// DetachSource handles DELETE /api/v1/projects/{id}/sources/{sourceId}
func (h *SourcesHandler) DetachSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project and Source IDs are required")
		return
	}
	projectID := parts[3]
	sourceID := parts[5]
	if err := h.sp.DeleteByProjectAndSource(r.Context(), auth.TenantID, projectID, sourceID); err != nil {
		WriteInternalError(w, "Failed to detach source")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// UpdateSourceProjectStatus handles PATCH /api/v1/projects/{id}/sources/{sourceId}/status
// Updates the status of a source-project link (active/paused)
func (h *SourcesHandler) UpdateSourceProjectStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 || parts[6] != "status" {
		WriteBadRequest(w, "INVALID_PATH", "Project ID, Source ID and /status are required")
		return
	}
	projectID := parts[3]
	sourceID := parts[5]

	var req struct {
		Status string `json:"status"` // active, paused
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if req.Status != "active" && req.Status != "paused" {
		WriteBadRequest(w, "INVALID_STATUS", "status must be active or paused")
		return
	}

	// Find existing link
	links, err := h.sp.ListByProject(r.Context(), auth.TenantID, projectID, nil)
	if err != nil {
		h.logger.Printf("Failed to list source-project links: %v", err)
		WriteInternalError(w, "Failed to find source-project link")
		return
	}

	var foundLink *models.SourceProject
	for _, link := range links {
		if link.SourceID == sourceID {
			foundLink = link
			break
		}
	}

	if foundLink == nil {
		WriteNotFound(w, "Source-project link not found")
		return
	}

	// Update status
	foundLink.Status = req.Status
	foundLink.UpdatedAt = time.Now().UTC()

	if err := h.sp.Upsert(r.Context(), foundLink); err != nil {
		h.logger.Printf("Failed to update source-project status: %v", err)
		WriteInternalError(w, "Failed to update source-project status")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"id":     foundLink.ID,
		"status": foundLink.Status,
	})
}

// CreateSource handles POST /api/v1/sources
func (h *SourcesHandler) CreateSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req struct {
		Kind        string               `json:"kind"` // rss, telegram_channel, web
		DisplayName string               `json:"displayName"`
		Details     models.SourceDetails `json:"details"`
		Status      string               `json:"status"` // active, paused, completed
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Validate kind
	if req.Kind != "rss" && req.Kind != "telegram_channel" && req.Kind != "web" {
		WriteBadRequest(w, "INVALID_KIND", "kind must be rss, telegram_channel, or web")
		return
	}

	// Validate required fields based on kind
	if req.Kind == "rss" && (req.Details.RSSURL == nil || *req.Details.RSSURL == "") {
		WriteBadRequest(w, "VALIDATION_ERROR", "rssUrl is required for RSS sources")
		return
	}
	if req.Kind == "telegram_channel" && (req.Details.ChannelUsername == nil || *req.Details.ChannelUsername == "") {
		WriteBadRequest(w, "VALIDATION_ERROR", "channelUsername is required for Telegram sources")
		return
	}
	if req.Kind == "web" && (req.Details.URI == nil || *req.Details.URI == "") {
		WriteBadRequest(w, "VALIDATION_ERROR", "uri is required for web sources")
		return
	}

	// Set default status
	status := req.Status
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "paused" && status != "completed" {
		status = "active"
	}

	// Normalize channel username (remove @ if present)
	if req.Kind == "telegram_channel" && req.Details.ChannelUsername != nil {
		username := strings.TrimPrefix(strings.TrimSpace(*req.Details.ChannelUsername), "@")
		req.Details.ChannelUsername = &username
	}

	// Generate display name if not provided
	displayName := req.DisplayName
	if displayName == "" {
		if req.Kind == "rss" && req.Details.RSSURL != nil {
			displayName = *req.Details.RSSURL
		} else if req.Kind == "telegram_channel" && req.Details.ChannelUsername != nil {
			displayName = "@" + *req.Details.ChannelUsername
		} else if req.Kind == "web" && req.Details.URI != nil {
			displayName = *req.Details.URI
		} else {
			displayName = req.Kind + " source"
		}
	}

	now := time.Now().UTC()
	ds := &models.DataSource{
		TenantID:    auth.TenantID, // Will be cleared for public sources in Create()
		DisplayName: displayName,
		Kind:        req.Kind,
		Status:      status,
		Details:     req.Details,
		OwnerUserID: auth.UID,
		CreatedAt:   now,
		UpdatedAt:   now,
		FirstSeen:   now,
		LastSeen:    now,
		ErrorCount:  0,
		TrustLevel:  0.5, // Default trust level
	}

	id, err := h.ds.Create(r.Context(), ds)
	if err != nil {
		h.logger.Printf("Failed to create source: %v", err)
		WriteInternalError(w, "Failed to create source")
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "source": ds})
}

// GetSource handles GET /api/v1/sources/{id}
func (h *SourcesHandler) GetSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		WriteBadRequest(w, "INVALID_PATH", "Source ID is required")
		return
	}
	sourceID := parts[2]

	ds, err := h.ds.Get(r.Context(), sourceID)
	if err != nil || ds == nil {
		WriteNotFound(w, "Source not found")
		return
	}

	WriteJSON(w, http.StatusOK, ds)
}

// UpdateSource handles PUT /api/v1/sources/{id}
func (h *SourcesHandler) UpdateSource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PUT is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		WriteBadRequest(w, "INVALID_PATH", "Source ID is required")
		return
	}
	sourceID := parts[2]

	// Get existing source
	ds, err := h.ds.Get(r.Context(), sourceID)
	if err != nil || ds == nil {
		WriteNotFound(w, "Source not found")
		return
	}

	var req struct {
		DisplayName *string               `json:"displayName,omitempty"`
		Details     *models.SourceDetails `json:"details,omitempty"`
		Status      *string               `json:"status,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Update fields
	if req.DisplayName != nil {
		ds.DisplayName = *req.DisplayName
	}
	if req.Details != nil {
		ds.Details = *req.Details
	}
	if req.Status != nil {
		status := *req.Status
		if status == "active" || status == "paused" || status == "completed" {
			ds.Status = status
		}
	}

	if err := h.ds.Upsert(r.Context(), ds); err != nil {
		h.logger.Printf("Failed to update source: %v", err)
		WriteInternalError(w, "Failed to update source")
		return
	}

	WriteJSON(w, http.StatusOK, ds)
}

// UpdateSourceStatus handles PATCH /api/v1/sources/{id}/status
func (h *SourcesHandler) UpdateSourceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[3] != "status" {
		WriteBadRequest(w, "INVALID_PATH", "Source ID and /status are required")
		return
	}
	sourceID := parts[2]

	// Get existing source
	ds, err := h.ds.Get(r.Context(), sourceID)
	if err != nil || ds == nil {
		WriteNotFound(w, "Source not found")
		return
	}

	var req struct {
		Status string `json:"status"` // active, paused, completed
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if req.Status != "active" && req.Status != "paused" && req.Status != "completed" {
		WriteBadRequest(w, "INVALID_STATUS", "status must be active, paused, or completed")
		return
	}

	ds.Status = req.Status
	if err := h.ds.Upsert(r.Context(), ds); err != nil {
		h.logger.Printf("Failed to update source status: %v", err)
		WriteInternalError(w, "Failed to update source status")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{"id": ds.ID, "status": ds.Status})
}

// ListSources handles GET /api/v1/sources
// Query parameters:
//   - projectId: filter by project (returns sources linked to this project)
//   - kind: filter by kind (rss, telegram_channel, web)
//   - status: filter by status (active, paused, completed)
//   - tenantId: filter by tenant (defaults to authenticated user's tenant)
func (h *SourcesHandler) ListSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Parse query parameters
	q := r.URL.Query()
	projectID := q.Get("projectId")
	kindFilter := q.Get("kind")
	statusFilter := q.Get("status")
	tenantIDFilter := q.Get("tenantId")

	// Default to authenticated user's tenant
	tenantID := auth.TenantID
	if tenantIDFilter != "" {
		// Allow filtering by other tenant only if user has admin rights (future enhancement)
		tenantID = tenantIDFilter
	}

	var sources []*models.DataSource
	var err error

	// Filter by project if specified
	if projectID != "" {
		// Verify project belongs to tenant
		// Note: This requires projectsRepo which SourcesHandler doesn't have
		// For now, we'll use ListByProject which already handles tenant scoping
		sources, err = h.ds.ListByProject(r.Context(), tenantID, projectID)
		if err != nil {
			h.logger.Printf("Failed to list sources by project: %v", err)
			WriteInternalError(w, "Failed to list sources")
			return
		}
	} else {
		// List all sources for tenant
		var kinds []string
		if kindFilter != "" {
			kinds = []string{kindFilter}
		}
		var status *string
		if statusFilter != "" {
			status = &statusFilter
		}
		sources, err = h.ds.ListByTenant(r.Context(), tenantID, kinds, status)
		if err != nil {
			h.logger.Printf("Failed to list sources by tenant: %v", err)
			WriteInternalError(w, "Failed to list sources")
			return
		}
	}

	// Note: Filters (kind and status) are already applied by ListByTenant when projectID is empty,
	// so no need for additional post-filtering. This keeps the code simpler and more efficient.

	WriteJSON(w, http.StatusOK, sources)
}

// Inline light helpers
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
