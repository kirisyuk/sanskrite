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

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

var (
	dryRun    = flag.Bool("dry-run", true, "Dry run mode: only report duplicates without making changes")
	projectID = flag.String("project", "", "GCP project ID (required)")
	bucket    = flag.String("bucket", "", "GCS bucket for fragment export (optional, for re-export)")
	tenantID  = flag.String("tenant", "global", "Tenant ID to process (default: global)")
	limit     = flag.Int("limit", 0, "Limit number of canonical paths to process (0 = all)")
	prefix    = flag.String("prefix", "", "Canonical path prefix filter (optional)")
	verbose   = flag.Bool("verbose", false, "Verbose logging")
	skipGCS   = flag.Bool("skip-gcs", false, "Skip GCS/Vertex export+delete side effects during dedup (faster, Firestore-only merge)")
)

func main() {
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()

	// Horizon fragments/edges are always in tenantId="global". Keep flag for UX, but enforce.
	if strings.TrimSpace(*tenantID) != "" && strings.TrimSpace(*tenantID) != "global" {
		log.Printf("WARNING: Horizon is global-only. Overriding --tenant=%q to \"global\".", strings.TrimSpace(*tenantID))
		*tenantID = "global"
	}

	// Initialize Firestore
	fsClient, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	var gcsClient *gcp.GCSClient
	bucketName := ""
	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient)
	if !*skipGCS {
		var err error
		gcsClient, err = gcp.NewGCSClient(ctx, *projectID)
		if err != nil {
			log.Fatalf("Failed to create GCS client: %v", err)
		}
		bucketName = *bucket
		if bucketName == "" {
			bucketName = firstNonEmpty(os.Getenv("NV_HORIZON_FRAGMENTS_BUCKET"), os.Getenv("HORIZON_FRAGMENTS_BUCKET"))
		}
		if bucketName == "" {
			bucketName = defaultHorizonFragmentsBucket(*projectID)
			log.Printf("INFO: Using default bucket: %s", bucketName)
		}
		fragmentsRepo = fragmentsRepo.WithStorage(gcsClient, bucketName)
	}

	// Initialize repositories
	edgesRepo := repo.NewDependencyEdgesRepository(fsClient)
	watchdogRepo := repo.NewWatchdogConfigsRepository(fsClient)
	lensesRepo := repo.NewLensesRepository(fsClient)
	materializedRepo := repo.NewMaterializedArtifactsRepository(fsClient)
	watchSubjectsRepo := repo.NewWatchSubjectsRepository(fsClient)
	versionsRepo := repo.NewHorizonFragmentVersionsRepository(fsClient)
	mergeAuditRepo := repo.NewFragmentMergeAuditRepository(fsClient)
	redirectsRepo := repo.NewCanonicalPathRedirectsRepository(fsClient)
	ontologyFactsRepo := corerepo.NewOntologyFactsRepository(fsClient)
	entitiesRepo := corerepo.NewEntityRepository(fsClient)
	mergeRepo := repo.NewFragmentsMergeRepository(fragmentsRepo, edgesRepo, mergeAuditRepo).
		WithRedirectsRepo(redirectsRepo).
		WithMaterializedRepo(materializedRepo).
		WithLensesRepo(lensesRepo).
		WithWatchSubjectsRepo(watchSubjectsRepo).
		WithOntologyFactsRepo(ontologyFactsRepo).
		WithEntitiesRepo(entitiesRepo)

	log.Printf("Starting deduplication scan (dry-run=%v, tenant=%s)...", *dryRun, *tenantID)

	// Step 1: Find all duplicates by canonicalPath
	duplicates, err := findDuplicates(ctx, fragmentsRepo, *tenantID, strings.TrimSpace(*prefix))
	if err != nil {
		log.Fatalf("Failed to find duplicates: %v", err)
	}

	if len(duplicates) == 0 {
		log.Println("No duplicates found!")
		return
	}

	log.Printf("Found %d canonical paths with duplicates", len(duplicates))

	// Step 2: Process each duplicate group
	stats := &DeduplicationStats{}
	processed := 0

	for canonicalPath, fragmentIDs := range duplicates {
		if *limit > 0 && processed >= *limit {
			break
		}

		if *verbose {
			log.Printf("Processing: %s (%d duplicates)", canonicalPath, len(fragmentIDs))
		}

		// Select primary fragment (best one to keep)
		primaryID, err := selectPrimaryFragment(ctx, fragmentsRepo, fragmentIDs)
		if err != nil {
			log.Printf("ERROR: Failed to select primary for %s: %v", canonicalPath, err)
			continue
		}

		duplicateIDs := make([]string, 0, len(fragmentIDs)-1)
		for _, id := range fragmentIDs {
			if id != primaryID {
				duplicateIDs = append(duplicateIDs, id)
			}
		}

		if *dryRun {
			log.Printf("  [DRY-RUN] Would keep: %s, remove: %v", primaryID, duplicateIDs)
			stats.DryRunDuplicates += len(duplicateIDs)
		} else {
			// Merge each duplicate via single entry point (data, edges, delete, audit)
			if mergeRepo != nil {
				for _, dupID := range duplicateIDs {
					_, err := mergeRepo.MergeFragment(ctx, primaryID, dupID, *tenantID, "tool_dedup", "horizon_deduplicate_fragments", "", "")
					if err != nil {
						log.Printf("WARNING: Merge %s -> %s failed: %v", dupID, primaryID, err)
						continue
					}
					stats.DeletedFragments++
				}
			} else {
				// Legacy: merge data, migrate refs, delete
				if err := mergeDuplicateFragmentData(ctx, fragmentsRepo, versionsRepo, primaryID, duplicateIDs); err != nil {
					log.Printf("WARNING: Failed to merge fragment data into primary %s for %s: %v", primaryID, canonicalPath, err)
				}
				if err := migrateReferences(ctx, primaryID, duplicateIDs, edgesRepo, watchdogRepo, lensesRepo, versionsRepo, fsClient, canonicalPath, stats); err != nil {
					log.Printf("ERROR: Failed to migrate references for %s: %v", canonicalPath, err)
					continue
				}
				for _, dupID := range duplicateIDs {
					if err := fragmentsRepo.Delete(ctx, dupID); err != nil {
						log.Printf("ERROR: Failed to delete fragment %s: %v", dupID, err)
						continue
					}
					stats.DeletedFragments++
				}
			}

			// Migrate watchdog/lenses/etc refs (merge repo doesn't do these)
			if err := migrateReferences(ctx, primaryID, duplicateIDs, edgesRepo, watchdogRepo, lensesRepo, versionsRepo, fsClient, canonicalPath, stats); err != nil {
				log.Printf("ERROR: Failed to migrate references for %s: %v", canonicalPath, err)
				continue
			}

			// Re-export primary fragment to GCS (to ensure it's up-to-date in Vertex)
			primaryFragment, err := fragmentsRepo.Get(ctx, primaryID)
			if err == nil {
				if err := fragmentsRepo.ExportToGCS(ctx, primaryFragment, false); err != nil {
					log.Printf("WARNING: Failed to re-export %s to GCS: %v", primaryID, err)
				} else {
					stats.ReExported++
				}
			}

			log.Printf("  ✓ Merged %d duplicates into %s", len(duplicateIDs), primaryID)
			stats.MergedGroups++
		}

		processed++
	}

	// Print summary
	log.Println("\n=== Deduplication Summary ===")
	if *dryRun {
		log.Printf("Dry-run mode: Found %d duplicate groups, %d total duplicate fragments", len(duplicates), stats.DryRunDuplicates)
		log.Println("Run with --dry-run=false to apply changes")
	} else {
		log.Printf("Merged %d duplicate groups", stats.MergedGroups)
		log.Printf("Deleted %d duplicate fragments", stats.DeletedFragments)
		log.Printf("Updated %d edges", stats.UpdatedEdges)
		log.Printf("Updated %d watchdog configs", stats.UpdatedWatchdogs)
		log.Printf("Updated %d lenses", stats.UpdatedLenses)
		log.Printf("Updated %d lens artifacts", stats.UpdatedLensArtifacts)
		log.Printf("Updated %d lens items", stats.UpdatedLensItems)
		log.Printf("Updated %d fragment profiles", stats.UpdatedProfiles)
		log.Printf("Updated %d missing info statuses", stats.UpdatedMissingInfo)
		log.Printf("Updated %d cascade entity records", stats.UpdatedCascadeEntity)
		log.Printf("Deleted %d fragment versions", stats.DeletedVersions)
		if gcsClient != nil {
			log.Printf("Re-exported %d fragments to GCS", stats.ReExported)
		}
	}
}

