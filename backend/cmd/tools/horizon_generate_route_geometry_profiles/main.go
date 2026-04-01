package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hpath "github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type candidateAggregate struct {
	ProfileKey              string   `json:"profileKey"`
	OrderedRefs             []string `json:"orderedRefs"`
	RouteVariantKey         string   `json:"routeVariantKey,omitempty"`
	SupplyLinkCanonicalPath string   `json:"supplyLinkCanonicalPath,omitempty"`
	PathArtifactKey         string   `json:"pathArtifactKey,omitempty"`
	Count                   int      `json:"count"`
}

type osrmResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"routes"`
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	reportPath := flag.String("report", "", "path to unresolved route aggregate JSON report")
	limit := flag.Int("limit", 0, "max candidate aggregates to process (0 = all)")
	apply := flag.Bool("apply", false, "apply writes to horizon_route_geometry_profiles")
	seaPython := flag.String("sea-python", "", "optional python executable with searoute installed")
	osrmURL := flag.String("osrm-url", "https://router.project-osrm.org", "OSRM base URL for land routing")
	sleepMs := flag.Int("sleep-ms", 250, "sleep between generated profiles")
	flag.Parse()

	if strings.TrimSpace(*reportPath) == "" {
		log.Fatal("--report is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fs.Close()

	db := fs.Client()
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	profilesRepo := hrepo.NewRouteGeometryProfilesRepository(db)

	candidates, err := loadCandidates(strings.TrimSpace(*reportPath))
	if err != nil {
		log.Fatalf("failed to load report: %v", err)
	}
	if *limit > 0 && len(candidates) > *limit {
		candidates = candidates[:*limit]
	}

	stats := map[string]int{}
	for idx, candidate := range candidates {
		refs := models.NormalizeOrderedRouteRefs(candidate.OrderedRefs)
		if len(refs) < 2 {
			stats["skipped_short"]++
			continue
		}
		fragsByPath, err := fragmentsRepo.BatchGetByCanonicalPaths(ctx, refs)
		if err != nil {
			stats["fetch_error"]++
			log.Printf("candidate fetch failed profileKey=%s err=%v", candidate.ProfileKey, err)
			continue
		}
		profile, reason, err := buildProfile(ctx, strings.TrimSpace(*tenantID), candidate, fragsByPath, strings.TrimSpace(*osrmURL), strings.TrimSpace(*seaPython))
		if err != nil {
			stats["build_error"]++
			log.Printf("candidate build failed profileKey=%s err=%v", candidate.ProfileKey, err)
			continue
		}
		if profile == nil {
			stats["skipped_"+reason]++
			continue
		}
		stats["generated_"+profile.Source]++
		log.Printf("route_profile generated idx=%d/%d profileKey=%s source=%s refs=%s", idx+1, len(candidates), profile.ProfileKey, profile.Source, strings.Join(profile.OrderedRefs, " -> "))
		if *apply {
			if err := profilesRepo.Upsert(ctx, profile); err != nil {
				stats["write_error"]++
				log.Printf("route_profile upsert failed profileKey=%s err=%v", profile.ProfileKey, err)
			} else {
				stats["applied"]++
			}
		}
		if *sleepMs > 0 {
			time.Sleep(time.Duration(*sleepMs) * time.Millisecond)
		}
	}

	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, stats[key]))
	}
	log.Printf("route_profile_generation summary: project=%s tenant=%s candidates=%d apply=%v stats=%s", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), len(candidates), *apply, strings.Join(parts, ","))
}

func loadCandidates(path string) ([]candidateAggregate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []candidateAggregate
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return strings.Join(out[i].OrderedRefs, "->") < strings.Join(out[j].OrderedRefs, "->")
		}
		return out[i].Count > out[j].Count
	})
	return out, nil
}

func buildProfile(
	ctx context.Context,
	tenantID string,
	candidate candidateAggregate,
	fragsByPath map[string]*models.HorizonFragment,
	osrmURL string,
	seaPython string,
) (*models.RouteGeometryProfile, string, error) {
	refs := models.NormalizeOrderedRouteRefs(candidate.OrderedRefs)
	mode := inferMode(refs, fragsByPath)
	switch mode {
	case "sea":
		if seaPython == "" {
			return nil, "sea_python_missing", nil
		}
		coords, err := buildSeaRoute(refs, fragsByPath, seaPython)
		if err != nil {
			return nil, "", err
		}
		if len(coords) < 2 {
			return nil, "sea_unresolved", nil
		}
		return &models.RouteGeometryProfile{
			TenantID:        tenantID,
			ProfileKey:      firstNonEmpty(candidate.ProfileKey, models.BuildRouteGeometryProfileKey(refs)),
			RouteVariantKey: strings.TrimSpace(candidate.RouteVariantKey),
			OrderedRefs:     refs,
			Coordinates:     coords,
			Source:          "auto_searoute",
			Accuracy:        "generated",
			Notes:           strings.TrimSpace(candidate.SupplyLinkCanonicalPath),
		}, "", nil
	case "land":
		coords, err := buildLandRoute(ctx, refs, fragsByPath, osrmURL)
		if err != nil {
			return nil, "", err
		}
		if len(coords) < 2 {
			return nil, "land_unresolved", nil
		}
		return &models.RouteGeometryProfile{
			TenantID:        tenantID,
			ProfileKey:      firstNonEmpty(candidate.ProfileKey, models.BuildRouteGeometryProfileKey(refs)),
			RouteVariantKey: strings.TrimSpace(candidate.RouteVariantKey),
			OrderedRefs:     refs,
			Coordinates:     coords,
			Source:          "auto_osrm_driving",
			Accuracy:        "generated",
			Notes:           strings.TrimSpace(candidate.SupplyLinkCanonicalPath),
		}, "", nil
	default:
		return nil, mode, nil
	}
}

func inferMode(refs []string, fragsByPath map[string]*models.HorizonFragment) string {
	tokens := make(map[string]struct{}, 64)
	addTokens := func(raw string) {
		raw = strings.ToLower(strings.TrimSpace(raw))
		if raw == "" {
			return
		}
		replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", ",", " ", "(", " ", ")", " ")
		for _, token := range strings.Fields(replacer.Replace(raw)) {
			token = strings.TrimSpace(token)
			if len(token) < 2 {
				continue
			}
			tokens[token] = struct{}{}
		}
	}
	for _, ref := range refs {
		addTokens(ref)
	}
	for _, frag := range fragsByPath {
		if frag == nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(frag.CanonicalPath)), "/loc/") {
			addTokens(frag.GeoFeatureType)
		}
		for _, label := range frag.Labels {
			addTokens(label)
		}
		for _, key := range []string{"facilityType", "routeType", "portType"} {
			addTokens(frag.Identifiers[key])
		}
	}
	if hasAnyToken(tokens, "airport", "airfield") {
		return "mode_unsupported_air"
	}
	if hasAnyToken(tokens, "dry", "gateway", "khorgos", "distribution", "logistics", "rail", "corridor", "inland", "warehouse") {
		return "land"
	}
	if hasAnyToken(tokens, "port", "terminal", "lng", "strait", "canal", "hormuz", "suez", "malacca", "adriatic", "hook") {
		return "sea"
	}
	from, okFrom := extractCoord(fragsByPath[refs[0]])
	to, okTo := extractCoord(fragsByPath[refs[len(refs)-1]])
	if !okFrom || !okTo {
		return "mode_unsupported_missing_coords"
	}
	distanceKm := haversineKm(from, to)
	fromCountry := routeCountry(refs[0], fragsByPath[refs[0]])
	toCountry := routeCountry(refs[len(refs)-1], fragsByPath[refs[len(refs)-1]])
	if fromCountry != "" && toCountry != "" && fromCountry == toCountry && distanceKm <= 1800 {
		return "land"
	}
	if len(refs) >= 3 && distanceKm <= 3500 {
		return "land"
	}
	if distanceKm <= 700 {
		return "land"
	}
	return "mode_unsupported_ambiguous_crossborder"
}

func hasAnyToken(tokens map[string]struct{}, values ...string) bool {
	for _, value := range values {
		if _, ok := tokens[strings.TrimSpace(strings.ToLower(value))]; ok {
			return true
		}
	}
	return false
}

func buildLandRoute(ctx context.Context, refs []string, fragsByPath map[string]*models.HorizonFragment, osrmURL string) ([]models.Coordinates, error) {
	all := make([]models.Coordinates, 0, 32)
	for idx := 0; idx < len(refs)-1; idx++ {
		a, okA := extractCoord(fragsByPath[refs[idx]])
		b, okB := extractCoord(fragsByPath[refs[idx+1]])
		if !okA || !okB {
			return nil, nil
		}
		segment, err := osrmRoute(ctx, osrmURL, a, b)
		if err != nil {
			return nil, err
		}
		all = appendCoordsDedup(all, segment)
	}
	if len(all) < 2 {
		return nil, nil
	}
	return all, nil
}

func buildSeaRoute(refs []string, fragsByPath map[string]*models.HorizonFragment, seaPython string) ([]models.Coordinates, error) {
	all := make([]models.Coordinates, 0, 128)
	for idx := 0; idx < len(refs)-1; idx++ {
		a, okA := extractCoord(fragsByPath[refs[idx]])
		b, okB := extractCoord(fragsByPath[refs[idx+1]])
		if !okA || !okB {
			return nil, nil
		}
		segment, err := seaRoute(seaPython, a, b)
		if err != nil {
			return nil, err
		}
		all = appendCoordsDedup(all, segment)
	}
	if len(all) < 2 {
		return nil, nil
	}
	return all, nil
}

func extractCoord(frag *models.HorizonFragment) (models.Coordinates, bool) {
	if frag == nil {
		return models.Coordinates{}, false
	}
	if models.HasRealGeoCoordinates(frag.Coordinates) {
		return *frag.Coordinates, true
	}
	if wm := frag.EffectiveHorizonMonitor(); wm != nil && wm.Coordinates != nil && models.HasRealGeoCoordinates(wm.Coordinates) {
		return *wm.Coordinates, true
	}
	return models.Coordinates{}, false
}

func osrmRoute(ctx context.Context, baseURL string, from, to models.Coordinates) ([]models.Coordinates, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/route/v1/driving/" +
		url.QueryEscape(fmt.Sprintf("%.6f,%.6f;%.6f,%.6f", from.Lon, from.Lat, to.Lon, to.Lat)) +
		"?overview=full&geometries=geojson"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("osrm status=%d body=%s", resp.StatusCode, string(body))
	}
	var payload osrmResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Routes) == 0 {
		return nil, nil
	}
	out := make([]models.Coordinates, 0, len(payload.Routes[0].Geometry.Coordinates))
	for _, pair := range payload.Routes[0].Geometry.Coordinates {
		if len(pair) < 2 {
			continue
		}
		out = appendCoordsDedup(out, []models.Coordinates{{Lat: pair[1], Lon: pair[0]}})
	}
	return out, nil
}

func seaRoute(pythonBin string, from, to models.Coordinates) ([]models.Coordinates, error) {
	script := `
