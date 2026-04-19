package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func dedupNamespaceFamilyFromPath(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	ns := strings.ToLower(strings.TrimSpace(parts[0]))
	switch ns {
	case "gov":
		return "org"
	case "region":
		return "loc"
	default:
		return ns
	}
}

// dedupExpectedFamilyForType maps an AH FragmentClass to its canonical
// namespace family token (the leading segment of canonical paths under that
// class). Subtype-bearing legacy classes (program/media_channel/deposit/
// supply_link/affected_zone/domain/sphere) cannot be distinguished from
// their parent class via this Class-only API; callers needing that
// distinction should call dedupExpectedFamilyForClassSubtype with both
// Class and Subtype.
func dedupExpectedFamilyForType(fragType FragmentType) string {
	switch fragType {
	case FragmentTypeOrganization:
		return "org"
	case FragmentTypeFacility:
		return "fac"
	case FragmentTypeGeoRegion:
		return "loc"
	case FragmentTypeProduct:
		return "product"
	case FragmentTypePerson:
		return "person"
	case FragmentTypeMarket:
		return "market"
	case FragmentTypeDecisionPoint:
		return "decision"
	case FragmentTypeMechanism:
		return "mechanism"
	case FragmentTypeCommunity:
		return "community"
	case FragmentTypeEvent:
		return "event"
	case FragmentTypeTrack:
		return "track"
	default:
		return ""
	}
}

// dedupExpectedFamilyForClassSubtype returns the canonical namespace family
// for the given (Class, Subtype) tuple, including the legacy subtype-bearing
// values (program → "program", media_channel → "media", deposit → "deposit",
// supply_link → "supply-link", affected_zone → "zone", domain → "domain",
// sphere → "sphere"). Falls back to dedupExpectedFamilyForType for plain
// classes.
func dedupExpectedFamilyForClassSubtype(class FragmentType, subtype string) string {
	switch {
	case class == FragmentTypeOrganization && subtype == "program":
		return "program"
	case class == FragmentTypeOrganization && subtype == "media_channel":
		return "media"
	case class == FragmentTypeFacility && subtype == "deposit":
		return "deposit"
	case class == FragmentTypeFacility && subtype == "supply_link":
		return "supply-link"
	case class == FragmentTypeGeoRegion && subtype == "affected_zone":
		return "zone"
	case class == FragmentTypeEvent && subtype == "domain":
		return "domain"
	case class == FragmentTypeEvent && subtype == "sphere":
		return "sphere"
	default:
		return dedupExpectedFamilyForType(class)
	}
}

func dedupFamilyForRequest(fragType FragmentType, canonicalPath string) string {
	if fam := dedupNamespaceFamilyFromPath(canonicalPath); fam != "" {
		return fam
	}
	return dedupExpectedFamilyForType(fragType)
}

func dedupFamilyForFragment(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if fam := dedupNamespaceFamilyFromPath(f.CanonicalPath); fam != "" {
		return fam
	}
	return dedupExpectedFamilyForClassSubtype(f.Class, f.Subtype)
}

func dedupHasHierarchyConflict(requestedCanonicalPath, replacementCanonicalPath string) bool {
	req := strings.TrimSpace(requestedCanonicalPath)
	rep := strings.TrimSpace(replacementCanonicalPath)
	if req == "" || rep == "" || req == rep {
		return false
	}
	// Parent/child path pairs are taxonomy hierarchy, not duplicates.
	return strings.HasPrefix(req, rep+"/") || strings.HasPrefix(rep, req+"/")
}

// isDedupReplacementCompatible enforces conservative replacement safety:
// - same FragmentType when both are known;
// - same canonical namespace family (gov->org, region->loc) when both are known.
// - no strict parent/child canonical-path hierarchy replacement.
func isDedupReplacementCompatible(requestedType FragmentType, requestedCanonicalPath string, replacement *models.HorizonFragment) bool {
	if replacement == nil {
		return false
	}
	if requestedType != "" && replacement.Class != "" && requestedType != replacement.Class {
		return false
	}
	reqFam := dedupFamilyForRequest(requestedType, requestedCanonicalPath)
	repFam := dedupFamilyForFragment(replacement)
	if reqFam != "" && repFam != "" && reqFam != repFam {
		return false
	}
	if dedupHasHierarchyConflict(requestedCanonicalPath, replacement.CanonicalPath) {
		return false
	}
	return true
}
