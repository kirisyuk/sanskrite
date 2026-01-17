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

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

// horizon_deduplicate_persons finds and merges duplicate PERSON fragments.
//
// Deduplication strategies (same as PersonDeduplicator):
// 1. Wikidata ID match
// 2. Exact legalName/shortName/label[en] match
// 3. Phonetic name match
// 4. Token-subset match (new: catches missing middle names)

var (
	projectID      = flag.String("project", "", "GCP project ID (required)")
	apply          = flag.Bool("apply", false, "Apply changes (default: dry-run)")
	dryRun         = flag.Bool("dry-run", false, "Force dry-run even if --apply is set")
	tenantID       = flag.String("tenant", "global", "Tenant ID (default: global)")
	verbose        = flag.Bool("verbose", false, "Verbose logging")
	limit          = flag.Int("limit", 0, "Limit number of clusters to process (0 = all)")
	skipVertexSync = flag.Bool("skip-vertex-sync", false, "Skip Vertex AI Search sync (faster, run bulk sync separately)")
	ragBucket      = flag.String("rag-bucket", "", "GCS bucket for fragments (default: {project}-rag-documents)")
	pageSize       = flag.Int("page-size", 500, "Firestore page size for loading PERSON fragments (pagination)")
	startAfter     = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID (__name__)")
	maxFragments   = flag.Int("max-fragments", 0, "Stop after loading this many PERSON fragments (0 = no limit)")
)

// DuplicateCluster represents a group of person fragments that are duplicates
type DuplicateCluster struct {
	MatchType   string                    // "wikidataId", "legalName", "shortName", "labelEN", "phonetic", "tokenSubset"
	MatchValue  string                    // The value that matched
	Fragments   []*models.HorizonFragment // All fragments in this cluster
	CanonicalID string                    // ID of the fragment to keep
}

