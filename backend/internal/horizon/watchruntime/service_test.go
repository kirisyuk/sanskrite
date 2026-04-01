package watchruntime

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/focusobject"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestAssessTriggerDecision(t *testing.T) {
	svc := &Service{}
	subject := &horizonmodels.WatchSubject{
		ID:                  "subj-1",
		TenantID:            "global",
		SubjectKind:         horizonmodels.WatchSubjectKindFragment,
		AnchorCanonicalPath: "/org/qa/qatarenergy",
		DisplayLabel:        "QatarEnergy",
		Status:              horizonmodels.WatchSubjectStatusActive,
		SourceMode:          horizonmodels.WatchSubjectSourceFacts,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeCustomFragment,
	}

	if decision := svc.AssessTrigger(subject, &horizonmodels.WatchTrigger{
		SubjectID:      subject.ID,
		TriggerType:    horizonmodels.WatchTriggerFactDelta,
		DedupeKey:      "t1",
		SignalStrength: 0.05,
		Confidence:     0.95,
	}); decision.Decision != horizonmodels.WatchTriggerDecisionWarmOnly {
		t.Fatalf("expected weak signal to warm only, got %+v", decision)
	}

	if decision := svc.AssessTrigger(subject, &horizonmodels.WatchTrigger{
		SubjectID:      subject.ID,
		TriggerType:    horizonmodels.WatchTriggerFactDelta,
		DedupeKey:      "t2",
		SignalStrength: 0.85,
		Confidence:     0.90,
		MatchMode:      horizonmodels.WatchTriggerMatchAnchor,
	}); decision.Decision != horizonmodels.WatchTriggerDecisionEnqueueRisk {
		t.Fatalf("expected strong direct fragment delta to enqueue risk, got %+v", decision)
	}
}

func TestAssessTriggerRecommendationApprovedEnqueuesScout(t *testing.T) {
	svc := &Service{}
	subject := &horizonmodels.WatchSubject{
		ID:                  "subj-2",
		TenantID:            "global",
		SubjectKind:         horizonmodels.WatchSubjectKindLensUniverse,
		AnchorCanonicalPath: "/lens/runtime/lens-1",
		DisplayLabel:        "Energy lens",
		Status:              horizonmodels.WatchSubjectStatusActive,
		SourceMode:          horizonmodels.WatchSubjectSourceHybrid,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeCombined,
	}
	decision := svc.AssessTrigger(subject, &horizonmodels.WatchTrigger{
		SubjectID:      subject.ID,
		TriggerType:    horizonmodels.WatchTriggerRecommendationApproved,
		DedupeKey:      "t3",
		SignalStrength: 0.95,
		Confidence:     0.99,
		MatchMode:      horizonmodels.WatchTriggerMatchAnchor,
	})
	if decision.Decision != horizonmodels.WatchTriggerDecisionEnqueueScout {
		t.Fatalf("expected approved recommendation to enqueue scout, got %+v", decision)
	}
}

func TestAssessTriggerPropagationGapCreatesRecommendation(t *testing.T) {
	svc := &Service{}
	subject := &horizonmodels.WatchSubject{
		ID:                  "subj-3",
		TenantID:            "global",
		SubjectKind:         horizonmodels.WatchSubjectKindLensUniverse,
		AnchorCanonicalPath: "/lens/runtime/lens-2",
		DisplayLabel:        "Helium lens",
		Status:              horizonmodels.WatchSubjectStatusActive,
		SourceMode:          horizonmodels.WatchSubjectSourceHybrid,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeGeneralized,
	}
	decision := svc.AssessTrigger(subject, &horizonmodels.WatchTrigger{
		SubjectID:      subject.ID,
		TriggerType:    horizonmodels.WatchTriggerPropagationGap,
		DedupeKey:      "t4",
		SignalStrength: 0.80,
		Confidence:     0.92,
		MatchMode:      horizonmodels.WatchTriggerMatchUniverse,
	})
	if decision.Decision != horizonmodels.WatchTriggerDecisionCreateOrRefreshRecommendation {
		t.Fatalf("expected propagation gap to create or refresh recommendation, got %+v", decision)
	}
}

