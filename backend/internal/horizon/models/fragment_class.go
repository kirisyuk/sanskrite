package models

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// fragment_class.go — model-level helpers around the ah.FragmentClass
// taxonomy. The taxonomy itself (FragmentClasses, structural vs decision-
// layer split, IsKnownFragmentClass, …) lives in `internal/taxonomy/ah`;
// this file adds:
//
//   - the LEGACY → AH mapping every model touchpoint needs while we run
//     mixed-shape Firestore data (UnmarshalJSON shims, migrate-cli, scout
//     adapters, FE compatibility logs, …);
//   - subtype helpers for the closed-enum subtype slots (community,
//     decision, mechanism, evidence, mobile_asset, product) so callers
//     stop poking strings literally;
//   - tiny ergonomic conveniences (FormatClassSubtype, ClassSupportsSubtype).
//
// What is NOT here:
//   - struct definitions for HorizonFragment / Fragment fields — those
//     land in fragment.go (P1.04).
//   - taxonomy data — that is generated into internal/taxonomy/ah.

// -----------------------------------------------------------------------------
// Legacy FragmentType → ah.FragmentClass mapping
// -----------------------------------------------------------------------------

// LegacyFragmentDisposition mirrors the disposition values used in
// docs/taxonomy/migration_map.yaml. Every legacy FragmentType value falls
// into exactly one of these buckets.
type LegacyFragmentDisposition string

const (
	// LegacyDispositionRename — the legacy value renames 1:1 to a new
	// FragmentClass. Subtype is empty.
	LegacyDispositionRename LegacyFragmentDisposition = "rename"
	// LegacyDispositionSubtype — the legacy value collapses into an
	// existing class with a non-empty Subtype.
	LegacyDispositionSubtype LegacyFragmentDisposition = "subtype"
	// LegacyDispositionFoldToAttributes — the legacy value disappears as
	// a fragment class entirely; its identity survives only on attributes
	// of *other* fragments. Migrate-cli deletes the original docs.
	LegacyDispositionFoldToAttributes LegacyFragmentDisposition = "fold_to_attributes"
	// LegacyDispositionRewriteEdge — the legacy value is an edge-as-
	// fragment artifact (SUPPLY_LINK), rewritten by migrate-cli as one
	// or more edges and then deleted.
	LegacyDispositionRewriteEdge LegacyFragmentDisposition = "rewrite_edge"
)

// LegacyFragmentAlias describes how a legacy FragmentType (UPPER_SNAKE
// string from the old common.go enum) lands on the AH taxonomy.
//
// For Disposition=Rename / Subtype, Class is non-empty.
// For Disposition=FoldToAttributes / RewriteEdge, Class is empty and the
// caller MUST treat the document as something to be migrated away rather
// than reshaped in place.
type LegacyFragmentAlias struct {
	LegacyType  string
	Class       ah.FragmentClass
	Subtype     string
	Disposition LegacyFragmentDisposition
	Notes       string
}

