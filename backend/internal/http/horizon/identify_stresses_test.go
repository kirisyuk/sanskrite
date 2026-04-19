package horizon

import (
	"context"
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFilterAndPrioritizeFindingsNormalizesAndScopes(t *testing.T) {
	canvas := &IdentifyStressesCanvas{}
	scope := &identifyStressScope{
		NodeRefs:       []string{"/org/global/a"},
		ArtifactKeys:   []string{"artifact/path-1"},
		SupplyLinkRefs: []string{"/supply-link/global/a/product/b"},
	}

	findings := []StressFinding{
		{
			TargetType:   "node",
			TargetRefs:   []string{"/org/global/a"},
			StressorType: "sanctions",
			Severity:     "critical",
			Evidence:     []StressEvidence{{URL: "https://example.com/a"}},
			EffectiveAt:  "2026-03-19T10:00:00Z",
		},
		{
			TargetType:   "path",
			ArtifactKey:  "/artifact/path-1",
			StressorType: "route_disruption",
			Mechanism:    "route_fragility",
			Severity:     "medium",
			Confidence:   0.8,
		},
		{
			TargetType:    "supply_link",
			SupplyLinkRef: "/supply-link/global/a/product/b",
			StressorType:  "capacity_drop",
			Severity:      "low",
			Confidence:    0.7,
		},
		{
			TargetType:   "node",
			TargetRefs:   []string{"/org/global/out-of-scope"},
			StressorType: "sanctions",
			Severity:     "critical",
			Confidence:   0.9,
		},
	}

	got := canvas.filterAndPrioritizeFindings(findings, scope, 10)
	if len(got) != 3 {
		t.Fatalf("expected 3 in-scope findings, got %d", len(got))
	}
	if got[0].TargetRefs[0] != "/org/global/a" {
		t.Fatalf("expected critical node finding to be first, got %+v", got[0])
	}
	if got[0].Mechanism != "control_exposure" {
		t.Fatalf("expected mechanism to be inferred from sanctions, got %q", got[0].Mechanism)
	}
	if got[0].Confidence != 0.62 {
		t.Fatalf("expected default confidence 0.62, got %v", got[0].Confidence)
	}
	if got[0].EventType != "sanctions" {
		t.Fatalf("expected event type to default to stressor type, got %q", got[0].EventType)
	}
	if got[0].FactType != "stress_finding" {
		t.Fatalf("expected fact type to default to stress_finding, got %q", got[0].FactType)
	}
	if got[1].ArtifactKey != "artifact/path-1" {
		t.Fatalf("expected artifact key to be normalized without leading slash, got %q", got[1].ArtifactKey)
	}
	if got[2].SupplyLinkRef != "/supply-link/global/a/product/b" {
		t.Fatalf("expected supply-link finding to survive scoping, got %+v", got[2])
	}
}

func TestIdentifyStressFingerprintAndTags(t *testing.T) {
	finding := StressFinding{
		TargetRefs:    []string{"/org/global/a"},
		Mechanism:     "route_fragility",
		StressorType:  "route_disruption",
		EffectiveAt:   "2026-03-19T23:14:00+06:00",
		SupplyLinkRef: "/supply-link/global/a/product/b",
		Evidence: []StressEvidence{
			{URL: "https://example.com/disruption"},
		},
	}

	fingerprint := identifyStressFingerprint(finding)
	want := "/org/global/a|route_fragility|https://example.com/disruption|2026-03-19"
	if fingerprint != want {
		t.Fatalf("unexpected fingerprint %q, want %q", fingerprint, want)
	}

	tags := stressFactTags(finding, fingerprint, "event")
	if !containsString(tags, "identify_stresses") {
		t.Fatalf("expected identify_stresses tag, got %v", tags)
	}
	if !containsString(tags, "stress_fingerprint:"+fingerprint) {
		t.Fatalf("expected fingerprint tag, got %v", tags)
	}
	if !containsString(tags, "stress_target:/org/global/a") {
		t.Fatalf("expected canonical target tag, got %v", tags)
	}
}

func TestCoverageAndGateStatus(t *testing.T) {
	richMetrics := map[string]horizonmodels.MetricValue{
		"lead_time_days":                      {Value: 17, Confidence: 0.9},
		"inventory_cover_days_at_destination": {Value: 6, Confidence: 0.9},
		"share":                               {Value: 0.7, Confidence: 0.9},
		"switch_time_days":                    {Value: 45, Confidence: 0.9},
		"technical_fit":                       {Value: 0.4, Confidence: 0.9},
	}
	if got := computeCoverageStatus(richMetrics); got != "rich" {
		t.Fatalf("expected rich coverage, got %q", got)
	}
	if got := computeGateStatus(map[string]float64{"route_fragility": 0.9}, "rich"); got != "corroborated" {
		t.Fatalf("expected corroborated gate status, got %q", got)
	}

	thinMetrics := map[string]horizonmodels.MetricValue{
		"lead_time_days": {Value: 9, Confidence: 0.7},
	}
	if got := computeCoverageStatus(thinMetrics); got != "thin" {
		t.Fatalf("expected thin coverage, got %q", got)
	}
	if got := computeGateStatus(map[string]float64{"route_fragility": 0.6}, "thin"); got != "sufficient" {
		t.Fatalf("expected sufficient gate status, got %q", got)
	}
	if got := computeGateStatus(map[string]float64{"route_fragility": 0.3}, "empty"); got != "best_effort" {
		t.Fatalf("expected best_effort gate status, got %q", got)
	}
}

func TestPublishIdentifyStressesCompletionPartialSuccess(t *testing.T) {
	var publishedType string
	var publishedTenant string
	var payload map[string]interface{}
	canvas := &IdentifyStressesCanvas{
		publisher: func(_ context.Context, eventType string, tenantID string, data interface{}) error {
			publishedType = eventType
			publishedTenant = tenantID
			payload, _ = data.(map[string]interface{})
			return nil
		},
	}

	lens := &horizonmodels.Lens{ID: "lens-1", ProjectID: "project-1"}
	canvas.publishIdentifyStressesCompletion(
		context.Background(),
		"tenant-1",
		lens,
		"job-1",
		"partial_success",
		4,
		3,
		1,
		2,
		1,
		1,
		2,
		[]string{"propagation batch failed"},
	)

	if publishedType != "horizon.identify_stresses.completed" {
		t.Fatalf("unexpected event type %q", publishedType)
	}
	if publishedTenant != "tenant-1" {
		t.Fatalf("unexpected tenant %q", publishedTenant)
	}
	if got := payload["status"]; got != "partial_success" {
		t.Fatalf("expected partial_success status, got %#v", got)
	}
	if got := payload["propagationErrors"]; got != 2 {
		t.Fatalf("expected propagationErrors=2, got %#v", got)
	}
	if got := payload["error"]; got != "propagation batch failed" {
		t.Fatalf("expected first error to be published, got %#v", got)
	}
}

func TestApplyFindingToFragmentUpdatesRiskMetricsAndQualifiers(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	fragment := &horizonmodels.HorizonFragment{
		CanonicalPath: "/supply-link/global/a/product/b",
		State:         map[string]horizonmodels.MetricValue{},
		Qualifiers:    map[string]interface{}{},
		Metadata:      horizonmodels.FragmentMetadata{},
	}
	finding := StressFinding{
		TargetType:       "supply_link",
		SupplyLinkRef:    "/supply-link/global/a/product/b",
		StressorType:     "sanctions",
		Mechanism:        "control_exposure",
		Severity:         "critical",
		Confidence:       0.91,
		AffectedZoneRefs: []string{"/zone/global/red-sea"},
		ZoneRefs:         []string{"/zone/maritime/red-sea"},
		GeoRefs:          []string{"/geo/eg", "/geo/sa"},
		MetricObservations: []StressMetricObservation{
			{MetricKey: "lead_time_days", Value: 17, Confidence: 0.88, Unit: "days"},
			{MetricKey: "share", Value: 0.74, Confidence: 0.82},
		},
		QualifierDeltas: map[string]interface{}{
			"sanctions_touchpoint": true,
			"dual_use_flag":        true,
		},
	}

	(&IdentifyStressesCanvas{}).applyFindingToFragment(fragment, finding, []string{"fact-1"}, now, true)

	if got := metricValue(fragment.State, "supply_chain_criticality"); got <= 0 {
		t.Fatalf("expected supply_chain_criticality to be set, got %v", got)
	}
	if got := metricValue(fragment.State, "route_fragility"); got <= 0 {
		t.Fatalf("expected route_fragility to be set for supply-link finding, got %v", got)
	}
	if got := metricValue(fragment.State, "route_rerouteability"); got <= 0 {
		t.Fatalf("expected route_rerouteability to be set, got %v", got)
	}
	if got := stringQualifier(fragment.Qualifiers, "primaryRiskMechanism"); got != "control_exposure" {
		t.Fatalf("expected primaryRiskMechanism=control_exposure, got %q", got)
	}
	if got := stringQualifier(fragment.Qualifiers, "riskCoverageStatus"); got != "thin" {
		t.Fatalf("expected riskCoverageStatus=thin, got %q", got)
	}
	if got := stringQualifier(fragment.Qualifiers, "riskGateStatus"); got == "" {
		t.Fatal("expected riskGateStatus to be computed")
	}
	if !containsString(interfaceToStrings(fragment.Qualifiers["affectedZoneRefs"]), "/zone/global/red-sea") {
		t.Fatalf("expected affected zone refs to include new zone, got %#v", fragment.Qualifiers["affectedZoneRefs"])
	}
	if fragment.Metadata.LastFactsAppliedAt.IsZero() {
		t.Fatal("expected LastFactsAppliedAt to be updated")
	}
}

func TestApplyFindingToArtifactUpdatesLegEdgeAndIndexes(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 30, 0, 0, time.UTC)
	segmentIndex := 0
	artifact := &horizonmodels.MaterializedArtifact{
		ID:           "artifact-1",
		TenantID:     "global",
		LensID:       "lens-1",
		Kind:         "SUPPLY_CHAIN_PATH",
		CanonicalKey: "artifact/path-1",
		ProductAxis:  "/product/material/lithium",
		BindingScore: 0.77,
		Payload: horizonmodels.BuildSupplyChainPathPayloadDetailed(
			[]horizonmodels.SupplyChainPathNode{
				{Type: "org", Ref: "/org/global/supplier-a"},
				{Type: "product", Ref: "/product/material/lithium"},
				{Type: "org", Ref: "/org/global/customer-b"},
			},
			[]horizonmodels.SupplyChainPathEdge{
				{
					Type:                    "SUPPLY_LINK",
					From:                    0,
					To:                      2,
					ProductRef:              "/product/material/lithium",
					SupplyLinkID:            "edge-1",
					SupplyLinkCanonicalPath: "/supply-link/global/supplier-a/product-material-lithium/customer-b",
					SegmentIndex:            &segmentIndex,
					CarriesProduct:          true,
					Role:                    "product_flow",
				},
			},
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:                   0,
					SupplierRef:             "/org/global/supplier-a",
					CustomerRef:             "/org/global/customer-b",
					ProductRef:              "/product/material/lithium",
					SupplyLinkCanonicalPath: "/supply-link/global/supplier-a/product-material-lithium/customer-b",
					SupplyLinkID:            "edge-1",
					SegmentConfidence:       0.8,
					TransportViaRefs:        []string{"/fac/eg/port-said"},
				},
			},
			nil,
			nil,
			nil,
			nil,
		),
	}

	finding := StressFinding{
		TargetType:       "supply_link",
		ArtifactKey:      "artifact/path-1",
		SupplyLinkRef:    "/supply-link/global/supplier-a/product-material-lithium/customer-b",
		ProductRef:       "/product/material/lithium",
		StressorType:     "route_disruption",
		Mechanism:        "route_fragility",
		Severity:         "high",
		Confidence:       0.86,
		FlowRefs:         []string{"/flow/maritime/suez"},
		ZoneRefs:         []string{"/zone/maritime/suez"},
		AffectedZoneRefs: []string{"/zone/global/red-sea"},
		MetricObservations: []StressMetricObservation{
			{MetricKey: "lead_time_days", Value: 21, Confidence: 0.8, Unit: "days"},
			{MetricKey: "path_stress_score", Value: 0.84, Confidence: 0.8},
		},
	}

	updated, touchedEdgeIDs, obsIDs := (&IdentifyStressesCanvas{}).applyFindingToArtifact(context.Background(), "global", artifact, finding, []string{"fact-1"}, now)
	if updated == nil {
		t.Fatal("expected updated artifact")
	}
	if !containsString(touchedEdgeIDs, "edge-1") {
		t.Fatalf("expected touched edge IDs to include edge-1, got %v", touchedEdgeIDs)
	}
	if len(obsIDs) != 0 {
		t.Fatalf("expected no persisted observation IDs without repos, got %v", obsIDs)
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(updated.Payload)
	if err != nil {
		t.Fatalf("ParseSupplyChainPathPayload() error = %v", err)
	}
	if len(parsed.Legs) != 1 || parsed.Legs[0].Stress == nil {
		t.Fatalf("expected enriched leg stress, got %+v", parsed.Legs)
	}
	if !containsString(parsed.Legs[0].AffectedZoneRefs, "/zone/global/red-sea") {
		t.Fatalf("expected leg affected zone refs to include /zone/global/red-sea, got %v", parsed.Legs[0].AffectedZoneRefs)
	}
	if !containsString(parsed.Legs[0].FlowRefs, "/flow/maritime/suez") {
		t.Fatalf("expected leg flow refs to include /flow/maritime/suez, got %v", parsed.Legs[0].FlowRefs)
	}
	if parsed.Legs[0].Stress.StressScore <= 0 {
		t.Fatalf("expected positive leg stress score, got %+v", parsed.Legs[0].Stress)
	}
	if parsed.Stress == nil || parsed.Stress.PathScore <= 0 {
		t.Fatalf("expected path stress to be recomputed, got %+v", parsed.Stress)
	}
	if !containsString(updated.AffectedZoneRefs, "/zone/global/red-sea") {
		t.Fatalf("expected artifact affected zones to be indexed, got %v", updated.AffectedZoneRefs)
	}
	if updated.PathStressScore <= 0 {
		t.Fatalf("expected top-level PathStressScore to be populated, got %v", updated.PathStressScore)
	}
	if len(parsed.Edges) != 1 || parsed.Edges[0].Qualifiers == nil {
		t.Fatalf("expected edge qualifiers to be updated, got %+v", parsed.Edges)
	}
	if got := parsed.Edges[0].Qualifiers[QualifierPathStressScore]; got == nil {
		t.Fatalf("expected edge qualifier %q to be set", QualifierPathStressScore)
	}
}

