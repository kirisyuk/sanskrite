package models

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// HorizonFragment представляет узел графа знаний (всегда глобальный)
type HorizonFragment struct {
	// BaseOntologyFields (общие с Entity)
	ID string `json:"id" firestore:"id"`
	// TenantID is usually "global" for world-graph fragments.
	// Exception: SUPPLY_LINK fragments can be tenant-scoped or "global" (assumption layer).
	TenantID    string            `json:"tenantId" firestore:"tenantId"`
	Version     int               `json:"version" firestore:"version"`
	CreatedAt   time.Time         `json:"createdAt" firestore:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt" firestore:"updatedAt"`
	CreatedBy   string            `json:"createdBy" firestore:"createdBy"`
	UpdatedBy   string            `json:"updatedBy" firestore:"updatedBy"`
	Labels      map[string]string `json:"labels,omitempty" firestore:"labels,omitempty"`
	Description string            `json:"description,omitempty" firestore:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty" firestore:"tags,omitempty"`
	Status      string            `json:"status" firestore:"status"` // active, deprecated, draft, archived

	// Основные поля
	CanonicalPath string `json:"canonicalPath" firestore:"canonicalPath"`
	// CanonicalParts is a semantic representation of canonicalPath.
	// When present, repositories may reassemble canonicalPath from these parts (best-effort)
	// to avoid clobbering concurrent, independent canonicalization fixes (geo vs legal form vs taxonomy).
	//
	// IMPORTANT: canonicalPath remains the read-path across the system for compatibility.
	CanonicalParts *taxonomy.CanonicalPathParts `json:"canonicalParts,omitempty" firestore:"canonicalParts,omitempty"`
	Type           FragmentType                 `json:"type" firestore:"type"`
	EntityPath     string                       `json:"entityPath" firestore:"entityPath"` // /Organization/Corporation/Public
	SectorPath     string                       `json:"sectorPath" firestore:"sectorPath"` // /Technology/Semiconductors/Manufacturing
	// IndustryPaths are canonical, multi-valued industry classifications (Horizon-first taxonomy).
	// Format: /industry/{sector}/{group}/{industry}/{subindustry}
	IndustryPaths []string `json:"industryPaths,omitempty" firestore:"industryPaths,omitempty"`

	// MarketSegments are canonical, multi-valued market segment paths for organizations/facilities.
	// Canonical format: /segment/{domain}/{vertical}/{geo}
	// - geo: ISO3166-1 alpha2 lowercase (2 chars) or "global"
	// This is an ORG-level classification (not a separate fragment type in MVP).
	MarketSegments []string `json:"marketSegments,omitempty" firestore:"marketSegments,omitempty"`

	// MarketSegmentNorms are derived, normalized "market coordinates" for each MarketSegment entry.
	// They are computed using a Firestore-backed dictionary (runtime taxonomy) and are intended for:
	// - consistent UI rendering (icons / grouping)
	// - future filtering/indexing
	MarketSegmentNorms []coremodels.MarketSegmentNorm `json:"marketSegmentNorms,omitempty" firestore:"marketSegmentNorms,omitempty"`
	// MarketSegmentNormDomains is a deduplicated list of normalized market domains (market_l1) for indexing/filtering.
	MarketSegmentNormDomains []string `json:"marketSegmentNormDomains,omitempty" firestore:"marketSegmentNormDomains,omitempty"`
	// MarketSegmentNormVerticals is a deduplicated list of normalized market verticals (market_l2) for indexing/filtering.
	MarketSegmentNormVerticals []string `json:"marketSegmentNormVerticals,omitempty" firestore:"marketSegmentNormVerticals,omitempty"`
	// MarketSegmentNormPairs is a deduplicated list of normalized domain/vertical pairs for indexing/filtering.
	// Format: "{domain}/{vertical}" if vertical != "", else "{domain}".
	MarketSegmentNormPairs []string `json:"marketSegmentNormPairs,omitempty" firestore:"marketSegmentNormPairs,omitempty"`

	// OrgSize is a coarse organization size bucket (ORG-level property).
	// Canonical values: "sme" | "le" | "enterprise" | "mega" | "na"
	OrgSize string `json:"orgSize,omitempty" firestore:"orgSize,omitempty"`

	// --- MARKET-specific (when Type == MARKET) ---
	// MarketDomain is a coarse canonical market domain.
	// Expected values (non-exhaustive, best-effort): finance, technology, industrial, consumer, healthcare, energy, materials, telecom, real-estate.
	MarketDomain string `json:"marketDomain,omitempty" firestore:"marketDomain,omitempty"`
	// MarketVertical is a more specific vertical/industry label within the domain (best-effort).
	MarketVertical string `json:"marketVertical,omitempty" firestore:"marketVertical,omitempty"`
	// MarketChannels is a multi-valued channel classification for the market (best-effort).
	// Examples: b2b, b2c, public-sector.
	MarketChannels []string `json:"marketChannels,omitempty" firestore:"marketChannels,omitempty"`
	// ParentMarketPath is the canonical path of the parent market in the hierarchy.
	// Example: /market/sa/event-marketing has parent /market/sa/marketing
	// This enables market hierarchy traversal (broader → narrower markets).
	ParentMarketPath string `json:"parentMarketPath,omitempty" firestore:"parentMarketPath,omitempty"`

	// --- GOV-specific (when Type == ORGANIZATION and CanonicalPath starts with /gov/) ---
	// GovType is the type/level of government body (best-effort classification).
	// Expected values: government, ministry, department, agency, committee, service, regulator, authority, bureau, office, council, commission
	GovType string `json:"govType,omitempty" firestore:"govType,omitempty"`
	// ParentGovPath is the canonical path of the parent government body in the administrative hierarchy.
	// Example: /gov/sa/sfda has parent /gov/sa/ministry-of-health
	// This enables administrative hierarchy traversal (superior → subordinate bodies).
	ParentGovPath string `json:"parentGovPath,omitempty" firestore:"parentGovPath,omitempty"`

	// --- COMMUNITY-specific (when Type == COMMUNITY) ---
	// CommunityType classifies the nature of the informal community.
	// Canonical values: trade | activist | professional | criminal | religious | ethnic | interest | dynasty | tribe
	// - trade: commercial networks without formal structure (hawala, informal trade networks)
	// - activist: political/social activist groups
	// - professional: professional communities without legal entity (OSINT researchers, open-source maintainers)
	// - criminal: criminal networks and organizations
	// - religious: religious communities and congregations
	// - ethnic: diaspora and ethnic communities
	// - interest: hobby/interest-based communities
	// - dynasty: influential families acting as unified actors (political dynasties, business families, royal houses)
	// - tribe: tribal/clan structures with identified leadership (Middle Eastern tribes, Central Asian zhuz, etc.)
	CommunityType string `json:"communityType,omitempty" firestore:"communityType,omitempty"`
	// CommunityPlatforms lists primary platforms/channels used by the community.
	// These are canonical paths to MEDIA_CHANNEL fragments.
	// Example: ["/media/telegram/kazan-zakaz", "/media/telegram/kazan-deals"]
	CommunityPlatforms []string `json:"communityPlatforms,omitempty" firestore:"communityPlatforms,omitempty"`
	// CommunityEstimatedSize is a rough estimate of community size (number of members).
	// Use ranges: "small" (<100), "medium" (100-1000), "large" (1000-10000), "massive" (>10000), or "unknown".
	CommunityEstimatedSize string `json:"communityEstimatedSize,omitempty" firestore:"communityEstimatedSize,omitempty"`
	// AffiliatedOrgPath is the canonical path of a formal organization the community is affiliated with (if any).
	// Example: /community/ae/trade/hawala-dubai might be affiliated with /org/ae/exchange-house-xyz
	AffiliatedOrgPath string `json:"affiliatedOrgPath,omitempty" firestore:"affiliatedOrgPath,omitempty"`

	// IndexPaths — денормализованные "координаты" фрагмента для детерминированного картографирования/фильтрации.
	// Собирается из canonicalPath + (sectorPath/entityPath + ancestors) + location + tags + identifiers + (optional spheres).
	// Ограничено по размеру (см. BuildIndexPaths/NormalizeIndexPaths).
	IndexPaths []string `json:"indexPaths,omitempty" firestore:"indexPaths,omitempty"`

	// Связь с Entity
	EntityID *string `json:"entityId,omitempty" firestore:"entityId,omitempty"`

	// Гибкие метрики
	State map[string]MetricValue `json:"state" firestore:"state"`
	// Qualifiers stores policy/runtime annotations (e.g., policyStatus/policyReason).
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`

	// HighProfile stores global, precomputed "high-profile" business signals derived from the graph.
	// It is optional and may be absent for older fragments or when backfill hasn't run yet.
	HighProfile *HighProfile `json:"highProfile,omitempty" firestore:"highProfile,omitempty"`

	// Обогащение данными
	KBRefs      []KBReference     `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`
	// NameHistory stores temporal aliases/rebrands/legacy names for explainable org identity transitions.
	NameHistory []NameHistoryEntry `json:"nameHistory,omitempty" firestore:"nameHistory,omitempty"`

	// Временная валидность
	ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`

	// Location с ISO стандартами
	HomeLocation *Location `json:"homeLocation,omitempty" firestore:"homeLocation,omitempty"`
	// Coordinates stores generic lat/lon for map rendering across all fragment kinds.
	Coordinates *Coordinates `json:"coordinates,omitempty" firestore:"coordinates,omitempty"`
	// HorizonMonitor stores typed fields imported from Horizon Monitor ontology.
	// It complements (but does not replace) generic qualifiers.
	HorizonMonitor *HorizonMonitorFragmentData `json:"horizonMonitor,omitempty" firestore:"horizonMonitor,omitempty"`
	// WorldMonitorLegacy keeps backward compatibility with legacy worldMonitor field.
	WorldMonitorLegacy *HorizonMonitorFragmentData `json:"worldMonitor,omitempty" firestore:"worldMonitor,omitempty"`
	// Track stores latest movement state for movable entities (TRACK type).
	Track *TrackFragmentData `json:"track,omitempty" firestore:"track,omitempty"`

	// Provenance
	Provenance Provenance `json:"provenance" firestore:"provenance"`

	// Метаданные и управление
	Metadata   FragmentMetadata `json:"metadata" firestore:"metadata"`
	Governance Governance       `json:"governance" firestore:"governance"`

	// Supply Link specific fields (when Type == FragmentTypeSupplyLink)
	// These fields are only used for SUPPLY_LINK fragments (tenant-specific)
	SupplierRef         *string `json:"supplierRef,omitempty" firestore:"supplierRef,omitempty"`                 // /org/... or /fac/... (nil = natural resource)
	CustomerRef         *string `json:"customerRef,omitempty" firestore:"customerRef,omitempty"`                 // /org/... or /fac/... (nil = end consumer)
	ProductRef          *string `json:"productRef,omitempty" firestore:"productRef,omitempty"`                   // /product/... (required for SUPPLY_LINK)
	SupplierFacilityRef *string  `json:"supplierFacilityRef,omitempty" firestore:"supplierFacilityRef,omitempty"` // /fac/... (optional)
	CustomerFacilityRef *string  `json:"customerFacilityRef,omitempty" firestore:"customerFacilityRef,omitempty"` // /fac/... (optional)
	TransportViaRefs   []string `json:"transportViaRefs,omitempty" firestore:"transportViaRefs,omitempty"`     // ordered /fac/... paths (ports, terminals, pipelines, warehouses)

	// Lifecycle — real-world lifecycle stage and dates (for FACILITY, DEPOSIT, ORGANIZATION).
	// LifecycleStage is a canonical value from AllowedLifecycleStages (e.g., "construction", "operational", "closed").
	LifecycleStage string `json:"lifecycleStage,omitempty" firestore:"lifecycleStage,omitempty"`
	// ExpectedStartDate — when the entity is planned to become operational (for construction/planning stages).
	ExpectedStartDate *time.Time `json:"expectedStartDate,omitempty" firestore:"expectedStartDate,omitempty"`
	// ActualStartDate — when the entity actually started operating.
	ActualStartDate *time.Time `json:"actualStartDate,omitempty" firestore:"actualStartDate,omitempty"`
	// ExpectedEndDate — when the entity is planned to close/decommission.
	ExpectedEndDate *time.Time `json:"expectedEndDate,omitempty" firestore:"expectedEndDate,omitempty"`
	// ActualEndDate — when the entity actually closed.
	ActualEndDate *time.Time `json:"actualEndDate,omitempty" firestore:"actualEndDate,omitempty"`
}

