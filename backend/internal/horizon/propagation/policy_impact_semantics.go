package propagation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// policyImpactSemantics: edge×axis matrix and decode helpers for policy-impact domain.
// Single source of truth for policy-impact transformation semantics (TransferHook, Decode).
type policyEdgeAxisSemantics struct {
	BaseFactor        float64
	DefaultAxisFactor float64
	AxisFactor        map[string]float64
}

func (s policyEdgeAxisSemantics) factorForAxis(axis string) (float64, bool) {
	axis = strings.TrimSpace(axis)
	if axis == "" {
		return 0, false
	}
	base := s.BaseFactor
	if base <= 0 {
		base = 1.0
	}
	if f, ok := s.AxisFactor[axis]; ok {
		if f <= 0 {
			return 0, false
		}
		return base * f, true
	}
	if s.DefaultAxisFactor <= 0 {
		return 0, false
	}
	return base * s.DefaultAxisFactor, true
}

var policyImpactEdgeAxisMatrix = map[horizonmodels.DependencyType]policyEdgeAxisSemantics{
	horizonmodels.DependencyTypeRegulates: {
		BaseFactor:        1.08,
		DefaultAxisFactor: 0,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalAccess:         1.25,
			horizonmodels.AxisLegalCompliance:     1.20,
			horizonmodels.AxisFinancialSettlement: 1.10,
			horizonmodels.AxisInfoUncertainty:     1.05,
			horizonmodels.AxisPhysicalFlowDemand:  0.85,
		},
	},
	horizonmodels.DependencyTypeAppliesTo: {
		BaseFactor:        1.05,
		DefaultAxisFactor: 0,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalAccess:         1.20,
			horizonmodels.AxisLegalCompliance:     1.18,
			horizonmodels.AxisFinancialSettlement: 1.05,
			horizonmodels.AxisInfoUncertainty:     1.05,
		},
	},
	horizonmodels.DependencyTypeImposesCondition: {
		BaseFactor:        1.10,
		DefaultAxisFactor: 0,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalAccess:         1.25,
			horizonmodels.AxisLegalCompliance:     1.20,
			horizonmodels.AxisFinancialSettlement: 1.10,
			horizonmodels.AxisInfoUncertainty:     1.10,
			horizonmodels.AxisPhysicalFlowDemand:  0.90,
		},
	},
	horizonmodels.DependencyTypeControlsAccess: {
		BaseFactor:        1.12,
		DefaultAxisFactor: 0,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalAccess:         1.30,
			horizonmodels.AxisLegalCompliance:     1.18,
			horizonmodels.AxisFinancialSettlement: 1.08,
			horizonmodels.AxisInfoUncertainty:     1.05,
		},
	},
	horizonmodels.DependencyTypeAdministers: {
		BaseFactor:        1.02,
		DefaultAxisFactor: 0.75,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalCompliance:     1.08,
			horizonmodels.AxisFinancialSettlement: 1.00,
			horizonmodels.AxisInfoUncertainty:     0.95,
		},
	},
	horizonmodels.DependencyTypeDependsOn: {
		BaseFactor:        0.97,
		DefaultAxisFactor: 0.70,
		AxisFactor: map[string]float64{
			horizonmodels.AxisFinancialSettlement:   1.00,
			horizonmodels.AxisFinancialCredit:       0.95,
			horizonmodels.AxisPhysicalFlowSupply:    1.05,
			horizonmodels.AxisPhysicalFlowDemand:    1.00,
			horizonmodels.AxisLogisticsCapacity:     0.95,
			horizonmodels.AxisLogisticsLatency:      0.95,
			horizonmodels.AxisOperationalContinuity: 0.95,
			horizonmodels.AxisLegalCompliance:       0.80,
			horizonmodels.AxisInfoUncertainty:       0.80,
		},
	},
	horizonmodels.DependencyTypeSuppliesTo: {
		BaseFactor:        0.95,
		DefaultAxisFactor: 0.65,
		AxisFactor: map[string]float64{
			horizonmodels.AxisPhysicalFlowSupply:    1.10,
			horizonmodels.AxisPhysicalFlowDemand:    1.05,
			horizonmodels.AxisFinancialSettlement:   1.00,
			horizonmodels.AxisLogisticsCapacity:     1.00,
			horizonmodels.AxisLogisticsLatency:      1.00,
			horizonmodels.AxisOperationalContinuity: 0.90,
		},
	},
	horizonmodels.DependencyTypeConsumes: {
		BaseFactor:        0.95,
		DefaultAxisFactor: 0.65,
		AxisFactor: map[string]float64{
			horizonmodels.AxisPhysicalFlowDemand:    1.10,
			horizonmodels.AxisPhysicalFlowSupply:    0.95,
			horizonmodels.AxisFinancialSettlement:   0.95,
			horizonmodels.AxisOperationalContinuity: 0.90,
		},
	},
	horizonmodels.DependencyTypeParticipatesIn: {
		BaseFactor:        0.90,
		DefaultAxisFactor: 0.65,
		AxisFactor: map[string]float64{
			horizonmodels.AxisPhysicalFlowDemand:  1.05,
			horizonmodels.AxisFinancialSettlement: 1.00,
			horizonmodels.AxisLegalCompliance:     0.90,
			horizonmodels.AxisInfoReputation:      0.90,
		},
	},
	horizonmodels.DependencyTypeOperatesIn: {
		BaseFactor:        0.88,
		DefaultAxisFactor: 0.60,
		AxisFactor: map[string]float64{
			horizonmodels.AxisGeoConflict:         1.10,
			horizonmodels.AxisLegalCompliance:     1.00,
			horizonmodels.AxisFinancialSettlement: 0.90,
			horizonmodels.AxisPhysicalFlowDemand:  0.85,
			horizonmodels.AxisInfoUncertainty:     0.85,
		},
	},
	horizonmodels.DependencyTypeLocatedIn: {
		BaseFactor:        0.88,
		DefaultAxisFactor: 0.60,
		AxisFactor: map[string]float64{
			horizonmodels.AxisGeoConflict:           1.10,
			horizonmodels.AxisLegalAccess:           1.00,
			horizonmodels.AxisLegalCompliance:       0.95,
			horizonmodels.AxisLogisticsCapacity:     0.90,
			horizonmodels.AxisLogisticsLatency:      0.90,
			horizonmodels.AxisOperationalContinuity: 0.90,
		},
	},
	horizonmodels.DependencyTypeTriggeredBy: {
		BaseFactor:        0.95,
		DefaultAxisFactor: 0.75,
		AxisFactor: map[string]float64{
			horizonmodels.AxisGeoConflict:           1.20,
			horizonmodels.AxisOperationalContinuity: 1.10,
			horizonmodels.AxisInfoUncertainty:       1.05,
		},
	},
	horizonmodels.DependencyTypeInfluences: {
		BaseFactor:        0.80,
		DefaultAxisFactor: 0.50,
		AxisFactor: map[string]float64{
			horizonmodels.AxisInfoUncertainty:     1.20,
			horizonmodels.AxisInfoReputation:      1.10,
			horizonmodels.AxisLegalCompliance:     0.90,
			horizonmodels.AxisPhysicalFlowDemand:  0.85,
			horizonmodels.AxisFinancialSettlement: 0.85,
		},
	},
	horizonmodels.DependencyTypePartnersWith: {
		BaseFactor:        0.80,
		DefaultAxisFactor: 0.55,
		AxisFactor: map[string]float64{
			horizonmodels.AxisInfoReputation:      1.05,
			horizonmodels.AxisInfoUncertainty:     1.00,
			horizonmodels.AxisPhysicalFlowDemand:  0.80,
			horizonmodels.AxisFinancialSettlement: 0.80,
		},
	},
	horizonmodels.DependencyTypeAssociatedWith: {
		BaseFactor:        0.75,
		DefaultAxisFactor: 0.55,
		AxisFactor: map[string]float64{
			horizonmodels.AxisInfoReputation:  1.05,
			horizonmodels.AxisInfoUncertainty: 1.00,
		},
	},
	horizonmodels.DependencyTypeOwns: {
		BaseFactor:        0.92,
		DefaultAxisFactor: 0.60,
		AxisFactor: map[string]float64{
			horizonmodels.AxisFinancialSettlement:   1.05,
			horizonmodels.AxisFinancialCredit:       1.05,
			horizonmodels.AxisLegalCompliance:       0.95,
			horizonmodels.AxisOperationalContinuity: 0.90,
		},
	},
	horizonmodels.DependencyTypeControls: {
		BaseFactor:        0.95,
		DefaultAxisFactor: 0.65,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalAccess:           1.05,
			horizonmodels.AxisLegalCompliance:       1.00,
			horizonmodels.AxisFinancialSettlement:   0.95,
			horizonmodels.AxisOperationalContinuity: 0.90,
		},
	},
	horizonmodels.DependencyTypeManages: {
		BaseFactor:        0.92,
		DefaultAxisFactor: 0.60,
		AxisFactor: map[string]float64{
			horizonmodels.AxisOperationalContinuity: 1.00,
			horizonmodels.AxisLegalCompliance:       0.90,
			horizonmodels.AxisFinancialSettlement:   0.85,
		},
	},
	horizonmodels.DependencyTypeInvestsIn: {
		BaseFactor:        0.85,
		DefaultAxisFactor: 0.60,
		AxisFactor: map[string]float64{
			horizonmodels.AxisFinancialCredit:     1.00,
			horizonmodels.AxisFinancialSettlement: 0.95,
			horizonmodels.AxisInfoReputation:      0.85,
		},
	},
	horizonmodels.DependencyTypeUsesMechanism: {
		BaseFactor:        0.90,
		DefaultAxisFactor: 0.60,
		AxisFactor: map[string]float64{
			horizonmodels.AxisLegalCompliance:     1.05,
			horizonmodels.AxisInfoUncertainty:     0.95,
			horizonmodels.AxisFinancialSettlement: 0.90,
		},
	},
	horizonmodels.DependencyTypeAffects: {
		BaseFactor:        0.92,
		DefaultAxisFactor: 0.70,
		AxisFactor: map[string]float64{
			horizonmodels.AxisGeoConflict:           1.15,
			horizonmodels.AxisOperationalContinuity: 1.10,
			horizonmodels.AxisPhysicalFlowDemand:    0.95,
			horizonmodels.AxisFinancialSettlement:   0.95,
			horizonmodels.AxisLegalCompliance:       0.90,
			horizonmodels.AxisInfoUncertainty:      0.95,
			horizonmodels.AxisLogisticsCapacity:     0.85,
		},
	},
	horizonmodels.DependencyTypeInGeoScope: {
		BaseFactor:        0.95,
		DefaultAxisFactor: 0.70,
		AxisFactor: map[string]float64{
			horizonmodels.AxisGeoConflict:     1.10,
			horizonmodels.AxisLegalAccess:     1.00,
			horizonmodels.AxisLegalCompliance: 1.00,
			horizonmodels.AxisInfoUncertainty: 0.95,
			horizonmodels.AxisInfoReputation:  0.90,
		},
	},
	horizonmodels.DependencyTypeAbout: {
		BaseFactor:        0.80,
		DefaultAxisFactor: 0.55,
		AxisFactor: map[string]float64{
			horizonmodels.AxisInfoUncertainty: 1.10,
			horizonmodels.AxisInfoReputation:  1.00,
		},
	},
}

