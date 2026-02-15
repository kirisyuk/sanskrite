package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/merge"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Merge abbreviation fragments into full-name fragments (heuristic: first-letter acronym match).
// Keeps the fragment with fuller path and fuller name; merges all data from both.
// Callable from horizon_merge_org_aliases or run standalone.

func main() {
	var (
		projectID      = flag.String("project", getenvDefault("GCP_PROJECT", "hatlone-test"), "GCP project id")
		apply          = flag.Bool("apply", false, "Apply changes (default: dry-run only)")
		dryRun         = flag.Bool("dry-run", false, "Force dry-run even if --apply is provided")
		limit          = flag.Int("limit", 0, "Max fragments to process (0 = no limit)")
		tenantID       = flag.String("tenant", "global", "TenantID for Horizon fragments")
		prefixes       = flag.String("prefixes", "/gov/,/org/", "Comma-separated path prefixes to process (e.g. /gov/,/org/)")
		fixStaleFrames = flag.Bool("fix-stale-frames", false, "Also migrate frames for legacy abbrev->full redirects (from runs where frames index was missing)")
	)
	flag.Parse()
	if *dryRun {
		*apply = false
	}

	ctx := context.Background()

	log.Printf("=== Horizon: Merge abbreviation -> full name ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d prefixes=%s fixStaleFrames=%v",
		*projectID, *tenantID, *apply, *limit, *prefixes, *fixStaleFrames)

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	framesRepo := horizonrepo.NewFramesRepository(db)

	prefixList := strings.Split(*prefixes, ",")
	for i, p := range prefixList {
		prefixList[i] = strings.TrimSpace(p)
	}
	var validPrefixes []string
	for _, p := range prefixList {
		if p != "" {
			validPrefixes = append(validPrefixes, p)
		}
	}
	if len(validPrefixes) == 0 {
		validPrefixes = []string{"/gov/", "/org/"}
	}

	cfg := merge.RunConfig{
		ProjectID:      *projectID,
		TenantID:       *tenantID,
		Apply:          *apply,
		Limit:          *limit,
		Prefixes:       validPrefixes,
		FixStaleFrames: *fixStaleFrames,
	}

	result, err := merge.RunAbbreviationMerge(ctx, db, fragmentsRepo, edgesRepo, framesRepo, cfg)
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}

	log.Printf("=== Summary ===")
	log.Printf("processed=%d merged=%d updatedFull=%d deletedAbbrev=%d updatedEdges=%d deletedDupEdges=%d updatedFrames=%d apply=%v",
		result.Processed, result.Merged, result.UpdatedFull, result.DeletedAbbrev,
		result.UpdatedEdges, result.DeletedDupEdges, result.UpdatedFrames, *apply)
}

func getenvDefault(k, def string) string {
	if v := os.Getenv(k); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}
