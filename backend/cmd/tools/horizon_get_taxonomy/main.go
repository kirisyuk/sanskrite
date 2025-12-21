package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Reads Horizon taxonomy from Firestore and prints it as JSON.
// Usage:
//   go run ./cmd/tools/horizon_get_taxonomy --project <gcp-project> --version v1
func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	version := flag.String("version", "v1", "taxonomy version doc id (default v1)")
	flag.Parse()

	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "--project is required (or set GCP_PROJECT_ID)")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()

	r := repo.NewHorizonTaxonomyRepository(fsClient.Client())
	t, err := r.Get(ctx, *version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "taxonomy get failed: %v\n", err)
		os.Exit(1)
	}

	b, _ := json.MarshalIndent(t, "", "  ")
	fmt.Println(string(b))
}