func policyImpactTransferFactor(edgeType horizonmodels.DependencyType, axis string) (float64, bool) {
	sem, ok := policyImpactEdgeAxisMatrix[edgeType]
	if !ok {
		return 0, false
	}
	return sem.factorForAxis(axis)
}

func policyImpactTransferNodeFactor(node *horizonmodels.HorizonFragment, axis string) float64 {
	if node == nil {
		return 1.0
	}
	axis = strings.TrimSpace(axis)
	if axis == "" {
		return 1.0
	}
	cp := strings.TrimSpace(node.CanonicalPath)
	switch node.Type {
	case horizonmodels.FragmentTypeAffectedZone:
		switch axis {
		case horizonmodels.AxisGeoConflict:
			return 1.25
		case horizonmodels.AxisOperationalContinuity:
			return 1.15
		case horizonmodels.AxisInfoUncertainty:
			return 1.08
		default:
			return 0.90
		}
	case horizonmodels.FragmentTypeGeoRegion:
		switch axis {
		case horizonmodels.AxisGeoConflict:
			return 1.15
		case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
			return 1.05
		case horizonmodels.AxisFinancialSettlement:
			return 0.70
		default:
			return 0.85
		}
	case horizonmodels.FragmentTypeMarket:
		switch axis {
		case horizonmodels.AxisPhysicalFlowDemand:
			return 1.10
		case horizonmodels.AxisFinancialSettlement:
			return 1.05
		case horizonmodels.AxisLegalCompliance:
			return 1.00
		default:
			return 0.90
		}
	case horizonmodels.FragmentTypeProgram, horizonmodels.FragmentTypeMechanism, horizonmodels.FragmentTypeDecisionPoint, horizonmodels.FragmentTypeDomain:
		switch axis {
		case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
			return 1.12
		case horizonmodels.AxisInfoUncertainty:
			return 1.08
		default:
			return 0.85
		}
	case horizonmodels.FragmentTypeCommunity, horizonmodels.FragmentTypePerson, horizonmodels.FragmentTypeMediaChannel:
		switch axis {
		case horizonmodels.AxisInfoReputation:
			return 1.12
		case horizonmodels.AxisInfoUncertainty:
			return 1.05
		case horizonmodels.AxisPhysicalFlowDemand:
			return 0.92
		default:
			return 0.70
		}
	case horizonmodels.FragmentTypeOrganization, horizonmodels.FragmentTypeFacility:
		if strings.HasPrefix(cp, "/gov/") {
			switch axis {
			case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
				return 1.10
			case horizonmodels.AxisInfoUncertainty:
				return 1.05
			default:
				return 0.90
			}
		}
		switch axis {
		case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisPhysicalFlowSupply:
			return 1.00
		case horizonmodels.AxisLegalCompliance:
			return 0.92
		case horizonmodels.AxisInfoUncertainty, horizonmodels.AxisInfoReputation:
			return 0.82
		default:
			return 0.90
		}
	default:
		return 0.85
	}
}

