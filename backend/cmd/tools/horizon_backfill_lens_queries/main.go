package main

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID = flag.String("project", "", "GCP project id")
	tenantID  = flag.String("tenant", "", "Optional tenant filter")
	lensID    = flag.String("lens", "", "Optional single lens id")
	limit     = flag.Int("limit", 0, "Max lenses to update")
	pageSize  = flag.Int("page-size", 200, "Firestore page size")
	fullScan  = flag.Bool("full-scan", false, "Scan every lens instead of only legacy query-backed lenses")
	apply     = flag.Bool("apply", false, "Apply changes")
)

type stats struct {
	scanned            int
	candidates         int
	updated            int
	backfilledOriginal int
	backfilledDomains  int
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore: %v", err)
	}
	defer client.Close()

	log.Printf("lens query backfill: project=%s tenant=%s lens=%s fullScan=%v apply=%v pageSize=%d limit=%d", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), strings.TrimSpace(*lensID), *fullScan, *apply, *pageSize, *limit)

	st := run(ctx, client.Client())
	log.Printf("done scanned=%d candidates=%d updated=%d originalQuery=%d domainQueries=%d", st.scanned, st.candidates, st.updated, st.backfilledOriginal, st.backfilledDomains)
}

func run(ctx context.Context, db *firestore.Client) stats {
	st := stats{}
	if db == nil {
		return st
	}
	col := db.Collection("horizon_lenses")
	if lens := strings.TrimSpace(*lensID); lens != "" {
		processDoc(ctx, col.Doc(lens), &st)
		return st
	}

	var q firestore.Query
	if *fullScan {
		q = col.OrderBy("updatedAt", firestore.Desc)
	} else {
		q = col.Where("viewConfig.originalQuery", ">", "")
	}
	if tenant := strings.TrimSpace(*tenantID); tenant != "" {
		q = q.Where("tenantId", "==", tenant)
	}
	iter := q.Documents(ctx)
	defer iter.Stop()

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("scan: %v", err)
		}
		processSnapshot(ctx, doc, &st)
		if *limit > 0 && st.candidates >= *limit {
			return st
		}
	}
	return st
}

func processDoc(ctx context.Context, ref *firestore.DocumentRef, st *stats) {
	if ref == nil {
		return
	}
	doc, err := ref.Get(ctx)
	if err != nil {
		log.Printf("WARN load lens=%s err=%v", ref.ID, err)
		return
	}
	processSnapshot(ctx, doc, st)
}

func processSnapshot(ctx context.Context, doc *firestore.DocumentSnapshot, st *stats) {
	if doc == nil || !doc.Exists() {
		return
	}
	st.scanned++
	var lens horizonmodels.Lens
	if err := doc.DataTo(&lens); err != nil {
		log.Printf("WARN decode lens=%s err=%v", doc.Ref.ID, err)
		return
	}

	legacyOriginal := ""
	if lens.ViewConfig != nil {
		if raw, ok := lens.ViewConfig["originalQuery"].(string); ok {
			legacyOriginal = strings.TrimSpace(raw)
		}
	}
	legacyDomains := []horizonmodels.LensDomainQuery(nil)
	if lens.ViewConfig != nil {
		legacyDomains = horizonmodels.ParseLensDomainQueries(lens.ViewConfig["domains"])
	}

	updates := map[string]interface{}{}
	changed := false
	if strings.TrimSpace(lens.OriginalQuery) == "" && legacyOriginal != "" {
		updates["originalQuery"] = legacyOriginal
		st.backfilledOriginal++
		changed = true
	}
	if len(lens.DomainQueries) == 0 && len(legacyDomains) > 0 {
		updates["domainQueries"] = horizonmodels.LensDomainQueriesToMaps(legacyDomains)
		st.backfilledDomains++
		changed = true
	}
	if !changed {
		return
	}
	st.candidates++
	log.Printf("candidate lens=%s tenant=%s name=%q original=%v domains=%d", doc.Ref.ID, lens.TenantID, lens.Name, updates["originalQuery"] != nil, len(legacyDomains))
	if !*apply {
		return
	}
	updates["updatedAt"] = time.Now().UTC()
	if _, err := doc.Ref.Set(ctx, updates, firestore.MergeAll); err != nil {
		log.Printf("WARN update lens=%s err=%v", doc.Ref.ID, err)
		return
	}
	st.updated++
}
