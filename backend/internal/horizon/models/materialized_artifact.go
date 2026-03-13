package models

import (
	"fmt"
	"strings"
	"time"
)

// MaterializedKind defines the type/schema of a MaterializedArtifact overlay.
type MaterializedKind string

const (
	MaterializedKindSupplyChainPath     MaterializedKind = "SUPPLY_CHAIN_PATH"
	MaterializedKindRegulatoryImpactMap MaterializedKind = "REGULATORY_IMPACT_MAP"
	MaterializedKindGovernanceChain     MaterializedKind = "GOVERNANCE_CHAIN"
	MaterializedKindEcosystemMap        MaterializedKind = "ECOSYSTEM_MAP"
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

	// Top-level spatial/path indexes for overlap discovery without parsing payload at read time.
	GeoRefs          []string `json:"geoRefs,omitempty" firestore:"geoRefs,omitempty"`
	FlowRefs         []string `json:"flowRefs,omitempty" firestore:"flowRefs,omitempty"`
	ZoneRefs         []string `json:"zoneRefs,omitempty" firestore:"zoneRefs,omitempty"`
	AffectedZoneRefs []string `json:"affectedZoneRefs,omitempty" firestore:"affectedZoneRefs,omitempty"`
	AnchorSupplier   string   `json:"anchorSupplier,omitempty" firestore:"anchorSupplier,omitempty"`
	AnchorCustomer   string   `json:"anchorCustomer,omitempty" firestore:"anchorCustomer,omitempty"`
	ProductAxis      string   `json:"productAxis,omitempty" firestore:"productAxis,omitempty"`

	// Response-only selection metadata. Not persisted.
	DiscoverySource      string                   `json:"discoverySource,omitempty" firestore:"-"`
	DiscoveryReason      string                   `json:"discoveryReason,omitempty" firestore:"-"`
	MatchedRefs          []string                 `json:"matchedRefs,omitempty" firestore:"-"`
	OverlapScore         float64                  `json:"overlapScore,omitempty" firestore:"-"`
	IsPinned             bool                     `json:"isPinned,omitempty" firestore:"-"`
	TraceRole            string                   `json:"traceRole,omitempty" firestore:"-"`
	TraceReason          string                   `json:"traceReason,omitempty" firestore:"-"`
	TracePriority        int                      `json:"tracePriority,omitempty" firestore:"-"`
	LegCount             int                      `json:"legCount,omitempty" firestore:"-"`
	BindingScore         float64                  `json:"bindingScore,omitempty" firestore:"-"`
	PathStressScore      float64                  `json:"pathStressScore,omitempty" firestore:"-"`
	BottleneckLeg        int                      `json:"bottleneckLegIndex,omitempty" firestore:"-"`
	SufficiencyStatus    string                   `json:"sufficiencyStatus,omitempty" firestore:"-"`
	SufficiencyScore     float64                  `json:"sufficiencyScore,omitempty" firestore:"-"`
	GapCount             int                      `json:"gapCount,omitempty" firestore:"-"`
	UnresolvedJoinCount  int                      `json:"unresolvedJoinCount,omitempty" firestore:"-"`
	ValidatedJoinCount   int                      `json:"validatedJoinCount,omitempty" firestore:"-"`
	IsConfirmedPath      bool                     `json:"isConfirmedPath,omitempty" firestore:"-"`
	FamilyID             string                   `json:"familyId,omitempty" firestore:"-"`
	FamilyPathCount      int                      `json:"familyPathCount,omitempty" firestore:"-"`
	FamilyValidatedCount int                      `json:"familyValidatedCount,omitempty" firestore:"-"`
	FamilyGapCount       int                      `json:"familyGapCount,omitempty" firestore:"-"`
	TraceEndpoints       *SupplyChainTraceContext `json:"traceEndpoints,omitempty" firestore:"-"`
}

type SupplyChainTraceEndpoint struct {
	CanonicalPath string       `json:"canonicalPath,omitempty"`
	Label         string       `json:"label,omitempty"`
	Type          string       `json:"type,omitempty"`
	Coordinates   *Coordinates `json:"coordinates,omitempty"`
	Resolved      bool         `json:"resolved"`
}

