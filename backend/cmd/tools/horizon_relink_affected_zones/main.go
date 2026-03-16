package main

import (
	"context"
	"flag"
	"log"
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
	projectID := flag.String("project", "etigram", "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	limit := flag.Int("limit", 500, "max affected zones to scan")
	onlyActive := flag.Bool("active-only", true, "process only non-archived zones")
	zonesArg := flag.String("zones", "", "optional comma-separated canonical paths to restrict relink")
	flag.Parse()

	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer client.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(client.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(client.Client())
	factsRepo := corerepo.NewOntologyFactsRepository(client.Client())

	vertexProject := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "VERTEX_SEARCH_PROJECT_ID", "vertex_search_project_id"))
	if vertexProject == "" {
		vertexProject = strings.TrimSpace(*projectID)
	}
	vertexLocation := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "VERTEX_SEARCH_LOCATION", "vertex_search_location"))
	if vertexLocation == "" {
		vertexLocation = "global"
	}
	vertexStore := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "VERTEX_SEARCH_ETIGRAM_DATA_STORE_ID", "vertex_search_etigram_data_store_id"))
	if vertexStore == "" {
		vertexStore = "etigram-fragments-store"
	}
	vertexClient := ai.NewVertexAISearchClient(vertexProject, vertexLocation, vertexStore, "")

	pipeline := discovery.NewHorizonMonitorPipeline(factsRepo, nil, fragmentsRepo, edgesRepo, nil, nil, log.Default()).
		WithVertexSearch(vertexClient)
	repairer := discovery.NewAffectedZoneRepairer(pipeline, nil)

	allowed := parseZones(*zonesArg)
	items, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), horizonmodels.FragmentTypeAffectedZone, *limit)
	if err != nil {
		log.Fatalf("list affected zones: %v", err)
	}

	var scanned, relinked, skipped, failed int
	for _, frag := range items {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		if *onlyActive && strings.EqualFold(strings.TrimSpace(frag.Status), string(horizonmodels.FragmentStatusArchived)) {
			skipped++
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[strings.TrimSpace(frag.CanonicalPath)]; !ok {
				continue
			}
		}
		scanned++
		itemCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
		ok, relinkErr := repairer.RelinkFragment(itemCtx, strings.TrimSpace(*tenantID), frag, "affected_zone_relink")
		cancel()
		if relinkErr != nil {
			failed++
			log.Printf("relink failed path=%s err=%v", frag.CanonicalPath, relinkErr)
			continue
		}
		if ok {
			relinked++
			log.Printf("relinked path=%s", frag.CanonicalPath)
		} else {
			skipped++
		}
	}

	log.Printf("summary scanned=%d relinked=%d skipped=%d failed=%d", scanned, relinked, skipped, failed)
}

func parseZones(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = struct{}{}
		}
	}
	return out
}
