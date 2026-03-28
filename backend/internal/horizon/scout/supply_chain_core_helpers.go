package scout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplysemantics"
)

func normalizeAndDedupSupplyLinkSegments(segments []*SupplyLinkSegment) []*SupplyLinkSegment {
	if len(segments) == 0 {
		return nil
	}
	merged := make(map[string]*SupplyLinkSegment, len(segments))
	order := make([]string, 0, len(segments))
	for _, seg := range segments {
		norm := normalizeSupplyLinkSegment(seg)
		if norm == nil {
			continue
		}
		key := supplyLinkSegmentDedupKey(norm)
		if key == "" {
			continue
		}
		if existing := merged[key]; existing != nil {
			mergeSupplyLinkSegment(existing, norm)
			continue
		}
		merged[key] = norm
		order = append(order, key)
	}
	out := make([]*SupplyLinkSegment, 0, len(order))
	for _, key := range order {
		if seg := merged[key]; seg != nil {
			out = append(out, seg)
		}
	}
	return out
}

func deriveSupplyLinkSegmentsFromEdges(edges []*ChainEdge) []*SupplyLinkSegment {
	if len(edges) == 0 {
		return nil
	}
	normalizedEdges := dedupeChainEdges(edges)
	type actorProductKey struct {
		supplier string
		product  string
	}
	producesByActorProduct := make(map[actorProductKey]*ChainEdge)
	productSuppliesByProduct := make(map[string][]*ChainEdge)
	derived := make([]*SupplyLinkSegment, 0, len(normalizedEdges))

	for _, edge := range normalizedEdges {
		norm := normalizeChainEdge(edge)
		if norm == nil {
			continue
		}
		src := strings.TrimSpace(norm.SourceCanonicalPath)
		dst := strings.TrimSpace(norm.TargetCanonicalPath)
		switch strings.TrimSpace(norm.RelationType) {
		case string(models.DependencyTypeProduces):
			if isSupplyChainProducerPath(src) && strings.HasPrefix(dst, "/product/") {
				key := actorProductKey{supplier: src, product: dst}
				if existing := producesByActorProduct[key]; existing == nil || norm.Confidence > existing.Confidence {
					producesByActorProduct[key] = norm
				}
			}
		case string(models.DependencyTypeSuppliesTo):
			product := extractProductQualifier(norm.Qualifiers)
			if isSupplyChainSupplierPath(src) && isSupplyChainCustomerPath(dst) && product != "" && src != dst {
				derived = append(derived, &SupplyLinkSegment{
					SupplierRef:      src,
					ProductRef:       product,
					CustomerRef:      dst,
					TransportViaRefs: parseStateStringSlice(norm.Qualifiers[models.QualifierTransportVia]),
					SegmentClass:     supplysemantics.SegmentClassExternalProductFlow,
					Confidence:       norm.Confidence,
				})
			}
			if strings.HasPrefix(src, "/product/") && isSupplyChainCustomerPath(dst) {
				if product == "" {
					product = src
				}
				if product != "" {
					productSuppliesByProduct[product] = append(productSuppliesByProduct[product], norm)
				}
			}
		}
	}

	for key, produceEdge := range producesByActorProduct {
		productSupplies := productSuppliesByProduct[key.product]
		if len(productSupplies) == 0 {
			continue
		}
		for _, productSupply := range productSupplies {
			if productSupply == nil {
				continue
			}
			customer := strings.TrimSpace(productSupply.TargetCanonicalPath)
			if !isSupplyChainCustomerPath(customer) || key.supplier == customer {
				continue
			}
			confidence := produceEdge.Confidence
			if productSupply.Confidence > confidence {
				confidence = productSupply.Confidence
			}
			derived = append(derived, &SupplyLinkSegment{
				SupplierRef:      key.supplier,
				ProductRef:       key.product,
				CustomerRef:      customer,
				TransportViaRefs: parseStateStringSlice(productSupply.Qualifiers[models.QualifierTransportVia]),
				SegmentClass:     supplysemantics.SegmentClassExternalProductFlow,
				Confidence:       confidence,
			})
		}
	}

	return normalizeAndDedupSupplyLinkSegments(derived)
}

