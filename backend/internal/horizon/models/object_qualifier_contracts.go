package models

import (
	"sort"
	"strings"
)

// ObjectQualifier is a controlled business qualifier used to steer specialization.
// Values are intentionally closed to avoid free-form hallucinated labels.
type ObjectQualifier string

const (
	ObjectQualifierNone ObjectQualifier = ""

	// Facility-oriented qualifiers
	ObjectQualifierRoute       ObjectQualifier = "route"
	ObjectQualifierSupply      ObjectQualifier = "supply"
	ObjectQualifierProcurement ObjectQualifier = "procurement"
	ObjectQualifierHub         ObjectQualifier = "hub"
	ObjectQualifierWarehouse   ObjectQualifier = "warehouse"
	ObjectQualifierProduction  ObjectQualifier = "production"

	// Organization-oriented qualifiers
	ObjectQualifierHolding     ObjectQualifier = "holding"
	ObjectQualifierOperator    ObjectQualifier = "operator"
	ObjectQualifierDistributor ObjectQualifier = "distributor"
	ObjectQualifierContractor  ObjectQualifier = "contractor"

	// Market-oriented qualifiers
	ObjectQualifierRegulated       ObjectQualifier = "regulated"
	ObjectQualifierConcentrated    ObjectQualifier = "concentrated"
	ObjectQualifierImportDependent ObjectQualifier = "import_dependent"

	// Product-oriented qualifiers
	ObjectQualifierCriticalInput ObjectQualifier = "critical_input"
	ObjectQualifierRegulatedGood ObjectQualifier = "regulated_good"

	// Government-oriented qualifiers
	ObjectQualifierRegulator     ObjectQualifier = "regulator"
	ObjectQualifierProcurer      ObjectQualifier = "procurer"
	ObjectQualifierOwnerOperator ObjectQualifier = "owner_operator"

	// Deposit-oriented qualifiers
	ObjectQualifierGreenfield ObjectQualifier = "greenfield"
	ObjectQualifierBrownfield ObjectQualifier = "brownfield"
	ObjectQualifierStrategic  ObjectQualifier = "strategic"
)

var objectQualifierAliases = map[string]ObjectQualifier{
	"route":           ObjectQualifierRoute,
	"route-facility":  ObjectQualifierRoute,
	"route_facility":  ObjectQualifierRoute,
	"logistics-route": ObjectQualifierRoute,
	"logistics_route": ObjectQualifierRoute,
	"corridor":        ObjectQualifierRoute,

	"supply":       ObjectQualifierSupply,
	"supplier":     ObjectQualifierSupply,
	"supply-chain": ObjectQualifierSupply,
	"supply_chain": ObjectQualifierSupply,
	"value-chain":  ObjectQualifierSupply,
	"value_chain":  ObjectQualifierSupply,
	"distribution": ObjectQualifierSupply,
	"fulfillment":  ObjectQualifierSupply,
	"procurement":  ObjectQualifierProcurement,
	"sourcing":     ObjectQualifierProcurement,
	"purchasing":   ObjectQualifierProcurement,
	"vendor":       ObjectQualifierProcurement,
	"hub":          ObjectQualifierHub,
	"warehouse":    ObjectQualifierWarehouse,
	"production":   ObjectQualifierProduction,

	"holding":          ObjectQualifierHolding,
	"operator":         ObjectQualifierOperator,
	"distributor":      ObjectQualifierDistributor,
	"contractor":       ObjectQualifierContractor,
	"regulated":        ObjectQualifierRegulated,
	"concentrated":     ObjectQualifierConcentrated,
	"import-dependent": ObjectQualifierImportDependent,
	"import_dependent": ObjectQualifierImportDependent,
	"critical-input":   ObjectQualifierCriticalInput,
	"critical_input":   ObjectQualifierCriticalInput,
	"regulated-good":   ObjectQualifierRegulatedGood,
	"regulated_good":   ObjectQualifierRegulatedGood,
	"regulator":        ObjectQualifierRegulator,
	"procurer":         ObjectQualifierProcurer,
	"owner-operator":   ObjectQualifierOwnerOperator,
	"owner_operator":   ObjectQualifierOwnerOperator,
	"greenfield":       ObjectQualifierGreenfield,
	"brownfield":       ObjectQualifierBrownfield,
	"strategic":        ObjectQualifierStrategic,
}