type Stats struct {
	TotalPersons     int
	ClustersFound    int
	FragmentsMerged  int
	FragmentsDeleted int
	EdgesReassigned  int
	EdgesDeleted     int
	FramesMigrated   int
	Errors           int
	DryRunDuplicates int
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *dryRun {
		*apply = false
	}

	ctx := context.Background()

	// Initialize Firestore
	fsClient, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	// Initialize GCS client
	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}

	// Determine bucket
	bucketName := *ragBucket
	if bucketName == "" {
		bucketName = os.Getenv("RAG_DOCUMENTS_BUCKET")
	}
	if bucketName == "" {
		bucketName = fmt.Sprintf("%s-rag-documents", strings.TrimSpace(*projectID))
	}

	// Initialize repositories
	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient).WithStorage(gcsClient, bucketName)
	edgesRepo := repo.NewDependencyEdgesRepository(fsClient)
	framesRepo := repo.NewFramesRepository(fsClient)

	// Vertex AI Search sync (optional)
	if *apply && !*skipVertexSync {
		vertexStoreID := os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")
		if vertexStoreID == "" {
			vertexStoreID = "rag-documents-store"
		}
		vertexLoc := os.Getenv("VERTEX_SEARCH_LOCATION")
		if vertexLoc == "" {
			vertexLoc = "global"
		}
		vertexSearchClient := ai.NewVertexAISearchClient(*projectID, vertexLoc, vertexStoreID, "")
		if vertexSearchClient != nil && vertexSearchClient.IsConfigured() {
			fragmentsRepo = fragmentsRepo.WithVertexSearchSync(func(ctx context.Context, gcsURI string) (string, error) {
				return vertexSearchClient.ImportDocuments(ctx, &ai.ImportDocumentsRequest{
					GCSPath:            strings.TrimSpace(gcsURI),
					ReconciliationMode: "INCREMENTAL",
					DataSchema:         "document",
				})
			})
		}
	}

	log.Printf("=== Horizon: Deduplicate PERSON fragments ===")
	log.Printf("project=%s tenant=%s apply=%v limit=%d skipVertexSync=%v pageSize=%d startAfter=%s maxFragments=%d",
		*projectID, *tenantID, *apply, *limit, *skipVertexSync, *pageSize, strings.TrimSpace(*startAfter), *maxFragments)

	// Step 1: Load all PERSON fragments
	log.Println("Loading PERSON fragments...")
	persons := make([]*models.HorizonFragment, 0, 1000)
	var lastDoc *firestore.DocumentSnapshot
	if strings.TrimSpace(*startAfter) != "" {
		if doc, err := fsClient.Collection("horizon_fragments").Doc(strings.TrimSpace(*startAfter)).Get(ctx); err == nil {
			lastDoc = doc
		} else {
			log.Printf("WARNING: failed to resolve start-after doc %s: %v", strings.TrimSpace(*startAfter), err)
		}
	}
	for {
		if *maxFragments > 0 && len(persons) >= *maxFragments {
			break
		}
		q := fsClient.Collection("horizon_fragments").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("type", "==", string(models.FragmentTypePerson)).
			OrderBy("__name__", firestore.Asc).
			Limit(*pageSize)
		if lastDoc != nil {
			q = q.StartAfter(lastDoc)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("Failed to load PERSON fragments: %v", err)
		}
		if len(docs) == 0 {
			break
		}
		for _, doc := range docs {
			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}
			if strings.TrimSpace(frag.ID) == "" {
				continue
			}
			persons = append(persons, &frag)
			if *maxFragments > 0 && len(persons) >= *maxFragments {
				break
			}
		}
		lastDoc = docs[len(docs)-1]
		if len(docs) < *pageSize {
			break
		}
	}
	log.Printf("Loaded %d PERSON fragments", len(persons))

	stats := &Stats{TotalPersons: len(persons)}

	// Step 2: Find duplicate clusters
	log.Println("Finding duplicate clusters...")
	clusters := findDuplicateClusters(persons)
	log.Printf("Found %d duplicate clusters", len(clusters))
	stats.ClustersFound = len(clusters)

	if len(clusters) == 0 {
		log.Println("No duplicates found!")
		return
	}

	// Step 3: Process each cluster
	processed := 0
	for _, cluster := range clusters {
		if *limit > 0 && processed >= *limit {
			break
		}

		if *verbose {
			log.Printf("Cluster [%s=%s]: %d fragments", cluster.MatchType, cluster.MatchValue, len(cluster.Fragments))
			for _, f := range cluster.Fragments {
				log.Printf("  - %s (id=%s)", f.CanonicalPath, f.ID)
			}
		}

		// Select canonical (best) fragment
		canonical := selectCanonicalFragment(cluster.Fragments)
		if canonical == nil {
			continue
		}
		cluster.CanonicalID = canonical.ID

		duplicates := make([]*models.HorizonFragment, 0, len(cluster.Fragments)-1)
		for _, f := range cluster.Fragments {
			if f.ID != canonical.ID {
				duplicates = append(duplicates, f)
			}
		}

		if len(duplicates) == 0 {
			continue
		}

		if *dryRun || !*apply {
			log.Printf("[DRY-RUN] Would merge %d duplicates into %s (%s)", len(duplicates), canonical.CanonicalPath, canonical.ID)
			for _, dup := range duplicates {
				log.Printf("  - Would delete: %s (id=%s)", dup.CanonicalPath, dup.ID)
			}
			stats.DryRunDuplicates += len(duplicates)
		} else {
			// Merge and delete duplicates
			for _, dup := range duplicates {
				reassigned, deleted, migrated, err := mergeAndDelete(ctx, fsClient, fragmentsRepo, edgesRepo, framesRepo, dup, canonical)
				if err != nil {
					log.Printf("ERROR: Failed to merge %s into %s: %v", dup.ID, canonical.ID, err)
					stats.Errors++
					continue
				}
				stats.EdgesReassigned += reassigned
				stats.EdgesDeleted += deleted
				stats.FramesMigrated += migrated
				stats.FragmentsDeleted++
				log.Printf("  ✓ Merged %s into %s (edges: +%d -%d, frames: %d)",
					dup.CanonicalPath, canonical.CanonicalPath, reassigned, deleted, migrated)
			}
			stats.FragmentsMerged++
		}

		processed++
	}

	// Print summary
	log.Println("\n=== Deduplication Summary ===")
	log.Printf("Total PERSON fragments: %d", stats.TotalPersons)
	log.Printf("Duplicate clusters found: %d", stats.ClustersFound)
	if *dryRun || !*apply {
		log.Printf("[DRY-RUN] Would delete %d duplicate fragments", stats.DryRunDuplicates)
		log.Println("Run with --apply to execute changes")
	} else {
		log.Printf("Clusters merged: %d", stats.FragmentsMerged)
		log.Printf("Fragments deleted: %d", stats.FragmentsDeleted)
		log.Printf("Edges reassigned: %d", stats.EdgesReassigned)
		log.Printf("Edges deleted (duplicates): %d", stats.EdgesDeleted)
		log.Printf("Frames migrated: %d", stats.FramesMigrated)
		log.Printf("Errors: %d", stats.Errors)
	}
}