type SupplyChainTraceContext struct {
	Supplier   *SupplyChainTraceEndpoint `json:"supplier,omitempty"`
	Customer   *SupplyChainTraceEndpoint `json:"customer,omitempty"`
	Upstream   *SupplyChainTraceEndpoint `json:"upstream,omitempty"`
	Downstream *SupplyChainTraceEndpoint `json:"downstream,omitempty"`
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
	Version    int                    `json:"v"`
	Nodes      []RelationsGraphNode   `json:"nodes"`
	Edges      []RelationsGraphEdge   `json:"edges,omitempty"`
	DomainMode string                 `json:"domainMode,omitempty"`
	RootEntity string                 `json:"rootEntity,omitempty"`
	Notes      map[string]interface{} `json:"notes,omitempty"`
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

	// Legs: optional ordered operational legs with route/zone bindings.
	Legs []SupplyChainPathLeg `json:"legs,omitempty"`

	// Stress: optional aggregated structural stress profile over the path.
	Stress *SupplyChainPathStress `json:"stress,omitempty"`

	// Sufficiency: explainable continuity assessment for the materialized chain.
	Sufficiency *SupplyChainPathSufficiency `json:"sufficiency,omitempty"`

	// Family: grouped chain-set metadata for related alternative/competing paths.
	Family *SupplyChainPathFamily `json:"family,omitempty"`

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

	// ProductNodeIndex/ProductRef make the carried product explicit on materialized edges,
	// especially for logistics legs where the product is otherwise only implied by the segment.
	ProductNodeIndex *int   `json:"productNodeIndex,omitempty"`
	ProductRef       string `json:"productRef,omitempty"`

	// SupplyLinkID/SegmentIndex trace the edge back to the originating operational segment.
	SupplyLinkID            string `json:"supplyLinkId,omitempty"`
	SupplyLinkCanonicalPath string `json:"supplyLinkCanonicalPath,omitempty"`
	SupplyLinkTenantID      string `json:"supplyLinkTenantId,omitempty"`
	SegmentIndex            *int   `json:"segmentIndex,omitempty"`

	// CarriesProduct marks edges that represent actual product flow/logistics, not just overlay semantics.
	CarriesProduct bool `json:"carriesProduct,omitempty"`

	// Role distinguishes flow edges inside the materialized path.
	// Examples: product_flow, logistics_leg, product_transformation.
	Role string `json:"role,omitempty"`

	// Metrics/Qualifiers carry best-effort operational semantics from the underlying SupplyLink segment.
	Metrics    map[string]MetricValue `json:"metrics,omitempty"`
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty"`

	// Relation: for ONTOLOGY edges, the relation type (e.g., "TRANSFORMS_INTO")
	Relation string `json:"relation,omitempty"`

	// FromProductIndex/ToProductIndex: for ONTOLOGY edges, product node indices
	FromProductIndex *int `json:"fromProductIndex,omitempty"`
	ToProductIndex   *int `json:"toProductIndex,omitempty"`

	// Mode: "optional", "required", "strict" (for validation/ranking)
	Mode string `json:"mode,omitempty"`
}

