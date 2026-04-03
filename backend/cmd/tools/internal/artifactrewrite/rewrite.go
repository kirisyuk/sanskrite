package artifactrewrite

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type Result struct {
	NodePath     string
	NodeFound    bool
	NodeCoords   *models.Coordinates
	NodeHomeName string
	Inspected    int
	Changed      int
	Applied      int
}

func RewriteNodeArtifacts(
	ctx context.Context,
	db *firestore.Client,
	nodeRef string,
	lensID string,
	limit int,
	apply bool,
	out io.Writer,
) (Result, error) {
	result := Result{NodePath: strings.TrimSpace(nodeRef)}
	if db == nil || strings.TrimSpace(nodeRef) == "" {
		return result, nil
	}

	artifactsRepo := hrepo.NewMaterializedArtifactsRepository(db)
	fragmentsRepo := hrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := hrepo.NewDependencyEdgesRepository(db)
	builder := pathbuilder.NewSupplyChainPathBuilder(fragmentsRepo, edgesRepo, nil)

	nodeFrag, err := fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(nodeRef))
	if err != nil {
		return result, err
	}
	if nodeFrag != nil {
		result.NodeFound = true
		if nodeFrag.Coordinates != nil {
			coords := *nodeFrag.Coordinates
			result.NodeCoords = &coords
		}
		if nodeFrag.HomeLocation != nil {
			result.NodeHomeName = strings.TrimSpace(nodeFrag.HomeLocation.Name)
		}
		if out != nil && nodeFrag.Coordinates != nil {
			fmt.Fprintf(out, "node=%s coords=%.6f,%.6f home=%s\n",
				strings.TrimSpace(nodeFrag.CanonicalPath),
				nodeFrag.Coordinates.Lat,
				nodeFrag.Coordinates.Lon,
				result.NodeHomeName,
			)
		}
	}

	artifacts, err := artifactsRepo.FindByNodeRefAnyTenant(ctx, strings.TrimSpace(nodeRef), models.MaterializedKindSupplyChainPath, limit)
	if err != nil {
		return result, err
	}

	sort.Slice(artifacts, func(i, j int) bool {
		if strings.TrimSpace(artifacts[i].LensID) == strings.TrimSpace(artifacts[j].LensID) {
			return strings.TrimSpace(artifacts[i].CanonicalKey) < strings.TrimSpace(artifacts[j].CanonicalKey)
		}
		return strings.TrimSpace(artifacts[i].LensID) < strings.TrimSpace(artifacts[j].LensID)
	})

	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if strings.TrimSpace(lensID) != "" && strings.TrimSpace(artifact.LensID) != strings.TrimSpace(lensID) {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		result.Inspected++
		if !artifactTouchesNode(parsed, strings.TrimSpace(nodeRef)) {
			continue
		}

		path := rebuildPathFromLegs(parsed)
		draft := builder.BuildMaterializationDraft(ctx, path)
		if draft == nil {
			continue
		}
		mergedLegs, mergedStress, mergedSufficiency, ok := mergeRewrittenArtifactPayload(parsed, draft)
		if !ok {
			if out != nil {
				fmt.Fprintf(out, "artifact=%s lens=%s skipped=leg_shape_changed current_legs=%d rebuilt_legs=%d\n",
					strings.TrimSpace(artifact.CanonicalKey),
					strings.TrimSpace(artifact.LensID),
					len(parsed.Legs),
					len(draft.Legs),
				)
			}
			continue
		}
		artifactChanged := !legsEqualForRewrite(parsed.Legs, mergedLegs) ||
			fmt.Sprintf("%v", parsed.Stress) != fmt.Sprintf("%v", mergedStress) ||
			fmt.Sprintf("%v", parsed.Sufficiency) != fmt.Sprintf("%v", mergedSufficiency)
		if out != nil {
			for idx := range mergedLegs {
				leg := &mergedLegs[idx]
				fmt.Fprintf(out, "artifact=%s lens=%s leg=%d source=%s coords=%d via=%s\n",
					strings.TrimSpace(artifact.CanonicalKey),
					strings.TrimSpace(artifact.LensID),
					idx,
					strings.TrimSpace(leg.RouteGeometrySource),
					len(leg.RouteCoordinates),
					strings.Join(leg.TransportViaRefs, " -> "),
				)
				if len(leg.RouteCoordinates) > 0 {
					first := leg.RouteCoordinates[0]
					mid := leg.RouteCoordinates[len(leg.RouteCoordinates)/2]
					last := leg.RouteCoordinates[len(leg.RouteCoordinates)-1]
					fmt.Fprintf(out, "  first=%.6f,%.6f mid=%.6f,%.6f last=%.6f,%.6f\n",
						first.Lat, first.Lon,
						mid.Lat, mid.Lon,
						last.Lat, last.Lon,
					)
				}
			}
		}
		if artifactChanged {
			result.Changed++
			if apply {
				artifact.Payload = models.BuildSupplyChainPathPayloadDetailed(
					parsed.Nodes,
					parsed.Edges,
					mergedLegs,
					mergedStress,
					mergedSufficiency,
					parsed.Family,
					parsed.Notes,
				)
				if err := artifactsRepo.Update(ctx, artifact); err != nil {
					return result, err
				}
				result.Applied++
			}
		}
	}

	if out != nil {
		fmt.Fprintf(out, "inspected=%d changed=%d applied=%d\n", result.Inspected, result.Changed, result.Applied)
	}
	return result, nil
}

