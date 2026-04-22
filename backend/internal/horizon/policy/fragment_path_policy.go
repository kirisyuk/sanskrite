package policy

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// fragmentPathPrefixes maps an AH (Class, subtype) pair to its canonical
// path prefix. Subtype="" means the entry applies to any subtype of the
// class. More specific (Class, subtype) entries override the bare-class
// entry.
type classSubtypeKey struct {
	Class   ah.FragmentClass
	Subtype string
}

var fragmentPathPrefixes = map[classSubtypeKey]string{
	{Class: ah.FragmentClassOrg}:                              "/org/",
	{Class: ah.FragmentClassOrg, Subtype: subtypeProgram}:     "/project/",
	{Class: ah.FragmentClassOrg, Subtype: "media_channel"}:    "/media/",
	{Class: ah.FragmentClassSite}:                             "/fac/",
	{Class: ah.FragmentClassSite, Subtype: subtypeDeposit}:    "/deposit/",
	{Class: ah.FragmentClassSite, Subtype: subtypeSupplyLink}: "/supply-link/",
	{Class: ah.FragmentClassGeo}:                              "/loc/",
	{Class: ah.FragmentClassGeo, Subtype: "affected_zone"}:    "/zone/",
	{Class: ah.FragmentClassProduct}:                          "/product/",
	{Class: ah.FragmentClassPerson}:                           "/person/",
	{Class: ah.FragmentClassCommunity}:                        "/community/",
	{Class: ah.FragmentClassMarket}:                           "/market/",
	{Class: ah.FragmentClassMobileAsset}:                      "/track/",
	{Class: ah.FragmentClassRegime}:                           "/regime/",
	{Class: ah.FragmentClassRoute}:                            "/route/",
	{Class: ah.FragmentClassVesselClass}:                      "/vessel_class/",
	{Class: ah.FragmentClassProject}:                          "/project/",
	{Class: ah.FragmentClassDecision}:                         "/decision/",
	{Class: ah.FragmentClassMechanism}:                        "/mechanism/",
	{Class: ah.FragmentClassEvidence}:                         "/evidence/",
}

// ExpectedCanonicalPathPrefix returns the canonical path prefix for a
// fragment (class, subtype). Falls back to the bare-class prefix when no
// specialized subtype prefix is registered.
func ExpectedCanonicalPathPrefix(class ah.FragmentClass, subtype string) string {
	subtype = strings.TrimSpace(subtype)
	if prefix, ok := fragmentPathPrefixes[classSubtypeKey{Class: class, Subtype: subtype}]; ok && prefix != "" {
		return prefix
	}
	return fragmentPathPrefixes[classSubtypeKey{Class: class}]
}

// IsCanonicalPathAllowedForType checks the canonical path prefix with
// known AH-aware exceptions (notably: org subtype=gov uses /gov/).
func IsCanonicalPathAllowedForType(class ah.FragmentClass, subtype, canonicalPath string) bool {
	path := strings.TrimSpace(canonicalPath)
	if path == "" {
		return false
	}
	expectedPrefix := ExpectedCanonicalPathPrefix(class, subtype)
	if expectedPrefix == "" {
		return true
	}
	if strings.HasPrefix(path, expectedPrefix) {
		return true
	}
	// Government bodies stay class=org with subtype=gov but use the /gov/ namespace.
	if class == ah.FragmentClassOrg && strings.HasPrefix(path, "/gov/") {
		return true
	}
	// Legacy site subtypes may appear under the bare /fac/ prefix during
	// the transitional window when subtype hasn't been backfilled yet.
	if class == ah.FragmentClassSite && strings.HasPrefix(path, "/fac/") {
		return true
	}
	return false
}
