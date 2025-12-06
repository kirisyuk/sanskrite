package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// SignalMonitoringService monitors signals and creates snapshots
type SignalMonitoringService struct {
	signalsRepo        *repo.ListeningSignalsRepository
	snapshotRefsRepo   *repo.SignalSnapshotRefsRepository
	coreDocsRepo       *repo.CoreDocsRepo
	documentsRepo      *repo.DocumentsRepository
	taskExecutor       *ai.AITaskExecutor
	genaiClient        *gen_combined.Client
	tavilyClient       interface {
		Search(ctx context.Context, query string, maxResults int) (*TavilySearchResponse, error)
	}
	checkInterval            time.Duration
	minRelevanceScore        float64 // Minimum relevance score for findings (default: 0.6)
	minConfidenceThreshold   float64 // Minimum confidence for creating snapshot (default: 0.5)
}

// TavilySearchResponse represents Tavily search response
type TavilySearchResponse struct {
	Results []TavilySearchResult `json:"results"`
}

// TavilySearchResult represents a single Tavily search result
type TavilySearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Score   float64 `json:"score"`
}

// NewSignalMonitoringService creates a new signal monitoring service
func NewSignalMonitoringService(
	signalsRepo *repo.ListeningSignalsRepository,
	snapshotRefsRepo *repo.SignalSnapshotRefsRepository,
	coreDocsRepo *repo.CoreDocsRepo,
	documentsRepo *repo.DocumentsRepository,
	taskExecutor *ai.AITaskExecutor,
) *SignalMonitoringService {
	return &SignalMonitoringService{
		signalsRepo:            signalsRepo,
		snapshotRefsRepo:       snapshotRefsRepo,
		coreDocsRepo:           coreDocsRepo,
		documentsRepo:          documentsRepo,
		taskExecutor:           taskExecutor,
		genaiClient:            nil, // Will be set via WithGenAIClient
		tavilyClient:           nil, // Will be set via WithTavilyClient if available
		checkInterval:          6 * time.Hour, // Default: every 6 hours
		minRelevanceScore:      0.6,  // Findings with score < 0.6 are filtered out
		minConfidenceThreshold: 0.5,  // Snapshots with confidence < 0.5 are not created
	}
}

// WithGenAIClient sets the GenAI client for structured output
func (s *SignalMonitoringService) WithGenAIClient(client *gen_combined.Client) *SignalMonitoringService {
	s.genaiClient = client
	return s
}

// WithTavilyClient sets the Tavily client for web search
func (s *SignalMonitoringService) WithTavilyClient(client interface {
	Search(ctx context.Context, query string, maxResults int) (*TavilySearchResponse, error)
}) *SignalMonitoringService {
	s.tavilyClient = client
	return s
}

// WithCheckInterval sets the check interval for the monitoring service
func (s *SignalMonitoringService) WithCheckInterval(interval time.Duration) *SignalMonitoringService {
	if interval < 5*time.Minute {
		interval = 5 * time.Minute // Minimum 5 minutes
	}
	s.checkInterval = interval
	return s
}

// WithRelevanceThreshold sets the minimum relevance score for findings
func (s *SignalMonitoringService) WithRelevanceThreshold(score float64) *SignalMonitoringService {
	if score >= 0 && score <= 1 {
		s.minRelevanceScore = score
	}
	return s
}

// WithConfidenceThreshold sets the minimum confidence for creating snapshots
func (s *SignalMonitoringService) WithConfidenceThreshold(score float64) *SignalMonitoringService {
	if score >= 0 && score <= 1 {
		s.minConfidenceThreshold = score
	}
	return s
}

// CheckInterval returns the current check interval
func (s *SignalMonitoringService) CheckInterval() time.Duration {
	return s.checkInterval
}

// ListAllActiveSignals returns all active signals (for external processing)
func (s *SignalMonitoringService) ListAllActiveSignals(ctx context.Context) ([]*models.ListeningSignal, error) {
	return s.signalsRepo.ListAllActive(ctx, 1000)
}

