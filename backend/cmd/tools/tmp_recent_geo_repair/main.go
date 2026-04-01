package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/cmd/tools/internal/artifactrewrite"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/geocoding"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	coreRepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var (
	projectID     = flag.String("project", "nv-horizon-test", "GCP project ID")
	tenantID      = flag.String("tenant", "global", "Tenant ID")
	limit         = flag.Int("limit", 260, "Recent fragment count to inspect")
	skip          = flag.Int("skip", 0, "Skip this many most-recent fragments before processing")
	apply         = flag.Bool("apply", false, "Apply changes")
	artifactLimit = flag.Int("artifact-limit", 500, "Max materialized supply-chain artifacts to rewrite per updated node")
	sinceRaw      = flag.String("since", "", "Only inspect fragments updated at/after RFC3339 timestamp")
	pathsRaw      = flag.String("paths", "", "Comma-separated canonical paths to repair directly")
)

type repairAction struct {
	UpdatedAt time.Time
	Type      hmodels.FragmentType
	Path      string
	Action    string
	Reason    string
}

func main() {
	flag.Parse()

	ctx := context.Background()
	since := parseSince(*sinceRaw)

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	cacheRepo := coreRepo.NewExternalDataCacheRepository(db)
	geocoder := combinedGeoClient(ctx, strings.TrimSpace(*projectID), cacheRepo)

	docs, err := loadRepairDocs(ctx, db, strings.TrimSpace(*tenantID), since)
	if err != nil {
		log.Fatalf("load fragments: %v", err)
	}

	actions := make([]repairAction, 0)
	applied := 0

	for idx, doc := range docs {
		if idx < *skip {
			continue
		}
		var frag hmodels.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		if strings.TrimSpace(frag.ID) == "" {
			frag.ID = doc.Ref.ID
		}
		if !since.IsZero() && frag.UpdatedAt.Before(since) {
			continue
		}

		cloned := cloneFragment(&frag)
		if cloned == nil {
			continue
		}

		action, changed := repairFragment(ctx, geocoder, &frag, cloned)
		if !changed {
			continue
		}
		actions = append(actions, action)
		if *apply {
			if err := applyRepair(ctx, db, doc.Ref, fragmentsRepo, cloned, action.Action, *artifactLimit); err != nil {
				log.Printf("update %s: %v", cloned.CanonicalPath, err)
				continue
			}
			applied++
		}
	}

	sort.Slice(actions, func(i, j int) bool {
		if actions[i].UpdatedAt.Equal(actions[j].UpdatedAt) {
			return actions[i].Path < actions[j].Path
		}
		return actions[i].UpdatedAt.After(actions[j].UpdatedAt)
	})

	fmt.Printf("recent_inspected=%d changed=%d applied=%d project=%s since=%s skip=%d\n", len(docs), len(actions), applied, strings.TrimSpace(*projectID), since.UTC().Format(time.RFC3339), *skip)
	for _, action := range actions {
		fmt.Printf("- %s | %s | %s | %s | %s\n",
			action.UpdatedAt.UTC().Format(time.RFC3339),
			action.Type,
			action.Path,
			action.Action,
			action.Reason,
		)
	}
}

func parseSince(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		log.Fatalf("invalid --since %q: %v", raw, err)
	}
	return parsed.UTC()
}

func loadRepairDocs(ctx context.Context, db *firestore.Client, tenant string, since time.Time) ([]*firestore.DocumentSnapshot, error) {
	if paths := parsePaths(*pathsRaw); len(paths) > 0 {
		docs := make([]*firestore.DocumentSnapshot, 0, len(paths))
		for _, path := range paths {
			found, err := db.Collection("horizon_fragments").
				Where("tenantId", "==", tenant).
				Where("canonicalPath", "==", path).
				Limit(1).
				Documents(ctx).
				GetAll()
			if err != nil {
				return nil, err
			}
			if len(found) == 0 {
				log.Printf("WARN path not found: %s", path)
				continue
			}
			docs = append(docs, found[0])
		}
		return docs, nil
	}
	return db.Collection("horizon_fragments").
		Where("tenantId", "==", tenant).
		OrderBy("updatedAt", firestore.Desc).
		Limit(*limit).
		Documents(ctx).
		GetAll()
}

