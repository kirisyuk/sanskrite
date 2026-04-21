package scout

import (
	"context"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"fmt"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"log"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// PRE-CREATE VALIDATION POLICY
// =============================================================================
// This policy defines validation rules that must pass before a fragment is created.
// Violations either block creation or trigger corrective actions (deduplication).

// PreCreateValidator validates fragments before creation and applies corrective policies.
type PreCreateValidator struct {
	geoBuilder       *GeoHierarchyBuilder
	orgDedup         *OrgDeduplicator
	productDedup     *ProductDeduplicator
	personDedup      *PersonDeduplicator
	facilityDedup    *FacilityDeduplicator
	validationPolicy *policy.PolicyExecutor // Optional: unified validation policy
	telemetryRepo    *repo.HorizonTelemetryRepository
	wikidataClient   coreontology.WikidataClientInterface
	geonamesClient   coreontology.GeoNamesClientInterface
	geocodingClient  coreontology.GeocodingClientInterface
}

// NewPreCreateValidator creates a new pre-create validator.
func NewPreCreateValidator(geoBuilder *GeoHierarchyBuilder, orgDedup *OrgDeduplicator, productDedup *ProductDeduplicator) *PreCreateValidator {
	return &PreCreateValidator{
		geoBuilder:   geoBuilder,
		orgDedup:     orgDedup,
		productDedup: productDedup,
	}
}

// WithPersonDedup sets the person deduplicator for phonetic matching.
func (v *PreCreateValidator) WithPersonDedup(dedup *PersonDeduplicator) *PreCreateValidator {
	v.personDedup = dedup
	return v
}

// WithFacilityDedup sets the facility deduplicator for name/path matching.
func (v *PreCreateValidator) WithFacilityDedup(dedup *FacilityDeduplicator) *PreCreateValidator {
	v.facilityDedup = dedup
	return v
}

// WithValidationPolicy sets the validation policy executor.
func (v *PreCreateValidator) WithValidationPolicy(executor *policy.PolicyExecutor) *PreCreateValidator {
	v.validationPolicy = executor
	return v
}

// WithTelemetryRepo wires aggregated policy KPI telemetry sink (best-effort).
func (v *PreCreateValidator) WithTelemetryRepo(telemetryRepo *repo.HorizonTelemetryRepository) *PreCreateValidator {
	v.telemetryRepo = telemetryRepo
	return v
}

// WithWikidataClient sets the Wikidata client for conservative enrichment (QID-known only).
// NOTE: This does NOT enable fuzzy Wikidata search; it only uses GetEntityData when wikidataId is already present.
func (v *PreCreateValidator) WithWikidataClient(client coreontology.WikidataClientInterface) *PreCreateValidator {
	v.wikidataClient = client
	return v
}

// WithGeoNamesClient sets the GeoNames client for deterministic geo enrichment/normalization.
// NOTE: wiring only; actual geo canonicalization rules are implemented elsewhere.
func (v *PreCreateValidator) WithGeoNamesClient(client coreontology.GeoNamesClientInterface) *PreCreateValidator {
	v.geonamesClient = client
	return v
}

// WithGeocodingClient sets the geocoding client for address-to-coordinates resolution (org/facility/gov).
func (v *PreCreateValidator) WithGeocodingClient(client coreontology.GeocodingClientInterface) *PreCreateValidator {
	v.geocodingClient = client
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
	fragment.Labels, fragment.Identifiers = naming.NormalizeFragmentNames(
		fragment.CanonicalPath,
		fragment.Labels,
		fragment.Identifiers,
	)

	log.Printf("%s Validating fragment: id=%s, type=%s, canonicalPath=%s",
		logPrefix, fragment.ID, fragment.Class, fragment.CanonicalPath)

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

			policy.ApplyValidationDecisionBestEffort(logPrefix, decision, policy.ValidationDecisionHandlers{
				Warn: func(msg string) {
					result.Warnings = append(result.Warnings, fmt.Sprintf("ValidationPolicy warning: %s", msg))
				},
				ApplyPenalty: func(_ float64, reason string) {
					if reason != "" {
						result.Warnings = append(result.Warnings, reason)
					}
				},
				SetPolicyStatus: func(status, reason string, penalty float64) {
					fragment.SetPolicyStatus(status, reason, penalty)
				},
			})
			v.recordPolicyDecisionTelemetry(ctx, fragment, decision)
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

	// Normalize market paths: collapse domain==vertical to prevent duplicates
	// (e.g. /market/iq/agriculture/agriculture → /market/iq/agriculture)
	if fragment.Class == ah.FragmentClassMarket && strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/market/") {
		fragment.CanonicalPath = collapseMarketDomainEqualVertical(fragment.CanonicalPath)
	}

	// ==========================================================================
	// Policy 1.5: Validate canonical path format (fail-open with deterministic repair)
	// ==========================================================================
	trimmedCanonicalPath := strings.TrimSpace(fragment.CanonicalPath)
	if !fragment.IsSupplyLinkFragment() {
		if err := taxonomy.ValidateCanonicalPath(trimmedCanonicalPath); err != nil {
			// Keep scouting pipeline fail-open: try deterministic repair before any hard block.
			if fixed, ok := repairInvalidCanonicalPath(fragment); ok {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("canonical path validation failed (%v), repaired to %q", err, fixed))
			} else {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("canonical path validation failed (non-blocking): %v", err))
			}
		}
	}

	// ==========================================================================
	// Policy 2: Enforce canonical path prefixes by type
	// ==========================================================================
	// Each fragment type should use its designated path prefix.
	prefix := policy.ExpectedCanonicalPathPrefix(fragment.Class, fragment.Subtype)
	if !policy.IsCanonicalPathAllowedForType(fragment.Class, fragment.Subtype, fragment.CanonicalPath) {
		// Best-effort auto-correction for obvious namespace/type mismatch.
		if fixed, changed := coerceCanonicalPathForType(fragment.CanonicalPath, fragment.Class, firstNonEmpty(
			strings.TrimSpace(fragment.Identifiers["legalName"]),
			strings.TrimSpace(fragment.Labels["en"]),
			strings.TrimSpace(fragment.Identifiers["shortName"]),
		)); changed && strings.TrimSpace(fixed) != "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("canonical path %q auto-corrected to %q for type %s",
					fragment.CanonicalPath, fixed, fragment.Class))
			fragment.CanonicalPath = fixed
		}
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("canonical path %q does not start with expected prefix %q for type %s",
				fragment.CanonicalPath, prefix, fragment.Class))
	}

	// Organization canonical paths must be strict: /org/{iso2|na|int|regional}/{slug}.
	if fragment.Class == ah.FragmentClassOrg {
		cp := strings.TrimSpace(fragment.CanonicalPath)
		if strings.HasPrefix(cp, "/org/") {
			parts := strings.Split(strings.Trim(cp, "/"), "/")
			if len(parts) != 3 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("non-canonical org path %q: expected /org/{geo}/{slug}", cp))
			}
		}
		// gov paths: enforce /gov/{iso2|na}/{slug} shape best-effort (warnings only; hard errors are handled by ValidateCanonicalPath elsewhere).
		if strings.HasPrefix(cp, "/gov/") {
			parts := strings.Split(strings.Trim(cp, "/"), "/")
			if len(parts) != 3 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("gov path %q should be /gov/{iso2|na}/{slug}", cp))
			}
		}
	}

	// ==========================================================================
	// Policy 3: Warn about non-canonical facility paths
	// ==========================================================================
	// Facilities should be nested under their parent organization:
	// Good: /fac/us/gigafactory-texas (or /fac/na/gigafactory-texas when geo unknown)
	if fragment.Class == ah.FragmentClassSite {
		pathParts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(pathParts) != 3 || pathParts[0] != "fac" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("non-canonical facility path %q - expected /fac/{geo}/{facility}", fragment.CanonicalPath))
		} else if strings.EqualFold(strings.TrimSpace(pathParts[1]), "na") && canonical.FacilitySlugLowInformation(strings.TrimSpace(pathParts[2])) {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("low-information /fac/na slug %q is collision-prone; prefer operator/site-specific facility slug or stronger geo grounding", strings.TrimSpace(pathParts[2])))
			v.recordFacilityIdentityTelemetry(ctx, fragment, strings.TrimSpace(pathParts[2]), "low_information_na_slug")
		}
	}

	// ==========================================================================
	// Policy 4: GEO_REGION must use /loc/ prefix
	// ==========================================================================
	// Legacy /region/ paths should be migrated to /loc/.
	// Note: GEO_REGION validation issues are warnings, not blockers (can be fixed post-factum)
	if fragment.Class == ah.FragmentClassGeo {
		if strings.HasPrefix(fragment.CanonicalPath, "/region/") {
			// NOTE: legacy /region/ had multiple shapes (including /region/na/{cc}/{admin1}/{city}).
			// Do NOT do a naive prefix replace; normalize into canonical /loc hierarchy.
			if norm := normalizeLegacyGeoPath(fragment.CanonicalPath); norm != "" {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("legacy /region/ path %q normalized to %q", fragment.CanonicalPath, norm))
				fragment.CanonicalPath = norm
			} else {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("legacy GEO_REGION canonicalPath %q cannot be normalized to /loc/{country}[/{level1}[/{level2}]] - will be fixed post-factum", fragment.CanonicalPath))
			}
		}

		if !strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("GEO_REGION canonicalPath %q should use /loc/ prefix - will be fixed post-factum", fragment.CanonicalPath))
		} else {
			// Best-effort: normalize any /loc path into variable-depth canonical shape.
			if norm := normalizeGeoPathToStrict(fragment.CanonicalPath); norm != "" && norm != fragment.CanonicalPath {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("non-canonical GEO path %q → %q", fragment.CanonicalPath, norm))
				fragment.CanonicalPath = norm
				if v.geoBuilder != nil && v.geoBuilder.fragmentsRepo != nil {
					existing, err := v.geoBuilder.fragmentsRepo.GetByCanonicalPath(context.Background(), norm)
					if err == nil && existing != nil && existing.ID != fragment.ID {
						result.ReplacementFrag = existing
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("found existing GEO_REGION %q (%s) - using instead", existing.CanonicalPath, existing.ID))
					}
				}
			}
			cc, _, _, ok := taxonomy.ParseLocCanonicalPath(fragment.CanonicalPath)
			if !ok {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("GEO_REGION canonicalPath %q should match /loc/{country}[/{level1}[/{level2}]] - will be fixed post-factum", fragment.CanonicalPath))
			} else if !taxonomy.IsValidLocCountryToken(cc) {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("GEO_REGION canonicalPath %q has invalid country/region %q - will be fixed post-factum", fragment.CanonicalPath, cc))
			} else {
				log.Printf("%s GEO_REGION validation passed: %s", logPrefix, fragment.CanonicalPath)
			}
		}

		// Best-effort: attach GeoNames ID to GEO_REGION fragments (fragment-only; no Entity writes).
		// This makes geo fragments linkable externally (like Wikidata), while taxonomy still drives hierarchy.
		if v.geonamesClient != nil && strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			cc, level1, level2, ok := taxonomy.ParseLocCanonicalPath(fragment.CanonicalPath)
			if ok && taxonomy.IsISO2CountryCode(cc) {
				adm := strings.ToLower(strings.TrimSpace(level1))
				citySlug := strings.ToLower(strings.TrimSpace(level2))
				if citySlug == "" {
					citySlug = strings.ToLower(strings.TrimSpace(level1))
					adm = ""
				}
				if citySlug != "" {
					cityName := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(citySlug))
					if cityName != "" {
						if best, ok := pickUnambiguousCityCandidate(ctx, v.geonamesClient, cityName, cc, adm); ok && best != nil && best.GeoNameID > 0 {
							if fragment.Identifiers == nil {
								fragment.Identifiers = map[string]string{}
							}
							fragment.Identifiers["geonamesId"] = strconv.Itoa(best.GeoNameID)
							// Also keep KBRefs mirrored (Validate() will ensure consistency).
							has := false
							for i := range fragment.KBRefs {
								if strings.EqualFold(strings.TrimSpace(fragment.KBRefs[i].KB), "GeoNames") {
									has = true
									fragment.KBRefs[i].KB = "GeoNames"
									if strings.TrimSpace(fragment.KBRefs[i].ExternalID) == "" {
										fragment.KBRefs[i].ExternalID = strconv.Itoa(best.GeoNameID)
									}
									break
								}
							}
							if !has {
								fragment.KBRefs = append(fragment.KBRefs, models.KBReference{
									KB:         "GeoNames",
									ExternalID: strconv.Itoa(best.GeoNameID),
									Confidence: 1.0,
								})
							}
						}
					}
				}
			}
		}
	}

	// ==========================================================================
	// Policy 4.5: Conservative Wikidata enrichment (QID-known only)
	// ==========================================================================
	// Optional: resolve missing Wikidata QID via search/scoring (feature-flagged; default OFF).
	// If enabled and confident, we attach wikidataId BEFORE enrichment/dedup so the rest of the pipeline becomes deterministic.
	if v.wikidataClient != nil {
		qr, _, attached, err := henrich.ResolveAndAttachWikidataQIDIfMissing(ctx, v.wikidataClient, fragment, henrich.QIDResolveOptionsFromEnv())
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("wikidata qid resolve skipped: %v", err))
		} else if attached && qr != nil && strings.TrimSpace(qr.QID) != "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("wikidata qid resolved: %s (score=%d, query=%q)", strings.TrimSpace(qr.QID), qr.Score, qr.Query))
		}
	}
	// If fragment already has a Wikidata ID, enrich:
	// - labels.en + description
	// - identifiers.legalName (P1448) + identifiers.shortName (P1813)
	// - industryPaths from P452 (mapped via runtime industry dictionary)
	//
	// IMPORTANT: We do NOT run search here (unless the feature-flagged resolver above attached a QID).
	if v.wikidataClient != nil {
		if _, err := henrich.EnrichFragmentFromWikidataIfPresent(ctx, v.wikidataClient, fragment, henrich.ConservativeWikidataOptions{
			Force:              true,
			Overwrite:          true,
			DisableCoordinates: true,
		}); err != nil {
			// Best-effort: do not block creation.
			result.Warnings = append(result.Warnings, fmt.Sprintf("wikidata enrichment skipped: %v", err))
		}
	}

	// Policy 4.6: Facility geo revalidation before generic coordinate enrichment.
	// This blocks a common debt pattern: facility path/homeLocation country is guessed
	// incorrectly, then survives forever as /fac/na/... or with the wrong ISO2.
	if fragment.Class == ah.FragmentClassSite && v.geocodingClient != nil {
		if revalidated, err := RevalidateFacilityGeo(ctx, fragment, v.geocodingClient); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("facility geo revalidation skipped: %v", err))
		} else if revalidated.Applied {
			msg := revalidated.Reason
			if strings.TrimSpace(revalidated.PreviousISO2) != "" && strings.TrimSpace(revalidated.ResolvedISO2) != "" &&
				!strings.EqualFold(revalidated.PreviousISO2, revalidated.ResolvedISO2) {
				msg += fmt.Sprintf(" (%s -> %s)", strings.ToUpper(strings.TrimSpace(revalidated.PreviousISO2)), strings.ToUpper(strings.TrimSpace(revalidated.ResolvedISO2)))
			}
			if strings.TrimSpace(revalidated.Query) != "" {
				msg += fmt.Sprintf(" via %q", revalidated.Query)
			}
			if strings.TrimSpace(revalidated.Provider) != "" {
				msg += fmt.Sprintf(" [%s]", revalidated.Provider)
			}
			result.Warnings = append(result.Warnings, msg)
		}
	}
	if fragment.Class == ah.FragmentClassOrg && v.geocodingClient != nil {
		if revalidated, err := RevalidateOrgGeo(ctx, fragment, v.geocodingClient); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("organization geo revalidation skipped: %v", err))
		} else if revalidated.Applied {
			result.Warnings = append(result.Warnings, OrgGeoWarning(revalidated))
		}
	}

	// Policy 4.7: Geocoding fallback for org/facility/gov when coordinates still missing
	if v.geocodingClient != nil {
		if enriched, err := henrich.EnrichCoordinatesFromGeocoding(ctx, v.geocodingClient, fragment); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("geocoding skipped: %v", err))
		} else if enriched {
			msg := "coordinates resolved via geocoding"
			if provider := strings.TrimSpace(fragment.Identifiers["geoCoordinatesProvider"]); provider != "" {
				msg += fmt.Sprintf(" [%s]", provider)
			}
			result.Warnings = append(result.Warnings, msg)
		}
	}
	if upgraded := maybeUpgradeFacilityPathFromHomeLocation(fragment); upgraded != "" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("facility path upgraded from jurisdiction-unknown to %q", upgraded))
	}

	// ==========================================================================
	// Policy 4: Auto-fill facilityKind from canonical path slug when missing
	// ==========================================================================
	if fragment.Class == ah.FragmentClassSite && strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/fac/") {
		if fragment.Identifiers == nil {
			fragment.Identifiers = map[string]string{}
		}
		currentRaw := strings.TrimSpace(fragment.Identifiers["facilityKind"])
		currentNorm := taxonomy.NormalizeFacilityKind(currentRaw)
		if currentNorm == taxonomy.FacilityKindUnknown || currentNorm == taxonomy.FacilityKindOther {
			kind := taxonomy.InferFacilityKind(
				fragment.CanonicalPath,
				fragment.Labels["en"],
				fragment.Description,
				fragment.EntityPath,
				strings.Join(fragment.IndustryPaths, " "),
				fragment.Identifiers["facilityType"],
				fragment.Identifiers["supplyChainRole"],
			)
			if kind != taxonomy.FacilityKindUnknown {
				fragment.Identifiers["facilityKind"] = string(kind)
				if kind == taxonomy.FacilityKindOther {
					result.Warnings = append(result.Warnings, "set facilityKind=other (no deterministic subtype signal)")
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("inferred facilityKind=%s from canonical path and context", kind))
				}
			}
		}
	}

	// ==========================================================================
	// Policy 4.5: Facility deduplication
	// ==========================================================================
	// Check if this facility already exists (by path, label, shortName, or normalized name in same geo).
	if fragment.Class == ah.FragmentClassSite && v.facilityDedup != nil {
		existing, err := v.facilityDedup.DeduplicateFacility(ctx, fragment)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			result.ReplacementFrag = existing
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("found existing facility %q (%s) - using instead of creating new",
					existing.CanonicalPath, existing.ID))
		}
	}
	if fragment.Class == ah.FragmentClassSite && result.ReplacementFrag == nil {
		if repairedPath, reason, ok := RepairSyntheticFacilityCanonicalPath(ctx, fragment, v.geocodingClient); ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %q", reason, repairedPath))
			fragment.CanonicalPath = repairedPath
		}
		if slug, blocked := forbiddenPlaceholderFacilitySlug(fragment.CanonicalPath); blocked {
			v.recordFacilityIdentityTelemetry(ctx, fragment, slug, "forbidden_placeholder_slug")
			result.IsValid = false
			result.BlockReason = fmt.Sprintf("forbidden /fac/ placeholder slug %q; do not create synthetic general facility nodes, keep org->product or provide concrete facility identity/geo", slug)
		}
	}

	// ==========================================================================
	// Policy 5: Organization deduplication
	// ==========================================================================
	// Check if this organization already exists (by Wikidata ID or shortName).
	if fragment.Class == ah.FragmentClassOrg && v.orgDedup != nil {
		existing, err := v.orgDedup.DeduplicateOrg(ctx, fragment)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			result.ReplacementFrag = existing
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("found existing organization %q (%s) - using instead of creating new",
					existing.CanonicalPath, existing.ID))
		}
	}
	if fragment.Class == ah.FragmentClassOrg && result.ReplacementFrag == nil {
		if repairedPath, reason, ok := RepairSyntheticOrgCanonicalPath(fragment); ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %q", reason, repairedPath))
			fragment.CanonicalPath = repairedPath
		}
		if cp := strings.TrimSpace(fragment.CanonicalPath); strings.HasPrefix(cp, "/org/") {
			if slug := lastPathSlug(cp); canonical.OrgSlugForbiddenPlaceholder(slug) {
				result.IsValid = false
				result.BlockReason = fmt.Sprintf("forbidden synthetic /org/ placeholder slug %q; provide a concrete organization identity or move the meaning into contract/path context", slug)
			}
		}
		if cp := strings.TrimSpace(fragment.CanonicalPath); strings.HasPrefix(cp, "/gov/") {
			if slug := lastPathSlug(cp); canonical.GovSlugForbiddenPlaceholder(slug) {
				result.IsValid = false
				result.BlockReason = fmt.Sprintf("forbidden synthetic /gov/ placeholder slug %q; provide a concrete jurisdictional authority or keep it as policy/context metadata", slug)
			}
		}
	}

	// ==========================================================================
	// Policy 6: Product deduplication
	// ==========================================================================
	// Check if this product already exists (by Wikidata ID, CAS number, or chemical formula).
	if fragment.Class == ah.FragmentClassProduct && v.productDedup != nil {
		existing, err := v.productDedup.DeduplicateProduct(ctx, fragment)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			result.ReplacementFrag = existing
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("found existing product %q (%s) - using instead of creating new",
					existing.CanonicalPath, existing.ID))
		}
	}

	// ==========================================================================
	// Policy 7: Person deduplication (includes phonetic matching)
	// ==========================================================================
	// Check if this person already exists (by Wikidata ID, name, or phonetic match).
	// Phonetic matching catches Arabic transliteration variations: Tahnoon/Tahnoun, Khalid/Halid, etc.
	if fragment.Class == ah.FragmentClassPerson && v.personDedup != nil {
		existing, err := v.personDedup.DeduplicatePerson(ctx, fragment)
		if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID {
			result.ReplacementFrag = existing
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("found existing person %q (%s) - using instead of creating new (phonetic/exact match)",
					existing.CanonicalPath, existing.ID))
		}
	}

	// ==========================================================================
	// Policy 7.5: Market deduplication (tag-based, auto when overlap >= 5)
	// ==========================================================================
	if fragment.Class == ah.FragmentClassMarket && v.geoBuilder != nil && v.geoBuilder.fragmentsRepo != nil {
		fragment.Tags = taxonomy.NormalizeMarketTags(fragment.Tags)
		if len(fragment.Tags) >= 5 {
			geo, sector, vertical := parseMarketGeoSectorForDedup(fragment.CanonicalPath)
			if geo != "" && sector != "" && vertical != "" {
				searchTokens := taxonomy.MarketTagSearchTokens(fragment.Tags, 10)
				if len(searchTokens) > 0 {
					candidates, err := v.geoBuilder.fragmentsRepo.FindByTags(ctx, searchTokens, 50)
					if err == nil && len(candidates) > 0 {
						fragTagSet := map[string]struct{}{}
						for _, t := range fragment.Tags {
							fragTagSet[t] = struct{}{}
						}
						var best *models.HorizonFragment
						bestOverlap := 0
						for _, cand := range candidates {
							if cand == nil || cand.ID == "" || cand.ID == fragment.ID {
								continue
							}
							if cand.Class != ah.FragmentClassMarket && !strings.HasPrefix(strings.TrimSpace(cand.CanonicalPath), "/market/") {
								continue
							}
							cGeo, cSector, cVertical := parseMarketGeoSectorForDedup(cand.CanonicalPath)
							if cGeo == "" || cSector == "" || cVertical == "" {
								continue
							}
							// Strict scope match: same geo + domain + vertical.
							// This prevents false merges between neighboring sub-markets in one domain.
							if cGeo != geo || cSector != sector || cVertical != vertical {
								continue
							}
							candTags := taxonomy.NormalizeMarketTags(cand.Tags)
							if len(candTags) == 0 {
								continue
							}
							overlap := 0
							for _, t := range candTags {
								if _, ok := fragTagSet[t]; ok {
									overlap++
								}
							}
							if overlap >= 5 && overlap > bestOverlap {
								best = cand
								bestOverlap = overlap
							}
						}
						if best != nil && strings.TrimSpace(best.CanonicalPath) != "" {
							fragment.CanonicalPath = strings.TrimSpace(best.CanonicalPath)
							result.ReplacementFrag = best
							result.Warnings = append(result.Warnings,
								fmt.Sprintf("found existing market %q (%s) - using instead of creating new (tag overlap=%d)",
									best.CanonicalPath, best.ID, bestOverlap))
						}
					}
				}
			}
		}
	}

	// ==========================================================================
	// Policy 7.6: Skeletal AFFECTED_ZONE twin reuse
	// ==========================================================================
	// Zones are uniquely path-scoped by condition_type, so the same place can drift into
	// multiple thin twins (e.g. /political_tension/... and /military_conflict/...).
	// Reuse an existing richer sibling only when the incoming zone is clearly skeletal.
	if fragment.IsAffectedZoneGeo() && v.geoBuilder != nil && v.geoBuilder.fragmentsRepo != nil && result.ReplacementFrag == nil {
		candidates, err := FindAffectedZoneTwinCandidates(ctx, v.geoBuilder.fragmentsRepo, fragment.CanonicalPath, fragment.ID, 128)
		if err == nil && len(candidates) > 0 {
			if existing, reason := SelectAffectedZoneTwinReplacement(fragment, candidates); existing != nil {
				result.ReplacementFrag = existing
				result.Warnings = append(result.Warnings, reason)
			}
		}
	}

	// ==========================================================================
	// Policy 8: Required fields validation
	// ==========================================================================
	if strings.TrimSpace(fragment.CanonicalPath) == "" {
		result.BlockReason = "canonical path is required"
		result.IsValid = false
		return result, nil
	}

	if fragment.Class == "" {
		result.BlockReason = "fragment type is required"
		result.IsValid = false
		return result, nil
	}
	if result.ReplacementFrag != nil && !isReplacementCompatible(fragment, result.ReplacementFrag) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("replacement %q (%s) is incompatible with requested %q (%s) - ignored",
				strings.TrimSpace(result.ReplacementFrag.CanonicalPath), result.ReplacementFrag.Class,
				strings.TrimSpace(fragment.CanonicalPath), fragment.Class))
		result.ReplacementFrag = nil
	}

	return result, nil
}