// Start starts the monitoring loop
func (s *SignalMonitoringService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Run immediately on startup
	s.processAllSignals(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processAllSignals(ctx)
		}
	}
}

// processAllSignals processes all active signals across all tenants
func (s *SignalMonitoringService) processAllSignals(ctx context.Context) {
	log.Printf("[SignalMonitoring] Starting signal monitoring cycle")

	// Get all active signals across all tenants
	signals, err := s.signalsRepo.ListAllActive(ctx, 1000)
	if err != nil {
		log.Printf("[SignalMonitoring] Failed to list active signals: %v", err)
		return
	}

	if len(signals) == 0 {
		log.Printf("[SignalMonitoring] No active signals found")
		return
	}

	log.Printf("[SignalMonitoring] Found %d active signals to process", len(signals))

	successCount := 0
	errorCount := 0

	for _, signal := range signals {
		if err := s.ProcessSignal(ctx, signal); err != nil {
			log.Printf("[SignalMonitoring] Failed to process signal %s (%s): %v", signal.Name, signal.ID, err)
			errorCount++
			continue
		}
		successCount++
	}

	log.Printf("[SignalMonitoring] Signal monitoring cycle completed: %d processed, %d errors", successCount, errorCount)
}

// ProcessAllActiveSignals processes all active signals for a tenant/project
func (s *SignalMonitoringService) ProcessAllActiveSignals(ctx context.Context, tenantID string, projectID *string) error {
	signals, err := s.signalsRepo.ListActive(ctx, tenantID, projectID)
	if err != nil {
		return fmt.Errorf("failed to list active signals: %w", err)
	}

	log.Printf("[SignalMonitoring] Found %d active signals for tenant %s", len(signals), tenantID)

	for _, signal := range signals {
		if err := s.ProcessSignal(ctx, signal); err != nil {
			log.Printf("[SignalMonitoring] Failed to process signal %s: %v", signal.ID, err)
			// Continue with other signals
			continue
		}
	}

	return nil
}

// ProcessSignal processes a single signal and creates a snapshot if new information is found
func (s *SignalMonitoringService) ProcessSignal(ctx context.Context, signal *models.ListeningSignal) error {
	if signal == nil || !signal.IsActive {
		return nil
	}

	log.Printf("[SignalMonitoring] Processing signal: %s (ID: %s)", signal.Name, signal.ID)

	// 1. Perform search using search queries
	searchResults, err := s.performSearch(ctx, signal)
	if err != nil {
		return fmt.Errorf("failed to perform search: %w", err)
	}

	if len(searchResults) == 0 {
		log.Printf("[SignalMonitoring] No search results found for signal %s", signal.ID)
		return nil
	}

	// 2. Get previous snapshot for comparison
	previousSnapshot, err := s.snapshotRefsRepo.GetLatestBySignal(ctx, signal.ID)
	if err != nil {
		log.Printf("[SignalMonitoring] No previous snapshot found for signal %s (this is the first snapshot)", signal.ID)
		previousSnapshot = nil
	}

	// 3. Use LLM to verify relevance and extract new information
	newInfo, err := s.analyzeSearchResults(ctx, signal, searchResults, previousSnapshot)
	if err != nil {
		return fmt.Errorf("failed to analyze search results: %w", err)
	}

	// 4. If there's new information, create a snapshot
	if newInfo == nil || len(newInfo.Findings) == 0 {
		log.Printf("[SignalMonitoring] No new information found for signal %s", signal.ID)
		return nil
	}

	// 5. Create snapshot document
	snapshotDoc, err := s.createSnapshotDocument(ctx, signal, newInfo, previousSnapshot)
	if err != nil {
		return fmt.Errorf("failed to create snapshot document: %w", err)
	}

	// 6. Create snapshot reference
	snapshotRef := &models.SignalSnapshotRef{
		SignalID:      signal.ID,
		SnapshotDocID: snapshotDoc.ID,
		CapturedAt:    time.Now().UTC(),
		IsActive:      true,
	}

	refID, err := s.snapshotRefsRepo.Create(ctx, snapshotRef)
	if err != nil {
		return fmt.Errorf("failed to create snapshot reference: %w", err)
	}
	snapshotRef.ID = refID

	log.Printf("[SignalMonitoring] Created snapshot for signal %s: snapshotDoc=%s, ref=%s", signal.ID, snapshotDoc.ID, refID)

	return nil
}

