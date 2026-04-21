package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	ah "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Category C — operational dedup scanner (exempt from the P13 legacy-token grep
// oracle in docs/taxonomy/REMAINING_WORK_SPEC.md §9.4): this tool mutates live
// Firestore fragments; keep relation/class handling aligned with ah.FragmentClass
// and ah.RelationName, not ad hoc string literals outside this package.
//
// horizon_dedup_scanner finds potential duplicate ORG fragments that slipped past
// real-time dedup (OrgDeduplicator + SessionGraph). It groups fragments by
// NormalizeEntityName(legalName) and uses LLM to confirm whether candidates
// within the same group are the same real-world entity. Confirmed duplicates
// are merged: edges are reassigned, canonical path redirects are created, and
// aliases are stored on the winning fragment.
//
// Usage:
//   go run ./cmd/tools/horizon_dedup_scanner --project=<GCP_PROJECT>
//   go run ./cmd/tools/horizon_dedup_scanner --project=<GCP_PROJECT> --apply

var (
	projectID   = flag.String("project", "", "GCP project ID (required)")
	tenantID    = flag.String("tenant", "global", "Tenant ID")
	apply       = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	maxGroups   = flag.Int("max-groups", 0, "Stop after processing N groups (0 = unlimited)")
	minGroupLen = flag.Int("min-group", 2, "Minimum group size to consider (default: 2)")
	verbose     = flag.Bool("verbose", false, "Verbose logging")
	typeFilter  = flag.String("type", "ORGANIZATION", "Fragment type to scan")
)

type dedupGroup struct {
	Key       string
	Fragments []*models.HorizonFragment
}

type llmDedupResult struct {
	IsSameEntity bool    `json:"isSameEntity"`
	Confidence   float64 `json:"confidence"`
	Reason       string  `json:"reason"`
}

func main() {
	flag.Parse()
	if *projectID == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --project is required")
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	fragRepo := repo.NewHorizonFragmentsRepository(db)
	edgesRepo := repo.NewDependencyEdgesRepository(db)
	redirectsRepo := repo.NewCanonicalPathRedirectsRepository(db)

	aiExecutor := initAIExecutor(ctx)

	fragType := ah.FragmentClass(strings.TrimSpace(*typeFilter))
	log.Printf("=== Dedup Scanner: project=%s tenant=%s type=%s apply=%v ===", *projectID, *tenantID, fragType, *apply)

	groups := loadAndGroupFragments(ctx, db, fragType)
	log.Printf("Found %d normalized-name groups with 2+ fragments", len(groups))

	if *maxGroups > 0 && len(groups) > *maxGroups {
		groups = groups[:*maxGroups]
		log.Printf("Truncated to %d groups (--max-groups)", *maxGroups)
	}

	var totalConfirmed, totalSkipped, totalMerged int
	for i, group := range groups {
		log.Printf("[%d/%d] Group %q (%d fragments)", i+1, len(groups), group.Key, len(group.Fragments))
		if *verbose {
			for _, f := range group.Fragments {
				log.Printf("  - %s (path=%s, legal=%s, short=%s)",
					f.ID, f.CanonicalPath,
					f.Identifiers["legalName"], f.Identifiers["shortName"])
			}
		}

		if len(group.Fragments) < *minGroupLen {
			totalSkipped++
			continue
		}

		winner := selectWinner(group.Fragments)
		for _, loser := range group.Fragments {
			if loser.ID == winner.ID {
				continue
			}

			confirmed := true
			if aiExecutor != nil {
				result, llmErr := confirmWithLLM(ctx, aiExecutor, winner, loser)
				if llmErr != nil {
					log.Printf("  LLM error: %v — skipping pair", llmErr)
					totalSkipped++
					continue
				}
				if !result.IsSameEntity || result.Confidence < 0.7 {
					log.Printf("  LLM: NOT same entity (conf=%.2f, reason=%s)", result.Confidence, result.Reason)
					totalSkipped++
					continue
				}
				log.Printf("  LLM: CONFIRMED same entity (conf=%.2f, reason=%s)", result.Confidence, result.Reason)
			}

			if confirmed {
				totalConfirmed++
				if *apply {
					if mergeErr := mergeFragments(ctx, db, fragRepo, edgesRepo, redirectsRepo, winner, loser); mergeErr != nil {
						log.Printf("  MERGE ERROR: %v", mergeErr)
					} else {
						totalMerged++
						log.Printf("  MERGED: %s -> %s", loser.CanonicalPath, winner.CanonicalPath)
					}
				} else {
					log.Printf("  [DRY-RUN] Would merge: %s -> %s", loser.CanonicalPath, winner.CanonicalPath)
				}
			}
		}
	}

	log.Printf("=== DONE: groups=%d confirmed=%d skipped=%d merged=%d ===",
		len(groups), totalConfirmed, totalSkipped, totalMerged)
}

