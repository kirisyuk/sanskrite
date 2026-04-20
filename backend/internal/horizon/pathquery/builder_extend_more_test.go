package pathquery

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestExtendWithPathbuilder_bfsUnreachable(t *testing.T) {
	fr := &memFrags{
		byID: map[string]*horizonmodels.HorizonFragment{
			"fa": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
			"fb": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
		},
	}
	mat := &memMat{}
	pb := pathbuilder.NewSupplyChainPathBuilder(nil, nil, nil)
	s := NewService(fr, &memEdges{}, mat, nil, pb)
	nq, err := normalizeQuery(Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointFragmentID, Value: "fa"},
		To:     &Endpoint{Kind: EndpointFragmentID, Value: "fb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.resolveEndpoints(context.Background(), &nq); err != nil {
		t.Fatal(err)
	}
	d := Diagnostics{}
	out := s.extendWithPathbuilder(context.Background(), &nq, &d)
	if len(out) != 0 {
		t.Fatalf("expected no paths when BFS unreachable, got %d", len(out))
	}
}

func TestExtendWithPathbuilder_upstreamAttempt(t *testing.T) {
	rel := ah.RelationNameSupplies
	fr := &memFrags{
		byID: map[string]*horizonmodels.HorizonFragment{
			"fa": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
			"fb": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
		},
	}
	edges := &memEdges{
		out: map[string][]*horizonmodels.DependencyEdge{
			"fa": {{SourceFragmentID: "fa", TargetFragmentID: "fb", Relation: rel}},
		},
	}
	mat := &memMat{}
	pb := pathbuilder.NewSupplyChainPathBuilder(nil, nil, nil)
	s := NewService(fr, edges, mat, nil, pb)
	nq, err := normalizeQuery(Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointFragmentID, Value: "fa"},
		To:     &Endpoint{Kind: EndpointFragmentID, Value: "fb"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.resolveEndpoints(context.Background(), &nq); err != nil {
		t.Fatal(err)
	}
	d := Diagnostics{}
	out := s.extendWithPathbuilder(context.Background(), &nq, &d)
	// Nil fragment/edge repos: builder returns empty or error — extend should not panic.
	_ = out
}