// LegacyFragmentTypeAliases is the canonical lookup for the migration
// shim. Source of truth: docs/taxonomy/migration_map.yaml. Keep this map
// in sync with that file — there is a CLI check (cmd/taxonomy_migrate
// validate) that fails if any legacy enum value is missing here.
var LegacyFragmentTypeAliases = map[string]LegacyFragmentAlias{
	"ORGANIZATION":   {LegacyType: "ORGANIZATION", Class: ah.FragmentClassOrg, Disposition: LegacyDispositionRename},
	"PROGRAM":        {LegacyType: "PROGRAM", Class: ah.FragmentClassOrg, Subtype: "program", Disposition: LegacyDispositionSubtype},
	"PERSON":         {LegacyType: "PERSON", Class: ah.FragmentClassPerson, Disposition: LegacyDispositionRename},
	"FACILITY":       {LegacyType: "FACILITY", Class: ah.FragmentClassSite, Disposition: LegacyDispositionRename},
	"DEPOSIT":        {LegacyType: "DEPOSIT", Class: ah.FragmentClassSite, Subtype: "deposit", Disposition: LegacyDispositionSubtype},
	"GEO_REGION":     {LegacyType: "GEO_REGION", Class: ah.FragmentClassGeo, Disposition: LegacyDispositionRename},
	"PRODUCT":        {LegacyType: "PRODUCT", Class: ah.FragmentClassProduct, Disposition: LegacyDispositionRename},
	"MARKET":         {LegacyType: "MARKET", Class: ah.FragmentClassMarket, Disposition: LegacyDispositionRename},
	"COMMUNITY":      {LegacyType: "COMMUNITY", Class: ah.FragmentClassCommunity, Disposition: LegacyDispositionRename},
	"MEDIA_CHANNEL":  {LegacyType: "MEDIA_CHANNEL", Class: ah.FragmentClassOrg, Subtype: "media_channel", Disposition: LegacyDispositionSubtype},
	"TRACK":          {LegacyType: "TRACK", Class: ah.FragmentClassMobileAsset, Disposition: LegacyDispositionRename, Notes: "mobile_asset_kind required attribute"},
	"EVENT":          {LegacyType: "EVENT", Class: ah.FragmentClassEvidence, Subtype: string(ah.EvidenceSubtypeEntity), Disposition: LegacyDispositionSubtype, Notes: "Subtype carries EvidenceSubtype enum value (event-style entries land as 'entity' or 'press_release' depending on shape)"},
	"AFFECTED_ZONE":  {LegacyType: "AFFECTED_ZONE", Class: ah.FragmentClassGeo, Subtype: "affected_zone", Disposition: LegacyDispositionSubtype},
	"DECISION_POINT": {LegacyType: "DECISION_POINT", Class: ah.FragmentClassDecision, Disposition: LegacyDispositionRename, Notes: "decision_subtype attribute required"},
	"MECHANISM":      {LegacyType: "MECHANISM", Class: ah.FragmentClassMechanism, Disposition: LegacyDispositionRename, Notes: "mechanism_category required"},
	"DOMAIN":         {LegacyType: "DOMAIN", Disposition: LegacyDispositionFoldToAttributes, Notes: "Identity survives only as attributes.domain on other fragments"},
	"SPHERE":         {LegacyType: "SPHERE", Disposition: LegacyDispositionFoldToAttributes, Notes: "Folded into HorizonFragment.spherePaths attribute"},
	"SUPPLY_LINK":    {LegacyType: "SUPPLY_LINK", Disposition: LegacyDispositionRewriteEdge, Notes: "Rewritten as supplies + leg_connects edges, original doc deleted"},
}

// LookupLegacyFragmentType returns the alias for a legacy FragmentType
// string (case-insensitive in the input). ok=false when the value is not
// a known legacy type.
func LookupLegacyFragmentType(legacy string) (LegacyFragmentAlias, bool) {
	key := strings.ToUpper(strings.TrimSpace(legacy))
	if key == "" {
		return LegacyFragmentAlias{}, false
	}
	alias, ok := LegacyFragmentTypeAliases[key]
	return alias, ok
}

// FragmentClassFromLegacyType is a convenience wrapper: returns
// (Class, Subtype, ok) for callers that don't care about the disposition
// detail. ok=false when the legacy value is unknown OR maps to a
// FoldToAttributes/RewriteEdge disposition (no in-place class).
func FragmentClassFromLegacyType(legacy string) (ah.FragmentClass, string, bool) {
	alias, ok := LookupLegacyFragmentType(legacy)
	if !ok {
		return "", "", false
	}
	if alias.Class == "" {
		return "", "", false
	}
	return alias.Class, alias.Subtype, true
}

// LegacyTypeForClass returns the historical UPPER_SNAKE FragmentType for
// a given (Class, Subtype) pair. Used purely for transition logging /
// telemetry parity — never for new wire formats.
//
// Returns "" when no clean inverse exists (e.g. the migration was a
// fold_to_attributes for which there is no fragment-class anchor).
func LegacyTypeForClass(class ah.FragmentClass, subtype string) string {
	subtype = strings.TrimSpace(subtype)
	for _, alias := range LegacyFragmentTypeAliases {
		if alias.Class != class {
			continue
		}
		if alias.Subtype == subtype {
			return alias.LegacyType
		}
	}
	for _, alias := range LegacyFragmentTypeAliases {
		if alias.Class == class && alias.Subtype == "" {
			return alias.LegacyType
		}
	}
	return ""
}

