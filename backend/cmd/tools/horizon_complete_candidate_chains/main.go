package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	horizongcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplychain"
)

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	lensID := flag.String("lens", "l8dOd17uKYYTKD0CI9hy", "lens id")
	maxArtifacts := flag.Int("max-artifacts-per-chain", 4, "max source artifacts to merge into one candidate chain")
	maxCandidates := flag.Int("max-candidates", 8, "max candidate chains to create/update")
	minUnionCountries := flag.Int("min-union-countries", 3, "minimum distinct countries across source artifacts")
	dryRun := flag.Bool("dry-run", false, "print candidates without writing")
	flag.Parse()

	ctx := context.Background()
	fs, err := horizongcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	service := supplychain.NewCandidateChainCompletionService(supplychain.CandidateChainCompletionDeps{
		LensesRepo:       horizonrepo.NewLensesRepository(fs.Client()),
		MaterializedRepo: horizonrepo.NewMaterializedArtifactsRepository(fs.Client()),
	})
	if service == nil {
		log.Fatalf("candidate completion service is not configured")
	}

	result, err := service.CompleteLens(ctx, supplychain.CandidateChainCompletionOptions{
		LensID:               strings.TrimSpace(*lensID),
		MaxArtifactsPerChain: *maxArtifacts,
		MaxCandidates:        *maxCandidates,
		MinUnionCountries:    *minUnionCountries,
		DryRun:               *dryRun,
	})
	if err != nil {
		log.Fatalf("candidate completion failed: %v", err)
	}

	fmt.Printf("lens=%s created=%d updated=%d skipped=%d dryRun=%v\n", result.LensID, result.Created, result.Updated, result.Skipped, *dryRun)
	for idx := range result.CandidateKeys {
		title := ""
		if idx < len(result.CandidateTitles) {
			title = result.CandidateTitles[idx]
		}
		fmt.Printf("%02d key=%s title=%q\n", idx+1, result.CandidateKeys[idx], title)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
