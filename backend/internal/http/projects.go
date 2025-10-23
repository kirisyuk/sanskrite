package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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
		if docs, err := h.coreDocs.ListByProject(r.Context(), authInfo.TenantID, projectID, 0); err == nil {
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
	Filename    string                 `json:"filename"`
	ContentType string                 `json:"contentType"`
	Description *string                `json:"description,omitempty"`
	SizeBytes   *int64                 `json:"sizeBytes,omitempty"`
	StoragePath *string                `json:"storagePath,omitempty"`
	External    *models.ExternalDocRef `json:"external,omitempty"`
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
		TenantID:    authInfo.TenantID,
		ProjectID:   projectID,
		Filename:    req.Filename,
		ContentType: req.ContentType,
		Description: req.Description,
		SizeBytes:   req.SizeBytes,
		StoragePath: req.StoragePath,
		External:    req.External,
		CreatedBy:   &authInfo.UID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	id, err := h.coreDocs.Create(r.Context(), doc)
	if err != nil {
		h.logger.Printf("CreateProjectDoc error: %v", err)
		WriteInternalError(w, "Failed to create document")
		return
	}
	doc.ID = id
	WriteJSON(w, http.StatusCreated, doc)
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
	docs, err := h.coreDocs.ListByProject(r.Context(), authInfo.TenantID, projectID, 0)
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
