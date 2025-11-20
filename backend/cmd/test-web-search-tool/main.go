package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/http"
)

func main() {
	logger := log.New(os.Stdout, "[Test] ", log.LstdFlags)

	// Create web search executor
	executor := http.NewWebSearchToolExecutor(logger, nil, nil) // nil for tavilyClient and usageMetricsRepo in test

	// Test query
	query := "latest news about AI"
	if len(os.Args) > 1 {
		query = os.Args[1]
	}

	fmt.Printf("Testing web_search tool with query: %s\n\n", query)

	// Prepare tool call arguments
	args := map[string]interface{}{
		"query": query,
		"limit": 5,
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		log.Fatalf("Failed to marshal args: %v", err)
	}

	// Execute tool
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Executing web_search tool...")
	result, err := executor.Execute(ctx, "web_search", argsJSON)
	if err != nil {
		log.Fatalf("Tool execution failed: %v", err)
	}

	// Parse and display results
	var response struct {
		Query    string `json:"query"`
		Results  []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
		Count    int    `json:"count"`
		Provider string `json:"provider"`
	}

	if err := json.Unmarshal(result, &response); err != nil {
		log.Fatalf("Failed to unmarshal response: %v", err)
	}

	fmt.Printf("\n✅ Tool execution successful!\n\n")
	fmt.Printf("Provider: %s\n", response.Provider)
	fmt.Printf("Query: %s\n", response.Query)
	fmt.Printf("Results count: %d\n\n", response.Count)

	for i, result := range response.Results {
		fmt.Printf("Result %d:\n", i+1)
		fmt.Printf("  Title: %s\n", result.Title)
		fmt.Printf("  URL: %s\n", result.URL)
		if len(result.Description) > 150 {
			fmt.Printf("  Description: %s...\n", result.Description[:150])
		} else {
			fmt.Printf("  Description: %s\n", result.Description)
		}
		fmt.Println()
	}

	fmt.Println("✅ All tests passed!")
}

