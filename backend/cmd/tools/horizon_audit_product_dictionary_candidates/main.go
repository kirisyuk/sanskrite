package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

var (
	projectID   = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID    = flag.String("tenant", "global", "tenant id")
	pageSize    = flag.Int("page-size", 250, "Firestore page size")
	sampleLimit = flag.Int("sample-limit", 20, "max samples to print per collection")
	activeOnly  = flag.Bool("active-only", true, "only include active candidates")
)

type synonymCandidate struct {
	ID              string
	ObservedPath    string   `firestore:"observedPath"`
	ObservedName    string   `firestore:"observedName"`
	ObservedAliasKey string  `firestore:"observedAliasKey"`
	EvidenceCount   int64    `firestore:"evidenceCount"`
	Status          string   `firestore:"status"`
	Active          bool     `firestore:"active"`
	SourceKinds     []string `firestore:"sourceKinds"`
}

type transformCandidate struct {
	ID                string   `firestore:"-"`
	FromPath          string   `firestore:"fromPath"`
	ToPath            string   `firestore:"toPath"`
	RelationHint      string   `firestore:"relationHint"`
	EvidenceCount     int64    `firestore:"evidenceCount"`
	Status            string   `firestore:"status"`
	Active            bool     `firestore:"active"`
	SourceKinds       []string `firestore:"sourceKinds"`
	ObservedEdgeTypes []string `firestore:"observedEdgeTypes"`
}

func main() {
	flag.Parse()
	if *pageSize <= 0 {
		*pageSize = 250
	}
	if *pageSize > 2000 {
		*pageSize = 2000
	}
	if *sampleLimit < 0 {
		*sampleLimit = 0
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	synonyms, err := loadSynonymCandidates(ctx, db)
	if err != nil {
		log.Fatalf("failed to load synonym candidates: %v", err)
	}
	transforms, err := loadTransformCandidates(ctx, db)
	if err != nil {
		log.Fatalf("failed to load transform candidates: %v", err)
	}

	fmt.Printf("Product Dictionary Candidate Audit\n")
	fmt.Printf("  project: %s\n", strings.TrimSpace(*projectID))
	fmt.Printf("  tenant: %s\n", strings.TrimSpace(*tenantID))
	fmt.Printf("  active_only: %t\n", *activeOnly)
	fmt.Printf("  synonym_candidates: %d\n", len(synonyms))
	fmt.Printf("  transform_candidates: %d\n", len(transforms))

	sort.SliceStable(synonyms, func(i, j int) bool {
		if synonyms[i].EvidenceCount != synonyms[j].EvidenceCount {
			return synonyms[i].EvidenceCount > synonyms[j].EvidenceCount
		}
		return synonyms[i].ObservedPath < synonyms[j].ObservedPath
	})
	sort.SliceStable(transforms, func(i, j int) bool {
		if transforms[i].EvidenceCount != transforms[j].EvidenceCount {
			return transforms[i].EvidenceCount > transforms[j].EvidenceCount
		}
		if transforms[i].FromPath != transforms[j].FromPath {
			return transforms[i].FromPath < transforms[j].FromPath
		}
		return transforms[i].ToPath < transforms[j].ToPath
	})

	if *sampleLimit > 0 {
		fmt.Printf("\nTop Synonym Candidates\n")
		for i, candidate := range synonyms {
			if i >= *sampleLimit {
				break
			}
			fmt.Printf("  - evidence=%d path=%s name=%q alias=%s status=%s sources=%s id=%s\n",
				candidate.EvidenceCount,
				candidate.ObservedPath,
				candidate.ObservedName,
				candidate.ObservedAliasKey,
				firstNonEmpty(candidate.Status, "new"),
				joinOrDash(candidate.SourceKinds),
				candidate.ID,
			)
		}

		fmt.Printf("\nTop Transform Candidates\n")
		for i, candidate := range transforms {
			if i >= *sampleLimit {
				break
			}
			fmt.Printf("  - evidence=%d from=%s to=%s relation=%s status=%s edge_types=%s sources=%s id=%s\n",
				candidate.EvidenceCount,
				candidate.FromPath,
				candidate.ToPath,
				firstNonEmpty(candidate.RelationHint, "-"),
				firstNonEmpty(candidate.Status, "new"),
				joinOrDash(candidate.ObservedEdgeTypes),
				joinOrDash(candidate.SourceKinds),
				candidate.ID,
			)
		}
	}
}

func loadSynonymCandidates(ctx context.Context, db *firestore.Client) ([]synonymCandidate, error) {
	base := db.Collection("horizon_product_synonym_candidates").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if *activeOnly {
		base = base.Where("active", "==", true)
	}
	return scanPages(ctx, base, func(doc *firestore.DocumentSnapshot) (synonymCandidate, error) {
		var candidate synonymCandidate
		if err := doc.DataTo(&candidate); err != nil {
			return synonymCandidate{}, err
		}
		candidate.ID = doc.Ref.ID
		return candidate, nil
	})
}

func loadTransformCandidates(ctx context.Context, db *firestore.Client) ([]transformCandidate, error) {
	base := db.Collection("horizon_product_transform_candidates").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if *activeOnly {
		base = base.Where("active", "==", true)
	}
	return scanPages(ctx, base, func(doc *firestore.DocumentSnapshot) (transformCandidate, error) {
		var candidate transformCandidate
		if err := doc.DataTo(&candidate); err != nil {
			return transformCandidate{}, err
		}
		candidate.ID = doc.Ref.ID
		return candidate, nil
	})
}

func scanPages[T any](ctx context.Context, base firestore.Query, decode func(*firestore.DocumentSnapshot) (T, error)) ([]T, error) {
	out := make([]T, 0)
	cursor := ""
	for {
		q := base.Limit(*pageSize)
		if cursor != "" {
			q = q.StartAfter(cursor)
		}
		iter := q.Documents(ctx)
		pageCount := 0
		lastDocID := ""
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				return nil, err
			}
			pageCount++
			lastDocID = doc.Ref.ID
			item, err := decode(doc)
			if err != nil {
				continue
			}
			out = append(out, item)
		}
		iter.Stop()
		if pageCount == 0 || pageCount < *pageSize {
			return out, nil
		}
		cursor = lastDocID
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

func joinOrDash(values []string) string {
	clean := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return "-"
	}
	return strings.Join(clean, ",")
}