// mergeDuplicateFragmentData merges fragment fields + state metrics from duplicate fragments (and their versions)
// into the primary fragment, preferring richer/higher-confidence MetricValues.
func mergeDuplicateFragmentData(
	ctx context.Context,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	versionsRepo *repo.HorizonFragmentVersionsRepository,
	primaryID string,
	duplicateIDs []string,
) error {
	if strings.TrimSpace(primaryID) == "" || len(duplicateIDs) == 0 {
		return nil
	}

	primary, err := fragmentsRepo.Get(ctx, primaryID)
	if err != nil || primary == nil {
		return fmt.Errorf("failed to load primary fragment %s: %w", primaryID, err)
	}

	changed := false

	// Merge each duplicate fragment.
	for _, dupID := range duplicateIDs {
		dupID = strings.TrimSpace(dupID)
		if dupID == "" || dupID == primaryID {
			continue
		}
		dup, err := fragmentsRepo.Get(ctx, dupID)
		if err != nil || dup == nil {
			continue
		}
		if mergeFragmentDataPreferMetrics(primary, dup) {
			changed = true
		}

		// Also merge metrics from historical versions of the duplicate (best-effort).
		// Versions store only State metrics, and are otherwise deleted during dedup.
		if versionsRepo != nil {
			versions, err := versionsRepo.ListByFragment(ctx, "global", dupID, 10000, 0)
			if err == nil && len(versions) > 0 {
				for _, v := range versions {
					if v == nil || v.State == nil || len(v.State) == 0 {
						continue
					}
					if mergeStatePreferMetrics(primary, v.State) {
						changed = true
					}
				}
			}
		}
	}

	if !changed {
		return nil
	}
	// Persist merged primary (Validate() runs inside Update).
	if err := fragmentsRepo.Update(ctx, primary); err != nil {
		return fmt.Errorf("failed to update merged primary fragment: %w", err)
	}
	return nil
}

