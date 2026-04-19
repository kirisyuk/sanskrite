package contracts

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	basemodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type ownershipTestFragmentsRepo struct {
	byID   map[string]*models.HorizonFragment
	byPath map[string]*models.HorizonFragment
}

func (r ownershipTestFragmentsRepo) Get(_ context.Context, id string) (*models.HorizonFragment, error) {
	return r.byID[id], nil
}

func (r ownershipTestFragmentsRepo) GetByCanonicalPath(_ context.Context, canonicalPath string) (*models.HorizonFragment, error) {
	return r.byPath[canonicalPath], nil
}

func (r ownershipTestFragmentsRepo) GetMany(_ context.Context, ids []string) (map[string]*models.HorizonFragment, error) {
	out := make(map[string]*models.HorizonFragment, len(ids))
	for _, id := range ids {
		if frag := r.byID[id]; frag != nil {
			out[id] = frag
		}
	}
	return out, nil
}

type ownershipTestEdgesRepo struct {
	edges []*models.DependencyEdge
}

func (r ownershipTestEdgesRepo) ListBySource(_ context.Context, _ string, sourceFragmentID string, edgeType *DependencyType, limit int) ([]*models.DependencyEdge, error) {
	return r.filter(limit, func(edge *models.DependencyEdge) bool {
		return edge.SourceFragmentID == sourceFragmentID && (edgeType == nil || edge.Relation == *edgeType)
	}), nil
}

func (r ownershipTestEdgesRepo) ListByTarget(_ context.Context, _ string, targetFragmentID string, edgeType *DependencyType, limit int) ([]*models.DependencyEdge, error) {
	return r.filter(limit, func(edge *models.DependencyEdge) bool {
		return edge.TargetFragmentID == targetFragmentID && (edgeType == nil || edge.Relation == *edgeType)
	}), nil
}

