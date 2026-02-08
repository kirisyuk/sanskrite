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
				FocusNodePaths: focusPaths,
				ResearchPlan: &models.ResearchPlan{
					MissingNodes: focusPaths,
					Reason:       "focus entities not found in graph",
				},
			},
		}, nil
	}

	edgeTypes := []models.DependencyType{
		models.DependencyTypeOwns,
		models.DependencyTypeSubsidiaryOf,
		models.DependencyTypeControls,
		models.DependencyTypeInvestsIn,
	}

	parentMap := map[string][]ownershipEdgeRef{} // childID -> parent edge refs
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
		for _, childID := range queue {
			if _, ok := visited[childID]; ok {
				continue
			}
			visited[childID] = struct{}{}

			for _, et := range edgeTypes {
				edges, err := in.EdgesRepo.ListByTarget(ctx, in.Job.TenantID, childID, &et, 200)
				if err != nil {
					continue
				}
				for _, edge := range edges {
					if edge == nil || edge.SourceFragmentID == "" {
						continue
					}
					parentMap[childID] = append(parentMap[childID], ownershipEdgeRef{Edge: edge})
					if _, ok := visited[edge.SourceFragmentID]; !ok {
						next = append(next, edge.SourceFragmentID)
					}
				}
			}
		}
		queue = next
		curDepth++
	}

	if len(parentMap) == 0 {
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

	for focusID := range focusFragments {
		buildOwnershipPaths(focusID, parentMap, nodeCache, []string{}, 0, maxDepth, &highlightPaths, ultimateOwners, &annotations)
	}

	focusNodePaths := make([]string, 0, len(focusPaths)+len(ultimateOwners))
	focusNodePaths = append(focusNodePaths, focusPaths...)
	for ownerID := range ultimateOwners {
		if frag := nodeCache[ownerID]; frag != nil && frag.CanonicalPath != "" {
			focusNodePaths = append(focusNodePaths, frag.CanonicalPath)
		}
	}
	focusNodePaths = dedupStrings(focusNodePaths)

	summary := "Ownership structure computed from existing graph data."
	if len(ultimateOwners) > 0 {
		summary = fmt.Sprintf("Ownership structure found with %d ultimate owner(s).", len(ultimateOwners))
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
			HierarchyAxis:  "ownership",
			FilterRelTypes: []string{"OWNS", "SUBSIDIARY_OF", "CONTROLS", "INVESTS_IN"},
			HighlightPaths: highlightPaths,
			FocusNodePaths: focusNodePaths,
			Annotations:    annotations,
			Summary:        summary,
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
	for _, key := range []string{"subject", "asset", "organization", "org"} {
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

func appendOwnershipAnnotations(
	edge *models.DependencyEdge,
	nodeCache map[string]*models.HorizonFragment,
	annotations *[]models.Annotation,
) {
	if edge == nil || edge.Qualifiers == nil {
		return
	}
	target := nodeCache[edge.TargetFragmentID]
	if target == nil || target.CanonicalPath == "" {
		return
	}
	for _, key := range []string{"stake_pct", "control_type", "ownership_percent"} {
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