// findDuplicateClusters groups PERSON fragments into duplicate clusters.
func findDuplicateClusters(persons []*models.HorizonFragment) []*DuplicateCluster {
	// Union-find structure for transitive clustering
	parent := make(map[string]string)
	var find func(id string) string
	find = func(id string) string {
		if parent[id] == "" {
			parent[id] = id
		}
		if parent[id] != id {
			parent[id] = find(parent[id])
		}
		return parent[id]
	}
	union := func(a, b string) {
		pa, pb := find(a), find(b)
		if pa != pb {
			parent[pa] = pb
		}
	}

	// Index fragments by various keys
	byWikidata := make(map[string][]*models.HorizonFragment)
	byLegalName := make(map[string][]*models.HorizonFragment)
	byShortName := make(map[string][]*models.HorizonFragment)
	byLabelEN := make(map[string][]*models.HorizonFragment)
	byPhonetic := make(map[string][]*models.HorizonFragment)
	byTokenSet := make(map[string][]*models.HorizonFragment) // token set key -> fragments

	for _, p := range persons {
		if p == nil || strings.TrimSpace(p.ID) == "" {
			continue
		}

		// Wikidata ID
		wikidataID := extractWikidataID(p)
		if wikidataID != "" {
			byWikidata[wikidataID] = append(byWikidata[wikidataID], p)
		}

		// Exact identifiers
		if legalName := strings.ToLower(strings.TrimSpace(p.Identifiers["legalName"])); legalName != "" {
			byLegalName[legalName] = append(byLegalName[legalName], p)
		}
		if shortName := strings.ToLower(strings.TrimSpace(p.Identifiers["shortName"])); shortName != "" {
			byShortName[shortName] = append(byShortName[shortName], p)
		}
		if labelEN := strings.ToLower(strings.TrimSpace(p.Labels["en"])); labelEN != "" {
			byLabelEN[labelEN] = append(byLabelEN[labelEN], p)
		}

		// Phonetic key
		phoneticKey := buildPhoneticKey(p)
		if phoneticKey != "" {
			byPhonetic[phoneticKey] = append(byPhonetic[phoneticKey], p)
		}

		// Token set key (for subset matching)
		tokenSetKey := buildTokenSetKey(p)
		if tokenSetKey != "" {
			byTokenSet[tokenSetKey] = append(byTokenSet[tokenSetKey], p)
		}
	}

	// Build unions from exact matches
	for _, frags := range byWikidata {
		if len(frags) > 1 {
			for i := 1; i < len(frags); i++ {
				union(frags[0].ID, frags[i].ID)
			}
		}
	}
	for _, frags := range byLegalName {
		if len(frags) > 1 {
			for i := 1; i < len(frags); i++ {
				union(frags[0].ID, frags[i].ID)
			}
		}
	}
	for _, frags := range byShortName {
		if len(frags) > 1 {
			for i := 1; i < len(frags); i++ {
				union(frags[0].ID, frags[i].ID)
			}
		}
	}
	for _, frags := range byLabelEN {
		if len(frags) > 1 {
			for i := 1; i < len(frags); i++ {
				union(frags[0].ID, frags[i].ID)
			}
		}
	}
	for _, frags := range byPhonetic {
		if len(frags) > 1 {
			for i := 1; i < len(frags); i++ {
				union(frags[0].ID, frags[i].ID)
			}
		}
	}

	// Token-subset matching: O(n²) but filtered
	// Store both token set and first name for each person
	type personTokenInfo struct {
		tokens    []string
		firstName string
	}
	tokenInfoByID := make(map[string]personTokenInfo)
	for _, p := range persons {
		if p == nil || strings.TrimSpace(p.ID) == "" {
			continue
		}
		tokens := buildTokenSet(p)
		// Require at least 4 tokens to avoid false positives on short names
		if len(tokens) >= 4 {
			firstName := extractFirstName(p)
			if firstName != "" {
				tokenInfoByID[p.ID] = personTokenInfo{tokens: tokens, firstName: firstName}
			}
		}
	}

	// Compare pairs for token-subset match (with first-name anchor)
	personIDs := make([]string, 0, len(tokenInfoByID))
	for id := range tokenInfoByID {
		personIDs = append(personIDs, id)
	}
	for i := 0; i < len(personIDs); i++ {
		for j := i + 1; j < len(personIDs); j++ {
			infoA := tokenInfoByID[personIDs[i]]
			infoB := tokenInfoByID[personIDs[j]]
			// First name must match
			if infoA.firstName != infoB.firstName {
				continue
			}
			if scout.IsTokenSubsetMatchExported(infoA.tokens, infoB.tokens) {
				union(personIDs[i], personIDs[j])
			}
		}
	}

	// Group by root
	groups := make(map[string][]*models.HorizonFragment)
	fragByID := make(map[string]*models.HorizonFragment)
	for _, p := range persons {
		if p == nil || strings.TrimSpace(p.ID) == "" {
			continue
		}
		fragByID[p.ID] = p
		root := find(p.ID)
		groups[root] = append(groups[root], p)
	}

	// Build clusters from groups with >1 fragment
	var clusters []*DuplicateCluster
	for _, frags := range groups {
		if len(frags) <= 1 {
			continue
		}
		// Determine match type (best effort)
		matchType := "tokenSubset"
		matchValue := ""
		for _, f := range frags {
			if wid := extractWikidataID(f); wid != "" {
				matchType = "wikidataId"
				matchValue = wid
				break
			}
		}
		if matchType == "tokenSubset" {
			if len(frags) > 0 {
				matchValue = strings.TrimSpace(frags[0].Labels["en"])
			}
		}
		clusters = append(clusters, &DuplicateCluster{
			MatchType:  matchType,
			MatchValue: matchValue,
			Fragments:  frags,
		})
	}

	return clusters
}

