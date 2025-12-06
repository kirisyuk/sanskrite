package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DocumentArtifactSignalsHandler manages document-artifact-signal relationships
type DocumentArtifactSignalsHandler struct {
	repo *repo.DocumentArtifactSignalsRepository
}

// NewDocumentArtifactSignalsHandler creates a new handler
func NewDocumentArtifactSignalsHandler(repo *repo.DocumentArtifactSignalsRepository) *DocumentArtifactSignalsHandler {
	return &DocumentArtifactSignalsHandler{repo: repo}
}

// CreateLink creates a new link between artifact and signal
// POST /api/v1/artifacts/:artifactId/signals
func (h *DocumentArtifactSignalsHandler) CreateLink(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID required")
		return
	}
	artifactID := path[2]

	var req struct {
		SignalID string `json:"signalId"`
		Priority int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	if req.SignalID == "" {
		WriteBadRequest(w, "MISSING_FIELD", "signalId is required")
		return
	}

	if req.Priority == 0 {
		req.Priority = 0 // Default priority
	}

	link := &models.DocumentArtifactSignal{
		DocumentArtifactID: artifactID,
		SignalID:            req.SignalID,
		Priority:            req.Priority,
		IsActive:            true,
		CreatedBy:          auth.UID,
	}

	id, err := h.repo.Create(r.Context(), link)
	if err != nil {
		WriteInternalError(w, "Failed to create link")
		return
	}
	link.ID = id

	WriteJSON(w, http.StatusCreated, link)
}

// ListLinksByArtifact lists all signals linked to an artifact
// GET /api/v1/artifacts/:artifactId/signals
func (h *DocumentArtifactSignalsHandler) ListLinksByArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID required")
		return
	}
	artifactID := path[2]

	links, err := h.repo.ListByArtifact(r.Context(), artifactID)
	if err != nil {
		WriteInternalError(w, "Failed to list links")
		return
	}

	WriteJSON(w, http.StatusOK, links)
}

// ListLinksBySignal lists all artifacts linked to a signal
// GET /api/v1/listening-signals/:signalId/artifacts
func (h *DocumentArtifactSignalsHandler) ListLinksBySignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}
	signalID := path[2]

	links, err := h.repo.ListBySignal(r.Context(), signalID)
	if err != nil {
		WriteInternalError(w, "Failed to list links")
		return
	}

	WriteJSON(w, http.StatusOK, links)
}

// UpdateLink updates a link
// PATCH /api/v1/artifacts/:artifactId/signals/:linkId
func (h *DocumentArtifactSignalsHandler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Link ID required")
		return
	}
	linkID := path[4]

	existing, err := h.repo.Get(r.Context(), linkID)
	if err != nil || existing == nil {
		WriteNotFound(w, "Link not found")
		return
	}

	var patch models.DocumentArtifactSignal
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Merge selective fields
	existing.Priority = patch.Priority
	existing.IsActive = patch.IsActive

	if err := h.repo.Update(r.Context(), existing); err != nil {
		WriteInternalError(w, "Failed to update link")
		return
	}

	WriteJSON(w, http.StatusOK, existing)
}

// DeleteLink deletes a link
// DELETE /api/v1/artifacts/:artifactId/signals/:linkId
func (h *DocumentArtifactSignalsHandler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Link ID required")
		return
	}
	linkID := path[4]

	if err := h.repo.Delete(r.Context(), linkID); err != nil {
		WriteInternalError(w, "Failed to delete link")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

