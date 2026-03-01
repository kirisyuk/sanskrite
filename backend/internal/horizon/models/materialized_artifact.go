package models

import (
	"fmt"
	"strings"
	"time"
)

// MaterializedKind defines the type/schema of a MaterializedArtifact overlay.
type MaterializedKind string

const (
	MaterializedKindSupplyChainPath      MaterializedKind = "SUPPLY_CHAIN_PATH"
	MaterializedKindRegulatoryImpactMap  MaterializedKind = "REGULATORY_IMPACT_MAP"
	MaterializedKindGovernanceChain      MaterializedKind = "GOVERNANCE_CHAIN"
	MaterializedKindEcosystemMap         MaterializedKind = "ECOSYSTEM_MAP"
)

// Valid checks if the MaterializedKind is valid.
func (k MaterializedKind) Valid() bool {
	switch k {
	case MaterializedKindSupplyChainPath, MaterializedKindRegulatoryImpactMap, MaterializedKindGovernanceChain, MaterializedKindEcosystemMap:
		return true
	default:
		return false
	}
}

// MaterializedArtifact is a persisted, algorithmically built overlay over the world graph.
// It is NOT a replacement for HorizonFragment; it is a derived view that references the world graph.
//
// MaterializedArtifact represents "layers" that can be built/rebuilt/filtered over the world graph
// (supply chain paths, political slices, risk exposure paths, etc.) without mixing "world" and "computed views".
type MaterializedArtifact struct {
	ID        string `json:"id" firestore:"id"`
	TenantID  string `json:"tenantId" firestore:"tenantId"` // "global" or tenant
	ProjectID string `json:"projectId,omitempty" firestore:"projectId,omitempty"`
	LensID    string `json:"lensId,omitempty" firestore:"lensId,omitempty"`

	// Kind defines schema of Payload.
	Kind MaterializedKind `json:"kind" firestore:"kind"`

	// A stable key for idempotent upserts & dedup (like exposure keys).
	// Format depends on Kind; for SUPPLY_CHAIN_PATH: /supply-chain/{tenant}/{hash} or similar.
	CanonicalKey string `json:"canonicalKey" firestore:"canonicalKey"`

	// References to model-layer metric observations (stored outside ontology_facts).
	// These are separate from evidence to maintain separation of concerns:
	// - ontology_facts = evidence pipeline (extraction/normalization)
	// - metric observations = model layer (metrics with units, time windows, modality, confidence)
	MetricObservationIDs []string `json:"metricObservationIds,omitempty" firestore:"metricObservationIds,omitempty"`

	// Evidence links (ontology_facts) that justify this overlay (optional).
	// These reference ontology_facts as proof points, but metrics are stored separately.
	EvidenceFactIDs []string `json:"evidenceFactIds,omitempty" firestore:"evidenceFactIds,omitempty"`

	// Payload is a versioned, kind-specific JSON object.
	// For SUPPLY_CHAIN_PATH, see SupplyChainPathPayload.
	Payload map[string]interface{} `json:"payload,omitempty" firestore:"payload,omitempty"`

	// Metadata
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`
	CreatedBy string    `json:"createdBy,omitempty" firestore:"createdBy,omitempty"`
	UpdatedBy string    `json:"updatedBy,omitempty" firestore:"updatedBy,omitempty"`

	// Status: active, deprecated, draft, archived
	Status string `json:"status" firestore:"status"`

	// PathStatus: hypothesis (from LLM/parsed chain, unvalidated) | validated (passed Critic invariants).
	// For SUPPLY_CHAIN_PATH overlays.
	PathStatus string `json:"pathStatus,omitempty" firestore:"pathStatus,omitempty"`

	// DisplayName is a human-readable name for the artifact (e.g., "CATL → Lithium Battery Cell → BMW AG").
	DisplayName string `json:"displayName,omitempty" firestore:"displayName,omitempty"`

	// Labels are structured key-value attributes for filtering and display.
	// For SUPPLY_CHAIN_PATH: productAxis, geoFrom, geoTo, anchorSupplier, anchorCustomer, segmentCount.
	Labels map[string]string `json:"labels,omitempty" firestore:"labels,omitempty"`

	// NodeRefs lists canonical paths of all participating entities.
	// Used as a reverse index (Firestore array-contains) to find chains through a given node.
	NodeRefs []string `json:"nodeRefs,omitempty" firestore:"nodeRefs,omitempty"`
}

// Validate checks if MaterializedArtifact is valid.
func (a *MaterializedArtifact) Validate() error {
	if a == nil {
		return fmt.Errorf("materialized artifact is nil")
	}
	if strings.TrimSpace(a.TenantID) == "" {
		return fmt.Errorf("tenantId is required")
	}
	if !a.Kind.Valid() {
		return fmt.Errorf("invalid kind: %q", string(a.Kind))
	}
	if strings.TrimSpace(a.CanonicalKey) == "" {
		return fmt.Errorf("canonicalKey is required")
	}
	if a.Payload == nil {
		return fmt.Errorf("payload is required")
	}

	// Kind-specific payload validation
	switch a.Kind {
	case MaterializedKindSupplyChainPath:
		if err := validateSupplyChainPathPayload(a.Payload); err != nil {
			return fmt.Errorf("invalid SUPPLY_CHAIN_PATH payload: %w", err)
		}
	case MaterializedKindRegulatoryImpactMap, MaterializedKindGovernanceChain, MaterializedKindEcosystemMap:
		if err := validateRelationsGraphPayload(a.Payload); err != nil {
			return fmt.Errorf("invalid %s payload: %w", string(a.Kind), err)
		}
	}

	return nil
}

// RelationsGraphPayload represents the payload structure for domain-specific relation graphs
// (GOVERNANCE_CHAIN, REGULATORY_IMPACT_MAP, ECOSYSTEM_MAP).
type RelationsGraphPayload struct {
	Version    int                     `json:"v"`
	Nodes      []RelationsGraphNode    `json:"nodes"`
	Edges      []RelationsGraphEdge    `json:"edges,omitempty"`
	DomainMode string                  `json:"domainMode,omitempty"`
	RootEntity string                  `json:"rootEntity,omitempty"`
	Notes      map[string]interface{}  `json:"notes,omitempty"`
}

// RelationsGraphNode represents an entity node in a domain relations graph.
type RelationsGraphNode struct {
	Type string `json:"type"`
	Ref  string `json:"ref"`
	Name string `json:"name,omitempty"`
}

// RelationsGraphEdge represents a typed relationship edge in a domain relations graph.
type RelationsGraphEdge struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Relation string `json:"relation"`
}

func validateRelationsGraphPayload(payload map[string]interface{}) error {
	if payload == nil {
		return fmt.Errorf("payload is nil")
	}
	v, ok := payload["v"]
	if !ok {
		return fmt.Errorf("payload missing version field 'v'")
	}
	version, ok := toFloat64(v)
	if !ok {
		return fmt.Errorf("payload version 'v' must be a number")
	}
	if int(version) != 1 {
		return fmt.Errorf("unsupported payload version: %d (expected 1)", int(version))
	}
	nodesRaw, ok := payload["nodes"]
	if !ok {
		return fmt.Errorf("payload missing 'nodes' field")
	}
	nodes, err := decodeRelationsGraphNodes(nodesRaw)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return fmt.Errorf("payload 'nodes' array cannot be empty")
	}
	for i, node := range nodes {
		if node.Ref == "" {
			return fmt.Errorf("payload node[%d] missing 'ref'", i)
		}
		if !strings.HasPrefix(node.Ref, "/") {
			return fmt.Errorf("payload node[%d] 'ref' must be a canonical path (start with /)", i)
		}
	}
	if edgesRaw, ok := payload["edges"]; ok {
		edges, err := decodeRelationsGraphEdges(edgesRaw)
		if err != nil {
			return err
		}
		for i, edge := range edges {
			if edge.From < 0 || edge.From >= len(nodes) {
				return fmt.Errorf("payload edge[%d] 'from' index %d out of range (nodes: %d)", i, edge.From, len(nodes))
			}
			if edge.To < 0 || edge.To >= len(nodes) {
				return fmt.Errorf("payload edge[%d] 'to' index %d out of range (nodes: %d)", i, edge.To, len(nodes))
			}
			if edge.Relation == "" {
				return fmt.Errorf("payload edge[%d] missing 'relation'", i)
			}
		}
	}
	return nil
}

// SupplyChainPathPayload represents the payload structure for SUPPLY_CHAIN_PATH kind.
// Version 1 schema.
type SupplyChainPathPayload struct {
	// Version of the payload schema
	Version int `json:"v"`

	// Nodes: ordered array of references to world graph entities (fragments).
	// Each node has type (org|fac|product) and canonicalPath ref.
	Nodes []SupplyChainPathNode `json:"nodes"`

	// Edges: optional array of edges/transitions between nodes.
	// Can represent supply links, ontology transitions (TRANSFORMS_INTO), etc.
	Edges []SupplyChainPathEdge `json:"edges,omitempty"`

	// Notes: optional metadata about how the path was built.
	Notes map[string]interface{} `json:"notes,omitempty"`
}

// SupplyChainPathNode represents a node in a supply chain path.
type SupplyChainPathNode struct {
	// Type: "org", "fac", "product"
	Type string `json:"type"`

	// Ref: canonical path to the fragment (e.g., "/org/catl", "/product/.../battery-cell")
	Ref string `json:"ref"`
}

// SupplyChainPathEdge represents an edge/transition in a supply chain path.
type SupplyChainPathEdge struct {
	// Type: "SUPPLY_LINK", "ONTOLOGY", etc.
	Type string `json:"type"`

	// From: index into Nodes array
	From int `json:"from"`

	// To: index into Nodes array
	To int `json:"to"`

	// ViaProductIndex: for SUPPLY_LINK, index of the product node (optional)
	ViaProductIndex *int `json:"viaProductIndex,omitempty"`

	// Relation: for ONTOLOGY edges, the relation type (e.g., "TRANSFORMS_INTO")
	Relation string `json:"relation,omitempty"`

	// FromProductIndex/ToProductIndex: for ONTOLOGY edges, product node indices
	FromProductIndex *int `json:"fromProductIndex,omitempty"`
	ToProductIndex   *int `json:"toProductIndex,omitempty"`

	// Mode: "optional", "required", "strict" (for validation/ranking)
	Mode string `json:"mode,omitempty"`
}

// validateSupplyChainPathPayload validates SUPPLY_CHAIN_PATH payload structure.
func validateSupplyChainPathPayload(payload map[string]interface{}) error {
	if payload == nil {
		return fmt.Errorf("payload is nil")
	}

	// Check version
	v, ok := payload["v"]
	if !ok {
		return fmt.Errorf("payload missing version field 'v'")
	}
	version, ok := toFloat64(v)
	if !ok {
		return fmt.Errorf("payload version 'v' must be a number")
	}
	if int(version) != 1 {
		return fmt.Errorf("unsupported payload version: %d (expected 1)", int(version))
	}

	// Check nodes
	nodes, ok := payload["nodes"]
	if !ok {
		return fmt.Errorf("payload missing 'nodes' field")
	}
	decodedNodes, err := decodeSupplyChainPathNodes(nodes)
	if err != nil {
		return err
	}
	if len(decodedNodes) == 0 {
		return fmt.Errorf("payload 'nodes' array cannot be empty")
	}

	// Validate each node
	for i, node := range decodedNodes {
		nodeType := node.Type
		if nodeType == "" {
			return fmt.Errorf("payload node[%d] missing or invalid 'type'", i)
		}
		if nodeType != "org" && nodeType != "fac" && nodeType != "product" && nodeType != "deposit" {
			return fmt.Errorf("payload node[%d] invalid type: %q (expected org|fac|product|deposit)", i, nodeType)
		}
		ref := node.Ref
		if ref == "" {
			return fmt.Errorf("payload node[%d] missing or invalid 'ref'", i)
		}
		if !strings.HasPrefix(ref, "/") {
			return fmt.Errorf("payload node[%d] 'ref' must be a canonical path (start with /)", i)
		}
	}

	// Edges are optional, but if present, validate indices
	if edges, ok := payload["edges"]; ok {
		decodedEdges, err := decodeSupplyChainPathEdges(edges)
		if err != nil {
			return err
		}
		for i, edge := range decodedEdges {
			fromIdx := edge.From
			toIdx := edge.To
			if fromIdx < 0 || fromIdx >= len(decodedNodes) {
				return fmt.Errorf("payload edge[%d] 'from' index %d out of range (nodes length: %d)", i, fromIdx, len(decodedNodes))
			}
			if toIdx < 0 || toIdx >= len(decodedNodes) {
				return fmt.Errorf("payload edge[%d] 'to' index %d out of range (nodes length: %d)", i, toIdx, len(decodedNodes))
			}
		}
	}

	return nil
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