func TestBuildWatchUniverseIncludesLensSurfaces(t *testing.T) {
	svc := &Service{}
	lens := &horizonmodels.Lens{
		ID:              "lens-1",
		TenantID:        "global",
		ProjectID:       "proj-1",
		Name:            "Energy",
		Status:          string(horizonmodels.LensStatusActive),
		Scope:           []string{"/org/qa/qatarenergy"},
		ResolvedQueries: map[string][]string{"/query/market:energy/**": {"/fac/qa/ras-laffan-industrial-city"}},
		ScopeArtifacts: map[string]horizonmodels.ScopeArtifactRef{
			"/artifact/supply/path-1": {
				NodeRefs:         []string{"/product/raw/gases/helium"},
				GeoRefs:          []string{"/loc/qa/da/ras-laffan"},
				AffectedZoneRefs: []string{"/zone/qa/military_conflict/ras-laffan"},
			},
		},
	}
	universe := svc.buildWatchUniverse(nil, lens)
	expected := map[string]struct{}{
		"/org/qa/qatarenergy":                   {},
		"/fac/qa/ras-laffan-industrial-city":    {},
		"/artifact/supply/path-1":               {},
		"/product/raw/gases/helium":             {},
		"/loc/qa/da/ras-laffan":                 {},
		"/zone/qa/military_conflict/ras-laffan": {},
	}
	for _, path := range universe {
		delete(expected, path)
	}
	if len(expected) != 0 {
		t.Fatalf("missing expected universe paths: %+v", expected)
	}
}

func TestBuildRiskJobDedupKeyIgnoresTriggerTypeWhenSemanticMatches(t *testing.T) {
	subject := &horizonmodels.WatchSubject{
		ID:                  "subj-4",
		TenantID:            "global",
		LensID:              "lens-1",
		AnchorCanonicalPath: "/zone/qa/military_conflict/ras-laffan",
	}
	left := &horizonmodels.WatchTrigger{
		ID:                "trigger-a",
		SubjectID:         subject.ID,
		TriggerType:       horizonmodels.WatchTriggerAffectedZoneChanged,
		SemanticWatermark: "event-123",
	}
	right := &horizonmodels.WatchTrigger{
		ID:                "trigger-b",
		SubjectID:         subject.ID,
		TriggerType:       horizonmodels.WatchTriggerSignalDetected,
		SemanticWatermark: "event-123",
	}
	leftKey := buildRiskJobDedupKey(subject, left, "proj-1", "risk")
	rightKey := buildRiskJobDedupKey(subject, right, "proj-1", "risk")
	if leftKey != rightKey {
		t.Fatalf("expected semantic-equivalent triggers to dedupe to same risk key, got %q vs %q", leftKey, rightKey)
	}
}

func TestBuildWatchScoutActionDedupKeyUsesRecommendationStage(t *testing.T) {
	subject := &horizonmodels.WatchSubject{
		ID:                  "subj-5",
		TenantID:            "global",
		LensID:              "lens-1",
		AnchorCanonicalPath: "/zone/qa/military_conflict/ras-laffan",
	}
	trigger := &horizonmodels.WatchTrigger{
		ID:          "trigger-c",
		SubjectID:   subject.ID,
		TriggerType: horizonmodels.WatchTriggerRecommendationApproved,
		Metadata: map[string]any{
			"recommendationId": "rec-1",
			"jobStage":         "targeted",
		},
	}
	key := buildWatchScoutActionDedupKey(subject, trigger, "proj-1", "lens-1")
	if key == "" || key != buildWatchScoutActionDedupKey(subject, trigger, "proj-1", "lens-1") {
		t.Fatalf("expected stable non-empty scout dedupe key, got %q", key)
	}
}

func TestIsFocusTargetFromKindAndMetadata(t *testing.T) {
	target := &horizonmodels.WatchTarget{
		Kind:          string(horizonmodels.WatchSubjectKindFocusObject),
		CanonicalPath: "/org/no/equinor",
	}
	if !isFocusTarget(target) {
		t.Fatalf("expected focus kind to enable focus target")
	}
	target = &horizonmodels.WatchTarget{
		Kind:          "company",
		CanonicalPath: "/org/no/equinor",
		Metadata: map[string]any{
			"focusObject": map[string]any{
				"relatedCanonicalPaths": []string{"/market/eu/gas"},
			},
		},
	}
	if !isFocusTarget(target) {
		t.Fatalf("expected focus metadata to enable focus target")
	}
}