// mergeFragmentDataPreferMetrics merges source fragment data into target. It is "fill-first" for identity fields,
// but merges State metrics by preferring higher-confidence/newer values and keeping more keys overall.
func mergeFragmentDataPreferMetrics(target, source *models.HorizonFragment) bool {
	changed := false
	if target == nil || source == nil {
		return false
	}

	// Labels: fill missing.
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	for k, v := range source.Labels {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Labels[k]) == "" {
			target.Labels[k] = v
			changed = true
		}
	}

	// Identifiers: fill missing.
	if target.Identifiers == nil {
		target.Identifiers = map[string]string{}
	}
	for k, v := range source.Identifiers {
		if strings.TrimSpace(v) != "" && strings.TrimSpace(target.Identifiers[k]) == "" {
			target.Identifiers[k] = v
			changed = true
		}
	}

	// KBRefs: union.
	existingRefs := map[string]bool{}
	for _, ref := range target.KBRefs {
		existingRefs[fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)] = true
	}
	for _, ref := range source.KBRefs {
		key := fmt.Sprintf("%s:%s", ref.KB, ref.ExternalID)
		if !existingRefs[key] {
			target.KBRefs = append(target.KBRefs, ref)
			changed = true
		}
	}

	// Description: fill if empty.
	if strings.TrimSpace(target.Description) == "" && strings.TrimSpace(source.Description) != "" {
		target.Description = source.Description
		changed = true
	}

	// Tags: union.
	tagSet := map[string]bool{}
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

	// OrgSize: fill if empty/na.
	if strings.TrimSpace(source.OrgSize) != "" {
		cur := strings.TrimSpace(strings.ToLower(target.OrgSize))
		in := strings.TrimSpace(source.OrgSize)
		if cur == "" || cur == "na" {
			target.OrgSize = in
			changed = true
		}
	}

	// MarketSegments: union + dedup.
	if len(source.MarketSegments) > 0 {
		seen := map[string]bool{}
		for _, s := range target.MarketSegments {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = true
			}
		}
		for _, s := range source.MarketSegments {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			target.MarketSegments = append(target.MarketSegments, s)
			changed = true
		}
	}

	// State metrics: union + prefer richer.
	if mergeStatePreferMetrics(target, source.State) {
		changed = true
	}

	// Track merged canonical paths for future alias resolution.
	if strings.TrimSpace(source.CanonicalPath) != "" {
		if target.Identifiers["merged_paths"] == "" {
			target.Identifiers["merged_paths"] = source.CanonicalPath
			changed = true
		} else if !strings.Contains(target.Identifiers["merged_paths"], source.CanonicalPath) {
			target.Identifiers["merged_paths"] += "," + source.CanonicalPath
			changed = true
		}
	}

	return changed
}

func mergeStatePreferMetrics(target *models.HorizonFragment, src map[string]models.MetricValue) bool {
	if target == nil || src == nil || len(src) == 0 {
		return false
	}
	if target.State == nil {
		target.State = map[string]models.MetricValue{}
	}
	changed := false
	for k, mv := range src {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		cur, ok := target.State[key]
		if !ok {
			target.State[key] = mv
			changed = true
			continue
		}
		if betterMetricValue(mv, cur) {
			target.State[key] = mv
			changed = true
		}
	}
	return changed
}

// betterMetricValue returns true if a is preferable to b.
func betterMetricValue(a, b models.MetricValue) bool {
	// Prefer higher confidence.
	if a.Confidence > b.Confidence+1e-9 {
		return true
	}
	if b.Confidence > a.Confidence+1e-9 {
		return false
	}
	// Prefer newer observation time.
	if b.ObservedAt.IsZero() && !a.ObservedAt.IsZero() {
		return true
	}
	if !b.ObservedAt.IsZero() && a.ObservedAt.IsZero() {
		return false
	}
	if !a.ObservedAt.IsZero() && !b.ObservedAt.IsZero() && a.ObservedAt.After(b.ObservedAt) {
		return true
	}
	// Prefer richer provenance.
	if len(a.Provenance.SourceIDs) > len(b.Provenance.SourceIDs) {
		return true
	}
	// Prefer unit presence.
	bu := ""
	au := ""
	if b.Unit != nil {
		bu = strings.TrimSpace(*b.Unit)
	}
	if a.Unit != nil {
		au = strings.TrimSpace(*a.Unit)
	}
	if bu == "" && au != "" {
		return true
	}
	// Prefer non-zero value.
	if b.Value == 0 && a.Value != 0 {
		return true
	}
	return false
}

type DeduplicationStats struct {
	MergedGroups         int
	DeletedFragments     int
	UpdatedEdges         int
	UpdatedWatchdogs     int
	UpdatedLenses        int
	UpdatedLensArtifacts int
	UpdatedLensItems     int
	UpdatedProfiles      int
	UpdatedMissingInfo   int
	UpdatedCascadeEntity int
	DeletedVersions      int
	ReExported           int
	DryRunDuplicates     int
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func defaultHorizonFragmentsBucket(projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if strings.EqualFold(projectID, "nv-horizon") {
		return "nv-horizon-fragments"
	}
	return fmt.Sprintf("%s-horizon-fragments", projectID)
}

// findDuplicates finds all canonical paths that have multiple fragments
func findDuplicates(ctx context.Context, fragmentsRepo *repo.HorizonFragmentsRepository, _ string, prefix string) (map[string][]string, error) {
	duplicates := make(map[string][]string)

	// Query all fragments for the tenant
	// fragmentsRepo.Query() already scopes to tenantId=="global" for Horizon.
	// Keep tenantID argument for API compatibility, but don't add redundant/contradictory filters.
	q := fragmentsRepo.Query()
	if prefix != "" {
		q = q.Where("canonicalPath", ">=", prefix).
			Where("canonicalPath", "<", prefix+"\ufffd").
			OrderBy("canonicalPath", firestore.Asc)
	}
	iter := q.Documents(ctx)
	defer iter.Stop()

	pathToIDs := make(map[string][]string)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate fragments: %w", err)
		}

		var fragment models.HorizonFragment
		if err := doc.DataTo(&fragment); err != nil {
			log.Printf("WARNING: Failed to parse fragment %s: %v", doc.Ref.ID, err)
			continue
		}

		canonicalPath := strings.TrimSpace(fragment.CanonicalPath)
		if canonicalPath == "" {
			continue
		}

		pathToIDs[canonicalPath] = append(pathToIDs[canonicalPath], strings.TrimSpace(doc.Ref.ID))
	}

	// Filter to only paths with duplicates
	for path, ids := range pathToIDs {
		if len(ids) > 1 {
			duplicates[path] = ids
		}
	}

	return duplicates, nil
}

