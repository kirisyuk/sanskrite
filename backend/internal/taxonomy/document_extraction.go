package taxonomy

import (
	"sort"
	"strings"
)

// DocumentExtractionTaxonomyRules returns taxonomy-aligned rules for document extraction prompts.
// Reuses canonical rules, event types, sphere paths, and entity types from the system taxonomy.
// Ensures extracted entities/events/spheres fit the taxonomy and can be canonicalized.
func DocumentExtractionTaxonomyRules() string {
	var sb strings.Builder

	// Entity types that produce valid canonical paths (EntityCanonicalPath returns non-empty).
	// Do NOT extract Location, Document, Other as entities - they don't map to canonical paths.
	// Location: use in event.location only; Document/Other: omit.
	entityTypes := []string{
		"Organization", "Person", "Product", "Facility", "Program", "Market",
		"GovernmentProject", "SupplyChainNode",
	}
	sb.WriteString("ENTITY TYPES (extract only these; omit Location/Document/Other):\n")
	sb.WriteString("  " + strings.Join(entityTypes, ", ") + "\n\n")

	// Event types from runtime taxonomy or fallback
	eventTypes := eventTypeAllowlistForPrompt()
	sb.WriteString("EVENT TYPES (eventType must be one of):\n")
	sb.WriteString("  " + strings.Join(eventTypes, ", ") + "\n\n")

	// Sphere paths - from runtime or common examples
	spherePaths := spherePathsForPrompt()
	sb.WriteString("SPHERE PATHS (spherePaths must be canonical /sphere/... paths):\n")
	sb.WriteString("  Examples: " + strings.Join(spherePaths, ", ") + "\n\n")

	// Canonical path rules for entities (reuse from core)
	sb.WriteString(CanonicalPathRulesPrompt())

	return sb.String()
}

func eventTypeAllowlistForPrompt() []string {
	t := getRuntimeTaxonomy()
	if t != nil && len(t.AllowedEventTypes) > 0 {
		out := make([]string, 0, len(t.AllowedEventTypes))
		for k := range t.AllowedEventTypes {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	// Fallback: same as NormalizeEventType
	return []string{
		"attack", "cyberattack", "sanction", "blockade", "policy_change", "license_change",
		"partnership", "acquisition", "release", "deprecation", "pricing_change",
		"availability_change", "capability_change", "protest", "strike", "election",
		"warning", "announcement", "incident", "other",
	}
}

func spherePathsForPrompt() []string {
	t := getRuntimeTaxonomy()
	if t != nil && len(t.AllowedSpherePaths) > 0 {
		out := make([]string, 0, len(t.AllowedSpherePaths))
		for k := range t.AllowedSpherePaths {
			out = append(out, k)
		}
		sort.Strings(out)
		// Limit to ~20 examples for prompt size
		if len(out) > 20 {
			out = out[:20]
		}
		return out
	}
	return []string{
		"/sphere/geopolitics", "/sphere/social", "/sphere/cyber", "/sphere/digital",
		"/sphere/logistics", "/sphere/logistics/maritime", "/sphere/energy",
		"/sphere/finance", "/sphere/policy", "/sphere/other",
	}
}
