package scout

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// SupplyChainParser converts supply chain objects from agent output
// into HorizonFragments and DependencyEdges for graph insertion.
type SupplyChainParser struct{}

// NewSupplyChainParser creates a new supply chain parser instance.
func NewSupplyChainParser() *SupplyChainParser {
	return &SupplyChainParser{}
}

// FlowEdgeContract captures completeness and validation gaps for segment metrics (plan 5.3).
// For validated: period/start/end required. For hypothesis: best-effort with partial marking.
type FlowEdgeContract struct {
	ContractCompleteness string                 `json:"contractCompleteness,omitempty"` // "full" | "partial"
	ValidationGaps       []string               `json:"validationGaps,omitempty"`
	RawPayload           map[string]interface{} `json:"rawPayload,omitempty"`
	ParseWarnings        []string               `json:"parseWarnings,omitempty"` // repair/coercion traceability (plan 3.4.4)
	Provenance           *FlowEdgeProvenance    `json:"provenance,omitempty"`    // method, source_ids (plan 5.3)
}

// FlowEdgeProvenance captures how the metric was obtained (plan 5.3).
type FlowEdgeProvenance struct {
	Method    string   `json:"method"`               // "reported" | "estimated" | "inferred"
	SourceIDs []string `json:"source_ids,omitempty"` // canonical paths or fragment IDs
}

// SupplyLinkSegment represents one actor→product→actor segment for SupplyLink materialization.
// PathBuilder uses SupplyLink fragments; these are created from parsed chains so PathBuilder has data.
type SupplyLinkSegment struct {
	SupplierRef         string
	ProductRef          string
	CustomerRef         string
	SupplierFacilityRef string
	CustomerFacilityRef string
	TransportViaRefs    []string
	Confidence          float64
	// State holds metrics (volume, share, etc.) from SegmentMetrics for SupplyLink.State
	State map[string]models.MetricValue
	// FlowEdgeContract: completeness, validation gaps, raw payload (plan 5.3, fail-open)
	FlowEdgeContract *FlowEdgeContract
}

