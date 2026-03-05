package canonical

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// NormalizeFragmentForPersist applies deterministic, shared canonical-path rewrites
// before persistence. It is fail-open and never blocks writes.
func NormalizeFragmentForPersist(fragment *models.HorizonFragment) {
	if fragment == nil {
		return
	}
	fragment.CanonicalPath = NormalizeCanonicalPathForPersist(fragment.CanonicalPath)
}

// NormalizeCanonicalPathForPersist applies shared canonical-path normalization rules.
// It keeps behavior conservative and deterministic, suitable for repository boundary usage.
func NormalizeCanonicalPathForPersist(path string) string {
	cp := strings.TrimSpace(path)
	if cp == "" {
		return cp
	}

	// Normalize GEO path shape to variable-depth /loc hierarchy when safe.
	if strings.HasPrefix(cp, "/loc/") {
		if norm := NormalizeGeoPathToStrict(cp); norm != "" {
			cp = norm
		}
	}

	// Normalize DOMAIN facet ordering.
	if strings.HasPrefix(cp, "/domain/") {
		if norm, err := taxonomy.NormalizeDomainCanonicalPath(cp); err == nil && strings.TrimSpace(norm) != "" {
			cp = strings.TrimSpace(norm)
		}
	}

	// Round-trip known namespaces via semantic parts to keep formatting stable.
	if parts, err := taxonomy.ParseCanonicalPathParts(cp); err == nil && parts != nil {
		normalizeCanonicalPartsInPlace(parts)
		if built, ok := parts.BuildCanonicalPath(); ok && strings.TrimSpace(built) != "" {
			cp = strings.TrimSpace(built)
		}
	}

	return cp
}

