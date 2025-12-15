package http

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type ShareHandler struct {
	shares   *repo.ShareTokensRepository
	projs    *repo.ProjectsRepository
	inter    *repo.InteractionsRepository
	coreDocs *repo.CoreDocsRepo
	storage  StorageClient
	bucket   string
	logger   *log.Logger
}

func NewShareHandler(shares *repo.ShareTokensRepository, projs *repo.ProjectsRepository, inter *repo.InteractionsRepository, logger *log.Logger) *ShareHandler {
	return &ShareHandler{shares: shares, projs: projs, inter: inter, logger: logger}
}

// WithCoreDocs binds CoreDocs repository
func (h *ShareHandler) WithCoreDocs(core *repo.CoreDocsRepo) *ShareHandler {
	h.coreDocs = core
	return h
}

// WithStorage wires storage client and bucket for document downloads
func (h *ShareHandler) WithStorage(client StorageClient, bucket string) *ShareHandler {
	h.storage = client
	h.bucket = bucket
	return h
}

type createShareRequest struct {
	ExpiresAt string `json:"expiresAt"` // ISO
}

type createShareResponse struct {
	ShareToken string `json:"shareToken"`
	ExpiresAt  string `json:"expiresAt"`
	Url        string `json:"url"`
}

func randomToken(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// POST /api/v1/projects/{id}/share
func (h *ShareHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		WriteBadRequest(w, "INVALID_PATH", "Missing project id")
		return
	}
	projectID := parts[3]

	var req createShareRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	exp := time.Now().UTC().Add(24 * time.Hour)
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			exp = t
		}
	}
	tok := randomToken(48)
	st := &repo.ShareToken{ProjectID: projectID, Token: tok, ExpiresAt: exp, CreatedBy: auth.UID}
	if _, err := h.shares.Create(r.Context(), st); err != nil {
		WriteInternalError(w, "Failed to create share token")
		return
	}
	base := r.Header.Get("X-External-Base-Url")
	if base == "" {
		base = strings.TrimSuffix("https://"+r.Host, "/")
	}
	resp := createShareResponse{ShareToken: tok, ExpiresAt: exp.Format(time.RFC3339), Url: base + "/shared/" + tok}
	WriteJSON(w, http.StatusCreated, resp)
}

