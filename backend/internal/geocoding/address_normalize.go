package geocoding

import (
	"strings"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/addressvalidate"
)

// IsPlausibleAddress delegates to addressvalidate (avoids import cycle with discovery).
func IsPlausibleAddress(addr string) bool {
	return addressvalidate.IsPlausibleAddress(addr)
}

// countryToISO2 maps common country names and codes to ISO3166-1 alpha2 (lowercase).
// Used for cache key normalization so "Moscow, RU", "Moscow, Russia", "Moscow, Russian Federation"
// all resolve to the same cache key.
var countryToISO2 = map[string]string{
	"united states of america": "us", "united states": "us", "usa": "us", "america": "us",
	"people's republic of china": "cn", "china": "cn", "prc": "cn",
	"russian federation": "ru", "russia": "ru",
	"united kingdom of great britain and northern ireland": "gb", "united kingdom": "gb",
	"great britain": "gb", "uk": "gb", "britain": "gb",
	"federal republic of germany": "de", "germany": "de", "deutschland": "de",
	"french republic": "fr", "france": "fr",
	"japan": "jp",
	"republic of korea": "kr", "south korea": "kr", "korea": "kr",
	"democratic people's republic of korea": "kp", "north korea": "kp", "dprk": "kp",
	"republic of india": "in", "india": "in",
	"federative republic of brazil": "br", "brazil": "br",
	"canada": "ca",
	"commonwealth of australia": "au", "australia": "au",
	"italian republic": "it", "italy": "it",
	"kingdom of spain": "es", "spain": "es",
	"united mexican states": "mx", "mexico": "mx",
	"republic of indonesia": "id", "indonesia": "id",
	"kingdom of the netherlands": "nl", "netherlands": "nl", "holland": "nl",
	"kingdom of saudi arabia": "sa", "saudi arabia": "sa",
	"swiss confederation": "ch", "switzerland": "ch",
	"republic of turkey": "tr", "turkey": "tr", "türkiye": "tr",
	"kingdom of sweden": "se", "sweden": "se",
	"republic of poland": "pl", "poland": "pl",
	"kingdom of belgium": "be", "belgium": "be",
	"republic of austria": "at", "austria": "at",
	"kingdom of norway": "no", "norway": "no",
	"kingdom of denmark": "dk", "denmark": "dk",
	"republic of finland": "fi", "finland": "fi",
	"hellenic republic": "gr", "greece": "gr",
	"portuguese republic": "pt", "portugal": "pt",
	"ireland": "ie",
	"new zealand": "nz",
	"republic of singapore": "sg", "singapore": "sg",
	"hong kong special administrative region": "hk", "hong kong": "hk",
	"republic of china": "tw", "taiwan": "tw",
	"state of israel": "il", "israel": "il",
	"united arab emirates": "ae", "uae": "ae",
	"islamic republic of iran": "ir", "iran": "ir",
	"republic of iraq": "iq", "iraq": "iq",
	"arab republic of egypt": "eg", "egypt": "eg",
	"republic of south africa": "za", "south africa": "za",
	"federal republic of nigeria": "ng", "nigeria": "ng",
	"argentine republic": "ar", "argentina": "ar",
	"republic of chile": "cl", "chile": "cl",
	"republic of colombia": "co", "colombia": "co",
	"bolivarian republic of venezuela": "ve", "venezuela": "ve",
	"republic of peru": "pe", "peru": "pe",
	"republic of the philippines": "ph", "philippines": "ph",
	"socialist republic of vietnam": "vn", "vietnam": "vn",
	"kingdom of thailand": "th", "thailand": "th",
	"malaysia": "my",
	"islamic republic of pakistan": "pk", "pakistan": "pk",
	"people's republic of bangladesh": "bd", "bangladesh": "bd",
	"czech republic": "cz", "czechia": "cz",
	"romania": "ro", "hungary": "hu", "ukraine": "ua",
	"republic of belarus": "by", "belarus": "by",
	"republic of kazakhstan": "kz", "kazakhstan": "kz",
	"republic of uzbekistan": "uz", "uzbekistan": "uz",
	"republic of azerbaijan": "az", "azerbaijan": "az",
	"georgia": "ge",
	"republic of armenia": "am", "armenia": "am",
	"kyrgyz republic": "kg", "kyrgyzstan": "kg",
	"republic of tajikistan": "tj", "tajikistan": "tj",
	"turkmenistan": "tm", "mongolia": "mn",
	"bahrain": "bh", "kuwait": "kw", "oman": "om", "qatar": "qa", "yemen": "ye",
	"brunei": "bn", "brunei darussalam": "bn",
	"cambodia": "kh", "laos": "la", "lao people's democratic republic": "la",
	"myanmar": "mm", "burma": "mm",
	"timor-leste": "tl", "east timor": "tl",
	"algeria": "dz", "angola": "ao", "morocco": "ma", "tunisia": "tn",
	"afghanistan": "af", "sri lanka": "lk", "nepal": "np", "maldives": "mv",
	// Aliases
	"u.s.a.": "us", "u.s.": "us", "u.k.": "gb", "p.r.c.": "cn",
	"mainland china": "cn", "soviet union": "ru", "ussr": "ru", "u.s.s.r.": "ru",
	"roc": "tw", "republic of taiwan": "tw",
}

// normalizeCountryPart returns ISO2 for known country names/codes, or the original string lowercased.
func normalizeCountryPart(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	// Check aliases first (e.g. "uk"->"gb", "usa"->"us")
	if iso2, ok := countryToISO2[s]; ok {
		return iso2
	}
	// Unknown: keep 2-letter codes as-is (valid ISO2), otherwise return lowercased
	if len(s) == 2 && unicode.IsLetter(rune(s[0])) && unicode.IsLetter(rune(s[1])) {
		return s
	}
	return s
}

// addressVariantsForCacheLookup returns candidate cache keys for an address.
// Primary is the fully normalized form; variants help match minor variations
// (e.g. "Moscow, RU" vs "Moscow, Russia") when the cache was populated with one form.
func addressVariantsForCacheLookup(addr string) []string {
	primary := normalizeAddressForCache(addr)
	if primary == "" {
		return nil
	}
	seen := map[string]struct{}{primary: {}}
	out := []string{primary}

	// Try normalizing the last comma-separated part (usually country)
	parts := splitAddressParts(primary)
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		rest := strings.Join(parts[:len(parts)-1], ", ")
		normLast := normalizeCountryPart(last)
		if normLast != last {
			variant := rest + ", " + normLast
			if _, ok := seen[variant]; !ok {
				seen[variant] = struct{}{}
				out = append(out, variant)
			}
		}
	}
	return out
}

func splitAddressParts(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
