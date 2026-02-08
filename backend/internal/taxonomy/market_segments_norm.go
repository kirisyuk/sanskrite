package taxonomy

import (
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

// SegmentNormRule defines an explicit override mapping for a market segment (domain/vertical).
// Key format (recommended): "/segment/{domain}/{vertical}" (geo is ignored).
type SegmentNormRule struct {
	NormDomain   string
	NormVertical string
	Channel      string // optional
}

var builtInChannelLikeDomains = map[string]struct{}{
	"b2b":           {},
	"b2c":           {},
	"commercial":    {},
	"industrial":    {},
	"consumer":      {},
	"public-sector": {},
	"public":        {},
	"government":    {},
	"gov":           {},
	"b2g":           {},
}

var builtInDomainAliases = map[string]string{
	// Public sector
	"public":     "public-sector",
	"government": "public-sector",
	"gov":        "public-sector",
	"b2g":        "public-sector",
	// Finance (GICS: "Financials" sector)
	"fintech":            "finance",
	"financial":          "finance",
	"financials":         "finance",
	"financial-services": "finance",
	"banking":            "finance",
	// Industrial (GICS: "Industrials" sector)
	"industrials": "industrial",
	// Business / professional-services
	"business":          "professional-services",
	"business-services": "professional-services",
	// Telecom variants (GICS: "Communication Services")
	"telecommunications":     "telecom",
	"communication-services": "telecom",
	"communications":         "telecom",
	// Real estate variants
	"real-estate": "real-estate",
	"realestate":  "real-estate",
	"property":    "real-estate",
	// Healthcare variants
	"health-care": "healthcare",
	"health":      "healthcare",
	// Consumer variants (GICS splits into "Consumer Discretionary" / "Consumer Staples")
	"consumer-discretionary": "consumer",
	"consumer-staples":       "consumer",
	// Materials (GICS: "Materials" sector)
	"basic-materials": "materials",
	// IT / tech variants
	"information-technology": "technology",
	"it":                     "technology",
	"tech":                   "technology",
}

var builtInVerticalAliases = map[string]string{
	"retail-banking": "banking",
	"cryptocurrency": "crypto",
}

var builtInVerticalFamily = map[string]string{
	// Finance family
	"banking":            "finance",
	"investment":         "finance",
	"asset-management":   "finance",
	"private-equity":     "finance",
	"venture-capital":    "finance",
	"financial-services": "finance",
	"fintech":            "finance",
	"insurance":          "finance",
	"payments":           "finance",
	"crypto":             "finance",
	// Technology family
	"software":       "technology",
	"it-services":    "technology",
	"cybersecurity":  "technology",
	"semiconductors": "technology",
	"electronics":    "technology",
	// Energy family
	"oil-and-gas": "energy",
	"utilities":   "energy",
	"renewables":  "energy",
	// Leisure nuance
	"casinos":  "entertainment",
	"gambling": "entertainment",
}

func normalizeSegToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// NormalizeMarketDomain applies domain alias normalization to a raw market domain string.
// Returns the canonical domain (e.g., "financials" → "finance", "business" → "professional-services").
// If no alias is found, returns the input slugified and lowercased.
// This MUST be called when:
//   - Building market canonical paths (to prevent creation of non-canonical domains)
//   - Comparing market domains for deduplication (to treat "financials" == "finance")
func NormalizeMarketDomain(raw string) string {
	d := strings.ToLower(strings.TrimSpace(raw))
	if d == "" {
		return ""
	}
	_, domainAliases, _, _, _ := runtimeSegmentRules()
	if canonical, ok := domainAliases[d]; ok && canonical != "" {
		return canonical
	}
	return d
}

func runtimeSegmentRules() (channelLike map[string]struct{}, domainAliases map[string]string, verticalAliases map[string]string, verticalFamily map[string]string, explicit map[string]SegmentNormRule) {
	// Start with built-ins.
	channelLike = map[string]struct{}{}
	for k := range builtInChannelLikeDomains {
		channelLike[k] = struct{}{}
	}
	domainAliases = map[string]string{}
	for k, v := range builtInDomainAliases {
		domainAliases[k] = v
	}
	verticalAliases = map[string]string{}
	for k, v := range builtInVerticalAliases {
		verticalAliases[k] = v
	}
	verticalFamily = map[string]string{}
	for k, v := range builtInVerticalFamily {
		verticalFamily[k] = v
	}
	explicit = map[string]SegmentNormRule{}

	// Overlay Firestore-backed market segment dictionary if present.
	if d := GetMarketSegmentDictionary(); d != nil {
		if len(d.ChannelDomains) > 0 {
			channelLike = map[string]struct{}{}
			for k := range d.ChannelDomains {
				channelLike[k] = struct{}{}
			}
		}
		for k, v := range d.DomainAliases {
			if k == "" || v == "" {
				continue
			}
			domainAliases[k] = v
		}
		for k, v := range d.VerticalAliases {
			if k == "" || v == "" {
				continue
			}
			verticalAliases[k] = v
		}
		for k, v := range d.VerticalFamilies {
			if k == "" || v == "" {
				continue
			}
			verticalFamily[k] = v
		}
		for k, v := range d.NormRules {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			explicit[k] = v
		}
	}
	return
}

func normPair(domain, vertical string) string {
	domain = strings.TrimSpace(domain)
	vertical = strings.TrimSpace(vertical)
	if domain == "" {
		return ""
	}
	if vertical == "" {
		return domain
	}
	return domain + "/" + vertical
}

// NormalizeMarketSegments converts canonical marketSegments (/segment/{domain}/{vertical}/{geo}) into:
// - per-segment normals (domain/vertical pair; channel when applicable)
// - deduped domain list
// - deduped vertical list
// - deduped pair list
//
// It is deterministic and safe; unknown values are preserved conservatively.
func NormalizeMarketSegments(marketSegments []string) ([]models.MarketSegmentNorm, []string, []string, []string) {
	if len(marketSegments) == 0 {
		return nil, nil, nil, nil
	}

	channelLike, domainAliases, verticalAliases, verticalFamily, explicit := runtimeSegmentRules()

	out := make([]models.MarketSegmentNorm, 0, len(marketSegments))

	seenNormPair := map[string]struct{}{}
	seenDom := map[string]struct{}{}
	seenVert := map[string]struct{}{}

	for _, s := range marketSegments {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		seg, ok := NormalizeSegmentPath(s)
		if !ok || seg == "" {
			continue
		}
		parts := strings.Split(strings.Trim(seg, "/"), "/")
		if len(parts) < 4 || parts[0] != "segment" {
			continue
		}
		domain := normalizeSegToken(parts[1])
		vertical := normalizeSegToken(parts[2])

		// Apply aliases (domain, then vertical).
		if v, ok := domainAliases[domain]; ok && strings.TrimSpace(v) != "" {
			domain = normalizeSegToken(v)
		}
		if v, ok := verticalAliases[vertical]; ok && strings.TrimSpace(v) != "" {
			vertical = normalizeSegToken(v)
		}

		channel := ""
		isChannel := false
		if _, ok := channelLike[domain]; ok {
			isChannel = true
			channel = domain
		}

		// Explicit override key: /segment/{domain}/{vertical} (after aliases).
		key := "/segment/" + domain + "/" + vertical
		if rule, ok := explicit[key]; ok && strings.TrimSpace(rule.NormDomain) != "" {
			nd := normalizeSegToken(rule.NormDomain)
			nv := normalizeSegToken(rule.NormVertical)
			ch := strings.TrimSpace(rule.Channel)
			if ch == "" && isChannel {
				ch = channel
			}
			p := normPair(nd, nv)
			if p == "" {
				continue
			}
			n := models.MarketSegmentNorm{
				SegmentPath:  seg,
				NormDomain:   nd,
				NormVertical: nv,
				NormPair:     p,
				Channel:      normalizeSegToken(ch),
			}
			out = append(out, n)
			if _, ok := seenNormPair[p]; !ok {
				seenNormPair[p] = struct{}{}
			}
			if nd != "" {
				seenDom[nd] = struct{}{}
			}
			if nv != "" {
				seenVert[nv] = struct{}{}
			}
			continue
		}

		nd := ""
		nv := ""
		if isChannel {
			// Channel-like domain: vertical often carries the actual market signal.
			if fam, ok := verticalFamily[vertical]; ok && strings.TrimSpace(fam) != "" {
				nd = normalizeSegToken(fam)
				nv = vertical
			} else {
				nd = vertical
				nv = ""
			}
		} else {
			// Regular domain/vertical.
			nd = domain
			nv = vertical
		}

		p := normPair(nd, nv)
		if p == "" {
			continue
		}

		n := models.MarketSegmentNorm{
			SegmentPath:  seg,
			NormDomain:   nd,
			NormVertical: nv,
			NormPair:     p,
			Channel:      channel,
		}
		out = append(out, n)

		seenNormPair[p] = struct{}{}
		if nd != "" {
			seenDom[nd] = struct{}{}
		}
		if nv != "" {
			seenVert[nv] = struct{}{}
		}
	}

	if len(out) == 0 {
		return nil, nil, nil, nil
	}

	doms := make([]string, 0, len(seenDom))
	for d := range seenDom {
		doms = append(doms, d)
	}
	sort.Strings(doms)

	verts := make([]string, 0, len(seenVert))
	for v := range seenVert {
		verts = append(verts, v)
	}
	sort.Strings(verts)

	pairs := make([]string, 0, len(seenNormPair))
	for p := range seenNormPair {
		pairs = append(pairs, p)
	}
	sort.Strings(pairs)

	// Preserve out order (same as marketSegments) — useful for UI "top segments".
	return out, doms, verts, pairs
}

// ---------------------------------------------------------------------------
// Region → member country mapping (ISO-2 lowercase)
// Used for geo-mismatch validation: an org from a member country can
// legitimately participate in a regional market (e.g. AE org → /market/gcc/…).
// ---------------------------------------------------------------------------

// regionMemberCountries maps region tokens to their member country ISO-2 codes.
var regionMemberCountries = map[string]map[string]struct{}{
	"gcc": toSet("sa", "ae", "bh", "kw", "qa", "om"),
	"mena": toSet(
		// GCC
		"sa", "ae", "bh", "kw", "qa", "om",
		// Levant + North Africa + others
		"eg", "iq", "jo", "lb", "ps", "sy", "ye",
		"dz", "ly", "ma", "tn", "sd", "mr",
		"ir",
	),
	"eu": toSet(
		"at", "be", "bg", "hr", "cy", "cz", "dk", "ee", "fi", "fr",
		"de", "gr", "hu", "ie", "it", "lv", "lt", "lu", "mt", "nl",
		"pl", "pt", "ro", "sk", "si", "es", "se",
	),
	"eaeu": toSet("ru", "by", "kz", "am", "kg"),
	"asean": toSet(
		"bn", "kh", "id", "la", "my", "mm", "ph", "sg", "th", "vn",
	),
	"brics": toSet("br", "ru", "in", "cn", "za", "eg", "et", "ir", "ae", "sa"),
	"opec": toSet(
		"dz", "ao", "cg", "gq", "ga", "ir", "iq", "kw", "ly", "ng",
		"sa", "ae", "ve",
	),
	"nafta": toSet("us", "ca", "mx"),  // USMCA
	"mercosur": toSet("ar", "br", "py", "uy"),
	"cis": toSet(
		"ru", "by", "kz", "am", "az", "kg", "md", "tj", "uz",
	),
	"apac": toSet(
		"au", "cn", "hk", "id", "in", "jp", "kr", "my", "nz", "ph",
		"sg", "th", "tw", "vn",
	),
	"latam": toSet(
		"ar", "bo", "br", "cl", "co", "cr", "cu", "do", "ec", "sv",
		"gt", "hn", "mx", "ni", "pa", "py", "pe", "uy", "ve",
	),
	"emea": toSet(
		// Europe (major)
		"at", "be", "ch", "cz", "de", "dk", "es", "fi", "fr", "gb",
		"gr", "hu", "ie", "it", "nl", "no", "pl", "pt", "ro", "ru",
		"se", "tr", "ua",
		// Middle-East (major)
		"ae", "bh", "eg", "il", "iq", "ir", "jo", "kw", "lb", "om",
		"qa", "sa", "sy", "ye",
		// Africa (major)
		"dz", "et", "ke", "ly", "ma", "ng", "sd", "tn", "za",
	),
}

func toSet(codes ...string) map[string]struct{} {
	s := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		s[c] = struct{}{}
	}
	return s
}

// IsCountryInRegion returns true if the given country (ISO-2, lowercase)
// is a member of the specified region (e.g. "gcc", "mena", "eu").
// Returns false for unknown regions.
func IsCountryInRegion(region, country string) bool {
	members, ok := regionMemberCountries[strings.ToLower(region)]
	if !ok {
		return false
	}
	_, found := members[strings.ToLower(country)]
	return found
}