// DELETE /api/v1/projects/{id}/share/{token}
func (h *ShareHandler) RevokeShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Missing token")
		return
	}
	token := parts[5]
	st, id, err := h.shares.GetByToken(r.Context(), token)
	if err != nil || st == nil {
		WriteNotFound(w, "Token not found")
		return
	}
	if err := h.shares.Revoke(r.Context(), id); err != nil {
		WriteInternalError(w, "Failed to revoke token")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GET /api/v1/public/share/{token}
func (h *ShareHandler) GetPublicShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Missing token")
		return
	}
	token := parts[4]
	st, id, err := h.shares.GetByToken(r.Context(), token)
	if err != nil || st == nil {
		WriteNotFound(w, "NOT_FOUND")
		return
	}
	if !st.IsActive || time.Now().UTC().After(st.ExpiresAt) {
		WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "Token expired or revoked", nil)
		return
	}

	// Touch access
	_ = h.shares.TouchAccess(r.Context(), id)

	// Load project (minimal)
	pr, err := h.projs.Get(r.Context(), st.ProjectID)
	if err != nil || pr == nil {
		WriteNotFound(w, "PROJECT_NOT_FOUND")
		return
	}
	// Load interactions
	items, _ := h.inter.ListByProject(r.Context(), st.ProjectID)

	// Build public response
	type PubAuthor struct {
		Kind        string  `json:"kind"`
		DisplayName *string `json:"displayName,omitempty"`
	}
	type PubGraph struct {
		ChartTitle string   `json:"chartTitle"`
		ChartType  *string  `json:"chartType,omitempty"`
		Table      [][]any  `json:"table"`
		Sources    []string `json:"sources"`
	}
	type PubRelation struct {
		Type     string `json:"type"`
		TargetID string `json:"targetId"`
	}
	type PubInteraction struct {
		ID               string                    `json:"id"`
		Kind             string                    `json:"kind"`
		Subkind          *string                   `json:"subkind,omitempty"`
		ReplyTo          *string                   `json:"replyTo,omitempty"`
		Content          *string                   `json:"content,omitempty"`
		ContentSanitized *string                   `json:"contentSanitized,omitempty"`
		GraphData        *PubGraph                 `json:"graphData,omitempty"`
		SearchQueries    []string                  `json:"searchQueries,omitempty"`
		Sources          []string                  `json:"sources,omitempty"`
		CreatedAt        time.Time                 `json:"createdAt"`
		Author           *PubAuthor                `json:"author,omitempty"`
		Metadata         *repo.InteractionMetadata `json:"metadata,omitempty"`
		Relations        []PubRelation             `json:"relations,omitempty"`
	}
	pub := struct {
		V     string `json:"v"`
		Share struct {
			TokenId   string    `json:"tokenId"`
			IsActive  bool      `json:"isActive"`
			ExpiresAt time.Time `json:"expiresAt"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"share"`
		Project struct {
			ID          string     `json:"id"`
			DisplayName string     `json:"displayName"`
			Description *string    `json:"description,omitempty"`
			CreatedAt   *time.Time `json:"createdAt,omitempty"`
		} `json:"project"`
		Interactions []PubInteraction `json:"interactions"`
	}{V: "1"}
	pub.Share.TokenId = id
	pub.Share.IsActive = st.IsActive
	pub.Share.ExpiresAt = st.ExpiresAt
	pub.Share.CreatedAt = st.CreatedAt
	pub.Project.ID = pr.ID
	pub.Project.DisplayName = pr.DisplayName
	pub.Project.Description = pr.Description
	pub.Project.CreatedAt = &pr.CreatedAt

	// Map interactions (sanitize)
	for _, it := range items {
		// Include 'user' | 'assistant' | 'system' (for compliance warnings and other evaluations)
		// Also include 'note' with 'discussion.flow.analysis' subkind (for Discussion Frame)
		// Also include legacy discovery.query/discovery.answer for backward compatibility
		if it.Kind != "user" && it.Kind != "assistant" && it.Kind != "system" && it.Kind != "discovery.query" && it.Kind != "discovery.answer" {
			// Include note interactions with discussion.flow.analysis subkind for Discussion Frame
			if it.Kind == "note" && it.Subkind != nil && *it.Subkind == "discussion.flow.analysis" {
				// Will be handled separately below
			} else {
				continue
			}
		}
		kind := it.Kind
		if kind == "discovery.query" {
			kind = "user"
		}
		if kind == "discovery.answer" {
			kind = "assistant"
		}
		// Keep 'note' kind for discussion.flow.analysis interactions (Discussion Frame)
		if kind == "note" && it.Subkind != nil && *it.Subkind == "discussion.flow.analysis" {
			kind = "note"
		}
		// Convert relations to PubRelation format
		var relations []PubRelation
		if len(it.Relations) > 0 {
			relations = make([]PubRelation, len(it.Relations))
			for i, rel := range it.Relations {
				relations[i] = PubRelation{Type: rel.Type, TargetID: rel.TargetID}
			}
		}
		pi := PubInteraction{
			ID:               it.ID,
			Kind:             kind,
			Subkind:          it.Subkind,
			ReplyTo:          it.ReplyTo,
			Content:          &it.Content,
			ContentSanitized: it.ContentSanitized,
			SearchQueries:    it.SearchQueries,
			Sources:          it.Sources,
			CreatedAt:        it.CreatedAt,
			Metadata:         it.Metadata,
			Relations:        relations,
		}
		if it.Author != nil {
			pi.Author = &PubAuthor{Kind: it.Author.Kind, DisplayName: it.Author.DisplayName}
		}
		// Graph data: best-effort heuristic from sanitized JSON embedded previously (FE already persists graphData client-side)
		// We keep this empty on server side for v1; FE will infer from contentSanitized when needed.
		pub.Interactions = append(pub.Interactions, pi)
	}

	WriteJSON(w, http.StatusOK, pub)
}

// validateShareToken validates a share token and returns the token and project ID if valid
func (h *ShareHandler) validateShareToken(r *http.Request) (*repo.ShareToken, string, error) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		return nil, "", fmt.Errorf("invalid path")
	}
	token := parts[4]
	st, _, err := h.shares.GetByToken(r.Context(), token)
	if err != nil || st == nil {
		return nil, "", fmt.Errorf("token not found")
	}
	if !st.IsActive || time.Now().UTC().After(st.ExpiresAt) {
		return nil, "", fmt.Errorf("token expired or revoked")
	}
	return st, st.ProjectID, nil
}