// -----------------------------------------------------------------------------
// Subtype helpers — closed-enum classes only
// -----------------------------------------------------------------------------

// classClosedSubtypes maps each FragmentClass to its closed subtype set.
// Classes absent from this map carry open-ended (free-form) subtypes
// (org, site, geo, project, …) — callers may store any string but the
// system will not validate it.
var classClosedSubtypes = func() map[ah.FragmentClass]map[string]struct{} {
	out := map[ah.FragmentClass]map[string]struct{}{}
	for _, v := range ah.CommunitySubtypeValues {
		ensureSubtypeSet(out, ah.FragmentClassCommunity)[string(v)] = struct{}{}
	}
	for _, v := range ah.DecisionSubtypeValues {
		ensureSubtypeSet(out, ah.FragmentClassDecision)[string(v)] = struct{}{}
	}
	for _, v := range ah.MechanismCategoryValues {
		ensureSubtypeSet(out, ah.FragmentClassMechanism)[string(v)] = struct{}{}
	}
	for _, v := range ah.EvidenceSubtypeValues {
		ensureSubtypeSet(out, ah.FragmentClassEvidence)[string(v)] = struct{}{}
	}
	for _, v := range ah.MobileAssetKindValues {
		ensureSubtypeSet(out, ah.FragmentClassMobileAsset)[string(v)] = struct{}{}
	}
	for _, v := range ah.ProductFamilyValues {
		ensureSubtypeSet(out, ah.FragmentClassProduct)[string(v)] = struct{}{}
	}
	return out
}()

func ensureSubtypeSet(m map[ah.FragmentClass]map[string]struct{}, c ah.FragmentClass) map[string]struct{} {
	if s, ok := m[c]; ok {
		return s
	}
	s := map[string]struct{}{}
	m[c] = s
	return s
}

// ClassHasClosedSubtypes reports whether the given class has a closed
// subtype enumeration (community, decision, mechanism, evidence,
// mobile_asset, product).
func ClassHasClosedSubtypes(c ah.FragmentClass) bool {
	_, ok := classClosedSubtypes[c]
	return ok
}

// ClassSupportsSubtype reports whether the given class can carry a
// subtype slot at all. Classes like /person and /vessel_class have a
// flat path schema and are NOT expected to set Subtype.
func ClassSupportsSubtype(c ah.FragmentClass) bool {
	switch c {
	case ah.FragmentClassPerson, ah.FragmentClassVesselClass:
		return false
	default:
		return true
	}
}

// IsKnownSubtype validates a subtype string against the closed set for
// the class. For classes with open subtypes, returns true for any
// non-empty input. For classes that don't support subtypes at all,
// returns true only for empty input.
func IsKnownSubtype(class ah.FragmentClass, subtype string) bool {
	subtype = strings.TrimSpace(subtype)
	if !ClassSupportsSubtype(class) {
		return subtype == ""
	}
	closed, ok := classClosedSubtypes[class]
	if !ok {
		return true
	}
	if subtype == "" {
		return false
	}
	_, found := closed[strings.ToLower(subtype)]
	return found
}

// NormalizeSubtype trims+lowercases the input and validates it for the
// class. Returns ("", true) when input is empty AND the class allows an
// empty subtype.
func NormalizeSubtype(class ah.FragmentClass, raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", !ClassHasClosedSubtypes(class)
	}
	if !ClassSupportsSubtype(class) {
		return "", false
	}
	if closed, ok := classClosedSubtypes[class]; ok {
		if _, found := closed[v]; !found {
			return v, false
		}
	}
	return v, true
}

// FormatClassSubtype joins class and subtype as "class/subtype" (or just
// "class" when subtype is empty). Useful for log lines and embedding
// vector text headers.
func FormatClassSubtype(class ah.FragmentClass, subtype string) string {
	subtype = strings.TrimSpace(subtype)
	if subtype == "" {
		return string(class)
	}
	return string(class) + "/" + subtype
}

