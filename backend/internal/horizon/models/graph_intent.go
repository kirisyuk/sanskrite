package models

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

const (
	GraphIntentVersionV1 = "graph-intent.v1"

	GraphIntentExecutionLanguageEN = "en"

	GraphIntentClassCreate    = "create"
	GraphIntentClassUpdate    = "update"
	GraphIntentClassDelete    = "delete"
	GraphIntentClassMerge     = "merge"
	GraphIntentClassTransform = "transform"
	GraphIntentClassMixed     = "mixed"
	GraphIntentClassUnknown   = "unknown"

	GraphIntentApplyModePatch = "patch"
	GraphIntentApplyModeDry   = "dry-run"
)

const (
	GraphIntentGroupingGovernanceStructure = "governance_structure"
	GraphIntentGroupingMarketMap           = "market_map"
	GraphIntentGroupingSupplyChainSkeleton = "supply_chain_skeleton"
	GraphIntentGroupingOwnershipControl    = "ownership_control"
	GraphIntentGroupingInvestmentFlow      = "investment_flow"
	GraphIntentGroupingRegulatoryLandscape = "regulatory_landscape"
	GraphIntentGroupingSocietalStructure   = "societal_structure"
)

const (
	GraphIntentRelationTypeAlignsWith    = "ALIGNS_WITH"
	GraphIntentRelationTypeOpposes       = "OPPOSES"
	GraphIntentRelationTypeCooperates    = "COOPERATES_WITH"
	GraphIntentRelationTypeConflictsWith = "CONFLICTS_WITH"
)

// GraphIntentPlanRequest is the request body for:
// POST /api/horizon/lenses/{lensId}/graph-intent/plan
type GraphIntentPlanRequest struct {
	Version           string                 `json:"version"`
	RawQuery          string                 `json:"rawQuery"`
	QueryLanguage     string                 `json:"queryLanguage,omitempty"`
	ExecutionLanguage string                 `json:"executionLanguage,omitempty"`
	Snapshot          GraphIntentSnapshot    `json:"snapshot"`
	Options           GraphIntentPlanOptions `json:"options,omitempty"`
}

type GraphIntentSnapshot struct {
	Nodes           []GraphIntentSnapshotNode `json:"nodes,omitempty"`
	Edges           []GraphIntentSnapshotEdge `json:"edges,omitempty"`
	SelectedNodeIDs []string                  `json:"selectedNodeIds,omitempty"`
	VisibleNodeIDs  []string                  `json:"visibleNodeIds,omitempty"`
	ViewState       GraphIntentViewState      `json:"viewState,omitempty"`
}

type GraphIntentViewState struct {
	Zoom    float64                `json:"zoom,omitempty"`
	Filters map[string]interface{} `json:"filters,omitempty"`
}

type GraphIntentSnapshotNode struct {
	NodeID        string   `json:"nodeId,omitempty"`
	FragmentID    string   `json:"fragmentId,omitempty"`
	CanonicalPath string   `json:"canonicalPath,omitempty"`
	Label         string   `json:"label,omitempty"`
	Type          string   `json:"type,omitempty"`
	Position      *XYPoint `json:"position,omitempty"`
}

type GraphIntentSnapshotEdge struct {
	EdgeID           string `json:"edgeId,omitempty"`
	SourceNodeID     string `json:"sourceNodeId,omitempty"`
	TargetNodeID     string `json:"targetNodeId,omitempty"`
	SourceFragmentID string `json:"sourceFragmentId,omitempty"`
	TargetFragmentID string `json:"targetFragmentId,omitempty"`
	RelationType     string `json:"relationType,omitempty"`
	Direction        string `json:"direction,omitempty"`
}

type XYPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type GraphIntentPlanOptions struct {
	AllowSearchGrounding   bool `json:"allowSearchGrounding,omitempty"`
	AllowInternalGrounding bool `json:"allowInternalGrounding,omitempty"`
	MaxNewNodes            int  `json:"maxNewNodes,omitempty"`
	MaxNewEdges            int  `json:"maxNewEdges,omitempty"`
}

// GraphIntentPlanResponse is returned by:
// POST /api/horizon/lenses/{lensId}/graph-intent/plan
type GraphIntentPlanResponse struct {
	Version                string                    `json:"version"`
	NormalizedEnglishQuery string                    `json:"normalizedEnglishQuery"`
	IntentClass            string                    `json:"intentClass"`
	Confidence             float64                   `json:"confidence"`
	Create                 GraphIntentCreateBlock    `json:"create"`
	Update                 GraphIntentUpdateBlock    `json:"update"`
	Delete                 GraphIntentDeleteBlock    `json:"delete"`
	Merge                  GraphIntentMergeBlock     `json:"merge"`
	Transform              GraphIntentTransformBlock `json:"transform"`
	Explanations           []string                  `json:"explanations,omitempty"`
	Warnings               []string                  `json:"warnings,omitempty"`
	RequiresConfirmation   bool                      `json:"requiresConfirmation"`
}

type GraphIntentCreateBlock struct {
	Groupings []GraphIntentGrouping `json:"groupings,omitempty"`
	Nodes     []GraphIntentNodeSpec `json:"nodes,omitempty"`
	Edges     []GraphIntentEdgeSpec `json:"edges,omitempty"`
}

type GraphIntentGrouping struct {
	GroupingID   string                  `json:"groupingId"`
	GroupingKind string                  `json:"groupingKind"`
	Title        string                  `json:"title"`
	Anchor       *GraphIntentAnchor      `json:"anchor,omitempty"`
	Entities     []GraphIntentNodeSpec   `json:"entities,omitempty"`
	Relations    []GraphIntentEdgeSpec   `json:"relations,omitempty"`
	Evidence     *GraphIntentEvidenceRef `json:"evidence,omitempty"`
}

type GraphIntentAnchor struct {
	CanonicalPathHint string `json:"canonicalPathHint,omitempty"`
}

type GraphIntentEvidenceRef struct {
	InternalRefs []string `json:"internalRefs,omitempty"`
	SearchRefs   []string `json:"searchRefs,omitempty"`
}

