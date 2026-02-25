package policy

import (
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// EdgeDirectionAutoFix describes deterministic rewrite for an invalid edge triple.
type EdgeDirectionAutoFix struct {
	NewType    horizonmodels.DependencyType
	ShouldSwap bool
	Reason     string
	Confidence float64
}

// ResolveEdgeDirectionAutoFix returns deterministic auto-fix for known invalid triples.
// These rules are shared across policy checker, ingestion-time canonicalization and batch tools.
// sourcePath/targetPath are optional; when provided, they enable GOV-aware exemptions.
func ResolveEdgeDirectionAutoFix(
	edgeType horizonmodels.DependencyType,
	sourceType, targetType horizonmodels.FragmentType,
	paths ...string,
) (EdgeDirectionAutoFix, bool) {
	sourcePath := ""
	targetPath := ""
	if len(paths) > 0 {
		sourcePath = paths[0]
	}
	if len(paths) > 1 {
		targetPath = paths[1]
	}
	_ = targetPath
	switch {
	case edgeType == horizonmodels.DependencyTypeDominates &&
		sourceType == horizonmodels.FragmentTypeGeoRegion &&
		targetType == horizonmodels.FragmentTypeMarket:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeInfluences,
			ShouldSwap: false,
			Reason:     "canonicalize: GEO_REGION DOMINATES MARKET -> GEO_REGION INFLUENCES MARKET",
			Confidence: 0.90,
		}, true
	case edgeType == horizonmodels.DependencyTypeOwns &&
		sourceType == horizonmodels.FragmentTypeOrganization &&
		targetType == horizonmodels.FragmentTypeProgram:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeAdministers,
			ShouldSwap: false,
			Reason:     "canonicalize: ORGANIZATION OWNS PROGRAM -> ORGANIZATION ADMINISTERS PROGRAM",
			Confidence: 0.95,
		}, true
	case edgeType != horizonmodels.DependencyTypeParticipatesIn &&
		sourceType == horizonmodels.FragmentTypeOrganization &&
		targetType == horizonmodels.FragmentTypeMarket &&
		!isGovRegulatorEdge(edgeType, sourcePath):
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ORGANIZATION * MARKET -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType != horizonmodels.DependencyTypeParticipatesIn &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeOrganization &&
		!isGovRegulatorEdge(edgeType, targetPath):
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET * ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeInvestsIn &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeOperatesIn &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET OPERATES_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeRelatedTo &&
		(sourceType == horizonmodels.FragmentTypeOrganization || sourceType == horizonmodels.FragmentTypeFacility || sourceType == horizonmodels.FragmentTypeCommunity || sourceType == horizonmodels.FragmentTypeProgram) &&
		targetType == horizonmodels.FragmentTypeMarket:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ENTITY RELATED_TO MARKET -> ENTITY PARTICIPATES_IN MARKET",
			Confidence: 0.90,
		}, true
	case edgeType == horizonmodels.DependencyTypeRelatedTo &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		(targetType == horizonmodels.FragmentTypeOrganization || targetType == horizonmodels.FragmentTypeFacility || targetType == horizonmodels.FragmentTypeCommunity || targetType == horizonmodels.FragmentTypeProgram):
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET RELATED_TO ENTITY -> ENTITY PARTICIPATES_IN MARKET",
			Confidence: 0.90,
		}, true
	case edgeType == horizonmodels.DependencyTypeProduces &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeFacility:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET PRODUCES FACILITY -> FACILITY PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeLocatedIn &&
		sourceType == horizonmodels.FragmentTypeOrganization &&
		targetType == horizonmodels.FragmentTypeFacility:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeOperatesIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ORGANIZATION LOCATED_IN FACILITY -> ORGANIZATION OPERATES_IN FACILITY",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeOwns &&
		sourceType == horizonmodels.FragmentTypeGeoRegion &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeLocatedIn,
			ShouldSwap: true,
			Reason:     "canonicalize: GEO_REGION OWNS ORGANIZATION -> ORGANIZATION LOCATED_IN GEO_REGION",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeOwns &&
		sourceType == horizonmodels.FragmentTypeCommunity &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeControls,
			ShouldSwap: false,
			Reason:     "canonicalize: COMMUNITY OWNS ORGANIZATION -> COMMUNITY CONTROLS ORGANIZATION",
			Confidence: 0.95,
		}, true
	case edgeType == horizonmodels.DependencyTypeFounded &&
		(sourceType == horizonmodels.FragmentTypeCommunity || sourceType == horizonmodels.FragmentTypeOrganization) &&
		targetType == horizonmodels.FragmentTypePerson:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeFounded,
			ShouldSwap: true,
			Reason:     "canonicalize: COMMUNITY/ORGANIZATION FOUNDED PERSON -> PERSON FOUNDED COMMUNITY/ORGANIZATION",
			Confidence: 0.95,
		}, true
	// ADMINISTERS: source MUST be ORGANIZATION/GEO_REGION. FACILITY cannot administer.
	// "Facility administered by org" → swap to ORG ADMINISTERS FACILITY.
	case edgeType == horizonmodels.DependencyTypeAdministers &&
		sourceType == horizonmodels.FragmentTypeFacility &&
		(targetType == horizonmodels.FragmentTypeOrganization || targetType == horizonmodels.FragmentTypeProgram || targetType == horizonmodels.FragmentTypeGeoRegion):
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeAdministers,
			ShouldSwap: true,
			Reason:     "canonicalize: FACILITY ADMINISTERS ORG -> ORG ADMINISTERS FACILITY (swap only)",
			Confidence: 0.95,
		}, true
	// ADMINISTERS: DEPOSIT cannot administer. "Deposit administered by gov/org" → swap.
	case edgeType == horizonmodels.DependencyTypeAdministers &&
		sourceType == horizonmodels.FragmentTypeDeposit &&
		(targetType == horizonmodels.FragmentTypeOrganization || targetType == horizonmodels.FragmentTypeProgram || targetType == horizonmodels.FragmentTypeGeoRegion):
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeAdministers,
			ShouldSwap: true,
			Reason:     "canonicalize: DEPOSIT ADMINISTERS ORG -> ORG ADMINISTERS DEPOSIT (swap only)",
			Confidence: 0.95,
		}, true
	// CONTROLS: source MUST be ORG/PERSON/COMMUNITY. PRODUCT cannot control.
	// "Product controlled by gov/org" → swap to ORG CONTROLS PRODUCT.
	case edgeType == horizonmodels.DependencyTypeControls &&
		sourceType == horizonmodels.FragmentTypeProduct &&
		(targetType == horizonmodels.FragmentTypeOrganization || targetType == horizonmodels.FragmentTypeFacility):
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeControls,
			ShouldSwap: true,
			Reason:     "canonicalize: PRODUCT CONTROLS ORG -> ORG CONTROLS PRODUCT (swap only)",
			Confidence: 0.95,
		}, true
	default:
		return EdgeDirectionAutoFix{}, false
	}
}

// govRegulatorEdgeTypes are edge types that GOV entities legitimately use
// towards MARKET targets. These must NOT be rewritten to PARTICIPATES_IN.
var govRegulatorEdgeTypes = map[horizonmodels.DependencyType]bool{
	horizonmodels.DependencyTypeRegulates:      true,
	horizonmodels.DependencyTypeControlsAccess: true,
	horizonmodels.DependencyTypeInfluences:     true,
	horizonmodels.DependencyTypeAdministers:    true,
}

func isGovRegulatorEdge(edgeType horizonmodels.DependencyType, entityPath string) bool {
	return strings.HasPrefix(entityPath, "/gov/") && govRegulatorEdgeTypes[edgeType]
}