// ParsedChainResult contains fragments and edges extracted from supply chains.
type ParsedChainResult struct {
	// Fragments are new or updated fragments discovered in the chains.
	// Keys are canonical paths.
	Fragments map[string]*models.HorizonFragment
	// Edges are supply chain edges extracted from the chains.
	Edges []*ChainEdge
	// SupplyLinkSegments are actor→product→actor segments for SupplyLink materialization.
	// Enables PathBuilder to traverse; created from Scout/Research output.
	SupplyLinkSegments []*SupplyLinkSegment
	// Stats contains diagnostic counters for parsing (for [SupplyChain*] telemetry).
	Stats ParsedChainStats
	// ParseWarnings collects parser-level warnings (repair, coercion, partial parse) for traceability.
	ParseWarnings []string `json:"parseWarnings,omitempty"`
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
	// Qualifiers carry edge-specific metadata (e.g. at_facility for TRANSFORMS_INTO).
	// Passed through to DependencyEdge.Qualifiers on materialization.
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty"`
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

		// Ensure transport facilities become fragments (same canonicalization/dedup as other nodes)
		for _, pth := range chain.TransportVia {
			pth = strings.TrimSpace(pth)
			if pth != "" && strings.HasPrefix(pth, "/fac/") && result.Fragments[pth] == nil {
				frag := p.createTransportFacilityFragment(pth)
				if frag != nil {
					result.Fragments[pth] = frag
				}
			}
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

			// Infer TRANSFORMS_INTO with at_facility when pattern: product -> facility -> product
			// The facility consumes upstream product and produces downstream product; transformation happens there.
			if i >= 1 && i < len(chain.Nodes)-1 {
				prevNode := chain.Nodes[i-1]
				nextNode := chain.Nodes[i+1]
				if p.isProductType(prevNode.Type, prevNode.CanonicalPath) &&
					p.isActorType(node.Type, node.CanonicalPath) &&
					strings.HasPrefix(strings.TrimSpace(node.CanonicalPath), "/fac/") &&
					p.isProductType(nextNode.Type, nextNode.CanonicalPath) {
					prevPath := strings.TrimSpace(prevNode.CanonicalPath)
					nextPath := strings.TrimSpace(nextNode.CanonicalPath)
					facPath := strings.TrimSpace(node.CanonicalPath)
					if prevPath != "" && nextPath != "" && facPath != "" {
						srcPath, dstPath := prevPath, nextPath
						if shouldSwapProductStageOrder(prevPath, nextPath) {
							srcPath, dstPath = nextPath, prevPath
						}
						transformEdge := &ChainEdge{
							SourceCanonicalPath: srcPath,
							TargetCanonicalPath: dstPath,
							RelationType:        string(models.DependencyTypeTransformsInto),
							Confidence:          chain.Confidence,
							Reason:              fmt.Sprintf("Supply chain: %s transforms into %s at %s. %s", prevNode.Name, nextNode.Name, node.Name, chain.Reason),
							Qualifiers: map[string]interface{}{
								models.QualifierAtFacility: facPath,
							},
						}
						result.Edges = append(result.Edges, transformEdge)
						result.Stats.EdgesCreated++
					}
				}
			}
		}
		// Extract SupplyLink segments (supplier actor→product→customer actor) for PathBuilder materialization
		for i := 0; i < len(chain.Nodes)-2; i++ {
			a, prod, b := chain.Nodes[i], chain.Nodes[i+1], chain.Nodes[i+2]
			if p.isSupplyActorType(a.Type, a.CanonicalPath) &&
				p.isProductType(prod.Type, prod.CanonicalPath) &&
				p.isSupplyActorType(b.Type, b.CanonicalPath) {
				srcPath := strings.TrimSpace(a.CanonicalPath)
				prodPath := strings.TrimSpace(prod.CanonicalPath)
				dstPath := strings.TrimSpace(b.CanonicalPath)
				if srcPath != "" && prodPath != "" && dstPath != "" {
					seg := &SupplyLinkSegment{
						SupplierRef: srcPath,
						ProductRef:  prodPath,
						CustomerRef: dstPath,
						Confidence:  chain.Confidence,
					}
					for _, pth := range chain.TransportVia {
						pth = strings.TrimSpace(pth)
						if pth != "" && strings.HasPrefix(pth, "/fac/") {
							seg.TransportViaRefs = append(seg.TransportViaRefs, pth)
						}
					}
					// Extract segmentMetrics for this segment (fromNodeIndex=i, toNodeIndex=i+2, productIndex=i+1)
					state, contract := p.extractSegmentState(chain.SegmentMetrics, i, i+2, i+1, sourceFragmentPath, chain.Confidence)
					if state == nil {
						seg.State = make(map[string]models.MetricValue)
					} else {
						seg.State = state
						seg.FlowEdgeContract = contract
						if contract != nil && len(contract.ParseWarnings) > 0 {
							for _, w := range contract.ParseWarnings {
								result.ParseWarnings = append(result.ParseWarnings, fmt.Sprintf("segment %s→%s: %s", srcPath, dstPath, w))
							}
						}
					}
					result.SupplyLinkSegments = append(result.SupplyLinkSegments, seg)
					// Material flow semantics: supplier actor -> customer actor via product.
					// This is the only place where SUPPLIES_TO is inferred from chain topology.
					supQualifiers := map[string]interface{}{
						"source_role":     "supplier",
						"target_role":     "customer",
						"direction_basis": "supply_flow",
						"product_ref":     prodPath,
					}
					if len(seg.TransportViaRefs) > 0 {
						supQualifiers[models.QualifierTransportVia] = seg.TransportViaRefs
					}
					supReason := fmt.Sprintf("Supply chain: %s supplies %s to %s. %s", a.Name, prod.Name, b.Name, chain.Reason)
					if len(seg.TransportViaRefs) > 0 {
						supReason = fmt.Sprintf("Supply chain: %s supplies %s to %s via %v. %s", a.Name, prod.Name, b.Name, seg.TransportViaRefs, chain.Reason)
					}
					result.Edges = append(result.Edges, &ChainEdge{
						SourceCanonicalPath: srcPath,
						TargetCanonicalPath: dstPath,
						RelationType:        string(models.DependencyTypeSuppliesTo),
						Confidence:          chain.Confidence,
						Reason:              supReason,
						Qualifiers:          supQualifiers,
					})
					result.Stats.EdgesCreated++
					// Alternatives: create additional SupplyLink segments for alternative suppliers
					for _, alt := range chain.Alternatives {
						if alt.CustomerNodeIndex == i+2 && alt.ProductNodeIndex == i+1 {
							for _, sup := range alt.Suppliers {
								supRef := strings.TrimSpace(sup.SupplierRef)
								if supRef != "" && supRef != srcPath {
									altState := map[string]models.MetricValue{
										"technical_fit":  {Value: sup.TechnicalFit},
										"regulatory_fit": {Value: sup.RegulatoryFit},
										"confidence":     {Value: sup.Confidence},
									}
									if sup.AvailableCapacity != nil {
										u := sup.AvailableCapacity.Unit
										altState["available_capacity"] = models.MetricValue{Value: sup.AvailableCapacity.Value, Unit: &u, Confidence: sup.AvailableCapacity.Confidence}
									}
									if sup.LeadTimeToSwitch != nil {
										u := sup.LeadTimeToSwitch.Unit
										if u == "" {
											u = "days"
										}
										altState["lead_time_to_switch"] = models.MetricValue{Value: sup.LeadTimeToSwitch.Value, Unit: &u, Confidence: sup.LeadTimeToSwitch.Confidence}
									}
									if sup.CostToSwitch != nil {
										u := sup.CostToSwitch.Unit
										if u == "" {
											u = "usd"
										}
										altState["cost_to_switch"] = models.MetricValue{Value: sup.CostToSwitch.Value, Unit: &u, Confidence: sup.CostToSwitch.Confidence}
									}
									altSeg := &SupplyLinkSegment{
										SupplierRef: supRef,
										ProductRef:  prodPath,
										CustomerRef: dstPath,
										Confidence:  sup.Confidence,
										State:       altState,
									}
									result.SupplyLinkSegments = append(result.SupplyLinkSegments, altSeg)
								}
							}
							break
						}
					}
				}
			}
		}

	}

	return result, nil
}

