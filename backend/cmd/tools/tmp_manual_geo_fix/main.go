package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/cmd/tools/internal/artifactrewrite"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "nv-horizon-test", "GCP project ID")
	path := flag.String("path", "", "Canonical path")
	lat := flag.Float64("lat", 0, "Latitude")
	lon := flag.Float64("lon", 0, "Longitude")
	country := flag.String("country", "", "ISO2 country code")
	name := flag.String("name", "", "Home location name")
	query := flag.String("query", "", "Geo query")
	formatted := flag.String("formatted", "", "Formatted address")
	provider := flag.String("provider", "manual_override", "Geo provider label")
	placeID := flag.String("place-id", "", "Place ID")
	clearCoords := flag.Bool("clear-coords", false, "Clear coordinates and geo resolution")
	artifactLimit := flag.Int("artifact-limit", 500, "Max materialized supply-chain artifacts to rewrite for the updated node")
	flag.Parse()

	if strings.TrimSpace(*path) == "" {
		log.Fatal("--path is required")
	}
	if !*clearCoords && (!hmodels.IsRealGeoCoordinate(*lat, *lon) || strings.TrimSpace(*country) == "") {
		log.Fatal("--lat/--lon and --country are required unless --clear-coords is set")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fsClient.Close()

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(fsClient.Client())
	fragment, err := fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(*path))
	if err != nil {
		log.Fatalf("load fragment: %v", err)
	}
	if fragment == nil || strings.TrimSpace(fragment.ID) == "" {
		log.Fatalf("fragment not found: %s", strings.TrimSpace(*path))
	}

	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
	}
	if fragment.Identifiers == nil {
		fragment.Identifiers = map[string]string{}
	}
	if fragment.Qualifiers == nil {
		fragment.Qualifiers = map[string]interface{}{}
	}

	cc := strings.ToUpper(strings.TrimSpace(*country))
	if *clearCoords {
		fragment.Coordinates = nil
		fragment.HasCoordinates = false
		if fragment.HomeLocation != nil {
			fragment.HomeLocation.ISO6709 = nil
			if cc != "" {
				fragment.HomeLocation.ISO3166_1 = &cc
			}
			if strings.TrimSpace(*name) != "" {
				fragment.HomeLocation.Name = strings.TrimSpace(*name)
			}
		}
		for _, key := range []string{
			"geoCoordinatesMethod",
			"geoCoordinatesProvider",
			"geoCoordinatesQuery",
			"geoCoordinatesFormattedAddress",
			"geoCoordinatesPlaceId",
			"geoCoordinatesCountryAlpha2",
			"geoVerifiedCountryAlpha2",
			"geoVerifiedPlaceId",
			"geoVerifiedMethod",
			"geoVerifiedProvider",
			"geoVerifiedFormattedAddress",
			"geoVerifiedQuery",
		} {
			delete(fragment.Identifiers, key)
		}
		fragment.Qualifiers["geoResolveTrace"] = map[string]interface{}{
			"phase":  "manual_geo_fix",
			"at":     time.Now().UTC().Format(time.RFC3339),
			"action": "cleared_coords",
			"reason": "generic_or_country_centroid_result",
		}
	} else {
		fragment.Coordinates = &hmodels.Coordinates{Lat: *lat, Lon: *lon}
		fragment.HasCoordinates = true
		if fragment.HomeLocation != nil {
			fragment.HomeLocation.ISO3166_1 = &cc
			iso6709 := fmt.Sprintf("%+09.6f%+010.6f/", *lat, *lon)
			fragment.HomeLocation.ISO6709 = &iso6709
			if strings.TrimSpace(*name) != "" {
				fragment.HomeLocation.Name = strings.TrimSpace(*name)
			}
		}
		fragment.Identifiers["geoCoordinatesMethod"] = "manual_override"
		fragment.Identifiers["geoCoordinatesProvider"] = strings.TrimSpace(*provider)
		fragment.Identifiers["geoCoordinatesQuery"] = strings.TrimSpace(*query)
		fragment.Identifiers["geoCoordinatesFormattedAddress"] = strings.TrimSpace(*formatted)
		fragment.Identifiers["geoCoordinatesCountryAlpha2"] = cc
		fragment.Identifiers["geoVerifiedMethod"] = "manual_override"
		fragment.Identifiers["geoVerifiedProvider"] = strings.TrimSpace(*provider)
		fragment.Identifiers["geoVerifiedQuery"] = strings.TrimSpace(*query)
		fragment.Identifiers["geoVerifiedFormattedAddress"] = strings.TrimSpace(*formatted)
		fragment.Identifiers["geoVerifiedCountryAlpha2"] = cc
		if strings.TrimSpace(*placeID) != "" {
			fragment.Identifiers["geoCoordinatesPlaceId"] = strings.TrimSpace(*placeID)
			fragment.Identifiers["geoVerifiedPlaceId"] = strings.TrimSpace(*placeID)
		} else {
			delete(fragment.Identifiers, "geoCoordinatesPlaceId")
			delete(fragment.Identifiers, "geoVerifiedPlaceId")
		}
		fragment.Qualifiers["geoResolveTrace"] = map[string]interface{}{
			"phase":  "manual_geo_fix",
			"at":     time.Now().UTC().Format(time.RFC3339),
			"action": "set_coords",
			"accepted": map[string]interface{}{
				"country":          cc,
				"provider":         strings.TrimSpace(*provider),
				"query":            strings.TrimSpace(*query),
				"formattedAddress": strings.TrimSpace(*formatted),
			},
		}
	}

	fragment.IndexPaths = hmodels.BuildIndexPaths(fragment, nil)
	fragment.UpdatedBy = "codex_manual_geo_fix"
	if err := fragmentsRepo.Update(ctx, fragment); err != nil {
		log.Fatalf("update fragment: %v", err)
	}
	log.Printf("updated %s", strings.TrimSpace(fragment.CanonicalPath))
	result, err := artifactrewrite.RewriteNodeArtifacts(
		ctx,
		fsClient.Client(),
		strings.TrimSpace(fragment.CanonicalPath),
		"",
		*artifactLimit,
		true,
		os.Stdout,
	)
	if err != nil {
		log.Fatalf("rewrite node artifacts: %v", err)
	}
	log.Printf("rewrote artifacts for %s inspected=%d changed=%d applied=%d",
		strings.TrimSpace(fragment.CanonicalPath),
		result.Inspected,
		result.Changed,
		result.Applied,
	)
}
