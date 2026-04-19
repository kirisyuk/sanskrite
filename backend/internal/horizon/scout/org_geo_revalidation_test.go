package scout

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestOrgGeoValidationQueries_AddsDeterministicHints(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/ua/ingas",
		Labels: map[string]string{
			"en": "Ingas LLC",
		},
	}

	queries := orgGeoValidationQueries(fragment)
	found := false
	for _, query := range queries {
		if query == "Ingas, Pivdenne, Odesa Oblast, Ukraine" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected deterministic Ingas query in %v", queries)
	}
}

func TestOrgGeoValidationQueries_UsesInferredPlaceHintFromName(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/cn/jabil-circuit-wuxi",
		Labels: map[string]string{
			"en": "Jabil Circuit Wuxi",
		},
	}

	queries := orgGeoValidationQueries(fragment)
	found := false
	for _, query := range queries {
		if query == "Jabil Circuit Wuxi, People's Republic of China" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected inferred-place org query in %v", queries)
	}
}

func TestShouldAcceptOrgGeoResult_RejectsCountryOnlyMatchWithoutPlace(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/ua/ingas",
		Labels: map[string]string{
			"en": "Ingas LLC",
		},
	}

	if shouldAcceptOrgGeoResult(fragment, "Ingas LLC, Ukraine", "Ukraine", "", "ua", "ua") {
		t.Fatal("expected country-only result to be rejected")
	}
}

func TestShouldAcceptOrgGeoResult_RejectsCountryOnlyMatchWhenCountryHintComesFromName(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/kz/intertop-kazakhstan",
		Labels: map[string]string{
			"en": "Intertop Kazakhstan",
		},
	}

	if shouldAcceptOrgGeoResult(fragment, "Intertop Kazakhstan, Kazakhstan", "Kazakhstan", "", "kz", "kz") {
		t.Fatal("expected country-only result to be rejected for org name ending with country")
	}
}

func TestShouldAcceptOrgGeoResult_AcceptsDeterministicSpecificMatch(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/ua/ingas",
		Labels: map[string]string{
			"en": "Ingas LLC",
		},
	}

	if !shouldAcceptOrgGeoResult(fragment, "Ingas, Pivdenne, Odesa Oblast, Ukraine", "Заводська, 3-і, Pivdenne, Odes'ka oblast, Ukraine, 65481", "", "ua", "ua") {
		t.Fatal("expected specific deterministic result to be accepted")
	}
}

func TestShouldAcceptOrgGeoResult_AcceptsSpecificPlaceMatchFromNameHint(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/cn/jabil-circuit-wuxi",
		Labels: map[string]string{
			"en": "Jabil Circuit Wuxi",
		},
	}

	if !shouldAcceptOrgGeoResult(fragment, "Jabil Circuit Wuxi, China", "Wuxi, Jiangsu, China", "CN", "cn", "cn") {
		t.Fatal("expected specific place result to be accepted")
	}
}

func TestShouldAcceptOrgGeoResult_AcceptsStrongNameSpecificCountryMatch(t *testing.T) {
	fragment := &models.HorizonFragment{
		Class:          FragmentTypeOrganization,
		CanonicalPath: "/org/cn/envision-group",
		Labels: map[string]string{
			"en": "Envision Group",
		},
	}

	if !shouldAcceptOrgGeoResult(fragment, "Envision Group, China", "Shanghai, China", "", "cn", "cn") {
		t.Fatal("expected strong-name country query to be accepted")
	}
}

func TestIsWeakGeoResolveQuery_RejectsGenericSingleTokenHints(t *testing.T) {
	if !isWeakGeoResolveQuery("Launch, CN") {
		t.Fatal("expected generic launch query to be weak")
	}
	if !isWeakGeoResolveQuery("PDF, Myanmar") {
		t.Fatal("expected acronym query to be weak")
	}
	if isWeakGeoResolveQuery("Envision Group, China") {
		t.Fatal("expected specific organization query to remain allowed")
	}
}
