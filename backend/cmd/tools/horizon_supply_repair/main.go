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

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

const toolName = "tool:horizon_supply_repair"

var (
	projectID              = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID               = flag.String("tenant", "global", "tenant id")
	mode                   = flag.String("mode", "audit", "mode: audit | repair | run-batches | search | top-anchors | query-products | query-facility")
	timeoutHours           = flag.Int("timeout-hours", 24, "context timeout in hours")
	apply                  = flag.Bool("apply", false, "apply writes in repair mode")
	skipAudit              = flag.Bool("skip-audit", false, "skip pre-repair audit summary in repair mode")
	rebuildUpstream        = flag.Bool("rebuild-upstream", true, "rebuild multi-hop upstream supply-chain paths for focused customer refs in repair mode")
	maxDepth               = flag.Int("max-depth", 6, "max depth for upstream path rebuild")
	maxPaths               = flag.Int("max-paths", 12, "max number of upstream paths per focused ref")
	createMissingFragments = flag.Bool("create-missing-fragments", false, "create minimal fragments for missing canonical paths referenced by supply evidence")
	pageSize               = flag.Int("page-size", 400, "scan page size")
	limit                  = flag.Int("limit", 0, "max records to scan per source kind (0 = no limit)")
	sampleLimit            = flag.Int("samples", 12, "sample rows to print in audit mode")
	searchQuery            = flag.String("query", "", "search query for mode=search")
	searchLimit            = flag.Int("search-limit", 10, "max search results")
	topCount               = flag.Int("top", 50, "row count for mode=top-anchors")
	anchorRole             = flag.String("anchor-role", "both", "mode=top-anchors: supplier | customer | both")
	focusRefsCSV           = flag.String("refs", "", "comma-separated canonical paths to focus targeted evidence collection on")
	refsFile               = flag.String("refs-file", "", "newline- or comma-separated canonical paths for mode=run-batches")
	expandOwnedFacilities  = flag.Bool("expand-owned-facilities", false, "expand focused organization refs to owned/controlled facilities")
	batchSize              = flag.Int("batch-size", 10, "mode=run-batches: number of seed refs per batch")
	maxBatches             = flag.Int("max-batches", 0, "mode=run-batches: optional cap on executed batches")
	resumeState            = flag.Bool("resume", true, "mode=run-batches: resume from existing state file when present")
	stateFile              = flag.String("state-file", "", "mode=run-batches: JSON progress file path")
	progressEvery          = flag.Int("progress-every", 25, "mode=run-batches: emit intra-batch progress every N triads")
	productsCSV            = flag.String("products", "", "comma-separated product canonical paths for mode=query-products")
	customersCSV           = flag.String("customers", "", "optional comma-separated customer canonical paths for mode=query-products")
	facilityPath           = flag.String("facility", "", "facility canonical path for mode=query-facility")
	ownerPath              = flag.String("owner", "", "optional owner canonical path for mode=query-facility")
)

type runner struct {
	ctx           context.Context
	db            *firestore.Client
	tenantID      string
	fragmentsRepo *repo.HorizonFragmentsRepository
	edgesRepo     *repo.DependencyEdgesRepository
	artifactsRepo *repo.MaterializedArtifactsRepository
	pathBuilder   *pathbuilder.SupplyChainPathBuilder
	pathResolver  *scout.CanonicalPathResolver

	fragmentByPath   map[string]*models.HorizonFragment
	fragmentByID     map[string]*models.HorizonFragment
	resolvedPathByIn map[string]string
	edgeExistsCache  map[string]bool
	fragmentsCreated int
}

type runBatchesOptions struct {
	StateFile              string
	Resume                 bool
	BatchSize              int
	MaxBatches             int
	ExpandOwnedFacilities  bool
	SkipAudit              bool
	ApplyWrites            bool
	CreateMissingFragments bool
	RebuildUpstream        bool
	MaxDepth               int
	MaxPaths               int
	Limit                  int
	PageSize               int
	SampleLimit            int
	ProgressEvery          int
}

type supplyEvidence struct {
	Key                 string
	SupplierRef         string
	ProductRef          string
	CustomerRef         string
	SupplierFacilityRef string
	CustomerFacilityRef string
	TransportViaRefs    []string
	Confidence          float64
	SourceKinds         map[string]int
	SourceIDs           []string
	Metrics             map[string]models.MetricValue
	SeenSupplyLink      bool
	SeenArtifact        bool
	SeenDirectActorEdge bool
}

type auditSummary struct {
	TenantID             string   `json:"tenantId"`
	Triads               int      `json:"triads"`
	FromSupplyLinks      int      `json:"fromSupplyLinks"`
	FromArtifacts        int      `json:"fromArtifacts"`
	FromActorEdges       int      `json:"fromActorEdges"`
	WithDirectActorEdge  int      `json:"withDirectActorEdge"`
	WithSupplyLink       int      `json:"withSupplyLink"`
	WithArtifact         int      `json:"withArtifact"`
	MissingProduces      int      `json:"missingProduces"`
	MissingProductSupply int      `json:"missingProductSupply"`
	MissingActorSupply   int      `json:"missingActorSupply"`
	MissingSupplyLink    int      `json:"missingSupplyLink"`
	MissingArtifact      int      `json:"missingArtifact"`
	MissingEndpoints     int      `json:"missingEndpoints"`
	Samples              []string `json:"samples,omitempty"`
}

type repairStats struct {
	TriadsScanned            int `json:"triadsScanned"`
	MissingEndpoints         int `json:"missingEndpoints"`
	ProducesCreated          int `json:"producesCreated"`
	ProductSuppliesCreated   int `json:"productSuppliesCreated"`
	ActorSuppliesCreated     int `json:"actorSuppliesCreated"`
	SupplyLinksCreated       int `json:"supplyLinksCreated"`
	SupplyLinksUpdated       int `json:"supplyLinksUpdated"`
	ArtifactsCreatedOrMerged int `json:"artifactsCreatedOrMerged"`
	UpstreamPathsRebuilt     int `json:"upstreamPathsRebuilt"`
	UpstreamPathErrors       int `json:"upstreamPathErrors"`
	FragmentsCreated         int `json:"fragmentsCreated"`
	Errors                   int `json:"errors"`
}

type productQueryRow struct {
	ProductRef string         `json:"productRef"`
	Suppliers  []string       `json:"suppliers"`
	Customers  []string       `json:"customers"`
	Triads     int            `json:"triads"`
	Sources    map[string]int `json:"sources"`
	Samples    []string       `json:"samples,omitempty"`
}

type facilityQueryResponse struct {
	FacilityPath     string   `json:"facilityPath"`
	OwnerPath        string   `json:"ownerPath,omitempty"`
	OwnerMatches     bool     `json:"ownerMatches"`
	ObservedProducts []string `json:"observedProducts"`
	ProducesEdges    []string `json:"producesEdges"`
	SupplyEvidence   []string `json:"supplyEvidence"`
	IncomingOwners   []string `json:"incomingOwners,omitempty"`
}

type anchorCountRow struct {
	CanonicalPath string   `json:"canonicalPath"`
	Count         int      `json:"count"`
	Roles         []string `json:"roles"`
}

type anchorCounter struct {
	Count int
	Roles map[string]struct{}
}

type repairProgress struct {
	Processed int          `json:"processed"`
	Total     int          `json:"total"`
	Stats     *repairStats `json:"stats,omitempty"`
}

type batchRunSummary struct {
	BatchIndex    int           `json:"batchIndex"`
	Status        string        `json:"status"`
	SeedRefs      []string      `json:"seedRefs,omitempty"`
	ResolvedRefs  []string      `json:"resolvedRefs,omitempty"`
	Triads        int           `json:"triads"`
	Audit         *auditSummary `json:"audit,omitempty"`
	Stats         *repairStats  `json:"stats,omitempty"`
	Error         string        `json:"error,omitempty"`
	StartedAt     time.Time     `json:"startedAt"`
	LastUpdatedAt time.Time     `json:"lastUpdatedAt"`
	FinishedAt    time.Time     `json:"finishedAt,omitempty"`
}

type batchRunState struct {
	TenantID          string            `json:"tenantId"`
	Status            string            `json:"status"`
	StartedAt         time.Time         `json:"startedAt"`
	LastHeartbeatAt   time.Time         `json:"lastHeartbeatAt"`
	FinishedAt        time.Time         `json:"finishedAt,omitempty"`
	SeedRefs          []string          `json:"seedRefs"`
	BatchSize         int               `json:"batchSize"`
	TotalBatches      int               `json:"totalBatches"`
	CompletedBatches  int               `json:"completedBatches"`
	FailedBatches     int               `json:"failedBatches"`
	CurrentBatchIndex int               `json:"currentBatchIndex"`
	CurrentPhase      string            `json:"currentPhase,omitempty"`
	CurrentProgress   *repairProgress   `json:"currentProgress,omitempty"`
	Aggregate         repairStats       `json:"aggregate"`
	Batches           []batchRunSummary `json:"batches,omitempty"`
}

