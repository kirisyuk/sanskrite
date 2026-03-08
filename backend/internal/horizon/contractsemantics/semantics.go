package contractsemantics

import (
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type GraphLayer string

const (
	GraphLayerCorePropagation GraphLayer = "core_propagation"
	GraphLayerPathCarried     GraphLayer = "path_carried"
	GraphLayerContractOverlay GraphLayer = "contract_overlay"
	GraphLayerInterpretive    GraphLayer = "interpretive"
)

type DomainModeProfile struct {
	Mode               string
	PrimaryLayer       GraphLayer
	AllowedEntities    []string
	AllowedEdges       []models.DependencyType
	PathCarriedEdges   []models.DependencyType
	OverlayEdges       []models.DependencyType
	InterpretiveEdges  []models.DependencyType
	ComplementaryModes []string
	ArtifactKinds      []models.MaterializedKind
	Description        string
}

type ComposedProfile struct {
	Modes              []string
	PrimaryLayers      []GraphLayer
	AllowedEntities    []string
	AllowedEdges       []models.DependencyType
	PathCarriedEdges   []models.DependencyType
	OverlayEdges       []models.DependencyType
	InterpretiveEdges  []models.DependencyType
	ComplementaryModes []string
	ArtifactKinds      []models.MaterializedKind
}

var executionOrder = map[string]int{
	taxonomy.DomainModeSupplyChain:  10,
	taxonomy.DomainModeGovernance:   20,
	taxonomy.DomainModeAssurance:    30,
	taxonomy.DomainModeMarket:       40,
	taxonomy.DomainModePolicyImpact: 50,
	taxonomy.DomainModeStrategic:    60,
	taxonomy.DomainModeEcosystem:    70,
	taxonomy.DomainModeGeneral:      80,
}

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

func ProfileForDomainMode(mode string) DomainModeProfile {
	mode = normalizeMode(mode)
	allowedEntities := append([]string(nil), taxonomy.AllowedEntityTypesForDomainMode(mode)...)
	allowedEdges := normalizeEdgeTypes(taxonomy.AllowedEdgeTypesForDomainMode(mode))
	switch mode {
	case taxonomy.DomainModeSupplyChain:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerPathCarried,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			PathCarriedEdges: []models.DependencyType{
				models.DependencyTypeProduces,
				models.DependencyTypeSuppliesTo,
				models.DependencyTypeTransformsInto,
				models.DependencyTypeExtractsFrom,
			},
			OverlayEdges: []models.DependencyType{
				models.DependencyTypeLocatedIn,
				models.DependencyTypeOperatesIn,
				models.DependencyTypeParticipatesIn,
				models.DependencyTypeHasMarketSegment,
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
	case taxonomy.DomainModeGovernance:
		return DomainModeProfile{
			Mode:            mode,
			PrimaryLayer:    GraphLayerContractOverlay,
			AllowedEntities: allowedEntities,
			AllowedEdges:    allowedEdges,
			OverlayEdges:    allowedEdges,
			ComplementaryModes: []string{
				taxonomy.DomainModeSupplyChain,
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
			ComplementaryModes: []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance, taxonomy.DomainModePolicyImpact},
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
			OverlayEdges: []models.DependencyType{
				models.DependencyTypeRegulates,
				models.DependencyTypeControlsAccess,
				models.DependencyTypeImposesCondition,
				models.DependencyTypeGatekeepsDecision,
				models.DependencyTypeUsesMechanism,
				models.DependencyTypeAppliesTo,
				models.DependencyTypeAdministers,
				models.DependencyTypeParticipatesIn,
				models.DependencyTypeLocatedIn,
				models.DependencyTypeOperatesIn,
			},
			InterpretiveEdges: []models.DependencyType{
				models.DependencyTypeInfluences,
				models.DependencyTypeDependsOn,
			},
			ArtifactKinds: []models.MaterializedKind{models.MaterializedKindRegulatoryImpactMap},
			ComplementaryModes: []string{
				taxonomy.DomainModeSupplyChain,
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
			OverlayEdges: []models.DependencyType{
				models.DependencyTypeParticipatesIn,
				models.DependencyTypeOwns,
				models.DependencyTypeSubsidiaryOf,
				models.DependencyTypeControls,
				models.DependencyTypeSuppliesTo,
				models.DependencyTypeRegulates,
				models.DependencyTypeInvestsIn,
				models.DependencyTypePartnersWith,
				models.DependencyTypeLocatedIn,
				models.DependencyTypeOperatesIn,
			},
			InterpretiveEdges: []models.DependencyType{
				models.DependencyTypeDependsOn,
				models.DependencyTypeInfluences,
				models.DependencyTypeCompetesWith,
			},
			ArtifactKinds:      []models.MaterializedKind{models.MaterializedKindEcosystemMap},
			ComplementaryModes: []string{taxonomy.DomainModeSupplyChain, taxonomy.DomainModeGovernance, taxonomy.DomainModeMarket, taxonomy.DomainModePolicyImpact},
			Description:        "Interpretive ecosystem context over operational graph.",
		}
	default:
		return DomainModeProfile{Mode: mode, PrimaryLayer: GraphLayerInterpretive}
	}
}

func ComposeDomainModes(modes []string) ComposedProfile {
	ordered := SortDomainModesForExecution(modes)
	out := ComposedProfile{Modes: ordered}
	entitySeen := map[string]struct{}{}
	edgeSeen := map[models.DependencyType]struct{}{}
	pathSeen := map[models.DependencyType]struct{}{}
	overlaySeen := map[models.DependencyType]struct{}{}
	interpretSeen := map[models.DependencyType]struct{}{}
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
		appendUniqueEdgeTypes(&out.AllowedEdges, edgeSeen, profile.AllowedEdges)
		appendUniqueEdgeTypes(&out.PathCarriedEdges, pathSeen, profile.PathCarriedEdges)
		appendUniqueEdgeTypes(&out.OverlayEdges, overlaySeen, profile.OverlayEdges)
		appendUniqueEdgeTypes(&out.InterpretiveEdges, interpretSeen, profile.InterpretiveEdges)
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

func ModeAllowsEdge(mode string, edgeType models.DependencyType) bool {
	profile := ProfileForDomainMode(mode)
	edgeType = normalizeEdgeType(edgeType)
	for _, allowed := range profile.AllowedEdges {
		if normalizeEdgeType(allowed) == edgeType {
			return true
		}
	}
	return false
}

func EdgeLayer(edgeType models.DependencyType) GraphLayer {
	edgeType = normalizeEdgeType(edgeType)
	switch edgeType {
	case models.DependencyTypeAffects, models.DependencyTypeTriggeredBy:
		return GraphLayerCorePropagation
	case models.DependencyTypeProduces, models.DependencyTypeSuppliesTo, models.DependencyTypeTransformsInto, models.DependencyTypeExtractsFrom:
		return GraphLayerPathCarried
	case models.DependencyTypeOwns, models.DependencyTypeSubsidiaryOf, models.DependencyTypeControls,
		models.DependencyTypeManages, models.DependencyTypeInvestsIn, models.DependencyTypeBoardMemberOf,
		models.DependencyTypeEmployedBy, models.DependencyTypeFounded, models.DependencyTypeAdministers,
		models.DependencyTypeAssures, models.DependencyTypeRegulates, models.DependencyTypeControlsAccess,
		models.DependencyTypeImposesCondition, models.DependencyTypeGatekeepsDecision, models.DependencyTypeUsesMechanism,
		models.DependencyTypeAppliesTo, models.DependencyTypeParticipatesIn, models.DependencyTypeLocatedIn,
		models.DependencyTypeOperatesIn, models.DependencyTypeHasMarketSegment, models.DependencyTypePartnersWith:
		return GraphLayerContractOverlay
	default:
		return GraphLayerInterpretive
	}
}

func EdgeSupportsPathCarrying(edgeType models.DependencyType) bool {
	return EdgeLayer(edgeType) == GraphLayerPathCarried
}

func normalizeMode(mode string) string {
	normalized := taxonomy.NormalizeDomainModes([]string{mode})
	if len(normalized) == 0 {
		return strings.TrimSpace(strings.ToLower(mode))
	}
	return normalized[0]
}

func normalizeEdgeTypes(values []string) []models.DependencyType {
	out := make([]models.DependencyType, 0, len(values))
	for _, v := range values {
		if t := normalizeEdgeType(models.DependencyType(v)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func normalizeEdgeType(edgeType models.DependencyType) models.DependencyType {
	edgeType = models.DependencyType(strings.TrimSpace(strings.ToUpper(string(edgeType))))
	return edgeType
}

func appendUniqueEdgeTypes(dst *[]models.DependencyType, seen map[models.DependencyType]struct{}, values []models.DependencyType) {
	for _, value := range values {
		value = normalizeEdgeType(value)
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
