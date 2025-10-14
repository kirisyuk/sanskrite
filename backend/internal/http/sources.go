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

// SourcesHandler manages project–source links
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

// Inline light helpers
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
