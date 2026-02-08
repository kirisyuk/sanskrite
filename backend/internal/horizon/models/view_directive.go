package models

// ViewDirective defines how to present contract results in the graph UI.
// It is a view-only instruction set layered on top of existing graph data.
type ViewDirective struct {
	ContractType    string          `json:"contractType"`
	HierarchyAxis   string          `json:"hierarchyAxis,omitempty"`   // "ownership" | "geo" | "market" | "org_parent" | ""
	HighlightPaths  []HighlightPath `json:"highlightPaths,omitempty"`  // canonical paths forming highlighted paths
	FilterRelTypes  []string        `json:"filterRelTypes,omitempty"`  // show only these relation types
	ExcludeRelTypes []string        `json:"excludeRelTypes,omitempty"` // hide these relation types
	GroupBy         string          `json:"groupBy,omitempty"`         // "relationType" | "fragmentType" | "geo" | ""
	FocusNodePaths  []string        `json:"focusNodePaths,omitempty"`  // canonical paths to focus/center
	Annotations     []Annotation    `json:"annotations,omitempty"`     // per-node/edge annotations
	Summary         string          `json:"summary,omitempty"`         // factual description of what is shown
	ResearchPlan    *ResearchPlan   `json:"researchPlan,omitempty"`    // what to research if data is missing
}

type HighlightPath struct {
	NodePaths []string `json:"nodePaths"`       // ordered canonical paths forming the path
	Label     string   `json:"label,omitempty"` // e.g. "ownership chain"
	Style     string   `json:"style,omitempty"` // "primary" | "secondary" | "warning"
}

type Annotation struct {
	CanonicalPath string `json:"canonicalPath"`
	Key           string `json:"key"`   // e.g. "stake_pct", "control_type"
	Value         string `json:"value"`
}

type ResearchPlan struct {
	MissingEdges []MissingEdge `json:"missingEdges,omitempty"`
	MissingNodes []string      `json:"missingNodes,omitempty"` // canonical paths to research
	Reason       string        `json:"reason,omitempty"`
}

type MissingEdge struct {
	FromPath     string `json:"fromPath"`
	ToPath       string `json:"toPath"`
	RelationType string `json:"relationType"`
}
