package scout

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// OrgDeduplicatorStats is a lightweight in-memory counter snapshot for observability.
type OrgDeduplicatorStats struct {
	Calls                 int64
	HitAliasReverseLookup int64
	HitPathVariant        int64
	HitCountryAsOrg       int64
	HitWikidataID         int64
	HitGovOrgBridge       int64
	HitLegalName          int64
	HitLabelEN            int64
	HitShortName          int64
	HitNormalizedLegal    int64
	HitWikidataSearch     int64
}

func isOrgWikidataSearchDedupEnabled() bool {
	// Default OFF: avoid false merges due to fuzzy Wikidata search.
	// Enable explicitly if you want aggressive org dedup:
	//   HORIZON_ENABLE_ORG_WIKIDATA_SEARCH_DEDUP=true
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HORIZON_ENABLE_ORG_WIKIDATA_SEARCH_DEDUP")))
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

// =============================================================================
// ORGANIZATION DEDUPLICATION POLICY
// =============================================================================
// Organizations are frequently mentioned under different names (aliases, legal names,
// abbreviations). This policy defines how to identify and merge duplicates.
//
// Deduplication Priority:
// 1. Wikidata ID match - highest confidence, same real-world entity
// 2. Exact shortName match (case-insensitive) - high confidence
// 3. Normalized legalName match - medium confidence (removes JSC, LLC, Inc, etc.)

// OrgDeduplicator handles organization deduplication using Wikidata and name matching.
type OrgDeduplicator struct {
	fragmentsRepo  *repo.HorizonFragmentsRepository
	wikidataClient coreontology.WikidataClientInterface
	typeNormalizer *TypeNormalizer

	calls                 int64
	hitAliasReverseLookup int64
	hitPathVariant        int64
	hitCountryAsOrg       int64
	hitWikidataID         int64
	hitGovOrgBridge       int64
	hitLegalName          int64
	hitLabelEN            int64
	hitShortName          int64
	hitNormalizedLegal    int64
	hitWikidataSearch     int64
}

// NewOrgDeduplicator creates a new organization deduplicator.
func NewOrgDeduplicator(fragmentsRepo *repo.HorizonFragmentsRepository, client coreontology.WikidataClientInterface) *OrgDeduplicator {
	return &OrgDeduplicator{
		fragmentsRepo:  fragmentsRepo,
		wikidataClient: client,
		typeNormalizer: NewTypeNormalizer(),
	}
}

// DeduplicateOrg attempts to find an existing organization fragment that matches
// the given fragment. If found, returns the existing fragment; otherwise returns
// the original fragment unchanged.
//
// Matching priority (Tier 1 = 100% confidence, Tier 2 = high confidence):
// Tier 1:
//  1. Wikidata ID match (from KBRefs or Identifiers)
//  2. Exact legalName match (case-insensitive) — юридическое название уникально
//
// Tier 2:
//  3. Exact label[en] match + same type
//  4. Exact shortName match
//  5. Normalized legalName match (removes LLC, Inc, Ltd, etc.)
//
// Tier 3 (not implemented — requires manual review):
//   - shortName prefix/suffix overlap
//   - canonicalPath slug containment
func (d *OrgDeduplicator) DeduplicateOrg(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil {
		return fragment, nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypeOrganization {
		return fragment, nil
	}
	atomic.AddInt64(&d.calls, 1)
	// Government bodies (/gov/{iso2|na}/{slug}) are modeled as ORGANIZATION, but MUST NOT be deduplicated by name-only
	// across jurisdictions (e.g., "Ministry of Defence").
	//
	// However, we DO allow a very narrow "gov -> org" bridge dedup when the jurisdiction is explicit and stable:
	// - /gov/{iso2}/{slug} may be an ingestion artifact for an org-like entity that should live under /org/{iso2}/...
	// - High-confidence bridge requires exact legalName + (shortName OR label[en]) match within the SAME iso2,
	//   and forbids conflicting Wikidata IDs.
	isGov := strings.HasPrefix(strings.TrimSpace(fragment.CanonicalPath), "/gov/")

	// =========================================================================
	// Tier 0: Canonical-path alias resolution (prevents creating obvious alias duplicates)
	// =========================================================================
	// Universal pattern: we sometimes already have a canonical fragment that declares this path as an alias via:
	// - identifiers.merged_paths
	// - identifiers.legacy_org_canonical_path
	//
	// Examples:
	// - /org/pif declared as merged_paths by canonical /org/public-investment-fund
	// - /org/riyadh-air declared as legacy_org_canonical_path by canonical /org/riyadh_air
	if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" {
		// 0a) Reverse lookup: find an existing org that explicitly lists this canonicalPath as an alias.
		for _, key := range []string{"merged_paths", "legacy_org_canonical_path", "legacy-org-canonical-path"} {
			cands, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, key, cp, models.FragmentTypeOrganization)
			if err == nil && len(cands) > 0 {
				// If there are multiple candidates (rare), pick the best-quality one.
				best := chooseBestOrgCandidate(cands)
				if best != nil && best.ID != "" && best.ID != fragment.ID {
					atomic.AddInt64(&d.hitAliasReverseLookup, 1)
					return best, nil
				}
			}
		}

		// 0b) Canonical path normalization: treat '-' and '_' variants as equivalent for /org/* slugs.
		// This catches duplicates created by different slugifiers.
		for _, alt := range orgCanonicalVariants(cp) {
			if alt == "" || alt == cp {
				continue
			}
			existing, err := d.fragmentsRepo.GetByCanonicalPath(ctx, alt)
			if err == nil && existing != nil && existing.ID != "" && existing.ID != fragment.ID && existing.Type == models.FragmentTypeOrganization {
				atomic.AddInt64(&d.hitPathVariant, 1)
				return existing, nil
			}
		}
	}

	// =========================================================================
	// Tier 0: Type normalization — is this actually a country?
	// =========================================================================
	// Organizations with legalName matching a known country should be treated as GEO_REGION.
	// Example: /org/usa with legalName="United States of America" → /loc/us/na/na
	if d.typeNormalizer != nil {
		normResult := d.typeNormalizer.NormalizeFragmentType(fragment)
		if normResult.ShouldNormalize && normResult.NormalizedType == models.FragmentTypeGeoRegion && normResult.MergeWithPath != "" {
			// This is actually a country! Find the existing GEO_REGION fragment
			existing, err := d.fragmentsRepo.GetByCanonicalPath(ctx, normResult.MergeWithPath)
			if err == nil && existing != nil && existing.ID != fragment.ID {
				atomic.AddInt64(&d.hitCountryAsOrg, 1)
				fmt.Printf("[OrgDeduplicator] Detected country-as-org: %s → merging with %s\n",
					fragment.CanonicalPath, existing.CanonicalPath)
				return existing, nil
			}
		}
	}

	// =========================================================================
	// Tier 1: Highest confidence — auto-merge
	// =========================================================================

	// Strategy 1: Match by Wikidata ID
	wikidataID := extractWikidataIDFromFragment(fragment)
	if wikidataID != "" {
		existing, err := d.findByWikidataID(ctx, wikidataID)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			atomic.AddInt64(&d.hitWikidataID, 1)
			return existing, nil
		}
	}

	// For government bodies, stop here by default to avoid false merges across countries based on names.
	// Exception: allow a guarded gov->org bridge for explicit ISO2 when names match strongly.
	if isGov {
		if existing, err := d.findGovOrgBridgeCandidate(ctx, fragment); err == nil && existing != nil && existing.ID != fragment.ID {
			atomic.AddInt64(&d.hitGovOrgBridge, 1)
			fmt.Printf("[OrgDeduplicator] Gov->Org bridge dedup: %s -> %s\n", fragment.CanonicalPath, existing.CanonicalPath)
			return existing, nil
		}
		return fragment, nil
	}

	// Strategy 2: Exact legalName match (case-insensitive)
	legalName := strings.TrimSpace(fragment.Identifiers["legalName"])
	if legalName != "" {
		existing, err := d.findByLegalName(ctx, fragment, legalName)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			atomic.AddInt64(&d.hitLegalName, 1)
			return existing, nil
		}
	}

	// =========================================================================
	// Tier 2: High confidence — auto-merge with logging
	// =========================================================================

	// Strategy 3: Exact label[en] match
	labelEN := strings.TrimSpace(fragment.Labels["en"])
	if labelEN != "" {
		existing, err := d.findByLabelEN(ctx, fragment, labelEN)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			atomic.AddInt64(&d.hitLabelEN, 1)
			return existing, nil
		}
	}

	// Strategy 3.5 (guarded): LOW-signal org stub rescue via normalized label
	// Example: /org/softbank-group label="softbank group" (no legalName/shortName, empty status)
	// should merge into /org/softbank legalName="SoftBank Group Corp."
	if labelEN != "" && strings.TrimSpace(fragment.Identifiers["legalName"]) == "" && strings.TrimSpace(fragment.Identifiers["shortName"]) == "" {
		if isLowSignalOrgStub(fragment) {
			normalizedLabel := NormalizeLegalName(labelEN)
			if normalizedLabel != "" {
				existing, err := d.findByNormalizedLegalName(ctx, fragment, normalizedLabel)
				if err == nil && existing != nil && existing.ID != fragment.ID {
					atomic.AddInt64(&d.hitNormalizedLegal, 1)
					return existing, nil
				}
			}
		}
	}

	// Strategy 4: Match by shortName
	shortName := strings.TrimSpace(fragment.Identifiers["shortName"])
	if shortName != "" {
		existing, err := d.findByShortName(ctx, fragment, shortName)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			atomic.AddInt64(&d.hitShortName, 1)
			return existing, nil
		}
	}

	// Strategy 5: Normalized legalName match (fallback for minor suffix differences)
	if legalName != "" {
		normalizedLegal := NormalizeLegalName(legalName)
		if normalizedLegal != "" {
			existing, err := d.findByNormalizedLegalName(ctx, fragment, normalizedLegal)
			if err == nil && existing != nil && existing.ID != fragment.ID {
				atomic.AddInt64(&d.hitNormalizedLegal, 1)
				return existing, nil
			}
		}
	}

	// Strategy 5.5: Alnum fingerprint fallback (e.g., "B.M.W." vs "BMW").
	// This is STRICTLY gated:
	// - same explicit jurisdiction (ISO2/composite), or no merge
	// - no conflicting Wikidata IDs
	// - for short fingerprints (<=4), require slug-core match too
	if existing, err := d.findByAlnumFingerprint(ctx, fragment); err == nil && existing != nil && existing.ID != fragment.ID {
		atomic.AddInt64(&d.hitNormalizedLegal, 1)
		return existing, nil
	}

	// Strategy 6: Try to resolve Wikidata ID via API if we have a client
	if isOrgWikidataSearchDedupEnabled() && d.wikidataClient != nil && wikidataID == "" {
		resolvedID := d.resolveWikidataID(ctx, fragment)
		if resolvedID != "" {
			existing, err := d.findByWikidataID(ctx, resolvedID)
			if err == nil && existing != nil && existing.ID != fragment.ID {
				atomic.AddInt64(&d.hitWikidataSearch, 1)
				return existing, nil
			}
			// Store resolved Wikidata ID in the fragment for future use
			if fragment.Identifiers == nil {
				fragment.Identifiers = make(map[string]string)
			}
			fragment.Identifiers["wikidataId"] = resolvedID
		}
	}

	return fragment, nil
}

