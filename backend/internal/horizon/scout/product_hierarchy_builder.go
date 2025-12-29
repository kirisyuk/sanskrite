package scout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// =============================================================================
// PRODUCT HIERARCHY POLICY
// =============================================================================
// Products form value chains where materials transform into other materials.
// This policy automatically creates TRANSFORMS_INTO edges based on Wikidata
// value chain relationships (P10565 "is produced from", P186 "material used").
//
// When a new product is created:
// 1. Extract upstream products (P10565, P186) from Wikidata
// 2. Extract downstream products (via SPARQL reverse lookup)
// 3. For each related product (up to 3 levels up, 3 levels down):
//    - Deduplicate and create if new
//    - Create TRANSFORMS_INTO edge
// 4. Fill entityPath/sectorPath from Wikidata (P31, P279, P452)
//
// TODO: Make hierarchy building asynchronous via job queue

// ProductHierarchyBuilder auto-creates product value chain hierarchy from Wikidata.
type ProductHierarchyBuilder struct {
	fragmentsRepo   *repo.HorizonFragmentsRepository
	edgesRepo       *repo.DependencyEdgesRepository
	wikidataClient  coreontology.WikidataClientInterface
	deduplicator    *ProductDeduplicator
	extractor       *ProductWikidataExtractor
	canonicalizer   *ProductCanonicalizer
	orgDeduplicator *OrgDeduplicator // For manufacturer/producer deduplication
}

// productHierarchyCache caches Wikidata data and fragments during hierarchy building.
type productHierarchyCache struct {
	productData map[string]*ProductWikidataData    // Q-ID -> extracted data
	fragments   map[string]*models.HorizonFragment // Q-ID -> fragment (existing or newly created)
	orgs        map[string]*models.HorizonFragment // Q-ID -> organization fragment (producers)
}

// NewProductHierarchyBuilder creates a new product hierarchy builder.
func NewProductHierarchyBuilder(
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	wikidataClient coreontology.WikidataClientInterface,
) *ProductHierarchyBuilder {
	return &ProductHierarchyBuilder{
		fragmentsRepo:   fragmentsRepo,
		edgesRepo:       edgesRepo,
		wikidataClient:  wikidataClient,
		deduplicator:    NewProductDeduplicator(fragmentsRepo, wikidataClient),
		extractor:       NewProductWikidataExtractor(wikidataClient),
		canonicalizer:   NewProductCanonicalizer(),
		orgDeduplicator: NewOrgDeduplicator(fragmentsRepo, wikidataClient),
	}
}

