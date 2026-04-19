package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	"github.com/igorkirisyuk/sanskrite/internal/geonames"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	ah "github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type sample struct {
	Path          string
	Type          string
	Label         string
	Place         string
	CurrentISO2   string
	ResolvedISO2  string
	ChangedCoords bool
	FormattedAddr string
	Query         string
	Provider      string
}

type mismatchSample struct {
	Path         string
	Type         string
	Label        string
	ExpectedISO2 string
	ActualISO2   string
	Lat          float64
	Lon          float64
	Provider     string
	Query        string
}

type kv struct {
	Key   string
	Count int
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	limit := flag.Int("limit", 300, "max fragments to probe")
	sampleN := flag.Int("samples", 12, "samples")
	apply := flag.Bool("apply", false, "persist accepted geo revalidation changes")
	pathFilter := flag.String("path", "", "exact canonical path to inspect")
	labelFilter := flag.String("label", "", "exact labels.en/legalName/shortName to inspect")
	startAfter := flag.String("start-after", "", "resume scan after this document ID")
	providerMode := flag.String("provider", "fallback", "geocoding provider mode: fallback|google|nominatim")
	progressEvery := flag.Int("progress-every", 100, "log progress every N scanned fragments (0 disables)")
	prefilterCountryMismatch := flag.Bool("prefilter-country-mismatch", true, "only geocode fragments whose current coordinates resolve to a different country than canonical iso2")
	flag.Parse()

	if key := gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	externalCacheRepo := coreRepo.NewExternalDataCacheRepository(fs.Client())
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(fs.Client())
	geoNamesClient := geoNamesCountryClient(externalCacheRepo)
	geoClient := combinedGeoClient(externalCacheRepo, strings.TrimSpace(*providerMode))
	if geoClient == nil {
		panic("geocoding client is not configured")
	}

	scanned := 0
	appliedCandidates := 0
	isoConflicts := 0
	coordRepairs := 0
	persisted := 0
	countryMismatchPrefiltered := 0
	byType := map[string]int{}
	byResolved := map[string]int{}
	byProvider := map[string]int{}
	samples := make([]sample, 0, *sampleN)
	mismatchSamples := make([]mismatchSample, 0, *sampleN)

	if strings.TrimSpace(*pathFilter) != "" || strings.TrimSpace(*labelFilter) != "" {
		targets, err := loadTargets(ctx, fs.Client(), fragmentsRepo, strings.TrimSpace(*tenantID), strings.TrimSpace(*pathFilter), strings.TrimSpace(*labelFilter))
		if err != nil {
			panic(err)
		}
		for _, frag := range targets {
			processFragment(ctx, frag, geoNamesClient, geoClient, fragmentsRepo, *apply, *prefilterCountryMismatch, *progressEvery, &scanned, &countryMismatchPrefiltered, &appliedCandidates, &isoConflicts, &coordRepairs, &persisted, byType, byResolved, byProvider, &samples, *sampleN, &mismatchSamples)
		}
	} else {
		q := fs.Client().Collection("horizon_fragments").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			OrderBy(firestore.DocumentID, firestore.Asc)
		if after := strings.TrimSpace(*startAfter); after != "" {
			q = q.StartAfter(after)
		}
		iter := q.Documents(ctx)
		defer iter.Stop()
		lastSeenID := strings.TrimSpace(*startAfter)
		for {
			if *limit > 0 && scanned >= *limit {
				break
			}
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				panic(err)
			}
			var frag h.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = strings.TrimSpace(doc.Ref.ID)
			}
			lastSeenID = strings.TrimSpace(doc.Ref.ID)
			processFragment(ctx, &frag, geoNamesClient, geoClient, fragmentsRepo, *apply, *prefilterCountryMismatch, *progressEvery, &scanned, &countryMismatchPrefiltered, &appliedCandidates, &isoConflicts, &coordRepairs, &persisted, byType, byResolved, byProvider, &samples, *sampleN, &mismatchSamples)
		}
		if lastSeenID != "" {
			fmt.Printf("last_seen_id=%s\n", lastSeenID)
		}
	}

	fmt.Printf("tenant=%s project=%s scanned=%d prefiltered_mismatches=%d candidates=%d iso_conflicts=%d coordinate_repairs=%d persisted=%d\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), scanned, countryMismatchPrefiltered, appliedCandidates, isoConflicts, coordRepairs, persisted)
	printTop("candidates_by_type", topCounts(byType))
	printTop("candidates_by_resolved_iso2", topCounts(byResolved))
	printTop("candidates_by_provider", topCounts(byProvider))
	if len(mismatchSamples) > 0 {
		fmt.Println("prefiltered_mismatch_samples:")
		for _, s := range mismatchSamples {
			fmt.Printf("  - type=%s path=%s expected=%s actual=%s lat=%.6f lon=%.6f label=%q provider=%s query=%q\n",
				s.Type, s.Path, s.ExpectedISO2, s.ActualISO2, s.Lat, s.Lon, s.Label, s.Provider, s.Query)
		}
	}
	if len(samples) > 0 {
		fmt.Println("samples:")
		for _, s := range samples {
			fmt.Printf("  - type=%s path=%s current=%s resolved=%s coords_changed=%t provider=%s label=%q place=%q query=%q formatted=%q\n",
				s.Type, s.Path, s.CurrentISO2, s.ResolvedISO2, s.ChangedCoords, s.Provider, s.Label, s.Place, s.Query, s.FormattedAddr)
		}
	}
}

