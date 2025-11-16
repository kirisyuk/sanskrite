package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// CoreDocsToolExecutor executes document-related tools for AI
type CoreDocsToolExecutor struct {
	coreDocs     *repo.CoreDocsRepo
	interactions *repo.InteractionsRepository
	docsRepo     *repo.DocumentsRepository
	chunksRepo   *repo.ChunksRepository
	emb          ai.EmbeddingProvider
	publisher    Publisher
	tenantID     string
	projectID    string
	streamID     string
	logger       *log.Logger
	// Active edit sessions: docId -> session
	editSessions map[string]*DocumentEditSession
}

// DocumentEditSession tracks an active document editing session
type DocumentEditSession struct {
	DocID          string
	BaseContent    string
	CurrentContent string
	InteractionID  string
	CreatedAt      time.Time
}

// NewCoreDocsToolExecutor creates a new executor for document tools
func NewCoreDocsToolExecutor(
	coreDocs *repo.CoreDocsRepo,
	interactions *repo.InteractionsRepository,
	docsRepo *repo.DocumentsRepository,
	chunksRepo *repo.ChunksRepository,
	emb ai.EmbeddingProvider,
	publisher Publisher,
	tenantID, projectID, streamID string,
	logger *log.Logger,
) *CoreDocsToolExecutor {
	return &CoreDocsToolExecutor{
		coreDocs:     coreDocs,
		interactions: interactions,
		docsRepo:     docsRepo,
		chunksRepo:   chunksRepo,
		emb:          emb,
		publisher:    publisher,
		tenantID:     tenantID,
		projectID:    projectID,
		streamID:     streamID,
		logger:       logger,
		editSessions: make(map[string]*DocumentEditSession),
	}
}

// IsAllowed checks if a tool is allowed
func (e *CoreDocsToolExecutor) IsAllowed(name string) bool {
	allowed := map[string]bool{
		"search_coredocs":         true,
		"create_coredoc":          true,
		"update_coredoc":          true,
		"update_coredoc_fragment":  true,
		"list_coredocs":            true,
		"get_coredoc":             true,
	}
	return allowed[name]
}

