package internalknowledge

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

type PreparedSubstrate struct {
	CorpusID      string
	Lens          *hmodels.Lens
	Question      string
	ScopePaths    []string
	Fragments     map[string]*hmodels.HorizonFragment
	Artifacts     map[string]*hmodels.MaterializedArtifact
	Routes        []RouteSummary
	Objects       []ObjectSummary
	Relations     []RelationSummary
	Products      []ProductSummary
	Gaps          []GapSummary
	Analytical    []*hmodels.AnalyticalPrimitive
	Aggregates    []*hmodels.AnalyticalAggregate
	SignalLanding []*hmodels.SignalLandingSummary
	Docs          []PreparedCorpusDoc
}

type PreparedCorpusDoc struct {
	ID          string
	Title       string
	Kind        string
	Temperature string
	Content     string
	StructData  map[string]any
}

type RouteSummary struct {
	ArtifactID           string
	ArtifactCanonicalKey string
	LegIndex             int
	SupplierRef          string
	CustomerRef          string
	ProductRef           string
	SupplyLinkRef        string
	TransportViaRefs     []string
	Mode                 string
	VesselType           string
	FlowRefs             []string
	GeoRefs              []string
	ZoneRefs             []string
	AffectedZoneRefs     []string
	RouteGeometrySource  string
	RouteCoordinatesN    int
}

type ProductSummary struct {
	ProductRef string
	UsedBy     int
}

type ObjectSummary struct {
	CanonicalPath string
	Type          string
	DisplayName   string
}

type RelationSummary struct {
	EdgeID      string
	EdgeType    string
	SourcePath  string
	SourceLabel string
	TargetPath  string
	TargetLabel string
	Score       float64
}

type GapSummary struct {
	Code    string
	Details string
}

type corridorSummary struct {
	ID               string
	SupplierRef      string
	CustomerRef      string
	ProductRef       string
	TransportViaRefs []string
	Modes            []string
	ArtifactKeys     []string
	RouteCount       int
}

type Builder struct {
	fragments              *hrepo.HorizonFragmentsRepository
	artifacts              *hrepo.MaterializedArtifactsRepository
	lensArtifacts          *hrepo.LensArtifactsRepository
	analyticalPrimitives   *hrepo.AnalyticalPrimitivesRepository
	analyticalAggregates   *hrepo.AnalyticalAggregatesRepository
	signalLandingSummaries *hrepo.SignalLandingSummariesRepository
}

const (
	PreparedTemperatureHot  = "hot"
	PreparedTemperatureWarm = "warm"
	PreparedTemperatureFull = "full"
)

func NewBuilder(
	fragments *hrepo.HorizonFragmentsRepository,
	artifacts *hrepo.MaterializedArtifactsRepository,
	lensArtifacts *hrepo.LensArtifactsRepository,
	analyticalPrimitives *hrepo.AnalyticalPrimitivesRepository,
	analyticalAggregates *hrepo.AnalyticalAggregatesRepository,
	signalLandingSummaries *hrepo.SignalLandingSummariesRepository,
) *Builder {
	return &Builder{
		fragments:              fragments,
		artifacts:              artifacts,
		lensArtifacts:          lensArtifacts,
		analyticalPrimitives:   analyticalPrimitives,
		analyticalAggregates:   analyticalAggregates,
		signalLandingSummaries: signalLandingSummaries,
	}
}

