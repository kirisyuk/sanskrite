package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
)

var (
	projectID = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID  = flag.String("tenant", "global", "tenant id")
	query     = flag.String("q", "", "search query")
	limit     = flag.Int("limit", 20, "max number of paths to print")
)

func main() {
	flag.Parse()
	if strings.TrimSpace(*query) == "" {
		log.Fatal("--q is required")
	}
	if *limit <= 0 {
		*limit = 20
	}
	if *limit > 100 {
		*limit = 100
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	runtime := productdict.NewRuntime(fsClient.Client()).WithTenantID(strings.TrimSpace(*tenantID))
	paths, err := runtime.SearchCanonicalProductPathsForTenant(ctx, strings.TrimSpace(*tenantID), strings.TrimSpace(*query), *limit)
	if err != nil {
		log.Fatalf("search failed: %v", err)
	}

	fmt.Printf("search query=%q tenant=%s limit=%d\n", strings.TrimSpace(*query), strings.TrimSpace(*tenantID), *limit)
	if len(paths) == 0 {
		fmt.Printf("  none\n")
		return
	}
	for _, path := range paths {
		fmt.Printf("  - %s\n", path)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