// ListPublicShareDocs handles GET /api/v1/public/share/{token}/docs
func (h *ShareHandler) ListPublicShareDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs not configured")
		return
	}

	st, projectID, err := h.validateShareToken(r)
	if err != nil {
		if err.Error() == "token not found" {
			WriteNotFound(w, "NOT_FOUND")
		} else if err.Error() == "token expired or revoked" {
			WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "Token expired or revoked", nil)
		} else {
			WriteBadRequest(w, "INVALID_PATH", "Invalid token")
		}
		return
	}

	// Touch access
	_ = h.shares.TouchAccess(r.Context(), st.ID)

	// Load project to get tenantID
	pr, err := h.projs.Get(r.Context(), projectID)
	if err != nil || pr == nil {
		WriteNotFound(w, "PROJECT_NOT_FOUND")
		return
	}

	// List all documents for the project
	docs, err := h.coreDocs.ListByProject(r.Context(), pr.TenantID, projectID, 0, nil)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("ListPublicShareDocs error: %v", err)
		}
		WriteInternalError(w, "Failed to list documents")
		return
	}

	// Build public response (sanitized document list)
	type PubDoc struct {
		ID          string  `json:"id"`
		Filename    string  `json:"filename"`
		ContentType string  `json:"contentType"`
		Description *string `json:"description,omitempty"`
		SizeBytes   *int64  `json:"sizeBytes,omitempty"`
		DocType     *string `json:"docType,omitempty"`
		CreatedAt   string  `json:"createdAt"`
		UpdatedAt   string  `json:"updatedAt"`
	}

	pubDocs := make([]PubDoc, 0, len(docs))
	for _, doc := range docs {
		pubDocs = append(pubDocs, PubDoc{
			ID:          doc.ID,
			Filename:    doc.Filename,
			ContentType: doc.ContentType,
			Description: doc.Description,
			SizeBytes:   doc.SizeBytes,
			DocType:     doc.DocType,
			CreatedAt:   doc.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   doc.UpdatedAt.Format(time.RFC3339),
		})
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"documents": pubDocs,
	})
}

// GetPublicShareDoc handles GET /api/v1/public/share/{token}/docs/{docId}
func (h *ShareHandler) GetPublicShareDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs not configured")
		return
	}

	st, projectID, err := h.validateShareToken(r)
	if err != nil {
		if err.Error() == "token not found" {
			WriteNotFound(w, "NOT_FOUND")
		} else if err.Error() == "token expired or revoked" {
			WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "Token expired or revoked", nil)
		} else {
			WriteBadRequest(w, "INVALID_PATH", "Invalid token")
		}
		return
	}

	// Extract docID from path
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 7 {
		WriteBadRequest(w, "INVALID_PATH", "Missing document ID")
		return
	}
	docID := parts[6]

	// Touch access
	_ = h.shares.TouchAccess(r.Context(), st.ID)

	// Load project to get tenantID
	pr, err := h.projs.Get(r.Context(), projectID)
	if err != nil || pr == nil {
		WriteNotFound(w, "PROJECT_NOT_FOUND")
		return
	}

	// Get document
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil {
		WriteNotFound(w, "Document not found")
		return
	}

	// Verify document belongs to the shared project
	if cd.ProjectID != projectID || cd.TenantID != pr.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}

	// Get document content
	var content string
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" && cd.ArtifactConfig != nil {
		content = assembleArtifactMarkdownFromConfig(cd.ArtifactConfig)
		if content == "" {
			WriteBadRequest(w, "NO_CONTENT", "Document has no content")
			return
		}
	} else {
		// Try to get full extracted text
		fullText, err := getFullExtractedTextFromCoreDoc(r.Context(), cd, h.storage)
		if err == nil && fullText != "" {
			content = fullText
		} else if cd.ExtractedText != nil && *cd.ExtractedText != "" {
			content = *cd.ExtractedText
		} else if cd.StoragePath != nil && strings.HasPrefix(*cd.StoragePath, "gs://") && h.storage != nil {
			bucket, object := splitGsPath(*cd.StoragePath)
			data, err := h.storage.ReadObject(r.Context(), bucket, object)
			if err != nil {
				WriteInternalError(w, "Failed to read document content")
				return
			}
			content = string(data)
		} else {
			WriteBadRequest(w, "NO_CONTENT", "Document has no readable content")
			return
		}
	}

	// Build public response
	type PubDocDetail struct {
		ID          string  `json:"id"`
		Filename    string  `json:"filename"`
		ContentType string  `json:"contentType"`
		Description *string `json:"description,omitempty"`
		SizeBytes   *int64  `json:"sizeBytes,omitempty"`
		DocType     *string `json:"docType,omitempty"`
		Content     string  `json:"content"`
		CreatedAt   string  `json:"createdAt"`
		UpdatedAt   string  `json:"updatedAt"`
	}

	pubDoc := PubDocDetail{
		ID:          cd.ID,
		Filename:    cd.Filename,
		ContentType: cd.ContentType,
		Description: cd.Description,
		SizeBytes:   cd.SizeBytes,
		DocType:     cd.DocType,
		Content:     content,
		CreatedAt:   cd.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   cd.UpdatedAt.Format(time.RFC3339),
	}

	WriteJSON(w, http.StatusOK, pubDoc)
}