func (r ownershipTestEdgesRepo) filter(limit int, keep func(edge *models.DependencyEdge) bool) []*models.DependencyEdge {
	out := make([]*models.DependencyEdge, 0)
	for _, edge := range r.edges {
		if edge == nil || !keep(edge) {
			continue
		}
		out = append(out, edge)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func TestOwnershipStructureHandler_ExpandsSupportLayersAndThinPlan(t *testing.T) {
	ctx := context.Background()
	handler := NewOwnershipStructureHandler()
	repos, edges := ownershipFixture()
	_ = edges

	out, err := handler.Execute(ctx, nil, pipeline.ContractInput{
		Job: &models.ScoutingJob{
			AgentJob: basemodels.AgentJob{TenantID: "tenant"},
		},
		Contract: &pipeline.DomainContract{
			ContractType:       "ownership-structure",
			FirstOrderEntities: []string{"/org/ae/difc-opco"},
		},
		FragmentsRepo: repos,
		EdgesRepo:     ownershipTestEdgesRepo{edges: edges},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out == nil || out.ViewDirective == nil {
		t.Fatalf("expected ownership view directive")
	}
	if !containsHighlightPath(out.ViewDirective.HighlightPaths, []string{
		"/org/ae/difc-opco",
		"/org/ae/difc-holdco",
		"/community/ae/dynasty/al-maktoum",
	}) {
		t.Fatalf("expected upstream ownership chain to dynasty")
	}
	for _, want := range []string{
		"/person/ae/sheikh-tariq",
		"/media/telegram/al-maktoum-majlis",
	} {
		if !containsString(out.ViewDirective.FocusNodePaths, want) {
			t.Fatalf("expected support node %q in focus nodes, got %v", want, out.ViewDirective.FocusNodePaths)
		}
	}
	for _, want := range []string{"OPERATES_VIA", "BOARD_MEMBER_OF", "EMPLOYED_BY"} {
		if !containsString(out.ViewDirective.FilterRelTypes, want) {
			t.Fatalf("expected filter rel type %q", want)
		}
	}
	if !containsAnnotation(out.ViewDirective.Annotations, "/org/ae/difc-holdco", "share_type", "beneficial") {
		t.Fatalf("expected beneficial share_type annotation on holdco")
	}
	if !containsAnnotationKey(out.ViewDirective.Annotations, "/org/ae/difc-opco", "voting_rights_percent") {
		t.Fatalf("expected voting_rights_percent annotation on opco")
	}
	if out.ViewDirective.ResearchPlan != nil {
		t.Fatalf("did not expect thin ownership research plan for rich ownership graph, got %#v", out.ViewDirective.ResearchPlan)
	}
}

func TestOwnershipStructureHandler_ShowsDownstreamOwnedBranchFromDynastyFocus(t *testing.T) {
	ctx := context.Background()
	handler := NewOwnershipStructureHandler()
	repos, edges := ownershipFixture()

	out, err := handler.Execute(ctx, nil, pipeline.ContractInput{
		Job: &models.ScoutingJob{
			AgentJob: basemodels.AgentJob{TenantID: "tenant"},
		},
		Contract: &pipeline.DomainContract{
			ContractType:       "ownership-structure",
			FirstOrderEntities: []string{"/community/ae/dynasty/al-maktoum"},
		},
		FragmentsRepo: repos,
		EdgesRepo:     ownershipTestEdgesRepo{edges: edges},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out == nil || out.ViewDirective == nil {
		t.Fatalf("expected ownership view directive")
	}
	if !containsHighlightPath(out.ViewDirective.HighlightPaths, []string{
		"/community/ae/dynasty/al-maktoum",
		"/org/ae/difc-holdco",
		"/org/ae/difc-opco",
	}) {
		t.Fatalf("expected downstream owned branch from dynasty focus")
	}
	if !containsString(out.ViewDirective.FocusNodePaths, "/org/ae/difc-opco") {
		t.Fatalf("expected downstream opco in focus nodes")
	}
}

func TestOwnershipStructureHandler_ThinOwnershipGraphReturnsResearchPlan(t *testing.T) {
	ctx := context.Background()
	handler := NewOwnershipStructureHandler()
	child := &models.HorizonFragment{ID: "child", CanonicalPath: "/org/ae/thin-opco", Class: FragmentTypeOrganization}
	parent := &models.HorizonFragment{ID: "parent", CanonicalPath: "/org/ae/thin-holdco", Class: FragmentTypeOrganization}
	repo := ownershipTestFragmentsRepo{
		byID:   map[string]*models.HorizonFragment{"child": child, "parent": parent},
		byPath: map[string]*models.HorizonFragment{child.CanonicalPath: child, parent.CanonicalPath: parent},
	}
	edges := []*models.DependencyEdge{
		{
			ID:               "thin-e1",
			SourceFragmentID: "parent",
			TargetFragmentID: "child",
			Relation:             DependencyTypeOwns,
		},
	}

	out, err := handler.Execute(ctx, nil, pipeline.ContractInput{
		Job: &models.ScoutingJob{
			AgentJob: basemodels.AgentJob{TenantID: "tenant"},
		},
		Contract: &pipeline.DomainContract{
			ContractType:       "ownership-structure",
			FirstOrderEntities: []string{child.CanonicalPath},
		},
		FragmentsRepo: repo,
		EdgesRepo:     ownershipTestEdgesRepo{edges: edges},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out == nil || out.ViewDirective == nil || out.ViewDirective.ResearchPlan == nil {
		t.Fatalf("expected research plan for thin ownership graph")
	}
	for _, want := range []string{child.CanonicalPath, parent.CanonicalPath} {
		if !containsString(out.ViewDirective.ResearchPlan.MissingNodes, want) {
			t.Fatalf("expected thin research plan to include %q, got %v", want, out.ViewDirective.ResearchPlan.MissingNodes)
		}
	}
}

func ownershipFixture() (ownershipTestFragmentsRepo, []*models.DependencyEdge) {
	fragments := []*models.HorizonFragment{
		{ID: "dynasty", CanonicalPath: "/community/ae/dynasty/al-maktoum", Class: FragmentTypeCommunity},
		{ID: "holdco", CanonicalPath: "/org/ae/difc-holdco", Class: FragmentTypeOrganization},
		{ID: "opco", CanonicalPath: "/org/ae/difc-opco", Class: FragmentTypeOrganization},
		{ID: "person", CanonicalPath: "/person/ae/sheikh-tariq", Class: FragmentTypePerson},
		{ID: "media", CanonicalPath: "/media/telegram/al-maktoum-majlis", Class: ah.FragmentClassOrg, Subtype: "media_channel"},
	}
	repo := ownershipTestFragmentsRepo{
		byID:   make(map[string]*models.HorizonFragment, len(fragments)),
		byPath: make(map[string]*models.HorizonFragment, len(fragments)),
	}
	for _, frag := range fragments {
		repo.byID[frag.ID] = frag
		repo.byPath[frag.CanonicalPath] = frag
	}

	edges := []*models.DependencyEdge{
		{
			ID:               "e1",
			SourceFragmentID: "dynasty",
			TargetFragmentID: "holdco",
			Relation:             DependencyTypeOwns,
			Qualifiers: map[string]interface{}{
				"share_type":        "beneficial",
				"ownership_percent": "87",
			},
			Metrics: map[string]models.MetricValue{
				"ownership_percent": {Value: 87},
			},
		},
		{
			ID:               "e2",
			SourceFragmentID: "holdco",
			TargetFragmentID: "opco",
			Relation:             DependencyTypeOwns,
			Qualifiers: map[string]interface{}{
				"share_type":            "direct",
				"ownership_percent":     "100",
				"voting_rights_percent": "100",
				"control_type":          "board",
				"mechanism":             "board_majority",
			},
			Metrics: map[string]models.MetricValue{
				"ownership_percent":     {Value: 100},
				"voting_rights_percent": {Value: 100},
			},
		},
		{
			ID:               "e3",
			SourceFragmentID: "person",
			TargetFragmentID: "dynasty",
			Relation:             DependencyTypeMemberOf,
		},
		{
			ID:               "e4",
			SourceFragmentID: "person",
			TargetFragmentID: "holdco",
			Relation:             DependencyTypeBoardMemberOf,
		},
		{
			ID:               "e5",
			SourceFragmentID: "person",
			TargetFragmentID: "opco",
			Relation:             DependencyTypeEmployedBy,
		},
		{
			ID:               "e6",
			SourceFragmentID: "dynasty",
			TargetFragmentID: "media",
			Relation:             DependencyTypeOperatesVia,
		},
	}
	return repo, edges
}

func containsHighlightPath(paths []models.HighlightPath, want []string) bool {
	for _, path := range paths {
		if len(path.NodePaths) != len(want) {
			continue
		}
		match := true
		for i := range want {
			if path.NodePaths[i] != want[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsAnnotation(in []models.Annotation, canonicalPath, key, value string) bool {
	for _, ann := range in {
		if ann.CanonicalPath == canonicalPath && ann.Key == key && ann.Value == value {
			return true
		}
	}
	return false
}

func containsAnnotationKey(in []models.Annotation, canonicalPath, key string) bool {
	for _, ann := range in {
		if ann.CanonicalPath == canonicalPath && ann.Key == key {
			return true
		}
	}
	return false
}

func containsString(in []string, want string) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}
