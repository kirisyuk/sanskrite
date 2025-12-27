package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// Smart deduplication tool that:
// 1. Finds duplicates by obvious signals (Wikidata ID, shortName, legalName)
// 2. Groups them into clusters
// 3. Selects canonical fragment in each cluster (prefers /loc/ for GEO_REGION)
// 4. Fixes canonical paths (adds /loc/ prefix for GEO_REGION if missing)
// 5. Merges data from duplicates into canonical
// 6. Reassigns edges to canonical
// 7. Deletes duplicate fragments
// 8. Deduplicates resulting edges

var (
	projectID       = flag.String("project", "", "GCP project ID (required)")
	apply           = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun          = flag.Bool("dry-run", false, "Force dry-run even if --apply is set")
	tenantID        = flag.String("tenant", "global", "Tenant ID (default: global)")
	verbose         = flag.Bool("verbose", false, "Verbose logging")
	typeFilter      = flag.String("type", "", "Filter by fragment type (GEO_REGION, ORGANIZATION, FACILITY, etc.)")
	skipVertexSync  = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync (faster, run bulk sync separately)")
)

// DuplicateCluster represents a group of fragments that are duplicates of each other
type DuplicateCluster struct {
	MatchType   string                    // "wikidata", "shortName", "legalName", "labelEN", "normalizedLegal", "geoCity"
	MatchValue  string                    // The value that matched
	Fragments   []*models.HorizonFragment // All fragments in this cluster
	CanonicalID string                    // ID of the fragment to keep
}

type Stats struct {
	TotalFragments     int
	ClustersFound      int
	FragmentsMerged    int
	FragmentsDeleted   int
	PathsFixed         int
	EdgesReassigned    int
	EdgesDeleted       int
	EdgesDeduplicated  int
	EdgesDirectionFixed int
}

