package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// DiscoveryHandler handles discovery-related HTTP requests
type DiscoveryHandler struct {
	projectsRepo     *repo.ProjectsRepository
	dataSourcesRepo  *repo.DataSourcesRepository
	interactionsRepo *repo.InteractionsRepository
	profilesRepo     *repo.SourceProfilesRepository
	logger           *log.Logger
	orchestrator     *discovery.Orchestrator
	// rag deps
	docsRepo           *repo.DocumentsRepository
	searchRepo         *repo.SearchEventsRepository
	sourceProjectsRepo *repo.SourceProjectsRepository
	chunksRepo         *repo.ChunksRepository
}

// NewDiscoveryHandler creates a new DiscoveryHandler
func NewDiscoveryHandler(
	projectsRepo *repo.ProjectsRepository,
	dataSourcesRepo *repo.DataSourcesRepository,
	logger *log.Logger,
) *DiscoveryHandler {
	return &DiscoveryHandler{
		projectsRepo:    projectsRepo,
		dataSourcesRepo: dataSourcesRepo,
		logger:          logger,
	}
}

// WithOrchestrator wires orchestrator instance for execution
func (h *DiscoveryHandler) WithOrchestrator(o *discovery.Orchestrator) *DiscoveryHandler {
	h.orchestrator = o
	return h
}

// WithDocuments wires documents repo for future endpoints
func (h *DiscoveryHandler) WithDocuments(d *repo.DocumentsRepository) *DiscoveryHandler {
	h.docsRepo = d
	return h
}

// WithSearchEvents wires search events repository
func (h *DiscoveryHandler) WithSearchEvents(sr *repo.SearchEventsRepository) *DiscoveryHandler {
	h.searchRepo = sr
	return h
}

// WithSourceProjects wires source-project links repository
func (h *DiscoveryHandler) WithSourceProjects(sp *repo.SourceProjectsRepository) *DiscoveryHandler {
	h.sourceProjectsRepo = sp
	return h
}

// WithChunks wires chunks repository for vector search
func (h *DiscoveryHandler) WithChunks(cr *repo.ChunksRepository) *DiscoveryHandler {
	h.chunksRepo = cr
	return h
}

