package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID  = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID   = flag.String("tenant", "global", "tenant id")
	lensID     = flag.String("lens-id", "", "optional lens id filter")
	limit      = flag.Int("limit", 5000, "max artifacts to scan")
	apply      = flag.Bool("apply", false, "persist rematerialized artifacts")
	onlyLegacy = flag.Bool("only-legacy", false, "only rebuild artifacts whose canonical key lacks sig-* suffix")
	timeout    = flag.Duration("timeout", 4*time.Hour, "overall timeout")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	artifactsRepo := repo.NewMaterializedArtifactsRepository(db)
	fragmentsRepo := repo.NewHorizonFragmentsRepository(db)
	edgesRepo := repo.NewDependencyEdgesRepository(db)
	analyticalPrimitivesRepo := repo.NewAnalyticalPrimitivesRepository(db)
	analyticalAggregatesRepo := repo.NewAnalyticalAggregatesRepository(db)
	refScopeProjectionsRepo := repo.NewRefScopeProjectionsRepository(db)
	pathFamilyProjectionsRepo := repo.NewPathFamilyProjectionsRepository(db)
	compiledWatchSubjectsRepo := repo.NewCompiledWatchSubjectsRepository(db)
	watchSubjectsRepo := repo.NewWatchSubjectsRepository(db)
	builder := pathbuilder.NewSupplyChainPathBuilder(fragmentsRepo, edgesRepo, artifactsRepo).
		WithAnalyticalPrimitivesRepository(analyticalPrimitivesRepo).
		WithAnalyticalAggregatesRepository(analyticalAggregatesRepo).
		WithRefScopeProjectionsRepository(refScopeProjectionsRepo).
		WithPathFamilyProjectionsRepository(pathFamilyProjectionsRepo).
		WithCompiledWatchSubjectsRepository(compiledWatchSubjectsRepo).
		WithWatchSubjectsRepository(watchSubjectsRepo)

	items, err := loadArtifacts(ctx, artifactsRepo, strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), *limit)
	if err != nil {
		log.Fatalf("load artifacts failed: %v", err)
	}

	scanned := 0
	eligible := 0
	rebuilt := 0
	skippedNoCustomer := 0
	skippedAlreadySig := 0
	errorsCount := 0

	for _, artifact := range items {
		if artifact == nil || artifact.Kind != models.MaterializedKindSupplyChainPath {
			continue
		}
		scanned++
		if *onlyLegacy && strings.Contains(strings.TrimSpace(artifact.CanonicalKey), "/sig-") {
			skippedAlreadySig++
			continue
		}
		leafCustomer := extractLeafCustomerRef(artifact)
		if leafCustomer == "" {
			skippedNoCustomer++
			continue
		}
		eligible++
		if !*apply {
			log.Printf("candidate id=%s lens=%s key=%s leaf=%s", strings.TrimSpace(artifact.ID), strings.TrimSpace(artifact.LensID), strings.TrimSpace(artifact.CanonicalKey), leafCustomer)
			continue
		}
		if err := builder.RebuildFromSupplyLinks(ctx, artifact, leafCustomer); err != nil {
			errorsCount++
			log.Printf("rebuild failed id=%s lens=%s key=%s err=%v", strings.TrimSpace(artifact.ID), strings.TrimSpace(artifact.LensID), strings.TrimSpace(artifact.CanonicalKey), err)
			continue
		}
		rebuilt++
		if rebuilt%100 == 0 {
			log.Printf("progress scanned=%d eligible=%d rebuilt=%d errors=%d", scanned, eligible, rebuilt, errorsCount)
		}
	}

	log.Printf(
		"rematerialize summary tenant=%s lens=%s scanned=%d eligible=%d rebuilt=%d skipped_no_customer=%d skipped_already_sig=%d errors=%d apply=%t",
		strings.TrimSpace(*tenantID),
		strings.TrimSpace(*lensID),
		scanned,
		eligible,
		rebuilt,
		skippedNoCustomer,
		skippedAlreadySig,
		errorsCount,
		*apply,
	)
}

func loadArtifacts(
	ctx context.Context,
	artifactsRepo *repo.MaterializedArtifactsRepository,
	tenantID string,
	lensID string,
	limit int,
) ([]*models.MaterializedArtifact, error) {
	if strings.TrimSpace(lensID) != "" {
		return artifactsRepo.ListByLens(ctx, strings.TrimSpace(lensID), limit)
	}
	return artifactsRepo.ListByTenantAndKind(ctx, strings.TrimSpace(tenantID), models.MaterializedKindSupplyChainPath, limit)
}

func extractLeafCustomerRef(artifact *models.MaterializedArtifact) string {
	if artifact == nil {
		return ""
	}
	if ref := strings.TrimSpace(artifact.AnchorCustomer); ref != "" {
		return ref
	}
	if artifact.Labels != nil {
		if ref := strings.TrimSpace(artifact.Labels["anchorCustomer"]); ref != "" {
			return ref
		}
	}
	parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil || len(parsed.Legs) == 0 {
		return ""
	}
	return strings.TrimSpace(parsed.Legs[len(parsed.Legs)-1].CustomerRef)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