func (v *PreCreateValidator) recordPolicyDecisionTelemetry(
	ctx context.Context,
	fragment *models.HorizonFragment,
	decision policy.PolicyDecision,
) {
	if v == nil || v.telemetryRepo == nil || fragment == nil {
		return
	}
	tenantID := strings.TrimSpace(fragment.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	snapshot := policy.BuildDecisionTelemetrySnapshot(policy.DecisionTelemetryInput{
		Kind:        "fragment",
		Decision:    decision,
		SourceType:  string(fragment.Class),
		FinalStatus: fragment.GetPolicyStatus(),
		FinalReason: fragment.GetPolicyReason(),
	})
	if len(snapshot.Counters) == 0 {
		return
	}
	_ = v.telemetryRepo.Increment(
		ctx,
		tenantID,
		"edge_policy",
		"kpi_rates",
		time.Hour,
		map[string]string{"kind": "fragment"},
		snapshot.Counters,
	)
}

func parseMarketGeoSectorForDedup(canonicalPath string) (string, string, string) {
	return parseMarketDedupScope(canonicalPath)
}

func forbiddenPlaceholderFacilitySlug(canonicalPath string) (string, bool) {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/fac/") {
		return "", false
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) != 3 {
		return "", false
	}
	slug := strings.TrimSpace(parts[2])
	if slug == "" {
		return "", false
	}
	return slug, canonical.FacilitySlugHasSyntheticTail(slug)
}

