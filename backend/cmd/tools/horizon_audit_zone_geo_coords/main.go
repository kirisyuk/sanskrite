package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var (
	projectID = flag.String("project", "etigram", "GCP project ID")
	limit     = flag.Int("limit", 0, "Max zones to audit (0 = all)")
	apply     = flag.Bool("apply", false, "Apply coordinate fixes to Firestore")
	dryRun    = flag.Bool("dry-run", true, "Dry-run only (default: true)")
	outPath   = flag.String("out", "", "Output JSONL file")
	verbose   = flag.Bool("verbose", false, "Log geocoding attempts")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	doApply := *apply && !*dryRun
	if doApply {
		log.Printf("APPLY mode: will update coordinates in Firestore")
	} else {
		log.Printf("DRY-RUN mode: no changes will be written")
	}

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore: %v", err)
	}
	defer fs.Close()

	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(fs)
	externalCache := coreRepo.NewExternalDataCacheRepository(fs)

	// GeoNames for reverse geocoding (country from lat/lon) and re-geocoding
	gnBase := httphandlers.GetGeoNamesClientFromEnv()
	if gnBase == nil {
		log.Fatal("GeoNames client not configured (GEONAMES_USERNAME)")
	}
	gnAdapter := geonames.NewAdapter(gnBase)
	gnCached := geonames.NewCachedClient(gnAdapter, externalCache, geonames.CacheOptionsFromEnv())

	// Geocoding: Nominatim (primary) + Google (fallback). Load Google key from env or Secret Manager.
	proj := strings.TrimSpace(*projectID)
	if key := gcp.GetEnvOrSecret(ctx, proj, "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && os.Getenv("GOOGLE_MAPS_API_KEY") == "" {
		os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}
	var geocodingClient coreontology.GeocodingClientInterface
	nomOpts := geocoding.NominatimOptionsFromEnv()
	nomCached := geocoding.NewCachedClientWithService(nomOpts, externalCache, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderNominatim)
	if gcBase := httphandlers.GetGoogleGeocodingClientFromEnv(); gcBase != nil {
		gcAdapter := geocoding.NewAdapter(gcBase)
		gcCached := geocoding.NewCachedClientWithService(gcAdapter, externalCache, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
		geocodingClient = geocoding.NewFallbackClient(nomCached, gcCached, log.Default())
		log.Printf("Geocoding: Nominatim (primary) + Google (fallback)")
	} else {
		geocodingClient = nomCached
		log.Printf("Geocoding: Nominatim only (GOOGLE_MAPS_API_KEY not in env/Secret Manager)")
	}

	zones, err := listZoneFragmentsWithCoords(ctx, fs, *limit)
	if err != nil {
		log.Fatalf("list zones: %v", err)
	}
	log.Printf("Found %d AFFECTED_ZONE fragments with coordinates", len(zones))
	if len(zones) == 0 {
		return
	}

	var out *os.File
	if p := strings.TrimSpace(*outPath); p != "" {
		out, err = os.Create(p)
		if err != nil {
			log.Fatalf("create out file: %v", err)
		}
		defer out.Close()
	} else {
		out = os.Stdout
	}
	enc := json.NewEncoder(out)
	if out == os.Stdout {
		enc.SetIndent("", "  ")
	}

	fixed := 0
	errors := 0
	for i, z := range zones {
		expectedCCs := extractExpectedCountryCodes(z.CanonicalPath)
		if len(expectedCCs) == 0 {
			// Regional token (europe, central-asia, etc.) — skip country check
			continue
		}

		lat, lon := z.Coordinates.Lat, z.Coordinates.Lon
		if !models.IsValidCoordinates(lat, lon) || !models.IsRealGeoCoordinate(lat, lon) {
			continue
		}

		actualCC, err := gnCached.GetCountryCode(ctx, lat, lon)
		if err != nil {
			log.Printf("[%d/%d] %s: GetCountryCode error: %v", i+1, len(zones), z.CanonicalPath, err)
			errors++
			continue
		}
		if actualCC == "" {
			// Point in water or unknown — skip
			continue
		}

		if contains(expectedCCs, actualCC) {
			continue
		}

		// Mismatch: coordinates not in expected country
		log.Printf("[MISMATCH] %s: geo=%s expected=%v actual=%s (%.4f, %.4f)",
			z.CanonicalPath, extractGeoFromPath(z.CanonicalPath), expectedCCs, actualCC, lat, lon)

		row := map[string]interface{}{
			"canonicalPath": z.CanonicalPath,
			"label":         z.Labels["en"],
			"expectedCCs":   expectedCCs,
			"actualCC":      actualCC,
			"lat":           lat,
			"lon":           lon,
		}
		fixedThis := false

		// Try to re-geocode via our geo services.
		// EnrichCoordinatesFromGeoNames skips when fragment already has coordinates, so temporarily clear.
		origCoords := z.Coordinates
		z.Coordinates = nil
		reCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		changed, err := henrich.EnrichCoordinatesFromGeoNames(reCtx, gnCached, z)
		cancel()
		if !changed && origCoords != nil {
			z.Coordinates = origCoords // restore if enrich didn't change
		}
		if err != nil {
			log.Printf("  EnrichCoordinatesFromGeoNames: %v", err)
			row["enrichError"] = err.Error()
		} else if changed && z.Coordinates != nil {
			newLat, newLon := z.Coordinates.Lat, z.Coordinates.Lon
			newCC, _ := gnCached.GetCountryCode(ctx, newLat, newLon)
			if contains(expectedCCs, newCC) {
				log.Printf("  -> fixed via GeoNames: (%.4f, %.4f) -> (%.4f, %.4f) [%s]", lat, lon, newLat, newLon, newCC)
				row["fixed"] = true
				row["newLat"] = newLat
				row["newLon"] = newLon
				row["newCC"] = newCC
				fixedThis = true
				if doApply {
					if err := fragmentsRepo.Update(ctx, z); err != nil {
						log.Printf("  update failed: %v", err)
						row["updateError"] = err.Error()
						errors++
					} else {
						fixed++
					}
				} else {
					fixed++
				}
			}
		}

		// If not fixed yet, try geocoding with label+country for each expected country (handles composite geo in-pk, kg-tj).
		if !fixedThis {
			label := ""
			if z.Labels != nil {
				label = strings.TrimSpace(z.Labels["en"])
			}
			if label == "" {
				label = extractSlugFromPath(z.CanonicalPath)
			}
			// Try geocoding with label+country
			if label != "" {
				for _, cc := range expectedCCs {
					if fixedThis {
						break
					}
					addr := label + ", " + cc
					reCtx2, cancel2 := context.WithTimeout(ctx, 15*time.Second)
					res, err := geocodingClient.Geocode(reCtx2, addr)
					cancel2()
					if *verbose {
						if err != nil {
							log.Printf("  geocode %q: err=%v", addr, err)
						} else if res == nil {
							log.Printf("  geocode %q: no result", addr)
						} else {
							gotCC, _ := gnCached.GetCountryCode(ctx, res.Lat, res.Lon)
							log.Printf("  geocode %q: (%.4f, %.4f) -> %s", addr, res.Lat, res.Lon, gotCC)
						}
					}
					if err != nil || res == nil || !models.IsValidCoordinates(res.Lat, res.Lon) {
						continue
					}
					newCC, _ := gnCached.GetCountryCode(ctx, res.Lat, res.Lon)
					if contains(expectedCCs, newCC) {
						log.Printf("  -> fixed via Geocoding(%s): (%.4f, %.4f) -> (%.4f, %.4f) [%s]", cc, lat, lon, res.Lat, res.Lon, newCC)
						z.Coordinates = &models.Coordinates{Lat: res.Lat, Lon: res.Lon}
						row["fixed"] = true
						row["newLat"] = res.Lat
						row["newLon"] = res.Lon
						row["newCC"] = newCC
						row["source"] = "geocoding"
						fixedThis = true
						if doApply {
							if err := fragmentsRepo.Update(ctx, z); err != nil {
								log.Printf("  update failed: %v", err)
								row["updateError"] = err.Error()
								errors++
							} else {
								fixed++
							}
						} else {
							fixed++
						}
						break
					}
				}
			}
			// Fallback: GeoNames Search when geocoding returns no result (try shorter query per country)
			if !fixedThis && label != "" {
				slug := extractSlugFromPath(z.CanonicalPath)
				searchTerms := []string{label, slug}
				if label != slug {
					// Add first two words as fallback for long names
					words := strings.Fields(slug)
					if len(words) >= 2 {
						searchTerms = append(searchTerms, words[0]+" "+words[1])
					}
				}
				// Known fallbacks for zones that geocode/GeoNames often miss
				if strings.Contains(slug, "somalia") {
					searchTerms = append(searchTerms, "Mogadishu")
				}
				if strings.Contains(slug, "darien") {
					searchTerms = append(searchTerms, "Yaviza", "Darien")
				}
				for _, cc := range expectedCCs {
					if fixedThis {
						break
					}
					for _, term := range searchTerms {
						if fixedThis || term == "" {
							break
						}
						opts := coreontology.GeoNamesSearchOptions{Country: cc, NameEquals: false}
						reCtx3, cancel3 := context.WithTimeout(ctx, 15*time.Second)
						resp, err := gnCached.Search(reCtx3, term, 10, opts)
						cancel3()
						if err != nil || resp == nil || len(resp.Results) == 0 {
							continue
						}
						// Pick first result in expected country
						for _, r := range resp.Results {
							if !strings.EqualFold(strings.TrimSpace(r.CountryCode), cc) {
								continue
							}
							latVal, errLat := strconv.ParseFloat(strings.TrimSpace(r.Lat), 64)
							lonVal, errLon := strconv.ParseFloat(strings.TrimSpace(r.Lng), 64)
							if errLat != nil || errLon != nil || !models.IsValidCoordinates(latVal, lonVal) {
								continue
							}
							newCC := strings.ToUpper(strings.TrimSpace(r.CountryCode))
							if contains(expectedCCs, newCC) {
								log.Printf("  -> fixed via GeoNames(%s): (%.4f, %.4f) -> (%.4f, %.4f) [%s] (%s)", cc, lat, lon, latVal, lonVal, newCC, r.Name)
								z.Coordinates = &models.Coordinates{Lat: latVal, Lon: lonVal}
								row["fixed"] = true
								row["newLat"] = latVal
								row["newLon"] = lonVal
								row["newCC"] = newCC
								row["source"] = "geonames"
								fixedThis = true
								if doApply {
									if err := fragmentsRepo.Update(ctx, z); err != nil {
										log.Printf("  update failed: %v", err)
										row["updateError"] = err.Error()
										errors++
									} else {
										fixed++
									}
								} else {
									fixed++
								}
								break
							}
						}
					}
				}
			}
		}

		if err := enc.Encode(row); err != nil {
			log.Printf("encode: %v", err)
		}
		if (i+1)%20 == 0 {
			log.Printf("Audited %d/%d", i+1, len(zones))
		}
	}
	log.Printf("Done. Mismatches fixed: %d", fixed)
}