func initAIExecutor(_ context.Context) *ai.AITaskExecutor {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("VERTEX_API_KEY"))
	}
	if apiKey == "" {
		log.Println("WARN: No GEMINI_API_KEY or VERTEX_API_KEY — LLM confirmation disabled, candidates only")
		return nil
	}
	mm := ai.NewAIModelManager()
	return ai.NewAITaskExecutor([]ai.AIService{ai.NewGeminiService(apiKey, mm)}, mm)
}

func loadAndGroupFragments(ctx context.Context, db *firestore.Client, fragType ah.FragmentClass) []dedupGroup {
	col := db.Collection("horizon_fragments")
	q := col.Where("tenantId", "==", "global").Where("class", "==", string(fragType)).OrderBy("__name__", firestore.Asc)

	index := make(map[string][]*models.HorizonFragment)
	total := 0

	iter := q.Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("WARN: Firestore iteration error: %v", err)
			break
		}
		var frag models.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		total++
		if total%1000 == 0 {
			log.Printf("  loaded %d fragments...", total)
		}

		legalName := strings.TrimSpace(frag.Identifiers["legalName"])
		if legalName == "" {
			legalName = strings.TrimSpace(frag.Labels["en"])
		}
		if legalName == "" {
			continue
		}

		key := naming.NormalizeEntityName(legalName)
		if key == "" || len(key) < 3 {
			continue
		}
		index[key] = append(index[key], &frag)
	}
	log.Printf("Loaded %d fragments total", total)

	var groups []dedupGroup
	for key, frags := range index {
		if len(frags) >= 2 {
			groups = append(groups, dedupGroup{Key: key, Fragments: frags})
		}
	}
	return groups
}

func selectWinner(fragments []*models.HorizonFragment) *models.HorizonFragment {
	best := fragments[0]
	for _, f := range fragments[1:] {
		if fragmentQuality(f) > fragmentQuality(best) {
			best = f
		}
	}
	return best
}

func fragmentQuality(f *models.HorizonFragment) int {
	score := 0
	if f.Identifiers["wikidataId"] != "" {
		score += 100
	}
	if f.Identifiers["legalName"] != "" {
		score += 50
	}
	if f.Identifiers["shortName"] != "" {
		score += 30
	}
	if f.Labels["en"] != "" {
		score += 20
	}
	score += len(f.KBRefs) * 10
	if f.Description != "" {
		score += 10
	}
	score += len(f.State) * 5
	if !strings.HasPrefix(f.CanonicalPath, "/discovered/") {
		score += 40
	}
	return score
}

func confirmWithLLM(ctx context.Context, executor *ai.AITaskExecutor, a, b *models.HorizonFragment) (*llmDedupResult, error) {
	prompt := fmt.Sprintf(`You are an entity deduplication expert. Determine if these two database records refer to the same real-world organization.

Entity A:
- Canonical Path: %s
- Legal Name: %s
- Short Name: %s
- Label (EN): %s
- Description: %s
- Wikidata ID: %s

Entity B:
- Canonical Path: %s
- Legal Name: %s
- Short Name: %s
- Label (EN): %s
- Description: %s
- Wikidata ID: %s

Rules:
- If both have Wikidata IDs and they differ, they are DIFFERENT entities.
- Government bodies from different countries with similar names are DIFFERENT.
- Organizations with same name but different jurisdictions need strong evidence.
- Legal form variations (Inc, Corp, Ltd, PJSC, JSC, A.S.) are NOT differentiators.
- Transliteration variants of the same name ARE the same entity.

Return JSON only: {"isSameEntity": bool, "confidence": 0.0-1.0, "reason": "brief explanation"}`,
		a.CanonicalPath, a.Identifiers["legalName"], a.Identifiers["shortName"], a.Labels["en"], truncate(a.Description, 200), a.Identifiers["wikidataId"],
		b.CanonicalPath, b.Identifiers["legalName"], b.Identifiers["shortName"], b.Labels["en"], truncate(b.Description, 200), b.Identifiers["wikidataId"],
	)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeDomainClassification,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       7,
			MinContextWindow:         4000,
			MaxOutputSize:            512,
		},
		Verifier: ai.NewSimpleJSONVerifier(),
	}

	res, err := executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("LLM task failed: %w", err)
	}

	raw, _ := json.Marshal(res)
	var result llmDedupResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse LLM result: %w", err)
	}
	return &result, nil
}

