package repo

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Package-local AgentHorde aliases. Repo is a wire/persistence boundary
// that historically used legacy `models.DependencyType` /
// `models.FragmentType` strings. Those types were removed during the
// AH taxonomy migration; we keep the legacy concept names here as
// `ah.RelationName` / `ah.FragmentClass`-typed constants so the
// repository can keep matching legacy persisted strings without leaking
// the legacy type vocabulary.
//
// Source-of-truth mapping: docs/taxonomy/migration_map.yaml.

// Relation aliases — canonical AH where it exists, raw snake-case
// literal where the legacy alias was folded/dropped/per-pair-routed.
// Repo MUST keep accepting all of these because Firestore documents
// written before the migration may still carry them.
const (
	// Direct AH equivalents.
	relSuppliesTo        = ah.RelationNameSupplies
	relProduces          = ah.RelationNameProduces
	relOwns              = ah.RelationNameBeneficiallyOwns
	relSubsidiaryOf      = ah.RelationNameSubsidiaryOf
	relControls          = ah.RelationNameControlsVia
	relInvestsIn         = ah.RelationNameInvestedIn
	relRegulates         = ah.RelationNameRegulates
	relAssures           = ah.RelationNameAssures
	relInfluences        = ah.RelationNameInfluences
	relGatekeepsDecision = ah.RelationNameGatekeeps
	relUsesMechanism     = ah.RelationNameWields
	relImposesCondition  = ah.RelationNameImposes
	relAdministers       = ah.RelationNameOperates
	relMemberOf          = ah.RelationNameMemberOf
	relTradedIn          = ah.RelationNameListedOn
	relPartnersWith      = ah.RelationNameJointVentureWith
	relAffects           = ah.RelationNameDisrupts

	// Legacy wire-only aliases (no AH equivalent — kept as snake-case
	// literals for read-side back-compat).
	relAuditedBy        ah.RelationName = "audited_by"
	relAssemblesInto    ah.RelationName = "assembles_into"
	relUsedIn           ah.RelationName = "used_in"
	relTransformsInto   ah.RelationName = "transforms_into"
	relDependsOn        ah.RelationName = "depends_on"
	relAssociatedWith   ah.RelationName = "associated_with"
	relOperatesIn       ah.RelationName = "operates_in"
)

// Fragment class aliases — same convention. Subtypes (program, deposit,
// supply_link, media_channel, gov, …) are NOT modelled here as types;
// callers compare frag.Class plus frag.Subtype where relevant.
const (
	classOrganization = ah.FragmentClassOrg
	classFacility     = ah.FragmentClassSite
	classGeoRegion    = ah.FragmentClassGeo
	classMarket       = ah.FragmentClassMarket
	classProduct      = ah.FragmentClassProduct
	classCommunity    = ah.FragmentClassCommunity
	classPerson       = ah.FragmentClassPerson
	classMechanism    = ah.FragmentClassMechanism
	classDecision     = ah.FragmentClassDecision
	classMobileAsset  = ah.FragmentClassMobileAsset
	classEvidence     = ah.FragmentClassEvidence
)

// edgeMetricValuesAsFloats projects models.MetricValue map onto a
// flat float64 map suitable for `policy.HasOwnershipEvidence` (which
// no longer depends on the models package).
// edgeFacetString returns the edge facet recorded under
// `Qualifiers["facet"]` (replaces the removed DependencyEdge.GetFacet
// helper). Returns "" when no facet is set.
func edgeFacetString(edge *models.DependencyEdge) string {
	if edge == nil || edge.Qualifiers == nil {
		return ""
	}
	if raw, ok := edge.Qualifiers["facet"]; ok {
		if s, ok := raw.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func edgeMetricValuesAsFloats(metrics map[string]models.MetricValue) map[string]float64 {
	if len(metrics) == 0 {
		return nil
	}
	out := make(map[string]float64, len(metrics))
	for key, mv := range metrics {
		out[key] = mv.Value
	}
	return out
}
