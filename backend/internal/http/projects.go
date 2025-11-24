package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ProjectsHandler handles project-related HTTP requests
type ProjectsHandler struct {
	repo         *repo.ProjectsRepository
	logger       *log.Logger
	dataSources  *repo.DataSourcesRepository
	profiles     *repo.SourceProfilesRepository
	interactions *repo.InteractionsRepository
	coreDocs     *repo.CoreDocsRepo
	// meta analysis repos for cascade
	metaEvents  *repo.MetaAnalysesRepository
	metaAggProj *repo.MetaAggregateProjectsRepository
	metaAggUser *repo.MetaAggregateUsersRepository
	// storage for uploads
	storage StorageClient
	bucket  string
	pub     *realtime.Publisher
	// ai executor for analysis tasks
	exec *ai.AITaskExecutor
	// RAG dependencies for CoreDoc indexing
	docsRepo   *repo.DocumentsRepository
	chunksRepo *repo.ChunksRepository
	emb        ai.EmbeddingProvider
	// Vertex AI Search client for checking document indexing status
	vertexSearchClient *ai.VertexAISearchClient
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

// WithMeta wires meta-analysis repositories for cascade deletes
func (h *ProjectsHandler) WithMeta(events *repo.MetaAnalysesRepository, aggProj *repo.MetaAggregateProjectsRepository, aggUser *repo.MetaAggregateUsersRepository) *ProjectsHandler {
	h.metaEvents = events
	h.metaAggProj = aggProj
	h.metaAggUser = aggUser
	return h
}

// WithCoreDocs binds CoreDocs repository
func (h *ProjectsHandler) WithCoreDocs(core *repo.CoreDocsRepo) *ProjectsHandler {
	h.coreDocs = core
	return h
}

// WithStorage wires storage client and bucket for uploads
func (h *ProjectsHandler) WithStorage(client StorageClient, bucket string) *ProjectsHandler {
	h.storage = client
	h.bucket = bucket
	return h
}

// WithPublisher wires realtime publisher for streaming events
func (h *ProjectsHandler) WithPublisher(pub *realtime.Publisher) *ProjectsHandler {
	h.pub = pub
	return h
}

// WithAIExecutor wires AI TaskExecutor for analysis tasks
func (h *ProjectsHandler) WithAIExecutor(exec *ai.AITaskExecutor) *ProjectsHandler {
	h.exec = exec
	return h
}

// WithRAG wires RAG repositories and embedding provider for CoreDoc indexing
func (h *ProjectsHandler) WithRAG(docs *repo.DocumentsRepository, chunks *repo.ChunksRepository, emb ai.EmbeddingProvider) *ProjectsHandler {
	h.docsRepo = docs
	h.chunksRepo = chunks
	h.emb = emb
	return h
}

// WithVertexSearch wires Vertex AI Search client for checking document indexing status
func (h *ProjectsHandler) WithVertexSearch(client *ai.VertexAISearchClient) *ProjectsHandler {
	h.vertexSearchClient = client
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
	// Cascade: delete documents and chunks for sources tied to this project
	if h.docsRepo != nil && h.dataSources != nil {
		sources, err := h.dataSources.ListByProject(r.Context(), authInfo.TenantID, projectID)
		if err == nil {
			for _, source := range sources {
				// List documents first to delete chunks
				docs, _ := h.docsRepo.ListBySource(r.Context(), authInfo.TenantID, source.ID, 0)
				// Delete chunks for each document
				if h.chunksRepo != nil {
					for _, doc := range docs {
						_ = h.chunksRepo.DeleteByDoc(r.Context(), authInfo.TenantID, doc.ID)
					}
				}
				// Delete all documents for this source (which will also delete from GCS)
				if err := h.docsRepo.DeleteBySource(r.Context(), authInfo.TenantID, source.ID); err != nil {
					h.logger.Printf("Warning: cascade delete documents for source %s: %v", source.ID, err)
				}
			}
		}
	}
	// Cascade: delete meta analysis events and project aggregates, recompute user aggregate
	if h.metaEvents != nil {
		if err := h.metaEvents.DeleteByProject(r.Context(), projectID); err != nil {
			h.logger.Printf("Warning: cascade delete meta events for project %s: %v", projectID, err)
		}
	}
	if h.metaAggProj != nil {
		if err := h.metaAggProj.Delete(r.Context(), projectID); err != nil {
			h.logger.Printf("Warning: delete meta aggregate project %s: %v", projectID, err)
		}
	}
	// Best-effort: recompute user aggregate by scanning remaining events
	if h.metaAggUser != nil && h.metaEvents != nil {
		go func(tenantID, userID string) {
			ctx := context.Background()
			events, err := h.metaEvents.ListByUser(ctx, tenantID, userID, 500)
			if err != nil {
				return
			}
			if len(events) == 0 {
				_ = h.metaAggUser.Delete(ctx, tenantID, userID)
				return
			}
			var s int
			var lead, focus, structure float64
			for _, e := range events {
				s++
				lead += e.Common.LeadershipIndex
				focus += e.Common.FocusRetention
				structure += e.Common.StructureScore
			}
			if s == 0 {
				return
			}
			ua := &models.MetaAggregateUser{
				TenantID:        tenantID,
				UserID:          userID,
				Version:         "coach.v1",
				Samples:         s,
				LeadershipIndex: lead / float64(s),
				FocusRetention:  focus / float64(s),
				StructureScore:  structure / float64(s),
			}
			ua.Effectiveness = 0.5*ua.LeadershipIndex + 0.25*ua.FocusRetention + 0.25*ua.StructureScore
			_ = h.metaAggUser.Upsert(ctx, ua)
		}(project.TenantID, project.OwnerUserID)
	}
	// Cascade: delete core docs for this project (best-effort)
	if h.coreDocs != nil {
		if docs, err := h.coreDocs.ListByProject(r.Context(), authInfo.TenantID, projectID, 0, nil); err == nil {
			for _, d := range docs {
				_, _ = h.coreDocs.Delete(r.Context(), d.ID)
			}
		}
	}

	h.logger.Printf("Successfully deleted project %s", projectID)
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Project deleted successfully",
	})
}

// ---- Core Docs sub-routes ----

// CreateCoreDocRequest is minimal metadata; upload flow handled separately (Storage signed URL or multipart in future)
type CreateCoreDocRequest struct {
	Filename       string                 `json:"filename"`
	ContentType    string                 `json:"contentType"`
	Description    *string                `json:"description,omitempty"`
	SizeBytes      *int64                 `json:"sizeBytes,omitempty"`
	StoragePath    *string                `json:"storagePath,omitempty"`
	External       *models.ExternalDocRef `json:"external,omitempty"`
	DocType        *string                `json:"docType,omitempty"`
	ArtifactConfig *models.ArtifactConfig `json:"artifactConfig,omitempty"`
}

// CreateProjectDoc handles POST /api/v1/projects/{id}/docs
func (h *ProjectsHandler) CreateProjectDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs repository is not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	// Extract projectId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 { // /api/v1/projects/{id}/docs
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	// Validate project ownership
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	var req CreateCoreDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}
	if req.Filename == "" || req.ContentType == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "filename and contentType are required")
		return
	}

	now := time.Now().UTC()
	doc := &models.CoreDoc{
		TenantID:       authInfo.TenantID,
		ProjectID:      projectID,
		Filename:       req.Filename,
		ContentType:    req.ContentType,
		Description:    req.Description,
		SizeBytes:      req.SizeBytes,
		StoragePath:    req.StoragePath,
		External:       req.External,
		DocType:        req.DocType,
		ArtifactConfig: req.ArtifactConfig,
		CreatedBy:      &authInfo.UID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	id, err := h.coreDocs.Create(r.Context(), doc)
	if err != nil {
		h.logger.Printf("CreateProjectDoc error: %v", err)
		WriteInternalError(w, "Failed to create document")
		return
	}
	doc.ID = id
	// Index CoreDoc for vector search if ExtractedText is available
	if doc.ExtractedText != nil && h.docsRepo != nil && h.chunksRepo != nil && h.emb != nil {
		if err := h.indexCoreDoc(r.Context(), authInfo.TenantID, doc); err != nil {
			h.logger.Printf("Warning: failed to index CoreDoc %s: %v", doc.ID, err)
		}
	}
	WriteJSON(w, http.StatusCreated, doc)
}

