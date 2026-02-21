// horizon_migrate_domain_as_config removes domain fragments and edges, updates lens scope.
// Part of Intent vs Domain Execution Contract refactoring (Phase 7).
//
// Run: go run ./cmd/tools/horizon_migrate_domain_as_config -project-id=YOUR_PROJECT [-apply] [-dry-run]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"google.golang.org/api/iterator"
)

func main() {
	var (
		projectID = flag.String("project-id", "", "GCP project ID (required)")
		apply     = flag.Bool("apply", false, "Apply changes (without -apply, runs dry-run only)")
		dryRun    = flag.Bool("dry-run", false, "Force dry-run even when -apply is set")
		batchSize = flag.Int("batch-size", 100, "Batch size for deletes")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("-project-id is required")
	}
	doApply := *apply && !*dryRun

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore client: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	stats := &migrateStats{}

	log.Printf("[migrate] mode=%s project=%s", map[bool]string{true: "APPLY", false: "DRY-RUN"}[doApply], *projectID)

	// 1) Find domain fragment IDs
	domainIDs, err := findDomainFragmentIDs(ctx, db, stats)
	if err != nil {
		log.Fatalf("find domain fragments: %v", err)
	}
	log.Printf("[migrate] domain fragments: %d", len(domainIDs))

	// 2) Delete edges involving domain fragments
	if err := deleteDomainEdges(ctx, db, domainIDs, doApply, *batchSize, stats); err != nil {
		log.Fatalf("delete domain edges: %v", err)
	}

	// 3) Delete domain fragments
	if err := deleteDomainFragments(ctx, db, domainIDs, doApply, *batchSize, stats); err != nil {
		log.Fatalf("delete domain fragments: %v", err)
	}

	// 4) Update lenses: remove /domain/ from scope and scopeFragments
	if err := updateLensScope(ctx, db, doApply, stats); err != nil {
		log.Fatalf("update lens scope: %v", err)
	}

	log.Printf("[migrate] done: fragments_deleted=%d edges_deleted=%d lenses_updated=%d",
		stats.FragmentsDeleted, stats.EdgesDeleted, stats.LensesUpdated)
}

type migrateStats struct {
	DomainFragmentsFound int
	FragmentsDeleted     int
	EdgesDeleted         int
	LensesScanned        int
	LensesUpdated        int
}

func findDomainFragmentIDs(ctx context.Context, db *firestore.Client, stats *migrateStats) (map[string]bool, error) {
	// Try type query first (requires index on type)
	ids, err := findDomainFragmentIDsByType(ctx, db, stats)
	if err == nil && len(ids) > 0 {
		return ids, nil
	}
	if err != nil {
		log.Printf("[migrate] type query failed (may need index): %v; falling back to full scan", err)
	}
	// Fallback: path range (requires index) or full scan
	ids, err = findDomainFragmentIDsByPath(ctx, db, stats)
	if err != nil {
		log.Printf("[migrate] path query failed: %v; falling back to full scan", err)
		return findDomainFragmentIDsFullScan(ctx, db, stats)
	}
	if len(ids) == 0 {
		return findDomainFragmentIDsFullScan(ctx, db, stats)
	}
	return ids, nil
}

func findDomainFragmentIDsByType(ctx context.Context, db *firestore.Client, stats *migrateStats) (map[string]bool, error) {
	col := db.Collection("horizon_fragments")
	it := col.Where("type", "==", "DOMAIN").Documents(ctx)
	domainIDs := make(map[string]bool)
	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		stats.DomainFragmentsFound++
		domainIDs[doc.Ref.ID] = true
	}
	return domainIDs, nil
}

func findDomainFragmentIDsByPath(ctx context.Context, db *firestore.Client, stats *migrateStats) (map[string]bool, error) {
	col := db.Collection("horizon_fragments")
	it := col.Where("canonicalPath", ">=", "/domain/").Where("canonicalPath", "<", "/domain0").Documents(ctx)
	domainIDs := make(map[string]bool)
	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			// No index for path range, fall back to full scan
			return findDomainFragmentIDsFullScan(ctx, db, stats)
		}
		stats.DomainFragmentsFound++
		domainIDs[doc.Ref.ID] = true
	}
	return domainIDs, nil
}

