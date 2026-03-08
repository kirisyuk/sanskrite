package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type kv struct {
	Key   string
	Count int
}

type hub struct {
	ID   string
	Path string
	Type string
	Deg  int
	In   int
	Out  int
}

type fragLite struct {
	ID             string
	Path           string
	Type           string
	MarketDomain   string
	MarketVertical string
	GeoFeatureType string
	GovType        string
}

type typeDegree struct {
	Nodes int
	Deg   int
	In    int
	Out   int
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	topN := flag.Int("top", 16, "top N items to print")
	flag.Parse()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()
	db := fs.Client()

	fragTypes := map[string]int{}
	fragPrefixes := map[string]int{}
	marketDomain := map[string]int{}
	geoTypes := map[string]int{}
	govTypes := map[string]int{}
	coverage := map[string]int{}
	fragByID := map[string]*fragLite{}

	fragIter := db.Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	fragCount := 0
	for {
		doc, err := fragIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var f h.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			continue
		}
		fragCount++
		inc(fragTypes, string(f.Type))
		inc(fragPrefixes, prefixOf(f.CanonicalPath))
		if strings.TrimSpace(f.MarketDomain) != "" {
			inc(marketDomain, f.MarketDomain)
			coverage["market_domain"]++
		}
		if strings.TrimSpace(f.MarketVertical) != "" {
			coverage["market_vertical"]++
		}
		if strings.TrimSpace(f.GeoFeatureType) != "" {
			inc(geoTypes, f.GeoFeatureType)
			coverage["geo_feature_type"]++
		}
		if strings.TrimSpace(f.GovType) != "" {
			inc(govTypes, f.GovType)
			coverage["gov_type"]++
		}
		if len(f.MarketSegments) > 0 {
			coverage["market_segments"]++
		}
		if len(f.IndustryPaths) > 0 {
			coverage["industry_paths"]++
		}
		if strings.TrimSpace(f.Description) != "" {
			coverage["description"]++
		}
		if f.HasCoordinates || (f.Coordinates != nil && (f.Coordinates.Lat != 0 || f.Coordinates.Lon != 0)) {
			coverage["coordinates"]++
		}
		fragByID[f.ID] = &fragLite{
			ID:             f.ID,
			Path:           f.CanonicalPath,
			Type:           string(f.Type),
			MarketDomain:   f.MarketDomain,
			MarketVertical: f.MarketVertical,
			GeoFeatureType: f.GeoFeatureType,
			GovType:        f.GovType,
		}
	}

	edgeTypes := map[string]int{}
	edgeFacets := map[string]int{}
	edgeFamilies := map[string]int{}
	qualifierCov := map[string]int{}
	degree := map[string]*hub{}
	typeTriples := map[string]int{}
	businessEdges := 0
	businessQualCov := map[string]int{}
	edgeCount := 0

	edgeIter := db.Collection("horizon_dependency_edges").Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	for {
		doc, err := edgeIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var e h.DependencyEdge
		if err := doc.DataTo(&e); err != nil {
			continue
		}
		edgeCount++
		inc(edgeTypes, string(e.Type))
		inc(edgeFacets, string(e.GetFacet()))
		family := classifyEdgeFamily(fragByID[e.SourceFragmentID], &e)
		inc(edgeFamilies, family)
		inc(typeTriples, fragmentTypeOf(fragByID[e.SourceFragmentID])+" -> "+string(e.Type)+" -> "+fragmentTypeOf(fragByID[e.TargetFragmentID]))
		if e.GetThroughputShare() > 0 {
			qualifierCov["throughput_share"]++
		}
		if e.GetReroutePenalty() > 0 {
			qualifierCov["reroute_penalty"]++
		}
		if e.GetCapacityConstraint() > 0 {
			qualifierCov["capacity_constraint"]++
		}
		if e.GetSubstitutability() > 0 {
			qualifierCov["substitutability"]++
		}
		if hasTransportVia(&e) {
			qualifierCov["transport_via"]++
		}
		if strings.TrimSpace(string(e.GetFacet())) != "" {
			qualifierCov["facet"]++
		}
		if family == "energy_supply_chain" || family == "chokepoint_logistics" || family == "facility_dependency" || family == "market_exposure" {
			businessEdges++
			if e.GetThroughputShare() > 0 {
				businessQualCov["throughput_share"]++
			}
			if e.GetReroutePenalty() > 0 {
				businessQualCov["reroute_penalty"]++
			}
			if e.GetCapacityConstraint() > 0 {
				businessQualCov["capacity_constraint"]++
			}
			if e.GetSubstitutability() > 0 {
				businessQualCov["substitutability"]++
			}
			if strings.TrimSpace(string(e.GetFacet())) != "" {
				businessQualCov["facet"]++
			}
		}
		bumpDegree(degree, fragByID, e.SourceFragmentID, true)
		bumpDegree(degree, fragByID, e.TargetFragmentID, false)
	}

	fmt.Printf("tenant=%s project=%s\nfragments=%d edges=%d\n\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), fragCount, edgeCount)
	printTop("fragment_types", topCounts(fragTypes, *topN))
	printTop("canonical_prefixes", topCounts(fragPrefixes, *topN))
	printTop("edge_types", topCounts(edgeTypes, *topN))
	printTop("edge_families", topCounts(edgeFamilies, *topN))
	printTop("edge_facets", topCounts(edgeFacets, *topN))

	fmt.Println("metadata_coverage:")
	for _, key := range []string{"coordinates", "description", "industry_paths", "market_segments", "market_domain", "market_vertical", "geo_feature_type", "gov_type"} {
		fmt.Printf("  %s: %d (%.1f%%)\n", key, coverage[key], pct(coverage[key], fragCount))
	}
	fmt.Println()

	fmt.Println("qualifier_coverage_all_edges:")
	for _, key := range []string{"facet", "throughput_share", "reroute_penalty", "capacity_constraint", "substitutability", "transport_via"} {
		fmt.Printf("  %s: %d (%.1f%%)\n", key, qualifierCov[key], pct(qualifierCov[key], edgeCount))
	}
	fmt.Println()

	fmt.Printf("business_edges: %d (%.1f%% of all edges)\n", businessEdges, pct(businessEdges, edgeCount))
	fmt.Println("qualifier_coverage_business_edges:")
	for _, key := range []string{"facet", "throughput_share", "reroute_penalty", "capacity_constraint", "substitutability"} {
		fmt.Printf("  %s: %d (%.1f%%)\n", key, businessQualCov[key], pct(businessQualCov[key], businessEdges))
	}
	fmt.Println()

	printTop("market_domains", topCounts(marketDomain, *topN))
	printTop("geo_feature_types", topCounts(geoTypes, *topN))
	printTop("gov_types", topCounts(govTypes, *topN))
	printTop("edge_type_triples", topCounts(typeTriples, *topN))

	typeDegreeStats := map[string]*typeDegree{}
	for id, hb := range degree {
		ft := fragmentTypeOf(fragByID[id])
		stats := typeDegreeStats[ft]
		if stats == nil {
			stats = &typeDegree{}
			typeDegreeStats[ft] = stats
		}
		stats.Nodes++
		stats.Deg += hb.Deg
		stats.In += hb.In
		stats.Out += hb.Out
	}
	typeDegreeRows := make([]kv, 0, len(typeDegreeStats))
	for ft, stats := range typeDegreeStats {
		avg := 0.0
		if stats.Nodes > 0 {
			avg = float64(stats.Deg) / float64(stats.Nodes)
		}
		typeDegreeRows = append(typeDegreeRows, kv{
			Key:   fmt.Sprintf("%s active_nodes=%d avg_deg=%.1f avg_in=%.1f avg_out=%.1f", ft, stats.Nodes, avg, avgPart(stats.In, stats.Nodes), avgPart(stats.Out, stats.Nodes)),
			Count: stats.Deg,
		})
	}
	sort.Slice(typeDegreeRows, func(i, j int) bool {
		if typeDegreeRows[i].Count == typeDegreeRows[j].Count {
			return typeDegreeRows[i].Key < typeDegreeRows[j].Key
		}
		return typeDegreeRows[i].Count > typeDegreeRows[j].Count
	})
	printTop("active_degree_by_type", limitRows(typeDegreeRows, *topN))
	fmt.Printf("isolated_or_unseen_nodes: %d (%.1f%% of fragments)\n\n", fragCount-len(degree), pct(fragCount-len(degree), fragCount))

	printHubs("top_hubs_overall", topHubs(degree, func(h hub) bool { return true }, *topN))
	printHubs("top_hubs_non_loc", topHubs(degree, func(h hub) bool { return !strings.HasPrefix(strings.TrimSpace(h.Path), "/loc/") }, *topN))
	printHubs("top_hubs_businessish", topHubs(degree, func(h hub) bool {
		p := strings.TrimSpace(h.Path)
		return strings.HasPrefix(p, "/fac/") || strings.HasPrefix(p, "/market/") || strings.HasPrefix(p, "/org/") || strings.HasPrefix(p, "/gov/")
	}, *topN))
}

