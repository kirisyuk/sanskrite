package repo

import "testing"

func TestUnitGraphSnapshotsRepositoryBuildSnapshotID(t *testing.T) {
	r := &UnitGraphSnapshotsRepository{}
	id1 := r.BuildSnapshotID("TenantA", "LensA", "/domain/v1/supply-chain/subject/huawei")
	id2 := r.BuildSnapshotID("tenanta", "lensa", "/domain/v1/supply-chain/subject/huawei")
	if id1 == "" {
		t.Fatalf("expected non-empty id")
	}
	if id1 != id2 {
		t.Fatalf("ids must be deterministic and case-insensitive: %q vs %q", id1, id2)
	}
}
