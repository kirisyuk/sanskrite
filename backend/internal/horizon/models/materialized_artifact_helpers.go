package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// BuildSupplyChainPathPayload creates a v1 SUPPLY_CHAIN_PATH payload from nodes, edges, optional legs, and stress profile.
func BuildSupplyChainPathPayload(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge, legs []SupplyChainPathLeg, stress *SupplyChainPathStress, notes map[string]interface{}) map[string]interface{} {
	return BuildSupplyChainPathPayloadWithSufficiency(nodes, edges, legs, stress, nil, notes)
}

func BuildSupplyChainPathPayloadWithSufficiency(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge, legs []SupplyChainPathLeg, stress *SupplyChainPathStress, sufficiency *SupplyChainPathSufficiency, notes map[string]interface{}) map[string]interface{} {
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
	if sufficiency != nil {
		payload["sufficiency"] = sufficiency
	}
	if len(notes) > 0 {
		payload["notes"] = notes
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
		legs = parsedLegs
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

	notes := map[string]interface{}{}
	if notesRaw, ok := payload["notes"]; ok {
		notesMap, ok := notesRaw.(map[string]interface{})
		if ok {
			notes = notesMap
		}
	}

	return &SupplyChainPathPayload{
		Version: int(version),
		Nodes:   nodes,
		Edges:   edges,
		Legs:    legs,
		Stress:  stress,
		Sufficiency: sufficiency,
		Notes:   notes,
	}, nil
}

// RepairSupplyChainPathLinkage fills missing supply-link linkage metadata on path legs and operational edges.
// resolver is optional; when provided it may resolve canonical path -> (id, tenantId, ok).
func RepairSupplyChainPathLinkage(artifact *MaterializedArtifact, resolver func(string) (string, string, bool)) (SupplyChainLinkageSummary, error) {
	summary := SupplyChainLinkageSummary{}
	if artifact == nil || artifact.Kind != MaterializedKindSupplyChainPath {
		return summary, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH")
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
		artifact.Payload = BuildSupplyChainPathPayloadWithSufficiency(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Notes)
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
		if edge.Type == "SUPPLY_LINK" {
			edge.Role = "product_flow"
			changed = true
		} else if edge.Type == "LOGISTICS" {
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
	if artifact.Kind != MaterializedKindSupplyChainPath {
		return nil, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH")
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
	if artifact.Kind != MaterializedKindSupplyChainPath {
		return nil, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH")
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
	if artifact.Kind != MaterializedKindSupplyChainPath {
		return nil, fmt.Errorf("artifact kind is not SUPPLY_CHAIN_PATH")
	}

	parsed, err := ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil {
		return nil, fmt.Errorf("parse payload failed: %w", err)
	}

	return parsed.Legs, nil
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

// PopulateSupplyChainPathIndexes fills top-level overlap indexes from payload and labels.
// It is safe for both new and legacy artifacts and does not require persistence.
func PopulateSupplyChainPathIndexes(artifact *MaterializedArtifact) {
	if artifact == nil || artifact.Kind != MaterializedKindSupplyChainPath {
		return
	}
	artifact.NodeRefs = uniqueTrimmedStrings(artifact.NodeRefs)
	artifact.GeoRefs = uniqueTrimmedStrings(artifact.GeoRefs)
	artifact.FlowRefs = uniqueTrimmedStrings(artifact.FlowRefs)
	artifact.ZoneRefs = uniqueTrimmedStrings(artifact.ZoneRefs)
	artifact.AffectedZoneRefs = uniqueTrimmedStrings(artifact.AffectedZoneRefs)

	if parsed, err := ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		summary := SummarizeSupplyChainPathBindings(parsed)
		artifact.NodeRefs = mergeUniqueStrings(artifact.NodeRefs, collectParsedNodeRefs(parsed))
		artifact.GeoRefs = mergeUniqueStrings(artifact.GeoRefs, summary.GeoRefs)
		artifact.FlowRefs = mergeUniqueStrings(artifact.FlowRefs, summary.FlowRefs)
		artifact.ZoneRefs = mergeUniqueStrings(artifact.ZoneRefs, summary.ZoneRefs)
		artifact.AffectedZoneRefs = mergeUniqueStrings(artifact.AffectedZoneRefs, summary.AffectedZoneRefs)
	}
	PopulateSupplyChainPathSufficiency(artifact)

	if artifact.Labels != nil {
		if artifact.AnchorSupplier == "" {
			artifact.AnchorSupplier = strings.TrimSpace(artifact.Labels["anchorSupplier"])
		}
		if artifact.AnchorCustomer == "" {
			artifact.AnchorCustomer = strings.TrimSpace(artifact.Labels["anchorCustomer"])
		}
		if artifact.ProductAxis == "" {
			artifact.ProductAxis = strings.TrimSpace(artifact.Labels["productAxis"])
		}
	}
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
	case MaterializedKindSupplyChainPath:
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
	case MaterializedKindGovernanceChain, MaterializedKindRegulatoryImpactMap, MaterializedKindEcosystemMap:
		kindLabel := map[MaterializedKind]string{
			MaterializedKindGovernanceChain:     "Governance Chain",
			MaterializedKindRegulatoryImpactMap: "Regulatory Impact Map",
			MaterializedKindEcosystemMap:        "Ecosystem Map",
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

	return BuildSupplyChainPathPayloadWithSufficiency(
		mergedNodes,
		mergedEdges,
		mergedLegs,
		mergePathStress(existingParsed.Stress, incomingParsed.Stress),
		mergePathSufficiency(existingParsed.Sufficiency, incomingParsed.Sufficiency),
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
		Supplier string
		Customer string
		Product  string
	}
	indexByKey := map[legKey]int{}
	merged := make([]SupplyChainPathLeg, 0, len(existing)+len(incoming))
	appendIfNew := func(legs []SupplyChainPathLeg) {
		for _, leg := range legs {
			k := legKey{
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
	if strings.TrimSpace(existing.Mode) == "" {
		existing.Mode = strings.TrimSpace(incoming.Mode)
	}
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
	return existing
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
	case MaterializedKindSupplyChainPath:
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
			}
			for i := range parsed.Legs {
				if replacePathInStringSlice(parsed.Legs[i].TransportViaRefs, oldPath, newPath) {
					changed = true
				}
				if replacePathInStringSlice(parsed.Legs[i].GeoRefs, oldPath, newPath) {
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
			}
			artifact.Payload = BuildSupplyChainPathPayloadWithSufficiency(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Notes)
		}
	case MaterializedKindRegulatoryImpactMap, MaterializedKindGovernanceChain, MaterializedKindEcosystemMap:
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