func main() {
	flag.Parse()

	timeout := 24 * time.Hour
	if timeoutHours != nil && *timeoutHours > 0 {
		timeout = time.Duration(*timeoutHours) * time.Hour
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	r := &runner{
		ctx:              ctx,
		db:               db,
		tenantID:         normalizeTenant(*tenantID),
		fragmentsRepo:    repo.NewHorizonFragmentsRepository(db),
		edgesRepo:        repo.NewDependencyEdgesRepository(db),
		artifactsRepo:    repo.NewMaterializedArtifactsRepository(db),
		fragmentByPath:   make(map[string]*models.HorizonFragment, 2048),
		fragmentByID:     make(map[string]*models.HorizonFragment, 2048),
		resolvedPathByIn: make(map[string]string, 4096),
		edgeExistsCache:  make(map[string]bool, 4096),
	}
	r.pathBuilder = pathbuilder.NewSupplyChainPathBuilder(r.fragmentsRepo, r.edgesRepo, r.artifactsRepo)
	r.pathResolver = scout.NewCanonicalPathResolver(r.fragmentsRepo, nil, scout.NewOrgDeduplicator(r.fragmentsRepo, nil))

	focusRefs, err := r.resolveFocusRefs(normalizeCSVPaths(*focusRefsCSV), *expandOwnedFacilities)
	if err != nil {
		log.Fatal(err)
	}

	switch strings.TrimSpace(*mode) {
	case "audit":
		evidence, err := r.loadEvidence(focusRefs, *limit, *pageSize)
		if err != nil {
			log.Fatal(err)
		}
		summary, err := r.auditEvidence(evidence, *sampleLimit)
		if err != nil {
			log.Fatal(err)
		}
		printAuditSummary(summary)
	case "repair":
		evidence, err := r.loadEvidence(focusRefs, *limit, *pageSize)
		if err != nil {
			log.Fatal(err)
		}
		if !*skipAudit {
			summary, err := r.auditEvidence(evidence, *sampleLimit)
			if err != nil {
				log.Fatal(err)
			}
			printAuditSummary(summary)
		}
		stats, err := r.repairEvidence(evidence, *apply, *createMissingFragments)
		if err != nil {
			log.Fatal(err)
		}
		if *rebuildUpstream && len(focusRefs) > 0 {
			rebuilt, rebuildErrs := r.rebuildUpstreamPaths(focusRefs, *apply, *maxDepth, *maxPaths)
			stats.UpstreamPathsRebuilt += rebuilt
			stats.UpstreamPathErrors += rebuildErrs
			stats.Errors += rebuildErrs
		}
		out, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(out))
	case "run-batches":
		seedRefs, err := loadSeedRefs(*refsFile, *focusRefsCSV)
		if err != nil {
			log.Fatal(err)
		}
		if len(seedRefs) == 0 {
			log.Fatal("--refs or --refs-file is required for mode=run-batches")
		}
		runStatePath := strings.TrimSpace(*stateFile)
		if runStatePath == "" {
			runStatePath = fmt.Sprintf("/tmp/horizon_supply_repair_%s_batches.json", r.tenantID)
		}
		state, err := r.runBatches(seedRefs, runBatchesOptions{
			StateFile:              runStatePath,
			Resume:                 *resumeState,
			BatchSize:              *batchSize,
			MaxBatches:             *maxBatches,
			ExpandOwnedFacilities:  *expandOwnedFacilities,
			SkipAudit:              *skipAudit,
			ApplyWrites:            *apply,
			CreateMissingFragments: *createMissingFragments,
			RebuildUpstream:        *rebuildUpstream,
			MaxDepth:               *maxDepth,
			MaxPaths:               *maxPaths,
			Limit:                  *limit,
			PageSize:               *pageSize,
			SampleLimit:            *sampleLimit,
			ProgressEvery:          *progressEvery,
		})
		if err != nil {
			log.Fatal(err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(state)
	case "search":
		if strings.TrimSpace(*searchQuery) == "" {
			log.Fatal("--query is required for mode=search")
		}
		results, err := r.fragmentsRepo.SearchPrefix(ctx, strings.TrimSpace(*searchQuery), *searchLimit)
		if err != nil {
			log.Fatal(err)
		}
		type searchRow struct {
			ID            string `json:"id"`
			Type          string `json:"type"`
			CanonicalPath string `json:"canonicalPath"`
			Label         string `json:"label"`
		}
		rows := make([]searchRow, 0, len(results))
		for _, result := range results {
			if result == nil {
				continue
			}
			rows = append(rows, searchRow{
				ID:            strings.TrimSpace(result.ID),
				Type:          string(result.Type),
				CanonicalPath: normalizePath(result.CanonicalPath),
				Label:         firstNonEmpty(result.Labels["en"], result.Identifiers["legalName"], result.Identifiers["shortName"]),
			})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	case "top-anchors":
		rows, err := r.topAnchors(*topCount, strings.TrimSpace(*anchorRole), *limit, *pageSize)
		if err != nil {
			log.Fatal(err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	case "query-products":
		productPaths := normalizeCSVPaths(*productsCSV)
		if len(productPaths) == 0 {
			log.Fatal("--products is required for mode=query-products")
		}
		customerPaths := make(map[string]struct{}, len(normalizeCSVPaths(*customersCSV)))
		for _, path := range normalizeCSVPaths(*customersCSV) {
			customerPaths[path] = struct{}{}
		}
		queryRefs := mergeStrings(focusRefs, productPaths)
		queryRefs = mergeStrings(queryRefs, normalizeCSVPaths(*customersCSV))
		evidence, err := r.loadEvidence(queryRefs, *limit, *pageSize)
		if err != nil {
			log.Fatal(err)
		}
		rows := r.queryProducts(evidence, productPaths, customerPaths)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	case "query-facility":
		if strings.TrimSpace(*facilityPath) == "" {
			log.Fatal("--facility is required for mode=query-facility")
		}
		queryRefs := mergeStrings(focusRefs, []string{strings.TrimSpace(*facilityPath), strings.TrimSpace(*ownerPath)})
		if len(focusRefs) == 0 {
			queryRefs, err = r.resolveFocusRefs(queryRefs, true)
			if err != nil {
				log.Fatal(err)
			}
		}
		evidence, err := r.loadEvidence(queryRefs, *limit, *pageSize)
		if err != nil {
			log.Fatal(err)
		}
		resp, err := r.queryFacility(strings.TrimSpace(*facilityPath), strings.TrimSpace(*ownerPath), evidence)
		if err != nil {
			log.Fatal(err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
	default:
		log.Fatalf("unsupported mode=%q", *mode)
	}
}

func (r *runner) loadEvidence(focusRefs []string, limit, pageSize int) (map[string]*supplyEvidence, error) {
	if len(focusRefs) > 0 {
		return r.collectEvidenceForRefs(focusRefs, limit)
	}
	return r.collectEvidence(limit, pageSize)
}

func (r *runner) collectEvidence(limit, pageSize int) (map[string]*supplyEvidence, error) {
	out := make(map[string]*supplyEvidence, 4096)

	if err := r.scanSupplyLinkFragments(out, limit, pageSize); err != nil {
		return nil, fmt.Errorf("scan supply links: %w", err)
	}
	if err := r.scanActorEdges(out, models.DependencyTypeSuppliesTo, limit, pageSize); err != nil {
		return nil, fmt.Errorf("scan actor supplies edges: %w", err)
	}
	if err := r.scanActorEdges(out, models.DependencyTypeDependsOn, limit, pageSize); err != nil {
		return nil, fmt.Errorf("scan actor depends edges: %w", err)
	}
	if err := r.scanArtifacts(out, limit, pageSize); err != nil {
		return nil, fmt.Errorf("scan artifacts: %w", err)
	}

	return out, nil
}

func (r *runner) collectEvidenceForRefs(refs []string, limit int) (map[string]*supplyEvidence, error) {
	out := make(map[string]*supplyEvidence, 256)
	refs = compactStrings(refs)
	if len(refs) == 0 {
		return out, nil
	}
	if err := r.scanFocusedSupplyLinks(out, refs, limit); err != nil {
		return nil, fmt.Errorf("scan focused supply links: %w", err)
	}
	if err := r.scanFocusedActorEdges(out, refs, limit); err != nil {
		return nil, fmt.Errorf("scan focused actor edges: %w", err)
	}
	if err := r.scanFocusedArtifacts(out, refs, limit); err != nil {
		return nil, fmt.Errorf("scan focused artifacts: %w", err)
	}
	return out, nil
}

func (r *runner) resolveFocusRefs(seed []string, expandOwnedFacilities bool) ([]string, error) {
	refs := compactStrings(seed)
	if !expandOwnedFacilities || len(refs) == 0 {
		return refs, nil
	}
	expanded := append([]string(nil), refs...)
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "/org/") && !strings.HasPrefix(ref, "/gov/") {
			continue
		}
		frag, err := r.getFragmentByPath(ref)
		if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		for _, edgeType := range []models.DependencyType{
			models.DependencyTypeOwns,
			models.DependencyTypeControls,
			models.DependencyTypeManages,
			models.DependencyTypeAdministers,
		} {
			edges, err := r.edgesRepo.ListBySource(r.ctx, r.tenantID, frag.ID, &edgeType, 200)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if edge == nil {
					continue
				}
				targetFrag, _ := r.getFragmentByID(edge.TargetFragmentID)
				if targetFrag == nil {
					continue
				}
				targetPath := normalizePath(targetFrag.CanonicalPath)
				if strings.HasPrefix(targetPath, "/fac/") {
					expanded = append(expanded, targetPath)
				}
			}
		}
	}
	return compactStrings(expanded), nil
}

func (r *runner) scanSupplyLinkFragments(out map[string]*supplyEvidence, limit, pageSize int) error {
	base := r.db.Collection("horizon_fragments").
		Where("tenantId", "==", r.tenantID).
		Where("type", "==", string(models.FragmentTypeSupplyLink)).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if pageSize <= 0 {
		pageSize = 400
	}
	scanned := 0
	lastDocID := ""
	for {
		q := base.Limit(pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		docs, err := q.Documents(r.ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}
		lastDocID = docs[len(docs)-1].Ref.ID
		for _, doc := range docs {
			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}
			scanned++
			if limit > 0 && scanned > limit {
				return nil
			}
			ev := supplyEvidence{
				SupplierRef:         deref(frag.SupplierRef),
				ProductRef:          deref(frag.ProductRef),
				CustomerRef:         deref(frag.CustomerRef),
				SupplierFacilityRef: deref(frag.SupplierFacilityRef),
				CustomerFacilityRef: deref(frag.CustomerFacilityRef),
				TransportViaRefs:    normalizePaths(frag.TransportViaRefs),
				Confidence:          metricValue(frag.State, "confidence", 0.5),
				Metrics:             cloneMetrics(frag.State),
			}
			r.addEvidence(out, ev, "supply_link", firstNonEmpty(frag.ID, doc.Ref.ID), true, false, false)
		}
		if len(docs) < pageSize {
			break
		}
	}

	return nil
}

func (r *runner) scanActorEdges(out map[string]*supplyEvidence, edgeType models.DependencyType, limit, pageSize int) error {
	base := r.db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", r.tenantID).
		Where("type", "==", string(edgeType)).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if pageSize <= 0 {
		pageSize = 400
	}
	scanned := 0
	lastDocID := ""
	for {
		q := base.Limit(pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		docs, err := q.Documents(r.ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}
		lastDocID = docs[len(docs)-1].Ref.ID
		for _, doc := range docs {
			var edge models.DependencyEdge
			if err := doc.DataTo(&edge); err != nil {
				continue
			}
			scanned++
			if limit > 0 && scanned > limit {
				return nil
			}

			productRef := extractProductRef(edge.Qualifiers)
			if productRef == "" {
				continue
			}
			srcFrag, _ := r.getFragmentByID(strings.TrimSpace(edge.SourceFragmentID))
			dstFrag, _ := r.getFragmentByID(strings.TrimSpace(edge.TargetFragmentID))
			if srcFrag == nil || dstFrag == nil {
				continue
			}
			srcPath := normalizePath(srcFrag.CanonicalPath)
			dstPath := normalizePath(dstFrag.CanonicalPath)
			if !isSupplyActorPath(srcPath) || !isSupplyCustomerPath(dstPath) {
				continue
			}

			ev := supplyEvidence{
				SupplierRef:      srcPath,
				ProductRef:       productRef,
				CustomerRef:      dstPath,
				TransportViaRefs: qualifierStringSlice(edge.Qualifiers, models.QualifierTransportVia),
				Confidence:       edge.Properties.Criticality,
				Metrics:          cloneMetrics(edge.Metrics),
			}
			r.addEvidence(out, ev, "actor_edge_"+string(edgeType), firstNonEmpty(edge.ID, doc.Ref.ID), false, false, true)
		}
		if len(docs) < pageSize {
			break
		}
	}
	return nil
}

func (r *runner) scanArtifacts(out map[string]*supplyEvidence, limit, pageSize int) error {
	base := r.db.Collection("horizon_materialized_artifacts").
		Where("tenantId", "==", r.tenantID).
		Where("kind", "==", string(models.MaterializedKindSupplyChainPath)).
		OrderBy(firestore.DocumentID, firestore.Asc)
	if pageSize <= 0 {
		pageSize = 400
	}
	scanned := 0
	lastDocID := ""
	for {
		q := base.Limit(pageSize)
		if lastDocID != "" {
			q = q.StartAfter(lastDocID)
		}
		docs, err := q.Documents(r.ctx).GetAll()
		if err != nil {
			return err
		}
		if len(docs) == 0 {
			break
		}
		lastDocID = docs[len(docs)-1].Ref.ID
		for _, doc := range docs {
			var artifact models.MaterializedArtifact
			if err := doc.DataTo(&artifact); err != nil {
				continue
			}
			scanned++
			if limit > 0 && scanned > limit {
				return nil
			}
			parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
			if err != nil || parsed == nil {
				continue
			}
			for _, leg := range parsed.Legs {
				ev := supplyEvidence{
					SupplierRef:      normalizePath(leg.SupplierRef),
					ProductRef:       normalizePath(leg.ProductRef),
					CustomerRef:      normalizePath(leg.CustomerRef),
					TransportViaRefs: normalizePaths(leg.TransportViaRefs),
					Confidence:       leg.SegmentConfidence,
				}
				r.addEvidence(out, ev, "artifact_leg", firstNonEmpty(artifact.ID, doc.Ref.ID), false, true, false)
			}
		}
		if len(docs) < pageSize {
			break
		}
	}
	return nil
}

func (r *runner) scanFocusedSupplyLinks(out map[string]*supplyEvidence, refs []string, limit int) error {
	refs = compactStrings(refs)
	if len(refs) == 0 {
		return nil
	}
	docIDs := map[string]struct{}{}
	for _, field := range []string{"supplierRef", "customerRef", "supplierFacilityRef", "customerFacilityRef", "productRef"} {
		if err := r.scanSupplyLinksByField(out, refs, field, limit, docIDs); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) scanSupplyLinksByField(out map[string]*supplyEvidence, refs []string, field string, limit int, docIDs map[string]struct{}) error {
	refs = compactStrings(refs)
	if len(refs) == 0 {
		return nil
	}
	maxRows := limit
	if maxRows <= 0 {
		maxRows = 500
	}
	chunks := chunkStrings(refs, 10)
	for _, chunk := range chunks {
		docs, err := r.db.Collection("horizon_fragments").
			Where("tenantId", "==", r.tenantID).
			Where("type", "==", string(models.FragmentTypeSupplyLink)).
			Where(field, "in", chunk).
			Limit(maxRows).
			Documents(r.ctx).
			GetAll()
		if err != nil {
			fallbackLimit := maxRows * 4
			if fallbackLimit < 200 {
				fallbackLimit = 200
			}
			frags, listErr := r.fragmentsRepo.ListByTypeAndTenant(r.ctx, r.tenantID, models.FragmentTypeSupplyLink, fallbackLimit)
			if listErr != nil {
				return err
			}
			for _, frag := range frags {
				if frag == nil || !supplyLinkFieldMatches(frag, field, refs) {
					continue
				}
				docID := strings.TrimSpace(frag.ID)
				if docID == "" {
					docID = strings.TrimSpace(frag.CanonicalPath)
				}
				if _, seen := docIDs[docID]; seen {
					continue
				}
				docIDs[docID] = struct{}{}
				r.addSupplyLinkFragmentEvidence(out, frag, docID)
				if limit > 0 && len(docIDs) >= limit {
					return nil
				}
			}
			continue
		}
		for _, doc := range docs {
			if _, seen := docIDs[doc.Ref.ID]; seen {
				continue
			}
			docIDs[doc.Ref.ID] = struct{}{}
			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}
			r.addSupplyLinkFragmentEvidence(out, &frag, firstNonEmpty(frag.ID, doc.Ref.ID))
			if limit > 0 && len(docIDs) >= limit {
				return nil
			}
		}
	}
	return nil
}

func (r *runner) addSupplyLinkFragmentEvidence(out map[string]*supplyEvidence, frag *models.HorizonFragment, sourceID string) {
	if frag == nil {
		return
	}
	ev := supplyEvidence{
		SupplierRef:         deref(frag.SupplierRef),
		ProductRef:          deref(frag.ProductRef),
		CustomerRef:         deref(frag.CustomerRef),
		SupplierFacilityRef: deref(frag.SupplierFacilityRef),
		CustomerFacilityRef: deref(frag.CustomerFacilityRef),
		TransportViaRefs:    normalizePaths(frag.TransportViaRefs),
		Confidence:          metricValue(frag.State, "confidence", 0.5),
		Metrics:             cloneMetrics(frag.State),
	}
	r.addEvidence(out, ev, "supply_link", sourceID, true, false, false)
}

func (r *runner) scanFocusedActorEdges(out map[string]*supplyEvidence, refs []string, limit int) error {
	refs = compactStrings(refs)
	if len(refs) == 0 {
		return nil
	}
	seenEdgeIDs := map[string]struct{}{}
	for _, ref := range refs {
		frag, err := r.getFragmentByPath(ref)
		if err != nil || frag == nil || strings.TrimSpace(frag.ID) == "" {
			continue
		}
		for _, edgeType := range []models.DependencyType{
			models.DependencyTypeSuppliesTo,
			models.DependencyTypeDependsOn,
		} {
			srcEdges, err := r.edgesRepo.ListBySource(r.ctx, r.tenantID, frag.ID, &edgeType, focusedEdgeLimit(limit))
			if err == nil {
				for _, edge := range srcEdges {
					if r.addActorEdgeEvidence(out, edge, seenEdgeIDs) && limit > 0 && len(seenEdgeIDs) >= limit {
						return nil
					}
				}
			}
			tgtEdges, err := r.edgesRepo.ListByTarget(r.ctx, r.tenantID, frag.ID, &edgeType, focusedEdgeLimit(limit))
			if err == nil {
				for _, edge := range tgtEdges {
					if r.addActorEdgeEvidence(out, edge, seenEdgeIDs) && limit > 0 && len(seenEdgeIDs) >= limit {
						return nil
					}
				}
			}
		}
	}
	return nil
}

func (r *runner) addActorEdgeEvidence(out map[string]*supplyEvidence, edge *models.DependencyEdge, seenEdgeIDs map[string]struct{}) bool {
	if edge == nil {
		return false
	}
	edgeID := strings.TrimSpace(edge.ID)
	if edgeID == "" {
		edgeID = fmt.Sprintf("%s|%s|%s", edge.SourceFragmentID, edge.TargetFragmentID, edge.Type)
	}
	if _, seen := seenEdgeIDs[edgeID]; seen {
		return false
	}
	productRef := extractProductRef(edge.Qualifiers)
	if productRef == "" {
		return false
	}
	srcFrag, _ := r.getFragmentByID(strings.TrimSpace(edge.SourceFragmentID))
	dstFrag, _ := r.getFragmentByID(strings.TrimSpace(edge.TargetFragmentID))
	if srcFrag == nil || dstFrag == nil {
		return false
	}
	srcPath := normalizePath(srcFrag.CanonicalPath)
	dstPath := normalizePath(dstFrag.CanonicalPath)
	if !isSupplyActorPath(srcPath) || !isSupplyCustomerPath(dstPath) {
		return false
	}
	seenEdgeIDs[edgeID] = struct{}{}
	ev := supplyEvidence{
		SupplierRef:      srcPath,
		ProductRef:       productRef,
		CustomerRef:      dstPath,
		TransportViaRefs: qualifierStringSlice(edge.Qualifiers, models.QualifierTransportVia),
		Confidence:       edge.Properties.Criticality,
		Metrics:          cloneMetrics(edge.Metrics),
	}
	r.addEvidence(out, ev, "actor_edge_"+string(edge.Type), edgeID, false, false, true)
	return true
}

func (r *runner) scanFocusedArtifacts(out map[string]*supplyEvidence, refs []string, limit int) error {
	refs = compactStrings(refs)
	if len(refs) == 0 {
		return nil
	}
	maxRows := limit
	if maxRows <= 0 {
		maxRows = 300
	}
	artifacts, err := r.artifactsRepo.FindByNodeRefsAny(r.ctx, r.tenantID, refs, models.MaterializedKindSupplyChainPath, maxRows)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			ev := supplyEvidence{
				SupplierRef:      normalizePath(leg.SupplierRef),
				ProductRef:       normalizePath(leg.ProductRef),
				CustomerRef:      normalizePath(leg.CustomerRef),
				TransportViaRefs: normalizePaths(leg.TransportViaRefs),
				Confidence:       leg.SegmentConfidence,
			}
			r.addEvidence(out, ev, "artifact_leg", strings.TrimSpace(artifact.ID), false, true, false)
		}
	}
	return nil
}

func (r *runner) addEvidence(out map[string]*supplyEvidence, raw supplyEvidence, sourceKind, sourceID string, supplyLink, artifact, actorEdge bool) {
	ev := normalizeEvidence(r.canonicalizeEvidence(raw))
	if ev.Key == "" {
		return
	}
	agg := out[ev.Key]
	if agg == nil {
		agg = &ev
		agg.SourceKinds = map[string]int{}
		agg.SourceIDs = nil
		out[ev.Key] = agg
	}
	if agg.SupplierRef == "" {
		agg.SupplierRef = ev.SupplierRef
	}
	if agg.ProductRef == "" {
		agg.ProductRef = ev.ProductRef
	}
	if agg.CustomerRef == "" {
		agg.CustomerRef = ev.CustomerRef
	}
	if agg.SupplierFacilityRef == "" {
		agg.SupplierFacilityRef = ev.SupplierFacilityRef
	}
	if agg.CustomerFacilityRef == "" {
		agg.CustomerFacilityRef = ev.CustomerFacilityRef
	}
	agg.TransportViaRefs = mergeStrings(agg.TransportViaRefs, ev.TransportViaRefs)
	if ev.Confidence > agg.Confidence {
		agg.Confidence = ev.Confidence
	}
	if agg.Metrics == nil {
		agg.Metrics = map[string]models.MetricValue{}
	}
	for k, v := range ev.Metrics {
		agg.Metrics[k] = v
	}
	agg.SourceKinds[sourceKind]++
	if sourceID != "" && !contains(agg.SourceIDs, sourceID) && len(agg.SourceIDs) < 6 {
		agg.SourceIDs = append(agg.SourceIDs, sourceID)
	}
	agg.SeenSupplyLink = agg.SeenSupplyLink || supplyLink
	agg.SeenArtifact = agg.SeenArtifact || artifact
	agg.SeenDirectActorEdge = agg.SeenDirectActorEdge || actorEdge
}

func normalizeEvidence(in supplyEvidence) supplyEvidence {
	in.SupplierRef = normalizePath(in.SupplierRef)
	in.ProductRef = normalizePath(in.ProductRef)
	in.CustomerRef = normalizePath(in.CustomerRef)
	in.SupplierFacilityRef = normalizePath(in.SupplierFacilityRef)
	in.CustomerFacilityRef = normalizePath(in.CustomerFacilityRef)
	in.TransportViaRefs = normalizePaths(in.TransportViaRefs)
	if in.SupplierRef == "" || in.ProductRef == "" || in.CustomerRef == "" {
		in.Key = ""
		return in
	}
	if in.SupplierRef == in.CustomerRef {
		in.Key = ""
		return in
	}
	in.Key = strings.ToLower(strings.Join([]string{in.SupplierRef, in.ProductRef, in.CustomerRef}, "|"))
	if in.Confidence <= 0 {
		in.Confidence = 0.5
	}
	return in
}

func (r *runner) canonicalizeEvidence(in supplyEvidence) supplyEvidence {
	in.SupplierRef = r.resolveCanonicalRef(in.SupplierRef)
	in.ProductRef = r.resolveCanonicalRef(in.ProductRef)
	in.CustomerRef = r.resolveCanonicalRef(in.CustomerRef)
	in.SupplierFacilityRef = r.resolveCanonicalRef(in.SupplierFacilityRef)
	in.CustomerFacilityRef = r.resolveCanonicalRef(in.CustomerFacilityRef)
	if len(in.TransportViaRefs) > 0 {
		rewritten := make([]string, 0, len(in.TransportViaRefs))
		for _, ref := range in.TransportViaRefs {
			if cp := r.resolveCanonicalRef(ref); cp != "" {
				rewritten = append(rewritten, cp)
			}
		}
		in.TransportViaRefs = rewritten
	}
	return in
}

func (r *runner) resolveCanonicalRef(path string) string {
	path = normalizePath(path)
	if path == "" {
		return ""
	}
	if resolved, ok := r.resolvedPathByIn[path]; ok && strings.TrimSpace(resolved) != "" {
		return strings.TrimSpace(resolved)
	}
	if frag := r.fragmentByPath[path]; frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
		resolved := normalizePath(frag.CanonicalPath)
		r.resolvedPathByIn[path] = resolved
		return resolved
	}
	if frag, err := r.fragmentsRepo.GetByCanonicalPath(r.ctx, path); err == nil && frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
		resolved := normalizePath(frag.CanonicalPath)
		r.fragmentByPath[resolved] = frag
		if strings.TrimSpace(frag.ID) != "" {
			r.fragmentByID[frag.ID] = frag
		}
		r.resolvedPathByIn[path] = resolved
		return resolved
	}
	fragmentType := scout.InferFragmentTypeFromCanonicalPath(path)
	if r.pathResolver != nil {
		if resolved, err := r.pathResolver.Resolve(r.ctx, path, fragmentType); err == nil && resolved != nil && strings.TrimSpace(resolved.CanonicalPath) != "" {
			out := normalizePath(resolved.CanonicalPath)
			r.resolvedPathByIn[path] = out
			if resolved.ExistingFragment != nil {
				r.fragmentByPath[out] = resolved.ExistingFragment
				if strings.TrimSpace(resolved.ExistingFragment.ID) != "" {
					r.fragmentByID[resolved.ExistingFragment.ID] = resolved.ExistingFragment
				}
			}
			return out
		}
	}
	r.resolvedPathByIn[path] = path
	return path
}

func (r *runner) auditEvidence(evidence map[string]*supplyEvidence, sampleLimit int) (*auditSummary, error) {
	summary := &auditSummary{
		TenantID: r.tenantID,
		Triads:   len(evidence),
		Samples:  make([]string, 0, sampleLimit),
	}
	keys := sortedEvidenceKeys(evidence)
	for _, key := range keys {
		ev := evidence[key]
		if ev == nil {
			continue
		}
		if ev.SeenSupplyLink {
			summary.FromSupplyLinks++
			summary.WithSupplyLink++
		}
		if ev.SeenArtifact {
			summary.FromArtifacts++
			summary.WithArtifact++
		}
		if ev.SeenDirectActorEdge {
			summary.FromActorEdges++
			summary.WithDirectActorEdge++
		}

		supplierFrag, err := r.getFragmentByPath(ev.SupplierRef)
		if err != nil {
			supplierFrag = nil
		}
		productFrag, err := r.getFragmentByPath(ev.ProductRef)
		if err != nil {
			productFrag = nil
		}
		customerFrag, err := r.getFragmentByPath(ev.CustomerRef)
		if err != nil {
			customerFrag = nil
		}
		if supplierFrag == nil || productFrag == nil || customerFrag == nil {
			summary.MissingEndpoints++
		}

		hasProduces := supplierFrag != nil && productFrag != nil && r.edgeExists(supplierFrag.ID, productFrag.ID, models.DependencyTypeProduces)
		if !hasProduces {
			summary.MissingProduces++
			appendSample(summary, sampleLimit, "missing PRODUCES", ev)
		}

		hasProductSupply := productFrag != nil && customerFrag != nil && r.edgeExists(productFrag.ID, customerFrag.ID, models.DependencyTypeSuppliesTo)
		if !hasProductSupply {
			summary.MissingProductSupply++
			appendSample(summary, sampleLimit, "missing PRODUCT->ACTOR SUPPLIES_TO", ev)
		}

		if !ev.SeenDirectActorEdge {
			summary.MissingActorSupply++
			appendSample(summary, sampleLimit, "missing ACTOR->ACTOR SUPPLIES_TO(productRef)", ev)
		}
		if !ev.SeenSupplyLink {
			summary.MissingSupplyLink++
			appendSample(summary, sampleLimit, "missing SUPPLY_LINK fragment", ev)
		}
		if !ev.SeenArtifact {
			summary.MissingArtifact++
			appendSample(summary, sampleLimit, "missing SUPPLY_CHAIN_PATH artifact", ev)
		}
	}
	return summary, nil
}

func (r *runner) repairEvidence(evidence map[string]*supplyEvidence, applyWrites, createMissing bool) (*repairStats, error) {
	return r.repairEvidenceWithProgress(evidence, applyWrites, createMissing, 0, nil)
}

func (r *runner) repairEvidenceWithProgress(
	evidence map[string]*supplyEvidence,
	applyWrites, createMissing bool,
	progressEvery int,
	progressFn func(repairProgress),
) (*repairStats, error) {
	stats := &repairStats{}
	total := len(evidence)
	keys := sortedEvidenceKeys(evidence)
	for idx, key := range keys {
		ev := evidence[key]
		if ev == nil {
			continue
		}
		stats.TriadsScanned++
		supplierFrag, err := r.getOrCreateFragmentByPath(ev.SupplierRef, createMissing, applyWrites)
		if err != nil {
			stats.Errors++
			continue
		}
		productFrag, err := r.getOrCreateFragmentByPath(ev.ProductRef, createMissing, applyWrites)
		if err != nil {
			stats.Errors++
			continue
		}
		customerFrag, err := r.getOrCreateFragmentByPath(ev.CustomerRef, createMissing, applyWrites)
		if err != nil {
			stats.Errors++
			continue
		}
		if supplierFrag == nil || productFrag == nil || customerFrag == nil {
			stats.MissingEndpoints++
			continue
		}

		if created, err := r.ensureProducesEdge(supplierFrag, productFrag, ev, applyWrites); err != nil {
			stats.Errors++
		} else if created {
			stats.ProducesCreated++
		}
		if created, err := r.ensureProductSuppliesEdge(productFrag, customerFrag, ev, applyWrites); err != nil {
			stats.Errors++
		} else if created {
			stats.ProductSuppliesCreated++
		}
		if created, err := r.ensureActorSuppliesEdge(supplierFrag, customerFrag, ev, applyWrites); err != nil {
			stats.Errors++
		} else if created {
			stats.ActorSuppliesCreated++
		}
		created, updated, err := r.ensureSupplyLink(ev, applyWrites)
		if err != nil {
			stats.Errors++
		} else {
			if created {
				stats.SupplyLinksCreated++
			}
			if updated {
				stats.SupplyLinksUpdated++
			}
		}
		if changed, err := r.ensureArtifact(ev, applyWrites); err != nil {
			stats.Errors++
		} else if changed {
			stats.ArtifactsCreatedOrMerged++
		}
		if progressFn != nil && shouldEmitProgress(idx+1, total, progressEvery) {
			progressFn(repairProgress{
				Processed: idx + 1,
				Total:     total,
				Stats:     cloneRepairStats(stats),
			})
		}
	}
	stats.FragmentsCreated = r.fragmentsCreated
	if progressFn != nil {
		progressFn(repairProgress{
			Processed: total,
			Total:     total,
			Stats:     cloneRepairStats(stats),
		})
		return stats, nil
	}
	return stats, nil
}

func shouldEmitProgress(processed, total, every int) bool {
	if processed <= 0 || total <= 0 {
		return false
	}
	if processed == total {
		return true
	}
	if every <= 0 {
		return false
	}
	return processed%every == 0
}

func cloneRepairStats(in *repairStats) *repairStats {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (r *runner) rebuildUpstreamPaths(refs []string, applyWrites bool, maxDepth, maxPaths int) (int, int) {
	if len(refs) == 0 {
		return 0, 0
	}
	opts := pathbuilder.DefaultBuildPathOptions()
	if maxDepth > 0 {
		opts.MaxDepth = maxDepth
	}
	if maxPaths > 0 {
		opts.MaxPaths = maxPaths
	}
	opts.TenantID = r.tenantID

	rebuilt := 0
	errs := 0
	seenKeys := map[string]struct{}{}
	for _, ref := range compactStrings(refs) {
		ref = normalizePath(ref)
		if !isSupplyCustomerPath(ref) {
			continue
		}
		paths, err := r.pathBuilder.BuildUpstreamPaths(r.ctx, ref, opts)
		if err != nil {
			errs++
			continue
		}
		for _, path := range paths {
			if path == nil || len(path.Segments) == 0 {
				continue
			}
			key := pathbuilder.BuildStructuralCanonicalKey(r.tenantID, path.Segments)
			if key == "" {
				continue
			}
			if _, seen := seenKeys[key]; seen {
				continue
			}
			seenKeys[key] = struct{}{}
			if !applyWrites {
				rebuilt++
				continue
			}
			if _, err := r.pathBuilder.MaterializePath(r.ctx, path, r.tenantID, "", key); err != nil {
				errs++
				continue
			}
			rebuilt++
		}
	}
	return rebuilt, errs
}

func (r *runner) queryProducts(evidence map[string]*supplyEvidence, products []string, customerFilter map[string]struct{}) []productQueryRow {
	want := map[string]struct{}{}
	for _, product := range products {
		want[normalizePath(product)] = struct{}{}
	}

	rows := make([]productQueryRow, 0, len(want))
	for product := range want {
		suppliers := map[string]struct{}{}
		customers := map[string]struct{}{}
		sources := map[string]int{}
		samples := make([]string, 0, 6)
		triads := 0
		for _, ev := range evidence {
			if ev == nil || ev.ProductRef != product {
				continue
			}
			if len(customerFilter) > 0 {
				if _, ok := customerFilter[ev.CustomerRef]; !ok {
					continue
				}
			}
			triads++
			suppliers[ev.SupplierRef] = struct{}{}
			customers[ev.CustomerRef] = struct{}{}
			for kind, n := range ev.SourceKinds {
				sources[kind] += n
			}
			if len(samples) < 6 {
				samples = append(samples, ev.SupplierRef+" -> "+ev.CustomerRef)
			}
		}
		rows = append(rows, productQueryRow{
			ProductRef: product,
			Suppliers:  sortedSet(suppliers),
			Customers:  sortedSet(customers),
			Triads:     triads,
			Sources:    sources,
			Samples:    samples,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ProductRef < rows[j].ProductRef })
	return rows
}

func (r *runner) queryFacility(facility, owner string, evidence map[string]*supplyEvidence) (*facilityQueryResponse, error) {
	facility = normalizePath(facility)
	owner = normalizePath(owner)
	facFrag, err := r.getFragmentByPath(facility)
	if err != nil || facFrag == nil {
		return nil, fmt.Errorf("facility not found: %s", facility)
	}

	resp := &facilityQueryResponse{
		FacilityPath:     facility,
		OwnerPath:        owner,
		ObservedProducts: []string{},
		ProducesEdges:    []string{},
		SupplyEvidence:   []string{},
		IncomingOwners:   []string{},
	}

	productSet := map[string]struct{}{}
	producesType := models.DependencyTypeProduces
	outEdges, err := r.edgesRepo.ListBySource(r.ctx, r.tenantID, facFrag.ID, &producesType, 200)
	if err == nil {
		for _, edge := range outEdges {
			if edge == nil {
				continue
			}
			targetFrag, _ := r.getFragmentByID(edge.TargetFragmentID)
			if targetFrag == nil || normalizePath(targetFrag.CanonicalPath) == "" {
				continue
			}
			cp := normalizePath(targetFrag.CanonicalPath)
			productSet[cp] = struct{}{}
			resp.ProducesEdges = append(resp.ProducesEdges, cp)
		}
	}

	for _, ev := range evidence {
		if ev == nil {
			continue
		}
		if ev.SupplierRef == facility || ev.SupplierFacilityRef == facility {
			productSet[ev.ProductRef] = struct{}{}
			if len(resp.SupplyEvidence) < 20 {
				resp.SupplyEvidence = append(resp.SupplyEvidence, ev.ProductRef+" -> "+ev.CustomerRef)
			}
		}
	}

	ownerTypes := []models.DependencyType{
		models.DependencyTypeOwns,
		models.DependencyTypeControls,
		models.DependencyTypeOperatesIn,
		models.DependencyTypeAdministers,
	}
	for _, edgeType := range ownerTypes {
		edges, err := r.edgesRepo.ListByTarget(r.ctx, r.tenantID, facFrag.ID, &edgeType, 50)
		if err != nil {
			continue
		}
		for _, edge := range edges {
			if edge == nil {
				continue
			}
			srcFrag, _ := r.getFragmentByID(edge.SourceFragmentID)
			if srcFrag == nil {
				continue
			}
			cp := normalizePath(srcFrag.CanonicalPath)
			if cp == "" {
				continue
			}
			resp.IncomingOwners = mergeStrings(resp.IncomingOwners, []string{cp})
			if owner != "" && cp == owner {
				resp.OwnerMatches = true
			}
		}
	}
	resp.ObservedProducts = sortedSet(productSet)
	sort.Strings(resp.ProducesEdges)
	sort.Strings(resp.SupplyEvidence)
	sort.Strings(resp.IncomingOwners)
	return resp, nil
}

func (r *runner) topAnchors(topN int, role string, limit, pageSize int) ([]anchorCountRow, error) {
	evidence, err := r.loadEvidence(nil, limit, pageSize)
	if err != nil {
		return nil, err
	}
	return summarizeTopAnchors(evidence, topN, role), nil
}

func summarizeTopAnchors(evidence map[string]*supplyEvidence, topN int, role string) []anchorCountRow {
	role = normalizeAnchorRole(role)
	counts := make(map[string]*anchorCounter, len(evidence))
	for _, ev := range evidence {
		if ev == nil {
			continue
		}
		if role == "supplier" || role == "both" {
			addAnchorCount(counts, ev.SupplierRef, "supplier")
			addAnchorCount(counts, ev.SupplierFacilityRef, "supplier_facility")
		}
		if role == "customer" || role == "both" {
			addAnchorCount(counts, ev.CustomerRef, "customer")
			addAnchorCount(counts, ev.CustomerFacilityRef, "customer_facility")
		}
	}

	rows := make([]anchorCountRow, 0, len(counts))
	for canonicalPath, counter := range counts {
		if counter == nil || counter.Count == 0 || canonicalPath == "" {
			continue
		}
		rows = append(rows, anchorCountRow{
			CanonicalPath: canonicalPath,
			Count:         counter.Count,
			Roles:         sortedSet(counter.Roles),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].CanonicalPath < rows[j].CanonicalPath
		}
		return rows[i].Count > rows[j].Count
	})
	if topN > 0 && len(rows) > topN {
		rows = rows[:topN]
	}
	return rows
}

func addAnchorCount(counts map[string]*anchorCounter, canonicalPath, role string) {
	canonicalPath = normalizePath(canonicalPath)
	role = strings.TrimSpace(role)
	if canonicalPath == "" || role == "" {
		return
	}
	counter := counts[canonicalPath]
	if counter == nil {
		counter = &anchorCounter{Roles: map[string]struct{}{}}
		counts[canonicalPath] = counter
	}
	counter.Count++
	counter.Roles[role] = struct{}{}
}

func normalizeAnchorRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "supplier", "customer", "both":
		return strings.ToLower(strings.TrimSpace(role))
	default:
		return "both"
	}
}

