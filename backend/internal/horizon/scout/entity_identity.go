package scout

import (
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"strings"
)

func normalizeIdentityText(in string) string {
	return naming.NormalizeLookupText(in)
}

func leafSlug(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
}

func extractPathJurisdiction(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "fac":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	default:
		return ""
	}
}

// extractMarketPathGeoDomain extracts geo and domain from a MARKET canonical path.
// Format: /market/{geo}/{domain}[/{vertical}...]. Returns ("", "") if not a valid market path.
func extractMarketPathGeoDomain(path string) (geo, domain string) {
	p := strings.TrimSpace(path)
	if p == "" || !strings.HasPrefix(p, "/market/") {
		return "", ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 3 {
		return "", ""
	}
	return strings.ToLower(strings.TrimSpace(parts[1])), strings.ToLower(strings.TrimSpace(parts[2]))
}

func bestNameForIdentityFromRelation(rel agents.DiscoveredRelation) string {
	if s := strings.TrimSpace(rel.TargetShortName); s != "" {
		return s
	}
	if s := strings.TrimSpace(rel.TargetLegalName); s != "" {
		return s
	}
	return strings.TrimSpace(rel.TargetName)
}

func bestNameForIdentityFromFragment(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if s := strings.TrimSpace(f.Identifiers["shortName"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["legalName"]); s != "" {
		return s
	}
	return strings.TrimSpace(f.Labels["en"])
}

func buildRelationTargetIdentityKey(rel agents.DiscoveredRelation, canonicalPath string, fragType FragmentType) string {
	if fragType == "" {
		fragType = InferFragmentTypeFromCanonicalPath(strings.TrimSpace(canonicalPath))
	}
	name := bestNameForIdentityFromRelation(rel)
	nameNorm := normalizeIdentityText(name)
	switch fragType {
	case ah.FragmentClassProduct:
		slug := leafSlug(canonicalPath)
		if slug != "" && slug != "na" {
			return "product|slug=" + slug
		}
		if nameNorm != "" {
			return "product|name=" + nameNorm
		}
	case ah.FragmentClassOrg:
		cc := extractPathJurisdiction(canonicalPath)
		slug := stripSlugLegalForm(leafSlug(canonicalPath))
		orgNorm := NormalizeLegalName(name)
		if orgNorm == "" {
			orgNorm = nameNorm
		}
		parts := []string{"org"}
		if cc != "" {
			parts = append(parts, "cc="+cc)
		}
		if orgNorm != "" {
			parts = append(parts, "name="+orgNorm)
		} else if slug != "" && slug != "na" {
			parts = append(parts, "slug="+slug)
		}
		if len(parts) > 1 {
			return strings.Join(parts, "|")
		}
	case ah.FragmentClassMarket:
		geo, domain := extractMarketPathGeoDomain(canonicalPath)
		slug := leafSlug(canonicalPath)
		parts := []string{"market"}
		if geo != "" && geo != "na" {
			parts = append(parts, "geo="+geo)
		}
		if domain != "" && domain != "na" {
			parts = append(parts, "domain="+domain)
		}
		if slug != "" && slug != "na" {
			parts = append(parts, "slug="+slug)
		}
		if nameNorm != "" && len(parts) == 1 {
			parts = append(parts, "name="+nameNorm)
		}
		if len(parts) > 1 {
			return strings.Join(parts, "|")
		}
	default:
		ns := strings.TrimSpace(strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")[0])
		if ns == "" {
			ns = strings.ToLower(strings.TrimSpace(string(fragType)))
		}
		slug := leafSlug(canonicalPath)
		if slug != "" && slug != "na" {
			return ns + "|slug=" + slug
		}
		if nameNorm != "" {
			return ns + "|name=" + nameNorm
		}
	}
	return ""
}

func buildFragmentIdentityKey(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	ft := fragment.Class
	if ft == "" {
		ft = InferFragmentTypeFromCanonicalPath(strings.TrimSpace(fragment.CanonicalPath))
	}
	name := bestNameForIdentityFromFragment(fragment)
	rel := agents.DiscoveredRelation{
		TargetName:      name,
		TargetLegalName: strings.TrimSpace(fragment.Identifiers["legalName"]),
		TargetShortName: strings.TrimSpace(fragment.Identifiers["shortName"]),
	}
	return buildRelationTargetIdentityKey(rel, strings.TrimSpace(fragment.CanonicalPath), ft)
}