func normalizeSupplyLinkSegment(seg *SupplyLinkSegment) *SupplyLinkSegment {
	if seg == nil {
		return nil
	}
	out := *seg
	out.SupplierRef = strings.TrimSpace(out.SupplierRef)
	out.ProductRef = strings.TrimSpace(out.ProductRef)
	out.CustomerRef = strings.TrimSpace(out.CustomerRef)
	out.SupplierFacilityRef = strings.TrimSpace(out.SupplierFacilityRef)
	out.CustomerFacilityRef = strings.TrimSpace(out.CustomerFacilityRef)
	out.TransportViaRefs = models.NormalizeTransportViaRefs(compactPathList(out.TransportViaRefs))
	if out.SupplierRef == "" || out.ProductRef == "" || out.CustomerRef == "" {
		return nil
	}
	if out.SupplierRef == out.CustomerRef {
		return nil
	}
	if !supplysemantics.IsMaterializableSegmentClass(out.SegmentClass) {
		return nil
	}
	if out.Confidence <= 0 {
		out.Confidence = 0.5
	}
	if out.State != nil {
		stateCopy := make(map[string]models.MetricValue, len(out.State))
		for key, value := range out.State {
			stateCopy[key] = value
		}
		out.State = stateCopy
	}
	if out.FlowEdgeContract != nil {
		contractCopy := *out.FlowEdgeContract
		if len(contractCopy.ValidationGaps) > 0 {
			contractCopy.ValidationGaps = append([]string(nil), contractCopy.ValidationGaps...)
		}
		if len(contractCopy.ParseWarnings) > 0 {
			contractCopy.ParseWarnings = append([]string(nil), contractCopy.ParseWarnings...)
		}
		if contractCopy.RawPayload != nil {
			rawCopy := make(map[string]interface{}, len(contractCopy.RawPayload))
			for key, value := range contractCopy.RawPayload {
				rawCopy[key] = value
			}
			contractCopy.RawPayload = rawCopy
		}
		if contractCopy.Provenance != nil {
			provCopy := *contractCopy.Provenance
			if len(provCopy.SourceIDs) > 0 {
				provCopy.SourceIDs = append([]string(nil), provCopy.SourceIDs...)
			}
			contractCopy.Provenance = &provCopy
		}
		out.FlowEdgeContract = &contractCopy
	}
	return &out
}

func supplyLinkSegmentDedupKey(seg *SupplyLinkSegment) string {
	if seg == nil {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(seg.SupplierRef),
		strings.TrimSpace(seg.ProductRef),
		strings.TrimSpace(seg.CustomerRef),
		strings.TrimSpace(seg.SupplierFacilityRef),
		strings.TrimSpace(seg.CustomerFacilityRef),
		strings.TrimSpace(seg.SegmentClass),
		models.BuildSupplyLinkRouteVariantKey(seg.TransportViaRefs),
	}, "|")
}

