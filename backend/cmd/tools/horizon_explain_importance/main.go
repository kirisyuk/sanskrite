package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/importance"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var (
		projectID  = flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
		tenantID   = flag.String("tenant", "global", "tenant id")
		fragmentID = flag.String("id", "", "fragment id")
		path       = flag.String("path", "", "canonical path")
		maxDirect  = flag.Int("max-direct", 6, "max direct structural contributors")
		maxPaths   = flag.Int("max-paths", 4, "max paths per axis")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID)")
	}
	if strings.TrimSpace(*fragmentID) == "" && strings.TrimSpace(*path) == "" {
		log.Fatal("one of --id or --path is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	scorer := importance.NewScorer(fragmentsRepo, edgesRepo)

	explainCfg := importance.ExplainConfig{
		MaxDirectContributors: *maxDirect,
		MaxAxisPaths:          *maxPaths,
	}

	var explanation importance.Explanation
	switch {
	case strings.TrimSpace(*fragmentID) != "":
		explanation, _, err = scorer.ExplainFragment(ctx, *tenantID, strings.TrimSpace(*fragmentID), explainCfg)
	default:
		explanation, _, err = scorer.ExplainFragmentByPath(ctx, *tenantID, strings.TrimSpace(*path), explainCfg)
	}
	if err != nil {
		log.Fatal(err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(explanation); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}
