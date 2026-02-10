package scout

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
)

// SupplyChainParser converts supply chain objects from agent output
// into HorizonFragments and DependencyEdges for graph insertion.
type SupplyChainParser struct{}

// NewSupplyChainParser creates a new supply chain parser instance.
func NewSupplyChainParser() *SupplyChainParser {
	return &SupplyChainParser{}
}

// ParsedChainResult contains fragments and edges extracted from supply chains.
type ParsedChainResult struct {
	// Fragments are new or updated fragments discovered in the chains.
	// Keys are canonical paths.
	Fragments map[string]*models.HorizonFragment
	// Edges are supply chain edges extracted from the chains.
	Edges []*ChainEdge
	// Stats contains diagnostic counters for parsing (for [SupplyChain*] telemetry).
	Stats ParsedChainStats
}

// ParsedChainStats captures diagnostics about parsing supply chains.
type ParsedChainStats struct {
	ChainsIn                 int `json:"chainsIn"`
	NodesTotal               int `json:"nodesTotal"`
	NodesWithCanonicalPath   int `json:"nodesWithCanonicalPath"`
	NodesSkippedEmptyCP      int `json:"nodesSkippedEmptyCanonicalPath"`
	NodesSkippedUnknownType  int `json:"nodesSkippedUnknownType"`
	EdgesAttempted           int `json:"edgesAttempted"`
	EdgesCreated             int `json:"edgesCreated"`
	EdgesSkippedMissingCP    int `json:"edgesSkippedMissingCanonicalPath"`
	EdgesInvertedToDependsOn int `json:"edgesInvertedToDependsOn"`
}

// ChainEdge represents an edge in a supply chain graph.
type ChainEdge struct {
	SourceCanonicalPath string
	TargetCanonicalPath string
	RelationType        string // PRODUCES, CONSUMES, SUPPLIES_TO, DEPENDS_ON, TRANSFORMS_INTO
	Confidence          float64
	Reason              string
}

// ParseSupplyChains converts agent supply chain output into fragments and edges.
// The function:
// 1. Extracts unique fragments from all chains (with deduplication by canonical path)
// 2. Creates edges between consecutive nodes (actor → product → actor)
// 3. Returns a ParsedChainResult with all fragments and edges
func (p *SupplyChainParser) ParseSupplyChains(chains []agents.SupplyChain, sourceFragmentPath string) (*ParsedChainResult, error) {
	result := &ParsedChainResult{
		Fragments: make(map[string]*models.HorizonFragment),
		Edges:     make([]*ChainEdge, 0),
	}

	if len(chains) == 0 {
		return result, nil
	}
	result.Stats.ChainsIn = len(chains)

	for _, chain := range chains {
		if len(chain.Nodes) < 2 {
			// Need at least 2 nodes to form an edge
			continue
		}

		// Process each node in the chain
		for i, node := range chain.Nodes {
			result.Stats.NodesTotal++
			canonicalPath := strings.TrimSpace(node.CanonicalPath)
			if canonicalPath == "" {
				result.Stats.NodesSkippedEmptyCP++
				continue
			}
			result.Stats.NodesWithCanonicalPath++

			// Create fragment if not already present
			if _, exists := result.Fragments[canonicalPath]; !exists {
				frag := p.createFragmentFromNode(node)
				if frag != nil {
					result.Fragments[canonicalPath] = frag
				} else {
					result.Stats.NodesSkippedUnknownType++
				}
			}

			// Create edge to next node
			if i < len(chain.Nodes)-1 {
				result.Stats.EdgesAttempted++
				nextNode := chain.Nodes[i+1]
				nextPath := strings.TrimSpace(nextNode.CanonicalPath)
				if nextPath == "" {
					result.Stats.EdgesSkippedMissingCP++
					continue
				}

				edge := p.createEdgeBetweenNodes(node, nextNode, chain.Confidence, chain.Reason)
				if edge != nil {
					result.Edges = append(result.Edges, edge)
					result.Stats.EdgesCreated++
					if edge.RelationType == string(models.DependencyTypeDependsOn) &&
						strings.TrimSpace(edge.SourceCanonicalPath) == strings.TrimSpace(nextPath) &&
						strings.TrimSpace(edge.TargetCanonicalPath) == strings.TrimSpace(canonicalPath) {
						// This corresponds to the product->actor inversion case.
						result.Stats.EdgesInvertedToDependsOn++
					}
				}
			}
		}
	}

	return result, nil
}