// performSearch performs search using signal's search queries
// Combines local document search and Tavily web search
func (s *SignalMonitoringService) performSearch(ctx context.Context, signal *models.ListeningSignal) ([]*models.Document, error) {
	var allDocs []*models.Document

	// 1. Search local documents
	if signal.ProjectID != nil {
		projectID := *signal.ProjectID
		sourceIDs := []string{fmt.Sprintf("project:%s", projectID)}
		
		limit := 50
		docs, err := s.documentsRepo.ListBySources(ctx, signal.TenantID, sourceIDs, limit, nil, nil)
		if err == nil {
			// Filter by time window (last 7 days)
			since := time.Now().UTC().Add(-7 * 24 * time.Hour)
			for _, doc := range docs {
				if doc.OriginPublishedAt != nil && doc.OriginPublishedAt.After(since) {
					allDocs = append(allDocs, doc)
				} else if doc.OriginPublishedAt == nil {
					// Include documents without published date (they might be recent)
					allDocs = append(allDocs, doc)
				}
			}
		}
	}

	// 2. Search web using Tavily (if available)
	if s.tavilyClient != nil && len(signal.SearchQueries) > 0 {
		// Combine search queries
		combinedQuery := strings.Join(signal.SearchQueries, " OR ")
		
		tavilyResults, err := s.tavilyClient.Search(ctx, combinedQuery, 10) // Get top 10 results
		if err == nil && tavilyResults != nil {
			// Convert Tavily results to Document-like structures for processing
			// We'll treat them as web search results in the analysis
			for _, result := range tavilyResults.Results {
				// Create a pseudo-document from Tavily result
				doc := &models.Document{
					ID:      fmt.Sprintf("tavily_%s_%d", signal.ID, time.Now().UnixNano()),
					Title:   result.Title,
					FullText: result.Content,
					URL:     result.URL, // Store URL directly
					SourceID: "tavily_web_search",
					TenantID: signal.TenantID,
					OriginPublishedAt: func() *time.Time {
						now := time.Now().UTC()
						return &now
					}(),
				}
				allDocs = append(allDocs, doc)
			}
		}
	}

	return allDocs, nil
}

// SignalAnalysisResult represents the result of LLM analysis (structured output)
type SignalAnalysisResult struct {
	Reasoning    ReasoningSteps `json:"reasoning"`
	Findings     []Finding      `json:"findings"`
	Summary      string         `json:"summary"`
	ContentDelta string         `json:"contentDelta"`
	Confidence   float64        `json:"confidence"`
}

// ReasoningSteps contains chain-of-thought reasoning
type ReasoningSteps struct {
	RelevanceCheck      string `json:"relevanceCheck"`      // How each result relates to signal
	NoveltyCheck        string `json:"noveltyCheck"`        // What's new vs previous snapshot
	ConfidenceRationale string `json:"confidenceRationale"` // Why this confidence score
}

// Finding represents a single finding with metadata
type Finding struct {
	Text           string      `json:"text"`
	RelevanceScore float64     `json:"relevanceScore"` // 0-1: how relevant to signal
	IsNew          bool        `json:"isNew"`          // true if not in previous snapshot
	SourceRefs     []SourceRef `json:"sourceRefs"`     // Sources supporting this finding
}

