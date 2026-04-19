package policy

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// EdgeDirectionAutoFix describes deterministic rewrite for an invalid edge triple.
type EdgeDirectionAutoFix struct {
	NewType    ah.RelationName
	ShouldSwap bool
	Reason     string
	Confidence float64
}

// ResolveEdgeDirectionAutoFix returns deterministic auto-fix for known invalid triples.
// These rules are shared across policy checker, ingestion-time canonicalization and batch tools.
// sourcePath/targetPath are optional; when provided, they enable GOV-aware exemptions.
func ResolveEdgeDirectionAutoFix(
	edgeType ah.RelationName,
	sourceType, targetType ah.FragmentClass,
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
	case edgeType == relDominates &&
		sourceType == classGeoRegion &&
		targetType == classMarket:
		return EdgeDirectionAutoFix{
			NewType:    relInfluences,
			ShouldSwap: false,
			Reason:     "canonicalize: GEO_REGION DOMINATES MARKET -> GEO_REGION INFLUENCES MARKET",
			Confidence: 0.90,
		}, true
	case edgeType == relOwns &&
		sourceType == classOrganization &&
		targetType == classProgram:
		return EdgeDirectionAutoFix{
			NewType:    relAdministers,
			ShouldSwap: false,
			Reason:     "canonicalize: ORGANIZATION OWNS PROGRAM -> ORGANIZATION ADMINISTERS PROGRAM",
			Confidence: 0.95,
		}, true
	// GOV (/gov/...) -> MARKET with non-regulator edge: convert to REGULATES.
	case edgeType != relParticipatesIn &&
		sourceType == classOrganization &&
		targetType == classMarket &&
		!isGovRegulatorEdge(edgeType, sourcePath) &&
		strings.HasPrefix(sourcePath, "/gov/"):
		return EdgeDirectionAutoFix{
			NewType:    relRegulates,
			ShouldSwap: false,
			Reason:     "canonicalize: GOV * MARKET -> GOV REGULATES MARKET",
			Confidence: 0.95,
		}, true
	// MARKET * GOV: swap so GOV REGULATES MARKET.
	case edgeType != relParticipatesIn &&
		sourceType == classMarket &&
		targetType == classOrganization &&
		!isGovRegulatorEdge(edgeType, targetPath) &&
		strings.HasPrefix(targetPath, "/gov/"):
		return EdgeDirectionAutoFix{
			NewType:    relRegulates,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET * GOV -> GOV REGULATES MARKET",
			Confidence: 0.95,
		}, true
	// Non-GOV ORG -> MARKET: convert to PARTICIPATES_IN.
	case edgeType != relParticipatesIn &&
		sourceType == classOrganization &&
		targetType == classMarket &&
		!isGovRegulatorEdge(edgeType, sourcePath):
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ORGANIZATION * MARKET -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType != relParticipatesIn &&
		sourceType == classMarket &&
		targetType == classOrganization &&
		!isGovRegulatorEdge(edgeType, targetPath):
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET * ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	// PRODUCT <-> MARKET should use TRADED_IN, not PARTICIPATES_IN/DEPENDS_ON.
	case edgeType != relTradedIn &&
		sourceType == classProduct &&
		targetType == classMarket:
		return EdgeDirectionAutoFix{
			NewType:    relTradedIn,
			ShouldSwap: false,
			Reason:     "canonicalize: PRODUCT * MARKET -> PRODUCT TRADED_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType != relTradedIn &&
		sourceType == classMarket &&
		targetType == classProduct:
		return EdgeDirectionAutoFix{
			NewType:    relTradedIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET * PRODUCT -> PRODUCT TRADED_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == relInvestsIn &&
		sourceType == classMarket &&
		targetType == classOrganization:
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET INVESTS_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == relOperatesIn &&
		sourceType == classMarket &&
		targetType == classOrganization:
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET OPERATES_IN ORGANIZATION -> ORGANIZATION PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == relRelatedTo &&
		(sourceType == classOrganization || sourceType == classFacility || sourceType == classCommunity || sourceType == classProgram) &&
		targetType == classMarket:
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ENTITY RELATED_TO MARKET -> ENTITY PARTICIPATES_IN MARKET",
			Confidence: 0.90,
		}, true
	case edgeType == relRelatedTo &&
		sourceType == classMarket &&
		(targetType == classOrganization || targetType == classFacility || targetType == classCommunity || targetType == classProgram):
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET RELATED_TO ENTITY -> ENTITY PARTICIPATES_IN MARKET",
			Confidence: 0.90,
		}, true
	case edgeType == relProduces &&
		sourceType == classMarket &&
		targetType == classFacility:
		return EdgeDirectionAutoFix{
			NewType:    relParticipatesIn,
			ShouldSwap: true,
			Reason:     "canonicalize: MARKET PRODUCES FACILITY -> FACILITY PARTICIPATES_IN MARKET",
			Confidence: 0.95,
		}, true
	case edgeType == relLocatedIn &&
		sourceType == classOrganization &&
		targetType == classFacility:
		return EdgeDirectionAutoFix{
			NewType:    relOperatesIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ORGANIZATION LOCATED_IN FACILITY -> ORGANIZATION OPERATES_IN FACILITY",
			Confidence: 0.95,
		}, true
	case edgeType == relOwns &&
		sourceType == classGeoRegion &&
		targetType == classOrganization:
		return EdgeDirectionAutoFix{
			NewType:    relLocatedIn,
			ShouldSwap: true,
			Reason:     "canonicalize: GEO_REGION OWNS ORGANIZATION -> ORGANIZATION LOCATED_IN GEO_REGION",
			Confidence: 0.95,
		}, true
	// LOCATED_IN with GEO_REGION as source is always reversed: entity LOCATED_IN geo, not geo LOCATED_IN entity.
	case edgeType == relLocatedIn &&
		sourceType == classGeoRegion &&
		(targetType == classOrganization ||
			targetType == classFacility ||
			targetType == classCommunity ||
			targetType == classPerson ||
			targetType == classDeposit ||
			targetType == classProgram):
		return EdgeDirectionAutoFix{
			NewType:    relLocatedIn,
			ShouldSwap: true,
			Reason:     "canonicalize: GEO_REGION LOCATED_IN ENTITY -> ENTITY LOCATED_IN GEO_REGION (swap only)",
			Confidence: 0.95,
		}, true
	// AFFILIATED_WITH with COMMUNITY as target should have COMMUNITY as source: entities affiliate with communities.
	case edgeType == relAffiliatedWith &&
		(sourceType == classOrganization ||
			sourceType == classFacility ||
			sourceType == classPerson ||
			sourceType == classProgram) &&
		targetType == classCommunity:
		return EdgeDirectionAutoFix{
			NewType:    relAffiliatedWith,
			ShouldSwap: true,
			Reason:     "canonicalize: ENTITY AFFILIATED_WITH COMMUNITY -> COMMUNITY AFFILIATED_WITH ENTITY (swap only)",
			Confidence: 0.95,
		}, true
	case (edgeType == relMemberOf ||
		edgeType == relHasSubgroup ||
		edgeType == relOwns) &&
		sourceType == classGeoRegion &&
		(targetType == classOrganization ||
			targetType == classFacility ||
			targetType == classCommunity ||
			targetType == classPerson ||
			targetType == classDeposit ||
			targetType == classProgram):
		return EdgeDirectionAutoFix{
			NewType:    relLocatedIn,
			ShouldSwap: true,
			Reason:     "canonicalize: GEO_REGION social/ownership inversion -> ENTITY LOCATED_IN GEO_REGION",
			Confidence: 0.95,
		}, true
	case (edgeType == relMemberOf ||
		edgeType == relHasSubgroup ||
		edgeType == relOwns) &&
		targetType == classGeoRegion &&
		(sourceType == classOrganization ||
			sourceType == classFacility ||
			sourceType == classCommunity ||
			sourceType == classPerson ||
			sourceType == classDeposit ||
			sourceType == classProgram):
		return EdgeDirectionAutoFix{
			NewType:    relLocatedIn,
			ShouldSwap: false,
			Reason:     "canonicalize: ENTITY social/ownership edge to GEO_REGION -> ENTITY LOCATED_IN GEO_REGION",
			Confidence: 0.95,
		}, true
	case edgeType == relFounded &&
		(sourceType == classCommunity || sourceType == classOrganization) &&
		targetType == classPerson:
		return EdgeDirectionAutoFix{
			NewType:    relFounded,
			ShouldSwap: true,
			Reason:     "canonicalize: COMMUNITY/ORGANIZATION FOUNDED PERSON -> PERSON FOUNDED COMMUNITY/ORGANIZATION",
			Confidence: 0.95,
		}, true
	// ADMINISTERS: source MUST be ORGANIZATION/GEO_REGION. FACILITY cannot administer.
	// "Facility administered by org" → swap to ORG ADMINISTERS FACILITY.
	case edgeType == relAdministers &&
		sourceType == classFacility &&
		(targetType == classOrganization || targetType == classProgram || targetType == classGeoRegion):
		return EdgeDirectionAutoFix{
			NewType:    relAdministers,
			ShouldSwap: true,
			Reason:     "canonicalize: FACILITY ADMINISTERS ORG -> ORG ADMINISTERS FACILITY (swap only)",
			Confidence: 0.95,
		}, true
	// ADMINISTERS: DEPOSIT cannot administer. "Deposit administered by gov/org" → swap.
	case edgeType == relAdministers &&
		sourceType == classDeposit &&
		(targetType == classOrganization || targetType == classProgram || targetType == classGeoRegion):
		return EdgeDirectionAutoFix{
			NewType:    relAdministers,
			ShouldSwap: true,
			Reason:     "canonicalize: DEPOSIT ADMINISTERS ORG -> ORG ADMINISTERS DEPOSIT (swap only)",
			Confidence: 0.95,
		}, true
	// CONTROLS: source MUST be ORG/PERSON/COMMUNITY. PRODUCT cannot control.
	// "Product controlled by gov/org" → swap to ORG CONTROLS PRODUCT.
	case edgeType == relControls &&
		sourceType == classProduct &&
		(targetType == classOrganization || targetType == classFacility):
		return EdgeDirectionAutoFix{
			NewType:    relControls,
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
var govRegulatorEdgeTypes = map[ah.RelationName]bool{
	relRegulates:      true,
	relControlsAccess: true,
	relInfluences:     true,
	relAdministers:    true,
}

func isGovRegulatorEdge(edgeType ah.RelationName, entityPath string) bool {
	return strings.HasPrefix(entityPath, "/gov/") && govRegulatorEdgeTypes[edgeType]
}