func (b *Builder) BuildForLens(ctx context.Context, lens *hmodels.Lens, question string) (*PreparedSubstrate, error) {
	if lens == nil {
		return nil, fmt.Errorf("lens is nil")
	}
	if b == nil || b.fragments == nil || b.artifacts == nil || b.lensArtifacts == nil {
		return nil, fmt.Errorf("prepared substrate builder is not fully configured")
	}

	scopePaths := preparedScopePaths(lens)
	artifacts, err := b.artifacts.ListByLens(ctx, strings.TrimSpace(lens.ID), 512)
	if err != nil {
		return nil, err
	}
	lensArtifacts, err := b.lensArtifacts.ListByLens(ctx, strings.TrimSpace(lens.ID), 2000)
	if err != nil {
		return nil, err
	}

	artifactMap := make(map[string]*hmodels.MaterializedArtifact, len(artifacts))
	pathRefs := make(map[string]struct{}, len(scopePaths)+128)
	analyticalScopeRefs := make(map[string]struct{}, len(scopePaths)+len(artifacts)+128)
	for _, path := range scopePaths {
		if isPreparedCanonicalPath(path) {
			pathRefs[path] = struct{}{}
		}
		addNonEmptyPath(analyticalScopeRefs, path)
	}
	for _, lensArtifact := range lensArtifacts {
		if lensArtifact == nil {
			continue
		}
		addNonEmptyPath(pathRefs, lensArtifact.CanonicalPath)
		addNonEmptyPath(pathRefs, lensArtifact.SourcePath)
		addNonEmptyPath(pathRefs, lensArtifact.TargetPath)
		addNonEmptyPath(analyticalScopeRefs, lensArtifact.CanonicalPath)
		addNonEmptyPath(analyticalScopeRefs, lensArtifact.SourcePath)
		addNonEmptyPath(analyticalScopeRefs, lensArtifact.TargetPath)
	}
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
			continue
		}
		artifactMap[artifact.ID] = artifact
		addNonEmptyPath(analyticalScopeRefs, artifact.CanonicalKey)
		for _, ref := range compactUnique(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			addNonEmptyPath(pathRefs, ref)
			addNonEmptyPath(analyticalScopeRefs, ref)
		}
		parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			for _, ref := range compactUnique(append([]string{leg.SupplierRef, leg.CustomerRef, leg.ProductRef, leg.SupplyLinkCanonicalPath}, leg.TransportViaRefs...)) {
				addNonEmptyPath(pathRefs, ref)
				addNonEmptyPath(analyticalScopeRefs, ref)
			}
		}
	}

	fragmentsByPath, err := b.fragments.BatchGetByCanonicalPaths(ctx, mapKeys(pathRefs))
	if err != nil {
		return nil, err
	}
	routes := summarizeRoutes(artifactMap)
	relations := summarizeLensArtifactRelations(lensArtifacts)
	objectScores := objectScoresForPreparedScope(mapKeys(pathRefs), routes, relations, artifactMap)
	objects := summarizePreparedObjects(fragmentsByPath, objectScores, 120)
	products := summarizeProducts(artifactMap)
	analyticalItems := []*hmodels.AnalyticalPrimitive(nil)
	if b.analyticalPrimitives != nil {
		analyticalItems, _ = b.analyticalPrimitives.ListByScopeRefs(ctx, strings.TrimSpace(lens.TenantID), mapKeys(analyticalScopeRefs), 240)
	}
	aggregateItems := []*hmodels.AnalyticalAggregate(nil)
	if b.analyticalAggregates != nil {
		if b.analyticalPrimitives != nil {
			if lensAggregate, err := analytical.EnsureLensAggregate(ctx, b.analyticalPrimitives, b.analyticalAggregates, nil, lens, mapKeys(analyticalScopeRefs), time.Now().UTC()); err == nil && lensAggregate != nil {
				aggregateItems = append(aggregateItems, lensAggregate)
			}
		}
		if stored, _ := b.analyticalAggregates.ListByLens(ctx, strings.TrimSpace(lens.TenantID), strings.TrimSpace(lens.ID), 40); len(stored) > 0 {
			seen := make(map[string]struct{}, len(aggregateItems))
			for _, item := range aggregateItems {
				if item != nil && strings.TrimSpace(item.ID) != "" {
					seen[strings.TrimSpace(item.ID)] = struct{}{}
				}
			}
			for _, item := range stored {
				if item == nil || strings.TrimSpace(item.ID) == "" {
					continue
				}
				if _, ok := seen[strings.TrimSpace(item.ID)]; ok {
					continue
				}
				seen[strings.TrimSpace(item.ID)] = struct{}{}
				aggregateItems = append(aggregateItems, item)
			}
		}
		if stored, _ := b.analyticalAggregates.ListByScopeRefs(ctx, strings.TrimSpace(lens.TenantID), mapKeys(analyticalScopeRefs), 80); len(stored) > 0 {
			seen := make(map[string]struct{}, len(aggregateItems))
			for _, item := range aggregateItems {
				if item != nil && strings.TrimSpace(item.ID) != "" {
					seen[strings.TrimSpace(item.ID)] = struct{}{}
				}
			}
			for _, item := range stored {
				if item == nil || strings.TrimSpace(item.ID) == "" {
					continue
				}
				if _, ok := seen[strings.TrimSpace(item.ID)]; ok {
					continue
				}
				seen[strings.TrimSpace(item.ID)] = struct{}{}
				aggregateItems = append(aggregateItems, item)
			}
		}
	}
	landingSummaries := []*hmodels.SignalLandingSummary(nil)
	if b.signalLandingSummaries != nil {
		landingSummaries, _ = b.signalLandingSummaries.ListByScopeRefs(ctx, strings.TrimSpace(lens.TenantID), mapKeys(analyticalScopeRefs), 80)
	}

	sub := &PreparedSubstrate{
		CorpusID:      resolveCorpusID(lens, question),
		Lens:          lens,
		Question:      resolvePreparedQuestion(lens, question),
		ScopePaths:    scopePaths,
		Fragments:     fragmentsByPath,
		Artifacts:     artifactMap,
		Routes:        routes,
		Objects:       objects,
		Relations:     relations,
		Products:      products,
		Gaps:          detectPreparedGaps(artifactMap, relations),
		Analytical:    analyticalItems,
		Aggregates:    aggregateItems,
		SignalLanding: landingSummaries,
	}
	sub.Docs = buildPreparedCorpusDocs(sub)
	return sub, nil
}

func PreparedScopeHash(sub *PreparedSubstrate) string {
	if sub == nil {
		return ""
	}
	scopeParts := compactUnique(append([]string{}, sub.ScopePaths...))
	return shortHash(strings.Join(scopeParts, "|"))
}

func PreparedSubstrateHash(sub *PreparedSubstrate) string {
	if sub == nil {
		return ""
	}
	rows := make([]string, 0, len(sub.Docs))
	for _, doc := range sub.Docs {
		rows = append(rows, doc.ID+"|"+doc.Kind+"|"+doc.Title+"|"+doc.Content)
	}
	sort.Strings(rows)
	return shortHash(strings.Join(rows, "\n"))
}

func PreparedSubstrateHashForTemperature(sub *PreparedSubstrate, temperature string) string {
	if sub == nil {
		return ""
	}
	rows := make([]string, 0, len(sub.Docs))
	for _, doc := range FilterPreparedDocsUpToTemperature(sub.Docs, temperature) {
		rows = append(rows, doc.ID+"|"+doc.Kind+"|"+doc.Temperature+"|"+doc.Title+"|"+doc.Content)
	}
	sort.Strings(rows)
	return shortHash(strings.Join(rows, "\n"))
}

func FilterPreparedDocsUpToTemperature(docs []PreparedCorpusDoc, temperature string) []PreparedCorpusDoc {
	if len(docs) == 0 {
		return nil
	}
	maxRank := preparedTemperatureRank(temperature)
	out := make([]PreparedCorpusDoc, 0, len(docs))
	for _, doc := range docs {
		if preparedTemperatureRank(doc.Temperature) <= maxRank {
			out = append(out, doc)
		}
	}
	return out
}

func preparedTemperatureRank(temperature string) int {
	switch strings.ToLower(strings.TrimSpace(temperature)) {
	case PreparedTemperatureHot:
		return 1
	case PreparedTemperatureWarm:
		return 2
	case PreparedTemperatureFull:
		return 3
	default:
		return 3
	}
}