func parsePaths(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func repairFragment(
	ctx context.Context,
	geocoder coreontology.GeocodingClientInterface,
	original *hmodels.HorizonFragment,
	cloned *hmodels.HorizonFragment,
) (repairAction, bool) {
	action := repairAction{
		UpdatedAt: original.UpdatedAt,
		Type:      original.Type,
		Path:      strings.TrimSpace(original.CanonicalPath),
	}
	switch original.Type {
	case hmodels.FragmentTypeFacility:
		if !strings.HasPrefix(strings.TrimSpace(original.CanonicalPath), "/fac/") {
			return action, false
		}
		resetGeoResolution(cloned)
		seedPathCountry(cloned)
		clearWeakHomeLocationName(cloned)
		res, err := scout.RevalidateFacilityGeo(ctx, cloned, geocoder)
		if err != nil {
			action.Action = "skip"
			action.Reason = fmt.Sprintf("facility revalidate error: %v", err)
			return action, false
		}
		if !res.Applied {
			return action, false
		}
		if !materiallyChanged(original, cloned) {
			return action, false
		}
		action.Action = "revalidated"
		action.Reason = res.Reason
		return action, true
	case hmodels.FragmentTypeOrganization:
		if !strings.HasPrefix(strings.TrimSpace(original.CanonicalPath), "/org/") && !strings.HasPrefix(strings.TrimSpace(original.CanonicalPath), "/gov/") {
			return action, false
		}
		resetGeoResolution(cloned)
		seedPathCountry(cloned)
		clearWeakHomeLocationName(cloned)
		res, err := scout.RevalidateOrgGeo(ctx, cloned, geocoder)
		if err != nil {
			action.Action = "skip"
			action.Reason = fmt.Sprintf("org revalidate error: %v", err)
			return action, false
		}
		if !res.Applied {
			return action, false
		}
		if !materiallyChanged(original, cloned) {
			return action, false
		}
		action.Action = "revalidated"
		action.Reason = res.Reason
		return action, true
	case hmodels.FragmentTypeMarket:
		if !shouldClearMarketCoordinates(original) {
			return action, false
		}
		clearFragmentCoordinates(cloned)
		seedPathCountry(cloned)
		if cloned.HomeLocation != nil && isWeakGeoPhrase(cloned.HomeLocation.Name) {
			cloned.HomeLocation.Name = ""
		}
		action.Action = "cleared_coords"
		action.Reason = "market coordinates came from sector-slug geocoding and are not semantically valid"
		return action, materiallyChanged(original, cloned)
	default:
		return action, false
	}
}

func shouldClearMarketCoordinates(fragment *hmodels.HorizonFragment) bool {
	if fragment == nil || fragment.Type != hmodels.FragmentTypeMarket {
		return false
	}
	if !hmodels.HasRealGeoCoordinates(fragment.Coordinates) {
		return false
	}
	query := ""
	method := ""
	if fragment.Identifiers != nil {
		query = strings.TrimSpace(fragment.Identifiers["geoCoordinatesQuery"])
		method = strings.TrimSpace(fragment.Identifiers["geoCoordinatesMethod"])
	}
	if method == "geocoding_candidate" {
		return true
	}
	if isWeakGeoPhrase(query) {
		return true
	}
	if fragment.HomeLocation == nil || strings.TrimSpace(fragment.HomeLocation.Name) == "" || isWeakGeoPhrase(fragment.HomeLocation.Name) {
		return true
	}
	return false
}

func seedPathCountry(fragment *hmodels.HorizonFragment) {
	if fragment == nil {
		return
	}
	cc := extractPathCountry(fragment.CanonicalPath)
	if cc == "" {
		return
	}
	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &hmodels.Location{}
	}
	if fragment.HomeLocation.ISO3166_1 == nil || !strings.EqualFold(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1), cc) {
		v := cc
		fragment.HomeLocation.ISO3166_1 = &v
	}
}

func extractPathCountry(path string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(path)), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "fac", "org", "gov", "market":
		if taxonomy.IsISO2CountryCode(parts[1]) {
			return strings.ToUpper(parts[1])
		}
	}
	return ""
}