func TestFocusInputFromTargetReadsFocusMetadata(t *testing.T) {
	target := &horizonmodels.WatchTarget{
		Label:          "Equinor",
		CanonicalPath:  "/org/no/equinor",
		QueryOverrides: []string{"Equinor gas"},
		DomainModes:    []string{"energy"},
		Metadata: map[string]any{
			"focusObject": map[string]any{
				"relatedCanonicalPaths": []any{"/market/eu/gas", "/zone/strait/hormuz"},
				"universeDepth":         3,
				"lookbackHours":         72,
				"maxImpactedEntities":   12,
				"maxPropagationHops":    6,
			},
		},
	}
	input := focusInputFromTarget(target)
	if input.UniverseDepth != 3 || input.LookbackHours != 72 || input.MaxImpactedEntities != 12 || input.MaxPropagationHops != 6 {
		t.Fatalf("unexpected focus input numeric config: %+v", input)
	}
	if len(input.RelatedCanonicalPaths) != 2 {
		t.Fatalf("expected related paths from focus metadata, got %+v", input.RelatedCanonicalPaths)
	}
}

func TestOverlapRankPrefersFocusSubject(t *testing.T) {
	target := &horizonmodels.WatchTarget{
		TenantID:      "global",
		UserID:        "user-1",
		CanonicalPath: "/org/no/equinor",
	}
	focus := &horizonmodels.WatchSubject{
		ID:                  "focus-1",
		TenantID:            "global",
		UserID:              "user-1",
		SubjectKind:         horizonmodels.WatchSubjectKindFocusObject,
		AnchorCanonicalPath: "/org/no/equinor",
	}
	fragment := &horizonmodels.WatchSubject{
		ID:                  "fragment-1",
		TenantID:            "global",
		UserID:              "user-1",
		SubjectKind:         horizonmodels.WatchSubjectKindFragment,
		AnchorCanonicalPath: "/org/no/equinor",
	}
	if !sameTargetScope(focus, target) || !sameTargetScope(fragment, target) {
		t.Fatalf("expected same anchor/user scope for overlap candidates")
	}
	if overlappingSubjectRank(focus) <= overlappingSubjectRank(fragment) {
		t.Fatalf("expected focus subject to outrank fragment subject")
	}
}

func TestResolveExecutionOverlapPlanAssignsLensUniverseOwner(t *testing.T) {
	svc := &Service{}
	snapshot := &focusobject.Snapshot{
		Focus: focusobject.SuggestedWatchSubject{
			AnchorCanonicalPath:   "/org/no/equinor",
			UniversePaths:         []string{"/org/no/equinor", "/market/eu/energy/natural-gas", "/zone/sea/logistics/strait-of-hormuz"},
			ArtifactCanonicalKeys: []string{"/artifact/supply/path-eu-gas"},
		},
	}
	lensUniverse := &horizonmodels.WatchSubject{
		ID:            "lens-subject",
		TenantID:      "global",
		SubjectKind:   horizonmodels.WatchSubjectKindLensUniverse,
		DisplayLabel:  "Europe energy lens",
		Status:        horizonmodels.WatchSubjectStatusActive,
		LensBindings:  []string{"lens-energy"},
		UniversePaths: []string{"/org/no/equinor", "/market/eu/energy/natural-gas", "/zone/sea/logistics/strait-of-hormuz", "/org/qa/qatargas"},
	}
	plan := svc.resolveExecutionOverlapPlanFromCandidates("global", []string{"lens-energy"}, "", "", "", snapshot, []*horizonmodels.WatchSubject{lensUniverse})
	if plan.PrimaryOwnerSubjectID != "lens-subject" {
		t.Fatalf("expected lens universe to become primary owner, got %+v", plan)
	}
	if len(plan.SharedUniversePaths) != 3 || len(plan.ResidualUniversePaths) != 0 {
		t.Fatalf("expected full coverage by owner, got %+v", plan)
	}
}