// selectCanonicalFragment selects the best fragment to keep from a cluster.
// Prefers: has Wikidata ID > has legalName > has more identifiers > older createdAt
func selectCanonicalFragment(frags []*models.HorizonFragment) *models.HorizonFragment {
	if len(frags) == 0 {
		return nil
	}
	if len(frags) == 1 {
		return frags[0]
	}

	// Score each fragment
	type scored struct {
		frag  *models.HorizonFragment
		score int
	}
	var scored_frags []scored
	for _, f := range frags {
		s := 0
		if extractWikidataID(f) != "" {
			s += 100
		}
		if strings.TrimSpace(f.Identifiers["legalName"]) != "" {
			s += 50
		}
		if strings.TrimSpace(f.Identifiers["shortName"]) != "" {
			s += 25
		}
		s += len(f.Identifiers) * 5
		s += len(f.KBRefs) * 10
		if f.Status == "active" {
			s += 20
		}
		scored_frags = append(scored_frags, scored{frag: f, score: s})
	}

	// Sort by score desc, then by createdAt asc (older is better)
	sort.Slice(scored_frags, func(i, j int) bool {
		if scored_frags[i].score != scored_frags[j].score {
			return scored_frags[i].score > scored_frags[j].score
		}
		return scored_frags[i].frag.Metadata.CreatedAt.Before(scored_frags[j].frag.Metadata.CreatedAt)
	})

	return scored_frags[0].frag
}

// mergeAndDelete merges dup into canonical and deletes dup.
func mergeAndDelete(
	ctx context.Context,
	db *firestore.Client,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	edgesRepo *repo.DependencyEdgesRepository,
	framesRepo *repo.FramesRepository,
	dup, canonical *models.HorizonFragment,
) (edgesReassigned, edgesDeleted, framesMigrated int, err error) {
	if dup == nil || canonical == nil {
		return 0, 0, 0, fmt.Errorf("nil dup/canonical")
	}
	if strings.TrimSpace(dup.ID) == "" || strings.TrimSpace(canonical.ID) == "" {
		return 0, 0, 0, fmt.Errorf("missing dup/canonical id")
	}
	if dup.ID == canonical.ID {
		return 0, 0, 0, nil
	}

	// Best-effort merge data from dup into canonical
	changed := mergeFragmentData(canonical, dup)
	if changed {
		if err := fragmentsRepo.Update(ctx, canonical); err != nil {
			return 0, 0, 0, fmt.Errorf("update canonical failed: %w", err)
		}
	}

	// Reassign edges (direct Firestore queries like geonames script)
	reassigned, deleted, err := reassignEdges(ctx, db, edgesRepo, dup.ID, canonical.ID)
	if err != nil {
		return reassigned, deleted, 0, err
	}

	// Migrate frames (direct Firestore queries)
	migrated, err := migrateFrames(ctx, db, framesRepo, "global", dup.CanonicalPath, canonical.CanonicalPath, 1000)
	if err != nil {
		return reassigned, deleted, migrated, err
	}

	// Delete duplicate fragment
	if err := fragmentsRepo.Delete(ctx, dup.ID); err != nil {
		return reassigned, deleted, migrated, fmt.Errorf("delete dup failed: %w", err)
	}

	return reassigned, deleted, migrated, nil
}

