package taxonomy

import (
	"strings"
)

// Scout/Research specialties are optional task-focus modules that steer extraction + grounded research.
// They are intentionally stored as plain strings in job state / lens viewConfig to keep schemas stable.
//
// Canonical specialty names (stable API):
// - "management": executive leadership / board / founders (people "above" the org)
// - "products_and_services": products/services the org produces/offers + inputs it consumes (and product->product transforms when scouting products)
const (
	SpecialtyManagement          = "management"
	SpecialtyProductsAndServices = "products_and_services"
)

var specialtySynonyms = map[string]string{
	// management
	"management":      SpecialtyManagement,
	"exec":            SpecialtyManagement,
	"executive":       SpecialtyManagement,
	"leadership":      SpecialtyManagement,
	"cxo":             SpecialtyManagement,
	"ceo":             SpecialtyManagement,
	"board":           SpecialtyManagement,
	"founder":         SpecialtyManagement,
	"governance":      SpecialtyManagement,
	"mgmt":            SpecialtyManagement,
	"management_team": SpecialtyManagement,

	// products/services
	"products_and_services": SpecialtyProductsAndServices,
	"products":              SpecialtyProductsAndServices,
	"services":              SpecialtyProductsAndServices,
	"offerings":             SpecialtyProductsAndServices,
	"products-services":     SpecialtyProductsAndServices,
	"product_io":            SpecialtyProductsAndServices,
	"product-io":            SpecialtyProductsAndServices,
	"productio":             SpecialtyProductsAndServices,
	"outputs":               SpecialtyProductsAndServices,
	"inputs_outputs":        SpecialtyProductsAndServices,
}

// NormalizeSpecialties takes a user-provided list and returns a deduplicated list of canonical specialties.
// Unknown values are ignored (forward compatible; prevents accidental prompt injection via arbitrary strings).
func NormalizeSpecialties(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))

	var add func(s string)
	add = func(s string) {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			return
		}
		// Allow comma-separated convenience.
		if strings.Contains(s, ",") {
			for _, p := range strings.Split(s, ",") {
				add(p)
			}
			return
		}
		if canon, ok := specialtySynonyms[s]; ok {
			s = canon
		}
		switch s {
		case SpecialtyManagement, SpecialtyProductsAndServices:
			// ok
		default:
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	for _, s := range in {
		add(s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func HasSpecialty(specialties []string, specialty string) bool {
	specialty = strings.TrimSpace(strings.ToLower(specialty))
	if specialtySynonyms[specialty] != "" {
		specialty = specialtySynonyms[specialty]
	}
	for _, s := range specialties {
		if strings.TrimSpace(strings.ToLower(s)) == specialty {
			return true
		}
	}
	return false
}
