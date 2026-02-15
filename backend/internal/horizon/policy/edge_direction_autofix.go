package policy

import horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"

// EdgeDirectionAutoFix describes deterministic rewrite for an invalid edge triple.
type EdgeDirectionAutoFix struct {
	NewType    horizonmodels.DependencyType
	ShouldSwap bool
	Reason     string
	Confidence float64
}

// ResolveEdgeDirectionAutoFix returns deterministic auto-fix for known invalid triples.
// These rules are shared across policy checker, ingestion-time canonicalization and batch tools.
func ResolveEdgeDirectionAutoFix(
	edgeType horizonmodels.DependencyType,
	sourceType, targetType horizonmodels.FragmentType,
) (EdgeDirectionAutoFix, bool) {
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
	case edgeType == horizonmodels.DependencyTypeInvestsIn &&
		sourceType == horizonmodels.FragmentTypeMarket &&
		targetType == horizonmodels.FragmentTypeOrganization:
		return EdgeDirectionAutoFix{
			NewType:    horizonmodels.DependencyTypeParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
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
	default:
		return EdgeDirectionAutoFix{}, false
	}
}