var allObjectQualifiers = []ObjectQualifier{
	ObjectQualifierRoute,
	ObjectQualifierSupply,
	ObjectQualifierProcurement,
	ObjectQualifierHub,
	ObjectQualifierWarehouse,
	ObjectQualifierProduction,
	ObjectQualifierHolding,
	ObjectQualifierOperator,
	ObjectQualifierDistributor,
	ObjectQualifierContractor,
	ObjectQualifierRegulated,
	ObjectQualifierConcentrated,
	ObjectQualifierImportDependent,
	ObjectQualifierCriticalInput,
	ObjectQualifierRegulatedGood,
	ObjectQualifierRegulator,
	ObjectQualifierProcurer,
	ObjectQualifierOwnerOperator,
	ObjectQualifierGreenfield,
	ObjectQualifierBrownfield,
	ObjectQualifierStrategic,
}

var objectQualifierAllowlistByType = map[FragmentType]map[ObjectQualifier]struct{}{
	FragmentTypeFacility: {
		ObjectQualifierRoute:       {},
		ObjectQualifierSupply:      {},
		ObjectQualifierProcurement: {},
		ObjectQualifierHub:         {},
		ObjectQualifierWarehouse:   {},
		ObjectQualifierProduction:  {},
		ObjectQualifierOperator:    {},
	},
	FragmentTypeOrganization: {
		ObjectQualifierHolding:     {},
		ObjectQualifierOperator:    {},
		ObjectQualifierDistributor: {},
		ObjectQualifierContractor:  {},
		ObjectQualifierSupply:      {},
		ObjectQualifierProcurement: {},
		ObjectQualifierRegulated:   {},
	},
	FragmentTypeMarket: {
		ObjectQualifierRegulated:       {},
		ObjectQualifierConcentrated:    {},
		ObjectQualifierImportDependent: {},
	},
	FragmentTypeProduct: {
		ObjectQualifierCriticalInput: {},
		ObjectQualifierRegulatedGood: {},
		ObjectQualifierStrategic:     {},
	},
	FragmentTypeDeposit: {
		ObjectQualifierGreenfield: {},
		ObjectQualifierBrownfield: {},
		ObjectQualifierStrategic:  {},
	},
	FragmentTypeProgram: {
		ObjectQualifierRegulated:   {},
		ObjectQualifierProcurement: {},
		ObjectQualifierStrategic:   {},
	},
	FragmentTypeGeoRegion: {
		ObjectQualifierRoute:       {},
		ObjectQualifierSupply:      {},
		ObjectQualifierProcurement: {},
		ObjectQualifierStrategic:   {},
	},
}

// Government entities are represented as ORGANIZATION with /gov/... paths.
var objectQualifierAllowlistGovPath = map[ObjectQualifier]struct{}{
	ObjectQualifierRegulator:     {},
	ObjectQualifierProcurer:      {},
	ObjectQualifierOwnerOperator: {},
	ObjectQualifierRegulated:     {},
	ObjectQualifierProcurement:   {},
}

func NormalizeObjectQualifier(raw string) ObjectQualifier {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return ObjectQualifierNone
	}
	if q, ok := objectQualifierAliases[s]; ok {
		return q
	}
	return ObjectQualifierNone
}

func IsAllowedObjectQualifierForType(fragmentType FragmentType, canonicalPath string, q ObjectQualifier) bool {
	if q == ObjectQualifierNone {
		return true
	}
	if strings.HasPrefix(strings.TrimSpace(strings.ToLower(canonicalPath)), "/gov/") {
		_, ok := objectQualifierAllowlistGovPath[q]
		return ok
	}
	if allowed, ok := objectQualifierAllowlistByType[fragmentType]; ok {
		_, ok = allowed[q]
		return ok
	}
	return false
}

func AllowedObjectQualifiersForType(fragmentType FragmentType, canonicalPath string) []ObjectQualifier {
	set := map[ObjectQualifier]struct{}{}
	if strings.HasPrefix(strings.TrimSpace(strings.ToLower(canonicalPath)), "/gov/") {
		for q := range objectQualifierAllowlistGovPath {
			set[q] = struct{}{}
		}
	}
	if allowed, ok := objectQualifierAllowlistByType[fragmentType]; ok {
		for q := range allowed {
			set[q] = struct{}{}
		}
	}
	out := make([]ObjectQualifier, 0, len(set))
	for q := range set {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return string(out[i]) < string(out[j]) })
	return out
}

func AllowedObjectQualifierStringsForType(fragmentType FragmentType, canonicalPath string) []string {
	qs := AllowedObjectQualifiersForType(fragmentType, canonicalPath)
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, string(q))
	}
	return out
}

// AllObjectQualifiers returns the global closed qualifier enum (without empty value).
func AllObjectQualifiers() []ObjectQualifier {
	out := make([]ObjectQualifier, len(allObjectQualifiers))
	copy(out, allObjectQualifiers)
	return out
}

// AllObjectQualifierStrings returns the global closed qualifier enum as strings.
func AllObjectQualifierStrings() []string {
	qs := AllObjectQualifiers()
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, string(q))
	}
	return out
}
