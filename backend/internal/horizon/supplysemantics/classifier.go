package supplysemantics

import "strings"

const (
	SegmentClassExternalProductFlow = "external_product_flow"
	SegmentClassInternalSupport     = "internal_support"
	SegmentClassInternalActorLink   = "internal_actor_link"
	SegmentClassInvalidSelfLoop     = "invalid_self_loop"
)

type SegmentClassification struct {
	Class   string
	Reasons []string
}

func ClassifySegment(supplierRef, productRef, productName, customerRef string, transportVia []string) SegmentClassification {
	return ClassifySegmentWithContext(supplierRef, "", "", productRef, productName, customerRef, "", "", transportVia)
}

func ClassifySegmentWithContext(
	supplierRef string,
	supplierName string,
	supplierRole string,
	productRef string,
	productName string,
	customerRef string,
	customerName string,
	customerRole string,
	transportVia []string,
) SegmentClassification {
	supplierRef = strings.TrimSpace(supplierRef)
	productRef = strings.TrimSpace(productRef)
	productName = strings.TrimSpace(productName)
	customerRef = strings.TrimSpace(customerRef)
	supplierRole = normalizeRoleHint(supplierRole)
	customerRole = normalizeRoleHint(customerRole)

	if supplierRef != "" && customerRef != "" && supplierRef == customerRef {
		return SegmentClassification{
			Class:   SegmentClassInvalidSelfLoop,
			Reasons: []string{"same_supplier_customer"},
		}
	}

	if looksLikeInternalSupportProduct(productRef, productName) && hasOperationalFacilityContext(supplierRef, customerRef, transportVia, productRef, productName) {
		return SegmentClassification{
			Class:   SegmentClassInternalSupport,
			Reasons: []string{"internal_support_product"},
		}
	}

	if looksLikeInternalActorLink(supplierRef, supplierName, supplierRole, customerRef, customerName, customerRole) {
		reasons := []string{"facility_org_internal_actor_link"}
		if customerRole != "" {
			reasons = append(reasons, "customer_role:"+customerRole)
		}
		if supplierRole != "" {
			reasons = append(reasons, "supplier_role:"+supplierRole)
		}
		return SegmentClassification{
			Class:   SegmentClassInternalActorLink,
			Reasons: reasons,
		}
	}

	return SegmentClassification{
		Class: SegmentClassExternalProductFlow,
	}
}

func IsMaterializableSegmentClass(class string) bool {
	class = strings.TrimSpace(class)
	return class == "" || class == SegmentClassExternalProductFlow
}

func normalizeRoleHint(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	role = strings.ReplaceAll(role, "-", "_")
	role = strings.ReplaceAll(role, " ", "_")
	return role
}

func hasOperationalFacilityContext(supplierRef, customerRef string, transportVia []string, productRef, productName string) bool {
	if strings.HasPrefix(strings.TrimSpace(supplierRef), "/fac/") || strings.HasPrefix(strings.TrimSpace(customerRef), "/fac/") {
		return true
	}
	for _, via := range transportVia {
		if strings.HasPrefix(strings.TrimSpace(via), "/fac/") {
			return true
		}
	}
	joined := strings.ToLower(strings.TrimSpace(productRef) + " " + strings.TrimSpace(productName))
	for _, kw := range []string{
		"port", "terminal", "warehouse", "industrial", "site", "plant", "refinery",
		"fab", "airport", "pipeline", "yard", "depot", "jetty", "tank farm", "facility",
	} {
		if strings.Contains(joined, kw) {
			return true
		}
	}
	return false
}

func looksLikeInternalSupportProduct(productRef, productName string) bool {
	joined := strings.ToLower(strings.TrimSpace(productRef) + " " + strings.TrimSpace(productName))
	if joined == "" {
		return false
	}

	for _, phrase := range []string{
		"port operations",
		"terminal operations",
		"warehouse operations",
		"logistics services",
		"distribution services",
		"industrial infrastructure",
		"site services",
		"facility services",
		"maintenance services",
		"handling services",
		"storage services",
		"utility services",
	} {
		if strings.Contains(joined, phrase) {
			return true
		}
	}

	hasSupport := false
	for _, token := range []string{
		"operations", "operation", "services", "service", "infrastructure",
		"utility", "utilities", "maintenance", "handling", "loading",
		"unloading", "storage", "warehousing", "distribution", "logistics",
	} {
		if strings.Contains(joined, token) {
			hasSupport = true
			break
		}
	}
	if !hasSupport {
		return false
	}
	for _, token := range []string{
		"port", "terminal", "warehouse", "industrial", "site", "plant",
		"refinery", "fab", "airport", "pipeline", "yard", "depot", "facility",
	} {
		if strings.Contains(joined, token) {
			return true
		}
	}
	return false
}

func looksLikeInternalActorLink(supplierRef, supplierName, supplierRole, customerRef, customerName, customerRole string) bool {
	supplierRef = strings.TrimSpace(supplierRef)
	customerRef = strings.TrimSpace(customerRef)
	if !strings.HasPrefix(supplierRef, "/fac/") {
		return false
	}
	if !strings.HasPrefix(customerRef, "/org/") && !strings.HasPrefix(customerRef, "/gov/") {
		return false
	}
	if isExplicitExternalCustomerRole(customerRole) {
		return false
	}
	if isOperatorOrControlRole(customerRole) || isOperatorOrControlRole(supplierRole) {
		return true
	}
	return sharesOperatorIdentity(supplierRef, supplierName, customerRef, customerName)
}

func isExplicitExternalCustomerRole(role string) bool {
	for _, token := range []string{
		"customer", "buyer", "offtaker", "consumer", "recipient", "distributor",
		"importer", "retailer", "trader", "wholesaler", "end_user", "processor",
		"assembler", "smelter", "refiner", "utility", "airline",
	} {
		if strings.Contains(role, token) {
			return true
		}
	}
	return false
}

func isOperatorOrControlRole(role string) bool {
	for _, token := range []string{
		"owner", "parent", "parent_owner", "holding", "controller", "controlled",
		"operator", "operating_company", "manager", "manages", "managed",
		"sponsor", "state", "administrator", "admin", "asset_owner",
	} {
		if strings.Contains(role, token) {
			return true
		}
	}
	return false
}

func sharesOperatorIdentity(supplierRef, supplierName, customerRef, customerName string) bool {
	facilityTokens := identityTokens(supplierRef + " " + supplierName)
	orgTokens := identityTokens(customerRef + " " + customerName)
	if len(facilityTokens) == 0 || len(orgTokens) == 0 {
		return false
	}
	for token := range facilityTokens {
		if _, ok := orgTokens[token]; ok {
			return true
		}
	}
	return false
}

func identityTokens(raw string) map[string]struct{} {
	raw = strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", ",", " ", "(", " ", ")", " ")
	raw = replacer.Replace(raw)
	stop := map[string]struct{}{
		"fac": {}, "org": {}, "gov": {}, "na": {}, "the": {}, "and": {}, "of": {}, "for": {},
		"company": {}, "co": {}, "corp": {}, "corporation": {}, "plc": {}, "ltd": {}, "llc": {}, "ag": {}, "sa": {},
		"facility": {}, "plant": {}, "port": {}, "terminal": {}, "refinery": {}, "fab": {}, "airport": {}, "complex": {}, "hub": {}, "center": {}, "city": {},
	}
	out := make(map[string]struct{})
	for _, token := range strings.Fields(raw) {
		if len(token) < 4 {
			continue
		}
		if _, skip := stop[token]; skip {
			continue
		}
		out[token] = struct{}{}
	}
	return out
}