// createFragmentFromNode creates a HorizonFragment from a supply chain node.
func (p *SupplyChainParser) createFragmentFromNode(node agents.SupplyChainNode) *models.HorizonFragment {
	canonicalPath := strings.TrimSpace(node.CanonicalPath)
	if canonicalPath == "" {
		return nil
	}

	fragType := p.inferFragmentType(node.Type, canonicalPath)
	if fragType == "" {
		return nil
	}

	frag := &models.HorizonFragment{
		CanonicalPath: canonicalPath,
		Type:          fragType,
		Labels:        make(map[string]string),
		Identifiers:   make(map[string]string),
		State:         make(map[string]models.MetricValue),
	}

	// Set name in labels
	name := strings.TrimSpace(node.Name)
	if name != "" {
		frag.Labels["en"] = name
		frag.Identifiers["shortName"] = name
		if fragType == models.FragmentTypeOrganization && strings.TrimSpace(frag.Identifiers["legalName"]) == "" {
			frag.Identifiers["legalName"] = name
		}
	}

	// Set role in identifiers if present
	role := strings.TrimSpace(node.Role)
	if role != "" {
		frag.Identifiers["supplyChainRole"] = role
	}

	return frag
}

// inferFragmentType determines the fragment type from node type or canonical path.
func (p *SupplyChainParser) inferFragmentType(nodeType string, canonicalPath string) models.FragmentType {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))

	switch nodeType {
	case "ORGANIZATION":
		return models.FragmentTypeOrganization
	case "FACILITY":
		return models.FragmentTypeFacility
	case "PRODUCT":
		return models.FragmentTypeProduct
	case "GEO_REGION":
		return models.FragmentTypeGeoRegion
	case "DEPOSIT":
		return models.FragmentTypeDeposit
	}

	// Fallback: infer from canonical path prefix
	canonicalPath = strings.TrimSpace(canonicalPath)
	switch {
	case strings.HasPrefix(canonicalPath, "/org/"):
		return models.FragmentTypeOrganization
	case strings.HasPrefix(canonicalPath, "/fac/"):
		return models.FragmentTypeFacility
	case strings.HasPrefix(canonicalPath, "/product/"):
		return models.FragmentTypeProduct
	case strings.HasPrefix(canonicalPath, "/loc/"):
		return models.FragmentTypeGeoRegion
	case strings.HasPrefix(canonicalPath, "/deposit/"):
		return models.FragmentTypeDeposit
	}

	return ""
}

// createEdgeBetweenNodes creates an edge based on the node types.
// Supply chain pattern: actor → product → actor → product
// - Actor (org/fac) → Product: PRODUCES
// - Product → Actor: actor CONSUMES product (or invert to DEPENDS_ON)
// - Actor → Actor: SUPPLIES_TO
// - Product → Product: TRANSFORMS_INTO
func (p *SupplyChainParser) createEdgeBetweenNodes(from, to agents.SupplyChainNode, confidence float64, reason string) *ChainEdge {
	fromPath := strings.TrimSpace(from.CanonicalPath)
	toPath := strings.TrimSpace(to.CanonicalPath)
	if fromPath == "" || toPath == "" {
		return nil
	}

	fromIsActor := p.isActorType(from.Type, fromPath)
	toIsActor := p.isActorType(to.Type, toPath)
	fromIsProduct := p.isProductType(from.Type, fromPath)
	toIsProduct := p.isProductType(to.Type, toPath)

	var relationType string

	switch {
	case fromIsActor && toIsProduct:
		// Actor produces product
		relationType = string(models.DependencyTypeProduces)
	case fromIsProduct && toIsActor:
		// Product is consumed by actor
		// Semantically: actor CONSUMES product, so we store as product -> actor with CONSUMED_BY
		// or invert to actor -> product with CONSUMES
		// For supply chain, we model as: the actor DEPENDS_ON the product (input)
		// So: target actor DEPENDS_ON source product
		// Invert the edge: toPath -> fromPath with DEPENDS_ON
		return &ChainEdge{
			SourceCanonicalPath: toPath,
			TargetCanonicalPath: fromPath,
			RelationType:        string(models.DependencyTypeDependsOn),
			Confidence:          confidence,
			Reason:              fmt.Sprintf("Supply chain: %s consumes %s. %s", to.Name, from.Name, reason),
		}
	case fromIsActor && toIsActor:
		// Direct actor to actor relationship
		relationType = string(models.DependencyTypeSuppliesTo)
	case fromIsProduct && toIsProduct:
		// Product transformation
		relationType = string(models.DependencyTypeTransformsInto)
	default:
		// Unknown pattern, use generic DEPENDS_ON
		relationType = string(models.DependencyTypeDependsOn)
	}

	return &ChainEdge{
		SourceCanonicalPath: fromPath,
		TargetCanonicalPath: toPath,
		RelationType:        relationType,
		Confidence:          confidence,
		Reason:              fmt.Sprintf("Supply chain: %s → %s. %s", from.Name, to.Name, reason),
	}
}

// isActorType checks if the node represents an actor (org or facility).
func (p *SupplyChainParser) isActorType(nodeType string, canonicalPath string) bool {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	if nodeType == "ORGANIZATION" || nodeType == "FACILITY" {
		return true
	}
	return strings.HasPrefix(canonicalPath, "/org/") || strings.HasPrefix(canonicalPath, "/fac/")
}

