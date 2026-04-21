package models

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
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
	// GeoSlug is a unified top-level geo token derived from canonicalPath (preferred) or homeLocation.
	// It enables cross-namespace filtering without querying namespace-specific nested fields.
	GeoSlug string `json:"geoSlug,omitempty" firestore:"geoSlug,omitempty"`
	// Class is the canonical AgentHorde fragment class (org, site, product,
	// decision, mechanism, …). Replaces the legacy `Type FragmentType`
	// field. Wire shape: top-level `class` JSON/Firestore key.
	Class ah.FragmentClass `json:"class" firestore:"class"`
	// Subtype refines Class with an open-ended (org, site, geo, project)
	// or closed-enum (community, decision, mechanism, evidence,
	// mobile_asset, product) discriminator. See ah.IsKnownSubtype /
	// models.NormalizeSubtype for validation rules.
	Subtype    string `json:"subtype,omitempty" firestore:"subtype,omitempty"`
	EntityPath string `json:"entityPath" firestore:"entityPath"` // /Organization/Corporation/Public
	// IndustryPaths are canonical, multi-valued industry classifications (Horizon-first taxonomy).
	// Format: /industry/{sector}/{group}/{industry}/{subindustry}
	IndustryPaths []string `json:"industryPaths,omitempty" firestore:"industryPaths,omitempty"`

	// MarketSegments are canonical, multi-valued market segment paths for organizations/facilities.
	// Canonical format: /segment/{domain}/{vertical}/{geo}
	// - geo: ISO3166-1 alpha2 lowercase (2 chars) or "global"
	// This is an ORG-level classification (not a separate fragment type in MVP).
	MarketSegments []string `json:"marketSegments,omitempty" firestore:"marketSegments,omitempty"`

	// MarketSegmentNorms are derived, normalized "market coordinates" for each MarketSegment entry.
	// They are computed from code-defined normalization rules (with optional runtime dictionary overlay) and are intended for:
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

	// --- AFFECTED_ZONE-specific (when Type == AFFECTED_ZONE) ---
	// RadiusKm is the approximate radius of the affected zone in kilometers (best-effort).
	// Used for exposure analysis and geographic scope estimation.
	RadiusKm float64 `json:"radiusKm,omitempty" firestore:"radiusKm,omitempty"`

	// HasCoordinates is set to true at write time when the fragment has valid geo coordinates.
	// Enables efficient Firestore queries for map rendering without 4x overscan.
	HasCoordinates bool `json:"hasCoordinates,omitempty" firestore:"hasCoordinates,omitempty"`

	// IndexPaths — денормализованные "координаты" фрагмента для детерминированного картографирования/фильтрации.
	// Собирается из canonicalPath + (industryPaths/entityPath + ancestors) + location + tags + identifiers + (optional spheres).
	// Ограничено по размеру (см. BuildIndexPaths/NormalizeIndexPaths).
	IndexPaths []string `json:"indexPaths,omitempty" firestore:"indexPaths,omitempty"`

	// SpherePaths are canonical sphere-of-activity classifications applicable to all entity types.
	// Format: /sphere/{L1}/{L2}/{L3...}. Multi-valued. Inferred from path, type, edges, and LLM enrichment.
	SpherePaths []string `json:"spherePaths,omitempty" firestore:"spherePaths,omitempty"`
	// ResolutionHint is a scale annotation: macro (top-level entity), unit (subdivision/segment),
	// detail (concrete object), or empty (unspecified). Not validated, best-effort.
	ResolutionHint string `json:"resolutionHint,omitempty" firestore:"resolutionHint,omitempty"`
	// ParentPath is the canonical path of the parent fragment in the hierarchy (universal field).
	// Synced from ParentGovPath, ParentMarketPath for backward compatibility.
	ParentPath string `json:"parentPath,omitempty" firestore:"parentPath,omitempty"`
	// GeoFeatureType classifies geographic entities (LOC). Values: city, region, country, strait, canal, port, pass, border-crossing, pipeline-corridor, airspace, other.
	// Chokepoint subset: strait, canal, pass, pipeline-corridor (used for propagation amplification).
	GeoFeatureType string `json:"geoFeatureType,omitempty" firestore:"geoFeatureType,omitempty"`

	// Связь с Entity
	EntityID *string `json:"entityId,omitempty" firestore:"entityId,omitempty"`

	// Гибкие метрики
	State map[string]MetricValue `json:"state" firestore:"state"`
	// Attributes is the AH-canonical attribute bag (carrier=fragment in
	// ATTRIBUTE_SPEC). Stores AH-promoted concepts that don't have a
	// dedicated typed Go field (lifecycle_stage mirrors, parent_path
	// fold, route_overlay_key, vessel_class_path, …). Use the typed
	// accessor methods (f.GeoSlug() / f.SetSpherePaths(...)) instead of
	// poking the map directly.
	Attributes map[string]interface{} `json:"attributes,omitempty" firestore:"attributes,omitempty"`
	// Qualifiers stores policy/runtime annotations (e.g., policyStatus/policyReason).
	Qualifiers map[string]interface{} `json:"qualifiers,omitempty" firestore:"qualifiers,omitempty"`

	// Обогащение данными
	KBRefs      []KBReference     `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`
	// NameHistory stores temporal aliases/rebrands/legacy names for explainable org identity transitions.
	NameHistory []NameHistoryEntry `json:"nameHistory,omitempty" firestore:"nameHistory,omitempty"`

	// AlternativeNames stores alternative entity names discovered during dedup merges.
	// These are raw names (not normalized) for future lookup and display.
	AlternativeNames []string `json:"alternativeNames,omitempty" firestore:"alternativeNames,omitempty"`
	// AlternativePaths stores canonical paths of fragments that were merged into this one.
	// Used by redirect lookup and alias resolution to avoid re-creating duplicates.
	AlternativePaths []string `json:"alternativePaths,omitempty" firestore:"alternativePaths,omitempty"`

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

	// Supply-link modeled as site/subtype supply_link (or /supply-link/ path).
	// Fields support leg materialization and route variants alongside `supplies` edges.
	SupplierRef         *string  `json:"supplierRef,omitempty" firestore:"supplierRef,omitempty"`                 // MUST be /fac/... or /deposit/... for extraction. NOT /org/. (nil = natural resource)
	CustomerRef         *string  `json:"customerRef,omitempty" firestore:"customerRef,omitempty"`                 // MUST be /fac/... NOT /org/. (nil = end consumer)
	ProductRef          *string  `json:"productRef,omitempty" firestore:"productRef,omitempty"`                   // /product/... (required for site/subtype supply_link fragments)
	SupplierFacilityRef *string  `json:"supplierFacilityRef,omitempty" firestore:"supplierFacilityRef,omitempty"` // /fac/... (optional)
	CustomerFacilityRef *string  `json:"customerFacilityRef,omitempty" firestore:"customerFacilityRef,omitempty"` // /fac/... (optional)
	TransportViaRefs    []string `json:"transportViaRefs,omitempty" firestore:"transportViaRefs,omitempty"`       // ordered /fac/... paths (ports, terminals, pipelines, warehouses)

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

