package main

import (
	"context"
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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID      = flag.String("project", "", "GCP project ID (required)")
	tenantID       = flag.String("tenant", "global", "Tenant ID (default: global)")
	daysBack       = flag.Int("days", 4, "Delete data created in the last N days")
	apply          = flag.Bool("apply", false, "Apply deletions (default: dry-run)")
	dryRun         = flag.Bool("dry-run", true, "Dry run mode: report only")
	skipVertexSync = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync")
	ragBucket      = flag.String("rag-bucket", "", "GCS bucket for fragments (default: {project}-rag-documents)")
	sampleLimit    = flag.Int("sample-limit", 10, "Max sample IDs to log per collection")
	skipVersions   = flag.Bool("skip-versions", false, "Skip cleanup of fragment versions")
)

type stats struct {
	scanned int
	matched int
	deleted int
	failed  int
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *daysBack <= 0 {
		log.Fatal("--days must be > 0")
	}
	if *apply {
		*dryRun = false
	}
	if *dryRun {
		*apply = false
	}

	ctx := context.Background()
	cutoff := time.Now().UTC().Add(-time.Duration(*daysBack) * 24 * time.Hour)

	log.Printf("Horizon cleanup: tenant=%s days=%d cutoff=%s dry-run=%v",
		strings.TrimSpace(*tenantID), *daysBack, cutoff.Format(time.RFC3339), *dryRun)

	// Initialize Firestore
	fsClient, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	edgesRepo := repo.NewDependencyEdgesRepository(fsClient)
	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient).WithEdgesRepo(edgesRepo)
	lensesRepo := repo.NewLensesRepository(fsClient)

	var vertexBatcher *repo.VertexSyncBatcher
	if *apply {
		// Initialize GCS client for fragment deletions.
		gcsClient, gerr := gcp.NewGCSClient(ctx, *projectID)
		if gerr != nil {
			log.Printf("WARNING: failed to init GCS client (skipping GCS delete): %v", gerr)
		} else {
			bucketName := strings.TrimSpace(*ragBucket)
			if bucketName == "" {
				bucketName = strings.TrimSpace(os.Getenv("RAG_DOCUMENTS_BUCKET"))
			}
			if bucketName == "" {
				bucketName = fmt.Sprintf("%s-rag-documents", strings.TrimSpace(*projectID))
			}
			if bucketName != "" {
				fragmentsRepo = fragmentsRepo.WithStorage(gcsClient, bucketName)
				log.Printf("GCS delete enabled: bucket=%s", bucketName)
			}
		}

		if !*skipVertexSync {
			vertexStoreID := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
			if vertexStoreID == "" {
				vertexStoreID = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"))
			}
			vertexLoc := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION"))
			if vertexLoc == "" {
				vertexLoc = "global"
			}
			if vertexStoreID != "" {
				vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexLoc, vertexStoreID, "")
				if vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
					vertexBatcher = repo.NewVertexSyncBatcher(
						func(ctx context.Context, uris []string) (string, error) {
							return vertexSearchClient.ImportDocumentsFromURIs(ctx, uris, "INCREMENTAL", "document")
						},
						repo.DefaultVertexSyncBatcherConfig(),
					)
					fragmentsRepo = fragmentsRepo.WithVertexSyncBatcher(vertexBatcher)
					log.Printf("Vertex AI Search sync enabled (batched)")
				}
			}
		}
	}

	fragmentsStats, deletedFragmentIDs := cleanupFragments(ctx, fsClient, fragmentsRepo, cutoff, *tenantID, *dryRun, *sampleLimit)
	edgesStats := cleanupEdges(ctx, fsClient, edgesRepo, cutoff, *tenantID, *dryRun, *sampleLimit)
	lensesStats, deletedLensIDs := cleanupLenses(ctx, fsClient, lensesRepo, cutoff, *tenantID, *dryRun, *sampleLimit)
	exposureStats := cleanupExposurePaths(ctx, fsClient, cutoff, *tenantID, deletedLensIDs, *dryRun, *sampleLimit)
	versionsStats := stats{}
	if !*skipVersions {
		versionsStats = cleanupFragmentVersions(ctx, fsClient, cutoff, *tenantID, deletedFragmentIDs, *dryRun, *sampleLimit)
	}

	if vertexBatcher != nil {
		vertexBatcher.FlushSync()
	}

	log.Println("\n=== Summary ===")
	log.Printf("Fragments: scanned=%d matched=%d deleted=%d failed=%d", fragmentsStats.scanned, fragmentsStats.matched, fragmentsStats.deleted, fragmentsStats.failed)
	log.Printf("Edges: scanned=%d matched=%d deleted=%d failed=%d", edgesStats.scanned, edgesStats.matched, edgesStats.deleted, edgesStats.failed)
	log.Printf("Lenses: scanned=%d matched=%d deleted=%d failed=%d", lensesStats.scanned, lensesStats.matched, lensesStats.deleted, lensesStats.failed)
	log.Printf("ExposurePaths: scanned=%d matched=%d deleted=%d failed=%d", exposureStats.scanned, exposureStats.matched, exposureStats.deleted, exposureStats.failed)
	log.Printf("FragmentVersions: scanned=%d matched=%d deleted=%d failed=%d", versionsStats.scanned, versionsStats.matched, versionsStats.deleted, versionsStats.failed)
	if *dryRun {
		log.Println("[DRY-RUN] No deletions performed. Use --apply to delete.")
	}
}

