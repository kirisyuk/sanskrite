package models

import (
	"strconv"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// attributes.go — typed accessors over the generic Attributes / Qualifiers
// maps that AgentHorde-aligned fragments and edges carry under the
// canonical-control-plane (schema_version 2.0) shape.
//
// Why this exists
// ---------------
// AH treats most semantic slots (geo_slug, coordinates, lifecycle_stage,
// route_fragment_id, gated_by_decision_fragment_id, …) as *attributes* on
// a generic map rather than first-class struct fields, to keep the wire
// shape stable across taxonomy revisions. To avoid polluting every call
// site with raw `m[key].(string)` poking, we centralise:
//
//   - low-level get/set helpers that handle nil-map init, json/firestore
//     numeric quirks (float64 ↔ int), and time RFC3339 parsing;
//   - typed accessor methods on the entities that already carry the maps
//     (currently *DependencyEdge — *HorizonFragment gains its
//     Attributes field in P1.04 and accessors land in the same patch).
//
// All keys must come from ah.AttrKey* constants — never hardcode the
// string here.
//
// Setter semantics
// ----------------
// Setters initialise the bag map if it was nil, store the value as the
// JSON/Firestore-friendly primitive (string / float64 / []string / RFC3339
// timestamp), and have an empty-value variant that *removes* the key
// rather than persisting empties — matches the AH "absence == unset"
// convention.

// -----------------------------------------------------------------------------
// Bag primitives (package-internal helpers)
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// Locally-defined attribute keys (not yet promoted to AH SoT)
// -----------------------------------------------------------------------------
//
// These keys live here transitionally because they are referenced by code
// but not yet declared in agents/shared/agenthorde_common/.../taxonomy.py.
// When promoted, the corresponding ah.AttrKey* constant should replace
// these literals and the local declaration removed.

// AttrEdgeModality is the runtime modality tag on a dependency edge:
// one of "actual" | "projected" | "historical". Value is lowercase
// snake_case to match the eventual AH attribute key.
const AttrEdgeModality = "edge_modality"

// attrBagSet stores v under key, initialising the map if needed. Returns
// the (possibly newly allocated) bag so the caller can re-assign the
// struct field.
func attrBagSet(bag map[string]interface{}, key string, v interface{}) map[string]interface{} {
	if bag == nil {
		bag = make(map[string]interface{}, 1)
	}
	bag[key] = v
	return bag
}

// attrBagDelete removes the key (and returns the same bag for chaining).
// nil-bag is a no-op.
func attrBagDelete(bag map[string]interface{}, key string) map[string]interface{} {
	if bag == nil {
		return nil
	}
	delete(bag, key)
	return bag
}

// attrString returns the value at key as a string. ok=false when the key
// is missing or the value is not a string.
func attrString(bag map[string]interface{}, key string) (string, bool) {
	v, ok := bag[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// attrFloat returns the value at key as a float64, accepting JSON-decoded
// float64 as well as int / int64 / json.Number-like string-typed numbers.
func attrFloat(bag map[string]interface{}, key string) (float64, bool) {
	v, ok := bag[key]
	if !ok || v == nil {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// attrBool returns the value at key as a bool, accepting native bool and
// the strings "true"/"false" (case-insensitive).
func attrBool(bag map[string]interface{}, key string) (bool, bool) {
	v, ok := bag[key]
	if !ok || v == nil {
		return false, false
	}
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		if b, err := strconv.ParseBool(x); err == nil {
			return b, true
		}
	}
	return false, false
}

// attrStringSlice returns the value at key as []string, tolerating
// []interface{} that JSON unmarshalling produces.
func attrStringSlice(bag map[string]interface{}, key string) ([]string, bool) {
	v, ok := bag[key]
	if !ok || v == nil {
		return nil, false
	}
	switch x := v.(type) {
	case []string:
		return x, true
	case []interface{}:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

// attrTime returns the value at key as a time.Time. Supports time.Time
// directly and RFC3339-encoded strings.
func attrTime(bag map[string]interface{}, key string) (time.Time, bool) {
	v, ok := bag[key]
	if !ok || v == nil {
		return time.Time{}, false
	}
	switch x := v.(type) {
	case time.Time:
		return x, true
	case *time.Time:
		if x != nil {
			return *x, true
		}
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// -----------------------------------------------------------------------------
// DependencyEdge typed accessors
// -----------------------------------------------------------------------------
//
// Edge attribute coverage per ATTRIBUTE_SPEC (carrier=edge or both):
//   - kind                             (string, generic edge kind tag)
//   - route_fragment_id                (string)
//   - product_fragment_id              (string)
//   - vessel_class_fragment_id         (string)
//   - vessel_class_path                (string)
//   - gated_by_decision_fragment_id    (string)
//   - influenced_by_decision_fragment_id (string)
//   - influence_kind                   (string, AH InfluenceKind enum)
//   - assurance_type                   (string, AH AssuranceType enum)
//   - disruption_kind                  (string, AH DisruptionKind enum)
//   - mode                             (string, transport mode)
//   - length_km                        (float64)
//   - severity                         (float64, 0..1)
//   - status                           (string)
//   - outcome                          (string)
//   - incoterm                         (string)
//   - schema_version                   (string)
//   - period_start / period_end        (time.Time)
//   - report_url                       (string)
//   - evidence_refs                    (string slice)

// Kind returns the generic AH "kind" tag attached to the edge (e.g.
// "physical_leg", "commercial", "actor_to_market").
func (e *DependencyEdge) Kind() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyKind)
	return v
}

// SetKind sets attributes.kind; passing "" removes the key.
func (e *DependencyEdge) SetKind(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyKind)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyKind, v)
}

// RouteFragmentID returns the /route fragment this edge runs along (used
// by `supplies` legs to locate themselves on a corridor).
func (e *DependencyEdge) RouteFragmentID() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyRouteFragmentId)
	return v
}

// SetRouteFragmentID sets attributes.route_fragment_id; "" removes.
func (e *DependencyEdge) SetRouteFragmentID(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyRouteFragmentId)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyRouteFragmentId, v)
}

