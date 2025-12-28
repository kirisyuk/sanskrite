package scout

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
)

var reCountryAlpha2 = regexp.MustCompile(`^[a-z]{2}$`)

// =============================================================================
// PRE-CREATE VALIDATION POLICY
// =============================================================================
// This policy defines validation rules that must pass before a fragment is created.
// Violations either block creation or trigger corrective actions (deduplication).

// PreCreateValidator validates fragments before creation and applies corrective policies.
type PreCreateValidator struct {
	geoBuilder      *GeoHierarchyBuilder
	orgDedup        *OrgDeduplicator
	productDedup    *ProductDeduplicator
	validationPolicy *policy.PolicyExecutor // Optional: unified validation policy
}

// NewPreCreateValidator creates a new pre-create validator.
func NewPreCreateValidator(geoBuilder *GeoHierarchyBuilder, orgDedup *OrgDeduplicator, productDedup *ProductDeduplicator) *PreCreateValidator {
	return &PreCreateValidator{
		geoBuilder: geoBuilder,
		orgDedup:   orgDedup,
		productDedup: productDedup,
	}
}

// WithValidationPolicy sets the validation policy executor.
func (v *PreCreateValidator) WithValidationPolicy(executor *policy.PolicyExecutor) *PreCreateValidator {
	v.validationPolicy = executor
	return v
}

// ValidationResult contains the result of pre-create validation.
type ValidationResult struct {
	IsValid         bool
	ReplacementFrag *models.HorizonFragment // Non-nil if should use existing fragment instead
	Warnings        []string
	BlockReason     string // Non-empty if creation should be blocked
}