func cleanupFragments(
	ctx context.Context,
	fsClient *firestore.Client,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	cutoff time.Time,
	tenant string,
	dryRun bool,
	sampleLimit int,
) (stats, []string) {
	out := stats{}
	deletedIDs := make([]string, 0)
	samples := 0

	q := fsClient.Collection("horizon_fragments").
		Where("tenantId", "==", tenant).
		Where("createdAt", ">=", cutoff)
	iter := q.Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("ERROR: fragment iteration failed: %v", err)
			out.failed++
			break
		}
		out.scanned++
		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			out.failed++
			continue
		}
		if strings.TrimSpace(fragment.ID) == "" {
			fragment.ID = doc.Ref.ID
		}
		out.matched++
		if dryRun {
			if samples < sampleLimit {
				log.Printf("[DRY-RUN] fragment %s canonicalPath=%s createdAt=%s", fragment.ID, fragment.CanonicalPath, fragment.CreatedAt.Format(time.RFC3339))
				samples++
			}
			continue
		}
		if err := fragmentsRepo.Delete(ctx, fragment.ID); err != nil {
			log.Printf("ERROR: delete fragment %s: %v", fragment.ID, err)
			out.failed++
			continue
		}
		out.deleted++
		deletedIDs = append(deletedIDs, fragment.ID)
	}

	return out, deletedIDs
}

func cleanupEdges(
	ctx context.Context,
	fsClient *firestore.Client,
	edgesRepo *repo.DependencyEdgesRepository,
	cutoff time.Time,
	tenant string,
	dryRun bool,
	sampleLimit int,
) stats {
	out := stats{}
	samples := 0

	q := fsClient.Collection("horizon_dependency_edges").
		Where("tenantId", "==", tenant).
		Where("metadata.createdAt", ">=", cutoff)
	iter := q.Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("ERROR: edge iteration failed: %v", err)
			out.failed++
			break
		}
		out.scanned++
		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			out.failed++
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = doc.Ref.ID
		}
		out.matched++
		if dryRun {
			if samples < sampleLimit {
				log.Printf("[DRY-RUN] edge %s type=%s createdAt=%s", edge.ID, edge.Type, edge.Metadata.CreatedAt.Format(time.RFC3339))
				samples++
			}
			continue
		}
		if err := edgesRepo.Delete(ctx, edge.ID); err != nil {
			log.Printf("ERROR: delete edge %s: %v", edge.ID, err)
			out.failed++
			continue
		}
		out.deleted++
	}

	return out
}

func cleanupLenses(
	ctx context.Context,
	fsClient *firestore.Client,
	lensesRepo *repo.LensesRepository,
	cutoff time.Time,
	tenant string,
	dryRun bool,
	sampleLimit int,
) (stats, []string) {
	out := stats{}
	deletedIDs := make([]string, 0)
	samples := 0

	q := fsClient.Collection("horizon_lenses").
		Where("tenantId", "==", tenant).
		Where("createdAt", ">=", cutoff)
	iter := q.Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("ERROR: lens iteration failed: %v", err)
			out.failed++
			break
		}
		out.scanned++
		var lens models.Lens
		if err := doc.DataTo(&lens); err != nil {
			out.failed++
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		out.matched++
		if dryRun {
			if samples < sampleLimit {
				log.Printf("[DRY-RUN] lens %s name=%s createdAt=%s", lens.ID, lens.Name, lens.CreatedAt.Format(time.RFC3339))
				samples++
			}
			continue
		}
		if err := lensesRepo.Delete(ctx, lens.ID); err != nil {
			log.Printf("ERROR: delete lens %s: %v", lens.ID, err)
			out.failed++
			continue
		}
		out.deleted++
		deletedIDs = append(deletedIDs, lens.ID)
	}

	return out, deletedIDs
}

