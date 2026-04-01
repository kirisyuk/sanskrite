package pathbuilder

import (
	"context"
	"math"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type ResolvedRouteGeometry struct {
	Coordinates []models.Coordinates
	FlowRefs    []string
	Source      string
}

func ResolveSupplyChainRouteGeometry(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	refs []string,
) ([]models.Coordinates, string) {
	resolved := ResolveSupplyChainRouteGeometryDetailed(ctx, fragmentsRepo, refs)
	return resolved.Coordinates, resolved.Source
}

func ResolveSupplyChainRouteGeometryDetailed(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	refs []string,
) ResolvedRouteGeometry {
	cleanRefs := models.NormalizeOrderedRouteRefs(refs)
	if len(cleanRefs) < 2 || fragmentsRepo == nil {
		return ResolvedRouteGeometry{}
	}

	fragsByPath, err := fragmentsRepo.BatchGetByCanonicalPaths(ctx, cleanRefs)
	if err != nil {
		return ResolvedRouteGeometry{}
	}
	cleanRefs = normalizeMaritimeOrderedRefs(cleanRefs, fragsByPath)

	if resolved := resolveRouteGeometryProfile(ctx, fragmentsRepo, cleanRefs); len(resolved.Coordinates) >= 2 {
		return resolved
	}
	if resolved := resolveCuratedRouteGeometry(cleanRefs, fragsByPath); len(resolved.Coordinates) >= 2 {
		return resolved
	}
	if resolved := resolveComposedRouteGeometry(ctx, fragmentsRepo, cleanRefs, fragsByPath); len(resolved.Coordinates) >= 2 {
		return resolved
	}
	if resolved := resolvePartialSegmentRouteGeometry(ctx, fragmentsRepo, cleanRefs, fragsByPath); len(resolved.Coordinates) >= 2 {
		return resolved
	}

	out := make([]models.Coordinates, 0, len(cleanRefs)+4)
	source := "fragmentary_waypoint_sequence"
	for _, ref := range cleanRefs {
		frag := fragsByPath[strings.TrimSpace(ref)]
		if frag == nil {
			continue
		}
		if route := extractFragmentRouteCoordinates(frag); len(route) >= 2 {
			oriented := orientRouteCoordinates(out, route)
			out = appendDedupedRouteCoordinates(out, oriented)
			source = "facility_route_overlay"
			continue
		}
		if coord, ok := extractFragmentCoordinate(frag); ok {
			out = appendDedupedRouteCoordinates(out, []models.Coordinates{coord})
		}
	}
	if len(out) < 2 {
		return ResolvedRouteGeometry{}
	}
	return ResolvedRouteGeometry{
		Coordinates: out,
		Source:      source,
	}
}

func resolveComposedRouteGeometry(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	cleanRefs []string,
	fragsByPath map[string]*models.HorizonFragment,
) ResolvedRouteGeometry {
	if len(cleanRefs) < 3 || fragmentsRepo == nil {
		return ResolvedRouteGeometry{}
	}

	out := make([]models.Coordinates, 0, len(cleanRefs)*8)
	flowRefs := make([]string, 0, len(cleanRefs))
	exactSegments := 0
	for idx := 0; idx < len(cleanRefs)-1; idx++ {
		pairRefs := cleanRefs[idx : idx+2]
		resolved := resolveKnownPairRouteGeometry(ctx, fragmentsRepo, pairRefs, fragsByPath)
		if len(resolved.Coordinates) < 2 {
			return ResolvedRouteGeometry{}
		}
		if strings.TrimSpace(resolved.Source) == "fragmentary_waypoint_sequence" {
			return ResolvedRouteGeometry{}
		}
		exactSegments++
		out = appendDedupedRouteCoordinates(out, resolved.Coordinates)
		flowRefs = appendUniqueRouteRefs(flowRefs, resolved.FlowRefs...)
	}
	if len(out) < 2 || exactSegments == 0 {
		return ResolvedRouteGeometry{}
	}
	return ResolvedRouteGeometry{
		Coordinates: out,
		FlowRefs:    flowRefs,
		Source:      "composed_route_segments",
	}
}

func resolvePartialSegmentRouteGeometry(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	cleanRefs []string,
	fragsByPath map[string]*models.HorizonFragment,
) ResolvedRouteGeometry {
	if len(cleanRefs) < 3 || fragmentsRepo == nil {
		return ResolvedRouteGeometry{}
	}

	out := make([]models.Coordinates, 0, len(cleanRefs)*8)
	flowRefs := make([]string, 0, len(cleanRefs))
	exactSegments := 0
	for idx := 0; idx < len(cleanRefs)-1; idx++ {
		pairRefs := cleanRefs[idx : idx+2]
		resolved := resolveKnownPairRouteGeometry(ctx, fragmentsRepo, pairRefs, fragsByPath)
		if len(resolved.Coordinates) < 2 {
			return ResolvedRouteGeometry{}
		}
		if strings.TrimSpace(resolved.Source) != "fragmentary_waypoint_sequence" {
			exactSegments++
			flowRefs = appendUniqueRouteRefs(flowRefs, resolved.FlowRefs...)
		}
		out = appendDedupedRouteCoordinates(out, resolved.Coordinates)
	}
	if len(out) < 2 || exactSegments == 0 {
		return ResolvedRouteGeometry{}
	}
	return ResolvedRouteGeometry{
		Coordinates: out,
		FlowRefs:    flowRefs,
		Source:      "partial_segment_geometry",
	}
}

func resolveKnownPairRouteGeometry(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	pairRefs []string,
	fragsByPath map[string]*models.HorizonFragment,
) ResolvedRouteGeometry {
	pairRefs = models.NormalizeOrderedRouteRefs(pairRefs)
	if len(pairRefs) < 2 {
		return ResolvedRouteGeometry{}
	}
	if resolved := resolveRouteGeometryProfile(ctx, fragmentsRepo, pairRefs); len(resolved.Coordinates) >= 2 {
		return resolved
	}
	if resolved := resolveCuratedRouteGeometry(pairRefs, fragsByPath); len(resolved.Coordinates) >= 2 {
		return resolved
	}

	out := make([]models.Coordinates, 0, 4)
	source := "fragmentary_waypoint_sequence"
	for _, ref := range pairRefs {
		frag := fragsByPath[strings.TrimSpace(ref)]
		if frag == nil {
			continue
		}
		if route := extractFragmentRouteCoordinates(frag); len(route) >= 2 {
			oriented := orientRouteCoordinates(out, route)
			out = appendDedupedRouteCoordinates(out, oriented)
			source = "facility_route_overlay"
			continue
		}
		if coord, ok := extractFragmentCoordinate(frag); ok {
			out = appendDedupedRouteCoordinates(out, []models.Coordinates{coord})
		}
	}
	if len(out) < 2 {
		return ResolvedRouteGeometry{}
	}
	return ResolvedRouteGeometry{
		Coordinates: out,
		Source:      source,
	}
}

func resolveRouteGeometryProfile(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	cleanRefs []string,
) ResolvedRouteGeometry {
	if fragmentsRepo == nil || len(cleanRefs) < 2 {
		return ResolvedRouteGeometry{}
	}
	db := fragmentsRepo.Firestore()
	if db == nil {
		return ResolvedRouteGeometry{}
	}
	profilesRepo := repo.NewRouteGeometryProfilesRepository(db)
	profileKey := models.BuildRouteGeometryProfileKey(cleanRefs)
	if profileKey == "" {
		return ResolvedRouteGeometry{}
	}
	tenantID := "global"
	for _, ref := range cleanRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if frag, err := fragmentsRepo.GetByCanonicalPath(ctx, ref); err == nil && frag != nil {
			if candidateTenant := strings.TrimSpace(frag.TenantID); candidateTenant != "" {
				tenantID = candidateTenant
				break
			}
		}
	}
	profile, err := profilesRepo.GetByProfileKey(ctx, tenantID, profileKey)
	if err != nil || profile == nil || len(profile.Coordinates) < 2 {
		return ResolvedRouteGeometry{}
	}
	coords := orientRouteLineToRefs(profile.Coordinates, cleanRefs, nil)
	return ResolvedRouteGeometry{
		Coordinates: coords,
		FlowRefs:    append([]string(nil), profile.FlowRefs...),
		Source:      strings.TrimSpace(firstNonEmpty(profile.Source, "route_geometry_profile")),
	}
}