// SourceRef represents a source reference
type SourceRef struct {
	Type  string `json:"type"`  // "document", "web_search", etc.
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// analyzeSearchResults uses LLM with structured output to verify relevance and extract new information
// Optimized: single LLM call with chain-of-thought reasoning and structured JSON schema
func (s *SignalMonitoringService) analyzeSearchResults(
	ctx context.Context,
	signal *models.ListeningSignal,
	searchResults []*models.Document,
	previousSnapshot *models.SignalSnapshotRef,
) (*SignalAnalysisResult, error) {
	// Prepare search results text
	resultsText := s.formatSearchResults(searchResults)

	// Prepare previous snapshot content for comparison
	var previousContent string
	var previousFindings []string
	if previousSnapshot != nil {
		prevDoc, err := s.coreDocsRepo.Get(ctx, previousSnapshot.SnapshotDocID)
		if err == nil && prevDoc != nil {
			if prevDoc.ExtractedText != nil {
				previousContent = *prevDoc.ExtractedText
			}
			// Extract key findings from ParseMeta
			if prevDoc.ParseMeta != nil {
				if findings, ok := prevDoc.ParseMeta["keyFindings"].([]interface{}); ok {
					for _, f := range findings {
						if fs, ok := f.(string); ok {
							previousFindings = append(previousFindings, fs)
						}
					}
				}
			}
		}
	}

	// Use GenAI client with structured output if available, otherwise fall back to TaskExecutor
	if s.genaiClient != nil {
		return s.analyzeWithStructuredOutput(ctx, signal, resultsText, previousContent, previousFindings)
	}
	
	// Fallback to TaskExecutor (legacy mode)
	return s.analyzeWithTaskExecutor(ctx, signal, resultsText, previousContent, previousFindings)
}

// analyzeWithStructuredOutput uses gen_combined with StrictJSONEnabled for optimal performance
func (s *SignalMonitoringService) analyzeWithStructuredOutput(
	ctx context.Context,
	signal *models.ListeningSignal,
	resultsText string,
	previousContent string,
	previousFindings []string,
) (*SignalAnalysisResult, error) {
	// Build enhanced prompt with chain-of-thought instructions
	prompt := s.buildEnhancedAnalysisPrompt(signal, resultsText, previousContent, previousFindings)

	// Define strict JSON schema
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reasoning": map[string]interface{}{
				"type": "object",
				"description": "Chain-of-thought reasoning for analysis",
				"properties": map[string]interface{}{
					"relevanceCheck": map[string]interface{}{
						"type":        "string",
						"description": "Explain how each search result relates to the signal (be specific)",
					},
					"noveltyCheck": map[string]interface{}{
						"type":        "string",
						"description": "Explain what is new compared to previous snapshot (or state 'first snapshot')",
					},
					"confidenceRationale": map[string]interface{}{
						"type":        "string",
						"description": "Explain why you assigned this confidence score",
					},
				},
				"required": []string{"relevanceCheck", "noveltyCheck", "confidenceRationale"},
			},
			"findings": map[string]interface{}{
				"type":        "array",
				"description": "Array of findings (only relevant and new ones)",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "The finding text",
						},
						"relevanceScore": map[string]interface{}{
							"type":        "number",
							"description": "Relevance to signal (0-1, only include if >= 0.6)",
							"minimum":     0,
							"maximum":     1,
						},
						"isNew": map[string]interface{}{
							"type":        "boolean",
							"description": "True if not in previous snapshot (semantic comparison)",
						},
						"sourceRefs": map[string]interface{}{
							"type":        "array",
							"description": "Sources supporting this finding",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"type": map[string]interface{}{
										"type":        "string",
										"description": "Source type: 'document' or 'web_search'",
									},
									"id": map[string]interface{}{
										"type":        "string",
										"description": "Source ID",
									},
									"title": map[string]interface{}{
										"type":        "string",
										"description": "Source title",
									},
									"url": map[string]interface{}{
										"type":        "string",
										"description": "Source URL (if web_search)",
									},
								},
								"required": []string{"type"},
							},
						},
					},
					"required": []string{"text", "relevanceScore", "isNew", "sourceRefs"},
				},
			},
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "Brief summary of all relevant and new information (2-3 sentences)",
			},
			"contentDelta": map[string]interface{}{
				"type":        "string",
				"description": "What is new compared to previous snapshot (empty if first snapshot)",
			},
			"confidence": map[string]interface{}{
				"type":        "number",
				"description": "Overall confidence (0-1) that findings are relevant and correct",
				"minimum":     0,
				"maximum":     1,
			},
		},
		"required": []string{"reasoning", "findings", "summary", "contentDelta", "confidence"},
	}

	// Create GenAI request with structured output
	req := gen_combined.GenAIRequest{
		Model:             "gemini_fast", // Fast model for monitoring tasks
		Prompt:            prompt,
		Temperature:       0.3, // Lower temperature for more consistent output
		MaxOutputTokens:   2048,
		StrictJSONEnabled: true,
		JSONSchema:        schema,
	}

	// Execute generation
	resp, err := s.genaiClient.GenAIGenerate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute structured analysis: %w", err)
	}

	// Parse structured response
	var analysisResult SignalAnalysisResult
	if err := json.Unmarshal([]byte(resp.Content), &analysisResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal structured result: %w", err)
	}

	// Validate confidence threshold
	if analysisResult.Confidence < s.minConfidenceThreshold {
		log.Printf("[SignalMonitoring] Low confidence (%.2f < %.2f) for signal %s, skipping snapshot", 
			analysisResult.Confidence, s.minConfidenceThreshold, signal.ID)
		return nil, nil
	}

	// Filter findings by relevance score and newness
	filteredFindings := make([]Finding, 0)
	for _, finding := range analysisResult.Findings {
		if finding.RelevanceScore >= s.minRelevanceScore && finding.IsNew {
			filteredFindings = append(filteredFindings, finding)
		}
	}

	if len(filteredFindings) == 0 {
		log.Printf("[SignalMonitoring] All findings filtered out (relevance < %.2f or not new) for signal %s", 
			s.minRelevanceScore, signal.ID)
		return nil, nil
	}

	analysisResult.Findings = filteredFindings
	log.Printf("[SignalMonitoring] Analysis complete: %d relevant + new findings (confidence: %.2f)", 
		len(filteredFindings), analysisResult.Confidence)

	return &analysisResult, nil
}

