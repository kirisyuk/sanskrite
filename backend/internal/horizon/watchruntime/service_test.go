package watchruntime

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
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
