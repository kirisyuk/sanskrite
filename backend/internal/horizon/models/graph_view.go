package models

import "strings"

const (
	GraphViewHierarchical = "hierarchical"
	GraphViewSupplyFlow   = "supply-flow"
	GraphViewInfluenceNet = "influence-network"
	GraphViewValueChain   = "value-chain"
)

// GraphViewForDirective derives the preferred graph projection for a directive.
// It keeps contract/view defaults centralized so backend emitters and fallback
// snapshot adapters stay aligned.
func GraphViewForDirective(contractType, hierarchyAxis string) string {
	contractType = strings.TrimSpace(strings.ToLower(contractType))
	hierarchyAxis = strings.TrimSpace(strings.ToLower(hierarchyAxis))

	switch contractType {
	case "ownership-structure":
		return GraphViewHierarchical
	case "control-map":
		return GraphViewInfluenceNet
	case "commonality-analysis":
		return GraphViewInfluenceNet
	}

	switch hierarchyAxis {
	case "ownership", "org_parent":
		return GraphViewHierarchical
	case "geo":
		return GraphViewInfluenceNet
	case "market":
		return GraphViewValueChain
	}

	switch {
	case strings.Contains(contractType, "supply"):
		return GraphViewSupplyFlow
	case strings.Contains(contractType, "influence"), strings.Contains(contractType, "stakeholder"):
		return GraphViewInfluenceNet
	case strings.Contains(contractType, "value"), strings.Contains(contractType, "margin"):
		return GraphViewValueChain
	default:
		return GraphViewHierarchical
	}
}