// EnsureProductHierarchy builds value chain hierarchy for a product fragment.
// Creates upstream (3 levels) and downstream (3 levels) products with TRANSFORMS_INTO edges.
func (b *ProductHierarchyBuilder) EnsureProductHierarchy(
	ctx context.Context,
	fragmentID string,
	fragment *models.HorizonFragment,
	provenanceMethod string,
	sourceID string,
) error {
	if b == nil || b.fragmentsRepo == nil || b.edgesRepo == nil {
		return nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypeProduct {
		return nil
	}
	if fragmentID == "" {
		return nil
	}

	// Get Wikidata Q-ID
	wikidataID := extractWikidataIDFromFragment(fragment)
	if wikidataID == "" {
		// Try to resolve from name
		name := strings.TrimSpace(fragment.Labels["en"])
		if name == "" {
			name = strings.TrimSpace(fragment.Identifiers["shortName"])
		}
		if name != "" && b.extractor != nil {
			if qID, err := b.extractor.SearchProduct(ctx, name); err == nil && qID != "" {
				wikidataID = qID
				// Store in fragment
				if fragment.Identifiers == nil {
					fragment.Identifiers = make(map[string]string)
				}
				fragment.Identifiers["wikidataId"] = qID
			}
		}
	}

	if wikidataID == "" {
		// Key skip reason: we can't enrich from Wikidata without a Q-ID.
		// Keep this log minimal; include job/source for traceability.
		fmt.Printf("[ProductHierarchyBuilder] Skip Wikidata enrichment: no wikidataId (product=%s source=%s)\n",
			strings.TrimSpace(fragment.CanonicalPath), strings.TrimSpace(sourceID))
		return nil // No Wikidata data, skip hierarchy building
	}

	// Per-call cache (do NOT store on the builder; builder can be used concurrently across scout jobs).
	cache := &productHierarchyCache{
		productData: make(map[string]*ProductWikidataData),
		fragments:   make(map[string]*models.HorizonFragment),
		orgs:        make(map[string]*models.HorizonFragment),
	}

	// Extract product data from Wikidata (with caching)
	productData, err := b.getProductDataCached(ctx, wikidataID, cache)
	if err != nil || productData == nil {
		fmt.Printf("[ProductHierarchyBuilder] Skip Wikidata enrichment: failed to fetch product data (qID=%s product=%s source=%s err=%v)\n",
			strings.TrimSpace(wikidataID), strings.TrimSpace(fragment.CanonicalPath), strings.TrimSpace(sourceID), err)
		return nil // Best effort
	}

	// Fill entityPath/sectorPath from Wikidata
	beforeWid := strings.TrimSpace(fragment.Identifiers["wikidataId"])
	beforeEntity := strings.TrimSpace(fragment.EntityPath)
	beforeSector := strings.TrimSpace(fragment.SectorPath)
	b.fillTaxonomyFromWikidata(ctx, fragment, productData)
	afterWid := strings.TrimSpace(fragment.Identifiers["wikidataId"])
	afterEntity := strings.TrimSpace(fragment.EntityPath)
	afterSector := strings.TrimSpace(fragment.SectorPath)

	// Persist any best-effort enrichment on the root fragment (wikidataId, entityPath, sectorPath).
	// This is important because this hook runs after Create().
	if b.fragmentsRepo != nil && (beforeWid != afterWid || beforeEntity != afterEntity || beforeSector != afterSector) {
		_ = b.fragmentsRepo.Update(ctx, fragment)
	}

	// Build upstream hierarchy (3 levels)
	visitedUp := map[string]struct{}{}
	if err := b.buildUpstreamHierarchy(ctx, fragmentID, fragment, productData, 3, provenanceMethod, sourceID, visitedUp, cache); err != nil {
		// Log but don't fail
		fmt.Printf("[ProductHierarchyBuilder] Failed to build upstream hierarchy: %v\n", err)
	}

	// Build downstream hierarchy (3 levels)
	visitedDown := map[string]struct{}{}
	if err := b.buildDownstreamHierarchy(ctx, fragmentID, fragment, productData, 3, provenanceMethod, sourceID, visitedDown, cache); err != nil {
		// Log but don't fail
		fmt.Printf("[ProductHierarchyBuilder] Failed to build downstream hierarchy: %v\n", err)
	}

	// Build producer relationships from Wikidata P176 (manufacturer)
	if err := b.buildProducers(ctx, fragmentID, fragment, productData, provenanceMethod, sourceID, cache); err != nil {
		// Log but don't fail
		fmt.Printf("[ProductHierarchyBuilder] Failed to build producers: %v\n", err)
	}

	return nil
}

// fillTaxonomyFromWikidata fills entityPath and sectorPath from Wikidata properties.
func (b *ProductHierarchyBuilder) fillTaxonomyFromWikidata(_ context.Context, fragment *models.HorizonFragment, data *ProductWikidataData) {
	// Map P31 (instance of) to entityPath
	// Common mappings:
	// Q11173 (chemical compound) → /Resource/RawMaterial/Chemical
	// Q11344 (mineral) → /Resource/RawMaterial/Mineral
	// Q11426 (agricultural product) → /Resource/RawMaterial/Agricultural
	// Q7397 (software) → /Resource/Component/Electronic (if applicable)

	entityPath := ""
	if len(data.InstanceOf) > 0 {
		// Map first instance-of to entityPath
		for _, instanceOf := range data.InstanceOf {
			entityPath = b.mapWikidataTypeToEntityPath(instanceOf)
			if entityPath != "" {
				break
			}
		}
	}

	// Map P452 (industry) to sectorPath
	sectorPath := ""
	if len(data.Industry) > 0 {
		// Map first industry to sectorPath
		for _, industry := range data.Industry {
			sectorPath = b.mapWikidataIndustryToSectorPath(industry)
			if sectorPath != "" {
				break
			}
		}
	}

	// Update fragment if we found mappings
	if entityPath != "" && fragment.EntityPath == "" {
		fragment.EntityPath = entityPath
	}
	if sectorPath != "" && fragment.SectorPath == "" {
		fragment.SectorPath = sectorPath
	}
}

// mapWikidataTypeToEntityPath maps Wikidata instance-of Q-ID to entityPath.
func (b *ProductHierarchyBuilder) mapWikidataTypeToEntityPath(qID string) string {
	// Common Wikidata types for products/materials
	typeMap := map[string]string{
		"Q11173": "/Resource/RawMaterial/Chemical",     // chemical compound
		"Q11344": "/Resource/RawMaterial/Mineral",      // mineral
		"Q11426": "/Resource/RawMaterial/Agricultural", // agricultural product
		"Q7397":  "/Resource/Component/Electronic",     // software (if applicable)
		"Q11461": "/Resource/Component/Electronic",     // semiconductor device
		"Q11466": "/Resource/Component/Mechanical",     // mechanical component
		"Q11467": "/Resource/Energy/Fossil",            // fossil fuel
		"Q11468": "/Resource/Energy/Electricity",       // electricity
	}
	return typeMap[qID]
}

// mapWikidataIndustryToSectorPath maps Wikidata industry Q-ID to sectorPath.
func (b *ProductHierarchyBuilder) mapWikidataIndustryToSectorPath(qID string) string {
	// Common Wikidata industries
	industryMap := map[string]string{
		"Q190117": "/Materials/Chemicals",       // chemical industry
		"Q190119": "/Materials/Mining",          // mining
		"Q190120": "/Energy/Fossil_Fuels",       // fossil fuel industry
		"Q190121": "/Technology/Semiconductors", // semiconductor industry
		"Q190122": "/Automotive/Components",     // automotive industry
	}
	return industryMap[qID]
}

// buildUpstreamHierarchy builds upstream products (materials that produce this product).
func (b *ProductHierarchyBuilder) buildUpstreamHierarchy(
	ctx context.Context,
	fragmentID string,
	fragment *models.HorizonFragment,
	productData *ProductWikidataData,
	depth int,
	provenanceMethod string,
	sourceID string,
	visited map[string]struct{},
	cache *productHierarchyCache,
) error {
	if depth <= 0 {
		return nil
	}

	// Ensure we don't loop forever on cyclical Wikidata graphs.
	if visited == nil {
		visited = map[string]struct{}{}
	}
	curQID := extractWikidataIDFromFragment(fragment)
	if curQID != "" {
		if _, ok := visited[curQID]; ok {
			return nil
		}
		visited[curQID] = struct{}{}
	}

	// Collect upstream Q-IDs from P10565 (is produced from) and P186 (material used)
	upstreamQIDs := make(map[string]bool)
	for _, qID := range productData.ProducedFrom {
		upstreamQIDs[qID] = true
	}
	for _, qID := range productData.MaterialUsed {
		upstreamQIDs[qID] = true
	}

	// For each upstream product, create fragment and edge
	for qID := range upstreamQIDs {
		upstreamFragment, err := b.ensureProductFromWikidata(ctx, qID, provenanceMethod, sourceID, cache)
		if err != nil || upstreamFragment == nil {
			continue // Best effort
		}

		// Create TRANSFORMS_INTO edge: upstream → current
		if err := b.ensureTransformsIntoEdge(ctx, upstreamFragment.ID, fragmentID, provenanceMethod, sourceID); err != nil {
			continue // Best effort
		}

		// Recursively build upstream for this product (depth-1)
		if depth > 1 {
			// Get upstream product data (with caching)
			upstreamData, err := b.getProductDataCached(ctx, qID, cache)
			if err == nil && upstreamData != nil {
				_ = b.buildUpstreamHierarchy(ctx, upstreamFragment.ID, upstreamFragment, upstreamData, depth-1, provenanceMethod, sourceID, visited, cache)
			}
		}
	}

	return nil
}

// buildDownstreamHierarchy builds downstream products (products produced from this one).
// Uses SPARQL reverse lookup: find all products where P10565 or P186 points to this Q-ID.
func (b *ProductHierarchyBuilder) buildDownstreamHierarchy(
	ctx context.Context,
	fragmentID string,
	fragment *models.HorizonFragment,
	productData *ProductWikidataData,
	depth int,
	provenanceMethod string,
	sourceID string,
	visited map[string]struct{},
	cache *productHierarchyCache,
) error {
	if depth <= 0 {
		return nil
	}

	if visited == nil {
		visited = map[string]struct{}{}
	}
	curQID := extractWikidataIDFromFragment(fragment)
	if curQID == "" {
		// If root fragment doesn't have wikidataId but we have productData, use it.
		curQID = strings.TrimSpace(productData.QID)
	}
	if curQID == "" {
		return nil
	}
	if _, ok := visited[curQID]; ok {
		return nil
	}
	visited[curQID] = struct{}{}

	downstreamQIDs, err := b.queryDownstreamProductsViaSPARQL(ctx, curQID, 25)
	if err != nil {
		return err
	}

	for _, childQID := range downstreamQIDs {
		childQID = strings.TrimSpace(childQID)
		if childQID == "" || childQID == curQID {
			continue
		}
		if _, ok := visited[childQID]; ok {
			continue
		}

		childFragment, err := b.ensureProductFromWikidata(ctx, childQID, provenanceMethod, sourceID, cache)
		if err != nil || childFragment == nil {
			continue
		}

		// Create TRANSFORMS_INTO edge: current → downstream
		if err := b.ensureTransformsIntoEdge(ctx, fragmentID, childFragment.ID, provenanceMethod, sourceID); err != nil {
			continue
		}

		if depth > 1 {
			// Get downstream product data (with caching)
			childData, err := b.getProductDataCached(ctx, childQID, cache)
			if err == nil && childData != nil {
				_ = b.buildDownstreamHierarchy(ctx, childFragment.ID, childFragment, childData, depth-1, provenanceMethod, sourceID, visited, cache)
			}
		}
	}

	return nil
}

// getProductDataCached gets product data from Wikidata with session-level caching.
func (b *ProductHierarchyBuilder) getProductDataCached(ctx context.Context, qID string, cache *productHierarchyCache) (*ProductWikidataData, error) {
	if qID == "" {
		return nil, fmt.Errorf("qID is required")
	}

	// Check session cache first
	if cache != nil {
		if cached, ok := cache.productData[qID]; ok {
			return cached, nil
		}
	}

	// Extract from Wikidata
	productData, err := b.extractor.ExtractProductData(ctx, qID)
	if err != nil || productData == nil {
		return nil, fmt.Errorf("failed to extract product data: %w", err)
	}

	// Cache for this session
	if cache != nil {
		cache.productData[qID] = productData
	}

	return productData, nil
}

// ensureProductFromWikidata ensures a product fragment exists for a Wikidata Q-ID.
// Returns existing fragment or creates new one. Uses session cache to avoid duplicate requests.
func (b *ProductHierarchyBuilder) ensureProductFromWikidata(
	ctx context.Context,
	qID string,
	_ string, // provenanceMethod - unused but kept for API consistency
	_ string, // sourceID - unused but kept for API consistency
	cache *productHierarchyCache,
) (*models.HorizonFragment, error) {
	if qID == "" {
		return nil, fmt.Errorf("qID is required")
	}

	// Check session cache first (for fragments created in this session)
	if cache != nil {
		if cached, ok := cache.fragments[qID]; ok {
			fmt.Printf("[ProductHierarchyBuilder] Reusing session-cached product from Wikidata: qID=%s id=%s canonicalPath=%s\n", qID, cached.ID, cached.CanonicalPath)
			return cached, nil
		}
	}

	// Check if product already exists in database
	existing, err := b.fragmentsRepo.FindByIdentifier(ctx, "wikidataId", qID)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			if frag.Type == models.FragmentTypeProduct {
				fmt.Printf("[ProductHierarchyBuilder] Reusing existing product by wikidataId: qID=%s id=%s canonicalPath=%s\n", qID, frag.ID, frag.CanonicalPath)
				// Cache for this session
				if cache != nil {
					cache.fragments[qID] = frag
				}
				return frag, nil
			}
		}
	}

	// Extract product data from Wikidata (with caching)
	productData, err := b.getProductDataCached(ctx, qID, cache)
	if err != nil || productData == nil {
		return nil, fmt.Errorf("failed to extract product data: %w", err)
	}

	// Canonicalize product name
	canonicalizeResult, err := b.canonicalizer.CanonicalizeProduct(ctx, productData.Label)
	if err != nil || canonicalizeResult == nil {
		return nil, fmt.Errorf("failed to canonicalize product: %w", err)
	}

	// Create fragment
	now := time.Now().UTC()
	f := &models.HorizonFragment{
		TenantID:      "global",
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     "system",
		UpdatedBy:     "system",
		CanonicalPath: canonicalizeResult.CanonicalPath,
		Type:          models.FragmentTypeProduct,
		Labels: map[string]string{
			"en": productData.Label,
		},
		Identifiers: map[string]string{
			"shortName": productData.Label,
		},
		Status: string(models.FragmentStatusActive),
		State:  map[string]models.MetricValue{},
		Governance: models.Governance{
			Owner:  "SYSTEM",
			Status: string(models.FragmentStatusActive),
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
		KBRefs: []models.KBReference{
			{
				KB:         "Wikidata",
				ExternalID: qID,
				Confidence: 1.0,
			},
		},
	}

	// Fill identifiers from Wikidata
	if productData.CASNumber != "" {
		if f.Identifiers == nil {
			f.Identifiers = make(map[string]string)
		}
		f.Identifiers["casNumber"] = productData.CASNumber
	}
	if productData.ChemicalFormula != "" {
		if f.Identifiers == nil {
			f.Identifiers = make(map[string]string)
		}
		f.Identifiers["chemicalFormula"] = productData.ChemicalFormula
	}
	if qID != "" {
		if f.Identifiers == nil {
			f.Identifiers = make(map[string]string)
		}
		f.Identifiers["wikidataId"] = qID
	}

	// Fill taxonomy from Wikidata
	b.fillTaxonomyFromWikidata(ctx, f, productData)

	// Deduplicate before creating
	deduplicated, err := b.deduplicator.DeduplicateProduct(ctx, f)
	if err == nil && deduplicated != nil && deduplicated.ID != "" && deduplicated.ID != f.ID {
		fmt.Printf("[ProductHierarchyBuilder] Deduplicated product from Wikidata: qID=%s candidateCP=%s -> existing id=%s canonicalPath=%s\n",
			qID, f.CanonicalPath, deduplicated.ID, deduplicated.CanonicalPath)
		return deduplicated, nil
	}

	// Create fragment
	id, err := b.fragmentsRepo.Create(ctx, f)
	if err != nil {
		return nil, err
	}
	f.ID = id
	fmt.Printf("[ProductHierarchyBuilder] Created product fragment from Wikidata: qID=%s id=%s canonicalPath=%s label=%q\n",
		qID, f.ID, f.CanonicalPath, productData.Label)

	// Cache for this session
	if cache != nil {
		cache.fragments[qID] = f
	}

	return f, nil
}

