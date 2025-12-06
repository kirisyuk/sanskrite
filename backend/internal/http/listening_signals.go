package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ListeningSignalsHandler manages CRUD for listening signals
type ListeningSignalsHandler struct {
	repo *repo.ListeningSignalsRepository
}

// NewListeningSignalsHandler creates a new handler
func NewListeningSignalsHandler(repo *repo.ListeningSignalsRepository) *ListeningSignalsHandler {
	return &ListeningSignalsHandler{repo: repo}
}

// CreateSignal creates a new listening signal
// POST /api/v1/projects/:projectId/listening-signals
// POST /api/v1/listening-signals (for tenant-wide signals)
func (h *ListeningSignalsHandler) CreateSignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var projectID *string
	
	// Check if projectId is in path
	if len(path) >= 4 && path[2] == "projects" {
		projectIDStr := path[3]
		projectID = &projectIDStr
	}

	var req models.ListeningSignal
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Force required identifiers
	req.TenantID = auth.TenantID
	req.ProjectID = projectID
	req.CreatedBy = auth.UID
	req.IsActive = true

	id, err := h.repo.Create(r.Context(), &req)
	if err != nil {
		WriteInternalError(w, "Failed to create signal")
		return
	}
	req.ID = id

	WriteJSON(w, http.StatusCreated, req)
}

// ListSignals lists signals for a project or tenant
// GET /api/v1/projects/:projectId/listening-signals
// GET /api/v1/listening-signals
func (h *ListeningSignalsHandler) ListSignals(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var projectID *string
	
	// Check if projectId is in path
	if len(path) >= 4 && path[2] == "projects" {
		projectIDStr := path[3]
		projectID = &projectIDStr
	}

	var signals []*models.ListeningSignal
	var err error

	if projectID != nil {
		signals, err = h.repo.ListByProject(r.Context(), auth.TenantID, *projectID)
	} else {
		signals, err = h.repo.ListByTenant(r.Context(), auth.TenantID, nil)
	}

	if err != nil {
		WriteInternalError(w, "Failed to list signals")
		return
	}

	WriteJSON(w, http.StatusOK, signals)
}

// GetSignal returns a signal by ID
// GET /api/v1/listening-signals/:id
func (h *ListeningSignalsHandler) GetSignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 3 {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}
	id := path[2]

	signal, err := h.repo.Get(r.Context(), id)
	if err != nil || signal == nil || signal.TenantID != auth.TenantID {
		WriteNotFound(w, "Signal not found")
		return
	}

	WriteJSON(w, http.StatusOK, signal)
}

// UpdateSignal updates a signal
// PATCH /api/v1/listening-signals/:id
func (h *ListeningSignalsHandler) UpdateSignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 3 {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}
	id := path[2]

	existing, err := h.repo.Get(r.Context(), id)
	if err != nil || existing == nil || existing.TenantID != auth.TenantID {
		WriteNotFound(w, "Signal not found")
		return
	}

	var patch models.ListeningSignal
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Merge selective fields
	if patch.Name != "" {
		existing.Name = patch.Name
	}
	if patch.Signal != "" {
		existing.Signal = patch.Signal
	}
	if patch.SearchQueries != nil {
		existing.SearchQueries = patch.SearchQueries
	}
	if patch.Description != nil {
		existing.Description = patch.Description
	}
	if patch.Tags != nil {
		existing.Tags = patch.Tags
	}
	// IsActive can be updated
	existing.IsActive = patch.IsActive

	if err := h.repo.Update(r.Context(), existing); err != nil {
		WriteInternalError(w, "Failed to update signal")
		return
	}

	WriteJSON(w, http.StatusOK, existing)
}

// ActivateSignal activates a signal
// POST /api/v1/listening-signals/:id/activate
// POST /api/v1/projects/:projectId/listening-signals/:id/activate
func (h *ListeningSignalsHandler) ActivateSignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var id string
	// Handle both /api/v1/listening-signals/:id/activate and /api/v1/projects/:projectId/listening-signals/:id/activate
	if len(path) >= 4 && path[3] == "activate" {
		id = path[2]
	} else if len(path) >= 7 && path[6] == "activate" {
		id = path[5]
	} else {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}

	signal, err := h.repo.Get(r.Context(), id)
	if err != nil || signal == nil || signal.TenantID != auth.TenantID {
		WriteNotFound(w, "Signal not found")
		return
	}

	signal.IsActive = true
	if err := h.repo.Update(r.Context(), signal); err != nil {
		WriteInternalError(w, "Failed to activate signal")
		return
	}

	WriteJSON(w, http.StatusOK, signal)
}

// DeactivateSignal deactivates a signal
// POST /api/v1/listening-signals/:id/deactivate
// POST /api/v1/projects/:projectId/listening-signals/:id/deactivate
func (h *ListeningSignalsHandler) DeactivateSignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var id string
	// Handle both /api/v1/listening-signals/:id/deactivate and /api/v1/projects/:projectId/listening-signals/:id/deactivate
	if len(path) >= 4 && path[3] == "deactivate" {
		id = path[2]
	} else if len(path) >= 7 && path[6] == "deactivate" {
		id = path[5]
	} else {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}

	signal, err := h.repo.Get(r.Context(), id)
	if err != nil || signal == nil || signal.TenantID != auth.TenantID {
		WriteNotFound(w, "Signal not found")
		return
	}

	signal.IsActive = false
	if err := h.repo.Update(r.Context(), signal); err != nil {
		WriteInternalError(w, "Failed to deactivate signal")
		return
	}

	WriteJSON(w, http.StatusOK, signal)
}

// DeleteSignal deletes a signal
// DELETE /api/v1/listening-signals/:id
func (h *ListeningSignalsHandler) DeleteSignal(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 3 {
		WriteBadRequest(w, "INVALID_PATH", "Signal ID required")
		return
	}
	id := path[2]

	signal, err := h.repo.Get(r.Context(), id)
	if err != nil || signal == nil || signal.TenantID != auth.TenantID {
		WriteNotFound(w, "Signal not found")
		return
	}

	if err := h.repo.Delete(r.Context(), id); err != nil {
		WriteInternalError(w, "Failed to delete signal")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