func combinedGeoClient(cacheRepo *coreRepo.ExternalDataCacheRepository, mode string) coreontology.GeocodingClientInterface {
	var googleClient coreontology.GeocodingClientInterface
	if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
		googleClient = geocoding.NewCachedClientWithService(geocoding.NewAdapter(gc), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
	}
	nominatimClient := geocoding.NewCachedClientWithService(geocoding.NominatimOptionsFromEnv(), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderNominatim)
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "google":
		return googleClient
	case "nominatim":
		return nominatimClient
	default:
		return geocoding.NewFallbackClient(googleClient, nominatimClient, nil)
	}
}

func geoNamesCountryClient(cacheRepo *coreRepo.ExternalDataCacheRepository) coreontology.GeoNamesClientInterface {
	base := httphandlers.GetGeoNamesClientFromEnv()
	if base == nil {
		return nil
	}
	return geonames.NewCachedClient(geonames.NewAdapter(base), cacheRepo, geonames.CacheOptionsFromEnv())
}

func cloneFragment(in *h.HorizonFragment) *h.HorizonFragment {
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

func extractCurrentISO2(fragment *h.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		if v := strings.ToUpper(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)); len(v) == 2 {
			return v
		}
	}
	if fragment.Identifiers != nil {
		for _, key := range []string{"countryIso2", "jurisdictionIso2", "country", "jurisdiction"} {
			if v := strings.ToUpper(strings.TrimSpace(fragment.Identifiers[key])); len(v) == 2 {
				return v
			}
		}
	}
	return ""
}

func fragmentLabel(fragment *h.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	return firstNonEmpty(
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["legalName"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
	)
}

func fragmentPlace(fragment *h.HorizonFragment) string {
	if fragment == nil || fragment.HomeLocation == nil {
		return ""
	}
	return strings.TrimSpace(fragment.HomeLocation.Name)
}

func fragmentMatchesLabel(fragment *h.HorizonFragment, label string) bool {
	if fragment == nil {
		return false
	}
	q := strings.TrimSpace(strings.ToLower(label))
	if q == "" {
		return false
	}
	values := []string{
		strings.ToLower(strings.TrimSpace(fragment.Labels["en"])),
		strings.ToLower(strings.TrimSpace(fragment.Identifiers["legalName"])),
		strings.ToLower(strings.TrimSpace(fragment.Identifiers["shortName"])),
		strings.ToLower(strings.TrimSpace(fragment.CanonicalPath)),
	}
	for _, value := range values {
		if value == q {
			return true
		}
	}
	return false
}

func loadTargets(
	ctx context.Context,
	db *firestore.Client,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	tenantID string,
	pathFilter string,
	labelFilter string,
) ([]*h.HorizonFragment, error) {
	if fragmentsRepo != nil && pathFilter != "" {
		frag, err := fragmentsRepo.GetByCanonicalPath(ctx, pathFilter)
		if err != nil {
			return nil, err
		}
		return []*h.HorizonFragment{frag}, nil
	}
	if fragmentsRepo != nil && labelFilter != "" {
		out := make([]*h.HorizonFragment, 0, 6)
		seen := map[string]struct{}{}
		add := func(items []*h.HorizonFragment) {
			for _, item := range items {
				if item == nil || strings.TrimSpace(item.ID) == "" {
					continue
				}
				if _, ok := seen[item.ID]; ok {
					continue
				}
				seen[item.ID] = struct{}{}
				out = append(out, item)
			}
		}
		if items, err := fragmentsRepo.FindByLabel(ctx, "en", labelFilter, ah.FragmentClass("")); err == nil {
			add(items)
		}
		for _, key := range []string{"legalName", "shortName"} {
			if items, err := fragmentsRepo.FindByIdentifier(ctx, key, labelFilter); err == nil {
				add(items)
			}
		}
		return out, nil
	}

	iter := db.Collection("horizon_fragments").Where("tenantId", "==", tenantID).Documents(ctx)
	defer iter.Stop()
	out := make([]*h.HorizonFragment, 0, 128)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var frag h.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		if strings.TrimSpace(frag.ID) == "" {
			frag.ID = strings.TrimSpace(doc.Ref.ID)
		}
		out = append(out, &frag)
	}
	return out, nil
}