func bumpDegree(degree map[string]*hub, frags map[string]*fragLite, id string, outgoing bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	hb := degree[id]
	if hb == nil {
		hb = &hub{ID: id}
		if fl := frags[id]; fl != nil {
			hb.Path = fl.Path
			hb.Type = fl.Type
		}
		degree[id] = hb
	}
	hb.Deg++
	if outgoing {
		hb.Out++
	} else {
		hb.In++
	}
}

func classifyEdgeFamily(from *fragLite, edge *h.DependencyEdge) string {
	if edge == nil {
		return "direct_dependency"
	}
	facet := string(edge.GetFacet())
	text := strings.ToLower(strings.Join([]string{string(edge.Type), facet}, " "))
	fromPath := ""
	fromGeo := ""
	fromDomain := ""
	fromVertical := ""
	if from != nil {
		fromPath = strings.ToLower(strings.TrimSpace(from.Path))
		fromGeo = strings.ToLower(strings.TrimSpace(from.GeoFeatureType))
		fromDomain = strings.ToLower(strings.TrimSpace(from.MarketDomain))
		fromVertical = strings.ToLower(strings.TrimSpace(from.MarketVertical))
	}
	text += " " + fromPath + " " + fromGeo + " " + fromDomain + " " + fromVertical
	switch edge.Type {
	case h.DependencyTypeRegulates, h.DependencyTypeAppliesTo, h.DependencyTypeImposesCondition, h.DependencyTypeControlsAccess, h.DependencyTypeAdministers, h.DependencyTypeGatekeepsDecision, h.DependencyTypeUsesMechanism:
		return "regulatory_control"
	case h.DependencyTypeSuppliesTo, h.DependencyTypeTransformsInto, h.DependencyTypeExtractsFrom, h.DependencyTypeProduces:
		if strings.Contains(text, "lng") || strings.Contains(text, "gas") || strings.Contains(text, "pipeline") || strings.Contains(text, "refinery") {
			return "energy_supply_chain"
		}
		return "facility_dependency"
	case h.DependencyTypeParticipatesIn, h.DependencyTypeHasMarketSegment, h.DependencyTypeHasSubmarket, h.DependencyTypeDominates:
		return "market_exposure"
	case h.DependencyTypeOwns, h.DependencyTypeControls, h.DependencyTypeManages, h.DependencyTypeInvestsIn:
		return "organizational_control"
	case h.DependencyTypeLocatedIn, h.DependencyTypeOperatesIn, h.DependencyTypeAffects, h.DependencyTypeTriggeredBy, h.DependencyTypeInGeoScope:
		if fromGeo == "strait" || fromGeo == "canal" || fromGeo == "pass" || fromGeo == "pipeline-corridor" {
			return "chokepoint_logistics"
		}
		return "geo_operational_exposure"
	case h.DependencyTypeDependsOn:
		if facet == string(h.FacetLogistics) || facet == string(h.FacetDistribution) || strings.Contains(text, "port") || strings.Contains(text, "shipping") || strings.Contains(text, "strait") || strings.Contains(text, "canal") {
			return "chokepoint_logistics"
		}
		if facet == string(h.FacetSupply) || facet == string(h.FacetInfrastructure) || strings.Contains(text, "pipeline") || strings.Contains(text, "terminal") || strings.Contains(text, "refinery") || strings.Contains(text, "lng") || strings.Contains(text, "gas") || strings.Contains(text, "power") {
			return "energy_supply_chain"
		}
		return "facility_dependency"
	}
	if strings.Contains(text, "/market/") {
		return "market_exposure"
	}
	if strings.Contains(text, "/fac/") {
		return "facility_dependency"
	}
	return "direct_dependency"
}

