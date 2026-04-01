package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ArtifactContextService gathers context for artifact generation
type ArtifactContextService struct {
	interactionsRepo *repo.InteractionsRepository
	coreDocsRepo     *repo.CoreDocsRepo
	graphBuilder     ArtifactGraphContextBuilder
}

// NewArtifactContextService creates a new context service
func NewArtifactContextService(
	interactionsRepo *repo.InteractionsRepository,
	coreDocsRepo *repo.CoreDocsRepo,
) *ArtifactContextService {
	return &ArtifactContextService{
		interactionsRepo: interactionsRepo,
		coreDocsRepo:     coreDocsRepo,
	}
}

func (s *ArtifactContextService) WithGraphContextBuilder(builder ArtifactGraphContextBuilder) *ArtifactContextService {
	s.graphBuilder = builder
	return s
}

// GatheredContext contains all gathered context for generation
type GatheredContext struct {
	ConversationText string
	DocumentTexts    []DocumentContext
	SearchResults    []SearchResult
	PinnedMessages   []string
	GraphContexts    []GraphContext
	TotalTokens      int
	ContextSnapshot  models.ArtifactContextSnapshot
}

// DocumentContext represents a document's content
type DocumentContext struct {
	ID    string
	Title string
	Text  string
}

// SearchResult represents a search result
type SearchResult struct {
	Text      string
	Source    string
	Relevance float64
}

type GraphSectionContext struct {
	ID                 string
	Title              string
	Description        string
	Prompt             string
	DeterministicDraft string
	FactsJSON          string
}

type GraphContext struct {
	Kind          string
	LensID        *string
	ReportType    *string
	Subject       string
	Summary       string
	VisiblePaths  *int
	VisibleHubs   *int
	VisibleChains *int
	Sections      map[string]GraphSectionContext
}

type ArtifactGraphContextBuilder interface {
	Build(ctx context.Context, tenantID string, projectID string, source models.ArtifactGraphContextSource) (*GraphContext, error)
}

// GatherContext collects context from all specified sources
func (s *ArtifactContextService) GatherContext(
	ctx context.Context,
	tenantID string,
	projectID string,
	sources models.ArtifactContextSources,
) (*GatheredContext, error) {
	result := &GatheredContext{
		DocumentTexts:  make([]DocumentContext, 0),
		SearchResults:  make([]SearchResult, 0),
		PinnedMessages: make([]string, 0),
		GraphContexts:  make([]GraphContext, 0),
	}

	// 1. Gather conversation context
	if sources.ConversationRange != nil {
		conversationText, err := s.gatherConversation(ctx, tenantID, projectID, sources.ConversationRange.From, sources.ConversationRange.To)
		if err != nil {
			return nil, fmt.Errorf("failed to gather conversation: %w", err)
		}
		result.ConversationText = conversationText
	}

	// 2. Gather document context
	if len(sources.Documents) > 0 {
		docs, err := s.gatherDocuments(ctx, sources.Documents)
		if err != nil {
			return nil, fmt.Errorf("failed to gather documents: %w", err)
		}
		result.DocumentTexts = docs
	}

	// 3. Gather search results
	if sources.SearchQuery != nil && *sources.SearchQuery != "" {
		searchResults, err := s.gatherSearchResults(ctx, tenantID, projectID, *sources.SearchQuery)
		if err != nil {
			// Log but don't fail - search is optional
			fmt.Printf("Warning: failed to gather search results: %v\n", err)
		} else {
			result.SearchResults = searchResults
		}
	}

	// 4. Gather pinned messages
	if len(sources.PinnedMessages) > 0 {
		pinnedTexts, err := s.gatherPinnedMessages(ctx, tenantID, projectID, sources.PinnedMessages)
		if err != nil {
			return nil, fmt.Errorf("failed to gather pinned messages: %w", err)
		}
		result.PinnedMessages = pinnedTexts
	}

	// 5. Gather graph-native context
	if len(sources.GraphSources) > 0 && s.graphBuilder != nil {
		graphContexts, err := s.gatherGraphContexts(ctx, tenantID, projectID, sources.GraphSources)
		if err != nil {
			return nil, fmt.Errorf("failed to gather graph context: %w", err)
		}
		result.GraphContexts = graphContexts
	}

	// 6. Calculate total tokens (approximate)
	totalChars := len(result.ConversationText)
	for _, doc := range result.DocumentTexts {
		totalChars += len(doc.Text)
	}
	for _, sr := range result.SearchResults {
		totalChars += len(sr.Text)
	}
	for _, pm := range result.PinnedMessages {
		totalChars += len(pm)
	}
	for _, gc := range result.GraphContexts {
		totalChars += len(gc.Subject) + len(gc.Summary)
		for _, section := range gc.Sections {
			totalChars += len(section.Title) + len(section.Description) + len(section.Prompt) + len(section.DeterministicDraft) + len(section.FactsJSON)
		}
	}
	result.TotalTokens = totalChars / 4 // Approximate

	// 7. Create context snapshot
	result.ContextSnapshot = s.createSnapshot(sources, result)

	return result, nil
}

