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
	apply      = flag.Bool("apply", false, "Apply path migrations")
	mode       = flag.String("mode", "all", "Mode: all|loc|orggov")
	limit      = flag.Int("limit", 0, "Max matching fragments to process (0 = unlimited)")
	pageSize   = flag.Int("page-size", 200, "Firestore scan page size")
	startAfter = flag.String("start-after", "", "Resume scan after this canonical path")
	samples    = flag.Int("samples", 20, "How many sample migrations to print")
	fromPath   = flag.String("from-path", "", "Optional exact canonicalPath to migrate")
	toPath     = flag.String("to-path", "", "Optional exact target canonicalPath for --from-path")
)

type counters struct {
	scanned       int
	candidates    int
	applied       int
	merged        int
	artifacts     int
	lenses        int
	lensArtifacts int
	frames        int
	agentJobs     int
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

	var geoClient coreontology.GeocodingClientInterface
	if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
		geoClient = geocoding.NewCachedClientWithService(geocoding.NewAdapter(gc), externalCacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
		log.Printf("google geocoding wired for placeholder-path backfill")
	}

	log.Printf("placeholder-path backfill: project=%s apply=%v mode=%s pageSize=%d limit=%d", strings.TrimSpace(*projectID), *apply, strings.TrimSpace(*mode), *pageSize, *limit)
	var st counters
	var sampleLines []string
	if strings.TrimSpace(*fromPath) != "" {
		runExplicitMove(ctx, fragmentsRepo, migrator, &st, &sampleLines)
		finish(st, sampleLines)
		return
	}
	switch strings.TrimSpace(*mode) {
	case "all", "loc":
		runPrefix(ctx, client.Client(), "/loc/", fragmentsRepo, migrator, locCandidate, &st, &sampleLines)
	}
	switch strings.TrimSpace(*mode) {
	case "all", "orggov":
		runPrefix(ctx, client.Client(), "/org/", fragmentsRepo, migrator, func(ctx context.Context, frag *models.HorizonFragment) (string, bool) {
			return orgGovCandidate(ctx, frag, geoClient)
		}, &st, &sampleLines)
		runPrefix(ctx, client.Client(), "/gov/", fragmentsRepo, migrator, func(ctx context.Context, frag *models.HorizonFragment) (string, bool) {
			return orgGovCandidate(ctx, frag, geoClient)
		}, &st, &sampleLines)
	}
	finish(st, sampleLines)
}

func runPrefix(
	ctx context.Context,
	db *firestore.Client,
	prefix string,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	migrator *hrepo.FragmentPathMigrator,
	candidate func(context.Context, *models.HorizonFragment) (string, bool),
	st *counters,
	sampleLines *[]string,
) {
	if db == nil || fragmentsRepo == nil || migrator == nil || candidate == nil {
		return
	}
	q := db.Collection("horizon_fragments").
		Where("canonicalPath", ">=", prefix).
		Where("canonicalPath", "<", prefix+"\ufffd").
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
			if err != nil || frag == nil || strings.TrimSpace(frag.TenantID) != "global" {
				continue
			}
			lastPath = strings.TrimSpace(frag.CanonicalPath)
			st.scanned++
			newPath, ok := candidate(ctx, frag)
			if !ok || strings.TrimSpace(newPath) == "" || strings.TrimSpace(newPath) == strings.TrimSpace(frag.CanonicalPath) {
				continue
			}
			st.candidates++
			if len(*sampleLines) < *samples {
				*sampleLines = append(*sampleLines, strings.TrimSpace(frag.CanonicalPath)+" -> "+strings.TrimSpace(newPath))
			}
			if *apply {
				res, err := migrator.MoveCanonicalPath(ctx, frag.ID, newPath, "placeholder_path_backfill")
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
			}
			if *limit > 0 && st.candidates >= *limit {
				break
			}
		}
		if *limit > 0 && st.candidates >= *limit {
			break
		}
		log.Printf("progress prefix=%s scanned=%d candidates=%d lastPath=%s", prefix, st.scanned, st.candidates, lastPath)
		if len(docs) < *pageSize {
			break
		}
		iter.Stop()
		iter = baseQuery.StartAfter(lastPath).Limit(*pageSize).Documents(ctx)
	}
}

func locCandidate(_ context.Context, frag *models.HorizonFragment) (string, bool) {
	if frag == nil || frag.Type != models.FragmentTypeGeoRegion {
		return "", false
	}
	return canonical.NormalizePlaceholderCanonicalPath(strings.TrimSpace(frag.CanonicalPath))
}

func orgGovCandidate(ctx context.Context, frag *models.HorizonFragment, geoClient coreontology.GeocodingClientInterface) (string, bool) {
	if frag == nil || frag.Type != models.FragmentTypeOrganization {
		return "", false
	}
	switch canonical.ClassifyPlaceholderCanonicalPath(strings.TrimSpace(frag.CanonicalPath)) {
	case canonical.PlaceholderPathOrgSynthetic, canonical.PlaceholderPathGovSynthetic:
	default:
		return "", false
	}
	if repairedPath, _, ok := scout.RepairSyntheticOrgCanonicalPath(frag); ok {
		return strings.TrimSpace(repairedPath), true
	}
	if geoClient == nil {
		return "", false
	}
	candidate := cloneFragment(frag)
	res, err := scout.RevalidateOrgGeo(ctx, candidate, geoClient)
	if err != nil || !res.Applied {
		return "", false
	}
	newPath := strings.TrimSpace(candidate.CanonicalPath)
	if newPath == "" || newPath == strings.TrimSpace(frag.CanonicalPath) {
		return "", false
	}
	return newPath, true
}

func runExplicitMove(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	migrator *hrepo.FragmentPathMigrator,
	st *counters,
	sampleLines *[]string,
) {
	if fragmentsRepo == nil || migrator == nil {
		return
	}
	oldPath := strings.TrimSpace(*fromPath)
	newPath := strings.TrimSpace(*toPath)
	if oldPath == "" {
		log.Fatal("--from-path is required for explicit move")
	}
	frag, err := fragmentsRepo.GetByCanonicalPath(ctx, oldPath)
	if err != nil || frag == nil {
		log.Fatalf("fragment not found for --from-path=%s: %v", oldPath, err)
	}
	if newPath == "" {
		if autoPath, _, ok := scout.RepairSyntheticOrgCanonicalPath(frag); ok {
			newPath = strings.TrimSpace(autoPath)
		}
	}
	if newPath == "" {
		log.Fatalf("could not infer target path for %s; pass --to-path explicitly", oldPath)
	}
	st.scanned++
	st.candidates++
	*sampleLines = append(*sampleLines, oldPath+" -> "+newPath)
	if !*apply {
		return
	}
	res, err := migrator.MoveCanonicalPath(ctx, frag.ID, newPath, "placeholder_path_backfill_explicit")
	if err != nil {
		log.Fatalf("move failed fragmentId=%s from=%s to=%s err=%v", frag.ID, oldPath, newPath, err)
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
}

func finish(st counters, sampleLines []string) {
	sort.Strings(sampleLines)
	for _, line := range sampleLines {
		log.Printf("sample migration: %s", line)
	}
	log.Printf("summary scanned=%d candidates=%d applied=%d merged=%d materialized=%d lenses=%d lens_artifacts=%d frames=%d agent_jobs=%d",
		st.scanned, st.candidates, st.applied, st.merged, st.artifacts, st.lenses, st.lensArtifacts, st.frames, st.agentJobs)
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