// CreateProjectDocFromText handles POST /api/v1/projects/{id}/docs/from-text
// Allows creating a CoreDoc directly from provided text content (e.g., LLM export),
// without a separate upload step.
func (h *ProjectsHandler) CreateProjectDocFromText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs repository is not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	// Extract projectId from path
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 { // /api/v1/projects/{id}/docs/from-text
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]
	// Validate project ownership
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	var req struct {
		Filename    string  `json:"filename"`
		ContentType string  `json:"contentType"`
		Description *string `json:"description,omitempty"`
		Text        string  `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}
	if req.Filename == "" || req.ContentType == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "filename and contentType are required")
		return
	}

	// Persist the text payload optionally to storage, to enable downloads
	var storagePath *string
	if h.storage != nil && h.bucket != "" {
		objectPath := fmt.Sprintf("%s/%s/%d-%s", authInfo.TenantID, projectID, time.Now().UnixNano(), sanitizeFilename(req.Filename))
		if err := h.storage.UploadBytes(r.Context(), h.bucket, objectPath, []byte(req.Text), req.ContentType, map[string]string{
			"tenantId":  authInfo.TenantID,
			"projectId": projectID,
			"filename":  req.Filename,
		}); err == nil {
			p := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
			storagePath = &p
		}
	}

	now := time.Now().UTC()
	size := int64(len([]byte(req.Text)))
	status := "parsed"
	doc := &models.CoreDoc{
		TenantID:      authInfo.TenantID,
		ProjectID:     projectID,
		Filename:      req.Filename,
		ContentType:   req.ContentType,
		Description:   req.Description,
		SizeBytes:     &size,
		StoragePath:   storagePath,
		ExtractedText: &req.Text,
		ParseStatus:   &status,
		ParseMeta:     map[string]any{"createdFrom": "from-text", "updatedAt": now.Format(time.RFC3339)},
		CreatedBy:     &authInfo.UID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	id, err := h.coreDocs.Create(r.Context(), doc)
	if err != nil {
		WriteInternalError(w, "Failed to create document")
		return
	}
	doc.ID = id
	if h.pub != nil {
		// Notify UI about new document
		_ = h.pub.PublishJSON(r.Context(), realtime.EventDocProcess, authInfo.TenantID, map[string]any{
			"projectId": projectID,
			"docId":     doc.ID,
			"status":    "uploaded",
			"ts":        time.Now().UnixMilli(),
		})
		_ = h.pub.PublishJSON(r.Context(), realtime.EventDocProcess, authInfo.TenantID, map[string]any{
			"projectId": projectID,
			"docId":     doc.ID,
			"status":    "parsed",
			"chars":     len(req.Text),
			"ts":        time.Now().UnixMilli(),
		})
	}
	// Index CoreDoc for vector search
	if h.docsRepo != nil && h.chunksRepo != nil && h.emb != nil {
		if err := h.indexCoreDoc(r.Context(), authInfo.TenantID, doc); err != nil {
			h.logger.Printf("Warning: failed to index CoreDoc %s: %v", doc.ID, err)
		}
	}
	WriteJSON(w, http.StatusCreated, doc)
}

// UpdateProjectDoc handles PATCH /api/v1/projects/{id}/docs/{docId}
// Allows renaming and updating description of a CoreDoc.
func (h *ProjectsHandler) UpdateProjectDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs repository is not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 { // /api/v1/projects/{id}/docs/{docId}
		WriteBadRequest(w, "INVALID_PATH", "Project and document ID are required")
		return
	}
	projectID, docID := parts[3], parts[5]
	// Access check via project
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil || cd.ProjectID != projectID || cd.TenantID != authInfo.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}
	var patch struct {
		Filename    *string `json:"filename,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Invalid request body")
		return
	}
	if patch.Filename != nil && strings.TrimSpace(*patch.Filename) != "" {
		cd.Filename = *patch.Filename
	}
	if patch.Description != nil {
		cd.Description = patch.Description
	}
	cd.UpdatedAt = time.Now().UTC()
	if err := h.coreDocs.Upsert(r.Context(), cd); err != nil {
		WriteInternalError(w, "Failed to update document")
		return
	}
	WriteJSON(w, http.StatusOK, cd)
}

// ListProjectDocs handles GET /api/v1/projects/{id}/docs
func (h *ProjectsHandler) ListProjectDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs repository is not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 { // /api/v1/projects/{id}/docs
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	// Parse excludeDocumentType query parameter (e.g., ?excludeDocumentType=post)
	var excludeDocumentType *string
	if excludeType := r.URL.Query().Get("excludeDocumentType"); excludeType != "" {
		excludeDocumentType = &excludeType
	}
	docs, err := h.coreDocs.ListByProject(r.Context(), authInfo.TenantID, projectID, 0, excludeDocumentType)
	if err != nil {
		h.logger.Printf("ListProjectDocs error: %v", err)
		WriteInternalError(w, "Failed to list documents")
		return
	}
	WriteJSON(w, http.StatusOK, docs)
}

// UploadProjectDoc handles POST /api/v1/projects/{id}/docs/upload (multipart/form-data)
func (h *ProjectsHandler) UploadProjectDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.coreDocs == nil || h.storage == nil || h.bucket == "" {
		WriteInternalError(w, "Uploads are not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	// Extract projectId
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 6 { // /api/v1/projects/{id}/docs/upload
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Parse multipart (limit 30MB)
	const maxUpload = 30 << 20
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		WriteBadRequest(w, "INVALID_MULTIPART", "Failed to parse multipart form")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		WriteBadRequest(w, "FILE_REQUIRED", "file field is required")
		return
	}
	defer file.Close()

	// Read into memory with size guard
	var buf []byte
	{
		// 32MB cap
		limited := &io.LimitedReader{R: file, N: 32 << 20}
		b, err := io.ReadAll(limited)
		if err != nil {
			WriteInternalError(w, "Failed to read file")
			return
		}
		if limited.N == 0 {
			WriteBadRequest(w, "FILE_TOO_LARGE", "File exceeds 32MB limit")
			return
		}
		buf = b
	}

	// Detect content type
	contentType := http.DetectContentType(buf[:min(512, len(buf))])
	if ct := header.Header.Get("Content-Type"); ct != "" {
		contentType = ct
	}
	// Normalize common text types
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".md":
		contentType = "text/markdown"
	case ".txt":
		contentType = "text/plain"
	case ".csv":
		contentType = "text/csv"
	case ".tsv":
		contentType = "text/tab-separated-values"
	}

	objectPath := fmt.Sprintf("%s/%s/%d-%s", authInfo.TenantID, projectID, time.Now().UnixNano(), sanitizeFilename(header.Filename))
	storagePath := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)

	if err := h.storage.UploadBytes(r.Context(), h.bucket, objectPath, buf, contentType, map[string]string{
		"tenantId":  authInfo.TenantID,
		"projectId": projectID,
		"filename":  header.Filename,
	}); err != nil {
		WriteInternalError(w, "Failed to upload to storage")
		return
	}

	size := int64(len(buf))
	desc := r.FormValue("description")
	var descPtr *string
	if desc != "" {
		descPtr = &desc
	}

	now := time.Now().UTC()
	doc := &models.CoreDoc{
		TenantID:    authInfo.TenantID,
		ProjectID:   projectID,
		Filename:    header.Filename,
		ContentType: contentType,
		Description: descPtr,
		SizeBytes:   &size,
		StoragePath: &storagePath,
		CreatedBy:   &authInfo.UID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	id, err := h.coreDocs.Create(r.Context(), doc)
	if err != nil {
		WriteInternalError(w, "Failed to save document")
		return
	}
	doc.ID = id
	// Optional: kick off parse event for frontend stream
	if h.pub != nil {
		_ = h.pub.PublishJSON(r.Context(), realtime.EventDocProcess, authInfo.TenantID, map[string]any{
			"projectId": projectID,
			"docId":     doc.ID,
			"status":    "uploaded",
			"ts":        time.Now().UnixMilli(),
		})
	}
	// Autoparse
	go func(tenant, pid, did string) {
		// reload fresh doc to include ID
		if d, err := h.coreDocs.Get(context.Background(), did); err == nil {
			_ = h.parseAndPersist(context.Background(), tenant, pid, d)
		}
	}(authInfo.TenantID, projectID, doc.ID)
	WriteJSON(w, http.StatusCreated, doc)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeFilename(name string) string {
	// very basic cleanup; keep ASCII letters, digits, dash, underscore, dot
	b := make([]rune, 0, len(name))
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b = append(b, r)
		} else {
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "file"
	}
	return string(b)
}