func listZoneFragmentsWithCoords(ctx context.Context, db *firestore.Client, limit int) ([]*models.HorizonFragment, error) {
	col := db.Collection("horizon_fragments")
	q := col.
		Where("tenantId", "==", "global").
		Where("canonicalPath", ">=", "/zone/").
		Where("canonicalPath", "<", "/zone/\ufffd").
		OrderBy("canonicalPath", firestore.Asc).
		Limit(5000)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	out := make([]*models.HorizonFragment, 0, len(docs))
	for _, d := range docs {
		var f models.HorizonFragment
		if err := d.DataTo(&f); err != nil {
			continue
		}
		if f.ID == "" {
			f.ID = d.Ref.ID
		}
		// Resolve coordinates from any source
		lat, lon, ok := resolveFragmentCoordinates(&f)
		if !ok || !models.IsRealGeoCoordinate(lat, lon) {
			continue
		}
		if f.Coordinates == nil {
			f.Coordinates = &models.Coordinates{Lat: lat, Lon: lon}
		}
		out = append(out, &f)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func resolveFragmentCoordinates(f *models.HorizonFragment) (float64, float64, bool) {
	if f == nil {
		return 0, 0, false
	}
	if f.Coordinates != nil && models.IsValidCoordinates(f.Coordinates.Lat, f.Coordinates.Lon) {
		return f.Coordinates.Lat, f.Coordinates.Lon, true
	}
	if f.Track != nil && models.IsValidCoordinates(f.Track.Lat, f.Track.Lon) {
		return f.Track.Lat, f.Track.Lon, true
	}
	if wm := f.EffectiveHorizonMonitor(); wm != nil && wm.Coordinates != nil &&
		models.IsValidCoordinates(wm.Coordinates.Lat, wm.Coordinates.Lon) {
		return wm.Coordinates.Lat, wm.Coordinates.Lon, true
	}
	if f.HomeLocation != nil && f.HomeLocation.ISO6709 != nil {
		if lat, lon, ok := models.ParseISO6709Coordinates(*f.HomeLocation.ISO6709); ok {
			return lat, lon, true
		}
	}
	return 0, 0, false
}

// extractExpectedCountryCodes returns ISO2 codes from zone geo slug.
// /zone/af/... -> ["AF"], /zone/tr-sy/... -> ["TR","SY"], /zone/europe/... -> [].
func extractExpectedCountryCodes(canonicalPath string) []string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/zone/") {
		return nil
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) < 2 || parts[0] != "zone" {
		return nil
	}
	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	if geo == "" || geo == "na" {
		return nil
	}
	// Regional token first (me=middle-east, mena, etc.) — no single country to check
	if taxonomy.IsRegionalGeoToken(geo) {
		return nil
	}
	// ISO2
	if len(geo) == 2 && isISO2(geo) {
		return []string{strings.ToUpper(geo)}
	}
	// Composite (tr-sy, ae-sa-om)
	if idx := strings.Index(geo, "-"); idx > 0 {
		ccs := make([]string, 0, 3)
		for _, part := range strings.Split(geo, "-") {
			p := strings.TrimSpace(part)
			if len(p) == 2 && isISO2(p) {
				ccs = append(ccs, strings.ToUpper(p))
			}
		}
		if len(ccs) > 0 {
			return ccs
		}
	}
	return nil
}

func extractGeoFromPath(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")
	if len(parts) >= 2 && parts[0] == "zone" {
		return parts[1]
	}
	return ""
}

func extractSlugFromPath(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")
	if len(parts) >= 4 && parts[0] == "zone" {
		return strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(parts[3]))
	}
	return ""
}

func isISO2(s string) bool {
	if len(s) != 2 {
		return false
	}
	s = strings.ToLower(strings.TrimSpace(s))
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func contains(ccs []string, cc string) bool {
	cc = strings.ToUpper(strings.TrimSpace(cc))
	for _, c := range ccs {
		if strings.ToUpper(strings.TrimSpace(c)) == cc {
			return true
		}
	}
	return false
}
