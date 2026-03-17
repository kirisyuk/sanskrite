package main

import (
	"context"
	"flag"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "etigram", "GCP project id")
		tenantID  = flag.String("tenant", "global", "tenant id")
		limit     = flag.Int("limit", 2000, "max affected zones to scan")
		apply     = flag.Bool("apply", false, "apply repair actions")
	)
	flag.Parse()

	ctx := context.Background()
	project := strings.TrimSpace(*projectID)
	if project == "" {
		log.Fatal("--project is required")
	}

	client, err := gcp.NewFirestoreClient(ctx, project)
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer client.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(client.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(client.Client())
	redirectsRepo := horizonrepo.NewCanonicalPathRedirectsRepository(client.Client())
	materializedRepo := horizonrepo.NewMaterializedArtifactsRepository(client.Client())
	lensesRepo := horizonrepo.NewLensesRepository(client.Client())
	watchSubjectsRepo := horizonrepo.NewWatchSubjectsRepository(client.Client())
	lensArtifactsRepo := horizonrepo.NewLensArtifactsRepository(client.Client())
	framesRepo := horizonrepo.NewFramesRepository(client.Client())
	mergeAuditRepo := horizonrepo.NewFragmentMergeAuditRepository(client.Client())
	mergeRepo := horizonrepo.NewFragmentsMergeRepository(fragmentsRepo, edgesRepo, mergeAuditRepo).
		WithRedirectsRepo(redirectsRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithWatchSubjectsRepo(watchSubjectsRepo).
		WithOntologyFactsRepo(corerepo.NewOntologyFactsRepository(client.Client())).
		WithEntitiesRepo(corerepo.NewEntityRepository(client.Client()))
	migrator := horizonrepo.NewFragmentPathMigrator(client.Client(), fragmentsRepo, redirectsRepo).
		WithMergeRepo(mergeRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithLensArtifactsRepo(lensArtifactsRepo).
		WithFramesRepo(framesRepo)

	var vertexClient *ai.VertexAISearchClient
	vertexProject := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, project, "VERTEX_SEARCH_PROJECT_ID", "vertex_search_project_id"))
	if vertexProject == "" {
		vertexProject = project
	}
	vertexLocation := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, project, "VERTEX_SEARCH_LOCATION", "vertex_search_location"))
	if vertexLocation == "" {
		vertexLocation = "global"
	}
	vertexStore := strings.TrimSpace(os.Getenv("VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID"))
	if vertexStore == "" {
		vertexStore = strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"))
	}
	if vertexStore == "" {
		vertexStore = strings.TrimSpace(gcp.GetEnvOrSecret(ctx, project, "VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID", "vertex_search_etigram_data_store_id"))
	}
	if vertexStore == "" {
		vertexStore = "etigram-fragments-store"
	}
	vertexClient = ai.NewVertexAISearchClient(vertexProject, vertexLocation, vertexStore, "")

	factsRepo := corerepo.NewOntologyFactsRepository(client.Client())
	pipeline := discovery.NewHorizonMonitorPipeline(factsRepo, nil, fragmentsRepo, edgesRepo, nil, nil, log.Default()).
		WithVertexSearch(vertexClient)
	repairer := discovery.NewAffectedZoneRepairer(pipeline, migrator)

	items, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), horizonmodels.FragmentTypeAffectedZone, *limit)
	if err != nil {
		log.Fatalf("list affected zones: %v", err)
	}

	var scanned, updated, cooled, archived, retargeted, merged int
	reasons := map[string]int{}
	samples := make([]string, 0, 20)
	now := time.Now().UTC()
	for _, frag := range items {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		scanned++
		itemCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		res, err := repairer.RepairFragment(itemCtx, strings.TrimSpace(*tenantID), frag, now, "affected_zone_repair", *apply)
		cancel()
		if err != nil {
			log.Printf("repair failed id=%s path=%s err=%v", frag.ID, frag.CanonicalPath, err)
			if scanned%25 == 0 {
				log.Printf("progress scanned=%d updated=%d cooled=%d archived=%d retargeted=%d merged=%d", scanned, updated, cooled, archived, retargeted, merged)
			}
			continue
		}
		if res == nil || res.Action == discovery.AffectedZoneRepairNoop {
			if scanned%25 == 0 {
				log.Printf("progress scanned=%d updated=%d cooled=%d archived=%d retargeted=%d merged=%d", scanned, updated, cooled, archived, retargeted, merged)
			}
			continue
		}
		reasons[res.Reason]++
		switch res.Action {
		case discovery.AffectedZoneRepairUpdated:
			updated++
		case discovery.AffectedZoneRepairCooled:
			cooled++
		case discovery.AffectedZoneRepairArchived:
			archived++
		case discovery.AffectedZoneRepairRetarget:
			retargeted++
		}
		if res.Merged {
			merged++
		}
		if len(samples) < 20 {
			samples = append(samples, string(res.Action)+" | "+res.OldPath+" -> "+res.NewPath+" | "+res.Reason)
		}
		if scanned%25 == 0 {
			log.Printf("progress scanned=%d updated=%d cooled=%d archived=%d retargeted=%d merged=%d", scanned, updated, cooled, archived, retargeted, merged)
		}
	}

	sort.Strings(samples)
	for _, sample := range samples {
		log.Printf("sample: %s", sample)
	}
	reasonKeys := make([]string, 0, len(reasons))
	for reason := range reasons {
		reasonKeys = append(reasonKeys, reason)
	}
	sort.Strings(reasonKeys)
	for _, reason := range reasonKeys {
		log.Printf("reason[%s]=%d", reason, reasons[reason])
	}
	log.Printf("summary scanned=%d updated=%d cooled=%d archived=%d retargeted=%d merged=%d apply=%v", scanned, updated, cooled, archived, retargeted, merged, *apply)
}
