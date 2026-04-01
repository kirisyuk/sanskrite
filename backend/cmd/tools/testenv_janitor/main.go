package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	taskspb "cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stringSet map[string]struct{}

func (s stringSet) Has(v string) bool {
	_, ok := s[strings.TrimSpace(v)]
	return ok
}

func parseCSVSet(raw string) stringSet {
	out := make(stringSet)
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		out[p] = struct{}{}
	}
	return out
}

func parseCSVSlice(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

type config struct {
	projectID string
	apply     bool
	dryRun    bool

	scope  string
	tenant string

	firestorePageSize        int
	firestorePageTimeout     time.Duration
	firestoreMaxRetries      int
	firestoreRetryBase       time.Duration
	firestoreBulkDatabase    string
	firestoreBulkWait        bool
	firestoreBulkPollEvery   time.Duration
	firestoreBulkTimeout     time.Duration
	scanSubcollectionsDryRun bool

	collections            []string
	preserveCollections    stringSet
	forceDeleteCollections stringSet

	keepUserDocIDs stringSet
	keepUserUIDs   stringSet
	sampleLimit    int

	cleanGCS    bool
	gcsMode     string
	gcsBuckets  []string
	gcsPrefixes []string

	cleanVertex    bool
	vertexLocation string
	vertexStores   []string
	vertexAPIKey   string

	purgeCloudTasks    bool
	cloudTasksLocation string
	cloudTaskQueues    []string

	purgePubSub         bool
	pubSubSubscriptions []string
	pubSubPrefixes      []string

	cleanBigQuery   bool
	bigQueryProject string
	bigQueryDataset string
	bigQueryMode    string
	bigQueryTables  []string
}

type stepResult struct {
	name    string
	details []string
	errors  []error
}

func (r *stepResult) addf(format string, args ...interface{}) {
	r.details = append(r.details, fmt.Sprintf(format, args...))
}

func (r *stepResult) adderr(err error) {
	if err == nil {
		return
	}
	r.errors = append(r.errors, err)
}

type collectionStats struct {
	collection string
	scanned    int
	matched    int
	deleted    int
	skipped    int
	failed     int
	samples    []string
}

type firestoreSummary struct {
	collections             []*collectionStats
	subcollectionsScanned   int
	subcollectionsMatched   int
	subcollectionsDeleted   int
	subcollectionsFailed    int
	topLevelCollectionsSeen int
}

type vertexListResponse struct {
	Documents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"documents"`
	NextPageToken string `json:"nextPageToken"`
}

type firestoreBulkDeleteRequest struct {
	CollectionIDs []string `json:"collectionIds"`
	NamespaceIDs  []string `json:"namespaceIds,omitempty"`
}

type firestoreLongRunningOperation struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("[janitor] project=%s apply=%v dryRun=%v scope=%s tenant=%s", cfg.projectID, cfg.apply, cfg.dryRun, cfg.scope, cfg.tenant)
	log.Printf("[janitor] firestore: delete-mode=bulk-only page-size=%d page-timeout=%s retries=%d retry-base=%s bulk-db=%s bulk-wait=%v bulk-poll=%s bulk-timeout=%s scan-subcollections-dry-run=%v",
		cfg.firestorePageSize,
		cfg.firestorePageTimeout,
		cfg.firestoreMaxRetries,
		cfg.firestoreRetryBase,
		cfg.firestoreBulkDatabase,
		cfg.firestoreBulkWait,
		cfg.firestoreBulkPollEvery,
		cfg.firestoreBulkTimeout,
		cfg.scanSubcollectionsDryRun,
	)
	log.Printf("[janitor] collections=%s preserve=%s force-delete=%s", joinOrDash(cfg.collections), joinOrDash(setToSortedSlice(cfg.preserveCollections)), joinOrDash(setToSortedSlice(cfg.forceDeleteCollections)))
	log.Printf("[janitor] keep-user-doc-ids=%s keep-user-uids=%s", joinOrDash(setToSortedSlice(cfg.keepUserDocIDs)), joinOrDash(setToSortedSlice(cfg.keepUserUIDs)))
	log.Printf("[janitor] gcs: clean=%v mode=%s buckets=%s prefixes=%s", cfg.cleanGCS, cfg.gcsMode, joinOrDash(cfg.gcsBuckets), joinOrDash(cfg.gcsPrefixes))
	log.Printf("[janitor] vertex: clean=%v location=%s stores=%s", cfg.cleanVertex, cfg.vertexLocation, joinOrDash(cfg.vertexStores))
	log.Printf("[janitor] cloud-tasks: purge=%v location=%s queues=%s", cfg.purgeCloudTasks, cfg.cloudTasksLocation, joinOrDash(cfg.cloudTaskQueues))
	log.Printf("[janitor] pubsub: purge=%v subscriptions=%s prefixes=%s", cfg.purgePubSub, joinOrDash(cfg.pubSubSubscriptions), joinOrDash(cfg.pubSubPrefixes))
	log.Printf("[janitor] bigquery: clean=%v project=%s dataset=%s mode=%s tables=%s", cfg.cleanBigQuery, cfg.bigQueryProject, cfg.bigQueryDataset, cfg.bigQueryMode, joinOrDash(cfg.bigQueryTables))

	ctx := context.Background()
	fsClient, err := firestore.NewClient(ctx, cfg.projectID)
	if err != nil {
		log.Fatalf("failed to init firestore client: %v", err)
	}
	defer fsClient.Close()

	results := make([]*stepResult, 0, 8)

	if cfg.purgeCloudTasks {
		results = append(results, purgeCloudTasksQueues(ctx, cfg))
	}
	if cfg.purgePubSub {
		results = append(results, purgePubSubBacklog(ctx, cfg))
	}

	results = append(results, cleanupFirestore(ctx, fsClient, cfg))

	if cfg.cleanGCS {
		results = append(results, cleanupGCS(ctx, cfg))
	}
	if cfg.cleanVertex {
		results = append(results, cleanupVertex(ctx, cfg))
	}
	if cfg.cleanBigQuery {
		results = append(results, cleanupBigQuery(ctx, cfg))
	}

	hadErrors := false
	log.Println("\n=== Janitor Summary ===")
	for _, res := range results {
		if res == nil {
			continue
		}
		log.Printf("[%s]", res.name)
		for _, d := range res.details {
			log.Printf("  - %s", d)
		}
		if len(res.errors) > 0 {
			hadErrors = true
			for _, e := range res.errors {
				log.Printf("  ! %v", e)
			}
		}
	}
	if cfg.dryRun {
		log.Println("[DRY-RUN] No destructive changes applied.")
	}
	if hadErrors {
		os.Exit(1)
	}
}

func parseConfig() (*config, error) {
	defaultProject := firstNonEmpty(strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT")))
	defaultTenant := firstNonEmpty(strings.TrimSpace(os.Getenv("JANITOR_TENANT")), "global")
	defaultPreserveCollections := firstNonEmpty(
		strings.TrimSpace(os.Getenv("JANITOR_PRESERVE_COLLECTIONS")),
		"core_projects,horizon_taxonomy,horizon_market_segment_dict,horizon_metric_specs,horizon_propagation_profiles,horizon_propagation_policies,horizon_validation_rules,horizon_synthesis_rules,horizon_canonization_rules,horizon_risk_appetite_profiles,horizon_fragment_profile_policies",
	)
	projectID := flag.String("project", defaultProject, "GCP project ID (required)")
	apply := flag.Bool("apply", false, "Apply deletions (default: dry-run)")
	dryRun := flag.Bool("dry-run", true, "Dry-run mode")

	scope := flag.String("scope", "all", "Firestore sweep scope: all|tenant")
	tenant := flag.String("tenant", defaultTenant, "Tenant ID (used for scope=tenant and default GCS prefixes)")
	firestorePageSize := flag.Int("firestore-page-size", 500, "Firestore page size for cursor mode")
	firestorePageTimeout := flag.Duration("firestore-page-timeout", 45*time.Second, "Per-page Firestore query timeout for cursor mode")
	firestoreMaxRetries := flag.Int("firestore-max-retries", 8, "Max retries for retryable Firestore errors")
	firestoreRetryBase := flag.Duration("firestore-retry-base", 2*time.Second, "Base retry delay for Firestore errors")
	firestoreBulkDatabase := flag.String("firestore-bulk-database", firstNonEmpty(strings.TrimSpace(os.Getenv("JANITOR_FIRESTORE_BULK_DATABASE")), "(default)"), "Firestore database for bulk delete API (usually (default))")
	firestoreBulkWait := flag.Bool("firestore-bulk-wait", true, "Wait for Firestore bulk delete operation completion")
	firestoreBulkPollEvery := flag.Duration("firestore-bulk-poll-every", 10*time.Second, "Polling interval for Firestore bulk delete operation")
	firestoreBulkTimeout := flag.Duration("firestore-bulk-timeout", 2*time.Hour, "Timeout for Firestore bulk delete operation")
	scanSubcollectionsDryRun := flag.Bool("scan-subcollections-dry-run", false, "In dry-run mode, recursively scan subcollections (slower)")

	collections := flag.String("collections", "", "Comma-separated top-level Firestore collections to process (default: all discovered)")
	preserveCollections := flag.String("preserve-collections", defaultPreserveCollections, "Comma-separated collections to preserve entirely")
	forceDeleteCollections := flag.String("force-delete-collections", "", "Comma-separated collections to force-delete even if preserved")

	keepUserDocIDs := flag.String("keep-user-doc-ids", strings.TrimSpace(os.Getenv("JANITOR_KEEP_USER_DOC_IDS")), "Comma-separated core_users doc IDs to preserve")
	keepUserUIDs := flag.String("keep-user-uids", strings.TrimSpace(os.Getenv("JANITOR_KEEP_USER_UIDS")), "Comma-separated user IDs/UIDs to preserve")
	sampleLimit := flag.Int("sample-limit", 10, "Max sample IDs to print per Firestore collection")

	cleanGCS := flag.Bool("clean-gcs", true, "Clean GCS artifacts")
	gcsMode := flag.String("gcs-mode", firstNonEmpty(strings.TrimSpace(os.Getenv("JANITOR_GCS_MODE")), "full"), "GCS cleanup mode: full|prefix")
	gcsBuckets := flag.String("gcs-buckets", strings.TrimSpace(os.Getenv("JANITOR_GCS_BUCKETS")), "Comma-separated GCS buckets to clean")
	gcsPrefixes := flag.String("gcs-prefixes", strings.TrimSpace(os.Getenv("JANITOR_GCS_PREFIXES")), "Comma-separated prefixes used when gcs-mode=prefix")

	cleanVertex := flag.Bool("clean-vertex", true, "Clean Vertex AI Search stores by listing and deleting documents")
	vertexLocation := flag.String("vertex-location", firstNonEmpty(strings.TrimSpace(os.Getenv("VERTEX_SEARCH_LOCATION")), "global"), "Vertex AI Search location")
	vertexStores := flag.String("vertex-stores", strings.TrimSpace(os.Getenv("JANITOR_VERTEX_STORES")), "Comma-separated Vertex AI Search dataStore IDs")
	vertexAPIKey := flag.String("vertex-api-key", strings.TrimSpace(os.Getenv("VERTEX_SEARCH_API_KEY")), "Optional API key for Vertex AI Search (ADC is used if empty)")

	purgeCloudTasks := flag.Bool("purge-cloud-tasks", true, "Purge Cloud Tasks queues")
	cloudTasksLocation := flag.String("cloud-tasks-location", firstNonEmpty(strings.TrimSpace(os.Getenv("CLOUD_TASKS_LOCATION")), "us-central1"), "Cloud Tasks location")
	cloudTaskQueues := flag.String("cloud-task-queues", strings.TrimSpace(os.Getenv("JANITOR_CLOUD_TASK_QUEUES")), "Comma-separated Cloud Tasks queue names")

	purgePubSub := flag.Bool("purge-pubsub", true, "Purge Pub/Sub backlog by seeking subscriptions to now")
	pubSubSubscriptions := flag.String("pubsub-subscriptions", strings.TrimSpace(os.Getenv("JANITOR_PUBSUB_SUBSCRIPTIONS")), "Comma-separated Pub/Sub subscriptions to purge")
	pubSubPrefixes := flag.String("pubsub-prefixes", firstNonEmpty(strings.TrimSpace(os.Getenv("JANITOR_PUBSUB_PREFIXES")), "nv-horizon.,source.poll,ground.refresh,chat.ontology.process,document.process"), "Subscription name prefixes used when pubsub-subscriptions is empty")

	cleanBigQuery := flag.Bool("clean-bigquery", false, "Clean BigQuery tables")
	bigQueryProject := flag.String("bigquery-project", firstNonEmpty(strings.TrimSpace(os.Getenv("BIGQUERY_PROJECT_ID")), defaultProject), "BigQuery project ID")
	bigQueryDataset := flag.String("bigquery-dataset", firstNonEmpty(strings.TrimSpace(os.Getenv("BIGQUERY_DATASET")), strings.TrimSpace(os.Getenv("BIGQUERY_DATASET_ID"))), "BigQuery dataset ID")
	bigQueryMode := flag.String("bigquery-mode", "tenant", "BigQuery cleanup mode: tenant|truncate")
	bigQueryTables := flag.String("bigquery-tables", strings.TrimSpace(os.Getenv("JANITOR_BIGQUERY_TABLES")), "Comma-separated BigQuery tables (default: all tables in dataset)")

	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		return nil, errors.New("--project is required")
	}

	if *apply {
		*dryRun = false
	}
	if *dryRun {
		*apply = false
	}

	scopeValue := strings.ToLower(strings.TrimSpace(*scope))
	if scopeValue != "all" && scopeValue != "tenant" {
		return nil, fmt.Errorf("invalid --scope=%q (expected all|tenant)", *scope)
	}
	if scopeValue == "tenant" && strings.TrimSpace(*tenant) == "" {
		return nil, errors.New("--tenant is required when --scope=tenant")
	}
	if *firestorePageSize <= 0 {
		return nil, errors.New("--firestore-page-size must be > 0")
	}
	if *firestorePageTimeout <= 0 {
		return nil, errors.New("--firestore-page-timeout must be > 0")
	}
	if *firestoreMaxRetries < 0 {
		return nil, errors.New("--firestore-max-retries must be >= 0")
	}
	if *firestoreRetryBase <= 0 {
		return nil, errors.New("--firestore-retry-base must be > 0")
	}
	if strings.TrimSpace(*firestoreBulkDatabase) == "" {
		return nil, errors.New("--firestore-bulk-database must not be empty")
	}
	if *firestoreBulkPollEvery <= 0 {
		return nil, errors.New("--firestore-bulk-poll-every must be > 0")
	}
	if *firestoreBulkTimeout <= 0 {
		return nil, errors.New("--firestore-bulk-timeout must be > 0")
	}
	if scopeValue == "tenant" && *apply {
		return nil, errors.New("bulk-only apply does not support --scope=tenant; use --scope=all")
	}

	gcsModeValue := strings.ToLower(strings.TrimSpace(*gcsMode))
	if gcsModeValue != "full" && gcsModeValue != "prefix" {
		return nil, fmt.Errorf("invalid --gcs-mode=%q (expected full|prefix)", *gcsMode)
	}

	bqMode := strings.ToLower(strings.TrimSpace(*bigQueryMode))
	if bqMode != "tenant" && bqMode != "truncate" {
		return nil, fmt.Errorf("invalid --bigquery-mode=%q (expected tenant|truncate)", *bigQueryMode)
	}

	cfg := &config{
		projectID:                strings.TrimSpace(*projectID),
		apply:                    *apply,
		dryRun:                   *dryRun,
		scope:                    scopeValue,
		tenant:                   strings.TrimSpace(*tenant),
		firestorePageSize:        *firestorePageSize,
		firestorePageTimeout:     *firestorePageTimeout,
		firestoreMaxRetries:      *firestoreMaxRetries,
		firestoreRetryBase:       *firestoreRetryBase,
		firestoreBulkDatabase:    strings.TrimSpace(*firestoreBulkDatabase),
		firestoreBulkWait:        *firestoreBulkWait,
		firestoreBulkPollEvery:   *firestoreBulkPollEvery,
		firestoreBulkTimeout:     *firestoreBulkTimeout,
		scanSubcollectionsDryRun: *scanSubcollectionsDryRun,
		collections:              parseCSVSlice(*collections),
		preserveCollections:      parseCSVSet(*preserveCollections),
		forceDeleteCollections:   parseCSVSet(*forceDeleteCollections),
		keepUserDocIDs:           parseCSVSet(*keepUserDocIDs),
		keepUserUIDs:             parseCSVSet(*keepUserUIDs),
		sampleLimit:              max(0, *sampleLimit),
		cleanGCS:                 *cleanGCS,
		gcsMode:                  gcsModeValue,
		gcsBuckets:               nil,
		gcsPrefixes:              parseCSVSlice(*gcsPrefixes),
		cleanVertex:              *cleanVertex,
		vertexLocation:           strings.TrimSpace(*vertexLocation),
		vertexStores:             nil,
		vertexAPIKey:             strings.TrimSpace(*vertexAPIKey),
		purgeCloudTasks:          *purgeCloudTasks,
		cloudTasksLocation:       strings.TrimSpace(*cloudTasksLocation),
		cloudTaskQueues:          nil,
		purgePubSub:              *purgePubSub,
		pubSubSubscriptions:      parseCSVSlice(*pubSubSubscriptions),
		pubSubPrefixes:           parseCSVSlice(*pubSubPrefixes),
		cleanBigQuery:            *cleanBigQuery,
		bigQueryProject:          strings.TrimSpace(*bigQueryProject),
		bigQueryDataset:          strings.TrimSpace(*bigQueryDataset),
		bigQueryMode:             bqMode,
		bigQueryTables:           parseCSVSlice(*bigQueryTables),
	}

	cfg.gcsBuckets = resolveGCSBuckets(cfg.projectID, strings.TrimSpace(*gcsBuckets))
	cfg.vertexStores = resolveVertexStores(strings.TrimSpace(*vertexStores))
	cfg.cloudTaskQueues = resolveCloudTaskQueues(strings.TrimSpace(*cloudTaskQueues))
	if len(cfg.gcsPrefixes) == 0 && cfg.gcsMode == "prefix" {
		if cfg.tenant != "" {
			cfg.gcsPrefixes = []string{strings.Trim(cfg.tenant, "/") + "/"}
		}
	}
	if cfg.cleanBigQuery {
		if cfg.bigQueryProject == "" {
			cfg.bigQueryProject = cfg.projectID
		}
		if cfg.bigQueryDataset == "" {
			return nil, errors.New("--bigquery-dataset is required when --clean-bigquery=true")
		}
		if cfg.bigQueryMode == "tenant" && strings.TrimSpace(cfg.tenant) == "" {
			return nil, errors.New("--tenant is required for --bigquery-mode=tenant")
		}
	}

	if cfg.cleanVertex && cfg.vertexLocation == "" {
		cfg.vertexLocation = "global"
	}

	return cfg, nil
}

func cleanupFirestore(ctx context.Context, fsClient *firestore.Client, cfg *config) *stepResult {
	res := &stepResult{name: "firestore"}

	collections, err := discoverCollections(ctx, fsClient, cfg.collections)
	if err != nil {
		res.adderr(fmt.Errorf("discover collections: %w", err))
		return res
	}
	if len(collections) == 0 {
		res.addf("no collections discovered")
		return res
	}

	summary := &firestoreSummary{topLevelCollectionsSeen: len(collections)}
	for _, colName := range collections {
		st := &collectionStats{collection: colName}
		summary.collections = append(summary.collections, st)

		if cfg.preserveCollections.Has(colName) && !cfg.forceDeleteCollections.Has(colName) {
			st.skipped++
			res.addf("collection=%s preserved (skipped)", colName)
			continue
		}

		if !cfg.dryRun {
			// Bulk-only apply mode: no cursor fallback.
			if strings.TrimSpace(colName) == "core_users" && (len(cfg.keepUserDocIDs) > 0 || len(cfg.keepUserUIDs) > 0) {
				st.skipped++
				res.addf("collection=%s skipped in bulk-only mode because keep-user filters are set", colName)
				continue
			}
			opName, berr := runFirestoreBulkDelete(ctx, cfg, colName)
			if berr != nil {
				st.failed++
				res.adderr(fmt.Errorf("collection=%s bulk-delete: %w", colName, berr))
				continue
			}
			// Server-side bulk delete does not expose per-document counts in the operation response.
			res.addf("collection=%s mode=bulk operation=%s", colName, opName)
			continue
		}

		if err := cleanupFirestoreCollectionCursor(ctx, fsClient.Collection(colName), colName, cfg, summary, st); err != nil {
			st.failed++
			res.adderr(fmt.Errorf("collection=%s iterate: %w", colName, err))
		}
		res.addf("collection=%s mode=cursor scanned=%d matched=%d deleted=%d skipped=%d failed=%d samples=%s", st.collection, st.scanned, st.matched, st.deleted, st.skipped, st.failed, joinOrDash(st.samples))
	}

	res.addf("subcollections scanned=%d matched=%d deleted=%d failed=%d", summary.subcollectionsScanned, summary.subcollectionsMatched, summary.subcollectionsDeleted, summary.subcollectionsFailed)
	return res
}

func cleanupFirestoreCollectionCursor(
	ctx context.Context,
	colRef *firestore.CollectionRef,
	colName string,
	cfg *config,
	summary *firestoreSummary,
	st *collectionStats,
) error {
	lastDocID := ""
	for {
		docs, err := listCollectionPage(ctx, colRef, lastDocID, cfg)
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			return nil
		}

		for _, doc := range docs {
			st.scanned++
			data := doc.Data()

			if shouldSkipDoc(cfg, colName, doc.Ref.ID, data) {
				st.skipped++
				continue
			}
			if cfg.scope == "tenant" && !matchesTenant(cfg.tenant, colName, doc.Ref.ID, data) {
				st.skipped++
				continue
			}

			st.matched++
			if len(st.samples) < cfg.sampleLimit {
				st.samples = append(st.samples, doc.Ref.ID)
			}

			if cfg.dryRun {
				if cfg.scanSubcollectionsDryRun {
					if err := deleteDocTree(ctx, doc.Ref, cfg, summary); err != nil {
						st.failed++
						continue
					}
				}
				st.deleted++
				continue
			}

			err := doWithFirestoreRetry(ctx, cfg, fmt.Sprintf("delete %s", doc.Ref.Path), func(opCtx context.Context) error {
				return deleteDocTree(opCtx, doc.Ref, cfg, summary)
			})
			if err != nil {
				st.failed++
				continue
			}
			st.deleted++
		}

		lastDocID = docs[len(docs)-1].Ref.ID
	}
}

func listCollectionPage(ctx context.Context, colRef *firestore.CollectionRef, lastDocID string, cfg *config) ([]*firestore.DocumentSnapshot, error) {
	var docs []*firestore.DocumentSnapshot
	err := doWithFirestoreRetry(ctx, cfg, fmt.Sprintf("list page %s start-after=%s", colRef.ID, lastDocID), func(opCtx context.Context) error {
		pageCtx, cancel := context.WithTimeout(opCtx, cfg.firestorePageTimeout)
		defer cancel()

		q := colRef.Query.OrderBy(firestore.DocumentID, firestore.Asc).Limit(cfg.firestorePageSize)
		if strings.TrimSpace(lastDocID) != "" {
			q = q.StartAfter(lastDocID)
		}

		out, err := q.Documents(pageCtx).GetAll()
		if err != nil {
			return err
		}
		docs = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return docs, nil
}

func runFirestoreBulkDelete(ctx context.Context, cfg *config, collection string) (string, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return "", fmt.Errorf("init token source: %w", err)
	}
	httpClient := &http.Client{Timeout: 60 * time.Second}

	endpoint := fmt.Sprintf(
		"https://firestore.googleapis.com/v1/projects/%s/databases/%s:bulkDeleteDocuments",
		url.PathEscape(cfg.projectID),
		url.PathEscape(cfg.firestoreBulkDatabase),
	)
	reqBody := &firestoreBulkDeleteRequest{
		CollectionIDs: []string{strings.TrimSpace(collection)},
		NamespaceIDs:  []string{""},
	}

	body, statusCode, err := doAuthenticatedJSONRequest(ctx, httpClient, ts, http.MethodPost, endpoint, cfg.projectID, reqBody)
	if err != nil {
		return "", err
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("bulkDelete API error %d: %s", statusCode, string(body))
	}
	var op firestoreLongRunningOperation
	if err := json.Unmarshal(body, &op); err != nil {
		return "", fmt.Errorf("decode bulkDelete response: %w", err)
	}
	if strings.TrimSpace(op.Name) == "" {
		return "", fmt.Errorf("bulkDelete response missing operation name: %s", string(body))
	}
	if !cfg.firestoreBulkWait {
		return op.Name, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, cfg.firestoreBulkTimeout)
	defer cancel()
	if err := waitFirestoreOperation(waitCtx, httpClient, ts, cfg.projectID, op.Name, cfg.firestoreBulkPollEvery); err != nil {
		return op.Name, err
	}
	return op.Name, nil
}

func waitFirestoreOperation(
	ctx context.Context,
	httpClient *http.Client,
	tokenSource oauth2.TokenSource,
	projectID, opName string,
	pollEvery time.Duration,
) error {
	opName = strings.TrimSpace(opName)
	if opName == "" {
		return errors.New("operation name is empty")
	}
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		endpoint := "https://firestore.googleapis.com/v1/" + opName
		body, statusCode, err := doAuthenticatedJSONRequest(ctx, httpClient, tokenSource, http.MethodGet, endpoint, projectID, nil)
		if err != nil {
			return err
		}
		if statusCode != http.StatusOK {
			return fmt.Errorf("operations.get API error %d: %s", statusCode, string(body))
		}

		var op firestoreLongRunningOperation
		if err := json.Unmarshal(body, &op); err != nil {
			return fmt.Errorf("decode operations.get response: %w", err)
		}
		if op.Done {
			if op.Error != nil && op.Error.Code != 0 {
				return fmt.Errorf("bulk delete operation failed: code=%d message=%s", op.Error.Code, op.Error.Message)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait bulk delete operation timed out/canceled: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func doWithFirestoreRetry(ctx context.Context, cfg *config, op string, fn func(opCtx context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt <= cfg.firestoreMaxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(cfg.firestoreRetryBase, attempt-1)
			log.Printf("[janitor][firestore] retry op=%s attempt=%d/%d delay=%s err=%v", op, attempt, cfg.firestoreMaxRetries, delay, lastErr)
			select {
			case <-ctx.Done():
				return fmt.Errorf("operation canceled while retrying: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if !isRetryableFirestoreErr(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func retryDelay(base time.Duration, exp int) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	delay := base
	for i := 0; i < exp; i++ {
		delay *= 2
		if delay >= 30*time.Second {
			return 30 * time.Second
		}
	}
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func isRetryableFirestoreErr(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted, codes.ResourceExhausted:
			return true
		}
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "service is currently unavailable") ||
		strings.Contains(msg, "query timed out") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "resource exhausted")
}

func deleteDocTree(ctx context.Context, docRef *firestore.DocumentRef, cfg *config, summary *firestoreSummary) error {
	if cfg.dryRun && !cfg.scanSubcollectionsDryRun {
		return nil
	}
	colIter := docRef.Collections(ctx)
	for {
		subCol, err := colIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			summary.subcollectionsFailed++
			return fmt.Errorf("list subcollections for doc=%s: %w", docRef.Path, err)
		}
		docsIter := subCol.Documents(ctx)
		for {
			subDoc, err := docsIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				summary.subcollectionsFailed++
				return fmt.Errorf("iterate subcollection=%s: %w", subCol.Path, err)
			}
			summary.subcollectionsScanned++
			summary.subcollectionsMatched++
			if err := deleteDocTree(ctx, subDoc.Ref, cfg, summary); err != nil {
				summary.subcollectionsFailed++
				return err
			}
			summary.subcollectionsDeleted++
		}
		docsIter.Stop()
	}
	if cfg.dryRun {
		return nil
	}
	_, err := docRef.Delete(ctx)
	if err != nil {
		return fmt.Errorf("delete doc=%s: %w", docRef.Path, err)
	}
	return nil
}

func shouldSkipDoc(cfg *config, collection, docID string, data map[string]interface{}) bool {
	if collection != "core_users" {
		return false
	}
	if cfg.keepUserDocIDs.Has(docID) {
		return true
	}
	for uid := range cfg.keepUserUIDs {
		if docID == uid || strings.HasSuffix(docID, "_"+uid) {
			return true
		}
	}
	if id := extractStringField(data, "id", "uid", "userId", "ID", "UID", "UserID"); id != "" && cfg.keepUserUIDs.Has(id) {
		return true
	}
	return false
}

func matchesTenant(tenant, collection, docID string, data map[string]interface{}) bool {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return true
	}
	if collection == "core_users" && strings.HasPrefix(docID, tenant+"_") {
		return true
	}
	if v := extractStringField(data, "tenantId", "tenantID", "TenantID", "pk", "Pk", "tenant"); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v) == tenant
	}
	return false
}

func cleanupGCS(ctx context.Context, cfg *config) *stepResult {
	res := &stepResult{name: "gcs"}
	if len(cfg.gcsBuckets) == 0 {
		res.addf("no buckets configured")
		return res
	}

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		res.adderr(fmt.Errorf("init storage client: %w", err))
		return res
	}
	defer storageClient.Close()

	for _, bucket := range cfg.gcsBuckets {
		bucket = strings.TrimSpace(bucket)
		if bucket == "" {
			continue
		}
		deleted := 0
		matched := 0
		failed := 0

		prefixes := cfg.gcsPrefixes
		if cfg.gcsMode == "full" {
			prefixes = []string{""}
		}
		for _, prefix := range prefixes {
			q := &storage.Query{Prefix: strings.TrimSpace(prefix)}
			it := storageClient.Bucket(bucket).Objects(ctx, q)
			for {
				obj, err := it.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					failed++
					res.adderr(fmt.Errorf("bucket=%s list prefix=%q: %w", bucket, prefix, err))
					break
				}
				matched++
				if cfg.dryRun {
					deleted++
					continue
				}
				if derr := storageClient.Bucket(bucket).Object(obj.Name).Delete(ctx); derr != nil {
					failed++
					res.adderr(fmt.Errorf("bucket=%s object=%s: %w", bucket, obj.Name, derr))
					continue
				}
				deleted++
			}
		}
		res.addf("bucket=%s mode=%s matched=%d deleted=%d failed=%d", bucket, cfg.gcsMode, matched, deleted, failed)
	}
	return res
}

func cleanupVertex(ctx context.Context, cfg *config) *stepResult {
	res := &stepResult{name: "vertex"}
	if len(cfg.vertexStores) == 0 {
		res.addf("no data stores configured")
		return res
	}

	for _, storeID := range cfg.vertexStores {
		storeID = strings.TrimSpace(storeID)
		if storeID == "" {
			continue
		}
		docIDs, err := listVertexDocumentIDs(ctx, cfg.projectID, cfg.vertexLocation, storeID)
		if err != nil {
			res.adderr(fmt.Errorf("store=%s list documents: %w", storeID, err))
			continue
		}
		deleted := 0
		failed := 0
		client := ai.NewVertexAISearchClient(cfg.projectID, cfg.vertexLocation, storeID, cfg.vertexAPIKey)
		if client == nil || !client.IsConfigured() {
			res.adderr(fmt.Errorf("store=%s vertex client is not configured", storeID))
			continue
		}
		for _, docID := range docIDs {
			if cfg.dryRun {
				deleted++
				continue
			}
			delCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			err := client.DeleteDocument(delCtx, docID)
			cancel()
			if err != nil {
				failed++
				res.adderr(fmt.Errorf("store=%s document=%s: %w", storeID, docID, err))
				continue
			}
			deleted++
		}
		samples := docIDs
		if len(samples) > 10 {
			samples = samples[:10]
		}
		res.addf("store=%s listed=%d deleted=%d failed=%d samples=%s", storeID, len(docIDs), deleted, failed, joinOrDash(samples))
	}

	return res
}

func listVertexDocumentIDs(ctx context.Context, projectID, location, dataStoreID string) ([]string, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("default token source: %w", err)
	}

	httpClient := &http.Client{Timeout: 45 * time.Second}
	docIDs := make([]string, 0, 1024)
	seen := map[string]struct{}{}

	nextToken := ""
	for {
		endpoint := fmt.Sprintf("https://discoveryengine.googleapis.com/v1beta/projects/%s/locations/%s/collections/default_collection/dataStores/%s/branches/default_branch/documents", url.PathEscape(projectID), url.PathEscape(location), url.PathEscape(dataStoreID))
		q := url.Values{}
		q.Set("pageSize", "100")
		if nextToken != "" {
			q.Set("pageToken", nextToken)
		}
		endpoint = endpoint + "?" + q.Encode()

		body, statusCode, err := doAuthenticatedGET(ctx, httpClient, ts, endpoint, projectID)
		if err != nil {
			return nil, err
		}
		if statusCode != http.StatusOK {
			return nil, fmt.Errorf("list documents API error %d: %s", statusCode, string(body))
		}

		var resp vertexListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode list response: %w", err)
		}
		for _, d := range resp.Documents {
			id := strings.TrimSpace(d.ID)
			if id == "" {
				id = parseDocumentIDFromName(d.Name)
			}
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			docIDs = append(docIDs, id)
		}
		if strings.TrimSpace(resp.NextPageToken) == "" {
			break
		}
		nextToken = strings.TrimSpace(resp.NextPageToken)
	}

	sort.Strings(docIDs)
	return docIDs, nil
}

func doAuthenticatedGET(ctx context.Context, httpClient *http.Client, tokenSource oauth2.TokenSource, endpoint, userProject string) ([]byte, int, error) {
	return doAuthenticatedJSONRequest(ctx, httpClient, tokenSource, http.MethodGet, endpoint, userProject, nil)
}

func doAuthenticatedJSONRequest(
	ctx context.Context,
	httpClient *http.Client,
	tokenSource oauth2.TokenSource,
	method, endpoint, userProject string,
	payload interface{},
) ([]byte, int, error) {
	token, err := tokenSource.Token()
	if err != nil {
		return nil, 0, fmt.Errorf("token source: %w", err)
	}

	var bodyReader io.Reader
	if payload != nil {
		raw, merr := json.Marshal(payload)
		if merr != nil {
			return nil, 0, fmt.Errorf("marshal request payload: %w", merr)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("X-Goog-User-Project", userProject)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	return body, resp.StatusCode, nil
}

func parseDocumentIDFromName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	idx := strings.LastIndex(name, "/documents/")
	if idx < 0 {
		return ""
	}
	id := name[idx+len("/documents/"):]
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	decoded, err := url.PathUnescape(id)
	if err != nil {
		return id
	}
	return decoded
}

func purgeCloudTasksQueues(ctx context.Context, cfg *config) *stepResult {
	res := &stepResult{name: "cloud-tasks"}
	if len(cfg.cloudTaskQueues) == 0 {
		res.addf("no queues configured")
		return res
	}
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		res.adderr(fmt.Errorf("init cloud tasks client: %w", err))
		return res
	}
	defer client.Close()

	for _, queue := range cfg.cloudTaskQueues {
		queue = strings.TrimSpace(queue)
		if queue == "" {
			continue
		}
		name := fmt.Sprintf("projects/%s/locations/%s/queues/%s", cfg.projectID, cfg.cloudTasksLocation, queue)
		if cfg.dryRun {
			res.addf("queue=%s action=purge (dry-run)", name)
			continue
		}
		if _, err := client.PurgeQueue(ctx, &taskspb.PurgeQueueRequest{Name: name}); err != nil {
			res.adderr(fmt.Errorf("purge queue=%s: %w", name, err))
			continue
		}
		res.addf("queue=%s purged", name)
	}

	return res
}

func purgePubSubBacklog(ctx context.Context, cfg *config) *stepResult {
	res := &stepResult{name: "pubsub"}
	client, err := pubsub.NewClient(ctx, cfg.projectID)
	if err != nil {
		res.adderr(fmt.Errorf("init pubsub client: %w", err))
		return res
	}
	defer client.Close()

	targetSubs, err := resolvePubSubSubscriptions(ctx, client, cfg.pubSubSubscriptions, cfg.pubSubPrefixes)
	if err != nil {
		res.adderr(err)
		return res
	}
	if len(targetSubs) == 0 {
		res.addf("no subscriptions matched")
		return res
	}

	now := time.Now().UTC()
	for _, subID := range targetSubs {
		sub := client.Subscription(subID)
		if cfg.dryRun {
			res.addf("subscription=%s action=seek_to_now(%s) (dry-run)", subID, now.Format(time.RFC3339))
			continue
		}
		if err := sub.SeekToTime(ctx, now); err != nil {
			res.adderr(fmt.Errorf("subscription=%s seek: %w", subID, err))
			continue
		}
		res.addf("subscription=%s seeked to %s", subID, now.Format(time.RFC3339))
	}

	return res
}

func resolvePubSubSubscriptions(ctx context.Context, client *pubsub.Client, explicitSubs, prefixes []string) ([]string, error) {
	if len(explicitSubs) > 0 {
		subs := make([]string, 0, len(explicitSubs))
		seen := map[string]struct{}{}
		for _, s := range explicitSubs {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			subs = append(subs, s)
		}
		sort.Strings(subs)
		return subs, nil
	}

	subs := make([]string, 0, 32)
	it := client.Subscriptions(ctx)
	for {
		sub, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list subscriptions: %w", err)
		}
		id := strings.TrimSpace(sub.ID())
		if id == "" {
			continue
		}
		if len(prefixes) == 0 {
			subs = append(subs, id)
			continue
		}
		for _, p := range prefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if strings.HasPrefix(id, p) {
				subs = append(subs, id)
				break
			}
		}
	}
	sort.Strings(subs)
	return subs, nil
}

func cleanupBigQuery(ctx context.Context, cfg *config) *stepResult {
	res := &stepResult{name: "bigquery"}
	client, err := bigquery.NewClient(ctx, cfg.bigQueryProject)
	if err != nil {
		res.adderr(fmt.Errorf("init bigquery client: %w", err))
		return res
	}
	defer client.Close()

	tables := cfg.bigQueryTables
	if len(tables) == 0 {
		ds := client.Dataset(cfg.bigQueryDataset)
		it := ds.Tables(ctx)
		for {
			t, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				res.adderr(fmt.Errorf("list tables: %w", err))
				return res
			}
			tables = append(tables, t.TableID)
		}
	}
	if len(tables) == 0 {
		res.addf("dataset=%s has no tables", cfg.bigQueryDataset)
		return res
	}
	sort.Strings(tables)

	for _, table := range tables {
		tableRef := fmt.Sprintf("`%s.%s.%s`", cfg.bigQueryProject, cfg.bigQueryDataset, table)
		query := ""
		if cfg.bigQueryMode == "truncate" {
			query = fmt.Sprintf("TRUNCATE TABLE %s", tableRef)
		} else {
			tenantField, ok, ferr := findTenantField(ctx, client, cfg.bigQueryDataset, table)
			if ferr != nil {
				res.adderr(fmt.Errorf("table=%s inspect schema: %w", table, ferr))
				continue
			}
			if !ok {
				res.addf("table=%s skipped (tenant field not found)", table)
				continue
			}
			query = fmt.Sprintf("DELETE FROM %s WHERE %s = @tenantId", tableRef, tenantField)
		}

		if cfg.dryRun {
			res.addf("table=%s query=%s (dry-run)", table, query)
			continue
		}
		q := client.Query(query)
		if cfg.bigQueryMode == "tenant" {
			q.Parameters = []bigquery.QueryParameter{{Name: "tenantId", Value: cfg.tenant}}
		}
		q.Location = "US"
		job, err := q.Run(ctx)
		if err != nil {
			res.adderr(fmt.Errorf("table=%s run query: %w", table, err))
			continue
		}
		status, err := job.Wait(ctx)
		if err != nil {
			res.adderr(fmt.Errorf("table=%s wait job: %w", table, err))
			continue
		}
		if status.Err() != nil {
			res.adderr(fmt.Errorf("table=%s query failed: %w", table, status.Err()))
			continue
		}
		res.addf("table=%s cleaned (%s)", table, cfg.bigQueryMode)
	}

	return res
}

func findTenantField(ctx context.Context, client *bigquery.Client, dataset, table string) (string, bool, error) {
	meta, err := client.Dataset(dataset).Table(table).Metadata(ctx)
	if err != nil {
		return "", false, err
	}
	for _, field := range meta.Schema {
		n := strings.TrimSpace(field.Name)
		if strings.EqualFold(n, "tenantId") {
			return n, true, nil
		}
		if strings.EqualFold(n, "tenantID") {
			return n, true, nil
		}
	}
	return "", false, nil
}

func discoverCollections(ctx context.Context, client *firestore.Client, selected []string) ([]string, error) {
	if len(selected) > 0 {
		out := make([]string, 0, len(selected))
		seen := map[string]struct{}{}
		for _, c := range selected {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
		sort.Strings(out)
		return out, nil
	}

	it := client.Collections(ctx)
	collections := make([]string, 0, 64)
	for {
		col, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if col != nil {
			collections = append(collections, col.ID)
		}
	}
	sort.Strings(collections)
	return collections, nil
}

func resolveGCSBuckets(projectID, raw string) []string {
	if strings.TrimSpace(raw) != "" {
		return parseCSVSlice(raw)
	}

	candidates := []string{
		strings.TrimSpace(os.Getenv("CORE_DOCS_BUCKET")),
		strings.TrimSpace(os.Getenv("RAG_DOCUMENTS_BUCKET")),
		firstNonEmpty(strings.TrimSpace(os.Getenv("NV_HORIZON_FRAGMENTS_BUCKET")), strings.TrimSpace(os.Getenv("HORIZON_FRAGMENTS_BUCKET"))),
	}
	if projectID != "" {
		candidates = append(candidates,
			projectID+"-core-docs",
			projectID+"-rag-documents",
			projectID+"-nv-horizon-fragments",
		)
	}
	out := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, b := range candidates {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

func resolveVertexStores(raw string) []string {
	if strings.TrimSpace(raw) != "" {
		return parseCSVSlice(raw)
	}
	candidates := []string{
		strings.TrimSpace(os.Getenv("VERTEX_SEARCH_DATA_STORE_ID")),
		strings.TrimSpace(os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID")),
		strings.TrimSpace(os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID")),
	}
	out := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func resolveCloudTaskQueues(raw string) []string {
	if strings.TrimSpace(raw) != "" {
		return parseCSVSlice(raw)
	}
	candidates := []string{
		firstNonEmpty(strings.TrimSpace(os.Getenv("CLOUD_TASKS_SCOUT_QUEUE")), "nv-horizon-scout"),
		firstNonEmpty(strings.TrimSpace(os.Getenv("CLOUD_TASKS_RISK_QUEUE")), "nv-horizon-risk"),
		firstNonEmpty(strings.TrimSpace(os.Getenv("CLOUD_TASKS_PROFILE_QUEUE")), "nv-horizon-profile-refresh"),
	}
	out := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, q := range candidates {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		if _, ok := seen[q]; ok {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	sort.Strings(out)
	return out
}

func extractStringField(m map[string]interface{}, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := toString(v)
			if strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

func setToSortedSlice(s stringSet) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func max(a, b int) int {
	if a >= b {
		return a
	}
	return b
}