func main() {
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required")
	}
	if *dryRun {
		*apply = false
	}

	ctx := context.Background()

	log.Printf("=== Horizon Smart Deduplication Tool ===")
	log.Printf("project=%s tenant=%s apply=%v typeFilter=%q", *projectID, *tenantID, *apply, *typeFilter)

	// Initialize Firestore
	fsClient, err := gcp.NewFirestoreClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("FATAL: failed to init GCS client: %v", err)
	}

	bucketName := getEnvDefault("HORIZON_FRAGMENTS_BUCKET", fmt.Sprintf("%s-horizon-fragments", *projectID))
	log.Printf("GCS bucket: %s", bucketName)

	fragmentsRepo := repo.NewHorizonFragmentsRepository(db).
		WithStorage(gcsClient, bucketName)

	// Optionally configure Vertex AI Search sync
	if !*skipVertexSync {
		vertexStoreID := getEnvDefault("VERTEX_SEARCH_DATA_STORE_ID", "rag-documents-store")
		vertexLocation := getEnvDefault("VERTEX_SEARCH_LOCATION", "global")
		vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexLocation, vertexStoreID, "")
		if vertexSearchClient.IsConfigured() {
			log.Printf("Vertex AI Search configured: store=%s location=%s", vertexStoreID, vertexLocation)
			fragmentsRepo = fragmentsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
				return vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
					GCSPath:            gcsURI,
					ReconciliationMode: "INCREMENTAL",
					DataSchema:         "document",
				})
			})
		} else {
			log.Printf("WARNING: Vertex AI Search not fully configured, skipping sync")
		}
	} else {
		log.Printf("Vertex AI Search sync DISABLED (--skip-vertex-sync). Run bulk sync separately after completion.")
	}
	edgesRepo := repo.NewDependencyEdgesRepository(db)

	stats := &Stats{}

	// Step 1: Load all fragments
	log.Printf("\n=== Step 1: Loading fragments ===")
	fragments, err := loadAllFragments(ctx, db, *tenantID, *typeFilter)
	if err != nil {
		log.Fatalf("FATAL: failed to load fragments: %v", err)
	}
	stats.TotalFragments = len(fragments)
	log.Printf("✓ Loaded %d fragments", len(fragments))

	// Step 2: Build indexes for duplicate detection
	log.Printf("\n=== Step 2: Building indexes ===")
	wikidataIndex := make(map[string][]*models.HorizonFragment)        // wikidataID -> fragments
	shortNameIndex := make(map[string][]*models.HorizonFragment)       // type:shortName -> fragments
	legalNameIndex := make(map[string][]*models.HorizonFragment)       // type:legalName -> fragments
	labelENIndex := make(map[string][]*models.HorizonFragment)         // type:label_en -> fragments (Tier 2)
	normalizedLegalIndex := make(map[string][]*models.HorizonFragment) // type:normalized_legal -> fragments
	geoCityIndex := make(map[string][]*models.HorizonFragment)         // city_name -> geo fragments

	for _, f := range fragments {
		// Index by Wikidata ID
		if wikidataID := extractWikidataID(f); wikidataID != "" {
			wikidataIndex[wikidataID] = append(wikidataIndex[wikidataID], f)
		}

		// Index by shortName (type-scoped)
		if shortName := strings.TrimSpace(f.Identifiers["shortName"]); shortName != "" {
			key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(shortName))
			shortNameIndex[key] = append(shortNameIndex[key], f)
		}

		// Index by legalName (type-scoped) — Tier 1 (100% confidence for orgs)
		if legalName := strings.TrimSpace(f.Identifiers["legalName"]); legalName != "" {
			key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(legalName))
			legalNameIndex[key] = append(legalNameIndex[key], f)
		}

		// Index by label[en] (type-scoped) — Tier 2 (high confidence)
		if labelEN := strings.TrimSpace(f.Labels["en"]); labelEN != "" {
			key := fmt.Sprintf("%s:%s", f.Type, strings.ToLower(labelEN))
			labelENIndex[key] = append(labelENIndex[key], f)
		}

		// Index by normalized legalName (removes JSC, LLC, Inc, etc.)
		if legalName := strings.TrimSpace(f.Identifiers["legalName"]); legalName != "" {
			normalized := normalizeLegalName(legalName)
			if normalized != "" {
				key := fmt.Sprintf("%s:%s", f.Type, normalized)
				normalizedLegalIndex[key] = append(normalizedLegalIndex[key], f)
			}
		}

		// Index GEO_REGION by city name (last part of path)
		if f.Type == models.FragmentTypeGeoRegion {
			cityName := extractCityFromPath(f.CanonicalPath)
			if cityName != "" {
				geoCityIndex[cityName] = append(geoCityIndex[cityName], f)
			}
		}
	}

	log.Printf("✓ Wikidata index: %d unique IDs", len(wikidataIndex))
	log.Printf("✓ ShortName index: %d unique keys", len(shortNameIndex))
	log.Printf("✓ LegalName index: %d unique keys", len(legalNameIndex))
	log.Printf("✓ Label[en] index: %d unique keys", len(labelENIndex))
	log.Printf("✓ Normalized legal index: %d unique keys", len(normalizedLegalIndex))
	log.Printf("✓ GEO city index: %d unique cities", len(geoCityIndex))

	// Build fragment type cache for edge direction fixing (Step 6)
	fragmentTypes := make(map[string]models.FragmentType)
	for _, f := range fragments {
		fragmentTypes[f.ID] = f.Type
	}
	log.Printf("✓ Fragment type cache: %d entries", len(fragmentTypes))

	// Step 3: Build duplicate clusters
	log.Printf("\n=== Step 3: Finding duplicate clusters ===")
	clusters := make([]*DuplicateCluster, 0)
	processedFragmentIDs := make(map[string]bool)

	// Priority 1: Wikidata ID matches (highest confidence)
	for wikidataID, frags := range wikidataIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "wikidata",
					MatchValue: wikidataID,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
			}
		}
	}
	log.Printf("  Found %d Wikidata-based clusters", len(clusters))

	// Priority 2: ShortName matches (same type)
	shortNameClusters := 0
	for key, frags := range shortNameIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "shortName",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				shortNameClusters++
			}
		}
	}
	log.Printf("  Found %d shortName-based clusters", shortNameClusters)

	// Priority 3: LegalName matches (same type)
	legalNameClusters := 0
	for key, frags := range legalNameIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "legalName",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				legalNameClusters++
			}
		}
	}
	log.Printf("  Found %d legalName-based clusters", legalNameClusters)

	// Priority 4: Label[en] matches (same type) — catches duplicates with same display name
	labelENClusters := 0
	for key, frags := range labelENIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "labelEN",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				labelENClusters++
			}
		}
	}
	log.Printf("  Found %d label[en]-based clusters", labelENClusters)

	// Priority 5: Normalized legalName matches (removes JSC, LLC, Inc, etc.)
	normalizedLegalClusters := 0
	for key, frags := range normalizedLegalIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "normalizedLegal",
					MatchValue: key,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				normalizedLegalClusters++
			}
		}
	}
	log.Printf("  Found %d normalizedLegal-based clusters", normalizedLegalClusters)

	// Priority 6: GEO city name matches
	geoCityClusters := 0
	for cityName, frags := range geoCityIndex {
		if len(frags) > 1 {
			var unprocessed []*models.HorizonFragment
			for _, f := range frags {
				if !processedFragmentIDs[f.ID] {
					unprocessed = append(unprocessed, f)
				}
			}
			if len(unprocessed) > 1 {
				cluster := &DuplicateCluster{
					MatchType:  "geoCity",
					MatchValue: cityName,
					Fragments:  unprocessed,
				}
				cluster.CanonicalID = selectCanonical(unprocessed)
				clusters = append(clusters, cluster)
				for _, f := range unprocessed {
					processedFragmentIDs[f.ID] = true
				}
				geoCityClusters++
			}
		}
	}
	log.Printf("  Found %d geoCity-based clusters", geoCityClusters)

	stats.ClustersFound = len(clusters)
	log.Printf("✓ Total duplicate clusters: %d", len(clusters))

	if len(clusters) == 0 {
		log.Printf("\nNo duplicates found!")
		return
	}

	// Step 4: Process each cluster
	log.Printf("\n=== Step 4: Processing clusters ===")
	for i, cluster := range clusters {
		if i%100 == 0 {
			log.Printf("  Progress: %d/%d clusters processed...", i, len(clusters))
		}

		if *verbose {
			log.Printf("\n---- Cluster %d/%d [%s: %s] ----", i+1, len(clusters), cluster.MatchType, cluster.MatchValue)
		}

		// Find canonical fragment
		var canonical *models.HorizonFragment
		var duplicates []*models.HorizonFragment
		for _, f := range cluster.Fragments {
			if f.ID == cluster.CanonicalID {
				canonical = f
			} else {
				duplicates = append(duplicates, f)
			}
		}

		if canonical == nil {
			log.Printf("  ERROR: canonical fragment not found for cluster %s", cluster.MatchValue)
			continue
		}

		if *verbose {
			log.Printf("  Canonical: %s (%s)", canonical.CanonicalPath, canonical.ID)
			for _, dup := range duplicates {
				log.Printf("  Duplicate: %s (%s)", dup.CanonicalPath, dup.ID)
			}
		}

		// Fix canonical path for GEO_REGION if needed
		pathFixed := false
		if canonical.Type == models.FragmentTypeGeoRegion {
			if !strings.HasPrefix(canonical.CanonicalPath, "/loc/") {
				oldPath := canonical.CanonicalPath
				canonical.CanonicalPath = fixGeoPath(canonical.CanonicalPath)
				pathFixed = true
				stats.PathsFixed++
				if *verbose {
					log.Printf("  Path fixed: %s -> %s", oldPath, canonical.CanonicalPath)
				}
			}
		}

		if !*apply {
			if *verbose {
				log.Printf("  [DRY-RUN] Would merge %d duplicates into canonical", len(duplicates))
			}
			stats.FragmentsMerged += len(duplicates)
			stats.FragmentsDeleted += len(duplicates)
			continue
		}

		// 4a. Merge data from duplicates into canonical
		changed := pathFixed
		for _, dup := range duplicates {
			if mergeFragmentData(canonical, dup) {
				changed = true
			}
		}
		if changed {
			if err := fragmentsRepo.Update(ctx, canonical); err != nil {
				log.Printf("  ERROR: failed to update canonical: %v", err)
				continue
			}
			if *verbose {
				log.Printf("  ✓ Merged data into canonical")
			}
		}
		stats.FragmentsMerged += len(duplicates)

		// 4b. Reassign edges from duplicates to canonical
		for _, dup := range duplicates {
			reassigned, deleted, err := reassignEdges(ctx, db, edgesRepo, dup.ID, canonical.ID)
			if err != nil {
				log.Printf("  ERROR: failed to reassign edges from %s: %v", dup.ID, err)
				continue
			}
			stats.EdgesReassigned += reassigned
			stats.EdgesDeleted += deleted
			if *verbose && (reassigned > 0 || deleted > 0) {
				log.Printf("  ✓ Edges from %s: %d reassigned, %d deleted", dup.ID, reassigned, deleted)
			}
		}

		// 4c. Delete duplicate fragments
		for _, dup := range duplicates {
			if err := fragmentsRepo.Delete(ctx, dup.ID); err != nil {
				log.Printf("  ERROR: failed to delete duplicate %s: %v", dup.ID, err)
				continue
			}
			stats.FragmentsDeleted++
		}
		if *verbose {
			log.Printf("  ✓ Deleted %d duplicate fragments", len(duplicates))
		}
	}

	// Step 5: Deduplicate edges globally
	log.Printf("\n=== Step 5: Deduplicating edges ===")
	if *apply {
		deduped, err := deduplicateEdges(ctx, db, edgesRepo, *tenantID)
		if err != nil {
			log.Printf("ERROR: edge deduplication failed: %v", err)
		} else {
			stats.EdgesDeduplicated = deduped
			log.Printf("✓ Deduplicated %d edges", deduped)
		}
	} else {
		log.Printf("[DRY-RUN] Would deduplicate edges after merge")
	}

	// Step 6: Fix edge directions (reverse edges that point in wrong direction)
	log.Printf("\n=== Step 6: Fixing edge directions ===")
	if *apply {
		fixed, err := fixEdgeDirections(ctx, db, edgesRepo, fragmentTypes, *tenantID)
		if err != nil {
			log.Printf("ERROR: edge direction fix failed: %v", err)
		} else {
			stats.EdgesDirectionFixed = fixed
			log.Printf("✓ Fixed direction of %d edges", fixed)
		}
	} else {
		log.Printf("[DRY-RUN] Would fix edge directions")
	}

	// Print summary
	log.Printf("\n=== Summary ===")
	log.Printf("Total fragments scanned: %d", stats.TotalFragments)
	log.Printf("Duplicate clusters found: %d", stats.ClustersFound)
	log.Printf("Fragments merged: %d", stats.FragmentsMerged)
	log.Printf("Fragments deleted: %d", stats.FragmentsDeleted)
	log.Printf("GEO paths fixed: %d", stats.PathsFixed)
	log.Printf("Edges reassigned: %d", stats.EdgesReassigned)
	log.Printf("Edges deleted (duplicates/self-loops): %d", stats.EdgesDeleted)
	log.Printf("Edges deduplicated: %d", stats.EdgesDeduplicated)
	log.Printf("Edges direction fixed: %d", stats.EdgesDirectionFixed)
	if !*apply {
		log.Printf("\n[DRY-RUN] No changes made. Use --apply to execute.")
	}
}

