package canonical

import "strings"

var orgForbiddenPlaceholderSlugs = map[string]struct{}{
	"na":                             {},
	"retailer":                       {},
	"freight-forwarder":              {},
	"generic-component-manufacturer": {},
	"generic-raw-material-supplier":  {},
	"electronic-component-supplier":  {},
	"upstream-steel-supplier":        {},
	"e-commerce-merchants":           {},
	"internal-security-providers":    {},
	"naval-forces":                   {},
	"generic-distributor":            {},
	"generic-logistics-provider":     {},
}

var govForbiddenPlaceholderSlugs = map[string]struct{}{
	"na":                          {},
	"ministry-of":                 {},
	"ministry-of-defense":         {},
	"ministry-of-interior":        {},
	"ministry-of-petroleum":       {},
	"united-states-department-of": {},
	"department-of":               {},
	"bureau-of":                   {},
}

func OrgSlugForbiddenPlaceholder(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	_, ok := orgForbiddenPlaceholderSlugs[slug]
	return ok
}

func GovSlugForbiddenPlaceholder(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	_, ok := govForbiddenPlaceholderSlugs[slug]
	return ok
}