func artifactTouchesNode(parsed *models.SupplyChainPathPayload, nodeRef string) bool {
	if parsed == nil || strings.TrimSpace(nodeRef) == "" {
		return false
	}
	for idx := range parsed.Legs {
		if legTouchesNode(&parsed.Legs[idx], nodeRef) {
			return true
		}
	}
	return false
}

func rebuildPathFromLegs(parsed *models.SupplyChainPathPayload) *pathbuilder.SupplyChainPath {
	if parsed == nil {
		return nil
	}
	segments := make([]pathbuilder.PathSegment, 0, len(parsed.Legs))
	for _, leg := range parsed.Legs {
		segments = append(segments, pathbuilder.PathSegment{
			SupplyLinkID:            strings.TrimSpace(leg.SupplyLinkID),
			SupplyLinkCanonicalPath: strings.TrimSpace(leg.SupplyLinkCanonicalPath),
			RouteVariantKey:         strings.TrimSpace(leg.RouteVariantKey),
			SupplyLinkTenantID:      strings.TrimSpace(leg.SupplyLinkTenantID),
			SupplierRef:             strings.TrimSpace(leg.SupplierRef),
			ProductRef:              strings.TrimSpace(leg.ProductRef),
			CustomerRef:             strings.TrimSpace(leg.CustomerRef),
			TransportViaRefs:        append([]string(nil), leg.TransportViaRefs...),
			VesselType:              strings.TrimSpace(leg.VesselType),
			SegmentClass:            "",
			Confidence:              leg.SegmentConfidence,
			TenantID:                strings.TrimSpace(leg.SupplyLinkTenantID),
		})
	}
	return &pathbuilder.SupplyChainPath{
		Segments: segments,
		Family:   parsed.Family,
	}
}

func mergeRewrittenArtifactPayload(
	parsed *models.SupplyChainPathPayload,
	draft *pathbuilder.SupplyChainMaterializationDraft,
) ([]models.SupplyChainPathLeg, *models.SupplyChainPathStress, *models.SupplyChainPathSufficiency, bool) {
	if parsed == nil || draft == nil {
		return nil, nil, nil, false
	}
	if len(parsed.Legs) != len(draft.Legs) {
		return nil, nil, nil, false
	}
	merged := make([]models.SupplyChainPathLeg, len(parsed.Legs))
	for idx := range parsed.Legs {
		next, ok := mergeRewrittenLeg(parsed.Legs[idx], draft.Legs[idx])
		if !ok {
			return nil, nil, nil, false
		}
		merged[idx] = next
	}
	return merged, draft.Stress, draft.Sufficiency, true
}

