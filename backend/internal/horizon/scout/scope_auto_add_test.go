package scout

import (
	"testing"

)

func TestShouldAutoAddRelationTargetToScope(t *testing.T) {
	if !shouldAutoAddRelationTargetToScope("/org/us/tesla", FragmentTypeOrganization, true) {
		t.Fatal("allowed organization target must be eligible for scope auto-add")
	}
	if shouldAutoAddRelationTargetToScope("/org/us/tesla", FragmentTypeOrganization, false) {
		t.Fatal("disallowed relation must not leak target into scope")
	}
	if shouldAutoAddRelationTargetToScope("/event/test", FragmentTypeEvent, true) {
		t.Fatal("non-eligible fragment types must not be auto-added to scope")
	}
}
