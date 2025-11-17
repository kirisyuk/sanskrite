package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

// KnowledgeSearchToolExecutor executes knowledge search tools for AI using Wikidata
type KnowledgeSearchToolExecutor struct {
	logger         *log.Logger
	wikidataClient *WikidataClient
}

// KnowledgeSearchResult represents a single knowledge search result
type KnowledgeSearchResult struct {
	ID          string `json:"id"`          // Q-identifier (e.g., "Q42")
	Title       string `json:"title"`       // Entity title
	Label       string `json:"label"`       // Human-readable label
	Description string `json:"description"` // Description
	URL         string `json:"url"`         // Wikidata/Wikipedia URL
}

// KnowledgeSearchResponse represents the response from knowledge search
// Format: {"query": "...", "results": [{"id": "...", "title": "...", "label": "...", "description": "...", "url": "..."}], "count": N, "provider": "wikidata"}
type KnowledgeSearchResponse struct {
	Query    string                  `json:"query"`    // Original search query
	Results  []KnowledgeSearchResult `json:"results"`  // Array of search results
	Count    int                     `json:"count"`    // Number of results returned
	Provider string                  `json:"provider"` // Search provider identifier
}

// NewKnowledgeSearchToolExecutor creates a new executor for knowledge search tools
// If wikidataClient is nil, it will be initialized
func NewKnowledgeSearchToolExecutor(logger *log.Logger, wikidataClient *WikidataClient) *KnowledgeSearchToolExecutor {
	// Use provided client or initialize (Wikidata doesn't need API key)
	if wikidataClient == nil {
		wikidataClient = GetWikidataClientFromEnv()
		if logger != nil {
			if wikidataClient != nil {
				logger.Printf("[KnowledgeSearch] Wikidata client initialized")
			} else {
				logger.Printf("[KnowledgeSearch] WARNING: Wikidata client initialization failed")
			}
		}
	} else if logger != nil {
		logger.Printf("[KnowledgeSearch] Wikidata client provided via dependency injection")
	}

	return &KnowledgeSearchToolExecutor{
		logger:         logger,
		wikidataClient: wikidataClient,
	}
}

// IsAllowed checks if a tool is allowed
func (e *KnowledgeSearchToolExecutor) IsAllowed(name string) bool {
	return name == "knowledge_search"
}

// Execute executes a tool call
func (e *KnowledgeSearchToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if name != "knowledge_search" {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return e.knowledgeSearch(ctx, args)
}

// knowledgeSearch performs knowledge search using Wikidata
func (e *KnowledgeSearchToolExecutor) knowledgeSearch(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}

	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	if strings.TrimSpace(params.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 20 {
		params.Limit = 20
	}

	if e.wikidataClient == nil {
		return nil, fmt.Errorf("Wikidata client not configured")
	}

	if e.logger != nil {
		e.logger.Printf("[KnowledgeSearch] Searching for: %s (limit: %d)", params.Query, params.Limit)
	}

	// Use Wikidata Search API
	wikidataResp, err := e.wikidataClient.Search(ctx, params.Query, params.Limit)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[KnowledgeSearch] Error: %v", err)
		}
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert Wikidata results to KnowledgeSearchResult format
	var results []KnowledgeSearchResult
	for _, wikidataResult := range wikidataResp.Results {
		results = append(results, KnowledgeSearchResult{
			ID:          wikidataResult.ID,
			Title:       wikidataResult.Title,
			Label:       wikidataResult.Label,
			Description: wikidataResult.Description,
			URL:         wikidataResult.URL,
		})
	}

	response := KnowledgeSearchResponse{
		Query:    params.Query,
		Results:  results,
		Count:    len(results),
		Provider: "wikidata",
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	if e.logger != nil {
		e.logger.Printf("[KnowledgeSearch] Found %d results", len(results))
	}

	return b, nil
}

// getKnowledgeSearchToolSchema returns JSON Schema definition for knowledge_search tool
func getKnowledgeSearchToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "knowledge_search",
		Description: "Search Wikidata knowledge base for structured information about entities, concepts, people, places, events, and facts. Use this tool when you need factual information, definitions, relationships between entities, historical data, scientific facts, or structured knowledge. Wikidata contains structured data from Wikipedia and other sources, making it ideal for factual queries, entity lookups, and knowledge graph queries.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query in natural language (e.g., 'Albert Einstein', 'Moscow', 'Python programming language', 'Nobel Prize winners in Physics', 'chemical elements')",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results to return (1-20, default: 10)",
					"minimum":     1,
					"maximum":     20,
				},
			},
			"required": []string{"query"},
		},
	}
}

// getKnowledgeSearchToolSchemas returns array of tool schemas (for consistency with web_search)
func getKnowledgeSearchToolSchemas() []gen_gemini.Tool {
	return []gen_gemini.Tool{getKnowledgeSearchToolSchema()}
}

