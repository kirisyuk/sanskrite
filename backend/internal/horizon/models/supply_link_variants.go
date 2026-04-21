package models

// Supply-link path helpers: canonical /supply-link/ paths, transport-via
// normalization, and route-variant merging for materialized legs.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

var canonicalTransportViaRefAliases = map[string]string{
	"/fac/ae/jebel-ali-terminal":      "/fac/ae/jebel-ali-port",
	"/fac/ae/jebel-ali-port-terminal": "/fac/ae/jebel-ali-port",
	"/fac/eg/suez-canal":              "/fac/eg/suez-terminal",
	"/fac/om/port-of-sohar":           "/fac/om/sohar-port",
	"/fac/sg/singapore-port":          "/fac/sg/port-of-singapore",
	"/fac/sea/strait-of-malacca":      "/fac/int/strait-of-malacca",
}

type SupplyLinkRouteVariant struct {
	VariantKey       string   `json:"variantKey,omitempty" firestore:"variantKey,omitempty"`
	TransportViaRefs []string `json:"transportViaRefs,omitempty" firestore:"transportViaRefs,omitempty"`
	VesselType       string   `json:"vesselType,omitempty" firestore:"vesselType,omitempty"`
	Confidence       float64  `json:"confidence,omitempty" firestore:"confidence,omitempty"`
}

// BuildSupplyLinkCanonicalPath generates a canonical path for a SupplyLink fragment.
// Format: /supply-link/{tenant}/{supplier-slug}/{product-slug}/{customer-slug}
// If supplier or customer is nil, uses "natural" or "end-consumer" respectively.
func BuildSupplyLinkCanonicalPath(tenantID string, supplierRef, productRef, customerRef *string) (string, error) {
	if tenantID == "" {
		return "", fmt.Errorf("tenantID must be set for SupplyLink")
	}
	if productRef == nil || strings.TrimSpace(*productRef) == "" {
		return "", fmt.Errorf("productRef is required for SupplyLink")
	}

	supplierSlug := buildSupplyLinkEndpointToken("natural", supplierRef)
	productSlug := buildSupplyLinkEndpointToken("", productRef)
	if productSlug == "" {
		return "", fmt.Errorf("invalid productRef: %s", *productRef)
	}
	customerSlug := buildSupplyLinkEndpointToken("end-consumer", customerRef)

	return fmt.Sprintf("/supply-link/%s/%s/%s/%s", tenantID, supplierSlug, productSlug, customerSlug), nil
}

