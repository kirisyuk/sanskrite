package models

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// LegacyRelationDecode is the structured result of normalising a legacy
// wire-level relation name (UPPER_SNAKE DependencyType) into the AH SoT.
//
// This is the public contract used by edgesemantics, policy/, scout/, and
// migrate-cli to translate inbound legacy strings before any AH-typed
// validation/persistence step. It mirrors the internal
// graphIntentRelationDecode struct used by graph_intent.go but is exposed
// for cross-package consumers.
type LegacyRelationDecode struct {
	// Relation is the canonical AH relation. Empty when the legacy alias
	// is intentionally dropped per migration_map.yaml; callers MUST check
	// Dropped before treating Relation as authoritative.
	Relation ah.RelationName
	// ExtraAttributes are attribute_spec keys + values the migration_map
	// requires alongside the rewritten edge (e.g. influence_kind on a
	// derived `influences` edge).
	ExtraAttributes map[string]interface{}
	// Note is a human-readable trace of the rewrite chain.
	Note string
	// Dropped is true when the legacy alias intentionally has no edge
	// successor (drop_after_migration / fold_to_*). Callers should skip
	// the edge or queue it for review.
	Dropped bool
	// PerPairRouting is true when the rewrite depends on (src_class,
	// dst_class) and MUST be resolved at the scout layer (DEPENDS_ON,
	// PARTICIPATES_IN, CORPORATE_TRANSITION). Caller MUST inspect
	// migration_map and provide endpoint context.
	PerPairRouting bool
}

// NormalizeRelationName accepts any of (a) a modern AH RelationName
// (lower or upper case), (b) a legacy DependencyType UPPER_SNAKE name,
// (c) a graph-intent affective alias (ALIGNS_WITH/...). Returns the
// decode with Relation populated to the AH equivalent, plus ok=true
// even when Dropped=true (so callers can distinguish "not a known
// alias" from "known alias whose canonical home is no longer an edge").
//
// Use this at every wire-protocol boundary (LLM output, FE wire,
// Firestore raw read) before doing AH-typed validation.
func NormalizeRelationName(s string) (LegacyRelationDecode, bool) {
	dec, ok := NormalizeGraphIntentRelationType(s)
	if !ok {
		return LegacyRelationDecode{}, false
	}
	return LegacyRelationDecode{
		Relation:        dec.Relation,
		ExtraAttributes: dec.ExtraAttributes,
		Note:            dec.Note,
		Dropped:         dec.Dropped,
		PerPairRouting:  dec.Relation == "" && !dec.Dropped,
	}, true
}

// MustNormalizeRelationName is the panicking version used in init()
// blocks where a static legacy name is known to be a valid alias. Code
// paths handling runtime input MUST use NormalizeRelationName.
func MustNormalizeRelationName(s string) ah.RelationName {
	dec, ok := NormalizeRelationName(s)
	if !ok || dec.Dropped || dec.Relation == "" {
		panic("models.MustNormalizeRelationName: unknown or non-edge legacy relation: " + s)
	}
	return dec.Relation
}

// NormalizeFragmentClassName accepts (a) a modern AH FragmentClass
// (lower or upper case), (b) a legacy FragmentType UPPER_SNAKE name,
// (c) a graph-intent legacy synonym (GOVERNMENT_AGENCY, ...). Returns
// the canonical class plus a non-empty subtype hint when the legacy
// alias collapses into (class, subtype).
//
// canonicalPathHint is consulted ONLY when the type token is empty or
// otherwise unrecognised; pass "" when no hint is available.
func NormalizeFragmentClassName(typeToken, canonicalPathHint string) (ah.FragmentClass, string, bool) {
	return MapGraphIntentNodeTypeToClass(typeToken, canonicalPathHint)
}

// MustNormalizeFragmentClassName is the panicking variant used in init()
// blocks for static legacy class tokens.
func MustNormalizeFragmentClassName(typeToken string) ah.FragmentClass {
	class, _, ok := NormalizeFragmentClassName(typeToken, "")
	if !ok {
		panic("models.MustNormalizeFragmentClassName: unknown legacy class token: " + typeToken)
	}
	return class
}

// NormalizeWireRelationStrict is a thin convenience that returns just
// the AH RelationName (or empty) and a "valid edge" flag, suitable for
// runtime guards that only care whether a wire alias is currently an
// edge in AH.
func NormalizeWireRelationStrict(s string) (ah.RelationName, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	dec, ok := NormalizeRelationName(s)
	if !ok || dec.Dropped || dec.Relation == "" {
		return "", false
	}
	return dec.Relation, true
}