func loadAllFragments(ctx context.Context, db *firestore.Client, tenantID, typeFilter string) ([]*models.HorizonFragment, error) {
	q := db.Collection("horizon_fragments").Where("tenantId", "==", tenantID)

	if typeFilter != "" {
		q = q.Where("type", "==", typeFilter)
	}

	iter := q.Documents(ctx)
	defer iter.Stop()

	var fragments []*models.HorizonFragment
	count := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate fragments: %w", err)
		}

		var f models.HorizonFragment
		if err := doc.DataTo(&f); err != nil {
			log.Printf("WARNING: failed to parse fragment %s: %v", doc.Ref.ID, err)
			continue
		}
		if strings.TrimSpace(f.ID) == "" {
			f.ID = doc.Ref.ID
		}
		fragments = append(fragments, &f)
		count++

		if count%1000 == 0 {
			log.Printf("  ... loaded %d fragments", count)
		}
	}

	return fragments, nil
}

// extractWikidataID extracts Wikidata ID from fragment's KBRefs or Identifiers
func extractWikidataID(f *models.HorizonFragment) string {
	// Check KBRefs
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

// selectCanonical chooses the best fragment to be the canonical one
// For GEO_REGION: strongly prefers fragments with /loc/ prefix
func selectCanonical(fragments []*models.HorizonFragment) string {
	if len(fragments) == 0 {
		return ""
	}
	if len(fragments) == 1 {
		return fragments[0].ID
	}

	// For GEO_REGION: first check if any have /loc/ prefix
	fragType := fragments[0].Type
	if fragType == models.FragmentTypeGeoRegion {
		for _, f := range fragments {
			if strings.HasPrefix(f.CanonicalPath, "/loc/") {
				// Found one with /loc/ - use scoring among /loc/ fragments only
				var locFragments []*models.HorizonFragment
				for _, ff := range fragments {
					if strings.HasPrefix(ff.CanonicalPath, "/loc/") {
						locFragments = append(locFragments, ff)
					}
				}
				return selectCanonicalByScore(locFragments)
			}
		}
	}

	// Default: score-based selection
	return selectCanonicalByScore(fragments)
}

// selectCanonicalByScore chooses canonical based on data quality score
func selectCanonicalByScore(fragments []*models.HorizonFragment) string {
	if len(fragments) == 0 {
		return ""
	}
	if len(fragments) == 1 {
		return fragments[0].ID
	}

	type scored struct {
		fragment *models.HorizonFragment
		score    int
	}
	var scoredFragments []scored

	for _, f := range fragments {
		score := 0

		// Prefer fragments with Wikidata ID (+100)
		if extractWikidataID(f) != "" {
			score += 100
		}

		// Prefer proper /loc/{country}/{admin1}/{city} format (+50)
		if strings.HasPrefix(f.CanonicalPath, "/loc/") {
			parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
			if len(parts) >= 4 {
				score += 50 // Full hierarchy
			} else if len(parts) >= 3 {
				score += 30 // Country + something
			} else {
				score += 10 // Just /loc/
			}
		}

		// Prefer verified status (+30)
		if f.Metadata.EpistemicStatus == models.EpistemicStatusVerified {
			score += 30
		}

		// Prefer more complete identifiers (+5 per identifier)
		score += len(f.Identifiers) * 5

		// Prefer more KBRefs (+10 per ref)
		score += len(f.KBRefs) * 10

		// Prefer older fragments (more established) (+10)
		if !f.Metadata.CreatedAt.IsZero() {
			age := time.Since(f.Metadata.CreatedAt).Hours() / 24
			if age > 7 {
				score += 10
			}
		}

		scoredFragments = append(scoredFragments, scored{f, score})
	}

	// Sort by score descending
	sort.Slice(scoredFragments, func(i, j int) bool {
		if scoredFragments[i].score != scoredFragments[j].score {
			return scoredFragments[i].score > scoredFragments[j].score
		}
		// Tie-breaker: older CreatedAt
		return scoredFragments[i].fragment.Metadata.CreatedAt.Before(scoredFragments[j].fragment.Metadata.CreatedAt)
	})

	if *verbose {
		log.Printf("    Canonical selection scores:")
		for _, sf := range scoredFragments {
			log.Printf("      %s (%s): score=%d", sf.fragment.CanonicalPath, sf.fragment.ID, sf.score)
		}
	}

	return scoredFragments[0].fragment.ID
}

// fixGeoPath adds /loc/ prefix to geo paths that don't have it
func fixGeoPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/loc/unknown"
	}

	// Already has /loc/ prefix
	if strings.HasPrefix(path, "/loc/") {
		return path
	}

	// Remove /region/ prefix if present and add /loc/
	if strings.HasPrefix(path, "/region/") {
		return "/loc/" + strings.TrimPrefix(path, "/region/")
	}

	// Just add /loc/ prefix
	if strings.HasPrefix(path, "/") {
		return "/loc" + path
	}
	return "/loc/" + path
}