// gatherConversation retrieves conversation messages in range
func (s *ArtifactContextService) gatherConversation(
	ctx context.Context,
	_ string, // tenantID
	projectID string,
	fromID string,
	toID string,
) (string, error) {
	// Get all interactions for project
	interactions, err := s.interactionsRepo.ListByProject(ctx, projectID)
	if err != nil {
		return "", err
	}

	// Find range indices
	startIdx := -1
	endIdx := -1
	for i, interaction := range interactions {
		if interaction.ID == fromID {
			startIdx = i
		}
		if interaction.ID == toID {
			endIdx = i
		}
	}

	if startIdx == -1 || endIdx == -1 {
		return "", fmt.Errorf("conversation range not found")
	}

	if startIdx > endIdx {
		startIdx, endIdx = endIdx, startIdx
	}

	// Build conversation text
	var builder strings.Builder
	for i := startIdx; i <= endIdx && i < len(interactions); i++ {
		interaction := interactions[i]
		role := string(interaction.Kind)
		content := interaction.Content
		if interaction.ContentSanitized != nil && *interaction.ContentSanitized != "" {
			content = *interaction.ContentSanitized
		}
		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
	}

	return builder.String(), nil
}

// gatherDocuments retrieves document texts
func (s *ArtifactContextService) gatherDocuments(
	ctx context.Context,
	documentIDs []string,
) ([]DocumentContext, error) {
	docs := make([]DocumentContext, 0, len(documentIDs))

	for _, docID := range documentIDs {
		doc, err := s.coreDocsRepo.Get(ctx, docID)
		if err != nil {
			// Skip missing documents
			continue
		}

		text := ""
		if doc.ExtractedText != nil {
			text = *doc.ExtractedText
		}

		title := doc.Filename

		docs = append(docs, DocumentContext{
			ID:    doc.ID,
			Title: title,
			Text:  text,
		})
	}

	return docs, nil
}

// gatherSearchResults performs search via Vertex AI Search (placeholder for future integration)
func (s *ArtifactContextService) gatherSearchResults(
	_ context.Context,
	_ string, // tenantID
	_ string, // projectID
	_ string, // query
) ([]SearchResult, error) {
	// TODO: Wire Vertex AI Search client and implement semantic search
	return []SearchResult{}, nil
}

// gatherPinnedMessages retrieves pinned message texts
func (s *ArtifactContextService) gatherPinnedMessages(
	ctx context.Context,
	_ string, // tenantID
	_ string, // projectID
	messageIDs []string,
) ([]string, error) {
	texts := make([]string, 0, len(messageIDs))

	for _, msgID := range messageIDs {
		interaction, err := s.interactionsRepo.Get(ctx, msgID)
		if err != nil {
			// Skip missing messages
			continue
		}

		content := interaction.Content
		if interaction.ContentSanitized != nil && *interaction.ContentSanitized != "" {
			content = *interaction.ContentSanitized
		}

		// Include notes if available
		if interaction.Metadata != nil && len(interaction.Metadata.Notes) > 0 {
			content += "\n\nNotes:\n"
			for _, note := range interaction.Metadata.Notes {
				content += fmt.Sprintf("- %s\n", note)
			}
		}

		texts = append(texts, content)
	}

	return texts, nil
}