// isProductType checks if the node represents a product.
func (p *SupplyChainParser) isProductType(nodeType string, canonicalPath string) bool {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	if nodeType == "PRODUCT" {
		return true
	}
	return strings.HasPrefix(canonicalPath, "/product/")
}


// ChainContext represents the cumulative supply chain state passed between jobs.
// It contains discovered nodes and edges so far, allowing agents to extend chains.
type ChainContext struct {
	// DiscoveredNodes are canonical paths of all discovered supply chain entities.
	DiscoveredNodes []string `json:"discoveredNodes"`
	// DiscoveredEdges are edges between nodes (source -> target with type).
	DiscoveredEdges []ChainContextEdge `json:"discoveredEdges"`
	// GapHints are specific gaps to fill (e.g., "find upstream supplier for /product/x").
	GapHints []string `json:"gapHints,omitempty"`
}

// ChainContextEdge is a minimal edge representation for context passing.
type ChainContextEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	EdgeType string `json:"edgeType"`
}

// NewChainContext creates an empty chain context.
func NewChainContext() *ChainContext {
	return &ChainContext{
		DiscoveredNodes: make([]string, 0),
		DiscoveredEdges: make([]ChainContextEdge, 0),
		GapHints:        make([]string, 0),
	}
}

// MergeFromParsedResult adds nodes and edges from a ParsedChainResult to the context.
func (cc *ChainContext) MergeFromParsedResult(result *ParsedChainResult) {
	if result == nil {
		return
	}

	// Add nodes (deduplicated)
	nodeSet := make(map[string]struct{})
	for _, n := range cc.DiscoveredNodes {
		nodeSet[n] = struct{}{}
	}
	for path := range result.Fragments {
		if _, exists := nodeSet[path]; !exists {
			cc.DiscoveredNodes = append(cc.DiscoveredNodes, path)
			nodeSet[path] = struct{}{}
		}
	}

	// Add edges (deduplicated by source+target+type)
	edgeSet := make(map[string]struct{})
	for _, e := range cc.DiscoveredEdges {
		key := e.Source + "|" + e.Target + "|" + e.EdgeType
		edgeSet[key] = struct{}{}
	}
	for _, e := range result.Edges {
		key := e.SourceCanonicalPath + "|" + e.TargetCanonicalPath + "|" + e.RelationType
		if _, exists := edgeSet[key]; !exists {
			cc.DiscoveredEdges = append(cc.DiscoveredEdges, ChainContextEdge{
				Source:   e.SourceCanonicalPath,
				Target:   e.TargetCanonicalPath,
				EdgeType: e.RelationType,
			})
			edgeSet[key] = struct{}{}
		}
	}
}

// ToJSON serializes the chain context to JSON for storage in job state.
func (cc *ChainContext) ToJSON() (string, error) {
	data, err := json.Marshal(cc)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseChainContext deserializes a chain context from job state.
func ParseChainContext(data interface{}) (*ChainContext, error) {
	if data == nil {
		return NewChainContext(), nil
	}

	var jsonStr string
	switch v := data.(type) {
	case string:
		jsonStr = v
	case map[string]interface{}:
		// Already unmarshaled as map, re-marshal and unmarshal
		bytes, err := json.Marshal(v)
		if err != nil {
			return NewChainContext(), nil
		}
		jsonStr = string(bytes)
	default:
		return NewChainContext(), nil
	}

	if jsonStr == "" {
		return NewChainContext(), nil
	}

	var cc ChainContext
	if err := json.Unmarshal([]byte(jsonStr), &cc); err != nil {
		return NewChainContext(), nil
	}

	return &cc, nil
}

// ToPromptContext generates a human-readable context string for LLM prompts.
func (cc *ChainContext) ToPromptContext() string {
	if len(cc.DiscoveredNodes) == 0 && len(cc.DiscoveredEdges) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("EXISTING SUPPLY CHAIN CONTEXT (discovered so far):\n")

	if len(cc.DiscoveredNodes) > 0 {
		sb.WriteString("Known entities:\n")
		for _, n := range cc.DiscoveredNodes {
			sb.WriteString(fmt.Sprintf("  - %s\n", n))
		}
	}

	if len(cc.DiscoveredEdges) > 0 {
		sb.WriteString("Known connections:\n")
		for _, e := range cc.DiscoveredEdges {
			sb.WriteString(fmt.Sprintf("  - %s -[%s]-> %s\n", e.Source, e.EdgeType, e.Target))
		}
	}

	if len(cc.GapHints) > 0 {
		sb.WriteString("Gaps to fill:\n")
		for _, g := range cc.GapHints {
			sb.WriteString(fmt.Sprintf("  - %s\n", g))
		}
	}

	sb.WriteString("\nEXTEND these chains by finding new suppliers, products, or customers.\n\n")

	return sb.String()
}
