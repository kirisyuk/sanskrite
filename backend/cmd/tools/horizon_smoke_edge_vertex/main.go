package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type queryReport struct {
	Query        string              `json:"query"`
	NodeHits     []fragmentHitReport `json:"nodeHits,omitempty"`
	EdgeHits     []fragmentHitReport `json:"edgeHits,omitempty"`
	EdgeDocs     []edgeDocReport     `json:"edgeDocs,omitempty"`
	PollAttempts int                 `json:"pollAttempts"`
}

type fragmentHitReport struct {
	ID            string  `json:"id"`
	CanonicalPath string  `json:"canonicalPath"`
	Label         string  `json:"label,omitempty"`
	Type          string  `json:"type,omitempty"`
	Score         float64 `json:"score"`
}

type edgeDocReport struct {
	EdgeID            string  `json:"edgeId"`
	EdgeType          string  `json:"edgeType"`
	SourceFragmentID  string  `json:"sourceFragmentId"`
	TargetFragmentID  string  `json:"targetFragmentId"`
	SourceCanonical   string  `json:"sourceCanonicalPath,omitempty"`
	TargetCanonical   string  `json:"targetCanonicalPath,omitempty"`
	SourceLabel       string  `json:"sourceLabel,omitempty"`
	TargetLabel       string  `json:"targetLabel,omitempty"`
	Score             float64 `json:"score"`
}

type smokeReport struct {
	Project          string         `json:"project"`
	Bucket           string         `json:"bucket"`
	NodeDataStore    string         `json:"nodeDataStore"`
	EdgeDataStore    string         `json:"edgeDataStore"`
	UpdatedEdges     []string       `json:"updatedEdges"`
	ExportedURIs     []string       `json:"exportedUris"`
	BatcherStats     map[string]int `json:"batcherStats"`
	AnchorFragments  []string       `json:"anchorFragments"`
	QueryReports     []queryReport  `json:"queryReports"`
}

type vertexFragmentHit struct {
	fragment *models.HorizonFragment
	score    float64
}

