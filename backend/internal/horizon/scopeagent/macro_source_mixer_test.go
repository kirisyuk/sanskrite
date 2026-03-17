package scopeagent

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestBuildMacroSourceBundleIncludesArtifactAndQueries(t *testing.T) {
	cfg := &horizonmodels.ScopeAgentConfig{
		SubjectID:          "subject-1",
		ScopeCanonicalPath: "/org/us/tesla",
		DomainModes:        []string{"supply_chain", "regulatory"},
		Metadata: map[string]any{
			"macroAgentClass":      "entity",
			"watchSubjectId":       "subject-1",
			"lensId":               "lens-1",
			"artifactCanonicalKey": "/supply-chain/test/path-1",
			"sourceMixPolicy":      map[string]any{"lookbackDays": 14},
		},
	}
	subject := &horizonmodels.WatchSubject{
		ID:           "subject-1",
		DisplayLabel: "Tesla",
		Metadata: map[string]any{
			"timeWindowPolicy": map[string]any{"lookbackDays": 7},
		},
	}
	artifact := &horizonmodels.MaterializedArtifact{
		ID:               "artifact-1",
		CanonicalKey:     "/supply-chain/test/path-1",
		Kind:             horizonmodels.MaterializedKindSupplyChainPath,
		DisplayName:      "Tesla battery path",
		ProductAxis:      "battery",
		NodeRefs:         []string{"/org/us/tesla"},
		AffectedZoneRefs: []string{"/zone/red-sea"},
	}
	triggers := []*horizonmodels.WatchTrigger{
		{TriggerReason: "port congestion"},
	}
	facts := []*coremodels.OntologyFact{
		{
			SourceRefs: []coremodels.SourceRef{
				{SourceID: "src-1", SourceType: coremodels.SourceTypeWeb},
				{SourceID: "src-1", SourceType: coremodels.SourceTypeWeb},
				{SourceID: "src-2", SourceType: coremodels.SourceTypeRSS},
			},
		},
	}

	bundle, queries := buildMacroSourceBundle(cfg, subject, artifact, triggers, facts)
	if bundle == nil {
		t.Fatal("expected non-nil source bundle")
	}
	if got := bundle["macroAgentClass"]; got != "entity" {
		t.Fatalf("expected macroAgentClass=entity, got %#v", got)
	}
	searchWindow, ok := bundle["searchWindow"].(map[string]any)
	if !ok || searchWindow["days"] != 7 {
		t.Fatalf("expected 7-day search window from subject policy, got %#v", bundle["searchWindow"])
	}
	if len(queries) == 0 {
		t.Fatal("expected expanded queries")
	}
	foundTriggerQuery := false
	for _, query := range queries {
		if query == "Tesla port congestion last 7 days (high recency)" {
			foundTriggerQuery = true
			break
		}
	}
	if !foundTriggerQuery {
		t.Fatalf("expected trigger follow-up query in %#v", queries)
	}
	sourceRefs, ok := bundle["sourceRefs"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any sourceRefs, got %#v", bundle["sourceRefs"])
	}
	if len(sourceRefs) != 2 {
		t.Fatalf("expected deduped source refs, got %#v", sourceRefs)
	}
	if bundle["artifactSummary"] == nil {
		t.Fatal("expected artifact summary in source bundle")
	}
}

func TestDerivedProposalTargetPathsDropsArtifactPaths(t *testing.T) {
	ctxItem := &runContext{
		Subject: &horizonmodels.WatchSubject{
			SubjectKind: horizonmodels.WatchSubjectKindPathOverlay,
			Metadata: map[string]any{
				"backingCanonicalPaths": []string{"/org/us/tesla", "/product/battery"},
			},
		},
		Artifact: &horizonmodels.MaterializedArtifact{
			NodeRefs: []string{"/org/us/tesla", "/product/battery"},
		},
	}

	paths := derivedProposalTargetPaths(ctxItem, []string{"/artifact/supply-chain/test/path-1"})
	if len(paths) != 2 {
		t.Fatalf("expected backing canonical paths only, got %#v", paths)
	}
	for _, path := range paths {
		if path == "/artifact/supply-chain/test/path-1" {
			t.Fatalf("artifact path leaked into canonical targets: %#v", paths)
		}
	}
}
