package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/graphenrich"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type counters map[string]int

type stats struct {
	fragmentsScanned int
	fragmentsChanged int
	fragmentsApplied int
	edgesScanned     int
	edgesChanged     int
	edgesApplied     int
	errors           int
	fragmentFields   counters
	edgeFields       counters
}

type fragmentSnapshot struct {
	geoFeatureType string
	govType        string
	marketDomain   string
	marketVertical string
	industryPaths  []string
	marketSegments []string
	tags           []string
	indexPaths     []string
}

type edgeSnapshot struct {
	qualifiers map[string]interface{}
	metrics    map[string]hmodels.MetricValue
}

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	mode := flag.String("mode", "all", "all|fragments|edges")
	pageSize := flag.Int("page-size", 200, "page size")
	limit := flag.Int("limit", 0, "max docs to process per collection (0 = all)")
	startAfter := flag.String("start-after", "", "resume after document id")
	pathPrefix := flag.String("path-prefix", "", "optional canonical path prefix filter (fragments or edge endpoints)")
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

	frRepo := hrepo.NewHorizonFragmentsRepository(db)
	edgesRepo := hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(frRepo)
	st := &stats{fragmentFields: counters{}, edgeFields: counters{}}
	prefix := strings.TrimSpace(*pathPrefix)

	log.Printf("graph heuristics backfill: project=%s tenant=%s mode=%s apply=%v pageSize=%d prefix=%q", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), strings.TrimSpace(*mode), *apply, *pageSize, prefix)

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "all":
		processFragments(ctx, db, frRepo, strings.TrimSpace(*tenantID), prefix, strings.TrimSpace(*startAfter), *pageSize, *limit, *apply, time.Duration(*sleepPerPageMs)*time.Millisecond, st)
		processEdges(ctx, db, frRepo, edgesRepo, strings.TrimSpace(*tenantID), prefix, strings.TrimSpace(*startAfter), *pageSize, *limit, *apply, time.Duration(*sleepPerPageMs)*time.Millisecond, st)
	case "fragments":
		processFragments(ctx, db, frRepo, strings.TrimSpace(*tenantID), prefix, strings.TrimSpace(*startAfter), *pageSize, *limit, *apply, time.Duration(*sleepPerPageMs)*time.Millisecond, st)
	case "edges":
		processEdges(ctx, db, frRepo, edgesRepo, strings.TrimSpace(*tenantID), prefix, strings.TrimSpace(*startAfter), *pageSize, *limit, *apply, time.Duration(*sleepPerPageMs)*time.Millisecond, st)
	default:
		log.Fatalf("unsupported --mode=%q", *mode)
	}

	printStats(st, *apply)
}

func processFragments(ctx context.Context, db *firestore.Client, frRepo *hrepo.HorizonFragmentsRepository, tenantID, pathPrefix, startAfter string, pageSize, limit int, apply bool, sleepPerPage time.Duration, st *stats) {
	if pageSize <= 0 {
		pageSize = 200
	}
	if tenantID == "" {
		tenantID = "global"
	}
	lastDocID := strings.TrimSpace(startAfter)
	for {
		q := db.Collection("horizon_fragments").Where("tenantId", "==", tenantID).OrderBy(firestore.DocumentID, firestore.Asc).Limit(pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			st.errors++
			log.Printf("fragment page fetch error: %v", err)
			return
		}
		if len(docs) == 0 {
			return
		}
		lastDocID = docs[len(docs)-1].Ref.ID
		for _, doc := range docs {
			if limit > 0 && st.fragmentsScanned >= limit {
				return
			}
			var frag hmodels.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				st.errors++
				continue
			}
			if frag.ID == "" {
				frag.ID = doc.Ref.ID
			}
			if pathPrefix != "" && !strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), pathPrefix) {
				continue
			}
			st.fragmentsScanned++
			before := snapshotFragment(&frag)
			graphenrich.ApplyFragmentHeuristics(&frag)
			frag.IndexPaths = hmodels.BuildIndexPaths(&frag, nil)
			after := snapshotFragment(&frag)
			changes := diffFragmentSnapshot(before, after)
			if len(changes) == 0 {
				continue
			}
			st.fragmentsChanged++
			for _, key := range changes {
				st.fragmentFields[key]++
			}
			if apply {
				if err := frRepo.UpdateDerived(ctx, &frag); err != nil {
					st.errors++
					log.Printf("fragment update failed: id=%s path=%s err=%v", frag.ID, frag.CanonicalPath, err)
					continue
				}
				st.fragmentsApplied++
			}
		}
		if sleepPerPage > 0 {
			time.Sleep(sleepPerPage)
		}
	}
}