func main() {
	var (
		projectID   = flag.String("project", getenv("GCP_PROJECT", "nv-horizon"), "GCP project")
		bucket      = flag.String("bucket", firstNonEmpty(os.Getenv("NV_HORIZON_FRAGMENTS_BUCKET"), os.Getenv("HORIZON_FRAGMENTS_BUCKET"), "nv-horizon-fragments"), "GCS bucket for horizon exports")
		vertexLoc   = flag.String("vertex-location", getenv("VERTEX_SEARCH_LOCATION", "global"), "Vertex AI Search location")
		nodeStore   = flag.String("node-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"), "nv-horizon-fragments-store"), "Vertex node datastore")
		edgeStore   = flag.String("edge-store", firstNonEmpty(os.Getenv("VERTEX_SEARCH_NV_HORIZON_EDGE_DATA_STORE_ID"), os.Getenv("VERTEX_SEARCH_HORIZON_EDGES_DATA_STORE_ID"), "nv-horizon-edges-store"), "Vertex edge datastore")
		pathsFlag   = flag.String("paths", "/zone/qa/military_conflict/ras-laffan,/zone/sea/military_conflict/bab-el-mandeb,/zone/ca/infrastructure_disruption/port-of-vancouver", "Comma-separated canonical paths to anchor smoke edges")
		queriesFlag = flag.String("queries", "qatar helium lng mri,bab el mandeb red sea closure shipping,port of vancouver dockworkers strike maritime capacity", "Comma-separated smoke search queries")
		limitPer    = flag.Int("limit-per-anchor", 8, "Max source/target edges per anchor fragment")
		topK        = flag.Int("top-k", 5, "Top hits per query")
		pollFor     = flag.Duration("poll-for", 2*time.Minute, "How long to poll for edge search results")
		pollEvery   = flag.Duration("poll-every", 12*time.Second, "Polling interval for edge search readiness")
	)
	flag.Parse()

	ctx := context.Background()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	gcsClient, err := gcp.NewGCSClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init GCS: %v", err)
	}

	vertexClient := ai.NewVertexAISearchClient(strings.TrimSpace(*projectID), strings.TrimSpace(*vertexLoc), strings.TrimSpace(*nodeStore), "")
	if !vertexClient.IsConfigured() {
		log.Fatalf("vertex client is not configured")
	}

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client()).
		WithFragmentsRepo(fragmentsRepo).
		WithStorage(gcsClient, strings.TrimSpace(*bucket))

	batcher := horizonrepo.NewVertexSyncBatcher(
		func(ctx context.Context, uris []string) (string, error) {
			return vertexClient.WithDataStore(strings.TrimSpace(*edgeStore)).ImportDocumentsFromURIs(ctx, uris, "INCREMENTAL", "document")
		},
		horizonrepo.VertexSyncBatcherConfig{
			MaxBatchSize:  20,
			MinBatchSize:  1,
			FlushInterval: 2 * time.Second,
		},
	)
	defer batcher.Close()
	edgesRepo.WithVertexSyncBatcher(batcher)

	anchorPaths := splitCSV(*pathsFlag)
	queryList := splitCSV(*queriesFlag)
	seenEdgeIDs := map[string]*models.DependencyEdge{}
	anchorReports := make([]string, 0, len(anchorPaths))

	for _, cp := range anchorPaths {
		frag, err := fragmentsRepo.GetByCanonicalPath(ctx, cp)
		if err != nil || frag == nil {
			log.Printf("anchor fragment not found canonicalPath=%s err=%v", cp, err)
			continue
		}
		anchorReports = append(anchorReports, fmt.Sprintf("%s (%s)", strings.TrimSpace(frag.CanonicalPath), strings.TrimSpace(frag.ID)))
		addEdges := func(items []*models.DependencyEdge) {
			for _, edge := range items {
				if edge == nil || strings.TrimSpace(edge.ID) == "" {
					continue
				}
				if _, ok := seenEdgeIDs[edge.ID]; ok {
					continue
				}
				seenEdgeIDs[edge.ID] = edge
			}
		}
		srcEdges, err := edgesRepo.ListBySource(ctx, "global", strings.TrimSpace(frag.ID), nil, *limitPer)
		if err != nil {
			log.Printf("list source edges failed anchor=%s: %v", cp, err)
		}
		addEdges(srcEdges)
		tgtEdges, err := edgesRepo.ListByTarget(ctx, "global", strings.TrimSpace(frag.ID), nil, *limitPer)
		if err != nil {
			log.Printf("list target edges failed anchor=%s: %v", cp, err)
		}
		addEdges(tgtEdges)
	}

	if len(seenEdgeIDs) == 0 {
		log.Fatalf("no edges found for anchor paths")
	}

	updatedEdges := make([]string, 0, len(seenEdgeIDs))
	exportedURIs := make([]string, 0, len(seenEdgeIDs))
	for _, edge := range seenEdgeIDs {
		if err := edgesRepo.Update(ctx, edge); err != nil {
			log.Fatalf("edge update/export failed id=%s: %v", edge.ID, err)
		}
		updatedEdges = append(updatedEdges, edge.ID)
		exportedURIs = append(exportedURIs, fmt.Sprintf("gs://%s/%s/horizon_edges/%s.json", strings.TrimSpace(*bucket), strings.TrimSpace(edge.TenantID), strings.TrimSpace(edge.ID)))
	}
	sort.Strings(updatedEdges)
	sort.Strings(exportedURIs)
	batcher.FlushSync()

	batches, docs, errs, pending := batcher.Stats()
	report := smokeReport{
		Project:       strings.TrimSpace(*projectID),
		Bucket:        strings.TrimSpace(*bucket),
		NodeDataStore: strings.TrimSpace(*nodeStore),
		EdgeDataStore: strings.TrimSpace(*edgeStore),
		UpdatedEdges:  updatedEdges,
		ExportedURIs:  exportedURIs,
		BatcherStats: map[string]int{
			"batches":   batches,
			"documents": docs,
			"errors":    errs,
			"pending":   pending,
		},
		AnchorFragments: anchorReports,
	}

	if errs > 0 {
		log.Printf("warning: vertex sync batcher recorded %d errors", errs)
	}

	for _, query := range queryList {
		qr := pollQueryReports(ctx, vertexClient, fragmentsRepo, strings.TrimSpace(*nodeStore), strings.TrimSpace(*edgeStore), query, *topK, *pollFor, *pollEvery)
		report.QueryReports = append(report.QueryReports, qr)
	}

	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("marshal report failed: %v", err)
	}
	fmt.Println(string(payload))
}

func pollQueryReports(ctx context.Context, vertexClient *ai.VertexAISearchClient, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, nodeStore, edgeStore, query string, topK int, pollFor, pollEvery time.Duration) queryReport {
	deadline := time.Now().Add(pollFor)
	attempts := 0
	last := queryReport{Query: query}
	for {
		attempts++
		last = runQueryReport(ctx, vertexClient, fragmentsRepo, nodeStore, edgeStore, query, topK)
		last.PollAttempts = attempts
		if len(last.EdgeHits) > 0 || time.Now().After(deadline) {
			return last
		}
		time.Sleep(pollEvery)
	}
}

func runQueryReport(ctx context.Context, vertexClient *ai.VertexAISearchClient, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, nodeStore, edgeStore, query string, topK int) queryReport {
	req := &ai.SearchRequest{Query: query, PageSize: topK}
	report := queryReport{Query: query}

	if resp, err := vertexClient.WithDataStore(nodeStore).Search(ctx, req); err == nil {
		report.NodeHits = fragmentHitsToReport(ctx, fragmentsRepo, loadVertexNodeHits(ctx, fragmentsRepo, resp.Results), topK)
	} else {
		log.Printf("node search failed query=%q: %v", query, err)
	}

	if resp, err := vertexClient.WithDataStore(edgeStore).Search(ctx, req); err == nil {
		report.EdgeDocs, report.EdgeHits = edgeResultsToReport(ctx, fragmentsRepo, resp.Results, topK)
	} else {
		log.Printf("edge search failed query=%q: %v", query, err)
	}

	return report
}