// assembleArtifactMarkdownFromConfig assembles markdown document from artifact sections
// This is a copy of the function from artifacts.go to avoid circular dependencies
func assembleArtifactMarkdownFromConfig(config *models.ArtifactConfig) string {
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

// StorageClient abstracts cloud storage upload/delete for easier testing
type StorageClient interface {
	UploadBytes(ctx context.Context, bucket, object string, data []byte, contentType string, metadata map[string]string) error
	DeleteObject(ctx context.Context, bucket, object string) error
	ReadObject(ctx context.Context, bucket, object string) ([]byte, error)
}

// ParseProjectDoc handles POST /api/v1/projects/{id}/docs/{docId}/parse
// It fetches the object from storage (if present), routes by MIME and extracts text into CoreDoc.ExtractedText
func (h *ProjectsHandler) ParseProjectDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.coreDocs == nil || h.storage == nil || h.bucket == "" {
		WriteInternalError(w, "Parsing not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/v1/projects/{id}/docs/{docId}/parse
	if len(parts) < 7 {
		WriteBadRequest(w, "INVALID_PATH", "Project and document ID are required")
		return
	}
	projectID, docID := parts[3], parts[5]
	// access check via project
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	// fetch doc
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil || cd.ProjectID != projectID || cd.TenantID != authInfo.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}
	if cd.External != nil {
		WriteBadRequest(w, "EXTERNAL_UNSUPPORTED", "External documents parsing is not implemented yet")
		return
	}
	if cd.StoragePath == nil {
		WriteBadRequest(w, "NO_STORAGE", "Document has no storagePath")
		return
	}
	if err := h.parseAndPersist(r.Context(), authInfo.TenantID, projectID, cd); err != nil {
		WriteInternalError(w, "Failed to parse document")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"id": cd.ID, "status": "parsed"})
}

// CheckDocumentIndexStatus handles GET /api/v1/projects/{id}/docs/{docId}/index-status
// Checks if document is indexed in Vertex AI Search
func (h *ProjectsHandler) CheckDocumentIndexStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs repository is not configured")
		return
	}
	if h.vertexSearchClient == nil || !h.vertexSearchClient.IsConfigured() {
		// If Vertex Search is not configured, return not indexed
		WriteJSON(w, http.StatusOK, map[string]any{"indexed": false, "reason": "vertex_search_not_configured"})
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// /api/v1/projects/{id}/docs/{docId}/index-status
	if len(parts) < 7 {
		WriteBadRequest(w, "INVALID_PATH", "Project and document ID are required")
		return
	}
	projectID, docID := parts[3], parts[5]
	// Verify project access
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	// Get the document
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil || cd.ProjectID != projectID || cd.TenantID != authInfo.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}
	// Check if document has storagePath (required for indexing)
	if cd.StoragePath == nil || *cd.StoragePath == "" {
		WriteJSON(w, http.StatusOK, map[string]any{"indexed": false, "reason": "no_storage_path"})
		return
	}

	// Try to find corresponding Document in rag_documents (which is actually indexed in Vertex AI Search)
	// Documents are created when CoreDoc is parsed and stored in RAG_DOCUMENTS_BUCKET
	if h.docsRepo != nil {
		ragDoc, err := h.docsRepo.GetByCoreDocID(r.Context(), authInfo.TenantID, docID)
		if err == nil && ragDoc != nil {
			// Check if document has VertexSearchIndexedAt timestamp - this is the source of truth
			if ragDoc.VertexSearchIndexedAt != nil && !ragDoc.VertexSearchIndexedAt.IsZero() {
				WriteJSON(w, http.StatusOK, map[string]any{"indexed": true, "indexedAt": ragDoc.VertexSearchIndexedAt})
				return
			}
			// If no timestamp, document is not indexed yet
			WriteJSON(w, http.StatusOK, map[string]any{"indexed": false, "reason": "not_indexed"})
			return
		}
	}

	// If no rag document found, document is not indexed
	WriteJSON(w, http.StatusOK, map[string]any{"indexed": false, "reason": "no_rag_document"})
}

// DownloadProjectDoc handles GET /api/v1/projects/{id}/docs/{docId}/download
func (h *ProjectsHandler) DownloadProjectDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if h.coreDocs == nil || h.storage == nil {
		WriteInternalError(w, "Downloads not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 {
		WriteBadRequest(w, "INVALID_PATH", "Project and document ID are required")
		return
	}
	projectID, docID := parts[3], parts[5]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil || cd.ProjectID != projectID || cd.TenantID != authInfo.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}

	// Handle dynamic artifacts: assemble from sections if no storage
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" && cd.ArtifactConfig != nil {
		var data []byte
		if cd.StoragePath != nil && strings.HasPrefix(*cd.StoragePath, "gs://") && h.storage != nil {
			// Try to read from storage first
			bucket, object := splitGsPath(*cd.StoragePath)
			var readErr error
			data, readErr = h.storage.ReadObject(r.Context(), bucket, object)
			if readErr != nil {
				// Fallback: assemble from sections
				markdownContent := assembleArtifactMarkdownFromConfig(cd.ArtifactConfig)
				data = []byte(markdownContent)
			}
		} else {
			// No storage, assemble from sections
			markdownContent := assembleArtifactMarkdownFromConfig(cd.ArtifactConfig)
			data = []byte(markdownContent)
		}

		if cd.ContentType != "" {
			w.Header().Set("Content-Type", cd.ContentType)
		} else {
			w.Header().Set("Content-Type", "text/markdown")
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sanitizeFilename(cd.Filename)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// Regular documents: require storage
	if cd.StoragePath == nil || !strings.HasPrefix(*cd.StoragePath, "gs://") {
		WriteBadRequest(w, "NO_STORAGE", "Document has no storagePath")
		return
	}
	bucket, object := splitGsPath(*cd.StoragePath)
	data, err := h.storage.ReadObject(r.Context(), bucket, object)
	if err != nil {
		WriteInternalError(w, "Failed to read object")
		return
	}
	if cd.ContentType != "" {
		w.Header().Set("Content-Type", cd.ContentType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sanitizeFilename(cd.Filename)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ExportDocToDocx handles POST /api/v1/projects/{id}/docs/{docId}/export?format=docx
// Converts markdown content to DOCX using Pandoc
func (h *ProjectsHandler) ExportDocToDocx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	// Parse path: /api/v1/projects/{id}/docs/{docId}/export
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 {
		WriteBadRequest(w, "INVALID_PATH", "Project and document ID are required")
		return
	}
	projectID, docID := parts[3], parts[5]

	// Verify project access
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Get the document
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil || cd.ProjectID != projectID || cd.TenantID != authInfo.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}

	// Get markdown content
	var markdownContent string
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" && cd.ArtifactConfig != nil {
		// For artifacts, assemble from sections
		markdownContent = assembleArtifactMarkdownFromConfig(cd.ArtifactConfig)
		if markdownContent == "" {
			WriteBadRequest(w, "NO_CONTENT", "Artifact has no content")
			return
		}
	} else if cd.ExtractedText != nil && *cd.ExtractedText != "" {
		markdownContent = *cd.ExtractedText
	} else if cd.StoragePath != nil && strings.HasPrefix(*cd.StoragePath, "gs://") && h.storage != nil {
		// Try to read from storage
		bucket, object := splitGsPath(*cd.StoragePath)
		data, err := h.storage.ReadObject(r.Context(), bucket, object)
		if err != nil {
			WriteInternalError(w, "Failed to read document content")
			return
		}
		markdownContent = string(data)
	} else {
		WriteBadRequest(w, "NO_CONTENT", "Document has no readable content")
		return
	}

	// Convert markdown to DOCX using Pandoc
	docxData, err := convertMarkdownToDocx(markdownContent)
	if err != nil {
		h.logger.Printf("Failed to convert markdown to DOCX: %v", err)
		WriteInternalError(w, "Failed to convert document to DOCX")
		return
	}

	// Determine output filename
	baseName := strings.TrimSuffix(cd.Filename, filepath.Ext(cd.Filename))
	if baseName == "" {
		baseName = "exported"
	}
	outputFilename := baseName + ".docx"

	// Set response headers
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", sanitizeFilename(outputFilename)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(docxData)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(docxData)
}

// convertMarkdownToDocx converts markdown content to DOCX using Pandoc
func convertMarkdownToDocx(markdown string) ([]byte, error) {
	// Create temporary files for input and output
	tmpMD, err := os.CreateTemp("", "export-*.md")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp markdown file: %w", err)
	}
	defer os.Remove(tmpMD.Name())
	defer tmpMD.Close()

	tmpDOCX, err := os.CreateTemp("", "export-*.docx")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp docx file: %w", err)
	}
	defer os.Remove(tmpDOCX.Name())

	// Write markdown to temp file
	if _, err := tmpMD.WriteString(markdown); err != nil {
		return nil, fmt.Errorf("failed to write markdown: %w", err)
	}
	if err := tmpMD.Close(); err != nil {
		return nil, fmt.Errorf("failed to close markdown file: %w", err)
	}

	// Run Pandoc to convert markdown to DOCX
	// Using --wrap=none to preserve line breaks, --standalone for proper document structure
	cmd := exec.Command("pandoc",
		tmpMD.Name(),
		"-o", tmpDOCX.Name(),
		"--wrap=none",
		"--standalone",
		"--from=markdown",
		"--to=docx")

	var out bytes.Buffer
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pandoc execution failed: %w, stderr: %s", err, out.String())
	}

	// Read the generated DOCX file
	docxData, err := os.ReadFile(tmpDOCX.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to read generated DOCX: %w", err)
	}

	return docxData, nil
}

