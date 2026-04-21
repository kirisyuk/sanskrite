package policy

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Additional package-local typed constants used by the legacy policy code that
// still reasons in legacy DependencyType / FragmentType vocabulary. These
// supplement wire_constants.go (canonical AH + wire-boundary relation keys
// listed in the task spec) and are required to keep policy rules matching
// legacy wire payloads while everything is being migrated to the AH SoT.
//
// Naming follows the legacy DependencyType/FragmentType identifier convention
// so the call-site rewrite is mechanical (DependencyTypeFoo -> relFoo,
// FragmentTypeBar -> classBar).
//
// Where a legacy concept has a direct AH RelationName/FragmentClass, we
// alias to it. Where the legacy concept has been dropped/folded per
// docs/taxonomy/migration_map.yaml, we keep a typed string with the
// historical lower_snake wire spelling so the policy can still match the
// legacy payload at the wire boundary (and route it to the appropriate
// fold/drop disposition).

// ---- additional canonical AH-backed relation aliases ----
const (
	relInfluences2 = ah.RelationNameInfluences // forward decl helper, unused
)

// ---- legacy wire-only relation aliases (no direct AH edge) ----
//
// All values are typed ah.RelationName so existing == comparisons keep
// compiling. Their string values match the legacy lower_snake wire form.
const (
	relDominates           ah.RelationName = "dominates"            // -> influences (kind=structural_dominance)
	relOperatesIn          ah.RelationName = "operates_in"          // -> attributes.operating_geos
	relConsumes            ah.RelationName = "consumes"             // -> reverse(supplies)
	relRelatedTo           ah.RelationName = "related_to"           // dropped
	relFounded             ah.RelationName = "founded"              // -> attributes.founders
	relHasSubgroup         ah.RelationName = "has_subgroup"         // -> attributes.subgroups
	relLocatedIn           ah.RelationName = "located_in"           // -> canonical_path
	relAffiliatedWith      ah.RelationName = "affiliated_with"      // dropped
	relEmployedBy          ah.RelationName = "employed_by"          // dropped
	relBoardMemberOf       ah.RelationName = "board_member_of"      // -> holds_board_seat
	relManages             ah.RelationName = "manages"              // -> reverse(managed_by)
	relKeyPerson           ah.RelationName = "key_person"           // -> attributes.key_persons
	relHasKeyPerson        ah.RelationName = "has_key_person"       // -> attributes.key_persons
	relRelativeOf          ah.RelationName = "relative_of"          // -> attributes.relations
	relAssociatedWith      ah.RelationName = "associated_with"      // dropped
	relCorporateTransition ah.RelationName = "corporate_transition" // attribute heuristic
	relExtractsFrom        ah.RelationName = "extracts_from"        // -> produces (production_kind=extraction)
	relSurfaces            ah.RelationName = "surfaces"             // dropped
	relBroadensTo          ah.RelationName = "broadens_to"          // dropped
	relNarrowsTo           ah.RelationName = "narrows_to"           // dropped
	relRelatedDomain       ah.RelationName = "related_domain"       // dropped
	relDomainRelation      ah.RelationName = "domain_relation"      // dropped
	relTransformsInto      ah.RelationName = "transforms_into"      // -> attributes.siteTransforms (kind=transform)
	relAssemblesInto       ah.RelationName = "assembles_into"       // -> attributes.siteTransforms (kind=assembly)
	relUsedIn              ah.RelationName = "used_in"              // dropped
	relAbout               ah.RelationName = "about"                // -> attributes.about_refs
	relRenamedTo           ah.RelationName = "renamed_to"           // -> attributes.name_history
	relRebrandedTo         ah.RelationName = "rebranded_to"         // -> attributes.name_history
	relLegalSuccessorOf    ah.RelationName = "legal_successor_of"   // -> reverse(merged_into)
	relMergedInto                          = ah.RelationNameMergedInto
	relSpunOffFrom         ah.RelationName = "spun_off_from"   // -> reverse(spun_out)
	relAcquiredBy          ah.RelationName = "acquired_by"     // -> reverse(acquired_stake_from)
	relTriggeredBy         ah.RelationName = "triggered_by"    // -> attributes.triggers
	relHasSubmarket        ah.RelationName = "has_submarket"   // -> attributes.submarkets
	relSuppliesTo2         ah.RelationName = "supplies_to"     // legacy SUPPLIES_TO wire spelling
	relParticipatesIn2     ah.RelationName = "participates_in" // alias for cross-file use
)

// ---- legacy fragment-class aliases ----
//
// These are typed ah.FragmentClass so existing == checks keep compiling.
// Several legacy FragmentTypes fold into the same AH class (org, geo, evidence)
// distinguished only by the closed `subtype` attribute. The package-level
// constants below map only to the AH class; subtype handling lives in the
// fragment_path_policy / canonicalizer layer.
const (
	classMediaChannel  = ah.FragmentClassOrg      // subtype=media_channel
	classAffectedZone  = ah.FragmentClassGeo      // subtype=affected_zone
	classEvent         = ah.FragmentClassEvidence // subtype=event
	classDecisionPoint = ah.FragmentClassDecision
	classSupplyLink2   = ah.FragmentClassSite // subtype=supply_link (secondary class alias)
	classRoute         = ah.FragmentClassRoute
	classRegime        = ah.FragmentClassRegime
	classProject       = ah.FragmentClassProject
	classVesselClass   = ah.FragmentClassVesselClass
)

// silence unused warnings for forward-decl helpers (kept for future migrations).
var (
	_ = relInfluences2
	_ = relSuppliesTo2
	_ = relParticipatesIn2
	_ = classSupplyLink2
)
