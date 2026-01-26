package taxonomy

import (
	"fmt"
	"sort"
	"strings"
)

// CanonicalPathParts is a semantic (field-addressable) representation of a Horizon canonical path.
//
// Rationale:
//   - Different subsystems want to "fix" different aspects of a canonical path (geo vs legal form vs taxonomy),
//     and those fixes should not conceptually clobber each other.
//   - A semantic struct makes it explicit which piece of meaning is being updated, independent of its position
//     in the string path. (E.g., geo is always Org.Geo / Gov.Geo / Market.Geo / Loc.Country, etc.)
//
// NOTE:
//   - This type is intentionally tolerant: it can represent legacy/flat paths as well as preferred ones.
//   - Storage strategy is decided elsewhere; callers may store the parts (e.g., in Firestore) and keep the
//     assembled canonicalPath as the read path for the rest of the system.
type CanonicalPathParts struct {
	Namespace string // e.g. "org", "gov", "product", "loc", ...

	Org       *OrgPathParts
	Gov       *GovPathParts
	Product   *ProductPathParts
	Community *CommunityPathParts
	Loc       *LocPathParts
	Segment   *SegmentPathParts
	Market    *MarketPathParts
	Fac       *FacilityPathParts
	Program   *ProgramPathParts
	Person    *PersonPathParts
	Media     *MediaPathParts
	Deposit   *DepositPathParts
	Sphere    *SpherePathParts
	Event     *EventPathParts
	Decision  *DecisionPathParts
	Mechanism *MechanismPathParts
}

// OrgPathParts represents /org paths. Canonical string form remains:
// - preferred: /org/{geo}/{baseSlug}-{legalForm?}
// - legacy:    /org/{slug} (no geo segment)
//
// legalForm is stored separately to allow selective fixes (e.g., add ltd) without touching geo.
type OrgPathParts struct {
	// Geo is ISO2, composite ISO2 (e.g. "ae-sa-om"), or "na". Empty when legacy path has no geo segment.
	Geo string
	// BaseSlug is org slug without legal form suffix.
	BaseSlug string
	// LegalForm is canonical legal form token (e.g., "ltd", "inc", "llc"). Optional.
	LegalForm string
	// Legacy indicates the original path was /org/{slug} (no geo segment).
	Legacy bool
}

// GovPathParts represents /gov/{geo}/{slug}.
type GovPathParts struct {
	Geo  string
	Slug string
}

// ProductPathParts represents /product paths.
//
// Supported shapes (non-exhaustive; best-effort):
// - hierarchical goods: /product/{category}/{type}/{stage}/{slug}[/{model-sku-slug}...]
// - hierarchical services: /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}...]
// - flat fallback: /product/{slug}
// - legacy org-scoped fallback (older code-path): /product/{orgSlug}/{productSlug}
type ProductPathParts struct {
	// Shape is a best-effort discriminator to aid callers.
	// Values: "hierarchical", "service", "flat", "org_scoped_flat", "unknown".
	Shape string

	// Goods taxonomy (hierarchical).
	Category string // raw|component|finished|service (service also has dedicated service-shape fields below)
	Type     string // mineral|agricultural|...
	Stage    string // extraction|refining|...
	Slug     string // final slug (required for all product shapes)
	// Tail preserves any extra segments after the known prefix so parse->build does not lose information.
	// For goods: segments after {slug}. For services: segments after {serviceSlug}. For unknown shapes: best-effort.
	Tail []string

	// Service taxonomy (when Shape=="service").
	ServiceDomain    string
	ServiceSubdomain string
	ServiceSlug      string
	// ServiceOffering is the first extra segment (if any) after {serviceSlug}. Kept for convenience.
	ServiceOffering string

	// Legacy org-scoped fallback (when Shape=="org_scoped_flat").
	OrgSlug string
}

// CommunityPathParts represents community paths.
//
// Preferred format: /community/{geo}/{type}/{slug}
// Legacy fallback:  /community/{geo}/{slug}
//
// NOTE: "/community" is not currently a taxonomy namespace in taxonomy/paths.go,
// but it is a first-class Horizon concept (see horizon models and scout prompts).
type CommunityPathParts struct {
	Geo    string // ISO2 or regional token like "global"
	Type   string // trade|activist|professional|criminal|religious|ethnic|interest|dynasty|tribe
	Slug   string
	Legacy bool // true if type segment is absent
}