// mergeFragmentData merges data from source into target (fill-only).
func mergeFragmentData(target, source *models.HorizonFragment) bool {
	if target == nil || source == nil {
		return false
	}
	changed := false

	// Merge identifiers (fill-only)
	if target.Identifiers == nil {
		target.Identifiers = make(map[string]string)
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(target.Identifiers[k]) == "" && strings.TrimSpace(v) != "" {
			target.Identifiers[k] = v
			changed = true
		}
	}

	// Merge labels (fill-only)
	if target.Labels == nil {
		target.Labels = make(map[string]string)
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(target.Labels[k]) == "" && strings.TrimSpace(v) != "" {
			target.Labels[k] = v
			changed = true
		}
	}

	// Merge KBRefs (append unique)
	seenKB := make(map[string]bool)
	for _, ref := range target.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		seenKB[key] = true
	}
	for _, ref := range source.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		if !seenKB[key] {
			target.KBRefs = append(target.KBRefs, ref)
			seenKB[key] = true
			changed = true
		}
	}

	if changed {
		target.Metadata.LastUpdatedAt = time.Now().UTC()
	}
	return changed
}

// reassignEdges moves dependency edges from oldFragmentID to newFragmentID, deleting duplicates/self-loops.
func reassignEdges(ctx context.Context, db *firestore.Client, edgesRepo *repo.DependencyEdgesRepository, oldID, newID string) (reassigned, deleted int, err error) {
	if db == nil || edgesRepo == nil {
		return 0, 0, fmt.Errorf("edges repo not configured")
	}
	tenant := "global"
	edgesCol := db.Collection("horizon_dependency_edges")

	// Source edges
	docs1, err := edgesCol.Where("tenantId", "==", tenant).Where("sourceFragmentId", "==", oldID).Documents(ctx).GetAll()
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
		ns, nt := newID, edge.TargetFragmentID
		// Self-loop check
		if ns == nt {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		// Duplicate check
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenant, ns, nt, edge.Type)
		if err == nil && existing != nil {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.SourceFragmentID = ns
		edge.TargetFragmentID = nt
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}

	// Target edges
	docs2, err := edgesCol.Where("tenantId", "==", tenant).Where("targetFragmentId", "==", oldID).Documents(ctx).GetAll()
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
		ns, nt := edge.SourceFragmentID, newID
		// Self-loop check
		if ns == nt {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		// Duplicate check
		existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenant, ns, nt, edge.Type)
		if err == nil && existing != nil {
			if err := edgesRepo.Delete(ctx, edge.ID); err == nil {
				deleted++
			}
			continue
		}
		edge.SourceFragmentID = ns
		edge.TargetFragmentID = nt
		if err := edgesRepo.Update(ctx, &edge); err == nil {
			reassigned++
		}
	}
	return reassigned, deleted, nil
}

// migrateFrames migrates frames from oldPath to newPath.
func migrateFrames(ctx context.Context, db *firestore.Client, framesRepo *repo.FramesRepository, tenantID, oldPath, newPath string, limit int) (int, error) {
	if db == nil || framesRepo == nil {
		return 0, nil
	}
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return 0, nil
	}
	frames, err := framesRepo.ListByTargetCanonicalPath(ctx, tenantID, oldPath, limit)
	if err != nil {
		return 0, err
	}
	if len(frames) == 0 {
		return 0, nil
	}
	col := db.Collection("horizon_frames")
	migrated := 0
	for _, f := range frames {
		if f == nil || strings.TrimSpace(f.ID) == "" {
			continue
		}
		_, err := col.Doc(f.ID).Update(ctx, []firestore.Update{
			{Path: "targetCanonicalPath", Value: newPath},
		})
		if err == nil {
			migrated++
		}
	}
	return migrated, nil
}