func maybeUpgradeFacilityPathFromHomeLocation(fragment *models.HorizonFragment) string {
	if newPath, ok := canonical.InferFacilityCanonicalPathFromGeo(fragment); ok {
		fragment.CanonicalPath = newPath
		return newPath
	}
	return ""
}

func (v *PreCreateValidator) recordFacilityIdentityTelemetry(
	ctx context.Context,
	fragment *models.HorizonFragment,
	slug string,
	reason string,
) {
	if v == nil || v.telemetryRepo == nil || fragment == nil {
		return
	}
	tenantID := strings.TrimSpace(fragment.TenantID)
	if tenantID == "" {
		tenantID = "global"
	}
	dim := map[string]string{
		"kind":   "facility",
		"reason": strings.TrimSpace(reason),
	}
	if slug != "" {
		dim["slug"] = strings.TrimSpace(slug)
	}
	_ = v.telemetryRepo.Increment(
		ctx,
		tenantID,
		"facility_identity",
		"kpi_rates",
		time.Hour,
		dim,
		map[string]int64{"collision_prone_total": 1},
	)
}

// isFinancialProductContext returns true if the fragment suggests a financial product
// (path prefix, labels, or identifiers indicate finance/banking/investment context).
// Used to avoid coercing financial products into generic mechanical/assembly paths.
func isFinancialProductContext(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	if strings.HasPrefix(cp, "/product/finance/") || strings.HasPrefix(cp, "/product/financial/") {
		return true
	}
	joined := cp + " " +
		strings.ToLower(strings.TrimSpace(fragment.Labels["en"])) + " " +
		strings.ToLower(strings.TrimSpace(fragment.Identifiers["legalName"])) + " " +
		strings.ToLower(strings.TrimSpace(fragment.Identifiers["shortName"]))
	keywords := []string{
		"equity", "equity-investment", "investment", "shareholding", "loan", "credit", "bond",
		"securities", "derivative", "fund", "deposit", "capital", "financing", "banking",
	}
	for _, kw := range keywords {
		if strings.Contains(joined, kw) {
			return true
		}
	}
	return false
}