// mergeFragmentData merges data from source into target (fill-only, doesn't overwrite)
func mergeFragmentData(target, source *models.HorizonFragment) bool {
	changed := false

	// Merge Labels (fill missing)
	if target.Labels == nil {
		target.Labels = make(map[string]string)
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Labels[k]) == "" {
			target.Labels[k] = v
			changed = true
		}
	}

	// Merge Identifiers (fill missing)
	if target.Identifiers == nil {
		target.Identifiers = make(map[string]string)
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Identifiers[k]) == "" {
			target.Identifiers[k] = v
			changed = true
		}
	}

	// Merge KBRefs (add missing)
	existingRefs := make(map[string]bool)
	for _, ref := range target.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		existingRefs[key] = true
	}
	for _, ref := range source.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		if !existingRefs[key] {
			target.KBRefs = append(target.KBRefs, ref)
			changed = true
		}
	}

	// Merge Description (fill if empty)
	if strings.TrimSpace(target.Description) == "" && strings.TrimSpace(source.Description) != "" {
		target.Description = source.Description
		changed = true
	}

	// Merge Tags (union)
	tagSet := make(map[string]bool)
	for _, t := range target.Tags {
		tagSet[t] = true
	}
	for _, t := range source.Tags {
		if !tagSet[t] {
			target.Tags = append(target.Tags, t)
			tagSet[t] = true
			changed = true
		}
	}

	// Track merged canonical paths
	if target.Identifiers["merged_paths"] == "" {
		target.Identifiers["merged_paths"] = source.CanonicalPath
	} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
		target.Identifiers["merged_paths"] += "," + source.CanonicalPath
	}
	changed = true

	return changed
}