func clearFragmentCoordinates(fragment *hmodels.HorizonFragment) {
	if fragment == nil {
		return
	}
	fragment.Coordinates = nil
	if fragment.HomeLocation != nil {
		fragment.HomeLocation.ISO6709 = nil
	}
	resetGeoResolution(fragment)
}

func resetGeoResolution(fragment *hmodels.HorizonFragment) {
	if fragment == nil {
		return
	}
	if fragment.Identifiers != nil {
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
	}
	if fragment.Qualifiers != nil {
		delete(fragment.Qualifiers, "geoResolveTrace")
	}
}

func clearWeakHomeLocationName(fragment *hmodels.HorizonFragment) {
	if fragment == nil || fragment.HomeLocation == nil {
		return
	}
	if isWeakGeoPhrase(fragment.HomeLocation.Name) {
		fragment.HomeLocation.Name = ""
	}
}

func materiallyChanged(before *hmodels.HorizonFragment, after *hmodels.HorizonFragment) bool {
	if before == nil || after == nil {
		return false
	}
	if strings.TrimSpace(before.CanonicalPath) != strings.TrimSpace(after.CanonicalPath) {
		return true
	}
	if before.HomeLocation == nil && after.HomeLocation != nil {
		return true
	}
	if before.HomeLocation != nil && after.HomeLocation == nil {
		return true
	}
	if !sameCoordinates(before.Coordinates, after.Coordinates) {
		return true
	}
	if before.HomeLocation != nil && after.HomeLocation != nil {
		if !sameStringPtr(before.HomeLocation.ISO3166_1, after.HomeLocation.ISO3166_1) {
			return true
		}
		if !sameStringPtr(before.HomeLocation.ISO6709, after.HomeLocation.ISO6709) {
			return true
		}
		if strings.TrimSpace(before.HomeLocation.Name) != strings.TrimSpace(after.HomeLocation.Name) {
			return true
		}
	}
	if geoAttr(before, "geoCoordinatesQuery") != geoAttr(after, "geoCoordinatesQuery") {
		return true
	}
	if geoAttr(before, "geoCoordinatesFormattedAddress") != geoAttr(after, "geoCoordinatesFormattedAddress") {
		return true
	}
	if geoAttr(before, "geoCoordinatesCountryAlpha2") != geoAttr(after, "geoCoordinatesCountryAlpha2") {
		return true
	}
	return false
}

func sameCoordinates(a, b *hmodels.Coordinates) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return abs(a.Lat-b.Lat) <= 0.000001 && abs(a.Lon-b.Lon) <= 0.000001
}

func sameStringPtr(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return strings.TrimSpace(*a) == strings.TrimSpace(*b)
}