func policyImpactDecodeScopeFactor(node *horizonmodels.HorizonFragment, scenario ScenarioInput) (float64, bool) {
	if node == nil {
		return 0, false
	}
	if !policyImpactDecodeTypeAllowed(node) {
		return 0, false
	}
	scopeGeo := policyImpactScenarioGeoScope(scenario)
	if scopeGeo == "" || scopeGeo == "global" {
		return policyImpactDecodeNodeFactor(node), true
	}
	nodeGeo, hasNodeGeo := policyImpactCanonicalGeo(node.CanonicalPath)
	if !hasNodeGeo {
		switch node.Type {
		case horizonmodels.FragmentTypeMechanism, horizonmodels.FragmentTypeDecisionPoint, horizonmodels.FragmentTypeDomain:
			return policyImpactDecodeNodeFactor(node) * 0.75, true
		default:
			return 0, false
		}
	}
	if !policyGeoOverlap(scopeGeo, nodeGeo) {
		return 0, false
	}
	factor := policyImpactDecodeNodeFactor(node)
	if scopeGeo != nodeGeo {
		factor *= 0.85
	}
	return factor, factor > 0
}

func policyImpactDecodeTypeAllowed(node *horizonmodels.HorizonFragment) bool {
	if node == nil {
		return false
	}
	switch node.Type {
	case horizonmodels.FragmentTypeGeoRegion,
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeProgram,
		horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeCommunity,
		horizonmodels.FragmentTypeDecisionPoint,
		horizonmodels.FragmentTypeMechanism,
		horizonmodels.FragmentTypeDomain:
		return true
	default:
		return false
	}
}

