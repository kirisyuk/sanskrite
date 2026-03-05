package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type countingGeoNames struct {
	base ontology.GeoNamesClientInterface
	n    atomic.Int64
}

func (c *countingGeoNames) Search(ctx context.Context, query string, maxResults int, opts ontology.GeoNamesSearchOptions) (*ontology.GeoNamesSearchResponse, error) {
	c.n.Add(1)
	return c.base.Search(ctx, query, maxResults, opts)
}
func (c *countingGeoNames) GetPlace(ctx context.Context, geonameID int) (*ontology.GeoNamesPlace, error) {
	c.n.Add(1)
	return c.base.GetPlace(ctx, geonameID)
}
func (c *countingGeoNames) GetHierarchy(ctx context.Context, geonameID int) (*ontology.GeoNamesHierarchyResponse, error) {
	c.n.Add(1)
	return c.base.GetHierarchy(ctx, geonameID)
}
func (c *countingGeoNames) GetCountryCode(ctx context.Context, lat, lon float64) (string, error) {
	c.n.Add(1)
	return c.base.GetCountryCode(ctx, lat, lon)
}

func main() {
	var (
		project = flag.String("project", "", "GCP project id (defaults to env GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT)")
		repeat  = flag.Int("repeat", 2, "How many times to run the full suite (default 2 to validate cache hits)")
	)
	flag.Parse()

	pid := strings.TrimSpace(*project)
	if pid == "" {
		pid = strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	}
	if pid == "" {
		pid = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if pid == "" {
		log.Fatal("project id not provided; pass --project or set GCP_PROJECT_ID/GOOGLE_CLOUD_PROJECT")
	}

	gnUser := strings.TrimSpace(os.Getenv("GEONAMES_USERNAME"))
	if gnUser == "" {
		gnUser = strings.TrimSpace(os.Getenv("HORIZON_GEONAMES_USERNAME"))
	}
	if gnUser == "" {
		log.Fatal("GEONAMES_USERNAME is not set")
	}

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, pid)
	if err != nil {
		log.Fatalf("firestore.NewClient failed: %v", err)
	}
	defer db.Close()

	cacheRepo := repo.NewExternalDataCacheRepository(db)
	httpClient := httphandlers.GetGeoNamesClientFromEnv()
	if httpClient == nil {
		log.Fatal("GeoNames HTTP client not configured (GEONAMES_USERNAME missing?)")
	}

	base := geonames.NewAdapter(httpClient)
	counting := &countingGeoNames{base: base}
	cached := geonames.NewCachedClient(counting, cacheRepo, geonames.CacheOptionsFromEnv())

	// Create resolver without repos (we're testing canonicalization logic + GeoNames verification only).
	resolver := scout.NewCanonicalPathResolver(nil, nil, nil).WithGeoNamesClient(cached)

	tests := []struct {
		in   string
		typ  hmodels.FragmentType
		note string
	}{
		{in: "/loc/uae", typ: hmodels.FragmentTypeGeoRegion, note: "country alias -> country-level canonical"},
		{in: "/loc/abu-dhabi", typ: hmodels.FragmentTypeGeoRegion, note: "loose city -> city-level canonical (if unambiguous)"},
		{in: "/loc/ae/abu-dhabi", typ: hmodels.FragmentTypeGeoRegion, note: "fix city/admin within AE"},
		{in: "/loc/us/springfield", typ: hmodels.FragmentTypeGeoRegion, note: "ambiguous city in-country should stay conservative"},
		{in: "/loc/springfield", typ: hmodels.FragmentTypeGeoRegion, note: "ambiguous city global should stay conservative"},
	}

	start := time.Now()
	for pass := 1; pass <= *repeat; pass++ {
		fmt.Printf("\n=== pass %d/%d ===\n", pass, *repeat)
		for _, tc := range tests {
			r, err := resolver.Resolve(ctx, tc.in, tc.typ)
			if err != nil {
				fmt.Printf("in=%-28s ERR=%v\n", tc.in, err)
				continue
			}
			out := ""
			steps := ""
			conf := 0.0
			if r != nil {
				out = r.CanonicalPath
				conf = r.Confidence
				if len(r.NormalizationSteps) > 0 {
					steps = strings.Join(r.NormalizationSteps, " | ")
				}
			}
			fmt.Printf("in=%-28s out=%-28s conf=%.3f note=%s\n", tc.in, out, conf, tc.note)
			if steps != "" {
				fmt.Printf("  steps: %s\n", steps)
			}
		}
		fmt.Printf("base_api_calls_so_far=%d\n", counting.n.Load())
	}

	fmt.Printf("\nTOTAL: base_api_calls=%d total_dur=%s\n", counting.n.Load(), time.Since(start))
	fmt.Println("Expectation: pass#2 should add ~0 API calls if cache is working.")
}
