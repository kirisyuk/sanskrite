package policy

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestCheckFragmentPath_PathNamespaceWinsForPersonPath(t *testing.T) {
	pc := NewPolicyChecker(nil, nil)
	frag := &horizonmodels.HorizonFragment{
		ID:            "frag-person",
		Class: classOrganization,
		CanonicalPath: "/person/khaled-bin-mohamed-bin-zayed-al-nahyan",
	}

	violations := pc.checkFragmentPath(context.Background(), frag)

	var hasTypePathMismatch bool
	for _, v := range violations {
		if v.Type == ViolationTypeMismatch && v.Rule == "fragment_type_path_namespace" {
			hasTypePathMismatch = true
			break
		}
	}

	if !hasTypePathMismatch {
		t.Fatalf("expected type/path mismatch violation for ORGANIZATION type with /person/ path")
	}
}

func TestCheckFragmentPath_OrgPathNoLegalFormRequired(t *testing.T) {
	// Legal form is no longer required; /org/{geo}/{slug} with base name only is valid.
	pc := NewPolicyChecker(nil, nil)
	frag := &horizonmodels.HorizonFragment{
		ID:            "frag-org",
		Class: classOrganization,
		CanonicalPath: "/org/us/acme",
		Labels:        map[string]string{"en": "Acme"},
	}

	violations := pc.checkFragmentPath(context.Background(), frag)
	for _, v := range violations {
		if v.Rule == "org_path_format" || v.Type == ViolationPathFormat {
			t.Fatalf("org path without legal form should stay valid; got violation: %s", v.Description)
		}
	}
}