func mergeSupplyLinkSegment(dst, incoming *SupplyLinkSegment) {
	if dst == nil || incoming == nil {
		return
	}
	if incoming.Confidence > dst.Confidence {
		dst.Confidence = incoming.Confidence
	}
	dst.TransportViaRefs = mergeUniqueStrings(dst.TransportViaRefs, incoming.TransportViaRefs)
	if dst.SupplierFacilityRef == "" {
		dst.SupplierFacilityRef = strings.TrimSpace(incoming.SupplierFacilityRef)
	}
	if dst.CustomerFacilityRef == "" {
		dst.CustomerFacilityRef = strings.TrimSpace(incoming.CustomerFacilityRef)
	}
	if dst.State == nil && incoming.State != nil {
		dst.State = make(map[string]models.MetricValue, len(incoming.State))
	}
	if dst.State != nil && incoming.State != nil {
		_ = mergeSupplyLinkMetricState(dst.State, incoming.State)
	}
	switch {
	case dst.FlowEdgeContract == nil:
		dst.FlowEdgeContract = incoming.FlowEdgeContract
	case incoming.FlowEdgeContract == nil:
	default:
		dst.FlowEdgeContract.ValidationGaps = mergeUniqueStrings(dst.FlowEdgeContract.ValidationGaps, incoming.FlowEdgeContract.ValidationGaps)
		dst.FlowEdgeContract.ParseWarnings = mergeUniqueStrings(dst.FlowEdgeContract.ParseWarnings, incoming.FlowEdgeContract.ParseWarnings)
		if incoming.FlowEdgeContract.ContractCompleteness == "full" && dst.FlowEdgeContract.ContractCompleteness != "full" {
			dst.FlowEdgeContract.ContractCompleteness = incoming.FlowEdgeContract.ContractCompleteness
		}
		if incoming.FlowEdgeContract.RawPayload != nil {
			if dst.FlowEdgeContract.RawPayload == nil {
				dst.FlowEdgeContract.RawPayload = make(map[string]interface{}, len(incoming.FlowEdgeContract.RawPayload))
			}
			for key, value := range incoming.FlowEdgeContract.RawPayload {
				dst.FlowEdgeContract.RawPayload[key] = value
			}
		}
		if incoming.FlowEdgeContract.Provenance != nil {
			if dst.FlowEdgeContract.Provenance == nil {
				provCopy := *incoming.FlowEdgeContract.Provenance
				if len(provCopy.SourceIDs) > 0 {
					provCopy.SourceIDs = append([]string(nil), provCopy.SourceIDs...)
				}
				dst.FlowEdgeContract.Provenance = &provCopy
			} else {
				if strings.TrimSpace(dst.FlowEdgeContract.Provenance.Method) == "" {
					dst.FlowEdgeContract.Provenance.Method = incoming.FlowEdgeContract.Provenance.Method
				}
				dst.FlowEdgeContract.Provenance.SourceIDs = mergeUniqueStrings(
					dst.FlowEdgeContract.Provenance.SourceIDs,
					incoming.FlowEdgeContract.Provenance.SourceIDs,
				)
			}
		}
	}
}

func buildOperationalEdgesFromSegments(segments []*SupplyLinkSegment) []*ChainEdge {
	if len(segments) == 0 {
		return nil
	}
	out := make([]*ChainEdge, 0, len(segments)*3)
	for _, seg := range normalizeAndDedupSupplyLinkSegments(segments) {
		if seg == nil {
			continue
		}
		supplier := strings.TrimSpace(seg.SupplierRef)
		product := strings.TrimSpace(seg.ProductRef)
		customer := strings.TrimSpace(seg.CustomerRef)
		if isSupplyChainProducerPath(supplier) {
			out = append(out, &ChainEdge{
				SourceCanonicalPath: supplier,
				TargetCanonicalPath: product,
				RelationType:        string(models.DependencyTypeProduces),
				Confidence:          seg.Confidence,
				Reason:              fmt.Sprintf("Supply chain segment: %s produces %s", supplier, product),
			})
		}
		if isSupplyChainCustomerPath(customer) {
			qualifiers := map[string]interface{}{
				"product_ref":     product,
				"productRef":      product,
				"source_role":     "product",
				"target_role":     "customer",
				"direction_basis": "supply_flow",
			}
			if len(seg.TransportViaRefs) > 0 {
				qualifiers[models.QualifierTransportVia] = append([]string(nil), seg.TransportViaRefs...)
			}
			out = append(out, &ChainEdge{
				SourceCanonicalPath: product,
				TargetCanonicalPath: customer,
				RelationType:        string(models.DependencyTypeSuppliesTo),
				Confidence:          seg.Confidence,
				Reason:              fmt.Sprintf("Supply chain segment: %s supplied to %s", product, customer),
				Qualifiers:          qualifiers,
			})
		}
		if isSupplyChainSupplierPath(supplier) && isSupplyChainCustomerPath(customer) && supplier != customer {
			qualifiers := map[string]interface{}{
				"product_ref":     product,
				"productRef":      product,
				"source_role":     "supplier",
				"target_role":     "customer",
				"direction_basis": "supply_flow",
			}
			if len(seg.TransportViaRefs) > 0 {
				qualifiers[models.QualifierTransportVia] = append([]string(nil), seg.TransportViaRefs...)
			}
			out = append(out, &ChainEdge{
				SourceCanonicalPath: supplier,
				TargetCanonicalPath: customer,
				RelationType:        string(models.DependencyTypeSuppliesTo),
				Confidence:          seg.Confidence,
				Reason:              fmt.Sprintf("Supply chain segment: %s supplies %s to %s", supplier, product, customer),
				Qualifiers:          qualifiers,
			})
		}
	}
	return dedupeChainEdges(out)
}

