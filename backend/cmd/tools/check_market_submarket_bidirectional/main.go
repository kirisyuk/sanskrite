// check_market_submarket_bidirectional finds MARKET↔MARKET pairs where both A→B and B→A
// have HAS_MARKET_SEGMENT edges (bidirectional market segment links). Reports path segment counts
// to validate hypothesis: path with more segments = segment (child), fewer = parent.
// With --apply, deletes the incorrect child→parent edge(s).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "", "GCP project ID (required)")
	tenantID := flag.String("tenant", "global", "Tenant ID")
	limit := flag.Int("limit", 0, "Max pairs to report (0 = all)")
	apply := flag.Bool("apply", false, "Apply fixes: delete incorrect child→parent edge(s)")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Firestore init: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)

	// Collect all HAS_SUBMARKET edges (with doc ID for deletion)
	type edgeKey struct{ src, tgt string }
	edges := make(map[edgeKey]string) // edgeKey -> docID
	iter := db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", *tenantID).
		Where("type", "==", string(models.DependencyTypeHasMarketSegment)).
		Documents(ctx)
	count := 0
	for {
		doc, err := iter.Next()
		if err != nil {
			break
		}
		var e models.DependencyEdge
		if err := doc.DataTo(&e); err != nil {
			continue
		}
		src := strings.TrimSpace(e.SourceFragmentID)
		tgt := strings.TrimSpace(e.TargetFragmentID)
		if src == "" || tgt == "" || src == tgt {
			continue
		}
		edges[edgeKey{src, tgt}] = doc.Ref.ID
		count++
	}
	log.Printf("Loaded %d HAS_MARKET_SEGMENT edges", count)

	// Find bidirectional pairs: both A→B and B→A exist
	bidir := make(map[edgeKey]struct{})
	for k := range edges {
		if _, ok := edges[edgeKey{k.tgt, k.src}]; ok {
			if k.src < k.tgt {
				bidir[k] = struct{}{}
			} else {
				bidir[edgeKey{k.tgt, k.src}] = struct{}{}
			}
		}
	}
	log.Printf("Found %d bidirectional HAS_MARKET_SEGMENT pairs", len(bidir))
	if len(bidir) == 0 {
		return
	}

	// Load fragment paths for all involved IDs (batch via Firestore GetAll)
	pathByID := make(map[string]string)
	seen := make(map[string]struct{})
	for k := range bidir {
		seen[k.src] = struct{}{}
		seen[k.tgt] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
		pathByID[id] = "(not found)"
	}
	// Batch get: Firestore GetAll supports up to 100 docs per call
	const batchSize = 100
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		refs := make([]*firestore.DocumentRef, len(batch))
		for j, id := range batch {
			refs[j] = db.Collection("horizon_fragments").Doc(id)
		}
		snaps, err := db.GetAll(ctx, refs)
		if err != nil {
			log.Printf("WARNING: batch get failed: %v", err)
			continue
		}
		for j, snap := range snaps {
			if !snap.Exists() {
				continue
			}
			data := snap.Data()
			if p, ok := data["canonicalPath"].(string); ok {
				pathByID[batch[j]] = strings.TrimSpace(p)
			} else {
				pathByID[batch[j]] = "(no path)"
			}
		}
	}

	// Report each pair with segment analysis; optionally fix
	fmt.Println("\n=== Bidirectional HAS_MARKET_SEGMENT pairs ===")
	fmt.Println("Hypothesis: path with MORE segments = segment (child); FEWER = parent market")
	fmt.Printf("Total: %d pairs\n", len(bidir))
	if *limit > 0 {
		fmt.Printf("Showing first %d\n", *limit)
	}
	if *apply {
		fmt.Println("APPLY MODE: will delete incorrect child→parent HAS_MARKET_SEGMENT edge(s)")
	}
	fmt.Println()

	reported := 0
	deleted := 0
	for k := range bidir {
		pathA := pathByID[k.src]
		pathB := pathByID[k.tgt]
		segA := segmentCount(pathA)
		segB := segmentCount(pathB)

		// Determine parent and child (who should be source of correct HAS_MARKET_SEGMENT: parent→child)
		var parentID, childID string
		var verdict string
		if strings.HasPrefix(pathB+"/", pathA+"/") || pathB == pathA {
			parentID, childID = k.src, k.tgt
			verdict = fmt.Sprintf("→ %s is PARENT (prefix), %s is SEGMENT", pathA, pathB)
		} else if strings.HasPrefix(pathA+"/", pathB+"/") {
			parentID, childID = k.tgt, k.src
			verdict = fmt.Sprintf("→ %s is PARENT (prefix), %s is SEGMENT", pathB, pathA)
		} else if segA < segB {
			parentID, childID = k.src, k.tgt
			verdict = fmt.Sprintf("→ %s is PARENT (%d seg), %s is SEGMENT (%d seg)", pathA, segA, pathB, segB)
		} else if segB < segA {
			parentID, childID = k.tgt, k.src
			verdict = fmt.Sprintf("→ %s is PARENT (%d seg), %s is SEGMENT (%d seg)", pathB, segB, pathA, segA)
		} else {
			verdict = fmt.Sprintf("→ EQUAL segments (%d each); SIBLINGS? removing both edges", segA)
		}

		baseA, baseB := commonBase(pathA, pathB)

		// Report (respect limit when not applying)
		if !*apply {
			if *limit > 0 && reported >= *limit {
				break
			}
			reported++
			fmt.Printf("A: %s (id=%s) seg=%d\n", pathA, k.src, segA)
			fmt.Printf("B: %s (id=%s) seg=%d\n", pathB, k.tgt, segB)
			if baseA != "" || baseB != "" {
				fmt.Printf("  base A: %q  base B: %q\n", baseA, baseB)
			}
			fmt.Printf("  %s\n", verdict)
			fmt.Println()
			continue
		}

		// Apply: delete incorrect edge(s)
		if parentID != "" && childID != "" {
			// Delete child→parent (wrong direction)
			edgeToDelete := edgeKey{childID, parentID}
			if docID, ok := edges[edgeToDelete]; ok {
				if err := edgesRepo.Delete(ctx, docID); err != nil {
					log.Printf("  ERROR: failed to delete edge %s: %v", docID, err)
				} else {
					deleted++
					log.Printf("  ✓ Deleted %s → %s (edge %s)", pathByID[childID], pathByID[parentID], docID)
				}
			}
		} else {
			// Siblings: delete both edges
			if docID1, ok := edges[k]; ok {
				if err := edgesRepo.Delete(ctx, docID1); err != nil {
					log.Printf("  ERROR: failed to delete edge %s: %v", docID1, err)
				} else {
					deleted++
					log.Printf("  ✓ Deleted sibling edge %s → %s", pathByID[k.src], pathByID[k.tgt])
				}
			}
			revK := edgeKey{k.tgt, k.src}
			if docID2, ok := edges[revK]; ok {
				if err := edgesRepo.Delete(ctx, docID2); err != nil {
					log.Printf("  ERROR: failed to delete edge %s: %v", docID2, err)
				} else {
					deleted++
					log.Printf("  ✓ Deleted sibling edge %s → %s", pathByID[k.tgt], pathByID[k.src])
				}
			}
		}
	}

	if *apply && deleted > 0 {
		log.Printf("\n✓ Deleted %d incorrect HAS_MARKET_SEGMENT edge(s)", deleted)
	}
}

func segmentCount(path string) int {
	if path == "" || path == "(not found)" || path == "(no path)" {
		return 0
	}
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "/"))
}

func commonBase(a, b string) (string, string) {
	if a == "" || b == "" || a == "(not found)" || b == "(not found)" || a == "(no path)" || b == "(no path)" {
		return "", ""
	}
	segA := strings.Split(strings.Trim(a, "/"), "/")
	segB := strings.Split(strings.Trim(b, "/"), "/")
	if len(segA) == 0 || len(segB) == 0 {
		return "", ""
	}
	i := 0
	for i < len(segA) && i < len(segB) && segA[i] == segB[i] {
		i++
	}
	if i == 0 {
		return "", ""
	}
	restA := ""
	if i < len(segA) {
		restA = strings.Join(segA[i:], "/")
	}
	restB := ""
	if i < len(segB) {
		restB = strings.Join(segB[i:], "/")
	}
	return restA, restB
}
