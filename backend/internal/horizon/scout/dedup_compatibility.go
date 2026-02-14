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

func dedupExpectedFamilyForType(fragType models.FragmentType) string {
	switch fragType {
	case models.FragmentTypeOrganization:
		return "org"
	case models.FragmentTypeFacility:
		return "fac"
	case models.FragmentTypeGeoRegion:
		return "loc"
	case models.FragmentTypeProduct:
		return "product"
	case models.FragmentTypePerson:
		return "person"
	case models.FragmentTypeMediaChannel:
		return "media"
	case models.FragmentTypeMarket:
		return "market"
	case models.FragmentTypeDecisionPoint:
		return "decision"
	case models.FragmentTypeMechanism:
		return "mechanism"
	case models.FragmentTypeProgram:
		return "program"
	case models.FragmentTypeDomain:
		return "domain"
	case models.FragmentTypeDeposit:
		return "deposit"
	case models.FragmentTypeCommunity:
		return "community"
	case models.FragmentTypeEvent:
		return "event"
	case models.FragmentTypeSphere:
		return "sphere"
	case models.FragmentTypeSupplyLink:
		return "supply-link"
	case models.FragmentTypeTrack:
		return "track"
	default:
		return ""
	}
}

func dedupFamilyForRequest(fragType models.FragmentType, canonicalPath string) string {
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
	return dedupExpectedFamilyForType(f.Type)
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
func isDedupReplacementCompatible(requestedType models.FragmentType, requestedCanonicalPath string, replacement *models.HorizonFragment) bool {
	if replacement == nil {
		return false
	}
	if requestedType != "" && replacement.Type != "" && requestedType != replacement.Type {
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
