package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type SupplyChainLinkageSummary struct {
	OperationalEdges int  `json:"operationalEdges"`
	Legs             int  `json:"legs"`
	Linked           int  `json:"linked"`
	ResolvedIDs      int  `json:"resolvedIds"`
	Unresolved       int  `json:"unresolved"`
	Changed          bool `json:"changed"`
	FullyLinked      bool `json:"fullyLinked"`
}

func decodeSupplyChainPathNodes(raw interface{}) ([]SupplyChainPathNode, error) {
	switch nodes := raw.(type) {
	case []SupplyChainPathNode:
		return nodes, nil
	case []map[string]interface{}:
		out := make([]SupplyChainPathNode, 0, len(nodes))
		for i, node := range nodes {
			nodeJSON, err := json.Marshal(node)
			if err != nil {
				return nil, fmt.Errorf("payload node[%d] marshal failed: %w", i, err)
			}
			var parsed SupplyChainPathNode
			if err := json.Unmarshal(nodeJSON, &parsed); err != nil {
				return nil, fmt.Errorf("payload node[%d] unmarshal failed: %w", i, err)
			}
			out = append(out, parsed)
		}
		return out, nil
	case []interface{}:
		out := make([]SupplyChainPathNode, 0, len(nodes))
		for i, n := range nodes {
			switch node := n.(type) {
			case SupplyChainPathNode:
				out = append(out, node)
			case map[string]interface{}:
				nodeJSON, err := json.Marshal(node)
				if err != nil {
					return nil, fmt.Errorf("payload node[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainPathNode
				if err := json.Unmarshal(nodeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload node[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload node[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload 'nodes' must be an array")
	}
}

func decodeSupplyChainPathEdges(raw interface{}) ([]SupplyChainPathEdge, error) {
	switch edges := raw.(type) {
	case []SupplyChainPathEdge:
		return edges, nil
	case []map[string]interface{}:
		out := make([]SupplyChainPathEdge, 0, len(edges))
		for i, edge := range edges {
			edgeJSON, err := json.Marshal(edge)
			if err != nil {
				return nil, fmt.Errorf("payload edge[%d] marshal failed: %w", i, err)
			}
			var parsed SupplyChainPathEdge
			if err := json.Unmarshal(edgeJSON, &parsed); err != nil {
				return nil, fmt.Errorf("payload edge[%d] unmarshal failed: %w", i, err)
			}
			out = append(out, parsed)
		}
		return out, nil
	case []interface{}:
		out := make([]SupplyChainPathEdge, 0, len(edges))
		for i, e := range edges {
			switch edge := e.(type) {
			case SupplyChainPathEdge:
				out = append(out, edge)
			case map[string]interface{}:
				edgeJSON, err := json.Marshal(edge)
				if err != nil {
					return nil, fmt.Errorf("payload edge[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainPathEdge
				if err := json.Unmarshal(edgeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload edge[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload edge[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload 'edges' must be an array")
	}
}

func decodeSupplyChainPathLegs(raw interface{}) ([]SupplyChainPathLeg, error) {
	switch legs := raw.(type) {
	case []SupplyChainPathLeg:
		return legs, nil
	case []map[string]interface{}:
		out := make([]SupplyChainPathLeg, 0, len(legs))
		for i, leg := range legs {
			legJSON, err := json.Marshal(leg)
			if err != nil {
				return nil, fmt.Errorf("payload leg[%d] marshal failed: %w", i, err)
			}
			var parsed SupplyChainPathLeg
			if err := json.Unmarshal(legJSON, &parsed); err != nil {
				return nil, fmt.Errorf("payload leg[%d] unmarshal failed: %w", i, err)
			}
			out = append(out, parsed)
		}
		return out, nil
	case []interface{}:
		out := make([]SupplyChainPathLeg, 0, len(legs))
		for i, rawLeg := range legs {
			switch leg := rawLeg.(type) {
			case SupplyChainPathLeg:
				out = append(out, leg)
			case map[string]interface{}:
				legJSON, err := json.Marshal(leg)
				if err != nil {
					return nil, fmt.Errorf("payload leg[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainPathLeg
				if err := json.Unmarshal(legJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload leg[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload leg[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload 'legs' must be an array")
	}
}

func orderedRouteRefsForLeg(supplierRef string, transportViaRefs []string, customerRef string) []string {
	refs := make([]string, 0, len(transportViaRefs)+2)
	if strings.TrimSpace(supplierRef) != "" {
		refs = append(refs, strings.TrimSpace(supplierRef))
	}
	for _, ref := range transportViaRefs {
		if strings.TrimSpace(ref) != "" {
			refs = append(refs, strings.TrimSpace(ref))
		}
	}
	if strings.TrimSpace(customerRef) != "" {
		refs = append(refs, strings.TrimSpace(customerRef))
	}
	return NormalizeOrderedRouteRefs(refs)
}

func inferRouteOverlayEndpointBindingPolicy(supplierRef, customerRef string, transportViaRefs []string) string {
	isExactRef := func(ref string) bool {
		ref = strings.TrimSpace(ref)
		switch {
		case strings.HasPrefix(ref, "/fac/"),
			strings.HasPrefix(ref, "/deposit/"),
			strings.HasPrefix(ref, "/loc/"),
			strings.HasPrefix(ref, "/zone/"),
			strings.HasPrefix(ref, "/track/"):
			return true
		default:
			return false
		}
	}
	if isExactRef(supplierRef) && isExactRef(customerRef) {
		allExact := true
		for _, ref := range transportViaRefs {
			if !isExactRef(ref) {
				allExact = false
				break
			}
		}
		if allExact {
			return "exact_refs"
		}
	}
	if len(transportViaRefs) > 0 {
		return "anchored_via_refs"
	}
	return "approx_endpoint_binding"
}

func routeOverlayQualityRank(source string) int {
	switch strings.TrimSpace(source) {
	case "route_geometry_profile", "curated_flow_catalog":
		return 5
	case "composed_route_segments":
		return 4
	case "facility_route_overlay":
		return 3
	case "partial_segment_geometry":
		return 2
	case "fragmentary_waypoint_sequence":
		return 1
	default:
		return 0
	}
}

func buildSupplyChainRouteOverlayKey(overlay SupplyChainRouteOverlay) string {
	if key := strings.TrimSpace(overlay.OverlayKey); key != "" {
		return key
	}
	if key := strings.TrimSpace(overlay.RouteVariantKey); key != "" {
		return "variant:" + key
	}
	if key := strings.TrimSpace(overlay.RouteProfileKey); key != "" {
		return "profile:" + key
	}
	refs := NormalizeOrderedRouteRefs(overlay.OrderedRefs)
	if len(refs) > 0 {
		return "refs:" + BuildRouteGeometryProfileKey(refs)
	}
	if len(overlay.TransportViaRefs) > 0 {
		return "via:" + BuildSupplyLinkRouteVariantKey(overlay.TransportViaRefs)
	}
	parts := []string{
		strings.TrimSpace(overlay.Mode),
		strings.TrimSpace(overlay.RouteGeometrySource),
		strings.TrimSpace(overlay.VesselType),
	}
	key := strings.Trim(strings.Join(parts, "|"), "|")
	if key == "" {
		return ""
	}
	return "overlay:" + key
}

func normalizeSupplyChainRouteOverlay(overlay SupplyChainRouteOverlay, leg SupplyChainPathLeg) SupplyChainRouteOverlay {
	overlay.TransportViaRefs = uniqueTrimmedStrings(firstNonEmptyStringSlice(overlay.TransportViaRefs, leg.TransportViaRefs))
	overlay.OrderedRefs = NormalizeOrderedRouteRefs(firstNonEmptyStringSlice(overlay.OrderedRefs, orderedRouteRefsForLeg(leg.SupplierRef, overlay.TransportViaRefs, leg.CustomerRef)))
	if len(overlay.TransportViaRefs) > 0 || strings.TrimSpace(overlay.RouteVariantKey) == "" {
		overlay.RouteVariantKey = strings.TrimSpace(BuildSupplyLinkRouteVariantKey(overlay.TransportViaRefs))
	}
	if len(overlay.OrderedRefs) > 0 {
		overlay.RouteProfileKey = strings.TrimSpace(BuildRouteGeometryProfileKey(overlay.OrderedRefs))
	}
	overlay.RouteGeometrySource = strings.TrimSpace(firstNonEmpty(overlay.RouteGeometrySource, leg.RouteGeometrySource))
	overlay.Mode = strings.TrimSpace(firstNonEmpty(overlay.Mode, leg.Mode))
	overlay.FlowRefs = uniqueTrimmedStrings(overlay.FlowRefs)
	overlay.AlternateFlowRefs = uniqueTrimmedStrings(overlay.AlternateFlowRefs)
	overlay.GeoRefs = uniqueTrimmedStrings(overlay.GeoRefs)
	overlay.ZoneRefs = uniqueTrimmedStrings(overlay.ZoneRefs)
	overlay.AffectedZoneRefs = uniqueTrimmedStrings(overlay.AffectedZoneRefs)
	overlay.SpatialSemantics = uniqueTrimmedStrings(overlay.SpatialSemantics)
	overlay.VesselType = strings.TrimSpace(firstNonEmpty(overlay.VesselType, leg.VesselType))
	overlay.EndpointBindingPolicy = strings.TrimSpace(firstNonEmpty(
		overlay.EndpointBindingPolicy,
		inferRouteOverlayEndpointBindingPolicy(leg.SupplierRef, leg.CustomerRef, overlay.TransportViaRefs),
	))
	if strings.TrimSpace(overlay.Status) == "" {
		overlay.Status = "alternate"
	}
	overlay.OverlayKey = buildSupplyChainRouteOverlayKey(SupplyChainRouteOverlay{
		RouteVariantKey:     overlay.RouteVariantKey,
		RouteProfileKey:     overlay.RouteProfileKey,
		OrderedRefs:         overlay.OrderedRefs,
		TransportViaRefs:    overlay.TransportViaRefs,
		Mode:                overlay.Mode,
		RouteGeometrySource: overlay.RouteGeometrySource,
		VesselType:          overlay.VesselType,
	})
	return overlay
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func synthesizeLegacyRouteOverlay(leg SupplyChainPathLeg) *SupplyChainRouteOverlay {
	if strings.TrimSpace(leg.RouteVariantKey) == "" &&
		len(leg.TransportViaRefs) == 0 &&
		len(leg.RouteCoordinates) == 0 &&
		strings.TrimSpace(leg.RouteGeometrySource) == "" &&
		strings.TrimSpace(leg.Mode) == "" &&
		len(leg.FlowRefs) == 0 &&
		len(leg.ZoneRefs) == 0 &&
		len(leg.AffectedZoneRefs) == 0 &&
		len(leg.SpatialSemantics) == 0 &&
		strings.TrimSpace(leg.VesselType) == "" {
		return nil
	}
	overlay := normalizeSupplyChainRouteOverlay(SupplyChainRouteOverlay{
		RouteVariantKey:     strings.TrimSpace(leg.RouteVariantKey),
		RouteProfileKey:     BuildRouteGeometryProfileKey(orderedRouteRefsForLeg(leg.SupplierRef, leg.TransportViaRefs, leg.CustomerRef)),
		OrderedRefs:         orderedRouteRefsForLeg(leg.SupplierRef, leg.TransportViaRefs, leg.CustomerRef),
		TransportViaRefs:    append([]string(nil), leg.TransportViaRefs...),
		RouteCoordinates:    append([]Coordinates(nil), leg.RouteCoordinates...),
		RouteGeometrySource: strings.TrimSpace(leg.RouteGeometrySource),
		Mode:                strings.TrimSpace(leg.Mode),
		FlowRefs:            append([]string(nil), leg.FlowRefs...),
		AlternateFlowRefs:   append([]string(nil), leg.AlternateFlowRefs...),
		GeoRefs:             append([]string(nil), leg.GeoRefs...),
		ZoneRefs:            append([]string(nil), leg.ZoneRefs...),
		AffectedZoneRefs:    append([]string(nil), leg.AffectedZoneRefs...),
		SpatialSemantics:    append([]string(nil), leg.SpatialSemantics...),
		VesselType:          strings.TrimSpace(leg.VesselType),
		BindingConfidence:   leg.BindingConfidence,
		SegmentConfidence:   leg.SegmentConfidence,
		Status:              "primary",
	}, leg)
	return &overlay
}

func selectPrimarySupplyChainRouteOverlay(overlays []SupplyChainRouteOverlay) int {
	if len(overlays) == 0 {
		return -1
	}
	bestIdx := 0
	bestScore := -1.0
	for idx, overlay := range overlays {
		score := overlay.BindingConfidence*10 + overlay.SegmentConfidence*4
		score += float64(routeOverlayQualityRank(overlay.RouteGeometrySource)) * 3
		score += float64(len(overlay.FlowRefs)) * 0.6
		if len(overlay.RouteCoordinates) >= 2 {
			score += 0.8
		}
		if strings.TrimSpace(overlay.Status) == "primary" {
			score += 1.5
		}
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}
	return bestIdx
}

// SupplyChainRouteOverlayConstraints describes route conditions that make one overlay viable and another blocked.
// This is the reusable selection contract for risk, simulation, QA, and on-demand route reasoning.
type SupplyChainRouteOverlayConstraints struct {
	BlockedFlowRefs         []string
	BlockedZoneRefs         []string
	BlockedAffectedZoneRefs []string
	BlockedGeoRefs          []string
	BlockedModes            []string
}

// SupplyChainRouteOverlaySelection groups overlays by whether they remain usable under the supplied constraints.
type SupplyChainRouteOverlaySelection struct {
	Primary *SupplyChainRouteOverlay
	Viable  []SupplyChainRouteOverlay
	Blocked []SupplyChainRouteOverlay
}

func routeOverlayBlockedByConstraints(overlay SupplyChainRouteOverlay, constraints SupplyChainRouteOverlayConstraints) bool {
	intersects := func(left, right []string) bool {
		if len(left) == 0 || len(right) == 0 {
			return false
		}
		seen := make(map[string]struct{}, len(right))
		for _, item := range right {
			item = strings.TrimSpace(item)
			if item != "" {
				seen[item] = struct{}{}
			}
		}
		for _, item := range left {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				return true
			}
		}
		return false
	}
	if intersects(overlay.FlowRefs, constraints.BlockedFlowRefs) {
		return true
	}
	if intersects(overlay.ZoneRefs, constraints.BlockedZoneRefs) {
		return true
	}
	if intersects(overlay.AffectedZoneRefs, constraints.BlockedAffectedZoneRefs) {
		return true
	}
	if intersects(overlay.GeoRefs, constraints.BlockedGeoRefs) {
		return true
	}
	for _, mode := range constraints.BlockedModes {
		if strings.TrimSpace(mode) != "" && strings.EqualFold(strings.TrimSpace(overlay.Mode), strings.TrimSpace(mode)) {
			return true
		}
	}
	return false
}

// SelectSupplyChainRouteOverlays evaluates a leg's route overlays against route constraints and returns
// viable/blocked alternatives plus the preferred viable overlay when one exists.
func SelectSupplyChainRouteOverlays(leg SupplyChainPathLeg, constraints SupplyChainRouteOverlayConstraints) SupplyChainRouteOverlaySelection {
	normalizeSupplyChainPathLegRouteOverlays(&leg)
	selection := SupplyChainRouteOverlaySelection{
		Viable:  make([]SupplyChainRouteOverlay, 0, len(leg.RouteOverlays)),
		Blocked: make([]SupplyChainRouteOverlay, 0, len(leg.RouteOverlays)),
	}
	for _, overlay := range leg.RouteOverlays {
		if routeOverlayBlockedByConstraints(overlay, constraints) {
			selection.Blocked = append(selection.Blocked, overlay)
			continue
		}
		selection.Viable = append(selection.Viable, overlay)
	}
	if idx := selectPrimarySupplyChainRouteOverlay(selection.Viable); idx >= 0 && idx < len(selection.Viable) {
		primary := selection.Viable[idx]
		selection.Primary = &primary
	}
	return selection
}

func normalizeSupplyChainPathLegRouteOverlays(leg *SupplyChainPathLeg) {
	if leg == nil {
		return
	}
	overlays := make([]SupplyChainRouteOverlay, 0, len(leg.RouteOverlays)+1)
	indexByKey := make(map[string]int, len(leg.RouteOverlays)+1)
	appendOverlay := func(overlay SupplyChainRouteOverlay) {
		overlay = normalizeSupplyChainRouteOverlay(overlay, *leg)
		if strings.TrimSpace(overlay.OverlayKey) == "" {
			return
		}
		if idx, ok := indexByKey[overlay.OverlayKey]; ok {
			current := overlays[idx]
			current.OrderedRefs = NormalizeOrderedRouteRefs(firstNonEmptyStringSlice(current.OrderedRefs, overlay.OrderedRefs))
			current.TransportViaRefs = mergeUniqueStrings(current.TransportViaRefs, overlay.TransportViaRefs)
			if len(current.RouteCoordinates) == 0 && len(overlay.RouteCoordinates) > 0 {
				current.RouteCoordinates = append([]Coordinates(nil), overlay.RouteCoordinates...)
			}
			if strings.TrimSpace(current.RouteGeometrySource) == "" {
				current.RouteGeometrySource = strings.TrimSpace(overlay.RouteGeometrySource)
			}
			if strings.TrimSpace(current.Mode) == "" {
				current.Mode = strings.TrimSpace(overlay.Mode)
			}
			current.FlowRefs = mergeUniqueStrings(current.FlowRefs, overlay.FlowRefs)
			current.AlternateFlowRefs = mergeUniqueStrings(current.AlternateFlowRefs, overlay.AlternateFlowRefs)
			current.GeoRefs = mergeUniqueStrings(current.GeoRefs, overlay.GeoRefs)
			current.ZoneRefs = mergeUniqueStrings(current.ZoneRefs, overlay.ZoneRefs)
			current.AffectedZoneRefs = mergeUniqueStrings(current.AffectedZoneRefs, overlay.AffectedZoneRefs)
			current.SpatialSemantics = mergeUniqueStrings(current.SpatialSemantics, overlay.SpatialSemantics)
			if strings.TrimSpace(current.VesselType) == "" {
				current.VesselType = strings.TrimSpace(overlay.VesselType)
			}
			if current.BindingConfidence <= 0 && overlay.BindingConfidence > 0 {
				current.BindingConfidence = overlay.BindingConfidence
			}
			if current.SegmentConfidence <= 0 && overlay.SegmentConfidence > 0 {
				current.SegmentConfidence = overlay.SegmentConfidence
			}
			if strings.TrimSpace(current.EndpointBindingPolicy) == "" {
				current.EndpointBindingPolicy = strings.TrimSpace(overlay.EndpointBindingPolicy)
			}
			if strings.TrimSpace(current.Status) == "" {
				current.Status = strings.TrimSpace(overlay.Status)
			}
			overlays[idx] = normalizeSupplyChainRouteOverlay(current, *leg)
			return
		}
		indexByKey[overlay.OverlayKey] = len(overlays)
		overlays = append(overlays, overlay)
	}
	if legacy := synthesizeLegacyRouteOverlay(*leg); legacy != nil {
		appendOverlay(*legacy)
	}
	for _, overlay := range leg.RouteOverlays {
		appendOverlay(overlay)
	}
	primaryIdx := selectPrimarySupplyChainRouteOverlay(overlays)
	for idx := range overlays {
		if idx == primaryIdx {
			overlays[idx].Status = "primary"
		} else if strings.TrimSpace(overlays[idx].Status) == "" || strings.TrimSpace(overlays[idx].Status) == "primary" {
			overlays[idx].Status = "alternate"
		}
	}
	leg.RouteOverlays = overlays
	if primaryIdx >= 0 {
		primary := overlays[primaryIdx]
		leg.RouteVariantKey = strings.TrimSpace(primary.RouteVariantKey)
		leg.TransportViaRefs = append([]string(nil), primary.TransportViaRefs...)
		leg.RouteCoordinates = append([]Coordinates(nil), primary.RouteCoordinates...)
		leg.RouteGeometrySource = strings.TrimSpace(primary.RouteGeometrySource)
		leg.VesselType = strings.TrimSpace(primary.VesselType)
		leg.Mode = strings.TrimSpace(firstNonEmpty(primary.Mode, leg.Mode))
	}
	leg.FlowRefs = nil
	leg.AlternateFlowRefs = nil
	leg.GeoRefs = nil
	leg.ZoneRefs = nil
	leg.AffectedZoneRefs = nil
	leg.SpatialSemantics = nil
	for _, overlay := range overlays {
		leg.FlowRefs = mergeUniqueStrings(leg.FlowRefs, overlay.FlowRefs)
		leg.AlternateFlowRefs = mergeUniqueStrings(leg.AlternateFlowRefs, overlay.AlternateFlowRefs)
		leg.GeoRefs = mergeUniqueStrings(leg.GeoRefs, overlay.GeoRefs)
		leg.ZoneRefs = mergeUniqueStrings(leg.ZoneRefs, overlay.ZoneRefs)
		leg.AffectedZoneRefs = mergeUniqueStrings(leg.AffectedZoneRefs, overlay.AffectedZoneRefs)
		leg.SpatialSemantics = mergeUniqueStrings(leg.SpatialSemantics, overlay.SpatialSemantics)
		if leg.BindingConfidence <= 0 && overlay.BindingConfidence > 0 {
			leg.BindingConfidence = overlay.BindingConfidence
		}
		if leg.SegmentConfidence <= 0 && overlay.SegmentConfidence > 0 {
			leg.SegmentConfidence = overlay.SegmentConfidence
		}
	}
}

func normalizeParsedSupplyChainPathLegs(legs []SupplyChainPathLeg) []SupplyChainPathLeg {
	if len(legs) == 0 {
		return legs
	}
	merged := mergePathLegs(legs, nil)
	for idx := range merged {
		normalizeSupplyChainPathLegRouteOverlays(&merged[idx])
	}
	return merged
}

func rawSupplyChainPathLegMaps(raw interface{}) []map[string]interface{} {
	switch value := raw.(type) {
	case []map[string]interface{}:
		return value
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			legMap, ok := item.(map[string]interface{})
			if !ok {
				return nil
			}
			out = append(out, legMap)
		}
		return out
	default:
		return nil
	}
}

func rawSupplyChainPathLegHasExplicitRouteOverlays(leg map[string]interface{}) bool {
	if len(leg) == 0 {
		return false
	}
	if _, ok := leg["routeOverlays"]; ok {
		return true
	}
	if _, ok := leg["RouteOverlays"]; ok {
		return true
	}
	return false
}

func supplyChainPathPayloadAlreadyNormalized(payload map[string]interface{}) bool {
	if len(payload) == 0 {
		return false
	}
	rawLegsValue, ok := payload["legs"]
	if !ok {
		rawLegsValue = payload["Legs"]
	}
	if rawLegsValue == nil {
		return false
	}
	rawLegs, err := decodeSupplyChainPathLegs(rawLegsValue)
	if err != nil || len(rawLegs) == 0 {
		return false
	}
	normalizedLegs := normalizeParsedSupplyChainPathLegs(append([]SupplyChainPathLeg(nil), rawLegs...))
	if len(rawLegs) != len(normalizedLegs) {
		return false
	}
	rawLegMaps := rawSupplyChainPathLegMaps(rawLegsValue)
	if len(rawLegMaps) != len(rawLegs) {
		return false
	}
	for _, legMap := range rawLegMaps {
		if !rawSupplyChainPathLegHasExplicitRouteOverlays(legMap) {
			return false
		}
	}
	return true
}

func decodeStringSlice(raw interface{}) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...), nil
	case []interface{}:
		out := make([]string, 0, len(values))
		for i, value := range values {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text == "<nil>" {
				return nil, fmt.Errorf("payload string[%d] must not be nil", i)
			}
			if text != "" {
				out = append(out, text)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload value must be a string array")
	}
}

func decodeSupplyChainAssemblyNodes(raw interface{}) ([]SupplyChainAssemblyNode, error) {
	switch nodes := raw.(type) {
	case nil:
		return nil, nil
	case []SupplyChainAssemblyNode:
		return nodes, nil
	case []interface{}:
		out := make([]SupplyChainAssemblyNode, 0, len(nodes))
		for i, n := range nodes {
			switch node := n.(type) {
			case SupplyChainAssemblyNode:
				out = append(out, node)
			case map[string]interface{}:
				nodeJSON, err := json.Marshal(node)
				if err != nil {
					return nil, fmt.Errorf("payload assembly node[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainAssemblyNode
				if err := json.Unmarshal(nodeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload assembly node[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload assembly node[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload assembly nodes must be an array")
	}
}

func decodeSupplyChainAssemblyAnchorComponents(raw interface{}) ([]SupplyChainAssemblyAnchorComponent, error) {
	switch components := raw.(type) {
	case nil:
		return nil, nil
	case []SupplyChainAssemblyAnchorComponent:
		return components, nil
	case []interface{}:
		out := make([]SupplyChainAssemblyAnchorComponent, 0, len(components))
		for i, value := range components {
			switch component := value.(type) {
			case SupplyChainAssemblyAnchorComponent:
				out = append(out, component)
			case map[string]interface{}:
				componentJSON, err := json.Marshal(component)
				if err != nil {
					return nil, fmt.Errorf("payload assembly anchorComponent[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainAssemblyAnchorComponent
				if err := json.Unmarshal(componentJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload assembly anchorComponent[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload assembly anchorComponent[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload assembly anchorComponents must be an array")
	}
}

func decodeSupplyChainAssemblySupplyEdges(raw interface{}) ([]SupplyChainAssemblySupplyEdge, error) {
	switch edges := raw.(type) {
	case nil:
		return nil, nil
	case []SupplyChainAssemblySupplyEdge:
		return edges, nil
	case []interface{}:
		out := make([]SupplyChainAssemblySupplyEdge, 0, len(edges))
		for i, value := range edges {
			switch edge := value.(type) {
			case SupplyChainAssemblySupplyEdge:
				out = append(out, edge)
			case map[string]interface{}:
				edgeJSON, err := json.Marshal(edge)
				if err != nil {
					return nil, fmt.Errorf("payload assembly supplyEdge[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainAssemblySupplyEdge
				if err := json.Unmarshal(edgeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload assembly supplyEdge[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload assembly supplyEdge[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload assembly supplyEdges must be an array")
	}
}

func decodeSupplyChainAssemblyTransformEdges(raw interface{}) ([]SupplyChainAssemblyTransformEdge, error) {
	switch edges := raw.(type) {
	case nil:
		return nil, nil
	case []SupplyChainAssemblyTransformEdge:
		return edges, nil
	case []interface{}:
		out := make([]SupplyChainAssemblyTransformEdge, 0, len(edges))
		for i, value := range edges {
			switch edge := value.(type) {
			case SupplyChainAssemblyTransformEdge:
				out = append(out, edge)
			case map[string]interface{}:
				edgeJSON, err := json.Marshal(edge)
				if err != nil {
					return nil, fmt.Errorf("payload assembly transformEdge[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainAssemblyTransformEdge
				if err := json.Unmarshal(edgeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload assembly transformEdge[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload assembly transformEdge[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload assembly transformEdges must be an array")
	}
}

func decodeSupplyChainAssemblyRouteBindings(raw interface{}) ([]SupplyChainAssemblyRouteBinding, error) {
	switch bindings := raw.(type) {
	case nil:
		return nil, nil
	case []SupplyChainAssemblyRouteBinding:
		return bindings, nil
	case []interface{}:
		out := make([]SupplyChainAssemblyRouteBinding, 0, len(bindings))
		for i, value := range bindings {
			switch binding := value.(type) {
			case SupplyChainAssemblyRouteBinding:
				out = append(out, binding)
			case map[string]interface{}:
				bindingJSON, err := json.Marshal(binding)
				if err != nil {
					return nil, fmt.Errorf("payload assembly routeBinding[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainAssemblyRouteBinding
				if err := json.Unmarshal(bindingJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload assembly routeBinding[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload assembly routeBinding[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload assembly routeBindings must be an array")
	}
}

func decodeSupplyChainAssemblyCoverage(raw interface{}) (*SupplyChainAssemblyCoverage, error) {
	if raw == nil {
		return nil, nil
	}
	switch typed := raw.(type) {
	case *SupplyChainAssemblyCoverage:
		return typed, nil
	case SupplyChainAssemblyCoverage:
		copy := typed
		return &copy, nil
	case map[string]interface{}:
		coverageJSON, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("payload assembly coverage marshal failed: %w", err)
		}
		var parsed SupplyChainAssemblyCoverage
		if err := json.Unmarshal(coverageJSON, &parsed); err != nil {
			return nil, fmt.Errorf("payload assembly coverage unmarshal failed: %w", err)
		}
		return &parsed, nil
	default:
		return nil, fmt.Errorf("payload assembly coverage must be an object")
	}
}

func decodeSupplyChainAssemblyBranchSummaries(raw interface{}) ([]SupplyChainAssemblyBranchSummary, error) {
	switch branches := raw.(type) {
	case nil:
		return nil, nil
	case []SupplyChainAssemblyBranchSummary:
		return branches, nil
	case []interface{}:
		out := make([]SupplyChainAssemblyBranchSummary, 0, len(branches))
		for i, value := range branches {
			switch branch := value.(type) {
			case SupplyChainAssemblyBranchSummary:
				out = append(out, branch)
			case map[string]interface{}:
				branchJSON, err := json.Marshal(branch)
				if err != nil {
					return nil, fmt.Errorf("payload assembly branch[%d] marshal failed: %w", i, err)
				}
				var parsed SupplyChainAssemblyBranchSummary
				if err := json.Unmarshal(branchJSON, &parsed); err != nil {
					return nil, fmt.Errorf("payload assembly branch[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("payload assembly branch[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("payload assembly branches must be an array")
	}
}

// BuildSupplyChainPathPayload creates a v1 SUPPLY_CHAIN_PATH payload from nodes, edges, optional legs, and stress profile.
func BuildSupplyChainPathPayload(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge, legs []SupplyChainPathLeg, stress *SupplyChainPathStress, notes map[string]interface{}) map[string]interface{} {
	return BuildSupplyChainPathPayloadWithSufficiency(nodes, edges, legs, stress, nil, notes)
}

func BuildSupplyChainPathPayloadWithSufficiency(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge, legs []SupplyChainPathLeg, stress *SupplyChainPathStress, sufficiency *SupplyChainPathSufficiency, notes map[string]interface{}) map[string]interface{} {
	return BuildSupplyChainPathPayloadDetailed(nodes, edges, legs, stress, sufficiency, nil, notes)
}

func BuildSupplyChainPathPayloadDetailed(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge, legs []SupplyChainPathLeg, stress *SupplyChainPathStress, sufficiency *SupplyChainPathSufficiency, family *SupplyChainPathFamily, notes map[string]interface{}) map[string]interface{} {
	if len(legs) > 0 {
		legs = normalizeParsedSupplyChainPathLegs(append([]SupplyChainPathLeg(nil), legs...))
	}
	payload := map[string]interface{}{
		"v":     1,
		"nodes": nodes,
	}
	if len(edges) > 0 {
		payload["edges"] = edges
	}
	if len(legs) > 0 {
		payload["legs"] = legs
	}
	if stress != nil {
		payload["stress"] = stress
	}
	if metrics := buildSupplyChainPathPayloadMetrics(legs, stress); len(metrics) > 0 {
		payload["metrics"] = metrics
	}
	if sufficiency != nil {
		payload["sufficiency"] = sufficiency
	}
	if family != nil {
		payload["family"] = family
	}
	if len(notes) > 0 {
		payload["notes"] = notes
	}
	return payload
}

func buildSupplyChainPathPayloadMetrics(legs []SupplyChainPathLeg, stress *SupplyChainPathStress) map[string]interface{} {
	if len(legs) == 0 && stress == nil {
		return nil
	}
	metrics := map[string]interface{}{}
	if len(legs) > 0 {
		var bindingTotal float64
		for _, leg := range legs {
			bindingTotal += leg.BindingConfidence
		}
		metrics["legCount"] = len(legs)
		metrics["bindingConfidence"] = bindingTotal / float64(len(legs))
	}
	if stress != nil {
		metrics["pathStressScore"] = stress.PathScore
		metrics["fragility"] = stress.Fragility
		metrics["rerouteability"] = stress.Rerouteability
		metrics["zonePressure"] = stress.ZonePressure
		metrics["affectedZonePressure"] = stress.AffectedZonePressure
		metrics["recoveryLatencyHours"] = stress.RecoveryLatencyHrs
		metrics["bottleneckLegIndex"] = stress.BottleneckLegIndex
		if len(stress.DominantFactors) > 0 {
			metrics["dominantFactors"] = append([]string(nil), stress.DominantFactors...)
		}
	}
	if len(metrics) == 0 {
		return nil
	}
	return metrics
}

type SupplyChainAssemblyGraphPayloadOptions struct {
	AnchorSet                       []string
	SourceAnchors                   []string
	ResolvedAnchors                 []string
	UnresolvedAnchors               []string
	AnchorProducts                  []string
	AnchorComponents                []SupplyChainAssemblyAnchorComponent
	ProductNodes                    []SupplyChainAssemblyNode
	ActorNodes                      []SupplyChainAssemblyNode
	SupplyEdges                     []SupplyChainAssemblySupplyEdge
	TransformEdges                  []SupplyChainAssemblyTransformEdge
	AssemblyEdges                   []SupplyChainAssemblyTransformEdge
	RouteBindings                   []SupplyChainAssemblyRouteBinding
	Coverage                        *SupplyChainAssemblyCoverage
	OpenGaps                        []string
	DirectPathKeys                  []string
	DerivedPathKeys                 []string
	PrimaryBranches                 []SupplyChainAssemblyBranchSummary
	SecondaryBranches               []SupplyChainAssemblyBranchSummary
	TopRoutes                       []string
	TopDistributionNodes            []string
	TopSuppliers                    []string
	TopProductFamilies              []string
	SuppressedSupportBranches       []SupplyChainAssemblyBranchSummary
	Stage                           string
	Provisional                     bool
	SourceMode                      string
	BackgroundContinuationScheduled bool
	BackgroundContinuationJobIDs    []string
	Notes                           map[string]interface{}
}

func BuildSupplyChainAssemblyGraphPayload(
	anchorProducts []string,
	productNodes []SupplyChainAssemblyNode,
	actorNodes []SupplyChainAssemblyNode,
	supplyEdges []SupplyChainAssemblySupplyEdge,
	transformEdges []SupplyChainAssemblyTransformEdge,
	assemblyEdges []SupplyChainAssemblyTransformEdge,
	routeBindings []SupplyChainAssemblyRouteBinding,
	coverage *SupplyChainAssemblyCoverage,
	openGaps []string,
	derivedPathKeys []string,
	notes map[string]interface{},
) map[string]interface{} {
	return BuildSupplyChainAssemblyGraphPayloadDetailed(SupplyChainAssemblyGraphPayloadOptions{
		AnchorSet:       anchorProducts,
		SourceAnchors:   anchorProducts,
		ResolvedAnchors: anchorProducts,
		AnchorProducts:  anchorProducts,
		ProductNodes:    productNodes,
		ActorNodes:      actorNodes,
		SupplyEdges:     supplyEdges,
		TransformEdges:  transformEdges,
		AssemblyEdges:   assemblyEdges,
		RouteBindings:   routeBindings,
		Coverage:        coverage,
		OpenGaps:        openGaps,
		DerivedPathKeys: derivedPathKeys,
		Stage:           "refined",
		SourceMode:      "supply_links",
		Notes:           notes,
	})
}

func BuildSupplyChainAssemblyGraphPayloadDetailed(opts SupplyChainAssemblyGraphPayloadOptions) map[string]interface{} {
	payload := map[string]interface{}{
		"v": 1,
	}
	if len(opts.AnchorSet) > 0 {
		payload["anchorSet"] = opts.AnchorSet
	}
	if len(opts.SourceAnchors) > 0 {
		payload["sourceAnchors"] = opts.SourceAnchors
	}
	if len(opts.ResolvedAnchors) > 0 {
		payload["resolvedAnchors"] = opts.ResolvedAnchors
	}
	if len(opts.UnresolvedAnchors) > 0 {
		payload["unresolvedAnchors"] = opts.UnresolvedAnchors
	}
	if len(opts.AnchorProducts) > 0 {
		payload["anchorProducts"] = opts.AnchorProducts
	}
	if len(opts.AnchorComponents) > 0 {
		payload["anchorComponents"] = opts.AnchorComponents
	}
	if len(opts.ProductNodes) > 0 {
		payload["productNodes"] = opts.ProductNodes
	}
	if len(opts.ActorNodes) > 0 {
		payload["actorNodes"] = opts.ActorNodes
	}
	if len(opts.SupplyEdges) > 0 {
		payload["supplyEdges"] = opts.SupplyEdges
	}
	if len(opts.TransformEdges) > 0 {
		payload["transformEdges"] = opts.TransformEdges
	}
	if len(opts.AssemblyEdges) > 0 {
		payload["assemblyEdges"] = opts.AssemblyEdges
	}
	if len(opts.RouteBindings) > 0 {
		payload["routeBindings"] = opts.RouteBindings
	}
	if opts.Coverage != nil {
		payload["coverage"] = opts.Coverage
	}
	if len(opts.OpenGaps) > 0 {
		payload["openGaps"] = opts.OpenGaps
	}
	if len(opts.DirectPathKeys) > 0 {
		payload["directPathKeys"] = opts.DirectPathKeys
	}
	if len(opts.DerivedPathKeys) > 0 {
		payload["derivedPathKeys"] = opts.DerivedPathKeys
	}
	if len(opts.PrimaryBranches) > 0 {
		payload["primaryBranches"] = opts.PrimaryBranches
	}
	if len(opts.SecondaryBranches) > 0 {
		payload["secondaryBranches"] = opts.SecondaryBranches
	}
	if len(opts.TopRoutes) > 0 {
		payload["topRoutes"] = opts.TopRoutes
	}
	if len(opts.TopDistributionNodes) > 0 {
		payload["topDistributionNodes"] = opts.TopDistributionNodes
	}
	if len(opts.TopSuppliers) > 0 {
		payload["topSuppliers"] = opts.TopSuppliers
	}
	if len(opts.TopProductFamilies) > 0 {
		payload["topProductFamilies"] = opts.TopProductFamilies
	}
	if len(opts.SuppressedSupportBranches) > 0 {
		payload["suppressedSupportBranches"] = opts.SuppressedSupportBranches
	}
	if strings.TrimSpace(opts.Stage) != "" {
		payload["stage"] = strings.TrimSpace(opts.Stage)
	}
	if opts.Provisional {
		payload["provisional"] = true
	}
	if strings.TrimSpace(opts.SourceMode) != "" {
		payload["sourceMode"] = strings.TrimSpace(opts.SourceMode)
	}
	if opts.BackgroundContinuationScheduled {
		payload["backgroundContinuationScheduled"] = true
	}
	if len(opts.BackgroundContinuationJobIDs) > 0 {
		payload["backgroundContinuationJobIds"] = opts.BackgroundContinuationJobIDs
	}
	if len(opts.Notes) > 0 {
		payload["notes"] = opts.Notes
	}
	return payload
}

// ParseSupplyChainPathPayload parses a SUPPLY_CHAIN_PATH payload into structured types.
func ParseSupplyChainPathPayload(payload map[string]interface{}) (*SupplyChainPathPayload, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload is nil")
	}

	v, ok := payload["v"]
	if !ok {
		return nil, fmt.Errorf("payload missing version field 'v'")
	}
	version, ok := toFloat64(v)
	if !ok {
		return nil, fmt.Errorf("payload version 'v' must be a number")
	}
	if int(version) != 1 {
		return nil, fmt.Errorf("unsupported payload version: %d (expected 1)", int(version))
	}

	nodesRaw, ok := payload["nodes"]
	if !ok {
		return nil, fmt.Errorf("payload missing 'nodes' field")
	}
	nodes, err := decodeSupplyChainPathNodes(nodesRaw)
	if err != nil {
		return nil, err
	}

	edges := []SupplyChainPathEdge{}
	if edgesRaw, ok := payload["edges"]; ok {
		parsedEdges, err := decodeSupplyChainPathEdges(edgesRaw)
		if err != nil {
			return nil, err
		}
		edges = parsedEdges
	}

	legs := []SupplyChainPathLeg{}
	if legsRaw, ok := payload["legs"]; ok {
		parsedLegs, err := decodeSupplyChainPathLegs(legsRaw)
		if err != nil {
			return nil, err
		}
		legs = normalizeParsedSupplyChainPathLegs(parsedLegs)
	}

	var stress *SupplyChainPathStress
	if stressRaw, ok := payload["stress"]; ok && stressRaw != nil {
		switch typed := stressRaw.(type) {
		case *SupplyChainPathStress:
			stress = typed
		case SupplyChainPathStress:
			stressCopy := typed
			stress = &stressCopy
		case map[string]interface{}:
			stressJSON, err := json.Marshal(typed)
			if err != nil {
				return nil, fmt.Errorf("payload stress marshal failed: %w", err)
			}
			var parsedStress SupplyChainPathStress
			if err := json.Unmarshal(stressJSON, &parsedStress); err != nil {
				return nil, fmt.Errorf("payload stress unmarshal failed: %w", err)
			}
			stress = &parsedStress
		default:
			return nil, fmt.Errorf("payload 'stress' must be an object")
		}
	}

	sufficiency, err := decodeSupplyChainPathSufficiency(payload["sufficiency"])
	if err != nil {
		return nil, err
	}
	family, err := decodeSupplyChainPathFamily(payload["family"])
	if err != nil {
		return nil, err
	}

	notes := map[string]interface{}{}
	if notesRaw, ok := payload["notes"]; ok {
		notesMap, ok := notesRaw.(map[string]interface{})
		if ok {
			notes = notesMap
		}
	}

	return &SupplyChainPathPayload{
		Version:     int(version),
		Nodes:       nodes,
		Edges:       edges,
		Legs:        legs,
		Stress:      stress,
		Sufficiency: sufficiency,
		Family:      family,
		Notes:       notes,
	}, nil
}

func ParseSupplyChainAssemblyGraphPayload(payload map[string]interface{}) (*SupplyChainAssemblyGraphPayload, error) {
	if err := validateSupplyChainAssemblyGraphPayload(payload); err != nil {
		return nil, err
	}
	anchorSet, err := decodeStringSlice(payload["anchorSet"])
	if err != nil {
		return nil, err
	}
	sourceAnchors, err := decodeStringSlice(payload["sourceAnchors"])
	if err != nil {
		return nil, err
	}
	resolvedAnchors, err := decodeStringSlice(payload["resolvedAnchors"])
	if err != nil {
		return nil, err
	}
	unresolvedAnchors, err := decodeStringSlice(payload["unresolvedAnchors"])
	if err != nil {
		return nil, err
	}
	anchors, err := decodeStringSlice(payload["anchorProducts"])
	if err != nil {
		return nil, err
	}
	anchorComponents, err := decodeSupplyChainAssemblyAnchorComponents(payload["anchorComponents"])
	if err != nil {
		return nil, err
	}
	productNodes, err := decodeSupplyChainAssemblyNodes(payload["productNodes"])
	if err != nil {
		return nil, err
	}
	actorNodes, err := decodeSupplyChainAssemblyNodes(payload["actorNodes"])
	if err != nil {
		return nil, err
	}
	supplyEdges, err := decodeSupplyChainAssemblySupplyEdges(payload["supplyEdges"])
	if err != nil {
		return nil, err
	}
	transformEdges, err := decodeSupplyChainAssemblyTransformEdges(payload["transformEdges"])
	if err != nil {
		return nil, err
	}
	assemblyEdges, err := decodeSupplyChainAssemblyTransformEdges(payload["assemblyEdges"])
	if err != nil {
		return nil, err
	}
	routeBindings, err := decodeSupplyChainAssemblyRouteBindings(payload["routeBindings"])
	if err != nil {
		return nil, err
	}
	coverage, err := decodeSupplyChainAssemblyCoverage(payload["coverage"])
	if err != nil {
		return nil, err
	}
	openGaps, err := decodeStringSlice(payload["openGaps"])
	if err != nil {
		return nil, err
	}
	directPathKeys, err := decodeStringSlice(payload["directPathKeys"])
	if err != nil {
		return nil, err
	}
	derivedPathKeys, err := decodeStringSlice(payload["derivedPathKeys"])
	if err != nil {
		return nil, err
	}
	primaryBranches, err := decodeSupplyChainAssemblyBranchSummaries(payload["primaryBranches"])
	if err != nil {
		return nil, err
	}
	secondaryBranches, err := decodeSupplyChainAssemblyBranchSummaries(payload["secondaryBranches"])
	if err != nil {
		return nil, err
	}
	topRoutes, err := decodeStringSlice(payload["topRoutes"])
	if err != nil {
		return nil, err
	}
	topDistributionNodes, err := decodeStringSlice(payload["topDistributionNodes"])
	if err != nil {
		return nil, err
	}
	topSuppliers, err := decodeStringSlice(payload["topSuppliers"])
	if err != nil {
		return nil, err
	}
	topProductFamilies, err := decodeStringSlice(payload["topProductFamilies"])
	if err != nil {
		return nil, err
	}
	suppressedSupportBranches, err := decodeSupplyChainAssemblyBranchSummaries(payload["suppressedSupportBranches"])
	if err != nil {
		return nil, err
	}
	notes := map[string]interface{}{}
	if notesRaw, ok := payload["notes"]; ok {
		if notesMap, ok := notesRaw.(map[string]interface{}); ok {
			notes = notesMap
		}
	}
	stage := ""
	if raw, ok := payload["stage"].(string); ok {
		stage = strings.TrimSpace(raw)
	}
	sourceMode := ""
	if raw, ok := payload["sourceMode"].(string); ok {
		sourceMode = strings.TrimSpace(raw)
	}
	provisional, _ := payload["provisional"].(bool)
	backgroundContinuationScheduled, _ := payload["backgroundContinuationScheduled"].(bool)
	backgroundContinuationJobIDs, err := decodeStringSlice(payload["backgroundContinuationJobIds"])
	if err != nil {
		return nil, err
	}
	return &SupplyChainAssemblyGraphPayload{
		Version:                         1,
		AnchorSet:                       anchorSet,
		SourceAnchors:                   sourceAnchors,
		ResolvedAnchors:                 resolvedAnchors,
		UnresolvedAnchors:               unresolvedAnchors,
		AnchorProducts:                  anchors,
		AnchorComponents:                anchorComponents,
		ProductNodes:                    productNodes,
		ActorNodes:                      actorNodes,
		SupplyEdges:                     supplyEdges,
		TransformEdges:                  transformEdges,
		AssemblyEdges:                   assemblyEdges,
		RouteBindings:                   routeBindings,
		Coverage:                        coverage,
		OpenGaps:                        openGaps,
		DirectPathKeys:                  directPathKeys,
		DerivedPathKeys:                 derivedPathKeys,
		PrimaryBranches:                 primaryBranches,
		SecondaryBranches:               secondaryBranches,
		TopRoutes:                       topRoutes,
		TopDistributionNodes:            topDistributionNodes,
		TopSuppliers:                    topSuppliers,
		TopProductFamilies:              topProductFamilies,
		SuppressedSupportBranches:       suppressedSupportBranches,
		Stage:                           stage,
		Provisional:                     provisional,
		SourceMode:                      sourceMode,
		BackgroundContinuationScheduled: backgroundContinuationScheduled,
		BackgroundContinuationJobIDs:    backgroundContinuationJobIDs,
		Notes:                           notes,
	}, nil
}

// RepairSupplyChainPathLinkage fills missing supply-link linkage metadata on path legs and operational edges.
// resolver is optional; when provided it may resolve canonical path -> (id, tenantId, ok).
func RepairSupplyChainPathLinkage(artifact *MaterializedArtifact, resolver func(string) (string, string, bool)) (SupplyChainLinkageSummary, error) {
	summary := SupplyChainLinkageSummary{}
	if artifact == nil || (artifact.Kind != MaterializedKindSupplyChainPath && artifact.Kind != MaterializedKindServiceChainPath) {
		return summary, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH or SERVICE_CHAIN_PATH")
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return summary, err
	}
	tenantID := strings.TrimSpace(artifact.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	if synthesizeLegacySupplyChainLegs(parsed, tenantID) {
		summary.Changed = true
	}

	for idx := range parsed.Legs {
		leg := &parsed.Legs[idx]
		if strings.TrimSpace(leg.SupplierRef) == "" || strings.TrimSpace(leg.CustomerRef) == "" || strings.TrimSpace(leg.ProductRef) == "" {
			continue
		}
		summary.Legs++
		if strings.TrimSpace(leg.SupplyLinkCanonicalPath) == "" {
			canonicalPath, cerr := BuildSupplyLinkCanonicalPath(
				tenantID,
				stringPtr(strings.TrimSpace(leg.SupplierRef)),
				stringPtr(strings.TrimSpace(leg.ProductRef)),
				stringPtr(strings.TrimSpace(leg.CustomerRef)),
			)
			if cerr == nil && strings.TrimSpace(canonicalPath) != "" {
				leg.SupplyLinkCanonicalPath = canonicalPath
				if strings.TrimSpace(leg.SupplyLinkTenantID) == "" {
					leg.SupplyLinkTenantID = tenantID
				}
				summary.Changed = true
			}
		}
		if strings.TrimSpace(leg.SupplyLinkCanonicalPath) != "" && resolver != nil {
			if id, resolvedTenantID, ok := resolver(strings.TrimSpace(leg.SupplyLinkCanonicalPath)); ok {
				if strings.TrimSpace(leg.SupplyLinkID) == "" && strings.TrimSpace(id) != "" {
					leg.SupplyLinkID = strings.TrimSpace(id)
					summary.Changed = true
				}
				if strings.TrimSpace(leg.SupplyLinkTenantID) == "" && strings.TrimSpace(resolvedTenantID) != "" {
					leg.SupplyLinkTenantID = strings.TrimSpace(resolvedTenantID)
					summary.Changed = true
				}
			}
		}
		if strings.TrimSpace(leg.SupplyLinkCanonicalPath) != "" {
			summary.Linked++
			if strings.TrimSpace(leg.SupplyLinkID) != "" {
				summary.ResolvedIDs++
			}
		} else {
			summary.Unresolved++
		}
	}

	for idx := range parsed.Edges {
		edge := &parsed.Edges[idx]
		if edge.Type != "SUPPLY_LINK" && edge.Type != "LOGISTICS" {
			continue
		}
		summary.OperationalEdges++
		if inferMissingOperationalEdgeMetadata(parsed, edge) {
			summary.Changed = true
		}
		if edge.SegmentIndex != nil && *edge.SegmentIndex >= 0 && *edge.SegmentIndex < len(parsed.Legs) {
			leg := parsed.Legs[*edge.SegmentIndex]
			if strings.TrimSpace(edge.SupplyLinkCanonicalPath) == "" && strings.TrimSpace(leg.SupplyLinkCanonicalPath) != "" {
				edge.SupplyLinkCanonicalPath = strings.TrimSpace(leg.SupplyLinkCanonicalPath)
				summary.Changed = true
			}
			if strings.TrimSpace(edge.SupplyLinkID) == "" && strings.TrimSpace(leg.SupplyLinkID) != "" {
				edge.SupplyLinkID = strings.TrimSpace(leg.SupplyLinkID)
				summary.Changed = true
			}
			if strings.TrimSpace(edge.SupplyLinkTenantID) == "" && strings.TrimSpace(leg.SupplyLinkTenantID) != "" {
				edge.SupplyLinkTenantID = strings.TrimSpace(leg.SupplyLinkTenantID)
				summary.Changed = true
			}
		}
		if strings.TrimSpace(edge.SupplyLinkCanonicalPath) == "" {
			supplierRef, customerRef, productRef := linkageRefsFromEdge(parsed, *edge)
			if supplierRef != "" && customerRef != "" && productRef != "" {
				canonicalPath, cerr := BuildSupplyLinkCanonicalPath(
					tenantID,
					stringPtr(supplierRef),
					stringPtr(productRef),
					stringPtr(customerRef),
				)
				if cerr == nil && strings.TrimSpace(canonicalPath) != "" {
					edge.SupplyLinkCanonicalPath = canonicalPath
					if strings.TrimSpace(edge.SupplyLinkTenantID) == "" {
						edge.SupplyLinkTenantID = tenantID
					}
					summary.Changed = true
				}
			}
		}
		if strings.TrimSpace(edge.SupplyLinkCanonicalPath) != "" && resolver != nil {
			if id, resolvedTenantID, ok := resolver(strings.TrimSpace(edge.SupplyLinkCanonicalPath)); ok {
				if strings.TrimSpace(edge.SupplyLinkID) == "" && strings.TrimSpace(id) != "" {
					edge.SupplyLinkID = strings.TrimSpace(id)
					summary.Changed = true
				}
				if strings.TrimSpace(edge.SupplyLinkTenantID) == "" && strings.TrimSpace(resolvedTenantID) != "" {
					edge.SupplyLinkTenantID = strings.TrimSpace(resolvedTenantID)
					summary.Changed = true
				}
			}
		}
		if strings.TrimSpace(edge.SupplyLinkCanonicalPath) != "" {
			summary.Linked++
			if strings.TrimSpace(edge.SupplyLinkID) != "" {
				summary.ResolvedIDs++
			}
		} else {
			summary.Unresolved++
		}
	}

	summary.FullyLinked = summary.Unresolved == 0 && (summary.Legs > 0 || summary.OperationalEdges > 0)
	if summary.Changed {
		artifact.Payload = BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
		PopulateSupplyChainPathIndexes(artifact)
	}
	return summary, nil
}

func inferMissingOperationalEdgeMetadata(parsed *SupplyChainPathPayload, edge *SupplyChainPathEdge) bool {
	if parsed == nil || edge == nil {
		return false
	}
	changed := false
	if strings.TrimSpace(edge.Role) == "" {
		switch edge.Type {
		case "SUPPLY_LINK":
			edge.Role = "product_flow"
			changed = true
		case "LOGISTICS":
			edge.Role = "logistics_leg"
			changed = true
		}
	}
	if !edge.CarriesProduct {
		edge.CarriesProduct = true
		changed = true
	}
	if edge.SegmentIndex != nil && *edge.SegmentIndex >= 0 {
		return changed
	}

	bestLegIdx := -1
	bestScore := 0
	edgeProductRef := strings.TrimSpace(edge.ProductRef)
	if edgeProductRef == "" && edge.ProductNodeIndex != nil && *edge.ProductNodeIndex >= 0 && *edge.ProductNodeIndex < len(parsed.Nodes) {
		edgeProductRef = strings.TrimSpace(parsed.Nodes[*edge.ProductNodeIndex].Ref)
	}
	if edgeProductRef == "" && edge.ViaProductIndex != nil && *edge.ViaProductIndex >= 0 && *edge.ViaProductIndex < len(parsed.Nodes) {
		edgeProductRef = strings.TrimSpace(parsed.Nodes[*edge.ViaProductIndex].Ref)
	}

	for legIdx := range parsed.Legs {
		leg := parsed.Legs[legIdx]
		score := scoreOperationalEdgeAgainstLeg(parsed, edge, &leg, edgeProductRef)
		if score > bestScore {
			bestScore = score
			bestLegIdx = legIdx
		}
	}
	if bestLegIdx >= 0 && bestScore >= 6 {
		idx := bestLegIdx
		edge.SegmentIndex = &idx
		changed = true
	}
	return changed
}

func synthesizeLegacySupplyChainLegs(parsed *SupplyChainPathPayload, tenantID string) bool {
	if parsed == nil || len(parsed.Edges) == 0 {
		return false
	}
	existingByCanonical := make(map[string]struct{}, len(parsed.Legs))
	maxLegIndex := -1
	for _, leg := range parsed.Legs {
		if leg.Index > maxLegIndex {
			maxLegIndex = leg.Index
		}
		if canonicalPath := strings.TrimSpace(leg.SupplyLinkCanonicalPath); canonicalPath != "" {
			existingByCanonical[canonicalPath] = struct{}{}
		}
	}

	type legacyLegGroup struct {
		CanonicalPath string
		EdgeIndexes   []int
	}
	var groups []legacyLegGroup
	groupIndexByCanonical := map[string]int{}
	for edgeIdx := range parsed.Edges {
		edge := parsed.Edges[edgeIdx]
		if edge.Type != "SUPPLY_LINK" && edge.Type != "LOGISTICS" {
			continue
		}
		canonicalPath := strings.TrimSpace(edge.SupplyLinkCanonicalPath)
		if canonicalPath == "" {
			supplierRef, customerRef, productRef := linkageRefsFromEdge(parsed, edge)
			if supplierRef != "" && customerRef != "" && productRef != "" {
				if inferredPath, err := BuildSupplyLinkCanonicalPath(tenantID, stringPtr(supplierRef), stringPtr(productRef), stringPtr(customerRef)); err == nil {
					canonicalPath = strings.TrimSpace(inferredPath)
				}
			}
		}
		if canonicalPath == "" {
			continue
		}
		if _, exists := existingByCanonical[canonicalPath]; exists {
			continue
		}
		groupIdx, exists := groupIndexByCanonical[canonicalPath]
		if !exists {
			groupIdx = len(groups)
			groupIndexByCanonical[canonicalPath] = groupIdx
			groups = append(groups, legacyLegGroup{CanonicalPath: canonicalPath})
		}
		groups[groupIdx].EdgeIndexes = append(groups[groupIdx].EdgeIndexes, edgeIdx)
	}

	changed := false
	for _, group := range groups {
		if len(group.EdgeIndexes) == 0 {
			continue
		}
		leg := buildLegacyLegFromOperationalEdges(parsed, group.CanonicalPath, group.EdgeIndexes, maxLegIndex+1, tenantID)
		if leg == nil {
			continue
		}
		parsed.Legs = append(parsed.Legs, *leg)
		existingByCanonical[group.CanonicalPath] = struct{}{}
		maxLegIndex = leg.Index
		changed = true
	}
	return changed
}

func buildLegacyLegFromOperationalEdges(parsed *SupplyChainPathPayload, canonicalPath string, edgeIndexes []int, legIndex int, tenantID string) *SupplyChainPathLeg {
	if parsed == nil || len(edgeIndexes) == 0 {
		return nil
	}
	leg := &SupplyChainPathLeg{
		Index:                   legIndex,
		SupplyLinkCanonicalPath: strings.TrimSpace(canonicalPath),
		SupplyLinkTenantID:      strings.TrimSpace(firstNonEmpty(tenantID, "global")),
	}
	productFlowFound := false
	viaSeen := map[int]struct{}{}
	for _, edgeIdx := range edgeIndexes {
		if edgeIdx < 0 || edgeIdx >= len(parsed.Edges) {
			continue
		}
		edge := parsed.Edges[edgeIdx]
		productRef := strings.TrimSpace(edge.ProductRef)
		if productRef == "" && edge.ProductNodeIndex != nil && *edge.ProductNodeIndex >= 0 && *edge.ProductNodeIndex < len(parsed.Nodes) {
			productRef = strings.TrimSpace(parsed.Nodes[*edge.ProductNodeIndex].Ref)
		}
		if productRef == "" && edge.ViaProductIndex != nil && *edge.ViaProductIndex >= 0 && *edge.ViaProductIndex < len(parsed.Nodes) {
			productRef = strings.TrimSpace(parsed.Nodes[*edge.ViaProductIndex].Ref)
		}
		if leg.ProductRef == "" && productRef != "" {
			leg.ProductRef = productRef
		}
		if leg.ProductNodeIndex == nil {
			if edge.ProductNodeIndex != nil && *edge.ProductNodeIndex >= 0 {
				idx := *edge.ProductNodeIndex
				leg.ProductNodeIndex = &idx
			} else if edge.ViaProductIndex != nil && *edge.ViaProductIndex >= 0 {
				idx := *edge.ViaProductIndex
				leg.ProductNodeIndex = &idx
			}
		}

		if edge.Type == "SUPPLY_LINK" {
			productFlowFound = true
			if edge.Qualifiers != nil && strings.TrimSpace(leg.RouteVariantKey) == "" {
				if raw, ok := edge.Qualifiers["route_variant_key"].(string); ok {
					leg.RouteVariantKey = strings.TrimSpace(raw)
				}
			}
			if edge.From >= 0 && edge.From < len(parsed.Nodes) {
				leg.SupplierRef = strings.TrimSpace(parsed.Nodes[edge.From].Ref)
				idx := edge.From
				leg.SupplierNodeIndex = &idx
			}
			if edge.To >= 0 && edge.To < len(parsed.Nodes) {
				leg.CustomerRef = strings.TrimSpace(parsed.Nodes[edge.To].Ref)
				idx := edge.To
				leg.CustomerNodeIndex = &idx
			}
			continue
		}

		if edge.Type == "LOGISTICS" {
			if leg.SupplierNodeIndex == nil && edge.From >= 0 {
				idx := edge.From
				leg.SupplierNodeIndex = &idx
				if edge.From < len(parsed.Nodes) {
					leg.SupplierRef = strings.TrimSpace(parsed.Nodes[edge.From].Ref)
				}
			}
			if edge.To >= 0 {
				if _, seen := viaSeen[edge.To]; !seen {
					viaSeen[edge.To] = struct{}{}
					leg.TransportViaNodeIndices = append(leg.TransportViaNodeIndices, edge.To)
					if edge.To < len(parsed.Nodes) {
						leg.TransportViaRefs = append(leg.TransportViaRefs, strings.TrimSpace(parsed.Nodes[edge.To].Ref))
					}
				}
			}
		}
	}
	if len(leg.TransportViaNodeIndices) > 0 && leg.CustomerNodeIndex == nil {
		lastVia := leg.TransportViaNodeIndices[len(leg.TransportViaNodeIndices)-1]
		leg.TransportViaNodeIndices = leg.TransportViaNodeIndices[:len(leg.TransportViaNodeIndices)-1]
		if len(leg.TransportViaRefs) > 0 {
			leg.TransportViaRefs = leg.TransportViaRefs[:len(leg.TransportViaRefs)-1]
		}
		idx := lastVia
		leg.CustomerNodeIndex = &idx
		if lastVia >= 0 && lastVia < len(parsed.Nodes) {
			leg.CustomerRef = strings.TrimSpace(parsed.Nodes[lastVia].Ref)
		}
	}
	if !productFlowFound && (leg.SupplierRef == "" || leg.CustomerRef == "") {
		firstEdge := parsed.Edges[edgeIndexes[0]]
		supplierRef, customerRef, _ := linkageRefsFromEdge(parsed, firstEdge)
		if leg.SupplierRef == "" {
			leg.SupplierRef = supplierRef
		}
		if leg.CustomerRef == "" {
			leg.CustomerRef = customerRef
		}
	}
	if leg.SupplierRef == "" || leg.CustomerRef == "" || leg.ProductRef == "" {
		return nil
	}
	if strings.TrimSpace(leg.RouteVariantKey) == "" {
		leg.RouteVariantKey = BuildSupplyLinkRouteVariantKey(leg.TransportViaRefs)
	}
	return leg
}

func scoreOperationalEdgeAgainstLeg(parsed *SupplyChainPathPayload, edge *SupplyChainPathEdge, leg *SupplyChainPathLeg, edgeProductRef string) int {
	if parsed == nil || edge == nil || leg == nil {
		return 0
	}
	score := 0
	if edgeProductRef != "" && edgeProductRef == strings.TrimSpace(leg.ProductRef) {
		score += 4
	}
	if edge.Type == "SUPPLY_LINK" {
		if matchesNodeRef(parsed, edge.From, leg.SupplierNodeIndex, leg.SupplierRef) {
			score += 4
		}
		if matchesNodeRef(parsed, edge.To, leg.CustomerNodeIndex, leg.CustomerRef) {
			score += 4
		}
		return score
	}

	route := make([]int, 0, len(leg.TransportViaNodeIndices)+2)
	if leg.SupplierNodeIndex != nil && *leg.SupplierNodeIndex >= 0 {
		route = append(route, *leg.SupplierNodeIndex)
	}
	route = append(route, leg.TransportViaNodeIndices...)
	if leg.CustomerNodeIndex != nil && *leg.CustomerNodeIndex >= 0 {
		route = append(route, *leg.CustomerNodeIndex)
	}
	for i := 0; i+1 < len(route); i++ {
		if route[i] == edge.From && route[i+1] == edge.To {
			score += 8
			break
		}
	}
	if matchesNodeRef(parsed, edge.From, leg.SupplierNodeIndex, leg.SupplierRef) {
		score += 2
	}
	if matchesNodeRef(parsed, edge.To, leg.CustomerNodeIndex, leg.CustomerRef) {
		score += 2
	}
	for _, viaIdx := range leg.TransportViaNodeIndices {
		if viaIdx == edge.From || viaIdx == edge.To {
			score++
		}
	}
	return score
}

func matchesNodeRef(parsed *SupplyChainPathPayload, edgeNodeIndex int, legNodeIndex *int, expectedRef string) bool {
	if legNodeIndex != nil && *legNodeIndex == edgeNodeIndex {
		return true
	}
	if parsed == nil || edgeNodeIndex < 0 || edgeNodeIndex >= len(parsed.Nodes) {
		return false
	}
	return strings.TrimSpace(parsed.Nodes[edgeNodeIndex].Ref) != "" &&
		strings.TrimSpace(parsed.Nodes[edgeNodeIndex].Ref) == strings.TrimSpace(expectedRef)
}

func linkageRefsFromEdge(parsed *SupplyChainPathPayload, edge SupplyChainPathEdge) (string, string, string) {
	if parsed == nil {
		return "", "", ""
	}
	productRef := strings.TrimSpace(edge.ProductRef)
	if productRef == "" && edge.ProductNodeIndex != nil && *edge.ProductNodeIndex >= 0 && *edge.ProductNodeIndex < len(parsed.Nodes) {
		productRef = strings.TrimSpace(parsed.Nodes[*edge.ProductNodeIndex].Ref)
	}
	supplierRef := ""
	customerRef := ""
	if edge.From >= 0 && edge.From < len(parsed.Nodes) {
		supplierRef = strings.TrimSpace(parsed.Nodes[edge.From].Ref)
	}
	if edge.To >= 0 && edge.To < len(parsed.Nodes) {
		customerRef = strings.TrimSpace(parsed.Nodes[edge.To].Ref)
	}
	return supplierRef, customerRef, productRef
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	v := value
	return &v
}

// GetSupplyChainPathNodes extracts nodes from a MaterializedArtifact payload.
func GetSupplyChainPathNodes(artifact *MaterializedArtifact) ([]SupplyChainPathNode, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	if artifact.Kind != MaterializedKindSupplyChainPath && artifact.Kind != MaterializedKindServiceChainPath {
		return nil, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH or SERVICE_CHAIN_PATH")
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return nil, fmt.Errorf("parse payload failed: %w", err)
	}

	return parsed.Nodes, nil
}

// GetSupplyChainPathEdges extracts edges from a MaterializedArtifact payload.
func GetSupplyChainPathEdges(artifact *MaterializedArtifact) ([]SupplyChainPathEdge, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	if artifact.Kind != MaterializedKindSupplyChainPath && artifact.Kind != MaterializedKindServiceChainPath {
		return nil, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH or SERVICE_CHAIN_PATH")
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return nil, fmt.Errorf("parse payload failed: %w", err)
	}

	return parsed.Edges, nil
}

// GetSupplyChainPathLegs extracts operational legs from a MaterializedArtifact payload.
func GetSupplyChainPathLegs(artifact *MaterializedArtifact) ([]SupplyChainPathLeg, error) {
	if artifact == nil {
		return nil, fmt.Errorf("artifact is nil")
	}
	if artifact.Kind != MaterializedKindSupplyChainPath && artifact.Kind != MaterializedKindServiceChainPath {
		return nil, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH or SERVICE_CHAIN_PATH")
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return nil, fmt.Errorf("parse payload failed: %w", err)
	}

	return parsed.Legs, nil
}

// NormalizeSupplyChainPathRouteOverlays rewrites a SUPPLY_CHAIN_PATH artifact payload so that
// each structural leg carries explicit routeOverlays and legacy route fields are synchronized
// from the selected primary overlay. Returns true when payload changed.
func NormalizeSupplyChainPathRouteOverlays(artifact *MaterializedArtifact) (bool, error) {
	if artifact == nil {
		return false, fmt.Errorf("artifact is nil")
	}
	if artifact.Kind != MaterializedKindSupplyChainPath && artifact.Kind != MaterializedKindServiceChainPath {
		return false, nil
	}
	if supplyChainPathPayloadAlreadyNormalized(artifact.Payload) {
		return false, nil
	}
	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return false, err
	}
	if parsed == nil {
		return false, nil
	}
	normalizedPayload := BuildSupplyChainPathPayloadDetailed(
		parsed.Nodes,
		parsed.Edges,
		parsed.Legs,
		parsed.Stress,
		parsed.Sufficiency,
		parsed.Family,
		parsed.Notes,
	)
	if reflect.DeepEqual(artifact.Payload, normalizedPayload) {
		return false, nil
	}
	artifact.Payload = normalizedPayload
	PopulateMaterializedArtifactIndexes(artifact)
	return true, nil
}

type SupplyChainPathBindingSummary struct {
	GeoRefs            []string
	FlowRefs           []string
	ZoneRefs           []string
	AffectedZoneRefs   []string
	SpatialSemantics   []string
	LegCount           int
	BindingConfidence  float64
	PathStressScore    float64
	BottleneckLegIndex int
	RecoveryLatencyHrs float64
}

func SummarizeSupplyChainPathBindings(parsed *SupplyChainPathPayload) SupplyChainPathBindingSummary {
	out := SupplyChainPathBindingSummary{}
	if parsed == nil || len(parsed.Legs) == 0 {
		return out
	}
	var confidenceTotal float64
	for _, leg := range parsed.Legs {
		out.GeoRefs = mergeUniqueStrings(out.GeoRefs, leg.GeoRefs)
		out.FlowRefs = mergeUniqueStrings(out.FlowRefs, leg.FlowRefs)
		out.ZoneRefs = mergeUniqueStrings(out.ZoneRefs, leg.ZoneRefs)
		out.AffectedZoneRefs = mergeUniqueStrings(out.AffectedZoneRefs, leg.AffectedZoneRefs)
		out.SpatialSemantics = mergeUniqueStrings(out.SpatialSemantics, leg.SpatialSemantics)
		confidenceTotal += leg.BindingConfidence
	}
	out.LegCount = len(parsed.Legs)
	out.BindingConfidence = confidenceTotal / float64(len(parsed.Legs))
	if parsed.Stress != nil {
		out.PathStressScore = parsed.Stress.PathScore
		out.BottleneckLegIndex = parsed.Stress.BottleneckLegIndex
		out.RecoveryLatencyHrs = parsed.Stress.RecoveryLatencyHrs
	}
	return out
}

// PopulateMaterializedArtifactIndexes fills top-level overlap indexes from payload and labels.
// It is safe for both new and legacy artifacts and does not require persistence.
func PopulateMaterializedArtifactIndexes(artifact *MaterializedArtifact) {
	if artifact == nil {
		return
	}
	nodeRefs := uniqueTrimmedStrings(artifact.NodeRefs)
	geoRefs := uniqueTrimmedStrings(artifact.GeoRefs)
	flowRefs := uniqueTrimmedStrings(artifact.FlowRefs)
	zoneRefs := uniqueTrimmedStrings(artifact.ZoneRefs)
	affectedZoneRefs := uniqueTrimmedStrings(artifact.AffectedZoneRefs)
	switch artifact.Kind {
	case MaterializedKindSupplyChainPath, MaterializedKindServiceChainPath:
		if parsed, err := ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
			summary := SummarizeSupplyChainPathBindings(parsed)
			nodeRefs = collectParsedNodeRefs(parsed)
			geoRefs = uniqueTrimmedStrings(summary.GeoRefs)
			flowRefs = uniqueTrimmedStrings(summary.FlowRefs)
			zoneRefs = uniqueTrimmedStrings(summary.ZoneRefs)
			affectedZoneRefs = uniqueTrimmedStrings(summary.AffectedZoneRefs)
			artifact.LegCount = summary.LegCount
			artifact.BindingScore = summary.BindingConfidence
			artifact.PathStressScore = summary.PathStressScore
			artifact.BottleneckLeg = summary.BottleneckLegIndex
			artifact.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
		}
		PopulateSupplyChainPathSufficiency(artifact)
		PopulateSupplyChainPathFamily(artifact)
	case MaterializedKindSupplyChainAssemblyGraph:
		if parsed, err := ParseSupplyChainAssemblyGraphPayload(artifact.Payload); err == nil && parsed != nil {
			nodeRefs = collectAssemblyNodeRefs(parsed)
			geoRefs = collectArtifactGeoRefs(nodeRefs)
			flowRefs = collectAssemblyFlowRefs(parsed)
		}
	}
	artifact.NodeRefs = nodeRefs
	artifact.GeoRefs = geoRefs
	artifact.FlowRefs = flowRefs
	artifact.ZoneRefs = zoneRefs
	artifact.AffectedZoneRefs = affectedZoneRefs
	if artifact.Labels != nil {
		if artifact.AnchorSupplier == "" {
			artifact.AnchorSupplier = strings.TrimSpace(artifact.Labels["anchorSupplier"])
		}
		if artifact.AnchorCustomer == "" {
			artifact.AnchorCustomer = strings.TrimSpace(artifact.Labels["anchorCustomer"])
		}
		if artifact.ProductAxis == "" {
			artifact.ProductAxis = strings.TrimSpace(firstNonEmpty(artifact.Labels["productAxis"], artifact.Labels["anchorProduct"]))
		}
	}
}

// PopulateSupplyChainPathIndexes keeps legacy call sites working.
func PopulateSupplyChainPathIndexes(artifact *MaterializedArtifact) {
	PopulateMaterializedArtifactIndexes(artifact)
}

func collectParsedNodeRefs(parsed *SupplyChainPathPayload) []string {
	if parsed == nil || len(parsed.Nodes) == 0 {
		return nil
	}
	out := make([]string, 0, len(parsed.Nodes))
	for _, node := range parsed.Nodes {
		ref := strings.TrimSpace(node.Ref)
		if ref != "" {
			out = append(out, ref)
		}
	}
	return uniqueTrimmedStrings(out)
}

func collectAssemblyNodeRefs(parsed *SupplyChainAssemblyGraphPayload) []string {
	if parsed == nil {
		return nil
	}
	out := make([]string, 0, len(parsed.ProductNodes)+len(parsed.ActorNodes))
	for _, node := range parsed.ProductNodes {
		if ref := strings.TrimSpace(node.Ref); ref != "" {
			out = append(out, ref)
		}
	}
	for _, node := range parsed.ActorNodes {
		if ref := strings.TrimSpace(node.Ref); ref != "" {
			out = append(out, ref)
		}
	}
	for _, binding := range parsed.RouteBindings {
		for _, ref := range binding.ViaRefs {
			if strings.TrimSpace(ref) != "" {
				out = append(out, strings.TrimSpace(ref))
			}
		}
	}
	return uniqueTrimmedStrings(out)
}

func collectAssemblyFlowRefs(parsed *SupplyChainAssemblyGraphPayload) []string {
	if parsed == nil || len(parsed.RouteBindings) == 0 {
		return nil
	}
	out := make([]string, 0, len(parsed.RouteBindings))
	for _, binding := range parsed.RouteBindings {
		if ref := strings.TrimSpace(binding.SupplyLinkCanonicalPath); ref != "" {
			out = append(out, ref)
		}
	}
	return uniqueTrimmedStrings(out)
}

func collectArtifactGeoRefs(nodeRefs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(nodeRefs))
	for _, ref := range nodeRefs {
		geoRef := artifactGeoRef(ref)
		if geoRef == "" {
			continue
		}
		if _, ok := seen[geoRef]; ok {
			continue
		}
		seen[geoRef] = struct{}{}
		out = append(out, geoRef)
	}
	sort.Strings(out)
	return out
}

func artifactGeoRef(ref string) string {
	parts, err := taxonomy.ParseCanonicalPathParts(strings.TrimSpace(ref))
	if err != nil || parts == nil {
		return ""
	}
	geo := ""
	switch parts.Namespace {
	case "loc":
		if parts.Loc != nil {
			geo = parts.Loc.Country
		}
	case "org":
		if parts.Org != nil {
			geo = parts.Org.Geo
		}
	case "gov":
		if parts.Gov != nil {
			geo = parts.Gov.Geo
		}
	case "fac":
		if parts.Fac != nil {
			geo = parts.Fac.Geo
		}
	case "market":
		if parts.Market != nil {
			geo = parts.Market.Geo
		}
	case "community":
		if parts.Community != nil {
			geo = parts.Community.Geo
		}
	case "program":
		if parts.Program != nil {
			geo = parts.Program.Jurisdiction
		}
	}
	geo = strings.ToLower(strings.TrimSpace(geo))
	if geo == "" || geo == "na" || geo == "int" {
		return ""
	}
	return "/loc/" + geo
}

func uniqueTrimmedStrings(values []string) []string {
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
	sort.Strings(out)
	return out
}

// FindNodeByRef finds a node index by canonical path reference.
func FindNodeByRef(nodes []SupplyChainPathNode, ref string) int {
	for i, node := range nodes {
		if node.Ref == ref {
			return i
		}
	}
	return -1
}

// FindNodesByType finds all node indices of a specific type.
func FindNodesByType(nodes []SupplyChainPathNode, nodeType string) []int {
	indices := []int{}
	for i, node := range nodes {
		if node.Type == nodeType {
			indices = append(indices, i)
		}
	}
	return indices
}

// GenerateVectorText produces a textual representation of a MaterializedArtifact
// suitable for Vertex AI Search indexing and semantic retrieval.
func (a *MaterializedArtifact) GenerateVectorText() string {
	if a == nil {
		return ""
	}

	var b strings.Builder

	if a.DisplayName != "" {
		b.WriteString("Supply Chain: ")
		b.WriteString(a.DisplayName)
		b.WriteString("\n")
	}

	if len(a.Labels) > 0 {
		if v := a.Labels["anchorSupplier"]; v != "" {
			b.WriteString("Root Supplier: ")
			b.WriteString(v)
			b.WriteString("\n")
		}
		if v := a.Labels["anchorCustomer"]; v != "" {
			b.WriteString("End Customer: ")
			b.WriteString(v)
			b.WriteString("\n")
		}
		if v := a.Labels["productAxis"]; v != "" {
			b.WriteString("Product: ")
			b.WriteString(v)
			b.WriteString("\n")
		}
		if v := a.Labels["geoFrom"]; v != "" {
			b.WriteString("Origin: ")
			b.WriteString(v)
			b.WriteString("\n")
		}
		if v := a.Labels["geoTo"]; v != "" {
			b.WriteString("Destination: ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}

	switch a.Kind {
	case MaterializedKindSupplyChainPath, MaterializedKindServiceChainPath:
		if a.Payload != nil {
			parsed, err := ParseSupplyChainPathPayload(a.Payload)
			if err == nil && parsed != nil {
				if len(parsed.Nodes) > 0 {
					b.WriteString("Participants: ")
					refs := make([]string, 0, len(parsed.Nodes))
					for _, n := range parsed.Nodes {
						refs = append(refs, n.Type+":"+n.Ref)
					}
					b.WriteString(strings.Join(refs, ", "))
					b.WriteString("\n")
				}
				for _, edge := range parsed.Edges {
					if edge.CarriesProduct && edge.ProductRef != "" {
						b.WriteString("Product Flow Edge: ")
						b.WriteString(edge.Role)
						b.WriteString(" ")
						b.WriteString(edge.ProductRef)
						b.WriteString("\n")
					}
					if edge.Type == "ONTOLOGY" && edge.Relation != "" {
						b.WriteString("Transformation: ")
						b.WriteString(edge.Relation)
						b.WriteString("\n")
					}
				}
				for _, leg := range parsed.Legs {
					if len(leg.FlowRefs) > 0 {
						b.WriteString("Flows: ")
						b.WriteString(strings.Join(leg.FlowRefs, ", "))
						b.WriteString("\n")
					}
					if len(leg.ZoneRefs) > 0 {
						b.WriteString("Zones: ")
						b.WriteString(strings.Join(leg.ZoneRefs, ", "))
						b.WriteString("\n")
					}
					if len(leg.AffectedZoneRefs) > 0 {
						b.WriteString("Affected Zones: ")
						b.WriteString(strings.Join(leg.AffectedZoneRefs, ", "))
						b.WriteString("\n")
					}
					if leg.Stress != nil && leg.Stress.StressScore > 0 {
						b.WriteString("Leg Stress: ")
						b.WriteString(fmt.Sprintf("%.2f", leg.Stress.StressScore))
						b.WriteString("\n")
					}
				}
				if parsed.Stress != nil && parsed.Stress.PathScore > 0 {
					b.WriteString("Path Stress: ")
					b.WriteString(fmt.Sprintf("%.2f", parsed.Stress.PathScore))
					b.WriteString("\n")
				}
			}
		}
	case MaterializedKindSupplyChainAssemblyGraph:
		if a.Payload != nil {
			parsed, err := ParseSupplyChainAssemblyGraphPayload(a.Payload)
			if err == nil && parsed != nil {
				displayAnchors := parsed.ResolvedAnchors
				if len(displayAnchors) == 0 {
					displayAnchors = parsed.AnchorSet
				}
				if len(displayAnchors) == 0 {
					displayAnchors = parsed.AnchorProducts
				}
				if len(displayAnchors) > 0 {
					b.WriteString("Anchors: ")
					b.WriteString(strings.Join(displayAnchors, ", "))
					b.WriteString("\n")
				}
				if len(parsed.ProductNodes) > 0 {
					b.WriteString("Product Nodes: ")
					refs := make([]string, 0, len(parsed.ProductNodes))
					for _, node := range parsed.ProductNodes {
						refs = append(refs, node.Ref)
					}
					b.WriteString(strings.Join(refs, ", "))
					b.WriteString("\n")
				}
				if len(parsed.ActorNodes) > 0 {
					b.WriteString("Actor Nodes: ")
					refs := make([]string, 0, len(parsed.ActorNodes))
					for _, node := range parsed.ActorNodes {
						refs = append(refs, node.Ref)
					}
					b.WriteString(strings.Join(refs, ", "))
					b.WriteString("\n")
				}
				for _, edge := range parsed.TransformEdges {
					b.WriteString("Transformation: ")
					b.WriteString(edge.FromProductRef)
					b.WriteString(" -> ")
					b.WriteString(edge.ToProductRef)
					b.WriteString("\n")
				}
				for _, edge := range parsed.AssemblyEdges {
					b.WriteString("Assembly: ")
					b.WriteString(edge.FromProductRef)
					b.WriteString(" => ")
					b.WriteString(edge.ToProductRef)
					b.WriteString("\n")
				}
			}
		}
	case MaterializedKindGovernanceChain, MaterializedKindRegulatoryImpactMap,
		MaterializedKindRegulatoryImpactTransmissionPath, MaterializedKindRegulatoryTimeline,
		MaterializedKindRegulatoryCascadeMap, MaterializedKindRegulatoryImpactScenario,
		MaterializedKindEcosystemMap:
		kindLabel := map[MaterializedKind]string{
			MaterializedKindGovernanceChain:                  "Governance Chain",
			MaterializedKindRegulatoryImpactMap:              "Regulatory Impact Map",
			MaterializedKindRegulatoryImpactTransmissionPath: "Regulatory Transmission Paths",
			MaterializedKindRegulatoryTimeline:               "Regulatory Timeline",
			MaterializedKindRegulatoryCascadeMap:             "Regulatory Cascade Map",
			MaterializedKindRegulatoryImpactScenario:         "Regulatory What-If Scenarios",
			MaterializedKindEcosystemMap:                     "Ecosystem Map",
		}
		if lbl, ok := kindLabel[a.Kind]; ok && a.DisplayName == "" {
			b.WriteString(lbl + "\n")
		}
		if a.Payload != nil {
			parsed, err := ParseRelationsGraphPayload(a.Payload)
			if err == nil && parsed != nil {
				if len(parsed.Nodes) > 0 {
					b.WriteString("Entities: ")
					refs := make([]string, 0, len(parsed.Nodes))
					for _, n := range parsed.Nodes {
						label := n.Ref
						if n.Name != "" {
							label = n.Name + " (" + n.Ref + ")"
						}
						refs = append(refs, label)
					}
					b.WriteString(strings.Join(refs, ", "))
					b.WriteString("\n")
				}
				for _, edge := range parsed.Edges {
					if edge.Relation != "" && edge.From >= 0 && edge.From < len(parsed.Nodes) && edge.To >= 0 && edge.To < len(parsed.Nodes) {
						fromName := parsed.Nodes[edge.From].Ref
						toName := parsed.Nodes[edge.To].Ref
						b.WriteString(fmt.Sprintf("Relation: %s -[%s]-> %s\n", fromName, edge.Relation, toName))
					}
				}
			}
		}
	}

	if len(a.NodeRefs) > 0 {
		sorted := make([]string, len(a.NodeRefs))
		copy(sorted, a.NodeRefs)
		sort.Strings(sorted)
		b.WriteString("Node Paths: ")
		b.WriteString(strings.Join(sorted, ", "))
		b.WriteString("\n")
	}

	if a.PathStatus != "" {
		b.WriteString("Status: ")
		b.WriteString(a.PathStatus)
		b.WriteString("\n")
	}

	return b.String()
}

// MergePayloads merges two SUPPLY_CHAIN_PATH payloads: union of nodes (by ref) and edges (by from+to+type).
func MergePayloads(existing, incoming map[string]interface{}) map[string]interface{} {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}

	existingParsed, err1 := ParseSupplyChainPathPayload(existing)
	incomingParsed, err2 := ParseSupplyChainPathPayload(incoming)
	if err1 != nil || err2 != nil {
		return incoming
	}

	mergedNodes := mergePathNodes(existingParsed.Nodes, incomingParsed.Nodes)
	mergedEdges := mergePathEdges(existingParsed.Edges, incomingParsed.Edges)
	mergedLegs := mergePathLegs(existingParsed.Legs, incomingParsed.Legs)

	mergedNotes := map[string]interface{}{}
	for k, v := range existingParsed.Notes {
		mergedNotes[k] = v
	}
	for k, v := range incomingParsed.Notes {
		mergedNotes[k] = v
	}

	return BuildSupplyChainPathPayloadDetailed(
		mergedNodes,
		mergedEdges,
		mergedLegs,
		mergePathStress(existingParsed.Stress, incomingParsed.Stress),
		mergePathSufficiency(existingParsed.Sufficiency, incomingParsed.Sufficiency),
		mergePathFamily(existingParsed.Family, incomingParsed.Family),
		mergedNotes,
	)
}

func mergePathNodes(existing, incoming []SupplyChainPathNode) []SupplyChainPathNode {
	seen := map[string]bool{}
	merged := make([]SupplyChainPathNode, 0, len(existing)+len(incoming))
	for _, n := range existing {
		if !seen[n.Ref] {
			merged = append(merged, n)
			seen[n.Ref] = true
		}
	}
	for _, n := range incoming {
		if !seen[n.Ref] {
			merged = append(merged, n)
			seen[n.Ref] = true
		}
	}
	return merged
}

func mergePathEdges(existing, incoming []SupplyChainPathEdge) []SupplyChainPathEdge {
	type edgeKey struct {
		From, To     int
		Type         string
		ProductRef   string
		Role         string
		SegmentIndex int
	}
	indexByKey := map[edgeKey]int{}
	merged := make([]SupplyChainPathEdge, 0, len(existing)+len(incoming))
	for _, e := range existing {
		segmentIndex := -1
		if e.SegmentIndex != nil {
			segmentIndex = *e.SegmentIndex
		}
		k := edgeKey{From: e.From, To: e.To, Type: e.Type, ProductRef: strings.TrimSpace(e.ProductRef), Role: strings.TrimSpace(e.Role), SegmentIndex: segmentIndex}
		if _, exists := indexByKey[k]; !exists {
			merged = append(merged, e)
			indexByKey[k] = len(merged) - 1
		}
	}
	for _, e := range incoming {
		segmentIndex := -1
		if e.SegmentIndex != nil {
			segmentIndex = *e.SegmentIndex
		}
		k := edgeKey{From: e.From, To: e.To, Type: e.Type, ProductRef: strings.TrimSpace(e.ProductRef), Role: strings.TrimSpace(e.Role), SegmentIndex: segmentIndex}
		if idx, exists := indexByKey[k]; exists {
			merged[idx] = mergeSupplyChainPathEdge(merged[idx], e)
			continue
		}
		if _, exists := indexByKey[k]; !exists {
			merged = append(merged, e)
			indexByKey[k] = len(merged) - 1
		}
	}
	return merged
}

func mergePathLegs(existing, incoming []SupplyChainPathLeg) []SupplyChainPathLeg {
	type legKey struct {
		Index    int
		LinkPath string
		Supplier string
		Customer string
		Product  string
	}
	indexByKey := map[legKey]int{}
	merged := make([]SupplyChainPathLeg, 0, len(existing)+len(incoming))
	appendIfNew := func(legs []SupplyChainPathLeg) {
		for _, leg := range legs {
			k := legKey{
				Index:    leg.Index,
				LinkPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
				Supplier: strings.TrimSpace(leg.SupplierRef),
				Customer: strings.TrimSpace(leg.CustomerRef),
				Product:  strings.TrimSpace(leg.ProductRef),
			}
			if idx, exists := indexByKey[k]; exists {
				merged[idx] = mergeSupplyChainPathLeg(merged[idx], leg)
				continue
			}
			merged = append(merged, leg)
			indexByKey[k] = len(merged) - 1
		}
	}
	appendIfNew(existing)
	appendIfNew(incoming)
	sort.SliceStable(merged, func(i, j int) bool {
		if merged[i].Index == merged[j].Index {
			if merged[i].SupplierRef == merged[j].SupplierRef {
				if merged[i].CustomerRef == merged[j].CustomerRef {
					return merged[i].ProductRef < merged[j].ProductRef
				}
				return merged[i].CustomerRef < merged[j].CustomerRef
			}
			return merged[i].SupplierRef < merged[j].SupplierRef
		}
		return merged[i].Index < merged[j].Index
	})
	return merged
}

func mergeSupplyChainPathEdge(existing, incoming SupplyChainPathEdge) SupplyChainPathEdge {
	if strings.TrimSpace(existing.SupplyLinkID) == "" {
		existing.SupplyLinkID = strings.TrimSpace(incoming.SupplyLinkID)
	}
	if strings.TrimSpace(existing.SupplyLinkCanonicalPath) == "" {
		existing.SupplyLinkCanonicalPath = strings.TrimSpace(incoming.SupplyLinkCanonicalPath)
	}
	if strings.TrimSpace(existing.SupplyLinkTenantID) == "" {
		existing.SupplyLinkTenantID = strings.TrimSpace(incoming.SupplyLinkTenantID)
	}
	if existing.ProductNodeIndex == nil && incoming.ProductNodeIndex != nil {
		idx := *incoming.ProductNodeIndex
		existing.ProductNodeIndex = &idx
	}
	if existing.ViaProductIndex == nil && incoming.ViaProductIndex != nil {
		idx := *incoming.ViaProductIndex
		existing.ViaProductIndex = &idx
	}
	if existing.FromProductIndex == nil && incoming.FromProductIndex != nil {
		idx := *incoming.FromProductIndex
		existing.FromProductIndex = &idx
	}
	if existing.ToProductIndex == nil && incoming.ToProductIndex != nil {
		idx := *incoming.ToProductIndex
		existing.ToProductIndex = &idx
	}
	if strings.TrimSpace(existing.Mode) == "" {
		existing.Mode = strings.TrimSpace(incoming.Mode)
	}
	if strings.TrimSpace(existing.Relation) == "" {
		existing.Relation = strings.TrimSpace(incoming.Relation)
	}
	if !existing.CarriesProduct && incoming.CarriesProduct {
		existing.CarriesProduct = true
	}
	if len(existing.Metrics) == 0 && len(incoming.Metrics) > 0 {
		existing.Metrics = incoming.Metrics
	}
	if len(existing.Qualifiers) == 0 && len(incoming.Qualifiers) > 0 {
		existing.Qualifiers = incoming.Qualifiers
	}
	return existing
}

func mergeSupplyChainPathLeg(existing, incoming SupplyChainPathLeg) SupplyChainPathLeg {
	if strings.TrimSpace(existing.SupplyLinkCanonicalPath) == "" {
		existing.SupplyLinkCanonicalPath = strings.TrimSpace(incoming.SupplyLinkCanonicalPath)
	}
	if strings.TrimSpace(existing.SupplyLinkID) == "" {
		existing.SupplyLinkID = strings.TrimSpace(incoming.SupplyLinkID)
	}
	if strings.TrimSpace(existing.SupplyLinkTenantID) == "" {
		existing.SupplyLinkTenantID = strings.TrimSpace(incoming.SupplyLinkTenantID)
	}
	if strings.TrimSpace(existing.RouteVariantKey) == "" {
		existing.RouteVariantKey = strings.TrimSpace(incoming.RouteVariantKey)
	}
	if existing.SupplierNodeIndex == nil && incoming.SupplierNodeIndex != nil {
		idx := *incoming.SupplierNodeIndex
		existing.SupplierNodeIndex = &idx
	}
	if existing.CustomerNodeIndex == nil && incoming.CustomerNodeIndex != nil {
		idx := *incoming.CustomerNodeIndex
		existing.CustomerNodeIndex = &idx
	}
	if existing.ProductNodeIndex == nil && incoming.ProductNodeIndex != nil {
		idx := *incoming.ProductNodeIndex
		existing.ProductNodeIndex = &idx
	}
	if len(existing.TransportViaNodeIndices) == 0 && len(incoming.TransportViaNodeIndices) > 0 {
		existing.TransportViaNodeIndices = append([]int(nil), incoming.TransportViaNodeIndices...)
	}
	if len(existing.TransportViaRefs) == 0 && len(incoming.TransportViaRefs) > 0 {
		existing.TransportViaRefs = append([]string(nil), incoming.TransportViaRefs...)
	}
	if len(existing.RouteCoordinates) == 0 && len(incoming.RouteCoordinates) > 0 {
		existing.RouteCoordinates = append([]Coordinates(nil), incoming.RouteCoordinates...)
	}
	if strings.TrimSpace(existing.RouteGeometrySource) == "" {
		existing.RouteGeometrySource = strings.TrimSpace(incoming.RouteGeometrySource)
	}
	if strings.TrimSpace(existing.VesselType) == "" {
		existing.VesselType = strings.TrimSpace(incoming.VesselType)
	}
	if strings.TrimSpace(existing.Mode) == "" {
		existing.Mode = strings.TrimSpace(incoming.Mode)
	}
	existing.RouteOverlays = mergeSupplyChainRouteOverlays(existing.RouteOverlays, incoming.RouteOverlays, existing, incoming)
	existing.GeoRefs = mergeUniqueStrings(existing.GeoRefs, incoming.GeoRefs)
	existing.FlowRefs = mergeUniqueStrings(existing.FlowRefs, incoming.FlowRefs)
	existing.AlternateFlowRefs = mergeUniqueStrings(existing.AlternateFlowRefs, incoming.AlternateFlowRefs)
	existing.ZoneRefs = mergeUniqueStrings(existing.ZoneRefs, incoming.ZoneRefs)
	existing.AffectedZoneRefs = mergeUniqueStrings(existing.AffectedZoneRefs, incoming.AffectedZoneRefs)
	existing.SpatialSemantics = mergeUniqueStrings(existing.SpatialSemantics, incoming.SpatialSemantics)
	if existing.BindingConfidence <= 0 && incoming.BindingConfidence > 0 {
		existing.BindingConfidence = incoming.BindingConfidence
	}
	if existing.SegmentConfidence <= 0 && incoming.SegmentConfidence > 0 {
		existing.SegmentConfidence = incoming.SegmentConfidence
	}
	if existing.Stress == nil && incoming.Stress != nil {
		existing.Stress = incoming.Stress
	}
	normalizeSupplyChainPathLegRouteOverlays(&existing)
	return existing
}

func mergeSupplyChainRouteOverlays(existingOverlays, incomingOverlays []SupplyChainRouteOverlay, existingLeg, incomingLeg SupplyChainPathLeg) []SupplyChainRouteOverlay {
	out := make([]SupplyChainRouteOverlay, 0, len(existingOverlays)+len(incomingOverlays)+2)
	indexByKey := make(map[string]int, len(existingOverlays)+len(incomingOverlays)+2)
	appendOverlay := func(overlay SupplyChainRouteOverlay, leg SupplyChainPathLeg) {
		overlay = normalizeSupplyChainRouteOverlay(overlay, leg)
		if strings.TrimSpace(overlay.OverlayKey) == "" {
			return
		}
		if idx, ok := indexByKey[overlay.OverlayKey]; ok {
			current := out[idx]
			current.TransportViaRefs = mergeUniqueStrings(current.TransportViaRefs, overlay.TransportViaRefs)
			current.OrderedRefs = NormalizeOrderedRouteRefs(firstNonEmptyStringSlice(current.OrderedRefs, overlay.OrderedRefs))
			if len(current.RouteCoordinates) == 0 && len(overlay.RouteCoordinates) > 0 {
				current.RouteCoordinates = append([]Coordinates(nil), overlay.RouteCoordinates...)
			}
			if strings.TrimSpace(current.RouteGeometrySource) == "" {
				current.RouteGeometrySource = strings.TrimSpace(overlay.RouteGeometrySource)
			}
			if strings.TrimSpace(current.Mode) == "" {
				current.Mode = strings.TrimSpace(overlay.Mode)
			}
			current.FlowRefs = mergeUniqueStrings(current.FlowRefs, overlay.FlowRefs)
			current.AlternateFlowRefs = mergeUniqueStrings(current.AlternateFlowRefs, overlay.AlternateFlowRefs)
			current.GeoRefs = mergeUniqueStrings(current.GeoRefs, overlay.GeoRefs)
			current.ZoneRefs = mergeUniqueStrings(current.ZoneRefs, overlay.ZoneRefs)
			current.AffectedZoneRefs = mergeUniqueStrings(current.AffectedZoneRefs, overlay.AffectedZoneRefs)
			current.SpatialSemantics = mergeUniqueStrings(current.SpatialSemantics, overlay.SpatialSemantics)
			if strings.TrimSpace(current.VesselType) == "" {
				current.VesselType = strings.TrimSpace(overlay.VesselType)
			}
			if current.BindingConfidence <= 0 && overlay.BindingConfidence > 0 {
				current.BindingConfidence = overlay.BindingConfidence
			}
			if current.SegmentConfidence <= 0 && overlay.SegmentConfidence > 0 {
				current.SegmentConfidence = overlay.SegmentConfidence
			}
			if strings.TrimSpace(current.EndpointBindingPolicy) == "" {
				current.EndpointBindingPolicy = strings.TrimSpace(overlay.EndpointBindingPolicy)
			}
			if strings.TrimSpace(current.Status) == "" {
				current.Status = strings.TrimSpace(overlay.Status)
			}
			out[idx] = normalizeSupplyChainRouteOverlay(current, leg)
			return
		}
		indexByKey[overlay.OverlayKey] = len(out)
		out = append(out, overlay)
	}
	if legacy := synthesizeLegacyRouteOverlay(existingLeg); legacy != nil {
		appendOverlay(*legacy, existingLeg)
	}
	for _, overlay := range existingOverlays {
		appendOverlay(overlay, existingLeg)
	}
	if legacy := synthesizeLegacyRouteOverlay(incomingLeg); legacy != nil {
		appendOverlay(*legacy, incomingLeg)
	}
	for _, overlay := range incomingOverlays {
		appendOverlay(overlay, incomingLeg)
	}
	return out
}

func mergePathSufficiency(existing, incoming *SupplyChainPathSufficiency) *SupplyChainPathSufficiency {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}
	statusRank := func(status string) int {
		switch strings.TrimSpace(status) {
		case SupplyChainSufficiencyForensicGap:
			return 3
		case SupplyChainSufficiencyHeuristic:
			return 2
		case SupplyChainSufficiencyValidated:
			return 1
		default:
			return 0
		}
	}
	merged := *existing
	if incoming.Score > merged.Score {
		merged.Score = incoming.Score
	}
	if statusRank(incoming.Status) > statusRank(merged.Status) {
		merged.Status = incoming.Status
	}
	if strings.TrimSpace(incoming.Summary) != "" {
		merged.Summary = incoming.Summary
	}
	merged.HardFailures = mergeUniqueStrings(merged.HardFailures, incoming.HardFailures)
	merged.Warnings = mergeUniqueStrings(merged.Warnings, incoming.Warnings)
	merged.OpenGaps = mergeUniqueStrings(merged.OpenGaps, incoming.OpenGaps)
	if len(incoming.JoinAssessments) > 0 {
		merged.JoinAssessments = append([]SupplyChainJoinAssessment(nil), incoming.JoinAssessments...)
	}
	if len(incoming.LegAssessments) > 0 {
		merged.LegAssessments = append([]SupplyChainLegAssessment(nil), incoming.LegAssessments...)
	}
	return &merged
}

func mergePathStress(existing, incoming *SupplyChainPathStress) *SupplyChainPathStress {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}
	if incoming.PathScore >= existing.PathScore {
		return incoming
	}
	return existing
}

func replacePathInStringSlice(items []string, oldPath, newPath string) bool {
	changed := false
	for i, item := range items {
		if strings.TrimSpace(item) == oldPath {
			items[i] = newPath
			changed = true
		}
	}
	return changed
}

func mergeUniqueStrings(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, item := range append(append([]string(nil), base...), extra...) {
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
	sort.Strings(out)
	return out
}

// PayloadNodesChanged returns true if the incoming payload added new nodes compared to existing.
func PayloadNodesChanged(existing, merged map[string]interface{}) bool {
	existingParsed, err1 := ParseSupplyChainPathPayload(existing)
	mergedParsed, err2 := ParseSupplyChainPathPayload(merged)
	if err1 != nil || err2 != nil {
		return true
	}
	if len(mergedParsed.Nodes) > len(existingParsed.Nodes) {
		return true
	}
	existingRefs := map[string]bool{}
	for _, n := range existingParsed.Nodes {
		existingRefs[n.Ref] = true
	}
	for _, n := range mergedParsed.Nodes {
		if !existingRefs[n.Ref] {
			return true
		}
	}
	return false
}

func decodeRelationsGraphNodes(raw interface{}) ([]RelationsGraphNode, error) {
	switch nodes := raw.(type) {
	case []RelationsGraphNode:
		return nodes, nil
	case []interface{}:
		out := make([]RelationsGraphNode, 0, len(nodes))
		for i, n := range nodes {
			switch node := n.(type) {
			case RelationsGraphNode:
				out = append(out, node)
			case map[string]interface{}:
				nodeJSON, err := json.Marshal(node)
				if err != nil {
					return nil, fmt.Errorf("node[%d] marshal failed: %w", i, err)
				}
				var parsed RelationsGraphNode
				if err := json.Unmarshal(nodeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("node[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("node[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("'nodes' must be an array")
	}
}

func decodeRelationsGraphEdges(raw interface{}) ([]RelationsGraphEdge, error) {
	switch edges := raw.(type) {
	case []RelationsGraphEdge:
		return edges, nil
	case []interface{}:
		out := make([]RelationsGraphEdge, 0, len(edges))
		for i, e := range edges {
			switch edge := e.(type) {
			case RelationsGraphEdge:
				out = append(out, edge)
			case map[string]interface{}:
				edgeJSON, err := json.Marshal(edge)
				if err != nil {
					return nil, fmt.Errorf("edge[%d] marshal failed: %w", i, err)
				}
				var parsed RelationsGraphEdge
				if err := json.Unmarshal(edgeJSON, &parsed); err != nil {
					return nil, fmt.Errorf("edge[%d] unmarshal failed: %w", i, err)
				}
				out = append(out, parsed)
			default:
				return nil, fmt.Errorf("edge[%d] must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("'edges' must be an array")
	}
}

// BuildRelationsGraphPayload creates a v1 relations graph payload.
func BuildRelationsGraphPayload(nodes []RelationsGraphNode, edges []RelationsGraphEdge, domainMode, rootEntity string, notes map[string]interface{}) map[string]interface{} {
	payload := map[string]interface{}{
		"v":     1,
		"nodes": nodes,
	}
	if len(edges) > 0 {
		payload["edges"] = edges
	}
	if domainMode != "" {
		payload["domainMode"] = domainMode
	}
	if rootEntity != "" {
		payload["rootEntity"] = rootEntity
	}
	if len(notes) > 0 {
		payload["notes"] = notes
	}
	return payload
}

// ParseRelationsGraphPayload parses a relations graph payload into structured types.
func ParseRelationsGraphPayload(payload map[string]interface{}) (*RelationsGraphPayload, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload is nil")
	}
	v, ok := payload["v"]
	if !ok {
		return nil, fmt.Errorf("payload missing version field 'v'")
	}
	version, ok := toFloat64(v)
	if !ok || int(version) != 1 {
		return nil, fmt.Errorf("unsupported payload version")
	}
	nodesRaw, ok := payload["nodes"]
	if !ok {
		return nil, fmt.Errorf("payload missing 'nodes' field")
	}
	nodes, err := decodeRelationsGraphNodes(nodesRaw)
	if err != nil {
		return nil, err
	}
	edges := []RelationsGraphEdge{}
	if edgesRaw, ok := payload["edges"]; ok {
		parsedEdges, err := decodeRelationsGraphEdges(edgesRaw)
		if err != nil {
			return nil, err
		}
		edges = parsedEdges
	}
	domainMode, _ := payload["domainMode"].(string)
	rootEntity, _ := payload["rootEntity"].(string)
	notes := map[string]interface{}{}
	if notesRaw, ok := payload["notes"]; ok {
		if m, ok := notesRaw.(map[string]interface{}); ok {
			notes = m
		}
	}
	return &RelationsGraphPayload{
		Version:    int(version),
		Nodes:      nodes,
		Edges:      edges,
		DomainMode: domainMode,
		RootEntity: rootEntity,
		Notes:      notes,
	}, nil
}

// ReplaceCanonicalPathInArtifact replaces oldPath with newPath in NodeRefs, Payload nodes, and Labels.
// Returns true if any replacement was made. Used when merging fragments so materialized artifacts
// stay consistent with the surviving canonical path.
func ReplaceCanonicalPathInArtifact(artifact *MaterializedArtifact, oldPath, newPath string) bool {
	if artifact == nil || strings.TrimSpace(oldPath) == "" || strings.TrimSpace(newPath) == "" || oldPath == newPath {
		return false
	}
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	changed := false

	// NodeRefs
	for i, ref := range artifact.NodeRefs {
		if strings.TrimSpace(ref) == oldPath {
			artifact.NodeRefs[i] = newPath
			changed = true
		}
	}
	if replacePathInStringSlice(artifact.GeoRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(artifact.FlowRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(artifact.ZoneRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(artifact.AffectedZoneRefs, oldPath, newPath) {
		changed = true
	}
	if strings.TrimSpace(artifact.AnchorSupplier) == oldPath {
		artifact.AnchorSupplier = newPath
		changed = true
	}
	if strings.TrimSpace(artifact.AnchorCustomer) == oldPath {
		artifact.AnchorCustomer = newPath
		changed = true
	}
	if strings.TrimSpace(artifact.ProductAxis) == oldPath {
		artifact.ProductAxis = newPath
		changed = true
	}

	// Labels (anchorSupplier, anchorCustomer, productAxis, geoFrom, geoTo may contain paths)
	labelPathKeys := []string{"anchorSupplier", "anchorCustomer", "productAxis", "geoFrom", "geoTo", "rootEntity"}
	if artifact.Labels != nil {
		for _, k := range labelPathKeys {
			if v, ok := artifact.Labels[k]; ok && strings.TrimSpace(v) == oldPath {
				artifact.Labels[k] = newPath
				changed = true
			}
		}
	}

	// Payload: nodes[].Ref
	if artifact.Payload == nil {
		return changed
	}
	switch artifact.Kind {
	case MaterializedKindSupplyChainPath, MaterializedKindServiceChainPath:
		parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
		if err == nil && parsed != nil {
			for i := range parsed.Nodes {
				if strings.TrimSpace(parsed.Nodes[i].Ref) == oldPath {
					parsed.Nodes[i].Ref = newPath
					changed = true
				}
			}
			for i := range parsed.Edges {
				if strings.TrimSpace(parsed.Edges[i].ProductRef) == oldPath {
					parsed.Edges[i].ProductRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.Edges[i].SupplyLinkCanonicalPath) == oldPath {
					parsed.Edges[i].SupplyLinkCanonicalPath = newPath
					changed = true
				}
			}
			for i := range parsed.Legs {
				if replacePathInStringSlice(parsed.Legs[i].TransportViaRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.Legs[i].GeoRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.Legs[i].FlowRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.Legs[i].AlternateFlowRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.Legs[i].ZoneRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.Legs[i].AffectedZoneRefs, oldPath, newPath) {
					changed = true
				}
				if strings.TrimSpace(parsed.Legs[i].SupplierRef) == oldPath {
					parsed.Legs[i].SupplierRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.Legs[i].CustomerRef) == oldPath {
					parsed.Legs[i].CustomerRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.Legs[i].ProductRef) == oldPath {
					parsed.Legs[i].ProductRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.Legs[i].SupplyLinkCanonicalPath) == oldPath {
					parsed.Legs[i].SupplyLinkCanonicalPath = newPath
					changed = true
				}
				for overlayIdx := range parsed.Legs[i].RouteOverlays {
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].OrderedRefs, oldPath, newPath) {
						changed = true
					}
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].TransportViaRefs, oldPath, newPath) {
						changed = true
					}
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].FlowRefs, oldPath, newPath) {
						changed = true
					}
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].AlternateFlowRefs, oldPath, newPath) {
						changed = true
					}
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].GeoRefs, oldPath, newPath) {
						changed = true
					}
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].ZoneRefs, oldPath, newPath) {
						changed = true
					}
					if replacePathInStringSlice(parsed.Legs[i].RouteOverlays[overlayIdx].AffectedZoneRefs, oldPath, newPath) {
						changed = true
					}
				}
			}
			artifact.Payload = BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
		}
	case MaterializedKindSupplyChainAssemblyGraph:
		parsed, err := ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
		if err == nil && parsed != nil {
			for i := range parsed.ProductNodes {
				if strings.TrimSpace(parsed.ProductNodes[i].Ref) == oldPath {
					parsed.ProductNodes[i].Ref = newPath
					changed = true
				}
			}
			for i := range parsed.ActorNodes {
				if strings.TrimSpace(parsed.ActorNodes[i].Ref) == oldPath {
					parsed.ActorNodes[i].Ref = newPath
					changed = true
				}
			}
			for i := range parsed.SupplyEdges {
				if strings.TrimSpace(parsed.SupplyEdges[i].SupplierRef) == oldPath {
					parsed.SupplyEdges[i].SupplierRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.SupplyEdges[i].ProductRef) == oldPath {
					parsed.SupplyEdges[i].ProductRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.SupplyEdges[i].CustomerRef) == oldPath {
					parsed.SupplyEdges[i].CustomerRef = newPath
					changed = true
				}
				if replacePathInStringSlice(parsed.SupplyEdges[i].TransportViaRefs, oldPath, newPath) {
					changed = true
				}
			}
			for i := range parsed.TransformEdges {
				if strings.TrimSpace(parsed.TransformEdges[i].FromProductRef) == oldPath {
					parsed.TransformEdges[i].FromProductRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.TransformEdges[i].ToProductRef) == oldPath {
					parsed.TransformEdges[i].ToProductRef = newPath
					changed = true
				}
			}
			for i := range parsed.AssemblyEdges {
				if strings.TrimSpace(parsed.AssemblyEdges[i].FromProductRef) == oldPath {
					parsed.AssemblyEdges[i].FromProductRef = newPath
					changed = true
				}
				if strings.TrimSpace(parsed.AssemblyEdges[i].ToProductRef) == oldPath {
					parsed.AssemblyEdges[i].ToProductRef = newPath
					changed = true
				}
			}
			for i := range parsed.RouteBindings {
				if strings.TrimSpace(parsed.RouteBindings[i].ProductRef) == oldPath {
					parsed.RouteBindings[i].ProductRef = newPath
					changed = true
				}
				if replacePathInStringSlice(parsed.RouteBindings[i].ViaRefs, oldPath, newPath) {
					changed = true
				}
			}
			if replacePathInStringSlice(parsed.AnchorProducts, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.AnchorSet, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.SourceAnchors, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.ResolvedAnchors, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.UnresolvedAnchors, oldPath, newPath) {
				changed = true
			}
			for i := range parsed.AnchorComponents {
				if replacePathInStringSlice(parsed.AnchorComponents[i].Anchors, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.AnchorComponents[i].ProductRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.AnchorComponents[i].ActorRefs, oldPath, newPath) {
					changed = true
				}
			}
			for i := range parsed.PrimaryBranches {
				if replacePathInAssemblyBranchSummary(&parsed.PrimaryBranches[i], oldPath, newPath) {
					changed = true
				}
			}
			for i := range parsed.SecondaryBranches {
				if replacePathInAssemblyBranchSummary(&parsed.SecondaryBranches[i], oldPath, newPath) {
					changed = true
				}
			}
			for i := range parsed.SuppressedSupportBranches {
				if replacePathInAssemblyBranchSummary(&parsed.SuppressedSupportBranches[i], oldPath, newPath) {
					changed = true
				}
			}
			if replacePathInStringSlice(parsed.TopRoutes, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.TopDistributionNodes, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.TopSuppliers, oldPath, newPath) {
				changed = true
			}
			if replacePathInStringSlice(parsed.TopProductFamilies, oldPath, newPath) {
				changed = true
			}
			artifact.Payload = BuildSupplyChainAssemblyGraphPayloadDetailed(SupplyChainAssemblyGraphPayloadOptions{
				AnchorSet:                       parsed.AnchorSet,
				SourceAnchors:                   parsed.SourceAnchors,
				ResolvedAnchors:                 parsed.ResolvedAnchors,
				UnresolvedAnchors:               parsed.UnresolvedAnchors,
				AnchorProducts:                  parsed.AnchorProducts,
				AnchorComponents:                parsed.AnchorComponents,
				ProductNodes:                    parsed.ProductNodes,
				ActorNodes:                      parsed.ActorNodes,
				SupplyEdges:                     parsed.SupplyEdges,
				TransformEdges:                  parsed.TransformEdges,
				AssemblyEdges:                   parsed.AssemblyEdges,
				RouteBindings:                   parsed.RouteBindings,
				Coverage:                        parsed.Coverage,
				OpenGaps:                        parsed.OpenGaps,
				DerivedPathKeys:                 parsed.DerivedPathKeys,
				PrimaryBranches:                 parsed.PrimaryBranches,
				SecondaryBranches:               parsed.SecondaryBranches,
				TopRoutes:                       parsed.TopRoutes,
				TopDistributionNodes:            parsed.TopDistributionNodes,
				TopSuppliers:                    parsed.TopSuppliers,
				TopProductFamilies:              parsed.TopProductFamilies,
				SuppressedSupportBranches:       parsed.SuppressedSupportBranches,
				Stage:                           parsed.Stage,
				Provisional:                     parsed.Provisional,
				SourceMode:                      parsed.SourceMode,
				BackgroundContinuationScheduled: parsed.BackgroundContinuationScheduled,
				BackgroundContinuationJobIDs:    parsed.BackgroundContinuationJobIDs,
				Notes:                           parsed.Notes,
			})
		}
	case MaterializedKindRegulatoryImpactMap, MaterializedKindRegulatoryImpactTransmissionPath,
		MaterializedKindRegulatoryTimeline, MaterializedKindRegulatoryCascadeMap,
		MaterializedKindRegulatoryImpactScenario,
		MaterializedKindGovernanceChain, MaterializedKindEcosystemMap:
		parsed, err := ParseRelationsGraphPayload(artifact.Payload)
		if err == nil && parsed != nil {
			for i := range parsed.Nodes {
				if strings.TrimSpace(parsed.Nodes[i].Ref) == oldPath {
					parsed.Nodes[i].Ref = newPath
					changed = true
				}
			}
			if parsed.RootEntity == oldPath {
				parsed.RootEntity = newPath
				changed = true
			}
			artifact.Payload = BuildRelationsGraphPayload(parsed.Nodes, parsed.Edges, parsed.DomainMode, parsed.RootEntity, parsed.Notes)
		}
	}
	if changed {
		PopulateMaterializedArtifactIndexes(artifact)
	}
	return changed
}

func replacePathInAssemblyBranchSummary(branch *SupplyChainAssemblyBranchSummary, oldPath string, newPath string) bool {
	if branch == nil {
		return false
	}
	changed := false
	if replacePathInStringSlice(branch.AnchorRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(branch.ProductRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(branch.ActorRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(branch.RouteRefs, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(branch.EntryNodes, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(branch.ExitNodes, oldPath, newPath) {
		changed = true
	}
	if replacePathInStringSlice(branch.CollapsedSupportingBranchIDs, oldPath, newPath) {
		changed = true
	}
	return changed
}

// ValidateSupplyChainPathStructure performs basic structural validation of a supply chain path.
// Checks that nodes alternate appropriately (org/fac → product → org/fac) and edges reference valid indices.
func ValidateSupplyChainPathStructure(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge) error {
	if len(nodes) == 0 {
		return fmt.Errorf("path must have at least one node")
	}

	// Validate node types
	for i, node := range nodes {
		if node.Type != "org" && node.Type != "fac" && node.Type != "product" && node.Type != "deposit" {
			return fmt.Errorf("node[%d] invalid type: %q (expected org|fac|product|deposit)", i, node.Type)
		}
		if node.Ref == "" {
			return fmt.Errorf("node[%d] missing ref", i)
		}
	}

	// Validate edges reference valid node indices
	for i, edge := range edges {
		if edge.From < 0 || edge.From >= len(nodes) {
			return fmt.Errorf("edge[%d] 'from' index %d out of range (nodes length: %d)", i, edge.From, len(nodes))
		}
		if edge.To < 0 || edge.To >= len(nodes) {
			return fmt.Errorf("edge[%d] 'to' index %d out of range (nodes length: %d)", i, edge.To, len(nodes))
		}
		if edge.ViaProductIndex != nil {
			viaIdx := *edge.ViaProductIndex
			if viaIdx < 0 || viaIdx >= len(nodes) {
				return fmt.Errorf("edge[%d] 'viaProductIndex' %d out of range (nodes length: %d)", i, viaIdx, len(nodes))
			}
			if nodes[viaIdx].Type != "product" {
				return fmt.Errorf("edge[%d] 'viaProductIndex' %d points to non-product node (type: %q)", i, viaIdx, nodes[viaIdx].Type)
			}
		}
		if edge.ProductNodeIndex != nil {
			productIdx := *edge.ProductNodeIndex
			if productIdx < 0 || productIdx >= len(nodes) {
				return fmt.Errorf("edge[%d] 'productNodeIndex' %d out of range (nodes length: %d)", i, productIdx, len(nodes))
			}
			if nodes[productIdx].Type != "product" {
				return fmt.Errorf("edge[%d] 'productNodeIndex' %d points to non-product node (type: %q)", i, productIdx, nodes[productIdx].Type)
			}
		}
		if strings.TrimSpace(edge.ProductRef) != "" && !strings.HasPrefix(strings.TrimSpace(edge.ProductRef), "/product/") {
			return fmt.Errorf("edge[%d] 'productRef' must be a product canonical path", i)
		}
	}

	return nil
}
