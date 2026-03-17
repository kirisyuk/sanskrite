package propagation

import (
	"context"
	"strings"
	"sync"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbinding"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathstress"
)

type supplyLinkOverlayIndex struct {
	mu      sync.RWMutex
	byPath  map[string][]supplyLinkOverlayRecord
}

type supplyLinkOverlayRecord struct {
	CanonicalPath      string
	SupplierPath       string
	CustomerPath       string
	SupplierGeoRef     string
	CustomerGeoRef     string
	ProductPath        string
	TransportVia       []string
	Confidence         float64
	Share              float64
	Volume             float64
	Contract           string
	Mode               string
	GeoRefs            []string
	FlowRefs           []string
	AlternateFlowRefs  []string
	ZoneRefs           []string
	AffectedZoneRefs   []string
	SpatialSemantics   []string
	BindingConfidence  float64
	StressScore        float64
	Fragility          float64
	Rerouteability     float64
	ZonePressure       float64
	AffectedPressure   float64
	RecoveryLatencyHrs float64
}

type supplyLinkVirtualTraversal struct {
	targetPath         string
	edgeType           horizonmodels.DependencyType
	traversalDirection string
	qualifiers         map[string]interface{}
	criticality        float64
}

func (e *PropagationEngine) ensureSupplyLinkOverlay(ctx context.Context) (*supplyLinkOverlayIndex, error) {
	_ = ctx
	if e == nil {
		return nil, nil
	}
	e.mu.RLock()
	if e.supplyLinkOverlay != nil {
		idx := e.supplyLinkOverlay
		e.mu.RUnlock()
		return idx, nil
	}
	e.mu.RUnlock()

	e.mu.Lock()
	if e.supplyLinkOverlay == nil {
		e.supplyLinkOverlay = &supplyLinkOverlayIndex{
			byPath: make(map[string][]supplyLinkOverlayRecord),
		}
	}
	idx := e.supplyLinkOverlay
	e.mu.Unlock()
	return idx, nil
}

func buildSupplyLinkOverlayRecords(ctx context.Context, links []*horizonmodels.HorizonFragment, binder *pathbinding.Engine) map[string][]supplyLinkOverlayRecord {
	out := make(map[string][]supplyLinkOverlayRecord, len(links)*2)
	stressEval := pathstress.NewEvaluator()
	for _, link := range links {
		if link == nil {
			continue
		}
		rec, ok := buildSupplyLinkOverlayRecord(link)
		if !ok {
			continue
		}
		if binder != nil {
			binding := binder.BindSegment(ctx, pathbinding.SegmentInput{
				SupplierRef:       rec.SupplierPath,
				CustomerRef:       rec.CustomerPath,
				ProductRef:        rec.ProductPath,
				TransportViaRefs:  rec.TransportVia,
				SegmentConfidence: rec.Confidence,
			})
			rec.Mode = binding.Mode
			rec.GeoRefs = append([]string(nil), binding.GeoRefs...)
			rec.FlowRefs = append([]string(nil), binding.FlowRefs...)
			rec.AlternateFlowRefs = append([]string(nil), binding.AlternateFlowRefs...)
			rec.ZoneRefs = append([]string(nil), binding.ZoneRefs...)
			rec.AffectedZoneRefs = append([]string(nil), binding.AffectedZoneRefs...)
			rec.SpatialSemantics = append([]string(nil), binding.SpatialSemantics...)
			rec.BindingConfidence = binding.BindingConfidence
			if stress := stressEval.EvaluateLeg(horizonmodels.SupplyChainPathLeg{
				SupplierRef:       rec.SupplierPath,
				CustomerRef:       rec.CustomerPath,
				ProductRef:        rec.ProductPath,
				TransportViaRefs:  append([]string(nil), rec.TransportVia...),
				Mode:              rec.Mode,
				GeoRefs:           append([]string(nil), rec.GeoRefs...),
				FlowRefs:          append([]string(nil), rec.FlowRefs...),
				AlternateFlowRefs: append([]string(nil), rec.AlternateFlowRefs...),
				ZoneRefs:          append([]string(nil), rec.ZoneRefs...),
				AffectedZoneRefs:  append([]string(nil), rec.AffectedZoneRefs...),
				SpatialSemantics:  append([]string(nil), rec.SpatialSemantics...),
				BindingConfidence: rec.BindingConfidence,
				SegmentConfidence: rec.Confidence,
			}); stress != nil {
				rec.StressScore = stress.StressScore
				rec.Fragility = stress.Fragility
				rec.Rerouteability = stress.Rerouteability
				rec.ZonePressure = stress.ZonePressure
				rec.AffectedPressure = stress.AffectedZonePressure
				rec.RecoveryLatencyHrs = stress.RecoveryLatencyHrs
			}
		}
		for _, key := range supplyLinkOverlayKeys(rec) {
			out[key] = append(out[key], rec)
		}
	}
	return out
}