// ValidateAndProcess validates a fragment and applies corrective policies.
// Returns:
// - ValidationResult with IsValid=true if creation should proceed
// - ReplacementFrag if an existing duplicate should be used instead
// - BlockReason if creation should be blocked entirely
func (v *PreCreateValidator) ValidateAndProcess(ctx context.Context, fragment *models.HorizonFragment) (*ValidationResult, error) {
	logPrefix := "[PreCreateValidator]"
	
	if v == nil {
		return &ValidationResult{IsValid: true}, nil
	}
	if fragment == nil {
		log.Printf("%s ERROR: fragment is nil", logPrefix)
		return &ValidationResult{IsValid: false, BlockReason: "nil fragment"}, nil
	}

	log.Printf("%s Validating fragment: id=%s, type=%s, canonicalPath=%s", 
		logPrefix, fragment.ID, fragment.Type, fragment.CanonicalPath)

	result := &ValidationResult{IsValid: true}

	// Use unified ValidationPolicy if available
	if v.validationPolicy != nil {
		log.Printf("%s Using unified ValidationPolicy", logPrefix)
		decision, err := v.validationPolicy.Execute(ctx, policy.PolicyInput{
			Kind:     "fragment",
			Context:  map[string]interface{}{"validationKind": "fragment"},
			Fragment: fragment,
		})

		if err != nil {
			log.Printf("%s ERROR: ValidationPolicy failed: %v", logPrefix, err)
			// Fall through to hardcoded validation
		} else {
			log.Printf("%s ValidationPolicy decision: action=%s, path=%s, confidence=%.2f", 
				logPrefix, decision.Action, decision.DecisionPath, decision.Confidence)

			switch decision.Action {
			case "reject":
				// Don't block - treat as warning instead
				// ValidationPolicy "reject" is advisory, not a blocker
				reason, _ := decision.Params["reason"].(string)
				log.Printf("%s WARNING: ValidationPolicy would reject fragment (non-blocking): %s", logPrefix, reason)
				result.Warnings = append(result.Warnings, fmt.Sprintf("ValidationPolicy warning: %s", reason))
				// Continue validation - don't block creation
			case "penalty":
				penalty, _ := decision.Params["penalty"].(float64)
				reason, _ := decision.Params["reason"].(string)
				log.Printf("%s PENALTY by ValidationPolicy: %.2f, reason: %s", logPrefix, penalty, reason)
				result.Warnings = append(result.Warnings, reason)
				// Continue with validation, but log penalty
			case "allow":
				log.Printf("%s ALLOWED by ValidationPolicy", logPrefix)
				// Continue with additional hardcoded checks for compatibility
			}
		}
	}

	// ==========================================================================
	// Policy 1: Block /discovered/ paths
	// ==========================================================================
	// The /discovered/ prefix indicates unverified, auto-generated paths that
	// should be canonicalized before creation.
	if strings.HasPrefix(fragment.CanonicalPath, "/discovered/") {
		result.BlockReason = "canonical path uses /discovered/ prefix - must be canonicalized first"
		result.IsValid = false
		return result, nil
	}

	// ==========================================================================
	// Policy 2: Enforce canonical path prefixes by type
	// ==========================================================================
	// Each fragment type should use its designated path prefix.
	prefix := v.expectedPathPrefix(fragment.Type)
	if prefix != "" && !strings.HasPrefix(fragment.CanonicalPath, prefix) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("canonical path %q does not start with expected prefix %q for type %s",
				fragment.CanonicalPath, prefix, fragment.Type))
	}

	// ==========================================================================
	// Policy 3: Warn about flat facility paths
	// ==========================================================================
	// Facilities should be nested under their parent organization:
	// Good: /fac/tesla/gigafactory-texas
	// Bad:  /fac/gigafactory-texas
	if fragment.Type == models.FragmentTypeFacility {
		pathParts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(pathParts) == 2 && pathParts[0] == "fac" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("flat facility path %q - consider nesting under parent org: /fac/{org}/%s",
					fragment.CanonicalPath, pathParts[1]))
		}
	}

	// ==========================================================================
	// Policy 4: GEO_REGION must use /loc/ prefix
	// ==========================================================================
	// Legacy /region/ paths should be migrated to /loc/.
	// Note: GEO_REGION validation issues are warnings, not blockers (can be fixed post-factum)
	if fragment.Type == models.FragmentTypeGeoRegion {
		if strings.HasPrefix(fragment.CanonicalPath, "/region/") {
			// NOTE: legacy /region/ had multiple shapes (including /region/na/{cc}/{admin1}/{city}).
			// Do NOT do a naive prefix replace; normalize into strict /loc/{cc}/{admin1}/{city}.
			if norm := normalizeLegacyGeoPath(fragment.CanonicalPath); norm != "" {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("legacy /region/ path %q normalized to %q", fragment.CanonicalPath, norm))
				fragment.CanonicalPath = norm
			} else {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("legacy GEO_REGION canonicalPath %q cannot be normalized to /loc/{country}/{admin1}/{city} - will be fixed post-factum", fragment.CanonicalPath))
			}
		}

		if !strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("GEO_REGION canonicalPath %q should use /loc/ prefix - will be fixed post-factum", fragment.CanonicalPath))
		} else {
			// Enforce strict 3-position geo model: /loc/{country}/{admin1}/{city}
			parts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
			if len(parts) == 2 && parts[0] == "loc" {
				// Non-canonical path like /loc/uae or /loc/saudi-arabia
				// Try to normalize to canonical /loc/{cc}/na/na
				// CAUTION: Avoid false positives like /loc/georgia (US state) vs /loc/ge/na/na (country)
				countrySlug := strings.ReplaceAll(parts[1], "-", " ")
				countrySlug = strings.ReplaceAll(countrySlug, "_", " ")
				canonicalPath := GetCountryCanonicalPath(countrySlug)
				if canonicalPath != "" && canonicalPath != fragment.CanonicalPath {
					// SAFETY: Only deduplicate if fragment is "empty" or label matches a country
					fragLabel := strings.ToLower(strings.TrimSpace(fragment.Labels["en"]))
					fragLegal := strings.ToLower(strings.TrimSpace(fragment.Identifiers["legalName"]))
					isEmpty := fragLabel == "" && fragLegal == ""
					labelMatchesCountry := IsKnownCountryName(fragLabel) || IsKnownCountryName(fragLegal)
					labelIsSlug := fragLabel == strings.ToLower(parts[1]) || fragLabel == countrySlug

					if isEmpty || labelMatchesCountry || labelIsSlug {
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("non-canonical GEO path %q → %q", fragment.CanonicalPath, canonicalPath))
						// Normalize the fragment path in-place so creation uses the strict /loc/{cc}/na/na form.
						fragment.CanonicalPath = canonicalPath
						// Try to find existing fragment at canonical path and deduplicate
						if v.geoBuilder != nil && v.geoBuilder.fragmentsRepo != nil {
							existing, err := v.geoBuilder.fragmentsRepo.GetByCanonicalPath(context.Background(), canonicalPath)
							if err == nil && existing != nil && existing.ID != fragment.ID {
								result.ReplacementFrag = existing
								result.Warnings = append(result.Warnings,
									fmt.Sprintf("found existing GEO_REGION %q (%s) - using instead", existing.CanonicalPath, existing.ID))
							}
						}
					} else {
						// Label doesn't match country — might be US state Georgia, etc.
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("GEO_REGION %q has label %q which doesn't match known country - skipping auto-merge", fragment.CanonicalPath, fragLabel))
					}
				} else {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("GEO_REGION canonicalPath %q should be /loc/{country}/{admin1}/{city} - will be fixed post-factum", fragment.CanonicalPath))
				}
			} else if len(parts) != 4 || parts[0] != "loc" {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("GEO_REGION canonicalPath %q should be /loc/{country}/{admin1}/{city} - will be fixed post-factum", fragment.CanonicalPath))
			} else if !reCountryAlpha2.MatchString(parts[1]) {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("GEO_REGION canonicalPath %q has invalid country code %q (expected ISO3166-1 alpha2 lowercase) - will be fixed post-factum", fragment.CanonicalPath, parts[1]))
			} else {
				log.Printf("%s GEO_REGION validation passed: %s", logPrefix, fragment.CanonicalPath)
			}
		}
	}

	// ==========================================================================
	// Policy 5: Organization deduplication
	// ==========================================================================
	// Check if this organization already exists (by Wikidata ID or shortName).
	if fragment.Type == models.FragmentTypeOrganization && v.orgDedup != nil {
		existing, err := v.orgDedup.DeduplicateOrg(ctx, fragment)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			result.ReplacementFrag = existing
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("found existing organization %q (%s) - using instead of creating new",
					existing.CanonicalPath, existing.ID))
		}
	}

	// ==========================================================================
	// Policy 6: Product deduplication
	// ==========================================================================
	// Check if this product already exists (by Wikidata ID, CAS number, or chemical formula).
	if fragment.Type == models.FragmentTypeProduct && v.productDedup != nil {
		existing, err := v.productDedup.DeduplicateProduct(ctx, fragment)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			result.ReplacementFrag = existing
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("found existing product %q (%s) - using instead of creating new",
					existing.CanonicalPath, existing.ID))
		}
	}

	// ==========================================================================
	// Policy 7: Required fields validation
	// ==========================================================================
	if strings.TrimSpace(fragment.CanonicalPath) == "" {
		result.BlockReason = "canonical path is required"
		result.IsValid = false
		return result, nil
	}

	if fragment.Type == "" {
		result.BlockReason = "fragment type is required"
		result.IsValid = false
		return result, nil
	}

	return result, nil
}

