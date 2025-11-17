package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// TavilyClient handles communication with Tavily Search API
type TavilyClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// TavilySearchRequest represents a search request to Tavily API
type TavilySearchRequest struct {
	Query             string `json:"query"`
	MaxResults        int    `json:"max_results,omitempty"`
	SearchDepth       string `json:"search_depth,omitempty"` // basic or advanced
	IncludeAnswer     bool   `json:"include_answer,omitempty"`
	IncludeRawContent bool   `json:"include_raw_content,omitempty"`
	IncludeImages     bool   `json:"include_images,omitempty"`
}

// TavilySearchResult represents a single search result from Tavily
type TavilySearchResult struct {
	Title         string  `json:"title"`
	URL           string  `json:"url"`
	Content       string  `json:"content"`
	Score         float64 `json:"score,omitempty"`
	RawContent    string  `json:"raw_content,omitempty"`
	PublishedDate string  `json:"published_date,omitempty"`
}

// TavilySearchResponse represents the response from Tavily API
type TavilySearchResponse struct {
	Query        string               `json:"query"`
	Results      []TavilySearchResult `json:"results"`
	Answer       string               `json:"answer,omitempty"`
	ResponseTime float64              `json:"response_time,omitempty"`
}

// NewTavilyClient creates a new Tavily API client
func NewTavilyClient(apiKey string) *TavilyClient {
	if apiKey == "" {
		return nil
	}

	// Clean API key (remove quotes if present)
	apiKey = strings.Trim(strings.TrimSpace(apiKey), "\"'")

	return &TavilyClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: "https://api.tavily.com",
	}
}

// GetTavilyClientFromEnv creates a Tavily client from environment variables
// This is the single source of truth for Tavily client initialization
// Checks TAVILY_API_KEY first, then TAVILY_API_KEY_TEST as fallback
func GetTavilyClientFromEnv() *TavilyClient {
	apiKey := strings.Trim(strings.TrimSpace(os.Getenv("TAVILY_API_KEY")), "\"'")
	if apiKey == "" {
		apiKey = strings.Trim(strings.TrimSpace(os.Getenv("TAVILY_API_KEY_TEST")), "\"'")
	}
	return NewTavilyClient(apiKey)
}

// Search performs a web search using Tavily API
func (c *TavilyClient) Search(ctx context.Context, query string, maxResults int) (*TavilySearchResponse, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("Tavily client not configured: API key missing")
	}

	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 20 {
		maxResults = 20
	}

	reqBody := TavilySearchRequest{
		Query:             query,
		MaxResults:        maxResults,
		SearchDepth:       "basic", // Use basic for faster results, can be "advanced" for deeper search
		IncludeAnswer:     false,   // We'll extract from results
		IncludeRawContent: false,
		IncludeImages:     false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/search"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Tavily API error %d: %s", resp.StatusCode, string(body))
	}

	var tavilyResp TavilySearchResponse
	if err := json.Unmarshal(body, &tavilyResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &tavilyResp, nil
}
