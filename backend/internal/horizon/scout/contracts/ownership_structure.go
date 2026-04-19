package contracts

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type OwnershipStructureHandler struct{}

type ownershipEdgeRef struct {
	Edge *models.DependencyEdge
}

var ownershipPrimaryEdgeTypes = []DependencyType{
	DependencyTypeOwns,
	DependencyTypeSubsidiaryOf,
	DependencyTypeControls,
	DependencyTypeInvestsIn,
}

var ownershipSupportEdgeTypes = []DependencyType{
	DependencyTypeHasSubgroup,
	DependencyTypeMemberOf,
	DependencyTypeAffiliatedWith,
	DependencyTypeOperatesVia,
	DependencyTypeBoardMemberOf,
	DependencyTypeEmployedBy,
	DependencyTypeFounded,
	DependencyTypeManages,
	DependencyTypeAdministers,
}

var ownershipFilterRelTypes = []string{
	"OWNS", "SUBSIDIARY_OF", "CONTROLS", "INVESTS_IN",
	"HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA",
	"BOARD_MEMBER_OF", "EMPLOYED_BY", "FOUNDED", "MANAGES", "ADMINISTERS",
}

func NewOwnershipStructureHandler() *OwnershipStructureHandler {
	return &OwnershipStructureHandler{}
}

func (h *OwnershipStructureHandler) ContractType() string {
	return "ownership-structure"
}

