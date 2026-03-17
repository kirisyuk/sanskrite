package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestShouldAutoAddRelationTargetToScope(t *testing.T) {
	if !shouldAutoAddRelationTargetToScope("/org/us/tesla", models.FragmentTypeOrganization, true) {
		t.Fatal("allowed organization target must be eligible for scope auto-add")
	}
	if shouldAutoAddRelationTargetToScope("/org/us/tesla", models.FragmentTypeOrganization, false) {
		t.Fatal("disallowed relation must not leak target into scope")
	}
	if shouldAutoAddRelationTargetToScope("/event/test", models.FragmentTypeEvent, true) {
		t.Fatal("non-eligible fragment types must not be auto-added to scope")
	}
}