func buildSupplyLinkOverlayRecord(link *horizonmodels.HorizonFragment) (supplyLinkOverlayRecord, bool) {
	if link == nil || link.Type != horizonmodels.FragmentTypeSupplyLink {
		return supplyLinkOverlayRecord{}, false
	}
	supplier := firstNonEmptyPath(link.SupplierFacilityRef, link.SupplierRef)
	customer := firstNonEmptyPath(link.CustomerFacilityRef, link.CustomerRef)
	product := strings.TrimSpace(derefString(link.ProductRef))
	if supplier == "" || customer == "" || product == "" {
		return supplyLinkOverlayRecord{}, false
	}
	confidence := metricValue(link, "confidence")
	share := metricValue(link, "share")
	if share > 1 {
		share = share / 100.0
	}
	share = clamp01(share)
	return supplyLinkOverlayRecord{
		CanonicalPath:  strings.TrimSpace(link.CanonicalPath),
		SupplierPath:   supplier,
		CustomerPath:   customer,
		SupplierGeoRef: canonicalGeoOverlayRef(supplier),
		CustomerGeoRef: canonicalGeoOverlayRef(customer),
		ProductPath:    product,
		TransportVia:   append([]string(nil), link.TransportViaRefs...),
		Confidence:     clamp01(confidence),
		Share:          share,
		Volume:         metricValue(link, "volume"),
		Contract:       supplyLinkContractStatus(link),
	}, true
}

func supplyLinkOverlayKeys(rec supplyLinkOverlayRecord) []string {
	keys := []string{rec.SupplierPath, rec.CustomerPath}
	if rec.SupplierGeoRef != "" {
		keys = append(keys, rec.SupplierGeoRef)
	}
	if rec.CustomerGeoRef != "" {
		keys = append(keys, rec.CustomerGeoRef)
	}
	for _, via := range rec.TransportVia {
		via = strings.TrimSpace(via)
		if via != "" {
			keys = append(keys, via)
		}
	}
	keys = append(keys, rec.AffectedZoneRefs...)
	return uniqPathList(keys)
}

func (e *PropagationEngine) getSupplyLinkVirtualNeighbors(
	ctx context.Context,
	frag *horizonmodels.HorizonFragment,
	strongSpecs map[horizonmodels.DependencyType]*horizonmodels.StrongEdgeSpec,
	allowSourceToTarget bool,
	allowTargetToSource bool,
) ([]neighborInfo, error) {
	if e == nil || frag == nil || e.fragmentsRepo == nil {
		return nil, nil
	}
	currentPath := strings.TrimSpace(frag.CanonicalPath)
	if currentPath == "" {
		return nil, nil
	}
	idx, err := e.ensureSupplyLinkOverlay(ctx)
	if err != nil || idx == nil {
		return nil, err
	}
	idx.mu.RLock()
	recs := append([]supplyLinkOverlayRecord(nil), idx.byPath[currentPath]...)
	idx.mu.RUnlock()
	if len(recs) == 0 {
		loaded, lerr := e.loadSupplyLinkOverlayRecordsForPath(ctx, currentPath)
		if lerr != nil {
			return nil, lerr
		}
		idx.mu.Lock()
		if idx.byPath == nil {
			idx.byPath = make(map[string][]supplyLinkOverlayRecord)
		}
		if _, exists := idx.byPath[currentPath]; !exists {
			idx.byPath[currentPath] = append([]supplyLinkOverlayRecord(nil), loaded...)
		}
		recs = append([]supplyLinkOverlayRecord(nil), idx.byPath[currentPath]...)
		idx.mu.Unlock()
	}
	if len(recs) == 0 {
		return nil, nil
	}

	cache := map[string]*horizonmodels.HorizonFragment{
		currentPath: frag,
	}
	neighbors := make([]neighborInfo, 0, len(recs)*2)
	for _, rec := range recs {
		for _, traversal := range buildSupplyLinkVirtualTraversals(currentPath, rec) {
			spec := strongSpecs[traversal.edgeType]
			if spec == nil {
				continue
			}
			direction := normalizeStrongEdgeDirection(spec.Direction)
			if traversal.traversalDirection == "source_to_target" {
				if !allowSourceToTarget || (direction != "source_to_target" && direction != "both") {
					continue
				}
			} else {
				if !allowTargetToSource || (direction != "target_to_source" && direction != "both") {
					continue
				}
			}
			targetFrag, err := e.loadFragmentByPathCached(ctx, cache, traversal.targetPath)
			if err != nil || targetFrag == nil || strings.TrimSpace(targetFrag.ID) == "" {
				continue
			}
			virtualEdge := buildSupplyLinkVirtualEdge(frag, targetFrag, rec, traversal)
			if virtualEdge == nil {
				continue
			}
			neighbors = append(neighbors, neighborInfo{
				fragmentID:         targetFrag.ID,
				edge:               virtualEdge,
				spec:               spec,
				traversalDirection: traversal.traversalDirection,
			})
		}
	}
	return neighbors, nil
}