func loadVertexNodeHits(ctx context.Context, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, results []ai.SearchResult) []vertexFragmentHit {
	out := make([]vertexFragmentHit, 0, len(results))
	for _, res := range results {
		id := vertexFragmentIDFromResult(res)
		if id == "" {
			continue
		}
		fragment, err := fragmentsRepo.Get(ctx, id)
		if err != nil || fragment == nil {
			continue
		}
		out = append(out, vertexFragmentHit{fragment: fragment, score: vertexResultScore(res)})
	}
	return out
}

func edgeResultsToReport(ctx context.Context, fragmentsRepo *horizonrepo.HorizonFragmentsRepository, results []ai.SearchResult, topK int) ([]edgeDocReport, []fragmentHitReport) {
	docs := make([]edgeDocReport, 0, min(topK, len(results)))
	lifted := map[string]vertexFragmentHit{}

	for _, res := range results {
		doc := edgeDocReport{
			EdgeID:           strings.TrimSuffix(strings.TrimSpace(res.Document.ID), ".json"),
			Score:            vertexResultScore(res),
			SourceFragmentID: strings.TrimSpace(structString(res.Document.StructData, "sourceFragmentId")),
			TargetFragmentID: strings.TrimSpace(structString(res.Document.StructData, "targetFragmentId")),
			EdgeType:         strings.TrimSpace(structString(res.Document.StructData, "edgeType")),
			SourceLabel:      strings.TrimSpace(structString(res.Document.StructData, "sourceLabel")),
			TargetLabel:      strings.TrimSpace(structString(res.Document.StructData, "targetLabel")),
			SourceCanonical:  strings.TrimSpace(structString(res.Document.StructData, "sourcePath")),
			TargetCanonical:  strings.TrimSpace(structString(res.Document.StructData, "targetPath")),
		}
		if len(docs) < topK {
			docs = append(docs, doc)
		}
		for _, fragmentID := range vertexEdgeFragmentIDs(res) {
			if fragmentID == "" {
				continue
			}
			fragment, err := fragmentsRepo.Get(ctx, fragmentID)
			if err != nil || fragment == nil {
				continue
			}
			score := vertexResultScore(res) * 0.92
			prev, ok := lifted[fragmentID]
			if ok && prev.score >= score {
				continue
			}
			lifted[fragmentID] = vertexFragmentHit{fragment: fragment, score: score}
		}
	}

	hits := make([]vertexFragmentHit, 0, len(lifted))
	for _, hit := range lifted {
		hits = append(hits, hit)
	}
	return docs, fragmentHitsToReport(ctx, fragmentsRepo, hits, topK)
}

func fragmentHitsToReport(_ context.Context, _ *horizonrepo.HorizonFragmentsRepository, hits []vertexFragmentHit, topK int) []fragmentHitReport {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return strings.TrimSpace(hits[i].fragment.CanonicalPath) < strings.TrimSpace(hits[j].fragment.CanonicalPath)
		}
		return hits[i].score > hits[j].score
	})
	out := make([]fragmentHitReport, 0, min(topK, len(hits)))
	for _, hit := range hits {
		if hit.fragment == nil {
			continue
		}
		out = append(out, fragmentHitReport{
			ID:            strings.TrimSpace(hit.fragment.ID),
			CanonicalPath: strings.TrimSpace(hit.fragment.CanonicalPath),
			Label:         fragmentLabel(hit.fragment),
			Type:          string(hit.fragment.Type),
			Score:         hit.score,
		})
		if len(out) >= topK {
			break
		}
	}
	return out
}

func vertexFragmentIDFromResult(res ai.SearchResult) string {
	if u := strings.TrimSpace(res.Document.URI); u != "" {
		if id := extractVertexDocumentIDFromURI(u); id != "" {
			return id
		}
	}
	if id := strings.TrimSuffix(strings.TrimSpace(res.Document.ID), ".json"); id != "" {
		return id
	}
	if res.Document.StructData != nil {
		if u, ok := res.Document.StructData["uri"].(string); ok && strings.TrimSpace(u) != "" {
			return extractVertexDocumentIDFromURI(u)
		}
	}
	return ""
}

func extractVertexDocumentIDFromURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	parts := strings.Split(uri, "/")
	if len(parts) == 0 {
		return ""
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	return strings.TrimSuffix(last, ".json")
}

func vertexEdgeFragmentIDs(res ai.SearchResult) []string {
	if res.Document.StructData == nil {
		return nil
	}
	return uniqueStrings([]string{
		structString(res.Document.StructData, "sourceFragmentId"),
		structString(res.Document.StructData, "targetFragmentId"),
	})
}

func vertexResultScore(res ai.SearchResult) float64 {
	if res.RelevanceScore > 0 {
		return res.RelevanceScore
	}
	if res.RankSignals != nil && res.RankSignals.SemanticSimilarityScore > 0 {
		return res.RankSignals.SemanticSimilarityScore
	}
	return 0
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func structString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

func fragmentLabel(fragment *models.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(fragment.Labels["display"]); v != "" {
		return v
	}
	for _, value := range fragment.Labels {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return strings.TrimSpace(fragment.CanonicalPath)
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
