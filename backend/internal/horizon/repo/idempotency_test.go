package repo

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestFragmentPathClaimDocIDMatchesCanonicalPathDocID(t *testing.T) {
	tid := "global"
	cp := "/org/qa/qatarenergy"
	if got, want := fragmentPathClaimDocID(tid, cp), canonicalPathDocID(tid, cp); got != want {
		t.Fatalf("fragmentPathClaimDocID=%q want=%q", got, want)
	}
}

func TestEdgeIdentityVariantStable(t *testing.T) {
	edge := &models.DependencyEdge{
		Qualifiers: map[string]interface{}{
			"edgeModality":   "actual",
			"facet":          "logistics",
			"assurance_type": "audit",
		},
	}
	got := edgeIdentityVariant(edge)
	want := "assurance_type=audit|edge_modality=actual|facet=logistics"
	if got != want {
		t.Fatalf("edgeIdentityVariant=%q want=%q", got, want)
	}
}

func TestNormalizedEdgeIdentitySymmetric(t *testing.T) {
	s1, t1, ty1, v1, id1 := normalizedEdgeIdentity("global", "b", "a", ah.RelationName("associated_with"), "")
	s2, t2, ty2, v2, id2 := normalizedEdgeIdentity("global", "a", "b", ah.RelationName("associated_with"), "")
	if s1 != "a" || t1 != "b" || ty1 != ah.RelationName("associated_with") || v1 != "" {
		t.Fatalf("unexpected normalized identity 1: %s %s %s %s", s1, t1, ty1, v1)
	}
	if s2 != "a" || t2 != "b" || ty2 != ah.RelationName("associated_with") || v2 != "" {
		t.Fatalf("unexpected normalized identity 2: %s %s %s %s", s2, t2, ty2, v2)
	}
	if id1 != id2 {
		t.Fatalf("expected symmetric identity ids to match: %q vs %q", id1, id2)
	}
}

func TestMergeEdgeForIdempotentUpsert(t *testing.T) {
	now := time.Now().UTC()
	dst := &models.DependencyEdge{
		ID: "edge_1",
		Properties: models.EdgeProperties{
			Criticality:   0.4,
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"facet": "logistics",
		},
		Provenance: models.Provenance{
			Method:    "rules",
			SourceIDs: []string{"s1"},
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
		Metrics: map[string]models.MetricValue{
			"share": {Value: 0.2, Confidence: 0.5},
		},
	}
	src := &models.DependencyEdge{
		Properties: models.EdgeProperties{
			Criticality:   0.8,
			Amplification: 1.3,
			LatencyHours:  12,
		},
		Qualifiers: map[string]interface{}{
			"edgeModality": "actual",
		},
		Provenance: models.Provenance{
			SourceIDs: []string{"s1", "s2"},
		},
		Metrics: map[string]models.MetricValue{
			"share":  {Value: 0.3, Confidence: 0.9},
			"volume": {Value: 10, Confidence: 0.7},
		},
	}
	if !mergeEdgeForIdempotentUpsert(dst, src) {
		t.Fatal("expected mergeEdgeForIdempotentUpsert to report changes")
	}
	if got := dst.Properties.Criticality; got != 0.8 {
		t.Fatalf("criticality=%v want=0.8", got)
	}
	if got := dst.Properties.Amplification; got != 1.3 {
		t.Fatalf("amplification=%v want=1.3", got)
	}
	if got := dst.Properties.LatencyHours; got != 12 {
		t.Fatalf("latency=%v want=12", got)
	}
	if got := dst.Qualifiers["edgeModality"]; got != "actual" {
		t.Fatalf("edgeModality=%v want=actual", got)
	}
	if len(dst.Provenance.SourceIDs) != 2 {
		t.Fatalf("source ids=%v want 2 ids", dst.Provenance.SourceIDs)
	}
	if got := dst.Metrics["share"].Confidence; got != 0.9 {
		t.Fatalf("share confidence=%v want=0.9", got)
	}
	if _, ok := dst.Metrics["volume"]; !ok {
		t.Fatal("expected volume metric to be merged")
	}
}
