package pathquery

import (
	"context"
	"errors"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type fakeExposureRepo struct {
	items []*horizonmodels.ExposurePathArtifact
	err   error
}

func (f *fakeExposureRepo) ListByLens(ctx context.Context, tenantID, lensID string, typ *horizonmodels.ExposureType, limit int) ([]*horizonmodels.ExposurePathArtifact, error) {
	if f == nil {
		return nil, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func TestExposureMatchesQuery_toPresent(t *testing.T) {
	exp := &horizonmodels.ExposurePathArtifact{
		ObjectiveID: "/org/a",
		RelatedID:   "/org/b",
	}
	nq := &normalizedQuery{
		ToPresent:      true,
		FromCanonical:  "/org/a",
		ToCanonical:    "/org/b",
		FromFragmentID: "",
	}
	if !exposureMatchesQuery(exp, nq) {
		t.Fatal("expected match")
	}
	nq.ToCanonical = "/org/c"
	if exposureMatchesQuery(exp, nq) {
		t.Fatal("expected no match")
	}
}

func TestExposureMatchesQuery_openEnded(t *testing.T) {
	exp := &horizonmodels.ExposurePathArtifact{ObjectiveID: "/org/a"}
	nq := &normalizedQuery{ToPresent: false, FromCanonical: "/org/a"}
	if !exposureMatchesQuery(exp, nq) {
		t.Fatal("expected match")
	}
}

func TestMergeExposurePathFamilies_addsPaths(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
		"/org/b": {ID: "fb", CanonicalPath: "/org/b", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/b"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m-exp",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/b"},
		Payload:  payload,
	}
	mat := &memMat{
		byNode: map[string][]*horizonmodels.MaterializedArtifact{
			"/org/b": {art},
		},
		byID: map[string]*horizonmodels.MaterializedArtifact{"m-exp": art},
	}
	exposure := &fakeExposureRepo{
		items: []*horizonmodels.ExposurePathArtifact{
			{
				TenantID:      "t",
				ProjectID:     "p",
				LensID:        "l1",
				Type:          horizonmodels.ExposureTypeSupplyChainRisk,
				ObjectiveID:   "/org/a",
				RelatedID:     "/org/b",
				ObjectiveKind: "org",
				RelatedKind:   "org",
			},
		},
	}
	s := NewService(fr, nil, mat, nil, nil).WithExposurePaths(exposure)
	q := Query{
		Tenant: "t",
		From:   Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		To:     &Endpoint{Kind: EndpointCanonicalPath, Value: "/org/b"},
		Context: SearchContext{
			LensID: "l1",
		},
		CacheMode: CacheModeDisabled,
	}
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.resolveEndpoints(context.Background(), &nq); err != nil {
		t.Fatal(err)
	}
	paths := []*horizonmodels.MaterializedArtifact{}
	diag := Diagnostics{}
	out := s.mergeExposurePathFamilies(context.Background(), q, &nq, paths, &diag)
	if len(out) != 1 || out[0].ID != "m-exp" {
		t.Fatalf("got %v", out)
	}
	if diag.ExposurePathFamilies != 1 {
		t.Fatalf("diag=%+v", diag)
	}
}

func TestMergeExposurePathFamilies_listErrorWarning(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a"},
	}}
	mat := &memMat{}
	exposure := &fakeExposureRepo{err: errors.New("boom")}
	s := NewService(fr, nil, mat, nil, nil).WithExposurePaths(exposure)
	q := Query{
		Tenant:  "t",
		From:    Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		Context: SearchContext{LensID: "l1"},
	}
	nq, err := normalizeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.resolveEndpoints(context.Background(), &nq)
	d := Diagnostics{}
	out := s.mergeExposurePathFamilies(context.Background(), q, &nq, nil, &d)
	if len(out) != 0 {
		t.Fatalf("expected no paths")
	}
	if len(d.Warnings) != 1 {
		t.Fatalf("warnings=%v", d.Warnings)
	}
}

func TestWithExposurePaths_nilService(t *testing.T) {
	var s *Service
	if s.WithExposurePaths(&fakeExposureRepo{}) != nil {
		t.Fatal()
	}
}
