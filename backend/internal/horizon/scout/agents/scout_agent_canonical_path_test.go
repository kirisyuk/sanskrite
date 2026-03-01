package agents

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestEnsureFragmentCanonicalPath_UsesTrimmedTargetForWhitespacePath(t *testing.T) {
	fragment := &models.HorizonFragment{CanonicalPath: "   "}

	ensureFragmentCanonicalPath(fragment, "  /org/eg/general-motors-egypt  ")

	if fragment.CanonicalPath != "/org/eg/general-motors-egypt" {
		t.Fatalf("canonicalPath = %q, want %q", fragment.CanonicalPath, "/org/eg/general-motors-egypt")
	}
}

func TestEnsureFragmentCanonicalPath_TrimsExistingPath(t *testing.T) {
	fragment := &models.HorizonFragment{CanonicalPath: "  /org/eg/general-motors-egypt  "}

	ensureFragmentCanonicalPath(fragment, "/org/us/ignored")

	if fragment.CanonicalPath != "/org/eg/general-motors-egypt" {
		t.Fatalf("canonicalPath = %q, want %q", fragment.CanonicalPath, "/org/eg/general-motors-egypt")
	}
}
