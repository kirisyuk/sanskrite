package pathquery

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// fakeSearchMat implements SupplyChainSearchMaterialized and FragmentTraceMaterialized for tests.
type fakeSearchMat struct {
	*memMat
	listByLens        []*horizonmodels.MaterializedArtifact
	listByLensEmpty   bool
	listTenantKind    []*horizonmodels.MaterializedArtifact
	nodeRefsAny       []*horizonmodels.MaterializedArtifact
	affectedZoneAny   []*horizonmodels.MaterializedArtifact
	byCanonicalAny    *horizonmodels.MaterializedArtifact
}

func (f *fakeSearchMat) ListByLens(ctx context.Context, lensID string, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	if f.listByLensEmpty {
		return nil, nil
	}
	return f.listByLens, nil
}

func (f *fakeSearchMat) GetByCanonicalKeyAnyKind(ctx context.Context, tenantID, key string) (*horizonmodels.MaterializedArtifact, error) {
	return f.byCanonicalAny, nil
}

func (f *fakeSearchMat) FindByNodeRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return f.nodeRefsAny, nil
}

func (f *fakeSearchMat) FindByFlowRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (f *fakeSearchMat) FindByZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return nil, nil
}

func (f *fakeSearchMat) FindByAffectedZoneRefsAny(ctx context.Context, tenantID string, refs []string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return f.affectedZoneAny, nil
}

func (f *fakeSearchMat) ListByTenantAndKind(ctx context.Context, tenantID string, kind horizonmodels.MaterializedKind, limit int) ([]*horizonmodels.MaterializedArtifact, error) {
	return f.listTenantKind, nil
}

type fakeLensGetter struct {
	lens *horizonmodels.Lens
}

func (f *fakeLensGetter) Get(ctx context.Context, id string) (*horizonmodels.Lens, error) {
	return f.lens, nil
}

func TestSearchSupplyChainAnchors_withLens(t *testing.T) {
	pinned := &horizonmodels.MaterializedArtifact{
		ID:       "pinned",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		IsPinned: true,
		NodeRefs: []string{"/org/x"},
	}
	mat := &fakeSearchMat{
		memMat:     &memMat{},
		listByLens: []*horizonmodels.MaterializedArtifact{pinned},
	}
	lens := &horizonmodels.Lens{ID: "L1", TenantID: "t"}
	lg := &fakeLensGetter{lens: lens}
	out, err := SearchSupplyChainAnchors(context.Background(), mat, lg, "t", []string{"/org/x"}, "L1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "pinned" {
		t.Fatalf("%v", out)
	}
}

func TestSearchSupplyChainAnchors_supplyLinkList(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplyLinkCanonicalPath: "/supply-link/foo"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	a := &horizonmodels.MaterializedArtifact{ID: "sl", Kind: horizonmodels.MaterializedKindSupplyChainPath, Payload: payload}
	mat := &fakeSearchMat{
		memMat:         &memMat{},
		listTenantKind: []*horizonmodels.MaterializedArtifact{a},
	}
	out, err := SearchSupplyChainAnchors(context.Background(), mat, nil, "t", []string{"/supply-link/foo"}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("%v", out)
	}
}

func TestSearchSupplyChainAnchors_findByNode(t *testing.T) {
	a := &horizonmodels.MaterializedArtifact{ID: "n", Kind: horizonmodels.MaterializedKindSupplyChainPath, NodeRefs: []string{"/org/a"}}
	mat := &fakeSearchMat{
		memMat: &memMat{
			byNode: map[string][]*horizonmodels.MaterializedArtifact{"/org/a": {a}},
			byID:   map[string]*horizonmodels.MaterializedArtifact{"n": a},
		},
	}
	out, err := SearchSupplyChainAnchors(context.Background(), mat, nil, "t", []string{"/org/a"}, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("%v", out)
	}
}

func TestCollectFragmentTraceSupplyChains_basic(t *testing.T) {
	leg := horizonmodels.SupplyChainPathLeg{Index: 0, SupplierRef: "/zone/z"}
	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(nil, nil, []horizonmodels.SupplyChainPathLeg{leg}, nil, nil, nil, nil)
	chain := &horizonmodels.MaterializedArtifact{
		ID:       "c1",
		Kind:     horizonmodels.MaterializedKindSupplyChainPath,
		NodeRefs: []string{"/zone/z"},
		Payload:  payload,
	}
	mat := &fakeSearchMat{
		memMat: &memMat{
			byNode: map[string][]*horizonmodels.MaterializedArtifact{"/zone/z": {chain}},
			byID:   map[string]*horizonmodels.MaterializedArtifact{"c1": chain},
		},
	}
	frag := &horizonmodels.HorizonFragment{TenantID: "t", CanonicalPath: "/zone/z"}
	out, err := CollectFragmentTraceSupplyChains(context.Background(), mat, nil, frag, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("%v", out)
	}
}

func TestCollectSupplyChainsForLens_scopedBranch(t *testing.T) {
	pinned := &horizonmodels.MaterializedArtifact{ID: "p1", Kind: horizonmodels.MaterializedKindSupplyChainPath}
	mat := &fakeSearchMat{
		memMat:     &memMat{byID: map[string]*horizonmodels.MaterializedArtifact{"p1": pinned}},
		listByLens: []*horizonmodels.MaterializedArtifact{pinned},
	}
	lens := &horizonmodels.Lens{ID: "L2", TenantID: "t"}
	scoped, disc, err := CollectSupplyChainsForLens(context.Background(), mat, lens, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 1 || len(disc) != 0 {
		t.Fatalf("scoped=%v disc=%v", scoped, disc)
	}
}

func TestCollectSupplyChainsForLens_discoveredOverlap(t *testing.T) {
	disc := &horizonmodels.MaterializedArtifact{ID: "d1", Kind: horizonmodels.MaterializedKindSupplyChainPath, NodeRefs: []string{"/org/n"}}
	mat := &fakeSearchMat{
		memMat:          &memMat{byID: map[string]*horizonmodels.MaterializedArtifact{"d1": disc}},
		listByLensEmpty: true,
		nodeRefsAny:     []*horizonmodels.MaterializedArtifact{disc},
	}
	lens := &horizonmodels.Lens{
		ID:       "L4",
		TenantID: "t",
		ScopeArtifacts: map[string]horizonmodels.ScopeArtifactRef{
			"k": {NodeRefs: []string{"/org/n"}},
		},
	}
	scoped, discovered, err := CollectSupplyChainsForLens(context.Background(), mat, lens, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped) != 0 || len(discovered) != 1 {
		t.Fatalf("scoped=%d discovered=%d", len(scoped), len(discovered))
	}
}

func TestLensHelpers_intersectAndSort(t *testing.T) {
	if intersectCount([]string{"a", "b"}, []string{"b"}) != 1 {
		t.Fatal()
	}
	if len(firstIntersect([]string{"a", "b"}, []string{"b"}, 1)) != 1 {
		t.Fatal()
	}
	ctx := buildSupplyChainLensContext(&horizonmodels.Lens{ID: "x"})
	_ = ctx
	art := []*horizonmodels.MaterializedArtifact{{TracePriority: 2}, {TracePriority: 3}}
	sortSupplyChainArtifacts(art)
	if art[0].TracePriority != 3 {
		t.Fatal()
	}
	if artifactPathStressScore(&horizonmodels.MaterializedArtifact{}) != 0 {
		t.Fatal()
	}
}