// selectPrimaryFragment selects the best fragment to keep from a group of duplicates
// Priority: 1) Verified status, 2) Most recent LastVerifiedAt, 3) Oldest CreatedAt
func selectPrimaryFragment(ctx context.Context, fragmentsRepo *repo.HorizonFragmentsRepository, fragmentIDs []string) (string, error) {
	if len(fragmentIDs) == 0 {
		return "", fmt.Errorf("empty fragment IDs list")
	}
	if len(fragmentIDs) == 1 {
		return fragmentIDs[0], nil
	}

	var candidates []*models.HorizonFragment
	for _, id := range fragmentIDs {
		frag, err := fragmentsRepo.Get(ctx, id)
		if err != nil {
			log.Printf("WARNING: Failed to get fragment %s: %v", id, err)
			continue
		}
		candidates = append(candidates, frag)
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no valid fragments found")
	}

	// Sort: verified > low_confidence, then by LastVerifiedAt desc, then by CreatedAt asc
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]

		// Prefer verified over low confidence
		if a.Metadata.EpistemicStatus == models.EpistemicStatusVerified &&
			b.Metadata.EpistemicStatus != models.EpistemicStatusVerified {
			return true
		}
		if b.Metadata.EpistemicStatus == models.EpistemicStatusVerified &&
			a.Metadata.EpistemicStatus != models.EpistemicStatusVerified {
			return false
		}

		// Prefer most recently verified
		if !a.Metadata.LastVerifiedAt.Equal(b.Metadata.LastVerifiedAt) {
			return a.Metadata.LastVerifiedAt.After(b.Metadata.LastVerifiedAt)
		}

		// Prefer oldest created (most established)
		return a.CreatedAt.Before(b.CreatedAt)
	})

	return candidates[0].ID, nil
}

// migrateReferences updates all references from duplicate fragments to the primary fragment
func migrateReferences(
	ctx context.Context,
	primaryID string,
	duplicateIDs []string,
	edgesRepo *repo.DependencyEdgesRepository,
	watchdogRepo *repo.WatchdogConfigsRepository,
	lensesRepo *repo.LensesRepository,
	versionsRepo *repo.HorizonFragmentVersionsRepository,
	fsClient *firestore.Client,
	canonicalPath string,
	stats *DeduplicationStats,
) error {
	// 1. Update DependencyEdges
	if err := migrateEdges(ctx, primaryID, duplicateIDs, edgesRepo, fsClient, stats); err != nil {
		return fmt.Errorf("migrate edges: %w", err)
	}

	// 2. Update WatchdogConfigs
	if err := migrateWatchdogs(ctx, primaryID, duplicateIDs, watchdogRepo, stats); err != nil {
		return fmt.Errorf("migrate watchdogs: %w", err)
	}

	// 3. Update Lenses (scopeFragments map)
	if err := migrateLenses(ctx, primaryID, duplicateIDs, canonicalPath, lensesRepo, stats); err != nil {
		return fmt.Errorf("migrate lenses: %w", err)
	}

	// 4. Update LensArtifacts (fragment + edge references)
	if err := migrateLensArtifacts(ctx, primaryID, duplicateIDs, canonicalPath, fsClient, stats); err != nil {
		return fmt.Errorf("migrate lens artifacts: %w", err)
	}

	// 5. Update LensItems (node + edge references)
	if err := migrateLensItems(ctx, primaryID, duplicateIDs, fsClient, stats); err != nil {
		return fmt.Errorf("migrate lens items: %w", err)
	}

	// 6. Update FragmentProfiles (fragmentId references)
	if err := migrateFragmentProfiles(ctx, primaryID, duplicateIDs, fsClient, stats); err != nil {
		return fmt.Errorf("migrate fragment profiles: %w", err)
	}

	// 7. Update MissingInfoStatus (fragmentId + deterministic ID)
	if err := migrateMissingInfoStatus(ctx, primaryID, duplicateIDs, fsClient, stats); err != nil {
		return fmt.Errorf("migrate missing info status: %w", err)
	}

	// 8. Update CascadeEntityRegistry (fragmentId bindings)
	if err := migrateCascadeEntityRegistry(ctx, primaryID, duplicateIDs, fsClient, stats); err != nil {
		return fmt.Errorf("migrate cascade entity registry: %w", err)
	}

	// 9. Delete fragment versions for duplicates (they're tied to specific fragment IDs)
	if err := deleteVersions(ctx, duplicateIDs, versionsRepo, fsClient, stats); err != nil {
		return fmt.Errorf("delete versions: %w", err)
	}

	return nil
}

// listEdgesBySourceDirect queries edges by source without OrderBy (avoids composite index requirement)
func listEdgesBySourceDirect(ctx context.Context, fsClient *firestore.Client, tenantID, sourceFragmentID string) ([]*models.DependencyEdge, error) {
	q := fsClient.Collection("horizon_dependency_edges").
		Where("tenantId", "==", tenantID).
		Where("sourceFragmentId", "==", sourceFragmentID).
		Limit(10000)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var edges []*models.DependencyEdge
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		// Backward compatibility: some historical edges might not have stored "id" field.
		// Ensure we can Update/Delete by using the document name.
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}
		edges = append(edges, &edge)
	}
	return edges, nil
}

