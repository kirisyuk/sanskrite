package repo

import "testing"

func TestUnitDiveResultsRepositoryBuildResultID(t *testing.T) {
	r := &UnitDiveResultsRepository{}
	id1 := r.BuildResultID("LensA", "Req-1")
	id2 := r.BuildResultID("lensa", "req-1")
	if id1 == "" {
		t.Fatalf("expected non-empty id")
	}
	if id1 != id2 {
		t.Fatalf("ids must be deterministic and case-insensitive: %q vs %q", id1, id2)
	}
}
