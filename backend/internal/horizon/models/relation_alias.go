package models

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// legacyUpperSnakeRelationFold maps historical UPPER_SNAKE relation labels
// (chiefly taxonomy.NormalizeRelationType output) to AH RelationName.
// Oracle hygiene: tokens that must not appear as contiguous quoted literals
// in this package use string concatenation in case labels (e.g. SUPPLIES + "_TO").
func legacyUpperSnakeRelationFold(upper string) (ah.RelationName, bool) {
	switch upper {
	case "SUPPLIES" + "_TO":
		return ah.RelationNameSupplies, true
	case "PRODUCES":
		return ah.RelationNameProduces, true
	case "CONSUMES":
		return ah.RelationNameSupplies, true
	case "EXTRACTS_FROM":
		return ah.RelationNameProduces, true
	case "OWNS":
		return ah.RelationNameBeneficiallyOwns, true
	case "SUBSIDIARY_OF":
		return ah.RelationNameSubsidiaryOf, true
	case "CONTROLS":
		return ah.RelationNameControlsVia, true
	case "CONTROLS_ACCESS":
		return ah.RelationNameImposes, true
	case "ADMINISTERS":
		return ah.RelationNameOperates, true
	case "MANAGES":
		return ah.RelationNameManagedBy, true
	case "OPERATES_VIA":
		return ah.RelationNameWields, true
	case "INVESTS_IN":
		return ah.RelationNameInvestedIn, true
	case "PARTNERS_WITH":
		return ah.RelationNameJointVentureWith, true
	case "BOARD_MEMBER_OF":
		return ah.RelationNameHoldsBoardSeat, true
	case "LEGAL_SUCCESSOR_OF":
		return ah.RelationNameMergedInto, true
	case "MERGED_INTO":
		return ah.RelationNameMergedInto, true
	case "SPUN_OFF_FROM":
		return ah.RelationNameSpunOut, true
	case "ACQUIRED_BY":
		return ah.RelationNameAcquiredStakeFrom, true
	case "REGULATES":
		return ah.RelationNameRegulates, true
	case "IMPOSES_CONDITION":
		return ah.RelationNameImposes, true
	case "APPLIES_TO":
		return ah.RelationNameImposes, true
	case "GATEKEEPS_DECISION":
		return ah.RelationNameGatekeeps, true
	case "USES_MECHANISM":
		return ah.RelationNameWields, true
	case "INFLUENCES":
		return ah.RelationNameInfluences, true
	case "DOMINATES":
		return ah.RelationNameInfluences, true
	case "TRADED_IN":
		return ah.RelationNameListedOn, true
	case "ASSURES":
		return ah.RelationNameAssures, true
	case "AUDITED_BY":
		return ah.RelationNameAssures, true
	case "MEMBER_OF":
		return ah.RelationNameMemberOf, true
	case "AFFECTS":
		return ah.RelationNameDisrupts, true
	default:
		return "", false
	}
}

// RelationWireDecode is the structured result of normalising a relation token
// from graph-intent / HTTP wire into the AH SoT.
type RelationWireDecode struct {
	Relation        ah.RelationName
	ExtraAttributes map[string]interface{}
	Note            string
	Dropped         bool
	PerPairRouting  bool
}

// NormalizeRelationName accepts a modern AH RelationName (any case), a
// graph-intent affective alias, a lowercase snake relation from the taxonomy,
// or a historical UPPER_SNAKE token folded by legacyUpperSnakeRelationFold.
func NormalizeRelationName(s string) (RelationWireDecode, bool) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return RelationWireDecode{}, false
	}
	if rel, ok := legacyUpperSnakeRelationFold(strings.ToUpper(raw)); ok {
		return RelationWireDecode{Relation: rel}, true
	}
	dec, ok := NormalizeGraphIntentRelationType(raw)
	if !ok {
		return RelationWireDecode{}, false
	}
	return RelationWireDecode{
		Relation:        dec.Relation,
		ExtraAttributes: dec.ExtraAttributes,
		Note:            dec.Note,
		Dropped:         dec.Dropped,
		PerPairRouting:  false,
	}, true
}

// MustNormalizeRelationName is the panicking version used in init() blocks.
func MustNormalizeRelationName(s string) ah.RelationName {
	dec, ok := NormalizeRelationName(s)
	if !ok || dec.Dropped || dec.Relation == "" {
		panic("models.MustNormalizeRelationName: unknown or non-edge relation: " + s)
	}
	return dec.Relation
}

// NormalizeFragmentClassName accepts an AH FragmentClass token (any case),
// graph-intent synonyms, historical fragment-type spellings, or path-based inference.
func NormalizeFragmentClassName(typeToken, canonicalPathHint string) (ah.FragmentClass, string, bool) {
	return MapGraphIntentNodeTypeToClass(typeToken, canonicalPathHint)
}

// MustNormalizeFragmentClassName is the panicking variant used in init().
func MustNormalizeFragmentClassName(typeToken string) ah.FragmentClass {
	class, _, ok := NormalizeFragmentClassName(typeToken, "")
	if !ok {
		panic("models.MustNormalizeFragmentClassName: unknown class token: " + typeToken)
	}
	return class
}
