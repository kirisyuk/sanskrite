package main

import (
	"context"
	"flag"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	projectID  = flag.String("project", "", "GCP project id")
	apply      = flag.Bool("apply", false, "Apply path repairs")
	limit      = flag.Int("limit", 0, "Max matching facilities to process (0 = unlimited)")
	pageSize   = flag.Int("page-size", 200, "Firestore scan page size")
	samples    = flag.Int("samples", 20, "How many sample repairs to print")
	startAfter = flag.String("start-after", "", "Resume scan after this canonical path")
	exactPath  = flag.String("exact-path", "", "Only process this exact canonical path")
)

type counters struct {
	scanned        int
	synthetic      int
	candidates     int
	applied        int
	merged         int
	materialized   int
	lenses         int
	lensArtifacts  int
	frames         int
	agentJobs      int
	blockedGeneric int
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	ctx := context.Background()
	if key := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(client.Client())
	externalCacheRepo := coreRepo.NewExternalDataCacheRepository(client.Client())
	redirectsRepo := hrepo.NewCanonicalPathRedirectsRepository(client.Client())
	materializedRepo := hrepo.NewMaterializedArtifactsRepository(client.Client())
	lensesRepo := hrepo.NewLensesRepository(client.Client())
	watchSubjectsRepo := hrepo.NewWatchSubjectsRepository(client.Client())
	lensArtifactsRepo := hrepo.NewLensArtifactsRepository(client.Client())
	framesRepo := hrepo.NewFramesRepository(client.Client())
	edgesRepo := hrepo.NewDependencyEdgesRepository(client.Client())
	mergeAuditRepo := hrepo.NewFragmentMergeAuditRepository(client.Client())
	ontologyFactsRepo := coreRepo.NewOntologyFactsRepository(client.Client())
	entitiesRepo := coreRepo.NewEntityRepository(client.Client())
	mergeRepo := hrepo.NewFragmentsMergeRepository(fragmentsRepo, edgesRepo, mergeAuditRepo).
		WithRedirectsRepo(redirectsRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithWatchSubjectsRepo(watchSubjectsRepo).
		WithOntologyFactsRepo(ontologyFactsRepo).
		WithEntitiesRepo(entitiesRepo)
	migrator := hrepo.NewFragmentPathMigrator(client.Client(), fragmentsRepo, redirectsRepo).
		WithMergeRepo(mergeRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithLensArtifactsRepo(lensArtifactsRepo).
		WithFramesRepo(framesRepo)

	var geocodingClient coreontology.GeocodingClientInterface
	if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
		geocodingClient = geocoding.NewCachedClientWithService(geocoding.NewAdapter(gc), externalCacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
		log.Printf("google geocoding wired for synthetic facility repair")
	}

	col := client.Client().Collection("horizon_fragments")
	q := col.
		Where("canonicalPath", ">=", "/fac/").
		Where("canonicalPath", "<", "/fac/\ufffd").
		OrderBy("canonicalPath", firestore.Asc)
	if strings.TrimSpace(*exactPath) != "" {
		q = col.Where("canonicalPath", "==", strings.TrimSpace(*exactPath)).OrderBy("canonicalPath", firestore.Asc)
	}
	lastPath := strings.TrimSpace(*startAfter)
	baseQuery := q
	if lastPath != "" {
		q = q.StartAfter(lastPath)
	}
	iter := q.Limit(*pageSize).Documents(ctx)
	defer iter.Stop()

	var st counters
	var sampleLines []string
	for {
		docs, err := iter.GetAll()
		if err != nil {
			log.Fatal(err)
		}
		if len(docs) == 0 {
			break
		}
		for _, doc := range docs {
			frag, err := fragmentsRepo.Get(ctx, doc.Ref.ID)
			if err != nil || frag == nil || strings.TrimSpace(frag.TenantID) != "global" || frag.Type != models.FragmentTypeFacility {
				continue
			}
			lastPath = strings.TrimSpace(frag.CanonicalPath)
			st.scanned++
			if canonical.ClassifyPlaceholderCanonicalPath(lastPath) != canonical.PlaceholderPathFacilitySynthetic {
				continue
			}
			st.synthetic++
			candidate := cloneFragment(frag)
			newPath, _, ok := scout.RepairSyntheticFacilityCanonicalPath(ctx, candidate, geocodingClient)
			if !ok || strings.TrimSpace(newPath) == "" || strings.TrimSpace(newPath) == strings.TrimSpace(frag.CanonicalPath) {
				st.blockedGeneric++
				continue
			}
			st.candidates++
			if len(sampleLines) < *samples {
				sampleLines = append(sampleLines, strings.TrimSpace(frag.CanonicalPath)+" -> "+strings.TrimSpace(newPath))
			}
			if *apply {
				res, err := migrator.MoveCanonicalPath(ctx, frag.ID, strings.TrimSpace(newPath), "synthetic_facility_repair")
				if err != nil {
					log.Printf("WARN move failed fragmentId=%s from=%s to=%s err=%v", frag.ID, frag.CanonicalPath, newPath, err)
					continue
				}
				st.applied++
				if res.MergedIntoExisting {
					st.merged++
				}
				st.materialized += res.MaterializedUpdated
				st.lenses += res.LensesUpdated
				st.lensArtifacts += res.LensArtifactsUpdated
				st.frames += res.FramesUpdated
				st.agentJobs += res.AgentJobsUpdated
			}
			if *limit > 0 && st.candidates >= *limit {
				break
			}
		}
		if *limit > 0 && st.candidates >= *limit {
			break
		}
		log.Printf("progress scanned=%d synthetic=%d candidates=%d lastPath=%s", st.scanned, st.synthetic, st.candidates, lastPath)
		if len(docs) < *pageSize {
			break
		}
		iter.Stop()
		iter = baseQuery.StartAfter(lastPath).Limit(*pageSize).Documents(ctx)
	}

	sort.Strings(sampleLines)
	for _, line := range sampleLines {
		log.Printf("sample repair: %s", line)
	}
	log.Printf("summary scanned=%d synthetic=%d candidates=%d applied=%d merged=%d blocked_generic=%d materialized=%d lenses=%d lens_artifacts=%d frames=%d agent_jobs=%d",
		st.scanned, st.synthetic, st.candidates, st.applied, st.merged, st.blockedGeneric, st.materialized, st.lenses, st.lensArtifacts, st.frames, st.agentJobs)
	if *apply {
		log.Printf("completed at %s", time.Now().UTC().Format(time.RFC3339))
	}
}

func cloneFragment(in *models.HorizonFragment) *models.HorizonFragment {
	if in == nil {
		return nil
	}
	cp := *in
	if in.HomeLocation != nil {
		loc := *in.HomeLocation
		cp.HomeLocation = &loc
	}
	if in.Coordinates != nil {
		coords := *in.Coordinates
		cp.Coordinates = &coords
	}
	if in.Labels != nil {
		cp.Labels = map[string]string{}
		for k, v := range in.Labels {
			cp.Labels[k] = v
		}
	}
	if in.Identifiers != nil {
		cp.Identifiers = map[string]string{}
		for k, v := range in.Identifiers {
			cp.Identifiers[k] = v
		}
	}
	return &cp
}
