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

// ProjectsHandler handles project-related HTTP requests
type ProjectsHandler struct {
	repo         *repo.ProjectsRepository
	logger       *log.Logger
	dataSources  *repo.DataSourcesRepository
	profiles     *repo.SourceProfilesRepository
	interactions *repo.InteractionsRepository
}

// NewProjectsHandler creates a new ProjectsHandler
func NewProjectsHandler(repo *repo.ProjectsRepository, logger *log.Logger) *ProjectsHandler {
	return &ProjectsHandler{
		repo:   repo,
		logger: logger,
	}
}

// WithDataSources binds DataSourcesRepository for cascade operations
func (h *ProjectsHandler) WithDataSources(dsrc *repo.DataSourcesRepository) *ProjectsHandler {
	h.dataSources = dsrc
	return h
}

// WithCascade wires additional repos for cascade deletes
func (h *ProjectsHandler) WithCascade(profiles *repo.SourceProfilesRepository, interactions *repo.InteractionsRepository) *ProjectsHandler {
	h.profiles = profiles
	h.interactions = interactions
	return h
}

// CreateProjectRequest represents the request body for creating a project
type CreateProjectRequest struct {
	DisplayName string  `json:"displayName"`
	Description *string `json:"description,omitempty"`
	View        *string `json:"view,omitempty"` // "discovery", "workspace", etc.
}

// CreateProject handles POST /api/v1/projects
func (h *ProjectsHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	if req.DisplayName == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "displayName is required")
		return
	}

	// Create project
	now := time.Now().UTC()
	status := "active"

	project := &models.Project{
		TenantID:    authInfo.TenantID,
		Pk:          authInfo.TenantID,
		DisplayName: req.DisplayName,
		Description: req.Description,
		Status:      status,
		View:        req.View,
		OwnerUserID: authInfo.UID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save to Firestore
	projectID, err := h.repo.Create(r.Context(), project)
	if err != nil {
		h.logger.Printf("Failed to create project: %v", err)
		WriteInternalError(w, "Failed to create project")
		return
	}

	project.ID = projectID
	h.logger.Printf("Successfully created project %s for user %s", projectID, authInfo.UID)

	WriteJSON(w, http.StatusCreated, project)
}

// ListProjects handles GET /api/v1/projects
func (h *ProjectsHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Get optional view filter from query params
	var view *string
	if viewParam := r.URL.Query().Get("view"); viewParam != "" {
		view = &viewParam
	}

	projects, err := h.repo.List(r.Context(), authInfo.TenantID, view)
	if err != nil {
		h.logger.Printf("Failed to list projects for tenant %s: %v", authInfo.TenantID, err)
		WriteInternalError(w, "Failed to list projects")
		return
	}

	WriteJSON(w, http.StatusOK, projects)
}

// GetProject handles GET /api/v1/projects/{id}
func (h *ProjectsHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract projectId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil {
		h.logger.Printf("Failed to fetch project %s: %v", projectID, err)
		WriteNotFound(w, "Project not found")
		return
	}

	// Check access: project must belong to user's tenant
	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	WriteJSON(w, http.StatusOK, project)
}

// DeleteProject handles DELETE /api/v1/projects/{id}
func (h *ProjectsHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract projectId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	// Fetch project to check ownership
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil {
		h.logger.Printf("Project not found: %s", projectID)
		WriteNotFound(w, "Project not found")
		return
	}

	// Check access
	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Delete project
	if err := h.repo.Delete(r.Context(), projectID); err != nil {
		h.logger.Printf("Failed to delete project %s: %v", projectID, err)
		WriteInternalError(w, "Failed to delete project")
		return
	}

	// Cascade for datasources: delete primary, unlink related
	if h.dataSources != nil {
		if err := h.dataSources.DeleteByProject(r.Context(), projectID); err != nil {
			h.logger.Printf("Warning: cascade delete for project %s encountered errors: %v", projectID, err)
		}
	}

	// Cascade: delete source profiles for sources tied to this project
	if h.profiles != nil && h.dataSources != nil {
		if err := h.profiles.DeleteByProject(r.Context(), authInfo.TenantID, projectID, h.dataSources); err != nil {
			h.logger.Printf("Warning: cascade delete profiles for project %s: %v", projectID, err)
		}
	}

	// Cascade: delete interactions for this project
	if h.interactions != nil {
		if err := h.interactions.DeleteByProject(r.Context(), projectID); err != nil {
			h.logger.Printf("Warning: cascade delete interactions for project %s: %v", projectID, err)
		}
	}

	h.logger.Printf("Successfully deleted project %s", projectID)
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Project deleted successfully",
	})
}

// UpdateProject handles PUT /api/v1/projects/{id}
func (h *ProjectsHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PUT is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract projectId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	// Fetch existing project
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil {
		h.logger.Printf("Project not found: %s", projectID)
		WriteNotFound(w, "Project not found")
		return
	}

	// Check access
	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Parse update request
	var updates models.Project
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// Update allowed fields
	if updates.DisplayName != "" {
		project.DisplayName = updates.DisplayName
	}
	if updates.Description != nil {
		project.Description = updates.Description
	}
	if updates.Status != "" {
		project.Status = updates.Status
	}
	if updates.View != nil {
		project.View = updates.View
	}

	// Save
	if err := h.repo.Upsert(r.Context(), project); err != nil {
		h.logger.Printf("Failed to update project %s: %v", projectID, err)
		WriteInternalError(w, "Failed to update project")
		return
	}

	h.logger.Printf("Successfully updated project %s", projectID)
	WriteJSON(w, http.StatusOK, project)
}
