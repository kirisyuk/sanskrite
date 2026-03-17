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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	projectID  = flag.String("project", "", "GCP project id")
	apply      = flag.Bool("apply", false, "Apply path migrations")
	limit      = flag.Int("limit", 0, "Max matching facilities to process (0 = unlimited)")
	pageSize   = flag.Int("page-size", 200, "Firestore scan page size")
	samples    = flag.Int("samples", 20, "How many sample migrations to print")
	prefix     = flag.String("prefix", "/fac/na/", "Canonical path prefix to scan")
	startAfter = flag.String("start-after", "", "Resume scan after this canonical path")
)

type counters struct {
	scanned         int
	candidates      int
	revalidated     int
	applied         int
	duplicateMerged int
	merged          int
	artifacts       int
	lenses          int
	lensArtifacts   int
	frames          int
	agentJobs       int
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
		log.Printf("google geocoding wired for facility geo-path backfill")
	}

	log.Printf("facility geo-path backfill: project=%s apply=%v pageSize=%d limit=%d", strings.TrimSpace(*projectID), *apply, *pageSize, *limit)

	var st counters
	var sampleLines []string
	scanPrefix := strings.TrimSpace(*prefix)
	if scanPrefix == "" {
		scanPrefix = "/fac/na/"
	}
	q := client.Client().Collection("horizon_fragments").
		Where("canonicalPath", ">=", scanPrefix).
		Where("canonicalPath", "<", scanPrefix+"\ufffd").
		OrderBy("canonicalPath", firestore.Asc)

	lastPath := strings.TrimSpace(*startAfter)
	baseQuery := q
	if lastPath != "" {
		q = q.StartAfter(lastPath)
	}
	iter := q.Limit(*pageSize).Documents(ctx)
	defer iter.Stop()

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
			if err != nil || frag == nil {
				continue
			}
			if strings.TrimSpace(frag.TenantID) != "global" || frag.Type != "FACILITY" {
				continue
			}
			lastPath = strings.TrimSpace(frag.CanonicalPath)
			st.scanned++
			originalPath := strings.TrimSpace(frag.CanonicalPath)
			candidate := cloneFragment(frag)
			if geocodingClient != nil {
				if revalidated, rerr := scout.RevalidateFacilityGeo(ctx, candidate, geocodingClient); rerr == nil && revalidated.Applied {
					st.revalidated++
				}
			}
			newPath := strings.TrimSpace(candidate.CanonicalPath)
			ok := newPath != "" && newPath != originalPath
			if !ok {
				continue
			}
			st.candidates++
			if len(sampleLines) < *samples {
				sampleLines = append(sampleLines, originalPath+" -> "+newPath)
			}
			if *apply {
				res, err := migrator.MoveCanonicalPath(ctx, frag.ID, newPath, "facility_geo_backfill")
				if err != nil {
					log.Printf("WARN move failed fragmentId=%s from=%s to=%s err=%v", frag.ID, frag.CanonicalPath, newPath, err)
					continue
				}
				st.applied++
				if res.MergedIntoExisting {
					st.merged++
				}
				st.artifacts += res.MaterializedUpdated
				st.lenses += res.LensesUpdated
				st.lensArtifacts += res.LensArtifactsUpdated
				st.frames += res.FramesUpdated
				st.agentJobs += res.AgentJobsUpdated
				st.duplicateMerged += mergeFacilityPathTwins(ctx, fragmentsRepo, mergeRepo, geocodingClient, originalPath, newPath, frag.ID)
			}
			if *limit > 0 && st.candidates >= *limit {
				break
			}
		}
		if *limit > 0 && st.candidates >= *limit {
			break
		}
		log.Printf("progress scanned=%d candidates=%d lastPath=%s", st.scanned, st.candidates, lastPath)
		if len(docs) < *pageSize {
			break
		}
		iter.Stop()
		iter = baseQuery.StartAfter(lastPath).Limit(*pageSize).Documents(ctx)
	}

	sort.Strings(sampleLines)
	for _, line := range sampleLines {
		log.Printf("sample migration: %s", line)
	}
	log.Printf("summary scanned=%d candidates=%d revalidated=%d applied=%d merged=%d duplicate_merged=%d materialized=%d lenses=%d lens_artifacts=%d frames=%d agent_jobs=%d",
		st.scanned, st.candidates, st.revalidated, st.applied, st.merged, st.duplicateMerged, st.artifacts, st.lenses, st.lensArtifacts, st.frames, st.agentJobs)
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

func mergeFacilityPathTwins(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	mergeRepo *hrepo.FragmentsMergeRepository,
	geocodingClient coreontology.GeocodingClientInterface,
	oldPath string,
	targetPath string,
	primaryID string,
) int {
	if fragmentsRepo == nil || mergeRepo == nil || geocodingClient == nil {
		return 0
	}
	oldPath = strings.TrimSpace(oldPath)
	targetPath = strings.TrimSpace(targetPath)
	primaryID = strings.TrimSpace(primaryID)
	if oldPath == "" || targetPath == "" || primaryID == "" {
		return 0
	}
	dups, err := fragmentsRepo.ListByCanonicalPath(ctx, oldPath)
	if err != nil || len(dups) == 0 {
		return 0
	}
	merged := 0
	for _, dup := range dups {
		if dup == nil || strings.TrimSpace(dup.ID) == "" || strings.TrimSpace(dup.ID) == primaryID {
			continue
		}
		if strings.TrimSpace(dup.TenantID) != "global" || dup.Type != "FACILITY" {
			continue
		}
		candidate := cloneFragment(dup)
		revalidated, rerr := scout.RevalidateFacilityGeo(ctx, candidate, geocodingClient)
		if rerr != nil || !revalidated.Applied {
			continue
		}
		if strings.TrimSpace(candidate.CanonicalPath) != targetPath {
			continue
		}
		if _, err := mergeRepo.MergeFragment(ctx, primaryID, strings.TrimSpace(dup.ID), "global", "tool_dedup", "facility_geo_backfill", "", ""); err != nil {
			log.Printf("WARN duplicate merge failed primary=%s duplicate=%s from=%s to=%s err=%v", primaryID, dup.ID, oldPath, targetPath, err)
			continue
		}
		merged++
	}
	return merged
}