func mergeRewrittenLeg(current models.SupplyChainPathLeg, rebuilt models.SupplyChainPathLeg) (models.SupplyChainPathLeg, bool) {
	if !sameLegIdentity(current, rebuilt) {
		return models.SupplyChainPathLeg{}, false
	}
	current.TransportViaRefs = append([]string(nil), rebuilt.TransportViaRefs...)
	if !intSlicesEqual(current.TransportViaNodeIndices, rebuilt.TransportViaNodeIndices) {
		current.TransportViaNodeIndices = append([]int(nil), rebuilt.TransportViaNodeIndices...)
	}
	current.RouteCoordinates = append([]models.Coordinates(nil), rebuilt.RouteCoordinates...)
	current.RouteGeometrySource = strings.TrimSpace(rebuilt.RouteGeometrySource)
	if strings.TrimSpace(rebuilt.VesselType) != "" || len(current.RouteCoordinates) > 0 {
		current.VesselType = strings.TrimSpace(rebuilt.VesselType)
	}
	current.Mode = strings.TrimSpace(rebuilt.Mode)
	current.GeoRefs = append([]string(nil), rebuilt.GeoRefs...)
	flowRefs, alternateFlowRefs := rewriteLegFlowRefs(current.FlowRefs, current.AlternateFlowRefs, rebuilt.FlowRefs)
	current.FlowRefs = flowRefs
	current.AlternateFlowRefs = alternateFlowRefs
	current.ZoneRefs = append([]string(nil), rebuilt.ZoneRefs...)
	current.AffectedZoneRefs = append([]string(nil), rebuilt.AffectedZoneRefs...)
	current.SpatialSemantics = append([]string(nil), rebuilt.SpatialSemantics...)
	current.BindingConfidence = rebuilt.BindingConfidence
	if rebuilt.SegmentConfidence > 0 {
		current.SegmentConfidence = rebuilt.SegmentConfidence
	}
	if len(current.RouteOverlays) > 0 || len(rebuilt.RouteOverlays) > 0 {
		_ = rewritePrimaryOverlays(&current, pathbuilder.ResolvedRouteGeometry{
			Coordinates: append([]models.Coordinates(nil), rebuilt.RouteCoordinates...),
			FlowRefs:    append([]string(nil), rebuilt.FlowRefs...),
			Source:      strings.TrimSpace(rebuilt.RouteGeometrySource),
		}, rebuilt.FlowRefs, rebuilt.AlternateFlowRefs, strings.TrimSpace(rebuilt.RouteGeometrySource))
	}
	return current, true
}

func sameLegIdentity(left, right models.SupplyChainPathLeg) bool {
	return strings.TrimSpace(left.SupplierRef) == strings.TrimSpace(right.SupplierRef) &&
		strings.TrimSpace(left.CustomerRef) == strings.TrimSpace(right.CustomerRef) &&
		strings.TrimSpace(left.ProductRef) == strings.TrimSpace(right.ProductRef) &&
		strings.TrimSpace(left.SupplyLinkCanonicalPath) == strings.TrimSpace(right.SupplyLinkCanonicalPath) &&
		strings.TrimSpace(left.RouteVariantKey) == strings.TrimSpace(right.RouteVariantKey)
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}

func legsEqualForRewrite(left, right []models.SupplyChainPathLeg) bool {
	return fmt.Sprintf("%v", left) == fmt.Sprintf("%v", right)
}

func edgesEqualForRewrite(left, right []models.SupplyChainPathEdge) bool {
	return fmt.Sprintf("%v", left) == fmt.Sprintf("%v", right)
}

func nodesEqualForRewrite(left, right []models.SupplyChainPathNode) bool {
	return fmt.Sprintf("%v", left) == fmt.Sprintf("%v", right)
}