func preparedScopePaths(lens *hmodels.Lens) []string {
	paths := workscope.LensWorkbenchScopePaths(lens)
	filtered := make([]string, 0, len(paths))
	for _, path := range compactUnique(paths) {
		if isPreparedCanonicalPath(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func isPreparedCanonicalPath(path string) bool {
	path = strings.TrimSpace(path)
	switch {
	case path == "":
		return false
	case strings.HasPrefix(path, hmodels.DomainScopePrefix):
		return false
	case strings.HasPrefix(path, hmodels.QueryScopePrefix):
		return false
	case strings.HasPrefix(path, hmodels.ArtifactScopePrefix):
		return false
	case strings.HasPrefix(path, "/"):
		return true
	default:
		return false
	}
}

func resolveCorpusID(lens *hmodels.Lens, question string) string {
	if lens == nil {
		return "internal-knowledge-" + shortHash(strings.TrimSpace(question))
	}
	base := strings.ToLower(strings.TrimSpace(lens.Name))
	base = strings.NewReplacer(" ", "-", "/", "-", "_", "-", ":", "-", ",", "-", ".", "-", "(", "", ")", "").Replace(base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "lens"
	}
	return base + "-" + shortHash(strings.TrimSpace(lens.ID))
}

func resolvePreparedQuestion(lens *hmodels.Lens, question string) string {
	if strings.TrimSpace(question) != "" {
		return strings.TrimSpace(question)
	}
	if lens != nil {
		if strings.TrimSpace(lens.OriginalQuery) != "" {
			return strings.TrimSpace(lens.OriginalQuery)
		}
		if strings.TrimSpace(lens.Name) != "" {
			return strings.TrimSpace(lens.Name)
		}
	}
	return "internal knowledge answer"
}

func summarizeLensArtifactRelations(items []*hmodels.LensArtifact) []RelationSummary {
	type key struct {
		source string
		edge   string
		target string
	}
	best := map[key]RelationSummary{}
	for _, item := range items {
		if item == nil || item.Type != hmodels.LensArtifactTypeEdge {
			continue
		}
		k := key{
			source: strings.TrimSpace(item.SourcePath),
			edge:   strings.TrimSpace(item.EdgeType),
			target: strings.TrimSpace(item.TargetPath),
		}
		prev := best[k]
		prev.EdgeID = firstNonEmpty(prev.EdgeID, strings.TrimSpace(item.EdgeID))
		prev.EdgeType = k.edge
		prev.SourcePath = k.source
		prev.TargetPath = k.target
		prev.SourceLabel = firstNonEmpty(prev.SourceLabel, k.source)
		prev.TargetLabel = firstNonEmpty(prev.TargetLabel, k.target)
		prev.Score++
		best[k] = prev
	}
	out := make([]RelationSummary, 0, len(best))
	for _, relation := range best {
		out = append(out, relation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			if out[i].EdgeType == out[j].EdgeType {
				if out[i].SourcePath == out[j].SourcePath {
					return out[i].TargetPath < out[j].TargetPath
				}
				return out[i].SourcePath < out[j].SourcePath
			}
			return out[i].EdgeType < out[j].EdgeType
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func objectScoresForPreparedScope(scopePaths []string, routes []RouteSummary, relations []RelationSummary, artifacts map[string]*hmodels.MaterializedArtifact) map[string]int {
	scores := map[string]int{}
	for _, path := range compactUnique(scopePaths) {
		scores[path] += 10
	}
	for _, route := range routes {
		for _, path := range compactUnique(append([]string{route.SupplierRef, route.CustomerRef, route.ProductRef, route.SupplyLinkRef}, append(route.TransportViaRefs, append(route.GeoRefs, append(route.ZoneRefs, route.AffectedZoneRefs...)...)...)...)) {
			if path != "" {
				scores[path] += 14
			}
		}
	}
	for _, relation := range relations {
		if relation.SourcePath != "" {
			scores[relation.SourcePath] += 6
		}
		if relation.TargetPath != "" {
			scores[relation.TargetPath] += 6
		}
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		for _, path := range compactUnique(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			if path != "" {
				scores[path] += 2
			}
		}
	}
	return scores
}

func summarizePreparedObjects(fragments map[string]*hmodels.HorizonFragment, scores map[string]int, limit int) []ObjectSummary {
	type ranked struct {
		path  string
		score int
	}
	rows := make([]ranked, 0, len(fragments))
	for path := range fragments {
		rows = append(rows, ranked{path: path, score: scores[path]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score == rows[j].score {
			return rows[i].path < rows[j].path
		}
		return rows[i].score > rows[j].score
	})
	if limit <= 0 {
		limit = 120
	}
	out := make([]ObjectSummary, 0, minInt(limit, len(rows)))
	for _, row := range rows {
		fragment := fragments[row.path]
		if fragment == nil {
			continue
		}
		out = append(out, ObjectSummary{
			CanonicalPath: row.path,
			Type:          string(fragment.Class),
			DisplayName:   fragmentDisplayName(fragment),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func summarizeProducts(items map[string]*hmodels.MaterializedArtifact) []ProductSummary {
	counts := map[string]int{}
	for _, route := range summarizeRoutes(items) {
		if productRef := strings.TrimSpace(route.ProductRef); productRef != "" {
			counts[productRef]++
		}
	}
	out := make([]ProductSummary, 0, len(counts))
	for productRef, usedBy := range counts {
		out = append(out, ProductSummary{ProductRef: productRef, UsedBy: usedBy})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UsedBy == out[j].UsedBy {
			return out[i].ProductRef < out[j].ProductRef
		}
		return out[i].UsedBy > out[j].UsedBy
	})
	return out
}

func summarizeRoutes(items map[string]*hmodels.MaterializedArtifact) []RouteSummary {
	out := make([]RouteSummary, 0, len(items)*4)
	for _, artifact := range sortedArtifacts(items) {
		if artifact == nil {
			continue
		}
		parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for idx, leg := range parsed.Legs {
			out = append(out, RouteSummary{
				ArtifactID:           strings.TrimSpace(artifact.ID),
				ArtifactCanonicalKey: strings.TrimSpace(artifact.CanonicalKey),
				LegIndex:             idx,
				SupplierRef:          strings.TrimSpace(leg.SupplierRef),
				CustomerRef:          strings.TrimSpace(leg.CustomerRef),
				ProductRef:           strings.TrimSpace(leg.ProductRef),
				SupplyLinkRef:        strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				TransportViaRefs:     compactUnique(leg.TransportViaRefs),
				Mode:                 strings.TrimSpace(leg.Mode),
				VesselType:           strings.TrimSpace(leg.VesselType),
				FlowRefs:             compactUnique(leg.FlowRefs),
				GeoRefs:              compactUnique(leg.GeoRefs),
				ZoneRefs:             compactUnique(leg.ZoneRefs),
				AffectedZoneRefs:     compactUnique(leg.AffectedZoneRefs),
				RouteGeometrySource:  strings.TrimSpace(leg.RouteGeometrySource),
				RouteCoordinatesN:    len(leg.RouteCoordinates),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ArtifactCanonicalKey == out[j].ArtifactCanonicalKey {
			return out[i].LegIndex < out[j].LegIndex
		}
		return out[i].ArtifactCanonicalKey < out[j].ArtifactCanonicalKey
	})
	return out
}

func detectPreparedGaps(artifacts map[string]*hmodels.MaterializedArtifact, relations []RelationSummary) []GapSummary {
	gaps := make([]GapSummary, 0, 6)
	if len(artifacts) == 0 {
		gaps = append(gaps, GapSummary{Code: "missing_materialized_paths", Details: "No materialized supply-chain paths were found for this scope."})
	}
	routes := summarizeRoutes(artifacts)
	if len(routes) == 0 {
		gaps = append(gaps, GapSummary{Code: "missing_route_legs", Details: "No route legs were found in materialized artifacts."})
	}
	missingVia := 0
	missingMode := 0
	for _, route := range routes {
		if len(route.TransportViaRefs) == 0 {
			missingVia++
		}
		if strings.TrimSpace(route.Mode) == "" {
			missingMode++
		}
	}
	if missingVia > 0 {
		gaps = append(gaps, GapSummary{Code: "route_detail_partial", Details: fmt.Sprintf("%d route legs have no transportVia refs.", missingVia)})
	}
	if missingMode > 0 {
		gaps = append(gaps, GapSummary{Code: "mode_partial", Details: fmt.Sprintf("%d route legs have no transport mode.", missingMode)})
	}
	if len(relations) == 0 {
		gaps = append(gaps, GapSummary{Code: "missing_relations", Details: "No dependency edges were loaded around the prepared scope."})
	}
	return gaps
}

func buildPreparedCorpusDocs(sub *PreparedSubstrate) []PreparedCorpusDoc {
	if sub == nil {
		return nil
	}
	docs := make([]PreparedCorpusDoc, 0, 128)
	corridors := summarizeCorridors(sub.Routes)
	hints := preparedQuestionHints(sub)
	addDocWithTemperature := func(id, title, kind, temperature, content string, refs []string, extra map[string]any) {
		if strings.TrimSpace(content) == "" {
			return
		}
		structData := map[string]any{
			"type":           "internal_knowledge_capsule",
			"kind":           kind,
			"temperature":    firstNonEmpty(strings.TrimSpace(temperature), PreparedTemperatureFull),
			"title":          title,
			"corpusId":       sub.CorpusID,
			"lensId":         firstNonEmpty(strings.TrimSpace(sub.Lens.ID)),
			"lensName":       firstNonEmpty(strings.TrimSpace(sub.Lens.Name)),
			"question":       sub.Question,
			"questionHints":  hints,
			"canonicalPaths": compactUnique(refs),
		}
		for k, v := range extra {
			structData[k] = v
		}
		docs = append(docs, PreparedCorpusDoc{
			ID:          stableDocID(sub.CorpusID, kind, id),
			Title:       title,
			Kind:        kind,
			Temperature: firstNonEmpty(strings.TrimSpace(temperature), PreparedTemperatureFull),
			Content:     strings.TrimSpace(content),
			StructData:  structData,
		})
	}
	addDocWithTemperature("overview", firstNonEmpty(sub.Lens.Name, sub.Question), "overview", PreparedTemperatureHot, buildPreparedOverviewDoc(sub, corridors, sub.Fragments), collectOverviewRefs(sub), map[string]any{
		"routeCount":    len(sub.Routes),
		"productCount":  len(sub.Products),
		"objectCount":   len(sub.Objects),
		"relationCount": len(sub.Relations),
		"gapCount":      len(sub.Gaps),
	})
	addDocWithTemperature("dossier", firstNonEmpty(sub.Lens.Name, sub.Question)+" dossier", "dossier", PreparedTemperatureHot, buildPreparedDossierDoc(sub, corridors, sub.Fragments), collectOverviewRefs(sub), map[string]any{
		"routeCount":    len(sub.Routes),
		"productCount":  len(sub.Products),
		"objectCount":   len(sub.Objects),
		"relationCount": len(sub.Relations),
		"gapCount":      len(sub.Gaps),
	})
	addDocWithTemperature("products", firstNonEmpty(sub.Lens.Name, sub.Question)+" products", "products", PreparedTemperatureHot, buildPreparedProductsDoc(sub, sub.Fragments), topProductRefs(sub.Products, 40), map[string]any{
		"productCount": len(sub.Products),
	})
	addDocWithTemperature("gaps", firstNonEmpty(sub.Lens.Name, sub.Question)+" gaps", "gaps", PreparedTemperatureHot, buildPreparedGapDoc(sub), collectOverviewRefs(sub), map[string]any{
		"gapCount": len(sub.Gaps),
	})
	for i, corridor := range corridors {
		if i >= 24 {
			break
		}
		temperature := PreparedTemperatureWarm
		if i < 12 {
			temperature = PreparedTemperatureHot
		}
		addDocWithTemperature(corridor.ID, buildCorridorTitle(corridor, sub.Fragments), "corridor", temperature, buildCorridorDoc(corridor, sub.Fragments), compactUnique(append([]string{corridor.SupplierRef, corridor.CustomerRef, corridor.ProductRef}, corridor.TransportViaRefs...)), map[string]any{
			"routeCount": corridor.RouteCount,
			"modes":      corridor.Modes,
		})
	}
	for i, relation := range sub.Relations {
		if i >= 24 {
			break
		}
		addDocWithTemperature(firstNonEmpty(relation.EdgeID, fmt.Sprintf("%s-%s-%s", relation.EdgeType, relation.SourcePath, relation.TargetPath)), buildRelationTitle(relation), "relation", PreparedTemperatureWarm, buildRelationDoc(relation), compactUnique([]string{relation.SourcePath, relation.TargetPath}), map[string]any{
			"edgeType": relation.EdgeType,
			"score":    relation.Score,
		})
	}
	for i, object := range sub.Objects {
		if i >= 24 {
			break
		}
		fragment := sub.Fragments[object.CanonicalPath]
		temperature := PreparedTemperatureWarm
		if i < 12 {
			temperature = PreparedTemperatureHot
		}
		addDocWithTemperature(object.CanonicalPath, firstNonEmpty(object.DisplayName, object.CanonicalPath), "object", temperature, buildObjectDoc(fragment), []string{object.CanonicalPath}, map[string]any{
			"fragmentType": object.Type,
		})
	}
	for i, artifact := range sortedArtifacts(sub.Artifacts) {
		if i >= 16 {
			break
		}
		temperature := PreparedTemperatureFull
		if i < 8 {
			temperature = PreparedTemperatureWarm
		}
		addDocWithTemperature(firstNonEmpty(artifact.CanonicalKey, artifact.ID), firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey), "route_artifact", temperature, buildRouteArtifactDoc(artifact, sub.Fragments), artifactPreparedRefs(artifact), map[string]any{
			"artifactKind":   string(artifact.Kind),
			"pathStatus":     artifact.PathStatus,
			"artifactKey":    artifact.CanonicalKey,
			"anchorSupplier": artifact.AnchorSupplier,
			"anchorCustomer": artifact.AnchorCustomer,
			"productAxis":    artifact.ProductAxis,
		})
	}
	for i, primitive := range sub.Analytical {
		if i >= 24 || primitive == nil {
			break
		}
		temperature := PreparedTemperatureWarm
		if i < 12 {
			temperature = PreparedTemperatureHot
		}
		addDocWithTemperature(firstNonEmpty(primitive.ID, fmt.Sprintf("%s-%s", primitive.PrimitiveType, primitive.ScopeRef)),
			firstNonEmpty(primitive.PrimitiveType, "analytical primitive"),
			"analytical_primitive",
			temperature,
			primitive.GenerateVectorText(),
			compactUnique(append(append(append([]string{primitive.ScopeRef, primitive.SubjectRef}, primitive.ProductRefs...), primitive.NodeRefs...), append(append(primitive.RouteRefs, primitive.ZoneRefs...), primitive.AffectedRefs...)...)),
			map[string]any{
				"primitiveType": primitive.PrimitiveType,
				"scopeType":     primitive.ScopeType,
				"severity":      primitive.SeverityScore,
				"severityBand":  primitive.SeverityBand,
				"layer":         primitive.Layer,
				"sourceClass":   primitive.SourceClass,
			},
		)
	}
	for i, aggregate := range sub.Aggregates {
		if i >= 24 || aggregate == nil {
			break
		}
		temperature := PreparedTemperatureWarm
		if strings.EqualFold(strings.TrimSpace(aggregate.ScopeType), "lens") || strings.EqualFold(strings.TrimSpace(aggregate.ScopeType), "watch_subject") || i < 12 {
			temperature = PreparedTemperatureHot
		}
		addDocWithTemperature(
			firstNonEmpty(aggregate.ID, fmt.Sprintf("%s-%s", aggregate.AggregateType, aggregate.ScopeRef)),
			firstNonEmpty(aggregate.AggregateType, "analytical aggregate"),
			"analytical_aggregate",
			temperature,
			aggregate.GenerateVectorText(),
			compactUnique(append(append(append([]string{aggregate.ScopeRef, aggregate.SubjectRef}, aggregate.ProductRefs...), aggregate.NodeRefs...), append(append(aggregate.RouteRefs, aggregate.ZoneRefs...), aggregate.AffectedRefs...)...)),
			map[string]any{
				"aggregateType":   aggregate.AggregateType,
				"scopeType":       aggregate.ScopeType,
				"severity":        aggregate.SeverityScore,
				"severityBand":    aggregate.SeverityBand,
				"watchSubjectIds": append([]string(nil), aggregate.WatchSubjectIDs...),
				"lensRefs":        append([]string(nil), aggregate.LensRefs...),
			},
		)
	}
	for i, summary := range sub.SignalLanding {
		if i >= 24 || summary == nil {
			break
		}
		temperature := PreparedTemperatureHot
		if i >= 12 {
			temperature = PreparedTemperatureWarm
		}
		addDocWithTemperature(
			firstNonEmpty(summary.ID, fmt.Sprintf("%s-%d", summary.ScopeRef, i)),
			firstNonEmpty(buildSignalLandingTitle(summary), "signal landing summary"),
			"signal_landing_summary",
			temperature,
			buildSignalLandingDoc(summary),
			signalLandingCanonicalRefs(summary),
			map[string]any{
				"scopeRef":           summary.ScopeRef,
				"missingPreciseZone": summary.MissingPreciseZone,
				"confirmedCount":     len(summary.TopConfirmedRefs),
				"inferredCount":      len(summary.TopInferredRefs),
				"affectedZoneCount":  len(summary.AffectedZoneRefs),
				"updatedAt":          summary.UpdatedAt.Format(time.RFC3339Nano),
				"eventCoreRefs":      signalLandingEventCoreRefs(summary),
				"haloRefs":           signalLandingHaloRefs(summary),
				"backgroundRefs":     compactUnique([]string{summary.ScopeRef}),
				"topClaims":          append([]string(nil), summary.TopClaims...),
				"chokepoints":        append([]string(nil), summary.Chokepoints...),
				"freshnessClass":     signalLandingFreshnessClass(summary.UpdatedAt),
			},
		)
	}
	return docs
}

func summarizeCorridors(routes []RouteSummary) []corridorSummary {
	type key struct {
		supplier string
		customer string
		product  string
	}
	type agg struct {
		corridorSummary
		viaSet      map[string]struct{}
		modeSet     map[string]struct{}
		artifactSet map[string]struct{}
	}
	grouped := map[key]*agg{}
	for _, route := range routes {
		k := key{
			supplier: strings.TrimSpace(route.SupplierRef),
			customer: strings.TrimSpace(route.CustomerRef),
			product:  strings.TrimSpace(route.ProductRef),
		}
		item := grouped[k]
		if item == nil {
			item = &agg{
				corridorSummary: corridorSummary{
					ID:          fmt.Sprintf("%s|%s|%s", k.supplier, k.customer, k.product),
					SupplierRef: k.supplier,
					CustomerRef: k.customer,
					ProductRef:  k.product,
				},
				viaSet:      map[string]struct{}{},
				modeSet:     map[string]struct{}{},
				artifactSet: map[string]struct{}{},
			}
			grouped[k] = item
		}
		item.RouteCount++
		for _, via := range route.TransportViaRefs {
			if via = strings.TrimSpace(via); via != "" {
				item.viaSet[via] = struct{}{}
			}
		}
		if mode := strings.TrimSpace(route.Mode); mode != "" {
			item.modeSet[mode] = struct{}{}
		}
		if key := strings.TrimSpace(route.ArtifactCanonicalKey); key != "" {
			item.artifactSet[key] = struct{}{}
		}
	}
	out := make([]corridorSummary, 0, len(grouped))
	for _, item := range grouped {
		item.TransportViaRefs = sortedSetKeys(item.viaSet)
		item.Modes = sortedSetKeys(item.modeSet)
		item.ArtifactKeys = sortedSetKeys(item.artifactSet)
		out = append(out, item.corridorSummary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RouteCount == out[j].RouteCount {
			if out[i].SupplierRef == out[j].SupplierRef {
				if out[i].CustomerRef == out[j].CustomerRef {
					return out[i].ProductRef < out[j].ProductRef
				}
				return out[i].CustomerRef < out[j].CustomerRef
			}
			return out[i].SupplierRef < out[j].SupplierRef
		}
		return out[i].RouteCount > out[j].RouteCount
	})
	return out
}

func buildPreparedOverviewDoc(sub *PreparedSubstrate, corridors []corridorSummary, fragments map[string]*hmodels.HorizonFragment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n", sub.Question)
	if sub.Lens != nil {
		fmt.Fprintf(&b, "Lens: %s\n", firstNonEmpty(sub.Lens.Name, sub.Lens.ID))
		if strings.TrimSpace(sub.Lens.OriginalQuery) != "" {
			fmt.Fprintf(&b, "Original framing: %s\n", strings.TrimSpace(sub.Lens.OriginalQuery))
		}
	}
	if len(sub.ScopePaths) > 0 {
		b.WriteString("Scope paths:\n")
		for i, path := range sub.ScopePaths {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s\n", path)
		}
	}
	if len(sub.Objects) > 0 {
		b.WriteString("Key objects:\n")
		for i, object := range sub.Objects {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s (%s) path=%s\n", firstNonEmpty(object.DisplayName, object.CanonicalPath), object.Type, object.CanonicalPath)
		}
	}
	if len(sub.Products) > 0 {
		b.WriteString("Products:\n")
		for i, product := range sub.Products {
			if i >= 20 {
				break
			}
			fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", preparedPathSummary(product.ProductRef, fragments), product.UsedBy)
		}
	}
	if len(corridors) > 0 {
		b.WriteString("Logistics corridors:\n")
		for i, corridor := range corridors {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s -> %s product=%s routes=%d via=%s modes=%s\n",
				preparedPathSummary(firstNonEmpty(corridor.SupplierRef, "?"), fragments),
				preparedPathSummary(firstNonEmpty(corridor.CustomerRef, "?"), fragments),
				preparedPathSummary(firstNonEmpty(corridor.ProductRef, "?"), fragments),
				corridor.RouteCount,
				strings.Join(preparedPathSummaries(corridor.TransportViaRefs, fragments), " | "),
				strings.Join(corridor.Modes, ", "),
			)
		}
	}
	if len(sub.Relations) > 0 {
		b.WriteString("Graph relations:\n")
		for i, relation := range sub.Relations {
			if i >= 40 {
				break
			}
			fmt.Fprintf(&b, "- %s [%s] %s evidence=%0.0f\n", firstNonEmpty(relation.SourcePath, "?"), relation.EdgeType, firstNonEmpty(relation.TargetPath, "?"), relation.Score)
		}
	}
	if len(sub.Gaps) > 0 {
		b.WriteString("Known gaps:\n")
		for _, gap := range sub.Gaps {
			fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
		}
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedProductsDoc(sub *PreparedSubstrate, fragments map[string]*hmodels.HorizonFragment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Products for question: %s\n", sub.Question)
	for i, product := range sub.Products {
		if i >= 40 {
			break
		}
		fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", preparedPathSummary(product.ProductRef, fragments), product.UsedBy)
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedDossierDoc(sub *PreparedSubstrate, corridors []corridorSummary, fragments map[string]*hmodels.HorizonFragment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Answer dossier for question: %s\n", sub.Question)
	if sub.Lens != nil {
		fmt.Fprintf(&b, "Scope: %s\n", firstNonEmpty(sub.Lens.Name, sub.Lens.ID))
	}
	b.WriteString("Objects:\n")
	for i, object := range sub.Objects {
		if i >= 12 {
			break
		}
		fmt.Fprintf(&b, "- %s [%s] path=%s\n", firstNonEmpty(object.DisplayName, object.CanonicalPath), firstNonEmpty(object.Type, "UNKNOWN"), object.CanonicalPath)
	}
	b.WriteString("Connections:\n")
	for i, relation := range sub.Relations {
		if i >= 16 {
			break
		}
		fmt.Fprintf(&b, "- %s [%s] %s\n", firstNonEmpty(relation.SourcePath, "?"), relation.EdgeType, firstNonEmpty(relation.TargetPath, "?"))
	}
	b.WriteString("Routes:\n")
	for i, corridor := range corridors {
		if i >= 16 {
			break
		}
		fmt.Fprintf(&b, "- %s -> %s product=%s via=%s modes=%s routes=%d\n",
			preparedPathSummary(firstNonEmpty(corridor.SupplierRef, "?"), fragments),
			preparedPathSummary(firstNonEmpty(corridor.CustomerRef, "?"), fragments),
			preparedPathSummary(firstNonEmpty(corridor.ProductRef, "?"), fragments),
			strings.Join(preparedPathSummaries(corridor.TransportViaRefs, fragments), " | "),
			strings.Join(corridor.Modes, ", "),
			corridor.RouteCount,
		)
	}
	if len(sub.Products) > 0 {
		b.WriteString("Products:\n")
		for i, product := range sub.Products {
			if i >= 12 {
				break
			}
			fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", preparedPathSummary(product.ProductRef, fragments), product.UsedBy)
		}
	}
	if len(sub.Gaps) > 0 {
		b.WriteString("Gaps:\n")
		for _, gap := range sub.Gaps {
			fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
		}
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedGapDoc(sub *PreparedSubstrate) string {
	if len(sub.Gaps) == 0 {
		return "No critical internal-knowledge gaps were detected for this prepared corpus."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Known gaps for question: %s\n", sub.Question)
	for _, gap := range sub.Gaps {
		fmt.Fprintf(&b, "- %s: %s\n", gap.Code, gap.Details)
	}
	return strings.TrimSpace(b.String())
}

func buildSignalLandingTitle(summary *hmodels.SignalLandingSummary) string {
	if summary == nil {
		return "signal landing summary"
	}
	if len(summary.AffectedZoneRefs) > 0 {
		return "Signal landing around " + summary.AffectedZoneRefs[0]
	}
	if len(summary.TopConfirmedRefs) > 0 {
		return "Signal landing around " + summary.TopConfirmedRefs[0]
	}
	return "Signal landing for " + strings.TrimSpace(summary.ScopeRef)
}

func buildSignalLandingDoc(summary *hmodels.SignalLandingSummary) string {
	if summary == nil {
		return ""
	}
	lines := []string{
		fmt.Sprintf("Scope: %s", strings.TrimSpace(summary.ScopeRef)),
	}
	if !summary.UpdatedAt.IsZero() {
		lines = append(lines, "Updated at: "+summary.UpdatedAt.UTC().Format(time.RFC3339))
	}
	if core := signalLandingEventCoreRefs(summary); len(core) > 0 {
		lines = append(lines, "Event core: "+strings.Join(firstNStrings(core, 8), ", "))
	}
	if halo := signalLandingHaloRefs(summary); len(halo) > 0 {
		lines = append(lines, "Impact halo: "+strings.Join(firstNStrings(halo, 10), ", "))
	}
	if len(summary.TopClaims) > 0 {
		lines = append(lines, "Claims: "+strings.Join(firstNStrings(summary.TopClaims, 6), "; "))
	}
	if len(summary.TopConfirmedRefs) > 0 {
		lines = append(lines, "Confirmed landed refs: "+strings.Join(firstNStrings(summary.TopConfirmedRefs, 8), ", "))
	}
	if len(summary.TopInferredRefs) > 0 {
		lines = append(lines, "Inferred landed refs: "+strings.Join(firstNStrings(summary.TopInferredRefs, 8), ", "))
	}
	if len(summary.AffectedZoneRefs) > 0 {
		lines = append(lines, "Affected zones: "+strings.Join(firstNStrings(summary.AffectedZoneRefs, 6), ", "))
	}
	if len(summary.Products) > 0 {
		lines = append(lines, "Products: "+strings.Join(firstNStrings(summary.Products, 8), ", "))
	}
	if len(summary.Routes) > 0 {
		lines = append(lines, "Routes and chokepoints: "+strings.Join(firstNStrings(summary.Routes, 8), ", "))
	}
	if len(summary.Facilities) > 0 {
		lines = append(lines, "Facilities: "+strings.Join(firstNStrings(summary.Facilities, 8), ", "))
	}
	if len(summary.TopMissingCandidates) > 0 {
		lines = append(lines, "Missing anchors: "+strings.Join(firstNStrings(summary.TopMissingCandidates, 6), ", "))
	}
	if summary.MissingPreciseZone {
		lines = append(lines, "Data gap: precise affected zone is still missing.")
	}
	return strings.Join(lines, "\n")
}

func signalLandingCanonicalRefs(summary *hmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return compactUnique(append(
		append(
			append([]string{summary.ScopeRef}, summary.TopConfirmedRefs...),
			summary.TopInferredRefs...,
		),
		append(summary.AffectedZoneRefs, append(summary.Products, append(append(summary.Routes, summary.Facilities...), summary.Chokepoints...)...)...)...,
	))
}

func signalLandingEventCoreRefs(summary *hmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return compactUnique(append(append([]string{summary.ScopeRef}, summary.TopConfirmedRefs...), append(summary.TopInferredRefs, summary.AffectedZoneRefs...)...))
}

func signalLandingHaloRefs(summary *hmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return compactUnique(append(summary.Products, append(append(summary.Routes, summary.Facilities...), summary.Chokepoints...)...))
}

func signalLandingFreshnessClass(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return "stale"
	}
	age := time.Since(updatedAt.UTC())
	switch {
	case age <= 2*time.Hour:
		return "flash"
	case age <= 24*time.Hour:
		return "hot"
	case age <= 7*24*time.Hour:
		return "warm"
	default:
		return "stale"
	}
}

func firstNStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) == 0 {
		return nil
	}
	values = compactUnique(values)
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func buildCorridorTitle(c corridorSummary, fragments map[string]*hmodels.HorizonFragment) string {
	return fmt.Sprintf(
		"Corridor %s -> %s product %s",
		preparedPathTitle(firstNonEmpty(c.SupplierRef, "?"), fragments),
		preparedPathTitle(firstNonEmpty(c.CustomerRef, "?"), fragments),
		preparedPathTitle(firstNonEmpty(c.ProductRef, "?"), fragments),
	)
}

func buildCorridorDoc(c corridorSummary, fragments map[string]*hmodels.HorizonFragment) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Supplier: %s\nCustomer: %s\nProduct: %s\nRoute count: %d\nTransport via: %s\nModes: %s\nMaterialized artifacts: %s",
		preparedPathSummary(firstNonEmpty(c.SupplierRef, "?"), fragments),
		preparedPathSummary(firstNonEmpty(c.CustomerRef, "?"), fragments),
		preparedPathSummary(firstNonEmpty(c.ProductRef, "?"), fragments),
		c.RouteCount,
		strings.Join(preparedPathSummaries(c.TransportViaRefs, fragments), " | "),
		strings.Join(c.Modes, ", "),
		strings.Join(c.ArtifactKeys, ", "),
	))
}

func buildRelationTitle(r RelationSummary) string {
	return fmt.Sprintf("Relation %s %s %s", firstNonEmpty(r.SourcePath, "?"), r.EdgeType, firstNonEmpty(r.TargetPath, "?"))
}

func buildRelationDoc(r RelationSummary) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Source: %s\nRelation: %s\nTarget: %s\nEvidence count: %0.0f",
		firstNonEmpty(r.SourcePath, "?"),
		r.EdgeType,
		firstNonEmpty(r.TargetPath, "?"),
		r.Score,
	))
}

func buildObjectDoc(fragment *hmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Object: %s\n", fragmentDisplayName(fragment))
	fmt.Fprintf(&b, "Canonical path: %s\n", fragment.CanonicalPath)
	fmt.Fprintf(&b, "Type: %s\n", fragment.Class)
	if strings.TrimSpace(fragment.Description) != "" {
		fmt.Fprintf(&b, "Description: %s\n", strings.TrimSpace(fragment.Description))
	}
	if len(fragment.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(compactUnique(fragment.Tags), ", "))
	}
	if len(fragment.IndustryPaths) > 0 {
		fmt.Fprintf(&b, "Industry paths: %s\n", strings.Join(compactUnique(fragment.IndustryPaths), ", "))
	}
	if len(fragment.MarketSegments) > 0 {
		fmt.Fprintf(&b, "Market segments: %s\n", strings.Join(compactUnique(fragment.MarketSegments), ", "))
	}
	return strings.TrimSpace(b.String())
}

func buildRouteArtifactDoc(artifact *hmodels.MaterializedArtifact, fragments map[string]*hmodels.HorizonFragment) string {
	if artifact == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Materialized route artifact: %s\n", firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey))
	fmt.Fprintf(&b, "Canonical key: %s\n", artifact.CanonicalKey)
	fmt.Fprintf(&b, "Kind: %s\n", artifact.Kind)
	fmt.Fprintf(&b, "Path status: %s\n", artifact.PathStatus)
	if strings.TrimSpace(artifact.ProductAxis) != "" {
		fmt.Fprintf(&b, "Product axis: %s\n", preparedPathSummary(artifact.ProductAxis, fragments))
	}
	if len(artifact.NodeRefs) > 0 {
		fmt.Fprintf(&b, "Node refs: %s\n", strings.Join(preparedPathSummaries(compactUnique(artifact.NodeRefs), fragments), ", "))
	}
	if len(artifact.GeoRefs) > 0 {
		fmt.Fprintf(&b, "Geo refs: %s\n", strings.Join(preparedPathSummaries(compactUnique(artifact.GeoRefs), fragments), ", "))
	}
	parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err == nil && parsed != nil {
		for _, leg := range parsed.Legs {
			fmt.Fprintf(&b, "- leg: %s -> %s product=%s via=%s mode=%s supplyLink=%s\n",
				preparedPathSummary(firstNonEmpty(leg.SupplierRef, "?"), fragments),
				preparedPathSummary(firstNonEmpty(leg.CustomerRef, "?"), fragments),
				preparedPathSummary(firstNonEmpty(leg.ProductRef, "?"), fragments),
				strings.Join(preparedPathSummaries(compactUnique(leg.TransportViaRefs), fragments), " | "),
				firstNonEmpty(leg.Mode, "unknown"),
				preparedPathSummary(firstNonEmpty(leg.SupplyLinkCanonicalPath, "-"), fragments),
			)
		}
	}
	return strings.TrimSpace(b.String())
}

func artifactPreparedRefs(artifact *hmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	refs := compactUnique(append(
		append(
			append(
				append(
					append([]string{artifact.CanonicalKey, artifact.AnchorSupplier, artifact.AnchorCustomer, artifact.ProductAxis}, artifact.NodeRefs...),
					artifact.GeoRefs...,
				),
				artifact.ZoneRefs...,
			),
			artifact.AffectedZoneRefs...,
		),
		artifact.FlowRefs...,
	))
	parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil {
		return refs
	}
	for _, leg := range parsed.Legs {
		refs = append(refs, leg.SupplierRef, leg.CustomerRef, leg.ProductRef, leg.SupplyLinkCanonicalPath)
		refs = append(refs, leg.TransportViaRefs...)
		refs = append(refs, leg.GeoRefs...)
		refs = append(refs, leg.ZoneRefs...)
		refs = append(refs, leg.AffectedZoneRefs...)
		refs = append(refs, leg.FlowRefs...)
	}
	return compactUnique(refs)
}

func preparedPathSummary(path string, fragments map[string]*hmodels.HorizonFragment) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "?" || path == "-" {
		return path
	}
	if fragment := fragments[path]; fragment != nil {
		if label := fragmentDisplayName(fragment); label != "" && label != path {
			return fmt.Sprintf("%s (%s)", label, path)
		}
	}
	return path
}

func preparedPathTitle(path string, fragments map[string]*hmodels.HorizonFragment) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "?" || path == "-" {
		return path
	}
	if fragment := fragments[path]; fragment != nil {
		if label := fragmentDisplayName(fragment); label != "" {
			return label
		}
	}
	return path
}

func preparedPathSummaries(paths []string, fragments map[string]*hmodels.HorizonFragment) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, path := range compactUnique(paths) {
		out = append(out, preparedPathSummary(path, fragments))
	}
	return out
}

func collectOverviewRefs(sub *PreparedSubstrate) []string {
	refs := append([]string{}, sub.ScopePaths...)
	for _, object := range sub.Objects {
		refs = append(refs, object.CanonicalPath)
	}
	return compactUnique(refs)
}

func topProductRefs(products []ProductSummary, limit int) []string {
	out := make([]string, 0, minInt(limit, len(products)))
	for _, product := range products {
		if len(out) >= limit {
			break
		}
		out = append(out, product.ProductRef)
	}
	return compactUnique(out)
}

func preparedQuestionHints(sub *PreparedSubstrate) []string {
	hints := []string{
		sub.Question,
		firstNonEmpty(sub.Lens.Name),
		firstNonEmpty(sub.Lens.OriginalQuery),
	}
	for _, object := range sub.Objects {
		if len(hints) >= 32 {
			break
		}
		hints = append(hints, object.CanonicalPath)
	}
	for _, product := range sub.Products {
		if len(hints) >= 40 {
			break
		}
		hints = append(hints, product.ProductRef)
	}
	return compactUnique(hints)
}

func stableDocID(corpusID, kind, seed string) string {
	return fmt.Sprintf("%s-%s-%s", shortSlug(kind), shortSlug(seed), shortHash(corpusID+"|"+kind+"|"+seed))
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])[:10]
}

func shortSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			if !lastDash {
				b.WriteRune(r)
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s = strings.Trim(b.String(), "-_")
	if s == "" {
		return "item"
	}
	if len(s) > 48 {
		return s[:48]
	}
	return s
}

func sortedArtifacts(items map[string]*hmodels.MaterializedArtifact) []*hmodels.MaterializedArtifact {
	out := make([]*hmodels.MaterializedArtifact, 0, len(items))
	for _, artifact := range items {
		if artifact != nil {
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		li := strings.TrimSpace(firstNonEmpty(out[i].CanonicalKey, out[i].ID))
		lj := strings.TrimSpace(firstNonEmpty(out[j].CanonicalKey, out[j].ID))
		return li < lj
	})
	return out
}

func fragmentDisplayName(fragment *hmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(fragment.Labels["en"]), strings.TrimSpace(fragment.Labels["ru"]), strings.TrimSpace(fragment.CanonicalPath))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func compactUnique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
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

func mapKeys[T any](m map[string]T) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, strings.TrimSpace(key))
	}
	sort.Strings(out)
	return compactUnique(out)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func addNonEmptyPath(dst map[string]struct{}, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	dst[path] = struct{}{}
}

func sortedSetKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	sort.Strings(out)
	return out
}