// ensureTransformsIntoEdge creates a TRANSFORMS_INTO edge from source to target if it doesn't exist.
func (b *ProductHierarchyBuilder) ensureTransformsIntoEdge(
	ctx context.Context,
	sourceID, targetID, provenanceMethod, sourceIDParam string,
) error {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return fmt.Errorf("invalid edge endpoints")
	}

	// Check if edge already exists
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceID, targetID, models.DependencyTypeTransformsInto)
	if err == nil {
		return nil // Already exists
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	// Create TRANSFORMS_INTO edge
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: sourceID,
		TargetFragmentID: targetID,
		Type:             models.DependencyTypeTransformsInto,
		Properties: models.EdgeProperties{
			Criticality:   0.7,
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"reason":   "product.hierarchy.wikidata",
			"inferred": true,
		},
		Provenance: models.Provenance{
			Method: strings.TrimSpace(provenanceMethod),
			SourceIDs: func() []string {
				if strings.TrimSpace(sourceIDParam) == "" {
					return nil
				}
				return []string{strings.TrimSpace(sourceIDParam)}
			}(),
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     time.Now().UTC(),
			LastUpdatedAt: time.Now().UTC(),
		},
	}

	_, err = b.edgesRepo.Create(ctx, edge)
	if err == nil {
		// Best-effort: enrich logs with canonical paths (avoid affecting pipeline on lookup failure).
		srcCP := ""
		dstCP := ""
		if b.fragmentsRepo != nil {
			if sf, ferr := b.fragmentsRepo.Get(ctx, sourceID); ferr == nil && sf != nil {
				srcCP = sf.CanonicalPath
			}
			if tf, ferr := b.fragmentsRepo.Get(ctx, targetID); ferr == nil && tf != nil {
				dstCP = tf.CanonicalPath
			}
		}
		if srcCP != "" || dstCP != "" {
			fmt.Printf("[ProductHierarchyBuilder] Created TRANSFORMS_INTO (wikidata) edge: %s -> %s (sourceID=%s targetID=%s)\n",
				srcCP, dstCP, sourceID, targetID)
		} else {
			fmt.Printf("[ProductHierarchyBuilder] Created TRANSFORMS_INTO (wikidata) edge: sourceID=%s targetID=%s\n", sourceID, targetID)
		}
	}
	return err
}