// IsSupplyLinkFragment reports a supply-link modeled as site/subtype supply_link
// or a /supply-link/ canonical path (companion to supplies edges / legs).
func (f *HorizonFragment) IsSupplyLinkFragment() bool {
	if f == nil {
		return false
	}
	if ClassSubtypeSupplyLink.MatchesFragment(f) {
		return true
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	return strings.HasPrefix(strings.ToLower(cp), "/supply-link/")
}

// IsProgramOrg reports Class=org, Subtype=program.
func (f *HorizonFragment) IsProgramOrg() bool {
	if f == nil {
		return false
	}
	return ClassSubtypeProgram.MatchesFragment(f)
}

// IsMediaChannelOrg reports Class=org, Subtype=media_channel.
func (f *HorizonFragment) IsMediaChannelOrg() bool {
	if f == nil {
		return false
	}
	return ClassSubtypeMediaChannel.MatchesFragment(f)
}

// IsDepositSite reports Class=site, Subtype=deposit.
func (f *HorizonFragment) IsDepositSite() bool {
	if f == nil {
		return false
	}
	return ClassSubtypeDeposit.MatchesFragment(f)
}

// IsAffectedZoneGeo reports Class=geo, Subtype=affected_zone.
func (f *HorizonFragment) IsAffectedZoneGeo() bool {
	if f == nil {
		return false
	}
	return ClassSubtypeAffectedZone.MatchesFragment(f)
}

// IsDomainScopeFragment reports domain workbench paths (/domain/… or historic /dom/…).
func (f *HorizonFragment) IsDomainScopeFragment() bool {
	if f == nil {
		return false
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	return strings.HasPrefix(cp, "/domain/") || strings.HasPrefix(cp, "/dom/")
}

// IsSphereScopeFragment reports sphere overlay paths (/sphere/… or historic /sph/…).
func (f *HorizonFragment) IsSphereScopeFragment() bool {
	if f == nil {
		return false
	}
	cp := strings.TrimSpace(f.CanonicalPath)
	return strings.HasPrefix(cp, "/sphere/") || strings.HasPrefix(cp, "/sph/")
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

	if f.Class == "" {
		return fmt.Errorf("class is required")
	}
	if !ah.IsKnownFragmentClass(f.Class) {
		return fmt.Errorf("unknown fragment class: %q", f.Class)
	}
	if f.Subtype != "" && !IsKnownSubtype(f.Class, f.Subtype) {
		return fmt.Errorf("subtype %q is not valid for class %q", f.Subtype, f.Class)
	}

	// Supply-link fragments use Class=site, Subtype=supply_link (tenant-scoped).
	if f.IsSupplyLinkFragment() {
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
	// Best-effort: keep OpenCorporates linkage consistent across both KBRefs and Identifiers.
	// OpenCorporates ExternalID format: jurisdiction_code/company_number (e.g. "gb/00102498").
	f.ensureOpenCorporatesMirroring()
	// Best-effort: keep generic coordinates filled from canonical geo-bearing fields.
	f.ensureCoordinatesMirroring()
	// Keep dual field compatibility: canonical horizonMonitor + legacy worldMonitor.
	f.SyncHorizonMonitorAliases()

	// Best-effort: normalize org-level classifiers (safe, deterministic).
	// These fields can be produced by Scout/Research and should be canonicalized before persistence.
	f.MarketSegments = taxonomy.NormalizeSegmentPaths(f.MarketSegments)
	f.OrgSize = taxonomy.NormalizeOrgSize(f.OrgSize)
	f.IndustryPaths = taxonomy.NormalizeIndustryPaths(f.IndustryPaths, string(f.Class))

	// Best-effort: keep site physical subtype deterministic at the model
	// boundary. This guarantees facilityKind is populated even when writes
	// bypass scout precreate flow (e.g., graph-intent apply,
	// low-confidence stubs, admin/tools paths). Triggers on:
	//   - new shape: Class=site (canonical path /site/...)
	//   - legacy shape: canonical path still starts with /fac/ (pre-
	//     migration docs not yet rewritten by migrate-cli).
	if f.Class == ah.FragmentClassSite || strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.CanonicalPath)), "/fac/") {
		if f.Identifiers == nil {
			f.Identifiers = map[string]string{}
		}
		curRaw := strings.TrimSpace(f.Identifiers["facilityKind"])
		curNorm := taxonomy.NormalizeFacilityKind(curRaw)
		if curNorm != taxonomy.FacilityKindUnknown && curNorm != taxonomy.FacilityKindOther {
			f.Identifiers["facilityKind"] = string(curNorm)
		} else {
			kind := taxonomy.InferFacilityKind(
				f.CanonicalPath,
				f.Labels["en"],
				f.Description,
				f.EntityPath,
				strings.Join(f.IndustryPaths, " "),
				f.Identifiers["facilityType"],
				f.Identifiers["supplyChainRole"],
			)
			if kind == taxonomy.FacilityKindUnknown {
				kind = taxonomy.FacilityKindOther
			}
			f.Identifiers["facilityKind"] = string(kind)
		}
	}

	// SpherePaths: normalize via L1/L2/L3+ taxonomy rules.
	if len(f.SpherePaths) > 0 {
		f.SpherePaths = taxonomy.NormalizeSpherePaths(f.SpherePaths)
	}
	// ResolutionHint: lowercase, validate against macro|unit|detail (empty = unspecified).
	if h := strings.ToLower(strings.TrimSpace(f.ResolutionHint)); h != "" {
		switch h {
		case "macro", "unit", "detail":
			f.ResolutionHint = h
		default:
			f.ResolutionHint = ""
		}
	}
	// ParentPath: sync from type-specific parent fields if empty.
	if f.ParentPath == "" {
		if f.ParentGovPath != "" {
			f.ParentPath = f.ParentGovPath
		} else if f.ParentMarketPath != "" {
			f.ParentPath = f.ParentMarketPath
		}
	}
	f.ParentPath = strings.TrimSpace(f.ParentPath)

	// Best-effort: normalize MARKET canonical path + fields (non-blocking).
	if f.Class == ah.FragmentClassMarket {
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

	// Best-effort: normalize GOV-specific fields. Triggers on:
	//   - new shape: Class=org && Subtype=gov (canonical path /org/{geo}/gov/{slug})
	//   - legacy shape: canonical path still starts with /gov/ (pre-
	//     migration docs not yet rewritten by migrate-cli).
	if (f.Class == ah.FragmentClassOrg && f.Subtype == "gov") ||
		strings.HasPrefix(f.CanonicalPath, "/gov/") {
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

	// Best-effort: normalize COMMUNITY-specific fields (when Class == community).
	if f.Class == ah.FragmentClassCommunity {
		// Normalize communityType to lowercase.
		f.CommunityType = strings.ToLower(strings.TrimSpace(f.CommunityType))
		// Validate communityType against allowed values (best-effort, non-blocking).
		allowedCommunityTypes := map[string]struct{}{
			"trade": {}, "activist": {}, "professional": {}, "criminal": {},
			"religious": {}, "ethnic": {}, "interest": {}, "dynasty": {}, "tribe": {},
			"representatives": {}, "industry-association": {}, "key-community": {},
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
		string(f.Class),
		f.CanonicalPath,
		f.MarketSegments,
		f.OrgSize,
		f.MarketDomain,
	)

	// Chokepoint tagging: LOC fragments with geoFeatureType in (strait, canal, pass, pipeline-corridor) get tag "chokepoint".
	if gt := strings.ToLower(strings.TrimSpace(f.GeoFeatureType)); gt != "" {
		switch gt {
		case "strait", "canal", "pass", "pipeline-corridor":
			f.Tags = appendTagIfMissing(f.Tags, "chokepoint")
		}
	}

	// Unified geo filter coordinate. Prefer canonicalPath semantics over inferred homeLocation.
	f.GeoSlug = DeriveGeoSlug(f.CanonicalPath, f.HomeLocation)

	// Best-effort: ensure indexPaths is always present for new/updated fragments.
	// This keeps expand/graph filtering deterministic without requiring backfills.
	f.IndexPaths = BuildIndexPaths(f, nil)
	return nil
}

func appendTagIfMissing(tags []string, tag string) []string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return tags
	}
	for _, t := range tags {
		if strings.ToLower(strings.TrimSpace(t)) == tag {
			return tags
		}
	}
	return append(tags, tag)
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

// ensureOpenCorporatesMirroring best-effort keeps OpenCorporates ID mirrored in both:
// - KBRefs (KB="OpenCorporates", ExternalID="gb/00102498")
// - Identifiers["openCorporatesId"] ("gb/00102498")
func (f *HorizonFragment) ensureOpenCorporatesMirroring() {
	if f == nil {
		return
	}
	kbID := ""
	kbIdx := -1
	for i, ref := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(ref.KB), "OpenCorporates") {
			kbID = strings.TrimSpace(ref.ExternalID)
			kbIdx = i
			break
		}
	}
	idID := ""
	if f.Identifiers != nil {
		idID = strings.TrimSpace(f.Identifiers["openCorporatesId"])
	}
	ocID := kbID
	if ocID == "" {
		ocID = idID
	}
	if ocID == "" {
		return
	}
	if f.Identifiers == nil {
		f.Identifiers = make(map[string]string)
	}
	f.Identifiers["openCorporatesId"] = ocID
	if kbIdx >= 0 {
		f.KBRefs[kbIdx].KB = "OpenCorporates"
		if strings.TrimSpace(f.KBRefs[kbIdx].ExternalID) == "" {
			f.KBRefs[kbIdx].ExternalID = ocID
		}
		return
	}
	f.KBRefs = append(f.KBRefs, KBReference{
		KB:         "OpenCorporates",
		ExternalID: ocID,
		Confidence: 0.85,
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
		f.HasCoordinates = true
		return
	}
	f.Coordinates = nil

	if f.Track != nil && IsValidCoordinates(f.Track.Lat, f.Track.Lon) {
		f.Coordinates = &Coordinates{Lat: f.Track.Lat, Lon: f.Track.Lon}
		f.HasCoordinates = true
		return
	}

	if wm := f.EffectiveHorizonMonitor(); wm != nil && wm.Coordinates != nil &&
		IsValidCoordinates(wm.Coordinates.Lat, wm.Coordinates.Lon) {
		f.Coordinates = &Coordinates{
			Lat: wm.Coordinates.Lat,
			Lon: wm.Coordinates.Lon,
		}
		f.HasCoordinates = true
		return
	}

	if f.HomeLocation != nil && f.HomeLocation.ISO6709 != nil {
		if lat, lon, ok := ParseISO6709Coordinates(*f.HomeLocation.ISO6709); ok {
			f.Coordinates = &Coordinates{Lat: lat, Lon: lon}
			f.HasCoordinates = true
			return
		}
	}

	f.HasCoordinates = false
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

	legalName := ""
	shortName := ""
	if f.Identifiers != nil {
		legalName = strings.TrimSpace(f.Identifiers["legalName"])
		shortName = strings.TrimSpace(f.Identifiers["shortName"])
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
	identityParts := make([]string, 0, 6)
	if primaryLabel != "" {
		identityParts = append(identityParts, fmt.Sprintf("label=%s", primaryLabel))
	}
	if f.Class != "" {
		// Embedding stability: keep the legacy `type=` envelope key so
		// already-indexed vectors still match newly-emitted text. The
		// value side moves to AH formatting (class[/subtype]).
		identityParts = append(identityParts, fmt.Sprintf("type=%s", FormatClassSubtype(f.Class, f.Subtype)))
	}
	if cp := strings.TrimSpace(f.CanonicalPath); cp != "" {
		identityParts = append(identityParts, fmt.Sprintf("canonicalPath=%s", cp))
	}
	if legalName != "" {
		identityParts = append(identityParts, fmt.Sprintf("legalName=%s", legalName))
	}
	if shortName != "" {
		identityParts = append(identityParts, fmt.Sprintf("shortName=%s", shortName))
	}
	if j := canonicalJurisdiction(f.CanonicalPath); j != "" {
		identityParts = append(identityParts, fmt.Sprintf("jurisdiction=%s", j))
	}
	if len(identityParts) > 0 {
		add("Identity envelope: " + strings.Join(identityParts, ", "))
	}
	if f.Description != "" {
		add(f.Description)
	}

	// Классификация и путь
	if f.Class != "" || f.EntityPath != "" || len(f.IndustryPaths) > 0 {
		classParts := []string{}
		if f.Class != "" {
			classParts = append(classParts, fmt.Sprintf("type %s", FormatClassSubtype(f.Class, f.Subtype)))
		}
		if f.EntityPath != "" {
			classParts = append(classParts, fmt.Sprintf("entity %s", f.EntityPath))
		}
		if len(f.IndustryPaths) > 0 {
			classParts = append(classParts, fmt.Sprintf("industries %s", strings.Join(f.IndustryPaths, ", ")))
		}
		add("Classification: " + strings.Join(classParts, ", "))
	}
	if f.CanonicalPath != "" {
		add("Canonical path: " + f.CanonicalPath)
	}
	if legalName != "" {
		add("Legal name: " + legalName)
	}
	if shortName != "" {
		add("Short name: " + shortName)
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

	// Смысловой контекст (окружение): родительские/связанные узлы для лучшей идентификации.
	contextPaths := make([]string, 0, 10)
	if v := strings.TrimSpace(f.ParentPath); v != "" {
		contextPaths = append(contextPaths, "parent="+v)
	}
	if v := strings.TrimSpace(f.ParentGovPath); v != "" {
		contextPaths = append(contextPaths, "parentGov="+v)
	}
	if v := strings.TrimSpace(f.ParentMarketPath); v != "" {
		contextPaths = append(contextPaths, "parentMarket="+v)
	}
	if v := strings.TrimSpace(f.AffiliatedOrgPath); v != "" {
		contextPaths = append(contextPaths, "affiliatedOrg="+v)
	}
	if f.SupplierRef != nil && strings.TrimSpace(*f.SupplierRef) != "" {
		contextPaths = append(contextPaths, "supplier="+strings.TrimSpace(*f.SupplierRef))
	}
	if f.CustomerRef != nil && strings.TrimSpace(*f.CustomerRef) != "" {
		contextPaths = append(contextPaths, "customer="+strings.TrimSpace(*f.CustomerRef))
	}
	if f.ProductRef != nil && strings.TrimSpace(*f.ProductRef) != "" {
		contextPaths = append(contextPaths, "product="+strings.TrimSpace(*f.ProductRef))
	}
	if f.SupplierFacilityRef != nil && strings.TrimSpace(*f.SupplierFacilityRef) != "" {
		contextPaths = append(contextPaths, "supplierFacility="+strings.TrimSpace(*f.SupplierFacilityRef))
	}
	if f.CustomerFacilityRef != nil && strings.TrimSpace(*f.CustomerFacilityRef) != "" {
		contextPaths = append(contextPaths, "customerFacility="+strings.TrimSpace(*f.CustomerFacilityRef))
	}
	if len(f.TransportViaRefs) > 0 {
		contextPaths = append(contextPaths, "transportVia="+strings.Join(f.TransportViaRefs, ","))
	}
	if len(contextPaths) > 0 {
		add("Neighborhood context: " + strings.Join(contextPaths, ", "))
	}

	// Иерархические пути (дублируются в разных форматах)
	if f.EntityPath != "" {
		pathParts := strings.Split(strings.Trim(f.EntityPath, "/"), "/")
		if len(pathParts) > 0 {
			add("Entity path tokens: " + strings.Join(pathParts, " "))
			add("Entity leaf: " + pathParts[len(pathParts)-1])
		}
	}
	if len(f.IndustryPaths) > 0 {
		add("Industry paths: " + strings.Join(f.IndustryPaths, ", "))
	}
	if len(f.SpherePaths) > 0 {
		add("Sphere paths: " + strings.Join(f.SpherePaths, ", "))
	}
	if len(f.IndexPaths) > 0 {
		indexPaths := append([]string(nil), f.IndexPaths...)
		sort.Strings(indexPaths)
		const maxIndexPaths = 20
		if len(indexPaths) > maxIndexPaths {
			add("Index paths: " + strings.Join(indexPaths[:maxIndexPaths], ", "))
			add(fmt.Sprintf("Index paths omitted: %d", len(indexPaths)-maxIndexPaths))
		} else {
			add("Index paths: " + strings.Join(indexPaths, ", "))
		}
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
	if f.MarketDomain != "" {
		add("Market domain: " + f.MarketDomain)
	}
	if f.MarketVertical != "" {
		add("Market vertical: " + f.MarketVertical)
	}
	if len(f.MarketChannels) > 0 {
		add("Market channels: " + strings.Join(f.MarketChannels, ", "))
	}
	if f.GovType != "" {
		add("Government type: " + f.GovType)
	}
	if f.CommunityType != "" {
		add("Community type: " + f.CommunityType)
	}
	if len(f.CommunityPlatforms) > 0 {
		add("Community platforms: " + strings.Join(f.CommunityPlatforms, ", "))
	}
	if v := strings.TrimSpace(f.CommunityEstimatedSize); v != "" {
		add("Community estimated size: " + v)
	}
	if f.RadiusKm > 0 {
		add(fmt.Sprintf("Affected zone radius: %.1f km", f.RadiusKm))
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
		const maxMetrics = 40
		limit := len(metricKeys)
		if limit > maxMetrics {
			limit = maxMetrics
		}
		for _, metricName := range metricKeys[:limit] {
			metricValue := f.State[metricName]
			metricNameHuman := strings.ReplaceAll(metricName, "_", " ")
			line := fmt.Sprintf("Metric %s value %.4f", metricNameHuman, metricValue.Value)
			if metricValue.Unit != nil && strings.TrimSpace(*metricValue.Unit) != "" {
				line += " " + strings.TrimSpace(*metricValue.Unit)
			}
			if metricValue.Confidence > 0 {
				line += fmt.Sprintf(" confidence %.2f", metricValue.Confidence)
			}
			if !metricValue.ObservedAt.IsZero() {
				line += " observed " + metricValue.ObservedAt.UTC().Format("2006-01-02")
			}
			add(line)
		}
		if len(metricKeys) > maxMetrics {
			add(fmt.Sprintf("Metrics omitted: %d", len(metricKeys)-maxMetrics))
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

func canonicalJurisdiction(canonicalPath string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(canonicalPath), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	// New AH class-prefixes (canonical post-migration shape).
	case "org", "site", "geo", "person", "community", "market",
		"regime", "decision", "evidence", "mobile_asset", "project":
		return strings.TrimSpace(parts[1])
	// Legacy prefixes — kept for transitional reads of pre-migration
	// canonicalPath strings that scout/agentmesh might still emit.
	case "gov", "program", "loc":
		return strings.TrimSpace(parts[1])
	default:
		return ""
	}
}
