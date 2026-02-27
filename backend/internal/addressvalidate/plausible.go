package addressvalidate

import (
	"regexp"
	"strings"
)

// nonAddressPatterns match strings that are clearly not addresses (metrics, headlines, etc.).
var nonAddressPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\s*=\s*\d`), // "comments = 185.000000", "x = 0.5"
	regexp.MustCompile(`_ratio|_improvement|improvement_ratio`),
	regexp.MustCompile(`\b(upvotes|comments|hn_comments|post_points|post_comments)\b`),
	regexp.MustCompile(`\b(beats|won't|CLI|database|SQL|LSM-tree|WAL|boosts?|diagnosis)\b`),
	regexp.MustCompile(`\d+\.\d{4,}`), // long decimal like 0.100000
}

const maxPlausibleAddressLen = 220

// IsPlausibleAddress returns false for strings that are clearly not addresses.
// Used to avoid calling geocoding APIs with headlines, metrics, or other garbage.
func IsPlausibleAddress(addr string) bool {
	s := strings.TrimSpace(addr)
	if s == "" || len(s) > maxPlausibleAddressLen {
		return false
	}
	lower := strings.ToLower(s)
	for _, re := range nonAddressPatterns {
		if re.MatchString(lower) {
			return false
		}
	}
	// "The X" long strings are often headlines
	if strings.HasPrefix(lower, "the ") && len(s) > 50 {
		return false
	}
	return true
}
