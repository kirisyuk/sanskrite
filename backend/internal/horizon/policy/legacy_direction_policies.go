package policy

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/edgesemantics"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// legacyEdgeDirectionPairs holds (src,dst) pairs accepted by legacy validation
// rules for relations that are NOT in the canonical AH endpoint catalog
// (ah.RelationEndpointKinds). They keep the policy layer compatible with
// historical wire payloads while the SoT migration completes.
//
// Each entry is a list of allowed (source, target) pairs. Inputs matching the
// pair (any direction) are reported as valid; reverse-only matches still
// recommend a swap. Empty entries (open-ended legacy relation) are reported
// as valid like the AH "no closed catalog" case.
var legacyEdgeDirectionPairs = map[ah.RelationName][][2]ah.FragmentClass{
	relSurfaces: {
		{classDomain, classDecisionPoint},
		{classEvidence, classDecisionPoint},
	},
	relControlsAccess: {
		{classOrganization, classMarket},
		{classOrganization, classFacility},
		{classOrganization, classDeposit},
		{classOrganization, classProduct},
	},
	relExtractsFrom: {
		{classFacility, classDeposit},
	},
	relKeyPerson: {
		{classPerson, classOrganization},
		{classPerson, classCommunity},
		{classPerson, classProgram},
	},
	relHasKeyPerson: {
		{classOrganization, classPerson},
		{classCommunity, classPerson},
		{classProgram, classPerson},
	},
	relCompetesWith: {
		{classOrganization, classOrganization},
		{classProduct, classProduct},
		{classProgram, classProgram},
	},
	relAuditedBy: {
		{classOrganization, classOrganization},
		{classFacility, classOrganization},
		{classProgram, classOrganization},
	},
	relRelatedTo: {
		{classPerson, classPerson},
		{classOrganization, classOrganization},
		{classProgram, classProgram},
	},
	relRelativeOf: {
		{classPerson, classPerson},
	},
	relDependsOn: {
		{classOrganization, classOrganization},
		{classOrganization, classFacility},
		{classOrganization, classProduct},
		{classOrganization, classProgram},
		{classFacility, classFacility},
		{classFacility, classOrganization},
		{classProduct, classProduct},
		{classProduct, classOrganization},
		{classProgram, classOrganization},
		{classProgram, classProgram},
	},
	relSuppliesTo2: {
		{classDeposit, classOrganization},
		{classDeposit, classFacility},
		{classFacility, classFacility},
		{classFacility, classOrganization},
		{classOrganization, classOrganization},
		{classOrganization, classFacility},
	},
	// PARTICIPATES_IN: PROGRAM/ORG/FACILITY/COMMUNITY → MARKET (PRODUCT explicitly excluded; use TRADED_IN).
	relParticipatesIn: {
		{classOrganization, classMarket},
		{classFacility, classMarket},
		{classCommunity, classMarket},
		{classProgram, classMarket},
	},
	// TRADED_IN (= ListedOn): historically allowed PRODUCT → MARKET as well as ORG → MARKET.
	// AH catalog only has ORG → MARKET; we add PRODUCT → MARKET here as a legacy fallback so
	// validation accepts the wire payload while canonicalization decides the final shape.
	relTradedIn: {
		{classOrganization, classMarket},
		{classProduct, classMarket},
	},
	relInvolvesActor: {
		{classDomain, classOrganization},
		{classDomain, classPerson},
		{classDomain, classCommunity},
	},
	relFocusesOn: {
		{classDomain, classMarket},
		{classDomain, classProduct},
		{classDomain, classFacility},
		{classDomain, classDeposit},
	},
	relAffiliatedWith: {
		{classCommunity, classOrganization},
		{classCommunity, classPerson},
		{classCommunity, classProgram},
	},
	relLocatedIn: {
		{classOrganization, classGeoRegion},
		{classFacility, classGeoRegion},
		{classCommunity, classGeoRegion},
		{classPerson, classGeoRegion},
		{classDeposit, classGeoRegion},
		{classProgram, classGeoRegion},
	},
	relHasMarketSegment: {
		{classMarket, classMarket},
	},
	relHasSubmarket: {
		{classMarket, classMarket},
	},
	relOperatesIn: {
		{classOrganization, classGeoRegion},
		{classOrganization, classMarket},
		{classFacility, classGeoRegion},
	},
	relAffects: {
		{classOrganization, classMarket},
		{classOrganization, classOrganization},
		{classFacility, classFacility},
		{classGeoRegion, classMarket},
		{classGeoRegion, classOrganization},
		{classGeoRegion, classFacility},
	},
	relAppliesTo: {
		{classMechanism, classOrganization},
		{classMechanism, classMarket},
		{classMechanism, classFacility},
		{classMechanism, classProduct},
	},
	// INFLUENCES legacy pairs that are not in the AH catalog yet still required
	// by historical policy (Sphere/Domain overlays were folded into evidence/decision).
	relInfluences: {
		{classPerson, classSphere},
		{classOrganization, classSphere},
		{classCommunity, classSphere},
		{classProgram, classSphere},
		{classPerson, classDomain},
		{classOrganization, classDomain},
		{classCommunity, classDomain},
		{classProgram, classDomain},
		{classMechanism, classOrganization},
		{classMechanism, classMarket},
	},
}

// legacyValidateEdgeDirection consults the local legacy direction table for
// relations that are not part of the AH catalog. It returns (valid,
// shouldReverse, rule, recognized): when recognized=false, callers should
// fall back to other validation paths (typically the AH catalog).
func legacyValidateEdgeDirection(rel ah.RelationName, src, dst ah.FragmentClass) (bool, bool, *edgesemantics.EdgeDirectionRule, bool) {
	pairs, ok := legacyEdgeDirectionPairs[rel]
	if !ok {
		return false, false, nil, false
	}
	rule := &edgesemantics.EdgeDirectionRule{
		Description: "legacy direction policy: " + string(rel),
	}
	if len(pairs) == 0 {
		return true, false, rule, true
	}
	for _, p := range pairs {
		if p[0] == src && p[1] == dst {
			return true, false, rule, true
		}
	}
	for _, p := range pairs {
		if p[0] == dst && p[1] == src {
			return false, true, rule, true
		}
	}
	return false, false, rule, true
}