// analyzeWithTaskExecutor is a fallback method using AITaskExecutor (legacy)
func (s *SignalMonitoringService) analyzeWithTaskExecutor(
	ctx context.Context,
	signal *models.ListeningSignal,
	resultsText string,
	previousContent string,
	previousFindings []string,
) (*SignalAnalysisResult, error) {
	// Build enhanced prompt
	prompt := s.buildEnhancedAnalysisPrompt(signal, resultsText, previousContent, previousFindings)

	// Create AI task
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			MinContextWindow:         8000,
			MaxOutputSize:            2000,
			MinCapabilityScore:       6,
			TextGenerationCapability: true,
		},
		Verifier:        ai.NewSimpleJSONVerifier(),
		EnableTelemetry: false,
	}

	// Execute structured task
	result, err := s.taskExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("failed to execute LLM analysis: %w", err)
	}

	// Parse result
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	var analysisResult SignalAnalysisResult
	if err := json.Unmarshal(resultJSON, &analysisResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal analysis result: %w", err)
	}

	// Validate confidence threshold
	if analysisResult.Confidence < s.minConfidenceThreshold {
		log.Printf("[SignalMonitoring] Low confidence (%.2f) for signal %s, skipping snapshot", 
			analysisResult.Confidence, signal.ID)
		return nil, nil
	}

	// Filter findings by relevance and newness
	filteredFindings := make([]Finding, 0)
	for _, finding := range analysisResult.Findings {
		if finding.RelevanceScore >= s.minRelevanceScore && finding.IsNew {
			filteredFindings = append(filteredFindings, finding)
		}
	}

	if len(filteredFindings) == 0 {
		log.Printf("[SignalMonitoring] All findings filtered out for signal %s", signal.ID)
		return nil, nil
	}

	analysisResult.Findings = filteredFindings
	return &analysisResult, nil
}