// SearchDocuments handles GET /api/v1/projects/{id}/search?language=&documentType=&limit=
func (h *DiscoveryHandler) SearchDocuments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	if _, err := h.projectsRepo.Get(r.Context(), projectID); err != nil {
		WriteNotFound(w, "Project not found")
		return
	}
	var sourceIDs []string
	weightsBySource := map[string]float64{}
	if h.sourceProjectsRepo != nil {
		status := "active"
		if sps, err := h.sourceProjectsRepo.ListByProject(r.Context(), auth.TenantID, projectID, &status); err == nil {
			for _, sp := range sps {
				sourceIDs = append(sourceIDs, sp.SourceID)
				if sp.Weights != nil {
					if wv, ok := sp.Weights["default"]; ok {
						weightsBySource[sp.SourceID] = wv
					}
				}
			}
		}
	}
	if len(sourceIDs) == 0 {
		// fallback to legacy linkage
		sources, err := h.dataSourcesRepo.ListByProject(r.Context(), auth.TenantID, projectID)
		if err != nil || len(sources) == 0 {
			WriteJSON(w, http.StatusOK, []any{})
			return
		}
		for _, s := range sources {
			sourceIDs = append(sourceIDs, s.ID)
		}
	}

	q := r.URL.Query()
	var language *string
	if v := q.Get("language"); v != "" {
		language = &v
	}
	var docType *string
	if v := q.Get("documentType"); v != "" {
		docType = &v
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	items, err := h.docsRepo.ListBySources(r.Context(), auth.TenantID, sourceIDs, limit, language, docType)
	if err != nil {
		WriteInternalError(w, "Search failed")
		return
	}
	// optional time window filters
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filtered := items[:0]
			for _, d := range items {
				if d.OriginPublishedAt == nil || d.OriginPublishedAt.After(t) {
					filtered = append(filtered, d)
				}
			}
			items = filtered
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filtered := items[:0]
			for _, d := range items {
				if d.OriginPublishedAt == nil || d.OriginPublishedAt.Before(t) {
					filtered = append(filtered, d)
				}
			}
			items = filtered
		}
	}
	// optional categories filter: comma-separated labels in ?categories=label1,label2
	if v := q.Get("categories"); strings.TrimSpace(v) != "" {
		wanted := map[string]struct{}{}
		for _, lab := range strings.Split(v, ",") {
			lab = strings.TrimSpace(lab)
			if lab != "" {
				wanted[strings.ToLower(lab)] = struct{}{}
			}
		}
		if len(wanted) > 0 {
			filtered := items[:0]
			for _, d := range items {
				match := false
				for _, c := range d.Categories {
					if c == nil {
						continue
					}
					if lblAny, ok := c["label"]; ok {
						if lbl, ok2 := lblAny.(string); ok2 {
							if _, ok3 := wanted[strings.ToLower(lbl)]; ok3 {
								match = true
								break
							}
						}
					}
				}
				if match {
					filtered = append(filtered, d)
				}
			}
			items = filtered
		}
	}
	// record search event (best-effort)
	if h.searchRepo != nil {
		filters := map[string]any{}
		if language != nil {
			filters["language"] = *language
		}
		if docType != nil {
			filters["documentType"] = *docType
		}
		var ids []string
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		_, _ = h.searchRepo.Create(r.Context(), &models.SearchEvent{TenantID: auth.TenantID, ProjectID: projectID, Query: "documents.search", Filters: filters, ResultsDocIDs: ids})
	}
	// recency + per-source weight boost ordering
	type scored struct {
		d     *models.Document
		score float64
	}
	scoredItems := make([]scored, 0, len(items))
	nowT := time.Now().UTC()
	for _, d := range items {
		ageDays := 365.0
		if d.OriginPublishedAt != nil {
			ageDays = nowT.Sub(*d.OriginPublishedAt).Hours() / 24.0
			if ageDays < 0 {
				ageDays = 0
			}
		}
		base := 1.0 / (1.0 + ageDays)
		wv := weightsBySource[d.SourceID]
		scoredItems = append(scoredItems, scored{d: d, score: base + wv})
	}
	sort.Slice(scoredItems, func(i, j int) bool { return scoredItems[i].score > scoredItems[j].score })
	out := make([]*models.Document, 0, len(scoredItems))
	for _, s := range scoredItems {
		out = append(out, s.d)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	// publish ground.refresh for top 5
	top := 5
	if len(out) < top {
		top = len(out)
	}
	for i := 0; i < top; i++ {
		if h.orchestrator != nil && h.orchestrator.Publish() != nil {
			_ = h.orchestrator.Publish().PublishJSON(r.Context(), realtime.EventGroundRefresh, auth.TenantID, map[string]any{"tenantId": auth.TenantID, "sourceId": out[i].SourceID, "documentId": out[i].ID})
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// WithInteractions wires interactions repo
func (h *DiscoveryHandler) WithInteractions(ir *repo.InteractionsRepository) *DiscoveryHandler {
	h.interactionsRepo = ir
	return h
}

// WithProfiles wires SourceProfiles repository (optional)
func (h *DiscoveryHandler) WithProfiles(sp *repo.SourceProfilesRepository) *DiscoveryHandler {
	h.profilesRepo = sp
	return h
}

// IngestProject handles POST /api/v1/projects/{id}/ingest
func (h *DiscoveryHandler) IngestProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	if h.orchestrator == nil {
		WriteInternalError(w, "Ingest not configured")
		return
	}
	go func() { _ = h.orchestrator.IngestProjectSources(context.Background(), authInfo.TenantID, projectID) }()
	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

// VectorSearchRequest for POST /api/v1/projects/{id}/vector-search
type VectorSearchRequest struct {
	Query        string  `json:"query"`
	TopK         int     `json:"topK"`
	Language     *string `json:"language,omitempty"`
	DocumentType *string `json:"documentType,omitempty"`
	Since        *string `json:"since,omitempty"`
	Until        *string `json:"until,omitempty"`
}

// VectorSearch performs KNN over chunks using the configured VectorIndex
func (h *DiscoveryHandler) VectorSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := parts[3]
	if _, err := h.projectsRepo.Get(r.Context(), projectID); err != nil {
		WriteNotFound(w, "Project not found")
		return
	}
	var body VectorSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Query) == "" {
		WriteBadRequest(w, "INVALID_JSON", "query is required")
		return
	}
	if body.TopK <= 0 {
		body.TopK = 10
	}

	// resolve sources and weights
	status := "active"
	var sourceIDs []string
	weightsBySource := map[string]float64{}
	if h.sourceProjectsRepo != nil {
		if sps, err := h.sourceProjectsRepo.ListByProject(r.Context(), auth.TenantID, projectID, &status); err == nil {
			for _, sp := range sps {
				sourceIDs = append(sourceIDs, sp.SourceID)
				if sp.Weights != nil {
					if wv, ok := sp.Weights["default"]; ok {
						weightsBySource[sp.SourceID] = wv
					}
				}
			}
		}
	}
	if len(sourceIDs) == 0 {
		if ds, err := h.dataSourcesRepo.ListByProject(r.Context(), auth.TenantID, projectID); err == nil {
			for _, s := range ds {
				sourceIDs = append(sourceIDs, s.ID)
			}
		}
	}
	if len(sourceIDs) == 0 {
		WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}

	// embed query
	exec := h.orchestrator.Exec()
	if exec == nil {
		WriteInternalError(w, "Embedding executor not configured")
		return
	}
	emb := ai.NewOpenAIEmbedding(exec, "text-embedding-3-small")
	qvecs, err := emb.EmbedTexts(r.Context(), []string{body.Query})
	if err != nil || len(qvecs) == 0 {
		WriteInternalError(w, "Embedding failed")
		return
	}
	qv := qvecs[0]

	// vector index query (using FirestoreVectorIndex adapter)
	if h.chunksRepo == nil {
		WriteInternalError(w, "Chunks repository not available")
		return
	}
	index := ai.NewFirestoreVectorIndex(h.chunksRepo)
	ids, err := index.Query(r.Context(), qv, body.TopK, map[string]any{"tenantId": auth.TenantID, "sourceIds": sourceIDs})
	if err != nil {
		WriteInternalError(w, "Vector query failed")
		return
	}
	chunks, err := h.chunksRepo.GetByIDs(r.Context(), ids)
	if err != nil {
		WriteInternalError(w, "Failed to load chunks")
		return
	}
	var sinceT, untilT *time.Time
	if body.Since != nil && strings.TrimSpace(*body.Since) != "" {
		if t, err := time.Parse(time.RFC3339, *body.Since); err == nil {
			sinceT = &t
		}
	}
	if body.Until != nil && strings.TrimSpace(*body.Until) != "" {
		if t, err := time.Parse(time.RFC3339, *body.Until); err == nil {
			untilT = &t
		}
	}
	type scored struct {
		c     *models.Chunk
		score float64
		doc   *models.Document
	}
	var scoredItems []scored
	nowT := time.Now().UTC()
	for _, c := range chunks {
		if body.DocumentType != nil && c.Attrs != nil {
			if v, ok := c.Attrs["documentType"].(string); ok {
				if strings.TrimSpace(v) != strings.TrimSpace(*body.DocumentType) {
					continue
				}
			}
		}
		doc, _ := h.docsRepo.GetByID(r.Context(), c.DocID)
		if body.Language != nil && doc != nil && strings.TrimSpace(doc.Language) != "" {
			if !strings.EqualFold(strings.TrimSpace(doc.Language), strings.TrimSpace(*body.Language)) {
				continue
			}
		}
		if body.DocumentType != nil && doc != nil && strings.TrimSpace(doc.DocumentType) != "" {
			if strings.TrimSpace(doc.DocumentType) != strings.TrimSpace(*body.DocumentType) {
				continue
			}
		}
		if (sinceT != nil || untilT != nil) && doc != nil && doc.OriginPublishedAt != nil {
			if sinceT != nil && doc.OriginPublishedAt.Before(*sinceT) {
				continue
			}
			if untilT != nil && doc.OriginPublishedAt.After(*untilT) {
				continue
			}
		}
		// similarity + recency + per-source weight boost
		sim := 0.0
		if len(c.Embedding) > 0 {
			sim = ai.CosineSimilarity(qv, c.Embedding)
		}
		ageDays := 365.0
		if doc != nil && doc.OriginPublishedAt != nil {
			ageDays = nowT.Sub(*doc.OriginPublishedAt).Hours() / 24.0
			if ageDays < 0 {
				ageDays = 0
			}
		}
		recency := 1.0 / (1.0 + ageDays)
		weight := 0.0
		// weightsBySource computed earlier in handler
		if wv, ok := weightsBySource[c.SourceID]; ok {
			weight = wv
		}
		scoredItems = append(scoredItems, scored{c: c, doc: doc, score: sim + recency + weight})
	}
	sort.Slice(scoredItems, func(i, j int) bool { return scoredItems[i].score > scoredItems[j].score })
	if body.TopK > 0 && len(scoredItems) > body.TopK {
		scoredItems = scoredItems[:body.TopK]
	}
	out := make([]map[string]any, 0, len(scoredItems))
	for _, s := range scoredItems {
		out = append(out, map[string]any{"docId": s.c.DocID, "sourceId": s.c.SourceID, "position": s.c.Position, "score": s.score, "text": s.c.Text})
	}
	// publish ground.refresh for top 3
	top := 3
	if len(out) < top {
		top = len(out)
	}
	for i := 0; i < top; i++ {
		if h.orchestrator != nil && h.orchestrator.Publish() != nil {
			_ = h.orchestrator.Publish().PublishJSON(r.Context(), realtime.EventGroundRefresh, auth.TenantID, map[string]any{"tenantId": auth.TenantID, "sourceId": out[i]["sourceId"], "documentId": out[i]["docId"]})
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ExecuteDiscoveryRequest represents the request body for discovery execution
type ExecuteDiscoveryRequest struct {
	ProjectID string  `json:"projectId"`
	QueryText string  `json:"queryText"`
	Language  *string `json:"language,omitempty"`
	Sources   struct {
		Web      []string `json:"web"`
		Telegram []string `json:"telegram"`
	} `json:"sources"`
	CorrelationID *string `json:"correlationId,omitempty"`
}

// ExecuteDiscovery handles POST /api/v1/sources-discovery/execute
func (h *DiscoveryHandler) ExecuteDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}

	var req ExecuteDiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse request body")
		return
	}

	// If projectID is provided, verify it exists and belongs to user's tenant
	if req.ProjectID != "" {
		project, err := h.projectsRepo.Get(r.Context(), req.ProjectID)
		if err != nil {
			h.logger.Printf("Project not found: %s", req.ProjectID)
			WriteNotFound(w, "Project not found")
			return
		}

		if project.TenantID != authInfo.TenantID {
			WriteForbidden(w, "Access denied")
			return
		}

		// Update project's lastInteractionAt
		if err := h.projectsRepo.UpdateLastInteraction(r.Context(), req.ProjectID); err != nil {
			h.logger.Printf("Failed to update project interaction time: %v", err)
			// Non-fatal, continue
		}

		// Persist user query as an interaction for chat history (when project is known)
		if h.interactionsRepo != nil && strings.TrimSpace(req.QueryText) != "" {
			_, _ = h.interactionsRepo.Create(r.Context(), &repo.Interaction{
				TenantID:  authInfo.TenantID,
				ProjectID: req.ProjectID,
				Kind:      "discovery.query",
				Content:   req.QueryText,
			})
		}
	}

	// Generate correlation ID if not provided
	correlationID := ""
	if req.CorrelationID != nil {
		correlationID = *req.CorrelationID
	} else {
		// Generate a simple ID (in production, use UUID)
		correlationID = generateCorrelationID()
	}

	// Parse and canonicalize sources
	var webSources []string
	var telegramSources []string

	for _, url := range req.Sources.Web {
		// URL canonicalization would happen here
		canonical := strings.TrimSpace(url)
		if canonical != "" {
			webSources = append(webSources, canonical)
		}
	}

	for _, handle := range req.Sources.Telegram {
		// Normalize telegram handles (remove @ if present)
		canonical := strings.TrimSpace(strings.TrimPrefix(handle, "@"))
		if canonical != "" {
			telegramSources = append(telegramSources, canonical)
		}
	}

	if len(webSources) == 0 && len(telegramSources) == 0 {
		WriteBadRequest(w, "VALIDATION_ERROR", "At least one source (web or telegram) is required")
		return
	}

	// Execute orchestrator (fire-and-forget go routine) using independent timeout-bound context
	if h.orchestrator != nil {
		go func() {
			lang := "en"
			if req.Language != nil && *req.Language != "" {
				lang = *req.Language
			}
			// Important: decouple from r.Context() so work continues after HTTP returns
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			_ = h.orchestrator.Execute(ctx, discovery.ExecuteInput{
				TenantID:    authInfo.TenantID,
				ProjectID:   req.ProjectID,
				QueryText:   req.QueryText,
				Language:    lang,
				WebSources:  webSources,
				TgHandles:   telegramSources,
				Correlation: correlationID,
				OwnerUID:    authInfo.UID,
			})
		}()
	}

	response := map[string]interface{}{
		"success":       true,
		"correlationId": correlationID,
		"message":       "Discovery task queued",
		"sourcesCount": map[string]int{
			"web":      len(webSources),
			"telegram": len(telegramSources),
		},
	}

	WriteJSON(w, http.StatusAccepted, response)
}

// ListProjectSources handles GET /api/v1/projects/{id}/datasources
func (h *DiscoveryHandler) ListProjectSources(w http.ResponseWriter, r *http.Request) {
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
	if len(pathParts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	// Verify project exists and belongs to user's tenant
	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil {
		h.logger.Printf("Project not found: %s", projectID)
		WriteNotFound(w, "Project not found")
		return
	}

	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}

	// Preferred: resolve via rag_source_projects (new linkage)
	var out []map[string]any
	if h.sourceProjectsRepo != nil {
		status := "active"
		links, err := h.sourceProjectsRepo.ListByProject(r.Context(), authInfo.TenantID, projectID, &status)
		if err != nil {
			h.logger.Printf("ListByProject (sourceProjects) error: %v", err)
		} else {
			for _, link := range links {
				if link == nil || strings.TrimSpace(link.SourceID) == "" {
					continue
				}
				ds, err := h.dataSourcesRepo.Get(r.Context(), link.SourceID)
				if err != nil || ds == nil {
					continue
				}
				m := map[string]interface{}{
					"id":            ds.ID,
					"tenantId":      ds.TenantID,
					"pk":            ds.Pk,
					"displayName":   ds.DisplayName,
					"kind":          ds.Kind,
					"status":        ds.Status,
					"researchId":    ds.ResearchID,
					"sessionId":     ds.SessionID,
					"ownerUserId":   ds.OwnerUserID,
					"sourceDetails": ds.Details,
					"lastPolledAt":  ds.LastPolledAt,
					"lastFoundAt":   ds.LastFoundAt,
					"errorCount":    ds.ErrorCount,
					"createdAt":     ds.CreatedAt,
					"updatedAt":     ds.UpdatedAt,
					"trustLevel":    ds.TrustLevel,
					"firstSeenAt":   ds.FirstSeen,
					"lastSeenAt":    ds.LastSeen,
					// new linkage metadata
					"link": map[string]any{
						"id":         link.ID,
						"projectId":  link.ProjectID,
						"sourceId":   link.SourceID,
						"status":     link.Status,
						"weights":    link.Weights,
						"syncConfig": link.SyncConfig,
						"createdAt":  link.CreatedAt,
						"updatedAt":  link.UpdatedAt,
					},
				}
				out = append(out, m)
			}
		}
	}
	if len(out) > 0 {
		WriteJSON(w, http.StatusOK, out)
		return
	}
	// Fallback: legacy lookup by projectId on core_datasources
	sources, err := h.dataSourcesRepo.ListByProject(r.Context(), authInfo.TenantID, projectID)
	if err != nil {
		h.logger.Printf("Failed to list sources for project %s: %v", projectID, err)
		WriteInternalError(w, "Failed to list sources")
		return
	}
	WriteJSON(w, http.StatusOK, sources)
}

// PublishDue exposes POST /api/v1/admin/sources/publish-due (secured via auth middleware)
func (h *DiscoveryHandler) PublishDue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	if h.orchestrator == nil || h.orchestrator.Publish() == nil {
		WriteInternalError(w, "Poller not configured")
		return
	}
	// Use the poller via webhooks handler; here we simply publish a signal event for due polling
	// In this minimal implementation, we just return OK; background ticker handles due work.
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ListProjectInteractions handles GET /api/v1/projects/{id}/interactions
func (h *DiscoveryHandler) ListProjectInteractions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil {
		WriteNotFound(w, "Project not found")
		return
	}
	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	if h.interactionsRepo == nil {
		WriteInternalError(w, "Interactions not configured")
		return
	}
	items, err := h.interactionsRepo.ListByProject(r.Context(), projectID)
	if err != nil {
		WriteInternalError(w, "Failed to list interactions")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ChatMessage handles POST /api/v1/projects/{id}/chat
func (h *DiscoveryHandler) ChatMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Project ID is required")
		return
	}
	projectID := pathParts[3]

	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil {
		WriteNotFound(w, "Project not found")
		return
	}
	if project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var body struct {
		Message  string  `json:"message"`
		Language *string `json:"language,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	lang := "en"
	if body.Language != nil && *body.Language != "" {
		lang = *body.Language
	}
	if strings.TrimSpace(body.Message) == "" {
		WriteBadRequest(w, "VALIDATION_ERROR", "message is required")
		return
	}
	if h.orchestrator == nil {
		WriteInternalError(w, "Chat is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	answer, err := h.orchestrator.Chat(ctx, authInfo.TenantID, projectID, body.Message, lang)
	if err != nil {
		WriteInternalError(w, "Failed to process chat message")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"answer": answer})
}

// generateCorrelationID generates a simple correlation ID
// In production, use UUID
func generateCorrelationID() string {
	// Simple implementation for now
	return fmt.Sprintf("disc_%d", time.Now().UnixNano())
}