func dedupeChainEdges(edges []*ChainEdge) []*ChainEdge {
	if len(edges) == 0 {
		return nil
	}
	merged := make(map[string]*ChainEdge, len(edges))
	order := make([]string, 0, len(edges))
	for _, edge := range edges {
		norm := normalizeChainEdge(edge)
		if norm == nil {
			continue
		}
		key := chainEdgeDedupKeyByPath(norm)
		if key == "" {
			continue
		}
		if existing := merged[key]; existing != nil {
			mergeChainEdges(existing, norm)
			continue
		}
		merged[key] = norm
		order = append(order, key)
	}
	out := make([]*ChainEdge, 0, len(order))
	for _, key := range order {
		if edge := merged[key]; edge != nil {
			out = append(out, edge)
		}
	}
	return out
}

func normalizeChainEdge(edge *ChainEdge) *ChainEdge {
	if edge == nil {
		return nil
	}
	src := strings.TrimSpace(edge.SourceCanonicalPath)
	dst := strings.TrimSpace(edge.TargetCanonicalPath)
	rel := strings.TrimSpace(edge.RelationType)
	if src == "" || dst == "" || rel == "" || src == dst {
		return nil
	}
	out := *edge
	out.SourceCanonicalPath = src
	out.TargetCanonicalPath = dst
	out.RelationType = rel
	if out.Confidence <= 0 {
		out.Confidence = 0.5
	}
	if edge.Qualifiers != nil {
		out.Qualifiers = cloneQualifierMap(edge.Qualifiers)
		normalizeChainEdgeQualifiers(out.Qualifiers)
	}
	return &out
}

func rewriteChainEdgeWithPathRewrites(edge *ChainEdge, pathRewrites map[string]string) *ChainEdge {
	norm := normalizeChainEdge(edge)
	if norm == nil {
		return nil
	}
	out := *norm
	out.SourceCanonicalPath = rewriteCanonicalPathWithMap(out.SourceCanonicalPath, pathRewrites)
	out.TargetCanonicalPath = rewriteCanonicalPathWithMap(out.TargetCanonicalPath, pathRewrites)
	if out.Qualifiers != nil {
		out.Qualifiers = rewriteChainEdgeQualifierRefs(out.Qualifiers, pathRewrites)
	}
	return normalizeChainEdge(&out)
}

func chainEdgeDedupKeyByPath(edge *ChainEdge) string {
	if edge == nil {
		return ""
	}
	parts := []string{
		strings.TrimSpace(edge.SourceCanonicalPath),
		strings.TrimSpace(edge.TargetCanonicalPath),
		strings.TrimSpace(edge.RelationType),
	}
	if variant := chainEdgeVariantKey(edge.RelationType, edge.Qualifiers); variant != "" {
		parts = append(parts, variant)
	}
	return strings.Join(parts, "|")
}

func chainEdgeVariantKey(relationType string, qualifiers map[string]interface{}) string {
	switch strings.TrimSpace(relationType) {
	case string(models.DependencyTypeSuppliesTo):
		if product := extractProductQualifier(qualifiers); product != "" {
			return "product=" + product
		}
	case string(models.DependencyTypeTransformsInto), string(models.DependencyTypeAssemblesInto):
		if atFacility := strings.TrimSpace(parseStateString(qualifiers[models.QualifierAtFacility])); atFacility != "" {
			return "at_facility=" + atFacility
		}
	}
	return ""
}