// SupplyChainPathLeg describes one ordered operational leg of a path, enriched
// with spatial bindings to routes, curated zones, and dynamic affected zones.
type SupplyChainPathLeg struct {
	Index                   int                   `json:"index"`
	SupplierRef             string                `json:"supplierRef,omitempty"`
	CustomerRef             string                `json:"customerRef,omitempty"`
	ProductRef              string                `json:"productRef,omitempty"`
	SupplyLinkCanonicalPath string                `json:"supplyLinkCanonicalPath,omitempty"`
	SupplyLinkID            string                `json:"supplyLinkId,omitempty"`
	SupplyLinkTenantID      string                `json:"supplyLinkTenantId,omitempty"`
	SupplierNodeIndex       *int                  `json:"supplierNodeIndex,omitempty"`
	CustomerNodeIndex       *int                  `json:"customerNodeIndex,omitempty"`
	ProductNodeIndex        *int                  `json:"productNodeIndex,omitempty"`
	TransportViaRefs        []string              `json:"transportViaRefs,omitempty"`
	TransportViaNodeIndices []int                 `json:"transportViaNodeIndices,omitempty"`
	Mode                    string                `json:"mode,omitempty"`
	GeoRefs                 []string              `json:"geoRefs,omitempty"`
	FlowRefs                []string              `json:"flowRefs,omitempty"`
	AlternateFlowRefs       []string              `json:"alternateFlowRefs,omitempty"`
	ZoneRefs                []string              `json:"zoneRefs,omitempty"`
	AffectedZoneRefs        []string              `json:"affectedZoneRefs,omitempty"`
	SpatialSemantics        []string              `json:"spatialSemantics,omitempty"`
	BindingConfidence       float64               `json:"bindingConfidence,omitempty"`
	SegmentConfidence       float64               `json:"segmentConfidence,omitempty"`
	Stress                  *SupplyChainLegStress `json:"stress,omitempty"`
}

// SupplyChainLegStress captures structural stress on a single path leg.
type SupplyChainLegStress struct {
	Fragility            float64  `json:"fragility,omitempty"`
	Rerouteability       float64  `json:"rerouteability,omitempty"`
	ZonePressure         float64  `json:"zonePressure,omitempty"`
	AffectedZonePressure float64  `json:"affectedZonePressure,omitempty"`
	RecoveryLatencyHrs   float64  `json:"recoveryLatencyHours,omitempty"`
	StressScore          float64  `json:"stressScore,omitempty"`
	DominantFactors      []string `json:"dominantFactors,omitempty"`
}

// SupplyChainPathStress aggregates structural stress across all legs of a path.
type SupplyChainPathStress struct {
	PathScore            float64  `json:"pathScore,omitempty"`
	Fragility            float64  `json:"fragility,omitempty"`
	Rerouteability       float64  `json:"rerouteability,omitempty"`
	ZonePressure         float64  `json:"zonePressure,omitempty"`
	AffectedZonePressure float64  `json:"affectedZonePressure,omitempty"`
	RecoveryLatencyHrs   float64  `json:"recoveryLatencyHours,omitempty"`
	BottleneckLegIndex   int      `json:"bottleneckLegIndex,omitempty"`
	DominantFactors      []string `json:"dominantFactors,omitempty"`
}

type SupplyChainPathSufficiency struct {
	Status          string                      `json:"status,omitempty"`
	Score           float64                     `json:"score,omitempty"`
	Summary         string                      `json:"summary,omitempty"`
	HardFailures    []string                    `json:"hardFailures,omitempty"`
	Warnings        []string                    `json:"warnings,omitempty"`
	OpenGaps        []string                    `json:"openGaps,omitempty"`
	JoinAssessments []SupplyChainJoinAssessment `json:"joinAssessments,omitempty"`
	LegAssessments  []SupplyChainLegAssessment  `json:"legAssessments,omitempty"`
}

type SupplyChainJoinAssessment struct {
	Index              int      `json:"index"`
	LeftLegIndex       int      `json:"leftLegIndex"`
	RightLegIndex      int      `json:"rightLegIndex"`
	ExpectedHandoffRef string   `json:"expectedHandoffRef,omitempty"`
	LeftCustomerRef    string   `json:"leftCustomerRef,omitempty"`
	RightSupplierRef   string   `json:"rightSupplierRef,omitempty"`
	ActorContinuity    string   `json:"actorContinuity,omitempty"`
	ProductContinuity  string   `json:"productContinuity,omitempty"`
	TemporalContinuity string   `json:"temporalContinuity,omitempty"`
	EntityReuse        string   `json:"entityReuse,omitempty"`
	Status             string   `json:"status,omitempty"`
	Reasons            []string `json:"reasons,omitempty"`
}

type SupplyChainLegAssessment struct {
	LegIndex                  int      `json:"legIndex"`
	RouteSensitive            bool     `json:"routeSensitive"`
	ExplicitTransportEvidence bool     `json:"explicitTransportEvidence"`
	BindingConfidence         float64  `json:"bindingConfidence,omitempty"`
	TimeKnown                 bool     `json:"timeKnown"`
	RouteStatus               string   `json:"routeStatus,omitempty"`
	Status                    string   `json:"status,omitempty"`
	Reasons                   []string `json:"reasons,omitempty"`
}

