package repo

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBuildWatchSubjectIDStable(t *testing.T) {
	id1 := BuildWatchSubjectID("global", "proj-1", "lens-1", "", "", horizonmodels.WatchSubjectKindLensUniverse, "/lens/lens-1")
	id2 := BuildWatchSubjectID("global", "proj-1", "lens-1", "", "", horizonmodels.WatchSubjectKindLensUniverse, "/lens/lens-1")
	if id1 == "" || id2 == "" {
		t.Fatalf("expected deterministic ids")
	}
	if id1 != id2 {
		t.Fatalf("expected identical ids, got %q vs %q", id1, id2)
	}
}

func TestBuildWatchSubjectIDDiffersByAnchor(t *testing.T) {
	id1 := BuildWatchSubjectID("global", "", "", "", "", horizonmodels.WatchSubjectKindExplicitTarget, "/org/qa/qatarenergy")
	id2 := BuildWatchSubjectID("global", "", "", "", "", horizonmodels.WatchSubjectKindExplicitTarget, "/org/qa/nakilat")
	if id1 == id2 {
		t.Fatalf("expected different ids for different anchors")
	}
}