// findGovOrgBridgeCandidate attempts to deduplicate a /gov/{iso2}/{slug} ORGANIZATION fragment into an /org/{iso2}/{slug}
// fragment when we have strong signals that they represent the same real-world entity.
//
// This is intentionally narrow and conservative:
// - requires explicit ISO2 on BOTH sides and they must match
// - requires exact legalName, and also requires (shortName OR label[en]) to match when provided
// - forbids conflicting Wikidata IDs
func (d *OrgDeduplicator) findGovOrgBridgeCandidate(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil || fragment == nil || fragment.Type != models.FragmentTypeOrganization {
		return nil, nil
	}
	fs := parseOrgPathScope(fragment.CanonicalPath)
	if fs.ns != "gov" || !fs.hasCC || !isExplicitCountryCode(fs.cc) {
		return nil, nil
	}
	legalName := strings.TrimSpace(fragment.Identifiers["legalName"])
	if legalName == "" {
		return nil, nil
	}

	cands, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "legalName", legalName, models.FragmentTypeOrganization)
	if err != nil || len(cands) == 0 {
		return nil, err
	}

	fragShort := strings.TrimSpace(fragment.Identifiers["shortName"])
	fragLabel := strings.TrimSpace(fragment.Labels["en"])
	fragQID := strings.TrimSpace(extractWikidataIDFromFragment(fragment))

	filtered := make([]*models.HorizonFragment, 0, len(cands))
	for _, c := range cands {
		if c == nil || c.Type != models.FragmentTypeOrganization || c.ID == "" || c.ID == fragment.ID {
			continue
		}
		cs := parseOrgPathScope(c.CanonicalPath)
		// Bridge target must be a country-scoped /org/{iso2}/{slug} (stable canonical namespace).
		if cs.ns != "org" || !cs.hasCC || !isExplicitCountryCode(cs.cc) {
			continue
		}
		if cs.cc != fs.cc {
			continue
		}

		// Never bridge if Wikidata conflicts (strong evidence they are distinct entities).
		cQID := strings.TrimSpace(extractWikidataIDFromFragment(c))
		if fragQID != "" && cQID != "" && !strings.EqualFold(fragQID, cQID) {
			continue
		}

		// Require an additional name signal beyond legalName if we have it on the gov fragment.
		// This keeps the bridge conservative, while still catching obvious artifacts like:
		// /gov/sa/saudi-olympic-and-paralympic-committee <-> /org/sa/sopc
		if fragShort != "" {
			if !strings.EqualFold(strings.TrimSpace(c.Identifiers["shortName"]), fragShort) {
				continue
			}
		} else if fragLabel != "" {
			if !strings.EqualFold(strings.TrimSpace(c.Labels["en"]), fragLabel) {
				continue
			}
		} else {
			// No extra signal; too risky to bridge.
			continue
		}

		filtered = append(filtered, c)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	return chooseBestOrgCandidate(filtered), nil
}

