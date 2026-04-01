package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/services"
)

// ArtifactsHandler manages CRUD for artifacts (CoreDoc with docType = dynamic_artifact)
type ArtifactsHandler struct {
	coreDocs     *repo.CoreDocsRepo
	storage      StorageClient
	bucket       string
	templates    *services.ArtifactTemplateService
	orchestrator *services.ArtifactOrchestratorService
}

func NewArtifactsHandler(core *repo.CoreDocsRepo) *ArtifactsHandler {
	return &ArtifactsHandler{coreDocs: core}
}

// WithStorage sets storage client and bucket for artifact handler
func (h *ArtifactsHandler) WithStorage(storage StorageClient, bucket string) *ArtifactsHandler {
	h.storage = storage
	h.bucket = bucket
	return h
}

func (h *ArtifactsHandler) WithTemplateService(service *services.ArtifactTemplateService) *ArtifactsHandler {
	h.templates = service
	return h
}

func (h *ArtifactsHandler) WithOrchestrator(orchestrator *services.ArtifactOrchestratorService) *ArtifactsHandler {
	h.orchestrator = orchestrator
	return h
}

// assembleArtifactMarkdown assembles markdown document from artifact sections
func assembleArtifactMarkdown(config *models.ArtifactConfig) string {
	if config == nil || len(config.Structure) == 0 {
		return ""
	}

	// Sort sections by order
	sections := make([]models.ArtifactSection, len(config.Structure))
	copy(sections, config.Structure)
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Order < sections[j].Order
	})

	var builder strings.Builder

	// Add each section
	for _, section := range sections {
		builder.WriteString(fmt.Sprintf("## %s\n\n", section.Title))

		if section.Content != nil && *section.Content != "" {
			builder.WriteString(*section.Content)
			builder.WriteString("\n\n")
		} else {
			builder.WriteString("*[Section not generated yet]*\n\n")
		}
	}

	return builder.String()
}

// SaveArtifactToStorage saves assembled artifact document to storage and updates StoragePath
// Implements ArtifactStorageSaver interface
func (h *ArtifactsHandler) SaveArtifactToStorage(ctx context.Context, tenantID, projectID string, artifact *models.CoreDoc) error {
	return h.saveArtifactToStorage(ctx, tenantID, projectID, artifact)
}