// LocPathParts represents /loc/{cc}/{admin1}/{city}.
type LocPathParts struct {
	Country string // ISO2, composite, or allowed regional token
	Admin1  string // geonames admin1 code/slug or "na"
	City    string // city slug or "na"
}

// SegmentPathParts represents /segment/{domain}/{vertical}/{geo}.
type SegmentPathParts struct {
	Domain   string
	Vertical string
	Geo      string // ISO2 or allowed regional bucket (global, emea, asean, ...)
}

// MarketPathParts represents market paths.
// Policy-preferred format: /market/{geo}/{domain}/{vertical}
// Legacy format: /market/{geo}/{slug} or /market/{slug}
type MarketPathParts struct {
	Geo      string
	Domain   string
	Vertical string
	Slug     string
	Legacy   bool
}

// FacilityPathParts represents /fac/{orgSlug}/{facilitySlug} (preferred) or /fac/{facilitySlug} (legacy).
type FacilityPathParts struct {
	OrgSlug      string
	FacilitySlug string
	Legacy       bool
}

// ProgramPathParts represents /program/{jurisdiction?}/{slug}.
type ProgramPathParts struct {
	Jurisdiction string
	Slug         string
	Legacy       bool
}

// PersonPathParts represents /person/{slug} (honorific may be embedded in slug).
type PersonPathParts struct {
	Slug string
}

// MediaPathParts represents /media/{platform}/{slug}.
type MediaPathParts struct {
	Platform string
	Slug     string
}

// DepositPathParts represents /deposit/{resource?}/{slug}.
type DepositPathParts struct {
	Resource string
	Slug     string
	Legacy   bool
}

// SpherePathParts represents /sphere/{slug}.
type SpherePathParts struct {
	Slug string
}

// EventPathParts represents /event/{slug}.
type EventPathParts struct {
	Slug string
}

// DecisionPathParts represents /decision/{domain-slug}/{decision-slug}.
type DecisionPathParts struct {
	DomainSlug   string
	DecisionSlug string
}

// MechanismPathParts represents /mechanism/{category}/{slug}.
type MechanismPathParts struct {
	Category string
	Slug     string
}