// findByWikidataID searches for an existing fragment with the given Wikidata ID.
func (d *OrgDeduplicator) findByWikidataID(ctx context.Context, wikidataID string) (*models.HorizonFragment, error) {
	if wikidataID == "" {
		return nil, nil
	}

	// Search by identifier field (returns slice)
	existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "wikidataId", wikidataID, models.FragmentTypeOrganization)
	if err == nil && len(existing) > 0 {
		return chooseBestOrgCandidate(existing), nil
	}

	return nil, nil
}

// findByShortName searches for an existing organization with the same shortName.
func (d *OrgDeduplicator) findByShortName(ctx context.Context, fragment *models.HorizonFragment, shortName string) (*models.HorizonFragment, error) {
	if shortName == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "shortName", shortName, models.FragmentTypeOrganization)
	if err == nil && len(existing) > 0 {
		existing = filterOrgDedupCandidates(fragment, existing, "short")
		existing = filterSelfConsistentOrgCandidates(existing)
		return chooseBestOrgCandidate(existing), nil
	}

	// Fallback: best-effort normalization for legal-form variants in shortName
	// (e.g., "Vianode AS" vs "Vianode JSC").
	normalizedShort := NormalizeLegalName(shortName)
	if normalizedShort != "" {
		existingNorm, errNorm := d.fragmentsRepo.FindByNormalizedLegalName(ctx, normalizedShort)
		if errNorm == nil && len(existingNorm) > 0 {
			existingNorm = filterOrgDedupCandidates(fragment, existingNorm, "normalized")
			existingNorm = filterSelfConsistentOrgCandidates(existingNorm)
			return chooseBestOrgCandidate(existingNorm), nil
		}
	}

	return nil, nil
}