func (e *PropagationEngine) loadSupplyLinkOverlayRecordsForPath(ctx context.Context, currentPath string) ([]supplyLinkOverlayRecord, error) {
	if e == nil || e.fragmentsRepo == nil {
		return nil, nil
	}
	currentPath = strings.TrimSpace(currentPath)
	if currentPath == "" {
		return nil, nil
	}
	linksByID := make(map[string]*horizonmodels.HorizonFragment)
	addLinks := func(items []*horizonmodels.HorizonFragment) {
		for _, item := range items {
			if item == nil || strings.TrimSpace(item.ID) == "" {
				continue
			}
			linksByID[item.ID] = item
		}
	}
	if strings.HasPrefix(currentPath, "/org/") || strings.HasPrefix(currentPath, "/gov/") || strings.HasPrefix(currentPath, "/fac/") || strings.HasPrefix(currentPath, "/deposit/") {
		if items, err := e.fragmentsRepo.FindSupplyLinksByCustomer(ctx, currentPath, "global", 256); err != nil {
			return nil, err
		} else {
			addLinks(items)
		}
		if items, err := e.fragmentsRepo.FindSupplyLinksBySupplier(ctx, currentPath, "global", 256); err != nil {
			return nil, err
		} else {
			addLinks(items)
		}
	}
	if strings.HasPrefix(currentPath, "/fac/") {
		if items, err := e.fragmentsRepo.FindSupplyLinksByTransportVia(ctx, currentPath, "global", 256); err != nil {
			return nil, err
		} else {
			addLinks(items)
		}
	}
	if len(linksByID) == 0 {
		return nil, nil
	}
	links := make([]*horizonmodels.HorizonFragment, 0, len(linksByID))
	for _, link := range linksByID {
		links = append(links, link)
	}
	binder := pathbinding.NewEngine(e.fragmentsRepo, e.edgesRepo)
	byPath := buildSupplyLinkOverlayRecords(ctx, links, binder)
	return append([]supplyLinkOverlayRecord(nil), byPath[currentPath]...), nil
}

