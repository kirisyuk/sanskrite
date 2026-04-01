package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/lensresearch"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type lensRunSummary struct {
	LensID         string `json:"lensId"`
	Name           string `json:"name"`
	TenantID       string `json:"tenantId"`
	ProjectID      string `json:"projectId"`
	BeforeCount    int    `json:"beforeCount"`
	RecoveredCount int    `json:"recoveredCount"`
	AfterCount     int    `json:"afterCount"`
	Changed        bool   `json:"changed"`
}

type runSummary struct {
	ProjectID       string           `json:"projectId"`
	Apply           bool             `json:"apply"`
	ProcessedLenses int              `json:"processedLenses"`
	ChangedLenses   int              `json:"changedLenses"`
	RecoveredRefs   int              `json:"recoveredRefs"`
	Lenses          []lensRunSummary `json:"lenses,omitempty"`
	GeneratedAt     string           `json:"generatedAt"`
}

func main() {
	ctx := context.Background()

	projectID := flag.String("project", firstNonEmpty(strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")), "nv-horizon"), "GCP project id")
	lensID := flag.String("lens", "", "optional single lens id")
	tenantID := flag.String("tenant", "", "optional tenant filter")
	status := flag.String("status", "", "optional status filter")
	apply := flag.Bool("apply", false, "persist recovered refs into lens.researchObjects")
	limitLenses := flag.Int("limit-lenses", 0, "optional max number of lenses to process")
	limitArtifacts := flag.Int("limit-artifacts", 4096, "max lens/materialized artifacts per lens")
	jsonOut := flag.Bool("json", true, "emit JSON")
	verbose := flag.Bool("verbose", false, "log progress to stderr")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		fmt.Fprintln(os.Stderr, "--project is required")
		os.Exit(2)
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		fmt.Fprintf(os.Stderr, "firestore init failed: %v\n", err)
		os.Exit(1)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	lensesRepo := repo.NewLensesRepository(db)
	lensArtifactsRepo := repo.NewLensArtifactsRepository(db)
	materializedRepo := repo.NewMaterializedArtifactsRepository(db)

	logf := func(format string, args ...interface{}) {
		if *verbose {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		}
	}

	lenses, err := loadTargetLenses(ctx, db, lensesRepo, strings.TrimSpace(*lensID), strings.TrimSpace(*tenantID), strings.TrimSpace(*status), *limitLenses)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load lenses failed: %v\n", err)
		os.Exit(1)
	}

	out := runSummary{
		ProjectID:   strings.TrimSpace(*projectID),
		Apply:       *apply,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, lens := range lenses {
		if lens == nil || strings.TrimSpace(lens.ID) == "" {
			continue
		}
		logf("processing lens=%s name=%q", lens.ID, lens.Name)
		recovered, err := recoverLensResearchObjects(ctx, lens, lensArtifactsRepo, materializedRepo, *limitArtifacts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "recover lens=%s failed: %v\n", lens.ID, err)
			os.Exit(1)
		}
		mergedPreview := lensresearch.MergeResearchObjects(lens.ResearchObjects, recovered)
		item := lensRunSummary{
			LensID:         strings.TrimSpace(lens.ID),
			Name:           strings.TrimSpace(lens.Name),
			TenantID:       strings.TrimSpace(lens.TenantID),
			ProjectID:      strings.TrimSpace(lens.ProjectID),
			BeforeCount:    len(lens.ResearchObjects),
			RecoveredCount: len(recovered),
			AfterCount:     len(mergedPreview),
			Changed:        len(mergedPreview) > len(lens.ResearchObjects),
		}
		if *apply && len(recovered) > 0 {
			if err := lensesRepo.MergeResearchObjects(ctx, lens.ID, recovered); err != nil {
				fmt.Fprintf(os.Stderr, "persist lens=%s failed: %v\n", lens.ID, err)
				os.Exit(1)
			}
		}
		out.ProcessedLenses++
		if item.Changed {
			out.ChangedLenses++
		}
		out.RecoveredRefs += item.RecoveredCount
		out.Lenses = append(out.Lenses, item)
	}

	sort.Slice(out.Lenses, func(i, j int) bool {
		if out.Lenses[i].AfterCount == out.Lenses[j].AfterCount {
			return out.Lenses[i].LensID < out.Lenses[j].LensID
		}
		return out.Lenses[i].AfterCount > out.Lenses[j].AfterCount
	})

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "encode output failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("project=%s apply=%t processed=%d changed=%d recoveredRefs=%d\n", out.ProjectID, out.Apply, out.ProcessedLenses, out.ChangedLenses, out.RecoveredRefs)
	for _, item := range out.Lenses {
		fmt.Printf("lens=%s before=%d recovered=%d after=%d changed=%t name=%q\n", item.LensID, item.BeforeCount, item.RecoveredCount, item.AfterCount, item.Changed, item.Name)
	}
}

func loadTargetLenses(ctx context.Context, db *firestore.Client, lensesRepo *repo.LensesRepository, lensID, tenantID, status string, limit int) ([]*models.Lens, error) {
	if lensID != "" {
		lens, err := lensesRepo.Get(ctx, lensID)
		if err != nil {
			return nil, err
		}
		return []*models.Lens{lens}, nil
	}

	iter := db.Collection("horizon_lenses").Documents(ctx)
	defer iter.Stop()

	out := make([]*models.Lens, 0, 128)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var lens models.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		if tenantID != "" && strings.TrimSpace(lens.TenantID) != tenantID {
			continue
		}
		if status != "" && !strings.EqualFold(strings.TrimSpace(lens.Status), status) {
			continue
		}
		out = append(out, &lens)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func recoverLensResearchObjects(
	ctx context.Context,
	lens *models.Lens,
	lensArtifactsRepo *repo.LensArtifactsRepository,
	materializedRepo *repo.MaterializedArtifactsRepository,
	limitArtifacts int,
) (map[string]models.LensResearchObjectRef, error) {
	recovered := lensresearch.BuildFromScopeArtifacts(lens.ScopeArtifacts, lens.UpdatedAt)

	lensArtifacts, err := lensArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), limitArtifacts)
	if err != nil {
		return nil, err
	}
	recovered = lensresearch.MergeResearchObjects(recovered, lensresearch.BuildFromLensArtifacts(lensArtifacts))

	materialized, err := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), limitArtifacts)
	if err != nil {
		return nil, err
	}
	recovered = lensresearch.MergeResearchObjects(recovered, lensresearch.BuildFromMaterializedArtifacts(materialized))
	return recovered, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