// listEdgesByTargetDirect queries edges by target without OrderBy (avoids composite index requirement)
func listEdgesByTargetDirect(ctx context.Context, fsClient *firestore.Client, tenantID, targetFragmentID string) ([]*models.DependencyEdge, error) {
	q := fsClient.Collection("horizon_dependency_edges").
		Where("tenantId", "==", tenantID).
		Where("targetFragmentId", "==", targetFragmentID).
		Limit(10000)

	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	var edges []*models.DependencyEdge
	for _, d := range docs {
		var edge models.DependencyEdge
		if err := d.DataTo(&edge); err != nil {
			continue
		}
		// Backward compatibility: some historical edges might not have stored "id" field.
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = d.Ref.ID
		}
		edges = append(edges, &edge)
	}
	return edges, nil
}

func migrateEdges(ctx context.Context, primaryID string, duplicateIDs []string, edgesRepo *repo.DependencyEdgesRepository, fsClient *firestore.Client, stats *DeduplicationStats) error {
	tenantID := "global"

	// Find all edges where duplicates are source or target
	// Use Firestore directly to avoid OrderBy requirement (which needs composite index)
	for _, dupID := range duplicateIDs {
		// Update edges where duplicate is source (without OrderBy)
		sourceEdges, err := listEdgesBySourceDirect(ctx, fsClient, tenantID, dupID)
		if err != nil {
			return fmt.Errorf("list source edges for %s: %w", dupID, err)
		}

		for _, edge := range sourceEdges {
			if strings.TrimSpace(edge.ID) == "" {
				log.Printf("WARNING: Skipping edge with empty id (dup=%s -> target=%s type=%s)", dupID, edge.TargetFragmentID, edge.Type)
				continue
			}
			if edge.Type == "" {
				log.Printf("WARNING: Skipping edge %s with empty type (dup=%s -> target=%s)", edge.ID, dupID, edge.TargetFragmentID)
				continue
			}

			// Check if edge already exists with primary as source
			existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, primaryID, edge.TargetFragmentID, edge.Type)
			if err == nil && existing != nil {
				// Merge metrics into the existing edge before deleting the duplicate edge.
				if mergeEdgeMetrics(existing, edge) {
					_ = edgesRepo.Update(ctx, existing) // best-effort
				}
				// Edge already exists, delete duplicate
				if err := edgesRepo.Delete(ctx, edge.ID); err != nil {
					log.Printf("WARNING: Failed to delete duplicate edge %s: %v", edge.ID, err)
				} else {
					stats.UpdatedEdges++
				}
			} else {
				// Update edge to use primary
				edge.SourceFragmentID = primaryID
				// If this becomes a self-loop (both endpoints collapse to primary), drop the edge.
				if edge.SourceFragmentID == edge.TargetFragmentID {
					if err := edgesRepo.Delete(ctx, edge.ID); err != nil {
						log.Printf("WARNING: Failed to delete self-loop edge %s (collapsed during dedupe): %v", edge.ID, err)
					} else {
						stats.UpdatedEdges++
					}
					continue
				}
				if err := edgesRepo.Update(ctx, edge); err != nil {
					log.Printf("WARNING: Failed to update edge %s: %v", edge.ID, err)
				} else {
					stats.UpdatedEdges++
				}
			}
		}

		// Update edges where duplicate is target (without OrderBy)
		targetEdges, err := listEdgesByTargetDirect(ctx, fsClient, tenantID, dupID)
		if err != nil {
			return fmt.Errorf("list target edges for %s: %w", dupID, err)
		}

		for _, edge := range targetEdges {
			if strings.TrimSpace(edge.ID) == "" {
				log.Printf("WARNING: Skipping edge with empty id (source=%s -> dup=%s type=%s)", edge.SourceFragmentID, dupID, edge.Type)
				continue
			}
			if edge.Type == "" {
				log.Printf("WARNING: Skipping edge %s with empty type (source=%s -> dup=%s)", edge.ID, edge.SourceFragmentID, dupID)
				continue
			}

			// Check if edge already exists with primary as target
			existing, err := edgesRepo.GetBySourceTargetAndType(ctx, tenantID, edge.SourceFragmentID, primaryID, edge.Type)
			if err == nil && existing != nil {
				// Merge metrics into the existing edge before deleting the duplicate edge.
				if mergeEdgeMetrics(existing, edge) {
					_ = edgesRepo.Update(ctx, existing) // best-effort
				}
				// Edge already exists, delete duplicate
				if err := edgesRepo.Delete(ctx, edge.ID); err != nil {
					log.Printf("WARNING: Failed to delete duplicate edge %s: %v", edge.ID, err)
				} else {
					stats.UpdatedEdges++
				}
			} else {
				// Update edge to use primary
				edge.TargetFragmentID = primaryID
				// If this becomes a self-loop (both endpoints collapse to primary), drop the edge.
				if edge.SourceFragmentID == edge.TargetFragmentID {
					if err := edgesRepo.Delete(ctx, edge.ID); err != nil {
						log.Printf("WARNING: Failed to delete self-loop edge %s (collapsed during dedupe): %v", edge.ID, err)
					} else {
						stats.UpdatedEdges++
					}
					continue
				}
				if err := edgesRepo.Update(ctx, edge); err != nil {
					log.Printf("WARNING: Failed to update edge %s: %v", edge.ID, err)
				} else {
					stats.UpdatedEdges++
				}
			}
		}
	}

	return nil
}