func (r *runner) runBatches(seedRefs []string, opts runBatchesOptions) (*batchRunState, error) {
	seedRefs = compactStrings(seedRefs)
	if len(seedRefs) == 0 {
		return nil, fmt.Errorf("seed refs are required")
	}
	batches := chunkStrings(seedRefs, opts.BatchSize)
	if len(batches) == 0 {
		return nil, fmt.Errorf("no batches to run")
	}

	state, err := loadOrInitBatchState(opts.StateFile, r.tenantID, seedRefs, opts.BatchSize, len(batches), opts.Resume)
	if err != nil {
		return nil, err
	}
	state.Status = "running"
	state.LastHeartbeatAt = time.Now().UTC()

	startBatch := state.CompletedBatches
	runLimit := len(batches)
	if opts.MaxBatches > 0 && startBatch+opts.MaxBatches < runLimit {
		runLimit = startBatch + opts.MaxBatches
	}

	for batchIndex := startBatch; batchIndex < runLimit; batchIndex++ {
		seedBatch := append([]string(nil), batches[batchIndex]...)
		summary := batchRunSummary{
			BatchIndex:    batchIndex + 1,
			Status:        "running",
			SeedRefs:      seedBatch,
			StartedAt:     time.Now().UTC(),
			LastUpdatedAt: time.Now().UTC(),
		}
		state.CurrentBatchIndex = batchIndex + 1
		state.CurrentPhase = "resolving_refs"
		state.CurrentProgress = nil
		upsertBatchSummary(state, summary)
		if err := persistBatchState(opts.StateFile, state); err != nil {
			return nil, err
		}

		batchRefs, err := r.resolveFocusRefs(seedBatch, opts.ExpandOwnedFacilities)
		if err != nil {
			summary.Status = "failed"
			summary.Error = err.Error()
			summary.LastUpdatedAt = time.Now().UTC()
			summary.FinishedAt = summary.LastUpdatedAt
			state.FailedBatches++
			upsertBatchSummary(state, summary)
			state.LastHeartbeatAt = summary.LastUpdatedAt
			state.CurrentPhase = "failed"
			if persistErr := persistBatchState(opts.StateFile, state); persistErr != nil {
				return nil, persistErr
			}
			return state, err
		}
		summary.ResolvedRefs = batchRefs
		summary.LastUpdatedAt = time.Now().UTC()
		upsertBatchSummary(state, summary)
		state.LastHeartbeatAt = summary.LastUpdatedAt
		state.CurrentPhase = "loading_evidence"
		if err := persistBatchState(opts.StateFile, state); err != nil {
			return nil, err
		}

		evidence, err := r.loadEvidence(batchRefs, opts.Limit, opts.PageSize)
		if err != nil {
			summary.Status = "failed"
			summary.Error = err.Error()
			summary.LastUpdatedAt = time.Now().UTC()
			summary.FinishedAt = summary.LastUpdatedAt
			state.FailedBatches++
			upsertBatchSummary(state, summary)
			state.LastHeartbeatAt = summary.LastUpdatedAt
			state.CurrentPhase = "failed"
			if persistErr := persistBatchState(opts.StateFile, state); persistErr != nil {
				return nil, persistErr
			}
			return state, err
		}
		summary.Triads = len(evidence)
		summary.LastUpdatedAt = time.Now().UTC()
		upsertBatchSummary(state, summary)
		state.LastHeartbeatAt = summary.LastUpdatedAt

		if !opts.SkipAudit {
			state.CurrentPhase = "audit"
			if err := persistBatchState(opts.StateFile, state); err != nil {
				return nil, err
			}
			audit, err := r.auditEvidence(evidence, opts.SampleLimit)
			if err != nil {
				summary.Status = "failed"
				summary.Error = err.Error()
				summary.LastUpdatedAt = time.Now().UTC()
				summary.FinishedAt = summary.LastUpdatedAt
				state.FailedBatches++
				upsertBatchSummary(state, summary)
				state.LastHeartbeatAt = summary.LastUpdatedAt
				state.CurrentPhase = "failed"
				if persistErr := persistBatchState(opts.StateFile, state); persistErr != nil {
					return nil, persistErr
				}
				return state, err
			}
			summary.Audit = audit
			summary.LastUpdatedAt = time.Now().UTC()
			upsertBatchSummary(state, summary)
			state.LastHeartbeatAt = summary.LastUpdatedAt
		}

		state.CurrentPhase = "repair"
		if err := persistBatchState(opts.StateFile, state); err != nil {
			return nil, err
		}
		stats, err := r.repairEvidenceWithProgress(evidence, opts.ApplyWrites, opts.CreateMissingFragments, opts.ProgressEvery, func(progress repairProgress) {
			now := time.Now().UTC()
			state.LastHeartbeatAt = now
			state.CurrentProgress = &progress
			state.CurrentPhase = "repair"
			summary.LastUpdatedAt = now
			summary.Stats = progress.Stats
			upsertBatchSummary(state, summary)
			_ = persistBatchState(opts.StateFile, state)
		})
		if err != nil {
			summary.Status = "failed"
			summary.Error = err.Error()
			summary.LastUpdatedAt = time.Now().UTC()
			summary.FinishedAt = summary.LastUpdatedAt
			summary.Stats = stats
			state.FailedBatches++
			upsertBatchSummary(state, summary)
			state.LastHeartbeatAt = summary.LastUpdatedAt
			state.CurrentPhase = "failed"
			if persistErr := persistBatchState(opts.StateFile, state); persistErr != nil {
				return nil, persistErr
			}
			return state, err
		}

		if opts.RebuildUpstream && len(batchRefs) > 0 {
			state.CurrentPhase = "rebuild_upstream"
			state.CurrentProgress = nil
			if err := persistBatchState(opts.StateFile, state); err != nil {
				return nil, err
			}
			rebuilt, rebuildErrs := r.rebuildUpstreamPaths(batchRefs, opts.ApplyWrites, opts.MaxDepth, opts.MaxPaths)
			stats.UpstreamPathsRebuilt += rebuilt
			stats.UpstreamPathErrors += rebuildErrs
			stats.Errors += rebuildErrs
		}

		summary.Status = "completed"
		summary.Stats = stats
		summary.LastUpdatedAt = time.Now().UTC()
		summary.FinishedAt = summary.LastUpdatedAt
		state.CompletedBatches = batchIndex + 1
		state.LastHeartbeatAt = summary.LastUpdatedAt
		state.CurrentProgress = nil
		state.CurrentPhase = "completed_batch"
		accumulateRepairStats(&state.Aggregate, stats)
		upsertBatchSummary(state, summary)
		if err := persistBatchState(opts.StateFile, state); err != nil {
			return nil, err
		}
	}

	state.CurrentBatchIndex = 0
	state.CurrentPhase = ""
	state.CurrentProgress = nil
	state.LastHeartbeatAt = time.Now().UTC()
	state.FinishedAt = state.LastHeartbeatAt
	if state.CompletedBatches >= state.TotalBatches {
		state.Status = "completed"
	} else {
		state.Status = "partial"
	}
	if err := persistBatchState(opts.StateFile, state); err != nil {
		return nil, err
	}
	return state, nil
}

