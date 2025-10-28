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

// WithInteractions wires interactions repo
func (h *DiscoveryHandler) WithInteractions(ir *repo.InteractionsRepository) *DiscoveryHandler {
	h.interactionsRepo = ir
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

	// Resolve via rag_source_projects (new linkage) exclusively
	if h.sourceProjectsRepo == nil {
		WriteInternalError(w, "Source-project linking not configured")
		return
	}
	var out []map[string]any
	status := "active"
	links, err := h.sourceProjectsRepo.ListByProject(r.Context(), authInfo.TenantID, projectID, &status)
	if err != nil {
		h.logger.Printf("ListByProject (sourceProjects) error: %v", err)
		WriteInternalError(w, "Failed to list sources")
		return
	}

	// Optionally prefetch profiles and index by dataSourceId
	profilesBySource := map[string]any{}
	if h.profilesRepo != nil {
		if profs, err := h.profilesRepo.ListByProject(r.Context(), authInfo.TenantID, projectID, h.dataSourcesRepo); err == nil {
			for _, sp := range profs {
				if sp == nil || strings.TrimSpace(sp.DataSourceID) == "" {
					continue
				}
				// Frontend expects `source.profile` to be the inner profile payload (not wrapper)
				profilesBySource[sp.DataSourceID] = sp.Profile
			}
		}
	}
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
		if p, ok := profilesBySource[ds.ID]; ok {
			m["profile"] = p
		}
		out = append(out, m)
	}
	WriteJSON(w, http.StatusOK, out)
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
		// If Firestore index not ready or other transient failures, return empty list instead of 500 to avoid UX flash
		// Common Firestore error for missing composite index: FailedPrecondition
		// We cannot import status package here; fallback to string check
		em := strings.ToLower(err.Error())
		if strings.Contains(em, "failedprecondition") || strings.Contains(em, "index") || strings.Contains(em, "unavailable") {
			WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}
		WriteInternalError(w, "Failed to list interactions")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateProjectInteraction handles POST /api/v1/projects/{id}/interactions
