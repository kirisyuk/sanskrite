package main

import (
	"context"
	"flag"
	"log"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")
	dryRun    = flag.Bool("dry-run", true, "Dry run mode: only report orphaned versions")
)

func main() {
	flag.Parse()

	if *projectID == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()

	// Initialize Firestore
	fsClient, err := firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	// Initialize repositories
	fragmentsRepo := repo.NewHorizonFragmentsRepository(fsClient)

	log.Printf("Scanning for orphaned fragment versions (dry-run=%v, tenant=%s)...", *dryRun, *tenantID)

	// Step 1: Get all existing fragment IDs
	log.Println("Loading all fragment IDs...")
	fragmentIDs := make(map[string]bool)
	q := fragmentsRepo.Query().Where("tenantId", "==", *tenantID)
	iter := q.Documents(ctx)
	defer iter.Stop()

	count := 0
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Failed to iterate fragments: %v", err)
		}
		fragmentIDs[doc.Ref.ID] = true
		count++
		if count%1000 == 0 {
			log.Printf("  Loaded %d fragment IDs...", count)
		}
	}
	log.Printf("Found %d existing fragments", len(fragmentIDs))

	// Step 2: Find orphaned versions
	log.Println("Scanning fragment versions...")
	versionsCol := fsClient.Collection("horizon_fragment_versions")
	versionsIter := versionsCol.Where("tenantId", "==", *tenantID).Documents(ctx)
	defer versionsIter.Stop()

	var orphanedVersions []string
	var totalVersions int
	var checkedVersions int

	for {
		doc, err := versionsIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Failed to iterate versions: %v", err)
		}

		totalVersions++
		if totalVersions%1000 == 0 {
			log.Printf("  Scanned %d versions, found %d orphaned...", totalVersions, len(orphanedVersions))
		}

		// Get fragmentId from version document
		data := doc.Data()
		fragmentID, ok := data["fragmentId"].(string)
		if !ok || fragmentID == "" {
			continue
		}

		checkedVersions++

		// Check if fragment still exists
		if !fragmentIDs[fragmentID] {
			orphanedVersions = append(orphanedVersions, doc.Ref.ID)
			if *dryRun && len(orphanedVersions) <= 10 {
				log.Printf("  Found orphaned version: %s (fragmentId: %s)", doc.Ref.ID, fragmentID)
			}
		}
	}

	log.Printf("\n=== Summary ===")
	log.Printf("Total versions scanned: %d", totalVersions)
	log.Printf("Versions checked (with fragmentId): %d", checkedVersions)
	log.Printf("Orphaned versions found: %d", len(orphanedVersions))

	if len(orphanedVersions) == 0 {
		log.Println("No orphaned versions found!")
		return
	}

	if *dryRun {
		log.Printf("\n[DRY-RUN] Would delete %d orphaned versions", len(orphanedVersions))
		log.Println("Run with --dry-run=false to delete them")
	} else {
		log.Printf("\nDeleting %d orphaned versions...", len(orphanedVersions))
		deleted := 0
		failed := 0

		for i, versionID := range orphanedVersions {
			_, err := versionsCol.Doc(versionID).Delete(ctx)
			if err != nil {
				log.Printf("ERROR: Failed to delete version %s: %v", versionID, err)
				failed++
			} else {
				deleted++
				if (i+1)%100 == 0 {
					log.Printf("  Deleted %d/%d versions...", deleted, len(orphanedVersions))
				}
			}
		}

		log.Printf("\n=== Deletion Summary ===")
		log.Printf("Deleted: %d", deleted)
		log.Printf("Failed: %d", failed)
	}
}

