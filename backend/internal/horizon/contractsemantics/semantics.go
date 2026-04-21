package contractsemantics

import (
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// GraphLayer is the architectural layer an edge participates in for
// the purposes of contract overlays and propagation.
type GraphLayer string

const (
	GraphLayerCorePropagation GraphLayer = "core_propagation"
	GraphLayerPathCarried     GraphLayer = "path_carried"
	GraphLayerContractOverlay GraphLayer = "contract_overlay"
	GraphLayerInterpretive    GraphLayer = "interpretive"
)

// DomainModeProfile is the contract surface for a single domain mode
// (supply_chain, governance, ...). All edge slices use the AH
// relation name space.
//
// Under the AH taxonomy many legacy edges (TRANSFORMS_INTO, USED_IN,
// HAS_MARKET_SEGMENT, OPERATES_IN, PARTICIPATES_IN ...) have been
// folded into attributes / canonical_path / siteTransforms and are
// no longer edges. Domain-mode profiles only enumerate AH-existing
// relations; the wire-format prompt allowlist returned by
// AllowedEdgeStringsForDomainMode is sourced from the legacy
// taxonomy SoT (so prompts keep their established vocabulary).
type DomainModeProfile struct {
	Mode               string
	PrimaryLayer       GraphLayer
	AllowedEntities    []string
	AllowedEdges       []ah.RelationName
	PathCarriedEdges   []ah.RelationName
	OverlayEdges       []ah.RelationName
	InterpretiveEdges  []ah.RelationName
	ComplementaryModes []string
	ArtifactKinds      []models.MaterializedKind
	Description        string
}

// ComposedProfile is the union of multiple DomainModeProfiles.
type ComposedProfile struct {
	Modes              []string
	PrimaryLayers      []GraphLayer
	AllowedEntities    []string
	AllowedEdges       []ah.RelationName
	PathCarriedEdges   []ah.RelationName
	OverlayEdges       []ah.RelationName
	InterpretiveEdges  []ah.RelationName
	ComplementaryModes []string
	ArtifactKinds      []models.MaterializedKind
}

var executionOrder = map[string]int{
	taxonomy.DomainModeSupplyChain:  10,
	taxonomy.DomainModeServiceChain: 15,
	taxonomy.DomainModeGovernance:   20,
	taxonomy.DomainModeAssurance:    30,
	taxonomy.DomainModeMarket:       40,
	taxonomy.DomainModePolicyImpact: 50,
	taxonomy.DomainModeStrategic:    60,
	taxonomy.DomainModeEcosystem:    70,
	taxonomy.DomainModeGeneral:      80,
}

// SortDomainModesForExecution returns a deterministic ordering of
// domain modes by their canonical execution priority.
func SortDomainModesForExecution(modes []string) []string {
	normalized := taxonomy.NormalizeDomainModes(modes)
	sort.SliceStable(normalized, func(i, j int) bool {
		li, lok := executionOrder[normalized[i]]
		lj, rok := executionOrder[normalized[j]]
		if !lok {
			li = 999
		}
		if !rok {
			lj = 999
		}
		if li == lj {
			return normalized[i] < normalized[j]
		}
		return li < lj
	})
	return normalized
}

// ProfileForDomainMode returns the contract profile for a single
// domain mode. AllowedEdges is derived from the legacy wire SoT
// translated into AH relations; the curated Path/Overlay/Interpretive
// slices are AH-native.
func ProfileForDomainMode(mode string) DomainModeProfile {
	mode = normalizeMode(mode)
	allowedEntities := append([]string(nil), taxonomy.AllowedEntityTypesForDomainMode(mode)...)
	allowedEdges := normalizeWireRelations(taxonomy.AllowedEdgeTypesForDomainMode(mode))
	switch mode {
	case taxonomy.DomainModeSupplyChain:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerPathCarried,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			PathCarriedEdges: []ah.RelationName{
				ah.RelationNameProduces,
				ah.RelationNameSupplies,
				ah.RelationNameLegConnects,
				ah.RelationNamePassesThrough,
				ah.RelationNameTransshipsAt,
				ah.RelationNameRequiresCapability,
			},
			OverlayEdges: []ah.RelationName{
				ah.RelationNameOperates,
				ah.RelationNameJointVentureWith,
				ah.RelationNameMemberOf,
				ah.RelationNameInvestedIn,
				ah.RelationNameSupportedBy,
			},
			ComplementaryModes: []string{
				taxonomy.DomainModeGovernance,
				taxonomy.DomainModeAssurance,
				taxonomy.DomainModeMarket,
				taxonomy.DomainModePolicyImpact,
			},
			ArtifactKinds: []models.MaterializedKind{models.MaterializedKindSupplyChainPath},
			Description:   "Operational product flow and transformation paths.",
		}
	case taxonomy.DomainModeServiceChain:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerPathCarried,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			PathCarriedEdges: []ah.RelationName{
				ah.RelationNameProduces,
				ah.RelationNameSupplies,
				ah.RelationNameRequiresCapability,
			},
			OverlayEdges: []ah.RelationName{
				ah.RelationNameOperates,
				ah.RelationNameWields,
				ah.RelationNameMemberOf,
			},
			InterpretiveEdges: []ah.RelationName{
				ah.RelationNameInfluences,
			},
			ComplementaryModes: []string{
				taxonomy.DomainModeGovernance,
				taxonomy.DomainModeAssurance,
				taxonomy.DomainModeMarket,
				taxonomy.DomainModePolicyImpact,
				taxonomy.DomainModeStrategic,
			},
			ArtifactKinds: []models.MaterializedKind{models.MaterializedKindServiceChainPath},
			Description:   "Operational service delivery and capability access paths.",
		}
	case taxonomy.DomainModeGovernance:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerContractOverlay,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			OverlayEdges:    allowedEdges,
			ComplementaryModes: []string{
				taxonomy.DomainModeSupplyChain,
				taxonomy.DomainModeServiceChain,
				taxonomy.DomainModePolicyImpact,
				taxonomy.DomainModeMarket,
			},
			ArtifactKinds: []models.MaterializedKind{models.MaterializedKindGovernanceChain},
			Description:   "Ownership, control, management, and administrative overlays over operational entities.",
		}
	case taxonomy.DomainModeAssurance:
		return DomainModeProfile{
			Mode:               mode,
			PrimaryLayer:       GraphLayerContractOverlay,
			AllowedEntities:    allowedEntities,
			AllowedEdges:       allowedEdges,
			OverlayEdges:       allowedEdges,
			ComplementaryModes: []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeServiceChain, taxonomy.DomainModeGovernance, taxonomy.DomainModePolicyImpact},
			Description:        "Audit, certification, and regulatory oversight overlays.",
		}
	case taxonomy.DomainModeMarket:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerContractOverlay,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			OverlayEdges:    allowedEdges,
			ComplementaryModes: []string{
				taxonomy.DomainModeSupplyChain,
				taxonomy.DomainModeServiceChain,
				taxonomy.DomainModePolicyImpact,
				taxonomy.DomainModeEcosystem,
			},
			Description: "Market participation and segment overlays.",
		}
	case taxonomy.DomainModePolicyImpact:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerContractOverlay,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			OverlayEdges: []ah.RelationName{
				ah.RelationNameRegulates,
				ah.RelationNameImposes,
				ah.RelationNameGatekeeps,
				ah.RelationNameWields,
				ah.RelationNameEnactedVia,
				ah.RelationNameSubjectTo,
				ah.RelationNameConstrainedBy,
				ah.RelationNameOperates,
				ah.RelationNameMemberOf,
			},
			InterpretiveEdges: []ah.RelationName{
				ah.RelationNameInfluences,
				ah.RelationNameDisrupts,
			},
			ArtifactKinds: []models.MaterializedKind{
				models.MaterializedKindRegulatoryImpactMap,
				models.MaterializedKindRegulatoryImpactTransmissionPath,
				models.MaterializedKindRegulatoryTimeline,
				models.MaterializedKindRegulatoryCascadeMap,
				models.MaterializedKindRegulatoryImpactScenario,
			},
			ComplementaryModes: []string{
				taxonomy.DomainModeSupplyChain,
				taxonomy.DomainModeServiceChain,
				taxonomy.DomainModeGovernance,
				taxonomy.DomainModeMarket,
				taxonomy.DomainModeStrategic,
			},
			Description: "Policy/regulatory overlays and transmission conditions that act on operational paths.",
		}
	case taxonomy.DomainModeStrategic:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerContractOverlay,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			OverlayEdges:    allowedEdges,
			ComplementaryModes: []string{
				taxonomy.DomainModePolicyImpact,
				taxonomy.DomainModeSupplyChain,
				taxonomy.DomainModeServiceChain,
				taxonomy.DomainModeGovernance,
			},
			Description: "Access control and strategic decision overlays.",
		}
	case taxonomy.DomainModeEcosystem:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerInterpretive,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			OverlayEdges: []ah.RelationName{
				ah.RelationNameBeneficiallyOwns,
				ah.RelationNameSubsidiaryOf,
				ah.RelationNameControlsVia,
				ah.RelationNameSupplies,
				ah.RelationNameRegulates,
				ah.RelationNameInvestedIn,
				ah.RelationNameJointVentureWith,
				ah.RelationNameMemberOf,
			},
			InterpretiveEdges: []ah.RelationName{
				ah.RelationNameInfluences,
			},
			ArtifactKinds:      []models.MaterializedKind{models.MaterializedKindEcosystemMap},
			ComplementaryModes: []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeServiceChain, taxonomy.DomainModeGovernance, taxonomy.DomainModeMarket, taxonomy.DomainModePolicyImpact},
			Description:        "Interpretive ecosystem context over operational graph.",
		}
	default:
		return DomainModeProfile{Mode: mode, PrimaryLayer: GraphLayerInterpretive}
	}
}

