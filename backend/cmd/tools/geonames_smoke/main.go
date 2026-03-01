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
		query   = flag.String("query", "Abu Dhabi", "City query to resolve")
		country = flag.String("country", "", "Optional ISO3166-1 alpha2 country filter (e.g., AE)")
		admin1  = flag.String("admin1", "", "Optional ADM1 code filter (e.g., AZ)")
		repeat  = flag.Int("repeat", 2, "How many times to call Search (default 2 to validate cache hit)")
	)
	flag.Parse()

	gnUser := strings.TrimSpace(os.Getenv("GEONAMES_USERNAME"))
	if gnUser == "" {
		gnUser = strings.TrimSpace(os.Getenv("HORIZON_GEONAMES_USERNAME"))
	}
	if gnUser == "" {
		log.Fatal("GEONAMES_USERNAME is not set")
	}

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

	opts := ontology.GeoNamesSearchOptions{
		Country:      strings.ToUpper(strings.TrimSpace(*country)),
		AdminCode1:   strings.ToUpper(strings.TrimSpace(*admin1)),
		FeatureClass: "P",
		NameEquals:   true,
	}

	start := time.Now()
	for i := 1; i <= *repeat; i++ {
		iterStart := time.Now()
		resp, err := cached.Search(ctx, strings.TrimSpace(*query), 10, opts)
		dur := time.Since(iterStart)
		if err != nil {
			log.Fatalf("Search iteration %d failed: %v", i, err)
		}
		n := 0
		if resp != nil {
			n = len(resp.Results)
		}
		top := ""
		if resp != nil && len(resp.Results) > 0 {
			r := resp.Results[0]
			top = fmt.Sprintf("top=%s cc=%s adm1=%s pop=%d geonameId=%d", r.Name, r.CountryCode, r.AdminCode1, r.Population, r.GeoNameID)
		}
		fmt.Printf("iter=%d dur=%s results=%d %s\n", i, dur, n, top)
	}

	fmt.Printf("base_api_calls=%d total_dur=%s\n", counting.n.Load(), time.Since(start))
	fmt.Println("If cache is working, repeat=2 should produce base_api_calls close to 1 (first call only).")
}
