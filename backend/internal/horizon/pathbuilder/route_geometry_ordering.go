package pathbuilder

import (
	"context"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var knownMaritimeWaypointRanks = map[string]int{
	"/fac/nl/port-of-rotterdam":          10,
	"/fac/fr/port-of-marseille":          20,
	"/fac/it/port-of-genoa":              24,
	"/fac/eg/suez-terminal":              30,
	"/fac/sea/bab-el-mandeb":             35,
	"/fac/sea/strait-of-hormuz-terminal": 45,
	"/fac/om/sohar-port":                 48,
	"/fac/ae/jebel-ali-port":             50,
	"/fac/int/strait-of-malacca":         70,
	"/fac/sg/port-of-singapore":          75,
	"/fac/id/port-of-belawan":            80,
	"/fac/id/port-of-tanjung-priok":      84,
	"/fac/vn/port-of-ho-chi-minh-city":   88,
}

func NormalizeLegTransportViaRefs(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	supplierRef string,
	viaRefs []string,
	customerRef string,
) []string {
	normalized := models.NormalizeTransportViaRefs(viaRefs)
	if len(normalized) < 2 || fragmentsRepo == nil {
		return normalized
	}
	ordered := append(append([]string{strings.TrimSpace(supplierRef)}, normalized...), strings.TrimSpace(customerRef))
	ordered = models.NormalizeOrderedRouteRefs(ordered)
	fragsByPath, err := fragmentsRepo.BatchGetByCanonicalPaths(ctx, ordered)
	if err != nil {
		return normalized
	}
	reordered := normalizeMaritimeOrderedRefs(ordered, fragsByPath)
	if len(reordered) < 2 {
		return normalized
	}
	return append([]string(nil), reordered[1:len(reordered)-1]...)
}

func normalizeMaritimeOrderedRefs(cleanRefs []string, fragsByPath map[string]*models.HorizonFragment) []string {
	if len(cleanRefs) < 4 {
		return cleanRefs
	}
	interior := append([]string(nil), cleanRefs[1:len(cleanRefs)-1]...)
	ranked := make([]maritimeRankedRef, 0, len(interior))
	for idx, ref := range interior {
		rank, ok := knownMaritimeWaypointRanks[strings.TrimSpace(ref)]
		if !ok {
			return cleanRefs
		}
		ranked = append(ranked, maritimeRankedRef{Ref: ref, Rank: rank, Index: idx})
	}
	ascending, ok := inferMaritimeOrderingAscending(cleanRefs[0], cleanRefs[len(cleanRefs)-1], fragsByPath)
	if !ok {
		return cleanRefs
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Rank == ranked[j].Rank {
			return ranked[i].Index < ranked[j].Index
		}
		if ascending {
			return ranked[i].Rank < ranked[j].Rank
		}
		return ranked[i].Rank > ranked[j].Rank
	})
	out := make([]string, 0, len(cleanRefs))
	out = append(out, cleanRefs[0])
	for _, item := range ranked {
		out = append(out, item.Ref)
	}
	out = append(out, cleanRefs[len(cleanRefs)-1])
	return out
}

type maritimeRankedRef struct {
	Ref   string
	Rank  int
	Index int
}

func inferMaritimeOrderingAscending(startRef, endRef string, fragsByPath map[string]*models.HorizonFragment) (bool, bool) {
	if startRank, ok := knownMaritimeWaypointRanks[strings.TrimSpace(startRef)]; ok {
		if endRank, ok := knownMaritimeWaypointRanks[strings.TrimSpace(endRef)]; ok {
			return startRank < endRank, true
		}
	}
	start, startOK := resolveRouteEndpoint(startRef, fragsByPath)
	end, endOK := resolveRouteEndpoint(endRef, fragsByPath)
	if !startOK || !endOK {
		return false, false
	}
	if absRouteDelta(start.Lon-end.Lon) < 10 {
		return false, false
	}
	return start.Lon < end.Lon, true
}
