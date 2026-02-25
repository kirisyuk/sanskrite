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

	// Event types from code-defined contract
	eventTypes := eventTypeAllowlistForPrompt()
	sb.WriteString("EVENT TYPES (eventType must be one of):\n")
	sb.WriteString("  " + strings.Join(eventTypes, ", ") + "\n\n")

	// Sphere paths from code-defined taxonomy
	spherePaths := spherePathsForPrompt()
	sb.WriteString("SPHERE PATHS (spherePaths must be canonical /sphere/... paths):\n")
	sb.WriteString("  Examples: " + strings.Join(spherePaths, ", ") + "\n\n")

	// Canonical path rules for entities (reuse from core)
	sb.WriteString(CanonicalPathRulesPrompt())

	return sb.String()
}

func eventTypeAllowlistForPrompt() []string {
	out := make([]string, 0, len(builtInAllowedEventTypes))
	for k := range builtInAllowedEventTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func spherePathsForPrompt() []string {
	out := AllSpherePaths()
	if len(out) > 20 {
		return out[:20]
	}
	return out
}