func buildSupplyLinkVirtualTraversals(currentPath string, rec supplyLinkOverlayRecord) []supplyLinkVirtualTraversal {
	out := make([]supplyLinkVirtualTraversal, 0, 8)
	currentPath = strings.TrimSpace(currentPath)
	if currentPath == "" {
		return out
	}
	supplyQualifiers := buildSupplyLinkQualifiers(rec, "supply")
	logisticsQualifiers := buildSupplyLinkQualifiers(rec, "logistics")
	pressureQualifiers := buildSupplyLinkQualifiers(rec, "pressure")
	if currentPath == rec.SupplierPath {
		out = append(out, supplyLinkVirtualTraversal{
			targetPath:         rec.CustomerPath,
			edgeType:           horizonmodels.DependencyTypeSuppliesTo,
			traversalDirection: "source_to_target",
			qualifiers:         supplyQualifiers,
			criticality:        supplyLinkCriticality(rec, false),
		})
	}
	if currentPath == rec.CustomerPath {
		out = append(out, supplyLinkVirtualTraversal{
			targetPath:         rec.SupplierPath,
			edgeType:           horizonmodels.DependencyTypeSuppliesTo,
			traversalDirection: "target_to_source",
			qualifiers:         supplyQualifiers,
			criticality:        supplyLinkCriticality(rec, false),
		})
		for _, via := range rec.TransportVia {
			via = strings.TrimSpace(via)
			if via == "" {
				continue
			}
			out = append(out, supplyLinkVirtualTraversal{
				targetPath:         via,
				edgeType:           horizonmodels.DependencyTypeDependsOn,
				traversalDirection: "source_to_target",
				qualifiers:         logisticsQualifiers,
				criticality:        supplyLinkCriticality(rec, true),
			})
		}
	}
	for _, via := range rec.TransportVia {
		via = strings.TrimSpace(via)
		if via == "" || via != currentPath {
			continue
		}
		out = append(out, supplyLinkVirtualTraversal{
			targetPath:         rec.CustomerPath,
			edgeType:           horizonmodels.DependencyTypeDependsOn,
			traversalDirection: "target_to_source",
			qualifiers:         logisticsQualifiers,
			criticality:        supplyLinkCriticality(rec, true),
		})
	}
	if currentPath == rec.SupplierGeoRef {
		out = append(out, supplyLinkVirtualTraversal{
			targetPath:         rec.SupplierPath,
			edgeType:           horizonmodels.DependencyTypeAffects,
			traversalDirection: "source_to_target",
			qualifiers:         pressureQualifiers,
			criticality:        supplyLinkCriticality(rec, true),
		})
		if len(rec.TransportVia) > 0 {
			firstVia := strings.TrimSpace(rec.TransportVia[0])
			if firstVia != "" {
				out = append(out, supplyLinkVirtualTraversal{
					targetPath:         firstVia,
					edgeType:           horizonmodels.DependencyTypeAffects,
					traversalDirection: "source_to_target",
					qualifiers:         pressureQualifiers,
					criticality:        supplyLinkCriticality(rec, true),
				})
			}
		}
	}
	if currentPath == rec.CustomerGeoRef {
		out = append(out, supplyLinkVirtualTraversal{
			targetPath:         rec.CustomerPath,
			edgeType:           horizonmodels.DependencyTypeAffects,
			traversalDirection: "source_to_target",
			qualifiers:         pressureQualifiers,
			criticality:        supplyLinkCriticality(rec, true),
		})
	}
	if stringInList(currentPath, rec.AffectedZoneRefs) {
		for _, via := range rec.TransportVia {
			via = strings.TrimSpace(via)
			if via == "" {
				continue
			}
			out = append(out, supplyLinkVirtualTraversal{
				targetPath:         via,
				edgeType:           horizonmodels.DependencyTypeAffects,
				traversalDirection: "source_to_target",
				qualifiers:         pressureQualifiers,
				criticality:        supplyLinkCriticality(rec, true),
			})
		}
		out = append(out, supplyLinkVirtualTraversal{
			targetPath:         rec.CustomerPath,
			edgeType:           horizonmodels.DependencyTypeAffects,
			traversalDirection: "source_to_target",
			qualifiers:         pressureQualifiers,
			criticality:        supplyLinkCriticality(rec, true),
		})
	}
	return out
}

func canonicalGeoOverlayRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	if len(parts) < 3 || len(parts[1]) != 2 {
		return ""
	}
	code := strings.ToLower(strings.TrimSpace(parts[1]))
	if code == "na" || code == "xx" || code == "zz" {
		return ""
	}
	return "/loc/" + code
}

func stringInList(want string, items []string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func buildSupplyLinkVirtualEdge(
	currentFrag *horizonmodels.HorizonFragment,
	targetFrag *horizonmodels.HorizonFragment,
	rec supplyLinkOverlayRecord,
	traversal supplyLinkVirtualTraversal,
) *horizonmodels.DependencyEdge {
	if currentFrag == nil || targetFrag == nil {
		return nil
	}
	sourceID := currentFrag.ID
	targetID := targetFrag.ID
	if traversal.traversalDirection == "target_to_source" {
		sourceID, targetID = targetFrag.ID, currentFrag.ID
	}
	edge := &horizonmodels.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: strings.TrimSpace(sourceID),
		TargetFragmentID: strings.TrimSpace(targetID),
		Type:             traversal.edgeType,
		Properties: horizonmodels.EdgeProperties{
			Criticality:   clamp01(traversal.criticality),
			Amplification: 1.0,
			LatencyHours:  supplyLinkLatencyHours(rec, traversal.edgeType),
		},
		Qualifiers: cloneQualifierMap(traversal.qualifiers),
	}
	if edge.Qualifiers == nil {
		edge.Qualifiers = map[string]interface{}{}
	}
	edge.Qualifiers["virtual_supply_link_overlay"] = true
	edge.Qualifiers["supply_link_path"] = rec.CanonicalPath
	edge.Qualifiers["product_ref"] = rec.ProductPath
	edge.Qualifiers["sourceCanonicalPath"] = rec.SupplierPath
	edge.Qualifiers["targetCanonicalPath"] = rec.CustomerPath
	if len(rec.TransportVia) > 0 {
		edge.Qualifiers[horizonmodels.QualifierTransportVia] = append([]string(nil), rec.TransportVia...)
	}
	return edge
}

