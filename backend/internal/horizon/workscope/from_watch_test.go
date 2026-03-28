package workscope

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestFromWatchSubject_lensUniverse(t *testing.T) {
	s := &horizonmodels.WatchSubject{
		TenantID:            "t1",
		ProjectID:           "p1",
		LensID:              "L1",
		SubjectKind:         horizonmodels.WatchSubjectKindLensUniverse,
		AnchorCanonicalPath: "/lens/meta",
		UniversePaths:       []string{"/geo/US", "/geo/CA"},
	}
	got := FromWatchSubject(s)
	if got.Refs.LensID != "L1" {
		t.Fatalf("lens %q", got.Refs.LensID)
	}
	if len(got.Refs.ScopePaths) != 3 {
		t.Fatalf("paths %v", got.Refs.ScopePaths)
	}
	if got.Intent.ScopeKind != ScopeKindWatchSubject {
		t.Fatalf("intent %+v", got.Intent)
	}
	req := RequestFromWatchSubject(s)
	if req.Intent.EntryPoint != EntryPointWatch || req.Intent.Action != ActionDeriveRisk {
		t.Fatalf("request intent %+v", req.Intent)
	}
	if !req.Intent.Projection.IncludePropagatedReach {
		t.Fatalf("expected propagated projection policy")
	}
}

func TestWatchScopeIntentDigest_stable(t *testing.T) {
	s := &horizonmodels.WatchSubject{
		TenantID:            "t1",
		LensID:              "L1",
		SubjectKind:         horizonmodels.WatchSubjectKindFragment,
		AnchorCanonicalPath: "/org/x",
	}
	d1 := WatchScopeIntentDigest(s)
	d2 := WatchScopeIntentDigest(s)
	if d1 != d2 || len(d1) != 64 {
		t.Fatalf("digest %q", d1)
	}
}