func (h *OwnershipStructureHandler) Execute(ctx context.Context, eng pipeline.Engine, in pipeline.ContractInput) (*pipeline.ContractOutput, error) {
	if in.Job == nil || in.EdgesRepo == nil || in.FragmentsRepo == nil {
		if in.Job != nil {
			trace.Log(ctx, "contract", "contract.ownership.input.missing", map[string]any{
				"tenantId":     strings.TrimSpace(in.Job.TenantID),
				"projectId":    strings.TrimSpace(in.Job.ProjectID),
				"lensId":       strings.TrimSpace(in.Job.LensID),
				"jobId":        strings.TrimSpace(in.Job.ID),
				"contractType": h.ContractType(),
			})
		}
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType: h.ContractType(),
				GraphView:    models.GraphViewHierarchical,
				ResearchPlan: &models.ResearchPlan{
					Reason: "contract input missing job or repositories",
				},
			},
		}, nil
	}

	focusPaths := ownershipFocusPaths(in.Contract)
	if len(focusPaths) == 0 {
		trace.Log(ctx, "contract", "contract.ownership.focus.empty", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType: h.ContractType(),
				GraphView:    models.GraphViewHierarchical,
				ResearchPlan: &models.ResearchPlan{
					Reason: "ownership focus entity not found in contract",
				},
			},
		}, nil
	}

	focusFragments := map[string]*models.HorizonFragment{}
	for _, cp := range focusPaths {
		frag, err := in.FragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err == nil && frag != nil {
			focusFragments[frag.ID] = frag
		}
	}
	if len(focusFragments) == 0 {
		trace.Log(ctx, "contract", "contract.ownership.focus.missing", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
			"focusPaths":   focusPaths,
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType:   h.ContractType(),
				GraphView:      models.GraphViewHierarchical,
				FocusNodePaths: focusPaths,
				ResearchPlan: &models.ResearchPlan{
					MissingNodes: focusPaths,
					Reason:       "focus entities not found in graph",
				},
			},
		}, nil
	}

	parentMap := map[string][]ownershipEdgeRef{} // childID -> parent edge refs
	childMap := map[string][]ownershipEdgeRef{}  // parentID -> child edge refs
	nodeCache := map[string]*models.HorizonFragment{}
	queue := make([]string, 0, len(focusFragments))
	visited := map[string]struct{}{}

	for id, frag := range focusFragments {
		queue = append(queue, id)
		nodeCache[id] = frag
	}

	maxDepth := 5
	curDepth := 0
	for len(queue) > 0 && curDepth < maxDepth {
		next := make([]string, 0)
		for _, nodeID := range queue {
			if _, ok := visited[nodeID]; ok {
				continue
			}
			visited[nodeID] = struct{}{}

			for _, et := range ownershipPrimaryEdgeTypes {
				inEdges, err := in.EdgesRepo.ListByTarget(ctx, in.Job.TenantID, nodeID, &et, 200)
				if err == nil {
					for _, edge := range inEdges {
						if edge == nil || edge.SourceFragmentID == "" {
							continue
						}
						parentMap[nodeID] = append(parentMap[nodeID], ownershipEdgeRef{Edge: edge})
						if _, ok := visited[edge.SourceFragmentID]; !ok {
							next = append(next, edge.SourceFragmentID)
						}
					}
				}

				outEdges, err := in.EdgesRepo.ListBySource(ctx, in.Job.TenantID, nodeID, &et, 200)
				if err == nil {
					for _, edge := range outEdges {
						if edge == nil || edge.TargetFragmentID == "" {
							continue
						}
						childMap[nodeID] = append(childMap[nodeID], ownershipEdgeRef{Edge: edge})
						if _, ok := visited[edge.TargetFragmentID]; !ok {
							next = append(next, edge.TargetFragmentID)
						}
					}
				}
			}
		}
		queue = next
		curDepth++
	}

	if len(parentMap) == 0 && len(childMap) == 0 {
		trace.Log(ctx, "contract", "contract.ownership.edges.missing", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
			"focusPaths":   focusPaths,
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType:   h.ContractType(),
				GraphView:      models.GraphViewHierarchical,
				FocusNodePaths: focusPaths,
				ResearchPlan: &models.ResearchPlan{
					MissingNodes: focusPaths,
					Reason:       "no ownership/control edges found in graph",
				},
			},
		}, nil
	}

	// Fetch all known nodes in one batch.
	needIDs := map[string]struct{}{}
	for childID, edges := range parentMap {
		needIDs[childID] = struct{}{}
		for _, ref := range edges {
			if ref.Edge != nil && ref.Edge.SourceFragmentID != "" {
				needIDs[ref.Edge.SourceFragmentID] = struct{}{}
			}
		}
	}
	for parentID, edges := range childMap {
		needIDs[parentID] = struct{}{}
		for _, ref := range edges {
			if ref.Edge != nil && ref.Edge.TargetFragmentID != "" {
				needIDs[ref.Edge.TargetFragmentID] = struct{}{}
			}
		}
	}
	idList := make([]string, 0, len(needIDs))
	for id := range needIDs {
		if _, ok := nodeCache[id]; ok {
			continue
		}
		idList = append(idList, id)
	}
	if len(idList) > 0 {
		if frags, err := in.FragmentsRepo.GetMany(ctx, idList); err == nil {
			for id, frag := range frags {
				if frag != nil {
					nodeCache[id] = frag
				}
			}
		}
	}

	// Build highlight paths from focus to ultimate owners.
	highlightPaths := make([]models.HighlightPath, 0)
	annotations := make([]models.Annotation, 0)
	ultimateOwners := map[string]struct{}{}
	terminalChildren := map[string]struct{}{}

	for focusID := range focusFragments {
		buildOwnershipPaths(focusID, parentMap, nodeCache, []string{}, 0, maxDepth, &highlightPaths, ultimateOwners, &annotations)
		buildOwnedPaths(focusID, childMap, nodeCache, []string{}, 0, maxDepth, &highlightPaths, terminalChildren, &annotations)
	}

	chainNodeIDs := map[string]struct{}{}
	for id := range focusFragments {
		chainNodeIDs[id] = struct{}{}
	}
	for childID, edges := range parentMap {
		chainNodeIDs[childID] = struct{}{}
		for _, ref := range edges {
			if ref.Edge != nil && ref.Edge.SourceFragmentID != "" {
				chainNodeIDs[ref.Edge.SourceFragmentID] = struct{}{}
			}
		}
	}
	for parentID, edges := range childMap {
		chainNodeIDs[parentID] = struct{}{}
		for _, ref := range edges {
			if ref.Edge != nil && ref.Edge.TargetFragmentID != "" {
				chainNodeIDs[ref.Edge.TargetFragmentID] = struct{}{}
			}
		}
	}

	supportCoverage := map[string]map[string]struct{}{}
	evidenceCoverage := map[string]bool{}
	for nodeID := range chainNodeIDs {
		for _, ref := range parentMap[nodeID] {
			if ref.Edge != nil && ownershipEdgeHasEvidence(ref.Edge) {
				evidenceCoverage[nodeID] = true
				evidenceCoverage[ref.Edge.SourceFragmentID] = true
			}
		}
		for _, ref := range childMap[nodeID] {
			if ref.Edge != nil && ownershipEdgeHasEvidence(ref.Edge) {
				evidenceCoverage[nodeID] = true
				evidenceCoverage[ref.Edge.TargetFragmentID] = true
			}
		}
	}

	for nodeID := range chainNodeIDs {
		for _, et := range ownershipSupportEdgeTypes {
			outEdges, _ := in.EdgesRepo.ListBySource(ctx, in.Job.TenantID, nodeID, &et, 200)
			for _, edge := range outEdges {
				if edge == nil || edge.TargetFragmentID == "" {
					continue
				}
				needIDs[edge.TargetFragmentID] = struct{}{}
				registerOwnershipSupport(supportCoverage, nodeID, string(et))
				registerOwnershipSupport(supportCoverage, edge.TargetFragmentID, string(et))
				appendOwnershipAnnotations(edge, nodeCache, &annotations)
			}
			inEdges, _ := in.EdgesRepo.ListByTarget(ctx, in.Job.TenantID, nodeID, &et, 200)
			for _, edge := range inEdges {
				if edge == nil || edge.SourceFragmentID == "" {
					continue
				}
				needIDs[edge.SourceFragmentID] = struct{}{}
				registerOwnershipSupport(supportCoverage, nodeID, string(et))
				registerOwnershipSupport(supportCoverage, edge.SourceFragmentID, string(et))
				appendOwnershipAnnotations(edge, nodeCache, &annotations)
			}
		}
	}

	// Fetch support nodes discovered after the initial ownership traversal.
	idList = idList[:0]
	for id := range needIDs {
		if _, ok := nodeCache[id]; ok {
			continue
		}
		idList = append(idList, id)
	}
	if len(idList) > 0 {
		if frags, err := in.FragmentsRepo.GetMany(ctx, idList); err == nil {
			for id, frag := range frags {
				if frag != nil {
					nodeCache[id] = frag
				}
			}
		}
	}

	focusNodePaths := make([]string, 0, len(needIDs))
	for ownerID := range ultimateOwners {
		if frag := nodeCache[ownerID]; frag != nil && frag.CanonicalPath != "" {
			focusNodePaths = append(focusNodePaths, frag.CanonicalPath)
		}
	}
	for leafID := range terminalChildren {
		if frag := nodeCache[leafID]; frag != nil && frag.CanonicalPath != "" {
			focusNodePaths = append(focusNodePaths, frag.CanonicalPath)
		}
	}
	for nodeID := range needIDs {
		if frag := nodeCache[nodeID]; frag != nil && frag.CanonicalPath != "" {
			focusNodePaths = append(focusNodePaths, frag.CanonicalPath)
		}
	}
	focusNodePaths = dedupStrings(focusNodePaths)
	annotations = dedupAnnotations(annotations)

	thinNodes := ownershipThinNodePaths(chainNodeIDs, nodeCache, supportCoverage, evidenceCoverage)
	var researchPlan *models.ResearchPlan
	if len(thinNodes) > 0 {
		researchPlan = &models.ResearchPlan{
			MissingNodes: thinNodes,
			Reason:       "ownership graph exists but is thin for some nodes; research beneficial owners, voting rights, people layer, and community/media structure for the marked nodes",
		}
	}

	summary := "Ownership structure computed from existing graph data."
	if len(ultimateOwners) > 0 {
		summary = fmt.Sprintf(
			"Ownership structure found with %d ultimate owner(s), %d downstream branch endpoint(s), and %d thin node(s).",
			len(ultimateOwners),
			len(terminalChildren),
			len(thinNodes),
		)
	}
	trace.Log(ctx, "contract", "contract.ownership.ok", map[string]any{
		"tenantId":       strings.TrimSpace(in.Job.TenantID),
		"projectId":      strings.TrimSpace(in.Job.ProjectID),
		"lensId":         strings.TrimSpace(in.Job.LensID),
		"jobId":          strings.TrimSpace(in.Job.ID),
		"contractType":   h.ContractType(),
		"focusCount":     len(focusPaths),
		"ownersCount":    len(ultimateOwners),
		"highlightPaths": len(highlightPaths),
	})

	return &pipeline.ContractOutput{
		ViewDirective: &models.ViewDirective{
			ContractType:   h.ContractType(),
			GraphView:      models.GraphViewHierarchical,
			HierarchyAxis:  "ownership",
			FilterRelTypes: ownershipFilterRelTypes,
			HighlightPaths: highlightPaths,
			FocusNodePaths: focusNodePaths,
			Annotations:    annotations,
			Summary:        summary,
			ResearchPlan:   researchPlan,
		},
	}, nil
}