func legTouchesNode(leg *models.SupplyChainPathLeg, nodeRef string) bool {
	if leg == nil || nodeRef == "" {
		return false
	}
	if strings.TrimSpace(leg.SupplierRef) == nodeRef || strings.TrimSpace(leg.CustomerRef) == nodeRef {
		return true
	}
	for _, via := range leg.TransportViaRefs {
		if strings.TrimSpace(via) == nodeRef {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendUniqueStrings(base []string, extra ...string) []string {
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

func rewriteLegFlowRefs(current []string, currentAlternate []string, resolved []string) ([]string, []string) {
	primary := appendUniqueStrings(nil, resolved...)
	if len(primary) == 0 {
		primary = appendUniqueStrings(nil, current...)
	}
	alternate := appendUniqueStrings(nil, currentAlternate...)
	for _, value := range current {
		value = strings.TrimSpace(value)
		if value == "" || stringSliceContains(primary, value) {
			continue
		}
		alternate = appendUniqueStrings(alternate, value)
	}
	for _, value := range resolved {
		value = strings.TrimSpace(value)
		if value == "" || stringSliceContains(primary, value) {
			continue
		}
		alternate = appendUniqueStrings(alternate, value)
	}
	return primary, removeStringSliceValues(alternate, primary...)
}

func rewritePrimaryOverlays(
	leg *models.SupplyChainPathLeg,
	resolved pathbuilder.ResolvedRouteGeometry,
	flowRefs []string,
	alternateFlowRefs []string,
	source string,
) bool {
	if leg == nil || len(leg.RouteOverlays) == 0 {
		return false
	}
	changed := false
	updatedAny := false
	for idx := range leg.RouteOverlays {
		overlay := &leg.RouteOverlays[idx]
		if len(leg.RouteOverlays) > 1 && strings.TrimSpace(overlay.Status) != "primary" {
			continue
		}
		if len(resolved.Coordinates) >= 2 {
			if !routeCoordinatesEqual(overlay.RouteCoordinates, resolved.Coordinates) || strings.TrimSpace(overlay.RouteGeometrySource) != source {
				overlay.RouteCoordinates = append([]models.Coordinates(nil), resolved.Coordinates...)
				overlay.RouteGeometrySource = source
				changed = true
			}
		} else if len(overlay.RouteCoordinates) > 0 || strings.TrimSpace(overlay.RouteGeometrySource) != "" {
			overlay.RouteCoordinates = nil
			overlay.RouteGeometrySource = ""
			changed = true
		}
		if !stringSlicesEqual(overlay.FlowRefs, flowRefs) {
			overlay.FlowRefs = append([]string(nil), flowRefs...)
			changed = true
		}
		if !stringSlicesEqual(overlay.AlternateFlowRefs, alternateFlowRefs) {
			overlay.AlternateFlowRefs = append([]string(nil), alternateFlowRefs...)
			changed = true
		}
		updatedAny = true
	}
	if updatedAny || len(leg.RouteOverlays) == 0 {
		return changed
	}
	overlay := &leg.RouteOverlays[0]
	if len(resolved.Coordinates) >= 2 {
		if !routeCoordinatesEqual(overlay.RouteCoordinates, resolved.Coordinates) || strings.TrimSpace(overlay.RouteGeometrySource) != source {
			overlay.RouteCoordinates = append([]models.Coordinates(nil), resolved.Coordinates...)
			overlay.RouteGeometrySource = source
			changed = true
		}
	} else if len(overlay.RouteCoordinates) > 0 || strings.TrimSpace(overlay.RouteGeometrySource) != "" {
		overlay.RouteCoordinates = nil
		overlay.RouteGeometrySource = ""
		changed = true
	}
	if !stringSlicesEqual(overlay.FlowRefs, flowRefs) {
		overlay.FlowRefs = append([]string(nil), flowRefs...)
		changed = true
	}
	if !stringSlicesEqual(overlay.AlternateFlowRefs, alternateFlowRefs) {
		overlay.AlternateFlowRefs = append([]string(nil), alternateFlowRefs...)
		changed = true
	}
	return changed
}

func stringSliceContains(values []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func removeStringSliceValues(values []string, remove ...string) []string {
	if len(values) == 0 {
		return nil
	}
	blocked := make(map[string]struct{}, len(remove))
	for _, value := range remove {
		value = strings.TrimSpace(value)
		if value != "" {
			blocked[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := blocked[value]; ok {
			continue
		}
		if stringSliceContains(out, value) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if strings.TrimSpace(a[idx]) != strings.TrimSpace(b[idx]) {
			return false
		}
	}
	return true
}

func routeCoordinatesEqual(a, b []models.Coordinates) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx].Lat != b[idx].Lat || a[idx].Lon != b[idx].Lon {
			return false
		}
	}
	return true
}