// reassignEdges moves edges from oldFragmentID to newFragmentID
func reassignEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, oldID, newID string) (reassigned, deleted int, err error) {
	edgesCol := db.Collection("horizon_dependency_edges")
	tenantID := "global"

	// Process source edges
	q1 := edgesCol.Where("tenantId", "==", tenantID).Where("sourceFragmentId", "==", oldID)
	docs1, err := q1.Documents(ctx).GetAll()
	if err != nil {
		return 0, 0, err
	}
	for _, d := range docs1 {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}

		// Check if this would become a self-loop
		if edge.TargetFragmentID == newID {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Check if edge already exists
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, newID, edge.TargetFragmentID, edge.Type)
		if err == nil && existing != nil {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Reassign
		edge.SourceFragmentID = newID
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}

	// Process target edges
	q2 := edgesCol.Where("tenantId", "==", tenantID).Where("targetFragmentId", "==", oldID)
	docs2, err := q2.Documents(ctx).GetAll()
	if err != nil {
		return reassigned, deleted, err
	}
	for _, d := range docs2 {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}

		// Check if this would become a self-loop
		if edge.SourceFragmentID == newID {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Check if edge already exists
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, edge.SourceFragmentID, newID, edge.Type)
		if err == nil && existing != nil {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}

		// Reassign
		edge.TargetFragmentID = newID
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}

	return reassigned, deleted, nil
}

