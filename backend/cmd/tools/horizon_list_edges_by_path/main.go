package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	canonicalPath := flag.String("path", "", "canonical path to inspect (e.g. /domain/v1/...)")
	limit := flag.Int("limit", 200, "max edges to fetch per direction")
	flag.Parse()

	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "--project is required (or set GCP_PROJECT_ID)")
		os.Exit(2)
	}
	if strings.TrimSpace(*canonicalPath) == "" {
		fmt.Fprintln(os.Stderr, "--path is required")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()

	frRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())

	cp := strings.TrimSpace(*canonicalPath)
	frag, err := frRepo.GetByCanonicalPath(ctx, cp)
	if err != nil || frag == nil {
		fmt.Fprintf(os.Stderr, "fragment not found for canonical path: %s (err=%v)\n", cp, err)
		os.Exit(1)
	}

	fmt.Printf("fragment: id=%s type=%s path=%s\n", frag.ID, frag.Type, frag.CanonicalPath)

	outEdges, err := edgesRepo.ListBySource(ctx, "global", frag.ID, nil, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list by source failed: %v\n", err)
		os.Exit(1)
	}
	inEdges, err := edgesRepo.ListByTarget(ctx, "global", frag.ID, nil, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list by target failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("outgoing edges: %d\n", len(outEdges))
	for _, e := range outEdges {
		fmt.Printf("  OUT %s -> %s (%s)\n", strings.TrimSpace(e.SourceFragmentID), strings.TrimSpace(e.TargetFragmentID), strings.TrimSpace(string(e.Type)))
	}
	fmt.Printf("incoming edges: %d\n", len(inEdges))
	for _, e := range inEdges {
		fmt.Printf("  IN  %s <- %s (%s)\n", strings.TrimSpace(e.TargetFragmentID), strings.TrimSpace(e.SourceFragmentID), strings.TrimSpace(string(e.Type)))
	}
}