type SupplyChainPathFamily struct {
	ID                 string   `json:"id,omitempty"`
	AnchorSupplier     string   `json:"anchorSupplier,omitempty"`
	AnchorCustomer     string   `json:"anchorCustomer,omitempty"`
	ProductAxis        string   `json:"productAxis,omitempty"`
	PathCount          int      `json:"pathCount,omitempty"`
	ValidatedPathCount int      `json:"validatedPathCount,omitempty"`
	HeuristicPathCount int      `json:"heuristicPathCount,omitempty"`
	GapPathCount       int      `json:"gapPathCount,omitempty"`
	OpenGaps           []string `json:"openGaps,omitempty"`
	CoResearchRound    int      `json:"coResearchRound,omitempty"`
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

	// Legs are optional, but if present validate their node indices.
	if legsRaw, ok := payload["legs"]; ok {
		decodedLegs, err := decodeSupplyChainPathLegs(legsRaw)
		if err != nil {
			return err
		}
		for i, leg := range decodedLegs {
			if leg.Index < 0 {
				return fmt.Errorf("payload leg[%d] has negative index", i)
			}
			if strings.TrimSpace(leg.SupplierRef) != "" || strings.TrimSpace(leg.CustomerRef) != "" || strings.TrimSpace(leg.ProductRef) != "" {
				if strings.TrimSpace(leg.SupplyLinkCanonicalPath) == "" {
					return fmt.Errorf("payload leg[%d] missing supplyLinkCanonicalPath", i)
				}
				if !strings.HasPrefix(strings.TrimSpace(leg.SupplyLinkCanonicalPath), "/supply-link/") {
					return fmt.Errorf("payload leg[%d] supplyLinkCanonicalPath must be a canonical supply-link path", i)
				}
			}
			for _, idx := range []*int{leg.SupplierNodeIndex, leg.CustomerNodeIndex, leg.ProductNodeIndex} {
				if idx != nil && (*idx < 0 || *idx >= len(decodedNodes)) {
					return fmt.Errorf("payload leg[%d] references out-of-range node index", i)
				}
			}
			for _, viaIdx := range leg.TransportViaNodeIndices {
				if viaIdx < 0 || viaIdx >= len(decodedNodes) {
					return fmt.Errorf("payload leg[%d] has out-of-range transportViaNodeIndex", i)
				}
			}
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
			edgeType := strings.TrimSpace(edge.Type)
			switch edgeType {
			case "SUPPLY_LINK", "LOGISTICS":
				if strings.TrimSpace(edge.SupplyLinkCanonicalPath) == "" {
					return fmt.Errorf("payload edge[%d] missing supplyLinkCanonicalPath", i)
				}
				if !strings.HasPrefix(strings.TrimSpace(edge.SupplyLinkCanonicalPath), "/supply-link/") {
					return fmt.Errorf("payload edge[%d] supplyLinkCanonicalPath must be a canonical supply-link path", i)
				}
				if edge.SegmentIndex == nil || *edge.SegmentIndex < 0 {
					return fmt.Errorf("payload edge[%d] missing segmentIndex", i)
				}
				expectedRole := "product_flow"
				if edgeType == "LOGISTICS" {
					expectedRole = "logistics_leg"
				}
				if strings.TrimSpace(edge.Role) != expectedRole {
					return fmt.Errorf("payload edge[%d] role %q must be %q for %s", i, edge.Role, expectedRole, edgeType)
				}
			}
		}
	}
	if sufficiencyRaw, ok := payload["sufficiency"]; ok && sufficiencyRaw != nil {
		sufficiency, err := decodeSupplyChainPathSufficiency(sufficiencyRaw)
		if err != nil {
			return err
		}
		if sufficiency != nil && !IsValidSupplyChainSufficiencyStatus(sufficiency.Status) {
			return fmt.Errorf("payload sufficiency has invalid status %q", sufficiency.Status)
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
