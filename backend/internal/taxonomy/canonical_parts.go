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
//   - This type is intentionally tolerant at parse time: older path shapes are best-effort
//     normalized into current canonical contracts.
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
	Project   *ProgramPathParts
	Person    *PersonPathParts
	Media     *MediaPathParts
	Deposit   *DepositPathParts
	Evidence  *EvidencePathParts
	Sphere    *SpherePathParts
	Event     *EventPathParts
	Decision  *DecisionPathParts
	Mechanism *MechanismPathParts
	// RawPathParts stores path segments for namespaces without dedicated structs (domain, zone, supply-link).
	// BuildCanonicalPath reconstructs from RawPathParts when set.
	// Firestore tag ensures correct serialization when CanonicalPathParts is stored in Firestore.
	RawPathParts *RawPathParts `json:"rawPathParts,omitempty" firestore:"RawPathParts,omitempty"`
}

// RawPathParts stores raw segments for domain, zone, supply-link and other namespaces.
// Used when full semantic parsing is not yet implemented; enables parse->build round-trip.
// Firestore tags ensure correct serialization/deserialization (without them, RawPathParts.Segments
// can be lost when loading from Firestore, causing "canonicalParts present but incomplete" warning).
type RawPathParts struct {
	Segments []string `json:"segments" firestore:"Segments"`
}

// OrgPathParts represents /org/{geo}/{slug}.
//
// Legal form is not used; slug is base name only.
type OrgPathParts struct {
	// Geo is ISO2, composite ISO2 (e.g. "ae-sa-om"), or "na".
	Geo string
	// BaseSlug is org slug (base name, no legal form).
	BaseSlug string
	// Legacy is deprecated and kept only for transitional handling of pre-contract records.
	Legacy bool
}

// GovPathParts represents /gov/{geo}/{slug}.
type GovPathParts struct {
	Geo  string
	Slug string
}

// ProductPathParts represents /product paths.
//
// Supported shapes (best-effort):
// - hierarchical goods: /product/{category}/{type}/{stage}/{slug}[/{model-sku-slug}...]
// - hierarchical services: /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}...]
// - transitional flat inputs may still be parsed for migration, but builders emit hierarchical form.
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

	// Deprecated transitional field for parsing older records.
	OrgSlug string
}

// CommunityPathParts represents community paths.
//
// Canonical format: /community/{geo}/{type}/{slug}
//
// NOTE: "/community" is not currently a taxonomy namespace in taxonomy/paths.go,
// but it is a first-class Horizon concept (see horizon models and scout prompts).
type CommunityPathParts struct {
	Geo    string // ISO2 or regional token like "global"
	Type   string // trade|activist|professional|criminal|religious|ethnic|interest|dynasty|tribe|representatives|industry-association|key-community
	Slug   string
	Legacy bool // deprecated transitional flag
}

// LocPathParts represents /loc/{country}[/{level1}[/{level2}]].
type LocPathParts struct {
	Country string // ISO2, composite, or allowed regional token
	Admin1  string // optional level-1 subdivision slug
	City    string // optional level-2 subdivision slug
}

// SegmentPathParts represents /segment/{domain}/{vertical}/{geo}.
type SegmentPathParts struct {
	Domain   string
	Vertical string
	Geo      string // ISO2 or allowed regional bucket (global, emea, asean, ...)
}

// MarketPathParts represents market paths.
// Canonical format: /market/{geo}/{domain}[/{vertical}...]
type MarketPathParts struct {
	Geo      string
	Domain   string
	Vertical string
	Slug     string
	Legacy   bool // deprecated transitional flag
}

// FacilityPathParts represents /fac/{geo}/{facilitySlug}.
type FacilityPathParts struct {
	Geo          string // ISO2, na, or regional (like org/gov)
	FacilitySlug string
	Legacy       bool // deprecated transitional flag
}

