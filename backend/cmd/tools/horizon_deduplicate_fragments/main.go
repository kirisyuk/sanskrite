package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	dryRun    = flag.Bool("dry-run", true, "Dry run mode: only report duplicates without making changes")
	projectID = flag.String("project", "", "GCP project ID (required)")
	bucket    = flag.String("bucket", "", "GCS bucket for fragment export (optional, for re-export)")
	tenantID  = flag.String("tenant", "global", "Tenant ID to process (default: global)")
	limit     = flag.Int("limit", 0, "Limit number of canonical paths to process (0 = all)")
	verbose   = flag.Bool("verbose", false, "Verbose logging")
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

	// Initialize GCS client (required for deletion from GCS)
	gcsClient, err := gcp.NewGCSClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}

	// Determine bucket (use provided or default)
	bucketName := *bucket
	if bucketName == "" {
		bucketName = os.Getenv("HORIZON_FRAGMENTS_BUCKET")
	}
	if bucketName == "" {
		bucketName = fmt.Sprintf("%s-horizon-fragments", *projectID)
		log.Printf("INFO: Using default bucket: %s", bucketName)
	}

	// Initialize repositories with GCS storage (required for deletion)
	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient).WithStorage(gcsClient, bucketName)
	edgesRepo := repo.NewDependencyEdgesRepository(fsClient)
	watchdogRepo := repo.NewWatchdogConfigsRepository(fsClient)
	lensesRepo := repo.NewLensesRepository(fsClient)
	versionsRepo := repo.NewHorizonFragmentVersionsRepository(fsClient)

	log.Printf("Starting deduplication scan (dry-run=%v, tenant=%s)...", *dryRun, *tenantID)

	// Step 1: Find all duplicates by canonicalPath
	duplicates, err := findDuplicates(ctx, fragmentsRepo, *tenantID)
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
			// Merge fragment data (especially metrics) from duplicates into primary BEFORE deleting.
			// Rationale: canonicalPath-level duplicates can be created by race conditions; the "losing" fragment
			// may still contain richer State metrics or identifiers. We want the richest data to survive.
			if err := mergeDuplicateFragmentData(ctx, fragmentsRepo, versionsRepo, primaryID, duplicateIDs); err != nil {
				log.Printf("WARNING: Failed to merge fragment data into primary %s for %s: %v", primaryID, canonicalPath, err)
				// Continue: reference migration + deletion is still useful, but may lose some data.
			}

			// Migrate all references from duplicates to primary
			if err := migrateReferences(ctx, primaryID, duplicateIDs, edgesRepo, watchdogRepo, lensesRepo, versionsRepo, fsClient, canonicalPath); err != nil {
				log.Printf("ERROR: Failed to migrate references for %s: %v", canonicalPath, err)
				continue
			}

			// Delete duplicate fragments
			for _, dupID := range duplicateIDs {
				if err := fragmentsRepo.Delete(ctx, dupID); err != nil {
					log.Printf("ERROR: Failed to delete fragment %s: %v", dupID, err)
					continue
				}
				stats.DeletedFragments++
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
	MergedGroups     int
	DeletedFragments int
	UpdatedEdges     int
	UpdatedWatchdogs int
	UpdatedLenses    int
	DeletedVersions  int
	ReExported       int
	DryRunDuplicates int
}

// findDuplicates finds all canonical paths that have multiple fragments
func findDuplicates(ctx context.Context, fragmentsRepo *repo.HorizonFragmentsRepository, tenantID string) (map[string][]string, error) {
	duplicates := make(map[string][]string)

	// Query all fragments for the tenant
	// fragmentsRepo.Query() already scopes to tenantId=="global" for Horizon.
	// Keep tenantID argument for API compatibility, but don't add redundant/contradictory filters.
	q := fragmentsRepo.Query()
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

		pathToIDs[canonicalPath] = append(pathToIDs[canonicalPath], fragment.ID)
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
) error {
	stats := &DeduplicationStats{}

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

	// 4. Delete fragment versions for duplicates (they're tied to specific fragment IDs)
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
	if src.Metrics != nil && len(src.Metrics) > 0 {
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
	if src.Qualifiers != nil && len(src.Qualifiers) > 0 {
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
	// Note: Lenses have scopeFragments map (canonicalPath -> fragmentID)
	// We need to update this map for all lenses that reference duplicate fragment IDs

	// Since we can't easily query by map values, we'll need to iterate all lenses
	// For efficiency, we'll use Firestore directly to iterate and update

	// This is a simplified approach - in production you might want to:
	// 1. Query lenses by project/tenant if you know them
	// 2. Use a more efficient indexing strategy

	// For now, we'll use a Firestore query to get all lenses and check their scopeFragments
	// This requires direct Firestore access, which we'll get from the repository's collection

	log.Printf("NOTE: Lens scopeFragments migration requires iterating all lenses - skipping for now")
	log.Printf("      Manual update needed: update scopeFragments['%s'] from duplicate IDs to %s", canonicalPath, primaryID)

	// TODO: Implement full lens migration if needed
	// Would require: 1) Get Firestore client, 2) Query all lenses, 3) Check scopeFragments map, 4) Update

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