func recordResult(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	apply bool,
	original *h.HorizonFragment,
	updated *h.HorizonFragment,
	currentISO2 string,
	applied bool,
	changedCoords bool,
	resolvedISO2 string,
	query string,
	formatted string,
	provider string,
	appliedCandidates *int,
	isoConflicts *int,
	coordRepairs *int,
	persisted *int,
	byType map[string]int,
	byResolved map[string]int,
	byProvider map[string]int,
	samples *[]sample,
	sampleN int,
) {
	if !applied || original == nil || updated == nil {
		return
	}
	*appliedCandidates = *appliedCandidates + 1
	byType[string(original.Class)]++
	if iso := strings.ToLower(strings.TrimSpace(resolvedISO2)); iso != "" {
		byResolved[iso]++
	}
	if p := strings.TrimSpace(provider); p != "" {
		byProvider[p]++
	}
	if strings.TrimSpace(currentISO2) != "" && strings.TrimSpace(resolvedISO2) != "" && !strings.EqualFold(currentISO2, resolvedISO2) {
		*isoConflicts = *isoConflicts + 1
	}
	if changedCoords {
		*coordRepairs = *coordRepairs + 1
	}
	if len(*samples) < sampleN {
		*samples = append(*samples, sample{
			Path:          original.CanonicalPath,
			Type:          string(original.Class),
			Label:         fragmentLabel(original),
			Place:         fragmentPlace(original),
			CurrentISO2:   currentISO2,
			ResolvedISO2:  resolvedISO2,
			ChangedCoords: changedCoords,
			FormattedAddr: formatted,
			Query:         query,
			Provider:      provider,
		})
	}
	if apply && fragmentsRepo != nil {
		updated.IndexPaths = h.BuildIndexPaths(updated, nil)
		if err := fragmentsRepo.Update(ctx, updated); err == nil {
			*persisted = *persisted + 1
		}
	}
}