// extractSegmentState builds SupplyLink.State from SegmentMetrics for a given segment.
// Returns state and FlowEdgeContract (contractCompleteness, validationGaps, rawPayload, provenance, parseWarnings) for fail-open.
func (p *SupplyChainParser) extractSegmentState(metrics []agents.SegmentMetrics, fromIdx, toIdx, productIdx int, sourceFragmentPath string, chainConfidence float64) (map[string]models.MetricValue, *FlowEdgeContract) {
	if len(metrics) == 0 {
		return nil, nil
	}
	state := make(map[string]models.MetricValue)
	var gaps []string
	rawPayload := make(map[string]interface{})
	completeness := "full"
	for _, m := range metrics {
		if m.FromNodeIndex != fromIdx || m.ToNodeIndex != toIdx || m.ProductIndex != productIdx {
			continue
		}
		if m.Volume != nil {
			origUnit := strings.TrimSpace(m.Volume.Unit)
			unit := taxonomy.NormalizeSupplyChainMetricUnit(origUnit, false)
			if origUnit != "" && !taxonomy.IsValidMetricUnit(unit, false) {
				if unit == "" {
					unit = strings.ToLower(origUnit)
				}
				gaps = append(gaps, "unknown_unit:"+unit)
				completeness = "partial"
			} else if unit == "" {
				unit = origUnit
			}
			u := unit
			state["volume"] = models.MetricValue{Value: m.Volume.Value, Unit: &u, Confidence: m.Volume.Confidence}
			rawPayload["volume"] = map[string]interface{}{"value": m.Volume.Value, "unit": origUnit, "confidence": m.Volume.Confidence}
		}
		if m.Share != nil {
			origUnit := strings.TrimSpace(m.Share.Unit)
			unit := taxonomy.NormalizeSupplyChainMetricUnit(origUnit, false)
			if origUnit != "" && !taxonomy.IsValidMetricUnit(unit, false) {
				if unit == "" {
					unit = strings.ToLower(origUnit)
				}
				gaps = append(gaps, "unknown_unit:"+unit)
				completeness = "partial"
			} else if unit == "" {
				unit = origUnit
			}
			u := unit
			state["share"] = models.MetricValue{Value: m.Share.Value, Unit: &u, Confidence: m.Share.Confidence}
		}
		if m.Period != "" {
			u := m.Period
			state["period"] = models.MetricValue{Value: 0, Unit: &u}
		} else if m.Volume != nil || m.Share != nil {
			gaps = append(gaps, "missing_period")
			completeness = "partial"
		}
		if m.Start != "" || m.End != "" {
			if m.Start != "" {
				state["start"] = models.MetricValue{Value: 0, Unit: &m.Start}
			}
			if m.End != "" {
				state["end"] = models.MetricValue{Value: 0, Unit: &m.End}
			}
		}
		if m.Confidence > 0 {
			if _, ok := state["confidence"]; !ok {
				state["confidence"] = models.MetricValue{Value: m.Confidence, Confidence: 0.8}
			}
		}
		break
	}
	if len(state) == 0 {
		return nil, nil
	}
	contract := &FlowEdgeContract{
		ContractCompleteness: completeness,
		ValidationGaps:       gaps,
	}
	if len(rawPayload) > 0 {
		contract.RawPayload = rawPayload
	}
	if len(gaps) > 0 {
		contract.ParseWarnings = append([]string{}, gaps...)
	}
	// Provenance: inferred from LLM; reported if high confidence and no gaps
	method := "inferred"
	if chainConfidence >= 0.8 && completeness == "full" {
		method = "reported"
	} else if chainConfidence >= 0.6 {
		method = "estimated"
	}
	contract.Provenance = &FlowEdgeProvenance{Method: method}
	if sourceFragmentPath != "" {
		contract.Provenance.SourceIDs = []string{sourceFragmentPath}
	}
	return state, contract
}