// NormalizeGeoPathToStrict normalizes /loc shapes into variable-depth /loc hierarchy
// for already-canonical geo tokens (ISO2 / regional / composite), removing "na" padding.
//
// Supported:
// - /loc/{cc}                   -> /loc/{cc}
// - /loc/{cc}/{level1}          -> /loc/{cc}/{level1}
// - /loc/{cc}/{level1}/{level2} -> /loc/{cc}/{level1}/{level2}
// - /loc/{cc}/na/na             -> /loc/{cc}
// - /loc/{cc}/{level1}/na       -> /loc/{cc}/{level1}
// Returns "" if normalization is unsafe.
func NormalizeGeoPathToStrict(path string) string {
	p := strings.TrimSpace(path)
	if !strings.HasPrefix(p, "/loc/") {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 || parts[0] != "loc" {
		return ""
	}

	cc := strings.ToLower(strings.TrimSpace(parts[1]))
	if cc == "" || !taxonomy.IsValidLocCountryToken(cc) {
		return ""
	}

	admin1 := ""
	city := ""
	switch len(parts) {
	case 2:
		// Country-level only.
	case 3:
		admin1 = slugOrEmpty(parts[2])
	case 4:
		admin1 = slugOrEmpty(parts[2])
		city = slugOrEmpty(parts[3])
	default:
		return ""
	}

	return taxonomy.GeoCanonicalPath(cc, admin1, city)
}

func slugOrEmpty(s string) string {
	if taxonomy.IsNAPlaceholderToken(s) {
		return ""
	}
	v := taxonomy.Slugify(strings.ToLower(strings.TrimSpace(s)))
	if v == "" || taxonomy.IsNAPlaceholderToken(v) {
		return ""
	}
	return v
}

func normalizeCanonicalPartsInPlace(parts *taxonomy.CanonicalPathParts) {
	if parts == nil {
		return
	}
	switch strings.TrimSpace(parts.Namespace) {
	case "org":
		if parts.Org == nil {
			return
		}
		parts.Org.Geo = normalizeGeoToken(parts.Org.Geo, "na", true)
		parts.Org.BaseSlug = taxonomy.Slugify(parts.Org.BaseSlug)
	case "gov":
		if parts.Gov == nil {
			return
		}
		parts.Gov.Geo = normalizeGeoToken(parts.Gov.Geo, "na", true)
		parts.Gov.Slug = taxonomy.Slugify(parts.Gov.Slug)
	case "product":
		if parts.Product == nil {
			return
		}
		p := parts.Product
		p.Shape = strings.TrimSpace(strings.ToLower(p.Shape))
		p.Category = taxonomy.Slugify(p.Category)
		p.Type = taxonomy.Slugify(p.Type)
		p.Stage = taxonomy.Slugify(p.Stage)
		p.Slug = taxonomy.Slugify(p.Slug)
		p.ServiceDomain = taxonomy.Slugify(p.ServiceDomain)
		p.ServiceSubdomain = taxonomy.Slugify(p.ServiceSubdomain)
		p.ServiceSlug = taxonomy.Slugify(p.ServiceSlug)
		p.ServiceOffering = taxonomy.Slugify(p.ServiceOffering)
		p.OrgSlug = taxonomy.Slugify(p.OrgSlug)
		if len(p.Tail) > 0 {
			out := make([]string, 0, len(p.Tail))
			for _, s := range p.Tail {
				if v := taxonomy.Slugify(s); v != "" {
					out = append(out, v)
				}
			}
			p.Tail = out
		}
	case "community":
		if parts.Community == nil {
			return
		}
		parts.Community.Geo = normalizeGeoToken(parts.Community.Geo, "global", true)
		parts.Community.Type = taxonomy.Slugify(parts.Community.Type)
		parts.Community.Slug = taxonomy.Slugify(parts.Community.Slug)
	case "loc":
		if parts.Loc == nil {
			return
		}
		parts.Loc.Country = normalizeGeoToken(parts.Loc.Country, "", false)
		parts.Loc.Admin1 = slugOrEmpty(parts.Loc.Admin1)
		parts.Loc.City = slugOrEmpty(parts.Loc.City)
	case "segment":
		if parts.Segment == nil {
			return
		}
		parts.Segment.Domain = taxonomy.Slugify(parts.Segment.Domain)
		parts.Segment.Vertical = taxonomy.Slugify(parts.Segment.Vertical)
		parts.Segment.Geo = normalizeGeoToken(parts.Segment.Geo, "global", false)
	case "market":
		if parts.Market == nil {
			return
		}
		parts.Market.Geo = normalizeGeoToken(parts.Market.Geo, "global", false)
		parts.Market.Domain = taxonomy.Slugify(parts.Market.Domain)
		parts.Market.Vertical = taxonomy.Slugify(parts.Market.Vertical)
		parts.Market.Slug = taxonomy.Slugify(parts.Market.Slug)
	case "fac":
		if parts.Fac == nil {
			return
		}
		parts.Fac.Geo = strings.ToLower(strings.TrimSpace(parts.Fac.Geo))
		if parts.Fac.Geo == "" {
			parts.Fac.Geo = "na"
		}
		parts.Fac.FacilitySlug = taxonomy.Slugify(parts.Fac.FacilitySlug)
	case "program":
		if parts.Program == nil {
			return
		}
		parts.Program.Jurisdiction = taxonomy.Slugify(parts.Program.Jurisdiction)
		parts.Program.Slug = taxonomy.Slugify(parts.Program.Slug)
	case "person":
		if parts.Person == nil {
			return
		}
		parts.Person.Slug = taxonomy.Slugify(parts.Person.Slug)
	case "media":
		if parts.Media == nil {
			return
		}
		parts.Media.Platform = taxonomy.Slugify(parts.Media.Platform)
		parts.Media.Slug = taxonomy.Slugify(parts.Media.Slug)
	case "deposit":
		if parts.Deposit == nil {
			return
		}
		parts.Deposit.Resource = taxonomy.Slugify(parts.Deposit.Resource)
		parts.Deposit.Slug = taxonomy.Slugify(parts.Deposit.Slug)
	case "event":
		if parts.Event == nil {
			return
		}
		parts.Event.Slug = taxonomy.Slugify(parts.Event.Slug)
	case "sphere":
		if parts.Sphere == nil {
			return
		}
		parts.Sphere.Slug = taxonomy.Slugify(parts.Sphere.Slug)
	case "decision":
		if parts.Decision == nil {
			return
		}
		parts.Decision.DomainSlug = taxonomy.Slugify(parts.Decision.DomainSlug)
		parts.Decision.DecisionSlug = taxonomy.Slugify(parts.Decision.DecisionSlug)
	case "mechanism":
		if parts.Mechanism == nil {
			return
		}
		parts.Mechanism.Category = taxonomy.Slugify(parts.Mechanism.Category)
		parts.Mechanism.Slug = taxonomy.Slugify(parts.Mechanism.Slug)
	}
}

func normalizeGeoToken(raw string, fallback string, allowInt bool) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return fallback
	}
	if v == "global" || v == "na" || (allowInt && v == "int") {
		return v
	}
	if taxonomy.IsISO2CountryCode(v) || taxonomy.IsRegionalGeoToken(v) || taxonomy.IsCompositeGeoToken(v) {
		return v
	}
	s := taxonomy.Slugify(v)
	if s == "" {
		return fallback
	}
	return s
}