func processEdges(ctx context.Context, db *firestore.Client, frRepo *hrepo.HorizonFragmentsRepository, edgesRepo *hrepo.DependencyEdgesRepository, tenantID, pathPrefix, startAfter string, pageSize, limit int, apply bool, sleepPerPage time.Duration, st *stats) {
	if pageSize <= 0 {
		pageSize = 200
	}
	if tenantID == "" {
		tenantID = "global"
	}
	lastDocID := strings.TrimSpace(startAfter)
	for {
		q := db.Collection("horizon_dependency_edges").Where("tenantId", "==", tenantID).OrderBy(firestore.DocumentID, firestore.Asc).Limit(pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		iter := q.Documents(ctx)
		docs := make([]*firestore.DocumentSnapshot, 0, pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				iter.Stop()
				st.errors++
				log.Printf("edge page fetch error: %v", err)
				return
			}
			docs = append(docs, doc)
		}
		iter.Stop()
		if len(docs) == 0 {
			return
		}
		lastDocID = docs[len(docs)-1].Ref.ID

		edges := make([]*hmodels.DependencyEdge, 0, len(docs))
		idSet := map[string]struct{}{}
		for _, doc := range docs {
			if limit > 0 && st.edgesScanned+len(edges) >= limit {
				break
			}
			var edge hmodels.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				st.errors++
				continue
			}
			if edge.ID == "" {
				edge.ID = doc.Ref.ID
			}
			edges = append(edges, &edge)
			if strings.TrimSpace(edge.SourceFragmentID) != "" {
				idSet[strings.TrimSpace(edge.SourceFragmentID)] = struct{}{}
			}
			if strings.TrimSpace(edge.TargetFragmentID) != "" {
				idSet[strings.TrimSpace(edge.TargetFragmentID)] = struct{}{}
			}
		}

		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		fragsByID, err := frRepo.GetMany(ctx, ids)
		if err != nil {
			st.errors++
			log.Printf("edge endpoint lookup failed: %v", err)
			return
		}

		for _, edge := range edges {
			if edge == nil {
				continue
			}
			if limit > 0 && st.edgesScanned >= limit {
				return
			}
			sourcePath := canonicalPathByID(fragsByID, edge.SourceFragmentID)
			targetPath := canonicalPathByID(fragsByID, edge.TargetFragmentID)
			if pathPrefix != "" && !strings.HasPrefix(sourcePath, pathPrefix) && !strings.HasPrefix(targetPath, pathPrefix) {
				continue
			}
			st.edgesScanned++
			before := snapshotEdge(edge)
			graphenrich.ApplyEdgeHeuristics(edge, sourcePath, targetPath)
			after := snapshotEdge(edge)
			changes := diffEdgeSnapshot(before, after)
			if len(changes) == 0 {
				continue
			}
			st.edgesChanged++
			for _, key := range changes {
				st.edgeFields[key]++
			}
			if apply {
				if err := edgesRepo.UpdateDerived(ctx, edge); err != nil {
					st.errors++
					log.Printf("edge update failed: id=%s type=%s err=%v", edge.ID, edge.Type, err)
					continue
				}
				st.edgesApplied++
			}
		}
		if sleepPerPage > 0 {
			time.Sleep(sleepPerPage)
		}
	}
}