// AllowedEdgeStringsForDomainMode returns the wire-format strings
// (UPPER_SNAKE legacy DependencyType) of allowed edges for a domain
// mode. This is the stable boundary used by LLM prompts and the
// frontend that has not yet migrated to the AH wire vocabulary.
func AllowedEdgeStringsForDomainMode(mode string) []string {
	return taxonomy.AllowedEdgeTypesForDomainMode(normalizeMode(mode))
}

// ComposeDomainModes unions multiple domain profiles into a single
// composed profile, deduplicating by AH relation.
func ComposeDomainModes(modes []string) ComposedProfile {
	ordered := SortDomainModesForExecution(modes)
	out := ComposedProfile{Modes: ordered}
	entitySeen := map[string]struct{}{}
	edgeSeen := map[ah.RelationName]struct{}{}
	pathSeen := map[ah.RelationName]struct{}{}
	overlaySeen := map[ah.RelationName]struct{}{}
	interpretSeen := map[ah.RelationName]struct{}{}
	compSeen := map[string]struct{}{}
	kindSeen := map[models.MaterializedKind]struct{}{}
	layerSeen := map[GraphLayer]struct{}{}
	for _, mode := range ordered {
		profile := ProfileForDomainMode(mode)
		if _, ok := layerSeen[profile.PrimaryLayer]; !ok && profile.PrimaryLayer != "" {
			layerSeen[profile.PrimaryLayer] = struct{}{}
			out.PrimaryLayers = append(out.PrimaryLayers, profile.PrimaryLayer)
		}
		for _, e := range profile.AllowedEntities {
			e = strings.TrimSpace(strings.ToUpper(e))
			if e == "" {
				continue
			}
			if _, ok := entitySeen[e]; ok {
				continue
			}
			entitySeen[e] = struct{}{}
			out.AllowedEntities = append(out.AllowedEntities, e)
		}
		appendUniqueRelations(&out.AllowedEdges, edgeSeen, profile.AllowedEdges)
		appendUniqueRelations(&out.PathCarriedEdges, pathSeen, profile.PathCarriedEdges)
		appendUniqueRelations(&out.OverlayEdges, overlaySeen, profile.OverlayEdges)
		appendUniqueRelations(&out.InterpretiveEdges, interpretSeen, profile.InterpretiveEdges)
		for _, mode := range profile.ComplementaryModes {
			mode = normalizeMode(mode)
			if mode == "" {
				continue
			}
			if _, ok := compSeen[mode]; ok {
				continue
			}
			compSeen[mode] = struct{}{}
			out.ComplementaryModes = append(out.ComplementaryModes, mode)
		}
		for _, kind := range profile.ArtifactKinds {
			if kind == "" {
				continue
			}
			if _, ok := kindSeen[kind]; ok {
				continue
			}
			kindSeen[kind] = struct{}{}
			out.ArtifactKinds = append(out.ArtifactKinds, kind)
		}
	}
	return out
}

