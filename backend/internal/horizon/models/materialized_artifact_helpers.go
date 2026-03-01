package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

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

// BuildSupplyChainPathPayload creates a v1 SUPPLY_CHAIN_PATH payload from nodes and edges.
func BuildSupplyChainPathPayload(nodes []SupplyChainPathNode, edges []SupplyChainPathEdge, notes map[string]interface{}) map[string]interface{} {
	payload := map[string]interface{}{
		"v":     1,
		"nodes": nodes,
	}
	if len(edges) > 0 {
		payload["edges"] = edges
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
		Notes:   notes,
	}, nil
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
					if edge.Type == "ONTOLOGY" && edge.Relation != "" {
						b.WriteString("Transformation: ")
						b.WriteString(edge.Relation)
						b.WriteString("\n")
					}
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

	mergedNotes := map[string]interface{}{}
	for k, v := range existingParsed.Notes {
		mergedNotes[k] = v
	}
	for k, v := range incomingParsed.Notes {
		mergedNotes[k] = v
	}

	return BuildSupplyChainPathPayload(mergedNodes, mergedEdges, mergedNotes)
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
		From, To int
		Type     string
	}
	seen := map[edgeKey]bool{}
	merged := make([]SupplyChainPathEdge, 0, len(existing)+len(incoming))
	for _, e := range existing {
		k := edgeKey{e.From, e.To, e.Type}
		if !seen[k] {
			merged = append(merged, e)
			seen[k] = true
		}
	}
	for _, e := range incoming {
		k := edgeKey{e.From, e.To, e.Type}
		if !seen[k] {
			merged = append(merged, e)
			seen[k] = true
		}
	}
	return merged
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
			artifact.Payload = BuildSupplyChainPathPayload(parsed.Nodes, parsed.Edges, parsed.Notes)
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
		if node.Type != "org" && node.Type != "fac" && node.Type != "product" {
			return fmt.Errorf("node[%d] invalid type: %q (expected org|fac|product)", i, node.Type)
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
	}

	return nil
}