// ProgramPathParts represents /program/{jurisdiction}/{slug}.
type ProgramPathParts struct {
	Jurisdiction string
	Slug         string
	Legacy       bool // deprecated transitional flag
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

// DepositPathParts represents /deposit/{resource}/{slug}.
type DepositPathParts struct {
	Resource string
	Slug     string
	Legacy   bool // deprecated transitional flag
}

// EvidencePathParts represents /evidence/{slug}.
type EvidencePathParts struct {
	Slug string
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
// It is best-effort and normalizes known transitional legacy shapes into current contracts.
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
		// Canonical: /org/{geo}/{slug}. When parsing legacy paths with -inc/-ltd etc., strip to get base.
		if len(parts) == 2 {
			base, _ := splitOrgSlugLegalForm(parts[1])
			out.Org = &OrgPathParts{Geo: "na", BaseSlug: base, Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			base, _ := splitOrgSlugLegalForm(parts[2])
			out.Org = &OrgPathParts{Geo: parts[1], BaseSlug: base, Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("org path must be /org/{geo}/{slug}: %q", path)

	case "gov":
		// /gov/{geo}/{slug}
		if len(parts) != 3 {
			return nil, fmt.Errorf("gov path must be /gov/{geo}/{slug}: %q", path)
		}
		out.Gov = &GovPathParts{Geo: parts[1], Slug: parts[2]}
		return out, nil

	case "product":
		// Supported:
		// - /product/{category}/{type}/{stage}/{slug}[/{model-sku-slug}...]
		// - /product/service/{domain}/{subdomain}/{slug}[/{offering-slug}...]
		// Transitional:
		// - /product/{slug}
		// - /product/{orgSlug}/{productSlug}
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
		// Canonical: /community/{geo}/{type}/{slug}.
		// Transitional normalization: /community/{geo}/{slug} -> /community/{geo}/interest/{slug}.
		if len(parts) == 3 {
			out.Community = &CommunityPathParts{Geo: parts[1], Type: "interest", Slug: parts[2], Legacy: true}
			return out, nil
		}
		if len(parts) == 4 {
			out.Community = &CommunityPathParts{Geo: parts[1], Type: parts[2], Slug: parts[3], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("community path must be /community/{geo}/{type}/{slug}: %q", path)

	case "loc":
		if len(parts) < 2 || len(parts) > 4 {
			return nil, fmt.Errorf("loc path must be /loc/{country}[/{level1}[/{level2}]]: %q", path)
		}
		level1 := ""
		level2 := ""
		if len(parts) >= 3 {
			level1 = parts[2]
		}
		if len(parts) >= 4 {
			level2 = parts[3]
		}
		out.Loc = &LocPathParts{Country: parts[1], Admin1: level1, City: level2}
		return out, nil

	case "segment":
		if len(parts) != 4 {
			return nil, fmt.Errorf("segment path must be /segment/{domain}/{vertical}/{geo}: %q", path)
		}
		out.Segment = &SegmentPathParts{Domain: parts[1], Vertical: parts[2], Geo: parts[3]}
		return out, nil

	case "market":
		// Canonical: /market/{geo}/{domain}[/{vertical}...].
		// Transitional normalization:
		// - /market/{slug} -> /market/global/{slug}
		// - /market/{geo}/{slug} -> /market/{geo}/{slug}
		if len(parts) == 2 {
			out.Market = &MarketPathParts{Geo: "global", Domain: parts[1], Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Market = &MarketPathParts{Geo: parts[1], Domain: parts[2], Slug: parts[2], Legacy: false}
			return out, nil
		}
		if len(parts) >= 4 {
			vertical := parts[3]
			if len(parts) > 4 {
				vertical = strings.Join(parts[3:], "/")
			}
			out.Market = &MarketPathParts{Geo: parts[1], Domain: parts[2], Vertical: vertical, Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("market path must be /market/{geo}/{domain}[/{vertical}...]: %q", path)

	case "fac":
		// Canonical: /fac/{geo}/{facilitySlug}.
		// Transitional normalization: /fac/{facilitySlug} -> /fac/na/{facilitySlug}.
		if len(parts) == 2 {
			out.Fac = &FacilityPathParts{Geo: "na", FacilitySlug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Fac = &FacilityPathParts{Geo: parts[1], FacilitySlug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("facility path must be /fac/{geo}/{facilitySlug}: %q", path)

	case "program":
		// Canonical: /program/{jurisdiction}/{slug}.
		// Transitional normalization: /program/{slug} -> /program/global/{slug}.
		if len(parts) == 2 {
			out.Program = &ProgramPathParts{Jurisdiction: "global", Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Program = &ProgramPathParts{Jurisdiction: parts[1], Slug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("program path must be /program/{jurisdiction}/{slug}: %q", path)

	case "project":
		// Canonical: /project/{jurisdiction}/{slug}.
		// Transitional normalization: /project/{slug} -> /project/global/{slug}.
		if len(parts) == 2 {
			out.Project = &ProgramPathParts{Jurisdiction: "global", Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Project = &ProgramPathParts{Jurisdiction: parts[1], Slug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("project path must be /project/{jurisdiction}/{slug}: %q", path)

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
		// Canonical: /deposit/{resource}/{slug}.
		// Transitional normalization: /deposit/{slug} -> /deposit/other/{slug}.
		if len(parts) == 2 {
			out.Deposit = &DepositPathParts{Resource: "other", Slug: parts[1], Legacy: true}
			return out, nil
		}
		if len(parts) == 3 {
			out.Deposit = &DepositPathParts{Resource: parts[1], Slug: parts[2], Legacy: false}
			return out, nil
		}
		return nil, fmt.Errorf("deposit path must be /deposit/{resource}/{slug}: %q", path)

	case "evidence":
		if len(parts) != 2 {
			return nil, fmt.Errorf("evidence path must be /evidence/{slug}: %q", path)
		}
		out.Evidence = &EvidencePathParts{Slug: parts[1]}
		return out, nil

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

	case "domain", "zone", "supply-link":
		// Store raw segments for parse->build round-trip. Full semantic parsing can be added later.
		out.RawPathParts = &RawPathParts{Segments: append([]string{}, parts...)}
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
		geo := strings.TrimSpace(p.Org.Geo)
		if geo == "" {
			geo = "na"
		}
		return JoinPath("org", geo, base), true

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
			if pp.Slug == "" {
				return "", false
			}
			return JoinPath("product", "finished", "mechanical", "assembly", pp.Slug), true
		case "flat":
			if pp.Slug == "" {
				return "", false
			}
			return JoinPath("product", "finished", "mechanical", "assembly", pp.Slug), true
		default:
			// Unknown shape: fail-safe.
			return "", false
		}

	case "community":
		if p.Community == nil || p.Community.Slug == "" {
			return "", false
		}
		geo := strings.TrimSpace(p.Community.Geo)
		if geo == "" {
			geo = "global"
		}
		typ := strings.TrimSpace(p.Community.Type)
		if typ == "" {
			typ = "interest"
		}
		return JoinPath("community", geo, typ, p.Community.Slug), true

	case "loc":
		if p.Loc == nil || strings.TrimSpace(p.Loc.Country) == "" {
			return "", false
		}
		// GeoCanonicalPath preserves regional/composite validation and strips placeholder tails.
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
		geo := strings.TrimSpace(p.Market.Geo)
		if geo == "" {
			geo = "global"
		}
		domain := strings.TrimSpace(p.Market.Domain)
		if domain == "" {
			domain = strings.TrimSpace(p.Market.Slug)
		}
		if geo != "" && domain != "" && p.Market.Vertical != "" {
			// Vertical may contain slashes for L3+ paths (e.g. "crop-production/rice-production").
			// Split and pass each segment separately to preserve hierarchy.
			segs := []string{"market", geo, domain}
			for _, s := range strings.Split(p.Market.Vertical, "/") {
				s = strings.TrimSpace(s)
				if s != "" {
					segs = append(segs, s)
				}
			}
			return JoinPath(segs...), true
		}
		if geo != "" && domain != "" {
			return JoinPath("market", geo, domain), true
		}
		return "", false

	case "fac":
		if p.Fac == nil || p.Fac.FacilitySlug == "" {
			return "", false
		}
		geo := strings.TrimSpace(p.Fac.Geo)
		if geo == "" {
			geo = "na"
		}
		return JoinPath("fac", geo, p.Fac.FacilitySlug), true

	case "program":
		if p.Program == nil || p.Program.Slug == "" {
			return "", false
		}
		j := strings.TrimSpace(p.Program.Jurisdiction)
		if j == "" {
			j = "global"
		}
		return JoinPath("program", j, p.Program.Slug), true

	case "project":
		if p.Project == nil || p.Project.Slug == "" {
			return "", false
		}
		j := strings.TrimSpace(p.Project.Jurisdiction)
		if j == "" {
			j = "global"
		}
		return JoinPath("project", j, p.Project.Slug), true

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
		resource := strings.TrimSpace(p.Deposit.Resource)
		if resource == "" {
			resource = "other"
		}
		return JoinPath("deposit", resource, p.Deposit.Slug), true

	case "evidence":
		if p.Evidence == nil || p.Evidence.Slug == "" {
			return "", false
		}
		return JoinPath("evidence", p.Evidence.Slug), true

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

	case "domain", "zone", "supply-link":
		if p.RawPathParts == nil || len(p.RawPathParts.Segments) < 2 {
			return "", false
		}
		return "/" + strings.Join(p.RawPathParts.Segments, "/"), true
	}
	return "", false
}

// legalFormSuffixesForParse used when parsing legacy paths (e.g. /org/us/tesla-inc). Strip to get base.
var legalFormSuffixesForParse = []string{
	"pjsc", "cjsc", "ojsc", "oao", "zao", "ao", "jsc", "ooo",
	"pllc", "llc", "inc", "ltd", "corp", "plc", "gmbh", "sarl", "srl", "ag", "sa", "spa",
	"as", "ab", "bv", "nv", "se", "co", "llp", "lp", "sp", "sup", "fsup", "soe",
	"kk", "oy", "oyj", "ltda", "sdnbhd", "ptyltd", "spzoo", "dac", "teo", "ulc",
	"scs", "sca", "snc", "senc", "sas", "sasu", "asa", "pteltd",
}

func splitOrgSlugLegalForm(slug string) (base string, legalForm string) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", ""
	}
	slug = Slugify(slug)
	if slug == "" {
		return "", ""
	}
	// Sort by length descending to match longer forms first (pjsc before jsc)
	suffixes := make([]string, len(legalFormSuffixesForParse))
	copy(suffixes, legalFormSuffixesForParse)
	sort.Slice(suffixes, func(i, j int) bool { return len(suffixes[i]) > len(suffixes[j]) })
	for _, lf := range suffixes {
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