// findByLegalName searches for an existing organization with the exact legalName (case-insensitive).
func (d *OrgDeduplicator) findByLegalName(ctx context.Context, fragment *models.HorizonFragment, legalName string) (*models.HorizonFragment, error) {
	if legalName == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByIdentifierAndType(ctx, "legalName", legalName, models.FragmentTypeOrganization)
	if err == nil && len(existing) > 0 {
		existing = filterOrgDedupCandidates(fragment, existing, "legal")
		existing = filterSelfConsistentOrgCandidates(existing)
		return chooseBestOrgCandidate(existing), nil
	}

	return nil, nil
}

// findByLabelEN searches for an existing organization with the exact label[en].
func (d *OrgDeduplicator) findByLabelEN(ctx context.Context, fragment *models.HorizonFragment, labelEN string) (*models.HorizonFragment, error) {
	if labelEN == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByLabel(ctx, "en", labelEN, models.FragmentTypeOrganization)
	if err == nil && len(existing) > 0 {
		existing = filterOrgDedupCandidates(fragment, existing, "label")
		existing = filterSelfConsistentOrgCandidates(existing)
		return chooseBestOrgCandidate(existing), nil
	}

	return nil, nil
}

// findByNormalizedLegalName searches for organizations where normalized legalName matches.
// This catches cases like "Wingtech Technology Co., Ltd." vs "Wingtech Technology Ltd".
func (d *OrgDeduplicator) findByNormalizedLegalName(ctx context.Context, fragment *models.HorizonFragment, normalizedName string) (*models.HorizonFragment, error) {
	if normalizedName == "" {
		return nil, nil
	}

	// FindByNormalizedLegalName is a specialized search that normalizes before comparison.
	// If repo doesn't support it, fall back to nil (skip this strategy).
	existing, err := d.fragmentsRepo.FindByNormalizedLegalName(ctx, normalizedName)
	if err == nil && len(existing) > 0 {
		// FindByNormalizedLegalName is already org-scoped, but we still select best in case of multiple.
		existing = filterOrgDedupCandidates(fragment, existing, "normalized")
		existing = filterSelfConsistentOrgCandidates(existing)
		return chooseBestOrgCandidate(existing), nil
	}

	return nil, nil
}

type orgPathScope struct {
	ns    string // "org" | "gov" | ""
	cc    string // iso2|na|"" ("" for legacy /org/{slug})
	slug  string
	hasCC bool
}