// ModeAllowsEdge reports whether the AH relation is permitted in
// the named domain mode.
func ModeAllowsEdge(mode string, rel ah.RelationName) bool {
	rel = normalizeRelation(rel)
	if rel == "" {
		return false
	}
	profile := ProfileForDomainMode(mode)
	for _, allowed := range profile.AllowedEdges {
		if normalizeRelation(allowed) == rel {
			return true
		}
	}
	return false
}

// EdgeLayer assigns an AH relation to its architectural graph layer.
func EdgeLayer(rel ah.RelationName) GraphLayer {
	switch normalizeRelation(rel) {
	case ah.RelationNameDisrupts:
		return GraphLayerCorePropagation
	case ah.RelationNameProduces, ah.RelationNameSupplies,
		ah.RelationNameLegConnects, ah.RelationNamePassesThrough,
		ah.RelationNameTransshipsAt, ah.RelationNameRequiresCapability:
		return GraphLayerPathCarried
	case ah.RelationNameBeneficiallyOwns, ah.RelationNameSubsidiaryOf, ah.RelationNameControlsVia,
		ah.RelationNameOperates, ah.RelationNameInvestedIn,
		ah.RelationNameAssures, ah.RelationNameRegulates, ah.RelationNameImposes,
		ah.RelationNameGatekeeps, ah.RelationNameWields, ah.RelationNameEnactedVia,
		ah.RelationNameSubjectTo, ah.RelationNameConstrainedBy,
		ah.RelationNameMemberOf, ah.RelationNameJointVentureWith,
		ah.RelationNameManagedBy, ah.RelationNameHoldsBoardSeat,
		ah.RelationNameSupportedBy, ah.RelationNameCharters:
		return GraphLayerContractOverlay
	case ah.RelationNameInfluences:
		return GraphLayerInterpretive
	default:
		return GraphLayerInterpretive
	}
}

