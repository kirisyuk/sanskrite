package models

import (
	"testing"
)

func TestSiteTransformsRoundTrip(t *testing.T) {
	f := &HorizonFragment{}
	f.SetSiteTransforms([]SiteTransform{
		{Kind: "refining", InputProducts: []string{"/product/a"}, OutputProducts: []string{"/product/b"}, YieldRatio: 0.9},
	})
	got := f.SiteTransforms()
	if len(got) != 1 || got[0].Kind != "refining" || len(got[0].InputProducts) != 1 {
		t.Fatalf("unexpected decode: %+v", got)
	}
	f.SetSiteTransforms(nil)
	if f.Attributes != nil {
		t.Fatalf("expected attributes cleared, got %v", f.Attributes)
	}
}

func TestAppendSiteTransformIdempotent(t *testing.T) {
	f := &HorizonFragment{}
	st := SiteTransform{Kind: "assembly", InputProducts: []string{"/p/in"}, OutputProducts: []string{"/p/out"}}
	f.AppendSiteTransform(st)
	f.AppendSiteTransform(st)
	if len(f.SiteTransforms()) != 1 {
		t.Fatalf("expected 1 transform, got %d", len(f.SiteTransforms()))
	}
	// Same kind but different I/O should append.
	f.AppendSiteTransform(SiteTransform{Kind: "assembly", InputProducts: []string{"/p/x"}, OutputProducts: []string{"/p/y"}})
	if len(f.SiteTransforms()) != 2 {
		t.Fatalf("expected 2 transforms, got %d", len(f.SiteTransforms()))
	}
}

func TestSiteTransformsFromGenericJSON(t *testing.T) {
	f := &HorizonFragment{
		Attributes: map[string]interface{}{
			attrSiteTransforms: []interface{}{
				map[string]interface{}{
					"kind":           "extraction",
					"inputProducts":  []interface{}{"/product/raw"},
					"outputProducts": []interface{}{"/product/conc"},
				},
			},
		},
	}
	got := f.SiteTransforms()
	if len(got) != 1 || got[0].Kind != "extraction" {
		t.Fatalf("got %+v", got)
	}
}