func upsertBatchSummary(state *batchRunState, summary batchRunSummary) {
	if state == nil {
		return
	}
	for i := range state.Batches {
		if state.Batches[i].BatchIndex == summary.BatchIndex {
			state.Batches[i] = summary
			return
		}
	}
	state.Batches = append(state.Batches, summary)
	sort.Slice(state.Batches, func(i, j int) bool {
		return state.Batches[i].BatchIndex < state.Batches[j].BatchIndex
	})
}

func loadOrInitBatchState(stateFile, tenantID string, seedRefs []string, batchSize, totalBatches int, resume bool) (*batchRunState, error) {
	if resume {
		if existing, err := loadBatchState(stateFile); err == nil && existing != nil {
			return existing, nil
		}
	}
	now := time.Now().UTC()
	return &batchRunState{
		TenantID:        tenantID,
		Status:          "initialized",
		StartedAt:       now,
		LastHeartbeatAt: now,
		SeedRefs:        append([]string(nil), seedRefs...),
		BatchSize:       batchSize,
		TotalBatches:    totalBatches,
		Batches:         []batchRunSummary{},
	}, nil
}

func loadBatchState(stateFile string) (*batchRunState, error) {
	stateFile = strings.TrimSpace(stateFile)
	if stateFile == "" {
		return nil, fmt.Errorf("state file is required")
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, err
	}
	var state batchRunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func persistBatchState(stateFile string, state *batchRunState) error {
	stateFile = strings.TrimSpace(stateFile)
	if stateFile == "" || state == nil {
		return nil
	}
	state.LastHeartbeatAt = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile, append(data, '\n'), 0o644)
}