// mergeEdgeMetrics merges src edge metrics/qualifiers into dst, preferring richer values.
func mergeEdgeMetrics(dst, src *models.DependencyEdge) bool {
	if dst == nil || src == nil {
		return false
	}
	changed := false
	if len(src.Metrics) > 0 {
		if dst.Metrics == nil {
			dst.Metrics = map[string]models.MetricValue{}
		}
		for k, mv := range src.Metrics {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if cur, ok := dst.Metrics[key]; ok {
				if betterMetricValue(mv, cur) {
					dst.Metrics[key] = mv
					changed = true
				}
			} else {
				dst.Metrics[key] = mv
				changed = true
			}
		}
	}
	// Qualifiers: fill missing only (never overwrite).
	if len(src.Qualifiers) > 0 {
		if dst.Qualifiers == nil {
			dst.Qualifiers = map[string]interface{}{}
		}
		for k, v := range src.Qualifiers {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			if _, ok := dst.Qualifiers[key]; ok {
				continue
			}
			dst.Qualifiers[key] = v
			changed = true
		}
	}
	return changed
}

func migrateWatchdogs(ctx context.Context, primaryID string, duplicateIDs []string, watchdogRepo *repo.WatchdogConfigsRepository, stats *DeduplicationStats) error {
	tenantID := "global"

	for _, dupID := range duplicateIDs {
		config, err := watchdogRepo.GetByTargetFragmentID(ctx, tenantID, dupID)
		if err != nil {
			// No watchdog config for this fragment, skip
			continue
		}

		// Check if primary already has a watchdog config
		primaryConfig, err := watchdogRepo.GetByTargetFragmentID(ctx, tenantID, primaryID)
		if err == nil && primaryConfig != nil {
			// Primary already has config, delete duplicate
			if err := watchdogRepo.Delete(ctx, config.ID); err != nil {
				log.Printf("WARNING: Failed to delete duplicate watchdog %s: %v", config.ID, err)
			} else {
				stats.UpdatedWatchdogs++
			}
		} else {
			// Update config to point to primary
			config.TargetFragmentID = primaryID
			if err := watchdogRepo.Update(ctx, config); err != nil {
				log.Printf("WARNING: Failed to update watchdog %s: %v", config.ID, err)
			} else {
				stats.UpdatedWatchdogs++
			}
		}
	}

	return nil
}

func migrateLenses(ctx context.Context, primaryID string, duplicateIDs []string, canonicalPath string, lensesRepo *repo.LensesRepository, stats *DeduplicationStats) error {
	// We update only lenses that explicitly include this canonicalPath in scope.
	// For canonicalPath-level dedup this is sufficient and avoids full collection scans.
	lenses, err := lensesRepo.FindByScope(ctx, "global", canonicalPath, 10000)
	if err != nil {
		return err
	}
	if len(lenses) == 0 {
		return nil
	}
	dupSet := make(map[string]struct{}, len(duplicateIDs))
	for _, id := range duplicateIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			dupSet[id] = struct{}{}
		}
	}
	for _, lens := range lenses {
		if lens == nil || strings.TrimSpace(lens.ID) == "" {
			continue
		}
		changed := false

		// ScopeFragments: canonicalPath -> fragmentID cache
		if lens.ScopeFragments != nil {
			if fid := strings.TrimSpace(lens.ScopeFragments[canonicalPath]); fid != "" {
				if _, ok := dupSet[fid]; ok {
					lens.ScopeFragments[canonicalPath] = primaryID
					changed = true
				}
			}
		}

		// ResolvedQueries: ensure canonicalPath de-dup and keep stable.
		if lens.ResolvedQueries != nil {
			for q, paths := range lens.ResolvedQueries {
				if len(paths) == 0 {
					continue
				}
				seen := make(map[string]struct{}, len(paths))
				out := make([]string, 0, len(paths))
				localChanged := false
				for _, p := range paths {
					p = strings.TrimSpace(p)
					if p == "" {
						localChanged = true
						continue
					}
					if _, ok := seen[p]; ok {
						localChanged = true
						continue
					}
					seen[p] = struct{}{}
					out = append(out, p)
				}
				if localChanged {
					lens.ResolvedQueries[q] = out
					changed = true
				}
			}
		}

		if !changed {
			continue
		}
		if err := lensesRepo.Update(ctx, lens); err != nil {
			log.Printf("WARNING: Failed to update lens %s: %v", lens.ID, err)
			continue
		}
		stats.UpdatedLenses++
	}
	return nil
}

