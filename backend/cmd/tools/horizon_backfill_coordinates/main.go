package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	projectID  = flag.String("project", "", "GCP project ID (required)")
	tenantID   = flag.String("tenant", "global", "Tenant ID (default: global)")
	limit      = flag.Int("limit", 0, "Max fragments to process (0 = all)")
	maxScan    = flag.Int("max-scan", 100000, "Max fragments to scan (safety cap)")
	pageSize   = flag.Int("page-size", 200, "Firestore page size")
	startAfter = flag.String("start-after", "", "Resume cursor: start after this document ID")

	apply  = flag.Bool("apply", false, "Apply changes (update fragments). Default: dry-run.")
	dryRun = flag.Bool("dry-run", false, "Force dry-run even if --apply")

	// Source selection
	skipGeonames   = flag.Bool("skip-geonames", false, "Skip GeoNames (for /loc/*)")
	skipGeocoding  = flag.Bool("skip-geocoding", false, "Skip Google Geocoding (for org/facility)")
	typesFilter    = flag.String("types", "", "Comma-separated fragment types to process (empty = all geo-relevant)")
	pathPrefix     = flag.String("path-prefix", "", "Only process canonicalPath with this prefix (e.g. /org/)")

	progressEvery = flag.Int("progress-every", 50, "Log progress every N processed (0=disable)")
)

type countingGeocoding struct {
	base coreontology.GeocodingClientInterface
	n    atomic.Int64
}

func (c *countingGeocoding) Geocode(ctx context.Context, address string) (*coreontology.GeocodingResult, error) {
	c.n.Add(1)
	return c.base.Geocode(ctx, address)
}

type countingGeoNames struct {
	base coreontology.GeoNamesClientInterface
	n    atomic.Int64
}

func (c *countingGeoNames) Search(ctx context.Context, query string, maxResults int, opts coreontology.GeoNamesSearchOptions) (*coreontology.GeoNamesSearchResponse, error) {
	c.n.Add(1)
	return c.base.Search(ctx, query, maxResults, opts)
}
func (c *countingGeoNames) GetPlace(ctx context.Context, geonameID int) (*coreontology.GeoNamesPlace, error) {
	c.n.Add(1)
	return c.base.GetPlace(ctx, geonameID)
}
func (c *countingGeoNames) GetHierarchy(ctx context.Context, geonameID int) (*coreontology.GeoNamesHierarchyResponse, error) {
	c.n.Add(1)
	return c.base.GetHierarchy(ctx, geonameID)
}

