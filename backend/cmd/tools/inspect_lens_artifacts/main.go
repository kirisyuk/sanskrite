package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type countRow struct {
	Key   string
	Count int
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
	lensID := flag.String("lens", "", "lens id")
	limit := flag.Int("limit", 5000, "max artifacts to inspect")
	pathQuery := flag.String("path-query", "", "optional substring filter for canonical/source/target paths")
	labelQuery := flag.String("label-query", "", "optional exact or substring filter for fragment display labels")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		fmt.Fprintln(os.Stderr, "--project is required")
		os.Exit(2)
	}
	if strings.TrimSpace(*lensID) == "" {
		fmt.Fprintln(os.Stderr, "--lens is required")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()

	repo := hrepo.NewLensArtifactsRepository(fsClient.Client())
	artifacts, err := repo.ListByLens(ctx, strings.TrimSpace(*lensID), *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list lens artifacts failed: %v\n", err)
		os.Exit(1)
	}

	total := len(artifacts)
	fragmentCount := 0
	edgeCount := 0
	sourceJobs := map[string]int{}
	fragmentPaths := map[string]int{}
	fragmentIDs := map[string]int{}
	displayLabels := map[string]int{}
	edgeIDs := map[string]int{}
	edgeTriples := map[string]int{}
	var pathMatches []string
	var labelMatches []string
	pathNeedle := strings.ToLower(strings.TrimSpace(*pathQuery))
	labelNeedle := strings.ToLower(strings.TrimSpace(*labelQuery))

	for _, a := range artifacts {
		if a == nil {
			continue
		}
		sourceJobs[strings.TrimSpace(a.SourceJobID)]++
		switch a.Type {
		case hmodels.LensArtifactTypeFragment:
			fragmentCount++
			fragmentPaths[strings.TrimSpace(a.CanonicalPath)]++
			fragmentIDs[strings.TrimSpace(a.FragmentID)]++
			displayLabels[strings.TrimSpace(a.DisplayLabel)]++
			if pathNeedle != "" && strings.Contains(strings.ToLower(strings.TrimSpace(a.CanonicalPath)), pathNeedle) {
				pathMatches = append(pathMatches, fmt.Sprintf("FRAGMENT %s id=%s path=%s label=%s job=%s", a.FragmentType, a.FragmentID, a.CanonicalPath, a.DisplayLabel, a.SourceJobID))
			}
			if labelNeedle != "" {
				label := strings.ToLower(strings.TrimSpace(a.DisplayLabel))
				if label != "" && strings.Contains(label, labelNeedle) {
					labelMatches = append(labelMatches, fmt.Sprintf("FRAGMENT %s id=%s path=%s label=%s job=%s", a.FragmentType, a.FragmentID, a.CanonicalPath, a.DisplayLabel, a.SourceJobID))
				}
			}
		case hmodels.LensArtifactTypeEdge:
			edgeCount++
			edgeIDs[strings.TrimSpace(a.EdgeID)]++
			triple := strings.TrimSpace(a.SourcePath) + " [" + strings.TrimSpace(a.EdgeType) + "] " + strings.TrimSpace(a.TargetPath)
			edgeTriples[triple]++
			if pathNeedle != "" {
				source := strings.ToLower(strings.TrimSpace(a.SourcePath))
				target := strings.ToLower(strings.TrimSpace(a.TargetPath))
				if strings.Contains(source, pathNeedle) || strings.Contains(target, pathNeedle) {
					pathMatches = append(pathMatches, fmt.Sprintf("EDGE %s id=%s %s -> %s job=%s", a.EdgeType, a.EdgeID, a.SourcePath, a.TargetPath, a.SourceJobID))
				}
			}
		}
	}

	fmt.Printf("lens=%s artifacts=%d fragments=%d edges=%d\n", strings.TrimSpace(*lensID), total, fragmentCount, edgeCount)
	printTop("sourceJobs", sourceJobs, 15)
	printDupes("duplicateFragmentPaths", fragmentPaths, 20)
	printDupes("duplicateFragmentIDs", fragmentIDs, 20)
	printDupes("duplicateFragmentDisplayLabels", displayLabels, 20)
	printDupes("duplicateEdgeIDs", edgeIDs, 20)
	printDupes("duplicateEdgeTriples", edgeTriples, 20)
	printLines("pathMatches", pathMatches, 50)
	printLines("labelMatches", labelMatches, 50)
}

func printTop(label string, counts map[string]int, n int) {
	rows := make([]countRow, 0, len(counts))
	for k, v := range counts {
		if strings.TrimSpace(k) == "" {
			continue
		}
		rows = append(rows, countRow{Key: k, Count: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Count > rows[j].Count
	})
	fmt.Printf("%s:\n", label)
	for i := 0; i < len(rows) && i < n; i++ {
		fmt.Printf("  %d  %s\n", rows[i].Count, rows[i].Key)
	}
}

func printDupes(label string, counts map[string]int, n int) {
	rows := make([]countRow, 0, len(counts))
	for k, v := range counts {
		if v <= 1 || strings.TrimSpace(k) == "" {
			continue
		}
		rows = append(rows, countRow{Key: k, Count: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Count > rows[j].Count
	})
	fmt.Printf("%s:\n", label)
	if len(rows) == 0 {
		fmt.Printf("  none\n")
		return
	}
	for i := 0; i < len(rows) && i < n; i++ {
		fmt.Printf("  %d  %s\n", rows[i].Count, rows[i].Key)
	}
}

func printLines(label string, rows []string, n int) {
	fmt.Printf("%s:\n", label)
	if len(rows) == 0 {
		fmt.Printf("  none\n")
		return
	}
	sort.Strings(rows)
	for i := 0; i < len(rows) && i < n; i++ {
		fmt.Printf("  %s\n", rows[i])
	}
}
