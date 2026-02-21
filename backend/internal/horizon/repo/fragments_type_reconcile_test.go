package repo

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestBestEffortReconcileFragmentTypeFromPath_PersonPath(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypeOrganization,
		CanonicalPath: "/person/jane-doe",
	}

	bestEffortReconcileFragmentTypeFromPath(frag)

	if frag.Type != models.FragmentTypePerson {
		t.Fatalf("expected type %s, got %s", models.FragmentTypePerson, frag.Type)
	}
}

func TestBestEffortReconcileFragmentTypeFromPath_GovPath(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypePerson,
		CanonicalPath: "/gov/ae/ministry-of-finance",
	}

	bestEffortReconcileFragmentTypeFromPath(frag)

	if frag.Type != models.FragmentTypeOrganization {
		t.Fatalf("expected type %s for gov path, got %s", models.FragmentTypeOrganization, frag.Type)
	}
}

func TestBestEffortReconcileFragmentTypeFromPath_UnknownPathKeepsType(t *testing.T) {
	frag := &models.HorizonFragment{
		Type:          models.FragmentTypePerson,
		CanonicalPath: "/unknown/foo",
	}

	bestEffortReconcileFragmentTypeFromPath(frag)

	if frag.Type != models.FragmentTypePerson {
		t.Fatalf("expected type to remain %s, got %s", models.FragmentTypePerson, frag.Type)
	}
}
