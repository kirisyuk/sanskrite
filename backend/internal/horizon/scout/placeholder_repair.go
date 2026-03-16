package scout

import (
	"context"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func RepairSyntheticFacilityCanonicalPath(
	ctx context.Context,
	fragment *models.HorizonFragment,
	geocodingClient coreontology.GeocodingClientInterface,
) (string, string, bool) {
	if fragment == nil || fragment.Type != models.FragmentTypeFacility {
		return "", "", false
	}
	if _, blocked := forbiddenPlaceholderFacilitySlug(strings.TrimSpace(fragment.CanonicalPath)); !blocked {
		return "", "", false
	}

	label := strings.TrimSpace(facilityCanonicalizationLabel(fragment))
	if label == "" {
		return "", "", false
	}
	labelISO2 := inferISO2FromLabel(label, fragment)
	placeHint := facilityPlaceHint(fragment)
	if geocodingClient != nil && (labelISO2 != "" || placeHint != "" || (fragment.HomeLocation != nil && strings.TrimSpace(fragment.HomeLocation.Name) != "")) {
		_, _ = RevalidateFacilityGeo(ctx, fragment, geocodingClient)
	}
	geo := strings.ToLower(strings.TrimSpace(fragment.Identifiers["geoVerifiedCountryAlpha2"]))
	if geo == "" {
		geo = facilityPathGeoToken(fragment.CanonicalPath)
	}
	if geo == "" {
		geo = labelISO2
	}
	if geo == "" {
		geo = "na"
	}
	if geo != "na" && labelISO2 != geo && placeHint == "" {
		return "", "", false
	}
	newPath := strings.TrimSpace(taxonomy.FacilityCanonicalPath(geo, label))
	if newPath == "" || newPath == strings.TrimSpace(fragment.CanonicalPath) {
		return "", "", false
	}
	if slug, blocked := forbiddenPlaceholderFacilitySlug(newPath); blocked || canonical.FacilitySlugHasSyntheticTail(lastPathSlug(newPath)) {
		_ = slug
		return "", "", false
	}
	if geo == "na" && canonical.FacilitySlugLowInformation(lastPathSlug(newPath)) {
		return "", "", false
	}
	return newPath, "synthetic facility placeholder path repaired from label/context", true
}

func RepairSyntheticOrgCanonicalPath(fragment *models.HorizonFragment) (string, string, bool) {
	if fragment == nil || fragment.Type != models.FragmentTypeOrganization {
		return "", "", false
	}
	cp := strings.TrimSpace(fragment.CanonicalPath)
	if !strings.HasPrefix(cp, "/org/") && !strings.HasPrefix(cp, "/gov/") {
		return "", "", false
	}
	slug := lastPathSlug(cp)
	isGov := strings.HasPrefix(cp, "/gov/")
	if isGov && !canonical.GovSlugForbiddenPlaceholder(slug) {
		return "", "", false
	}
	if !isGov && !canonical.OrgSlugForbiddenPlaceholder(slug) {
		return "", "", false
	}
	newPath := strings.TrimSpace(inferOrgCanonicalPathFromHints(fragment))
	if newPath == "" || newPath == cp {
		return "", "", false
	}
	return newPath, "synthetic org/gov placeholder path repaired from deterministic hints", true
}

func facilityCanonicalizationLabel(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	label := strings.TrimSpace(firstNonEmpty(
		strings.TrimSpace(fragment.Identifiers["legalName"]),
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
	))
	if label == "" {
		return ""
	}
	normalized := strings.TrimSpace(taxonomy.Slugify(label))
	if normalized == "" || canonical.FacilitySlugHasSyntheticTail(normalized) {
		return ""
	}
	return label
}

func facilityPathGeoToken(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")
	if len(parts) != 3 || parts[0] != "fac" {
		return ""
	}
	geo := strings.ToLower(strings.TrimSpace(parts[1]))
	if geo == "" || geo == "na" {
		return ""
	}
	return geo
}