// extractWikidataID extracts Wikidata ID from fragment.
func extractWikidataID(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	// Check identifiers
	if wid := strings.TrimSpace(f.Identifiers["wikidataId"]); wid != "" {
		return wid
	}
	// Check KBRefs
	for _, ref := range f.KBRefs {
		if strings.EqualFold(ref.KB, "Wikidata") && strings.TrimSpace(ref.ExternalID) != "" {
			return strings.TrimSpace(ref.ExternalID)
		}
	}
	return ""
}

// buildPhoneticKey builds phonetic key for a person fragment.
func buildPhoneticKey(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}
	names := []string{
		f.Labels["en"],
		f.Identifiers["legalName"],
		f.Identifiers["shortName"],
	}
	if strings.HasPrefix(f.CanonicalPath, "/person/") {
		parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
		if len(parts) >= 2 {
			names = append(names, strings.ReplaceAll(parts[len(parts)-1], "-", " "))
		}
	}
	var bestKey string
	for _, name := range names {
		key := scout.NormalizePhoneticKey(name)
		if key != "" && len(key) > len(bestKey) {
			bestKey = key
		}
	}
	return bestKey
}

// extractFirstName extracts the first core token (first name) from a person fragment.
// Skips titles and particles.
func extractFirstName(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}

	particles := map[string]struct{}{
		"al": {}, "il": {}, "bin": {}, "bint": {}, "ibn": {}, "ben": {}, "abu": {}, "banu": {},
		"sheikh": {}, "dr": {}, "captain": {}, "general": {}, "prince": {}, "princess": {},
	}

	names := []string{
		f.Labels["en"],
		f.Identifiers["legalName"],
		f.Identifiers["shortName"],
	}
	if strings.HasPrefix(f.CanonicalPath, "/person/") {
		parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
		if len(parts) >= 2 {
			names = append(names, strings.ReplaceAll(parts[len(parts)-1], "-", " "))
		}
	}

	for _, name := range names {
		tokens := normalizeNameToTokens(name)
		for _, t := range tokens {
			if t == "" {
				continue
			}
			// Skip particles and titles
			if _, isParticle := particles[t]; isParticle {
				continue
			}
			return t // Return first non-particle token
		}
	}
	return ""
}

// buildTokenSetKey builds a sorted token set key for grouping.
func buildTokenSetKey(f *models.HorizonFragment) string {
	tokens := buildTokenSet(f)
	// Require at least 4 tokens to avoid false positives
	if len(tokens) < 4 {
		return ""
	}
	sort.Strings(tokens)
	return strings.Join(tokens, "|")
}

// buildTokenSet builds a token set for a person fragment.
func buildTokenSet(f *models.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	names := []string{
		f.Labels["en"],
		f.Identifiers["legalName"],
		f.Identifiers["shortName"],
	}
	if strings.HasPrefix(f.CanonicalPath, "/person/") {
		parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
		if len(parts) >= 2 {
			names = append(names, strings.ReplaceAll(parts[len(parts)-1], "-", " "))
		}
	}

	tokenSet := make(map[string]struct{})
	particles := map[string]struct{}{
		"al": {}, "il": {}, "bin": {}, "bint": {}, "ibn": {}, "ben": {}, "abo": {}, "bano": {},
	}
	for _, name := range names {
		tokens := normalizeNameToTokens(name)
		for _, t := range tokens {
			if _, isParticle := particles[t]; !isParticle && t != "" {
				tokenSet[t] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(tokenSet))
	for t := range tokenSet {
		result = append(result, t)
	}
	return result
}

// normalizeNameToTokens normalizes a name and returns phonetically-normalized tokens.
func normalizeNameToTokens(name string) []string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}

	replacements := []struct{ from, to string }{
		{"oo", "o"}, {"ou", "o"}, {"uu", "o"},
		{"ee", "i"}, {"ie", "i"}, {"ei", "i"},
		{"y", "i"}, {"e", "i"}, {"u", "o"},
		{"kh", "h"}, {"ph", "f"}, {"qu", "k"}, {"aa", "a"},
	}
	for _, r := range replacements {
		name = strings.ReplaceAll(name, r.from, r.to)
	}

	var b strings.Builder
	b.Grow(len(name))
	prevChar := ' '
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			vowels := "aeiou"
			if r == prevChar && !strings.ContainsRune(vowels, r) {
				continue
			}
			b.WriteRune(r)
			prevChar = r
		} else if r == ' ' && prevChar != ' ' {
			b.WriteRune(r)
			prevChar = ' '
		}
	}

	return strings.Fields(strings.TrimSpace(b.String()))
}
