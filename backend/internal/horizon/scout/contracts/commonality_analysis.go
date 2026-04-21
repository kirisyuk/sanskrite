package contracts

import (
	"context"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"sort"
	"strings"
)

type CommonalityAnalysisHandler struct{}

type neighborInfo struct {
	EdgeTypes map[string]struct{}
}

func NewCommonalityAnalysisHandler() *CommonalityAnalysisHandler {
	return &CommonalityAnalysisHandler{}
}

func (h *CommonalityAnalysisHandler) ContractType() string {
	return "commonality-analysis"
}

func (h *CommonalityAnalysisHandler) Execute(ctx context.Context, eng pipeline.Engine, in pipeline.ContractInput) (*pipeline.ContractOutput, error) {
	if in.Job == nil || in.EdgesRepo == nil || in.FragmentsRepo == nil {
		if in.Job != nil {
			trace.Log(ctx, "contract", "contract.commonality.input.missing", map[string]any{
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

	focusPaths := commonalityFocusPaths(in.Contract, in.Job)
	if len(focusPaths) < 2 {
		trace.Log(ctx, "contract", "contract.commonality.focus.insufficient", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
			"focusCount":   len(focusPaths),
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType: h.ContractType(),
				GraphView:    models.GraphViewInfluenceNet,
				ResearchPlan: &models.ResearchPlan{
					Reason: "need at least two focus entities for commonality",
				},
			},
		}, nil
	}

	anchors := make([]*models.HorizonFragment, 0, len(focusPaths))
	for _, cp := range focusPaths {
		frag, err := in.FragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err == nil && frag != nil {
			anchors = append(anchors, frag)
		}
	}
	if len(anchors) < 2 {
		trace.Log(ctx, "contract", "contract.commonality.anchors.missing", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
			"focusCount":   len(focusPaths),
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

	neighborByAnchor := make([]map[string]*neighborInfo, 0, len(anchors))
	allNeighborIDs := map[string]struct{}{}

	for _, anchor := range anchors {
		neighbors := map[string]*neighborInfo{}
		edgesOut, _ := in.EdgesRepo.ListBySource(ctx, in.Job.TenantID, anchor.ID, nil, 200)
		edgesIn, _ := in.EdgesRepo.ListByTarget(ctx, in.Job.TenantID, anchor.ID, nil, 200)
		for _, edge := range append(edgesOut, edgesIn...) {
			if edge == nil {
				continue
			}
			edgeType := strings.TrimSpace(string(edge.Relation))
			if edgeType == "" || !commonalityEdgeTypeAllowed(edgeType) {
				continue
			}
			neighborID := edge.TargetFragmentID
			if edge.TargetFragmentID == anchor.ID {
				neighborID = edge.SourceFragmentID
			}
			if neighborID == "" || neighborID == anchor.ID {
				continue
			}
			info := neighbors[neighborID]
			if info == nil {
				info = &neighborInfo{EdgeTypes: map[string]struct{}{}}
				neighbors[neighborID] = info
			}
			info.EdgeTypes[edgeType] = struct{}{}
			allNeighborIDs[neighborID] = struct{}{}
		}
		neighborByAnchor = append(neighborByAnchor, neighbors)
	}

	type sharedInfo struct {
		count     int
		edgeTypes map[string]struct{}
	}
	shared := map[string]*sharedInfo{}
	for neighborID := range allNeighborIDs {
		count := 0
		edgeTypes := map[string]struct{}{}
		for _, neighbors := range neighborByAnchor {
			if _, ok := neighbors[neighborID]; ok {
				count++
				for et := range neighbors[neighborID].EdgeTypes {
					edgeTypes[et] = struct{}{}
				}
			}
		}
		if count >= 2 {
			shared[neighborID] = &sharedInfo{count: count, edgeTypes: edgeTypes}
		}
	}

	if len(shared) == 0 {
		trace.Log(ctx, "contract", "contract.commonality.shared.empty", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
			"anchorsCount": len(anchors),
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType:   h.ContractType(),
				GraphView:      models.GraphViewInfluenceNet,
				FocusNodePaths: focusPaths,
				ResearchPlan: &models.ResearchPlan{
					Reason: "no shared neighbors found in graph",
				},
			},
		}, nil
	}

	sharedIDs := make([]string, 0, len(shared))
	for id := range shared {
		sharedIDs = append(sharedIDs, id)
	}
	sort.Slice(sharedIDs, func(i, j int) bool {
		ai := shared[sharedIDs[i]]
		aj := shared[sharedIDs[j]]
		if ai == nil || aj == nil {
			return ai != nil
		}
		if ai.count == aj.count {
			li := len(ai.edgeTypes)
			lj := len(aj.edgeTypes)
			if li == lj {
				return sharedIDs[i] < sharedIDs[j]
			}
			return li > lj
		}
		return ai.count > aj.count
	})
	if len(sharedIDs) > 8 {
		sharedIDs = sharedIDs[:8]
	}

	frags, _ := in.FragmentsRepo.GetMany(ctx, sharedIDs)
	sharedPaths := make([]string, 0, len(sharedIDs))
	for _, id := range sharedIDs {
		if frag := frags[id]; frag != nil && frag.CanonicalPath != "" {
			if commonalityGenericPath(frag.CanonicalPath) {
				continue
			}
			sharedPaths = append(sharedPaths, frag.CanonicalPath)
		}
	}
	if len(sharedPaths) == 0 {
		trace.Log(ctx, "contract", "contract.commonality.shared.generic_only", map[string]any{
			"tenantId":     strings.TrimSpace(in.Job.TenantID),
			"projectId":    strings.TrimSpace(in.Job.ProjectID),
			"lensId":       strings.TrimSpace(in.Job.LensID),
			"jobId":        strings.TrimSpace(in.Job.ID),
			"contractType": h.ContractType(),
			"anchorsCount": len(anchors),
		})
		return &pipeline.ContractOutput{
			ViewDirective: &models.ViewDirective{
				ContractType:   h.ContractType(),
				GraphView:      models.GraphViewInfluenceNet,
				FocusNodePaths: focusPaths,
				ResearchPlan: &models.ResearchPlan{
					Reason: "shared neighbors are too generic to display",
				},
			},
		}, nil
	}

	highlightPaths := make([]models.HighlightPath, 0)
	for _, anchor := range anchors {
		if anchor.CanonicalPath == "" {
			continue
		}
		for _, sharedPath := range sharedPaths {
			highlightPaths = append(highlightPaths, models.HighlightPath{
				NodePaths: []string{anchor.CanonicalPath, sharedPath},
				Label:     "shared link",
				Style:     "secondary",
			})
		}
	}

	focusNodePaths := dedupStrings(append(focusPaths, sharedPaths...))
	filterRelTypes := commonRelationTypes(neighborByAnchor)

	summary := fmt.Sprintf("Found %d shared neighbor(s) across %d anchors.", len(sharedPaths), len(anchors))
	trace.Log(ctx, "contract", "contract.commonality.ok", map[string]any{
		"tenantId":       strings.TrimSpace(in.Job.TenantID),
		"projectId":      strings.TrimSpace(in.Job.ProjectID),
		"lensId":         strings.TrimSpace(in.Job.LensID),
		"jobId":          strings.TrimSpace(in.Job.ID),
		"contractType":   h.ContractType(),
		"anchorsCount":   len(anchors),
		"sharedCount":    len(sharedPaths),
		"highlightPaths": len(highlightPaths),
	})

	return &pipeline.ContractOutput{
		ViewDirective: &models.ViewDirective{
			ContractType:   h.ContractType(),
			GraphView:      models.GraphViewInfluenceNet,
			FocusNodePaths: focusNodePaths,
			HighlightPaths: highlightPaths,
			FilterRelTypes: filterRelTypes,
			Summary:        summary,
		},
	}, nil
}

func commonalityFocusPaths(contract *pipeline.DomainContract, job *models.ScoutingJob) []string {
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
	if len(out) > 0 {
		return dedupStrings(out)
	}
	if contract.CanonicalPath != "" {
		out = append(out, extractCanonicalPathsFromDomainPath(contract.CanonicalPath)...)
	}
	if len(out) > 0 {
		return dedupStrings(out)
	}
	if job != nil && job.State != nil {
		switch v := job.State["scope"].(type) {
		case []string:
			for _, cp := range v {
				cp = strings.TrimSpace(cp)
				if cp == "" {
					continue
				}
				if taxonomy.ValidateCanonicalPath(cp) == nil {
					out = append(out, cp)
				}
			}
		case []interface{}:
			for _, raw := range v {
				cp, ok := raw.(string)
				if !ok {
					continue
				}
				cp = strings.TrimSpace(cp)
				if cp == "" {
					continue
				}
				if taxonomy.ValidateCanonicalPath(cp) == nil {
					out = append(out, cp)
				}
			}
		}
	}
	return dedupStrings(out)
}

func commonRelationTypes(neighborByAnchor []map[string]*neighborInfo) []string {
	counts := map[string]int{}
	for _, neighbors := range neighborByAnchor {
		for _, info := range neighbors {
			for typ := range info.EdgeTypes {
				counts[typ]++
			}
		}
	}
	type pair struct {
		key string
		val int
	}
	var items []pair
	for k, v := range counts {
		items = append(items, pair{key: k, val: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].val == items[j].val {
			return items[i].key < items[j].key
		}
		return items[i].val > items[j].val
	})
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.key)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func commonalityEdgeTypeAllowed(edgeType string) bool {
	edgeType = strings.ToUpper(strings.TrimSpace(edgeType))
	if edgeType == "" {
		return false
	}
	switch edgeType {
	case string(ah.RelationNameIncorporatedIn),
		string(ah.RelationNameOperates),
		string(ah.RelationNameMemberOf),
		string(ah.RelationNameInfluences):
		return false
	default:
		return true
	}
}

func extractCanonicalPathsFromDomainPath(domainPath string) []string {
	path := strings.TrimSpace(domainPath)
	if path == "" || !strings.HasPrefix(path, "/domain/") {
		return nil
	}
	parts := strings.Split(path, "/")
	tokens := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		tokens = append(tokens, p)
	}
	if len(tokens) < 4 {
		return nil
	}
	contractType := tokens[2]
	facetKeys := map[string]struct{}{
		"subject": {}, "asset": {}, "organization": {}, "org": {}, "person": {},
		"geo": {}, "country": {}, "region": {}, "market": {}, "program": {}, "product": {},
	}
	namespaces := map[string]struct{}{
		"org": {}, "gov": {}, "person": {}, "product": {}, "program": {}, "market": {},
		"fac": {}, "loc": {}, "sphere": {}, "event": {}, "media": {},
	}
	out := make([]string, 0)
	for i := 3; i < len(tokens); i++ {
		if _, ok := namespaces[tokens[i]]; !ok {
			continue
		}
		j := i + 1
		for j < len(tokens) {
			if _, ok := facetKeys[tokens[j]]; ok {
				break
			}
			if tokens[j] == "domain" || tokens[j] == "v1" || tokens[j] == contractType {
				break
			}
			j++
		}
		cp := "/" + strings.Join(tokens[i:j], "/")
		if taxonomy.ValidateCanonicalPath(cp) == nil {
			out = append(out, cp)
		}
		i = j - 1
	}
	return out
}

func commonalityGenericPath(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	switch p {
	case "/geo/world", "/geo/global", "/geo/earth", "/geo/international":
		return true
	default:
		return false
	}
}
