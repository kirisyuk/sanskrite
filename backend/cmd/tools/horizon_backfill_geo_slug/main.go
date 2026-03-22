package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID       = flag.String("project", "", "GCP project ID (required)")
	tenantID        = flag.String("tenant", "global", "Tenant ID (default: global)")
	pageSize        = flag.Int("page-size", 500, "Page size")
	limit           = flag.Int("limit", 0, "Max matching documents to update (0 = unlimited)")
	dryRun          = flag.Bool("dry-run", true, "Dry run mode")
	overwrite       = flag.Bool("overwrite", false, "Overwrite existing geoSlug even if present")
	touchTimestamps = flag.Bool("touch-timestamps", false, "Also update updatedAt and metadata.lastUpdatedAt")
)

type stats struct {
	Scanned                int
	Candidates             int
	WouldWrite             int
	Wrote                  int
	Skipped                int
	SkippedTenantMismatch  int
	SkippedNoDerivedGeo    int
	SkippedExistingGeo     int
	SkippedAlreadyUpToDate int
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *pageSize <= 0 {
		*pageSize = 500
	}

	ctx := context.Background()
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore client: %v", err)
	}
	defer db.Close()

	log.Printf("=== Horizon: geoSlug backfill ===")
	log.Printf("project=%s tenant=%s dryRun=%v overwrite=%v pageSize=%d limit=%d touchTimestamps=%v",
		strings.TrimSpace(*projectID),
		strings.TrimSpace(*tenantID),
		*dryRun,
		*overwrite,
		*pageSize,
		*limit,
		*touchTimestamps,
	)

	var st stats
	lastDocID := ""

	for {
		if *limit > 0 && st.Candidates >= *limit {
			break
		}
		q := db.Collection("horizon_fragments").
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}

		iter := q.Documents(ctx)
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				log.Fatalf("page iteration failed after scanned=%d: %v", st.Scanned, err)
			}
			pageDocs = append(pageDocs, doc)
		}
		iter.Stop()
		if len(pageDocs) == 0 {
			break
		}

		for _, doc := range pageDocs {
			if doc == nil || !doc.Exists() {
				continue
			}
			var fragment horizonmodels.HorizonFragment
			if err := doc.DataTo(&fragment); err != nil {
				log.Printf("WARNING: failed to decode fragment %s: %v", doc.Ref.ID, err)
				continue
			}
			if strings.TrimSpace(fragment.ID) == "" {
				fragment.ID = doc.Ref.ID
			}
			st.Scanned++
			lastDocID = doc.Ref.ID
			if strings.TrimSpace(fragment.TenantID) != strings.TrimSpace(*tenantID) {
				st.Skipped++
				st.SkippedTenantMismatch++
				continue
			}

			nextGeo := horizonmodels.DeriveGeoSlug(fragment.CanonicalPath, fragment.HomeLocation)
			currentGeo := strings.ToLower(strings.TrimSpace(fragment.GeoSlug))
			if nextGeo == "" {
				st.Skipped++
				st.SkippedNoDerivedGeo++
				continue
			}
			if !*overwrite && currentGeo != "" {
				st.Skipped++
				st.SkippedExistingGeo++
				continue
			}
			if currentGeo == nextGeo {
				st.Skipped++
				st.SkippedAlreadyUpToDate++
				continue
			}

			st.Candidates++
			st.WouldWrite++
			if *limit > 0 && st.Candidates > *limit {
				break
			}
			if *dryRun {
				continue
			}

			updates := []firestore.Update{
				{Path: "geoSlug", Value: nextGeo},
			}
			if *touchTimestamps {
				now := time.Now().UTC()
				updates = append(updates,
					firestore.Update{Path: "updatedAt", Value: now},
					firestore.Update{Path: "metadata.lastUpdatedAt", Value: now},
				)
			}
			if _, err := doc.Ref.Update(ctx, updates); err != nil {
				log.Printf("WARNING: update failed id=%s canonicalPath=%s err=%v", fragment.ID, strings.TrimSpace(fragment.CanonicalPath), err)
				continue
			}
			st.Wrote++
			if *limit > 0 && st.Wrote >= *limit {
				break
			}
		}
		if *limit > 0 && st.Wrote >= *limit {
			break
		}
		if *limit > 0 && st.Candidates >= *limit {
			break
		}
	}

	log.Printf("done: scanned=%d candidates=%d wouldWrite=%d wrote=%d skipped=%d tenantMismatch=%d noDerivedGeo=%d existingGeo=%d alreadyUpToDate=%d",
		st.Scanned,
		st.Candidates,
		st.WouldWrite,
		st.Wrote,
		st.Skipped,
		st.SkippedTenantMismatch,
		st.SkippedNoDerivedGeo,
		st.SkippedExistingGeo,
		st.SkippedAlreadyUpToDate,
	)
}