func policyImpactDecodeNodeFactor(node *horizonmodels.HorizonFragment) float64 {
	if node == nil {
		return 0
	}
	cp := strings.TrimSpace(node.CanonicalPath)
	switch node.Type {
	case horizonmodels.FragmentTypeGeoRegion:
		return 1.00
	case horizonmodels.FragmentTypeMarket:
		return 1.00
	case horizonmodels.FragmentTypeProgram:
		return 0.95
	case horizonmodels.FragmentTypeOrganization:
		if strings.HasPrefix(cp, "/gov/") {
			return 0.92
		}
		return 0.88
	case horizonmodels.FragmentTypeFacility:
		return 0.82
	case horizonmodels.FragmentTypeCommunity:
		return 0.78
	case horizonmodels.FragmentTypeDecisionPoint, horizonmodels.FragmentTypeMechanism:
		return 0.72
	case horizonmodels.FragmentTypeDomain:
		return 0.70
	default:
		return 0
	}
}

func policyImpactScenarioGeoScope(scenario ScenarioInput) string {
	locPath := strings.TrimSpace(policyImpactLocationPath(scenario.Parameters))
	if locPath == "" {
		return ""
	}
	geo, _ := policyImpactCanonicalGeo(locPath)
	return geo
}

func policyImpactCanonicalGeo(canonicalPath string) (string, bool) {
	cp := strings.TrimSpace(strings.ToLower(canonicalPath))
	if cp == "" {
		return "", false
	}
	parts, err := taxonomy.ParseCanonicalPathParts(cp)
	if err != nil || parts == nil {
		return "", false
	}
	switch parts.Namespace {
	case "loc":
		if parts.Loc != nil && strings.TrimSpace(parts.Loc.Country) != "" {
			return strings.TrimSpace(parts.Loc.Country), true
		}
	case "org":
		if parts.Org != nil && !parts.Org.Legacy && strings.TrimSpace(parts.Org.Geo) != "" {
			return strings.TrimSpace(parts.Org.Geo), true
		}
	case "gov":
		if parts.Gov != nil && strings.TrimSpace(parts.Gov.Geo) != "" {
			return strings.TrimSpace(parts.Gov.Geo), true
		}
	case "market":
		if parts.Market != nil && strings.TrimSpace(parts.Market.Geo) != "" {
			return strings.TrimSpace(parts.Market.Geo), true
		}
	case "community":
		if parts.Community != nil && strings.TrimSpace(parts.Community.Geo) != "" {
			return strings.TrimSpace(parts.Community.Geo), true
		}
	case "program":
		if parts.Program != nil && !parts.Program.Legacy && strings.TrimSpace(parts.Program.Jurisdiction) != "" {
			return strings.TrimSpace(parts.Program.Jurisdiction), true
		}
	case "domain":
		flat := strings.Split(strings.Trim(cp, "/"), "/")
		for i := 0; i+1 < len(flat); i++ {
			if flat[i] == "geo" {
				v := strings.TrimSpace(flat[i+1])
				if v != "" {
					return v, true
				}
			}
		}
	}
	return "", false
}

func policyGeoOverlap(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aParts := strings.Split(a, "-")
	bParts := strings.Split(b, "-")
	for _, x := range aParts {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		for _, y := range bParts {
			y = strings.TrimSpace(y)
			if y == "" {
				continue
			}
			if x == y {
				return true
			}
		}
	}
	return false
}
