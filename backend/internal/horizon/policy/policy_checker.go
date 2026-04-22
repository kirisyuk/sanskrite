package policy

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// =============================================================================
// REPOSITORY INTERFACES (to avoid circular imports with repo package)
// =============================================================================

// FragmentsReader provides read access to HorizonFragments.
type FragmentsReader interface {
	Get(ctx context.Context, id string) (*horizonmodels.HorizonFragment, error)
}

// EdgesReader provides read access to DependencyEdges.
type EdgesReader interface {
	Get(ctx context.Context, id string) (*horizonmodels.DependencyEdge, error)
}

// DuplicateCheckResult is the structured response from LLM duplicate check.
type DuplicateCheckResult struct {
	IsDuplicate bool    `json:"isDuplicate"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

// PolicyChecker performs policy checks on job outputs.
// Combines LLM-based semantic checks (duplicates) with deterministic rule checks.
type PolicyChecker struct {
	fragmentsRepo  FragmentsReader
	edgesRepo      EdgesReader
	vertexSearch   *ai.VertexAISearchClient
	aiExecutor     *ai.AITaskExecutor
	validationExec *PolicyExecutor
	geonamesClient ontology.GeoNamesClientInterface
	taxonomyRules  *TaxonomyRules

	// Configuration
	enableLLMChecks            bool
	duplicatePrefetchLimit     int
	confidenceThreshold        float64
	autoFixConfidenceThreshold float64
	jobContextLimit            int
	externalContextLimit       int
	maxDuplicateChecks         int
}

// PolicyCheckerOption is a functional option for PolicyChecker.
type PolicyCheckerOption func(*PolicyChecker)

// NewPolicyChecker creates a new PolicyChecker.
func NewPolicyChecker(
	fragmentsRepo FragmentsReader,
	edgesRepo EdgesReader,
	opts ...PolicyCheckerOption,
) *PolicyChecker {
	pc := &PolicyChecker{
		fragmentsRepo:              fragmentsRepo,
		edgesRepo:                  edgesRepo,
		taxonomyRules:              DefaultTaxonomyRules(),
		enableLLMChecks:            true,
		duplicatePrefetchLimit:     20,
		confidenceThreshold:        0.7,
		autoFixConfidenceThreshold: 0.9,
		jobContextLimit:            12,
		externalContextLimit:       8,
		maxDuplicateChecks:         120,
	}

	for _, opt := range opts {
		opt(pc)
	}

	return pc
}

// WithVertexSearch sets the Vertex AI Search client for duplicate prefetch.
func WithVertexSearch(client *ai.VertexAISearchClient) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		pc.vertexSearch = client
	}
}

// WithAIExecutor sets the AI task executor for semantic checks.
func WithAIExecutor(executor *ai.AITaskExecutor) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		pc.aiExecutor = executor
	}
}

// WithValidationExecutor wires unified validation policy execution into checker runtime.
func WithValidationExecutor(executor *PolicyExecutor) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		pc.validationExec = executor
	}
}

// WithGeoNamesClient sets the GeoNames client for geo validation.
func WithGeoNamesClient(client ontology.GeoNamesClientInterface) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		pc.geonamesClient = client
	}
}

// WithTaxonomyRules sets custom taxonomy rules.
func WithTaxonomyRules(rules *TaxonomyRules) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		pc.taxonomyRules = rules
	}
}

// WithLLMChecksEnabled enables or disables LLM-based checks.
func WithLLMChecksEnabled(enabled bool) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		pc.enableLLMChecks = enabled
	}
}

// WithAutoFixConfidenceThreshold sets the confidence threshold for auto-fix actions.
func WithAutoFixConfidenceThreshold(threshold float64) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		if threshold > 0 && threshold <= 1 {
			pc.autoFixConfidenceThreshold = threshold
		}
	}
}

// WithJobContextLimit sets the maximum number of job-created fragments in prompt context.
func WithJobContextLimit(limit int) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		if limit > 0 {
			pc.jobContextLimit = limit
		}
	}
}

// WithExternalContextLimit sets the maximum number of external candidates in prompt context.
func WithExternalContextLimit(limit int) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		if limit > 0 {
			pc.externalContextLimit = limit
		}
	}
}

// WithMaxDuplicateChecks caps the number of LLM duplicate checks per job.
func WithMaxDuplicateChecks(limit int) PolicyCheckerOption {
	return func(pc *PolicyChecker) {
		if limit > 0 {
			pc.maxDuplicateChecks = limit
		}
	}
}

// CheckJobOutput performs policy checks on all fragments and edges created/modified in a job.
func (pc *PolicyChecker) CheckJobOutput(
	ctx context.Context,
	tenantID string,
	jobID string,
	createdFragmentIDs []string,
	modifiedFragmentIDs []string,
	createdEdgeIDs []string,
	modifiedEdgeIDs []string,
) (*PolicyCheckResult, error) {
	startTime := time.Now()

	result := &PolicyCheckResult{
		JobID:      jobID,
		TenantID:   tenantID,
		Violations: make([]PolicyViolation, 0),
		CheckedAt:  startTime,
	}

	// Collect all fragment IDs to check
	allFragmentIDs := make(map[string]bool)
	for _, id := range createdFragmentIDs {
		allFragmentIDs[id] = true
	}
	for _, id := range modifiedFragmentIDs {
		allFragmentIDs[id] = true
	}

	// Collect all edge IDs to check
	allEdgeIDs := make(map[string]bool)
	for _, id := range createdEdgeIDs {
		allEdgeIDs[id] = true
	}
	for _, id := range modifiedEdgeIDs {
		allEdgeIDs[id] = true
	}

	// Load fragments
	fragments := make([]*horizonmodels.HorizonFragment, 0, len(allFragmentIDs))
	for id := range allFragmentIDs {
		frag, err := pc.fragmentsRepo.Get(ctx, id)
		if err != nil {
			log.Printf("[PolicyChecker] WARNING: failed to load fragment %s: %v", id, err)
			continue
		}
		if frag != nil {
			fragments = append(fragments, frag)
		}
	}
	result.FragmentsChecked = len(fragments)

	// Load edges
	edges := make([]*horizonmodels.DependencyEdge, 0, len(allEdgeIDs))
	for id := range allEdgeIDs {
		edge, err := pc.edgesRepo.Get(ctx, id)
		if err != nil {
			log.Printf("[PolicyChecker] WARNING: failed to load edge %s: %v", id, err)
			continue
		}
		if edge != nil {
			edges = append(edges, edge)
		}
	}
	result.EdgesChecked = len(edges)

	log.Printf("[PolicyChecker] Checking %d fragments and %d edges for job %s",
		len(fragments), len(edges), jobID)

	// === DETERMINISTIC CHECKS ===

	// 1. Check fragment canonical paths
	for _, frag := range fragments {
		violations := pc.checkFragmentPath(ctx, frag)
		result.Violations = append(result.Violations, violations...)
	}

	// 2. Check edge directions
	for _, edge := range edges {
		violations := pc.checkEdge(ctx, tenantID, edge)
		result.Violations = append(result.Violations, violations...)
	}

	// === LLM CHECKS (if enabled) ===

	if pc.enableLLMChecks && pc.aiExecutor != nil {
		// 3. Check for duplicates using LLM + job context (+ optional Vertex prefetch)
		checkCount := 0
		for _, frag := range fragments {
			violations := pc.checkForDuplicatesWithContext(ctx, frag, fragments, &checkCount)
			result.Violations = append(result.Violations, violations...)
			if pc.maxDuplicateChecks > 0 && checkCount >= pc.maxDuplicateChecks {
				break
			}
		}
	}

	// Compute summary
	for _, v := range result.Violations {
		result.TotalViolations++
		switch v.Severity {
		case SeverityCritical:
			result.CriticalCount++
		case SeverityWarning:
			result.WarningCount++
		}
		if v.AutoFixable {
			result.AutoFixableCount++
		}
	}

	log.Printf("[PolicyChecker] Check complete for job %s: %d violations found (%d critical, %d warning, %d auto-fixable) in %v",
		jobID, result.TotalViolations, result.CriticalCount, result.WarningCount, result.AutoFixableCount,
		time.Since(startTime))

	return result, nil
}

// checkFragmentPath checks a fragment's canonical path against taxonomy rules.
func (pc *PolicyChecker) checkFragmentPath(ctx context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation

	path := strings.TrimSpace(frag.CanonicalPath)
	if path == "" {
		return violations
	}

	if shouldValidateCanonicalPath(path) {
		if err := taxonomy.ValidateCanonicalPath(path); err != nil {
			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationPathFormat,
				Severity:    SeverityWarning,
				FragmentID:  frag.ID,
				ActualValue: path,
				Rule:        "canonical_path",
				Description: err.Error(),
				Confidence:  1.0,
				AutoFixable: false,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}
			// Best-effort deterministic auto-fixes for common canonical path errors:
			// - normalize obvious ISO2 aliases (uae->ae, usa->us, uk->gb)
			// - normalize /loc depth without forced "na" padding
			if fixed := pc.bestEffortFixCanonicalPath(path); fixed != "" && fixed != path {
				if fixErr := taxonomy.ValidateCanonicalPath(fixed); fixErr == nil {
					v.ExpectedValue = fixed
					v.AutoFixable = true
					v.SuggestedFix = &EnforcementTask{
						Type:     TaskFixPath,
						TargetID: frag.ID,
						Params: map[string]interface{}{
							"currentPath": path,
							"correctPath": fixed,
							"reason":      "canonical_path_best_effort_normalization",
						},
					}
				}
			}
			violations = append(violations, v)
			return violations
		}
	}

	// Type/path mismatches happen in the wild (e.g., stale type with corrected canonicalPath).
	// Keep validating by path namespace to avoid emitting namespace-specific fixes for the wrong entity family.
	if !IsCanonicalPathAllowedForType(frag.Class, frag.Subtype, path) {
		expectedPrefix := ExpectedCanonicalPathPrefix(frag.Class, frag.Subtype)
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationTypeMismatch,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "fragment_type_path_namespace",
			Description: fmt.Sprintf("Fragment type %s/%s is incompatible with canonical path %q (expected prefix %q)", frag.Class, frag.Subtype, path, expectedPrefix),
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	switch {
	case strings.HasPrefix(path, "/loc/"):
		violations = append(violations, pc.checkGeoPath(ctx, frag)...)

	case strings.HasPrefix(path, "/gov/"):
		violations = append(violations, pc.checkGovPath(ctx, frag)...)

	case strings.HasPrefix(path, "/org/"):
		violations = append(violations, pc.checkOrgPath(ctx, frag)...)

	case strings.HasPrefix(path, "/product/"):
		violations = append(violations, pc.checkProductPath(ctx, frag)...)

	case strings.HasPrefix(path, "/person/"):
		violations = append(violations, pc.checkPersonPath(ctx, frag)...)

	case strings.HasPrefix(path, "/decision/"):
		violations = append(violations, pc.checkDecisionPointPath(ctx, frag)...)

	case strings.HasPrefix(path, "/mechanism/"):
		violations = append(violations, pc.checkMechanismPath(ctx, frag)...)
	}

	return violations
}

func shouldValidateCanonicalPath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/org/"),
		strings.HasPrefix(path, "/gov/"),
		strings.HasPrefix(path, "/community/"),
		strings.HasPrefix(path, "/person/"),
		strings.HasPrefix(path, "/product/"),
		strings.HasPrefix(path, "/project/"),
		strings.HasPrefix(path, "/domain/"),
		strings.HasPrefix(path, "/evidence/"),
		strings.HasPrefix(path, "/market/"),
		strings.HasPrefix(path, "/segment/"),
		strings.HasPrefix(path, "/fac/"),
		strings.HasPrefix(path, "/loc/"),
		strings.HasPrefix(path, "/sphere/"),
		strings.HasPrefix(path, "/media/"),
		strings.HasPrefix(path, "/deposit/"),
		strings.HasPrefix(path, "/decision/"),
		strings.HasPrefix(path, "/mechanism/"):
		return true
	default:
		return false
	}
}

// checkGeoPath validates a GEO_REGION canonical path.
func (pc *PolicyChecker) checkGeoPath(ctx context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := frag.CanonicalPath

	// Basic format validation
	valid, reason := pc.taxonomyRules.ValidateGeoPath(path)
	if !valid {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "geo_path_format",
			Description: reason,
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
		return violations
	}

	// Check admin1 code against GeoNames
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 4 {
		cc := parts[1]
		admin1 := parts[2]
		city := parts[3]

		// Skip if already using "na" for unknown
		if admin1 == "na" {
			return violations
		}

		// Check if admin1 looks like a city name instead of admin code
		// Admin codes are typically short (2-3 chars) or numeric
		if len(admin1) > 5 && !isNumeric(admin1) {
			// This looks like a city name in admin1 slot
			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationGeoNamesAdmin,
				Severity:    SeverityWarning,
				FragmentID:  frag.ID,
				ActualValue: path,
				Rule:        "geonames_admin_code",
				Description: fmt.Sprintf("Admin1 '%s' looks like a city name, not a GeoNames admin code", admin1),
				Confidence:  0.8,
				AutoFixable: false,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}

			// Try to resolve correct path via GeoNames
			if pc.geonamesClient != nil {
				correctPath := pc.resolveGeoNamesPath(ctx, cc, admin1, city, frag)
				if correctPath != "" && correctPath != path {
					v.ExpectedValue = correctPath
					v.SuggestedFix = &EnforcementTask{
						Type:     TaskResolveGeoNames,
						TargetID: frag.ID,
						Params: map[string]interface{}{
							"currentPath": path,
							"correctPath": correctPath,
						},
					}
					v.AutoFixable = true
				}
			}

			violations = append(violations, v)
		}
	}

	return violations
}

// resolveGeoNamesPath attempts to resolve the correct GeoNames-based path.
func (pc *PolicyChecker) resolveGeoNamesPath(ctx context.Context, cc, admin1, city string, _ *horizonmodels.HorizonFragment) string {
	if pc.geonamesClient == nil {
		return ""
	}

	// Search for the city in GeoNames
	query := city
	if admin1 != "na" && admin1 != "" {
		query = city + " " + admin1
	}

	resp, err := pc.geonamesClient.Search(ctx, query, 5, ontology.GeoNamesSearchOptions{
		Country:      cc,
		FeatureClass: "P", // Populated place
	})
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return ""
	}

	// Find best match
	for _, place := range resp.Results {
		if strings.EqualFold(place.CountryCode, cc) {
			adminCode := strings.ToLower(strings.TrimSpace(place.AdminCode1))
			if adminCode == "" {
				adminCode = "na"
			}
			citySlug := slugify(place.Name)
			return fmt.Sprintf("/loc/%s/%s/%s", strings.ToLower(cc), adminCode, citySlug)
		}
	}

	return ""
}

// checkOrgPath validates an ORGANIZATION canonical path.
func (pc *PolicyChecker) checkOrgPath(_ context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := strings.TrimSpace(frag.CanonicalPath)
	if !strings.HasPrefix(path, "/org/") {
		return violations
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Check ISO2 validity
	if len(parts) >= 2 {
		iso2 := parts[1]
		// "/org/na/..." is allowed but means unknown jurisdiction/geo.
		// We treat it as a warning with an LLM-based auto-fix (infer ISO2 from fragment context).
		if iso2 == "na" && len(parts) >= 3 && parts[0] == "org" {
			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationGeoUnknown,
				Severity:    SeverityWarning,
				FragmentID:  frag.ID,
				ActualValue: path,
				Rule:        "org_geo_unknown",
				Description: "Organization canonical path uses geo token 'na' (unknown jurisdiction). Try to infer ISO3166-1 alpha-2 from fragment context (labels/identifiers/description/homeLocation and nearby GEO edges like LOCATED_IN / OPERATES_IN).",
				Confidence:  0.8,
				AutoFixable: true,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}
			v.SuggestedFix = &EnforcementTask{
				Type:     TaskFixISO2,
				TargetID: frag.ID,
				Params: map[string]interface{}{
					"currentPath": path,
					"namespace":   "org",
				},
			}
			violations = append(violations, v)
		}
		// "int" is special for international orgs. Regional tokens (eu, asean, brics, gcc) are allowed.
		if !taxonomy.IsValidGeoForOrgGovFac(iso2) {
			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationInvalidISO2,
				Severity:    SeverityWarning,
				FragmentID:  frag.ID,
				ActualValue: path,
				Rule:        "org_iso2",
				Description: fmt.Sprintf("Invalid ISO2 country code: %s", iso2),
				Confidence:  1.0,
				AutoFixable: false,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}
			// Best-effort: normalize common ISO2 aliases for org/gov paths (deterministic).
			if norm := normalizeISO2Alias(iso2); norm != "" && norm != iso2 {
				parts[1] = norm
				fixed := "/" + strings.Join(parts, "/")
				if fixErr := taxonomy.ValidateCanonicalPath(fixed); fixErr == nil {
					v.ExpectedValue = fixed
					v.AutoFixable = true
					v.SuggestedFix = &EnforcementTask{
						Type:     TaskFixPath,
						TargetID: frag.ID,
						Params: map[string]interface{}{
							"currentPath": path,
							"correctPath": fixed,
							"reason":      "normalize_iso2_alias",
						},
					}
				}
			}
			violations = append(violations, v)
		}
	}

	return violations
}

// checkGovPath validates a GOVERNMENT canonical path.
func (pc *PolicyChecker) checkGovPath(_ context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := strings.TrimSpace(frag.CanonicalPath)
	if !strings.HasPrefix(path, "/gov/") {
		return violations
	}

	valid, reason := pc.taxonomyRules.ValidateGovPath(path)
	if !valid {
		v := PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "gov_path_format",
			Description: reason,
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		}
		// Best-effort: normalize ISO2 aliases in /gov/{iso2}/...
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 3 && parts[0] == "gov" {
			if norm := normalizeISO2Alias(parts[1]); norm != "" && norm != parts[1] {
				parts[1] = norm
				fixed := "/" + strings.Join(parts, "/")
				if fixErr := taxonomy.ValidateCanonicalPath(fixed); fixErr == nil {
					v.ExpectedValue = fixed
					v.AutoFixable = true
					v.SuggestedFix = &EnforcementTask{
						Type:     TaskFixPath,
						TargetID: frag.ID,
						Params: map[string]interface{}{
							"currentPath": path,
							"correctPath": fixed,
							"reason":      "normalize_iso2_alias",
						},
					}
				}
			}
		}
		violations = append(violations, v)
	}

	// "/gov/na/..." is allowed but means unknown jurisdiction/geo.
	// We treat it as a warning with an LLM-based auto-fix (infer ISO2 from fragment context).
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "gov" {
		iso2 := parts[1]
		if iso2 == "na" {
			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationGeoUnknown,
				Severity:    SeverityWarning,
				FragmentID:  frag.ID,
				ActualValue: path,
				Rule:        "gov_geo_unknown",
				Description: "Government canonical path uses geo token 'na' (unknown jurisdiction). Try to infer ISO3166-1 alpha-2 or " + taxonomy.CompositeGeoDescriptionForPrompts() + " (for intergovernmental bodies) from fragment context and nearby GEO edges.",
				Confidence:  0.8,
				AutoFixable: true,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}
			v.SuggestedFix = &EnforcementTask{
				Type:     TaskFixISO2,
				TargetID: frag.ID,
				Params: map[string]interface{}{
					"currentPath": path,
					"namespace":   "gov",
				},
			}
			violations = append(violations, v)
		}
	}

	return violations
}

// checkProductPath validates a PRODUCT canonical path.
func (pc *PolicyChecker) checkProductPath(_ context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := frag.CanonicalPath

	valid, reason := pc.taxonomyRules.ValidateProductPath(path)
	if !valid {
		v := PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "product_path_format",
			Description: reason,
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		}
		// Best-effort deterministic fix if product is known in code-defined taxonomy:
		// Map /product/{...}/{slug} or /product/{slug} -> /product/{category}/{type}/{stage}/{slug}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "product" {
			slug := strings.TrimSpace(parts[len(parts)-1])
			if slug != "" {
				if spec := taxonomy.GetProductSpec(slug); spec != nil {
					fixed := fmt.Sprintf("/product/%s/%s/%s/%s", spec.Category, spec.Type, spec.Stage, slug)
					if fixed != path {
						if fixErr := taxonomy.ValidateCanonicalPath(fixed); fixErr == nil {
							v.ExpectedValue = fixed
							v.AutoFixable = true
							v.SuggestedFix = &EnforcementTask{
								Type:     TaskFixPath,
								TargetID: frag.ID,
								Params: map[string]interface{}{
									"currentPath": path,
									"correctPath": fixed,
									"reason":      "known_product_spec",
								},
							}
						}
					}
				}
			}
		}
		violations = append(violations, v)
	}

	return violations
}

// bestEffortFixCanonicalPath performs small deterministic normalizations for common bad paths.
// It is intentionally conservative: it only fixes what we can safely infer without LLM.
func (pc *PolicyChecker) bestEffortFixCanonicalPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	// Normalize separators and lowercase for path segments (taxonomy requires lowercase + '-' slugs).
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	for i := range parts {
		seg := strings.ToLower(strings.TrimSpace(parts[i]))
		seg = strings.ReplaceAll(seg, "_", "-")
		for strings.Contains(seg, "--") {
			seg = strings.ReplaceAll(seg, "--", "-")
		}
		parts[i] = seg
	}

	switch parts[0] {
	case "org", "gov":
		// normalize iso2 aliases (uae->ae, usa->us, uk->gb)
		if len(parts) >= 2 {
			if norm := normalizeISO2Alias(parts[1]); norm != "" {
				parts[1] = norm
			}
		}
		return "/" + strings.Join(parts, "/")

	case "loc":
		if len(parts) >= 2 {
			if norm := normalizeISO2Alias(parts[1]); norm != "" {
				parts[1] = norm
			}
		}
		level1 := ""
		level2 := ""
		if len(parts) >= 3 {
			level1 = parts[2]
		}
		if len(parts) >= 4 {
			level2 = parts[3]
		}
		if fixed := taxonomy.GeoCanonicalPath(parts[1], level1, level2); fixed != "" {
			return fixed
		}
		return "/" + strings.Join(parts, "/")

	case "segment":
		// segment geo is last segment (often user passes uae/usa)
		if len(parts) >= 2 {
			last := parts[len(parts)-1]
			if norm := normalizeISO2Alias(last); norm != "" {
				parts[len(parts)-1] = norm
			}
		}
		return "/" + strings.Join(parts, "/")
	}
	return "/" + strings.Join(parts, "/")
}

// normalizeISO2Alias maps common non-ISO2 geo tokens into ISO3166-1 alpha2.
// This is intentionally small and deterministic (no org/product hardcoding).
func normalizeISO2Alias(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	s = strings.Trim(s, " .,_-")
	switch s {
	case "uae":
		return "ae"
	case "usa":
		return "us"
	case "uk":
		return "gb"
	default:
		return ""
	}
}

// checkPersonPath validates a PERSON canonical path.
func (pc *PolicyChecker) checkPersonPath(_ context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := frag.CanonicalPath

	valid, reason := pc.taxonomyRules.ValidatePersonPath(path)
	if !valid {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "person_path_format",
			Description: reason,
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	return violations
}

// checkDecisionPointPath validates a DECISION_POINT canonical path.
// Format: /decision/{domain-slug}/{decision-slug}
func (pc *PolicyChecker) checkDecisionPointPath(_ context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := frag.CanonicalPath

	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /decision/{domain-slug}/{decision-slug} = 3 parts
	if len(parts) != 3 || parts[0] != "decision" {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityCritical,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "decision_point_path_format",
			Description: fmt.Sprintf("DECISION_POINT path must be /decision/{domain-slug}/{decision-slug}, got: %s", path),
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
		return violations
	}

	domainSlug := parts[1]
	decisionSlug := parts[2]

	// Validate slugs are lowercase kebab-case
	if domainSlug != strings.ToLower(domainSlug) || strings.Contains(domainSlug, " ") {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "decision_point_domain_slug",
			Description: fmt.Sprintf("Domain slug must be lowercase kebab-case: %s", domainSlug),
			Confidence:  1.0,
			AutoFixable: true,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	if decisionSlug != strings.ToLower(decisionSlug) || strings.Contains(decisionSlug, " ") {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "decision_point_decision_slug",
			Description: fmt.Sprintf("Decision slug must be lowercase kebab-case: %s", decisionSlug),
			Confidence:  1.0,
			AutoFixable: true,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	// Check that DecisionPoint has a title and description
	if strings.TrimSpace(frag.Labels["en"]) == "" {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationMissingField,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: "",
			Rule:        "decision_point_title",
			Description: "DECISION_POINT must have a title in labels.en",
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	if strings.TrimSpace(frag.Description) == "" {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationMissingField,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: "",
			Rule:        "decision_point_description",
			Description: "DECISION_POINT should have a description explaining what it controls",
			Confidence:  0.9,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	return violations
}

// checkMechanismPath validates a MECHANISM canonical path.
// Format: /mechanism/{category}/{slug}
func (pc *PolicyChecker) checkMechanismPath(_ context.Context, frag *horizonmodels.HorizonFragment) []PolicyViolation {
	var violations []PolicyViolation
	path := frag.CanonicalPath

	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /mechanism/{category}/{slug} = 3 parts
	if len(parts) != 3 || parts[0] != "mechanism" {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityCritical,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "mechanism_path_format",
			Description: fmt.Sprintf("MECHANISM path must be /mechanism/{category}/{slug}, got: %s", path),
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
		return violations
	}

	category := parts[1]
	slug := parts[2]

	// Validate category is one of the allowed values
	if !IsValidMechanismCategory(category) {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityCritical,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "mechanism_category",
			Description: fmt.Sprintf("MECHANISM category must be one of: regulatory, economic, technological, contractual, political. Got: %s", category),
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	// Validate slug is lowercase kebab-case
	if slug != strings.ToLower(slug) || strings.Contains(slug, " ") {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationPathFormat,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: path,
			Rule:        "mechanism_slug",
			Description: fmt.Sprintf("Mechanism slug must be lowercase kebab-case: %s", slug),
			Confidence:  1.0,
			AutoFixable: true,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	// Check that Mechanism has a title
	if strings.TrimSpace(frag.Labels["en"]) == "" {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationMissingField,
			Severity:    SeverityWarning,
			FragmentID:  frag.ID,
			ActualValue: "",
			Rule:        "mechanism_title",
			Description: "MECHANISM must have a title in labels.en",
			Confidence:  1.0,
			AutoFixable: false,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		})
	}

	return violations
}

// checkEdge validates an edge against direction and type rules.
func (pc *PolicyChecker) checkEdge(ctx context.Context, _ string, edge *horizonmodels.DependencyEdge) []PolicyViolation {
	var violations []PolicyViolation

	// Check for self-loop
	if IsSelfLoopIDs(edge.SourceFragmentID, edge.TargetFragmentID) {
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationSelfLoop,
			Severity:    SeverityCritical,
			EdgeID:      edge.ID,
			ActualValue: fmt.Sprintf("%s -> %s", edge.SourceFragmentID, edge.TargetFragmentID),
			Rule:        "no_self_loops",
			Description: "Edge points to itself (self-loop)",
			Confidence:  1.0,
			AutoFixable: true,
			SuggestedFix: &EnforcementTask{
				Type:     TaskDeleteEdge,
				TargetID: edge.ID,
				Params: map[string]interface{}{
					"reason": "self_loop",
				},
			},
			DetectedAt: time.Now(),
			DetectedBy: "deterministic",
		})
		return violations // No point checking other rules for self-loops
	}

	// Get endpoint fragments
	sourceFrag, targetFrag, err := pc.getEdgeEndpoints(ctx, edge)
	if err != nil {
		// Orphaned edge
		violations = append(violations, PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationOrphanedEdge,
			Severity:    SeverityCritical,
			EdgeID:      edge.ID,
			ActualValue: fmt.Sprintf("%s -> %s [%s]", edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation),
			Rule:        "no_orphaned_edges",
			Description: fmt.Sprintf("Edge has missing endpoint: %v", err),
			Confidence:  1.0,
			AutoFixable: true,
			SuggestedFix: &EnforcementTask{
				Type:     TaskDeleteEdge,
				TargetID: edge.ID,
				Params: map[string]interface{}{
					"reason": "orphaned",
				},
			},
			DetectedAt: time.Now(),
			DetectedBy: "deterministic",
		})
		return violations
	}
	sourceType := sourceFrag.Class
	targetType := targetFrag.Class
	sourcePath := sourceFrag.CanonicalPath
	targetPath := targetFrag.CanonicalPath

	// If the edge was already validated during materialization, skip redundant re-validation.
	_, alreadyValidated := edge.Qualifiers["pipelineValidated"]
	if !alreadyValidated {
		useUnified := policyUnifiedContractEnabled() && pc.validationExec != nil
		shadow := policyUnifiedShadowModeEnabled() && pc.validationExec != nil
		if useUnified || shadow {
			unifiedViolations, decision, err := pc.checkEdgeDirectionWithValidationPolicy(ctx, edge, sourceType, targetType, sourcePath, targetPath)
			if err != nil {
				LogPolicyEvent("policy_checker_policy", PolicyLogStageError, map[string]interface{}{
					"edge_id":     edge.ID,
					"action":      "validation_executor_error",
					"error_class": "policy_execution_error",
					"error":       err,
				})
			} else if useUnified {
				violations = append(violations, unifiedViolations...)
				if shadow {
					legacy := pc.checkEdgeDirectionLegacy(edge, sourceType, targetType)
					pc.logEdgeDirectionShadowDiff(edge, decision, unifiedViolations, legacy)
				}
			} else {
				legacy := pc.checkEdgeDirectionLegacy(edge, sourceType, targetType)
				violations = append(violations, legacy...)
				pc.logEdgeDirectionShadowDiff(edge, decision, unifiedViolations, legacy)
			}
		} else {
			violations = append(violations, pc.checkEdgeDirectionLegacy(edge, sourceType, targetType)...)
		}
	}

	// If the edge was already canonicalized during materialization, skip redundant re-canonicalization.
	_, alreadyCanonicalized := edge.Qualifiers["pipelineCanonicalized"]
	if !alreadyCanonicalized {
		reason, _ := edge.Qualifiers["reason"].(string)
		canon := ApplyEdgeCanonicalizationPolicy(EdgeCanonicalizationInput{
			EdgeType:        edge.Relation,
			SourceType:      sourceType,
			TargetType:      targetType,
			SourcePath:      sourcePath,
			TargetPath:      targetPath,
			RawRelationType: string(edge.Relation),
			Reason:          strings.TrimSpace(reason),
			Qualifiers:      edge.Qualifiers,
		})
		if canon.Changed {
			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationTypeMismatch,
				Severity:    SeverityWarning,
				EdgeID:      edge.ID,
				ActualValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]", edge.SourceFragmentID, sourceType, edge.TargetFragmentID, targetType, edge.Relation),
				Rule:        "edge_canonicalization",
				Description: canon.Reason,
				Confidence:  0.9,
				AutoFixable: true,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}

			v.ExpectedValue = string(canon.EdgeType)
			v.SuggestedFix = &EnforcementTask{
				Type:     TaskCanonicalizeEdge,
				TargetID: edge.ID,
				Params: map[string]interface{}{
					"newType":    string(canon.EdgeType),
					"shouldSwap": canon.ShouldSwap,
				},
			}

			violations = append(violations, v)
		}
	}

	// Check symmetric edge normalization
	if pc.taxonomyRules.IsSymmetricEdge(edge.Relation) {
		normalizedSource, normalizedTarget, _, swapped := NormalizeSymmetricEdge(
			edge.SourceFragmentID, edge.TargetFragmentID, edge.Relation)
		if swapped {
			violations = append(violations, PolicyViolation{
				ID:            uuid.New().String(),
				Type:          ViolationSymmetricEdge,
				Severity:      SeverityInfo,
				EdgeID:        edge.ID,
				ActualValue:   fmt.Sprintf("%s -> %s", edge.SourceFragmentID, edge.TargetFragmentID),
				ExpectedValue: fmt.Sprintf("%s -> %s", normalizedSource, normalizedTarget),
				Rule:          "symmetric_edge_normalization",
				Description:   fmt.Sprintf("Symmetric edge %s should have lexicographically ordered endpoints", edge.Relation),
				Confidence:    1.0,
				AutoFixable:   true,
				SuggestedFix: &EnforcementTask{
					Type:     TaskNormalizeSymmetric,
					TargetID: edge.ID,
				},
				DetectedAt: time.Now(),
				DetectedBy: "deterministic",
			})
		}
	}

	// Check strategic edge qualifiers
	violations = append(violations, pc.checkStrategicEdgeQualifiers(edge)...)
	// Soft completeness checks for business-critical edges (best-effort; no hard reject).
	violations = append(violations, pc.checkEdgeCompleteness(edge)...)

	return violations
}

func (pc *PolicyChecker) checkEdgeDirectionWithValidationPolicy(
	ctx context.Context,
	edge *horizonmodels.DependencyEdge,
	sourceType, targetType ah.FragmentClass,
	sourcePath, targetPath string,
) ([]PolicyViolation, PolicyDecision, error) {
	input := PolicyInput{
		Kind: "edge",
		Context: map[string]interface{}{
			"validationKind":      "edge",
			"sourceFragmentID":    edge.SourceFragmentID,
			"targetFragmentID":    edge.TargetFragmentID,
			"sourceType":          string(sourceType),
			"targetType":          string(targetType),
			"sourceCanonicalPath": sourcePath,
			"targetCanonicalPath": targetPath,
			"relationType":        string(edge.Relation),
		},
		Edge: edge,
	}
	decision, err := pc.validationExec.Execute(ctx, input)
	if err != nil {
		return nil, PolicyDecision{}, err
	}
	decision = NormalizeValidationDecision(decision)

	actual := fmt.Sprintf("%s (%s) -> %s (%s) [%s]", edge.SourceFragmentID, sourceType, edge.TargetFragmentID, targetType, edge.Relation)
	switch decision.Action {
	case ActionReverse:
		return []PolicyViolation{
			{
				ID:          uuid.New().String(),
				Type:        ViolationEdgeDirection,
				Severity:    SeverityWarning,
				EdgeID:      edge.ID,
				ActualValue: actual,
				ExpectedValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]",
					edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Relation),
				Rule:        "edge_direction",
				Description: decisionReason(decision, "edge direction invalid, reversible"),
				Confidence:  decision.Confidence,
				AutoFixable: true,
				SuggestedFix: &EnforcementTask{
					Type:     TaskReverseEdge,
					TargetID: edge.ID,
				},
				DetectedAt: time.Now(),
				DetectedBy: "deterministic",
			},
		}, decision, nil
	case ActionRewriteType:
		newType, _ := decision.Params["newType"].(string)
		shouldSwap, _ := decision.Params["shouldReverse"].(bool)
		if strings.TrimSpace(newType) == "" {
			return nil, decision, nil
		}
		return []PolicyViolation{
			{
				ID:          uuid.New().String(),
				Type:        ViolationTypeMismatch,
				Severity:    SeverityWarning,
				EdgeID:      edge.ID,
				ActualValue: actual,
				ExpectedValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]",
					edge.SourceFragmentID, sourceType, edge.TargetFragmentID, targetType, strings.TrimSpace(newType)),
				Rule:        "edge_direction",
				Description: decisionReason(decision, "rewrite edge type"),
				Confidence:  decision.Confidence,
				AutoFixable: true,
				SuggestedFix: &EnforcementTask{
					Type:     TaskCanonicalizeEdge,
					TargetID: edge.ID,
					Params: map[string]interface{}{
						"newType":    strings.TrimSpace(newType),
						"shouldSwap": shouldSwap,
					},
				},
				DetectedAt: time.Now(),
				DetectedBy: "deterministic",
			},
		}, decision, nil
	case ActionAutoFix:
		newType, _ := decision.Params["newType"].(string)
		shouldSwap, _ := decision.Params["shouldReverse"].(bool)
		if strings.TrimSpace(newType) != "" {
			return []PolicyViolation{
				{
					ID:          uuid.New().String(),
					Type:        ViolationTypeMismatch,
					Severity:    SeverityWarning,
					EdgeID:      edge.ID,
					ActualValue: actual,
					ExpectedValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]",
						edge.SourceFragmentID, sourceType, edge.TargetFragmentID, targetType, strings.TrimSpace(newType)),
					Rule:        "edge_direction",
					Description: decisionReason(decision, "auto_fix edge type"),
					Confidence:  decision.Confidence,
					AutoFixable: true,
					SuggestedFix: &EnforcementTask{
						Type:     TaskCanonicalizeEdge,
						TargetID: edge.ID,
						Params: map[string]interface{}{
							"newType":    strings.TrimSpace(newType),
							"shouldSwap": shouldSwap,
						},
					},
					DetectedAt: time.Now(),
					DetectedBy: "deterministic",
				},
			}, decision, nil
		}
		if shouldSwap {
			return []PolicyViolation{
				{
					ID:          uuid.New().String(),
					Type:        ViolationEdgeDirection,
					Severity:    SeverityWarning,
					EdgeID:      edge.ID,
					ActualValue: actual,
					ExpectedValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]",
						edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Relation),
					Rule:        "edge_direction",
					Description: decisionReason(decision, "auto_fix direction"),
					Confidence:  decision.Confidence,
					AutoFixable: true,
					SuggestedFix: &EnforcementTask{
						Type:     TaskReverseEdge,
						TargetID: edge.ID,
					},
					DetectedAt: time.Now(),
					DetectedBy: "deterministic",
				},
			}, decision, nil
		}
	case ActionQuarantineSoft:
		severity := SeverityCritical
		shouldReverse, _ := decision.Params["shouldReverse"].(bool)
		if shouldReverse {
			severity = SeverityWarning
		}
		v := PolicyViolation{
			ID:          uuid.New().String(),
			Type:        ViolationEdgeDirection,
			Severity:    severity,
			EdgeID:      edge.ID,
			ActualValue: actual,
			Rule:        "edge_direction",
			Description: decisionReason(decision, "edge direction invalid"),
			Confidence:  decision.Confidence,
			AutoFixable: shouldReverse,
			DetectedAt:  time.Now(),
			DetectedBy:  "deterministic",
		}
		if shouldReverse {
			v.ExpectedValue = fmt.Sprintf("%s (%s) -> %s (%s) [%s]", edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Relation)
			v.SuggestedFix = &EnforcementTask{
				Type:     TaskReverseEdge,
				TargetID: edge.ID,
			}
		}
		return []PolicyViolation{v}, decision, nil
	}

	return nil, decision, nil
}

func (pc *PolicyChecker) checkEdgeDirectionLegacy(
	edge *horizonmodels.DependencyEdge,
	sourceType, targetType ah.FragmentClass,
) []PolicyViolation {
	var violations []PolicyViolation

	isValid, shouldReverse, reason := pc.taxonomyRules.ValidateEdgeDirection(edge.Relation, sourceType, targetType)
	if isValid {
		return violations
	}

	if !shouldReverse {
		if fix, ok := ResolveEdgeDirectionAutoFix(edge.Relation, sourceType, targetType); ok {
			expectedSourceID := edge.SourceFragmentID
			expectedTargetID := edge.TargetFragmentID
			expectedSourceType := sourceType
			expectedTargetType := targetType
			if fix.ShouldSwap {
				expectedSourceID, expectedTargetID = edge.TargetFragmentID, edge.SourceFragmentID
				expectedSourceType, expectedTargetType = targetType, sourceType
			}

			v := PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationEdgeDirection,
				Severity:    SeverityWarning,
				EdgeID:      edge.ID,
				ActualValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]", edge.SourceFragmentID, sourceType, edge.TargetFragmentID, targetType, edge.Relation),
				Rule:        "edge_direction",
				Description: fix.Reason,
				Confidence:  fix.Confidence,
				AutoFixable: true,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			}
			v.ExpectedValue = fmt.Sprintf(
				"%s (%s) -> %s (%s) [%s]",
				expectedSourceID, expectedSourceType, expectedTargetID, expectedTargetType, fix.NewType,
			)
			v.SuggestedFix = &EnforcementTask{
				Type:     TaskCanonicalizeEdge,
				TargetID: edge.ID,
				Params: map[string]interface{}{
					"newType":    string(fix.NewType),
					"shouldSwap": fix.ShouldSwap,
				},
			}
			return append(violations, v)
		}
	}

	severity := SeverityWarning
	if !shouldReverse {
		severity = SeverityCritical
	}
	v := PolicyViolation{
		ID:          uuid.New().String(),
		Type:        ViolationEdgeDirection,
		Severity:    severity,
		EdgeID:      edge.ID,
		ActualValue: fmt.Sprintf("%s (%s) -> %s (%s) [%s]", edge.SourceFragmentID, sourceType, edge.TargetFragmentID, targetType, edge.Relation),
		Rule:        "edge_direction",
		Description: reason,
		Confidence:  1.0,
		AutoFixable: shouldReverse,
		DetectedAt:  time.Now(),
		DetectedBy:  "deterministic",
	}
	if shouldReverse {
		v.ExpectedValue = fmt.Sprintf("%s (%s) -> %s (%s) [%s]", edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Relation)
		v.SuggestedFix = &EnforcementTask{
			Type:     TaskReverseEdge,
			TargetID: edge.ID,
		}
	}
	return append(violations, v)
}

func (pc *PolicyChecker) logEdgeDirectionShadowDiff(
	edge *horizonmodels.DependencyEdge,
	decision PolicyDecision,
	unified, legacy []PolicyViolation,
) {
	if len(unified) == len(legacy) {
		same := true
		for i := range unified {
			if unified[i].Type != legacy[i].Type ||
				unified[i].Severity != legacy[i].Severity ||
				strings.TrimSpace(unified[i].ExpectedValue) != strings.TrimSpace(legacy[i].ExpectedValue) {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	LogPolicyEvent("policy_checker_policy", PolicyLogStageFallback, map[string]interface{}{
		"edge_id":               edge.ID,
		"source_id":             edge.SourceFragmentID,
		"target_id":             edge.TargetFragmentID,
		"relation_type":         edge.Relation,
		"action":                decision.Action,
		"decision_path":         decision.DecisionPath,
		"unified_violation_cnt": len(unified),
		"legacy_violation_cnt":  len(legacy),
	})
}

func decisionReason(decision PolicyDecision, fallback string) string {
	if reason, _ := decision.Params["reason"].(string); strings.TrimSpace(reason) != "" {
		return strings.TrimSpace(reason)
	}
	return fallback
}

// checkEdgeCompleteness adds best-effort enrichment hints for incomplete business-critical edges.
// These are warnings/info only and do not block the pipeline.
func (pc *PolicyChecker) checkEdgeCompleteness(edge *horizonmodels.DependencyEdge) []PolicyViolation {
	var violations []PolicyViolation
	if edge == nil {
		return violations
	}
	now := time.Now()

	add := func(sev PolicyViolationSeverity, rule, desc, expected string) {
		violations = append(violations, PolicyViolation{
			ID:            uuid.New().String(),
			Type:          ViolationMissingField,
			Severity:      sev,
			EdgeID:        edge.ID,
			ActualValue:   string(edge.Relation),
			ExpectedValue: expected,
			Rule:          rule,
			Description:   desc,
			Confidence:    0.95,
			AutoFixable:   false,
			DetectedAt:    now,
			DetectedBy:    "deterministic",
		})
	}

	hasQualifier := func(keys ...string) bool {
		if edge.Qualifiers == nil {
			return false
		}
		for _, k := range keys {
			if v, ok := edge.Qualifiers[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
				return true
			}
		}
		return false
	}
	hasMetric := func(keys ...string) bool {
		if edge.Metrics == nil {
			return false
		}
		for _, k := range keys {
			if _, ok := edge.Metrics[k]; ok {
				return true
			}
		}
		return false
	}
	hasContractQualifier := func(keys ...string) bool {
		for _, k := range keys {
			if !taxonomy.IsValidEdgeQualifier(string(edge.Relation), k) {
				continue
			}
			if hasQualifier(k) {
				return true
			}
		}
		return false
	}

	switch edge.Relation {
	case relOwns:
		if !hasMetric("ownership_percent") && !hasQualifier("ownership_percent", "stake_pct") {
			add(
				SeverityWarning,
				"edge_completeness_owns_percent",
				"OWNS edge missing ownership percentage. Enrichment recommended: capture stake/share percent.",
				"metrics.ownership_percent or qualifiers.ownership_percent",
			)
		}
		if !hasContractQualifier("share_type") {
			add(
				SeverityInfo,
				"edge_completeness_owns_share_type",
				"OWNS edge missing share type. Enrichment recommended: distinguish direct, indirect, and beneficial ownership.",
				"qualifiers.share_type",
			)
		}

	case relSubsidiaryOf:
		if !hasContractQualifier("share_type") {
			add(
				SeverityInfo,
				"edge_completeness_subsidiary_share_type",
				"SUBSIDIARY_OF edge missing share type. Enrichment recommended: capture whether the upstream owner is direct, indirect, or beneficial.",
				"qualifiers.share_type",
			)
		}

	case relControls:
		if !hasContractQualifier("control_type", "mechanism") {
			add(
				SeverityWarning,
				"edge_completeness_controls_mechanism",
				"CONTROLS edge missing control basis. Enrichment recommended: capture control_type and/or mechanism (board, veto, trustee, state mandate, etc.).",
				"qualifiers.control_type or qualifiers.mechanism",
			)
		}

	case relInvestsIn:
		if !hasMetric("investment_amount", "round_size_total") {
			add(
				SeverityWarning,
				"edge_completeness_invests_amount",
				"INVESTS_IN edge missing investment amount. Enrichment recommended: capture investment_amount/round_size_total.",
				"metrics.investment_amount",
			)
		}
		if !hasQualifier("round", "fundingRound", "roundType") {
			add(
				SeverityInfo,
				"edge_completeness_invests_round",
				"INVESTS_IN edge missing round qualifier. Enrichment recommended: seed/series_a/series_b/etc.",
				"qualifiers.round",
			)
		}

	case relEmployedBy, relBoardMemberOf, relManages:
		modality := ""
		if edge.Qualifiers != nil {
			if raw, ok := edge.Qualifiers["edgeModality"]; ok {
				modality = strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
				if modality == "<nil>" {
					modality = ""
				}
			}
		}
		if modality == "historical" && edge.ValidTo == nil {
			add(
				SeverityWarning,
				"edge_temporal_historical_without_end",
				"Historical relation lacks ValidTo. Enrichment recommended: set relationship end date.",
				"validTo (YYYY-MM-DD)",
			)
		}
		if modality == "" && edge.ValidFrom == nil && edge.ValidTo == nil {
			add(
				SeverityInfo,
				"edge_temporal_unknown_modality",
				"Relation has no temporal modality or validity window. Enrichment recommended: mark actual/projected/historical and add validFrom/validTo when known.",
				"qualifiers.edgeModality and/or validFrom/validTo",
			)
		}

	case relInGeoScope:
		if !hasContractQualifier("scope_basis") {
			add(
				SeverityInfo,
				"edge_completeness_geo_scope_basis",
				"IN_GEO_SCOPE edge missing scope basis. Enrichment recommended: capture whether scope is legal, administrative, operational, or de facto.",
				"qualifiers.scope_basis",
			)
		}

	case relAffects,
		relInfluences,
		relAppliesTo,
		relImposesCondition,
		relOperatesVia:
		if !hasContractQualifier(
			"pass_through_estimate",
			"channel_type",
			"lag_quarters",
			"measurement_lag_months",
			"measurement_confidence",
			"tax_base_coverage",
			"elasticity_hint",
			"sector_sensitivity",
			"scope_basis",
			"impact",
			"type",
		) {
			add(
				SeverityInfo,
				"edge_completeness_transmission_qualifiers",
				fmt.Sprintf("%s edge is missing transmission qualifiers. Enrichment recommended: capture scope, channel, lag, or pass-through semantics.", edge.Relation),
				"qualifiers.pass_through_estimate or qualifiers.channel_type or qualifiers.measurement_lag_months",
			)
		}
	}

	return violations
}

// checkStrategicEdgeQualifiers validates qualifiers for strategic layer edges.
func (pc *PolicyChecker) checkStrategicEdgeQualifiers(edge *horizonmodels.DependencyEdge) []PolicyViolation {
	var violations []PolicyViolation

	// Early return for non-strategic edges
	switch edge.Relation {
	case relControlsAccess,
		relImposesCondition,
		relUsesMechanism,
		relAppliesTo:
		// Continue to validation
	default:
		return violations // Not a strategic edge, no qualifiers needed
	}

	// Ensure Qualifiers map exists for safe access
	qualifiers := edge.Qualifiers
	if qualifiers == nil {
		qualifiers = make(map[string]interface{})
	}

	switch edge.Relation {
	case relControlsAccess:
		// CONTROLS_ACCESS requires 'type' qualifier (block|enable|delay|monitor)
		typeVal, hasType := qualifiers["type"]
		if !hasType {
			violations = append(violations, PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationMissingField,
				Severity:    SeverityWarning,
				EdgeID:      edge.ID,
				ActualValue: "",
				Rule:        "controls_access_type_qualifier",
				Description: "CONTROLS_ACCESS edge missing required 'type' qualifier (block|enable|delay|monitor)",
				Confidence:  1.0,
				AutoFixable: false,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			})
		} else if typeStr, ok := typeVal.(string); ok {
			if !IsValidControlAccessType(typeStr) {
				violations = append(violations, PolicyViolation{
					ID:          uuid.New().String(),
					Type:        ViolationTypeMismatch,
					Severity:    SeverityWarning,
					EdgeID:      edge.ID,
					ActualValue: typeStr,
					Rule:        "controls_access_type_value",
					Description: fmt.Sprintf("CONTROLS_ACCESS 'type' must be one of: block, enable, delay, monitor. Got: %s", typeStr),
					Confidence:  1.0,
					AutoFixable: false,
					DetectedAt:  time.Now(),
					DetectedBy:  "deterministic",
				})
			}
		}

	case relImposesCondition:
		// IMPOSES_CONDITION requires 'type' qualifier (cost|compliance|technical)
		typeVal, hasType := qualifiers["type"]
		if !hasType {
			violations = append(violations, PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationMissingField,
				Severity:    SeverityWarning,
				EdgeID:      edge.ID,
				ActualValue: "",
				Rule:        "imposes_condition_type_qualifier",
				Description: "IMPOSES_CONDITION edge missing required 'type' qualifier (cost|compliance|technical)",
				Confidence:  1.0,
				AutoFixable: false,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			})
		} else if typeStr, ok := typeVal.(string); ok {
			if !IsValidConditionType(typeStr) {
				violations = append(violations, PolicyViolation{
					ID:          uuid.New().String(),
					Type:        ViolationTypeMismatch,
					Severity:    SeverityWarning,
					EdgeID:      edge.ID,
					ActualValue: typeStr,
					Rule:        "imposes_condition_type_value",
					Description: fmt.Sprintf("IMPOSES_CONDITION 'type' must be one of: cost, compliance, technical. Got: %s", typeStr),
					Confidence:  1.0,
					AutoFixable: false,
					DetectedAt:  time.Now(),
					DetectedBy:  "deterministic",
				})
			}
		}
		// 'impact' is recommended but not strictly required
		if _, hasImpact := qualifiers["impact"]; !hasImpact {
			violations = append(violations, PolicyViolation{
				ID:          uuid.New().String(),
				Type:        ViolationMissingField,
				Severity:    SeverityInfo, // Info level since it's recommended, not required
				EdgeID:      edge.ID,
				ActualValue: "",
				Rule:        "imposes_condition_impact_qualifier",
				Description: "IMPOSES_CONDITION edge should have 'impact' qualifier describing the effect",
				Confidence:  0.8,
				AutoFixable: false,
				DetectedAt:  time.Now(),
				DetectedBy:  "deterministic",
			})
		}

	case relUsesMechanism:
		// USES_MECHANISM: target should be a /mechanism/... path
		// This is validated by edge direction rules, no extra qualifiers needed
		break

	case relAppliesTo:
		// APPLIES_TO: source should be a /mechanism/... path
		// This is validated by edge direction rules, no extra qualifiers needed
		break
	}

	return violations
}

// getEdgeEndpoints resolves source/target fragments for an edge.
func (pc *PolicyChecker) getEdgeEndpoints(ctx context.Context, edge *horizonmodels.DependencyEdge) (*horizonmodels.HorizonFragment, *horizonmodels.HorizonFragment, error) {
	source, err := pc.fragmentsRepo.Get(ctx, edge.SourceFragmentID)
	if err != nil || source == nil {
		return nil, nil, fmt.Errorf("source fragment not found: %s", edge.SourceFragmentID)
	}

	target, err := pc.fragmentsRepo.Get(ctx, edge.TargetFragmentID)
	if err != nil || target == nil {
		return nil, nil, fmt.Errorf("target fragment not found: %s", edge.TargetFragmentID)
	}

	return source, target, nil
}

// checkForDuplicates uses LLM + Vertex prefetch to detect potential duplicates.
func (pc *PolicyChecker) checkForDuplicatesWithContext(ctx context.Context, frag *horizonmodels.HorizonFragment, jobFragments []*horizonmodels.HorizonFragment, checkCount *int) []PolicyViolation {
	var violations []PolicyViolation

	if pc.aiExecutor == nil || frag == nil {
		return violations
	}

	// Build search query from fragment names
	searchTerms := []string{}
	if label := strings.TrimSpace(frag.Labels["en"]); label != "" {
		searchTerms = append(searchTerms, label)
	}
	if short := strings.TrimSpace(frag.Identifiers["shortName"]); short != "" {
		searchTerms = append(searchTerms, short)
	}
	if legal := strings.TrimSpace(frag.Identifiers["legalName"]); legal != "" {
		searchTerms = append(searchTerms, legal)
	}

	if len(searchTerms) == 0 {
		return violations
	}

	query := strings.Join(searchTerms, " ")

	// Prepare job context candidates (same type)
	jobCandidates := pc.jobContextCandidates(frag, jobFragments, pc.jobContextLimit)

	// Prefetch candidates from Vertex AI Search (existing system)
	externalCandidates := []*horizonmodels.HorizonFragment{}
	if pc.vertexSearch != nil {
		candidates, err := pc.prefetchDuplicateCandidates(ctx, frag, query)
		if err != nil {
			log.Printf("[PolicyChecker] WARNING: prefetch failed for %s: %v", frag.ID, err)
		} else {
			externalCandidates = candidates
		}
	}
	externalCandidates = filterExternalCandidates(frag, jobCandidates, externalCandidates, pc.externalContextLimit)

	// Merge candidates, prioritize job-created items first
	candidates := append([]*horizonmodels.HorizonFragment{}, jobCandidates...)
	candidates = append(candidates, externalCandidates...)

	if len(candidates) == 0 {
		return violations
	}

	// Use LLM to check each candidate
	for _, candidate := range candidates {
		if candidate.ID == frag.ID {
			continue // Skip self
		}
		if shouldSkipDuplicateCandidate(frag, candidate) {
			continue
		}
		if pc.maxDuplicateChecks > 0 && checkCount != nil && *checkCount >= pc.maxDuplicateChecks {
			break
		}

		// Deterministic fast-path for common ORG near-duplicates:
		// e.g. "entourage-marketing-and-events-llc" vs "entourage-marketing-events-llc".
		// This avoids relying on LLM duplicate checks for simple stopword/formatting differences.
		if ok, conf, reason := pc.isDeterministicOrgNearDuplicate(frag, candidate); ok {
			canonicalID, duplicateID := pc.selectCanonical(frag, candidate)
			canonicalPath := canonicalPathForID(frag, candidate, canonicalID)
			violations = append(violations, PolicyViolation{
				ID:              uuid.New().String(),
				Type:            ViolationDuplicate,
				Severity:        duplicateSeverity(conf, pc.autoFixConfidenceThreshold),
				FragmentID:      duplicateID,
				DuplicateOfID:   canonicalID,
				DuplicateOfPath: canonicalPath,
				ActualValue:     frag.CanonicalPath,
				ExpectedValue:   canonicalPath,
				Rule:            "no_duplicates",
				Description:     reason,
				Confidence:      conf,
				AutoFixable:     pc.canAutoFixDuplicate(frag, candidate, conf),
				SuggestedFix:    pc.duplicateFixTask(frag, candidate, canonicalID, duplicateID, conf),
				DetectedAt:      time.Now(),
				DetectedBy:      "deterministic",
			})
			continue
		}

		jobContext := pc.jobContextSummaries(candidate, jobCandidates)
		source := "existing"
		if isJobCandidate(candidate, jobCandidates) {
			source = "job"
		}
		isDuplicate, confidence, reason := pc.llmCheckDuplicateWithContext(ctx, frag, candidate, jobContext, source)
		if checkCount != nil {
			*checkCount++
		}
		if isDuplicate && confidence >= pc.confidenceThreshold {
			// Determine which should be canonical
			canonicalID, duplicateID := pc.selectCanonical(frag, candidate)
			canonicalPath := canonicalPathForID(frag, candidate, canonicalID)

			violations = append(violations, PolicyViolation{
				ID:              uuid.New().String(),
				Type:            ViolationDuplicate,
				Severity:        duplicateSeverity(confidence, pc.autoFixConfidenceThreshold),
				FragmentID:      duplicateID,
				DuplicateOfID:   canonicalID,
				DuplicateOfPath: canonicalPath,
				ActualValue:     frag.CanonicalPath,
				ExpectedValue:   canonicalPath,
				Rule:            "no_duplicates",
				Description:     reason,
				Confidence:      confidence,
				AutoFixable:     pc.canAutoFixDuplicate(frag, candidate, confidence),
				SuggestedFix:    pc.duplicateFixTask(frag, candidate, canonicalID, duplicateID, confidence),
				DetectedAt:      time.Now(),
				DetectedBy:      "llm",
			})
		}
	}

	return violations
}

// isDeterministicOrgNearDuplicate returns true for common ORG path variants that differ only by
// "and" tokens or embedded legal-form letters (e.g., "...-l-l-c-llc"), while sharing the same geo.
// This is intentionally narrow to avoid false-positive merges.
func (pc *PolicyChecker) isDeterministicOrgNearDuplicate(a, b *horizonmodels.HorizonFragment) (bool, float64, string) {
	if a == nil || b == nil {
		return false, 0, ""
	}
	ap := strings.TrimSpace(a.CanonicalPath)
	bp := strings.TrimSpace(b.CanonicalPath)
	if !strings.HasPrefix(ap, "/org/") || !strings.HasPrefix(bp, "/org/") {
		return false, 0, ""
	}
	ageo, aslug, okA := normalizeOrgPathForNearDup(ap)
	bgeo, bslug, okB := normalizeOrgPathForNearDup(bp)
	if !okA || !okB {
		return false, 0, ""
	}
	if ageo == "" || bgeo == "" || ageo != bgeo {
		return false, 0, ""
	}
	if aslug == "" || bslug == "" || aslug != bslug {
		return false, 0, ""
	}
	// If canonical paths are already identical, let the canonicalPath collision merge handle it.
	if ap == bp {
		return false, 0, ""
	}
	return true, 0.95, fmt.Sprintf("Near-duplicate ORGANIZATION canonical paths differ only by stopwords/formatting: %s vs %s", ap, bp)
}

func normalizeOrgPathForNearDup(path string) (geo string, slug string, ok bool) {
	path = strings.TrimSpace(path)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// /org/{geo}/{slug}
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "org" {
		return "", "", false
	}
	geo = strings.ToLower(strings.TrimSpace(parts[1]))
	slug = strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if geo == "" || slug == "" {
		return "", "", false
	}

	// Strip legal form suffix if present (for legacy paths).
	for _, lf := range []string{"pjsc", "cjsc", "ojsc", "jsc", "ooo", "llc", "inc", "ltd", "corp", "plc", "gmbh", "sarl", "srl", "ag", "sa", "spa", "as", "ab", "bv", "nv"} {
		if strings.HasSuffix(slug, "-"+lf) {
			slug = strings.TrimSuffix(slug, "-"+lf)
			break
		}
	}

	toks := strings.Split(slug, "-")
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		// Ignore a narrow stopword that frequently causes duplicate slugs.
		if t == "and" {
			continue
		}
		out = append(out, t)
	}

	// Remove trailing embedded legal-form letters (e.g., "-l-l-c") which can appear before "-llc".
	// We only do this for a small set of common legal forms to keep it safe.
	removeTrailingLetters := func(letters string) {
		letters = strings.ToLower(strings.TrimSpace(letters))
		if letters == "" || len(out) < len(letters) {
			return
		}
		n := len(letters)
		start := len(out) - n
		for i := 0; i < n; i++ {
			if len(out[start+i]) != 1 || out[start+i] != string(letters[i]) {
				return
			}
		}
		out = out[:start]
	}
	removeTrailingLetters("llc")
	removeTrailingLetters("ltd")

	slug = strings.Join(out, "-")
	return geo, slug, slug != ""
}

// prefetchDuplicateCandidates searches for potential duplicate fragments using Vertex AI Search.
func (pc *PolicyChecker) prefetchDuplicateCandidates(ctx context.Context, frag *horizonmodels.HorizonFragment, query string) ([]*horizonmodels.HorizonFragment, error) {
	// Build filter for same type
	filter := fmt.Sprintf(`structData.type: "%s"`, frag.Class)

	req := &ai.SearchRequest{
		Query:    query,
		PageSize: pc.duplicatePrefetchLimit,
		Filter:   filter,
	}

	resp, err := pc.vertexSearch.Search(ctx, req)
	if err != nil {
		return nil, err
	}

	candidates := make([]*horizonmodels.HorizonFragment, 0)
	for _, result := range resp.Results {
		if result.Document.StructData == nil {
			continue
		}

		// Extract fragment ID from Vertex result
		fragID, ok := result.Document.StructData["id"].(string)
		if !ok || fragID == "" {
			continue
		}

		// Load full fragment
		candidate, err := pc.fragmentsRepo.Get(ctx, fragID)
		if err != nil || candidate == nil {
			continue
		}

		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

// llmCheckDuplicate uses LLM to determine if two fragments are duplicates.
func (pc *PolicyChecker) llmCheckDuplicateWithContext(ctx context.Context, frag1, frag2 *horizonmodels.HorizonFragment, jobContext []string, source string) (bool, float64, string) {
	if pc.aiExecutor == nil {
		return false, 0, ""
	}

	prompt := pc.buildDuplicateCheckPromptWithContext(frag1, frag2, jobContext, source)

	// Create verifier for structured response
	verifier := ai.NewGenericVerifier(func(result DuplicateCheckResult) error {
		if result.Confidence < 0 || result.Confidence > 1 {
			return fmt.Errorf("confidence must be between 0 and 1")
		}
		return nil
	})

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability:  true,
			SearchGroundingCapability: false,
			MinCapabilityScore:        6,
			MinContextWindow:          4000,
			MaxOutputSize:             4096,
		},
		Verifier: verifier,
	}

	// IMPORTANT: policy checks run under an overall timeout (see ScoutEngine.runPolicyCheck).
	// Use a per-call timeout so one slow/blocked provider doesn't consume the entire budget and
	// cause all subsequent fallbacks (and result persistence) to fail with DeadlineExceeded.
	llmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	raw, err := pc.aiExecutor.ExecuteStructuredTask(llmCtx, task)
	if err != nil {
		log.Printf("[PolicyChecker] LLM duplicate check failed: %v", err)
		return false, 0, ""
	}

	// Type assert to DuplicateCheckResult
	result, ok := raw.(DuplicateCheckResult)
	if !ok {
		// Try to convert from map
		if m, ok := raw.(map[string]interface{}); ok {
			result = DuplicateCheckResult{
				IsDuplicate: getBool(m, "isDuplicate"),
				Confidence:  getFloat(m, "confidence"),
				Reason:      getString(m, "reason"),
			}
		} else {
			log.Printf("[PolicyChecker] Failed to parse LLM response: unexpected type %T", raw)
			return false, 0, ""
		}
	}

	return result.IsDuplicate, result.Confidence, result.Reason
}

// buildDuplicateCheckPromptWithContext builds the LLM prompt for duplicate checking.
func (pc *PolicyChecker) buildDuplicateCheckPromptWithContext(frag1, frag2 *horizonmodels.HorizonFragment, jobContext []string, source string) string {
	var sb strings.Builder

	sb.WriteString("You are a Policy Enforcement Agent checking for duplicate entities.\n\n")
	sb.WriteString(pc.taxonomyRules.ToLLMContext())
	sb.WriteString("\n\n## Task\n")
	sb.WriteString("Determine if these two fragments represent the SAME real-world entity.\n\n")

	sb.WriteString("## Fragment 1 (newly created)\n")
	sb.WriteString(fmt.Sprintf("- Canonical Path: %s\n", frag1.CanonicalPath))
	sb.WriteString(fmt.Sprintf("- Type: %s\n", frag1.Class))
	if label := frag1.Labels["en"]; label != "" {
		sb.WriteString(fmt.Sprintf("- Label: %s\n", label))
	}
	if short := frag1.Identifiers["shortName"]; short != "" {
		sb.WriteString(fmt.Sprintf("- Short Name: %s\n", short))
	}
	if legal := frag1.Identifiers["legalName"]; legal != "" {
		sb.WriteString(fmt.Sprintf("- Legal Name: %s\n", legal))
	}
	if wikidata := frag1.Identifiers["wikidataId"]; wikidata != "" {
		sb.WriteString(fmt.Sprintf("- Wikidata ID: %s\n", wikidata))
	}

	sb.WriteString("\n## Fragment 2 (existing)\n")
	sb.WriteString(fmt.Sprintf("- Canonical Path: %s\n", frag2.CanonicalPath))
	sb.WriteString(fmt.Sprintf("- Type: %s\n", frag2.Class))
	if label := frag2.Labels["en"]; label != "" {
		sb.WriteString(fmt.Sprintf("- Label: %s\n", label))
	}
	if short := frag2.Identifiers["shortName"]; short != "" {
		sb.WriteString(fmt.Sprintf("- Short Name: %s\n", short))
	}
	if legal := frag2.Identifiers["legalName"]; legal != "" {
		sb.WriteString(fmt.Sprintf("- Legal Name: %s\n", legal))
	}
	if wikidata := frag2.Identifiers["wikidataId"]; wikidata != "" {
		sb.WriteString(fmt.Sprintf("- Wikidata ID: %s\n", wikidata))
	}

	sb.WriteString("\n## Important Considerations\n")
	sb.WriteString("- If Wikidata IDs are present and DIFFERENT, they are NOT duplicates.\n")
	sb.WriteString("- 'Group' or 'Holdings' in name often indicates a DIFFERENT entity from the operating company.\n")
	sb.WriteString("- Legal forms (Inc, LLC, JSC) don't affect identity - 'Aramco' and 'Saudi Aramco JSC' can be the same.\n")
	sb.WriteString("- Different jurisdictions (ISO2 codes) may indicate different legal entities.\n")
	sb.WriteString("- Be conservative: if there is any doubt, respond isDuplicate=false with lower confidence.\n")
	sb.WriteString("- Do NOT infer a duplicate from similar names alone without supporting identifiers.\n")
	if source == "job" {
		sb.WriteString("- The candidate fragment was created within the same job; be extra careful with merges.\n")
	} else {
		sb.WriteString("- The candidate fragment existed before this job; avoid false merges.\n")
	}

	if len(jobContext) > 0 {
		sb.WriteString("\n## Job Context (other fragments created/updated in this job)\n")
		for _, line := range jobContext {
			sb.WriteString("- " + line + "\n")
		}
		sb.WriteString("(This list may be truncated for size.)\n")
	}

	sb.WriteString("\n## Response Format\n")
	sb.WriteString("Respond with ONLY a JSON object:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"isDuplicate\": true/false,\n")
	sb.WriteString("  \"confidence\": 0.0-1.0,\n")
	sb.WriteString("  \"reason\": \"Brief explanation\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n")

	return sb.String()
}

// selectCanonical determines which fragment should be the canonical one.
func (pc *PolicyChecker) selectCanonical(frag1, frag2 *horizonmodels.HorizonFragment) (canonicalID, duplicateID string) {
	// Prefer fragment with more data
	score1 := pc.fragmentCompleteness(frag1)
	score2 := pc.fragmentCompleteness(frag2)

	// Prefer existing (older) fragment
	if frag1.CreatedAt.After(frag2.CreatedAt) {
		score2 += 10
	} else {
		score1 += 10
	}

	// Prefer canonical path format
	if isCanonicalPathFormat(frag1.CanonicalPath, frag1.Class) {
		score1 += 5
	}
	if isCanonicalPathFormat(frag2.CanonicalPath, frag2.Class) {
		score2 += 5
	}

	if score1 >= score2 {
		return frag1.ID, frag2.ID
	}
	return frag2.ID, frag1.ID
}

// fragmentCompleteness calculates a completeness score for a fragment.
func (pc *PolicyChecker) fragmentCompleteness(frag *horizonmodels.HorizonFragment) int {
	score := 0

	if frag.Labels["en"] != "" {
		score += 2
	}
	if frag.Identifiers["legalName"] != "" {
		score += 2
	}
	if frag.Identifiers["shortName"] != "" {
		score += 1
	}
	if frag.Identifiers["wikidataId"] != "" {
		score += 5
	}
	if frag.Description != "" {
		score += 1
	}
	if len(frag.KBRefs) > 0 {
		score += 3
	}

	return score
}

// Helper functions

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Remove multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

func shouldSkipDuplicateCandidate(frag *horizonmodels.HorizonFragment, candidate *horizonmodels.HorizonFragment) bool {
	fid := strings.TrimSpace(frag.Identifiers["wikidataId"])
	cid := strings.TrimSpace(candidate.Identifiers["wikidataId"])
	if fid != "" && cid != "" && !strings.EqualFold(fid, cid) {
		return true
	}
	return false
}

func duplicateSeverity(confidence, autoFixThreshold float64) PolicyViolationSeverity {
	if confidence >= autoFixThreshold {
		return SeverityCritical
	}
	return SeverityWarning
}

func canonicalPathForID(frag *horizonmodels.HorizonFragment, candidate *horizonmodels.HorizonFragment, canonicalID string) string {
	if frag != nil && frag.ID == canonicalID {
		return frag.CanonicalPath
	}
	if candidate != nil && candidate.ID == canonicalID {
		return candidate.CanonicalPath
	}
	return ""
}

func (pc *PolicyChecker) canAutoFixDuplicate(frag *horizonmodels.HorizonFragment, candidate *horizonmodels.HorizonFragment, confidence float64) bool {
	if confidence < pc.autoFixConfidenceThreshold {
		return false
	}
	fid := strings.TrimSpace(frag.Identifiers["wikidataId"])
	cid := strings.TrimSpace(candidate.Identifiers["wikidataId"])
	if fid != "" && cid != "" {
		return strings.EqualFold(fid, cid)
	}
	if fid == "" && cid == "" {
		return confidence >= 0.95
	}
	return false
}

func (pc *PolicyChecker) duplicateFixTask(frag *horizonmodels.HorizonFragment, candidate *horizonmodels.HorizonFragment, canonicalID, duplicateID string, confidence float64) *EnforcementTask {
	if !pc.canAutoFixDuplicate(frag, candidate, confidence) {
		return nil
	}
	return &EnforcementTask{
		Type:     TaskMergeFragments,
		TargetID: duplicateID,
		Params: map[string]interface{}{
			"canonicalId": canonicalID,
		},
	}
}

func (pc *PolicyChecker) jobContextCandidates(frag *horizonmodels.HorizonFragment, jobFragments []*horizonmodels.HorizonFragment, limit int) []*horizonmodels.HorizonFragment {
	if frag == nil || limit <= 0 {
		return nil
	}
	type scored struct {
		frag  *horizonmodels.HorizonFragment
		score int
	}
	scoredList := make([]scored, 0, len(jobFragments))
	for _, jf := range jobFragments {
		if jf == nil || jf.ID == frag.ID {
			continue
		}
		if jf.Class != frag.Class {
			continue
		}
		score := overlapScore(frag, jf)
		scoredList = append(scoredList, scored{frag: jf, score: score})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].score == scoredList[j].score {
			return scoredList[i].frag.ID < scoredList[j].frag.ID
		}
		return scoredList[i].score > scoredList[j].score
	})
	out := make([]*horizonmodels.HorizonFragment, 0, minInt(limit, len(scoredList)))
	for _, item := range scoredList {
		if len(out) >= limit {
			break
		}
		out = append(out, item.frag)
	}
	return out
}

func (pc *PolicyChecker) jobContextSummaries(candidate *horizonmodels.HorizonFragment, jobCandidates []*horizonmodels.HorizonFragment) []string {
	out := make([]string, 0, len(jobCandidates))
	for _, jf := range jobCandidates {
		if jf == nil {
			continue
		}
		if candidate != nil && jf.ID == candidate.ID {
			continue
		}
		out = append(out, summarizeFragment(jf))
	}
	return out
}

func filterExternalCandidates(frag *horizonmodels.HorizonFragment, jobCandidates []*horizonmodels.HorizonFragment, external []*horizonmodels.HorizonFragment, limit int) []*horizonmodels.HorizonFragment {
	if frag == nil || limit <= 0 {
		return nil
	}
	jobIDs := map[string]struct{}{}
	for _, jf := range jobCandidates {
		if jf != nil {
			jobIDs[jf.ID] = struct{}{}
		}
	}
	out := make([]*horizonmodels.HorizonFragment, 0, limit)
	for _, cand := range external {
		if cand == nil || cand.ID == frag.ID {
			continue
		}
		if _, exists := jobIDs[cand.ID]; exists {
			continue
		}
		out = append(out, cand)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func isJobCandidate(candidate *horizonmodels.HorizonFragment, jobCandidates []*horizonmodels.HorizonFragment) bool {
	if candidate == nil {
		return false
	}
	for _, jc := range jobCandidates {
		if jc != nil && jc.ID == candidate.ID {
			return true
		}
	}
	return false
}

func overlapScore(a *horizonmodels.HorizonFragment, b *horizonmodels.HorizonFragment) int {
	aTokens := tokenizeFragment(a)
	if len(aTokens) == 0 {
		return 0
	}
	bTokens := tokenizeFragment(b)
	score := 0
	for token := range aTokens {
		if _, ok := bTokens[token]; ok {
			score++
		}
	}
	return score
}

func tokenizeFragment(frag *horizonmodels.HorizonFragment) map[string]struct{} {
	tokens := map[string]struct{}{}
	if frag == nil {
		return tokens
	}
	addTokens(tokens, frag.CanonicalPath)
	addTokens(tokens, frag.Labels["en"])
	addTokens(tokens, frag.Identifiers["shortName"])
	addTokens(tokens, frag.Identifiers["legalName"])
	if v := strings.TrimSpace(frag.Identifiers["wikidataId"]); v != "" {
		tokens[strings.ToLower(v)] = struct{}{}
	}
	return tokens
}

func addTokens(dst map[string]struct{}, s string) {
	for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == '/' || r == '-' || r == '_' || r == '.' || r == ',' || r == ' ' || r == '(' || r == ')' || r == '[' || r == ']'
	}) {
		if len(t) < 2 {
			continue
		}
		dst[t] = struct{}{}
	}
}

func summarizeFragment(frag *horizonmodels.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	label := strings.TrimSpace(frag.Labels["en"])
	if label == "" {
		label = strings.TrimSpace(frag.Identifiers["shortName"])
	}
	if label == "" {
		label = strings.TrimSpace(frag.Identifiers["legalName"])
	}
	wikidata := strings.TrimSpace(frag.Identifiers["wikidataId"])
	if wikidata != "" {
		return fmt.Sprintf("%s (%s, %s)", frag.CanonicalPath, frag.Class, wikidata)
	}
	if label != "" {
		return fmt.Sprintf("%s (%s, %s)", frag.CanonicalPath, frag.Class, label)
	}
	return fmt.Sprintf("%s (%s)", frag.CanonicalPath, frag.Class)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Helper functions for parsing map[string]interface{} from LLM response
func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch f := v.(type) {
		case float64:
			return f
		case float32:
			return float64(f)
		case int:
			return float64(f)
		case int64:
			return float64(f)
		}
	}
	return 0
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func isCanonicalPathFormat(path string, fragType ah.FragmentClass) bool {
	path = strings.TrimSpace(path)

	switch fragType {
	case classGeoRegion:
		_, _, _, ok := taxonomy.ParseLocCanonicalPath(path)
		return ok

	case classOrganization:
		parts := strings.Split(strings.Trim(path, "/"), "/")
		return len(parts) >= 3 && (parts[0] == "org" || parts[0] == "gov")

	case classPerson:
		parts := strings.Split(strings.Trim(path, "/"), "/")
		return len(parts) == 2 && parts[0] == "person"
	}

	return true
}