func (h *DiscoveryHandler) CreateProjectInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.interactionsRepo == nil {
		WriteInternalError(w, "Interactions repo not configured")
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
	var body struct {
		Kind    string          `json:"kind"`
		Content string          `json:"content"`
		Summary *string         `json:"summary,omitempty"`
		Model   *string         `json:"model,omitempty"`
		Usage   *repo.UsageMeta `json:"usage,omitempty"`
		// Optional extended fields (safe defaults if missing)
		Subkind          *string                    `json:"subkind,omitempty"`
		ContentSanitized *string                    `json:"contentSanitized,omitempty"`
		Author           *repo.ParticipantRef       `json:"author,omitempty"`
		Recipients       []string                   `json:"recipients,omitempty"`
		ReplyTo          *string                    `json:"replyTo,omitempty"`
		AgentProfile     *repo.AgentProfileSnapshot `json:"agentProfile,omitempty"`
		Evaluations      map[string]any             `json:"evaluations,omitempty"`
		Metadata         *repo.InteractionMetadata  `json:"metadata,omitempty"`
		Relations        []struct {
			Type     string `json:"type" firestore:"type"`
			TargetID string `json:"targetId" firestore:"targetId"`
		} `json:"relations,omitempty"`
		Language                 *string `json:"language,omitempty"`
		SystemInstructionApplied *string `json:"systemInstructionApplied,omitempty"`
		GenerationParams         *struct {
			Temperature      *float64 `json:"temperature,omitempty" firestore:"temperature,omitempty"`
			MaxOutputTokens  *int     `json:"maxOutputTokens,omitempty" firestore:"maxOutputTokens,omitempty"`
			GroundingEnabled *bool    `json:"groundingEnabled,omitempty" firestore:"groundingEnabled,omitempty"`
		} `json:"generationParams,omitempty"`
		HighRisk *struct {
			Level     string   `json:"level" firestore:"level"`
			Score     float64  `json:"score" firestore:"score"`
			Rationale *string  `json:"rationale,omitempty" firestore:"rationale,omitempty"`
			Triggers  []string `json:"triggers,omitempty" firestore:"triggers,omitempty"`
		} `json:"highRisk,omitempty"`
		OriginalMessageID *string  `json:"originalMessageId,omitempty"`
		CoreDocIDs        []string `json:"coreDocIds,omitempty"`
		SearchQueries     []string `json:"searchQueries,omitempty"`
		Sources           []string `json:"sources,omitempty"`
		// Sections (optional client-provided for future-proofing; server derives when missing)
		SectionID *string `json:"sectionId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	// normalize kind
	k := strings.ToLower(strings.TrimSpace(body.Kind))
	switch k {
	case "user", "assistant", "system", "note", "section_start", "section_end":
	default:
		k = "note"
	}
	// validate content presence only for non-section kinds
	if k != "section_start" && k != "section_end" {
		if strings.TrimSpace(body.Content) == "" {
			WriteBadRequest(w, "INVALID_JSON", "kind and content are required")
			return
		}
	}
	// Helper: compute currently open sectionId (last start not yet ended)
	resolveOpenSection := func(ctx context.Context) (string, error) {
		items, err := h.interactionsRepo.ListByProject(ctx, projectID)
		if err != nil {
			return "", err
		}
		current := ""
		for _, it := range items {
			if it == nil {
				continue
			}
			if strings.EqualFold(it.Kind, "section_start") {
				current = it.ID
			} else if strings.EqualFold(it.Kind, "section_end") {
				// end applies to explicit sectionId if present, else close current
				sid := ""
				if it.SectionID != nil {
					sid = strings.TrimSpace(*it.SectionID)
				}
				if sid == "" || sid == current {
					current = ""
				}
			}
		}
		return current, nil
	}

	// Special handling for sections
	if k == "section_start" {
		// Auto-close previous open section if exists
		if openID, err := resolveOpenSection(r.Context()); err == nil && strings.TrimSpace(openID) != "" {
			// Insert section_end referencing the open section
			_, _ = h.interactionsRepo.Create(r.Context(), &repo.Interaction{
				TenantID:  authInfo.TenantID,
				ProjectID: projectID,
				Kind:      "section_end",
				Content:   "",
				SectionID: &openID,
			})
		}
		// Create the new section_start (title in Content)
		startID, err := h.interactionsRepo.Create(r.Context(), &repo.Interaction{
			TenantID:  authInfo.TenantID,
			ProjectID: projectID,
			Kind:      "section_start",
			Content:   body.Content,
		})
		if err != nil {
			WriteInternalError(w, "Failed to create interaction")
			return
		}
		// Update its sectionId to self for easier grouping
		_ = h.interactionsRepo.Update(r.Context(), startID, map[string]interface{}{"sectionId": startID})
		WriteJSON(w, http.StatusCreated, map[string]any{"id": startID})
		return
	}
	if k == "section_end" {
		// Close current section if any
		openID, _ := resolveOpenSection(r.Context())
		if body.SectionID != nil && strings.TrimSpace(*body.SectionID) != "" {
			openID = strings.TrimSpace(*body.SectionID)
		}
		var sidPtr *string
		if strings.TrimSpace(openID) != "" {
			sid := openID
			sidPtr = &sid
		}
		endID, err := h.interactionsRepo.Create(r.Context(), &repo.Interaction{
			TenantID:  authInfo.TenantID,
			ProjectID: projectID,
			Kind:      "section_end",
			Content:   "",
			SectionID: sidPtr,
		})
		if err != nil {
			WriteInternalError(w, "Failed to create interaction")
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]any{"id": endID})
		return
	}

	// For normal interactions, attach current sectionId if any
	var sectionPtr *string
	if strings.TrimSpace(k) != "section_start" && strings.TrimSpace(k) != "section_end" {
		// Prefer client-provided sectionId, else resolve
		if body.SectionID != nil && strings.TrimSpace(*body.SectionID) != "" {
			sid := strings.TrimSpace(*body.SectionID)
			sectionPtr = &sid
		} else if openID, err := resolveOpenSection(r.Context()); err == nil && strings.TrimSpace(openID) != "" {
			sid := openID
			sectionPtr = &sid
		}
	}

	id, err := h.interactionsRepo.Create(r.Context(), &repo.Interaction{
		TenantID:                 authInfo.TenantID,
		ProjectID:                projectID,
		Kind:                     k,
		Content:                  body.Content,
		Summary:                  body.Summary,
		Model:                    body.Model,
		Usage:                    body.Usage,
		Subkind:                  body.Subkind,
		ContentSanitized:         body.ContentSanitized,
		Author:                   body.Author,
		Recipients:               body.Recipients,
		ReplyTo:                  body.ReplyTo,
		AgentProfile:             body.AgentProfile,
		Evaluations:              body.Evaluations,
		Metadata:                 body.Metadata,
		Relations:                body.Relations,
		Language:                 body.Language,
		SystemInstructionApplied: body.SystemInstructionApplied,
		GenerationParams:         body.GenerationParams,
		HighRisk:                 body.HighRisk,
		OriginalMessageID:        body.OriginalMessageID,
		CoreDocIDs:               body.CoreDocIDs,
		SearchQueries:            body.SearchQueries,
		Sources:                  body.Sources,
		SectionID:                sectionPtr,
	})
	if err != nil {
		WriteInternalError(w, "Failed to create interaction")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// DeleteProjectInteraction handles DELETE /api/v1/projects/{id}/interactions/{interactionId}
func (h *DiscoveryHandler) DeleteProjectInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is allowed", nil)
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.interactionsRepo == nil {
		WriteInternalError(w, "Interactions repo not configured")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project and Interaction IDs are required")
		return
	}
	projectID := parts[3]
	interactionID := parts[5]
	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	// optional: verify interaction belongs to project
	if it, err := h.interactionsRepo.Get(r.Context(), interactionID); err == nil {
		if it.ProjectID != projectID || it.TenantID != authInfo.TenantID {
			WriteForbidden(w, "Access denied")
			return
		}
	}
	if err := h.interactionsRepo.DeleteByID(r.Context(), interactionID); err != nil {
		WriteInternalError(w, "Failed to delete interaction")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// UpdateProjectInteraction handles PATCH /api/v1/projects/{id}/interactions/{interactionId}
func (h *DiscoveryHandler) UpdateProjectInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PATCH is allowed", nil)
		return
	}
	authInfo := GetAuthInfo(r.Context())
	if authInfo == nil {
		WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.interactionsRepo == nil {
		WriteInternalError(w, "Interactions repo not configured")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		WriteBadRequest(w, "INVALID_PATH", "Project and Interaction IDs are required")
		return
	}
	projectID := parts[3]
	interactionID := parts[5]
	project, err := h.projectsRepo.Get(r.Context(), projectID)
	if err != nil || project.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	// verify interaction belongs to project and tenant
	it, err := h.interactionsRepo.Get(r.Context(), interactionID)
	if err != nil || it == nil || it.ProjectID != projectID || it.TenantID != authInfo.TenantID {
		WriteForbidden(w, "Access denied")
		return
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		WriteBadRequest(w, "INVALID_JSON", "Failed to parse body")
		return
	}
	if len(payload) == 0 {
		WriteBadRequest(w, "VALIDATION_ERROR", "No fields to update")
		return
	}
	// Whitelist fields allowed for update
	allowed := map[string]struct{}{
		"kind": {}, "subkind": {}, "content": {}, "contentSanitized": {}, "summary": {},
		"model": {}, "usage": {}, "author": {}, "recipients": {}, "replyTo": {},
		"agentProfile": {}, "evaluations": {}, "metadata": {}, "relations": {}, "language": {},
		"systemInstructionApplied": {}, "generationParams": {}, "highRisk": {},
		"originalMessageId": {}, "coreDocIds": {}, "searchQueries": {}, "sources": {},
	}
	updates := map[string]interface{}{}
	for k, v := range payload {
		if _, ok := allowed[k]; ok {
			updates[k] = v
		}
	}
	if len(updates) == 0 {
		WriteBadRequest(w, "VALIDATION_ERROR", "No updatable fields provided")
		return
	}
	if err := h.interactionsRepo.Update(r.Context(), interactionID, updates); err != nil {
		WriteInternalError(w, "Failed to update interaction")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
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
