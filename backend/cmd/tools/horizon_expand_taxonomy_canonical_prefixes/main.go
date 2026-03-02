package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Expands Horizon taxonomy canonicalPrefixes in Firestore WITHOUT overwriting the full taxonomy document.
//
// Usage:
//   go run ./cmd/tools/horizon_expand_taxonomy_canonical_prefixes --project <gcp-project> --version v1
func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id (defaults to $GCP_PROJECT_ID)")
	version := flag.String("version", "v1", "taxonomy version doc id (default v1)")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID)")
	}
	if strings.TrimSpace(*version) == "" {
		*version = "v1"
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	taxRepo := repo.NewHorizonTaxonomyRepository(fsClient.Client())
	taxDoc, err := taxRepo.Get(ctx, strings.TrimSpace(*version))
	if err != nil {
		log.Fatalf("taxonomy get failed: %v", err)
	}

	old := append([]string(nil), taxDoc.CanonicalPrefixes...)

	// Derive canonical prefixes from the codebase canonical namespaces.
	wantSet := map[string]struct{}{}
	for ns := range taxonomy.ValidCanonicalPathNamespace {
		ns = strings.ToLower(strings.TrimSpace(ns))
		if ns == "" {
			continue
		}
		wantSet["/"+ns+"/"] = struct{}{}
	}

	// Horizon taxonomy also stores /industry/ prefixes (classification paths).
	wantSet["/industry/"] = struct{}{}

	preferredOrder := []string{
		"/gov/",
		"/org/",
		"/fac/",
		"/loc/",
		"/person/",
		"/product/",
		"/program/",
		"/market/",
		"/segment/",
		"/event/",
		"/sphere/",
		"/media/",
		"/deposit/",
		"/domain/",
		"/community/",
		"/decision/",
		"/mechanism/",
		"/industry/",
		"/track/",
	}

	normalize := func(p string) string {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			return ""
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if !strings.HasSuffix(p, "/") {
			p = p + "/"
		}
		for strings.Contains(p, "//") {
			p = strings.ReplaceAll(p, "//", "/")
		}
		return p
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(old)+len(wantSet))

	// Preserve existing order (normalized) first.
	for _, p := range old {
		p = normalize(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	// Append missing canonical prefixes in preferred order.
	for _, p := range preferredOrder {
		p = normalize(p)
		if p == "" {
			continue
		}
		if _, ok := wantSet[p]; !ok {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	// Append any remaining derived prefixes deterministically (future-proof).
	rest := make([]string, 0)
	for p := range wantSet {
		p = normalize(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		rest = append(rest, p)
	}
	sort.Strings(rest)
	out = append(out, rest...)

	if equalStringSlices(old, out) {
		fmt.Printf("No changes: taxonomy %s canonicalPrefixes already up to date (%d)\n", taxDoc.Version, len(out))
		return
	}

	now := time.Now().UTC()
	doc := fsClient.Client().Collection("horizon_taxonomy").Doc(strings.TrimSpace(*version))

	updates := []firestore.Update{
		{Path: "canonicalPrefixes", Value: out},
		{Path: "updatedAt", Value: now},
	}

	if _, err := doc.Update(ctx, updates); err != nil {
		// If doc doesn't exist or Update isn't allowed, fall back to merge-set (non-destructive).
		_, err2 := doc.Set(ctx, map[string]interface{}{
			"canonicalPrefixes": out,
			"updatedAt":         now,
		}, firestore.MergeAll)
		if err2 != nil {
			log.Fatalf("taxonomy update failed: updateErr=%v mergeSetErr=%v", err, err2)
		}
	}

	fmt.Printf("Updated taxonomy %s in project=%s\n", strings.TrimSpace(*version), strings.TrimSpace(*projectID))
	fmt.Printf("canonicalPrefixes: %d -> %d\n", len(old), len(out))
	for _, p := range out {
		fmt.Println(p)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	normalize := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	for i := range a {
		if normalize(a[i]) != normalize(b[i]) {
			return false
		}
	}
	return true
}

