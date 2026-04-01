package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	queriesRaw := flag.String("queries", "", "semicolon-separated geocoding queries")
	flag.Parse()

	if key := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}

	queries := compact(strings.Split(strings.TrimSpace(*queriesRaw), ";"))
	if len(queries) == 0 {
		panic("--queries is required")
	}

	var providers []namedClient
	if nom := geocoding.NominatimOptionsFromEnv(); nom != nil {
		providers = append(providers, namedClient{name: coreontology.GeocodingProviderNominatim, client: nom})
	}
	if google := httphandlers.GetGoogleGeocodingClientFromEnv(); google != nil {
		providers = append(providers, namedClient{name: coreontology.GeocodingProviderGoogle, client: geocoding.NewAdapter(google)})
	}
	if len(providers) == 0 {
		panic("no geocoding providers configured")
	}

	for _, query := range queries {
		fmt.Printf("query=%q\n", query)
		for _, provider := range providers {
			result, err := provider.client.Geocode(ctx, query)
			switch {
			case err != nil:
				fmt.Printf("  provider=%s error=%q\n", provider.name, err)
			case result == nil:
				fmt.Printf("  provider=%s result=nil\n", provider.name)
			default:
				fmt.Printf("  provider=%s lat=%.6f lon=%.6f placeId=%q formatted=%q\n",
					firstNonEmpty(result.Provider, provider.name), result.Lat, result.Lon, result.PlaceID, result.FormattedAddress)
			}
		}
		fmt.Println()
	}
}

type namedClient struct {
	name   string
	client coreontology.GeocodingClientInterface
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}