func loadSeedRefs(refsFile, refsCSV string) ([]string, error) {
	refs := normalizeCSVPaths(refsCSV)
	if strings.TrimSpace(refsFile) == "" {
		return refs, nil
	}
	data, err := os.ReadFile(strings.TrimSpace(refsFile))
	if err != nil {
		return nil, err
	}
	raw := strings.ReplaceAll(string(data), "\n", ",")
	raw = strings.ReplaceAll(raw, "\r", ",")
	return mergeStrings(refs, normalizeCSVPaths(raw)), nil
}

func accumulateRepairStats(dst, src *repairStats) {
	if dst == nil || src == nil {
		return
	}
	dst.TriadsScanned += src.TriadsScanned
	dst.MissingEndpoints += src.MissingEndpoints
	dst.ProducesCreated += src.ProducesCreated
	dst.ProductSuppliesCreated += src.ProductSuppliesCreated
	dst.ActorSuppliesCreated += src.ActorSuppliesCreated
	dst.SupplyLinksCreated += src.SupplyLinksCreated
	dst.SupplyLinksUpdated += src.SupplyLinksUpdated
	dst.ArtifactsCreatedOrMerged += src.ArtifactsCreatedOrMerged
	dst.UpstreamPathsRebuilt += src.UpstreamPathsRebuilt
	dst.UpstreamPathErrors += src.UpstreamPathErrors
	dst.FragmentsCreated += src.FragmentsCreated
	dst.Errors += src.Errors
}