func mergeFragments(
	ctx context.Context,
	db *firestore.Client,
	fragRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	redirectsRepo *repo.CanonicalPathRedirectsRepository,
	winner, loser *models.HorizonFragment,
) error {
	fragRepo.AppendDedupAliases(ctx, winner.ID, collectAltNames(loser), []string{loser.CanonicalPath})

	reassigned, deleted, err := reassignEdges(ctx, db, edgesRepo, *tenantID, loser.ID, winner.ID)
	if err != nil {
		return fmt.Errorf("reassign edges: %w", err)
	}
	log.Printf("    edges: reassigned=%d deleted(dup)=%d", reassigned, deleted)

	if err := redirectsRepo.Upsert(ctx, *tenantID, loser.CanonicalPath, winner.CanonicalPath, "dedup_scanner_llm", string(loser.Class)); err != nil {
		log.Printf("    WARN: redirect creation failed: %v", err)
	}

	_, err = db.Collection("horizon_fragments").Doc(loser.ID).Update(ctx, []firestore.Update{
		{Path: "status", Value: "deprecated"},
		{Path: "updatedAt", Value: time.Now()},
		{Path: "qualifiers.mergedInto", Value: winner.CanonicalPath},
		{Path: "qualifiers.mergedAt", Value: time.Now().Format(time.RFC3339)},
		{Path: "qualifiers.mergeReason", Value: "dedup_scanner_llm"},
	})
	if err != nil {
		return fmt.Errorf("deprecate loser: %w", err)
	}
	return nil
}

func reassignEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID, oldID, newID string) (reassigned, deleted int, err error) {
	col := db.Collection("horizon_dependency_edges")

	for _, field := range []string{"sourceFragmentId", "targetFragmentId"} {
		docs, qErr := col.Where("tenantId", "==", tenantID).Where(field, "==", oldID).Documents(ctx).GetAll()
		if qErr != nil {
			err = qErr
			return
		}
		for _, doc := range docs {
			var edge models.DependencyEdge
			if dErr := doc.DataTo(&edge); dErr != nil {
				continue
			}
			newSource := edge.SourceFragmentID
			newTarget := edge.TargetFragmentID
			if field == "sourceFragmentId" {
				newSource = newID
			} else {
				newTarget = newID
			}

			if newSource == newTarget {
				if _, delErr := doc.Ref.Delete(ctx); delErr == nil {
					deleted++
				}
				continue
			}

			existing, _ := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, newSource, newTarget, edge.Relation)
			if existing != nil {
				if _, delErr := doc.Ref.Delete(ctx); delErr == nil {
					deleted++
				}
				continue
			}

			updates := []firestore.Update{
				{Path: field, Value: newID},
				{Path: "updatedAt", Value: time.Now()},
			}
			if _, uErr := doc.Ref.Update(ctx, updates); uErr == nil {
				reassigned++
			}
		}
	}
	return
}

func collectAltNames(f *models.HorizonFragment) []string {
	var names []string
	if ln := strings.TrimSpace(f.Identifiers["legalName"]); ln != "" {
		names = append(names, ln)
	}
	if sn := strings.TrimSpace(f.Identifiers["shortName"]); sn != "" {
		names = append(names, sn)
	}
	if label := strings.TrimSpace(f.Labels["en"]); label != "" {
		names = append(names, label)
	}
	return names
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
