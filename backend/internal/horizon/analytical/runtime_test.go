package analytical

import (
	"testing"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestCompileWatchSubjectNormalizesRuntimeProjection(t *testing.T) {
	subject := &hmodels.WatchSubject{
		ID:                  "wsub_1",
		TenantID:            "global",
		LensID:              "lens-a",
		LensBindings:        []string{"lens-b", "lens-a"},
		AnchorCanonicalPath: "/fac/de/port-of-hamburg",
		UniversePaths:       []string{"/fac/de/port-of-hamburg", "/fac/de/port-of-bremerhaven"},
		RuntimeMode:         hmodels.WatchSubjectRuntimeCombined,
		Priority:            80,
	}

	compiled := CompileWatchSubject(subject, []string{"fam-2", "fam-1", "fam-1"}, []string{"/scope/lens-a", "/scope/lens-a"})
	if compiled == nil {
		t.Fatalf("expected compiled watch subject")
	}
	if compiled.WatchSubjectID != "wsub_1" {
		t.Fatalf("unexpected watch subject id: %q", compiled.WatchSubjectID)
	}
	if len(compiled.LensIDs) != 2 || compiled.LensIDs[0] != "lens-a" || compiled.LensIDs[1] != "lens-b" {
		t.Fatalf("unexpected lens ids: %#v", compiled.LensIDs)
	}
	if len(compiled.CompiledRefSet) != 2 {
		t.Fatalf("unexpected compiled ref set: %#v", compiled.CompiledRefSet)
	}
	if len(compiled.CompiledPathFamilyIDs) != 2 || compiled.CompiledPathFamilyIDs[0] != "fam-1" || compiled.CompiledPathFamilyIDs[1] != "fam-2" {
		t.Fatalf("unexpected compiled path family ids: %#v", compiled.CompiledPathFamilyIDs)
	}
	if len(compiled.CompiledAggregateScopeRefs) != 1 || compiled.CompiledAggregateScopeRefs[0] != "/scope/lens-a" {
		t.Fatalf("unexpected aggregate scope refs: %#v", compiled.CompiledAggregateScopeRefs)
	}
	if compiled.BasisDigest == "" {
		t.Fatalf("expected basis digest")
	}
}