func (r *runner) ensureProducesEdge(supplierFrag, productFrag *models.HorizonFragment, ev *supplyEvidence, applyWrites bool) (bool, error) {
	if supplierFrag == nil || productFrag == nil {
		return false, nil
	}
	if r.edgeExists(supplierFrag.ID, productFrag.ID, models.DependencyTypeProduces) {
		return false, nil
	}
	if !applyWrites {
		return true, nil
	}
	edge := newDerivedEdge(supplierFrag.ID, productFrag.ID, models.DependencyTypeProduces, ev, supplierFrag.CanonicalPath, productFrag.CanonicalPath)
	_, err := r.edgesRepo.Create(r.ctx, edge)
	if err == nil {
		r.edgeExistsCache[r.edgeExistsKey(supplierFrag.ID, productFrag.ID, models.DependencyTypeProduces)] = true
	}
	return err == nil, err
}

func (r *runner) ensureProductSuppliesEdge(productFrag, customerFrag *models.HorizonFragment, ev *supplyEvidence, applyWrites bool) (bool, error) {
	if productFrag == nil || customerFrag == nil {
		return false, nil
	}
	if r.edgeExists(productFrag.ID, customerFrag.ID, models.DependencyTypeSuppliesTo) {
		return false, nil
	}
	if !applyWrites {
		return true, nil
	}
	edge := newDerivedEdge(productFrag.ID, customerFrag.ID, models.DependencyTypeSuppliesTo, ev, productFrag.CanonicalPath, customerFrag.CanonicalPath)
	edge.Qualifiers["productRef"] = ev.ProductRef
	edge.Qualifiers["product_ref"] = ev.ProductRef
	_, err := r.edgesRepo.Create(r.ctx, edge)
	if err == nil {
		r.edgeExistsCache[r.edgeExistsKey(productFrag.ID, customerFrag.ID, models.DependencyTypeSuppliesTo)] = true
	}
	return err == nil, err
}

