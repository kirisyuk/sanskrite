package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// canonicalPrefixToClass enumerates the canonical-path prefixes the runtime
// recognizes when reconciling a path back to an AH (Class, Subtype) tuple.
var canonicalPrefixToClass = []struct {
	prefix  string
	class   ah.FragmentClass
	subtype string
}{
	// Modern AH prefixes (preferred going forward).
	{"/org/", ah.FragmentClassOrg, ""},
	{"/site/", ah.FragmentClassSite, ""},
	{"/geo/", ah.FragmentClassGeo, ""},
	{"/product/", ah.FragmentClassProduct, ""},
	{"/market/", ah.FragmentClassMarket, ""},
	{"/community/", ah.FragmentClassCommunity, ""},
	{"/person/", ah.FragmentClassPerson, ""},
	{"/decision/", ah.FragmentClassDecision, ""},
	{"/mechanism/", ah.FragmentClassMechanism, ""},
	{"/evidence/", ah.FragmentClassEvidence, ""},
	{"/mobile_asset/", ah.FragmentClassMobileAsset, ""},
	{"/regime/", ah.FragmentClassRegime, ""},
	{"/route/", ah.FragmentClassRoute, ""},
	{"/vessel_class/", ah.FragmentClassVesselClass, ""},
	{"/project/", ah.FragmentClassProject, ""},

	// Legacy prefixes — collapsed via docs/archive/taxonomy-migration-2026/migration_map.yaml.
	{"/gov/", ah.FragmentClassOrg, "gov"},
	{"/program/", ah.FragmentClassOrg, "program"},
	{"/media/", ah.FragmentClassOrg, "media_channel"},
	{"/fac/", ah.FragmentClassSite, ""},
	{"/loc/", ah.FragmentClassGeo, ""},
	{"/region/", ah.FragmentClassGeo, ""},
	{"/zone/", ah.FragmentClassGeo, "affected_zone"},
	{"/deposit/", ah.FragmentClassSite, "deposit"},
	{"/track/", ah.FragmentClassMobileAsset, ""},
	{"/supply-link/", ah.FragmentClassSite, "supply_link"},
}

// ExpectedClassFromCanonicalPath returns the AH FragmentClass and
// optional subtype implied by a canonical path prefix.
func ExpectedClassFromCanonicalPath(path string) (ah.FragmentClass, string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", false
	}
	lower := strings.ToLower(path)
	for _, entry := range canonicalPrefixToClass {
		if strings.HasPrefix(lower, entry.prefix) {
			return entry.class, entry.subtype, true
		}
	}
	return "", "", false
}

// ReconcileClassWithCanonicalPath returns a reconciled (class, subtype)
// when the canonical path implies a different class than the supplied
// `current` value. The boolean second result is true when reconciliation
// changed the class. When the path is unknown the input is echoed back.
func ReconcileClassWithCanonicalPath(current ah.FragmentClass, path string) (ah.FragmentClass, string, bool) {
	expected, subtype, ok := ExpectedClassFromCanonicalPath(path)
	if !ok || expected == "" {
		return current, "", false
	}
	if current == "" || current != expected {
		return expected, subtype, true
	}
	return current, subtype, false
}
