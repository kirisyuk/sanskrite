package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"testing"
)

func TestShouldAutoAddRelationTargetToScope(t *testing.T) {
	if !shouldAutoAddRelationTargetToScope("/org/us/tesla", ah.FragmentClassOrg, true) {
		t.Fatal("allowed organization target must be eligible for scope auto-add")
	}
	if shouldAutoAddRelationTargetToScope("/org/us/tesla", ah.FragmentClassOrg, false) {
		t.Fatal("disallowed relation must not leak target into scope")
	}
	if shouldAutoAddRelationTargetToScope("/event/test", ah.FragmentClassEvidence, true) {
		t.Fatal("non-eligible fragment types must not be auto-added to scope")
	}
}
