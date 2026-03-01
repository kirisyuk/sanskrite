package scout

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	henrich "github.com/igorkirisyuk/sanskrite/internal/horizon/enrich"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var reCountryAlpha2 = regexp.MustCompile(`^[a-z]{2}$`)

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
	if fragment.Type == models.FragmentTypeMarket && strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/market/") {
		fragment.CanonicalPath = collapseMarketDomainEqualVertical(fragment.CanonicalPath)
	}

	// ==========================================================================
	// Policy 1.5: Validate canonical path format (fail-open with deterministic repair)
	// ==========================================================================
	trimmedCanonicalPath := strings.TrimSpace(fragment.CanonicalPath)
	if fragment.Type != models.FragmentTypeSupplyLink {
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
	prefix := policy.ExpectedCanonicalPathPrefix(fragment.Type)
	if !policy.IsCanonicalPathAllowedForType(fragment.Type, fragment.CanonicalPath) {
		// Best-effort auto-correction for obvious namespace/type mismatch.
		if fixed, changed := coerceCanonicalPathForType(fragment.CanonicalPath, fragment.Type, firstNonEmpty(
			strings.TrimSpace(fragment.Identifiers["legalName"]),
			strings.TrimSpace(fragment.Labels["en"]),
			strings.TrimSpace(fragment.Identifiers["shortName"]),
		)); changed && strings.TrimSpace(fixed) != "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("canonical path %q auto-corrected to %q for type %s",
					fragment.CanonicalPath, fixed, fragment.Type))
			fragment.CanonicalPath = fixed
		}
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("canonical path %q does not start with expected prefix %q for type %s",
				fragment.CanonicalPath, prefix, fragment.Type))
	}

	// Organization canonical paths must be strict: /org/{iso2|na|int|regional}/{slug}.
	if fragment.Type == models.FragmentTypeOrganization {
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
	// Good: /fac/tesla/gigafactory-texas (or /fac/na/gigafactory-texas when owner unknown)
	if fragment.Type == models.FragmentTypeFacility {
		pathParts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
		if len(pathParts) != 3 || pathParts[0] != "fac" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("non-canonical facility path %q - expected /fac/{org}/{facility}", fragment.CanonicalPath))
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
			// Best-effort: strictify any /loc path (e.g., /loc/id/na -> /loc/id/na/na) before validation.
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

		// Best-effort: attach GeoNames ID to GEO_REGION fragments (fragment-only; no Entity writes).
		// This makes geo fragments linkable externally (like Wikidata), while taxonomy still drives hierarchy.
		if v.geonamesClient != nil && strings.HasPrefix(fragment.CanonicalPath, "/loc/") {
			parts := strings.Split(strings.Trim(fragment.CanonicalPath, "/"), "/")
			// Only attach for strict /loc/{cc}/{adm1}/{city} when city is present.
			if len(parts) == 4 && parts[0] == "loc" && reCountryAlpha2.MatchString(parts[1]) && strings.TrimSpace(parts[3]) != "" && strings.ToLower(strings.TrimSpace(parts[3])) != "na" {
				cc := strings.ToLower(strings.TrimSpace(parts[1]))
				adm := strings.ToLower(strings.TrimSpace(parts[2]))
				citySlug := strings.ToLower(strings.TrimSpace(parts[3]))
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
						attachGeoCoordinatesFromGeoNames(fragment, best)
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
			Force:     true,
			Overwrite: true,
		}); err != nil {
			// Best-effort: do not block creation.
			result.Warnings = append(result.Warnings, fmt.Sprintf("wikidata enrichment skipped: %v", err))
		}
	}

	// Policy 4.6: Geocoding fallback for org/facility/gov when coordinates still missing
	if v.geocodingClient != nil {
		if enriched, err := henrich.EnrichCoordinatesFromGeocoding(ctx, v.geocodingClient, fragment); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("geocoding skipped: %v", err))
		} else if enriched {
			result.Warnings = append(result.Warnings, "coordinates resolved via geocoding")
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
	// Policy 7: Person deduplication (includes phonetic matching)
	// ==========================================================================
	// Check if this person already exists (by Wikidata ID, name, or phonetic match).
	// Phonetic matching catches Arabic transliteration variations: Tahnoon/Tahnoun, Khalid/Halid, etc.
	if fragment.Type == models.FragmentTypePerson && v.personDedup != nil {
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
	if fragment.Type == models.FragmentTypeMarket && v.geoBuilder != nil && v.geoBuilder.fragmentsRepo != nil {
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
							if cand.Type != models.FragmentTypeMarket && !strings.HasPrefix(strings.TrimSpace(cand.CanonicalPath), "/market/") {
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
	// Policy 8: Required fields validation
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
	if result.ReplacementFrag != nil && !isReplacementCompatible(fragment, result.ReplacementFrag) {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("replacement %q (%s) is incompatible with requested %q (%s) - ignored",
				strings.TrimSpace(result.ReplacementFrag.CanonicalPath), result.ReplacementFrag.Type,
				strings.TrimSpace(fragment.CanonicalPath), fragment.Type))
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
		SourceType:  string(fragment.Type),
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

func attachGeoCoordinatesFromGeoNames(fragment *models.HorizonFragment, place *coreontology.GeoNamesPlace) {
	if fragment == nil || place == nil {
		return
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(place.Lat), 64)
	if err != nil {
		return
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(place.Lng), 64)
	if err != nil {
		return
	}
	if !models.IsValidCoordinates(lat, lon) {
		return
	}

	if fragment.Coordinates == nil || !models.IsValidCoordinates(fragment.Coordinates.Lat, fragment.Coordinates.Lon) {
		fragment.Coordinates = &models.Coordinates{
			Lat: lat,
			Lon: lon,
		}
	}

	if fragment.HomeLocation == nil {
		fragment.HomeLocation = &models.Location{}
	}
	iso := fmt.Sprintf("%+.6f%+.6f/", lat, lon)
	if fragment.HomeLocation.ISO6709 == nil || strings.TrimSpace(*fragment.HomeLocation.ISO6709) == "" {
		fragment.HomeLocation.ISO6709 = &iso
	}
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
	if fragment == nil || fragment.Type == "" {
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
	switch fragment.Type {
	case models.FragmentTypeOrganization:
		candidate = taxonomy.JoinPath("org", "na", slug)
	case models.FragmentTypeFacility:
		candidate = taxonomy.JoinPath("fac", "na", slug)
	case models.FragmentTypeGeoRegion:
		candidate = taxonomy.JoinPath("loc", "na", "na", slug)
	case models.FragmentTypeProduct:
		if isFinancialProductContext(fragment) {
			candidate = taxonomy.JoinPath("product", "service", "financial", "na", slug)
		} else {
			candidate = taxonomy.JoinPath("product", "finished", "mechanical", "assembly", slug)
		}
	case models.FragmentTypeProgram:
		candidate = taxonomy.JoinPath("program", "global", slug)
	case models.FragmentTypeMarket:
		candidate = taxonomy.JoinPath("market", "global", slug)
	case models.FragmentTypePerson:
		candidate = taxonomy.JoinPath("person", slug)
	case models.FragmentTypeMediaChannel:
		candidate = taxonomy.JoinPath("media", slug)
	case models.FragmentTypeDecisionPoint:
		candidate = taxonomy.JoinPath("decision", "general", slug)
	case models.FragmentTypeMechanism:
		candidate = taxonomy.JoinPath("mechanism", "political", slug)
	case models.FragmentTypeDomain:
		candidate = taxonomy.JoinPath("domain", "v1", "other", slug)
	case models.FragmentTypeDeposit:
		candidate = taxonomy.JoinPath("deposit", "other", slug)
	case models.FragmentTypeCommunity:
		candidate = taxonomy.JoinPath("community", "global", "interest", slug)
	case models.FragmentTypeEvent:
		candidate = taxonomy.JoinPath("event", slug)
	case models.FragmentTypeSphere:
		candidate = taxonomy.JoinPath("sphere", slug)
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

func fragmentTypeForCompatibility(f *models.HorizonFragment) models.FragmentType {
	if f == nil {
		return ""
	}
	if strings.TrimSpace(string(f.Type)) != "" {
		return f.Type
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
var CanonicalPathPolicies = map[models.FragmentType]CanonicalPathPolicy{
	models.FragmentTypeOrganization: {
		Prefix:      "/org/",
		Example:     "/org/us/tesla",
		Description: "Organization paths are jurisdiction-scoped: /org/{iso2|na}/{slug}. Government bodies use /gov/{iso2|na}/{slug}",
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
		Example:     "/product/finished/mechanical/assembly/model-3",
		Description: "Product paths use strict taxonomy: /product/{category}/{type}/{stage}/{slug}",
	},
	models.FragmentTypePerson: {
		Prefix:      "/person/",
		Example:     "/person/elon-musk",
		Description: "Person paths use lowercase hyphenated names",
	},
	models.FragmentTypeMediaChannel: {
		Prefix:      "/media/",
		Example:     "/media/telegram/v-kazani-poymut",
		Description: "Media channel endpoints (e.g., Telegram) use /media/{platform}/{slug}",
	},
	models.FragmentTypeDecisionPoint: {
		Prefix:      "/decision/",
		Example:     "/decision/ai-chips/euv-export-control",
		Description: "Decision points are domain-scoped: /decision/{domain-slug}/{decision-slug}",
	},
	models.FragmentTypeMechanism: {
		Prefix:      "/mechanism/",
		Example:     "/mechanism/regulatory/export-control",
		Description: "Mechanisms are reusable control tools: /mechanism/{category}/{slug}",
	},
}

func (v *PreCreateValidator) String() string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("PreCreateValidator(geo=%v, orgDedup=%v, personDedup=%v)", v.geoBuilder != nil, v.orgDedup != nil, v.personDedup != nil)
}