func parseOrgPathScope(cp string) orgPathScope {
	p := strings.TrimSpace(cp)
	if p == "" || !strings.HasPrefix(p, "/") {
		return orgPathScope{}
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 2 {
		return orgPathScope{}
	}
	switch parts[0] {
	case "org":
		// /org/{slug} (legacy) OR /org/{iso2|na}/{slug}
		if len(parts) == 2 {
			return orgPathScope{ns: "org", slug: parts[1], hasCC: false}
		}
		if len(parts) >= 3 {
			return orgPathScope{ns: "org", cc: strings.ToLower(strings.TrimSpace(parts[1])), slug: parts[2], hasCC: true}
		}
	case "gov":
		// /gov/{iso2|na}/{slug}
		if len(parts) >= 3 {
			return orgPathScope{ns: "gov", cc: strings.ToLower(strings.TrimSpace(parts[1])), slug: parts[2], hasCC: true}
		}
	}
	return orgPathScope{}
}

func isExplicitCountryCode(cc string) bool {
	cc = strings.ToLower(strings.TrimSpace(cc))
	return cc != "" && cc != "na"
}

// filterOrgDedupCandidates applies jurisdiction-aware filtering to prevent false cross-country merges.
// This is intentionally conservative: it may reduce merging, but it preserves correctness.
//
// Exception: when one side is /org/na/... (unknown jurisdiction) and the other is /org/{cc}/...,
// we allow the merge if the slug cores (name without legal form suffix) are identical.
// This catches duplicates like /org/na/stc-bank-plc vs /org/sa/stc-bank-pjsc where the
// same entity was ingested with different legal forms and missing jurisdiction.
func filterOrgDedupCandidates(fragment *models.HorizonFragment, cands []*models.HorizonFragment, strength string) []*models.HorizonFragment {
	if fragment == nil || len(cands) == 0 {
		return cands
	}
	fs := parseOrgPathScope(fragment.CanonicalPath)
	if fs.ns == "gov" {
		// Government bodies: only allow Wikidata-based merges (handled separately).
		return nil
	}
	out := make([]*models.HorizonFragment, 0, len(cands))
	for _, c := range cands {
		if c == nil || c.Type != models.FragmentTypeOrganization {
			continue
		}
		cs := parseOrgPathScope(c.CanonicalPath)
		// Never merge /org/... with /gov/... by name-only.
		if fs.ns != "" && cs.ns != "" && fs.ns != cs.ns {
			continue
		}
		// If both sides have explicit (non-na) country codes and they differ, reject.
		if isExplicitCountryCode(fs.cc) && isExplicitCountryCode(cs.cc) && fs.cc != cs.cc {
			continue
		}
		// For weaker signals, avoid merging unknown-jurisdiction orgs (/org/na/...) into country-scoped ones
		// UNLESS slug cores (name without legal form) are identical — strong evidence of same entity.
		switch strength {
		case "short", "label", "normalized":
			if strings.ToLower(strings.TrimSpace(fs.cc)) == "na" && isExplicitCountryCode(cs.cc) {
				if !slugCoresMatch(fs.slug, cs.slug) {
					continue
				}
				// Slug cores match (e.g. "stc-bank-plc" and "stc-bank-pjsc" both have core "stc-bank").
				// Allow the merge — the country-scoped path is the better canonical.
			}
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// slugCoresMatch compares two org slug strings after stripping legal form suffixes.
// e.g., "stc-bank-pjsc" and "stc-bank-plc" both reduce to "stc-bank" → match.
func slugCoresMatch(slugA, slugB string) bool {
	coreA := stripSlugLegalForm(strings.ToLower(strings.TrimSpace(slugA)))
	coreB := stripSlugLegalForm(strings.ToLower(strings.TrimSpace(slugB)))
	return coreA != "" && coreB != "" && coreA == coreB
}

// stripSlugLegalForm removes trailing legal form suffixes from a slug.
// Uses the same forms as normalizeLegalForm but operates on hyphenated slugs.
func stripSlugLegalForm(slug string) string {
	legalForms := []string{
		"pjsc", "cjsc", "ojsc", "jsc",
		"pllc", "llc", "inc", "ltd", "co", "corp",
		"plc", "gmbh", "sarl", "srl", "ag", "sa", "spa", "as", "ab",
		"bv", "nv", "se", "lp", "llp", "soe", "sup", "fsup", "sp",
	}
	changed := true
	for changed {
		changed = false
		for _, lf := range legalForms {
			suffix := "-" + lf
			if strings.HasSuffix(slug, suffix) {
				slug = strings.TrimSuffix(slug, suffix)
				changed = true
				break
			}
		}
	}
	// Normalize common corp filler tokens that often appear before legal form
	// and should not split identity cores (e.g., wingtech-technology-co-ltd vs wingtech-technology-ltd).
	for _, filler := range []string{"-co", "-company", "-group", "-holding", "-holdings"} {
		if strings.HasSuffix(slug, filler) {
			slug = strings.TrimSuffix(slug, filler)
		}
	}
	return slug
}

// orgCanonicalVariants returns alternative canonical path variants for org slugs
// where different slugifiers might use '-' vs '_' (e.g., /org/riyadh-air vs /org/riyadh_air).
func orgCanonicalVariants(canonicalPath string) []string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/org/") {
		return []string{cp}
	}
	slug := strings.TrimPrefix(cp, "/org/")
	if slug == "" {
		return []string{cp}
	}
	alt := slug
	if strings.Contains(alt, "_") {
		alt = strings.ReplaceAll(alt, "_", "-")
	} else if strings.Contains(alt, "-") {
		alt = strings.ReplaceAll(alt, "-", "_")
	} else {
		return []string{cp}
	}
	return []string{cp, "/org/" + alt}
}

// chooseBestOrgCandidate picks the "best" org fragment among candidates.
// Heuristics are intentionally simple and safe:
// - Prefer ACTIVE governance over DORMANT/empty
// - Prefer having entityId linkage
// - Prefer having legalName/shortName identifiers
// - Prefer older fragments as tie-breaker
func chooseBestOrgCandidate(cands []*models.HorizonFragment) *models.HorizonFragment {
	var best *models.HorizonFragment
	bestScore := -1
	for _, f := range cands {
		if f == nil || f.Type != models.FragmentTypeOrganization {
			continue
		}
		score := 0
		switch strings.ToUpper(strings.TrimSpace(f.Governance.Status)) {
		case string(models.FragmentStatusActive):
			score += 50
		case string(models.FragmentStatusDormant):
			score += 10
		case string(models.FragmentStatusArchived):
			score += 0
		default:
			score += 5
		}
		if f.EntityID != nil && strings.TrimSpace(*f.EntityID) != "" {
			score += 25
		}
		// Strong signals: prefer fragments with Wikidata ID and industryPaths.
		if extractWikidataIDFromFragment(f) != "" {
			score += 100
		}
		if len(f.IndustryPaths) > 0 {
			score += 50
		}
		if strings.TrimSpace(f.Identifiers["legalName"]) != "" {
			score += 10
		}
		if strings.TrimSpace(f.Identifiers["shortName"]) != "" {
			score += 5
		}
		if strings.TrimSpace(f.Identifiers["merged_paths"]) != "" || strings.TrimSpace(f.Identifiers["legacy_org_canonical_path"]) != "" {
			score += 5
		}
		if best == nil || score > bestScore {
			best = f
			bestScore = score
			continue
		}
		// Tie-breaker: older CreatedAt (prefer earlier)
		if score == bestScore && !f.CreatedAt.IsZero() && !best.CreatedAt.IsZero() && f.CreatedAt.Before(best.CreatedAt) {
			best = f
		}
	}
	return best
}

func (d *OrgDeduplicator) findByAlnumFingerprint(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil || fragment == nil {
		return nil, nil
	}
	fs := parseOrgPathScope(fragment.CanonicalPath)
	if !isExplicitCountryCode(fs.cc) {
		return nil, nil
	}
	fp := orgNameAlnumFingerprint(fragment)
	if fp == "" {
		return nil, nil
	}

	cands, err := d.fragmentsRepo.ListByTypeAndTenant(ctx, "global", models.FragmentTypeOrganization, 800)
	if err != nil || len(cands) == 0 {
		return nil, err
	}

	fragQID := strings.TrimSpace(extractWikidataIDFromFragment(fragment))
	matches := make([]*models.HorizonFragment, 0, 8)
	for _, c := range cands {
		if c == nil || c.ID == "" || c.ID == fragment.ID || c.Type != models.FragmentTypeOrganization {
			continue
		}
		cs := parseOrgPathScope(c.CanonicalPath)
		if !isExplicitCountryCode(cs.cc) || cs.cc != fs.cc {
			continue
		}
		cfp := orgNameAlnumFingerprint(c)
		if cfp == "" || cfp != fp {
			continue
		}
		cQID := strings.TrimSpace(extractWikidataIDFromFragment(c))
		if fragQID != "" && cQID != "" && !strings.EqualFold(fragQID, cQID) {
			continue
		}
		if !orgSelfConsistent(c) {
			continue
		}
		if len(fp) <= 4 && !slugCoresMatch(fs.slug, cs.slug) {
			continue
		}
		matches = append(matches, c)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return chooseBestOrgCandidate(matches), nil
}

func filterSelfConsistentOrgCandidates(cands []*models.HorizonFragment) []*models.HorizonFragment {
	if len(cands) == 0 {
		return cands
	}
	out := make([]*models.HorizonFragment, 0, len(cands))
	for _, c := range cands {
		if c == nil {
			continue
		}
		if orgSelfConsistent(c) {
			out = append(out, c)
		}
	}
	return out
}

func orgSelfConsistent(f *models.HorizonFragment) bool {
	if f == nil {
		return false
	}
	scope := parseOrgPathScope(strings.TrimSpace(f.CanonicalPath))
	if scope.ns != "org" && scope.ns != "gov" {
		return true
	}
	slug := strings.TrimSpace(scope.slug)
	if slug == "" {
		return true
	}
	name := strings.TrimSpace(f.Identifiers["legalName"])
	if name == "" {
		name = strings.TrimSpace(f.Labels["en"])
	}
	if name == "" {
		name = strings.TrimSpace(f.Identifiers["shortName"])
	}
	if name == "" {
		return true
	}
	slugTokens := meaningfulTokens(strings.ReplaceAll(stripSlugLegalForm(strings.ToLower(slug)), "-", " "))
	nameTokens := meaningfulTokens(NormalizeLegalName(name))
	if len(slugTokens) == 0 || len(nameTokens) == 0 {
		return true
	}
	nameSet := make(map[string]struct{}, len(nameTokens))
	for _, t := range nameTokens {
		nameSet[t] = struct{}{}
	}
	for _, t := range slugTokens {
		if _, ok := nameSet[t]; ok {
			return true
		}
	}
	return false
}

func meaningfulTokens(s string) []string {
	raw := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(raw) == 0 {
		return nil
	}
	stop := map[string]struct{}{
		"the": {}, "and": {}, "of": {}, "for": {}, "in": {},
		"group": {}, "holding": {}, "holdings": {}, "company": {}, "co": {}, "corp": {}, "corporation": {},
		"inc": {}, "llc": {}, "ltd": {}, "plc": {}, "llp": {}, "lp": {}, "gmbh": {}, "ag": {}, "sa": {}, "sarl": {}, "srl": {},
		"spa": {}, "bv": {}, "nv": {}, "ab": {}, "as": {}, "jsc": {}, "pjsc": {}, "oy": {}, "oyj": {}, "ao": {}, "ooo": {},
		"ministry": {}, "ministerie": {}, "department": {}, "authority": {}, "agency": {}, "bureau": {}, "office": {},
		"state": {}, "national": {}, "federal": {}, "government": {},
	}
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		var b strings.Builder
		for _, r := range t {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(unicode.ToLower(r))
			}
		}
		t = b.String()
		if t == "" || len(t) < 3 {
			continue
		}
		if _, skip := stop[t]; skip {
			continue
		}
		out = append(out, t)
	}
	return out
}

func orgNameAlnumFingerprint(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	raw := strings.TrimSpace(f.Identifiers["legalName"])
	if raw == "" {
		raw = strings.TrimSpace(f.Identifiers["shortName"])
	}
	if raw == "" {
		raw = strings.TrimSpace(f.Labels["en"])
	}
	if raw == "" {
		return ""
	}
	norm := NormalizeLegalName(raw)
	if norm == "" {
		norm = strings.ToLower(strings.TrimSpace(raw))
	}
	var b strings.Builder
	for _, r := range norm {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func isLowSignalOrgStub(f *models.HorizonFragment) bool {
	if f == nil || f.Type != models.FragmentTypeOrganization {
		return false
	}
	// Strong identifiers → not a stub.
	if strings.TrimSpace(f.Identifiers["legalName"]) != "" || strings.TrimSpace(f.Identifiers["shortName"]) != "" || extractWikidataIDFromFragment(f) != "" {
		return false
	}
	// Weak lifecycle signals (common for auto-generated duplicates).
	if strings.TrimSpace(f.Status) != "" && !strings.EqualFold(strings.TrimSpace(f.Governance.Status), string(models.FragmentStatusDormant)) {
		return false
	}
	// Extra weak signal: generic/incorrect taxonomy placement.
	if strings.EqualFold(strings.TrimSpace(f.SectorPath), "/Geography") {
		return true
	}
	// Default: label-only + weak lifecycle is sufficient for stub classification.
	return true
}

// resolveWikidataID attempts to find a Wikidata ID for the organization using the API.
func (d *OrgDeduplicator) resolveWikidataID(ctx context.Context, fragment *models.HorizonFragment) string {
	if d.wikidataClient == nil {
		return ""
	}

	// Try to search by organization name
	name := strings.TrimSpace(fragment.Labels["en"])
	if name == "" {
		name = strings.TrimSpace(fragment.Identifiers["legalName"])
	}
	if name == "" {
		name = strings.TrimSpace(fragment.Identifiers["shortName"])
	}
	if name == "" {
		return ""
	}

	// Search Wikidata for organizations matching this name
	response, err := d.wikidataClient.Search(ctx, name, 3)
	if err != nil || response == nil || len(response.Results) == 0 {
		return ""
	}

	// Return the first result's ID
	return response.Results[0].ID
}

// extractWikidataIDFromFragment extracts Wikidata ID from various locations.
func extractWikidataIDFromFragment(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}

	// Check fragment's KBRefs
	for _, ref := range f.KBRefs {
		if strings.EqualFold(ref.KB, "Wikidata") || strings.EqualFold(ref.KB, "wikidata") {
			return strings.TrimSpace(ref.ExternalID)
		}
	}

	// Check Identifiers
	if wid := strings.TrimSpace(f.Identifiers["wikidataId"]); wid != "" {
		return wid
	}
	if wid := strings.TrimSpace(f.Identifiers["wikidata"]); wid != "" {
		return wid
	}

	return ""
}

// =============================================================================
// LEGAL NAME NORMALIZATION POLICY
// =============================================================================
// Legal names often contain suffixes that differ between entries but refer to
// the same organization. This policy normalizes them for matching.

// LegalSuffixesToRemove defines common legal suffixes to strip during normalization.
var LegalSuffixesToRemove = []string{
	" jsc", " pjsc", " cjsc", " ojsc",
	" llc", " pllc", " inc", " ltd", " co", " corp", " corporation",
	" group", " holdings", " limited", " company", " pty ltd",
	" sa", " ag", " se", " gmbh", " ab", " oy", " as", " spa",
	" nv", " bv", " sarl", " srl", " plc", " lp", " llp",
	" s.a.", " a.g.", " s.p.a.", " n.v.", " b.v.", " s.à r.l.", " p.l.c.",
	" p.j.s.c.", " c.j.s.c.",
}

// reParenthetical matches parenthetical annotations that LLMs often inject into org names.
// Examples: "(formerly stc pay)", "(ex-Aramex)", "(now STC Bank)", "(fka STC Pay)", "(dba ...)"
var reParenthetical = regexp.MustCompile(`\s*\((?:formerly|previously|ex-?|now|fka|dba|aka|trading as|prev\.?|f/k/a|d/b/a|a/k/a)\s[^)]*\)`)

// stripParentheticalAnnotations removes LLM-injected parenthetical annotations from org names.
// e.g., "STC Bank (formerly stc pay)" → "STC Bank"
func stripParentheticalAnnotations(name string) string {
	cleaned := reParenthetical.ReplaceAllString(name, "")
	return strings.TrimSpace(cleaned)
}

// NormalizeLegalName removes common legal suffixes, parenthetical annotations,
// and normalizes spacing/punctuation for deduplication matching.
func NormalizeLegalName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	// Remove parenthetical annotations like "(formerly stc pay)" before suffix stripping.
	// This prevents "(formerly ...)" from masking the legal suffix at the end.
	name = stripParentheticalAnnotations(name)

	// Normalize punctuation and spacing
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, "&", "and")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")

	// Remove legal suffixes iteratively after punctuation normalization.
	// This handles variants like:
	// - "co ltd"
	// - "co., ltd."
	// - "inc ltd" (stacked noise)
	changed := true
	for changed {
		changed = false
		for _, suffix := range LegalSuffixesToRemove {
			sfx := strings.ToLower(strings.TrimSpace(suffix))
			sfx = strings.ReplaceAll(sfx, ".", "")
			sfx = strings.ReplaceAll(sfx, ",", "")
			sfx = strings.Join(strings.Fields(sfx), " ")
			if sfx == "" {
				continue
			}
			if strings.HasSuffix(name, " "+sfx) {
				name = strings.TrimSpace(strings.TrimSuffix(name, " "+sfx))
				changed = true
				break
			}
			if name == sfx {
				name = ""
				changed = true
				break
			}
		}
	}

	name = strings.Join(strings.Fields(name), " ")

	return name
}