func snapshotFragment(f *hmodels.HorizonFragment) fragmentSnapshot {
	return fragmentSnapshot{
		geoFeatureType: strings.TrimSpace(f.GeoFeatureType),
		govType:        strings.TrimSpace(f.GovType),
		marketDomain:   strings.TrimSpace(f.MarketDomain),
		marketVertical: strings.TrimSpace(f.MarketVertical),
		industryPaths:  append([]string(nil), f.IndustryPaths...),
		marketSegments: append([]string(nil), f.MarketSegments...),
		tags:           append([]string(nil), f.Tags...),
		indexPaths:     append([]string(nil), f.IndexPaths...),
	}
}

func diffFragmentSnapshot(before, after fragmentSnapshot) []string {
	changed := make([]string, 0, 8)
	if before.geoFeatureType != after.geoFeatureType {
		changed = append(changed, "geo_feature_type")
	}
	if before.govType != after.govType {
		changed = append(changed, "gov_type")
	}
	if before.marketDomain != after.marketDomain {
		changed = append(changed, "market_domain")
	}
	if before.marketVertical != after.marketVertical {
		changed = append(changed, "market_vertical")
	}
	if !reflect.DeepEqual(before.industryPaths, after.industryPaths) {
		changed = append(changed, "industry_paths")
	}
	if !reflect.DeepEqual(before.marketSegments, after.marketSegments) {
		changed = append(changed, "market_segments")
	}
	if !reflect.DeepEqual(before.tags, after.tags) {
		changed = append(changed, "tags")
	}
	if !reflect.DeepEqual(before.indexPaths, after.indexPaths) {
		changed = append(changed, "index_paths")
	}
	return changed
}

func snapshotEdge(edge *hmodels.DependencyEdge) edgeSnapshot {
	qualifiers := map[string]interface{}{}
	for k, v := range edge.Qualifiers {
		qualifiers[k] = v
	}
	metrics := map[string]hmodels.MetricValue{}
	for k, v := range edge.Metrics {
		metrics[k] = v
	}
	return edgeSnapshot{qualifiers: qualifiers, metrics: metrics}
}

func diffEdgeSnapshot(before, after edgeSnapshot) []string {
	changed := make([]string, 0, 2)
	if !reflect.DeepEqual(before.qualifiers, after.qualifiers) {
		changed = append(changed, "qualifiers")
	}
	if !reflect.DeepEqual(before.metrics, after.metrics) {
		changed = append(changed, "metrics")
	}
	return changed
}

func canonicalPathByID(frags map[string]*hmodels.HorizonFragment, id string) string {
	if frag := frags[strings.TrimSpace(id)]; frag != nil {
		return strings.TrimSpace(frag.CanonicalPath)
	}
	return ""
}

func printStats(st *stats, apply bool) {
	fmt.Printf("mode_summary apply=%v\n", apply)
	fmt.Printf("  fragments_scanned=%d changed=%d applied=%d\n", st.fragmentsScanned, st.fragmentsChanged, st.fragmentsApplied)
	fmt.Printf("  edges_scanned=%d changed=%d applied=%d\n", st.edgesScanned, st.edgesChanged, st.edgesApplied)
	fmt.Printf("  errors=%d\n", st.errors)
	printCounterBlock("fragment_field_changes", st.fragmentFields)
	printCounterBlock("edge_field_changes", st.edgeFields)
}

func printCounterBlock(name string, counts counters) {
	fmt.Printf("%s:\n", name)
	if len(counts) == 0 {
		fmt.Println("  (none)")
		return
	}
	type row struct {
		key   string
		count int
	}
	rows := make([]row, 0, len(counts))
	for k, v := range counts {
		rows = append(rows, row{key: k, count: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count == rows[j].count {
			return rows[i].key < rows[j].key
		}
		return rows[i].count > rows[j].count
	})
	for _, row := range rows {
		fmt.Printf("  %s=%d\n", row.key, row.count)
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