// expectedPathPrefix returns the expected canonical path prefix for a fragment type.
func (v *PreCreateValidator) expectedPathPrefix(fragType models.FragmentType) string {
	switch fragType {
	case models.FragmentTypeOrganization:
		return "/org/"
	case models.FragmentTypeFacility:
		return "/fac/"
	case models.FragmentTypeGeoRegion:
		return "/loc/"
	case models.FragmentTypeProduct:
		return "/product/"
	case models.FragmentTypePerson:
		return "/person/"
	default:
		return ""
	}
}

// =============================================================================
// CANONICAL PATH POLICIES
// =============================================================================
// These policies define how canonical paths should be structured.

// CanonicalPathPolicy describes the expected format for a fragment type.
type CanonicalPathPolicy struct {
	Prefix      string
	Example     string
	Description string
}

// CanonicalPathPolicies defines the expected path format for each fragment type.
var CanonicalPathPolicies = map[models.FragmentType]CanonicalPathPolicy{
	models.FragmentTypeOrganization: {
		Prefix:      "/org/",
		Example:     "/org/tesla",
		Description: "Organization paths use lowercase hyphenated short names",
	},
	models.FragmentTypeFacility: {
		Prefix:      "/fac/",
		Example:     "/fac/tesla/gigafactory-texas",
		Description: "Facility paths are nested under parent organization",
	},
	models.FragmentTypeGeoRegion: {
		Prefix:      "/loc/",
		Example:     "/loc/us/tx/austin",
		Description: "Geographic paths use /loc/{country}/{admin1}/{city} format",
	},
	models.FragmentTypeProduct: {
		Prefix:      "/product/",
		Example:     "/product/tesla/model-3",
		Description: "Product paths are nested under manufacturer organization",
	},
	models.FragmentTypePerson: {
		Prefix:      "/person/",
		Example:     "/person/elon-musk",
		Description: "Person paths use lowercase hyphenated names",
	},
}

func (v *PreCreateValidator) String() string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("PreCreateValidator(geo=%v, orgDedup=%v)", v.geoBuilder != nil, v.orgDedup != nil)
}
