// add_llm_search_source creates the LLM search grounding quasi-source in Firestore.
// Run once before horizon_news_by_fragments to avoid fallback to path: canonicalPath.
//
// Usage:
//
//	GCP_PROJECT_ID=xxx go run ./cmd/tools/add_llm_search_source -project=xxx
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project ID")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		*projectID = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project or GCP_PROJECT_ID required")
	}

	ctx := context.Background()
	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer fs.Close()

	dsRepo := repo.NewDataSourcesRepository(fs)
	id, err := dsRepo.EnsureLLMSearchQuasiSource(ctx)
	if err != nil {
		log.Fatalf("EnsureLLMSearchQuasiSource failed: %v", err)
	}
	log.Printf("LLM search grounding source ready: id=%s (llm-search-grounding://gemini)", id)
}