func migrateLensArtifacts(ctx context.Context, primaryID string, duplicateIDs []string, canonicalPath string, fsClient *firestore.Client, stats *DeduplicationStats) error {
	col := fsClient.Collection("horizon_lens_artifacts")
	dupSet := make(map[string]struct{}, len(duplicateIDs))
	for _, id := range duplicateIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			dupSet[id] = struct{}{}
		}
	}

	for _, dupID := range duplicateIDs {
		dupID = strings.TrimSpace(dupID)
		if dupID == "" {
			continue
		}

		// FRAGMENT artifacts
		docs, err := col.Where("type", "==", string(models.LensArtifactTypeFragment)).Where("fragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list fragment artifacts for %s: %w", dupID, err)
		}
		for _, d := range docs {
			var a models.LensArtifact
			if err := d.DataTo(&a); err != nil {
				continue
			}

			// De-dupe by (lensId, canonicalPath): if canonical artifact already exists with primaryID, drop duplicate doc.
			existing, err := col.Where("lensId", "==", strings.TrimSpace(a.LensID)).
				Where("type", "==", string(models.LensArtifactTypeFragment)).
				Where("canonicalPath", "==", canonicalPath).
				Limit(5).
				Documents(ctx).GetAll()
			if err == nil {
				shouldDelete := false
				for _, e := range existing {
					if e.Ref.ID == d.Ref.ID {
						continue
					}
					var ex models.LensArtifact
					if err := e.DataTo(&ex); err != nil {
						continue
					}
					if strings.TrimSpace(ex.FragmentID) == primaryID {
						shouldDelete = true
						break
					}
				}
				if shouldDelete {
					if _, err := d.Ref.Delete(ctx); err == nil {
						stats.UpdatedLensArtifacts++
					}
					continue
				}
			}

			updates := []firestore.Update{
				{Path: "fragmentId", Value: primaryID},
				{Path: "canonicalPath", Value: canonicalPath},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update lens artifact %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedLensArtifacts++
		}

		// EDGE artifacts by sourceFragId
		srcDocs, err := col.Where("type", "==", string(models.LensArtifactTypeEdge)).Where("sourceFragId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list edge artifacts by source for %s: %w", dupID, err)
		}
		for _, d := range srcDocs {
			var a models.LensArtifact
			if err := d.DataTo(&a); err != nil {
				continue
			}
			targetID := strings.TrimSpace(a.TargetFragID)
			if _, ok := dupSet[targetID]; ok {
				targetID = primaryID
			}
			if targetID == primaryID {
				// collapse into self-loop edge artifact -> delete
				if _, err := d.Ref.Delete(ctx); err == nil {
					stats.UpdatedLensArtifacts++
				}
				continue
			}
			updates := []firestore.Update{
				{Path: "sourceFragId", Value: primaryID},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update edge artifact(source) %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedLensArtifacts++
		}

		// EDGE artifacts by targetFragId
		tgtDocs, err := col.Where("type", "==", string(models.LensArtifactTypeEdge)).Where("targetFragId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list edge artifacts by target for %s: %w", dupID, err)
		}
		for _, d := range tgtDocs {
			var a models.LensArtifact
			if err := d.DataTo(&a); err != nil {
				continue
			}
			sourceID := strings.TrimSpace(a.SourceFragID)
			if _, ok := dupSet[sourceID]; ok {
				sourceID = primaryID
			}
			if sourceID == primaryID {
				if _, err := d.Ref.Delete(ctx); err == nil {
					stats.UpdatedLensArtifacts++
				}
				continue
			}
			updates := []firestore.Update{
				{Path: "targetFragId", Value: primaryID},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update edge artifact(target) %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedLensArtifacts++
		}
	}
	return nil
}

func migrateLensItems(ctx context.Context, primaryID string, duplicateIDs []string, fsClient *firestore.Client, stats *DeduplicationStats) error {
	col := fsClient.Collection("horizon_lens_items")
	dupSet := make(map[string]struct{}, len(duplicateIDs))
	for _, id := range duplicateIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			dupSet[id] = struct{}{}
		}
	}

	for _, dupID := range duplicateIDs {
		dupID = strings.TrimSpace(dupID)
		if dupID == "" {
			continue
		}

		// node items
		nodeDocs, err := col.Where("kind", "==", "node").Where("fragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list node lens items for %s: %w", dupID, err)
		}
		for _, d := range nodeDocs {
			updates := []firestore.Update{
				{Path: "fragmentId", Value: primaryID},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update lens item(node) %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedLensItems++
		}

		// edge items by source
		srcDocs, err := col.Where("kind", "==", "edge").Where("sourceFragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list edge lens items by source for %s: %w", dupID, err)
		}
		for _, d := range srcDocs {
			data := d.Data()
			targetID, _ := data["targetFragmentId"].(string)
			targetID = strings.TrimSpace(targetID)
			if _, ok := dupSet[targetID]; ok {
				targetID = primaryID
			}
			if targetID == primaryID {
				if _, err := d.Ref.Delete(ctx); err == nil {
					stats.UpdatedLensItems++
				}
				continue
			}
			updates := []firestore.Update{
				{Path: "sourceFragmentId", Value: primaryID},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update lens item(edge source) %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedLensItems++
		}

		// edge items by target
		tgtDocs, err := col.Where("kind", "==", "edge").Where("targetFragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list edge lens items by target for %s: %w", dupID, err)
		}
		for _, d := range tgtDocs {
			data := d.Data()
			sourceID, _ := data["sourceFragmentId"].(string)
			sourceID = strings.TrimSpace(sourceID)
			if _, ok := dupSet[sourceID]; ok {
				sourceID = primaryID
			}
			if sourceID == primaryID {
				if _, err := d.Ref.Delete(ctx); err == nil {
					stats.UpdatedLensItems++
				}
				continue
			}
			updates := []firestore.Update{
				{Path: "targetFragmentId", Value: primaryID},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update lens item(edge target) %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedLensItems++
		}
	}
	return nil
}