// queryDownstreamProductsViaSPARQL finds downstream products that reference the given Q-ID via
// P10565 (is produced from) or P186 (material used).
func (b *ProductHierarchyBuilder) queryDownstreamProductsViaSPARQL(ctx context.Context, qID string, limit int) ([]string, error) {
	qID = strings.TrimSpace(qID)
	if qID == "" {
		return nil, fmt.Errorf("qID is required")
	}
	if limit <= 0 {
		limit = 25
	}

	// Wikidata Query Service (WDQS)
	// Note: keep query simple & bounded.
	sparql := fmt.Sprintf(`
SELECT DISTINCT ?item WHERE {
  { ?item wdt:P10565 wd:%s . }
  UNION
  { ?item wdt:P186 wd:%s . }
}
LIMIT %d
`, qID, qID, limit)

	endpoint := "https://query.wikidata.org/sparql"
	u, _ := url.Parse(endpoint)
	u.RawQuery = url.Values{
		"format": []string{"json"},
		"query":  []string{sparql},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "Sanskrite/1.0 (https://sanskrite.com)")

	// Use a short-lived client; callers already provide ctx. Keep it simple.
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wdqs query failed: status=%d", resp.StatusCode)
	}

	var parsed struct {
		Results struct {
			Bindings []struct {
				Item struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"item"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(parsed.Results.Bindings))
	for _, b := range parsed.Results.Bindings {
		v := strings.TrimSpace(b.Item.Value)
		if v == "" {
			continue
		}
		// Expected: http://www.wikidata.org/entity/Q123
		if strings.Contains(v, "/entity/") {
			parts := strings.Split(v, "/entity/")
			if len(parts) == 2 && strings.HasPrefix(parts[1], "Q") {
				out = append(out, parts[1])
				continue
			}
		}
		// Fallback: if somehow already a Q-ID
		if strings.HasPrefix(v, "Q") {
			out = append(out, v)
		}
	}

	return out, nil
}

// =============================================================================
// PRODUCER RELATIONSHIPS (P176 - Manufacturer)
// =============================================================================

// buildProducers creates PRODUCES edges from manufacturers to the product.
// Uses Wikidata P176 (manufacturer) to find organizations that produce this product.
func (b *ProductHierarchyBuilder) buildProducers(
	ctx context.Context,
	productFragmentID string,
	productFragment *models.HorizonFragment,
	productData *ProductWikidataData,
	provenanceMethod string,
	sourceID string,
	cache *productHierarchyCache,
) error {
	if productData == nil || len(productData.Manufacturers) == 0 {
		return nil
	}

	for _, manufacturerQID := range productData.Manufacturers {
		manufacturerQID = strings.TrimSpace(manufacturerQID)
		if manufacturerQID == "" || !strings.HasPrefix(manufacturerQID, "Q") {
			continue
		}

		// Create or find the manufacturer organization
		orgFragment, err := b.ensureProducerFromWikidata(ctx, manufacturerQID, provenanceMethod, sourceID, cache)
		if err != nil || orgFragment == nil {
			fmt.Printf("[ProductHierarchyBuilder] Warning: failed to ensure producer %s: %v\n", manufacturerQID, err)
			continue
		}

		// Create PRODUCES edge: Organization -> Product
		if err := b.ensureProducesEdge(ctx, orgFragment.ID, productFragmentID, provenanceMethod, sourceID); err != nil {
			fmt.Printf("[ProductHierarchyBuilder] Warning: failed to create PRODUCES edge %s -> %s: %v\n",
				orgFragment.CanonicalPath, productFragment.CanonicalPath, err)
			continue
		}
	}

	return nil
}

// ensureProducerFromWikidata ensures an organization fragment exists for a Wikidata Q-ID (manufacturer).
// Returns existing fragment or creates new one. Uses session cache to avoid duplicate requests.
func (b *ProductHierarchyBuilder) ensureProducerFromWikidata(
	ctx context.Context,
	qID string,
	provenanceMethod string,
	sourceID string,
	cache *productHierarchyCache,
) (*models.HorizonFragment, error) {
	if qID == "" {
		return nil, fmt.Errorf("qID is required")
	}

	// Check session cache first (for orgs created in this session)
	if cache != nil {
		if cached, ok := cache.orgs[qID]; ok {
			fmt.Printf("[ProductHierarchyBuilder] Producer re-use (session cache): qID=%s path=%s\n", qID, cached.CanonicalPath)
			return cached, nil
		}
	}

	// Check if org already exists in database by wikidataId
	existing, err := b.fragmentsRepo.FindByIdentifier(ctx, "wikidataId", qID)
	if err == nil && len(existing) > 0 {
		for _, frag := range existing {
			if frag.Type == models.FragmentTypeOrganization {
				fmt.Printf("[ProductHierarchyBuilder] Producer re-use (DB): qID=%s path=%s id=%s\n", qID, frag.CanonicalPath, frag.ID)
				// Cache for this session
				if cache != nil {
					cache.orgs[qID] = frag
				}
				return frag, nil
			}
		}
	}

	// Extract organization data from Wikidata
	if b.wikidataClient == nil {
		return nil, fmt.Errorf("wikidata client not configured")
	}

	entityData, err := b.wikidataClient.GetEntityData(ctx, qID)
	if err != nil || entityData == nil {
		return nil, fmt.Errorf("failed to get entity data for %s: %w", qID, err)
	}

	label := strings.TrimSpace(entityData.Label)
	if label == "" {
		return nil, fmt.Errorf("entity %s has no label", qID)
	}

	// Build canonical path for organization
	slug := Slugify(label)
	canonicalPath := "/org/" + slug

	// Create fragment
	now := time.Now().UTC()
	f := &models.HorizonFragment{
		TenantID:      "global",
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
		CreatedBy:     "system",
		UpdatedBy:     "system",
		CanonicalPath: canonicalPath,
		Type:          models.FragmentTypeOrganization,
		Labels: map[string]string{
			"en": label,
		},
		Identifiers: map[string]string{
			"shortName":  label,
			"wikidataId": qID,
		},
		Description: strings.TrimSpace(entityData.Description),
		Status:      string(models.FragmentStatusActive),
		State:       map[string]models.MetricValue{},
		Governance: models.Governance{
			Owner:  "SYSTEM",
			Status: string(models.FragmentStatusActive),
		},
		Metadata: models.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
		KBRefs: []models.KBReference{
			{
				KB:         "Wikidata",
				ExternalID: qID,
				Confidence: 1.0,
			},
		},
		Provenance: models.Provenance{
			Method: strings.TrimSpace(provenanceMethod),
			SourceIDs: func() []string {
				if strings.TrimSpace(sourceID) == "" {
					return nil
				}
				return []string{strings.TrimSpace(sourceID)}
			}(),
		},
	}

	// Deduplicate before creating
	if b.orgDeduplicator != nil {
		deduplicated, err := b.orgDeduplicator.DeduplicateOrg(ctx, f)
		if err == nil && deduplicated != nil && deduplicated.ID != "" && deduplicated.ID != f.ID {
			fmt.Printf("[ProductHierarchyBuilder] Producer dedup hit: qID=%s -> existing id=%s path=%s\n",
				qID, deduplicated.ID, deduplicated.CanonicalPath)
			// Cache for this session
			if cache != nil {
				cache.orgs[qID] = deduplicated
			}
			return deduplicated, nil
		}
	}

	// Create fragment
	id, err := b.fragmentsRepo.Create(ctx, f)
	if err != nil {
		return nil, err
	}
	f.ID = id

	fmt.Printf("[ProductHierarchyBuilder] Created producer from Wikidata: qID=%s id=%s path=%s label=%s\n",
		qID, id, f.CanonicalPath, label)

	// Cache for this session
	if cache != nil {
		cache.orgs[qID] = f
	}

	return f, nil
}

// ensureProducesEdge creates a PRODUCES edge from organization to product if it doesn't exist.
func (b *ProductHierarchyBuilder) ensureProducesEdge(
	ctx context.Context,
	orgID, productID, provenanceMethod, sourceIDParam string,
) error {
	if orgID == "" || productID == "" || orgID == productID {
		return fmt.Errorf("invalid edge endpoints")
	}

	// Check if edge already exists
	_, err := b.edgesRepo.GetBySourceTargetAndType(ctx, "global", orgID, productID, models.DependencyTypeProduces)
	if err == nil {
		return nil // Already exists
	}
	if !errors.Is(err, repo.ErrEdgeNotFound) {
		return err
	}

	// Create PRODUCES edge: Organization -> Product
	edge := &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: orgID,
		TargetFragmentID: productID,
		Type:             models.DependencyTypeProduces,
		Properties: models.EdgeProperties{
			Criticality:   0.7,
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: map[string]interface{}{
			"reason":   "product.hierarchy.wikidata.manufacturer",
			"inferred": true,
		},
		Provenance: models.Provenance{
			Method: strings.TrimSpace(provenanceMethod),
			SourceIDs: func() []string {
				if strings.TrimSpace(sourceIDParam) == "" {
					return nil
				}
				return []string{strings.TrimSpace(sourceIDParam)}
			}(),
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     time.Now().UTC(),
			LastUpdatedAt: time.Now().UTC(),
		},
	}

	id, err := b.edgesRepo.Create(ctx, edge)
	if err != nil {
		return err
	}

	// Log edge creation
	// Best-effort: resolve canonical paths for logging
	var orgPath, productPath string
	if org, err := b.fragmentsRepo.Get(ctx, orgID); err == nil && org != nil {
		orgPath = org.CanonicalPath
	}
	if prod, err := b.fragmentsRepo.Get(ctx, productID); err == nil && prod != nil {
		productPath = prod.CanonicalPath
	}
	if orgPath != "" && productPath != "" {
		fmt.Printf("[ProductHierarchyBuilder] Created PRODUCES (wikidata.manufacturer): %s -> %s (edge=%s)\n",
			orgPath, productPath, id)
	} else {
		fmt.Printf("[ProductHierarchyBuilder] Created PRODUCES (wikidata.manufacturer): %s -> %s (edge=%s)\n",
			orgID, productID, id)
	}

	return nil
}