// deduplicateEdges removes duplicate edges (same source+target+type)
func deduplicateEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, tenantID string) (int, error) {
	q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID)
	iter := q.Documents(ctx)
	defer iter.Stop()

	// Group edges by (source, target, type)
	groups := make(map[string][]*models.DependencyEdge)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, err
		}

		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = doc.Ref.ID
		}

		key := fmt.Sprintf("%s|%s|%s", edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
		groups[key] = append(groups[key], &edge)
	}

	// Delete duplicates (keep first by CreatedAt)
	deleted := 0
	for _, group := range groups {
		if len(group) <= 1 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			return group[i].Metadata.CreatedAt.Before(group[j].Metadata.CreatedAt)
		})
		for i := 1; i < len(group); i++ {
			if err := edgesRepo.Delete(ctx, group[i].ID); err == nil {
				deleted++
			}
		}
	}

	return deleted, nil
}

func getEnvDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// normalizeLegalName removes common suffixes like JSC, LLC, Inc, etc.
func normalizeLegalName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	
	// Common suffixes to remove
	suffixes := []string{
		" jsc", " llc", " inc", " inc.", " ltd", " ltd.", " limited",
		" corp", " corp.", " corporation", " co", " co.", " company",
		" plc", " ag", " gmbh", " sa", " sas", " bv", " nv", " ab",
		" oy", " as", " a/s", " kft", " zrt", " sp. z o.o.", " sp.zo.o",
		" pty", " pty.", " pty ltd", " pte", " pte.", " pte ltd",
		" holding", " holdings", " group", " international",
	}
	
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	
	// Remove punctuation and extra spaces
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, "'", "")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	
	return strings.TrimSpace(name)
}