func mergeChainEdges(dst, incoming *ChainEdge) {
	if dst == nil || incoming == nil {
		return
	}
	if incoming.Confidence > dst.Confidence {
		dst.Confidence = incoming.Confidence
	}
	if len(strings.TrimSpace(incoming.Reason)) > len(strings.TrimSpace(dst.Reason)) {
		dst.Reason = incoming.Reason
	}
	dst.Qualifiers = mergeChainEdgeQualifiers(dst.Qualifiers, incoming.Qualifiers)
}

func mergeChainEdgeQualifiers(existing, incoming map[string]interface{}) map[string]interface{} {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	out := cloneQualifierMap(existing)
	if out == nil {
		out = map[string]interface{}{}
	}
	for key, value := range incoming {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		switch key {
		case models.QualifierTransportVia:
			out[key] = compactPathList(append(parseStateStringSlice(out[key]), parseStateStringSlice(value)...))
		default:
			out[key] = value
		}
	}
	normalizeChainEdgeQualifiers(out)
	return out
}

func cloneQualifierMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizeChainEdgeQualifiers(q map[string]interface{}) {
	if len(q) == 0 {
		return
	}
	if product := extractProductQualifier(q); product != "" {
		q["product_ref"] = product
		q["productRef"] = product
	}
	if rawVia, ok := q[models.QualifierTransportVia]; ok {
		if via := compactPathList(parseStateStringSlice(rawVia)); len(via) > 0 {
			q[models.QualifierTransportVia] = via
		} else {
			delete(q, models.QualifierTransportVia)
		}
	}
}

func rewriteChainEdgeQualifierRefs(q map[string]interface{}, pathRewrites map[string]string) map[string]interface{} {
	if len(q) == 0 {
		return nil
	}
	out := cloneQualifierMap(q)
	if out == nil {
		return nil
	}
	if product := extractProductQualifier(out); product != "" {
		product = rewriteCanonicalPathWithMap(product, pathRewrites)
		out["product_ref"] = product
		out["productRef"] = product
	}
	if atFacility := strings.TrimSpace(parseStateString(out[models.QualifierAtFacility])); atFacility != "" {
		out[models.QualifierAtFacility] = rewriteCanonicalPathWithMap(atFacility, pathRewrites)
	}
	if rawVia, ok := out[models.QualifierTransportVia]; ok {
		rewrittenVia := make([]string, 0, len(parseStateStringSlice(rawVia)))
		for _, via := range parseStateStringSlice(rawVia) {
			if rewritten := rewriteCanonicalPathWithMap(via, pathRewrites); rewritten != "" {
				rewrittenVia = append(rewrittenVia, rewritten)
			}
		}
		if len(rewrittenVia) > 0 {
			out[models.QualifierTransportVia] = compactPathList(rewrittenVia)
		} else {
			delete(out, models.QualifierTransportVia)
		}
	}
	normalizeChainEdgeQualifiers(out)
	return out
}

func rewriteCanonicalPathWithMap(path string, pathRewrites map[string]string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if len(pathRewrites) == 0 {
		return path
	}
	if rewritten, ok := pathRewrites[path]; ok && strings.TrimSpace(rewritten) != "" {
		return strings.TrimSpace(rewritten)
	}
	return path
}

func extractProductQualifier(q map[string]interface{}) string {
	if q == nil {
		return ""
	}
	for _, key := range []string{"product_ref", "productRef"} {
		if value, ok := q[key]; ok {
			if product := strings.TrimSpace(parseStateString(value)); strings.HasPrefix(product, "/product/") {
				return product
			}
		}
	}
	return ""
}

func compactPathList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
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

func isSupplyChainProducerPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/fac/") || strings.HasPrefix(path, "/deposit/")
}

func isSupplyChainSupplierPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/fac/")
}

func isSupplyChainCustomerPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/fac/") || strings.HasPrefix(path, "/gov/")
}
