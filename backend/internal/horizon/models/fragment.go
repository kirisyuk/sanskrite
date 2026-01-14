package models

import (
	"fmt"
	"strings"
	"time"

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
	CanonicalPath string       `json:"canonicalPath" firestore:"canonicalPath"`
	Type          FragmentType `json:"type" firestore:"type"`
	EntityPath    string       `json:"entityPath" firestore:"entityPath"` // /Organization/Corporation/Public
	SectorPath    string       `json:"sectorPath" firestore:"sectorPath"` // /Technology/Semiconductors/Manufacturing
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

	// IndexPaths — денормализованные "координаты" фрагмента для детерминированного картографирования/фильтрации.
	// Собирается из canonicalPath + (sectorPath/entityPath + ancestors) + location + tags + identifiers + (optional spheres).
	// Ограничено по размеру (см. BuildIndexPaths/NormalizeIndexPaths).
	IndexPaths []string `json:"indexPaths,omitempty" firestore:"indexPaths,omitempty"`

	// Связь с Entity
	EntityID *string `json:"entityId,omitempty" firestore:"entityId,omitempty"`

	// Гибкие метрики
	State map[string]MetricValue `json:"state" firestore:"state"`

	// Обогащение данными
	KBRefs      []KBReference     `json:"kbRefs,omitempty" firestore:"kbRefs,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty" firestore:"identifiers,omitempty"`

	// Временная валидность
	ValidFrom *time.Time `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo   *time.Time `json:"validTo,omitempty" firestore:"validTo,omitempty"`

	// Location с ISO стандартами
	HomeLocation *Location `json:"homeLocation,omitempty" firestore:"homeLocation,omitempty"`

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
	SupplierFacilityRef *string `json:"supplierFacilityRef,omitempty" firestore:"supplierFacilityRef,omitempty"` // /fac/... (optional)
	CustomerFacilityRef *string `json:"customerFacilityRef,omitempty" firestore:"customerFacilityRef,omitempty"` // /fac/... (optional)

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

// Validate проверяет валидность HorizonFragment
func (f *HorizonFragment) Validate() error {
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
	// Best-effort: keep Wikidata linkage consistent across both KBRefs and Identifiers.
	// This enables reliable dedup/search (e.g., FindByIdentifier("wikidataId", "...")) even for legacy fragments
	// that may have stored Wikidata only in kbRefs.
	f.ensureWikidataMirroring()
	// Best-effort: keep GeoNames linkage consistent across both KBRefs and Identifiers.
	// GeoNames ExternalID is a numeric string (geonameId).
	f.ensureGeoNamesMirroring()

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

	// Best-effort: compute derived market segment normals for stable UI + indexing.
	f.MarketSegmentNorms, f.MarketSegmentNormDomains, f.MarketSegmentNormVerticals, f.MarketSegmentNormPairs =
		taxonomy.NormalizeMarketSegments(f.MarketSegments)

	// Best-effort: ensure indexPaths is always present for new/updated fragments.
	// This keeps expand/graph filtering deterministic without requiring backfills.
	f.IndexPaths = BuildIndexPaths(f, nil)
	return nil
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

// GenerateVectorText генерирует vectorText для семантического поиска
// См. docs/horizon/architecture/semantic-search.md
func (f *HorizonFragment) GenerateVectorText() string {
	var parts []string

	// Канонический путь и имя
	parts = append(parts, f.CanonicalPath)
	if name, ok := f.Labels["en"]; ok && name != "" {
		parts = append(parts, name)
	}
	// Добавляем статус для контекста
	parts = append(parts, fmt.Sprintf("Status: %s", f.Status))
	if f.Metadata.EpistemicStatus != "" {
		parts = append(parts, fmt.Sprintf("Epistemic Status: %s", f.Metadata.EpistemicStatus))
	}

	if f.Description != "" {
		parts = append(parts, f.Description)
	}

	// Алиасы из labels
	for lang, label := range f.Labels {
		if lang != "en" && label != "" {
			parts = append(parts, label)
		}
	}

	// Иерархические пути (дублируются в разных форматах)
	if f.EntityPath != "" {
		parts = append(parts, f.EntityPath)
		// Разбиваем путь и добавляем варианты
		pathParts := strings.Split(strings.Trim(f.EntityPath, "/"), "/")
		parts = append(parts, strings.Join(pathParts, " "))
		if len(pathParts) > 0 {
			parts = append(parts, pathParts[len(pathParts)-1]) // Последний элемент
		}
	}

	// Отраслевой путь
	if f.SectorPath != "" {
		parts = append(parts, f.SectorPath)
		sectorParts := strings.Split(strings.Trim(f.SectorPath, "/"), "/")
		parts = append(parts, strings.Join(sectorParts, " "))
		if len(sectorParts) > 0 {
			parts = append(parts, sectorParts[len(sectorParts)-1])
		}
	}

	// Локация
	if f.HomeLocation != nil {
		if f.HomeLocation.Name != "" {
			parts = append(parts, f.HomeLocation.Name)
		}
		if f.HomeLocation.ISO3166_1 != nil {
			parts = append(parts, fmt.Sprintf("ISO3166-1: %s", *f.HomeLocation.ISO3166_1))
			parts = append(parts, *f.HomeLocation.ISO3166_1)
		}
		if f.HomeLocation.ISO3166_2 != nil {
			parts = append(parts, fmt.Sprintf("ISO3166-2: %s", *f.HomeLocation.ISO3166_2))
			parts = append(parts, *f.HomeLocation.ISO3166_2)
		}
		if f.HomeLocation.ISO6709 != nil {
			parts = append(parts, fmt.Sprintf("ISO6709: %s", *f.HomeLocation.ISO6709))
		}
	}

	// Метрики (название и значение)
	for metricName, metricValue := range f.State {
		parts = append(parts, fmt.Sprintf("%s: %.2f", metricName, metricValue.Value))
		// Добавляем человекочитаемый вариант
		metricNameHuman := strings.ReplaceAll(metricName, "_", " ")
		parts = append(parts, fmt.Sprintf("%s %.0f%%", metricNameHuman, metricValue.Value*100))
		if metricValue.Unit != nil {
			parts = append(parts, fmt.Sprintf("%s %s", metricNameHuman, *metricValue.Unit))
		}
	}

	// Идентификаторы
	for key, value := range f.Identifiers {
		parts = append(parts, fmt.Sprintf("%s: %s", key, value))
		parts = append(parts, value) // Только значение тоже добавляем
	}

	// Теги и метки
	if len(f.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("Tags: %s", strings.Join(f.Tags, " ")))
		for _, tag := range f.Tags {
			parts = append(parts, tag)
		}
	}

	// KBRefs
	for _, kbRef := range f.KBRefs {
		parts = append(parts, fmt.Sprintf("%s: %s", kbRef.KB, kbRef.ExternalID))
		parts = append(parts, kbRef.ExternalID)
	}

	return strings.Join(parts, "\n")
}
