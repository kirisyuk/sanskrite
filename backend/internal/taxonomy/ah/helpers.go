package ah

// Hand-written helpers over the generated taxonomy SoT in
// taxonomy_generated.go. Kept in a separate non-generated file so the
// codegen step never overwrites them.
//
// Anything in this file MUST be derivable purely from the generated
// data (FragmentClasses, RelationNames, RelationAllowedLayers, …) so
// that adding a new fragment class or relation in the SoT is enough —
// no manual edits here are required for the new value to be recognised.

import "strings"

// -----------------------------------------------------------------------------
// FragmentClass helpers
// -----------------------------------------------------------------------------

// fragmentClassSet is a O(1) membership check populated lazily from the
// generated FragmentClasses slice. The init runs at package load time so
// callers never see a partial state.
var fragmentClassSet = func() map[FragmentClass]struct{} {
	out := make(map[FragmentClass]struct{}, len(FragmentClasses))
	for _, c := range FragmentClasses {
		out[c] = struct{}{}
	}
	return out
}()

// IsKnownFragmentClass reports whether v matches one of the canonical
// AgentHorde fragment classes (case-sensitive snake_case).
func IsKnownFragmentClass(v FragmentClass) bool {
	if v == "" {
		return false
	}
	_, ok := fragmentClassSet[v]
	return ok
}

// IsStructuralFragmentClass reports whether the class participates in
// structural relations (org/site/product/...) as opposed to the decision
// layer (decision/mechanism).
func IsStructuralFragmentClass(v FragmentClass) bool {
	_, ok := StructuralFragmentClasses[v]
	return ok
}

// IsDecisionLayerFragmentClass reports whether the class lives in the
// decision/governance layer (decision, mechanism).
func IsDecisionLayerFragmentClass(v FragmentClass) bool {
	_, ok := DecisionLayerFragmentClasses[v]
	return ok
}

// NormalizeFragmentClass trims, lowercases, and validates the input.
// Returns the canonical FragmentClass plus an `ok` flag; ok=false means
// the value is unknown (caller decides whether to drop, error, or
// quarantine).
func NormalizeFragmentClass(raw string) (FragmentClass, bool) {
	v := FragmentClass(strings.ToLower(strings.TrimSpace(raw)))
	if v == "" {
		return "", false
	}
	if _, ok := fragmentClassSet[v]; !ok {
		return v, false
	}
	return v, true
}

// -----------------------------------------------------------------------------
// RelationName helpers
// -----------------------------------------------------------------------------

var relationNameSet = func() map[RelationName]struct{} {
	out := make(map[RelationName]struct{}, len(RelationNames))
	for _, r := range RelationNames {
		out[r] = struct{}{}
	}
	return out
}()

// IsKnownRelation reports whether v matches one of the canonical
// AgentHorde relations (case-sensitive snake_case).
func IsKnownRelation(v RelationName) bool {
	if v == "" {
		return false
	}
	_, ok := relationNameSet[v]
	return ok
}

// NormalizeRelation trims, lowercases, and validates the input. Returns
// the canonical RelationName plus an ok flag.
func NormalizeRelation(raw string) (RelationName, bool) {
	v := RelationName(strings.ToLower(strings.TrimSpace(raw)))
	if v == "" {
		return "", false
	}
	if _, ok := relationNameSet[v]; !ok {
		return v, false
	}
	return v, true
}

// RelationLayers returns the layer set for a relation, or nil when the
// relation is unknown.
func RelationLayers(v RelationName) []Layer {
	if layers, ok := RelationAllowedLayers[v]; ok {
		return layers
	}
	return nil
}

// RelationAllowedOnLayer reports whether a relation is allowed on the
// given layer per the AH allowed-layers table.
func RelationAllowedOnLayer(rel RelationName, layer Layer) bool {
	for _, l := range RelationAllowedLayers[rel] {
		if l == layer {
			return true
		}
	}
	return false
}

// RelationSection returns the layer-section bucket for a relation
// (decision_layer, ownership, flow_topology, …) or "" when unknown.
func RelationSection(v RelationName) LayerSection {
	if s, ok := RelationLayerSection[v]; ok {
		return s
	}
	return ""
}

// EndpointKindRowsFor returns the canonical (src_class, dst_class) rows
// allowed for the relation, or nil when the relation has no closed
// endpoint catalog (open-ended relations such as influences with attr-
// driven semantics).
func EndpointKindRowsFor(v RelationName) []EndpointKindRow {
	return RelationEndpointKinds[v]
}

// EndpointKindAllowed reports whether (from, to) is a valid endpoint
// pair for the relation per the AH endpoint catalog. When the relation
// has no rows, returns true (open-ended). When the pair is in the
// blacklist, returns false even if no positive rows exist.
func EndpointKindAllowed(rel RelationName, from, to FragmentClass) bool {
	for _, b := range RelationEndpointBlacklist[rel] {
		if b.From == from && b.To == to {
			return false
		}
	}
	rows := RelationEndpointKinds[rel]
	if len(rows) == 0 {
		return true
	}
	for _, r := range rows {
		if r.From == from && r.To == to {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Attribute spec helpers
// -----------------------------------------------------------------------------

// attributeSpecByName indexes AttributeSpecs by Name for O(1) lookups.
var attributeSpecByName = func() map[string]AttributeSpec {
	out := make(map[string]AttributeSpec, len(AttributeSpecs))
	for _, s := range AttributeSpecs {
		out[s.Name] = s
	}
	return out
}()

// AttributeSpecFor returns the spec for an attribute key, or nil when
// the key is not declared in the SoT.
func AttributeSpecFor(key string) *AttributeSpec {
	if spec, ok := attributeSpecByName[key]; ok {
		return &spec
	}
	return nil
}