// EdgeSupportsPathCarrying reports whether the relation participates
// in a path-carried (operational) flow.
func EdgeSupportsPathCarrying(rel ah.RelationName) bool {
	return EdgeLayer(rel) == GraphLayerPathCarried
}

func normalizeMode(mode string) string {
	normalized := taxonomy.NormalizeDomainModes([]string{mode})
	if len(normalized) == 0 {
		return strings.TrimSpace(strings.ToLower(mode))
	}
	return normalized[0]
}

// normalizeWireRelations translates legacy UPPER_SNAKE wire strings
// from the taxonomy SoT into canonical AH relation names. Wire
// strings that have no AH edge home (folded into attributes,
// canonical_path, or dropped per migration_map.yaml) are silently
// skipped — those vocabulary items remain on the wire for prompt
// continuity but no longer correspond to a real edge.
func normalizeWireRelations(values []string) []ah.RelationName {
	out := make([]ah.RelationName, 0, len(values))
	seen := make(map[ah.RelationName]struct{}, len(values))
	for _, v := range values {
		dec, ok := models.NormalizeRelationName(v)
		if !ok || dec.Dropped || dec.Relation == "" {
			continue
		}
		if _, dup := seen[dec.Relation]; dup {
			continue
		}
		seen[dec.Relation] = struct{}{}
		out = append(out, dec.Relation)
	}
	return out
}

func normalizeRelation(rel ah.RelationName) ah.RelationName {
	trimmed := strings.TrimSpace(string(rel))
	if trimmed == "" {
		return ""
	}
	if normalized, ok := ah.NormalizeRelation(trimmed); ok {
		return normalized
	}
	if dec, ok := models.NormalizeRelationName(trimmed); ok && !dec.Dropped && dec.Relation != "" {
		return dec.Relation
	}
	return ah.RelationName(trimmed)
}

func appendUniqueRelations(dst *[]ah.RelationName, seen map[ah.RelationName]struct{}, values []ah.RelationName) {
	for _, value := range values {
		value = normalizeRelation(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		*dst = append(*dst, value)
	}
}