func (r *runner) ensureActorSuppliesEdge(supplierFrag, customerFrag *models.HorizonFragment, ev *supplyEvidence, applyWrites bool) (bool, error) {
	if supplierFrag == nil || customerFrag == nil {
		return false, nil
	}
	if strings.TrimSpace(supplierFrag.ID) == strings.TrimSpace(customerFrag.ID) {
		return false, nil
	}
	edges, err := r.edgesRepo.ListBySourceTargetAndType(r.ctx, r.tenantID, supplierFrag.ID, customerFrag.ID, models.DependencyTypeSuppliesTo, 20)
	if err != nil {
		return false, err
	}
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		if matchesProductQualifier(edge.Qualifiers, ev.ProductRef) {
			return false, nil
		}
	}
	if !applyWrites {
		return true, nil
	}
	edge := newDerivedEdge(supplierFrag.ID, customerFrag.ID, models.DependencyTypeSuppliesTo, ev, supplierFrag.CanonicalPath, customerFrag.CanonicalPath)
	edge.Qualifiers["productRef"] = ev.ProductRef
	edge.Qualifiers["product_ref"] = ev.ProductRef
	_, err = r.edgesRepo.Create(r.ctx, edge)
	return err == nil, err
}

func (r *runner) ensureSupplyLink(ev *supplyEvidence, applyWrites bool) (created bool, updated bool, err error) {
	if normalizePath(ev.SupplierRef) == normalizePath(ev.CustomerRef) {
		return false, false, nil
	}
	supplierRef := ev.SupplierRef
	productRef := ev.ProductRef
	customerRef := ev.CustomerRef
	canonicalPath, err := models.BuildSupplyLinkCanonicalPath(r.tenantID, &supplierRef, &productRef, &customerRef)
	if err != nil {
		return false, false, err
	}

	snapshots, err := r.db.Collection("horizon_fragments").
		Where("tenantId", "==", r.tenantID).
		Where("canonicalPath", "==", canonicalPath).
		Limit(1).
		Documents(r.ctx).
		GetAll()
	if err != nil {
		return false, false, err
	}
	if len(snapshots) > 0 {
		if !applyWrites {
			return false, true, nil
		}
		var existing models.HorizonFragment
		if err := snapshots[0].DataTo(&existing); err != nil {
			return false, false, err
		}
		changed := false
		if existing.State == nil {
			existing.State = map[string]models.MetricValue{}
		}
		if curr := metricValue(existing.State, "confidence", 0); ev.Confidence > curr {
			existing.State["confidence"] = models.MetricValue{Value: ev.Confidence, Confidence: 0.8}
			changed = true
		}
		mergedVia := mergeStrings(existing.TransportViaRefs, ev.TransportViaRefs)
		if !equalStrings(existing.TransportViaRefs, mergedVia) {
			existing.TransportViaRefs = mergedVia
			changed = true
		}
		if existing.Qualifiers == nil {
			existing.Qualifiers = map[string]interface{}{}
		}
		if applyRepairQualifiers(existing.Qualifiers, ev) {
			changed = true
		}
		if !changed {
			return false, false, nil
		}
		if strings.TrimSpace(existing.ID) == "" {
			existing.ID = snapshots[0].Ref.ID
		}
		err = r.fragmentsRepo.Update(r.ctx, &existing)
		return false, err == nil, err
	}

	if !applyWrites {
		return true, false, nil
	}
	now := time.Now().UTC()
	frag := &models.HorizonFragment{
		TenantID:         r.tenantID,
		Type:             models.FragmentTypeSupplyLink,
		CanonicalPath:    canonicalPath,
		SupplierRef:      &supplierRef,
		ProductRef:       &productRef,
		CustomerRef:      &customerRef,
		Status:           "active",
		TransportViaRefs: append([]string(nil), ev.TransportViaRefs...),
		State:            map[string]models.MetricValue{"confidence": {Value: ev.Confidence, Confidence: 0.8}},
		Governance:       models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
		Metadata:         models.FragmentMetadata{CreatedAt: now, LastUpdatedAt: now, LastVerifiedAt: now, EpistemicStatus: models.EpistemicStatusLowConfidence},
		Provenance:       models.Provenance{Method: toolName, SourceIDs: append([]string(nil), ev.SourceIDs...)},
	}
	if ev.SupplierFacilityRef != "" {
		frag.SupplierFacilityRef = strPtr(ev.SupplierFacilityRef)
	}
	if ev.CustomerFacilityRef != "" {
		frag.CustomerFacilityRef = strPtr(ev.CustomerFacilityRef)
	}
	frag.Qualifiers = map[string]interface{}{}
	applyRepairQualifiers(frag.Qualifiers, ev)
	_, err = r.fragmentsRepo.Create(r.ctx, frag)
	return err == nil, false, err
}

func (r *runner) ensureArtifact(ev *supplyEvidence, applyWrites bool) (bool, error) {
	segment := pathbuilder.PathSegment{
		SupplierRef:      ev.SupplierRef,
		ProductRef:       ev.ProductRef,
		CustomerRef:      ev.CustomerRef,
		TransportViaRefs: append([]string(nil), ev.TransportViaRefs...),
		Confidence:       ev.Confidence,
		TenantID:         r.tenantID,
		Metrics:          cloneMetrics(ev.Metrics),
	}
	if ev.SupplierFacilityRef != "" {
		segment.SupplierFacilityRef = strPtr(ev.SupplierFacilityRef)
	}
	if ev.CustomerFacilityRef != "" {
		segment.CustomerFacilityRef = strPtr(ev.CustomerFacilityRef)
	}
	path := &pathbuilder.SupplyChainPath{
		Segments:   []pathbuilder.PathSegment{segment},
		Confidence: ev.Confidence,
		BuildMode:  "repair_from_supply_evidence",
		MaxDepth:   1,
	}
	key := pathbuilder.BuildStructuralCanonicalKey(r.tenantID, path.Segments)
	if key == "" {
		return false, nil
	}
	if _, err := r.artifactsRepo.GetByCanonicalKey(r.ctx, r.tenantID, models.MaterializedKindSupplyChainPath, key); err == nil {
		if !applyWrites {
			return false, nil
		}
	}
	if !applyWrites {
		return true, nil
	}
	_, err := r.pathBuilder.MaterializePath(r.ctx, path, r.tenantID, "", key)
	return err == nil, err
}

func newDerivedEdge(sourceID, targetID string, edgeType models.DependencyType, ev *supplyEvidence, sourcePath, targetPath string) *models.DependencyEdge {
	now := time.Now().UTC()
	qualifiers := map[string]interface{}{
		"fromSupplyChain":     true,
		"repairedBy":          toolName,
		"repairSources":       sortedSourceKinds(ev.SourceKinds),
		"sourceCanonicalPath": normalizePath(sourcePath),
		"targetCanonicalPath": normalizePath(targetPath),
		"reason":              "repaired_from_supply_evidence",
		"facet":               string(models.FacetSupply),
	}
	if len(ev.TransportViaRefs) > 0 {
		qualifiers[models.QualifierTransportVia] = append([]string(nil), ev.TransportViaRefs...)
	}
	if len(ev.SourceIDs) > 0 {
		qualifiers["repairSourceIds"] = append([]string(nil), ev.SourceIDs...)
	}
	return &models.DependencyEdge{
		TenantID:         "global",
		SourceFragmentID: strings.TrimSpace(sourceID),
		TargetFragmentID: strings.TrimSpace(targetID),
		Type:             edgeType,
		Properties: models.EdgeProperties{
			Criticality:   clamp01(ev.Confidence),
			Amplification: 1.0,
			LatencyHours:  24,
		},
		Qualifiers: qualifiers,
		Provenance: models.Provenance{
			Method:    toolName,
			SourceIDs: append([]string(nil), ev.SourceIDs...),
		},
		Metadata: models.EdgeMetadata{
			CreatedAt:     now,
			LastUpdatedAt: now,
		},
		Metrics: cloneMetrics(ev.Metrics),
	}
}