// ParseCanonicalPathParts parses a canonical path string into semantic parts.
// It is best-effort and supports both preferred and legacy shapes.
func ParseCanonicalPathParts(path string) (*CanonicalPathParts, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("canonical path is empty")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("canonical path must start with '/': %q", path)
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("canonical path must have at least 2 segments: %q", path)
	}
	ns := strings.TrimSpace(parts[0])
	out := &CanonicalPathParts{Namespace: ns}

	switch ns {
	case "org":
		// /org/{slug} (legacy) OR /org/{geo}/{slug}
		if len(parts) == 2 {
			base, lf := splitOrgSlugLegalForm(parts[1])
			out.Org = &OrgPathParts{Geo: "", BaseSlug: base, LegalForm: lf, Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			base, lf := splitOrgSlugLegalForm(parts[2])
			out.Org = &OrgPathParts{Geo: parts[1], BaseSlug: base, LegalForm: lf, Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("org path must be /org/{slug} (legacy) or /org/{geo}/{slug}: %q", path)

	case "gov":
		// /gov/{geo}/{slug}
		if len(parts) != 3 {
			return nil, fmt.Errorf("gov path must be /gov/{geo}/{slug}: %q", path)
		}
		out.Gov = &GovPathParts{Geo: parts[1], Slug: parts[2]}
		return out, nil

	case "product":
		// Supported:
		// - /product/{slug}
		// - /product/{orgSlug}/{productSlug} (legacy)
		// - /product/{category}/{type}/{stage}/{slug}[/{model-sku-slug}...]
		// - /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}...]
		if len(parts) == 2 {
			out.Product = &ProductPathParts{Shape: "flat", Slug: parts[1]}
			return out, nil
		}
		if len(parts) == 3 {
			out.Product = &ProductPathParts{Shape: "org_scoped_flat", OrgSlug: parts[1], Slug: parts[2]}
			return out, nil
		}
		if len(parts) >= 5 && parts[1] == "service" {
			pp := &ProductPathParts{
				Shape:            "service",
				ServiceDomain:    parts[2],
				ServiceSubdomain: parts[3],
				ServiceSlug:      parts[4],
				Slug:             parts[4],
			}
			if len(parts) >= 6 {
				pp.ServiceOffering = parts[5]
				pp.Tail = append([]string{}, parts[5:]...)
			}
			out.Product = pp
			return out, nil
		}
		if len(parts) >= 5 {
			pp := &ProductPathParts{
				Shape:    "hierarchical",
				Category: parts[1],
				Type:     parts[2],
				Stage:    parts[3],
				Slug:     parts[4],
			}
			if len(parts) >= 6 {
				pp.Tail = append([]string{}, parts[5:]...)
			}
			out.Product = pp
			return out, nil
		}
		out.Product = &ProductPathParts{Shape: "unknown"}
		return out, nil

	case "community":
		// Preferred: /community/{geo}/{type}/{slug} (4 parts)
		// Legacy:    /community/{geo}/{slug} (3 parts)
		if len(parts) == 3 {
			out.Community = &CommunityPathParts{Geo: parts[1], Type: "", Slug: parts[2], Legacy: true}
			return out, nil
		}
		if len(parts) == 4 {
			out.Community = &CommunityPathParts{Geo: parts[1], Type: parts[2], Slug: parts[3], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("community path must be /community/{geo}/{slug} (legacy) or /community/{geo}/{type}/{slug}: %q", path)

	case "loc":
		if len(parts) != 4 {
			return nil, fmt.Errorf("loc path must be /loc/{cc}/{admin1}/{city}: %q", path)
		}
		out.Loc = &LocPathParts{Country: parts[1], Admin1: parts[2], City: parts[3]}
		return out, nil

	case "segment":
		if len(parts) != 4 {
			return nil, fmt.Errorf("segment path must be /segment/{domain}/{vertical}/{geo}: %q", path)
		}
		out.Segment = &SegmentPathParts{Domain: parts[1], Vertical: parts[2], Geo: parts[3]}
		return out, nil

	case "market":
		// Preferred: /market/{geo}/{domain}/{vertical}
		// Legacy: /market/{geo}/{slug} OR /market/{slug}
		if len(parts) == 2 {
			out.Market = &MarketPathParts{Geo: "", Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Market = &MarketPathParts{Geo: parts[1], Slug: parts[2], Legacy: true}
			return out, nil
		}
		if len(parts) == 4 {
			out.Market = &MarketPathParts{Geo: parts[1], Domain: parts[2], Vertical: parts[3], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("market path must be /market/{slug} (legacy), /market/{geo}/{slug} (legacy), or /market/{geo}/{domain}/{vertical}: %q", path)

	case "fac":
		// Preferred: /fac/{orgSlug}/{facilitySlug}
		// Legacy: /fac/{facilitySlug}
		if len(parts) == 2 {
			out.Fac = &FacilityPathParts{OrgSlug: "", FacilitySlug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Fac = &FacilityPathParts{OrgSlug: parts[1], FacilitySlug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("facility path must be /fac/{facilitySlug} (legacy) or /fac/{orgSlug}/{facilitySlug}: %q", path)

	case "program":
		// Preferred: /program/{jurisdiction}/{slug}
		// Legacy: /program/{slug}
		if len(parts) == 2 {
			out.Program = &ProgramPathParts{Jurisdiction: "", Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Program = &ProgramPathParts{Jurisdiction: parts[1], Slug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("program path must be /program/{slug} (legacy) or /program/{jurisdiction}/{slug}: %q", path)

	case "person":
		if len(parts) != 2 {
			return nil, fmt.Errorf("person path must be /person/{slug}: %q", path)
		}
		out.Person = &PersonPathParts{Slug: parts[1]}
		return out, nil

	case "media":
		if len(parts) != 3 {
			return nil, fmt.Errorf("media path must be /media/{platform}/{slug}: %q", path)
		}
		out.Media = &MediaPathParts{Platform: parts[1], Slug: parts[2]}
		return out, nil

	case "deposit":
		// Preferred: /deposit/{resource}/{slug}
		// Legacy: /deposit/{slug}
		if len(parts) == 2 {
			out.Deposit = &DepositPathParts{Resource: "", Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Deposit = &DepositPathParts{Resource: parts[1], Slug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("deposit path must be /deposit/{slug} (legacy) or /deposit/{resource}/{slug}: %q", path)

	case "sphere":
		if len(parts) != 2 {
			return nil, fmt.Errorf("sphere path must be /sphere/{slug}: %q", path)
		}
		out.Sphere = &SpherePathParts{Slug: parts[1]}
		return out, nil

	case "event":
		if len(parts) != 2 {
			return nil, fmt.Errorf("event path must be /event/{slug}: %q", path)
		}
		out.Event = &EventPathParts{Slug: parts[1]}
		return out, nil

	case "decision":
		if len(parts) != 3 {
			return nil, fmt.Errorf("decision path must be /decision/{domain-slug}/{decision-slug}: %q", path)
		}
		out.Decision = &DecisionPathParts{DomainSlug: parts[1], DecisionSlug: parts[2]}
		return out, nil

	case "mechanism":
		if len(parts) != 3 {
			return nil, fmt.Errorf("mechanism path must be /mechanism/{category}/{slug}: %q", path)
		}
		out.Mechanism = &MechanismPathParts{Category: parts[1], Slug: parts[2]}
		return out, nil

	default:
		// Unknown namespace: return a minimal struct. Validation is handled elsewhere.
		return out, nil
	}
}

// BuildCanonicalPath reassembles a canonicalPath string from semantic parts.
//
// Safety:
//   - If parts are incomplete, it returns ("", false) so callers can keep the existing canonicalPath
//     and log a warning (fail-open, non-blocking).
func (p *CanonicalPathParts) BuildCanonicalPath() (string, bool) {
	if p == nil {
		return "", false
	}
	switch strings.TrimSpace(p.Namespace) {
	case "org":
		if p.Org == nil {
			return "", false
		}
		base := strings.TrimSpace(p.Org.BaseSlug)
		if base == "" {
			return "", false
		}
		slug := base
		if lf := strings.TrimSpace(p.Org.LegalForm); lf != "" {
			slug = slug + "-" + lf
		}
		if p.Org.Legacy || strings.TrimSpace(p.Org.Geo) == "" {
			return JoinPath("org", slug), true
		}
		return JoinPath("org", p.Org.Geo, slug), true

	case "gov":
		if p.Gov == nil || strings.TrimSpace(p.Gov.Geo) == "" || strings.TrimSpace(p.Gov.Slug) == "" {
			return "", false
		}
		return JoinPath("gov", p.Gov.Geo, p.Gov.Slug), true

	case "product":
		if p.Product == nil {
			return "", false
		}
		pp := p.Product
		switch pp.Shape {
		case "hierarchical":
			if pp.Category == "" || pp.Type == "" || pp.Stage == "" || pp.Slug == "" {
				return "", false
			}
			if len(pp.Tail) > 0 {
				return JoinPath(append([]string{"product", pp.Category, pp.Type, pp.Stage, pp.Slug}, pp.Tail...)...), true
			}
			return JoinPath("product", pp.Category, pp.Type, pp.Stage, pp.Slug), true
		case "service":
			if pp.ServiceDomain == "" || pp.ServiceSubdomain == "" || pp.ServiceSlug == "" {
				return "", false
			}
			if len(pp.Tail) > 0 {
				return JoinPath(append([]string{"product", "service", pp.ServiceDomain, pp.ServiceSubdomain, pp.ServiceSlug}, pp.Tail...)...), true
			}
			return JoinPath("product", "service", pp.ServiceDomain, pp.ServiceSubdomain, pp.ServiceSlug), true
		case "org_scoped_flat":
			if pp.OrgSlug == "" || pp.Slug == "" {
				return "", false
			}
			return JoinPath("product", pp.OrgSlug, pp.Slug), true
		case "flat":
			if pp.Slug == "" {
				return "", false
			}
			return JoinPath("product", pp.Slug), true
		default:
			// Unknown shape: fail-safe.
			return "", false
		}

	case "community":
		if p.Community == nil || p.Community.Geo == "" || p.Community.Slug == "" {
			return "", false
		}
		if p.Community.Legacy || strings.TrimSpace(p.Community.Type) == "" {
			return JoinPath("community", p.Community.Geo, p.Community.Slug), true
		}
		return JoinPath("community", p.Community.Geo, p.Community.Type, p.Community.Slug), true

	case "loc":
		if p.Loc == nil || p.Loc.Country == "" || p.Loc.Admin1 == "" || p.Loc.City == "" {
			return "", false
		}
		// GeoCanonicalPath preserves special handling for "na" defaults and regional tokens.
		if built := GeoCanonicalPath(p.Loc.Country, p.Loc.Admin1, p.Loc.City); strings.TrimSpace(built) != "" {
			return built, true
		}
		return "", false

	case "segment":
		if p.Segment == nil || p.Segment.Domain == "" || p.Segment.Vertical == "" || p.Segment.Geo == "" {
			return "", false
		}
		return JoinPath("segment", p.Segment.Domain, p.Segment.Vertical, p.Segment.Geo), true

	case "market":
		if p.Market == nil {
			return "", false
		}
		if p.Market.Geo != "" && p.Market.Domain != "" && p.Market.Vertical != "" {
			return JoinPath("market", p.Market.Geo, p.Market.Domain, p.Market.Vertical), true
		}
		if p.Market.Geo != "" && p.Market.Slug != "" {
			return JoinPath("market", p.Market.Geo, p.Market.Slug), true
		}
		if p.Market.Slug != "" {
			return JoinPath("market", p.Market.Slug), true
		}
		return "", false

	case "fac":
		if p.Fac == nil || p.Fac.FacilitySlug == "" {
			return "", false
		}
		if p.Fac.OrgSlug == "" {
			return JoinPath("fac", p.Fac.FacilitySlug), true
		}
		return JoinPath("fac", p.Fac.OrgSlug, p.Fac.FacilitySlug), true

	case "program":
		if p.Program == nil || p.Program.Slug == "" {
			return "", false
		}
		if p.Program.Jurisdiction == "" {
			return JoinPath("program", p.Program.Slug), true
		}
		return JoinPath("program", p.Program.Jurisdiction, p.Program.Slug), true

	case "person":
		if p.Person == nil || p.Person.Slug == "" {
			return "", false
		}
		return JoinPath("person", p.Person.Slug), true

	case "media":
		if p.Media == nil || p.Media.Platform == "" || p.Media.Slug == "" {
			return "", false
		}
		return JoinPath("media", p.Media.Platform, p.Media.Slug), true

	case "deposit":
		if p.Deposit == nil || p.Deposit.Slug == "" {
			return "", false
		}
		if p.Deposit.Resource == "" {
			return JoinPath("deposit", p.Deposit.Slug), true
		}
		return JoinPath("deposit", p.Deposit.Resource, p.Deposit.Slug), true

	case "sphere":
		if p.Sphere == nil || p.Sphere.Slug == "" {
			return "", false
		}
		return JoinPath("sphere", p.Sphere.Slug), true

	case "event":
		if p.Event == nil || p.Event.Slug == "" {
			return "", false
		}
		return JoinPath("event", p.Event.Slug), true

	case "decision":
		if p.Decision == nil || strings.TrimSpace(p.Decision.DomainSlug) == "" || strings.TrimSpace(p.Decision.DecisionSlug) == "" {
			return "", false
		}
		return JoinPath("decision", p.Decision.DomainSlug, p.Decision.DecisionSlug), true

	case "mechanism":
		if p.Mechanism == nil || strings.TrimSpace(p.Mechanism.Category) == "" || strings.TrimSpace(p.Mechanism.Slug) == "" {
			return "", false
		}
		return JoinPath("mechanism", p.Mechanism.Category, p.Mechanism.Slug), true
	}
	return "", false
}

func splitOrgSlugLegalForm(slug string) (base string, legalForm string) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", ""
	}
	// Ensure we match on canonical, slugified tokens.
	slug = Slugify(slug)
	if slug == "" {
		return "", ""
	}

	// Build a "known legal forms" list for suffix parsing.
	// We union:
	// - canonical rules list (shared contract)
	// - additional canonical tokens used by normalizeLegalForm() and stripLegalFormSuffix()
	forms := map[string]struct{}{}
	rules := DefaultCanonicalRules()
	for _, lf := range rules.Org.LegalForms {
		lf = strings.TrimSpace(strings.ToLower(lf))
		if lf != "" {
			forms[lf] = struct{}{}
		}
	}
	for _, lf := range []string{"cjsc", "llp", "lp", "sp", "sup", "fsup", "soe", "se", "co", "pty"} {
		forms[lf] = struct{}{}
	}

	ordered := make([]string, 0, len(forms))
	for lf := range forms {
		ordered = append(ordered, lf)
	}
	sort.Slice(ordered, func(i, j int) bool {
		// Prefer longer matches first (pjsc before jsc).
		if len(ordered[i]) != len(ordered[j]) {
			return len(ordered[i]) > len(ordered[j])
		}
		return ordered[i] < ordered[j]
	})

	for _, lf := range ordered {
		suffix := "-" + lf
		if strings.HasSuffix(slug, suffix) {
			b := strings.TrimSuffix(slug, suffix)
			b = strings.TrimSuffix(b, "-")
			if b != "" {
				return b, lf
			}
		}
	}
	return slug, ""
}
