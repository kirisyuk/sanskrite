package supplysemantics

import "testing"

func TestClassifySegment_SelfLoop(t *testing.T) {
	classification := ClassifySegment(
		"/fac/qa/ras-laffan-port",
		"/product/services/logistics/port-operations",
		"Port Operations",
		"/fac/qa/ras-laffan-port",
		nil,
	)
	if classification.Class != SegmentClassInvalidSelfLoop {
		t.Fatalf("class=%q", classification.Class)
	}
}

func TestClassifySegment_InternalSupport(t *testing.T) {
	classification := ClassifySegment(
		"/fac/qa/ras-laffan-port",
		"/product/services/logistics/port-operations",
		"Port Operations",
		"/org/qa/qatarenergy",
		nil,
	)
	if classification.Class != SegmentClassInternalSupport {
		t.Fatalf("class=%q", classification.Class)
	}
}

func TestClassifySegment_ExternalProductFlow(t *testing.T) {
	classification := ClassifySegment(
		"/org/qa/qatarenergy",
		"/product/raw/hydrocarbon/distribution/lng",
		"Liquefied Natural Gas",
		"/fac/gb/south-hook-terminal",
		[]string{"/fac/sea/strait-of-hormuz-terminal"},
	)
	if classification.Class != SegmentClassExternalProductFlow {
		t.Fatalf("class=%q", classification.Class)
	}
}

func TestClassifySegment_InternalActorLinkByRole(t *testing.T) {
	classification := ClassifySegmentWithContext(
		"/fac/gb/south-hook-terminal",
		"South Hook LNG Terminal",
		"",
		"/product/raw/hydrocarbon/distribution/lng",
		"Liquefied Natural Gas",
		"/org/qa/qatarenergy",
		"QatarEnergy",
		"owner",
		nil,
	)
	if classification.Class != SegmentClassInternalActorLink {
		t.Fatalf("class=%q", classification.Class)
	}
}
