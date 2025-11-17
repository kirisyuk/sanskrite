package research

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// TavilySearchClient interface for web search (avoids circular import)
type TavilySearchClient interface {
	Search(ctx context.Context, query string, maxResults int) (*TavilySearchResponse, error)
}

// TavilySearchResponse represents search results
type TavilySearchResponse struct {
	Query        string
	Results      []TavilySearchResult
	Answer       string
	ResponseTime float64
}

// TavilySearchResult represents a single search result
type TavilySearchResult struct {
	Title         string
	URL           string
	Content       string
	Score         float64
	RawContent    string
	PublishedDate string
}

// Executor performs parallel web searches for sub-queries
type Executor struct {
	tavilyClient TavilySearchClient
}

// NewExecutor creates a new Executor instance
func NewExecutor(tavilyClient TavilySearchClient) *Executor {
	return &Executor{
		tavilyClient: tavilyClient,
	}
}

// Execute performs parallel searches for all sub-queries
func (e *Executor) Execute(ctx context.Context, subQueries []string, maxSourcesPerQuery int) ([]Source, error) {
	if e.tavilyClient == nil {
		return nil, fmt.Errorf("Tavily client not configured")
	}

	var sources []Source
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, len(subQueries))

	// Parallel search for each sub-query
	for _, query := range subQueries {
		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			log.Printf("[Executor] Searching for: %s", q)

			results, err := e.tavilyClient.Search(ctx, q, maxSourcesPerQuery)
			if err != nil {
				log.Printf("[Executor] Search failed for '%s': %v", q, err)
				errCh <- fmt.Errorf("search failed for '%s': %w", q, err)
				return
			}

			mu.Lock()
			for _, result := range results.Results {
				sources = append(sources, Source{
					Title:   result.Title,
					URL:     result.URL,
					Content: result.Content,
					Score:   result.Score,
				})
			}
			mu.Unlock()

			log.Printf("[Executor] Found %d sources for '%s'", len(results.Results), q)
		}(query)
	}

	wg.Wait()
	close(errCh)

	// Check for errors (non-fatal, continue with partial results)
	hasErrors := false
	for err := range errCh {
		if err != nil {
			log.Printf("[Executor] Error during execution: %v", err)
			hasErrors = true
		}
	}

	// Deduplicate sources by URL
	sources = deduplicateSources(sources)

	log.Printf("[Executor] Total unique sources collected: %d", len(sources))
	if hasErrors && len(sources) == 0 {
		return nil, fmt.Errorf("all searches failed")
	}

	return sources, nil
}

// deduplicateSources removes duplicate sources by URL
func deduplicateSources(sources []Source) []Source {
	seen := make(map[string]bool)
	var unique []Source

	for _, source := range sources {
		if !seen[source.URL] {
			seen[source.URL] = true
			unique = append(unique, source)
		}
	}

	return unique
}