func TestSubjectExecutionCoveredByOwnerRequiresFullTriggerCoverage(t *testing.T) {
	subject := &horizonmodels.WatchSubject{
		ID:                  "focus-1",
		TenantID:            "global",
		SubjectKind:         horizonmodels.WatchSubjectKindFocusObject,
		AnchorCanonicalPath: "/org/no/equinor",
		Status:              horizonmodels.WatchSubjectStatusActive,
		Metadata: map[string]any{
			executionCoverageMetadataKey: map[string]any{
				"primaryOwnerSubjectId": "lens-subject",
				"sharedUniversePaths": []string{
					"/org/no/equinor",
					"/market/eu/energy/natural-gas",
				},
			},
		},
	}
	if !subjectExecutionCoveredByOwner(subject, &horizonmodels.WatchTrigger{
		TriggerType:    horizonmodels.WatchTriggerSignalDetected,
		CanonicalPaths: []string{"/org/no/equinor"},
	}) {
		t.Fatalf("expected covered trigger path to be delegated to owner")
	}
	if subjectExecutionCoveredByOwner(subject, &horizonmodels.WatchTrigger{
		TriggerType:    horizonmodels.WatchTriggerSignalDetected,
		CanonicalPaths: []string{"/org/no/equinor", "/zone/sea/logistics/strait-of-hormuz"},
	}) {
		t.Fatalf("expected residual path to keep execution on subject")
	}
}

func TestBuildMaterialSignalSubstrateGroupsByEvent(t *testing.T) {
	now := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	eventID := "evt-1"
	facts := []*coremodels.OntologyFact{
		{
			BaseOntologyFields: coremodels.BaseOntologyFields{
				ID:             "sig-1",
				CanonicalPaths: []string{"/org/no/equinor"},
			},
			Kind:       "signal",
			Type:       "risk:logistics/capacity:hormuz",
			Title:      "Hormuz disruption",
			Summary:    "Shipping lane disruption",
			EventID:    &eventID,
			ObservedAt: &now,
		},
		{
			BaseOntologyFields: coremodels.BaseOntologyFields{
				ID:             "sig-2",
				CanonicalPaths: []string{"/market/eu/energy/natural-gas"},
			},
			Kind:       "signal",
			Type:       "risk:logistics/capacity:hormuz",
			Title:      "Hormuz disruption",
			Summary:    "Market tightening",
			EventID:    &eventID,
			ObservedAt: &now,
		},
	}
	substrate := buildMaterialSignalSubstrate(facts, 10)
	if len(substrate.Bundles) != 1 {
		t.Fatalf("expected one grouped bundle, got %+v", substrate.Bundles)
	}
	if substrate.SignalCount != 2 {
		t.Fatalf("expected grouped signal count 2, got %+v", substrate)
	}
}

func TestCollapseWideRiskPointsMergesByOwnerAndPath(t *testing.T) {
	now := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	points := []WideRiskObserverPoint{
		{
			ID:             "p1",
			OwnerSubjectID: "lens-subject",
			SubjectID:      "focus-1",
			CanonicalPath:  "/org/no/equinor",
			Headline:       "Hormuz disruption",
			Hotness:        0.7,
			GeneratedAt:    now,
		},
		{
			ID:               "p2",
			OwnerSubjectID:   "lens-subject",
			SubjectID:        "focus-2",
			CanonicalPath:    "/org/no/equinor",
			Headline:         "Hormuz disruption",
			Hotness:          0.6,
			AffectedSubjects: []string{"/market/eu/energy/natural-gas"},
			GeneratedAt:      now,
		},
	}
	collapsed := collapseWideRiskPoints(points, 10)
	if len(collapsed) != 1 {
		t.Fatalf("expected merged point, got %+v", collapsed)
	}
	if len(collapsed[0].AffectedSubjects) != 1 {
		t.Fatalf("expected affected subject carried into merged point, got %+v", collapsed[0])
	}
}

func TestCollapseMaterialBundlesMergesByBundleID(t *testing.T) {
	now := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	substrate := collapseMaterialBundles([]MaterialSignalBundle{
		{
			ID:             "event:1",
			Headline:       "Hormuz disruption",
			SignalCount:    2,
			CanonicalPaths: []string{"/org/no/equinor"},
			SourceRefs:     []string{"src-1"},
			LatestObserved: now,
		},
		{
			ID:             "event:1",
			Headline:       "Hormuz disruption",
			SignalCount:    1,
			CanonicalPaths: []string{"/market/eu/energy/natural-gas"},
			SourceRefs:     []string{"src-2"},
			LatestObserved: now.Add(1 * time.Hour),
		},
	}, 10)
	if len(substrate.Bundles) != 1 {
		t.Fatalf("expected merged bundle, got %+v", substrate.Bundles)
	}
	if substrate.SignalCount != 3 || substrate.SourceCount != 2 {
		t.Fatalf("unexpected collapsed substrate %+v", substrate)
	}
}