type GraphIntentNodeSpec struct {
	TempID            string                 `json:"tempId,omitempty"`
	Label             string                 `json:"label"`
	Type              string                 `json:"type"`
	CanonicalPathHint string                 `json:"canonicalPathHint,omitempty"`
	State             map[string]interface{} `json:"state,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type GraphIntentEdgeSpec struct {
	TempID       string                 `json:"tempId,omitempty"`
	FromTempID   string                 `json:"fromTempId,omitempty"`
	ToTempID     string                 `json:"toTempId,omitempty"`
	FromNodeID   string                 `json:"fromNodeId,omitempty"`
	ToNodeID     string                 `json:"toNodeId,omitempty"`
	RelationType string                 `json:"relationType"`
	Direction    string                 `json:"direction,omitempty"`
	Confidence   float64                `json:"confidence,omitempty"`
	Qualifiers   map[string]interface{} `json:"qualifiers,omitempty"`
	Metrics      map[string]interface{} `json:"metrics,omitempty"`
}

type GraphIntentUpdateBlock struct {
	NodePatches     []GraphIntentNodePatch       `json:"nodePatches,omitempty"`
	EdgePatches     []GraphIntentEdgePatch       `json:"edgePatches,omitempty"`
	RelationRewrite []GraphIntentRelationRewrite `json:"relationRewrite,omitempty"`
}

// GraphIntentDeleteBlock specifies nodes and edges to remove from the graph.
// Node deletion cascades to edges (edges referencing the node are removed).
type GraphIntentDeleteBlock struct {
	NodeIDs []string `json:"nodeIds,omitempty"`
	EdgeIDs []string `json:"edgeIds,omitempty"`
}

// GraphIntentMergeBlock merges duplicate nodes into a primary node.
// LLM selects primary; duplicates are reassigned (edges redirected) and then removed.
// Edge reassignment deduplicates: if primary already has same (source,target,type), duplicate edge is deleted.
type GraphIntentMergeBlock struct {
	PrimaryNodeID     string   `json:"primaryNodeId"`
	DuplicateNodeIDs  []string `json:"duplicateNodeIds,omitempty"`
	MergeFragmentData *bool    `json:"mergeFragmentData,omitempty"` // fill-first merge of Labels, Identifiers, State. Nil = true.
}

type GraphIntentNodePatch struct {
	PatchID       string                 `json:"patchId,omitempty"`
	NodeID        string                 `json:"nodeId,omitempty"`
	TempID        string                 `json:"tempId,omitempty"`
	FragmentID    string                 `json:"fragmentId,omitempty"`
	CanonicalPath string                 `json:"canonicalPath,omitempty"`
	Label         *string                `json:"label,omitempty"`
	Type          *string                `json:"type,omitempty"`
	StatePatch    map[string]interface{} `json:"statePatch,omitempty"`
	MetadataPatch map[string]interface{} `json:"metadataPatch,omitempty"`
}

type GraphIntentEdgePatch struct {
	PatchID          string                 `json:"patchId,omitempty"`
	EdgeID           string                 `json:"edgeId,omitempty"`
	SourceNodeID     string                 `json:"sourceNodeId,omitempty"`
	TargetNodeID     string                 `json:"targetNodeId,omitempty"`
	SourceFragmentID string                 `json:"sourceFragmentId,omitempty"`
	TargetFragmentID string                 `json:"targetFragmentId,omitempty"`
	RelationType     *string                `json:"relationType,omitempty"`
	Direction        *string                `json:"direction,omitempty"`
	Attributes       map[string]interface{} `json:"attributes,omitempty"`
}

type GraphIntentRelationRewrite struct {
	RewriteID   string                         `json:"rewriteId,omitempty"`
	Match       GraphIntentRelationMatch       `json:"match"`
	ReplaceWith GraphIntentRelationReplacement `json:"replaceWith"`
	Reason      string                         `json:"reason,omitempty"`
}

type GraphIntentRelationMatch struct {
	RelationType  string   `json:"relationType,omitempty"`
	SourceNodeIDs []string `json:"sourceNodeIds,omitempty"`
	TargetNodeIDs []string `json:"targetNodeIds,omitempty"`
}

type GraphIntentRelationReplacement struct {
	RelationType string `json:"relationType"`
	Direction    string `json:"direction,omitempty"`
}

type GraphIntentTransformBlock struct {
	DeriveChains []GraphIntentDeriveChain `json:"deriveChains,omitempty"`
	Restructure  []GraphIntentRestructure `json:"restructure,omitempty"`
}

type GraphIntentDeriveChain struct {
	ChainID       string                      `json:"chainId,omitempty"`
	FromNodeID    string                      `json:"fromNodeId,omitempty"`
	ToNodeID      string                      `json:"toNodeId,omitempty"`
	Constraints   GraphIntentChainConstraints `json:"constraints,omitempty"`
	ProposedPath  []string                    `json:"proposedPath,omitempty"`
	ProposedEdges []string                    `json:"proposedEdges,omitempty"`
	Confidence    float64                     `json:"confidence,omitempty"`
}

type GraphIntentChainConstraints struct {
	AllowedRelationTypes []string `json:"allowedRelationTypes,omitempty"`
	MaxHops              int      `json:"maxHops,omitempty"`
}

type GraphIntentRestructure struct {
	RestructureID string                 `json:"restructureId,omitempty"`
	ScopeNodeIDs  []string               `json:"scopeNodeIds,omitempty"`
	Mode          string                 `json:"mode,omitempty"`
	Params        map[string]interface{} `json:"params,omitempty"`
}

type GraphIntentApplyRequest struct {
	Version        string                  `json:"version"`
	Plan           GraphIntentPlanResponse `json:"plan"`
	ApplyMode      string                  `json:"applyMode,omitempty"`
	IdempotencyKey string                  `json:"idempotencyKey"`
}

type GraphIntentUnresolvedItem struct {
	TempID          string `json:"tempId,omitempty"`
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggestedAction,omitempty"`
}

type GraphIntentApplyResponse struct {
	Applied          bool                        `json:"applied"`
	CreatedNodeCount int                         `json:"createdNodeCount"`
	CreatedEdgeCount int                         `json:"createdEdgeCount"`
	UpdatedNodeCount int                         `json:"updatedNodeCount"`
	UpdatedEdgeCount int                         `json:"updatedEdgeCount"`
	DeletedNodeCount int                         `json:"deletedNodeCount"`
	DeletedEdgeCount int                         `json:"deletedEdgeCount"`
	UnresolvedItems  []GraphIntentUnresolvedItem `json:"unresolvedItems,omitempty"`
	Warnings         []string                    `json:"warnings,omitempty"`
	ActionPreview    *GraphIntentActionPlan      `json:"actionPreview,omitempty"`
}