func (d *OrgDeduplicator) String() string {
	if d == nil {
		return "<nil>"
	}
	hasClient := d.wikidataClient != nil
	return fmt.Sprintf("OrgDeduplicator(repo=%v, wikidata=%v)", d.fragmentsRepo != nil, hasClient)
}

// Stats returns a best-effort snapshot of in-memory counters.
func (d *OrgDeduplicator) Stats() OrgDeduplicatorStats {
	if d == nil {
		return OrgDeduplicatorStats{}
	}
	return OrgDeduplicatorStats{
		Calls:                 atomic.LoadInt64(&d.calls),
		HitAliasReverseLookup: atomic.LoadInt64(&d.hitAliasReverseLookup),
		HitPathVariant:        atomic.LoadInt64(&d.hitPathVariant),
		HitCountryAsOrg:       atomic.LoadInt64(&d.hitCountryAsOrg),
		HitWikidataID:         atomic.LoadInt64(&d.hitWikidataID),
		HitGovOrgBridge:       atomic.LoadInt64(&d.hitGovOrgBridge),
		HitLegalName:          atomic.LoadInt64(&d.hitLegalName),
		HitLabelEN:            atomic.LoadInt64(&d.hitLabelEN),
		HitShortName:          atomic.LoadInt64(&d.hitShortName),
		HitNormalizedLegal:    atomic.LoadInt64(&d.hitNormalizedLegal),
		HitWikidataSearch:     atomic.LoadInt64(&d.hitWikidataSearch),
	}
}