func TestBindFindingToScopeReplacesMismatchedArtifactWithMatchingPathLeg(t *testing.T) {
	canvas := &IdentifyStressesCanvas{}

	wrong := &horizonmodels.MaterializedArtifact{
		CanonicalKey: "artifact/wrong",
		ProductAxis:  "/product/component/electronic/manufacturing/logic-wafer",
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Type: "org", Ref: "/org/nl/asml-holding-nv"},
				{Type: "product", Ref: "/product/component/electronic/manufacturing/logic-wafer"},
				{Type: "org", Ref: "/org/us/nvidia-corp"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:                   0,
					SupplierRef:             "/org/nl/asml-holding-nv",
					CustomerRef:             "/org/us/nvidia-corp",
					ProductRef:              "/product/component/electronic/manufacturing/logic-wafer",
					SupplyLinkCanonicalPath: "/supply-link/global/asml-holding-nv/logic-wafer/nvidia-corp",
				},
			},
			nil,
			nil,
		),
	}
	right := &horizonmodels.MaterializedArtifact{
		CanonicalKey: "artifact/right",
		ProductAxis:  "/product/finished/mechanical/assembly/euv-lithography-system",
		Payload: horizonmodels.BuildSupplyChainPathPayload(
			[]horizonmodels.SupplyChainPathNode{
				{Type: "org", Ref: "/org/nl/asml-holding-nv"},
				{Type: "product", Ref: "/product/finished/mechanical/assembly/euv-lithography-system"},
				{Type: "org", Ref: "/org/tw/taiwan-semiconductor-manufacturing-co-ltd"},
			},
			nil,
			[]horizonmodels.SupplyChainPathLeg{
				{
					Index:                   2,
					SupplierRef:             "/org/nl/asml-holding-nv",
					CustomerRef:             "/org/tw/taiwan-semiconductor-manufacturing-co-ltd",
					ProductRef:              "/product/finished/mechanical/assembly/euv-lithography-system",
					SupplyLinkCanonicalPath: "/supply-link/global/asml-holding-nv/euv-lithography-system/taiwan-semiconductor-manufacturing-co-ltd",
				},
			},
			nil,
			nil,
		),
	}

	scope := &identifyStressScope{
		Artifacts: []*horizonmodels.MaterializedArtifact{wrong, right},
		NodeRefs: []string{
			"/org/nl/asml-holding-nv",
			"/org/tw/taiwan-semiconductor-manufacturing-co-ltd",
		},
	}
	finding := StressFinding{
		TargetType:    "product_on_link",
		ArtifactKey:   "artifact/wrong",
		SupplyLinkRef: "/supply-link/global/asml-holding-nv/euv-lithography-system/taiwan-semiconductor-manufacturing-co-ltd",
		ProductRef:    "/product/finished/mechanical/assembly/euv-lithography-system",
		TargetRefs: []string{
			"/org/nl/asml-holding-nv",
			"/org/tw/taiwan-semiconductor-manufacturing-co-ltd",
		},
	}

	got := canvas.bindFindingToScope(finding, scope)
	if got.ArtifactKey != "artifact/right" {
		t.Fatalf("expected artifact/right, got %q", got.ArtifactKey)
	}
	if got.TargetType != "path_leg" {
		t.Fatalf("expected path_leg target type, got %q", got.TargetType)
	}
	if got.LegIndex == nil || *got.LegIndex != 2 {
		t.Fatalf("expected leg index 2, got %+v", got.LegIndex)
	}
	if got.ProductRef != "/product/finished/mechanical/assembly/euv-lithography-system" {
		t.Fatalf("expected EUV product ref, got %q", got.ProductRef)
	}
}