func ownershipFocusPaths(contract *pipeline.DomainContract) []string {
	if contract == nil {
		return nil
	}
	out := make([]string, 0)
	for _, cp := range contract.FirstOrderEntities {
		cp = strings.TrimSpace(cp)
		if cp == "" {
			continue
		}
		if taxonomy.ValidateCanonicalPath(cp) == nil {
			out = append(out, cp)
		}
	}
	if len(out) > 0 {
		return dedupStrings(out)
	}
	for _, key := range []string{"subject", "asset", "organization", "org", "owner", "controller", "beneficiary", "beneficial_owner"} {
		if vals, ok := contract.Facets[key]; ok {
			for _, v := range vals {
				v = strings.TrimSpace(v)
				if v == "" || !strings.HasPrefix(v, "/") {
					continue
				}
				if taxonomy.ValidateCanonicalPath(v) == nil {
					out = append(out, v)
				}
			}
		}
	}
	return dedupStrings(out)
}

func buildOwnershipPaths(
	focusID string,
	parentMap map[string][]ownershipEdgeRef,
	nodeCache map[string]*models.HorizonFragment,
	prefix []string,
	depth int,
	maxDepth int,
	paths *[]models.HighlightPath,
	ultimateOwners map[string]struct{},
	annotations *[]models.Annotation,
) {
	if depth > maxDepth {
		return
	}

	frag := nodeCache[focusID]
	if frag != nil && frag.CanonicalPath != "" {
		if stringSliceContains(prefix, frag.CanonicalPath) {
			return
		}
		prefix = append(prefix, frag.CanonicalPath)
	} else {
		return
	}

	edges := parentMap[focusID]
	if len(edges) == 0 {
		ultimateOwners[focusID] = struct{}{}
		*paths = append(*paths, models.HighlightPath{
			NodePaths: append([]string(nil), prefix...),
			Label:     "ownership chain",
			Style:     "primary",
		})
		return
	}

	for _, ref := range edges {
		if ref.Edge == nil || ref.Edge.SourceFragmentID == "" {
			continue
		}
		edge := ref.Edge
		if edge.Qualifiers != nil {
			appendOwnershipAnnotations(edge, nodeCache, annotations)
		}
		buildOwnershipPaths(edge.SourceFragmentID, parentMap, nodeCache, append([]string(nil), prefix...), depth+1, maxDepth, paths, ultimateOwners, annotations)
	}
}