// ---- Entities analysis ----

type analyzeEntitiesRequest struct {
	Prompt string `json:"prompt,omitempty"`
}

// AnalyzeEntities handles POST /api/v1/projects/{id}/analyze/entities (async)
func (h *ProjectsHandler) AnalyzeEntities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.exec == nil || h.coreDocs == nil || h.interactions == nil {
		WriteInternalError(w, "Entities analysis not configured")
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 { // /api/v1/projects/{id}/analyze/entities
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	// Validate project
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var req analyzeEntitiesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Fire-and-forget worker with bounded timeout
	go func(tenantID, pid, userPrompt, userID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{"projectId": pid, "kind": "entities", "status": "started", "ts": time.Now().UnixMilli()})
		}

		// Publish WS: analyzing start
		if h.pub != nil {
			payload := map[string]any{
				"projectId": pid,
				"docId":     "entities.json",
				"status":    "parsing",
				"ts":        time.Now().UnixMilli(),
			}
			log.Printf("[AnalyzeEntities] Publishing parsing status: %+v", payload)
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, payload)
		}

		// Load interactions (limit window size)
		items, _ := h.interactions.ListByProject(ctx, pid)
		// Build compact transcript
		builder := &strings.Builder{}
		maxChars := 16000
		for _, it := range items {
			if it == nil || (it.Kind != "user" && it.Kind != "assistant" && it.Kind != "system") {
				continue
			}
			line := fmt.Sprintf("- %s: %s\n", it.Kind, it.Content)
			if builder.Len()+len(line) > maxChars {
				break
			}
			builder.WriteString(line)
		}
		transcript := builder.String()

		// Load previous entities CoreDoc (latest)
		var prevJSON string
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "entities.json") && d.ExtractedText != nil {
					prevJSON = *d.ExtractedText
					break
				}
			}
		}

		// Compose prompt
		instructions := `You are extracting unique, concretely identifiable entities from the chat transcript.
        Return STRICT JSON with shape: { "entities": [ { "id": string, "name": string, "type": "Person"|"Organization"|"Location"|"Product"|"Document"|"Facility"|"Other", "aliases"?: string[], "description"?: string } ] }.
        Rules:
        - ONLY include concrete, named items (no abstract categories). Examples of ALLOWED vs REJECTED:
          • ALLOWED: "GS1" (Organization), "OKTRU" (Organization), "OKTRU 01.11.11.110" (Document), "GTIN 04891234567890" (Document), "SPK" (Organization), "Agromarket" (Organization), "Contract No. 45/2024" (Document), "NK RK 14-2024" (Document), "Atyrau Region" (Location).
          • REJECTED: "technical regulation" (too generic), "open databases" (too generic), "contracts" (no identifier), "buyers", "sellers", technologies like "chat-bot", "OCR" (not entities).
        - Names must be normalized to the official form (for Russian, nominative case) and without quotes.
        - If user prompt provides scope, obey it. If empty, find all relevant entities.
        - Do NOT include entities previously extracted (provided below) or those marked as excluded:true.
        - Use description only if certain from transcript, otherwise from public well-known facts succinctly.
        `
		composed := instructions + "\nUSER_PROMPT:\n" + strings.TrimSpace(userPrompt) + "\nTRANSCRIPT:\n" + transcript + "\nPREVIOUS_ENTITIES_JSON:\n" + strings.TrimSpace(prevJSON)

		authInfo := GetAuthInfo(r.Context())
		task := &ai.AITask{
			TaskType:     ai.TaskTypeContentAnalysis,
			InputContent: composed,
			AIRequirements: ai.AIRequirements{
				MinContextWindow:         8000,
				MaxOutputSize:            4096,
				MinCapabilityScore:       6,
				TextGenerationCapability: true,
			},
			Verifier:        ai.NewMapVerifier([]string{"entities"}, nil),
			EnableTelemetry: true,
			TenantID:        tenantID,
			UserID: func() string {
				if authInfo != nil {
					return authInfo.UID
				}
				return ""
			}(),
			Project: pid,
		}
		parsed, err := h.exec.ExecuteStructuredTask(ctx, task)
		if err != nil {
			if h.pub != nil {
				payload := map[string]any{"projectId": pid, "docId": "entities.json", "status": "failed", "error": err.Error(), "ts": time.Now().UnixMilli()}
				log.Printf("[AnalyzeEntities] Publishing failed status: %+v", payload)
				_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, payload)
				_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
					"projectId": pid,
					"kind":      "entities",
					"status":    "failed",
					"error":     err.Error(),
					"elapsedMs": time.Since(started).Milliseconds(),
					"ts":        time.Now().UnixMilli(),
				})
			}
			return
		}
		// Merge with previous entities (dedup + preserve excluded)
		// Normalize key: lower(name)|type
		normKey := func(name, typ string) string {
			n := strings.TrimSpace(strings.ToLower(name))
			t := strings.TrimSpace(strings.ToLower(typ))
			return n + "|" + t
		}
		type entity struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Type        string   `json:"type"`
			Aliases     []string `json:"aliases,omitempty"`
			Description string   `json:"description,omitempty"`
			Excluded    bool     `json:"excluded,omitempty"`
		}
		var prev struct {
			Entities []entity `json:"entities"`
		}
		if prevJSON != "" {
			_ = json.Unmarshal([]byte(prevJSON), &prev)
		}
		var cur struct {
			Entities []entity `json:"entities"`
		}
		if bb, e := json.Marshal(parsed); e == nil {
			_ = json.Unmarshal(bb, &cur)
		}
		prevMap := map[string]entity{}
		excluded := map[string]bool{}
		for _, e := range prev.Entities {
			k := normKey(e.Name, e.Type)
			prevMap[k] = e
			if e.Excluded {
				excluded[k] = true
			}
		}
		merged := make([]entity, 0, len(prev.Entities)+len(cur.Entities))
		merged = append(merged, prev.Entities...)
		for _, e := range cur.Entities {
			k := normKey(e.Name, e.Type)
			if excluded[k] {
				continue
			}
			if _, ok := prevMap[k]; ok {
				continue
			}
			merged = append(merged, e)
		}
		out := struct {
			Entities []entity `json:"entities"`
		}{Entities: merged}
		b, _ := json.MarshalIndent(out, "", "  ")
		text := string(b)

		// Optionally persist to Cloud Storage to enable download API
		var storagePath *string
		if h.storage != nil && h.bucket != "" {
			objectPath := fmt.Sprintf("%s/%s/%d-entities.json", tenantID, pid, time.Now().UnixNano())
			if err := h.storage.UploadBytes(ctx, h.bucket, objectPath, []byte(text), "application/json", map[string]string{
				"kind":     "coredoc",
				"filename": "entities.json",
			}); err == nil {
				p := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
				storagePath = &p
			}
		}

		// Upsert CoreDoc (fixed filename)
		now := time.Now().UTC()
		doc := &models.CoreDoc{
			TenantID:      tenantID,
			ProjectID:     pid,
			Filename:      "entities.json",
			ContentType:   "application/json",
			ExtractedText: &text,
			StoragePath:   storagePath,
			ParseStatus:   strPtr("parsed"),
			ParseMeta:     map[string]any{"updatedAt": now.Format(time.RFC3339)},
			CreatedBy:     &auth.UID,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		// Try find existing entities.json to preserve ID
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "entities.json") {
					doc.ID = d.ID
					break
				}
			}
		}
		if doc.ID == "" {
			if id, err := h.coreDocs.Create(ctx, doc); err == nil {
				doc.ID = id
			}
		} else {
			_ = h.coreDocs.Upsert(ctx, doc)
		}
		if h.pub != nil {
			payload := map[string]any{"projectId": pid, "docId": "entities.json", "filename": "entities.json", "status": "parsed", "ts": time.Now().UnixMilli()}
			log.Printf("[AnalyzeEntities] Publishing parsed status: %+v", payload)
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, payload)
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "entities",
				"status":    "done",
				"count":     len(merged),
				"elapsedMs": time.Since(started).Milliseconds(),
				"ts":        time.Now().UnixMilli(),
			})
		}
	}(auth.TenantID, projectID, req.Prompt, auth.UID)

	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// ---- Generic analyzers (facts, events, metrics, decisions) ----

