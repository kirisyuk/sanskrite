package models

import (
	"encoding/json"
	"fmt"
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