// ProductFragmentID returns the /product fragment this edge carries
// (e.g. for a `supplies` edge whose endpoints are sites/markets but
// whose semantics specify which product is moving).
func (e *DependencyEdge) ProductFragmentID() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyProductFragmentId)
	return v
}

// SetProductFragmentID sets attributes.product_fragment_id; "" removes.
func (e *DependencyEdge) SetProductFragmentID(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyProductFragmentId)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyProductFragmentId, v)
}

// VesselClassFragmentID returns the /vessel_class fragment this edge
// references (e.g. for `transships_at` or `passes_through` legs).
func (e *DependencyEdge) VesselClassFragmentID() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyVesselClassFragmentId)
	return v
}

// SetVesselClassFragmentID sets attributes.vessel_class_fragment_id; ""
// removes.
func (e *DependencyEdge) SetVesselClassFragmentID(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyVesselClassFragmentId)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyVesselClassFragmentId, v)
}

// VesselClassPath returns the canonical path (e.g. "/vessel_class/lng_carrier")
// of the vessel class associated with the edge.
func (e *DependencyEdge) VesselClassPath() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyVesselClassPath)
	return v
}

// SetVesselClassPath sets attributes.vessel_class_path; "" removes.
func (e *DependencyEdge) SetVesselClassPath(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyVesselClassPath)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyVesselClassPath, v)
}

// GatedByDecisionFragmentID returns the /decision fragment that gates
// this edge (an `enacted_via` / `imposes` linkage made queryable on the
// edge itself).
func (e *DependencyEdge) GatedByDecisionFragmentID() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyGatedByDecisionFragmentId)
	return v
}

// SetGatedByDecisionFragmentID sets attributes.gated_by_decision_fragment_id;
// "" removes.
func (e *DependencyEdge) SetGatedByDecisionFragmentID(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyGatedByDecisionFragmentId)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyGatedByDecisionFragmentId, v)
}

// InfluencedByDecisionFragmentID returns the /decision fragment that
// informally influences this edge (the `influences` analogue of the
// hard-gating attribute above).
func (e *DependencyEdge) InfluencedByDecisionFragmentID() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyInfluencedByDecisionFragmentId)
	return v
}