func buildOwnedPaths(
	focusID string,
	childMap map[string][]ownershipEdgeRef,
	nodeCache map[string]*models.HorizonFragment,
	prefix []string,
	depth int,
	maxDepth int,
	paths *[]models.HighlightPath,
	terminalChildren map[string]struct{},
	annotations *[]models.Annotation,
) {
	if depth > maxDepth {
		return
	}

	frag := nodeCache[focusID]
	if frag != nil && frag.CanonicalPath != "" {
		if stringSliceContains(prefix, frag.CanonicalPath) {
			return
		}
		prefix = append(prefix, frag.CanonicalPath)
	} else {
		return
	}

	edges := childMap[focusID]
	if len(edges) == 0 {
		terminalChildren[focusID] = struct{}{}
		if len(prefix) > 1 {
			*paths = append(*paths, models.HighlightPath{
				NodePaths: append([]string(nil), prefix...),
				Label:     "owned branch",
				Style:     "secondary",
			})
		}
		return
	}

	for _, ref := range edges {
		if ref.Edge == nil || ref.Edge.TargetFragmentID == "" {
			continue
		}
		if ref.Edge.Qualifiers != nil || len(ref.Edge.Metrics) > 0 {
			appendOwnershipAnnotations(ref.Edge, nodeCache, annotations)
		}
		buildOwnedPaths(ref.Edge.TargetFragmentID, childMap, nodeCache, append([]string(nil), prefix...), depth+1, maxDepth, paths, terminalChildren, annotations)
	}
}

func appendOwnershipAnnotations(
	edge *models.DependencyEdge,
	nodeCache map[string]*models.HorizonFragment,
	annotations *[]models.Annotation,
) {
	if edge == nil {
		return
	}
	target := nodeCache[edge.TargetFragmentID]
	if target == nil || target.CanonicalPath == "" {
		return
	}
	for _, key := range []string{"stake_pct", "control_type", "ownership_percent", "voting_percent", "voting_rights_percent", "share_type", "mechanism"} {
		if v, ok := edge.Qualifiers[key]; ok {
			val := strings.TrimSpace(fmt.Sprint(v))
			if val == "" {
				continue
			}
			*annotations = append(*annotations, models.Annotation{
				CanonicalPath: target.CanonicalPath,
				Key:           key,
				Value:         val,
			})
		}
	}
	for _, key := range []string{"ownership_percent", "voting_percent", "voting_rights_percent"} {
		if mv, ok := edge.Metrics[key]; ok {
			*annotations = append(*annotations, models.Annotation{
				CanonicalPath: target.CanonicalPath,
				Key:           key,
				Value:         strings.TrimSpace(fmt.Sprintf("%.4g", mv.Value)),
			})
		}
	}
}