func (s *ArtifactContextService) gatherGraphContexts(
	ctx context.Context,
	tenantID string,
	projectID string,
	sources []models.ArtifactGraphContextSource,
) ([]GraphContext, error) {
	out := make([]GraphContext, 0, len(sources))
	for _, source := range sources {
		item, err := s.graphBuilder.Build(ctx, tenantID, projectID, source)
		if err != nil {
			return nil, err
		}
		if item == nil {
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

// createSnapshot creates a context snapshot for versioning
func (s *ArtifactContextService) createSnapshot(
	sources models.ArtifactContextSources,
	gathered *GatheredContext,
) models.ArtifactContextSnapshot {
	snapshot := models.ArtifactContextSnapshot{
		Documents:      sources.Documents,
		PinnedMessages: sources.PinnedMessages,
		GraphSources:   make([]models.ArtifactGraphContextSnapshot, 0),
		MessageCount:   0,
		DocumentTitles: make([]string, 0),
	}

	if sources.ConversationRange != nil {
		snapshot.ConversationRange = &struct {
			From string `json:"from" firestore:"from"`
			To   string `json:"to" firestore:"to"`
		}{
			From: sources.ConversationRange.From,
			To:   sources.ConversationRange.To,
		}
		// Approximate message count from conversation text
		snapshot.MessageCount = strings.Count(gathered.ConversationText, "\n\n")
	}

	if sources.SearchQuery != nil {
		snapshot.SearchQuery = sources.SearchQuery
		resultCount := len(gathered.SearchResults)
		snapshot.SearchResultCount = &resultCount
	}

	for _, doc := range gathered.DocumentTexts {
		snapshot.DocumentTitles = append(snapshot.DocumentTitles, doc.Title)
	}
	for _, graphCtx := range gathered.GraphContexts {
		sectionIDs := make([]string, 0, len(graphCtx.Sections))
		for id := range graphCtx.Sections {
			sectionIDs = append(sectionIDs, id)
		}
		snapshot.GraphSources = append(snapshot.GraphSources, models.ArtifactGraphContextSnapshot{
			Kind:          graphCtx.Kind,
			LensID:        graphCtx.LensID,
			ReportType:    graphCtx.ReportType,
			Subject:       stringPtrOrNil(graphCtx.Subject),
			SectionIDs:    sectionIDs,
			VisiblePaths:  graphCtx.VisiblePaths,
			VisibleHubs:   graphCtx.VisibleHubs,
			VisibleChains: graphCtx.VisibleChains,
		})
	}

	return snapshot
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// OptimizeForModel trims context to fit within model limits
func (s *ArtifactContextService) OptimizeForModel(
	context *GatheredContext,
	modelName string,
	maxTokens int,
) *GatheredContext {
	if context.TotalTokens <= maxTokens {
		return context
	}

	// Calculate reduction ratio
	ratio := float64(maxTokens) / float64(context.TotalTokens)

	// Trim conversation (keep most recent)
	if len(context.ConversationText) > 0 {
		targetLen := int(float64(len(context.ConversationText)) * ratio)
		if targetLen < len(context.ConversationText) {
			// Keep the end (most recent)
			context.ConversationText = context.ConversationText[len(context.ConversationText)-targetLen:]
		}
	}

	// Trim documents (truncate each proportionally)
	for i := range context.DocumentTexts {
		targetLen := int(float64(len(context.DocumentTexts[i].Text)) * ratio)
		if targetLen < len(context.DocumentTexts[i].Text) {
			context.DocumentTexts[i].Text = context.DocumentTexts[i].Text[:targetLen] + "..."
		}
	}

	// Recalculate tokens
	totalChars := len(context.ConversationText)
	for _, doc := range context.DocumentTexts {
		totalChars += len(doc.Text)
	}
	context.TotalTokens = totalChars / 4

	return context
}

// GetContextDiff calculates the difference between two context snapshots
func (s *ArtifactContextService) GetContextDiff(
	oldSnapshot models.ArtifactContextSnapshot,
	newSnapshot models.ArtifactContextSnapshot,
) ContextDiff {
	diff := ContextDiff{
		NewMessages:        0,
		ChangedDocuments:   0,
		SearchChanged:      false,
		OverallChangeScore: 0.0,
	}

	// Calculate message difference
	diff.NewMessages = newSnapshot.MessageCount - oldSnapshot.MessageCount
	if diff.NewMessages < 0 {
		diff.NewMessages = 0
	}

	// Calculate document changes
	oldDocs := make(map[string]bool)
	for _, docID := range oldSnapshot.Documents {
		oldDocs[docID] = true
	}
	for _, docID := range newSnapshot.Documents {
		if !oldDocs[docID] {
			diff.ChangedDocuments++
		}
	}

	// Check search query change
	oldQuery := ""
	if oldSnapshot.SearchQuery != nil {
		oldQuery = *oldSnapshot.SearchQuery
	}
	newQuery := ""
	if newSnapshot.SearchQuery != nil {
		newQuery = *newSnapshot.SearchQuery
	}
	diff.SearchChanged = oldQuery != newQuery

	// Calculate overall change score (0-1)
	messageScore := float64(diff.NewMessages) / 10.0 // 10 new messages = 1.0
	if messageScore > 1.0 {
		messageScore = 1.0
	}

	docScore := float64(diff.ChangedDocuments) / 3.0 // 3 new docs = 1.0
	if docScore > 1.0 {
		docScore = 1.0
	}

	searchScore := 0.0
	if diff.SearchChanged {
		searchScore = 0.5
	}

	diff.OverallChangeScore = (messageScore + docScore + searchScore) / 3.0

	return diff
}

// ContextDiff represents changes between two context snapshots
type ContextDiff struct {
	NewMessages        int
	ChangedDocuments   int
	SearchChanged      bool
	OverallChangeScore float64 // 0-1
}