// FragmentMetadata метаданные жизненного цикла
type FragmentMetadata struct {
	CreatedAt      time.Time `json:"createdAt" firestore:"createdAt"`
	LastUpdatedAt  time.Time `json:"lastUpdatedAt" firestore:"lastUpdatedAt"`
	LastVerifiedAt time.Time `json:"lastVerifiedAt" firestore:"lastVerifiedAt"`
	// LastFactsSeenAt is a watermark for incremental "facts slice" processing (watchdog/scout).
	// It is independent from LastVerifiedAt to allow cheap fact-driven refresh without full LLM runs.
	LastFactsSeenAt time.Time `json:"lastFactsSeenAt" firestore:"lastFactsSeenAt"`
	// LastFactsAppliedAt is updated when fast-path applies facts to the fragment without LLM.
	LastFactsAppliedAt time.Time       `json:"lastFactsAppliedAt" firestore:"lastFactsAppliedAt"`
	SourceFrameID      string          `json:"sourceFrameId" firestore:"sourceFrameId"`
	EpistemicStatus    EpistemicStatus `json:"epistemicStatus" firestore:"epistemicStatus"`
}

// NameHistoryEntry stores a historical name variant with optional validity window.
// Kind values are normalized to: short | legal | brand | aka.
type NameHistoryEntry struct {
	Name      string     `json:"name" firestore:"name"`
	Kind      string     `json:"kind" firestore:"kind"`
	ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`
}

// Validate проверяет валидность HorizonFragment
func (f *HorizonFragment) Validate() error {
	f.NormalizeHorizonMonitorAliases()

	if f.Type == "" {
		return fmt.Errorf("type is required")
	}

	// SupplyLink fragments can be tenant-scoped OR global-scoped (assumption layer).
	// All other fragments must be global.
	if f.Type == FragmentTypeSupplyLink {
		if f.TenantID == "" {
			return fmt.Errorf("tenantId must be set for SUPPLY_LINK fragments")
		}
		// Validate SupplyLink-specific fields using detailed validation
		if err := ValidateSupplyLinkFields(f); err != nil {
			return err
		}
		// Auto-generate canonicalPath if not set
		if f.CanonicalPath == "" {
			canonicalPath, err := BuildSupplyLinkCanonicalPath(f.TenantID, f.SupplierRef, f.ProductRef, f.CustomerRef)
			if err != nil {
				return fmt.Errorf("failed to generate canonicalPath for SUPPLY_LINK: %w", err)
			}
			f.CanonicalPath = canonicalPath
		}
	} else {
		// Non-SupplyLink fragments must be global
		if f.TenantID != "global" && f.TenantID != "" {
			return fmt.Errorf("tenantId must be 'global' or empty for HorizonFragment (except SUPPLY_LINK)")
		}
		// Non-SupplyLink fragments require canonicalPath
		if f.CanonicalPath == "" {
			return fmt.Errorf("canonicalPath is required")
		}
	}
	if f.Governance.Owner != "SYSTEM" {
		return fmt.Errorf("governance.owner must be 'SYSTEM'")
	}
	if f.State == nil {
		f.State = make(map[string]MetricValue)
	}
	// Shared naming policy: keep legal/short/display names consistent and enforce Latin-only labels.en.
	f.Labels, f.Identifiers = naming.NormalizeFragmentNames(f.CanonicalPath, f.Labels, f.Identifiers)
	// Best-effort: trim and normalize temporal alias history.
	f.normalizeNameHistory()
	// Best-effort: keep Wikidata linkage consistent across both KBRefs and Identifiers.
	// This enables reliable dedup/search (e.g., FindByIdentifier("wikidataId", "...")) even for legacy fragments
	// that may have stored Wikidata only in kbRefs.
	f.ensureWikidataMirroring()
	// Best-effort: keep GeoNames linkage consistent across both KBRefs and Identifiers.
	// GeoNames ExternalID is a numeric string (geonameId).
	f.ensureGeoNamesMirroring()
	// Best-effort: keep generic coordinates filled from canonical geo-bearing fields.
	f.ensureCoordinatesMirroring()
	// Keep dual field compatibility: canonical horizonMonitor + legacy worldMonitor.
	f.SyncHorizonMonitorAliases()

	// Best-effort: normalize org-level classifiers (safe, deterministic).
	// These fields can be produced by Scout/Research and should be canonicalized before persistence.
	f.MarketSegments = taxonomy.NormalizeSegmentPaths(f.MarketSegments)
	f.OrgSize = taxonomy.NormalizeOrgSize(f.OrgSize)

	// Best-effort: normalize MARKET canonical path + fields (non-blocking).
	if f.Type == FragmentTypeMarket {
		// Tighten flat /market/{slug} into /market/global/{slug}.
		// NOTE: do not rewrite legacy /market/{domain}/{slug} because geo is unknown.
		cp := strings.TrimSpace(f.CanonicalPath)
		if strings.HasPrefix(cp, "/market/") {
			parts := strings.Split(strings.Trim(cp, "/"), "/")
			// market/{x}
			if len(parts) == 2 && parts[0] == "market" && strings.TrimSpace(parts[1]) != "" {
				f.CanonicalPath = "/market/global/" + strings.TrimSpace(parts[1])
			}
		}

		// Normalize domain/vertical/channels (best-effort).
		f.MarketDomain = strings.ToLower(strings.TrimSpace(f.MarketDomain))
		f.MarketVertical = strings.ToLower(strings.TrimSpace(f.MarketVertical))
		// Dedupe/normalize channels.
		if len(f.MarketChannels) > 0 {
			seen := map[string]struct{}{}
			out := make([]string, 0, len(f.MarketChannels))
			for _, ch := range f.MarketChannels {
				ch = strings.ToLower(strings.TrimSpace(ch))
				// Canonicalize common aliases (keep aligned with segment channel conventions).
				switch ch {
				case "gov", "government", "public":
					ch = "public-sector"
				}
				if ch == "" {
					continue
				}
				if _, ok := seen[ch]; ok {
					continue
				}
				seen[ch] = struct{}{}
				out = append(out, ch)
			}
			f.MarketChannels = out
		}

		// Best-effort: normalize parent market path.
		f.ParentMarketPath = strings.TrimSpace(f.ParentMarketPath)
		if f.ParentMarketPath != "" && !strings.HasPrefix(f.ParentMarketPath, "/market/") {
			f.ParentMarketPath = "" // invalid parent path, clear it
		}

	}

	// Best-effort: normalize GOV-specific fields (when canonicalPath starts with /gov/).
	if strings.HasPrefix(f.CanonicalPath, "/gov/") {
		// Normalize govType to lowercase.
		f.GovType = strings.ToLower(strings.TrimSpace(f.GovType))
		// Validate govType against allowed values (best-effort, non-blocking).
		allowedGovTypes := map[string]struct{}{
			"government": {}, "ministry": {}, "department": {}, "agency": {},
			"authority": {}, "regulator": {}, "committee": {}, "service": {},
			"bureau": {}, "office": {}, "council": {}, "commission": {},
		}
		if _, ok := allowedGovTypes[f.GovType]; !ok && f.GovType != "" {
			// Unknown govType - keep it but don't block (best-effort).
		}
		// Normalize parent gov path.
		f.ParentGovPath = strings.TrimSpace(f.ParentGovPath)
		if f.ParentGovPath != "" && !strings.HasPrefix(f.ParentGovPath, "/gov/") {
			f.ParentGovPath = "" // invalid parent path, clear it
		}
	}

	// Best-effort: normalize COMMUNITY-specific fields (when Type == COMMUNITY).
	if f.Type == FragmentTypeCommunity {
		// Normalize communityType to lowercase.
		f.CommunityType = strings.ToLower(strings.TrimSpace(f.CommunityType))
		// Validate communityType against allowed values (best-effort, non-blocking).
		allowedCommunityTypes := map[string]struct{}{
			"trade": {}, "activist": {}, "professional": {}, "criminal": {},
			"religious": {}, "ethnic": {}, "interest": {}, "dynasty": {}, "tribe": {},
		}
		if _, ok := allowedCommunityTypes[f.CommunityType]; !ok && f.CommunityType != "" {
			// Unknown communityType - keep it but don't block (best-effort).
		}
		// Normalize communityEstimatedSize to lowercase.
		f.CommunityEstimatedSize = strings.ToLower(strings.TrimSpace(f.CommunityEstimatedSize))
		// Validate against allowed values.
		allowedSizes := map[string]struct{}{
			"small": {}, "medium": {}, "large": {}, "massive": {}, "unknown": {},
		}
		if _, ok := allowedSizes[f.CommunityEstimatedSize]; !ok && f.CommunityEstimatedSize != "" {
			f.CommunityEstimatedSize = "unknown"
		}
		// Normalize affiliated org path.
		f.AffiliatedOrgPath = strings.TrimSpace(f.AffiliatedOrgPath)
		if f.AffiliatedOrgPath != "" && !strings.HasPrefix(f.AffiliatedOrgPath, "/org/") {
			f.AffiliatedOrgPath = "" // invalid org path, clear it
		}
		// Dedupe and normalize community platforms (media channel paths).
		if len(f.CommunityPlatforms) > 0 {
			seen := map[string]struct{}{}
			out := make([]string, 0, len(f.CommunityPlatforms))
			for _, p := range f.CommunityPlatforms {
				p = strings.TrimSpace(p)
				if p == "" || !strings.HasPrefix(p, "/media/") {
					continue
				}
				if _, ok := seen[p]; ok {
					continue
				}
				seen[p] = struct{}{}
				out = append(out, p)
			}
			f.CommunityPlatforms = out
		}

		// Validate canonical path format: /community/{geo}/{type}/{slug}
		cp := strings.TrimSpace(f.CanonicalPath)
		if strings.HasPrefix(cp, "/community/") {
			parts := strings.Split(strings.Trim(cp, "/"), "/")
			// community/{geo}/{type}/{slug} = 4 parts
			if len(parts) == 4 {
				// Normalize geo to lowercase
				parts[1] = strings.ToLower(parts[1])
				// Normalize type to lowercase
				parts[2] = strings.ToLower(parts[2])
				f.CanonicalPath = "/" + strings.Join(parts, "/")
			}
		}
	}

	// Best-effort: compute derived market segment normals for stable UI + indexing.
	f.MarketSegmentNorms, f.MarketSegmentNormDomains, f.MarketSegmentNormVerticals, f.MarketSegmentNormPairs =
		taxonomy.NormalizeMarketSegments(f.MarketSegments)

	// Best-effort: normalize user-provided tags and inject deterministic business-relevance taxonomy tags.
	// This is intentionally fail-open and does not block persistence.
	f.Tags = taxonomy.NormalizeFragmentTagsWithBusiness(
		f.Tags,
		string(f.Type),
		f.CanonicalPath,
		f.MarketSegments,
		f.OrgSize,
		f.MarketDomain,
	)

	// Best-effort: ensure indexPaths is always present for new/updated fragments.
	// This keeps expand/graph filtering deterministic without requiring backfills.
	f.IndexPaths = BuildIndexPaths(f, nil)
	return nil
}

func (f *HorizonFragment) normalizeNameHistory() {
	if f == nil || len(f.NameHistory) == 0 {
		return
	}
	out := make([]NameHistoryEntry, 0, len(f.NameHistory))
	seen := make(map[string]struct{}, len(f.NameHistory))
	for _, e := range f.NameHistory {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(e.Kind))
		switch kind {
		case "short", "legal", "brand", "aka":
			// allowed
		default:
			kind = "aka"
		}
		key := kind + "|" + strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, NameHistoryEntry{
			Name:      name,
			Kind:      kind,
			ValidFrom: e.ValidFrom,
			ValidTo:   e.ValidTo,
		})
	}
	if len(out) == 0 {
		f.NameHistory = nil
		return
	}
	f.NameHistory = out
}

// ensureWikidataMirroring best-effort keeps Wikidata external ID mirrored in both:
// - KBRefs (KB="Wikidata", ExternalID="Q...")
// - Identifiers["wikidataId"] ("Q...")
//
// This function is intentionally conservative:
// - If a Wikidata KBRef exists, it is treated as the source of truth.
// - If only identifiers contain a Wikidata ID, we add a KBRef.
// - If both exist but differ, we overwrite Identifiers["wikidataId"] to match KBRefs.
func (f *HorizonFragment) ensureWikidataMirroring() {
	if f == nil {
		return
	}

	// 1) Read from KBRefs first (source of truth if present).
	kbWid := ""
	kbIdx := -1
	for i, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "Wikidata") {
			kbWid = strings.TrimSpace(ref.ExternalID)
			kbIdx = i
			break
		}
	}

	// 2) Read from Identifiers if present.
	idWid := ""
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["wikidataId"]); v != "" {
			idWid = v
		} else if v := strings.TrimSpace(f.Identifiers["wikidata"]); v != "" {
			// Legacy key support.
			idWid = v
		}
	}

	// Choose authoritative ID.
	wid := kbWid
	if wid == "" {
		wid = idWid
	}
	if wid == "" {
		return
	}

	// Ensure identifiers map exists and contains wikidataId.
	if f.Identifiers == nil {
		f.Identifiers = make(map[string]string)
	}
	f.Identifiers["wikidataId"] = wid

	// Ensure KBRefs contains Wikidata ref.
	if kbIdx >= 0 {
		// Normalize casing and ensure ExternalID is set (don't downgrade confidence).
		f.KBRefs[kbIdx].KB = "Wikidata"
		if strings.TrimSpace(f.KBRefs[kbIdx].ExternalID) == "" {
			f.KBRefs[kbIdx].ExternalID = wid
		}
		return
	}
	f.KBRefs = append(f.KBRefs, KBReference{
		KB:         "Wikidata",
		ExternalID: wid,
		Confidence: 1.0,
	})
}

// ensureGeoNamesMirroring best-effort keeps GeoNames external ID mirrored in both:
// - KBRefs (KB="GeoNames", ExternalID="292968")
// - Identifiers["geonamesId"] ("292968")
//
// This function is intentionally conservative:
// - If a GeoNames KBRef exists, it is treated as the source of truth.
// - If only identifiers contain a GeoNames ID, we add a KBRef.
// - If both exist but differ, we overwrite Identifiers["geonamesId"] to match KBRefs.
func (f *HorizonFragment) ensureGeoNamesMirroring() {
	if f == nil {
		return
	}

	// 1) Read from KBRefs first (source of truth if present).
	kbID := ""
	kbIdx := -1
	for i, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "GeoNames") {
			kbID = strings.TrimSpace(ref.ExternalID)
			kbIdx = i
			break
		}
	}

	// 2) Read from Identifiers if present.
	idID := ""
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["geonamesId"]); v != "" {
			idID = v
		} else if v := strings.TrimSpace(f.Identifiers["geonameId"]); v != "" {
			// Legacy key support.
			idID = v
		} else if v := strings.TrimSpace(f.Identifiers["geonames"]); v != "" {
			// Very legacy key support.
			idID = v
		}
	}

	// Choose authoritative ID.
	gid := kbID
	if gid == "" {
		gid = idID
	}
	if gid == "" {
		return
	}

	// Ensure identifiers map exists and contains geonamesId.
	if f.Identifiers == nil {
		f.Identifiers = make(map[string]string)
	}
	f.Identifiers["geonamesId"] = gid

	// Ensure KBRefs contains GeoNames ref.
	if kbIdx >= 0 {
		// Normalize casing and ensure ExternalID is set (don't downgrade confidence).
		f.KBRefs[kbIdx].KB = "GeoNames"
		if strings.TrimSpace(f.KBRefs[kbIdx].ExternalID) == "" {
			f.KBRefs[kbIdx].ExternalID = gid
		}
		return
	}
	f.KBRefs = append(f.KBRefs, KBReference{
		KB:         "GeoNames",
		ExternalID: gid,
		Confidence: 1.0,
	})
}

// ensureCoordinatesMirroring best-effort keeps generic fragment coordinates populated from:
// 1) explicit fragment coordinates,
// 2) track coordinates,
// 3) horizonMonitor coordinates,
// 4) homeLocation ISO6709.
func (f *HorizonFragment) ensureCoordinatesMirroring() {
	if f == nil {
		return
	}

	if f.Coordinates != nil && IsValidCoordinates(f.Coordinates.Lat, f.Coordinates.Lon) {
		return
	}
	f.Coordinates = nil

	if f.Track != nil && IsValidCoordinates(f.Track.Lat, f.Track.Lon) {
		f.Coordinates = &Coordinates{Lat: f.Track.Lat, Lon: f.Track.Lon}
		return
	}

	if wm := f.EffectiveHorizonMonitor(); wm != nil && wm.Coordinates != nil &&
		IsValidCoordinates(wm.Coordinates.Lat, wm.Coordinates.Lon) {
		f.Coordinates = &Coordinates{
			Lat: wm.Coordinates.Lat,
			Lon: wm.Coordinates.Lon,
		}
		return
	}

	if f.HomeLocation != nil && f.HomeLocation.ISO6709 != nil {
		if lat, lon, ok := ParseISO6709Coordinates(*f.HomeLocation.ISO6709); ok {
			f.Coordinates = &Coordinates{Lat: lat, Lon: lon}
		}
	}
}

// GenerateVectorText генерирует vectorText для семантического поиска
// См. docs/horizon/architecture/semantic-search.md
func (f *HorizonFragment) GenerateVectorText() string {
	var parts []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}

	primaryLabel := ""
	if name, ok := f.Labels["en"]; ok && strings.TrimSpace(name) != "" {
		primaryLabel = strings.TrimSpace(name)
	} else {
		for _, label := range f.Labels {
			if strings.TrimSpace(label) != "" {
				primaryLabel = strings.TrimSpace(label)
				break
			}
		}
	}

	// Основная идентичность
	if primaryLabel != "" {
		add(primaryLabel)
	}
	if f.Description != "" {
		add(f.Description)
	}

	// Классификация и путь
	if f.Type != "" || f.EntityPath != "" || f.SectorPath != "" {
		classParts := []string{}
		if f.Type != "" {
			classParts = append(classParts, fmt.Sprintf("type %s", f.Type))
		}
		if f.EntityPath != "" {
			classParts = append(classParts, fmt.Sprintf("entity %s", f.EntityPath))
		}
		if f.SectorPath != "" {
			classParts = append(classParts, fmt.Sprintf("sector %s", f.SectorPath))
		}
		add("Classification: " + strings.Join(classParts, ", "))
	}
	if f.CanonicalPath != "" {
		add("Canonical path: " + f.CanonicalPath)
	}

	// Алиасы из labels
	if len(f.Labels) > 0 {
		langs := make([]string, 0, len(f.Labels))
		for lang := range f.Labels {
			langs = append(langs, lang)
		}
		sort.Strings(langs)
		aliases := make([]string, 0, len(langs))
		for _, lang := range langs {
			if lang == "en" {
				continue
			}
			if label := strings.TrimSpace(f.Labels[lang]); label != "" {
				aliases = append(aliases, label)
			}
		}
		if len(aliases) > 0 {
			add("Aliases: " + strings.Join(aliases, ", "))
		}
	}

	// Иерархические пути (дублируются в разных форматах)
	if f.EntityPath != "" {
		pathParts := strings.Split(strings.Trim(f.EntityPath, "/"), "/")
		if len(pathParts) > 0 {
			add("Entity path tokens: " + strings.Join(pathParts, " "))
			add("Entity leaf: " + pathParts[len(pathParts)-1])
		}
	}
	if f.SectorPath != "" {
		sectorParts := strings.Split(strings.Trim(f.SectorPath, "/"), "/")
		if len(sectorParts) > 0 {
			add("Sector path tokens: " + strings.Join(sectorParts, " "))
			add("Sector leaf: " + sectorParts[len(sectorParts)-1])
		}
	}
	if len(f.IndustryPaths) > 0 {
		add("Industry paths: " + strings.Join(f.IndustryPaths, ", "))
	}

	// Маркет-сегменты и нормализации
	if len(f.MarketSegments) > 0 {
		add("Market segments: " + strings.Join(f.MarketSegments, ", "))
	}
	if len(f.MarketSegmentNormPairs) > 0 {
		add("Market segment norms: " + strings.Join(f.MarketSegmentNormPairs, ", "))
	}
	if f.OrgSize != "" {
		add("Organization size: " + f.OrgSize)
	}

	// Локация
	if f.Coordinates != nil {
		add(fmt.Sprintf("Coordinates: %.6f, %.6f", f.Coordinates.Lat, f.Coordinates.Lon))
	}
	if f.HomeLocation != nil {
		locParts := []string{}
		if f.HomeLocation.Name != "" {
			locParts = append(locParts, f.HomeLocation.Name)
		}
		if f.HomeLocation.ISO3166_1 != nil && strings.TrimSpace(*f.HomeLocation.ISO3166_1) != "" {
			locParts = append(locParts, fmt.Sprintf("ISO3166-1 %s", strings.TrimSpace(*f.HomeLocation.ISO3166_1)))
		}
		if f.HomeLocation.ISO3166_2 != nil && strings.TrimSpace(*f.HomeLocation.ISO3166_2) != "" {
			locParts = append(locParts, fmt.Sprintf("ISO3166-2 %s", strings.TrimSpace(*f.HomeLocation.ISO3166_2)))
		}
		if f.HomeLocation.ISO6709 != nil && strings.TrimSpace(*f.HomeLocation.ISO6709) != "" {
			locParts = append(locParts, fmt.Sprintf("ISO6709 %s", strings.TrimSpace(*f.HomeLocation.ISO6709)))
		}
		if len(locParts) > 0 {
			add("Location: " + strings.Join(locParts, ", "))
		}
	}

	if wm := f.EffectiveHorizonMonitor(); wm != nil {
		if wm.ObjectKind != "" {
			add("HorizonMonitor kind: " + string(wm.ObjectKind))
		}
		if wm.SourceObjectID != "" {
			add("HorizonMonitor source object: " + wm.SourceObjectID)
		}
		if wm.Coordinates != nil {
			add(fmt.Sprintf("HorizonMonitor coordinates: %.6f, %.6f", wm.Coordinates.Lat, wm.Coordinates.Lon))
		}
		if len(wm.Countries) > 0 {
			add("HorizonMonitor countries: " + strings.Join(wm.Countries, ", "))
		}
		if wm.FacilityType != "" {
			add("HorizonMonitor facility type: " + wm.FacilityType)
		}
		if wm.MarketType != "" {
			add("HorizonMonitor market type: " + wm.MarketType)
		}
		if wm.PipelineType != "" {
			add("HorizonMonitor pipeline type: " + wm.PipelineType)
		}
		if wm.PortType != "" {
			add("HorizonMonitor port type: " + wm.PortType)
		}
		if wm.Mineral != "" {
			add("HorizonMonitor mineral: " + wm.Mineral)
		}
		if wm.Operator != "" {
			add("HorizonMonitor operator: " + wm.Operator)
		}
		if wm.Status != "" {
			add("HorizonMonitor status: " + wm.Status)
		}
		if wm.Capacity != "" {
			add("HorizonMonitor capacity: " + wm.Capacity)
		}
		if wm.CapacityTbps != nil {
			add(fmt.Sprintf("HorizonMonitor capacity tbps: %.2f", *wm.CapacityTbps))
		}
	}
	if f.Track != nil {
		tr := f.Track
		if tr.Kind != "" {
			add("Track kind: " + string(tr.Kind))
		}
		if tr.ObjectType != "" {
			add("Track object type: " + tr.ObjectType)
		}
		if tr.ExternalTrackID != "" {
			add("Track external id: " + tr.ExternalTrackID)
		}
		add(fmt.Sprintf("Track coordinates: %.6f, %.6f", tr.Lat, tr.Lon))
		if tr.HeadingDeg != nil {
			add(fmt.Sprintf("Track heading: %.1f deg", *tr.HeadingDeg))
		}
		if tr.SpeedKts != nil {
			add(fmt.Sprintf("Track speed: %.1f kts", *tr.SpeedKts))
		}
		if tr.AltitudeM != nil {
			add(fmt.Sprintf("Track altitude: %.1f m", *tr.AltitudeM))
		}
		if tr.DepthM != nil {
			add(fmt.Sprintf("Track depth: %.1f m", *tr.DepthM))
		}
		if !tr.LastSeenAt.IsZero() {
			add("Track last seen: " + tr.LastSeenAt.UTC().Format(time.RFC3339))
		}
		if tr.GeoHash != "" {
			add("Track geohash: " + tr.GeoHash)
		}
	}

	// Метрики
	if len(f.State) > 0 {
		metricKeys := make([]string, 0, len(f.State))
		for key := range f.State {
			metricKeys = append(metricKeys, key)
		}
		sort.Strings(metricKeys)
		for _, metricName := range metricKeys {
			metricValue := f.State[metricName]
			metricNameHuman := strings.ReplaceAll(metricName, "_", " ")
			line := fmt.Sprintf("Metric %s value %.4f", metricNameHuman, metricValue.Value)
			if metricValue.Unit != nil && strings.TrimSpace(*metricValue.Unit) != "" {
				line += " " + strings.TrimSpace(*metricValue.Unit)
			}
			add(line)
		}
	}

	// Историческая история имён (для поиска по бывшим названиям)
	if len(f.NameHistory) > 0 {
		names := make([]string, 0, len(f.NameHistory))
		for _, nh := range f.NameHistory {
			name := strings.TrimSpace(nh.Name)
			if name == "" {
				continue
			}
			names = append(names, name)
		}
		if len(names) > 0 {
			add("Former names: " + strings.Join(names, ", "))
		}
	}

	// Идентификаторы
	if len(f.Identifiers) > 0 {
		keys := make([]string, 0, len(f.Identifiers))
		for key := range f.Identifiers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, key := range keys {
			if value := strings.TrimSpace(f.Identifiers[key]); value != "" {
				pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
			}
		}
		if len(pairs) > 0 {
			add("Identifiers: " + strings.Join(pairs, ", "))
		}
	}

	// Теги и метки
	if len(f.Tags) > 0 {
		add("Tags: " + strings.Join(f.Tags, ", "))
	}

	// KBRefs
	if len(f.KBRefs) > 0 {
		kbParts := make([]string, 0, len(f.KBRefs))
		for _, kbRef := range f.KBRefs {
			if strings.TrimSpace(kbRef.KB) == "" && strings.TrimSpace(kbRef.ExternalID) == "" {
				continue
			}
			kbParts = append(kbParts, fmt.Sprintf("%s:%s", strings.TrimSpace(kbRef.KB), strings.TrimSpace(kbRef.ExternalID)))
		}
		if len(kbParts) > 0 {
			add("Knowledge bases: " + strings.Join(kbParts, ", "))
		}
	}

	// Статус и эпистемика
	if f.Status != "" {
		add("Status: " + f.Status)
	}
	if f.Metadata.EpistemicStatus != "" {
		add("Epistemic status: " + string(f.Metadata.EpistemicStatus))
	}

	return strings.Join(parts, "\n")
}