func inc(m map[string]int, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "(empty)"
	}
	m[key]++
}

func prefixOf(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "(none)"
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return "(none)"
	}
	return "/" + parts[0]
}

func pct(v, total int) float64 {
	if total <= 0 {
		return 0
	}
	return 100 * float64(v) / float64(total)
}

func topCounts(in map[string]int, limit int) []kv {
	out := make([]kv, 0, len(in))
	for key, count := range in {
		out = append(out, kv{Key: key, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func topHubs(in map[string]*hub, keep func(hub) bool, limit int) []hub {
	out := make([]hub, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		if keep != nil && !keep(*item) {
			continue
		}
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Deg == out[j].Deg {
			return out[i].Path < out[j].Path
		}
		return out[i].Deg > out[j].Deg
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func printTop(title string, items []kv) {
	fmt.Println(title + ":")
	for _, item := range items {
		fmt.Printf("  %s: %d\n", item.Key, item.Count)
	}
	fmt.Println()
}

func printHubs(title string, items []hub) {
	fmt.Println(title + ":")
	for i, item := range items {
		fmt.Printf("  %2d. deg=%d in=%d out=%d type=%s path=%s\n", i+1, item.Deg, item.In, item.Out, item.Type, item.Path)
	}
	fmt.Println()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func fragmentTypeOf(fl *fragLite) string {
	if fl == nil || strings.TrimSpace(fl.Type) == "" {
		return "(unknown)"
	}
	return strings.TrimSpace(fl.Type)
}

func avgPart(sum, count int) float64 {
	if count <= 0 {
		return 0
	}
	return float64(sum) / float64(count)
}

func limitRows[T any](rows []T, limit int) []T {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return rows[:limit]
}

func hasTransportVia(edge *h.DependencyEdge) bool {
	if edge == nil || edge.Qualifiers == nil {
		return false
	}
	raw, ok := edge.Qualifiers[h.QualifierTransportVia]
	if !ok || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case []string:
		return len(v) > 0
	case []interface{}:
		return len(v) > 0
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return false
	}
}