type stats struct {
	Scanned         int64
	Processed       int64
	FromGeonames    int64
	FromGeocoding   int64
	SkippedHas      int64
	SkippedNoAddr   int64
	Errors          int64
	Applied         int64
	GeocodingCalls  int64 // total Geocode() invocations (with address)
	GeocodingAPI    int64 // actual API calls (cache misses)
	GeonamesCalls   int64
	GeonamesAPI     int64
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *tenantID != "" && *tenantID != "global" {
		*tenantID = "global"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("Received interrupt, stopping...")
		cancel()
	}()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	externalCacheRepo := coreRepo.NewExternalDataCacheRepository(db)

	// GeoNames client (for /loc/*) with API call counter for cache hit rate
	var geonamesClient coreontology.GeoNamesClientInterface
	var geonamesCounter *countingGeoNames
	if !*skipGeonames {
		gnBase := httphandlers.GetGeoNamesClientFromEnv()
		if gnBase != nil {
			gnAdapter := geonames.NewAdapter(gnBase)
			geonamesCounter = &countingGeoNames{base: gnAdapter}
			geonamesClient = geonames.NewCachedClient(geonamesCounter, externalCacheRepo, geonames.CacheOptionsFromEnv())
			log.Printf("GeoNames client enabled (for /loc/*)")
		} else {
			log.Printf("GeoNames disabled: GEONAMES_USERNAME not set")
		}
	}

	// Google Geocoding client (for org/facility/community/program) with API call counter for cache hit rate
	var geocodingClient coreontology.GeocodingClientInterface
	var geocodingCounter *countingGeocoding
	if !*skipGeocoding {
		gcBase := httphandlers.GetGoogleGeocodingClientFromEnv()
		if gcBase != nil {
			gcAdapter := geocoding.NewAdapter(gcBase)
			geocodingCounter = &countingGeocoding{base: gcAdapter}
			geocodingClient = geocoding.NewCachedClient(geocodingCounter, externalCacheRepo, geocoding.CacheOptionsFromEnv())
			log.Printf("Google Geocoding client enabled (for org/facility/community/program)")
		} else {
			log.Printf("Google Geocoding disabled: GOOGLE_MAPS_API_KEY not set")
		}
	}

	if geonamesClient == nil && geocodingClient == nil {
		log.Fatal("At least one of GeoNames or Google Geocoding must be enabled")
	}

	typeFilter := parseTypesFilter(*typesFilter)
	prefix := strings.TrimSpace(*pathPrefix)

	log.Printf("Starting coordinates backfill (project=%s apply=%v limit=%d maxScan=%d prefix=%q types=%v)",
		*projectID, *apply && !*dryRun, *limit, *maxScan, prefix, typeFilter)

	var st stats
	lastSeen := strings.TrimSpace(*startAfter)
	col := db.Collection("horizon_fragments")

	for {
		if *limit > 0 && atomic.LoadInt64(&st.Processed) >= int64(*limit) {
			break
		}
		if atomic.LoadInt64(&st.Scanned) >= int64(*maxScan) {
			log.Printf("Reached maxScan=%d, stopping", *maxScan)
			break
		}

		q := col.Where("tenantId", "==", *tenantID).
			Where("governance.status", "==", string(hmodels.FragmentStatusActive)).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastSeen != "" {
			q = q.StartAfter(lastSeen)
		}

		iter := q.Documents(ctx)
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
					log.Printf("Transient error, retrying in 5s: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				log.Fatalf("Iterate: %v", err)
			}
			pageDocs = append(pageDocs, doc)
		}
		iter.Stop()

		if len(pageDocs) == 0 {
			break
		}

		for _, doc := range pageDocs {
			atomic.AddInt64(&st.Scanned, 1)
			lastSeen = doc.Ref.ID

			if *limit > 0 && atomic.LoadInt64(&st.Processed) >= int64(*limit) {
				break
			}

			var frag hmodels.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				atomic.AddInt64(&st.Errors, 1)
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}

			// Filter by path prefix
			cp := strings.TrimSpace(frag.CanonicalPath)
			if prefix != "" && !strings.HasPrefix(cp, prefix) {
				continue
			}

			// Filter by type
			if len(typeFilter) > 0 {
				if _, ok := typeFilter[frag.Type]; !ok {
					continue
				}
			} else {
				// Default: geo-relevant types only
				if _, ok := henrich.GeoRelevantTypes[frag.Type]; !ok {
					if frag.Type != hmodels.FragmentTypeGeoRegion {
						continue
					}
				}
			}

			// Skip if already has valid coordinates
			if frag.Coordinates != nil && hmodels.IsValidCoordinates(frag.Coordinates.Lat, frag.Coordinates.Lon) {
				atomic.AddInt64(&st.SkippedHas, 1)
				continue
			}
			if hmodels.HasRealGeoCoordinates(frag.Coordinates) {
				atomic.AddInt64(&st.SkippedHas, 1)
				continue
			}

			atomic.AddInt64(&st.Processed, 1)

			changed := false

			// /loc/*: GeoNames
			if geonamesClient != nil && strings.HasPrefix(cp, "/loc/") {
				atomic.AddInt64(&st.GeonamesCalls, 1)
				attached, gid := maybeAttachGeoNamesIDAndCoords(ctx, geonamesClient, &frag)
				if attached && gid != "" {
					changed = true
					atomic.AddInt64(&st.FromGeonames, 1)
				}
			}

			// org/facility/community/program: Google Geocoding
			if !changed && geocodingClient != nil {
				addr := henrich.BuildAddressFromFragment(&frag)
				if addr != "" {
					atomic.AddInt64(&st.GeocodingCalls, 1)
				}
				enriched, err := henrich.EnrichCoordinatesFromGeocoding(ctx, geocodingClient, &frag)
				if err != nil {
					atomic.AddInt64(&st.Errors, 1)
					log.Printf("Geocoding %s: %v", frag.ID, err)
					continue
				}
				if enriched {
					changed = true
					atomic.AddInt64(&st.FromGeocoding, 1)
				} else if addr != "" {
					atomic.AddInt64(&st.SkippedNoAddr, 1) // had address but geocoding returned nothing
				}
			}

			if changed && (*apply && !*dryRun) {
				frag.IndexPaths = hmodels.BuildIndexPaths(&frag, nil)
				if err := fragmentsRepo.Update(ctx, &frag); err != nil {
					atomic.AddInt64(&st.Errors, 1)
					log.Printf("Update %s: %v", frag.ID, err)
					continue
				}
				atomic.AddInt64(&st.Applied, 1)
			}

			if *progressEvery > 0 && atomic.LoadInt64(&st.Processed)%int64(*progressEvery) == 0 {
				gcAPI := int64(0)
				gnAPI := int64(0)
				if geocodingCounter != nil {
					gcAPI = geocodingCounter.n.Load()
				}
				if geonamesCounter != nil {
					gnAPI = geonamesCounter.n.Load()
				}
				gcCalls := atomic.LoadInt64(&st.GeocodingCalls)
				gcHitRate := ""
				if gcCalls > 0 {
					hits := gcCalls - gcAPI
					pct := 100.0 * float64(hits) / float64(gcCalls)
					gcHitRate = fmt.Sprintf(" geoCacheHit=%.1f%%", pct)
				}
				log.Printf("Progress: scanned=%d processed=%d fromGeonames=%d fromGeocoding=%d skippedHas=%d skippedNoAddr=%d applied=%d errors=%d geoAPI=%d gnAPI=%d%s last=%s",
					atomic.LoadInt64(&st.Scanned), atomic.LoadInt64(&st.Processed),
					atomic.LoadInt64(&st.FromGeonames), atomic.LoadInt64(&st.FromGeocoding),
					atomic.LoadInt64(&st.SkippedHas), atomic.LoadInt64(&st.SkippedNoAddr),
					atomic.LoadInt64(&st.Applied), atomic.LoadInt64(&st.Errors), gcAPI, gnAPI, gcHitRate, lastSeen)
			}
		}

		if len(pageDocs) < *pageSize {
			break
		}
	}

	// Final stats with cache hit rate
	gcAPI := int64(0)
	gnAPI := int64(0)
	if geocodingCounter != nil {
		gcAPI = geocodingCounter.n.Load()
	}
	if geonamesCounter != nil {
		gnAPI = geonamesCounter.n.Load()
	}
	gcCalls := atomic.LoadInt64(&st.GeocodingCalls)
	gcHitRate := "N/A"
	if gcCalls > 0 {
		hits := gcCalls - gcAPI
		pct := 100.0 * float64(hits) / float64(gcCalls)
		gcHitRate = fmt.Sprintf("%.1f%%", pct)
	}
	gnCalls := atomic.LoadInt64(&st.GeonamesCalls)
	gnHitRate := "N/A"
	if gnCalls > 0 {
		hits := gnCalls - gnAPI
		pct := 100.0 * float64(hits) / float64(gnCalls)
		gnHitRate = fmt.Sprintf("%.1f%%", pct)
	}

	log.Printf("Done. scanned=%d processed=%d fromGeonames=%d fromGeocoding=%d skippedHas=%d skippedNoAddr=%d applied=%d errors=%d",
		atomic.LoadInt64(&st.Scanned), atomic.LoadInt64(&st.Processed),
		atomic.LoadInt64(&st.FromGeonames), atomic.LoadInt64(&st.FromGeocoding),
		atomic.LoadInt64(&st.SkippedHas), atomic.LoadInt64(&st.SkippedNoAddr),
		atomic.LoadInt64(&st.Applied), atomic.LoadInt64(&st.Errors))
	log.Printf("Cache: geocoding calls=%d API=%d hitRate=%s | geonames calls=%d API=%d hitRate=%s",
		gcCalls, gcAPI, gcHitRate, gnCalls, gnAPI, gnHitRate)

	if *apply && !*dryRun && atomic.LoadInt64(&st.Applied) > 0 {
		log.Printf("Applied %d coordinate updates. Consider running horizon_backfill_index_paths if indexPaths need refresh.", atomic.LoadInt64(&st.Applied))
	}
}