func cleanupExposurePaths(
	ctx context.Context,
	fsClient *firestore.Client,
	cutoff time.Time,
	tenant string,
	deletedLensIDs []string,
	dryRun bool,
	sampleLimit int,
) stats {
	out := stats{}
	samples := 0
	deletedLensSet := map[string]struct{}{}
	for _, id := range deletedLensIDs {
		if strings.TrimSpace(id) != "" {
			deletedLensSet[id] = struct{}{}
		}
	}

	q := fsClient.Collection("horizon_exposure_paths").
		Where("tenantId", "==", tenant).
		Where("createdAt", ">=", cutoff)
	iter := q.Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("ERROR: exposure path iteration failed: %v", err)
			out.failed++
			break
		}
		out.scanned++
		var exposure models.ExposurePathArtifact
		if err := doc.DataTo(&exposure); err != nil {
			out.failed++
			continue
		}
		if strings.TrimSpace(exposure.ID) == "" {
			exposure.ID = doc.Ref.ID
		}
		out.matched++
		if dryRun {
			if samples < sampleLimit {
				log.Printf("[DRY-RUN] exposurePath %s lensId=%s createdAt=%s", exposure.ID, exposure.LensID, exposure.CreatedAt.Format(time.RFC3339))
				samples++
			}
			continue
		}
		if _, err := fsClient.Collection("horizon_exposure_paths").Doc(exposure.ID).Delete(ctx); err != nil {
			log.Printf("ERROR: delete exposure path %s: %v", exposure.ID, err)
			out.failed++
			continue
		}
		out.deleted++
	}

	// Also delete exposure paths for deleted lenses even if createdAt is older.
	if len(deletedLensSet) == 0 {
		return out
	}
	for lensID := range deletedLensSet {
		qLens := fsClient.Collection("horizon_exposure_paths").
			Where("tenantId", "==", tenant).
			Where("lensId", "==", lensID)
		iterLens := qLens.Documents(ctx)
		for {
			doc, err := iterLens.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("ERROR: exposure path iteration (lens %s) failed: %v", lensID, err)
				out.failed++
				break
			}
			out.scanned++
			var exposure models.ExposurePathArtifact
			if err := doc.DataTo(&exposure); err != nil {
				out.failed++
				continue
			}
			if strings.TrimSpace(exposure.ID) == "" {
				exposure.ID = doc.Ref.ID
			}
			out.matched++
			if dryRun {
				if samples < sampleLimit {
					log.Printf("[DRY-RUN] exposurePath %s lensId=%s (lens cleanup)", exposure.ID, lensID)
					samples++
				}
				continue
			}
			if _, err := fsClient.Collection("horizon_exposure_paths").Doc(exposure.ID).Delete(ctx); err != nil {
				log.Printf("ERROR: delete exposure path %s: %v", exposure.ID, err)
				out.failed++
				continue
			}
			out.deleted++
		}
		iterLens.Stop()
	}

	return out
}

func cleanupFragmentVersions(
	ctx context.Context,
	fsClient *firestore.Client,
	cutoff time.Time,
	tenant string,
	deletedFragmentIDs []string,
	dryRun bool,
	sampleLimit int,
) stats {
	out := stats{}
	samples := 0
	versionsCol := fsClient.Collection("horizon_fragment_versions")

	for _, fragmentID := range deletedFragmentIDs {
		fragmentID = strings.TrimSpace(fragmentID)
		if fragmentID == "" {
			continue
		}
		q := versionsCol.Where("tenantId", "==", tenant).Where("fragmentId", "==", fragmentID)
		iter := q.Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Printf("ERROR: versions iteration failed (fragment %s): %v", fragmentID, err)
				out.failed++
				break
			}
			out.scanned++
			var version repo.HorizonFragmentVersion
			if err := doc.DataTo(&version); err != nil {
				out.failed++
				continue
			}
			out.matched++
			if dryRun {
				if samples < sampleLimit {
					log.Printf("[DRY-RUN] fragment version %s fragmentId=%s createdAt=%s", doc.Ref.ID, fragmentID, version.CreatedAt.Format(time.RFC3339))
					samples++
				}
				continue
			}
			if _, err := versionsCol.Doc(doc.Ref.ID).Delete(ctx); err != nil {
				log.Printf("ERROR: delete fragment version %s: %v", doc.Ref.ID, err)
				out.failed++
				continue
			}
			out.deleted++
		}
		iter.Stop()
	}

	// Delete recent versions even if fragments are untouched (optional cleanup of recent mutations).
	q := versionsCol.Where("tenantId", "==", tenant).Where("createdAt", ">=", cutoff)
	iter := q.Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("ERROR: versions iteration failed (recent): %v", err)
			out.failed++
			break
		}
		out.scanned++
		var version repo.HorizonFragmentVersion
		if err := doc.DataTo(&version); err != nil {
			out.failed++
			continue
		}
		out.matched++
		if dryRun {
			if samples < sampleLimit {
				log.Printf("[DRY-RUN] recent fragment version %s fragmentId=%s createdAt=%s", doc.Ref.ID, version.FragmentID, version.CreatedAt.Format(time.RFC3339))
				samples++
			}
			continue
		}
		if _, err := versionsCol.Doc(doc.Ref.ID).Delete(ctx); err != nil {
			log.Printf("ERROR: delete fragment version %s: %v", doc.Ref.ID, err)
			out.failed++
			continue
		}
		out.deleted++
	}
	iter.Stop()

	return out
}