func buildSupplyLinkQualifiers(rec supplyLinkOverlayRecord, mode string) map[string]interface{} {
	out := map[string]interface{}{
		"fromSupplyChain":          true,
		"from_supply_link_overlay": true,
		"product_ref":              rec.ProductPath,
	}
	switch strings.TrimSpace(mode) {
	case "logistics":
		out["facet"] = string(horizonmodels.FacetLogistics)
		if len(rec.TransportVia) > 0 {
			out[horizonmodels.QualifierTransportVia] = append([]string(nil), rec.TransportVia...)
		}
		if likelyConstrainedRoute(rec.TransportVia) {
			out[horizonmodels.QualifierReroutePenalty] = 0.42
			out[horizonmodels.QualifierCapacityConstraint] = 0.24
		}
	case "supply":
		out["facet"] = string(horizonmodels.FacetSupply)
	case "pressure":
		out["facet"] = string(horizonmodels.FacetLogistics)
		out["pressure_transfer"] = true
		if len(rec.TransportVia) > 0 {
			out[horizonmodels.QualifierTransportVia] = append([]string(nil), rec.TransportVia...)
		}
	default:
		out["facet"] = string(horizonmodels.FacetSupply)
	}
	if rec.Share > 0 {
		out[horizonmodels.QualifierThroughputShare] = rec.Share
	}
	if rec.Contract != "" {
		out[horizonmodels.QualifierTransmissionContractVersion] = horizonmodels.TransmissionContractVersionV1
		switch rec.Contract {
		case "full":
			out[horizonmodels.QualifierTransmissionContractStatus] = horizonmodels.TransmissionContractStatusComplete
			out[horizonmodels.QualifierTransmissionContractMode] = horizonmodels.TransmissionContractModeExplicit
		case "partial":
			out[horizonmodels.QualifierTransmissionContractStatus] = horizonmodels.TransmissionContractStatusPartial
			out[horizonmodels.QualifierTransmissionContractMode] = horizonmodels.TransmissionContractModeExplicit
		default:
			out[horizonmodels.QualifierTransmissionContractStatus] = horizonmodels.TransmissionContractStatusMissing
			out[horizonmodels.QualifierTransmissionContractMode] = horizonmodels.TransmissionContractModeInferred
		}
	} else {
		out[horizonmodels.QualifierTransmissionContractVersion] = horizonmodels.TransmissionContractVersionV1
		out[horizonmodels.QualifierTransmissionContractStatus] = horizonmodels.TransmissionContractStatusMissing
		out[horizonmodels.QualifierTransmissionContractMode] = horizonmodels.TransmissionContractModeInferred
	}
	if strings.TrimSpace(rec.Mode) != "" {
		out[horizonmodels.QualifierRouteMode] = strings.TrimSpace(rec.Mode)
	}
	if len(rec.GeoRefs) > 0 {
		out[horizonmodels.QualifierGeoRefs] = append([]string(nil), rec.GeoRefs...)
	}
	if len(rec.FlowRefs) > 0 {
		out[horizonmodels.QualifierFlowRefs] = append([]string(nil), rec.FlowRefs...)
	}
	if len(rec.AlternateFlowRefs) > 0 {
		out[horizonmodels.QualifierAlternateFlowRefs] = append([]string(nil), rec.AlternateFlowRefs...)
	}
	if len(rec.ZoneRefs) > 0 {
		out[horizonmodels.QualifierZoneRefs] = append([]string(nil), rec.ZoneRefs...)
	}
	if len(rec.AffectedZoneRefs) > 0 {
		out[horizonmodels.QualifierAffectedZoneRefs] = append([]string(nil), rec.AffectedZoneRefs...)
	}
	if len(rec.SpatialSemantics) > 0 {
		out[horizonmodels.QualifierSpatialSemantics] = append([]string(nil), rec.SpatialSemantics...)
	}
	if rec.BindingConfidence > 0 {
		out[horizonmodels.QualifierBindingConfidence] = rec.BindingConfidence
	}
	if rec.StressScore > 0 {
		out[horizonmodels.QualifierPathStressScore] = rec.StressScore
	}
	if rec.Fragility > 0 {
		out[horizonmodels.QualifierPathFragility] = rec.Fragility
	}
	if rec.Rerouteability > 0 {
		out[horizonmodels.QualifierPathRerouteability] = rec.Rerouteability
	}
	if rec.ZonePressure > 0 {
		out[horizonmodels.QualifierPathZonePressure] = rec.ZonePressure
	}
	if rec.AffectedPressure > 0 {
		out[horizonmodels.QualifierPathAffectedPressure] = rec.AffectedPressure
	}
	if rec.RecoveryLatencyHrs > 0 {
		out[horizonmodels.QualifierRecoveryLatencyHours] = rec.RecoveryLatencyHrs
	}
	return out
}