func normalizeRouteRefs(refs []string) []string {
	return models.NormalizeOrderedRouteRefs(refs)
}

func extractFragmentCoordinate(frag *models.HorizonFragment) (models.Coordinates, bool) {
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

func extractFragmentRouteCoordinates(frag *models.HorizonFragment) []models.Coordinates {
	if frag == nil {
		return nil
	}
	wm := frag.EffectiveHorizonMonitor()
	if wm == nil || len(wm.Route) == 0 {
		return nil
	}
	out := make([]models.Coordinates, 0, len(wm.Route))
	for _, coord := range wm.Route {
		if !models.IsRealGeoCoordinate(coord.Lat, coord.Lon) {
			continue
		}
		out = appendDedupedRouteCoordinates(out, []models.Coordinates{coord})
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func orientRouteCoordinates(existing []models.Coordinates, route []models.Coordinates) []models.Coordinates {
	if len(route) < 2 {
		return route
	}
	if len(existing) == 0 {
		return route
	}
	last := existing[len(existing)-1]
	startDistance := routeDistanceKm(last, route[0])
	endDistance := routeDistanceKm(last, route[len(route)-1])
	if endDistance < startDistance {
		return reverseCoordinates(route)
	}
	return route
}

func orientRouteLineToRefs(route []models.Coordinates, refs []string, fragsByPath map[string]*models.HorizonFragment) []models.Coordinates {
	if len(route) < 2 {
		return route
	}
	var start models.Coordinates
	var end models.Coordinates
	startOK := false
	endOK := false
	if fragsByPath != nil {
		if frag := fragsByPath[strings.TrimSpace(refs[0])]; frag != nil {
			start, startOK = extractFragmentCoordinate(frag)
		}
		if frag := fragsByPath[strings.TrimSpace(refs[len(refs)-1])]; frag != nil {
			end, endOK = extractFragmentCoordinate(frag)
		}
	}
	if !startOK || !endOK {
		return route
	}
	direct := routeDistanceKm(start, route[0]) + routeDistanceKm(end, route[len(route)-1])
	reverse := routeDistanceKm(start, route[len(route)-1]) + routeDistanceKm(end, route[0])
	if reverse < direct {
		return reverseCoordinates(route)
	}
	return route
}

func reverseCoordinates(route []models.Coordinates) []models.Coordinates {
	out := make([]models.Coordinates, 0, len(route))
	for idx := len(route) - 1; idx >= 0; idx-- {
		out = append(out, route[idx])
	}
	return out
}

func appendDedupedRouteCoordinates(base []models.Coordinates, extra []models.Coordinates) []models.Coordinates {
	for _, coord := range extra {
		if !models.IsRealGeoCoordinate(coord.Lat, coord.Lon) {
			continue
		}
		if len(base) > 0 {
			last := base[len(base)-1]
			if math.Abs(last.Lat-coord.Lat) < 1e-7 && math.Abs(last.Lon-coord.Lon) < 1e-7 {
				continue
			}
		}
		base = append(base, coord)
	}
	return base
}

func maybeAttachEndpoint(route []models.Coordinates, endpoint models.Coordinates, thresholdKm float64, prepend bool) []models.Coordinates {
	if len(route) == 0 || !models.IsRealGeoCoordinate(endpoint.Lat, endpoint.Lon) {
		return route
	}
	probe := route[0]
	if !prepend {
		probe = route[len(route)-1]
	}
	if routeDistanceKm(endpoint, probe) > thresholdKm {
		return route
	}
	if prepend {
		return appendDedupedRouteCoordinates([]models.Coordinates{endpoint}, route)
	}
	return appendDedupedRouteCoordinates(route, []models.Coordinates{endpoint})
}

func routeDistanceKm(a models.Coordinates, b models.Coordinates) float64 {
	const earthRadiusKm = 6371.0
	toRadians := func(value float64) float64 {
		return value * math.Pi / 180
	}
	dLat := toRadians(b.Lat - a.Lat)
	dLon := toRadians(b.Lon - a.Lon)
	lat1 := toRadians(a.Lat)
	lat2 := toRadians(b.Lat)
	term := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(term), math.Sqrt(1-term))
}

func appendUniqueRouteRefs(base []string, extra ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range base {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range extra {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