func NormalizeTransportViaRefs(refs []string) []string {
	seen := make(map[string]struct{}, len(refs))
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = canonicalizeTransportViaRef(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func canonicalizeTransportViaRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if canonical, ok := canonicalTransportViaRefAliases[ref]; ok {
		return canonical
	}
	return ref
}

func BuildSupplyLinkRouteVariantKey(transportViaRefs []string) string {
	normalized := NormalizeTransportViaRefs(transportViaRefs)
	if len(normalized) == 0 {
		return "direct"
	}
	return "via-" + shortPathHash(strings.Join(normalized, "\n"))
}

func NewSupplyLinkRouteVariant(transportViaRefs []string, vesselType string, confidence float64) SupplyLinkRouteVariant {
	normalized := NormalizeTransportViaRefs(transportViaRefs)
	if confidence < 0 {
		confidence = 0
	}
	return SupplyLinkRouteVariant{
		VariantKey:       BuildSupplyLinkRouteVariantKey(normalized),
		TransportViaRefs: normalized,
		VesselType:       strings.TrimSpace(vesselType),
		Confidence:       confidence,
	}
}

func ExtractSupplyLinkRouteVariants(
	qualifiers map[string]interface{},
	legacyTransportViaRefs []string,
	legacyVesselType string,
	legacyConfidence float64,
) []SupplyLinkRouteVariant {
	raw := interface{}(nil)
	if qualifiers != nil {
		raw = qualifiers["routeVariants"]
	}
	decoded := decodeSupplyLinkRouteVariants(raw)
	if len(decoded) == 0 {
		return []SupplyLinkRouteVariant{
			NewSupplyLinkRouteVariant(legacyTransportViaRefs, legacyVesselType, legacyConfidence),
		}
	}
	for idx := range decoded {
		decoded[idx] = NormalizeSupplyLinkRouteVariant(decoded[idx])
	}
	return MergeSupplyLinkRouteVariants(nil, decoded)
}

func MergeSupplyLinkRouteVariants(existing, incoming []SupplyLinkRouteVariant) []SupplyLinkRouteVariant {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	merged := make(map[string]SupplyLinkRouteVariant, len(existing)+len(incoming))
	order := make([]string, 0, len(existing)+len(incoming))
	apply := func(items []SupplyLinkRouteVariant) {
		for _, item := range items {
			item = NormalizeSupplyLinkRouteVariant(item)
			if strings.TrimSpace(item.VariantKey) == "" {
				continue
			}
			if current, ok := merged[item.VariantKey]; ok {
				if item.Confidence > current.Confidence {
					current.Confidence = item.Confidence
				}
				if strings.TrimSpace(current.VesselType) == "" {
					current.VesselType = strings.TrimSpace(item.VesselType)
				}
				if len(current.TransportViaRefs) == 0 && len(item.TransportViaRefs) > 0 {
					current.TransportViaRefs = append([]string(nil), item.TransportViaRefs...)
				}
				merged[item.VariantKey] = current
				continue
			}
			merged[item.VariantKey] = item
			order = append(order, item.VariantKey)
		}
	}
	apply(existing)
	apply(incoming)
	out := make([]SupplyLinkRouteVariant, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return out
}

func PrimarySupplyLinkRouteVariant(variants []SupplyLinkRouteVariant) *SupplyLinkRouteVariant {
	if len(variants) == 0 {
		return nil
	}
	bestIdx := 0
	for idx := 1; idx < len(variants); idx++ {
		if variants[idx].Confidence > variants[bestIdx].Confidence {
			bestIdx = idx
			continue
		}
		if variants[idx].Confidence == variants[bestIdx].Confidence && len(variants[idx].TransportViaRefs) > len(variants[bestIdx].TransportViaRefs) {
			bestIdx = idx
		}
	}
	best := NormalizeSupplyLinkRouteVariant(variants[bestIdx])
	return &best
}

func NormalizeSupplyLinkRouteVariant(variant SupplyLinkRouteVariant) SupplyLinkRouteVariant {
	variant.TransportViaRefs = NormalizeTransportViaRefs(variant.TransportViaRefs)
	variant.VesselType = strings.TrimSpace(variant.VesselType)
	if strings.TrimSpace(variant.VariantKey) == "" {
		variant.VariantKey = BuildSupplyLinkRouteVariantKey(variant.TransportViaRefs)
	}
	return variant
}

func decodeSupplyLinkRouteVariants(raw interface{}) []SupplyLinkRouteVariant {
	if raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case []SupplyLinkRouteVariant:
		out := make([]SupplyLinkRouteVariant, 0, len(value))
		for _, item := range value {
			out = append(out, NormalizeSupplyLinkRouteVariant(item))
		}
		return out
	case []map[string]interface{}, []interface{}:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out []SupplyLinkRouteVariant
		if err := json.Unmarshal(payload, &out); err != nil {
			return nil
		}
		for idx := range out {
			out[idx] = NormalizeSupplyLinkRouteVariant(out[idx])
		}
		return out
	default:
		return nil
	}
}

func buildSupplyLinkEndpointToken(fallback string, ref *string) string {
	if ref == nil || strings.TrimSpace(*ref) == "" {
		return normalizeSlug(fallback)
	}
	cp := strings.TrimSpace(*ref)
	slug := normalizeSlug(extractSlugFromPath(cp))
	if slug == "" {
		slug = normalizeSlug(fallback)
	}
	if slug == "" {
		return ""
	}
	return slug + "-" + shortPathHash(cp)
}

func shortPathHash(path string) string {
	raw := strings.ToLower(strings.TrimSpace(path))
	if raw == "" {
		return ""
	}
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:5])
}

// NormalizeOrgRefToFacilityFallback intentionally does not synthesize facility placeholders from
// /org or /gov paths. The graph now prefers org->product when the concrete facility is unknown.
// Returns the original path only for already-concrete /fac or /deposit refs; otherwise empty.
func NormalizeOrgRefToFacilityFallback(orgPath string) string {
	orgPath = strings.TrimSpace(orgPath)
	if orgPath == "" {
		return ""
	}
	if strings.HasPrefix(orgPath, "/fac/") || strings.HasPrefix(orgPath, "/deposit/") {
		return orgPath
	}
	return ""
}

// extractSlugFromPath extracts the last segment from a canonical path.
// E.g., "/org/albemarle" -> "albemarle", "/product/lithium-carbonate" -> "lithium-carbonate"
func extractSlugFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// Remove leading slash
	path = strings.TrimPrefix(path, "/")
	// Split by "/" and take the last segment
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// normalizeSlug normalizes a slug for use in canonical paths.
// Converts to lowercase, replaces spaces and special chars with hyphens.
func normalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Replace spaces and underscores with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Remove any non-alphanumeric characters except hyphens
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	s = result.String()
	// Remove consecutive hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	// Remove leading/trailing hyphens
	s = strings.Trim(s, "-")
	if s == "" {
		return "unknown"
	}
	return s
}

