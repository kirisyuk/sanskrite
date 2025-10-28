package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// ArtifactTemplatesHandler handles artifact template endpoints
type ArtifactTemplatesHandler struct {
	service *services.ArtifactTemplateService
}

// NewArtifactTemplatesHandler creates a new handler
func NewArtifactTemplatesHandler(service *services.ArtifactTemplateService) *ArtifactTemplatesHandler {
	return &ArtifactTemplatesHandler{service: service}
}

// ListTemplates returns available templates
// GET /api/v1/artifact-templates?category=technical
func (h *ArtifactTemplatesHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	category := r.URL.Query().Get("category")
	var categoryPtr *string
	if category != "" {
		categoryPtr = &category
	}

	templates, err := h.service.ListAvailableTemplates(r.Context(), auth.TenantID, categoryPtr)
	if err != nil {
		WriteInternalError(w, "Failed to list templates")
		return
	}

	WriteJSON(w, http.StatusOK, templates)
}

// GetTemplate returns a specific template
// GET /api/v1/artifact-templates/:id
func (h *ArtifactTemplatesHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract template ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Template ID is required")
		return
	}
	templateID := pathParts[3]

	template, err := h.service.GetTemplate(r.Context(), templateID)
	if err != nil {
		WriteInternalError(w, "Failed to get template")
		return
	}

	WriteJSON(w, http.StatusOK, template)
}

// CreateTemplate creates a new custom template
// POST /api/v1/artifact-templates
func (h *ArtifactTemplatesHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req models.ArtifactTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Validate required fields
	if req.Name == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Template name required")
		return
	}
	if req.Category == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Template category required")
		return
	}
	if len(req.DefaultStructure) == 0 {
		WriteBadRequest(w, "MISSING_FIELD", "Template structure required")
		return
	}

	templateID, err := h.service.CreateCustomTemplate(r.Context(), &req, auth.TenantID, auth.UID)
	if err != nil {
		WriteInternalError(w, "Failed to create template")
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]string{
		"id": templateID,
	})
}

// UpdateTemplate updates an existing template
// PUT /api/v1/artifact-templates/:id
func (h *ArtifactTemplatesHandler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract template ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Template ID is required")
		return
	}
	templateID := pathParts[3]

	var req models.ArtifactTemplate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	req.ID = templateID

	// Get existing template to check ownership
	existing, err := h.service.GetTemplate(r.Context(), templateID)
	if err != nil {
		WriteNotFound(w, "Template not found")
		return
	}

	// Check ownership
	if existing.CreatedBy != auth.UID {
		WriteForbidden(w, "Only template creator can update")
		return
	}

	if err := h.service.UpdateTemplate(r.Context(), &req, auth.UID); err != nil {
		WriteInternalError(w, "Failed to update template")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// DeleteTemplate deletes a template
// DELETE /api/v1/artifact-templates/:id
func (h *ArtifactTemplatesHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract template ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Template ID is required")
		return
	}
	templateID := pathParts[3]

	// Get existing template to check ownership
	existing, err := h.service.GetTemplate(r.Context(), templateID)
	if err != nil {
		WriteNotFound(w, "Template not found")
		return
	}

	// Check ownership
	if existing.CreatedBy != auth.UID {
		WriteForbidden(w, "Only template creator can delete")
		return
	}

	if err := h.service.DeleteTemplate(r.Context(), templateID, auth.UID); err != nil {
		WriteInternalError(w, "Failed to delete template")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// PublishTemplate publishes a template to community
// POST /api/v1/artifact-templates/:id/publish
func (h *ArtifactTemplatesHandler) PublishTemplate(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract template ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Template ID is required")
		return
	}
	templateID := pathParts[3]

	err := h.service.PublishToCommunity(r.Context(), templateID, auth.UID)
	if err != nil {
		WriteInternalError(w, "Failed to publish template")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GenerateStructure generates document structure using AI
// POST /api/v1/artifact-templates/generate-structure
func (h *ArtifactTemplatesHandler) GenerateStructure(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req services.StructureRequirements
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Validate required fields
	if req.DocumentType == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Document type required")
		return
	}
	if req.Topic == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Topic required")
		return
	}

	sections, err := h.service.GenerateStructure(r.Context(), req)
	if err != nil {
		WriteInternalError(w, "Failed to generate structure")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"sections": sections,
	})
}

// CreateFromArtifact creates a template from an existing artifact
// POST /api/v1/artifact-templates/from-artifact
func (h *ArtifactTemplatesHandler) CreateFromArtifact(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req services.CreateFromArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Validate required fields
	if req.ArtifactID == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Artifact ID required")
		return
	}
	if req.TemplateName == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Template name required")
		return
	}
	if req.Category == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Category required")
		return
	}

	templateID, err := h.service.CreateFromArtifact(r.Context(), req, auth.TenantID, auth.UID)
	if err != nil {
		WriteInternalError(w, "Failed to create template from artifact")
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]string{
		"id": templateID,
	})
}