func parseTypesFilter(s string) map[hmodels.FragmentType]struct{} {
	out := map[hmodels.FragmentType]struct{}{}
	for _, t := range strings.Split(s, ",") {
		t = strings.TrimSpace(strings.ToUpper(t))
		if t == "" {
			continue
		}
		out[hmodels.FragmentType(t)] = struct{}{}
	}
	return out
}

// maybeAttachGeoNamesIDAndCoords attaches GeoNames ID and coordinates for /loc/{cc}/{adm1}/{city}.
// Returns (attached, geonameId).
func maybeAttachGeoNamesIDAndCoords(ctx context.Context, gn coreontology.GeoNamesClientInterface, f *hmodels.HorizonFragment) (bool, string) {
	if gn == nil || f == nil {
		return false, ""
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	if !strings.HasPrefix(cp, "/loc/") {
		return false, ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 4 || parts[0] != "loc" {
		return false, ""
	}
	cc := strings.ToUpper(strings.TrimSpace(parts[1]))
	adm := strings.TrimSpace(parts[2])
	city := strings.TrimSpace(parts[3])
	if len(cc) != 2 || strings.EqualFold(city, "na") || city == "" {
		return false, ""
	}

	for _, r := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(r.KB), "GeoNames") && strings.TrimSpace(r.ExternalID) != "" {
			// Already has ID - try GetPlace for coordinates if missing
			gid, _ := parseInt(strings.TrimSpace(r.ExternalID))
			if gid > 0 && (f.Coordinates == nil || !hmodels.IsValidCoordinates(f.Coordinates.Lat, f.Coordinates.Lon)) {
				place, err := gn.GetPlace(ctx, gid)
				if err == nil && place != nil && place.Lat != "" && place.Lng != "" {
					attachGeoCoordsFromPlace(f, place)
					return true, r.ExternalID
				}
			}
			return false, r.ExternalID
		}
	}

	cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(city)))
	if cityName == "" {
		return false, ""
	}

	opts := coreontology.GeoNamesSearchOptions{Country: cc, FeatureClass: "P", NameEquals: true}
	if len(adm) == 2 && adm[0] >= 'A' && adm[0] <= 'Z' {
		opts.AdminCode1 = strings.ToUpper(adm)
	}

	resp, err := gn.Search(ctx, cityName, 10, opts)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return false, ""
	}

	var results []coreontology.GeoNamesPlace
	for _, r := range resp.Results {
		if strings.EqualFold(strings.TrimSpace(r.CountryCode), cc) {
			results = append(results, r)
		}
	}
	if len(results) == 0 {
		return false, ""
	}

	best := results[0]
	if len(results) > 1 {
		for _, r := range results {
			if r.Population > best.Population {
				best = r
			}
		}
	}

	if best.GeoNameID <= 0 {
		return false, ""
	}

	gidStr := fmt.Sprintf("%d", best.GeoNameID)
	attachGeoCoordsFromPlace(f, &best)

	if f.Identifiers == nil {
		f.Identifiers = map[string]string{}
	}
	f.Identifiers["geonamesId"] = gidStr

	hasKB := false
	for i := range f.KBRefs {
		if strings.EqualFold(f.KBRefs[i].KB, "GeoNames") {
			f.KBRefs[i].ExternalID = gidStr
			hasKB = true
			break
		}
	}
	if !hasKB {
		f.KBRefs = append(f.KBRefs, hmodels.KBReference{KB: "GeoNames", ExternalID: gidStr, Confidence: 1.0})
	}

	return true, gidStr
}

func attachGeoCoordsFromPlace(f *hmodels.HorizonFragment, p *coreontology.GeoNamesPlace) {
	if f == nil || p == nil {
		return
	}
	lat, _ := parseFloat(p.Lat)
	lon, _ := parseFloat(p.Lng)
	if !hmodels.IsValidCoordinates(lat, lon) {
		return
	}
	f.Coordinates = &hmodels.Coordinates{Lat: lat, Lon: lon}
	if f.HomeLocation == nil {
		f.HomeLocation = &hmodels.Location{}
	}
	iso := fmt.Sprintf("%+.6f%+.6f/", lat, lon)
	f.HomeLocation.ISO6709 = &iso
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}