// IsSiteSubtype reports whether the given subtype is a recognised site
// subtype. Site has no closed enum, so the well-known set is maintained
// here as the canonical list of values migrate-cli, scout, and the FE
// agree on. New site subtypes should be added here AND to the FE side.
func IsSiteSubtype(subtype string) bool {
	switch strings.ToLower(strings.TrimSpace(subtype)) {
	case "":
		return false
	case "seaport", "airport", "rail_terminal", "pipeline_terminal",
		"refinery", "smelter", "mine", "deposit", "factory",
		"power_plant", "data_center", "depot", "warehouse",
		"border_crossing", "supply_link":
		return true
	default:
		return false
	}
}

// IsOrgSubtype reports whether the given subtype is a recognised org
// subtype. Mirrors IsSiteSubtype: open enumeration but with a known
// canonical set the codebase agrees on.
func IsOrgSubtype(subtype string) bool {
	switch strings.ToLower(strings.TrimSpace(subtype)) {
	case "":
		return false
	case "corp", "gov", "audit_firm", "program", "media_channel",
		"exchange", "regulator", "ngo", "trade_body", "intl_body":
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// ClassSubtype — typed (FragmentClass, Subtype) tuple
// -----------------------------------------------------------------------------

// ClassSubtype is a typed (FragmentClass, Subtype) pair used by call-sites
// that need to distinguish, for example, "site/deposit" from "site/refinery"
// in policy tables, edge-direction allowlists, and test fixtures.
//
// This is the canonical replacement for the legacy single-string FragmentType
// enum (FACILITY, DEPOSIT, AFFECTED_ZONE, …). Callers MUST NOT introduce
// parallel string sentinels (e.g. "__legacy_deposit") — those break identity
// matching against actual persisted documents whose Class+Subtype follow
// the AH taxonomy.
type ClassSubtype struct {
	Class   ah.FragmentClass
	Subtype string
}

// CS is a tiny constructor sugar for ClassSubtype — the alphabet of
// callers building static tables benefits from compactness.
func CS(class ah.FragmentClass, subtype string) ClassSubtype {
	return ClassSubtype{Class: class, Subtype: strings.TrimSpace(subtype)}
}

// String renders the tuple as "class" or "class/subtype" via FormatClassSubtype.
func (cs ClassSubtype) String() string {
	return FormatClassSubtype(cs.Class, cs.Subtype)
}

// Matches reports whether the (Class, Subtype) tuple equals (class, subtype).
// Empty Subtype on the receiver is a wildcard ("any subtype within class").
// Empty Subtype on the argument matches only when the receiver also has empty
// Subtype.
func (cs ClassSubtype) Matches(class ah.FragmentClass, subtype string) bool {
	if cs.Class != class {
		return false
	}
	if cs.Subtype == "" {
		return true
	}
	return strings.EqualFold(cs.Subtype, strings.TrimSpace(subtype))
}

// MatchesFragment reports whether the receiver's (Class, Subtype) tuple
// matches the given fragment's class+subtype. Nil-safe.
func (cs ClassSubtype) MatchesFragment(f *HorizonFragment) bool {
	if f == nil {
		return false
	}
	return cs.Matches(f.Class, f.Subtype)
}

// Well-known ClassSubtype constants for legacy-fragment-type call-sites.
// These are the canonical replacements for the removed FragmentTypeProgram /
// FragmentTypeMediaChannel / … sentinels. New call-sites should reference
// these (or build their own with CS()) — never a "__legacy_*" string.
var (
	ClassSubtypeProgram      = ClassSubtype{Class: ah.FragmentClassOrg, Subtype: "program"}
	ClassSubtypeMediaChannel = ClassSubtype{Class: ah.FragmentClassOrg, Subtype: "media_channel"}
	ClassSubtypeDeposit      = ClassSubtype{Class: ah.FragmentClassSite, Subtype: "deposit"}
	ClassSubtypeSupplyLink   = ClassSubtype{Class: ah.FragmentClassSite, Subtype: "supply_link"}
	ClassSubtypeAffectedZone = ClassSubtype{Class: ah.FragmentClassGeo, Subtype: "affected_zone"}
)