// AnalyzeFacts handles POST /api/v1/projects/{id}/analyze/facts (async)
func (h *ProjectsHandler) AnalyzeFacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.exec == nil || h.coreDocs == nil || h.interactions == nil {
		WriteInternalError(w, "Facts analysis not configured")
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var req analyzeEntitiesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	go func(tenantID, pid, userPrompt, userID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "facts",
				"status":    "started",
				"ts":        time.Now().UnixMilli(),
			})
		}
		items, _ := h.interactions.ListByProject(ctx, pid)
		b := &strings.Builder{}
		maxChars := 16000
		for _, it := range items {
			if it == nil || (it.Kind != "user" && it.Kind != "assistant" && it.Kind != "system") {
				continue
			}
			line := fmt.Sprintf("- %s: %s\n", it.Kind, it.Content)
			if b.Len()+len(line) > maxChars {
				break
			}
			b.WriteString(line)
		}
		transcript := b.String()
		var prevJSON string
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "facts.json") && d.ExtractedText != nil {
					prevJSON = *d.ExtractedText
					break
				}
			}
		}
		// Prompt
		instructions := `Extract verifiable facts from the transcript.
Return STRICT JSON: { "facts": [ { "statement": string, "confidence"?: number (0..1), "evidence"?: string[], "sourceIds"?: string[] } ] }.
Guidelines:
- Facts must be concrete statements derivable from the transcript (no opinions).
- Prefer short, atomic statements; avoid duplicates.
`
		composed := instructions + "\nUSER_PROMPT:\n" + strings.TrimSpace(userPrompt) + "\nTRANSCRIPT:\n" + transcript + "\nPREVIOUS_FACTS_JSON:\n" + strings.TrimSpace(prevJSON)
		task := &ai.AITask{TaskType: ai.TaskTypeContentAnalysis, InputContent: composed, AIRequirements: ai.AIRequirements{MinContextWindow: 8000, MaxOutputSize: 4096, MinCapabilityScore: 6, TextGenerationCapability: true}, Verifier: ai.NewMapVerifier([]string{"facts"}, nil), EnableTelemetry: true, TenantID: tenantID, UserID: userID, Project: pid}
		parsed, err := h.exec.ExecuteStructuredTask(ctx, task)
		if err != nil {
			if h.pub != nil {
				_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "facts.json", "status": "failed", "error": err.Error(), "ts": time.Now().UnixMilli()})
				_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
					"projectId": pid,
					"kind":      "facts",
					"status":    "failed",
					"error":     err.Error(),
					"elapsedMs": time.Since(started).Milliseconds(),
					"ts":        time.Now().UnixMilli(),
				})
			}
			return
		}
		type fact struct {
			Statement  string   `json:"statement"`
			Evidence   []string `json:"evidence,omitempty"`
			SourceIDs  []string `json:"sourceIds,omitempty"`
			Confidence float64  `json:"confidence,omitempty"`
		}
		var prev struct {
			Facts []fact `json:"facts"`
		}
		if prevJSON != "" {
			_ = json.Unmarshal([]byte(prevJSON), &prev)
		}
		var cur struct {
			Facts []fact `json:"facts"`
		}
		if bb, e := json.Marshal(parsed); e == nil {
			_ = json.Unmarshal(bb, &cur)
		}
		norm := func(s string) string { return strings.TrimSpace(strings.ToLower(s)) }
		seen := map[string]bool{}
		merged := make([]fact, 0, len(prev.Facts)+len(cur.Facts))
		for _, f := range prev.Facts {
			k := norm(f.Statement)
			seen[k] = true
			merged = append(merged, f)
		}
		for _, f := range cur.Facts {
			k := norm(f.Statement)
			if !seen[k] {
				seen[k] = true
				merged = append(merged, f)
			}
		}
		out := struct {
			Facts []fact `json:"facts"`
		}{Facts: merged}
		buf, _ := json.MarshalIndent(out, "", "  ")
		text := string(buf)
		var storagePath *string
		if h.storage != nil && h.bucket != "" {
			objectPath := fmt.Sprintf("%s/%s/%d-facts.json", tenantID, pid, time.Now().UnixNano())
			if err := h.storage.UploadBytes(ctx, h.bucket, objectPath, []byte(text), "application/json", map[string]string{"kind": "coredoc", "filename": "facts.json"}); err == nil {
				p := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
				storagePath = &p
			}
		}
		now := time.Now().UTC()
		doc := &models.CoreDoc{TenantID: tenantID, ProjectID: pid, Filename: "facts.json", ContentType: "application/json", ExtractedText: &text, StoragePath: storagePath, ParseStatus: strPtr("parsed"), ParseMeta: map[string]any{"updatedAt": now.Format(time.RFC3339)}, CreatedBy: &auth.UID, CreatedAt: now, UpdatedAt: now}
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "facts.json") {
					doc.ID = d.ID
					break
				}
			}
		}
		if doc.ID == "" {
			if id, err := h.coreDocs.Create(ctx, doc); err == nil {
				doc.ID = id
			}
		} else {
			_ = h.coreDocs.Upsert(ctx, doc)
		}
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "facts.json", "filename": "facts.json", "status": "parsed", "ts": time.Now().UnixMilli()})
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "facts",
				"status":    "done",
				"count":     len(merged),
				"elapsedMs": time.Since(started).Milliseconds(),
				"ts":        time.Now().UnixMilli(),
			})
		}
	}(auth.TenantID, projectID, req.Prompt, auth.UID)
	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// AnalyzeEvents handles POST /api/v1/projects/{id}/analyze/events (async)
func (h *ProjectsHandler) AnalyzeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.exec == nil || h.coreDocs == nil || h.interactions == nil {
		WriteInternalError(w, "Events analysis not configured")
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var req analyzeEntitiesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	go func(tenantID, pid, userPrompt, userID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "events",
				"status":    "started",
				"ts":        time.Now().UnixMilli(),
			})
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "events.json", "status": "parsing", "ts": time.Now().UnixMilli()})
		}
		items, _ := h.interactions.ListByProject(ctx, pid)
		b := &strings.Builder{}
		maxChars := 16000
		for _, it := range items {
			if it == nil || (it.Kind != "user" && it.Kind != "assistant" && it.Kind != "system") {
				continue
			}
			line := fmt.Sprintf("- %s: %s\n", it.Kind, it.Content)
			if b.Len()+len(line) > maxChars {
				break
			}
			b.WriteString(line)
		}
		transcript := b.String()
		var prevJSON string
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "events.json") && d.ExtractedText != nil {
					prevJSON = *d.ExtractedText
					break
				}
			}
		}
		instructions := `Detect discrete events.
Return STRICT JSON: { "events": [ { "ts": string(RFC3339), "title": string, "summary"?: string, "actors"?: string[], "tags"?: string[], "evidence"?: string[], "sourceIds"?: string[], "severity"?: number(0..1), "confidence"?: number(0..1) } ] }.
Rules: prefer precise timestamps if available; severity/confidence in 0..1.
`
		composed := instructions + "\nUSER_PROMPT:\n" + strings.TrimSpace(userPrompt) + "\nTRANSCRIPT:\n" + transcript + "\nPREVIOUS_EVENTS_JSON:\n" + strings.TrimSpace(prevJSON)
		task := &ai.AITask{TaskType: ai.TaskTypeContentAnalysis, InputContent: composed, AIRequirements: ai.AIRequirements{MinContextWindow: 8000, MaxOutputSize: 4096, MinCapabilityScore: 6, TextGenerationCapability: true}, Verifier: ai.NewMapVerifier([]string{"events"}, nil), EnableTelemetry: true, TenantID: tenantID, UserID: userID, Project: pid}
		parsed, err := h.exec.ExecuteStructuredTask(ctx, task)
		if err != nil {
			if h.pub != nil {
				_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "events.json", "status": "failed", "error": err.Error(), "ts": time.Now().UnixMilli()})
				_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
					"projectId": pid,
					"kind":      "events",
					"status":    "failed",
					"error":     err.Error(),
					"elapsedMs": time.Since(started).Milliseconds(),
					"ts":        time.Now().UnixMilli(),
				})
			}
			return
		}
		type event struct {
			ID         string   `json:"id,omitempty"`
			Ts         string   `json:"ts"`
			Title      string   `json:"title"`
			Summary    string   `json:"summary,omitempty"`
			Actors     []string `json:"actors,omitempty"`
			Tags       []string `json:"tags,omitempty"`
			Evidence   []string `json:"evidence,omitempty"`
			SourceIDs  []string `json:"sourceIds,omitempty"`
			Severity   float64  `json:"severity,omitempty"`
			Confidence float64  `json:"confidence,omitempty"`
		}
		var prev struct {
			Events []event `json:"events"`
		}
		if prevJSON != "" {
			_ = json.Unmarshal([]byte(prevJSON), &prev)
		}
		var cur struct {
			Events []event `json:"events"`
		}
		if bb, e := json.Marshal(parsed); e == nil {
			_ = json.Unmarshal(bb, &cur)
		}
		key := func(e event) string { return strings.ToLower(strings.TrimSpace(e.Ts + "|" + e.Title)) }
		seen := map[string]bool{}
		merged := make([]event, 0, len(prev.Events)+len(cur.Events))
		for _, v := range prev.Events {
			k := key(v)
			seen[k] = true
			merged = append(merged, v)
		}
		for _, v := range cur.Events {
			k := key(v)
			if !seen[k] {
				seen[k] = true
				merged = append(merged, v)
			}
		}
		out := struct {
			Events []event `json:"events"`
		}{Events: merged}
		buf, _ := json.MarshalIndent(out, "", "  ")
		text := string(buf)
		var storagePath *string
		if h.storage != nil && h.bucket != "" {
			objectPath := fmt.Sprintf("%s/%s/%d-events.json", tenantID, pid, time.Now().UnixNano())
			if err := h.storage.UploadBytes(ctx, h.bucket, objectPath, []byte(text), "application/json", map[string]string{"kind": "coredoc", "filename": "events.json"}); err == nil {
				p := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
				storagePath = &p
			}
		}
		now := time.Now().UTC()
		doc := &models.CoreDoc{TenantID: tenantID, ProjectID: pid, Filename: "events.json", ContentType: "application/json", ExtractedText: &text, StoragePath: storagePath, ParseStatus: strPtr("parsed"), ParseMeta: map[string]any{"updatedAt": now.Format(time.RFC3339)}, CreatedBy: &auth.UID, CreatedAt: now, UpdatedAt: now}
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "events.json") {
					doc.ID = d.ID
					break
				}
			}
		}
		if doc.ID == "" {
			if id, err := h.coreDocs.Create(ctx, doc); err == nil {
				doc.ID = id
			}
		} else {
			_ = h.coreDocs.Upsert(ctx, doc)
		}
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "events.json", "filename": "events.json", "status": "parsed", "ts": time.Now().UnixMilli()})
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "events",
				"status":    "done",
				"count":     len(merged),
				"elapsedMs": time.Since(started).Milliseconds(),
				"ts":        time.Now().UnixMilli(),
			})
		}
	}(auth.TenantID, projectID, req.Prompt, auth.UID)
	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// AnalyzeMetrics handles POST /api/v1/projects/{id}/analyze/metrics (async)
