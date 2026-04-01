package internalknowledge

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

type PreparedSubstrate struct {
	CorpusID   string
	Lens       *hmodels.Lens
	Question   string
	ScopePaths []string
	Fragments  map[string]*hmodels.HorizonFragment
	Artifacts  map[string]*hmodels.MaterializedArtifact
	Routes     []RouteSummary
	Objects    []ObjectSummary
	Relations  []RelationSummary
	Products   []ProductSummary
	Gaps       []GapSummary
	Docs       []PreparedCorpusDoc
}

type PreparedCorpusDoc struct {
	ID         string
	Title      string
	Kind       string
	Content    string
	StructData map[string]any
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
	fragments     *hrepo.HorizonFragmentsRepository
	artifacts     *hrepo.MaterializedArtifactsRepository
	lensArtifacts *hrepo.LensArtifactsRepository
}

func NewBuilder(
	fragments *hrepo.HorizonFragmentsRepository,
	artifacts *hrepo.MaterializedArtifactsRepository,
	lensArtifacts *hrepo.LensArtifactsRepository,
) *Builder {
	return &Builder{
		fragments:     fragments,
		artifacts:     artifacts,
		lensArtifacts: lensArtifacts,
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
	for _, path := range scopePaths {
		if isPreparedCanonicalPath(path) {
			pathRefs[path] = struct{}{}
		}
	}
	for _, lensArtifact := range lensArtifacts {
		if lensArtifact == nil {
			continue
		}
		addNonEmptyPath(pathRefs, lensArtifact.CanonicalPath)
		addNonEmptyPath(pathRefs, lensArtifact.SourcePath)
		addNonEmptyPath(pathRefs, lensArtifact.TargetPath)
	}
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
			continue
		}
		artifactMap[artifact.ID] = artifact
		for _, ref := range compactUnique(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			addNonEmptyPath(pathRefs, ref)
		}
		parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			for _, ref := range compactUnique(append([]string{leg.SupplierRef, leg.CustomerRef, leg.ProductRef, leg.SupplyLinkCanonicalPath}, leg.TransportViaRefs...)) {
				addNonEmptyPath(pathRefs, ref)
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

	sub := &PreparedSubstrate{
		CorpusID:   resolveCorpusID(lens, question),
		Lens:       lens,
		Question:   resolvePreparedQuestion(lens, question),
		ScopePaths: scopePaths,
		Fragments:  fragmentsByPath,
		Artifacts:  artifactMap,
		Routes:     routes,
		Objects:    objects,
		Relations:  relations,
		Products:   products,
		Gaps:       detectPreparedGaps(artifactMap, relations),
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
			Type:          string(fragment.Type),
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
	addDoc := func(id, title, kind, content string, refs []string, extra map[string]any) {
		if strings.TrimSpace(content) == "" {
			return
		}
		structData := map[string]any{
			"type":           "internal_knowledge_capsule",
			"kind":           kind,
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
			ID:         stableDocID(sub.CorpusID, kind, id),
			Title:      title,
			Kind:       kind,
			Content:    strings.TrimSpace(content),
			StructData: structData,
		})
	}
	addDoc("overview", firstNonEmpty(sub.Lens.Name, sub.Question), "overview", buildPreparedOverviewDoc(sub, corridors), collectOverviewRefs(sub), map[string]any{
		"routeCount":    len(sub.Routes),
		"productCount":  len(sub.Products),
		"objectCount":   len(sub.Objects),
		"relationCount": len(sub.Relations),
		"gapCount":      len(sub.Gaps),
	})
	addDoc("dossier", firstNonEmpty(sub.Lens.Name, sub.Question)+" dossier", "dossier", buildPreparedDossierDoc(sub, corridors), collectOverviewRefs(sub), map[string]any{
		"routeCount":    len(sub.Routes),
		"productCount":  len(sub.Products),
		"objectCount":   len(sub.Objects),
		"relationCount": len(sub.Relations),
		"gapCount":      len(sub.Gaps),
	})
	addDoc("products", firstNonEmpty(sub.Lens.Name, sub.Question)+" products", "products", buildPreparedProductsDoc(sub), topProductRefs(sub.Products, 40), map[string]any{
		"productCount": len(sub.Products),
	})
	addDoc("gaps", firstNonEmpty(sub.Lens.Name, sub.Question)+" gaps", "gaps", buildPreparedGapDoc(sub), collectOverviewRefs(sub), map[string]any{
		"gapCount": len(sub.Gaps),
	})
	for i, corridor := range corridors {
		if i >= 24 {
			break
		}
		addDoc(corridor.ID, buildCorridorTitle(corridor), "corridor", buildCorridorDoc(corridor), compactUnique(append([]string{corridor.SupplierRef, corridor.CustomerRef, corridor.ProductRef}, corridor.TransportViaRefs...)), map[string]any{
			"routeCount": corridor.RouteCount,
			"modes":      corridor.Modes,
		})
	}
	for i, relation := range sub.Relations {
		if i >= 24 {
			break
		}
		addDoc(firstNonEmpty(relation.EdgeID, fmt.Sprintf("%s-%s-%s", relation.EdgeType, relation.SourcePath, relation.TargetPath)), buildRelationTitle(relation), "relation", buildRelationDoc(relation), compactUnique([]string{relation.SourcePath, relation.TargetPath}), map[string]any{
			"edgeType": relation.EdgeType,
			"score":    relation.Score,
		})
	}
	for i, object := range sub.Objects {
		if i >= 24 {
			break
		}
		fragment := sub.Fragments[object.CanonicalPath]
		addDoc(object.CanonicalPath, firstNonEmpty(object.DisplayName, object.CanonicalPath), "object", buildObjectDoc(fragment), []string{object.CanonicalPath}, map[string]any{
			"fragmentType": object.Type,
		})
	}
	for i, artifact := range sortedArtifacts(sub.Artifacts) {
		if i >= 16 {
			break
		}
		addDoc(firstNonEmpty(artifact.CanonicalKey, artifact.ID), firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey), "route_artifact", buildRouteArtifactDoc(artifact), compactUnique(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...)), map[string]any{
			"artifactKind":   string(artifact.Kind),
			"pathStatus":     artifact.PathStatus,
			"artifactKey":    artifact.CanonicalKey,
			"anchorSupplier": artifact.AnchorSupplier,
			"anchorCustomer": artifact.AnchorCustomer,
			"productAxis":    artifact.ProductAxis,
		})
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

func buildPreparedOverviewDoc(sub *PreparedSubstrate, corridors []corridorSummary) string {
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
			fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", product.ProductRef, product.UsedBy)
		}
	}
	if len(corridors) > 0 {
		b.WriteString("Logistics corridors:\n")
		for i, corridor := range corridors {
			if i >= 24 {
				break
			}
			fmt.Fprintf(&b, "- %s -> %s product=%s routes=%d via=%s modes=%s\n",
				firstNonEmpty(corridor.SupplierRef, "?"),
				firstNonEmpty(corridor.CustomerRef, "?"),
				firstNonEmpty(corridor.ProductRef, "?"),
				corridor.RouteCount,
				strings.Join(corridor.TransportViaRefs, " | "),
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

func buildPreparedProductsDoc(sub *PreparedSubstrate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Products for question: %s\n", sub.Question)
	for i, product := range sub.Products {
		if i >= 40 {
			break
		}
		fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", product.ProductRef, product.UsedBy)
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedDossierDoc(sub *PreparedSubstrate, corridors []corridorSummary) string {
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
			firstNonEmpty(corridor.SupplierRef, "?"),
			firstNonEmpty(corridor.CustomerRef, "?"),
			firstNonEmpty(corridor.ProductRef, "?"),
			strings.Join(corridor.TransportViaRefs, " | "),
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
			fmt.Fprintf(&b, "- %s usedByRoutes=%d\n", product.ProductRef, product.UsedBy)
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

func buildCorridorTitle(c corridorSummary) string {
	return fmt.Sprintf("Corridor %s -> %s product %s", firstNonEmpty(c.SupplierRef, "?"), firstNonEmpty(c.CustomerRef, "?"), firstNonEmpty(c.ProductRef, "?"))
}

func buildCorridorDoc(c corridorSummary) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Supplier: %s\nCustomer: %s\nProduct: %s\nRoute count: %d\nTransport via: %s\nModes: %s\nMaterialized artifacts: %s",
		firstNonEmpty(c.SupplierRef, "?"),
		firstNonEmpty(c.CustomerRef, "?"),
		firstNonEmpty(c.ProductRef, "?"),
		c.RouteCount,
		strings.Join(c.TransportViaRefs, " | "),
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
	fmt.Fprintf(&b, "Type: %s\n", fragment.Type)
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

func buildRouteArtifactDoc(artifact *hmodels.MaterializedArtifact) string {
	if artifact == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Materialized route artifact: %s\n", firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey))
	fmt.Fprintf(&b, "Canonical key: %s\n", artifact.CanonicalKey)
	fmt.Fprintf(&b, "Kind: %s\n", artifact.Kind)
	fmt.Fprintf(&b, "Path status: %s\n", artifact.PathStatus)
	if len(artifact.NodeRefs) > 0 {
		fmt.Fprintf(&b, "Node refs: %s\n", strings.Join(compactUnique(artifact.NodeRefs), ", "))
	}
	if len(artifact.GeoRefs) > 0 {
		fmt.Fprintf(&b, "Geo refs: %s\n", strings.Join(compactUnique(artifact.GeoRefs), ", "))
	}
	parsed, err := hmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err == nil && parsed != nil {
		for _, leg := range parsed.Legs {
			fmt.Fprintf(&b, "- leg: %s -> %s product=%s via=%s mode=%s supplyLink=%s\n",
				firstNonEmpty(leg.SupplierRef, "?"),
				firstNonEmpty(leg.CustomerRef, "?"),
				firstNonEmpty(leg.ProductRef, "?"),
				strings.Join(compactUnique(leg.TransportViaRefs), " | "),
				firstNonEmpty(leg.Mode, "unknown"),
				firstNonEmpty(leg.SupplyLinkCanonicalPath, "-"),
			)
		}
	}
	return strings.TrimSpace(b.String())
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