// ValidateSupplyLinkFields validates that a fragment has valid SupplyLink fields.
// This is a more detailed validation than the general Validate() method.
func ValidateSupplyLinkFields(f *HorizonFragment) error {
	if f == nil {
		return fmt.Errorf("fragment is nil")
	}
	if !f.IsSupplyLinkFragment() {
		return fmt.Errorf("fragment must be a supply-link site (Class=site, Subtype=supply_link, or /supply-link/ canonical path)")
	}
	// SupplyLink can be tenant-scoped OR global-scoped (global = assumption layer).
	if f.TenantID == "" {
		return fmt.Errorf("tenantID must be set for SUPPLY_LINK")
	}
	if f.ProductRef == nil || strings.TrimSpace(*f.ProductRef) == "" {
		return fmt.Errorf("productRef is required for SUPPLY_LINK")
	}
	// Validate productRef is a valid canonical path
	if !strings.HasPrefix(*f.ProductRef, "/product/") {
		return fmt.Errorf("productRef must be a product canonical path (starts with /product/)")
	}
	// SupplierRef: /fac/, /deposit/, /org/, or /gov/ (full chain org|gov→fac→product→fac→org|gov)
	if f.SupplierRef != nil && strings.TrimSpace(*f.SupplierRef) != "" {
		p := strings.TrimSpace(*f.SupplierRef)
		if !strings.HasPrefix(p, "/fac/") && !strings.HasPrefix(p, "/deposit/") && !strings.HasPrefix(p, "/org/") && !strings.HasPrefix(p, "/gov/") {
			return fmt.Errorf("supplierRef must be facility, deposit, org, or gov canonical path (starts with /fac/, /deposit/, /org/, or /gov/)")
		}
	}
	// CustomerRef: /fac/, /org/, or /gov/ (full chain org|gov→fac→product→fac→org|gov)
	if f.CustomerRef != nil && strings.TrimSpace(*f.CustomerRef) != "" {
		p := strings.TrimSpace(*f.CustomerRef)
		if !strings.HasPrefix(p, "/fac/") && !strings.HasPrefix(p, "/org/") && !strings.HasPrefix(p, "/gov/") {
			return fmt.Errorf("customerRef must be facility, org, or gov canonical path (starts with /fac/, /org/, or /gov/)")
		}
	}
	// Validate facility refs if present
	if f.SupplierFacilityRef != nil && strings.TrimSpace(*f.SupplierFacilityRef) != "" {
		if !strings.HasPrefix(*f.SupplierFacilityRef, "/fac/") {
			return fmt.Errorf("supplierFacilityRef must be a facility canonical path (starts with /fac/)")
		}
	}
	if f.CustomerFacilityRef != nil && strings.TrimSpace(*f.CustomerFacilityRef) != "" {
		if !strings.HasPrefix(*f.CustomerFacilityRef, "/fac/") {
			return fmt.Errorf("customerFacilityRef must be a facility canonical path (starts with /fac/)")
		}
	}
	// Validate transportViaRefs if present (ports, terminals, pipelines, warehouses)
	for _, p := range f.TransportViaRefs {
		p = strings.TrimSpace(p)
		if p != "" && !strings.HasPrefix(p, "/fac/") {
			return fmt.Errorf("transportViaRefs entries must be facility canonical paths (start with /fac/)")
		}
	}
	// At least one of SupplierRef or CustomerRef must be set
	if (f.SupplierRef == nil || strings.TrimSpace(*f.SupplierRef) == "") &&
		(f.CustomerRef == nil || strings.TrimSpace(*f.CustomerRef) == "") {
		return fmt.Errorf("at least one of supplierRef or customerRef must be set for SUPPLY_LINK")
	}
	return nil
}

// IsSupplyLink reports site/subtype supply_link fragments (or /supply-link/ paths).
func IsSupplyLink(f *HorizonFragment) bool {
	return f.IsSupplyLinkFragment()
}

// GetSupplyLinkKey returns a stable key for a SupplyLink (for deduplication).
// Format: {tenantID}:{supplierRef}:{productRef}:{customerRef}
func GetSupplyLinkKey(f *HorizonFragment) string {
	if !IsSupplyLink(f) {
		return ""
	}
	supplier := "natural"
	if f.SupplierRef != nil {
		supplier = *f.SupplierRef
	}
	customer := "end-consumer"
	if f.CustomerRef != nil {
		customer = *f.CustomerRef
	}
	product := ""
	if f.ProductRef != nil {
		product = *f.ProductRef
	}
	return fmt.Sprintf("%s:%s:%s:%s", f.TenantID, supplier, product, customer)
}
