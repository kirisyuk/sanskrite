package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ArtifactsHandler manages CRUD for artifacts (CoreDoc with docType = dynamic_artifact)
type ArtifactsHandler struct {
	coreDocs *repo.CoreDocsRepo
}

func NewArtifactsHandler(core *repo.CoreDocsRepo) *ArtifactsHandler {
	return &ArtifactsHandler{coreDocs: core}
}

// CreateArtifact creates a new artifact in a project
// POST /api/v1/projects/:projectId/artifacts
func (h *ArtifactsHandler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID required")
		return
	}
	projectID := path[3]

	var req models.CoreDoc
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Force required identifiers
	req.TenantID = auth.TenantID
	req.ProjectID = projectID
	// Ensure it's an artifact
	dt := "dynamic_artifact"
	req.DocType = &dt
	if req.ArtifactConfig == nil {
		req.ArtifactConfig = &models.ArtifactConfig{
			Format:             "text",
			Focus:              "analysis",
			OrchestratorModel:  "gemini-quality",
			DefaultModel:       "gemini-quality",
			ReviewPrompt:       "Review and refine for clarity, consistency, and completeness.",
			Structure:          []models.ArtifactSection{},
			ContextSources:     models.ArtifactContextSources{},
			AutoUpdateEnabled:  false,
			VersionsRetained:   10,
			Versions:           []models.ArtifactVersion{},
			GenerationSettings: models.GenerationSettings{PreferredLanguage: "en", SystemPrompt: "You are a helpful assistant."},
		}
	}

	id, err := h.coreDocs.Create(r.Context(), &req)
	if err != nil {
		WriteInternalError(w, "Failed to create artifact")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// ListArtifacts lists artifacts for a project
// GET /api/v1/projects/:projectId/artifacts
func (h *ArtifactsHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID required")
		return
	}
	projectID := path[3]
	items, err := h.coreDocs.ListByProject(r.Context(), auth.TenantID, projectID, 0)
	if err != nil {
		WriteInternalError(w, "Failed to list artifacts")
		return
	}
	// Filter artifacts only
	out := make([]*models.CoreDoc, 0, len(items))
	for _, d := range items {
		if d.DocType != nil && *d.DocType == "dynamic_artifact" {
			out = append(out, d)
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

// GetArtifact returns an artifact by ID
// GET /api/v1/projects/:projectId/artifacts/:id
func (h *ArtifactsHandler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID required")
		return
	}
	id := parts[5]
	d, err := h.coreDocs.Get(r.Context(), id)
	if err != nil || d == nil || d.TenantID != auth.TenantID {
		WriteNotFound(w, "Artifact not found")
		return
	}
	WriteJSON(w, http.StatusOK, d)
}

// UpdateArtifact updates an artifact config (PATCH)
// PATCH /api/v1/projects/:projectId/artifacts/:id
func (h *ArtifactsHandler) UpdateArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID required")
		return
	}
	id := parts[5]
	existing, err := h.coreDocs.Get(r.Context(), id)
	if err != nil || existing == nil || existing.TenantID != auth.TenantID {
		WriteNotFound(w, "Artifact not found")
		return
	}
	var patch models.CoreDoc
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}
	// Merge selective fields
	if patch.Description != nil {
		existing.Description = patch.Description
	}
	if patch.ArtifactConfig != nil {
		existing.ArtifactConfig = patch.ArtifactConfig
	}
	if err := h.coreDocs.Upsert(r.Context(), existing); err != nil {
		WriteInternalError(w, "Failed to update artifact")
		return
	}
	WriteJSON(w, http.StatusOK, existing)
}

// DeleteArtifact deletes an artifact
// DELETE /api/v1/projects/:projectId/artifacts/:id
func (h *ArtifactsHandler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID required")
		return
	}
	id := parts[5]
	d, err := h.coreDocs.Get(r.Context(), id)
	if err != nil || d == nil || d.TenantID != auth.TenantID {
		WriteNotFound(w, "Artifact not found")
		return
	}
	ok, derr := h.coreDocs.Delete(r.Context(), id)
	if derr != nil || !ok {
		WriteInternalError(w, "Failed to delete artifact")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