// extractCityFromPath extracts the city name from a canonical path
// e.g., "/loc/kz/astana/astana" -> "astana"
// e.g., "/loc/us/tx/austin" -> "austin"
// e.g., "/region/astana" -> "astana"
func extractCityFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	
	// Get last non-"na" part
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.ToLower(parts[i])
		if part != "na" && part != "" && part != "loc" && part != "region" {
			return part
		}
	}
	
	return ""
}

// EdgeDirectionRule defines valid source and target types for an edge type
type EdgeDirectionRule struct {
	ValidSourceTypes []models.FragmentType
	ValidTargetTypes []models.FragmentType
}

// edgeDirectionRules defines which fragment types are allowed as source/target for each edge type
var edgeDirectionRules = map[models.DependencyType]EdgeDirectionRule{
	// REGULATES: Only Organization (regulator) can regulate others
	// Facility cannot be a source
	models.DependencyTypeRegulates: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
			models.FragmentTypeGeoRegion,
		},
	},
	// OWNS: Organization or Person owns something
	// Facility and Product cannot own
	models.DependencyTypeOwns: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization, models.FragmentTypePerson},
		ValidTargetTypes: []models.FragmentType{
			models.FragmentTypeOrganization,
			models.FragmentTypeFacility,
			models.FragmentTypeProduct,
		},
	},
	// PRODUCES: Organization or Facility produces Product
	// Product, GeoRegion, Person cannot produce
	models.DependencyTypeProduces: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization, models.FragmentTypeFacility},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeProduct},
	},
	// LOCATED_IN: Entity is located in GeoRegion
	// GeoRegion can be located in parent GeoRegion
	models.DependencyTypeLocatedIn: {
		ValidSourceTypes: []models.FragmentType{
			models.FragmentTypeFacility,
			models.FragmentTypeOrganization,
			models.FragmentTypePerson,
			models.FragmentTypeGeoRegion,
		},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion},
	},
	// OPERATES_IN: Organization operates in GeoRegion or Facility
	models.DependencyTypeOperatesIn: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeGeoRegion, models.FragmentTypeFacility},
	},
	// SUPPLIES_TO: Organization/Facility supplies to Organization/Facility
	// Product and GeoRegion cannot supply
	models.DependencyTypeSuppliesTo: {
		ValidSourceTypes: []models.FragmentType{models.FragmentTypeOrganization, models.FragmentTypeFacility},
		ValidTargetTypes: []models.FragmentType{models.FragmentTypeOrganization, models.FragmentTypeFacility},
	},
}

