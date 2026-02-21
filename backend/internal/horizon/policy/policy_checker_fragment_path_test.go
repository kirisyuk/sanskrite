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
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/person/khaled-bin-mohamed-bin-zayed-al-nahyan",
	}

	violations := pc.checkFragmentPath(context.Background(), frag)

	var hasMissingLegalForm bool
	var hasTypePathMismatch bool
	for _, v := range violations {
		if v.Type == ViolationMissingLegalForm {
			hasMissingLegalForm = true
		}
		if v.Type == ViolationTypeMismatch && v.Rule == "fragment_type_path_namespace" {
			hasTypePathMismatch = true
		}
	}

	if hasMissingLegalForm {
		t.Fatalf("did not expect missing legal form violation for person canonical path")
	}
	if !hasTypePathMismatch {
		t.Fatalf("expected type/path mismatch violation for ORGANIZATION type with /person/ path")
	}
}

func TestCheckFragmentPath_OrgPathStillChecksMissingLegalForm(t *testing.T) {
	pc := NewPolicyChecker(nil, nil)
	frag := &horizonmodels.HorizonFragment{
		ID:            "frag-org",
		Type:          horizonmodels.FragmentTypeOrganization,
		CanonicalPath: "/org/us/acme",
		Labels:        map[string]string{"en": "Acme"},
	}

	violations := pc.checkFragmentPath(context.Background(), frag)
	found := false
	for _, v := range violations {
		if v.Type == ViolationMissingLegalForm && v.Rule == "org_legal_form" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing legal form violation for org path without legal form suffix")
	}
}
