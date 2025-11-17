package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

// WebSearchToolExecutor executes web search tools for AI
type WebSearchToolExecutor struct {
	logger       *log.Logger
	tavilyClient *TavilyClient
}

// SearchResult represents a single search result
type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// WebSearchResponse represents the response from web search
// This format is returned to the model as JSON and should be structured for easy parsing
// Format: {"query": "...", "results": [{"title": "...", "url": "...", "description": "..."}], "count": N, "provider": "..."}
type WebSearchResponse struct {
	Query    string         `json:"query"`    // Original search query
	Results  []SearchResult `json:"results"`  // Array of search results
	Count    int            `json:"count"`    // Number of results returned
	Provider string         `json:"provider"` // Search provider identifier
}

// NewWebSearchToolExecutor creates a new executor for web search tools
// If tavilyClient is nil, it will be initialized from environment variables
func NewWebSearchToolExecutor(logger *log.Logger, tavilyClient *TavilyClient) *WebSearchToolExecutor {
	// Use provided client or initialize from env (for backward compatibility)
	if tavilyClient == nil {
		tavilyClient = GetTavilyClientFromEnv()
		if logger != nil {
			if tavilyClient != nil {
				logger.Printf("[WebSearch] Tavily client initialized from environment")
			} else {
				logger.Printf("[WebSearch] WARNING: TAVILY_API_KEY not set, web search will fail")
			}
		}
	} else if logger != nil {
		logger.Printf("[WebSearch] Tavily client provided via dependency injection")
	}

	return &WebSearchToolExecutor{
		logger:       logger,
		tavilyClient: tavilyClient,
	}
}

// IsAllowed checks if a tool is allowed
func (e *WebSearchToolExecutor) IsAllowed(name string) bool {
	return name == "web_search"
}

// Execute executes a tool call
func (e *WebSearchToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	if name != "web_search" {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return e.webSearch(ctx, args)
}

// webSearch performs web search using unified GenAI mechanism
func (e *WebSearchToolExecutor) webSearch(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
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

	if e.tavilyClient == nil {
		return nil, fmt.Errorf("Tavily client not configured: TAVILY_API_KEY not set")
	}

	if e.logger != nil {
		e.logger.Printf("[WebSearch] Searching for: %s (limit: %d)", params.Query, params.Limit)
	}

	// Use Tavily Search API
	results, err := e.searchWithTavily(ctx, params.Query, params.Limit)
	if err != nil {
		if e.logger != nil {
			e.logger.Printf("[WebSearch] Error: %v", err)
		}
		return nil, fmt.Errorf("search failed: %w", err)
	}

	response := WebSearchResponse{
		Query:    params.Query,
		Results:  results,
		Count:    len(results),
		Provider: "tavily",
	}

	b, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	if e.logger != nil {
		e.logger.Printf("[WebSearch] Found %d results", len(results))
	}

	return b, nil
}

// searchWithTavily performs search using Tavily Search API
func (e *WebSearchToolExecutor) searchWithTavily(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	tavilyResp, err := e.tavilyClient.Search(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("tavily search failed: %w", err)
	}

	if e.logger != nil {
		e.logger.Printf("[WebSearch] Tavily response: query=%s, resultsCount=%d, responseTime=%.2fs",
			tavilyResp.Query, len(tavilyResp.Results), tavilyResp.ResponseTime)
	}

	var results []SearchResult
	for i, tavilyResult := range tavilyResp.Results {
		if i >= limit {
			break
		}

		// Truncate description if too long
		description := tavilyResult.Content
		if len(description) > 500 {
			description = description[:500] + "..."
		}

		// Use title from Tavily, fallback to URL domain if empty
		title := tavilyResult.Title
		if title == "" {
			title = e.extractTitleFromURL(tavilyResult.URL)
		}

		results = append(results, SearchResult{
			Title:       title,
			URL:         tavilyResult.URL,
			Description: description,
		})
	}

	if e.logger != nil {
		e.logger.Printf("[WebSearch] Extracted %d results from Tavily", len(results))
	}

	return results, nil
}

// extractTitleFromURL extracts a title from URL (domain name or path)
func (e *WebSearchToolExecutor) extractTitleFromURL(urlStr string) string {
	// Remove protocol
	urlStr = strings.TrimPrefix(urlStr, "https://")
	urlStr = strings.TrimPrefix(urlStr, "http://")

	// Extract domain name
	parts := strings.Split(urlStr, "/")
	if len(parts) > 0 {
		domain := parts[0]
		// Remove www. prefix
		domain = strings.TrimPrefix(domain, "www.")
		return domain
	}
	return urlStr
}

// getWebSearchToolSchema returns JSON Schema definition for web_search tool
func getWebSearchToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "web_search",
		Description: "Search the web for current information. Use this tool when you need to find up-to-date information from the internet, check current events, look up facts, or find recent news. This is a separate web search tool, not related to search grounding or other built-in search capabilities.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query in natural language (e.g., 'latest news about AI', 'current weather in Moscow', 'recent developments in quantum computing')",
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