// SetInfluencedByDecisionFragmentID sets the attribute; "" removes.
func (e *DependencyEdge) SetInfluencedByDecisionFragmentID(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyInfluencedByDecisionFragmentId)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyInfluencedByDecisionFragmentId, v)
}

// InfluenceKind returns the InfluenceKind enum string for `influences`
// edges (e.g. "advisory", "lobby", "informal").
func (e *DependencyEdge) InfluenceKind() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyInfluenceKind)
	return v
}

// SetInfluenceKind sets attributes.influence_kind; "" removes.
func (e *DependencyEdge) SetInfluenceKind(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyInfluenceKind)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyInfluenceKind, v)
}

// AssuranceType returns the AssuranceType enum for `assures` edges
// (e.g. "audit", "certification", "regulatory_oversight").
func (e *DependencyEdge) AssuranceType() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyAssuranceType)
	return v
}

// SetAssuranceType sets attributes.assurance_type; "" removes.
func (e *DependencyEdge) SetAssuranceType(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyAssuranceType)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyAssuranceType, v)
}

// DisruptionKind returns the DisruptionKind enum for `disrupts` edges.
func (e *DependencyEdge) DisruptionKind() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyDisruptionKind)
	return v
}

// SetDisruptionKind sets attributes.disruption_kind; "" removes.
func (e *DependencyEdge) SetDisruptionKind(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyDisruptionKind)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyDisruptionKind, v)
}

// Mode returns the transport mode tag attached to the edge (sea, rail,
// road, pipeline, …) when relevant.
func (e *DependencyEdge) Mode() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyMode)
	return v
}

// SetMode sets attributes.mode; "" removes.
func (e *DependencyEdge) SetMode(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyMode)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyMode, v)
}

// LengthKm returns attributes.length_km, ok=false when missing.
func (e *DependencyEdge) LengthKm() (float64, bool) {
	if e == nil {
		return 0, false
	}
	return attrFloat(e.Attributes, ah.AttrKeyLengthKm)
}

// SetLengthKm sets attributes.length_km. Negative values are stored
// verbatim — caller is responsible for upstream validation.
func (e *DependencyEdge) SetLengthKm(v float64) {
	if e == nil {
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyLengthKm, v)
}

// Severity returns attributes.severity (0..1).
func (e *DependencyEdge) Severity() (float64, bool) {
	if e == nil {
		return 0, false
	}
	return attrFloat(e.Attributes, ah.AttrKeySeverity)
}

// SetSeverity sets attributes.severity.
func (e *DependencyEdge) SetSeverity(v float64) {
	if e == nil {
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeySeverity, v)
}

// Status returns attributes.status (free-form lifecycle marker).
func (e *DependencyEdge) Status() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyStatus)
	return v
}

// SetStatus sets attributes.status; "" removes.
func (e *DependencyEdge) SetStatus(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyStatus)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyStatus, v)
}

// Incoterm returns attributes.incoterm for commercial flow legs.
func (e *DependencyEdge) Incoterm() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyIncoterm)
	return v
}

// SetIncoterm sets attributes.incoterm; "" removes.
func (e *DependencyEdge) SetIncoterm(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyIncoterm)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyIncoterm, v)
}

// SchemaVersion returns the schema_version stamp ("2.0" for AH-aligned
// edges).
func (e *DependencyEdge) SchemaVersion() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeySchemaVersion)
	return v
}

// SetSchemaVersion stamps attributes.schema_version; "" removes.
func (e *DependencyEdge) SetSchemaVersion(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeySchemaVersion)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeySchemaVersion, v)
}

// EvidenceRefs returns attributes.evidence_refs (slice of evidence
// fragment IDs supporting this edge).
func (e *DependencyEdge) EvidenceRefs() []string {
	if e == nil {
		return nil
	}
	v, _ := attrStringSlice(e.Attributes, ah.AttrKeyEvidenceRefs)
	return v
}

// SetEvidenceRefs replaces attributes.evidence_refs; nil/empty removes
// the key.
func (e *DependencyEdge) SetEvidenceRefs(v []string) {
	if e == nil {
		return
	}
	if len(v) == 0 {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyEvidenceRefs)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyEvidenceRefs, v)
}