// shouldReverseEdge checks if an edge has wrong direction and should be reversed
func shouldReverseEdge(edgeType models.DependencyType, sourceType, targetType models.FragmentType) bool {
	rule, exists := edgeDirectionRules[edgeType]
	if !exists {
		return false // No rule, don't reverse
	}

	sourceValid := false
	for _, t := range rule.ValidSourceTypes {
		if t == sourceType {
			sourceValid = true
			break
		}
	}

	targetValid := false
	for _, t := range rule.ValidTargetTypes {
		if t == targetType {
			targetValid = true
			break
		}
	}

	// If current direction is wrong, check if reversed would be valid
	if !sourceValid || !targetValid {
		// Check if reversed direction is valid
		reversedSourceValid := false
		for _, t := range rule.ValidSourceTypes {
			if t == targetType {
				reversedSourceValid = true
				break
			}
		}

		reversedTargetValid := false
		for _, t := range rule.ValidTargetTypes {
			if t == sourceType {
				reversedTargetValid = true
				break
			}
		}

		// Only reverse if the reversed direction is valid
		if reversedSourceValid && reversedTargetValid {
			return true
		}
	}

	return false
}

// fixEdgeDirections reverses edges that point in the wrong direction
// Uses pagination (100 edges per page) and pre-built fragment type cache for efficiency
func fixEdgeDirections(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, fragmentTypes map[string]models.FragmentType, tenantID string) (int, error) {
	log.Printf("Fixing edge directions with pagination (page size: 100)...")
	log.Printf("Using pre-loaded fragment type cache with %d entries", len(fragmentTypes))

	const pageSize = 100
	fixed := 0
	totalChecked := 0
	var lastDoc *firestore.DocumentSnapshot

	for {
		// Build paginated query
		q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID).Limit(pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}

		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			return fixed, fmt.Errorf("failed to get edges page: %w", err)
		}

		if len(docs) == 0 {
			break // No more pages
		}

		// Process this page
		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			if strings.TrimSpace(edge.ID) == "" {
				edge.ID = doc.Ref.ID
			}

			sourceType := fragmentTypes[edge.SourceFragmentID]
			targetType := fragmentTypes[edge.TargetFragmentID]

			if sourceType == "" || targetType == "" {
				continue // Skip if we can't determine types (orphaned edge or deleted fragment)
			}

			if shouldReverseEdge(edge.Type, sourceType, targetType) {
				// Reverse the edge
				oldSource := edge.SourceFragmentID
				edge.SourceFragmentID = edge.TargetFragmentID
				edge.TargetFragmentID = oldSource

				if err := edgesRepo.Update(ctx, &edge); err != nil {
					log.Printf("  ERROR: failed to reverse edge %s: %v", edge.ID, err)
					continue
				}

				if *verbose {
					log.Printf("  ✓ Reversed edge %s: %s (%s) -> %s (%s) [%s]",
						edge.ID, edge.TargetFragmentID, targetType, edge.SourceFragmentID, sourceType, edge.Type)
				}
				fixed++
			}
		}

		totalChecked += len(docs)
		if totalChecked%1000 == 0 || len(docs) < pageSize {
			log.Printf("  Progress: checked %d edges, fixed %d so far...", totalChecked, fixed)
		}

		lastDoc = docs[len(docs)-1]

		if len(docs) < pageSize {
			break // Last page
		}
	}

	log.Printf("  Completed: checked %d edges total", totalChecked)
	return fixed, nil
}

