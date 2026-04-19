package repo

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestBestEffortReconcileFragmentTypeFromPath_PersonPath(t *testing.T) {
	frag := &models.HorizonFragment{
		Class:         ah.FragmentClassOrg,
		CanonicalPath: "/person/jane-doe",
	}

	bestEffortReconcileFragmentTypeFromPath(frag)

	if frag.Class != ah.FragmentClassPerson {
		t.Fatalf("expected class %s, got %s", ah.FragmentClassPerson, frag.Class)
	}
}

func TestBestEffortReconcileFragmentTypeFromPath_GovPath(t *testing.T) {
	frag := &models.HorizonFragment{
		Class:         ah.FragmentClassPerson,
		CanonicalPath: "/gov/ae/ministry-of-finance",
	}

	bestEffortReconcileFragmentTypeFromPath(frag)

	if frag.Class != ah.FragmentClassOrg {
		t.Fatalf("expected class %s for gov path, got %s", ah.FragmentClassOrg, frag.Class)
	}
}

func TestBestEffortReconcileFragmentTypeFromPath_UnknownPathKeepsType(t *testing.T) {
	frag := &models.HorizonFragment{
		Class:         ah.FragmentClassPerson,
		CanonicalPath: "/unknown/foo",
	}

	bestEffortReconcileFragmentTypeFromPath(frag)

	if frag.Class != ah.FragmentClassPerson {
		t.Fatalf("expected class to remain %s, got %s", ah.FragmentClassPerson, frag.Class)
	}
}