import json, sys
import searoute as sr
origin=[float(sys.argv[1]), float(sys.argv[2])]
destination=[float(sys.argv[3]), float(sys.argv[4])]
route=sr.searoute(origin, destination)
print(json.dumps(route["geometry"]["coordinates"]))
`
	cmd := exec.Command(
		pythonBin,
		"-c", script,
		fmt.Sprintf("%.6f", from.Lon),
		fmt.Sprintf("%.6f", from.Lat),
		fmt.Sprintf("%.6f", to.Lon),
		fmt.Sprintf("%.6f", to.Lat),
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sea route command failed: %w stderr=%s", err, strings.TrimSpace(stderr.String()))
	}
	var raw [][]float64
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, err
	}
	out := make([]models.Coordinates, 0, len(raw))
	for _, pair := range raw {
		if len(pair) < 2 {
			continue
		}
		out = appendCoordsDedup(out, []models.Coordinates{{Lat: pair[1], Lon: pair[0]}})
	}
	return out, nil
}

func appendCoordsDedup(base, extra []models.Coordinates) []models.Coordinates {
	for _, coord := range extra {
		if !models.IsRealGeoCoordinate(coord.Lat, coord.Lon) {
			continue
		}
		if len(base) > 0 {
			last := base[len(base)-1]
			if abs(last.Lat-coord.Lat) < 1e-7 && abs(last.Lon-coord.Lon) < 1e-7 {
				continue
			}
		}
		base = append(base, coord)
	}
	return base
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func routeCountry(ref string, frag *models.HorizonFragment) string {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if strings.HasPrefix(ref, "/fac/") || strings.HasPrefix(ref, "/org/") || strings.HasPrefix(ref, "/gov/") {
		parts := strings.Split(strings.Trim(ref, "/"), "/")
		if len(parts) >= 2 {
			cc := strings.TrimSpace(parts[1])
			if len(cc) == 2 && cc != "na" {
				return cc
			}
		}
	}
	if frag != nil && frag.HomeLocation != nil && frag.HomeLocation.ISO3166_1 != nil {
		cc := strings.TrimSpace(strings.ToLower(*frag.HomeLocation.ISO3166_1))
		if len(cc) == 2 && cc != "na" {
			return cc
		}
	}
	return ""
}

func haversineKm(a, b models.Coordinates) float64 {
	return routeDistanceKm(a, b)
}

func routeDistanceKm(a, b models.Coordinates) float64 {
	const earthRadiusKm = 6371.0
	toRadians := func(value float64) float64 {
		return value * 3.141592653589793 / 180
	}
	dLat := toRadians(b.Lat - a.Lat)
	dLon := toRadians(b.Lon - a.Lon)
	lat1 := toRadians(a.Lat)
	lat2 := toRadians(b.Lat)
	term := sin2(dLat/2) + cos(lat1)*cos(lat2)*sin2(dLon/2)
	return earthRadiusKm * 2 * atan2Sqrt(term, 1-term)
}

func sin2(v float64) float64 {
	s := math.Sin(v)
	return s * s
}

func cos(v float64) float64 {
	return math.Cos(v)
}

func atan2Sqrt(a, b float64) float64 {
	return math.Atan2(math.Sqrt(a), math.Sqrt(b))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var _ = hpath.ResolveSupplyChainRouteGeometryDetailed