func geoAttr(fragment *hmodels.HorizonFragment, key string) string {
	if fragment == nil || fragment.Identifiers == nil {
		return ""
	}
	return strings.TrimSpace(fragment.Identifiers[key])
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func isWeakGeoPhrase(raw string) bool {
	head := strings.TrimSpace(strings.Split(strings.TrimSpace(raw), ",")[0])
	if head == "" {
		return true
	}
	tokens := strings.Fields(normalizeGeoPhrase(head))
	if len(tokens) == 0 {
		return true
	}
	signal := 0
	for _, token := range tokens {
		switch {
		case len(token) <= 2:
			continue
		case isWeakGeoToken(token):
			continue
		default:
			signal++
		}
	}
	if signal == 0 {
		return true
	}
	return len(tokens) == 1 && signal == 1 && len(tokens[0]) <= 4
}

func isWeakGeoToken(token string) bool {
	switch strings.TrimSpace(strings.ToLower(token)) {
	case "plant", "factory", "facility", "facilities", "field", "launch", "power", "campus", "center", "centre",
		"station", "terminal", "site", "network", "group", "company", "industries", "industry", "service", "services",
		"ministry", "department", "authority", "bureau", "office", "national", "people", "peoples", "defence", "defense",
		"production", "processing", "manufacturing", "general", "pdf", "hub", "hubs", "chemical", "chemicals",
		"showroom", "warehouse", "warehousing", "depot", "storage", "logistics", "distribution", "retail",
		"consumer", "industrial", "automotive", "personal", "care", "commercial", "operations", "sales",
		"border", "crossing", "checkpoint", "post", "supermarket", "hypermarket", "store", "shop", "outlet":
		return true
	default:
		return false
	}
}

func normalizeGeoPhrase(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	repl := strings.NewReplacer(",", " ", ".", " ", "-", " ", "_", " ", "(", " ", ")", " ", "/", " ")
	s = repl.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func cloneFragment(in *hmodels.HorizonFragment) *hmodels.HorizonFragment {
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
	if in.Qualifiers != nil {
		cp.Qualifiers = map[string]interface{}{}
		for k, v := range in.Qualifiers {
			cp.Qualifiers[k] = v
		}
	}
	return &cp
}

func applyRepair(
	ctx context.Context,
	db *firestore.Client,
	ref *firestore.DocumentRef,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	fragment *hmodels.HorizonFragment,
	action string,
	artifactLimit int,
) error {
	if ref == nil || fragment == nil {
		return nil
	}
	updateFragmentOnly := func() error {
		if action == "cleared_coords" {
			updates := []firestore.Update{
				{Path: "coordinates", Value: firestore.Delete},
				{Path: "hasCoordinates", Value: false},
				{Path: "updatedAt", Value: time.Now().UTC()},
				{Path: "updatedBy", Value: "codex_recent_geo_fix"},
				{Path: "homeLocation.iso6709", Value: firestore.Delete},
				{Path: "qualifiers.geoResolveTrace", Value: firestore.Delete},
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
				updates = append(updates, firestore.Update{Path: "identifiers." + key, Value: firestore.Delete})
			}
			if fragment.HomeLocation != nil && strings.TrimSpace(fragment.HomeLocation.Name) == "" {
				updates = append(updates, firestore.Update{Path: "homeLocation.name", Value: firestore.Delete})
			} else if fragment.HomeLocation != nil {
				updates = append(updates, firestore.Update{Path: "homeLocation.name", Value: fragment.HomeLocation.Name})
			}
			if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
				updates = append(updates, firestore.Update{Path: "homeLocation.iso3166_1", Value: strings.TrimSpace(*fragment.HomeLocation.ISO3166_1)})
			}
			if len(fragment.IndexPaths) > 0 {
				updates = append(updates, firestore.Update{Path: "indexPaths", Value: fragment.IndexPaths})
			}
			_, err := ref.Update(ctx, updates)
			return err
		}
		fragment.IndexPaths = hmodels.BuildIndexPaths(fragment, nil)
		return fragmentsRepo.Update(ctx, fragment)
	}
	if action == "cleared_coords" {
		if err := updateFragmentOnly(); err != nil {
			return err
		}
		_, err := artifactrewrite.RewriteNodeArtifacts(ctx, db, strings.TrimSpace(fragment.CanonicalPath), "", artifactLimit, true, nil)
		return err
	}
	if err := updateFragmentOnly(); err != nil {
		return err
	}
	_, err := artifactrewrite.RewriteNodeArtifacts(ctx, db, strings.TrimSpace(fragment.CanonicalPath), "", artifactLimit, true, nil)
	return err
}

func combinedGeoClient(ctx context.Context, project string, cacheRepo *coreRepo.ExternalDataCacheRepository) coreontology.GeocodingClientInterface {
	if key := gcp.GetEnvOrSecret(ctx, project, "GOOGLE_MAPS_API_KEY", "google_maps_api_key"); key != "" && strings.TrimSpace(os.Getenv("GOOGLE_MAPS_API_KEY")) == "" {
		_ = os.Setenv("GOOGLE_MAPS_API_KEY", key)
	}
	var googleClient coreontology.GeocodingClientInterface
	if gc := httphandlers.GetGoogleGeocodingClientFromEnv(); gc != nil {
		googleClient = geocoding.NewCachedClientWithService(geocoding.NewAdapter(gc), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderGoogle)
	}
	nominatimClient := geocoding.NewCachedClientWithService(geocoding.NominatimOptionsFromEnv(), cacheRepo, geocoding.CacheOptionsFromEnv(), coreontology.GeocodingProviderNominatim)
	if googleClient == nil {
		return nominatimClient
	}
	return geocoding.NewFallbackClient(googleClient, nominatimClient, nil)
}
