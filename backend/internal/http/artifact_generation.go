package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// ArtifactGenerationHandler handles artifact generation endpoints
type ArtifactGenerationHandler struct {
	orchestrator *services.ArtifactOrchestratorService
}

// NewArtifactGenerationHandler creates a new handler
func NewArtifactGenerationHandler(orchestrator *services.ArtifactOrchestratorService) *ArtifactGenerationHandler {
	return &ArtifactGenerationHandler{orchestrator: orchestrator}
}

// StartGeneration starts artifact generation
// POST /api/v1/artifacts/:id/generate
func (h *ArtifactGenerationHandler) StartGeneration(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract artifact ID from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID is required")
		return
	}
	artifactID := pathParts[3]

	// Parse request body
	var req struct {
		Mode      string `json:"mode"`      // "auto" | "interactive"
		ProjectID string `json:"projectId"` // Required for context gathering
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	// Validate mode
	if req.Mode != "auto" && req.Mode != "interactive" {
		req.Mode = "interactive" // Default
	}

	// Start generation
	session, err := h.orchestrator.StartGeneration(r.Context(), services.StartGenerationRequest{
		ArtifactID: artifactID,
		Mode:       req.Mode,
		TenantID:   auth.TenantID,
		ProjectID:  req.ProjectID,
	})

	if err != nil {
		WriteInternalError(w, "Failed to start generation")
		return
	}

	WriteJSON(w, http.StatusOK, session)
}

// GenerateSection generates a specific section
// POST /api/v1/artifacts/:id/sections/:sectionId/generate
func (h *ArtifactGenerationHandler) GenerateSection(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Extract IDs from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID and section ID are required")
		return
	}
	// artifactID := pathParts[3]
	sectionID := pathParts[5]

	// Parse request body
	var req struct {
		SessionID      string  `json:"sessionId"`
		ProjectID      string  `json:"projectId"`
		PromptOverride *string `json:"promptOverride,omitempty"`
		ModelOverride  *string `json:"modelOverride,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	if req.SessionID == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Session ID is required")
		return
	}

	// Generate section
	result, err := h.orchestrator.GenerateSection(r.Context(), services.GenerateSectionRequest{
		SessionID:      req.SessionID,
		SectionID:      sectionID,
		TenantID:       auth.TenantID,
		ProjectID:      req.ProjectID,
		UserID:         auth.UID,
		PromptOverride: req.PromptOverride,
		ModelOverride:  req.ModelOverride,
	})

	if err != nil {
		WriteInternalError(w, "Failed to generate section")
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// RegenerateSection regenerates a section with new prompt
// POST /api/v1/artifacts/:id/sections/:sectionId/regenerate
func (h *ArtifactGenerationHandler) RegenerateSection(w http.ResponseWriter, r *http.Request) {
	// Same as GenerateSection but with required promptOverride
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID and section ID are required")
		return
	}
	sectionID := pathParts[5]

	var req struct {
		SessionID string  `json:"sessionId"`
		ProjectID string  `json:"projectId"`
		NewPrompt string  `json:"newPrompt"`
		Model     *string `json:"model,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	if req.SessionID == "" || req.NewPrompt == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Session ID and new prompt are required")
		return
	}

	result, err := h.orchestrator.GenerateSection(r.Context(), services.GenerateSectionRequest{
		SessionID:      req.SessionID,
		SectionID:      sectionID,
		TenantID:       auth.TenantID,
		ProjectID:      req.ProjectID,
		UserID:         auth.UID,
		PromptOverride: &req.NewPrompt,
		ModelOverride:  req.Model,
	})

	if err != nil {
		WriteInternalError(w, "Failed to regenerate section")
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// AssembleDocument assembles final document from sections
// POST /api/v1/artifacts/:id/assemble
func (h *ArtifactGenerationHandler) AssembleDocument(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	if req.SessionID == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Session ID is required")
		return
	}

	assembled, err := h.orchestrator.AssembleDocument(r.Context(), req.SessionID)
	if err != nil {
		WriteInternalError(w, "Failed to assemble document")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{
		"content": assembled,
	})
}

// SaveVersion saves current state as a new version
// POST /api/v1/artifacts/:id/versions
func (h *ArtifactGenerationHandler) SaveVersion(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req struct {
		SessionID string `json:"sessionId"`
		Trigger   string `json:"trigger"` // "manual" | "auto" | "scheduled"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}

	if req.SessionID == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Session ID is required")
		return
	}

	if req.Trigger == "" {
		req.Trigger = "manual"
	}

	version, err := h.orchestrator.SaveVersion(r.Context(), req.SessionID, req.Trigger)
	if err != nil {
		WriteInternalError(w, "Failed to save version")
		return
	}

	WriteJSON(w, http.StatusCreated, version)
}

// GetSession retrieves generation session status
// GET /api/v1/generation-sessions/:sessionId
func (h *ArtifactGenerationHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Session ID is required")
		return
	}
	sessionID := pathParts[3]

	session, err := h.orchestrator.GetSession(r.Context(), sessionID)
	if err != nil {
		WriteInternalError(w, "Failed to get session")
		return
	}
	WriteJSON(w, http.StatusOK, session)
}

// CancelGeneration cancels an in-progress generation
// POST /api/v1/generation-sessions/:sessionId/cancel
func (h *ArtifactGenerationHandler) CancelGeneration(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Session ID is required")
		return
	}
	sessionID := pathParts[3]

	err := h.orchestrator.CancelGeneration(r.Context(), sessionID)
	if err != nil {
		WriteInternalError(w, "Failed to cancel generation")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// RestoreVersion restores an old version
// POST /api/v1/artifacts/:id/versions/:versionId/restore
func (h *ArtifactGenerationHandler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Artifact ID and version ID are required")
		return
	}
	// artifactID := pathParts[3]
	// versionID := pathParts[5]

	artifactID := pathParts[3]
	versionID := pathParts[5]
	if err := h.orchestrator.RestoreVersion(r.Context(), artifactID, versionID); err != nil {
		WriteInternalError(w, "Failed to restore version")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}