func (h *ProjectsHandler) AnalyzeMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.exec == nil || h.coreDocs == nil || h.interactions == nil {
		WriteInternalError(w, "Metrics analysis not configured")
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var req analyzeEntitiesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	go func(tenantID, pid, userPrompt, userID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "metrics",
				"status":    "started",
				"ts":        time.Now().UnixMilli(),
			})
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "metrics.json", "status": "parsing", "ts": time.Now().UnixMilli()})
		}
		items, _ := h.interactions.ListByProject(ctx, pid)
		b := &strings.Builder{}
		maxChars := 16000
		for _, it := range items {
			if it == nil || (it.Kind != "user" && it.Kind != "assistant" && it.Kind != "system") {
				continue
			}
			line := fmt.Sprintf("- %s: %s\n", it.Kind, it.Content)
			if b.Len()+len(line) > maxChars {
				break
			}
			b.WriteString(line)
		}
		transcript := b.String()
		var prevJSON string
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "metrics.json") && d.ExtractedText != nil {
					prevJSON = *d.ExtractedText
					break
				}
			}
		}
		instructions := `Extract measurable metrics with methodology.
Return STRICT JSON: { "metrics": [ {
  "key": string, "label": string, "description"?: string, "value": number|string, "unit"?: string,
  "updateISO"?: string, "updateDays"?: number, "evidence"?: string[], "sourceIds"?: string[], "confidence"?: number(0..1),
  "definition": string,
  "scope": { "geography"?: string, "sector"?: string, "industry"?: string, "period"?: string },
  "computation": { "formula"?: string, "numerator"?: string, "denominator"?: string, "aggregation"?: string },
  "detectionCues": { "keywords"?: string[], "patterns"?: string[], "positiveExamples"?: string[], "negativeExamples"?: string[] },
  "dataSources"?: string[], "qualityNotes"?: string
} ] }.
Guidelines: keys stable and normalized; prefer domain-specific scope.
`
		composed := instructions + "\nUSER_PROMPT:\n" + strings.TrimSpace(userPrompt) + "\nTRANSCRIPT:\n" + transcript + "\nPREVIOUS_METRICS_JSON:\n" + strings.TrimSpace(prevJSON)
		task := &ai.AITask{TaskType: ai.TaskTypeContentAnalysis, InputContent: composed, AIRequirements: ai.AIRequirements{MinContextWindow: 8000, MaxOutputSize: 4096, MinCapabilityScore: 6, TextGenerationCapability: true}, Verifier: ai.NewMapVerifier([]string{"metrics"}, nil), EnableTelemetry: true, TenantID: tenantID, UserID: userID, Project: pid}
		parsed, err := h.exec.ExecuteStructuredTask(ctx, task)
		if err != nil {
			if h.pub != nil {
				_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "metrics.json", "status": "failed", "error": err.Error(), "ts": time.Now().UnixMilli()})
				_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
					"projectId": pid,
					"kind":      "metrics",
					"status":    "failed",
					"error":     err.Error(),
					"elapsedMs": time.Since(started).Milliseconds(),
					"ts":        time.Now().UnixMilli(),
				})
			}
			return
		}
		type metric struct {
			Key         string   `json:"key"`
			Label       string   `json:"label"`
			Description string   `json:"description,omitempty"`
			Value       any      `json:"value"`
			Unit        string   `json:"unit,omitempty"`
			UpdateISO   string   `json:"updateISO,omitempty"`
			UpdateDays  *int     `json:"updateDays,omitempty"`
			Evidence    []string `json:"evidence,omitempty"`
			SourceIDs   []string `json:"sourceIds,omitempty"`
			Confidence  float64  `json:"confidence,omitempty"`
			Definition  string   `json:"definition"`
			Scope       struct {
				Geography string `json:"geography,omitempty"`
				Sector    string `json:"sector,omitempty"`
				Industry  string `json:"industry,omitempty"`
				Period    string `json:"period,omitempty"`
			} `json:"scope"`
			Computation struct {
				Formula     string `json:"formula,omitempty"`
				Numerator   string `json:"numerator,omitempty"`
				Denominator string `json:"denominator,omitempty"`
				Aggregation string `json:"aggregation,omitempty"`
			} `json:"computation"`
			DetectionCues struct {
				Keywords         []string `json:"keywords,omitempty"`
				Patterns         []string `json:"patterns,omitempty"`
				PositiveExamples []string `json:"positiveExamples,omitempty"`
				NegativeExamples []string `json:"negativeExamples,omitempty"`
			} `json:"detectionCues"`
			DataSources  []string `json:"dataSources,omitempty"`
			QualityNotes string   `json:"qualityNotes,omitempty"`
		}
		var prev struct {
			Metrics []metric `json:"metrics"`
		}
		if prevJSON != "" {
			_ = json.Unmarshal([]byte(prevJSON), &prev)
		}
		var cur struct {
			Metrics []metric `json:"metrics"`
		}
		if bb, e := json.Marshal(parsed); e == nil {
			_ = json.Unmarshal(bb, &cur)
		}
		normKey := func(s string) string { return strings.TrimSpace(strings.ToLower(s)) }
		seen := map[string]bool{}
		merged := make([]metric, 0, len(prev.Metrics)+len(cur.Metrics))
		for _, m := range prev.Metrics {
			k := normKey(m.Key)
			seen[k] = true
			merged = append(merged, m)
		}
		for _, m := range cur.Metrics {
			k := normKey(m.Key)
			if !seen[k] {
				seen[k] = true
				merged = append(merged, m)
			}
		}
		out := struct {
			Metrics []metric `json:"metrics"`
		}{Metrics: merged}
		buf, _ := json.MarshalIndent(out, "", "  ")
		text := string(buf)
		var storagePath *string
		if h.storage != nil && h.bucket != "" {
			objectPath := fmt.Sprintf("%s/%s/%d-metrics.json", tenantID, pid, time.Now().UnixNano())
			if err := h.storage.UploadBytes(ctx, h.bucket, objectPath, []byte(text), "application/json", map[string]string{"kind": "coredoc", "filename": "metrics.json"}); err == nil {
				p := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
				storagePath = &p
			}
		}
		now := time.Now().UTC()
		doc := &models.CoreDoc{TenantID: tenantID, ProjectID: pid, Filename: "metrics.json", ContentType: "application/json", ExtractedText: &text, StoragePath: storagePath, ParseStatus: strPtr("parsed"), ParseMeta: map[string]any{"updatedAt": now.Format(time.RFC3339)}, CreatedBy: &auth.UID, CreatedAt: now, UpdatedAt: now}
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "metrics.json") {
					doc.ID = d.ID
					break
				}
			}
		}
		if doc.ID == "" {
			if id, err := h.coreDocs.Create(ctx, doc); err == nil {
				doc.ID = id
			}
		} else {
			_ = h.coreDocs.Upsert(ctx, doc)
		}
		if h.pub != nil {
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "metrics.json", "filename": "metrics.json", "status": "parsed", "ts": time.Now().UnixMilli()})
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "metrics",
				"status":    "done",
				"count":     len(merged),
				"elapsedMs": time.Since(started).Milliseconds(),
				"ts":        time.Now().UnixMilli(),
			})
		}
	}(auth.TenantID, projectID, req.Prompt, auth.UID)
	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// AnalyzeDecisions handles POST /api/v1/projects/{id}/analyze/decisions (async)
