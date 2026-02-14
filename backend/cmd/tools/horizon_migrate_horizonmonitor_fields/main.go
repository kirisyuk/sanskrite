package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type migrateStats struct {
	DataSourcesScanned int
	DataSourcesUpdated int
	FragmentsScanned   int
	FragmentsUpdated   int
}

func main() {
	var (
		projectID = flag.String("project-id", "", "GCP project ID (required)")
		apply     = flag.Bool("apply", false, "Apply updates (default is dry-run)")
		limit     = flag.Int("limit", 0, "Optional max number of docs per collection")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("-project-id is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	stats := &migrateStats{}

	if err := migrateDataSources(ctx, db, *apply, *limit, stats); err != nil {
		log.Fatalf("migrate datasources: %v", err)
	}
	if err := migrateFragments(ctx, db, *apply, *limit, stats); err != nil {
		log.Fatalf("migrate fragments: %v", err)
	}

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLY"
	}
	log.Printf("[%s] finished: datasources scanned=%d updated=%d, fragments scanned=%d updated=%d",
		mode, stats.DataSourcesScanned, stats.DataSourcesUpdated, stats.FragmentsScanned, stats.FragmentsUpdated)
}

func migrateDataSources(ctx context.Context, db *firestore.Client, apply bool, limit int, stats *migrateStats) error {
	col := db.Collection("core_datasources")
	q := col.Query
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, doc := range docs {
		stats.DataSourcesScanned++
		updates := make([]firestore.Update, 0, 8)
		raw := doc.Data()

		kindRaw, _ := raw["kind"].(string)
		if models.NormalizeSourceKind(kindRaw) == models.SourceKindHorizonFeed && strings.TrimSpace(kindRaw) != models.SourceKindHorizonFeed {
			updates = append(updates, firestore.Update{Path: "kind", Value: models.SourceKindHorizonFeed})
		}

		sd, _ := raw["sourceDetails"].(map[string]interface{})
		for _, pair := range [][2]string{
			{"worldmonitorCategory", "horizonMonitorCategory"},
			{"worldmonitorFeedType", "horizonMonitorFeedType"},
			{"worldmonitorIngestMode", "horizonMonitorIngestMode"},
			{"worldmonitorSignalType", "horizonMonitorSignalType"},
			{"worldmonitorRequiresAuth", "horizonMonitorRequiresAuth"},
		} {
			oldKey, newKey := pair[0], pair[1]
			if _, hasNew := sd[newKey]; hasNew {
				continue
			}
			if v, hasOld := sd[oldKey]; hasOld {
				updates = append(updates, firestore.Update{
					Path:  "sourceDetails." + newKey,
					Value: v,
				})
			}
		}

		if len(updates) == 0 {
			continue
		}
		stats.DataSourcesUpdated++
		log.Printf("core_datasources/%s updates=%d", doc.Ref.ID, len(updates))
		if apply {
			if _, err := doc.Ref.Update(ctx, updates); err != nil {
				return fmt.Errorf("update datasource %s: %w", doc.Ref.ID, err)
			}
		}
	}
	return nil
}

func migrateFragments(ctx context.Context, db *firestore.Client, apply bool, limit int, stats *migrateStats) error {
	col := db.Collection("horizon_fragments")
	q := col.Query
	if limit > 0 {
		q = q.Limit(limit)
	}
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		return err
	}
	for _, doc := range docs {
		stats.FragmentsScanned++
		raw := doc.Data()
		if _, hasNew := raw["horizonMonitor"]; hasNew {
			continue
		}
		oldVal, hasOld := raw["worldMonitor"]
		if !hasOld {
			continue
		}

		stats.FragmentsUpdated++
		log.Printf("horizon_fragments/%s add horizonMonitor from worldMonitor", doc.Ref.ID)
		if apply {
			if _, err := doc.Ref.Update(ctx, []firestore.Update{
				{Path: "horizonMonitor", Value: oldVal},
			}); err != nil {
				return fmt.Errorf("update fragment %s: %w", doc.Ref.ID, err)
			}
		}
	}
	return nil
}