func applyRepairQualifiers(q map[string]interface{}, ev *supplyEvidence) bool {
	if q == nil {
		return false
	}
	changed := false
	if _, ok := q["repairedBy"]; !ok {
		q["repairedBy"] = toolName
		changed = true
	}
	if _, ok := q["repairSources"]; !ok {
		q["repairSources"] = sortedSourceKinds(ev.SourceKinds)
		changed = true
	}
	if _, ok := q["fromSupplyChain"]; !ok {
		q["fromSupplyChain"] = true
		changed = true
	}
	if len(ev.TransportViaRefs) > 0 {
		current := qualifierStringSlice(q, models.QualifierTransportVia)
		merged := mergeStrings(current, ev.TransportViaRefs)
		if !equalStrings(current, merged) {
			q[models.QualifierTransportVia] = merged
			changed = true
		}
	}
	return changed
}

func (r *runner) getFragmentByID(id string) (*models.HorizonFragment, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if frag := r.fragmentByID[id]; frag != nil {
		return frag, nil
	}
	frag, err := r.fragmentsRepo.Get(r.ctx, id)
	if err != nil || frag == nil {
		return nil, err
	}
	r.fragmentByID[id] = frag
	if cp := normalizePath(frag.CanonicalPath); cp != "" {
		r.fragmentByPath[cp] = frag
	}
	return frag, nil
}

func (r *runner) getFragmentByPath(path string) (*models.HorizonFragment, error) {
	path = normalizePath(path)
	if path == "" {
		return nil, nil
	}
	if frag := r.fragmentByPath[path]; frag != nil {
		return frag, nil
	}
	frag, err := r.fragmentsRepo.GetByCanonicalPath(r.ctx, path)
	if err != nil || frag == nil {
		return nil, err
	}
	r.fragmentByPath[path] = frag
	if strings.TrimSpace(frag.ID) != "" {
		r.fragmentByID[frag.ID] = frag
	}
	return frag, nil
}

func (r *runner) edgeExists(sourceID, targetID string, edgeType models.DependencyType) bool {
	key := r.edgeExistsKey(sourceID, targetID, edgeType)
	if exists, ok := r.edgeExistsCache[key]; ok {
		return exists
	}
	_, err := r.edgesRepo.GetBySourceTargetAndType(r.ctx, r.tenantID, sourceID, targetID, edgeType)
	exists := err == nil
	r.edgeExistsCache[key] = exists
	return exists
}

func (r *runner) edgeExistsKey(sourceID, targetID string, edgeType models.DependencyType) string {
	return strings.TrimSpace(sourceID) + "|" + strings.TrimSpace(targetID) + "|" + string(edgeType)
}

func (r *runner) getOrCreateFragmentByPath(path string, createMissing, applyWrites bool) (*models.HorizonFragment, error) {
	path = r.resolveCanonicalRef(path)
	if path == "" {
		return nil, nil
	}
	frag, err := r.getFragmentByPath(path)
	if frag != nil || !createMissing {
		return frag, err
	}
	if !applyWrites {
		return &models.HorizonFragment{CanonicalPath: path, Type: scout.InferFragmentTypeFromCanonicalPath(path), ID: "dry-run:" + path}, nil
	}
	now := time.Now().UTC()
	label := humanizePath(path)
	frag = &models.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: path,
		Type:          scout.InferFragmentTypeFromCanonicalPath(path),
		Labels:        map[string]string{"en": label},
		Status:        "active",
		State:         map[string]models.MetricValue{},
		Governance:    models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
		Metadata: models.FragmentMetadata{
			CreatedAt:       now,
			LastUpdatedAt:   now,
			LastVerifiedAt:  now,
			EpistemicStatus: models.EpistemicStatusLowConfidence,
		},
		Provenance: models.Provenance{
			Method:    toolName,
			SourceIDs: []string{"create_missing_fragment"},
		},
	}
	switch frag.Type {
	case models.FragmentTypeProduct:
		frag.EntityPath = "/Product"
	case models.FragmentTypeFacility:
		frag.EntityPath = "/Organization/Facility"
	default:
		frag.EntityPath = "/Organization"
	}
	id, err := r.fragmentsRepo.Create(r.ctx, frag)
	if err != nil {
		return nil, err
	}
	frag.ID = id
	r.fragmentsCreated++
	r.fragmentByPath[path] = frag
	r.fragmentByID[id] = frag
	return frag, nil
}

func extractProductRef(q map[string]interface{}) string {
	for _, key := range []string{"productRef", "product_ref"} {
		if q == nil {
			continue
		}
		if raw, ok := q[key]; ok {
			if s := normalizePath(fmt.Sprint(raw)); strings.HasPrefix(s, "/product/") {
				return s
			}
		}
	}
	return ""
}

func matchesProductQualifier(q map[string]interface{}, productRef string) bool {
	return extractProductRef(q) == normalizePath(productRef)
}

func qualifierStringSlice(q map[string]interface{}, key string) []string {
	if q == nil {
		return nil
	}
	raw, ok := q[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return normalizePaths(typed)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizePath(fmt.Sprint(item)))
		}
		return compactStrings(out)
	default:
		return nil
	}
}

func printAuditSummary(summary *auditSummary) {
	if summary == nil {
		return
	}
	fmt.Printf("tenant=%s triads=%d\n", summary.TenantID, summary.Triads)
	fmt.Printf("  evidence: supply_links=%d artifacts=%d actor_edges=%d\n", summary.FromSupplyLinks, summary.FromArtifacts, summary.FromActorEdges)
	fmt.Printf("  present: direct_actor_edge=%d supply_link=%d artifact=%d\n", summary.WithDirectActorEdge, summary.WithSupplyLink, summary.WithArtifact)
	fmt.Printf("  missing: produces=%d product_supplies=%d actor_supplies=%d supply_link=%d artifact=%d endpoints=%d\n",
		summary.MissingProduces, summary.MissingProductSupply, summary.MissingActorSupply, summary.MissingSupplyLink, summary.MissingArtifact, summary.MissingEndpoints)
	if len(summary.Samples) > 0 {
		fmt.Println("  samples:")
		for _, sample := range summary.Samples {
			fmt.Printf("    - %s\n", sample)
		}
	}
}

func appendSample(summary *auditSummary, sampleLimit int, prefix string, ev *supplyEvidence) {
	if summary == nil || ev == nil || len(summary.Samples) >= sampleLimit {
		return
	}
	summary.Samples = append(summary.Samples, fmt.Sprintf("%s: %s -> %s -> %s", prefix, ev.SupplierRef, ev.ProductRef, ev.CustomerRef))
}

func sortedEvidenceKeys(m map[string]*supplyEvidence) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSourceKinds(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func normalizeTenant(tenant string) string {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return "global"
	}
	return tenant
}

func normalizeCSVPaths(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if cp := normalizePath(part); cp != "" {
			out = append(out, cp)
		}
	}
	return compactStrings(out)
}

func normalizePath(path string) string {
	return canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(path))
}

func normalizePaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if cp := normalizePath(path); cp != "" {
			out = append(out, cp)
		}
	}
	return compactStrings(out)
}

func supplyLinkFieldMatches(frag *models.HorizonFragment, field string, refs []string) bool {
	if frag == nil {
		return false
	}
	refSet := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if cp := normalizePath(ref); cp != "" {
			refSet[cp] = struct{}{}
		}
	}
	if len(refSet) == 0 {
		return false
	}
	var value string
	switch field {
	case "supplierRef":
		value = deref(frag.SupplierRef)
	case "customerRef":
		value = deref(frag.CustomerRef)
	case "supplierFacilityRef":
		value = deref(frag.SupplierFacilityRef)
	case "customerFacilityRef":
		value = deref(frag.CustomerFacilityRef)
	case "productRef":
		value = deref(frag.ProductRef)
	default:
		return false
	}
	_, ok := refSet[normalizePath(value)]
	return ok
}

func chunkStrings(values []string, size int) [][]string {
	values = compactStrings(values)
	if len(values) == 0 {
		return nil
	}
	if size <= 0 {
		size = 10
	}
	out := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		out = append(out, append([]string(nil), values[start:end]...))
	}
	return out
}

func focusedEdgeLimit(limit int) int {
	if limit > 0 {
		if limit < 100 {
			return 100
		}
		return limit
	}
	return 300
}

func metricValue(m map[string]models.MetricValue, key string, fallback float64) float64 {
	if m == nil {
		return fallback
	}
	if mv, ok := m[key]; ok && mv.Value > 0 {
		return mv.Value
	}
	return fallback
}

func cloneMetrics(m map[string]models.MetricValue) map[string]models.MetricValue {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]models.MetricValue, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mergeStrings(a, b []string) []string {
	return compactStrings(append(append([]string(nil), a...), b...))
}

func equalStrings(a, b []string) bool {
	a = compactStrings(a)
	b = compactStrings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compactStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func sortedSet(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	if v == 0 {
		return 0.5
	}
	return v
}

func isSupplyActorPath(path string) bool {
	return strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/fac/") || strings.HasPrefix(path, "/gov/") || strings.HasPrefix(path, "/deposit/")
}

func isSupplyCustomerPath(path string) bool {
	return strings.HasPrefix(path, "/org/") || strings.HasPrefix(path, "/fac/") || strings.HasPrefix(path, "/gov/")
}

func humanizePath(path string) string {
	path = normalizePath(path)
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	last := strings.ReplaceAll(parts[len(parts)-1], "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	return strings.Title(last)
}