func processFragment(
	ctx context.Context,
	frag *h.HorizonFragment,
	geoNamesClient coreontology.GeoNamesClientInterface,
	geoClient coreontology.GeocodingClientInterface,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	apply bool,
	prefilterCountryMismatch bool,
	progressEvery int,
	scanned *int,
	countryMismatchPrefiltered *int,
	appliedCandidates *int,
	isoConflicts *int,
	coordRepairs *int,
	persisted *int,
	byType map[string]int,
	byResolved map[string]int,
	byProvider map[string]int,
	samples *[]sample,
	sampleN int,
	mismatchSamples *[]mismatchSample,
) {
	if frag == nil {
		return
	}
	if frag.Class != ah.FragmentClassSite && frag.Class != ah.FragmentClassOrg {
		return
	}
	*scanned = *scanned + 1
	if progressEvery > 0 && *scanned%progressEvery == 0 {
		log.Printf(
			"progress scanned=%d prefiltered_mismatches=%d candidates=%d coord_repairs=%d persisted=%d last_seen_id=%s path=%s",
			*scanned,
			*countryMismatchPrefiltered,
			*appliedCandidates,
			*coordRepairs,
			*persisted,
			strings.TrimSpace(frag.ID),
			strings.TrimSpace(frag.CanonicalPath),
		)
	}
	if prefilterCountryMismatch {
		actualISO2, expectedISO2, ok := mismatchCountryPair(ctx, geoNamesClient, frag)
		if !ok {
			return
		}
		*countryMismatchPrefiltered = *countryMismatchPrefiltered + 1
		if mismatchSamples != nil && len(*mismatchSamples) < sampleN && frag.Coordinates != nil {
		*mismatchSamples = append(*mismatchSamples, mismatchSample{
			Path:         strings.TrimSpace(frag.CanonicalPath),
			Type:         string(frag.Class),
				Label:        fragmentLabel(frag),
				ExpectedISO2: expectedISO2,
				ActualISO2:   actualISO2,
				Lat:          frag.Coordinates.Lat,
				Lon:          frag.Coordinates.Lon,
				Provider:     strings.TrimSpace(frag.Identifiers["geoCoordinatesProvider"]),
				Query:        strings.TrimSpace(frag.Identifiers["geoCoordinatesQuery"]),
			})
		}
	}

	currentISO2 := extractCurrentISO2(frag)
	clone := cloneFragment(frag)
	switch frag.Class {
	case ah.FragmentClassSite:
		res, _ := scout.RevalidateFacilityGeo(ctx, clone, geoClient)
		recordResult(ctx, fragmentsRepo, apply, frag, clone, currentISO2, res.Applied, res.ChangedCoordinates, res.ResolvedISO2, res.Query, res.FormattedAddress, res.Provider, appliedCandidates, isoConflicts, coordRepairs, persisted, byType, byResolved, byProvider, samples, sampleN)
	case ah.FragmentClassOrg:
		res, _ := scout.RevalidateOrgGeo(ctx, clone, geoClient)
		recordResult(ctx, fragmentsRepo, apply, frag, clone, currentISO2, res.Applied, res.ChangedCoordinates, res.ResolvedISO2, res.Query, res.FormattedAddress, res.Provider, appliedCandidates, isoConflicts, coordRepairs, persisted, byType, byResolved, byProvider, samples, sampleN)
	}
}

func mismatchCountryPair(ctx context.Context, geoNamesClient coreontology.GeoNamesClientInterface, fragment *h.HorizonFragment) (actual string, expected string, ok bool) {
	if geoNamesClient == nil || fragment == nil || fragment.Coordinates == nil {
		return "", "", false
	}
	if !h.IsRealGeoCoordinate(fragment.Coordinates.Lat, fragment.Coordinates.Lon) {
		return "", "", false
	}
	expected = expectedISO2ForPrefilter(fragment)
	if expected == "" {
		return "", "", false
	}
	var err error
	actual, err = geoNamesClient.GetCountryCode(ctx, fragment.Coordinates.Lat, fragment.Coordinates.Lon)
	if err != nil || strings.TrimSpace(actual) == "" {
		return "", "", false
	}
	if strings.EqualFold(strings.TrimSpace(actual), expected) {
		return "", "", false
	}
	return strings.ToUpper(strings.TrimSpace(actual)), strings.ToUpper(strings.TrimSpace(expected)), true
}

func expectedISO2ForPrefilter(fragment *h.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	switch fragment.Class {
	case ah.FragmentClassSite:
		return strings.ToUpper(strings.TrimSpace(scoutFacilityPathISO2(cp)))
	case ah.FragmentClassOrg:
		return strings.ToUpper(strings.TrimSpace(scoutOrgPathISO2(cp)))
	default:
		return ""
	}
}

func scoutFacilityPathISO2(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(canonicalPath)), "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	if len(parts[1]) == 2 && parts[1] != "na" {
		return parts[1]
	}
	return ""
}

func scoutOrgPathISO2(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(canonicalPath)), "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	if (parts[0] == "org" || parts[0] == "gov") && len(parts[1]) == 2 && parts[1] != "na" {
		return parts[1]
	}
	return ""
}

func topCounts(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{Key: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func printTop(title string, rows []kv) {
	fmt.Println(title + ":")
	if len(rows) == 0 {
		fmt.Println("  (none)")
		fmt.Println()
		return
	}
	for _, row := range rows {
		fmt.Printf("  %s: %d\n", row.Key, row.Count)
	}
	fmt.Println()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}