// repairInvalidCanonicalPath tries to deterministically map an invalid canonical path
// into a valid, type-compatible fallback path so creation can proceed without breaking
// the scouting -> research -> critic -> policy chain.
func repairInvalidCanonicalPath(fragment *models.HorizonFragment) (string, bool) {
	if fragment == nil || fragment.Class == "" {
		return "", false
	}

	seed := firstNonEmpty(
		strings.TrimSpace(fragment.Identifiers["legalName"]),
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Identifiers["shortName"]),
		lastPathSlug(strings.TrimSpace(fragment.CanonicalPath)),
	)
	slug := taxonomy.Slugify(seed)
	if slug == "" {
		slug = "unknown"
	}

	var candidate string
	switch {
	case fragment.IsProgramOrg():
		candidate = taxonomy.JoinPath("program", "global", slug)
	case fragment.IsMediaChannelOrg():
		candidate = taxonomy.JoinPath("media", slug)
	case fragment.IsDepositSite():
		candidate = taxonomy.JoinPath("deposit", "other", slug)
	case fragment.IsDomainScopeFragment():
		candidate = taxonomy.JoinPath("domain", "v1", "other", slug)
	case fragment.IsSphereScopeFragment():
		candidate = taxonomy.JoinPath("sphere", slug)
	case fragment.Class == ah.FragmentClassOrg:
		candidate = taxonomy.JoinPath("org", "na", slug)
	case fragment.Class == ah.FragmentClassSite:
		candidate = taxonomy.JoinPath("fac", "na", slug)
	case fragment.Class == ah.FragmentClassGeo:
		candidate = taxonomy.JoinPath("loc", "na", "na", slug)
	case fragment.Class == ah.FragmentClassProduct:
		if isFinancialProductContext(fragment) {
			candidate = taxonomy.JoinPath("product", "service", "financial", "na", slug)
		} else {
			candidate = taxonomy.JoinPath("product", "finished", "mechanical", "assembly", slug)
		}
	case fragment.Class == ah.FragmentClassMarket:
		candidate = taxonomy.JoinPath("market", "global", slug)
	case fragment.Class == ah.FragmentClassPerson:
		candidate = taxonomy.JoinPath("person", slug)
	case fragment.Class == ah.FragmentClassDecision:
		candidate = taxonomy.JoinPath("decision", "general", slug)
	case fragment.Class == ah.FragmentClassMechanism:
		candidate = taxonomy.JoinPath("mechanism", "political", slug)
	case fragment.Class == ah.FragmentClassCommunity:
		candidate = taxonomy.JoinPath("community", "global", "interest", slug)
	case fragment.Class == ah.FragmentClassEvidence:
		candidate = taxonomy.JoinPath("event", slug)
	default:
		return "", false
	}

	if err := taxonomy.ValidateCanonicalPath(candidate); err != nil {
		return "", false
	}
	fragment.CanonicalPath = candidate
	return candidate, true
}