func findDomainFragmentIDsFullScan(ctx context.Context, db *firestore.Client, stats *migrateStats) (map[string]bool, error) {
	col := db.Collection("horizon_fragments")
	it := col.Documents(ctx)
	domainIDs := make(map[string]bool)
	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		data := doc.Data()
		cp, _ := data["canonicalPath"].(string)
		ft, _ := data["type"].(string)
		if strings.HasPrefix(strings.TrimSpace(cp), "/domain/") || strings.TrimSpace(ft) == "DOMAIN" {
			domainIDs[doc.Ref.ID] = true
			stats.DomainFragmentsFound++
		}
	}
	return domainIDs, nil
}

func deleteDomainEdges(ctx context.Context, db *firestore.Client, domainIDs map[string]bool, apply bool, batchSize int, stats *migrateStats) error {
	if len(domainIDs) == 0 {
		return nil
	}
	col := db.Collection("horizon_dependency_edges")
	it := col.Documents(ctx)
	var toDelete []*firestore.DocumentRef

	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		data := doc.Data()
		src, _ := data["sourceFragmentId"].(string)
		tgt, _ := data["targetFragmentId"].(string)
		if domainIDs[src] || domainIDs[tgt] {
			toDelete = append(toDelete, doc.Ref)
		}
	}

	for i := 0; i < len(toDelete); i += batchSize {
		end := i + batchSize
		if end > len(toDelete) {
			end = len(toDelete)
		}
		batch := toDelete[i:end]
		if apply {
			for _, ref := range batch {
				if _, err := ref.Delete(ctx); err != nil {
					return fmt.Errorf("delete edge %s: %w", ref.ID, err)
				}
				stats.EdgesDeleted++
			}
		} else {
			stats.EdgesDeleted += len(batch)
		}
		log.Printf("[migrate] edges: %d/%d", stats.EdgesDeleted, len(toDelete))
	}
	return nil
}

func deleteDomainFragments(ctx context.Context, db *firestore.Client, domainIDs map[string]bool, apply bool, batchSize int, stats *migrateStats) error {
	if len(domainIDs) == 0 {
		return nil
	}
	col := db.Collection("horizon_fragments")
	deleted := 0
	for id := range domainIDs {
		ref := col.Doc(id)
		if apply {
			if _, err := ref.Delete(ctx); err != nil {
				// Doc might already be gone
				log.Printf("[migrate] warn: delete fragment %s: %v", id, err)
				continue
			}
		}
		deleted++
		stats.FragmentsDeleted++
		if deleted%batchSize == 0 {
			log.Printf("[migrate] fragments: %d/%d", deleted, len(domainIDs))
		}
	}
	return nil
}

func updateLensScope(ctx context.Context, db *firestore.Client, apply bool, stats *migrateStats) error {
	col := db.Collection("horizon_lenses")
	it := col.Documents(ctx)

	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		stats.LensesScanned++

		data := doc.Data()
		scopeRaw, ok := data["scope"]
		if !ok {
			continue
		}
		scope, ok := scopeRaw.([]interface{})
		if !ok {
			continue
		}

		var newScope []string
		for _, v := range scope {
			s, ok := v.(string)
			if !ok {
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(s), "/domain/") {
				newScope = append(newScope, strings.TrimSpace(s))
			}
		}

		if len(newScope) == len(scope) {
			continue
		}

		updates := []firestore.Update{
			{Path: "scope", Value: newScope},
			{Path: "updatedAt", Value: firestore.ServerTimestamp},
		}

		// Remove /domain/ from scopeFragments
		if sfRaw, ok := data["scopeFragments"]; ok {
			if sfMap, ok := sfRaw.(map[string]interface{}); ok {
				newSF := make(map[string]interface{})
				for k, v := range sfMap {
					if !strings.HasPrefix(strings.TrimSpace(k), "/domain/") {
						newSF[k] = v
					}
				}
				if len(newSF) != len(sfMap) {
					updates = append(updates, firestore.Update{Path: "scopeFragments", Value: newSF})
				}
			}
		}

		if apply {
			if _, err := doc.Ref.Update(ctx, updates); err != nil {
				return fmt.Errorf("update lens %s: %w", doc.Ref.ID, err)
			}
		}
		stats.LensesUpdated++
		log.Printf("[migrate] lens %s: scope %d -> %d", doc.Ref.ID, len(scope), len(newScope))
	}
	return nil
}
