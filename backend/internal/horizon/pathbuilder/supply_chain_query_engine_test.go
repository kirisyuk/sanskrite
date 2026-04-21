package pathbuilder

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestTraceWithSnapshot_ExactProductChain(t *testing.T) {
	engine := NewSupplyChainQueryEngine(nil, nil)
	snapshot := engine.snapshotFromLinks([]*models.HorizonFragment{
		testSupplyLinkFragment(
			"s1",
			"/supply-link/global/supplier-a/enzyme/processor-b",
			"/org/us/supplier-a",
			"/product/chemical/enzyme/industrial/enzyme-a",
			"/org/us/processor-b",
		),
		testSupplyLinkFragment(
			"s2",
			"/supply-link/global/processor-b/enzyme/customer-c",
			"/org/us/processor-b",
			"/product/chemical/enzyme/industrial/enzyme-a",
			"/org/us/customer-c",
		),
	})

	res, err := engine.traceWithSnapshot(
		context.Background(),
		snapshot,
		"/product/chemical/enzyme/industrial/enzyme-a",
		map[string]struct{}{"/product/chemical/enzyme/industrial/enzyme-a": {}},
		DefaultSupplyChainQueryOptions(),
		false,
	)
	if err != nil {
		t.Fatalf("traceWithSnapshot() error = %v", err)
	}
	if len(res.Paths) != 1 {
		t.Fatalf("paths=%d, want 1", len(res.Paths))
	}
	if len(res.Paths[0].Segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(res.Paths[0].Segments))
	}
	if got := res.Paths[0].Segments[0].SupplierRef; got != "/org/us/supplier-a" {
		t.Fatalf("first supplier=%q", got)
	}
	if got := res.Paths[0].Segments[1].CustomerRef; got != "/org/us/customer-c" {
		t.Fatalf("last customer=%q", got)
	}
	if len(res.Families) != 1 || res.Families[0].PathCount != 1 {
		t.Fatalf("families=%+v", res.Families)
	}
}

func TestTraceWithSnapshot_DownstreamTransformChain(t *testing.T) {
	engine := NewSupplyChainQueryEngine(nil, nil)
	snapshot := engine.snapshotFromLinks([]*models.HorizonFragment{
		testSupplyLinkFragment(
			"s1",
			"/supply-link/global/supplier-a/enzyme/plant-b",
			"/org/us/supplier-a",
			"/product/chemical/enzyme/industrial/enzyme-a",
			"/org/us/plant-b",
		),
		testSupplyLinkFragment(
			"s2",
			"/supply-link/global/plant-b/patch/distributor-c",
			"/org/us/plant-b",
			"/product/finished/medical/assembly/enzyme-patch",
			"/org/us/distributor-c",
		),
	})
	snapshot.ProductDownstream["/product/chemical/enzyme/industrial/enzyme-a"] = map[string]struct{}{
		"/product/finished/medical/assembly/enzyme-patch": {},
	}
	snapshot.ProductUpstream["/product/finished/medical/assembly/enzyme-patch"] = map[string]struct{}{
		"/product/chemical/enzyme/industrial/enzyme-a": {},
	}

	opts := DefaultSupplyChainQueryOptions()
	opts.ClosureMode = ProductClosureDownstream
	res, err := engine.traceWithSnapshot(
		context.Background(),
		snapshot,
		"/product/chemical/enzyme/industrial/enzyme-a",
		map[string]struct{}{
			"/product/chemical/enzyme/industrial/enzyme-a":    {},
			"/product/finished/medical/assembly/enzyme-patch": {},
		},
		opts,
		false,
	)
	if err != nil {
		t.Fatalf("traceWithSnapshot() error = %v", err)
	}
	if len(res.Paths) != 1 {
		t.Fatalf("paths=%d, want 1", len(res.Paths))
	}
	if len(res.Paths[0].Segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(res.Paths[0].Segments))
	}
	if res.Paths[0].Family == nil {
		t.Fatal("expected family metadata")
	}
	if got := res.Paths[0].Family.ProductAxis; got != "/product/finished/medical/assembly/enzyme-patch" {
		t.Fatalf("family product axis=%q", got)
	}
}