// PeriodStart returns attributes.period_start as a time.Time.
func (e *DependencyEdge) PeriodStart() (time.Time, bool) {
	if e == nil {
		return time.Time{}, false
	}
	return attrTime(e.Attributes, ah.AttrKeyPeriodStart)
}

// SetPeriodStart sets attributes.period_start; zero value removes the key.
func (e *DependencyEdge) SetPeriodStart(t time.Time) {
	if e == nil {
		return
	}
	if t.IsZero() {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyPeriodStart)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyPeriodStart, t.UTC().Format(time.RFC3339))
}

// PeriodEnd returns attributes.period_end as a time.Time.
func (e *DependencyEdge) PeriodEnd() (time.Time, bool) {
	if e == nil {
		return time.Time{}, false
	}
	return attrTime(e.Attributes, ah.AttrKeyPeriodEnd)
}

// SetPeriodEnd sets attributes.period_end; zero value removes the key.
func (e *DependencyEdge) SetPeriodEnd(t time.Time) {
	if e == nil {
		return
	}
	if t.IsZero() {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyPeriodEnd)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyPeriodEnd, t.UTC().Format(time.RFC3339))
}

// ReportURL returns attributes.report_url (typically on `assures` edges
// pointing at the audit/certification report).
func (e *DependencyEdge) ReportURL() string {
	if e == nil {
		return ""
	}
	v, _ := attrString(e.Attributes, ah.AttrKeyReportUrl)
	return v
}

// SetReportURL sets attributes.report_url; "" removes.
func (e *DependencyEdge) SetReportURL(v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Attributes = attrBagDelete(e.Attributes, ah.AttrKeyReportUrl)
		return
	}
	e.Attributes = attrBagSet(e.Attributes, ah.AttrKeyReportUrl, v)
}

// -----------------------------------------------------------------------------
// Qualifier accessors (free-form runtime/policy bag)
// -----------------------------------------------------------------------------
//
// Qualifiers is the legacy-carrier bag for things not yet promoted to a
// closed AH attribute spec, plus runtime stamps such as the materializer
// stamp set or policy bookkeeping (policyStatus, policyTrace, …).
//
// Two access patterns are exposed:
//
//   - Free-form QualifierString / SetQualifierString — for keys that are
//     genuinely runtime-only (policyStatus, policyMessage, …) and never
//     promoted to AH.
//   - Promoted-key accessors — none yet; will be added as Phase 2 promotes
//     edge concepts into the SoT.

// QualifierString returns qualifiers[key] as a string.
func (e *DependencyEdge) QualifierString(key string) (string, bool) {
	if e == nil {
		return "", false
	}
	return attrString(e.Qualifiers, key)
}

// SetQualifierString sets qualifiers[key]; "" removes.
func (e *DependencyEdge) SetQualifierString(key, v string) {
	if e == nil {
		return
	}
	if v == "" {
		e.Qualifiers = attrBagDelete(e.Qualifiers, key)
		return
	}
	e.Qualifiers = attrBagSet(e.Qualifiers, key, v)
}

// QualifierFloat returns qualifiers[key] as float64.
func (e *DependencyEdge) QualifierFloat(key string) (float64, bool) {
	if e == nil {
		return 0, false
	}
	return attrFloat(e.Qualifiers, key)
}

// SetQualifierFloat sets qualifiers[key].
func (e *DependencyEdge) SetQualifierFloat(key string, v float64) {
	if e == nil {
		return
	}
	e.Qualifiers = attrBagSet(e.Qualifiers, key, v)
}

// QualifierBool returns qualifiers[key] as bool.
func (e *DependencyEdge) QualifierBool(key string) (bool, bool) {
	if e == nil {
		return false, false
	}
	return attrBool(e.Qualifiers, key)
}

// SetQualifierBool sets qualifiers[key].
func (e *DependencyEdge) SetQualifierBool(key string, v bool) {
	if e == nil {
		return
	}
	e.Qualifiers = attrBagSet(e.Qualifiers, key, v)
}
