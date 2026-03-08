package pathbinding

import "testing"

func TestBindSegment_InferRouteAndZoneBindings(t *testing.T) {
	engine := NewEngine(nil, nil)
	binding := engine.BindSegment(nil, SegmentInput{
		SupplierRef:      "/fac/ae/jebel-ali-terminal",
		CustomerRef:      "/fac/in/mundra-port-terminal",
		ProductRef:       "/product/raw/hydrocarbon/distribution/lng",
		TransportViaRefs: []string{"/fac/sea/strait-of-hormuz-terminal"},
	})
	if binding.Mode == "" {
		t.Fatal("expected route mode")
	}
	if len(binding.FlowRefs) == 0 {
		t.Fatal("expected flow bindings")
	}
	if len(binding.ZoneRefs) == 0 {
		t.Fatal("expected zone bindings")
	}
	if binding.BindingConfidence <= 0 {
		t.Fatal("expected positive binding confidence")
	}
}

func TestBindSegment_DoesNotOverbindGenericFacilityPair(t *testing.T) {
	engine := NewEngine(nil, nil)
	binding := engine.BindSegment(nil, SegmentInput{
		SupplierRef: "/fac/na/industrial-shredding-complex",
		CustomerRef: "/fac/na/sensor-based-sorting-facility",
		ProductRef:  "/product/processed/material/shredded-scrap-mix",
	})
	if len(binding.FlowRefs) != 0 {
		t.Fatalf("expected no flow bindings for generic pair, got %v", binding.FlowRefs)
	}
	if len(binding.ZoneRefs) != 0 {
		t.Fatalf("expected no zone bindings for generic pair, got %v", binding.ZoneRefs)
	}
}

func TestBindSegment_KeepsTargetedSeaRouteBinding(t *testing.T) {
	engine := NewEngine(nil, nil)
	binding := engine.BindSegment(nil, SegmentInput{
		SupplierRef:      "/fac/ae/jebel-ali-terminal",
		CustomerRef:      "/fac/in/mundra-port-terminal",
		ProductRef:       "/product/raw/hydrocarbon/lng",
		TransportViaRefs: []string{"/fac/sea/strait-of-hormuz-terminal"},
	})
	if len(binding.FlowRefs) == 0 {
		t.Fatal("expected targeted flow binding")
	}
	if len(binding.FlowRefs) > 2 {
		t.Fatalf("expected bounded flow bindings, got %v", binding.FlowRefs)
	}
	if len(binding.ZoneRefs) == 0 {
		t.Fatal("expected targeted zone binding")
	}
	if len(binding.ZoneRefs) > 2 {
		t.Fatalf("expected bounded zone bindings, got %v", binding.ZoneRefs)
	}
}

func TestBindSegment_DoesNotBindRandomRegionalFlowFromGenericRoadEvidence(t *testing.T) {
	engine := NewEngine(nil, nil)
	binding := engine.BindSegment(nil, SegmentInput{
		SupplierRef: "/fac/na/frejus-road-tunnel",
		CustomerRef: "/org/de/bmw-ag",
		ProductRef:  "/product/services/logistics/road-freight",
	})
	if len(binding.FlowRefs) != 0 {
		t.Fatalf("expected no flow binding from generic road evidence, got %v", binding.FlowRefs)
	}
}