func TestTraceWithSnapshot_FiltersByActorEndpoints(t *testing.T) {
	engine := NewSupplyChainQueryEngine(nil, nil)
	snapshot := engine.snapshotFromLinks([]*models.HorizonFragment{
		testSupplyLinkFragment(
			"s1",
			"/supply-link/global/a/enzyme/b",
			"/org/us/company-a",
			"/product/chemical/enzyme/industrial/enzyme-a",
			"/org/us/company-b",
		),
		testSupplyLinkFragment(
			"s2",
			"/supply-link/global/b/enzyme/c",
			"/org/us/company-b",
			"/product/chemical/enzyme/industrial/enzyme-a",
			"/org/us/company-c",
		),
		testSupplyLinkFragment(
			"s3",
			"/supply-link/global/x/enzyme/y",
			"/org/us/company-x",
			"/product/chemical/enzyme/industrial/enzyme-a",
			"/org/us/company-y",
		),
	})

	opts := DefaultSupplyChainQueryOptions()
	opts.SupplierRef = "/org/us/company-a"
	opts.CustomerRef = "/org/us/company-c"
	res, err := engine.traceWithSnapshot(
		context.Background(),
		snapshot,
		"/product/chemical/enzyme/industrial/enzyme-a",
		map[string]struct{}{"/product/chemical/enzyme/industrial/enzyme-a": {}},
		opts,
		false,
	)
	if err != nil {
		t.Fatalf("traceWithSnapshot() error = %v", err)
	}
	if len(res.Paths) != 1 {
		t.Fatalf("paths=%d, want 1", len(res.Paths))
	}
	if len(res.Paths[0].Segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(res.Paths[0].Segments))
	}
	if got := res.Paths[0].Segments[0].SupplierRef; got != "/org/us/company-a" {
		t.Fatalf("supplier=%q", got)
	}
	if got := res.Paths[0].Segments[1].CustomerRef; got != "/org/us/company-c" {
		t.Fatalf("customer=%q", got)
	}
}

func TestSnapshotFromLinks_ExplodesRouteVariants(t *testing.T) {
	engine := NewSupplyChainQueryEngine(nil, nil)
	frag := testSupplyLinkFragment(
		"s1",
		"/supply-link/global/a/enzyme/b",
		"/org/us/company-a",
		"/product/chemical/enzyme/industrial/enzyme-a",
		"/org/us/company-b",
	)
	frag.Qualifiers = map[string]interface{}{
		"routeVariants": []map[string]interface{}{
			{
				"variantKey":       "via-suez",
				"transportViaRefs": []string{"/fac/eg/suez-terminal"},
				"confidence":       0.81,
			},
			{
				"variantKey":       "via-cape",
				"transportViaRefs": []string{"/fac/za/cape-of-good-hope-terminal"},
				"confidence":       0.79,
			},
		},
	}
	snapshot := engine.snapshotFromLinks([]*models.HorizonFragment{frag})
	if len(snapshot.Links) != 2 {
		t.Fatalf("links=%d, want 2 route variants", len(snapshot.Links))
	}
	if snapshot.Links[0].RouteVariantKey == snapshot.Links[1].RouteVariantKey {
		t.Fatalf("route variant keys should differ: %+v", snapshot.Links)
	}
}

func testSupplyLinkFragment(id string, canonicalPath string, supplierRef string, productRef string, customerRef string) *models.HorizonFragment {
	return &models.HorizonFragment{
		ID:            id,
		TenantID:      "global",
		Class:         ah.FragmentClassSite,
		Subtype:       "supply_link",
		CanonicalPath: canonicalPath,
		SupplierRef:   stringPtr(supplierRef),
		ProductRef:    stringPtr(productRef),
		CustomerRef:   stringPtr(customerRef),
		State: map[string]models.MetricValue{
			"confidence": {Value: 0.9},
		},
	}
}

func stringPtr(value string) *string {
	return &value
}