// formatSearchResults formats search results for LLM analysis
func (s *SignalMonitoringService) formatSearchResults(results []*models.Document) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Found %d search results:\n\n", len(results)))

	for i, doc := range results {
		builder.WriteString(fmt.Sprintf("Result %d:\n", i+1))
		
		// Determine source type
		sourceType := "document"
		if strings.HasPrefix(doc.ID, "tavily_") {
			sourceType = "web_search"
		}
		builder.WriteString(fmt.Sprintf("Source Type: %s\n", sourceType))
		
		if doc.Title != "" {
			builder.WriteString(fmt.Sprintf("Title: %s\n", doc.Title))
		}
		if doc.FullText != "" {
			// Truncate to first 500 chars
			text := doc.FullText
			if len(text) > 500 {
				text = text[:500] + "..."
			}
			builder.WriteString(fmt.Sprintf("Content: %s\n", text))
		}
		if doc.OriginPublishedAt != nil {
			builder.WriteString(fmt.Sprintf("Published: %s\n", doc.OriginPublishedAt.Format(time.RFC3339)))
		}
		if doc.SourceID == "tavily_web_search" {
			builder.WriteString("Source: Web Search (Tavily)\n")
			if doc.URL != "" {
				builder.WriteString(fmt.Sprintf("URL: %s\n", doc.URL))
			}
		}
		builder.WriteString(fmt.Sprintf("ID: %s\n\n", doc.ID))
	}

	return builder.String()
}

// buildEnhancedAnalysisPrompt builds enhanced prompt with chain-of-thought instructions
func (s *SignalMonitoringService) buildEnhancedAnalysisPrompt(
	signal *models.ListeningSignal,
	resultsText string,
	previousContent string,
	previousFindings []string,
) string {
	var prompt strings.Builder

	prompt.WriteString("You are analyzing search results for a monitoring signal using chain-of-thought reasoning.\n\n")
	
	prompt.WriteString("=== SIGNAL DEFINITION ===\n")
	prompt.WriteString(fmt.Sprintf("Signal Name: %s\n", signal.Name))
	prompt.WriteString(fmt.Sprintf("Signal Text: %s\n", signal.Signal))
	prompt.WriteString(fmt.Sprintf("Search Queries: %s\n\n", strings.Join(signal.SearchQueries, ", ")))

	prompt.WriteString("=== SEARCH RESULTS ===\n")
	prompt.WriteString(resultsText)
	prompt.WriteString("\n")

	if len(previousFindings) > 0 {
		prompt.WriteString("=== PREVIOUS SNAPSHOT FINDINGS ===\n")
		prompt.WriteString("Already captured findings (exclude semantic duplicates):\n")
		for i, f := range previousFindings {
			prompt.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
		}
		prompt.WriteString("\n")
	}

	prompt.WriteString("=== TASK ===\n")
	prompt.WriteString("Analyze search results using the following chain-of-thought process:\n\n")
	
	prompt.WriteString("1. RELEVANCE CHECK:\n")
	prompt.WriteString("   - For each search result, determine if it's DIRECTLY relevant to the signal\n")
	prompt.WriteString("   - Assign relevance score (0-1): 0.9-1.0=highly relevant, 0.6-0.8=relevant, <0.6=irrelevant\n")
	prompt.WriteString(fmt.Sprintf("   - ONLY include findings with relevance score >= %.1f\n\n", s.minRelevanceScore))
	
	prompt.WriteString("2. NOVELTY CHECK:\n")
	if len(previousFindings) > 0 {
		prompt.WriteString("   - Compare each finding SEMANTICALLY with previous findings\n")
		prompt.WriteString("   - Mark isNew=false if finding conveys same information (even if different wording)\n")
		prompt.WriteString("   - Mark isNew=false if finding is subset/generalization of previous finding\n")
		prompt.WriteString("   - Mark isNew=true ONLY if it adds genuinely new factual information\n\n")
	} else {
		prompt.WriteString("   - This is the first snapshot, mark all findings as isNew=true\n\n")
	}
	
	prompt.WriteString("3. EXTRACTION:\n")
	prompt.WriteString("   - Extract findings that are: relevant (score >= 0.6) AND new (isNew=true)\n")
	prompt.WriteString("   - Include source references for each finding\n")
	prompt.WriteString("   - Write concise, factual findings (not opinions or speculation)\n\n")
	
	prompt.WriteString("4. CONFIDENCE:\n")
	prompt.WriteString("   - Assign overall confidence (0-1) based on:\n")
	prompt.WriteString("     * Quality of sources\n")
	prompt.WriteString("     * Clarity of connection to signal\n")
	prompt.WriteString("     * Amount of supporting evidence\n\n")
	
	prompt.WriteString("Return structured JSON with reasoning steps and findings.")
	
	return prompt.String()
}