// createTransportFacilityFragment creates a minimal HorizonFragment for a transport facility path.
// Used when transportVia paths are not already in chain nodes; same canonicalization rules apply.
func (p *SupplyChainParser) createTransportFacilityFragment(canonicalPath string) *models.HorizonFragment {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" || !strings.HasPrefix(canonicalPath, "/fac/") {
		return nil
	}
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	name := parts[len(parts)-1]
	if name == "" && len(parts) > 1 {
		name = parts[len(parts)-2]
	}
	name = strings.ReplaceAll(name, "-", " ")
	return &models.HorizonFragment{
		CanonicalPath: canonicalPath,
		Type:          models.FragmentTypeFacility,
		Labels:        map[string]string{"en": name},
		Identifiers:   map[string]string{},
		State:         make(map[string]models.MetricValue),
	}
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
	case "PROGRAM":
		return models.FragmentTypeProgram
	case "MARKET":
		return models.FragmentTypeMarket
	case "GEO_REGION":
		return models.FragmentTypeGeoRegion
	case "DEPOSIT":
		return models.FragmentTypeDeposit
	case "COMMUNITY":
		return models.FragmentTypeCommunity
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
	case strings.HasPrefix(canonicalPath, "/program/"):
		return models.FragmentTypeProgram
	case strings.HasPrefix(canonicalPath, "/market/"):
		return models.FragmentTypeMarket
	case strings.HasPrefix(canonicalPath, "/loc/"):
		return models.FragmentTypeGeoRegion
	case strings.HasPrefix(canonicalPath, "/deposit/"):
		return models.FragmentTypeDeposit
	case strings.HasPrefix(canonicalPath, "/community/"):
		return models.FragmentTypeCommunity
	}

	return ""
}