func supplyLinkCriticality(rec supplyLinkOverlayRecord, logistics bool) float64 {
	score := 0.44 + rec.Confidence*0.18 + rec.Share*0.24
	if rec.Contract == "full" {
		score += 0.06
	}
	if logistics && len(rec.TransportVia) > 0 {
		score += 0.08
	}
	if likelyConstrainedRoute(rec.TransportVia) {
		score += 0.06
	}
	return clamp01(score)
}

func supplyLinkLatencyHours(rec supplyLinkOverlayRecord, edgeType horizonmodels.DependencyType) int {
	if edgeType == horizonmodels.DependencyTypeDependsOn {
		if len(rec.TransportVia) > 0 {
			return 18
		}
		return 12
	}
	if len(rec.TransportVia) > 0 {
		return 36
	}
	return 24
}

func likelyConstrainedRoute(paths []string) bool {
	for _, path := range paths {
		text := strings.ToLower(strings.TrimSpace(path))
		for _, token := range []string{
			"strait", "canal", "hormuz", "mandeb", "suez", "malacca", "bosporus", "dardanelles",
			"pipeline", "corridor", "port-of-", "terminal",
		} {
			if strings.Contains(text, token) {
				return true
			}
		}
	}
	return false
}

func (e *PropagationEngine) loadFragmentByPathCached(
	ctx context.Context,
	cache map[string]*horizonmodels.HorizonFragment,
	path string,
) (*horizonmodels.HorizonFragment, error) {
	path = strings.TrimSpace(path)
	if path == "" || e == nil || e.fragmentsRepo == nil {
		return nil, nil
	}
	if frag, ok := cache[path]; ok {
		return frag, nil
	}
	frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, path)
	if err != nil || frag == nil {
		fallback := supplyLinkOperationalFallbackPath(path)
		if fallback != "" && fallback != path {
			frag, err = e.fragmentsRepo.GetByCanonicalPath(ctx, fallback)
			if err == nil && frag != nil {
				cache[path] = frag
				return frag, nil
			}
		}
		cache[path] = nil
		return nil, err
	}
	cache[path] = frag
	return frag, nil
}

func supplyLinkOperationalFallbackPath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/org/") && !strings.HasPrefix(path, "/gov/") {
		return ""
	}
	return horizonmodels.NormalizeOrgRefToFacilityFallback(path)
}

func supplyLinkContractStatus(link *horizonmodels.HorizonFragment) string {
	if link == nil || len(link.Qualifiers) == 0 {
		return ""
	}
	raw, ok := link.Qualifiers["flow_edge_contract"].(map[string]interface{})
	if !ok {
		return ""
	}
	if status, ok := raw["contractCompleteness"].(string); ok {
		return strings.ToLower(strings.TrimSpace(status))
	}
	return ""
}

func metricValue(link *horizonmodels.HorizonFragment, key string) float64 {
	if link == nil || link.State == nil {
		return 0
	}
	mv, ok := link.State[strings.TrimSpace(key)]
	if !ok {
		return 0
	}
	return mv.Value
}

func firstNonEmptyPath(values ...*string) string {
	for _, value := range values {
		if s := strings.TrimSpace(derefString(value)); s != "" {
			return s
		}
	}
	return ""
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func uniqPathList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func cloneQualifierMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