// createSnapshotDocument creates a CoreDoc snapshot document
func (s *SignalMonitoringService) createSnapshotDocument(
	ctx context.Context,
	signal *models.ListeningSignal,
	analysisResult *SignalAnalysisResult,
	previousSnapshot *models.SignalSnapshotRef,
) (*models.CoreDoc, error) {
	now := time.Now().UTC()

	// Extract key findings as strings for backward compatibility
	keyFindings := make([]string, 0, len(analysisResult.Findings))
	for _, f := range analysisResult.Findings {
		keyFindings = append(keyFindings, f.Text)
	}

	// Collect all unique sources
	sourcesMap := make(map[string]SourceRef)
	for _, finding := range analysisResult.Findings {
		for _, src := range finding.SourceRefs {
			key := src.Type + ":" + src.ID
			sourcesMap[key] = src
		}
	}
	sources := make([]SourceRef, 0, len(sourcesMap))
	for _, src := range sourcesMap {
		sources = append(sources, src)
	}

	// Prepare ParseMeta with full structured data
	parseMeta := map[string]interface{}{
		"signalId":     signal.ID,
		"capturedAt":   now.Format(time.RFC3339),
		"summary":      analysisResult.Summary,
		"keyFindings":  keyFindings,          // Backward compatible: array of strings
		"findings":     analysisResult.Findings, // Full structured findings with scores
		"sources":      sources,
		"contentDelta": analysisResult.ContentDelta,
		"matchCount":   len(analysisResult.Findings),
		"confidence":   analysisResult.Confidence,
		"reasoning":    analysisResult.Reasoning, // Chain-of-thought reasoning
	}

	if previousSnapshot != nil {
		parseMeta["previousSnapshotId"] = previousSnapshot.ID
	}

	// Create snapshot document
	snapshotDoc := &models.CoreDoc{
		DocType:     stringPtr("signal_snapshot"),
		TenantID:    signal.TenantID,
		ProjectID:   func() string { if signal.ProjectID != nil { return *signal.ProjectID }; return "" }(),
		Filename:    fmt.Sprintf("snapshot_%s_%d.json", signal.ID, now.Unix()),
		ContentType: "application/json",
		ExtractedText: stringPtr(analysisResult.Summary),
		ParseMeta:    parseMeta,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Save to CoreDocsRepo (will be automatically indexed)
	docID, err := s.coreDocsRepo.Create(ctx, snapshotDoc)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot document: %w", err)
	}
	snapshotDoc.ID = docID

	return snapshotDoc, nil
}

// Helper function
func stringPtr(s string) *string {
	return &s
}