type GraphIntentActionPlan struct {
	Actions []GraphIntentAction `json:"actions"`
}

type GraphIntentAction struct {
	ActionID string                 `json:"actionId"`
	Kind     string                 `json:"kind"`
	Scope    string                 `json:"scope"`
	RefID    string                 `json:"refId,omitempty"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
}

func DefaultGraphIntentPlanOptions() GraphIntentPlanOptions {
	return GraphIntentPlanOptions{
		AllowSearchGrounding:   true,
		AllowInternalGrounding: true,
		MaxNewNodes:            60,
		MaxNewEdges:            120,
	}
}

func (r *GraphIntentPlanRequest) NormalizeAndValidate() error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	r.Version = strings.TrimSpace(r.Version)
	if r.Version == "" {
		r.Version = GraphIntentVersionV1
	}
	if r.Version != GraphIntentVersionV1 {
		return fmt.Errorf("unsupported version: %s", r.Version)
	}
	r.RawQuery = strings.TrimSpace(r.RawQuery)
	if r.RawQuery == "" {
		return fmt.Errorf("rawQuery is required")
	}
	r.QueryLanguage = strings.TrimSpace(strings.ToLower(r.QueryLanguage))
	if r.QueryLanguage == "" {
		r.QueryLanguage = "auto"
	}
	r.ExecutionLanguage = strings.TrimSpace(strings.ToLower(r.ExecutionLanguage))
	if r.ExecutionLanguage == "" {
		r.ExecutionLanguage = GraphIntentExecutionLanguageEN
	}
	def := DefaultGraphIntentPlanOptions()
	if r.Options.MaxNewNodes <= 0 {
		r.Options.MaxNewNodes = def.MaxNewNodes
	}
	if r.Options.MaxNewEdges <= 0 {
		r.Options.MaxNewEdges = def.MaxNewEdges
	}
	if r.Options.MaxNewNodes > 500 {
		return fmt.Errorf("maxNewNodes exceeds hard limit: %d", r.Options.MaxNewNodes)
	}
	if r.Options.MaxNewEdges > 1000 {
		return fmt.Errorf("maxNewEdges exceeds hard limit: %d", r.Options.MaxNewEdges)
	}
	if r.Snapshot.ViewState.Zoom == 0 {
		r.Snapshot.ViewState.Zoom = 1.0
	}
	return nil
}

func (r *GraphIntentApplyRequest) NormalizeAndValidate() error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	r.Version = strings.TrimSpace(r.Version)
	if r.Version == "" {
		r.Version = GraphIntentVersionV1
	}
	if r.Version != GraphIntentVersionV1 {
		return fmt.Errorf("unsupported version: %s", r.Version)
	}
	r.ApplyMode = strings.TrimSpace(strings.ToLower(r.ApplyMode))
	if r.ApplyMode == "" {
		r.ApplyMode = GraphIntentApplyModePatch
	}
	if r.ApplyMode != GraphIntentApplyModePatch && r.ApplyMode != GraphIntentApplyModeDry {
		return fmt.Errorf("invalid applyMode: %s", r.ApplyMode)
	}
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	if r.IdempotencyKey == "" {
		return fmt.Errorf("idempotencyKey is required")
	}
	if err := r.Plan.NormalizeAndValidate(); err != nil {
		return err
	}
	return nil
}

func (p *GraphIntentPlanResponse) NormalizeAndValidate() error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}
	p.Version = strings.TrimSpace(p.Version)
	if p.Version == "" {
		p.Version = GraphIntentVersionV1
	}
	if p.Version != GraphIntentVersionV1 {
		return fmt.Errorf("unsupported plan version: %s", p.Version)
	}
	p.IntentClass = strings.TrimSpace(strings.ToLower(p.IntentClass))
	if p.IntentClass == "" {
		p.IntentClass = GraphIntentClassUnknown
	}
	switch p.IntentClass {
	case GraphIntentClassCreate, GraphIntentClassUpdate, GraphIntentClassDelete, GraphIntentClassMerge, GraphIntentClassTransform, GraphIntentClassMixed, GraphIntentClassUnknown:
	default:
		return fmt.Errorf("invalid intentClass: %s", p.IntentClass)
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if strings.TrimSpace(p.NormalizedEnglishQuery) == "" {
		return fmt.Errorf("normalizedEnglishQuery is required")
	}
	for i := range p.Create.Groupings {
		g := &p.Create.Groupings[i]
		g.GroupingKind = strings.TrimSpace(strings.ToLower(g.GroupingKind))
		if !IsGraphIntentGroupingKindAllowed(g.GroupingKind) {
			return fmt.Errorf("invalid groupingKind: %s", g.GroupingKind)
		}
		for j := range g.Entities {
			n := &g.Entities[j]
			if strings.TrimSpace(n.Label) == "" {
				return fmt.Errorf("create.groupings.entities.label is required")
			}
			if !IsGraphIntentNodeTypeAllowed(n.Type) {
				return fmt.Errorf("invalid grouping entity type: %s", n.Type)
			}
			cp := strings.TrimSpace(n.CanonicalPathHint)
			if cp == "" {
				return fmt.Errorf("create.groupings.entities.canonicalPathHint is required for %q", n.Label)
			}
			if !strings.HasPrefix(cp, "/") {
				cp = "/" + cp
			}
			if norm, changed := normalizeMechanismCanonicalPathForGraphIntent(cp); changed {
				cp = norm
				n.CanonicalPathHint = cp
			}
			if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
				return fmt.Errorf("create.groupings.entities.canonicalPathHint invalid for %q: %w", n.Label, err)
			}
		}
		for _, rel := range g.Relations {
			if !IsGraphIntentRelationTypeAllowed(rel.RelationType) {
				return fmt.Errorf("invalid grouping relationType: %s", rel.RelationType)
			}
		}
	}
	for i := range p.Create.Nodes {
		n := &p.Create.Nodes[i]
		if strings.TrimSpace(n.Label) == "" {
			return fmt.Errorf("create.nodes.label is required")
		}
		if !IsGraphIntentNodeTypeAllowed(n.Type) {
			return fmt.Errorf("invalid create.nodes.type: %s", n.Type)
		}
		cp := strings.TrimSpace(n.CanonicalPathHint)
		if cp == "" {
			return fmt.Errorf("create.nodes.canonicalPathHint is required for %q", n.Label)
		}
		if !strings.HasPrefix(cp, "/") {
			cp = "/" + cp
		}
		if norm, changed := normalizeMechanismCanonicalPathForGraphIntent(cp); changed {
			cp = norm
			n.CanonicalPathHint = cp
		}
		if err := taxonomy.ValidateCanonicalPath(cp); err != nil {
			return fmt.Errorf("create.nodes.canonicalPathHint invalid for %q: %w", n.Label, err)
		}
	}
	for _, e := range p.Create.Edges {
		if !IsGraphIntentRelationTypeAllowed(e.RelationType) {
			return fmt.Errorf("invalid create edge relationType: %s", e.RelationType)
		}
	}
	for _, ep := range p.Update.EdgePatches {
		if ep.RelationType != nil && strings.TrimSpace(*ep.RelationType) != "" && !IsGraphIntentRelationTypeAllowed(*ep.RelationType) {
			return fmt.Errorf("invalid edge patch relationType: %s", strings.TrimSpace(*ep.RelationType))
		}
	}
	for _, np := range p.Update.NodePatches {
		if np.Type != nil && strings.TrimSpace(*np.Type) != "" && !IsGraphIntentNodeTypeAllowed(*np.Type) {
			return fmt.Errorf("invalid node patch type: %s", strings.TrimSpace(*np.Type))
		}
	}
	for _, rw := range p.Update.RelationRewrite {
		if strings.TrimSpace(rw.Match.RelationType) != "" && !IsGraphIntentRelationTypeAllowed(rw.Match.RelationType) {
			return fmt.Errorf("invalid relationRewrite.match.relationType: %s", rw.Match.RelationType)
		}
		if !IsGraphIntentRelationTypeAllowed(rw.ReplaceWith.RelationType) {
			return fmt.Errorf("invalid relationRewrite.replaceWith.relationType: %s", rw.ReplaceWith.RelationType)
		}
	}
	for _, ch := range p.Transform.DeriveChains {
		for _, rt := range ch.Constraints.AllowedRelationTypes {
			if !IsGraphIntentRelationTypeAllowed(rt) {
				return fmt.Errorf("invalid deriveChains.constraints.allowedRelationTypes item: %s", rt)
			}
		}
	}
	return nil
}

func (p *GraphIntentPlanResponse) EstimatedCreateCounts() (nodes int, edges int) {
	for _, g := range p.Create.Groupings {
		nodes += len(g.Entities)
		edges += len(g.Relations)
	}
	nodes += len(p.Create.Nodes)
	edges += len(p.Create.Edges)
	return nodes, edges
}

func normalizeMechanismCanonicalPathForGraphIntent(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "mechanism" {
		return "", false
	}
	category := strings.ToLower(strings.TrimSpace(parts[1]))
	switch category {
	case "regulatory", "economic", "technological", "contractual", "political":
		return "", false
	}
	mapped := map[string]string{
		"tax":         "regulatory",
		"fiscal":      "economic",
		"monetary":    "economic",
		"sanctions":   "regulatory",
		"tariff":      "regulatory",
		"subsidy":     "economic",
		"incentive":   "economic",
		"license":     "regulatory",
		"permit":      "regulatory",
		"quota":       "regulatory",
		"embargo":     "regulatory",
		"ban":         "regulatory",
		"restriction": "regulatory",
	}
	next, ok := mapped[category]
	if !ok {
		next = "regulatory"
	}
	return "/mechanism/" + next + "/" + parts[2], true
}

func (p *GraphIntentPlanResponse) IsPotentiallyDestructive() bool {
	if len(p.Merge.PrimaryNodeID) > 0 && len(p.Merge.DuplicateNodeIDs) > 0 {
		return true
	}
	if len(p.Delete.NodeIDs) > 0 || len(p.Delete.EdgeIDs) > 0 {
		return true
	}
	if len(p.Update.RelationRewrite) == 0 {
		return false
	}
	for _, rw := range p.Update.RelationRewrite {
		if len(rw.Match.SourceNodeIDs) == 0 && len(rw.Match.TargetNodeIDs) == 0 {
			return true
		}
	}
	return false
}

func (p *GraphIntentPlanResponse) MapToActions() *GraphIntentActionPlan {
	if p == nil {
		return &GraphIntentActionPlan{Actions: []GraphIntentAction{}}
	}
	actions := make([]GraphIntentAction, 0, 64)
	nextID := 1
	push := func(kind, scope, ref string, payload map[string]interface{}) {
		actions = append(actions, GraphIntentAction{
			ActionID: fmt.Sprintf("a%d", nextID),
			Kind:     kind,
			Scope:    scope,
			RefID:    strings.TrimSpace(ref),
			Payload:  payload,
		})
		nextID++
	}

	for _, g := range p.Create.Groupings {
		push("create_grouping", "create", g.GroupingID, map[string]interface{}{
			"groupingKind": g.GroupingKind,
			"title":        g.Title,
		})
		for _, n := range g.Entities {
			ref := n.TempID
			if strings.TrimSpace(ref) == "" {
				ref = n.CanonicalPathHint
			}
			push("create_node", "create", ref, map[string]interface{}{
				"label":             n.Label,
				"type":              n.Type,
				"canonicalPathHint": n.CanonicalPathHint,
			})
		}
		for _, e := range g.Relations {
			ref := e.TempID
			if strings.TrimSpace(ref) == "" {
				ref = e.FromTempID + "->" + e.ToTempID + ":" + e.RelationType
			}
			push("create_edge", "create", ref, map[string]interface{}{
				"fromTempId":   e.FromTempID,
				"toTempId":     e.ToTempID,
				"relationType": strings.ToUpper(strings.TrimSpace(e.RelationType)),
				"direction":    strings.ToLower(strings.TrimSpace(e.Direction)),
			})
		}
	}
	for _, n := range p.Create.Nodes {
		ref := n.TempID
		if strings.TrimSpace(ref) == "" {
			ref = n.CanonicalPathHint
		}
		push("create_node", "create", ref, map[string]interface{}{
			"label":             n.Label,
			"type":              n.Type,
			"canonicalPathHint": n.CanonicalPathHint,
		})
	}
	for _, e := range p.Create.Edges {
		ref := e.TempID
		if strings.TrimSpace(ref) == "" {
			ref = e.FromTempID + "->" + e.ToTempID + ":" + e.RelationType
		}
		push("create_edge", "create", ref, map[string]interface{}{
			"fromTempId":   e.FromTempID,
			"toTempId":     e.ToTempID,
			"relationType": strings.ToUpper(strings.TrimSpace(e.RelationType)),
			"direction":    strings.ToLower(strings.TrimSpace(e.Direction)),
		})
	}

	for _, np := range p.Update.NodePatches {
		ref := firstNonEmpty(np.PatchID, np.NodeID, np.TempID, np.FragmentID, np.CanonicalPath)
		payload := map[string]interface{}{}
		if np.Label != nil {
			payload["label"] = *np.Label
		}
		if np.Type != nil {
			payload["type"] = *np.Type
		}
		if len(np.StatePatch) > 0 {
			payload["statePatch"] = np.StatePatch
		}
		if len(np.MetadataPatch) > 0 {
			payload["metadataPatch"] = np.MetadataPatch
		}
		push("update_node", "update", ref, payload)
	}
	for _, ep := range p.Update.EdgePatches {
		ref := firstNonEmpty(ep.PatchID, ep.EdgeID)
		payload := map[string]interface{}{
			"sourceNodeId":     ep.SourceNodeID,
			"targetNodeId":     ep.TargetNodeID,
			"sourceFragmentId": ep.SourceFragmentID,
			"targetFragmentId": ep.TargetFragmentID,
		}
		if ep.RelationType != nil {
			payload["relationType"] = *ep.RelationType
		}
		if ep.Direction != nil {
			payload["direction"] = *ep.Direction
		}
		if len(ep.Attributes) > 0 {
			payload["attributes"] = ep.Attributes
		}
		push("update_edge", "update", ref, payload)
	}
	for _, rw := range p.Update.RelationRewrite {
		ref := firstNonEmpty(rw.RewriteID, rw.Match.RelationType+"->"+rw.ReplaceWith.RelationType)
		push("rewrite_relation", "update", ref, map[string]interface{}{
			"match":       rw.Match,
			"replaceWith": rw.ReplaceWith,
			"reason":      rw.Reason,
		})
	}
	for _, nodeID := range p.Delete.NodeIDs {
		push("delete_node", "delete", strings.TrimSpace(nodeID), map[string]interface{}{"nodeId": nodeID})
	}
	for _, edgeID := range p.Delete.EdgeIDs {
		push("delete_edge", "delete", strings.TrimSpace(edgeID), map[string]interface{}{"edgeId": edgeID})
	}
	if strings.TrimSpace(p.Merge.PrimaryNodeID) != "" && len(p.Merge.DuplicateNodeIDs) > 0 {
		push("merge_nodes", "merge", p.Merge.PrimaryNodeID, map[string]interface{}{
			"primaryNodeId":     p.Merge.PrimaryNodeID,
			"duplicateNodeIds":  p.Merge.DuplicateNodeIDs,
			"mergeFragmentData": p.Merge.MergeFragmentData,
		})
	}

	for _, dc := range p.Transform.DeriveChains {
		ref := firstNonEmpty(dc.ChainID, dc.FromNodeID+"->"+dc.ToNodeID)
		push("derive_chain", "transform", ref, map[string]interface{}{
			"fromNodeId":    dc.FromNodeID,
			"toNodeId":      dc.ToNodeID,
			"constraints":   dc.Constraints,
			"proposedPath":  dc.ProposedPath,
			"proposedEdges": dc.ProposedEdges,
		})
	}
	for _, rs := range p.Transform.Restructure {
		ref := firstNonEmpty(rs.RestructureID, rs.Mode)
		push("restructure_graph", "transform", ref, map[string]interface{}{
			"scopeNodeIds": rs.ScopeNodeIDs,
			"mode":         rs.Mode,
			"params":       rs.Params,
		})
	}

	return &GraphIntentActionPlan{Actions: actions}
}

func IsGraphIntentGroupingKindAllowed(kind string) bool {
	_, ok := graphIntentAllowedGroupingKinds[strings.TrimSpace(strings.ToLower(kind))]
	return ok
}

func IsGraphIntentRelationTypeAllowed(relationType string) bool {
	_, ok := graphIntentAllowedRelationTypes[strings.ToUpper(strings.TrimSpace(relationType))]
	return ok
}

func IsGraphIntentNodeTypeAllowed(nodeType string) bool {
	_, ok := graphIntentAllowedNodeTypes[strings.ToUpper(strings.TrimSpace(nodeType))]
	return ok
}

// graphIntentRelationDecode is the structured result of normalising a
// wire-level relationType (UPPER_SNAKE) into the AH SoT.
type graphIntentRelationDecode struct {
	// Relation is the canonical AH relation. Empty when the legacy
	// alias is intentionally dropped per migration_map (e.g. LOCATED_IN
	// folds into canonical_path; the caller must handle that case).
	Relation ah.RelationName
	// ExtraAttributes are attribute_spec keys + values that the
	// migration_map says must accompany the rewritten edge (e.g. an
	// `influence_kind` on a derived `influences` edge).
	ExtraAttributes map[string]interface{}
	// Note is a human-readable trace of the rewrite chain — surfaced in
	// graph-intent warnings so authors can see "OWNS -> beneficially_owns
	// (per migration_map.yaml)".
	Note string
	// Dropped is true when the wire alias intentionally has no
	// successor (drop_after_migration / fold_to_*). Callers should
	// either skip the edge or queue it for review per the map.
	Dropped bool
}

// NormalizeGraphIntentRelationType maps a graph-intent wire relationType to the
// AgentHorde RelationName SoT. Accepts three input shapes:
//
//  1. Modern AH name (lower or upper-case, "supplies" / "SUPPLIES");
//  2. Graph-intent affective alias (ALIGNS_WITH/OPPOSES/COOPERATES_WITH/
//     CONFLICTS_WITH) — folded onto `influences` with closed-enum
//     attribute payload per the AgentHorde InfluenceKind enum;
//  3. Legacy DependencyType UPPER_SNAKE name (per migration_map.yaml).
//
// Returns ok=true even when the result is intentionally dropped; the
// Dropped flag distinguishes "not a known wire alias" (ok=false) from
// "known wire alias whose canonical home is no longer an edge"
// (ok=true, Dropped=true). Callers MUST honour Dropped before emitting
// an edge.
func NormalizeGraphIntentRelationType(relationType string) (graphIntentRelationDecode, bool) {
	raw := strings.TrimSpace(relationType)
	if raw == "" {
		return graphIntentRelationDecode{}, false
	}
	upper := strings.ToUpper(raw)

	if rel, ok := graphIntentAffectiveAliases[upper]; ok {
		return rel, true
	}
	if rel, ok := ah.NormalizeRelation(strings.ToLower(raw)); ok {
		return graphIntentRelationDecode{Relation: rel}, true
	}
	if dec, ok := legacyRelationAliases[upper]; ok {
		return dec, true
	}
	return graphIntentRelationDecode{}, false
}

// MapGraphIntentNodeTypeToClass maps a graph-intent wire nodeType to the
// AgentHorde (FragmentClass, subtype) tuple. Accepts:
//
//  1. Modern AH class name (lower or upper-case): "site", "ORG", …;
//  2. Legacy UPPER_SNAKE FragmentType (ORGANIZATION/FACILITY/…) routed
//     through models.LegacyFragmentTypeAliases (the migration_map);
//  3. Path-shape inference from canonicalPathHint when nothing else
//     matches (legacy /fac/* → site, /gov/* → org, …).
//
// The third return value is true when the mapping is confident; false
// when the function fell back to ah.FragmentClassOrg as the historical
// default (callers should warn).
func MapGraphIntentNodeTypeToClass(nodeType, canonicalPathHint string) (ah.FragmentClass, string, bool) {
	raw := strings.TrimSpace(nodeType)
	if raw != "" {
		if class, ok := ah.NormalizeFragmentClass(strings.ToLower(raw)); ok {
			return class, "", true
		}
		upper := strings.ToUpper(raw)
		switch upper {
		case "GOVERNMENT_AGENCY":
			return ah.FragmentClassOrg, "gov", true
		case "COMMUNITY_DYNASTY":
			return ah.FragmentClassCommunity, "ethnic", true
		case "COMMUNITY_TRIBE":
			return ah.FragmentClassCommunity, "ethnic", true
		}
		if class, subtype, ok := FragmentClassFromLegacyType(upper); ok {
			return class, subtype, true
		}
	}

	cp := strings.TrimSpace(strings.ToLower(canonicalPathHint))
	switch {
	case strings.HasPrefix(cp, "/org/"):
		return ah.FragmentClassOrg, "", true
	case strings.HasPrefix(cp, "/gov/"):
		return ah.FragmentClassOrg, "gov", true
	case strings.HasPrefix(cp, "/program/"):
		return ah.FragmentClassOrg, "program", true
	case strings.HasPrefix(cp, "/media/"):
		return ah.FragmentClassOrg, "media_channel", true
	case strings.HasPrefix(cp, "/site/"), strings.HasPrefix(cp, "/fac/"):
		return ah.FragmentClassSite, "", true
	case strings.HasPrefix(cp, "/deposit/"):
		return ah.FragmentClassSite, "deposit", true
	case strings.HasPrefix(cp, "/loc/"), strings.HasPrefix(cp, "/geo/"), strings.HasPrefix(cp, "/region/"):
		return ah.FragmentClassGeo, "", true
	case strings.HasPrefix(cp, "/zone/"):
		return ah.FragmentClassGeo, "affected_zone", true
	case strings.HasPrefix(cp, "/product/"):
		return ah.FragmentClassProduct, "", true
	case strings.HasPrefix(cp, "/market/"):
		return ah.FragmentClassMarket, "", true
	case strings.HasPrefix(cp, "/person/"):
		return ah.FragmentClassPerson, "", true
	case strings.HasPrefix(cp, "/community/"):
		return ah.FragmentClassCommunity, "", true
	case strings.HasPrefix(cp, "/decision/"):
		return ah.FragmentClassDecision, "", true
	case strings.HasPrefix(cp, "/mechanism/"):
		return ah.FragmentClassMechanism, "", true
	case strings.HasPrefix(cp, "/event/"):
		return ah.FragmentClassEvidence, "event", true
	case strings.HasPrefix(cp, "/evidence/"):
		return ah.FragmentClassEvidence, "", true
	case strings.HasPrefix(cp, "/mobile_asset/"), strings.HasPrefix(cp, "/track/"):
		return ah.FragmentClassMobileAsset, "", true
	case strings.HasPrefix(cp, "/regime/"):
		return ah.FragmentClassRegime, "", true
	case strings.HasPrefix(cp, "/route/"):
		return ah.FragmentClassRoute, "", true
	case strings.HasPrefix(cp, "/vessel_class/"):
		return ah.FragmentClassVesselClass, "", true
	case strings.HasPrefix(cp, "/project/"):
		return ah.FragmentClassProject, "", true
	}
	return ah.FragmentClassOrg, "", false
}

// GraphIntentAllowedRelationTypesSorted returns the wire-level relation
// alphabet currently accepted on graph-intent endpoints. It is the union
// of (a) the modern AH RelationName SoT, (b) the four affective aliases,
// and (c) the legacy UPPER_SNAKE wire aliases that we still parse for
// back-compat.
func GraphIntentAllowedRelationTypesSorted() []string {
	out := make([]string, 0, len(graphIntentAllowedRelationTypes))
	for t := range graphIntentAllowedRelationTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func GraphIntentAllowedGroupingKindsSorted() []string {
	out := make([]string, 0, len(graphIntentAllowedGroupingKinds))
	for k := range graphIntentAllowedGroupingKinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func GraphIntentAllowedNodeTypesSorted() []string {
	out := make([]string, 0, len(graphIntentAllowedNodeTypes))
	for t := range graphIntentAllowedNodeTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

var graphIntentAllowedGroupingKinds = map[string]struct{}{
	GraphIntentGroupingGovernanceStructure: {},
	GraphIntentGroupingMarketMap:           {},
	GraphIntentGroupingSupplyChainSkeleton: {},
	GraphIntentGroupingOwnershipControl:    {},
	GraphIntentGroupingInvestmentFlow:      {},
	GraphIntentGroupingRegulatoryLandscape: {},
	GraphIntentGroupingSocietalStructure:   {},
}

// graphIntentAffectiveAliases maps the four graph-intent affective
// labels onto the AH influences relation with the relevant
// influence_kind / influence_polarity attribute payload. Listed
// separately from legacyRelationAliases because these are GRAPH-INTENT-
// SPECIFIC inventions (never existed in the legacy DependencyType enum).
var graphIntentAffectiveAliases = map[string]graphIntentRelationDecode{
	GraphIntentRelationTypeAlignsWith: {
		Relation:        ah.RelationNameInfluences,
		ExtraAttributes: map[string]interface{}{"influence_kind": "advisory", "influence_polarity": "positive"},
		Note:            "ALIGNS_WITH -> influences (positive, advisory)",
	},
	GraphIntentRelationTypeCooperates: {
		Relation:        ah.RelationNameJointVentureWith,
		Note:            "COOPERATES_WITH -> joint_venture_with (structural cooperation)",
	},
	GraphIntentRelationTypeOpposes: {
		Relation:        ah.RelationNameInfluences,
		ExtraAttributes: map[string]interface{}{"influence_kind": "advisory", "influence_polarity": "negative"},
		Note:            "OPPOSES -> influences (negative)",
	},
	GraphIntentRelationTypeConflictsWith: {
		Relation:        ah.RelationNameContradicts,
		Note:            "CONFLICTS_WITH -> contradicts (decision-layer)",
	},
}

// legacyRelationAliases is the migration_map.yaml dependency_type_map
// transcribed for the wire-level decoder. Every legacy UPPER_SNAKE
// DependencyType value MUST appear here (CI guard: graph_intent_test.go
// asserts coverage). Three shapes:
//
//   - Relation set: straight rename / renamed-with-attrs / reverse (the
//     reverse case is handled at the SCOUT layer because it requires
//     swapping src/dst, not at the wire decoder).
//   - Dropped=true: drop_after_migration or fold_to_attributes/
//     fold_to_canonical_path. Caller drops the edge (eventually queued
//     as candidate when the disposition says so).
//   - Relation="" with Note: needs_design or candidate_queue dispositions
//     — flagged here so review tooling can spot legacy plans still
//     emitting them.
var legacyRelationAliases = map[string]graphIntentRelationDecode{
	// Core supply-chain.
	"SUPPLIES_TO":     {Relation: ah.RelationNameSupplies, Note: "SUPPLIES_TO -> supplies"},
	"PRODUCES":        {Relation: ah.RelationNameProduces, Note: "PRODUCES -> produces"},
	"CONSUMES":        {Relation: ah.RelationNameSupplies, Note: "CONSUMES -> supplies (src/dst swapped at scout layer)"},
	"EXTRACTS_FROM":   {Relation: ah.RelationNameProduces, ExtraAttributes: map[string]interface{}{"production_kind": "extraction"}, Note: "EXTRACTS_FROM -> produces (production_kind=extraction)"},
	"USED_IN":         {Dropped: true, Note: "USED_IN dropped: redundant inverse of produces"},
	"TRANSFORMS_INTO": {Dropped: true, Note: "TRANSFORMS_INTO folded onto site siteTransforms[]"},
	"ASSEMBLES_INTO":  {Dropped: true, Note: "ASSEMBLES_INTO folded onto site siteTransforms[]"},

	// Geographic / locational.
	"LOCATED_IN":   {Dropped: true, Note: "LOCATED_IN folded into canonical_path geo slot"},
	"OPERATES_IN":  {Dropped: true, Note: "OPERATES_IN folded into attributes.operating_geos[]"},
	"IN_GEO_SCOPE": {Dropped: true, Note: "IN_GEO_SCOPE folded into attributes.scope_geos[]"},

	// Ownership / control.
	"OWNS":            {Relation: ah.RelationNameBeneficiallyOwns, Note: "OWNS -> beneficially_owns"},
	"SUBSIDIARY_OF":   {Relation: ah.RelationNameSubsidiaryOf, Note: "SUBSIDIARY_OF -> subsidiary_of"},
	"CONTROLS":        {Relation: ah.RelationNameControlsVia, Note: "CONTROLS -> controls_via"},
	"CONTROLS_ACCESS": {Relation: ah.RelationNameImposes, ExtraAttributes: map[string]interface{}{"mechanism_category": "access_control"}, Note: "CONTROLS_ACCESS rewritten to wields+imposes via synthesized /mechanism/access-control/* — wire decoder emits imposes; scout layer synthesizes mechanism node"},
	"ADMINISTERS":     {Relation: ah.RelationNameOperates, Note: "ADMINISTERS -> operates"},
	"MANAGES":         {Relation: ah.RelationNameManagedBy, Note: "MANAGES -> managed_by (src/dst swapped at scout layer)"},
	"OPERATES_VIA":    {Relation: ah.RelationNameWields, Note: "OPERATES_VIA -> wields"},

	// Corporate events.
	"INVESTS_IN":           {Relation: ah.RelationNameInvestedIn, Note: "INVESTS_IN -> invested_in"},
	"PARTNERS_WITH":        {Relation: ah.RelationNameJointVentureWith, Note: "PARTNERS_WITH -> joint_venture_with"},
	"BOARD_MEMBER_OF":      {Relation: ah.RelationNameHoldsBoardSeat, Note: "BOARD_MEMBER_OF -> holds_board_seat"},
	"FOUNDED":              {Dropped: true, Note: "FOUNDED folded into attributes.founders[]"},
	"EMPLOYED_BY":          {Dropped: true, Note: "EMPLOYED_BY dropped (LLM noise)"},
	"WORKS_FOR":            {Dropped: true, Note: "WORKS_FOR dropped (duplicate of EMPLOYED_BY)"},
	"KEY_PERSON":           {Dropped: true, Note: "KEY_PERSON folded into attributes.key_persons[]"},
	"HAS_KEY_PERSON":       {Dropped: true, Note: "HAS_KEY_PERSON folded into attributes.key_persons[]"},
	"RENAMED_TO":           {Dropped: true, Note: "RENAMED_TO folded into nameHistory"},
	"REBRANDED_TO":         {Dropped: true, Note: "REBRANDED_TO folded into nameHistory"},
	"LEGAL_SUCCESSOR_OF":   {Relation: ah.RelationNameMergedInto, Note: "LEGAL_SUCCESSOR_OF -> merged_into (src/dst swapped at scout layer)"},
	"MERGED_INTO":          {Relation: ah.RelationNameMergedInto, Note: "MERGED_INTO -> merged_into"},
	"SPUN_OFF_FROM":        {Relation: ah.RelationNameSpunOut, Note: "SPUN_OFF_FROM -> spun_out (src/dst swapped at scout layer)"},
	"ACQUIRED_BY":          {Relation: ah.RelationNameAcquiredStakeFrom, Note: "ACQUIRED_BY -> acquired_stake_from (src/dst swapped at scout layer)"},
	"CORPORATE_TRANSITION": {Note: "CORPORATE_TRANSITION routes by attributes.transition_kind at scout layer"},

	// Regulation / regime.
	"REGULATES":          {Relation: ah.RelationNameRegulates, Note: "REGULATES -> regulates"},
	"IMPOSES_CONDITION":  {Relation: ah.RelationNameImposes, Note: "IMPOSES_CONDITION -> imposes (mechanism synthesized at scout layer)"},
	"APPLIES_TO":         {Relation: ah.RelationNameImposes, Note: "APPLIES_TO -> imposes (mechanism synthesized at scout layer)"},
	"GATEKEEPS_DECISION": {Relation: ah.RelationNameGatekeeps, Note: "GATEKEEPS_DECISION -> gatekeeps"},
	"USES_MECHANISM":     {Relation: ah.RelationNameWields, Note: "USES_MECHANISM -> wields"},

	// Influence / soft power.
	"INFLUENCES": {Relation: ah.RelationNameInfluences, Note: "INFLUENCES -> influences (requires attributes.influence_kind)"},
	"DOMINATES":  {Relation: ah.RelationNameInfluences, ExtraAttributes: map[string]interface{}{"influence_kind": "structural_dominance"}, Note: "DOMINATES -> influences (influence_kind=structural_dominance)"},

	// Markets.
	"HAS_MARKET_SEGMENT": {Dropped: true, Note: "HAS_MARKET_SEGMENT folded into attributes.market_segment_norms[]"},
	"HAS_SUBMARKET":      {Dropped: true, Note: "HAS_SUBMARKET folded into attributes.submarkets[]"},
	"TRADED_IN":          {Relation: ah.RelationNameListedOn, Note: "TRADED_IN -> listed_on"},
	"COMPETES_WITH":      {Dropped: true, Note: "COMPETES_WITH dropped (symmetric, noisy)"},
	"PARTICIPATES_IN":    {Note: "PARTICIPATES_IN routes per (src_class, dst_class) at scout layer"},

	// Assurance.
	"ASSURES":     {Relation: ah.RelationNameAssures, Note: "ASSURES -> assures (requires attributes.assurance_type)"},
	"AUDITED_BY":  {Relation: ah.RelationNameAssures, ExtraAttributes: map[string]interface{}{"assurance_type": "audit"}, Note: "AUDITED_BY -> assures (assurance_type=audit, src/dst swapped at scout layer)"},

	// Membership / affiliation.
	"MEMBER_OF":       {Relation: ah.RelationNameMemberOf, Note: "MEMBER_OF -> member_of"},
	"AFFILIATED_WITH": {Dropped: true, Note: "AFFILIATED_WITH dropped (vague LLM noise)"},
	"ASSOCIATED_WITH": {Dropped: true, Note: "ASSOCIATED_WITH dropped (vague LLM noise)"},
	"RELATIVE_OF":     {Dropped: true, Note: "RELATIVE_OF folded into attributes.relations[]"},
	"RELATED_TO":      {Dropped: true, Note: "RELATED_TO dropped (generic)"},

	// Domain / sphere bookkeeping.
	"DOMAIN_RELATION": {Dropped: true, Note: "DOMAIN_RELATION dropped with DOMAIN class"},
	"RELATED_DOMAIN":  {Dropped: true, Note: "RELATED_DOMAIN dropped with DOMAIN class"},
	"BROADENS_TO":     {Dropped: true, Note: "BROADENS_TO dropped (domain bookkeeping)"},
	"NARROWS_TO":      {Dropped: true, Note: "NARROWS_TO dropped (domain bookkeeping)"},
	"FOCUSES_ON":      {Dropped: true, Note: "FOCUSES_ON dropped (domain bookkeeping)"},
	"HAS_SUBGROUP":    {Dropped: true, Note: "HAS_SUBGROUP folded into attributes.subgroups[]"},

	// Event scaffolding.
	"INVOLVES_ACTOR": {Dropped: true, Note: "INVOLVES_ACTOR folded into attributes.actors[] on evidence"},
	"ABOUT":          {Dropped: true, Note: "ABOUT folded into attributes.about_refs[]"},
	"SURFACES":       {Dropped: true, Note: "SURFACES dropped (superseded by Provenance.sourceFrameId)"},
	"AFFECTS":        {Relation: ah.RelationNameDisrupts, Note: "AFFECTS -> disrupts (requires attributes.disruption_kind)"},
	"TRIGGERED_BY":   {Dropped: true, Note: "TRIGGERED_BY folded into attributes.triggers[]"},

	// Generic catch-all (per-pair routing happens at scout layer).
	"DEPENDS_ON": {Note: "DEPENDS_ON routes per (src_class, dst_class) at scout layer"},
}

// graphIntentAllowedRelationTypes is the wire alphabet currently accepted
// on graph-intent endpoints. Built dynamically from the AH SoT plus the
// four affective aliases plus the legacy UPPER_SNAKE keys, so a taxonomy
// codegen run automatically refreshes it without manual edits here.
var graphIntentAllowedRelationTypes = func() map[string]struct{} {
	out := make(map[string]struct{}, len(ah.RelationNames)+len(graphIntentAffectiveAliases)+len(legacyRelationAliases))
	for _, r := range ah.RelationNames {
		out[strings.ToUpper(string(r))] = struct{}{}
		out[string(r)] = struct{}{}
	}
	for k := range graphIntentAffectiveAliases {
		out[k] = struct{}{}
	}
	for k := range legacyRelationAliases {
		out[k] = struct{}{}
	}
	return out
}()

// graphIntentAllowedNodeTypes is the wire alphabet for fragment classes.
// Mirrors graphIntentAllowedRelationTypes structure: AH SoT + legacy
// FragmentType UPPER_SNAKE keys (via models.LegacyFragmentTypeAliases) +
// graph-intent-specific synonyms.
var graphIntentAllowedNodeTypes = func() map[string]struct{} {
	out := make(map[string]struct{}, len(ah.FragmentClasses)+len(LegacyFragmentTypeAliases)+8)
	for _, c := range ah.FragmentClasses {
		out[strings.ToUpper(string(c))] = struct{}{}
		out[string(c)] = struct{}{}
	}
	for legacy := range LegacyFragmentTypeAliases {
		out[legacy] = struct{}{}
	}
	out["GOVERNMENT_AGENCY"] = struct{}{}
	out["COMMUNITY_DYNASTY"] = struct{}{}
	out["COMMUNITY_TRIBE"] = struct{}{}
	return out
}()
