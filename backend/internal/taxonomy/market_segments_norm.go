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
	"public":             "public-sector",
	"government":         "public-sector",
	"gov":                "public-sector",
	"b2g":                "public-sector",
	"fintech":            "finance",
	"financial-services": "finance",
	"industrials":        "industrial",
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
