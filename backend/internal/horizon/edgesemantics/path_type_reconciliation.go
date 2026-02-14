package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var canonicalPrefixToType = []struct {
	prefix string
	typ    models.FragmentType
}{
	{prefix: "/org/", typ: models.FragmentTypeOrganization},
	{prefix: "/gov/", typ: models.FragmentTypeOrganization}, // GOV namespace is modeled as ORGANIZATION.
	{prefix: "/fac/", typ: models.FragmentTypeFacility},
	{prefix: "/loc/", typ: models.FragmentTypeGeoRegion},
	{prefix: "/person/", typ: models.FragmentTypePerson},
	{prefix: "/product/", typ: models.FragmentTypeProduct},
	{prefix: "/market/", typ: models.FragmentTypeMarket},
	{prefix: "/deposit/", typ: models.FragmentTypeDeposit},
	{prefix: "/community/", typ: models.FragmentTypeCommunity},
	{prefix: "/media/", typ: models.FragmentTypeMediaChannel},
	{prefix: "/domain/", typ: models.FragmentTypeDomain},
	{prefix: "/program/", typ: models.FragmentTypeProgram},
	{prefix: "/decision/", typ: models.FragmentTypeDecisionPoint},
	{prefix: "/mechanism/", typ: models.FragmentTypeMechanism},
	{prefix: "/event/", typ: models.FragmentTypeEvent},
	{prefix: "/sphere/", typ: models.FragmentTypeSphere},
	{prefix: "/supply-link/", typ: models.FragmentTypeSupplyLink},
	{prefix: "/track/", typ: models.FragmentTypeTrack},
}

// ExpectedTypeFromCanonicalPath returns expected FragmentType by canonicalPath prefix.
func ExpectedTypeFromCanonicalPath(path string) (models.FragmentType, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	for _, item := range canonicalPrefixToType {
		if strings.HasPrefix(path, item.prefix) {
			return item.typ, true
		}
	}
	return "", false
}

// ReconcileTypeWithCanonicalPath returns a reconciled type when path implies another type.
func ReconcileTypeWithCanonicalPath(current models.FragmentType, path string) (models.FragmentType, bool) {
	expected, ok := ExpectedTypeFromCanonicalPath(path)
	if !ok || expected == "" {
		return current, false
	}
	if current == "" || current != expected {
		return expected, true
	}
	return current, false
}