// DownloadPublicShareDoc handles GET /api/v1/public/share/{token}/docs/{docId}/download
func (h *ShareHandler) DownloadPublicShareDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	if h.coreDocs == nil || h.storage == nil {
		WriteInternalError(w, "Downloads not configured")
		return
	}

	st, projectID, err := h.validateShareToken(r)
	if err != nil {
		if err.Error() == "token not found" {
			WriteNotFound(w, "NOT_FOUND")
		} else if err.Error() == "token expired or revoked" {
			WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "Token expired or revoked", nil)
		} else {
			WriteBadRequest(w, "INVALID_PATH", "Invalid token")
		}
		return
	}

	// Extract docID from path
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 8 {
		WriteBadRequest(w, "INVALID_PATH", "Missing document ID")
		return
	}
	docID := parts[6]

	// Touch access
	_ = h.shares.TouchAccess(r.Context(), st.ID)

	// Load project to get tenantID
	pr, err := h.projs.Get(r.Context(), projectID)
	if err != nil || pr == nil {
		WriteNotFound(w, "PROJECT_NOT_FOUND")
		return
	}

	// Get document
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil {
		WriteNotFound(w, "Document not found")
		return
	}

	// Verify document belongs to the shared project
	if cd.ProjectID != projectID || cd.TenantID != pr.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}

	// Handle dynamic artifacts: assemble from sections if no storage
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" && cd.ArtifactConfig != nil {
		var data []byte
		if cd.StoragePath != nil && strings.HasPrefix(*cd.StoragePath, "gs://") && h.storage != nil {
			bucket, object := splitGsPath(*cd.StoragePath)
			var readErr error
			data, readErr = h.storage.ReadObject(r.Context(), bucket, object)
			if readErr != nil {
				markdownContent := assembleArtifactMarkdownFromConfig(cd.ArtifactConfig)
				data = []byte(markdownContent)
			}
		} else {
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

// ExportPublicShareDocToDocx handles POST /api/v1/public/share/{token}/docs/{docId}/export
func (h *ShareHandler) ExportPublicShareDocToDocx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.coreDocs == nil {
		WriteInternalError(w, "Core docs not configured")
		return
	}

	st, projectID, err := h.validateShareToken(r)
	if err != nil {
		if err.Error() == "token not found" {
			WriteNotFound(w, "NOT_FOUND")
		} else if err.Error() == "token expired or revoked" {
			WriteError(w, http.StatusGone, "TOKEN_EXPIRED", "Token expired or revoked", nil)
		} else {
			WriteBadRequest(w, "INVALID_PATH", "Invalid token")
		}
		return
	}

	// Extract docID from path
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 8 {
		WriteBadRequest(w, "INVALID_PATH", "Missing document ID")
		return
	}
	docID := parts[6]

	// Touch access
	_ = h.shares.TouchAccess(r.Context(), st.ID)

	// Load project to get tenantID
	pr, err := h.projs.Get(r.Context(), projectID)
	if err != nil || pr == nil {
		WriteNotFound(w, "PROJECT_NOT_FOUND")
		return
	}

	// Get document
	cd, err := h.coreDocs.Get(r.Context(), docID)
	if err != nil || cd == nil {
		WriteNotFound(w, "Document not found")
		return
	}

	// Verify document belongs to the shared project
	if cd.ProjectID != projectID || cd.TenantID != pr.TenantID {
		WriteNotFound(w, "Document not found")
		return
	}

	// Get markdown content
	var markdownContent string
	if cd.DocType != nil && *cd.DocType == "dynamic_artifact" && cd.ArtifactConfig != nil {
		markdownContent = assembleArtifactMarkdownFromConfig(cd.ArtifactConfig)
		if markdownContent == "" {
			WriteBadRequest(w, "NO_CONTENT", "Artifact has no content")
			return
		}
	} else {
		fullText, err := getFullExtractedTextFromCoreDoc(r.Context(), cd, h.storage)
		if err == nil && fullText != "" {
			markdownContent = fullText
		} else if cd.ExtractedText != nil && *cd.ExtractedText != "" {
			markdownContent = *cd.ExtractedText
		} else if cd.StoragePath != nil && strings.HasPrefix(*cd.StoragePath, "gs://") && h.storage != nil {
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
	}

	// Convert markdown to DOCX using Pandoc
	docxData, err := convertMarkdownToDocx(markdownContent)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("Failed to convert markdown to DOCX: %v", err)
		}
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
