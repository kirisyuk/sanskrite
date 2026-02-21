package propagation

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type amplitudeBreakdown struct {
	Amplitude float64
	Factors   map[string]float64
}

func classifySignalTransfer(axis string, edgeType horizonmodels.DependencyType, fromFrag, toFrag *horizonmodels.HorizonFragment) (channel string, scope string, spillover string, fromGeo string, toGeo string) {
	channel = classifySignalChannel(axis, edgeType)
	fromGeo, _ = policyImpactCanonicalGeo(fragmentCanonicalPath(fromFrag))
	toGeo, _ = policyImpactCanonicalGeo(fragmentCanonicalPath(toFrag))

	scope = "unknown"
	switch {
	case fromGeo != "" && toGeo != "":
		if policyGeoOverlap(fromGeo, toGeo) {
			scope = "domestic"
		} else {
			scope = "cross_border"
		}
	}

	switch scope {
	case "domestic":
		switch channel {
		case "regulatory", "economic", "operational":
			spillover = "direct_domestic"
		case "informational":
			spillover = "domestic_informational"
		case "geopolitical":
			spillover = "domestic_geopolitical"
		default:
			spillover = "domestic_other"
		}
	case "cross_border":
		switch channel {
		case "economic":
			spillover = "cross_border_economic"
		case "informational":
			spillover = "cross_border_informational"
		case "regulatory":
			spillover = "cross_border_regulatory"
		case "geopolitical":
			spillover = "cross_border_geopolitical"
		case "operational":
			spillover = "cross_border_operational"
		default:
			spillover = "cross_border_other"
		}
	default:
		spillover = "unknown"
	}
	return channel, scope, spillover, fromGeo, toGeo
}

func classifySignalChannel(axis string, edgeType horizonmodels.DependencyType) string {
	axis = strings.TrimSpace(axis)
	switch axis {
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		return "regulatory"
	case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand, horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		return "economic"
	case horizonmodels.AxisOperationalContinuity:
		return "operational"
	case horizonmodels.AxisInfoUncertainty, horizonmodels.AxisInfoReputation:
		return "informational"
	case horizonmodels.AxisGeoConflict:
		return "geopolitical"
	}

	switch edgeType {
	case horizonmodels.DependencyTypeRegulates,
		horizonmodels.DependencyTypeAppliesTo,
		horizonmodels.DependencyTypeImposesCondition,
		horizonmodels.DependencyTypeControlsAccess,
		horizonmodels.DependencyTypeAdministers:
		return "regulatory"
	case horizonmodels.DependencyTypeDependsOn,
		horizonmodels.DependencyTypeSuppliesTo,
		horizonmodels.DependencyTypeConsumes,
		horizonmodels.DependencyTypeParticipatesIn,
		horizonmodels.DependencyTypeInvestsIn:
		return "economic"
	case horizonmodels.DependencyTypeInfluences,
		horizonmodels.DependencyTypePartnersWith,
		horizonmodels.DependencyTypeAssociatedWith,
		horizonmodels.DependencyTypeAbout:
		return "informational"
	case horizonmodels.DependencyTypeOperatesIn,
		horizonmodels.DependencyTypeLocatedIn,
		horizonmodels.DependencyTypeInGeoScope:
		return "geopolitical"
	case horizonmodels.DependencyTypeManages:
		return "operational"
	default:
		return "mixed"
	}
}

func fragmentCanonicalPath(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	return strings.TrimSpace(f.CanonicalPath)
}

func cloneFactorMap(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeFactorMaps(base map[string]float64, add map[string]float64, prefix string) map[string]float64 {
	if len(add) == 0 {
		return base
	}
	if base == nil {
		base = make(map[string]float64, len(add))
	}
	prefix = strings.TrimSpace(prefix)
	for k, v := range add {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if prefix != "" {
			k = prefix + k
		}
		base[k] = v
	}
	return base
}