func ownershipEdgeHasEvidence(edge *models.DependencyEdge) bool {
	if edge == nil {
		return false
	}
	for _, key := range []string{"ownership_percent", "stake_pct", "voting_percent", "voting_rights_percent", "share_type", "control_type", "mechanism"} {
		if edge.Qualifiers != nil {
			if v, ok := edge.Qualifiers[key]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
				return true
			}
		}
		if edge.Metrics != nil {
			if _, ok := edge.Metrics[key]; ok {
				return true
			}
		}
	}
	return false
}

func registerOwnershipSupport(coverage map[string]map[string]struct{}, nodeID string, edgeType string) {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(edgeType) == "" {
		return
	}
	if coverage[nodeID] == nil {
		coverage[nodeID] = map[string]struct{}{}
	}
	coverage[nodeID][edgeType] = struct{}{}
}

func ownershipThinNodePaths(
	chainNodeIDs map[string]struct{},
	nodeCache map[string]*models.HorizonFragment,
	supportCoverage map[string]map[string]struct{},
	evidenceCoverage map[string]bool,
) []string {
	out := make([]string, 0)
	for nodeID := range chainNodeIDs {
		frag := nodeCache[nodeID]
		if frag == nil || strings.TrimSpace(frag.CanonicalPath) == "" {
			continue
		}
		support := supportCoverage[nodeID]
		hasSupport := len(support) > 0
		hasEvidence := evidenceCoverage[nodeID]
		needsResearch := false
		// MIGRATION NOTE (AH taxonomy / docs/taxonomy/migration_map.yaml):
		// supportCoverage stores raw scout DependencyType values which
		// under AH are snake_case (RelationName slugs like "member_of"
		// or legacy literals like "has_subgroup"). The pre-migration
		// allow-list pinned on UPPER_SNAKE wire labels and silently
		// missed every snake_case entry, falsely flagging well-supported
		// community/person nodes as "thin". Build the probe set from
		// the scout DependencyType constants so the comparison stays
		// case-aligned.
		switch frag.Class {
		case FragmentTypeOrganization:
			needsResearch = !hasEvidence || !hasSupport
		case FragmentTypeCommunity:
			needsResearch = !supportContainsAny(support,
				string(DependencyTypeHasSubgroup),
				string(DependencyTypeMemberOf),
				string(DependencyTypeAffiliatedWith),
				string(DependencyTypeOperatesVia),
			)
		case FragmentTypePerson:
			needsResearch = !supportContainsAny(support,
				string(DependencyTypeMemberOf),
				string(DependencyTypeAffiliatedWith),
				string(DependencyTypeBoardMemberOf),
				string(DependencyTypeEmployedBy),
				string(DependencyTypeFounded),
			)
		default:
			needsResearch = !hasEvidence && !hasSupport
		}
		if needsResearch {
			out = append(out, frag.CanonicalPath)
		}
	}
	return dedupStrings(out)
}

func supportContainsAny(support map[string]struct{}, edgeTypes ...string) bool {
	if len(support) == 0 {
		return false
	}
	for _, edgeType := range edgeTypes {
		if _, ok := support[edgeType]; ok {
			return true
		}
	}
	return false
}

func dedupAnnotations(in []models.Annotation) []models.Annotation {
	seen := map[string]struct{}{}
	out := make([]models.Annotation, 0, len(in))
	for _, ann := range in {
		key := strings.TrimSpace(ann.CanonicalPath) + "|" + strings.TrimSpace(ann.Key) + "|" + strings.TrimSpace(ann.Value)
		if key == "||" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ann)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CanonicalPath == out[j].CanonicalPath {
			if out[i].Key == out[j].Key {
				return out[i].Value < out[j].Value
			}
			return out[i].Key < out[j].Key
		}
		return out[i].CanonicalPath < out[j].CanonicalPath
	})
	return out
}

func stringSliceContains(items []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func dedupStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