func (h *ProjectsHandler) AnalyzeDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.exec == nil || h.coreDocs == nil || h.interactions == nil {
		WriteInternalError(w, "Decisions analysis not configured")
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != auth.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var req analyzeEntitiesRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	go func(tenantID, pid, userPrompt, userID string) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		started := time.Now()
		log.Printf("[AnalyzeDecisions] Starting analysis for project=%s, tenantId=%s", pid, tenantID)
		if h.pub != nil {
			log.Printf("[AnalyzeDecisions] Publishing started status...")
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "decisions",
				"status":    "started",
				"ts":        time.Now().UnixMilli(),
			})
		}
		items, _ := h.interactions.ListByProject(ctx, pid)
		b := &strings.Builder{}
		maxChars := 16000
		for _, it := range items {
			if it == nil || (it.Kind != "user" && it.Kind != "assistant" && it.Kind != "system") {
				continue
			}
			line := fmt.Sprintf("- %s: %s\n", it.Kind, it.Content)
			if b.Len()+len(line) > maxChars {
				break
			}
			b.WriteString(line)
		}
		transcript := b.String()
		var prevJSON string
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "decisions.json") && d.ExtractedText != nil {
					prevJSON = *d.ExtractedText
					break
				}
			}
		}
		instructions := `Extract decisions.
Return STRICT JSON: { "decisions": [ {
  "id"?: string, "title": string, "rationale": string, "options"?: string[], "selected"?: string,
  "criteria"?: [{"name": string, "score": number(0..1)}],
  "actors"?: [{"id"?: string, "name": string, "type"?: string}],
  "evidence"?: string[], "sourceIds"?: string[], "severity"?: number(0..1), "confidence"?: number(0..1)
} ] }.
`
		composed := instructions + "\nUSER_PROMPT:\n" + strings.TrimSpace(userPrompt) + "\nTRANSCRIPT:\n" + transcript + "\nPREVIOUS_DECISIONS_JSON:\n" + strings.TrimSpace(prevJSON)
		task := &ai.AITask{TaskType: ai.TaskTypeContentAnalysis, InputContent: composed, AIRequirements: ai.AIRequirements{MinContextWindow: 8000, MaxOutputSize: 4096, MinCapabilityScore: 6, TextGenerationCapability: true}, Verifier: ai.NewMapVerifier([]string{"decisions"}, nil), EnableTelemetry: true, TenantID: tenantID, UserID: userID, Project: pid}
		parsed, err := h.exec.ExecuteStructuredTask(ctx, task)
		if err != nil {
			if h.pub != nil {
				_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "decisions.json", "status": "failed", "error": err.Error(), "ts": time.Now().UnixMilli()})
				_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
					"projectId": pid,
					"kind":      "decisions",
					"status":    "failed",
					"error":     err.Error(),
					"elapsedMs": time.Since(started).Milliseconds(),
					"ts":        time.Now().UnixMilli(),
				})
			}
			return
		}
		type actor struct {
			ID   string `json:"id,omitempty"`
			Name string `json:"name"`
			Type string `json:"type,omitempty"`
		}
		type decision struct {
			ID        string   `json:"id,omitempty"`
			Title     string   `json:"title"`
			Rationale string   `json:"rationale"`
			Selected  string   `json:"selected,omitempty"`
			Options   []string `json:"options,omitempty"`
			Criteria  []struct {
				Name  string  `json:"name"`
				Score float64 `json:"score"`
			} `json:"criteria,omitempty"`
			Actors     []actor  `json:"actors,omitempty"`
			Evidence   []string `json:"evidence,omitempty"`
			SourceIDs  []string `json:"sourceIds,omitempty"`
			Severity   float64  `json:"severity,omitempty"`
			Confidence float64  `json:"confidence,omitempty"`
		}
		var prev struct {
			Decisions []decision `json:"decisions"`
		}
		if prevJSON != "" {
			_ = json.Unmarshal([]byte(prevJSON), &prev)
		}
		var cur struct {
			Decisions []decision `json:"decisions"`
		}
		if bb, e := json.Marshal(parsed); e == nil {
			_ = json.Unmarshal(bb, &cur)
		}
		norm := func(s string) string { return strings.TrimSpace(strings.ToLower(s)) }
		seen := map[string]bool{}
		merged := make([]decision, 0, len(prev.Decisions)+len(cur.Decisions))
		for _, d := range prev.Decisions {
			k := norm(d.Title)
			seen[k] = true
			merged = append(merged, d)
		}
		for _, d := range cur.Decisions {
			k := norm(d.Title)
			if !seen[k] {
				seen[k] = true
				merged = append(merged, d)
			}
		}
		out := struct {
			Decisions []decision `json:"decisions"`
		}{Decisions: merged}
		buf, _ := json.MarshalIndent(out, "", "  ")
		text := string(buf)
		var storagePath *string
		if h.storage != nil && h.bucket != "" {
			objectPath := fmt.Sprintf("%s/%s/%d-decisions.json", tenantID, pid, time.Now().UnixNano())
			if err := h.storage.UploadBytes(ctx, h.bucket, objectPath, []byte(text), "application/json", map[string]string{"kind": "coredoc", "filename": "decisions.json"}); err == nil {
				p := fmt.Sprintf("gs://%s/%s", h.bucket, objectPath)
				storagePath = &p
			}
		}
		now := time.Now().UTC()
		doc := &models.CoreDoc{TenantID: tenantID, ProjectID: pid, Filename: "decisions.json", ContentType: "application/json", ExtractedText: &text, StoragePath: storagePath, ParseStatus: strPtr("parsed"), ParseMeta: map[string]any{"updatedAt": now.Format(time.RFC3339)}, CreatedBy: &auth.UID, CreatedAt: now, UpdatedAt: now}
		if docs, err := h.coreDocs.ListByProject(ctx, tenantID, pid, 50, nil); err == nil {
			for _, d := range docs {
				if strings.EqualFold(d.Filename, "decisions.json") {
					doc.ID = d.ID
					break
				}
			}
		}
		if doc.ID == "" {
			if id, err := h.coreDocs.Create(ctx, doc); err == nil {
				doc.ID = id
			}
		} else {
			_ = h.coreDocs.Upsert(ctx, doc)
		}
		if h.pub != nil {
			log.Printf("[AnalyzeDecisions] Analysis complete, publishing done status (count=%d, elapsed=%dms)", len(merged), time.Since(started).Milliseconds())
			_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": pid, "docId": "decisions.json", "filename": "decisions.json", "status": "parsed", "ts": time.Now().UnixMilli()})
			_ = h.pub.PublishJSON(ctx, realtime.EventAnalysisStatus, tenantID, map[string]any{
				"projectId": pid,
				"kind":      "decisions",
				"status":    "done",
				"count":     len(merged),
				"elapsedMs": time.Since(started).Milliseconds(),
				"ts":        time.Now().UnixMilli(),
			})
		}
	}(auth.TenantID, projectID, req.Prompt, auth.UID)
	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}
func strPtr(s string) *string { return &s }