// createEdgeBetweenNodes creates an edge based on the node types.
// Supply chain pattern: actor → product → actor → product
// - Actor (org/fac) → Product: PRODUCES
// - Product → Actor: actor CONSUMES product (or invert to DEPENDS_ON)
// - Facility → Deposit: EXTRACTS_FROM
// - Actor → Actor: no direct inference (ambiguous without product-flow context)
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
		relationType = string(models.DependencyTypeProduces)
	case fromIsProduct && toIsActor:
		return &ChainEdge{
			SourceCanonicalPath: toPath,
			TargetCanonicalPath: fromPath,
			RelationType:        string(models.DependencyTypeDependsOn),
			Confidence:          confidence,
			Reason:              fmt.Sprintf("Supply chain: %s consumes %s. %s", to.Name, from.Name, reason),
		}
	case p.isFacilityType(from.Type, fromPath) && p.isDepositType(to.Type, toPath):
		return &ChainEdge{
			SourceCanonicalPath: fromPath,
			TargetCanonicalPath: toPath,
			RelationType:        string(models.DependencyTypeExtractsFrom),
			Confidence:          confidence,
			Reason:              fmt.Sprintf("Supply chain: %s extracts from %s. %s", from.Name, to.Name, reason),
		}
	case fromIsActor && toIsActor:
		return nil
	case fromIsProduct && toIsProduct:
		relationType = string(models.DependencyTypeTransformsInto)
		srcPath, dstPath := fromPath, toPath
		if shouldSwapProductStageOrder(fromPath, toPath) {
			srcPath, dstPath = toPath, fromPath
		}
		return &ChainEdge{
			SourceCanonicalPath: srcPath,
			TargetCanonicalPath: dstPath,
			RelationType:        relationType,
			Confidence:          confidence,
			Reason:              fmt.Sprintf("Supply chain: %s → %s. %s", from.Name, to.Name, reason),
		}
	default:
		relationType = string(models.DependencyTypeDependsOn)
	}
	_ = fromIsProduct
	_ = toIsProduct

	return &ChainEdge{
		SourceCanonicalPath: fromPath,
		TargetCanonicalPath: toPath,
		RelationType:        relationType,
		Confidence:          confidence,
		Reason:              fmt.Sprintf("Supply chain: %s → %s. %s", from.Name, to.Name, reason),
	}
}

// isActorType checks if the node represents an actor (org, facility, or deposit for extraction).
func (p *SupplyChainParser) isActorType(nodeType string, canonicalPath string) bool {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	if nodeType == "ORGANIZATION" || nodeType == "FACILITY" || nodeType == "DEPOSIT" {
		return true
	}
	return strings.HasPrefix(canonicalPath, "/org/") || strings.HasPrefix(canonicalPath, "/fac/") || strings.HasPrefix(canonicalPath, "/deposit/")
}

// isSupplyActorType checks if a node can be a supplier/customer endpoint in SUPPLIES_TO.
func (p *SupplyChainParser) isSupplyActorType(nodeType string, canonicalPath string) bool {
	return p.isOrganizationType(nodeType, canonicalPath) || p.isFacilityType(nodeType, canonicalPath)
}

func (p *SupplyChainParser) isOrganizationType(nodeType string, canonicalPath string) bool {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	if nodeType == "ORGANIZATION" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(canonicalPath), "/org/")
}

func (p *SupplyChainParser) isFacilityType(nodeType string, canonicalPath string) bool {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	if nodeType == "FACILITY" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(canonicalPath), "/fac/")
}

func (p *SupplyChainParser) isDepositType(nodeType string, canonicalPath string) bool {
	nodeType = strings.ToUpper(strings.TrimSpace(nodeType))
	if nodeType == "DEPOSIT" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(canonicalPath), "/deposit/")
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

// shouldSwapProductStageOrder returns true when a TRANSFORMS_INTO edge has
// downstream product as source and upstream product as target.
// Correct semantics: upstream (lower stage rank) → downstream (higher stage rank).
// Delegates to models.ProductStageRankFromCanonicalPath (single source of truth).
func shouldSwapProductStageOrder(sourcePath, targetPath string) bool {
	srcRank, srcOk := models.ProductStageRankFromCanonicalPath(sourcePath)
	tgtRank, tgtOk := models.ProductStageRankFromCanonicalPath(targetPath)
	if srcOk && tgtOk && srcRank > tgtRank {
		return true
	}
	return false
}
