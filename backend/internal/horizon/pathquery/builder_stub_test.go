package pathquery

import (
	"context"
	"errors"
	"fmt"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

type stubUpstream struct {
	paths        []*pathbuilder.SupplyChainPath
	buildErr     error
	artifacts    []*horizonmodels.MaterializedArtifact
	prepareErr   error
}

func (s *stubUpstream) BuildUpstreamPaths(ctx context.Context, customerRef string, opts pathbuilder.BuildPathOptions) ([]*pathbuilder.SupplyChainPath, error) {
	if s.buildErr != nil {
		return nil, s.buildErr
	}
	return s.paths, nil
}

func (s *stubUpstream) PrepareMaterializedPathArtifactsBatch(ctx context.Context, paths []*pathbuilder.SupplyChainPath, tenantID, lensID string, canonicalKeys []string) ([]*horizonmodels.MaterializedArtifact, []string, error) {
	if s.prepareErr != nil {
		return nil, nil, s.prepareErr
	}
	return s.artifacts, nil, nil
}

func TestExtendWithPathbuilder_stubSuccess(t *testing.T) {
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
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/b"}
	art := &horizonmodels.MaterializedArtifact{
		ID:       "stub1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		Payload:  horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil),
	}
	stub := &stubUpstream{
		paths: []*pathbuilder.SupplyChainPath{
			{Segments: []pathbuilder.PathSegment{{SupplierRef: "/org/a", CustomerRef: "/org/b", ProductRef: "/p/x"}}},
		},
		artifacts: []*horizonmodels.MaterializedArtifact{art},
	}
	s := NewService(fr, edges, mat, nil, stub)
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
	if len(out) != 1 || out[0].ID != "stub1" {
		t.Fatalf("got %v", out)
	}
}

func TestExtendWithPathbuilder_buildError(t *testing.T) {
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
	stub := &stubUpstream{buildErr: errors.New("no paths")}
	s := NewService(fr, edges, &memMat{}, nil, stub)
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
		t.Fatalf("expected empty, got %v", out)
	}
}

func TestExtendWithPathbuilder_prepareErrorWarning(t *testing.T) {
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
	stub := &stubUpstream{
		paths:      []*pathbuilder.SupplyChainPath{{Segments: []pathbuilder.PathSegment{{SupplierRef: "/org/a", CustomerRef: "/org/b"}}}},
		prepareErr: errors.New("prepare failed"),
	}
	s := NewService(fr, edges, &memMat{}, nil, stub)
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
		t.Fatal()
	}
	if len(d.Warnings) != 1 {
		t.Fatalf("warnings=%v", d.Warnings)
	}
}

func TestExtendWithPathbuilder_truncationCap(t *testing.T) {
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
	var many []*pathbuilder.SupplyChainPath
	for i := 0; i < maxOpenEndedPaths+5; i++ {
		many = append(many, &pathbuilder.SupplyChainPath{
			Segments: []pathbuilder.PathSegment{{SupplierRef: "/org/a", CustomerRef: "/org/b", ProductRef: "/p/x"}},
		})
	}
	emptyPayload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, nil, nil, nil, nil, nil)
	arts := make([]*horizonmodels.MaterializedArtifact, 0, maxOpenEndedPaths)
	for i := 0; i < maxOpenEndedPaths; i++ {
		arts = append(arts, &horizonmodels.MaterializedArtifact{
			ID:       fmt.Sprintf("stub-%d", i),
			TenantID: "t",
			Kind:     horizonmodels.MaterializedKindSupplyChainPath,
			Payload:  emptyPayload,
		})
	}
	stub := &stubUpstream{paths: many, artifacts: arts}
	s := NewService(fr, edges, &memMat{}, nil, stub)
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
	if !d.TruncatedResultCap {
		t.Fatal("expected truncation")
	}
	if len(out) != maxOpenEndedPaths {
		t.Fatalf("len=%d", len(out))
	}
}