// DeleteProjectDoc handles DELETE /api/v1/projects/{id}/docs/{docId}
func (h *ProjectsHandler) DeleteProjectDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs repository is not configured")
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project and document ID are required")
		return
	}
	projectID, docID := parts[3], parts[5]
	project, err := h.repo.Get(r.Context(), projectID)
	if err != nil || project == nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil || cd.ProjectID != projectID || cd.TenantID != authInfo.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}
	// Cascade delete: find and delete associated Document and Chunks
	if h.docsRepo != nil && h.chunksRepo != nil {
		if doc, err := h.docsRepo.GetByCoreDocID(r.Context(), authInfo.TenantID, docID); err == nil && doc != nil {
			// Delete all chunks for this document
			if err := h.chunksRepo.DeleteByDoc(r.Context(), authInfo.TenantID, doc.ID); err != nil {
				h.logger.Printf("Warning: failed to delete chunks for doc %s: %v", doc.ID, err)
			}
			// Delete the document
			if err := h.docsRepo.Delete(r.Context(), doc.ID); err != nil {
				h.logger.Printf("Warning: failed to delete document %s: %v", doc.ID, err)
			}
		}
	}
	if cd.StoragePath != nil && strings.HasPrefix(*cd.StoragePath, "gs://") && h.storage != nil {
		bucket, object := splitGsPath(*cd.StoragePath)
		_ = h.storage.DeleteObject(r.Context(), bucket, object)
	}
	_, _ = h.coreDocs.Delete(r.Context(), docID)
	if h.pub != nil {
		_ = h.pub.PublishJSON(r.Context(), realtime.EventDocProcess, authInfo.TenantID, map[string]any{"projectId": projectID, "docId": docID, "status": "deleted", "ts": time.Now().UnixMilli()})
	}
	w.WriteHeader(http.StatusNoContent)
}

// indexCoreDoc indexes a CoreDoc by creating/updating Document and Chunks with embeddings
func (h *ProjectsHandler) indexCoreDoc(ctx context.Context, tenantID string, cd *models.CoreDoc) error {
	if h.docsRepo == nil || h.chunksRepo == nil || h.emb == nil {
		// RAG not configured, skip indexing
		return nil
	}
	if cd.ExtractedText == nil || strings.TrimSpace(*cd.ExtractedText) == "" {
		// No text to index
		return nil
	}
	// Skip indexing for dynamic artifacts (they are generated, not indexed)
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" {
		return nil
	}

	// Virtual SourceID for project documents
	virtualSourceID := fmt.Sprintf("project:%s", cd.ProjectID)
	externalID := fmt.Sprintf("coredoc:%s", cd.ID)
	text := strings.TrimSpace(*cd.ExtractedText)

	// Check if Document already exists
	existingDoc, _ := h.docsRepo.GetByCoreDocID(ctx, tenantID, cd.ID)
	var docID string
	now := time.Now().UTC()

	if existingDoc != nil {
		// Update existing document
		existingDoc.FullText = text
		existingDoc.Title = cd.Filename
		existingDoc.ContentType = cd.ContentType
		existingDoc.DocumentType = "coredoc"
		existingDoc.DocumentPath = fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID)
		existingDoc.ContentHash = repo.HashContent(text)
		existingDoc.UpdatedAt = now
		existingDoc.Status = "new"
		if err := h.docsRepo.Upsert(ctx, existingDoc); err != nil {
			return fmt.Errorf("failed to update document: %w", err)
		}
		docID = existingDoc.ID
		// Delete old chunks before creating new ones
		if err := h.chunksRepo.DeleteByDoc(ctx, tenantID, docID); err != nil {
			h.logger.Printf("Warning: failed to delete old chunks for doc %s: %v", docID, err)
		}
	} else {
		// Create new document
		doc := &models.Document{
			TenantID:     tenantID,
			SourceID:     virtualSourceID,
			ExternalID:   externalID,
			Title:        cd.Filename,
			DocumentPath: fmt.Sprintf("coredoc/%s/%s", cd.ProjectID, cd.ID),
			FullText:     text,
			Status:       "new",
			ContentType:  cd.ContentType,
			DocumentType: "coredoc",
			ContentHash:  repo.HashContent(text),
			IngestedAt:   now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Attrs: map[string]any{
				"coreDocId":    cd.ID,
				"projectId":    cd.ProjectID,
				"documentType": "coredoc",
			},
		}
		id, err := h.docsRepo.Create(ctx, doc)
		if err != nil {
			return fmt.Errorf("failed to create document: %w", err)
		}
		docID = id
	}

	// Chunk and embed
	parts := discovery.ChunkText(text, 1200)
	if len(parts) == 0 {
		return nil
	}

	vectors, err := h.emb.EmbedTexts(ctx, parts)
	if err != nil {
		return fmt.Errorf("failed to create embeddings: %w", err)
	}

	for i := range parts {
		ch := &models.Chunk{
			TenantID:         tenantID,
			DocID:            docID,
			SourceID:         virtualSourceID,
			Position:         i,
			Text:             parts[i],
			Embedding:        vectors[i],
			EmbeddingModel:   h.emb.ModelID(),
			EmbeddingVersion: "v1",
			Attrs:            map[string]any{"documentType": "coredoc", "coreDocId": cd.ID},
			ContentHash:      repo.HashContent(parts[i]),
			CreatedAt:        now,
		}
		if _, err := h.chunksRepo.Create(ctx, ch); err != nil {
			return fmt.Errorf("failed to create chunk: %w", err)
		}
	}

	// Mark document as embedded
	if doc, _ := h.docsRepo.GetByID(ctx, docID); doc != nil {
		doc.Status = "embedded"
		_ = h.docsRepo.Upsert(ctx, doc)
	}

	return nil
}

// helper: parse and persist + publish events
func (h *ProjectsHandler) parseAndPersist(ctx context.Context, tenantID, projectID string, cd *models.CoreDoc) error {
	if h.pub != nil {
		_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": projectID, "docId": cd.ID, "status": "parsing", "ts": time.Now().UnixMilli()})
	}
	bucket, object := splitGsPath(*cd.StoragePath)
	data, err := h.storage.ReadObject(ctx, bucket, object)
	if err != nil {
		return err
	}

	// Special handling for CSV/TSV to produce structured meta and normalized copy
	ct := strings.ToLower(cd.ContentType)
	isCSV := strings.Contains(ct, "text/csv") || strings.Contains(ct, "application/csv") || strings.HasSuffix(strings.ToLower(cd.Filename), ".csv")
	isTSV := strings.Contains(ct, "tab-separated") || strings.HasSuffix(strings.ToLower(cd.Filename), ".tsv")
	if isCSV || isTSV {
		text, meta, normalized, perr := parseCSVBytes(data, isTSV)
		if perr != nil {
			return perr
		}
		// Upload normalized CSV copy
		if len(normalized) > 0 && h.storage != nil && h.bucket != "" {
			normalizedObject := fmt.Sprintf("%s/%s/%s-normalized.csv", tenantID, projectID, cd.ID)
			if err := h.storage.UploadBytes(ctx, h.bucket, normalizedObject, normalized, "text/csv", map[string]string{
				"tenantId":   tenantID,
				"projectId":  projectID,
				"docId":      cd.ID,
				"normalized": "true",
			}); err == nil {
				if meta == nil {
					meta = map[string]any{}
				}
				meta["normalizedPath"] = fmt.Sprintf("gs://%s/%s", h.bucket, normalizedObject)
			}
		}
		status := "parsed"
		cd.ExtractedText = &text
		cd.ParseStatus = &status
		if cd.ParseMeta == nil {
			cd.ParseMeta = map[string]any{}
		}
		for k, v := range meta {
			cd.ParseMeta[k] = v
		}
		cd.ParseMeta["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
	} else {
		// default path for non-CSV
		t, pages, perr := parseByContentType(cd.ContentType, data)
		if perr != nil {
			return perr
		}
		status := "parsed"
		cd.ExtractedText = &t
		cd.ParseStatus = &status
		if pages != nil {
			cd.PageCount = pages
		}
		if cd.ParseMeta == nil {
			cd.ParseMeta = map[string]any{}
		}
		cd.ParseMeta["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	if err := h.coreDocs.Upsert(ctx, cd); err != nil {
		return err
	}
	if h.pub != nil {
		chars := 0
		if cd.ExtractedText != nil {
			chars = len(*cd.ExtractedText)
		}
		_ = h.pub.PublishJSON(ctx, realtime.EventDocProcess, tenantID, map[string]any{"projectId": projectID, "docId": cd.ID, "status": "parsed", "chars": chars, "ts": time.Now().UnixMilli()})
	}
	// Index CoreDoc for vector search
	if err := h.indexCoreDoc(ctx, tenantID, cd); err != nil {
		h.logger.Printf("Warning: failed to index CoreDoc %s: %v", cd.ID, err)
		// Don't fail parsing if indexing fails
	}
	return nil
}

func splitGsPath(gs string) (bucket, object string) {
	s := strings.TrimPrefix(gs, "gs://")
	i := strings.Index(s, "/")
	if i <= 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
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
