package main

import (
	"context"
	"flag"
	"log"
	"os"
	"reflect"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type snapshot struct {
	nodeRefs         []string
	geoRefs          []string
	flowRefs         []string
	zoneRefs         []string
	affectedZoneRefs []string
	anchorSupplier   string
	anchorCustomer   string
	productAxis      string
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	pageSize := flag.Int("page-size", 200, "page size")
	limit := flag.Int("limit", 0, "max docs to process (0 = all)")
	startAfter := flag.String("start-after", "", "resume after document id")
	apply := flag.Bool("apply", false, "apply writes (default dry-run)")
	sleepPerPageMs := flag.Int("sleep-per-page-ms", 100, "sleep between pages")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fs.Close()
	db := fs.Client()
	repo := hrepo.NewMaterializedArtifactsRepository(db)

	scanned := 0
	changed := 0
	applied := 0
	errors := 0
	lastDocID := strings.TrimSpace(*startAfter)
	sleepPerPage := time.Duration(*sleepPerPageMs) * time.Millisecond

	for {
		q := db.Collection("horizon_materialized_artifacts").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("kind", "==", string(hmodels.MaterializedKindSupplyChainPath)).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("page fetch failed: %v", err)
		}
		if len(docs) == 0 {
			break
		}
		lastDocID = docs[len(docs)-1].Ref.ID
		for _, doc := range docs {
			if *limit > 0 && scanned >= *limit {
				break
			}
			var artifact hmodels.MaterializedArtifact
			if err := doc.DataTo(&artifact); err != nil {
				errors++
				continue
			}
			if artifact.ID == "" {
				artifact.ID = doc.Ref.ID
			}
			scanned++
			before := snap(&artifact)
			hmodels.PopulateSupplyChainPathIndexes(&artifact)
			after := snap(&artifact)
			if reflect.DeepEqual(before, after) {
				continue
			}
			changed++
			if *apply {
				if err := repo.Update(ctx, &artifact); err != nil {
					errors++
					log.Printf("update failed: id=%s key=%s err=%v", artifact.ID, artifact.CanonicalKey, err)
					continue
				}
				applied++
			}
		}
		if *limit > 0 && scanned >= *limit {
			break
		}
		if sleepPerPage > 0 {
			time.Sleep(sleepPerPage)
		}
	}

	log.Printf("supply_chain_path_index_backfill summary: project=%s tenant=%s scanned=%d changed=%d applied=%d errors=%d lastDoc=%s apply=%v", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), scanned, changed, applied, errors, lastDocID, *apply)
}

func snap(artifact *hmodels.MaterializedArtifact) snapshot {
	if artifact == nil {
		return snapshot{}
	}
	return snapshot{
		nodeRefs:         append([]string(nil), artifact.NodeRefs...),
		geoRefs:          append([]string(nil), artifact.GeoRefs...),
		flowRefs:         append([]string(nil), artifact.FlowRefs...),
		zoneRefs:         append([]string(nil), artifact.ZoneRefs...),
		affectedZoneRefs: append([]string(nil), artifact.AffectedZoneRefs...),
		anchorSupplier:   strings.TrimSpace(artifact.AnchorSupplier),
		anchorCustomer:   strings.TrimSpace(artifact.AnchorCustomer),
		productAxis:      strings.TrimSpace(artifact.ProductAxis),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
