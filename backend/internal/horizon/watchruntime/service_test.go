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
	}); !decision.WarmOnly || decision.WakeScopeAgent {
		t.Fatalf("expected weak signal to warm only, got %+v", decision)
	}

	if decision := svc.AssessTrigger(subject, &horizonmodels.WatchTrigger{
		SubjectID:      subject.ID,
		TriggerType:    horizonmodels.WatchTriggerFactDelta,
		DedupeKey:      "t2",
		SignalStrength: 0.85,
		Confidence:     0.90,
	}); !decision.WakeScopeAgent || decision.Noop {
		t.Fatalf("expected strong signal to wake scope agent, got %+v", decision)
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
