package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonsignallanding "github.com/igorkirisyuk/sanskrite/internal/horizon/signallanding"
	horizontelemetry "github.com/igorkirisyuk/sanskrite/internal/horizon/telemetry"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	var (
		projectID = flag.String("project", "nv-horizon", "GCP project id")
		tenantID  = flag.String("tenant", "global", "tenant id")
		limit     = flag.Int("limit", 2000, "max affected zones to scan")
		apply     = flag.Bool("apply", false, "write updates to Firestore")
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

	telemetryRepo := horizonrepo.NewHorizonTelemetryRepository(client.Client())
	externalCacheRepo := corerepo.NewExternalDataCacheRepository(client.Client())
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(client.Client())
	if key := gcp.GetEnvOrSecret(ctx, project, "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && os.Getenv("GOOGLE_MAPS_API_KEY") == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}

	geocoder := buildGeocodingClient(externalCacheRepo, telemetryRepo)
	if geocoder == nil {
		log.Fatal("geocoding client is not configured")
	}

	service := horizonsignallanding.NewService(nil, nil, nil, nil, nil, fragmentsRepo).
		WithGeocodingClient(geocoder)

	items, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), horizonmodels.FragmentTypeAffectedZone, *limit)
	if err != nil {
		log.Fatalf("list affected zones: %v", err)
	}

	var scanned, missing, updated int
	for _, frag := range items {
		if frag == nil || frag.Type != horizonmodels.FragmentTypeAffectedZone {
			continue
		}
		scanned++
		if frag.Coordinates != nil && horizonmodels.IsRealGeoCoordinate(frag.Coordinates.Lat, frag.Coordinates.Lon) {
			continue
		}
		missing++
		itemCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		changed, err := service.BackfillAffectedZoneCoordinates(itemCtx, frag)
		cancel()
		if err != nil {
			log.Printf("backfill failed path=%s err=%v", frag.CanonicalPath, err)
			continue
		}
		if !changed {
			continue
		}
		if *apply {
			if err := fragmentsRepo.Update(ctx, frag); err != nil {
				log.Printf("update failed path=%s err=%v", frag.CanonicalPath, err)
				continue
			}
		}
		updated++
		log.Printf("updated path=%s coords=%.6f,%.6f apply=%v", frag.CanonicalPath, frag.Coordinates.Lat, frag.Coordinates.Lon, *apply)
	}

	log.Printf("summary scanned=%d missing=%d updated=%d apply=%v", scanned, missing, updated, *apply)
}

func buildGeocodingClient(
	externalCacheRepo *corerepo.ExternalDataCacheRepository,
	telemetryRepo *horizonrepo.HorizonTelemetryRepository,
) ontology.GeocodingClientInterface {
	var googleClient ontology.GeocodingClientInterface
	if gcRaw := httphandlers.GetGoogleGeocodingClientFromEnv(); gcRaw != nil {
		gcAdapter := geocoding.NewAdapter(gcRaw)
		var gcBase ontology.GeocodingClientInterface = gcAdapter
		if telemetryRepo != nil {
			gcBase = horizontelemetry.NewGeocodingTelemetryWrapper(gcAdapter, telemetryRepo)
		}
		googleClient = geocoding.NewCachedClientWithService(gcBase, externalCacheRepo, geocoding.CacheOptionsFromEnv(), ontology.GeocodingProviderGoogle)
	}

	nomRaw := geocoding.NominatimOptionsFromEnv()
	var nomBase ontology.GeocodingClientInterface = nomRaw
	if telemetryRepo != nil {
		nomBase = horizontelemetry.NewNominatimTelemetryWrapper(nomRaw, telemetryRepo)
	}
	nomCached := geocoding.NewCachedClientWithService(nomBase, externalCacheRepo, geocoding.CacheOptionsFromEnv(), ontology.GeocodingProviderNominatim)
	return geocoding.NewFallbackClient(googleClient, nomCached, log.Default())
}