// saveArtifactToStorage saves assembled artifact document to storage and updates StoragePath
func (h *ArtifactsHandler) saveArtifactToStorage(ctx context.Context, tenantID, projectID string, artifact *models.CoreDoc) error {
	if h.storage == nil || h.bucket == "" {
		return nil // Storage not configured, skip
	}

	if artifact.ArtifactConfig == nil {
		return nil // Not an artifact
	}

	// Assemble markdown from sections
	markdownContent := assembleArtifactMarkdown(artifact.ArtifactConfig)
	if markdownContent == "" {
		return nil // No content to save
	}

	// Delete old file if exists
	if artifact.StoragePath != nil && strings.HasPrefix(*artifact.StoragePath, "gs://") {
		bucket, object := splitGsPath(*artifact.StoragePath)
		_ = h.storage.DeleteObject(ctx, bucket, object) // Ignore errors - old file might not exist
	}

	// Generate object path
	objectPath := fmt.Sprintf("%s/%s/%d-%s", tenantID, projectID, time.Now().UnixNano(), sanitizeFilename(artifact.Filename))

	// Upload to storage
	if err := h.storage.UploadBytes(ctx, h.bucket, objectPath, []byte(markdownContent), artifact.ContentType, map[string]string{
		"tenantId":  tenantID,
		"projectId": projectID,
		"filename":  artifact.Filename,
		"docType":   "dynamic_artifact",
	}); err != nil {
		return fmt.Errorf("failed to upload artifact to storage: %w", err)
	}

	// Update StoragePath
	storagePath := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
	artifact.StoragePath = &storagePath

	// Update ExtractedText for compatibility
	artifact.ExtractedText = &markdownContent

	// Update size
	size := int64(len([]byte(markdownContent)))
	artifact.SizeBytes = &size

	// Update artifact in database with new StoragePath
	if err := h.coreDocs.Upsert(ctx, artifact); err != nil {
		// Log but don't fail - storage is updated
	}

	return nil
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
			OrchestratorModel:  "gemini-fast",
			DefaultModel:       "gemini-fast",
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
	req.ID = id

	// Save assembled document to storage if sections have content
	if err := h.saveArtifactToStorage(r.Context(), auth.TenantID, projectID, &req); err != nil {
		// Log error but don't fail creation
		// Artifact is created, storage is optional
	}

	// Update artifact with StoragePath if it was set
	if req.StoragePath != nil {
		if err := h.coreDocs.Upsert(r.Context(), &req); err != nil {
			// Log but don't fail
		}
	}

	// Note: Dynamic artifacts are not indexed in Vertex AI Search (they are generated dynamically)
	// But they are automatically synced to BigQuery via coreDocs.Create/Upsert -> WithBigQuerySync

	WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// CreateArtifactFromTemplate creates a dynamic artifact from a template and optional graph context.
// POST /api/v1/projects/:projectId/artifacts/from-template
func (h *ArtifactsHandler) CreateArtifactFromTemplate(w http.ResponseWriter, r *http.Request) {
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.templates == nil {
		WriteInternalError(w, "Template service not configured")
		return
	}

	path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(path) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID required")
		return
	}
	projectID := path[3]

	var req struct {
		TemplateID          string                              `json:"templateId"`
		Filename            string                              `json:"filename"`
		UserRequest         *string                             `json:"userRequest,omitempty"`
		PreferredLanguage   *string                             `json:"preferredLanguage,omitempty"`
		ContextSources      *models.ArtifactContextSources      `json:"contextSources,omitempty"`
		GraphSources        []models.ArtifactGraphContextSource `json:"graphSources,omitempty"`
		SourceInteractionID *string                             `json:"sourceInteractionId,omitempty"`
		AutoGenerate        bool                                `json:"autoGenerate"`
		GenerationMode      string                              `json:"generationMode,omitempty"`
		MaxOutputTokens     *int                                `json:"maxOutputTokens,omitempty"`
		ThinkingBudget      *int                                `json:"thinkingBudget,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}
	if strings.TrimSpace(req.TemplateID) == "" {
		WriteBadRequest(w, "MISSING_FIELD", "Template ID is required")
		return
	}

	template, err := h.templates.GetTemplate(r.Context(), strings.TrimSpace(req.TemplateID), &auth.TenantID)
	if err != nil || template == nil {
		WriteNotFound(w, "Template not found")
		return
	}

	contextSources := models.ArtifactContextSources{}
	if req.ContextSources != nil {
		contextSources = *req.ContextSources
	}
	if len(req.GraphSources) > 0 {
		contextSources.GraphSources = req.GraphSources
	}

	if len(template.GraphSourceKinds) > 0 {
		if !templateMatchesGraphSources(template, contextSources.GraphSources) {
			WriteBadRequest(w, "TEMPLATE_CONTEXT_MISMATCH", "Template is not compatible with the provided graph sources")
			return
		}
	}

	preferredLanguage := "en"
	if req.PreferredLanguage != nil && strings.TrimSpace(*req.PreferredLanguage) != "" {
		preferredLanguage = strings.TrimSpace(*req.PreferredLanguage)
	}
	systemPrompt := strings.TrimSpace(firstTemplatePrompt(template.SystemPrompts, preferredLanguage))
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant."
	}

	defaultModel := strings.TrimSpace(template.RecommendedSectionModel)
	if defaultModel == "" {
		defaultModel = "gemini-fast"
	}
	orchestratorModel := strings.TrimSpace(template.RecommendedOrchestratorModel)
	if orchestratorModel == "" {
		orchestratorModel = defaultModel
	}

	structure := make([]models.ArtifactSection, 0, len(template.DefaultStructure))
	for _, section := range template.DefaultStructure {
		description := strings.TrimSpace(section.Description)
		prompt := strings.TrimSpace(section.PromptTemplate)
		structure = append(structure, models.ArtifactSection{
			ID:                section.ID,
			Title:             section.Title,
			Order:             section.Order,
			Prompt:            prompt,
			Description:       stringPtrIfNotEmpty(description),
			Required:          section.Required,
			Editable:          section.Editable,
			AutoUpdateEnabled: false,
			DependsOn:         append([]string(nil), section.DependsOn...),
		})
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = strings.TrimSpace(template.Name)
	}
	if filename == "" {
		filename = "Dynamic Document"
	}

	now := time.Now().UTC()
	docType := "dynamic_artifact"
	artifact := &models.CoreDoc{
		TenantID:    auth.TenantID,
		ProjectID:   projectID,
		Filename:    filename,
		ContentType: "text/markdown",
		DocType:     &docType,
		ArtifactConfig: &models.ArtifactConfig{
			Format:            firstNonEmptyValue(template.RecommendedFormat, "report"),
			Focus:             firstNonEmptyValue(template.RecommendedFocus, "executive"),
			OrchestratorModel: orchestratorModel,
			DefaultModel:      defaultModel,
			ReviewPrompt:      "Review and refine for clarity, consistency, and completeness.",
			Structure:         structure,
			ContextSources:    contextSources,
			AutoUpdateEnabled: false,
			VersionsRetained:  10,
			Versions:          []models.ArtifactVersion{},
			StructureGeneration: &models.StructureGenerationConfig{
				Mode:           "template",
				SourceTemplate: stringPtrIfNotEmpty(template.ID),
				GeneratedAt:    &now,
				UserModified:   false,
			},
			GenerationSettings: models.GenerationSettings{
				PreferredLanguage: preferredLanguage,
				SystemPrompt:      systemPrompt,
				MaxOutputTokens:   req.MaxOutputTokens,
				ThinkingBudget:    req.ThinkingBudget,
			},
			UserRequest:         req.UserRequest,
			SourceInteractionID: req.SourceInteractionID,
		},
	}

	id, err := h.coreDocs.Create(r.Context(), artifact)
	if err != nil {
		WriteInternalError(w, "Failed to create artifact")
		return
	}
	artifact.ID = id

	var session *models.GenerationSession
	var generationError string
	if req.AutoGenerate && h.orchestrator != nil {
		mode := strings.TrimSpace(req.GenerationMode)
		if mode != "interactive" {
			mode = "auto"
		}
		session, err = h.orchestrator.StartGeneration(r.Context(), services.StartGenerationRequest{
			ArtifactID: id,
			Mode:       mode,
			TenantID:   auth.TenantID,
			ProjectID:  projectID,
		})
		if err != nil {
			generationError = err.Error()
		}
	}

	WriteJSON(w, http.StatusCreated, map[string]any{
		"artifact":        artifact,
		"session":         session,
		"generationError": stringPtrIfNotEmpty(generationError),
	})
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
	items, err := h.coreDocs.ListByProject(r.Context(), auth.TenantID, projectID, 0, nil)
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

func stringPtrIfNotEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func firstTemplatePrompt(prompts map[string]string, preferredLanguage string) string {
	if len(prompts) == 0 {
		return ""
	}
	if prompt := strings.TrimSpace(prompts[strings.ToLower(strings.TrimSpace(preferredLanguage))]); prompt != "" {
		return prompt
	}
	if prompt := strings.TrimSpace(prompts["en"]); prompt != "" {
		return prompt
	}
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt) != "" {
			return strings.TrimSpace(prompt)
		}
	}
	return ""
}

func templateMatchesGraphSources(template *models.ArtifactTemplate, sources []models.ArtifactGraphContextSource) bool {
	if template == nil {
		return false
	}
	if len(template.GraphSourceKinds) == 0 {
		return true
	}
	if len(sources) == 0 {
		return false
	}
	for _, source := range sources {
		kind := strings.TrimSpace(source.Kind)
		if kind == "" {
			continue
		}
		for _, allowed := range template.GraphSourceKinds {
			if strings.EqualFold(strings.TrimSpace(allowed), kind) {
				return true
			}
		}
	}
	return false
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
		// Reassemble and save to storage when config changes
		if err := h.saveArtifactToStorage(r.Context(), auth.TenantID, existing.ProjectID, existing); err != nil {
			// Log error but don't fail update
		}
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