// Execute executes a tool call
func (e *CoreDocsToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "search_coredocs":
		return e.searchCoreDocs(ctx, args)
	case "create_coredoc":
		return e.createCoreDoc(ctx, args)
	case "update_coredoc":
		return e.updateCoreDoc(ctx, args)
	case "update_coredoc_fragment":
		return e.updateCoreDocFragment(ctx, args)
	case "list_coredocs":
		return e.listCoreDocs(ctx, args)
	case "get_coredoc":
		return e.getCoreDoc(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// indexCoreDoc indexes a CoreDoc for vector search
func (e *CoreDocsToolExecutor) indexCoreDoc(ctx context.Context, coreDoc *models.CoreDoc) error {
	if e.docsRepo == nil || e.chunksRepo == nil || e.emb == nil {
		return fmt.Errorf("RAG components not configured")
	}

	if coreDoc.ExtractedText == nil || *coreDoc.ExtractedText == "" {
		return nil // Nothing to index
	}

	text := *coreDoc.ExtractedText
	if strings.TrimSpace(text) == "" {
		return nil
	}

	// Virtual sourceID for CoreDocs
	virtualSourceID := fmt.Sprintf("core_docs_%s", coreDoc.ProjectID)

	// Check if Document already exists for this CoreDoc
	existingDoc, err := e.docsRepo.GetByCoreDocID(ctx, e.tenantID, coreDoc.ID)
	if err == nil && existingDoc != nil {
		// Update existing document
		existingDoc.FullText = text
		existingDoc.Title = coreDoc.Filename
		existingDoc.ContentType = coreDoc.ContentType
		existingDoc.UpdatedAt = time.Now().UTC()
		existingDoc.Status = "new" // Reset to trigger re-indexing
		if err := e.docsRepo.Upsert(ctx, existingDoc); err != nil {
			return fmt.Errorf("failed to update document: %w", err)
		}
		// Delete old chunks
		if err := e.chunksRepo.DeleteByDoc(ctx, e.tenantID, existingDoc.ID); err != nil {
			e.logger.Printf("Warning: failed to delete old chunks: %v", err)
		}
	} else {
		// Create new Document
		now := time.Now().UTC()
		doc := &models.Document{
			TenantID:     e.tenantID,
			SourceID:     virtualSourceID,
			ExternalID:   coreDoc.ID,
			Title:        coreDoc.Filename,
			FullText:     text,
			ContentType:  coreDoc.ContentType,
			DocumentType: "coredoc",
			Status:       "new",
			ContentHash:  repo.HashContent(text),
			IngestedAt:   now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Attrs: map[string]any{
				"coreDocId":  coreDoc.ID,
				"projectId":  coreDoc.ProjectID,
				"filename":   coreDoc.Filename,
				"contentType": coreDoc.ContentType,
			},
		}
		docID, err := e.docsRepo.Create(ctx, doc)
		if err != nil {
			return fmt.Errorf("failed to create document: %w", err)
		}
		doc.ID = docID
		existingDoc = doc
	}

	// Chunk and embed
	parts := discovery.ChunkText(text, 1200)
	if len(parts) == 0 {
		return nil
	}

	vectors, err := e.emb.EmbedTexts(ctx, parts)
	if err != nil {
		return fmt.Errorf("failed to create embeddings: %w", err)
	}

	now := time.Now().UTC()
	for i := range parts {
		ch := &models.Chunk{
			TenantID:         e.tenantID,
			DocID:            existingDoc.ID,
			SourceID:        virtualSourceID,
			Position:         i,
			Text:             parts[i],
			Embedding:        vectors[i],
			EmbeddingModel:   e.emb.ModelID(),
			EmbeddingVersion: "v1",
			Attrs: map[string]any{
				"coreDocId":   coreDoc.ID,
				"projectId":   coreDoc.ProjectID,
				"documentType": "coredoc",
			},
			ContentHash: repo.HashContent(parts[i]),
			CreatedAt:   now,
		}
		if _, err := e.chunksRepo.Create(ctx, ch); err != nil {
			return fmt.Errorf("failed to create chunk: %w", err)
		}
	}

	// Mark document as embedded
	existingDoc.Status = "embedded"
	if err := e.docsRepo.Upsert(ctx, existingDoc); err != nil {
		return fmt.Errorf("failed to mark document as embedded: %w", err)
	}

	return nil
}

// searchCoreDocs performs semantic search across CoreDocs
func (e *CoreDocsToolExecutor) searchCoreDocs(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Query string `json:"query"`
		Limit *int   `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	limit := 5
	if params.Limit != nil && *params.Limit > 0 && *params.Limit <= 20 {
		limit = *params.Limit
	}

	// Embed query
	qvecs, err := e.emb.EmbedTexts(ctx, []string{params.Query})
	if err != nil || len(qvecs) == 0 {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	qv := qvecs[0]

	// Virtual sourceID for this project
	virtualSourceID := fmt.Sprintf("core_docs_%s", e.projectID)

	// Vector search
	index := ai.NewFirestoreVectorIndex(e.chunksRepo)
	chunkIDs, err := index.Query(ctx, qv, limit*2, map[string]any{
		"tenantId":  e.tenantID,
		"sourceIds": []string{virtualSourceID},
	})
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	chunks, err := e.chunksRepo.GetByIDs(ctx, chunkIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	// Group by CoreDoc and collect relevant fragments
	type docResult struct {
		DocID      string   `json:"docId"`
		Filename   string   `json:"filename"`
		Fragments  []string `json:"fragments"`
		MatchCount int      `json:"matchCount"`
	}
	docMap := make(map[string]*docResult)

	for _, chunk := range chunks {
		coreDocID := ""
		if chunk.Attrs != nil {
			if id, ok := chunk.Attrs["coreDocId"].(string); ok {
				coreDocID = id
			}
		}
		if coreDocID == "" {
			continue
		}

		if docMap[coreDocID] == nil {
			docMap[coreDocID] = &docResult{
				DocID:     coreDocID,
				Fragments: make([]string, 0),
			}
		}
		docMap[coreDocID].Fragments = append(docMap[coreDocID].Fragments, chunk.Text)
		docMap[coreDocID].MatchCount++
	}

	// Load CoreDoc metadata
	results := make([]docResult, 0, len(docMap))
	for coreDocID, result := range docMap {
		coreDoc, err := e.coreDocs.Get(ctx, coreDocID)
		if err != nil {
			continue
		}
		if coreDoc.ProjectID != e.projectID {
			continue
		}
		result.Filename = coreDoc.Filename
		results = append(results, *result)
	}

	// Sort by match count (descending) and limit
	if len(results) > limit {
		// Simple sort by match count
		for i := 0; i < len(results)-1; i++ {
			for j := i + 1; j < len(results); j++ {
				if results[j].MatchCount > results[i].MatchCount {
					results[i], results[j] = results[j], results[i]
				}
			}
		}
		results = results[:limit]
	}

	response := map[string]interface{}{
		"documents": results,
		"query":     params.Query,
		"count":     len(results),
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

// createCoreDoc creates a new CoreDoc
func (e *CoreDocsToolExecutor) createCoreDoc(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Filename    string  `json:"filename"`
		Content     string  `json:"content"`
		ContentType string  `json:"contentType"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.Filename == "" || params.ContentType == "" || params.Content == "" {
		return nil, fmt.Errorf("filename, contentType, and content are required")
	}

	// Persist to storage if available
	var storagePath *string
	// Note: storage would need to be passed to executor if we want to use it
	// For now, store content in ExtractedText

	now := time.Now().UTC()
	size := int64(len([]byte(params.Content)))
	status := "parsed"
	coreDoc := &models.CoreDoc{
		TenantID:      e.tenantID,
		ProjectID:     e.projectID,
		Filename:      params.Filename,
		ContentType:   params.ContentType,
		Description:   params.Description,
		SizeBytes:     &size,
		StoragePath:   storagePath,
		ExtractedText: &params.Content,
		ParseStatus:   &status,
		ParseMeta:     map[string]any{"createdFrom": "tool", "updatedAt": now.Format(time.RFC3339)},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	id, err := e.coreDocs.Create(ctx, coreDoc)
	if err != nil {
		return nil, fmt.Errorf("failed to create document: %w", err)
	}
	coreDoc.ID = id

	// Index the document
	if err := e.indexCoreDoc(ctx, coreDoc); err != nil {
		e.logger.Printf("Warning: failed to index CoreDoc %s: %v", id, err)
		// Continue anyway - document is created
	}

	// Publish WebSocket event
	if e.publisher != nil {
		_ = e.publisher.PublishJSON(ctx, realtime.EventDocProcess, e.tenantID, map[string]any{
			"projectId": e.projectID,
			"docId":     coreDoc.ID,
			"status":    "created",
			"ts":        time.Now().UnixMilli(),
		})
	}

	response := map[string]interface{}{
		"docId":    coreDoc.ID,
		"filename": coreDoc.Filename,
		"status":   "created",
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

// updateCoreDoc updates an entire CoreDoc
func (e *CoreDocsToolExecutor) updateCoreDoc(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		DocID       string  `json:"docId"`
		Content     *string `json:"content,omitempty"`
		ContentType *string `json:"contentType,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.DocID == "" {
		return nil, fmt.Errorf("docId is required")
	}

	coreDoc, err := e.coreDocs.Get(ctx, params.DocID)
	if err != nil {
		return nil, fmt.Errorf("document not found: %w", err)
	}
	if coreDoc.ProjectID != e.projectID {
		return nil, fmt.Errorf("access denied")
	}

	// Update fields
	if params.Content != nil {
		coreDoc.ExtractedText = params.Content
		size := int64(len([]byte(*params.Content)))
		coreDoc.SizeBytes = &size
	}
	if params.ContentType != nil {
		coreDoc.ContentType = *params.ContentType
	}
	if params.Description != nil {
		coreDoc.Description = params.Description
	}
	coreDoc.UpdatedAt = time.Now().UTC()

	if err := e.coreDocs.Upsert(ctx, coreDoc); err != nil {
		return nil, fmt.Errorf("failed to update document: %w", err)
	}

	// Re-index if content changed
	if params.Content != nil {
		if err := e.indexCoreDoc(ctx, coreDoc); err != nil {
			e.logger.Printf("Warning: failed to re-index CoreDoc %s: %v", params.DocID, err)
		}
	}

	// Publish WebSocket event
	if e.publisher != nil {
		_ = e.publisher.PublishJSON(ctx, realtime.EventDocProcess, e.tenantID, map[string]any{
			"projectId": e.projectID,
			"docId":     coreDoc.ID,
			"status":    "updated",
			"ts":        time.Now().UnixMilli(),
		})
	}

	// Return updated content so LLM can see the result
	response := map[string]interface{}{
		"docId":    coreDoc.ID,
		"status":   "updated",
		"filename": coreDoc.Filename,
	}
	if coreDoc.ExtractedText != nil {
		response["content"] = *coreDoc.ExtractedText
		response["length"] = len(*coreDoc.ExtractedText)
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

// updateCoreDocFragment performs fragmentary updates to a CoreDoc
func (e *CoreDocsToolExecutor) updateCoreDocFragment(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		DocID      string `json:"docId"`
		Operation  string `json:"operation"` // replace, insert, append, prepend
		SearchText string `json:"searchText,omitempty"`
		NewText    string `json:"newText"`
		Position   *int   `json:"position,omitempty"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.DocID == "" || params.NewText == "" {
		return nil, fmt.Errorf("docId and newText are required")
	}

	// Get or create edit session
	session := e.editSessions[params.DocID]
	if session == nil {
		// Load document
		coreDoc, err := e.coreDocs.Get(ctx, params.DocID)
		if err != nil {
			return nil, fmt.Errorf("document not found: %w", err)
		}
		if coreDoc.ProjectID != e.projectID {
			return nil, fmt.Errorf("access denied")
		}

		baseContent := ""
		if coreDoc.ExtractedText != nil {
			baseContent = *coreDoc.ExtractedText
		}

		// Create temporary interaction for context
		interaction := &repo.Interaction{
			TenantID:   e.tenantID,
			ProjectID:  e.projectID,
			Kind:       "document.edit",
			Content:    baseContent,
			CoreDocIDs: []string{params.DocID},
		}
		interactionID, err := e.interactions.Create(ctx, interaction)
		if err != nil {
			return nil, fmt.Errorf("failed to create edit interaction: %w", err)
		}

		session = &DocumentEditSession{
			DocID:          params.DocID,
			BaseContent:    baseContent,
			CurrentContent: baseContent,
			InteractionID:  interactionID,
			CreatedAt:      time.Now().UTC(),
		}
		e.editSessions[params.DocID] = session
	}

	// Apply operation
	var newContent string
	switch params.Operation {
	case "replace":
		if params.SearchText == "" {
			return nil, fmt.Errorf("searchText is required for replace operation")
		}
		if !strings.Contains(session.CurrentContent, params.SearchText) {
			return nil, fmt.Errorf("searchText not found in document")
		}
		newContent = strings.Replace(session.CurrentContent, params.SearchText, params.NewText, 1)
	case "insert":
		if params.Position == nil {
			return nil, fmt.Errorf("position is required for insert operation")
		}
		pos := *params.Position
		if pos < 0 || pos > len(session.CurrentContent) {
			return nil, fmt.Errorf("position out of range")
		}
		newContent = session.CurrentContent[:pos] + params.NewText + session.CurrentContent[pos:]
	case "append":
		newContent = session.CurrentContent + params.NewText
	case "prepend":
		newContent = params.NewText + session.CurrentContent
	default:
		return nil, fmt.Errorf("invalid operation: %s", params.Operation)
	}

	session.CurrentContent = newContent

	// Update temporary interaction
	if err := e.interactions.Update(ctx, session.InteractionID, map[string]interface{}{
		"content": newContent,
	}); err != nil {
		e.logger.Printf("Warning: failed to update edit interaction: %v", err)
	}

	// Return current content so LLM can see what the document looks like after the change
	response := map[string]interface{}{
		"docId":     params.DocID,
		"operation": params.Operation,
		"status":    "updated",
		"length":    len(newContent),
		"content":   newContent, // Include full content so LLM can see the result
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

// getCoreDoc retrieves a CoreDoc by ID and returns its full content
func (e *CoreDocsToolExecutor) getCoreDoc(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		DocID string `json:"docId"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if params.DocID == "" {
		return nil, fmt.Errorf("docId is required")
	}

	coreDoc, err := e.coreDocs.Get(ctx, params.DocID)
	if err != nil {
		return nil, fmt.Errorf("document not found: %w", err)
	}
	if coreDoc.ProjectID != e.projectID {
		return nil, fmt.Errorf("access denied")
	}

	// Check if there's an active edit session (to return current state)
	session := e.editSessions[params.DocID]
	currentContent := ""
	if session != nil {
		currentContent = session.CurrentContent
	} else if coreDoc.ExtractedText != nil {
		currentContent = *coreDoc.ExtractedText
	}

	response := map[string]interface{}{
		"docId":          coreDoc.ID,
		"filename":       coreDoc.Filename,
		"contentType":    coreDoc.ContentType,
		"content":        currentContent,
		"length":         len(currentContent),
		"hasPendingEdits": session != nil,
	}
	if coreDoc.Description != nil {
		response["description"] = *coreDoc.Description
	}
	if coreDoc.SizeBytes != nil {
		response["sizeBytes"] = *coreDoc.SizeBytes
	}
	response["createdAt"] = coreDoc.CreatedAt.Format(time.RFC3339)
	response["updatedAt"] = coreDoc.UpdatedAt.Format(time.RFC3339)

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

// listCoreDocs lists CoreDocs in the project
func (e *CoreDocsToolExecutor) listCoreDocs(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Limit *int `json:"limit"`
	}
	json.Unmarshal(args, &params) // Ignore errors, use defaults

	limit := 50
	if params.Limit != nil && *params.Limit > 0 && *params.Limit <= 100 {
		limit = *params.Limit
	}

	docs, err := e.coreDocs.ListByProject(ctx, e.tenantID, e.projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}

	type docInfo struct {
		ID          string  `json:"id"`
		Filename    string  `json:"filename"`
		ContentType string  `json:"contentType"`
		Description *string `json:"description,omitempty"`
		SizeBytes   *int64  `json:"sizeBytes,omitempty"`
		CreatedAt   string  `json:"createdAt"`
		UpdatedAt   string  `json:"updatedAt"`
	}

	results := make([]docInfo, 0, len(docs))
	for _, doc := range docs {
		results = append(results, docInfo{
			ID:          doc.ID,
			Filename:    doc.Filename,
			ContentType: doc.ContentType,
			Description: doc.Description,
			SizeBytes:   doc.SizeBytes,
			CreatedAt:   doc.CreatedAt.Format(time.RFC3339),
			UpdatedAt:   doc.UpdatedAt.Format(time.RFC3339),
		})
	}

	response := map[string]interface{}{
		"documents": results,
		"count":     len(results),
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return b, nil
}

// FinalizeEditSession applies changes from edit session to CoreDoc and cleans up
func (e *CoreDocsToolExecutor) FinalizeEditSession(ctx context.Context, docID string) error {
	session, ok := e.editSessions[docID]
	if !ok {
		return nil // No active session
	}

	// Load CoreDoc
	coreDoc, err := e.coreDocs.Get(ctx, docID)
	if err != nil {
		return fmt.Errorf("document not found: %w", err)
	}

	// Apply changes
	coreDoc.ExtractedText = &session.CurrentContent
	size := int64(len([]byte(session.CurrentContent)))
	coreDoc.SizeBytes = &size
	coreDoc.UpdatedAt = time.Now().UTC()

	if err := e.coreDocs.Upsert(ctx, coreDoc); err != nil {
		return fmt.Errorf("failed to update document: %w", err)
	}

	// Re-index
	if err := e.indexCoreDoc(ctx, coreDoc); err != nil {
		e.logger.Printf("Warning: failed to re-index CoreDoc %s: %v", docID, err)
	}

	// Delete temporary interaction
	if err := e.interactions.DeleteByID(ctx, session.InteractionID); err != nil {
		e.logger.Printf("Warning: failed to delete edit interaction: %v", err)
	}

	// Remove session
	delete(e.editSessions, docID)

	// Publish WebSocket event
	if e.publisher != nil {
		_ = e.publisher.PublishJSON(ctx, realtime.EventDocProcess, e.tenantID, map[string]any{
			"projectId": e.projectID,
			"docId":     docID,
			"status":    "updated",
			"ts":        time.Now().UnixMilli(),
		})
	}

	return nil
}

// CleanupEditSessions finalizes all active edit sessions (called when AI finishes editing)
func (e *CoreDocsToolExecutor) CleanupEditSessions(ctx context.Context) {
	for docID := range e.editSessions {
		if err := e.FinalizeEditSession(ctx, docID); err != nil {
			e.logger.Printf("Error finalizing edit session for %s: %v", docID, err)
		}
	}
}