func migrateFragmentProfiles(ctx context.Context, primaryID string, duplicateIDs []string, fsClient *firestore.Client, stats *DeduplicationStats) error {
	col := fsClient.Collection("horizon_fragment_profiles")
	for _, dupID := range duplicateIDs {
		dupID = strings.TrimSpace(dupID)
		if dupID == "" {
			continue
		}
		docs, err := col.Where("fragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list fragment profiles for %s: %w", dupID, err)
		}
		for _, d := range docs {
			updates := []firestore.Update{
				{Path: "fragmentId", Value: primaryID},
				{Path: "updatedAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update fragment profile %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedProfiles++
		}
	}
	return nil
}

func migrateMissingInfoStatus(ctx context.Context, primaryID string, duplicateIDs []string, fsClient *firestore.Client, stats *DeduplicationStats) error {
	col := fsClient.Collection("horizon_missing_info_status")
	for _, dupID := range duplicateIDs {
		dupID = strings.TrimSpace(dupID)
		if dupID == "" {
			continue
		}
		docs, err := col.Where("fragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list missing info statuses for %s: %w", dupID, err)
		}
		for _, d := range docs {
			var old models.MissingInfoStatus
			if err := d.DataTo(&old); err != nil {
				continue
			}
			newID := repo.GenerateMissingInfoID(primaryID, old.QuestionText)
			if strings.TrimSpace(newID) == "" {
				continue
			}
			oldFragmentID := old.FragmentID
			old.FragmentID = primaryID
			old.ID = newID
			old.UpdatedAt = old.UpdatedAt.UTC()
			if old.CreatedAt.IsZero() {
				old.CreatedAt = time.Now().UTC()
			}

			targetRef := col.Doc(newID)
			targetSnap, err := targetRef.Get(ctx)
			if err == nil && targetSnap.Exists() {
				var cur models.MissingInfoStatus
				if err := targetSnap.DataTo(&cur); err == nil {
					merged := mergeMissingInfoStatus(cur, old)
					merged.ID = newID
					merged.FragmentID = primaryID
					if _, err := targetRef.Set(ctx, &merged); err != nil {
						log.Printf("WARNING: Failed to merge missing info status %s into %s: %v", d.Ref.ID, newID, err)
						continue
					}
					if d.Ref.ID != newID {
						_, _ = d.Ref.Delete(ctx)
					}
					stats.UpdatedMissingInfo++
					continue
				}
			}

			if _, err := targetRef.Set(ctx, &old); err != nil {
				log.Printf("WARNING: Failed to move missing info status %s -> %s: %v", d.Ref.ID, newID, err)
				continue
			}
			if d.Ref.ID != newID || oldFragmentID != primaryID {
				_, _ = d.Ref.Delete(ctx)
			}
			stats.UpdatedMissingInfo++
		}
	}
	return nil
}

func mergeMissingInfoStatus(a, b models.MissingInfoStatus) models.MissingInfoStatus {
	out := a
	if strings.TrimSpace(out.QuestionText) == "" && strings.TrimSpace(b.QuestionText) != "" {
		out.QuestionText = b.QuestionText
	}
	if strings.TrimSpace(out.QuestionHash) == "" && strings.TrimSpace(b.QuestionHash) != "" {
		out.QuestionHash = b.QuestionHash
	}
	if b.Attempts > out.Attempts {
		out.Attempts = b.Attempts
	}
	if out.CreatedAt.IsZero() || (!b.CreatedAt.IsZero() && b.CreatedAt.Before(out.CreatedAt)) {
		out.CreatedAt = b.CreatedAt
	}
	if b.UpdatedAt.After(out.UpdatedAt) {
		out.UpdatedAt = b.UpdatedAt
	}
	if b.LastAttemptAt != nil && (out.LastAttemptAt == nil || b.LastAttemptAt.After(*out.LastAttemptAt)) {
		t := *b.LastAttemptAt
		out.LastAttemptAt = &t
	}
	if b.NextAttemptAt != nil && (out.NextAttemptAt == nil || b.NextAttemptAt.After(*out.NextAttemptAt)) {
		t := *b.NextAttemptAt
		out.NextAttemptAt = &t
	}
	if b.ExhaustedAt != nil && (out.ExhaustedAt == nil || b.ExhaustedAt.After(*out.ExhaustedAt)) {
		t := *b.ExhaustedAt
		out.ExhaustedAt = &t
	}
	if b.ResolvedAt != nil && (out.ResolvedAt == nil || b.ResolvedAt.After(*out.ResolvedAt)) {
		t := *b.ResolvedAt
		out.ResolvedAt = &t
	}
	if strings.TrimSpace(out.SourceFingerprint) == "" && strings.TrimSpace(b.SourceFingerprint) != "" {
		out.SourceFingerprint = b.SourceFingerprint
	}
	out.Status = pickMissingInfoStatus(out.Status, b.Status)
	return out
}

func pickMissingInfoStatus(a, b string) string {
	rank := func(s string) int {
		switch strings.TrimSpace(strings.ToLower(s)) {
		case models.MissingInfoStatusResolved:
			return 3
		case models.MissingInfoStatusExhausted:
			return 2
		case models.MissingInfoStatusOpen:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func migrateCascadeEntityRegistry(ctx context.Context, primaryID string, duplicateIDs []string, fsClient *firestore.Client, stats *DeduplicationStats) error {
	col := fsClient.Collection("horizon_cascade_entities")
	for _, dupID := range duplicateIDs {
		dupID = strings.TrimSpace(dupID)
		if dupID == "" {
			continue
		}
		docs, err := col.Where("fragmentId", "==", dupID).Limit(10000).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("list cascade entities for %s: %w", dupID, err)
		}
		for _, d := range docs {
			updates := []firestore.Update{
				{Path: "fragmentId", Value: primaryID},
				{Path: "lastSeenAt", Value: firestore.ServerTimestamp},
			}
			if _, err := d.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: Failed to update cascade entity %s: %v", d.Ref.ID, err)
				continue
			}
			stats.UpdatedCascadeEntity++
		}
	}
	return nil
}

func deleteVersions(ctx context.Context, duplicateIDs []string, versionsRepo *repo.HorizonFragmentVersionsRepository, fsClient *firestore.Client, stats *DeduplicationStats) error {
	tenantID := "global"

	for _, dupID := range duplicateIDs {
		// List all versions for this fragment
		versions, err := versionsRepo.ListByFragment(ctx, tenantID, dupID, 10000, 0)
		if err != nil {
			log.Printf("WARNING: Failed to list versions for fragment %s: %v", dupID, err)
			continue
		}

		// Delete all versions using Firestore directly
		versionsCol := fsClient.Collection("horizon_fragment_versions")
		for _, version := range versions {
			_, err := versionsCol.Doc(version.VersionID).Delete(ctx)
			if err != nil {
				log.Printf("WARNING: Failed to delete version %s: %v", version.VersionID, err)
			} else {
				stats.DeletedVersions++
			}
		}
	}

	return nil
}
