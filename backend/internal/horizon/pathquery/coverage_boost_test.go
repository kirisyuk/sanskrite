package pathquery

import (
	"context"
	"errors"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestTraceContainsString(t *testing.T) {
	if traceContainsString([]string{"  a ", "b"}, "a") != true {
		t.Fatal()
	}
	if traceContainsString([]string{"x"}, "y") {
		t.Fatal()
	}
	if traceContainsString([]string{"x"}, "") {
		t.Fatal()
	}
}

func TestSortSupplyChainArtifacts(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	newer := time.Now().UTC()
	a := &horizonmodels.MaterializedArtifact{
		ID: "a", TracePriority: 1, IsPinned: false, OverlapScore: 0.5, UpdatedAt: newer,
	}
	b := &horizonmodels.MaterializedArtifact{
		ID: "b", TracePriority: 2, IsPinned: false, OverlapScore: 0.5, UpdatedAt: old,
	}
	c := &horizonmodels.MaterializedArtifact{ID: "c"} // nil-safe branch with another nil
	list := []*horizonmodels.MaterializedArtifact{c, nil, a, b}
	sortSupplyChainArtifacts(list)
	// b before a (higher TracePriority)
	if list[0] != b {
		t.Fatalf("order %v %v", list[0].ID, list[1].ID)
	}
}

func TestSearchSupplyChainAnchors_supplyLinkTenantList(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{SupplyLinkCanonicalPath: "/supply-link/sl1"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		Payload:  payload,
	}
	mat := &fakeSearchMat{
		memMat:         &memMat{},
		listTenantKind: []*horizonmodels.MaterializedArtifact{art},
	}
	out, err := SearchSupplyChainAnchors(context.Background(), mat, nil, "t", []string{"/supply-link/sl1"}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("%v", out)
	}
}

func TestSearchSupplyChainAnchors_findByNodeRefError(t *testing.T) {
	mat := &fakeSearchMat{memMat: &memMat{}}
	mat.byNode = map[string][]*horizonmodels.MaterializedArtifact{} // not used
	// memMat.FindByNodeRef returns nil,nil - force error by using a custom mat
	errMat := &errNodeRefMat{err: errors.New("nope")}
	_, err := SearchSupplyChainAnchors(context.Background(), errMat, nil, "t", []string{"/org/a"}, "", 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

type errNodeRefMat struct {
	err error
}

func (e *errNodeRefMat) FindByNodeRef(ctx context.Context, tenantID string, path string, kind horizonmodels.MaterializedKind) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, e.err
}

func (e *errNodeRefMat) Get(ctx context.Context, id string) (*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) GetByCanonicalKeyAnyKind(ctx context.Context, tenantID, key string) (*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) FindByNodeRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) FindByFlowRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) FindByZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) FindByAffectedZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (e *errNodeRefMat) ListByTenantAndKind(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func TestQueryPaths_cacheGetErrorStillSucceeds(t *testing.T) {
	fr := &memFrags{byCP: map[string]*horizonmodels.HorizonFragment{
		"/org/a": {ID: "fa", CanonicalPath: "/org/a", TenantID: "t"},
	}}
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/a", CustomerRef: "/org/z"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:       "m1",
		TenantID: "t",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/org/a"},
		Payload:  payload,
	}
	mat := &memMat{byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {art}}}
	s := NewService(fr, nil, mat, nil, nil).WithPathCacheForTest(errGetCache{err: errors.New("firestore down")})
	res, err := s.QueryPaths(context.Background(), Query{
		Tenant:    "t",
		From:      Endpoint{Kind: EndpointCanonicalPath, Value: "/org/a"},
		CacheMode: CacheModeDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Paths) != 1 {
		t.Fatalf("%v", res.Paths)
	}
}

type errGetCache struct {
	err error
}

func (e errGetCache) getDoc(ctx context.Context, cacheKey string) (*pathCacheDocument, error) {
	return nil, e.err
}

func (e errGetCache) setDoc(ctx context.Context, cacheKey string, doc pathCacheDocument) error {
	return nil
}

func (e errGetCache) deleteDoc(ctx context.Context, cacheKey string) error {
	return nil
}

func (e errGetCache) deleteByNode(ctx context.Context, tenantID, canonicalPath string) error {
	return nil
}

func TestExposureTouchesFragment_hit(t *testing.T) {
	exp := &horizonmodels.ExposurePathArtifact{NodeIDs: []string{"frag-9"}}
	if !exposureTouchesFragment(exp, "frag-9") {
		t.Fatal()
	}
	if exposureTouchesFragment(exp, "other") {
		t.Fatal()
	}
}

func TestResolveLookupCanonicals_fragment(t *testing.T) {
	fr := &memFrags{byID: map[string]*horizonmodels.HorizonFragment{
		"fid": {ID: "fid", CanonicalPath: "/org/z"},
	}}
	s := NewService(fr, nil, &memMat{}, nil, nil)
	out := s.resolveLookupCanonicals(context.Background(), "t", "fid")
	if len(out) != 1 || out[0] != "/org/z" {
		t.Fatalf("%v", out)
	}
}

func TestCollectFragmentTraceSupplyChains_affectedZone(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/zone/z1", CustomerRef: "/org/c"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	art := &horizonmodels.MaterializedArtifact{
		ID:               "az1",
		Kind:             horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs:         []string{},
		Payload:          payload,
		AffectedZoneRefs: []string{"/geo/affected/x"},
	}
	mat := &fakeSearchMat{
		memMat: &memMat{
			byNode: map[string][]*horizonmodels.MaterializedArtifact{},
		},
		affectedZoneAny: []*horizonmodels.MaterializedArtifact{art},
	}
	frag := &horizonmodels.HorizonFragment{
		TenantID:       "t",
		CanonicalPath:  "/geo/affected/x",
		Class:          ah.FragmentClassGeo,
		Subtype:        "affected_zone",
	}
	out, err := CollectFragmentTraceSupplyChains(context.Background(), mat, nil, frag, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("%v", out)
	}
}

func TestCollectFragmentTraceSupplyChains_withLensPinned(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/org/x", CustomerRef: "/org/y"}
	nodes := []horizonmodels.SupplyChainPathNode{{Type: "org", Ref: "/org/x"}}
	pinned := &horizonmodels.MaterializedArtifact{
		ID:       "pinned",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		IsPinned: true,
		Payload:  horizonmodels.BuildSupplyChainPathPayloadDetailed(nodes, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil),
	}
	mat := &fakeSearchMat{
		memMat:     &memMat{},
		listByLens: []*horizonmodels.MaterializedArtifact{pinned},
	}
	frag := &horizonmodels.HorizonFragment{TenantID: "t", CanonicalPath: "/org/x"}
	lg := &fakeLensGetter{lens: &horizonmodels.Lens{ID: "L1", TenantID: "t"}}
	out, err := CollectFragmentTraceSupplyChains(context.Background(), mat, lg, frag, "L1")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("%v", out)
	}
}

func TestPathTouchesFromCanonical_productRef(t *testing.T) {
	p := &pathbuilder.SupplyChainPath{
		Segments: []pathbuilder.PathSegment{{ProductRef: "/product/p1"}},
	}
	if !pathTouchesFromCanonical(p, "/product/p1") {
		t.Fatal()
	}
}