func canonicalNamespaceFamily(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if cp == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	ns := strings.ToLower(strings.TrimSpace(parts[0]))
	switch ns {
	case "gov":
		return "org"
	case "region":
		return "loc"
	default:
		return ns
	}
}

func fragmentTypeForCompatibility(f *models.HorizonFragment) FragmentType {
	if f == nil {
		return ""
	}
	if strings.TrimSpace(string(f.Class)) != "" {
		return f.Class
	}
	return InferFragmentTypeFromCanonicalPath(strings.TrimSpace(f.CanonicalPath))
}

func isReplacementCompatible(requested *models.HorizonFragment, replacement *models.HorizonFragment) bool {
	if requested == nil || replacement == nil {
		return false
	}
	reqType := fragmentTypeForCompatibility(requested)
	repType := fragmentTypeForCompatibility(replacement)
	if reqType != "" && repType != "" && reqType != repType {
		return false
	}
	reqNS := canonicalNamespaceFamily(requested.CanonicalPath)
	repNS := canonicalNamespaceFamily(replacement.CanonicalPath)
	if reqNS != "" && repNS != "" && reqNS != repNS {
		return false
	}
	return true
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
var CanonicalPathPolicies = map[FragmentType]CanonicalPathPolicy{
	ah.FragmentClassOrg: {
		Prefix:      "/org/",
		Example:     "/org/us/tesla",
		Description: "Organization paths are jurisdiction-scoped: /org/{iso2|na}/{slug}. Government bodies use /gov/{iso2|na}/{slug}",
	},
	ah.FragmentClassSite: {
		Prefix:      "/fac/",
		Example:     "/fac/us/gigafactory-texas",
		Description: "Facility paths are nested under parent organization",
	},
	ah.FragmentClassGeo: {
		Prefix:      "/loc/",
		Example:     "/loc/us/tx/austin",
		Description: "Geographic paths use /loc/{country}[/{level1}[/{level2}]] format",
	},
	ah.FragmentClassProduct: {
		Prefix:      "/product/",
		Example:     "/product/finished/mechanical/assembly/model-3",
		Description: "Product paths use strict taxonomy: /product/{category}/{type}/{stage}/{slug}",
	},
	ah.FragmentClassPerson: {
		Prefix:      "/person/",
		Example:     "/person/elon-musk",
		Description: "Person paths use lowercase hyphenated names",
	},
	// MIGRATION NOTE: legacy ah.FragmentClassOrg collapses onto
	// (org, media_channel). The map key cannot represent the subtype, so
	// the /media/ prefix description has been folded into the org entry's
	// note for callers reading this variable. New /media/{platform}/{slug}
	// callers should look up the prefix via policy.ExpectedCanonicalPathPrefix
	// with Subtype="media_channel" instead of this Class-keyed map.
	ah.FragmentClassDecision: {
		Prefix:      "/decision/",
		Example:     "/decision/ai-chips/euv-export-control",
		Description: "Decision points are domain-scoped: /decision/{domain-slug}/{decision-slug}",
	},
	ah.FragmentClassMechanism: {
		Prefix:      "/mechanism/",
		Example:     "/mechanism/regulatory/export-control",
		Description: "Mechanisms are reusable control tools: /mechanism/{category}/{slug}",
	},
}

func (v *PreCreateValidator) String() string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("PreCreateValidator(geo=%v, orgDedup=%v, personDedup=%v, facilityDedup=%v)", v.geoBuilder != nil, v.orgDedup != nil, v.personDedup != nil, v.facilityDedup != nil)
}
