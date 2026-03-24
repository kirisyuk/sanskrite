package contracts

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
)

type ControlMapHandler struct{}

func NewControlMapHandler() *ControlMapHandler {
	return &ControlMapHandler{}
}

func (h *ControlMapHandler) ContractType() string {
	return "control-map"
}

func (h *ControlMapHandler) Execute(ctx context.Context, eng pipeline.Engine, in pipeline.ContractInput) (*pipeline.ContractOutput, error) {
	if in.Job == nil || in.EdgesRepo == nil || in.FragmentsRepo == nil {
		if in.Job != nil {
			trace.Log(ctx, "contract", "contract.control_map.input.missing", map[string]any{
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
				GraphView:    models.GraphViewInfluenceNet,
				ResearchPlan: &models.ResearchPlan{
					Reason: "contract input missing job or repositories",
				},
			},
		}, nil
	}

	focusPaths := ownershipFocusPaths(in.Contract)
	if len(focusPaths) == 0 {
		trace.Log(ctx, "contract", "contract.control_map.focus.empty", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType: h.ContractType(),
				GraphView:    models.GraphViewInfluenceNet,
				ResearchPlan: &models.ResearchPlan{
					Reason: "control focus entity not found in contract",
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
		trace.Log(ctx, "contract", "contract.control_map.focus.missing", map[string]any{
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
				GraphView:      models.GraphViewInfluenceNet,
				FocusNodePaths: focusPaths,
				ResearchPlan: &models.ResearchPlan{
					MissingNodes: focusPaths,
					Reason:       "focus entities not found in graph",
				},
			},
		}, nil
	}

	primaryTypes := []models.DependencyType{
		models.DependencyTypeControls,
		models.DependencyTypeManages,
		models.DependencyTypeAdministers,
	}
	supportingTypes := []models.DependencyType{
		models.DependencyTypeOwns,
		models.DependencyTypeInvestsIn,
		models.DependencyTypeHasSubgroup,
		models.DependencyTypeMemberOf,
		models.DependencyTypeAffiliatedWith,
		models.DependencyTypeOperatesVia,
	}
	edgeTypes := append(append([]models.DependencyType(nil), primaryTypes...), supportingTypes...)

	nodeCache := map[string]*models.HorizonFragment{}
	focusNodePaths := make([]string, 0, len(focusFragments))
	annotations := make([]models.Annotation, 0, 8)
	seenFocus := map[string]struct{}{}
	queue := make([]string, 0, len(focusFragments))
	visited := map[string]struct{}{}

	for id, frag := range focusFragments {
		queue = append(queue, id)
		nodeCache[id] = frag
		if frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
			cp := strings.TrimSpace(frag.CanonicalPath)
			if _, ok := seenFocus[cp]; !ok {
				seenFocus[cp] = struct{}{}
				focusNodePaths = append(focusNodePaths, cp)
			}
		}
	}

	maxDepth := 5
	edgeCount := 0
	for depth := 0; len(queue) > 0 && depth < maxDepth; depth++ {
		next := make([]string, 0, len(queue)*2)
		for _, nodeID := range queue {
			if _, ok := visited[nodeID]; ok {
				continue
			}
			visited[nodeID] = struct{}{}
			for _, edgeType := range edgeTypes {
				outEdges, _ := in.EdgesRepo.ListBySource(ctx, in.Job.TenantID, nodeID, &edgeType, 200)
				for _, edge := range outEdges {
					if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
						continue
					}
					edgeCount++
					appendControlAnnotations(edge, nodeCache, &annotations)
					addControlFocusNode(ctx, in.FragmentsRepo, edge.TargetFragmentID, nodeCache, seenFocus, &focusNodePaths)
					if _, ok := visited[edge.TargetFragmentID]; !ok {
						next = append(next, edge.TargetFragmentID)
					}
				}

				inEdges, _ := in.EdgesRepo.ListByTarget(ctx, in.Job.TenantID, nodeID, &edgeType, 200)
				for _, edge := range inEdges {
					if edge == nil || strings.TrimSpace(edge.SourceFragmentID) == "" {
						continue
					}
					edgeCount++
					appendControlAnnotations(edge, nodeCache, &annotations)
					addControlFocusNode(ctx, in.FragmentsRepo, edge.SourceFragmentID, nodeCache, seenFocus, &focusNodePaths)
					if _, ok := visited[edge.SourceFragmentID]; !ok {
						next = append(next, edge.SourceFragmentID)
					}
				}
			}
		}
		queue = next
	}

	if edgeCount == 0 {
		trace.Log(ctx, "contract", "contract.control_map.edges.missing", map[string]any{
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
				GraphView:      models.GraphViewInfluenceNet,
				FocusNodePaths: focusPaths,
				FilterRelTypes: []string{"CONTROLS", "MANAGES", "ADMINISTERS", "OWNS", "INVESTS_IN", "HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA"},
				ResearchPlan: &models.ResearchPlan{
					MissingNodes: focusPaths,
					Reason:       "no control or management edges found in graph",
				},
			},
		}, nil
	}

	summary := fmt.Sprintf("Control map computed from existing graph data (%d related edge(s)).", edgeCount)
	trace.Log(ctx, "contract", "contract.control_map.ok", map[string]any{
		"tenantId":     strings.TrimSpace(in.Job.TenantID),
		"projectId":    strings.TrimSpace(in.Job.ProjectID),
		"lensId":       strings.TrimSpace(in.Job.LensID),
		"jobId":        strings.TrimSpace(in.Job.ID),
		"contractType": h.ContractType(),
		"focusCount":   len(focusPaths),
		"edgeCount":    edgeCount,
	})

	return &pipeline.ContractOutput{
		ViewDirective: &models.ViewDirective{
			ContractType:   h.ContractType(),
			GraphView:      models.GraphViewInfluenceNet,
			FilterRelTypes: []string{"CONTROLS", "MANAGES", "ADMINISTERS", "OWNS", "INVESTS_IN", "HAS_SUBGROUP", "MEMBER_OF", "AFFILIATED_WITH", "OPERATES_VIA"},
			FocusNodePaths: dedupStrings(focusNodePaths),
			Annotations:    annotations,
			Summary:        summary,
		},
	}, nil
}

func addControlFocusNode(
	ctx context.Context,
	fragmentsRepo pipeline.FragmentsReader,
	fragmentID string,
	nodeCache map[string]*models.HorizonFragment,
	seenFocus map[string]struct{},
	focusNodePaths *[]string,
) {
	if strings.TrimSpace(fragmentID) == "" {
		return
	}
	frag := nodeCache[fragmentID]
	if frag == nil && fragmentsRepo != nil {
		loaded, err := fragmentsRepo.Get(ctx, fragmentID)
		if err == nil && loaded != nil {
			frag = loaded
			nodeCache[fragmentID] = loaded
		}
	}
	if frag == nil || strings.TrimSpace(frag.CanonicalPath) == "" {
		return
	}
	cp := strings.TrimSpace(frag.CanonicalPath)
	if _, ok := seenFocus[cp]; ok {
		return
	}
	seenFocus[cp] = struct{}{}
	*focusNodePaths = append(*focusNodePaths, cp)
}

func appendControlAnnotations(
	edge *models.DependencyEdge,
	nodeCache map[string]*models.HorizonFragment,
	annotations *[]models.Annotation,
) {
	if edge == nil || edge.Qualifiers == nil {
		return
	}
	target := nodeCache[edge.TargetFragmentID]
	if target == nil || strings.TrimSpace(target.CanonicalPath) == "" {
		return
	}
	for _, key := range []string{
		"control_type",
		"mechanism",
		"ownership_percent",
		"stake_pct",
		"voting_percent",
		"voting_rights_percent",
		"share_type",
	} {
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
