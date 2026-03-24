package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	appcache "github.com/igorkirisyuk/sanskrite/internal/cache"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var (
	mode            = flag.String("mode", "report", "Mode: report|backfill-relation-kind|dedup-backfill")
	projectID       = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project ID")
	tenantID        = flag.String("tenant", "global", "Tenant ID")
	reportInPath    = flag.String("report-in", "", "Optional existing transform review report JSON to load instead of rescanning the database")
	outputPath      = flag.String("out", "", "Optional path to write full JSON report")
	verbose         = flag.Bool("verbose", false, "Verbose logging")
	llmEnabled      = flag.Bool("use-llm", true, "Use LLM to adjudicate transform candidates")
	llmModel        = flag.String("llm-model", "gemini-3.1-flash-lite-preview", "LLM model for semantic transform review")
	persistPairs    = flag.Bool("persist-pairs", false, "Persist accepted transform pairs to Firestore")
	pairCollection  = flag.String("pair-collection", "horizon_product_transform_pairs", "Firestore collection for accepted transform pairs")
	candidateLimit  = flag.Int("candidate-limit", 800, "Maximum candidates to review (0 = unlimited)")
	candidateOffset = flag.Int("candidate-offset", 0, "Skip this many top-ranked candidates")
	minScore        = flag.Float64("min-score", 2.0, "Minimum heuristic score to keep a transform candidate")
	maxActorInputs  = flag.Int("max-actor-inputs", 12, "Skip actor I/O inference for actors with more distinct input products")
	maxActorOutputs = flag.Int("max-actor-outputs", 12, "Skip actor I/O inference for actors with more distinct output products")
	maxActorCross   = flag.Int("max-actor-cross", 72, "Skip actor I/O inference when input/output cross-product exceeds this number")
)

var toolRedisClient = appcache.NewRedisClient()
var toolProductDictRuntime *productdict.Runtime

type productRecord struct {
	ID            string
	CanonicalPath string
	PreferredName string
	Description   string
	PathShape     string
	PathSlug      string
	TaxonomySig   string
	Category      string
	ProductType   string
	Stage         string
	StageOrder    int
	QualityScore  int
}

type actorInfo struct {
	ID            string
	CanonicalPath string
	PreferredName string
	FragmentType  string
}

type synonymPairDoc struct {
	CanonicalFragmentID string
	CanonicalPath       string
	SynonymFragmentID   string
	SynonymPath         string
}

type productCanonicalizer struct {
	byFragmentRoot map[string]string
	byPathRoot     map[string]string
}

type directTransformEvidence struct {
	EdgeID string `json:"edgeId,omitempty"`
}

type actorTransformEvidence struct {
	ActorRef       string `json:"actorRef,omitempty"`
	ActorName      string `json:"actorName,omitempty"`
	InputKind      string `json:"inputKind,omitempty"`
	InputRef       string `json:"inputRef,omitempty"`
	OutputKind     string `json:"outputKind,omitempty"`
	OutputRef      string `json:"outputRef,omitempty"`
	InputEvidence  string `json:"inputEvidence,omitempty"`
	OutputEvidence string `json:"outputEvidence,omitempty"`
}

type assemblyTransformEvidence struct {
	ArtifactID string `json:"artifactId,omitempty"`
	Relation   string `json:"relation,omitempty"`
	Status     string `json:"status,omitempty"`
}

type transformCandidate struct {
	ID            string
	From          productRecord
	To            productRecord
	Score         float64
	Reasons       []string
	DirectEdges   []directTransformEvidence
	ActorEvidence []actorTransformEvidence
	AssemblyEdges []assemblyTransformEvidence
}

type transformCandidateLite struct {
	ID                 string      `json:"id"`
	From               productLite `json:"from"`
	To                 productLite `json:"to"`
	HeuristicScore     float64     `json:"heuristicScore"`
	Reasons            []string    `json:"reasons,omitempty"`
	DirectEdgeCount    int         `json:"directEdgeCount"`
	ActorEvidenceCount int         `json:"actorEvidenceCount"`
	AssemblyEdgeCount  int         `json:"assemblyEdgeCount"`
	ActorSamples       []string    `json:"actorSamples,omitempty"`
}

type productLite struct {
	ID            string `json:"id"`
	CanonicalPath string `json:"canonicalPath"`
	PreferredName string `json:"preferredName"`
	Description   string `json:"description,omitempty"`
	PathShape     string `json:"pathShape,omitempty"`
	PathSlug      string `json:"pathSlug,omitempty"`
	TaxonomySig   string `json:"taxonomySignature,omitempty"`
	Category      string `json:"category,omitempty"`
	ProductType   string `json:"productType,omitempty"`
	Stage         string `json:"stage,omitempty"`
	StageOrder    int    `json:"stageOrder,omitempty"`
	QualityScore  int    `json:"qualityScore,omitempty"`
}

type transformReviewEntry struct {
	CandidateID      string                      `json:"candidateId"`
	FromFragmentID   string                      `json:"fromFragmentId"`
	FromPath         string                      `json:"fromPath"`
	FromName         string                      `json:"fromName"`
	ToFragmentID     string                      `json:"toFragmentId"`
	ToPath           string                      `json:"toPath"`
	ToName           string                      `json:"toName"`
	Accepted         bool                        `json:"accepted"`
	Confidence       float64                     `json:"confidence"`
	DecisionSource   string                      `json:"decisionSource"`
	Reason           string                      `json:"reason,omitempty"`
	HeuristicScore   float64                     `json:"heuristicScore"`
	Reasons          []string                    `json:"reasons,omitempty"`
	DirectEdges      []directTransformEvidence   `json:"directEdges,omitempty"`
	ActorEvidence    []actorTransformEvidence    `json:"actorEvidence,omitempty"`
	AssemblyEvidence []assemblyTransformEvidence `json:"assemblyEvidence,omitempty"`
	FromStage        string                      `json:"fromStage,omitempty"`
	ToStage          string                      `json:"toStage,omitempty"`
	FromStageOrder   int                         `json:"fromStageOrder,omitempty"`
	ToStageOrder     int                         `json:"toStageOrder,omitempty"`
}

type transformReviewSummary struct {
	ProductsLoaded          int  `json:"productsLoaded"`
	SynonymPairsLoaded      int  `json:"synonymPairsLoaded"`
	TransformEdgesLoaded    int  `json:"transformEdgesLoaded"`
	ProducesEdgesLoaded     int  `json:"producesEdgesLoaded"`
	SupplyLinksLoaded       int  `json:"supplyLinksLoaded"`
	AssemblyArtifactsLoaded int  `json:"assemblyArtifactsLoaded"`
	CandidatesGenerated     int  `json:"candidatesGenerated"`
	CandidatesReviewed      int  `json:"candidatesReviewed"`
	AcceptedPairs           int  `json:"acceptedPairs"`
	RejectedPairs           int  `json:"rejectedPairs"`
	CuratedRejects          int  `json:"curatedRejects"`
	PersistedPairs          int  `json:"persistedPairs"`
	LLMEnabled              bool `json:"llmEnabled"`
}

type transformReviewReport struct {
	GeneratedAt string                 `json:"generatedAt"`
	ProjectID   string                 `json:"projectId"`
	TenantID    string                 `json:"tenantId"`
	Summary     transformReviewSummary `json:"summary"`
	Pairs       []transformReviewEntry `json:"pairs"`
}

type llmTransformDecision struct {
	CandidateID string  `json:"candidateId"`
	Accepted    bool    `json:"accepted"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

type candidateAccumulator struct {
	FromID           string
	FromPath         string
	ToID             string
	ToPath           string
	Reasons          map[string]struct{}
	DirectEdgeIDs    map[string]struct{}
	ActorEvidence    map[string]actorTransformEvidence
	AssemblyEvidence map[string]assemblyTransformEvidence
}

type actorProductEvidence struct {
	ProductPath string
	ProductID   string
	Kind        string
	Ref         string
	Evidence    string
}

type actorIO struct {
	Inputs  map[string]actorProductEvidence
	Outputs map[string]actorProductEvidence
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	toolProductDictRuntime = productdict.NewRuntime(fsClient.Client()).
		WithTenantID(strings.TrimSpace(*tenantID)).
		WithTransformCollection(strings.TrimSpace(*pairCollection))
	if toolRedisClient != nil {
		toolProductDictRuntime.WithRedis(toolRedisClient)
	}

	log.Printf("=== Horizon Product Transform Miner ===")
	log.Printf("project=%s tenant=%s", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID))

	switch strings.TrimSpace(*mode) {
	case "report":
	case "backfill-relation-kind":
		updated, assembly, usage, transformation, err := backfillRelationKind(ctx, fsClient.Client())
		if err != nil {
			log.Fatalf("failed to backfill relationKind: %v", err)
		}
		fmt.Printf("\nRelationKind Backfill\n")
		fmt.Printf("  updated_docs: %d\n", updated)
		fmt.Printf("  assembly_docs: %d\n", assembly)
		fmt.Printf("  usage_docs: %d\n", usage)
		fmt.Printf("  transformation_docs: %d\n", transformation)
		return
	case "dedup-backfill":
		active, deactivated, err := backfillSemanticTransformDedup(ctx, fsClient.Client())
		if err != nil {
			log.Fatalf("failed to backfill semantic dedup: %v", err)
		}
		fmt.Printf("\nTransform Semantic Dedup Backfill\n")
		fmt.Printf("  active_docs: %d\n", active)
		fmt.Printf("  deactivated_docs: %d\n", deactivated)
		return
	default:
		log.Fatalf("unsupported --mode=%q (want report|backfill-relation-kind|dedup-backfill)", strings.TrimSpace(*mode))
	}

	if path := strings.TrimSpace(*reportInPath); path != "" {
		report, err := loadReport(path)
		if err != nil {
			log.Fatalf("failed to load report: %v", err)
		}
		report = applyConservativeCuration(report)
		if *persistPairs {
			persisted, err := persistAcceptedPairs(ctx, fsClient.Client(), report.Pairs)
			if err != nil {
				log.Fatalf("failed to persist accepted transform pairs: %v", err)
			}
			report.Summary.PersistedPairs = persisted
		}
		if path := strings.TrimSpace(*outputPath); path != "" {
			if err := writeReport(path, report); err != nil {
				log.Fatalf("failed to write report: %v", err)
			}
		}
		printSummary(report)
		return
	}

	products, err := loadProducts(ctx, fsClient.Client())
	if err != nil {
		log.Fatalf("failed to load products: %v", err)
	}
	log.Printf("loaded %d product fragments", len(products))

	synonymPairs, canonicalizer, err := loadSynonymCanonicalizer(ctx, fsClient.Client())
	if err != nil {
		log.Fatalf("failed to load synonym canonicalizer: %v", err)
	}
	log.Printf("loaded %d active synonym pairs", len(synonymPairs))

	transformEdges, err := loadProductTransitionEdges(ctx, fsClient.Client())
	if err != nil {
		log.Fatalf("failed to load product transition edges: %v", err)
	}
	log.Printf("loaded %d product transition edges (TRANSFORMS_INTO + ASSEMBLES_INTO)", len(transformEdges))

	producesEdges, err := loadDependencyEdgesByType(ctx, fsClient.Client(), models.DependencyTypeProduces)
	if err != nil {
		log.Fatalf("failed to load PRODUCES edges: %v", err)
	}
	log.Printf("loaded %d PRODUCES edges", len(producesEdges))

	supplyLinks, err := loadSupplyLinks(ctx, fsClient.Client())
	if err != nil {
		log.Fatalf("failed to load SUPPLY_LINK fragments: %v", err)
	}
	log.Printf("loaded %d SUPPLY_LINK fragments", len(supplyLinks))

	assemblyArtifacts, err := loadAssemblyArtifacts(ctx, fsClient.Client())
	if err != nil {
		log.Fatalf("failed to load assembly artifacts: %v", err)
	}
	log.Printf("loaded %d assembly artifacts", len(assemblyArtifacts))

	actorIDs := collectActorIDsFromProduces(producesEdges)
	actors, err := loadActorsByID(ctx, fsClient.Client(), actorIDs)
	if err != nil {
		log.Fatalf("failed to load actors for PRODUCES edges: %v", err)
	}

	candidates := buildTransformCandidates(products, actors, canonicalizer, transformEdges, producesEdges, supplyLinks, assemblyArtifacts)
	log.Printf("generated %d transform candidates", len(candidates))

	aiExecutor := initAIExecutor(ctx)
	report, err := reviewTransformCandidates(ctx, candidates, aiExecutor)
	if err != nil {
		log.Fatalf("failed to review transform candidates: %v", err)
	}
	report = applyConservativeCuration(report)
	report.Summary.ProductsLoaded = len(products)
	report.Summary.SynonymPairsLoaded = len(synonymPairs)
	report.Summary.TransformEdgesLoaded = len(transformEdges)
	report.Summary.ProducesEdgesLoaded = len(producesEdges)
	report.Summary.SupplyLinksLoaded = len(supplyLinks)
	report.Summary.AssemblyArtifactsLoaded = len(assemblyArtifacts)

	if *persistPairs {
		persisted, err := persistAcceptedPairs(ctx, fsClient.Client(), report.Pairs)
		if err != nil {
			log.Fatalf("failed to persist accepted transform pairs: %v", err)
		}
		report.Summary.PersistedPairs = persisted
	}

	if path := strings.TrimSpace(*outputPath); path != "" {
		if err := writeReport(path, report); err != nil {
			log.Fatalf("failed to write report: %v", err)
		}
		log.Printf("wrote report: %s", path)
	}
	printSummary(report)
}

func initAIExecutor(ctx context.Context) *ai.AITaskExecutor {
	if !*llmEnabled {
		return nil
	}
	apiKey := strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "GEMINI_API_KEY", "gemini_api_key"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(gcp.GetEnvOrSecret(ctx, strings.TrimSpace(*projectID), "VERTEX_API_KEY", "vertex_api_key"))
	}
	if apiKey == "" {
		log.Printf("WARN: no GEMINI_API_KEY or VERTEX_API_KEY (env/secret), semantic review disabled")
		return nil
	}
	mm := ai.NewAIModelManager()
	return ai.NewAITaskExecutor([]ai.AIService{ai.NewGeminiService(apiKey, mm)}, mm)
}

func loadProducts(ctx context.Context, db *firestore.Client) (map[string]productRecord, error) {
	iter := db.Collection("horizon_fragments").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "==", string(models.FragmentTypeProduct)).
		Documents(ctx)
	defer iter.Stop()

	out := make(map[string]productRecord, 4096)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var frag models.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			if *verbose {
				log.Printf("skip product parse error %s: %v", doc.Ref.ID, err)
			}
			continue
		}
		if strings.TrimSpace(frag.ID) == "" {
			frag.ID = doc.Ref.ID
		}
		rec := buildProductRecord(&frag)
		if rec.ID == "" || rec.CanonicalPath == "" {
			continue
		}
		out[rec.ID] = rec
	}
	return out, nil
}

func loadSynonymCanonicalizer(ctx context.Context, db *firestore.Client) ([]synonymPairDoc, productCanonicalizer, error) {
	iter := db.Collection("horizon_product_synonym_pairs").Documents(ctx)
	defer iter.Stop()

	pairs := make([]synonymPairDoc, 0, 512)
	parentID := map[string]string{}
	parentPath := map[string]string{}

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, productCanonicalizer{}, err
		}
		data := doc.Data()
		if strings.TrimSpace(stringValue(data["tenantId"])) != strings.TrimSpace(*tenantID) {
			continue
		}
		if !boolValue(data["active"]) {
			continue
		}
		pair := synonymPairDoc{
			CanonicalFragmentID: strings.TrimSpace(stringValue(data["canonicalFragmentId"])),
			CanonicalPath:       strings.TrimSpace(stringValue(data["canonicalPath"])),
			SynonymFragmentID:   strings.TrimSpace(stringValue(data["synonymFragmentId"])),
			SynonymPath:         strings.TrimSpace(stringValue(data["synonymPath"])),
		}
		if pair.CanonicalPath == "" || pair.SynonymPath == "" || pair.CanonicalPath == pair.SynonymPath {
			continue
		}
		pairs = append(pairs, pair)
		if pair.CanonicalFragmentID != "" && pair.SynonymFragmentID != "" {
			parentID[pair.SynonymFragmentID] = pair.CanonicalFragmentID
		}
		parentPath[pair.SynonymPath] = pair.CanonicalPath
	}

	resolveID := func(id string) string {
		seen := map[string]struct{}{}
		cur := strings.TrimSpace(id)
		for cur != "" {
			next, ok := parentID[cur]
			if !ok || strings.TrimSpace(next) == "" {
				return cur
			}
			if _, exists := seen[cur]; exists {
				return cur
			}
			seen[cur] = struct{}{}
			cur = strings.TrimSpace(next)
		}
		return strings.TrimSpace(id)
	}
	resolvePath := func(path string) string {
		seen := map[string]struct{}{}
		cur := strings.TrimSpace(path)
		for cur != "" {
			next, ok := parentPath[cur]
			if !ok || strings.TrimSpace(next) == "" {
				return cur
			}
			if _, exists := seen[cur]; exists {
				return cur
			}
			seen[cur] = struct{}{}
			cur = strings.TrimSpace(next)
		}
		return strings.TrimSpace(path)
	}

	byFragmentRoot := map[string]string{}
	for id := range parentID {
		byFragmentRoot[id] = resolveID(id)
	}
	byPathRoot := map[string]string{}
	for path := range parentPath {
		byPathRoot[path] = resolvePath(path)
	}
	return pairs, productCanonicalizer{
		byFragmentRoot: byFragmentRoot,
		byPathRoot:     byPathRoot,
	}, nil
}

func loadDependencyEdgesByType(ctx context.Context, db *firestore.Client, edgeType models.DependencyType) ([]models.DependencyEdge, error) {
	iter := db.Collection("horizon_dependency_edges").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "==", string(edgeType)).
		Documents(ctx)
	defer iter.Stop()

	out := make([]models.DependencyEdge, 0, 4096)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var edge models.DependencyEdge
		if err := doc.DataTo(&edge); err != nil {
			if *verbose {
				log.Printf("skip edge parse error %s: %v", doc.Ref.ID, err)
			}
			continue
		}
		if strings.TrimSpace(edge.ID) == "" {
			edge.ID = doc.Ref.ID
		}
		out = append(out, edge)
	}
	return out, nil
}

func loadProductTransitionEdges(ctx context.Context, db *firestore.Client) ([]models.DependencyEdge, error) {
	edgeTypes := []models.DependencyType{
		models.DependencyTypeTransformsInto,
		models.DependencyTypeAssemblesInto,
	}
	out := make([]models.DependencyEdge, 0, 4096)
	for _, edgeType := range edgeTypes {
		edges, err := loadDependencyEdgesByType(ctx, db, edgeType)
		if err != nil {
			return nil, err
		}
		out = append(out, edges...)
	}
	return out, nil
}

func loadSupplyLinks(ctx context.Context, db *firestore.Client) ([]models.HorizonFragment, error) {
	iter := db.Collection("horizon_fragments").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "==", string(models.FragmentTypeSupplyLink)).
		Documents(ctx)
	defer iter.Stop()

	out := make([]models.HorizonFragment, 0, 4096)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var frag models.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			if *verbose {
				log.Printf("skip supply link parse error %s: %v", doc.Ref.ID, err)
			}
			continue
		}
		if strings.TrimSpace(frag.ID) == "" {
			frag.ID = doc.Ref.ID
		}
		out = append(out, frag)
	}
	return out, nil
}

func loadAssemblyArtifacts(ctx context.Context, db *firestore.Client) ([]models.MaterializedArtifact, error) {
	iter := db.Collection("horizon_materialized_artifacts").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("kind", "==", string(models.MaterializedKindSupplyChainAssemblyGraph)).
		Documents(ctx)
	defer iter.Stop()

	out := make([]models.MaterializedArtifact, 0, 128)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var artifact models.MaterializedArtifact
		if err := doc.DataTo(&artifact); err != nil {
			if *verbose {
				log.Printf("skip assembly artifact parse error %s: %v", doc.Ref.ID, err)
			}
			continue
		}
		if strings.TrimSpace(artifact.ID) == "" {
			artifact.ID = doc.Ref.ID
		}
		out = append(out, artifact)
	}
	return out, nil
}

func collectActorIDsFromProduces(edges []models.DependencyEdge) []string {
	set := map[string]struct{}{}
	for _, edge := range edges {
		id := strings.TrimSpace(edge.SourceFragmentID)
		if id != "" {
			set[id] = struct{}{}
		}
	}
	return mapKeysSorted(set)
}

func loadActorsByID(ctx context.Context, db *firestore.Client, ids []string) (map[string]actorInfo, error) {
	out := map[string]actorInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	col := db.Collection("horizon_fragments")
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		refs := make([]*firestore.DocumentRef, 0, end-start)
		for _, id := range ids[start:end] {
			refs = append(refs, col.Doc(id))
		}
		docs, err := db.GetAll(ctx, refs)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			if doc == nil || !doc.Exists() {
				continue
			}
			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}
			out[frag.ID] = actorInfo{
				ID:            frag.ID,
				CanonicalPath: strings.TrimSpace(frag.CanonicalPath),
				PreferredName: pickPreferredProductName(&frag, lastSegment(frag.CanonicalPath)),
				FragmentType:  string(frag.Type),
			}
		}
	}
	return out, nil
}

func buildTransformCandidates(
	products map[string]productRecord,
	actors map[string]actorInfo,
	canonicalizer productCanonicalizer,
	transformEdges []models.DependencyEdge,
	producesEdges []models.DependencyEdge,
	supplyLinks []models.HorizonFragment,
	assemblyArtifacts []models.MaterializedArtifact,
) []transformCandidate {
	byPath := make(map[string]productRecord, len(products))
	for _, rec := range products {
		byPath[rec.CanonicalPath] = rec
	}
	resolveProduct := func(fragmentID, path string) (productRecord, bool) {
		if rootID := strings.TrimSpace(canonicalizer.byFragmentRoot[strings.TrimSpace(fragmentID)]); rootID != "" {
			if rec, ok := products[rootID]; ok {
				return rec, true
			}
		}
		if rec, ok := products[strings.TrimSpace(fragmentID)]; ok {
			return rec, true
		}
		rootPath := strings.TrimSpace(path)
		if resolved := strings.TrimSpace(canonicalizer.byPathRoot[rootPath]); resolved != "" {
			rootPath = resolved
		}
		if rec, ok := byPath[rootPath]; ok {
			return rec, true
		}
		return productRecord{}, false
	}

	accumulators := map[string]*candidateAccumulator{}
	addCandidate := func(fromRec, toRec productRecord, reason string, mutate func(*candidateAccumulator)) {
		if !acceptProductPair(fromRec, toRec) {
			return
		}
		key := transformPairDocID(fromRec.CanonicalPath, toRec.CanonicalPath)
		item := accumulators[key]
		if item == nil {
			item = &candidateAccumulator{
				FromID:           fromRec.ID,
				FromPath:         fromRec.CanonicalPath,
				ToID:             toRec.ID,
				ToPath:           toRec.CanonicalPath,
				Reasons:          map[string]struct{}{},
				DirectEdgeIDs:    map[string]struct{}{},
				ActorEvidence:    map[string]actorTransformEvidence{},
				AssemblyEvidence: map[string]assemblyTransformEvidence{},
			}
			accumulators[key] = item
		}
		if reason != "" {
			item.Reasons[reason] = struct{}{}
		}
		if mutate != nil {
			mutate(item)
		}
	}

	for _, edge := range transformEdges {
		fromRec, ok := resolveProduct(edge.SourceFragmentID, "")
		if !ok {
			continue
		}
		toRec, ok := resolveProduct(edge.TargetFragmentID, "")
		if !ok {
			continue
		}
		addCandidate(fromRec, toRec, fmt.Sprintf("direct_%s", strings.ToLower(strings.TrimSpace(string(edge.Type)))), func(item *candidateAccumulator) {
			if id := strings.TrimSpace(edge.ID); id != "" {
				item.DirectEdgeIDs[id] = struct{}{}
			}
		})
	}

	actorIOs := map[string]*actorIO{}
	ensureActor := func(actorRef string) *actorIO {
		actorRef = strings.TrimSpace(actorRef)
		if actorRef == "" {
			return nil
		}
		item := actorIOs[actorRef]
		if item == nil {
			item = &actorIO{
				Inputs:  map[string]actorProductEvidence{},
				Outputs: map[string]actorProductEvidence{},
			}
			actorIOs[actorRef] = item
		}
		return item
	}

	for _, edge := range producesEdges {
		productRec, ok := resolveProduct(edge.TargetFragmentID, "")
		if !ok {
			continue
		}
		actor := actors[strings.TrimSpace(edge.SourceFragmentID)]
		actorRef := strings.TrimSpace(actor.CanonicalPath)
		if actorRef == "" {
			continue
		}
		slot := ensureActor(actorRef)
		if slot == nil {
			continue
		}
		slot.Outputs[productRec.CanonicalPath] = actorProductEvidence{
			ProductPath: productRec.CanonicalPath,
			ProductID:   productRec.ID,
			Kind:        "produces_edge",
			Ref:         strings.TrimSpace(edge.ID),
			Evidence:    firstNonEmpty(actor.PreferredName, actorRef),
		}
	}

	for _, link := range supplyLinks {
		productRef := derefString(link.ProductRef)
		productRec, ok := resolveProduct("", productRef)
		if !ok {
			continue
		}

		customerRef := firstNonEmpty(derefString(link.CustomerFacilityRef), derefString(link.CustomerRef))
		if customerRef != "" {
			slot := ensureActor(customerRef)
			slot.Inputs[productRec.CanonicalPath] = actorProductEvidence{
				ProductPath: productRec.CanonicalPath,
				ProductID:   productRec.ID,
				Kind:        "supply_link_customer",
				Ref:         firstNonEmpty(link.ID, link.CanonicalPath),
				Evidence:    strings.TrimSpace(link.CanonicalPath),
			}
		}

		supplierRef := firstNonEmpty(derefString(link.SupplierFacilityRef), derefString(link.SupplierRef))
		if supplierRef != "" {
			slot := ensureActor(supplierRef)
			slot.Outputs[productRec.CanonicalPath] = actorProductEvidence{
				ProductPath: productRec.CanonicalPath,
				ProductID:   productRec.ID,
				Kind:        "supply_link_supplier",
				Ref:         firstNonEmpty(link.ID, link.CanonicalPath),
				Evidence:    strings.TrimSpace(link.CanonicalPath),
			}
		}
	}

	for actorRef, io := range actorIOs {
		if len(io.Inputs) == 0 || len(io.Outputs) == 0 {
			continue
		}
		if *maxActorInputs > 0 && len(io.Inputs) > *maxActorInputs {
			continue
		}
		if *maxActorOutputs > 0 && len(io.Outputs) > *maxActorOutputs {
			continue
		}
		if *maxActorCross > 0 && len(io.Inputs)*len(io.Outputs) > *maxActorCross {
			continue
		}
		for _, input := range io.Inputs {
			fromRec, ok := resolveProduct(input.ProductID, input.ProductPath)
			if !ok {
				continue
			}
			for _, output := range io.Outputs {
				toRec, ok := resolveProduct(output.ProductID, output.ProductPath)
				if !ok {
					continue
				}
				if !acceptProductPair(fromRec, toRec) {
					continue
				}
				if fromRec.StageOrder > 0 && toRec.StageOrder > 0 && toRec.StageOrder < fromRec.StageOrder {
					continue
				}
				addCandidate(fromRec, toRec, "actor_io_transform", func(item *candidateAccumulator) {
					key := actorRef + "|" + fromRec.CanonicalPath + "|" + toRec.CanonicalPath
					item.ActorEvidence[key] = actorTransformEvidence{
						ActorRef:       actorRef,
						ActorName:      actorDisplayName(actorRef, actors),
						InputKind:      input.Kind,
						InputRef:       input.Ref,
						OutputKind:     output.Kind,
						OutputRef:      output.Ref,
						InputEvidence:  input.Evidence,
						OutputEvidence: output.Evidence,
					}
				})
			}
		}
	}

	for _, artifact := range assemblyArtifacts {
		payload := models.SupplyChainAssemblyGraphPayload{}
		raw, err := json.Marshal(artifact.Payload)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		edges := append([]models.SupplyChainAssemblyTransformEdge{}, payload.TransformEdges...)
		edges = append(edges, payload.AssemblyEdges...)
		for _, edge := range edges {
			fromRec, ok := resolveProduct("", strings.TrimSpace(edge.FromProductRef))
			if !ok {
				continue
			}
			toRec, ok := resolveProduct("", strings.TrimSpace(edge.ToProductRef))
			if !ok {
				continue
			}
			addCandidate(fromRec, toRec, "assembly_transform_edge", func(item *candidateAccumulator) {
				key := strings.TrimSpace(artifact.ID) + "|" + strings.TrimSpace(edge.Relation) + "|" + strings.TrimSpace(edge.Status)
				item.AssemblyEvidence[key] = assemblyTransformEvidence{
					ArtifactID: strings.TrimSpace(artifact.ID),
					Relation:   strings.TrimSpace(edge.Relation),
					Status:     strings.TrimSpace(edge.Status),
				}
			})
		}
	}

	candidates := make([]transformCandidate, 0, len(accumulators))
	for _, item := range accumulators {
		fromRec, ok := resolveProduct(item.FromID, item.FromPath)
		if !ok {
			continue
		}
		toRec, ok := resolveProduct(item.ToID, item.ToPath)
		if !ok {
			continue
		}
		score := scoreTransformCandidate(fromRec, toRec, item)
		if score < *minScore {
			continue
		}
		candidate := transformCandidate{
			ID:            transformPairDocID(fromRec.CanonicalPath, toRec.CanonicalPath),
			From:          fromRec,
			To:            toRec,
			Score:         score,
			Reasons:       mapKeysSorted(item.Reasons),
			DirectEdges:   directEvidenceSlice(item.DirectEdgeIDs),
			ActorEvidence: actorEvidenceSlice(item.ActorEvidence),
			AssemblyEdges: assemblyEvidenceSlice(item.AssemblyEvidence),
		}
		candidates = append(candidates, candidate)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if len(candidates[i].DirectEdges) != len(candidates[j].DirectEdges) {
			return len(candidates[i].DirectEdges) > len(candidates[j].DirectEdges)
		}
		if len(candidates[i].ActorEvidence) != len(candidates[j].ActorEvidence) {
			return len(candidates[i].ActorEvidence) > len(candidates[j].ActorEvidence)
		}
		return candidates[i].ID < candidates[j].ID
	})

	if *candidateOffset > 0 {
		if *candidateOffset >= len(candidates) {
			return nil
		}
		candidates = candidates[*candidateOffset:]
	}
	if *candidateLimit > 0 && len(candidates) > *candidateLimit {
		candidates = candidates[:*candidateLimit]
	}
	return candidates
}

func reviewTransformCandidates(ctx context.Context, candidates []transformCandidate, aiExecutor *ai.AITaskExecutor) (transformReviewReport, error) {
	report := transformReviewReport{
		GeneratedAt: nowRFC3339(),
		ProjectID:   strings.TrimSpace(*projectID),
		TenantID:    strings.TrimSpace(*tenantID),
		Summary: transformReviewSummary{
			CandidatesGenerated: len(candidates),
			LLMEnabled:          aiExecutor != nil && *llmEnabled,
		},
		Pairs: make([]transformReviewEntry, 0, len(candidates)),
	}
	if len(candidates) == 0 {
		return report, nil
	}

	if aiExecutor == nil || !*llmEnabled {
		for _, candidate := range candidates {
			report.Pairs = append(report.Pairs, materializeReview(candidate, true, "heuristic", clamp01(candidate.Score/8.0), "LLM disabled; accepted by heuristic threshold"))
		}
	} else {
		const batchSize = 12
		for start := 0; start < len(candidates); start += batchSize {
			end := start + batchSize
			if end > len(candidates) {
				end = len(candidates)
			}
			batch := candidates[start:end]
			decisions, err := adjudicateTransformBatchWithLLM(ctx, aiExecutor, batch)
			if err != nil {
				for _, candidate := range batch {
					report.Pairs = append(report.Pairs, materializeReview(candidate, false, "llm_error", 0, err.Error()))
				}
				continue
			}
			decisionMap := map[string]llmTransformDecision{}
			for _, decision := range decisions {
				decisionMap[strings.TrimSpace(decision.CandidateID)] = decision
			}
			for _, candidate := range batch {
				decision, ok := decisionMap[candidate.ID]
				if !ok {
					report.Pairs = append(report.Pairs, materializeReview(candidate, false, "llm_missing", 0, "no decision returned"))
					continue
				}
				report.Pairs = append(report.Pairs, materializeReview(candidate, decision.Accepted, "llm", decision.Confidence, decision.Reason))
			}
		}
	}

	sort.SliceStable(report.Pairs, func(i, j int) bool {
		if report.Pairs[i].Accepted != report.Pairs[j].Accepted {
			return report.Pairs[i].Accepted && !report.Pairs[j].Accepted
		}
		if report.Pairs[i].Confidence != report.Pairs[j].Confidence {
			return report.Pairs[i].Confidence > report.Pairs[j].Confidence
		}
		return report.Pairs[i].CandidateID < report.Pairs[j].CandidateID
	})

	report.Summary.CandidatesReviewed = len(report.Pairs)
	for _, pair := range report.Pairs {
		if pair.Accepted {
			report.Summary.AcceptedPairs++
		} else {
			report.Summary.RejectedPairs++
		}
	}
	return report, nil
}

func applyConservativeCuration(report transformReviewReport) transformReviewReport {
	for i := range report.Pairs {
		pair := &report.Pairs[i]
		if strings.TrimSpace(pair.DecisionSource) == "manual_curated_reject" {
			pair.Accepted = false
			continue
		}
		if pair.Accepted {
			if reason, accept := conservativeAcceptReason(*pair); accept {
				pair.DecisionSource = "curated_accept"
				if strings.TrimSpace(pair.Reason) == "" {
					pair.Reason = reason
				} else if !strings.Contains(pair.Reason, reason) {
					pair.Reason = strings.TrimSpace(pair.Reason) + "; " + reason
				}
				continue
			}
		}
		if !pair.Accepted {
			if reason, accept := conservativeAcceptReason(*pair); accept {
				pair.Accepted = true
				pair.DecisionSource = "curated_accept"
				if strings.TrimSpace(pair.Reason) == "" {
					pair.Reason = reason
				} else {
					pair.Reason = strings.TrimSpace(pair.Reason) + "; " + reason
				}
			}
			continue
		}
		if reason, reject := conservativeRejectReason(*pair); reject {
			pair.Accepted = false
			pair.DecisionSource = "curated_reject"
			if strings.TrimSpace(pair.Reason) == "" {
				pair.Reason = reason
			} else {
				pair.Reason = strings.TrimSpace(pair.Reason) + "; " + reason
			}
			report.Summary.CuratedRejects++
		}
	}
	report.Summary.AcceptedPairs = 0
	report.Summary.RejectedPairs = 0
	for _, pair := range report.Pairs {
		if pair.Accepted {
			report.Summary.AcceptedPairs++
		} else {
			report.Summary.RejectedPairs++
		}
	}
	return report
}

func conservativeAcceptReason(pair transformReviewEntry) (string, bool) {
	all := strings.ToLower(strings.Join([]string{
		pair.FromPath,
		pair.ToPath,
		pair.FromName,
		pair.ToName,
		pair.Reason,
	}, " | "))
	if containsAny(all, "service", "services", "design", "freight", "logistics", "handling", "route", "document", "compliance", "/product/service/") {
		return "", false
	}
	if containsAny(all, "/electricity", "low-carbon-electricity") {
		return "", false
	}
	if isDirectBatteryMaterialToCellStep(pair) {
		return "curated_accept: direct battery material to cell manufacturing step", true
	}
	if isNearestFinalAssemblyStep(pair) {
		return "curated_accept: nearest direct assembly into final product is allowed when no useful intermediate product is missing", true
	}
	return "", false
}

func conservativeRejectReason(pair transformReviewEntry) (string, bool) {
	all := strings.ToLower(strings.Join([]string{
		pair.FromPath,
		pair.ToPath,
		pair.FromName,
		pair.ToName,
		pair.Reason,
	}, " | "))
	if containsAny(all, "service", "services", "design", "freight", "logistics", "handling", "route", "document", "compliance", "/product/service/") {
		return "curated_reject: abstract/logistics/service pair", true
	}
	if containsAny(all, "/electricity", "low-carbon-electricity") {
		return "curated_reject: energy-generation output is outside canonical product processing pairs", true
	}
	if isUpstreamBatteryPrecursorToCell(pair) {
		return "curated_reject: upstream battery precursor skips immediate active-material stage before cell manufacturing", true
	}
	if isBatteryMaterialPath(pair.FromPath) && isBatteryLikeOutput(pair.ToPath, pair.ToName) && !isDirectBatteryIntermediate(pair.ToPath, pair.ToName) {
		return "curated_reject: battery material skips direct intermediate manufacturing steps", true
	}
	if isBatteryCellPath(pair.FromPath) && containsAny(pair.ToPath, "electric-vehicles", "/peugeot-e-3008") {
		return "curated_reject: battery cell to final vehicle skips pack/system stage", true
	}
	if isBatteryCellPath(pair.FromPath) && isVehicleLikeOutput(pair.ToPath, pair.ToName) {
		return "curated_reject: battery cell to final vehicle skips pack/system stage", true
	}
	if containsAny(pair.FromPath, "battery-anode") && containsAny(pair.ToPath, "battery-pack", "lithium-ion-battery-pack") {
		return "curated_reject: anode material to battery pack skips cell stage", true
	}
	if isGenericSemiconductorSource(pair.FromPath, pair.FromName) && isSpecificElectronicsAssemblyTarget(pair.ToPath) {
		return "curated_reject: generic semiconductor source is too broad for a canonical direct transform step", true
	}
	if containsAny(pair.FromPath, "automotive-semiconductor-wafer") && containsAny(pair.ToPath, "electronic-control-unit") {
		return "curated_reject: semiconductor wafer to ecu skips chip/control electronics stages", true
	}
	if containsAny(pair.FromPath, "semiconductor-wafer") && containsAny(pair.ToPath, "packaged-microcontroller") {
		return "curated_reject: wafer to packaged microcontroller skips obvious intermediate chip stage", true
	}
	if containsAny(pair.FromPath, "inconel-738-turbine-blades", "turbine-blades-and-vanes") && containsAny(pair.ToPath, "gas-turbine-blades") {
		return "curated_reject: specific blade variant to generic blade class is class/subclass, not transform", true
	}
	if containsAny(pair.FromPath, "gas-turbine-hot-path") && containsAny(pair.ToPath, "gas-turbine-blades") {
		return "curated_reject: hot-path assembly to blade component is not a canonical direct transform", true
	}
	if containsAny(pair.FromPath, "automotive-steel-sheet", "automotive-components") && isVehicleLikeOutput(pair.ToPath, pair.ToName) && !isNearestFinalAssemblyStep(pair) {
		return "curated_reject: broad material/component bundle to final vehicle skips major intermediate assemblies", true
	}
	if containsAny(pair.FromPath, "iron-ore") && containsAny(pair.ToPath, "steel-coil", "steel-sheet", "hot-rolled-steel", "cold-rolled-steel") {
		return "curated_reject: iron ore to finished steel product skips smelting and crude-steel intermediates", true
	}
	if containsAny(pair.FromPath, "pet-preforms", "plastic-closures", "aluminum-beverage-can") && containsAny(pair.ToPath, "soft-drinks", "bottled-water", "isotonic-drinks") {
		return "curated_reject: packaging component to finished beverage skips packaging/filling stage", true
	}
	if containsAny(pair.FromPath, "steel-plate", "steel-coil", "automotive-steel", "high-grade-automotive-steel") &&
		(isVehicleLikeOutput(pair.ToPath, pair.ToName) || containsAny(pair.ToPath, "marine-vessel", "mining-truck", "abs-braking-system")) &&
		!isNearestFinalAssemblyStep(pair) {
		return "curated_reject: broad steel material to final vehicle/system skips component fabrication stages", true
	}
	if containsAny(pair.FromPath, "coking-coal", "/coke") && containsAny(pair.ToPath, "crude-steel", "steel-coil", "hot-rolled-steel") {
		return "curated_reject: coke/coal to downstream steel product skips pig-iron and primary steelmaking stages", true
	}
	if containsAny(pair.FromPath, "coking-coal", "/coke") && containsAny(pair.ToPath, "/steel", "green-steel", "low-carbon-steel") {
		return "curated_reject: coke/coal to steel skips pig-iron and primary steelmaking stages", true
	}
	if containsAny(pair.FromPath, "lithium-ion-battery", "electric-vehicle-battery") && !containsAny(pair.FromPath, "pack", "module", "system") && isVehicleLikeOutput(pair.ToPath, pair.ToName) {
		return "curated_reject: generic battery to final vehicle skips pack/system assembly stage", true
	}
	if containsAny(pair.FromPath, "natural-gas") && containsAny(pair.ToPath, "gasoline") {
		return "curated_reject: natural gas to gasoline is not a direct canonical processing step", true
	}
	if containsAny(pair.FromPath, "natural-rubber", "synthetic-rubber", "tsr-20", "standard-indonesian-rubber") && containsAny(pair.ToPath, "tire", "tires") {
		return "curated_reject: upstream rubber to finished tire skips compound and tire-building stages", true
	}
	if containsAny(pair.FromPath, "flotation-reagents", "software-integration", "precision-machining") {
		return "curated_reject: service/process activity is not a canonical product relation source", true
	}
	if containsAny(pair.FromPath, "industrial-gases") && containsAny(pair.ToPath, "helium", "liquid-helium", "ultra-high-purity-helium") {
		return "curated_reject: broad industrial gas family to specific purified gas is class/subclass, not canonical transform", true
	}
	return "", false
}

func adjudicateTransformBatchWithLLM(ctx context.Context, exec *ai.AITaskExecutor, batch []transformCandidate) ([]llmTransformDecision, error) {
	input := make([]transformCandidateLite, 0, len(batch))
	for _, candidate := range batch {
		input = append(input, transformCandidateLite{
			ID:                 candidate.ID,
			From:               toProductLite(candidate.From),
			To:                 toProductLite(candidate.To),
			HeuristicScore:     candidate.Score,
			Reasons:            candidate.Reasons,
			DirectEdgeCount:    len(candidate.DirectEdges),
			ActorEvidenceCount: len(candidate.ActorEvidence),
			AssemblyEdgeCount:  len(candidate.AssemblyEdges),
			ActorSamples:       actorSamples(candidate.ActorEvidence),
		})
	}
	payload, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, err
	}

	validate := func(out []llmTransformDecision) error {
		if len(out) != len(input) {
			return fmt.Errorf("expected %d decisions, got %d", len(input), len(out))
		}
		allowed := map[string]struct{}{}
		for _, item := range input {
			allowed[item.ID] = struct{}{}
		}
		seen := map[string]struct{}{}
		for i, item := range out {
			if _, ok := allowed[strings.TrimSpace(item.CandidateID)]; !ok {
				return fmt.Errorf("decision[%d] unknown candidateId", i)
			}
			if _, dup := seen[strings.TrimSpace(item.CandidateID)]; dup {
				return fmt.Errorf("decision[%d] duplicate candidateId", i)
			}
			seen[strings.TrimSpace(item.CandidateID)] = struct{}{}
			if item.Confidence < 0 || item.Confidence > 1 {
				return fmt.Errorf("decision[%d] confidence out of range", i)
			}
		}
		return nil
	}
	verifier := ai.NewGenericVerifier[[]llmTransformDecision](validate)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeAliasEnrichment,
		InputContent: buildTransformReviewPrompt(string(payload)),
		AIRequirements: ai.AIRequirements{
			PreferredModel:           strings.TrimSpace(*llmModel),
			FallbackModels:           []string{"gemini-3-flash-preview", "gemini-2.5-flash-lite", "gemini-2.5-flash"},
			MinContextWindow:         32000,
			MaxOutputSize:            16384,
			MinCapabilityScore:       8,
			TextGenerationCapability: true,
		},
		Verifier: verifier,
	}
	parsedAny, err := exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, err
	}
	parsed, ok := parsedAny.([]llmTransformDecision)
	if !ok {
		return nil, fmt.Errorf("unexpected LLM output type %T", parsedAny)
	}
	return parsed, nil
}

func buildTransformReviewPrompt(payload string) string {
	return "You are reviewing PRODUCT/MATERIAL relation pairs for a supply-chain graph.\n" +
		"You need TRUE canonical product-relation pairs only: upstream input/component/operational input -> direct downstream product.\n" +
		"Accept only when the FROM product can plausibly be processed, refined, converted, manufactured, assembled into, or directly used in the TO product.\n" +
		"Only accept DIRECT or ADJACENT canonical product relation steps. Do NOT accept long multi-step jumps that skip obvious intermediates.\n" +
		"If A usually becomes B, then C, then D, reject A -> D and accept only the nearest canonical step(s).\n" +
		"These are NOT synonyms. If the pair is just two names for the same thing, reject it.\n" +
		"Class-subclass, general-specific, family-member, or grade-variant relationships are NOT canonical product relations and must be rejected.\n" +
		"Logistics, services, routes, handling, freight, ports, compliance, documents, and business activities are NOT products and must be rejected.\n" +
		"Do not accept simple co-occurrence or trading adjacency. Receiving product A and selling product B is not enough unless it clearly implies direct processing, direct assembly, or direct operational usage.\n" +
		"Direct final assembly is allowed when it is the nearest concrete step and there is no useful missing intermediate product in the data. Example: a finished display module -> a specific smartphone model can be accepted. Likewise, finished components such as tires, battery packs, cockpit modules, or powertrains can map directly into a final vehicle when that is the nearest real assembly step.\n" +
		"Direct usage is allowed only when it is a specific and strong product/product dependency, not a broad end-use category. Example: helium -> MRI scanner can be accepted. Example reject: helium -> healthcare industry.\n" +
		"Good accepts: bauxite -> alumina; alumina -> aluminum; crude oil -> naphtha; naphtha -> ethylene; copper concentrate -> blister copper; cathode active material precursor -> cathode active material; battery cell -> battery module; battery module -> battery pack; aluminum ingot -> aluminum sheet; helium -> MRI scanner.\n" +
		"Good rejects: aluminum -> alumina; LNG -> natural gas; sodium hydroxide -> caustic soda; fruit -> pineapple; steel -> automotive steel; maritime freight -> nickel concentrate; port handling services -> maritime freight; battery pack -> LFP battery cell; iron ore -> hot-rolled steel coil; lithium carbonate -> battery cell; semiconductor design -> application processor; nuclear fuel assembly -> electricity; helium -> healthcare industry.\n" +
		"If direction looks backwards, reject it. If evidence looks like generic supply/trade without direct product semantics, reject it. If one side is broader/narrower taxonomy only, reject it. If the pair jumps across several missing intermediates, reject it.\n" +
		"Return EXACTLY one strict JSON array. Do not wrap it in an outer object. Do not add markdown. Do not add prose.\n" +
		"Each array item must use this exact structure:\n" +
		"[\n" +
		"  {\n" +
		"    \"candidateId\": \"<string candidate id from input>\",\n" +
		"    \"accepted\": <true|false>,\n" +
		"    \"confidence\": <number from 0 to 1>,\n" +
		"    \"reason\": \"<short explanation>\"\n" +
		"  }\n" +
		"]\n" +
		"Include every input candidate exactly once. Keep keys spelled exactly as above.\n\n" +
		"Candidates JSON:\n" + payload + "\n\n" +
		"Return strict JSON only."
}

func materializeReview(candidate transformCandidate, accepted bool, source string, confidence float64, reason string) transformReviewEntry {
	return transformReviewEntry{
		CandidateID:      candidate.ID,
		FromFragmentID:   candidate.From.ID,
		FromPath:         candidate.From.CanonicalPath,
		FromName:         candidate.From.PreferredName,
		ToFragmentID:     candidate.To.ID,
		ToPath:           candidate.To.CanonicalPath,
		ToName:           candidate.To.PreferredName,
		Accepted:         accepted,
		Confidence:       confidence,
		DecisionSource:   source,
		Reason:           strings.TrimSpace(reason),
		HeuristicScore:   candidate.Score,
		Reasons:          candidate.Reasons,
		DirectEdges:      candidate.DirectEdges,
		ActorEvidence:    candidate.ActorEvidence,
		AssemblyEvidence: candidate.AssemblyEdges,
		FromStage:        candidate.From.Stage,
		ToStage:          candidate.To.Stage,
		FromStageOrder:   candidate.From.StageOrder,
		ToStageOrder:     candidate.To.StageOrder,
	}
}

func persistAcceptedPairs(ctx context.Context, db *firestore.Client, reviewed []transformReviewEntry) (int, error) {
	existing, err := loadActiveTransformPairDocs(ctx, db)
	if err != nil {
		return 0, err
	}
	desired, synonymDesired := buildDesiredTransformDocs(existing, reviewed)
	if _, err := syncDesiredSynonymDocsTransform(ctx, db, loadActiveSynonymPairDocsTransform(ctx, db), synonymDesired); err != nil {
		return 0, err
	}
	if _, err := syncDesiredTransformDocs(ctx, db, existing, desired); err != nil {
		return 0, err
	}
	return len(desired), nil
}

func backfillRelationKind(ctx context.Context, db *firestore.Client) (updated int, assembly int, usage int, transformation int, err error) {
	type pendingSync struct {
		fromPath     string
		toPath       string
		relationKind string
		active       bool
	}
	col := db.Collection(strings.TrimSpace(*pairCollection))
	iter := col.Where("tenantId", "==", strings.TrimSpace(*tenantID)).Documents(ctx)
	now := firestore.ServerTimestamp

	batch := db.Batch()
	pending := 0
	pendingSyncs := make([]pendingSync, 0, 400)
	flush := func() error {
		if pending == 0 {
			return nil
		}
		if _, err := batch.Commit(ctx); err != nil {
			return err
		}
		if toolProductDictRuntime != nil {
			for _, syncOp := range pendingSyncs {
				if err := toolProductDictRuntime.SyncTransformPairWrite(ctx, strings.TrimSpace(*tenantID), syncOp.fromPath, syncOp.toPath, syncOp.relationKind, syncOp.active); err != nil {
					return err
				}
			}
		}
		batch = db.Batch()
		pending = 0
		pendingSyncs = pendingSyncs[:0]
		return nil
	}

	for {
		doc, derr := iter.Next()
		if derr == iterator.Done {
			break
		}
		if derr != nil {
			return updated, assembly, usage, transformation, derr
		}
		data := doc.Data()
		pair := transformReviewEntry{
			CandidateID:    doc.Ref.ID,
			FromFragmentID: stringValue(data["fromFragmentId"]),
			FromPath:       stringValue(data["fromPath"]),
			FromName:       stringValue(data["fromName"]),
			ToFragmentID:   stringValue(data["toFragmentId"]),
			ToPath:         stringValue(data["toPath"]),
			ToName:         stringValue(data["toName"]),
			Reason:         stringValue(data["reason"]),
			DecisionSource: stringValue(data["decisionSource"]),
		}
		if pair.FromPath == "" || pair.ToPath == "" {
			continue
		}
		relationKind := inferRelationKind(pair)
		switch relationKind {
		case "assembly":
			assembly++
		case "usage":
			usage++
		default:
			transformation++
		}
		batch.Set(doc.Ref, map[string]interface{}{
			"relationKind": relationKind,
			"updatedAt":    now,
		}, firestore.MergeAll)
		pendingSyncs = append(pendingSyncs, pendingSync{
			fromPath:     pair.FromPath,
			toPath:       pair.ToPath,
			relationKind: relationKind,
			active:       !doc.Exists() || boolValue(data["active"]),
		})
		pending++
		updated++
		if pending >= 400 {
			if err := flush(); err != nil {
				return updated, assembly, usage, transformation, err
			}
		}
	}
	if err := flush(); err != nil {
		return updated, assembly, usage, transformation, err
	}
	return updated, assembly, usage, transformation, nil
}

func buildProductRecord(f *models.HorizonFragment) productRecord {
	pathShape, pathSlug, taxonomySig, category, productType, stage := inspectProductPath(strings.TrimSpace(f.CanonicalPath))
	rec := productRecord{
		ID:            strings.TrimSpace(f.ID),
		CanonicalPath: strings.TrimSpace(f.CanonicalPath),
		PreferredName: pickPreferredProductName(f, pathSlug),
		Description:   strings.TrimSpace(f.Description),
		PathShape:     pathShape,
		PathSlug:      pathSlug,
		TaxonomySig:   taxonomySig,
		Category:      category,
		ProductType:   productType,
		Stage:         stage,
		StageOrder:    stageOrder(stage),
		QualityScore:  scoreProductQuality(f, pathShape),
	}
	return rec
}

func inspectProductPath(path string) (shape string, slug string, taxonomySig string, category string, productType string, stage string) {
	parts, err := taxonomy.ParseCanonicalPathParts(strings.TrimSpace(path))
	if err != nil || parts == nil || parts.Namespace != "product" || parts.Product == nil {
		return "", "", "", "", "", ""
	}
	pp := parts.Product
	shape = strings.ToLower(strings.TrimSpace(pp.Shape))
	switch shape {
	case "hierarchical":
		slug = strings.ToLower(strings.TrimSpace(pp.Slug))
		category = strings.ToLower(strings.TrimSpace(pp.Category))
		productType = strings.ToLower(strings.TrimSpace(pp.Type))
		stage = strings.ToLower(strings.TrimSpace(pp.Stage))
		taxonomySig = strings.Join(filterNonEmptyStrings([]string{category, productType, stage}), "|")
	case "service":
		slug = strings.ToLower(strings.TrimSpace(pp.ServiceSlug))
		category = "service"
		taxonomySig = strings.Join(filterNonEmptyStrings([]string{"service", strings.ToLower(strings.TrimSpace(pp.ServiceDomain)), strings.ToLower(strings.TrimSpace(pp.ServiceSubdomain))}), "|")
	default:
		slug = strings.ToLower(strings.TrimSpace(pp.Slug))
	}
	return shape, slug, taxonomySig, category, productType, stage
}

func pickPreferredProductName(f *models.HorizonFragment, fallbackSlug string) string {
	candidates := []string{
		strings.TrimSpace(f.Labels["en"]),
		strings.TrimSpace(f.Identifiers["shortName"]),
		strings.TrimSpace(f.Identifiers["legalName"]),
	}
	for _, candidate := range candidates {
		if isUsableProductName(candidate) {
			return candidate
		}
	}
	if fallbackSlug != "" {
		return slugToTitle(fallbackSlug)
	}
	return strings.TrimSpace(f.CanonicalPath)
}

func scoreProductQuality(f *models.HorizonFragment, pathShape string) int {
	score := 0
	switch pathShape {
	case "hierarchical":
		score += 100
	case "flat":
		score += 50
	case "service":
		score += 10
	}
	if strings.TrimSpace(f.Labels["en"]) != "" {
		score += 15
	}
	if strings.TrimSpace(f.Description) != "" {
		score += 5
	}
	if strings.EqualFold(strings.TrimSpace(f.Status), "active") {
		score += 5
	}
	score += minInt(len(f.KBRefs), 4) * 5
	score += minInt(len(f.Identifiers), 6) * 3
	if !strings.HasPrefix(strings.TrimSpace(f.CanonicalPath), "/discovered/") {
		score += 5
	}
	return score
}

func stageOrder(stage string) int {
	stage = strings.ToLower(strings.TrimSpace(stage))
	if stage == "" {
		return 0
	}
	order := taxonomy.GetProductStageOrder()
	for i, item := range order {
		if strings.EqualFold(strings.TrimSpace(item), stage) {
			return i + 1
		}
	}
	return 0
}

func acceptProductPair(fromRec, toRec productRecord) bool {
	if strings.TrimSpace(fromRec.ID) == "" || strings.TrimSpace(toRec.ID) == "" {
		return false
	}
	if strings.TrimSpace(fromRec.CanonicalPath) == "" || strings.TrimSpace(toRec.CanonicalPath) == "" {
		return false
	}
	if fromRec.CanonicalPath == toRec.CanonicalPath {
		return false
	}
	if !strings.HasPrefix(fromRec.CanonicalPath, "/product/") || !strings.HasPrefix(toRec.CanonicalPath, "/product/") {
		return false
	}
	if strings.EqualFold(fromRec.PathShape, "service") || strings.EqualFold(toRec.PathShape, "service") {
		return false
	}
	if normalizeNameKey(fromRec.PreferredName) == normalizeNameKey(toRec.PreferredName) {
		return false
	}
	if fromRec.PathSlug != "" && fromRec.PathSlug == toRec.PathSlug {
		return false
	}
	return true
}

func scoreTransformCandidate(fromRec, toRec productRecord, item *candidateAccumulator) float64 {
	score := 0.0
	score += math.Min(float64(len(item.DirectEdgeIDs))*3.0, 12.0)
	score += math.Min(float64(len(item.AssemblyEvidence))*2.5, 8.0)
	score += math.Min(float64(len(item.ActorEvidence))*1.2, 8.0)
	if len(item.DirectEdgeIDs) > 0 && len(item.ActorEvidence) > 0 {
		score += 1.25
	}
	if len(item.AssemblyEvidence) > 0 && (len(item.DirectEdgeIDs) > 0 || len(item.ActorEvidence) > 0) {
		score += 1.0
	}
	if fromRec.StageOrder > 0 && toRec.StageOrder > 0 {
		if toRec.StageOrder > fromRec.StageOrder {
			score += 1.0
		} else if toRec.StageOrder == fromRec.StageOrder {
			score += 0.3
		} else {
			score -= 2.0
		}
	}
	if fromRec.PathShape == "hierarchical" && toRec.PathShape == "hierarchical" {
		score += 0.5
	}
	if fromRec.ProductType != "" && toRec.ProductType != "" && fromRec.ProductType == toRec.ProductType {
		score += 0.35
	}
	score += float64(fromRec.QualityScore+toRec.QualityScore) / 400.0
	if normalizeNameKey(fromRec.PreferredName) == normalizeNameKey(toRec.PreferredName) {
		score -= 4
	}
	return score
}

func toProductLite(rec productRecord) productLite {
	return productLite{
		ID:            rec.ID,
		CanonicalPath: rec.CanonicalPath,
		PreferredName: rec.PreferredName,
		Description:   rec.Description,
		PathShape:     rec.PathShape,
		PathSlug:      rec.PathSlug,
		TaxonomySig:   rec.TaxonomySig,
		Category:      rec.Category,
		ProductType:   rec.ProductType,
		Stage:         rec.Stage,
		StageOrder:    rec.StageOrder,
		QualityScore:  rec.QualityScore,
	}
}

func actorSamples(evidence []actorTransformEvidence) []string {
	out := make([]string, 0, minInt(len(evidence), 5))
	seen := map[string]struct{}{}
	for _, item := range evidence {
		value := firstNonEmpty(item.ActorName, item.ActorRef)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func actorDisplayName(actorRef string, actors map[string]actorInfo) string {
	for _, actor := range actors {
		if strings.TrimSpace(actor.CanonicalPath) == strings.TrimSpace(actorRef) {
			return firstNonEmpty(actor.PreferredName, actor.CanonicalPath)
		}
	}
	return actorRef
}

func directEvidenceSlice(set map[string]struct{}) []directTransformEvidence {
	keys := mapKeysSorted(set)
	out := make([]directTransformEvidence, 0, len(keys))
	for _, key := range keys {
		out = append(out, directTransformEvidence{EdgeID: key})
	}
	return out
}

func actorEvidenceSlice(set map[string]actorTransformEvidence) []actorTransformEvidence {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]actorTransformEvidence, 0, len(keys))
	for _, key := range keys {
		out = append(out, set[key])
	}
	return out
}

func assemblyEvidenceSlice(set map[string]assemblyTransformEvidence) []assemblyTransformEvidence {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]assemblyTransformEvidence, 0, len(keys))
	for _, key := range keys {
		out = append(out, set[key])
	}
	return out
}

func transformPairDocID(fromPath, toPath string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(fromPath) + "\n" + strings.TrimSpace(toPath)))
	return hex.EncodeToString(sum[:])
}

func writeReport(path string, report transformReviewReport) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func loadReport(path string) (transformReviewReport, error) {
	var report transformReviewReport
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, err
	}
	return report, nil
}

func printSummary(report transformReviewReport) {
	fmt.Printf("\nTransform Summary\n")
	fmt.Printf("  products_loaded: %d\n", report.Summary.ProductsLoaded)
	fmt.Printf("  synonym_pairs_loaded: %d\n", report.Summary.SynonymPairsLoaded)
	fmt.Printf("  transform_edges_loaded: %d\n", report.Summary.TransformEdgesLoaded)
	fmt.Printf("  produces_edges_loaded: %d\n", report.Summary.ProducesEdgesLoaded)
	fmt.Printf("  supply_links_loaded: %d\n", report.Summary.SupplyLinksLoaded)
	fmt.Printf("  assembly_artifacts_loaded: %d\n", report.Summary.AssemblyArtifactsLoaded)
	fmt.Printf("  candidates_generated: %d\n", report.Summary.CandidatesGenerated)
	fmt.Printf("  candidates_reviewed: %d\n", report.Summary.CandidatesReviewed)
	fmt.Printf("  accepted_pairs: %d\n", report.Summary.AcceptedPairs)
	fmt.Printf("  rejected_pairs: %d\n", report.Summary.RejectedPairs)
	fmt.Printf("  persisted_pairs: %d\n", report.Summary.PersistedPairs)
	fmt.Printf("  llm_enabled: %t\n", report.Summary.LLMEnabled)

	limit := minInt(len(report.Pairs), 25)
	for i := 0; i < limit; i++ {
		pair := report.Pairs[i]
		status := "reject"
		if pair.Accepted {
			status = "accept"
		}
		fmt.Printf("%d. %s conf=%.3f %s -> %s reason=%s\n", i+1, status, pair.Confidence, pair.FromName, pair.ToName, pair.Reason)
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func boolValue(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	default:
		return false
	}
}

func stringValue(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return ""
	}
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func lastSegment(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func normalizeNameKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func slugToTitle(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	parts := strings.FieldsFunc(slug, func(r rune) bool {
		return r == '-' || r == '_' || unicode.IsSpace(r)
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func isUsableProductName(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !strings.HasPrefix(s, "/")
}

func filterNonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}

func mapKeysSorted[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func containsAny(s string, needles ...string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, needle := range needles {
		if strings.Contains(s, strings.ToLower(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func isBatteryMaterialPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return containsAny(path,
		"pcam",
		"cathode-active-material",
		"battery-cathode",
		"battery-anode",
		"lithium-carbonate",
		"lithium-hydroxide",
		"cobalt-sulfate",
		"nickel-sulfate",
		"nickel-cobalt-manganese-sulfate",
	)
}

func isBatteryCellPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return containsAny(path,
		"battery-cell",
		"battery-cells",
		"ev-battery-cell",
		"ev-battery-cells",
		"lithium-ion-cell",
		"lithium-ion-cells",
		"lithium-ion-battery-cell",
		"lithium-ion-cells",
	)
}

func isBatteryLikeOutput(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	return containsAny(s,
		"battery",
		"ev-battery",
		"battery-pack",
		"battery-system",
		"high-voltage-battery-pack",
		"lithium-ion-battery",
	)
}

func isDirectBatteryIntermediate(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	return containsAny(s,
		"battery-cell",
		"battery-cells",
		"ev-battery-cell",
		"ev-battery-cells",
		"lithium-ion-cell",
		"lithium-ion-cells",
		"lithium-ion-battery-cell",
		"lithium-ion-battery-cells",
		"battery-electrode",
		"electrode",
		"cathode",
		"anode",
		"pcam",
		"cathode-active-material",
		"ncm-cathode-active-material",
	)
}

func isDirectBatteryMaterialToCellStep(pair transformReviewEntry) bool {
	if !isImmediateBatteryCellFeedstockPath(pair.FromPath) {
		return false
	}
	if !containsAny(strings.ToLower(strings.TrimSpace(pair.ToPath+" "+pair.ToName)),
		"battery-cell",
		"battery-cells",
		"ev-battery-cell",
		"ev-battery-cells",
		"lithium-ion-cell",
		"lithium-ion-cells",
		"lithium-ion-battery-cell",
		"lithium-ion-battery-cells",
		"unformed-battery-cell",
		"lfp-battery-cell",
		"lfp-battery-cells",
	) {
		return false
	}
	return true
}

func isImmediateBatteryCellFeedstockPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return containsAny(path,
		"cathode-active-material",
		"ncm-cathode-active-material",
		"battery-anode",
		"anode-material",
	)
}

func isUpstreamBatteryPrecursorToCell(pair transformReviewEntry) bool {
	fromPath := strings.ToLower(strings.TrimSpace(pair.FromPath))
	if !containsAny(fromPath,
		"pcam",
		"lithium-carbonate",
		"lithium-hydroxide",
		"battery-grade-lithium-hydroxide",
		"cobalt-sulfate",
		"nickel-sulfate",
		"nickel-cobalt-manganese-sulfate",
	) {
		return false
	}
	return containsAny(strings.ToLower(strings.TrimSpace(pair.ToPath+" "+pair.ToName)),
		"battery-cell",
		"battery-cells",
		"ev-battery-cell",
		"ev-battery-cells",
		"lithium-ion-cell",
		"lithium-ion-cells",
		"lithium-ion-battery-cell",
		"lithium-ion-battery-cells",
		"unformed-battery-cell",
		"lfp-battery-cell",
		"lfp-battery-cells",
	)
}

func isGenericSemiconductorSource(path string, name string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(path, "/semiconductor") || name == "semiconductor"
}

func isSpecificElectronicsAssemblyTarget(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	return containsAny(path,
		"/pcba",
		"/camera-module",
		"/display-module",
		"/liquid-crystal-display-module",
		"/logic-semiconductors",
	)
}

func isVehicleLikeOutput(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	return containsAny(s,
		"electric-vehicle",
		"electric-vehicles",
		"passenger-vehicle",
		"passenger-vehicles",
		"passenger-car",
		"passenger-cars",
		"pickup-truck",
		"suv",
		"sedan",
		"sports-car",
		"sports-cars",
		"luxury-sports-cars",
		"luxury passenger vehicles",
		"/bmw-",
		"/renault-",
		"/chery-",
		"/nissan-",
		"/toyota-",
		"/chevrolet-",
		"/peugeot-",
	)
}

func isNearestFinalAssemblyStep(pair transformReviewEntry) bool {
	if !isVehicleLikeOutput(pair.ToPath, pair.ToName) && !isFinalDeviceLikeOutput(pair.ToPath, pair.ToName) {
		return false
	}
	if isBroadMaterialSource(pair.FromPath, pair.FromName) {
		return false
	}
	if !isTerminalAssemblyTarget(pair.ToPath, pair.ToName) {
		return false
	}
	return isSpecificAssemblyComponentSource(pair.FromPath, pair.FromName)
}

func inferRelationKind(pair transformReviewEntry) string {
	if isUsageLikePair(pair) {
		return "usage"
	}
	if isAssemblyLikePair(pair) {
		return "assembly"
	}
	return "transformation"
}

func isUsageLikePair(pair transformReviewEntry) bool {
	all := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		pair.FromPath,
		pair.ToPath,
		pair.FromName,
		pair.ToName,
		pair.Reason,
		strings.Join(pair.Reasons, " "),
	}, " | ")))
	if all == "" {
		return false
	}
	if containsAny(all,
		" used in ",
		"is used in",
		"used by",
		"coolant",
		"cryogen",
		"shielding gas",
		"process gas",
		"working fluid",
		"reagent",
		"catalyst",
		"etchant",
		"consumable",
		"helium",
	) {
		if containsAny(all, "industry", "sector", "market", "application", "services") {
			return false
		}
		if isAssemblyLikePair(pair) {
			return false
		}
		return true
	}
	return false
}

func isAssemblyLikePair(pair transformReviewEntry) bool {
	if isNearestFinalAssemblyStep(pair) {
		return true
	}
	if hasAssemblyEvidence(pair) && isSpecificAssemblyComponentSource(pair.FromPath, pair.FromName) {
		return true
	}
	if isSpecificAssemblyComponentSource(pair.FromPath, pair.FromName) && isAssemblyLikeTarget(pair.ToPath, pair.ToName) {
		return true
	}
	return false
}

func hasAssemblyEvidence(pair transformReviewEntry) bool {
	return len(pair.AssemblyEvidence) > 0
}

func isFinalDeviceLikeOutput(path string, name string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	name = strings.ToLower(strings.TrimSpace(name))
	if containsAny(path,
		"/product/finished/electronic/assembly/smartphone",
		"/product/finished/electronic/assembly/iphone",
		"/product/finished/electronic/assembly/huawei-mate",
		"/product/finished/electronic/assembly/tablet",
		"/product/finished/electronic/assembly/ipad",
		"/product/finished/electronic/assembly/consumer-electronics",
		"/product/finished/electronic/assembly/electronic-toys",
		"/product/finished/mechanical/assembly/smartphone",
		"/product/finished/mechanical/assembly/tablet",
	) {
		return true
	}
	return name == "smartphone" ||
		name == "iphone" ||
		name == "tablet" ||
		name == "ipad" ||
		name == "consumer electronics" ||
		name == "electronic toys"
}

func isBroadMaterialSource(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	return containsAny(s,
		"steel-coil",
		"steel-sheet",
		"steel-slab",
		"automotive-steel",
		"high-grade-automotive-steel",
		"cold-rolled-steel-sheet",
		"hot-rolled-steel-strip",
		"iron-ore",
		"crude-steel",
		"pig-iron",
		"alumina",
		"aluminum-ingot",
		"aluminium-ingot",
		"polypropylene",
		"synthetic-rubber",
		"natural-rubber",
		"tsr-20",
	)
}

func isSpecificAssemblyComponentSource(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	return containsAny(s,
		"tire",
		"tires",
		"battery-cell",
		"battery-pack",
		"battery module",
		"battery-module",
		"module",
		"powertrain",
		"display-module",
		"display assembly",
		"cockpit",
		"chassis-module",
		"chassis corner module",
		"control-unit",
		"sensor",
		"radar",
		"braking-system",
		"instrument-cluster",
		"seat",
		"seating-system",
		"harness",
		"pcba",
		"printed-circuit-board-assembly",
		"electronic-control-unit",
		"engine-control-unit",
		"semiconductor-package",
		"packaged-semiconductor",
		"semiconductor-chip",
		"logic-chip",
		"microcontroller",
		"chipset",
		"display-driver",
		"optical column",
	)
}

func isAssemblyLikeTarget(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	if isVehicleLikeOutput(path, name) || isFinalDeviceLikeOutput(path, name) {
		return true
	}
	return containsAny(s,
		"module",
		"pack",
		"system",
		"unit",
		"board",
		"logic-board",
		"pcba",
		"printed-circuit-board-assembly",
		"sensor",
		"platform",
		"control-unit",
		"engine-control-unit",
		"instrument-cluster",
		"cockpit",
		"radar",
		"battery-management-system",
		"adas-platform",
		"power-supply-unit",
		"display-module",
		"package",
	)
}

func isTerminalAssemblyTarget(path string, name string) bool {
	s := strings.ToLower(strings.TrimSpace(path + " " + name))
	if isVehicleLikeOutput(path, name) {
		return true
	}
	return containsAny(s,
		"/smartphone",
		"/iphone",
		"/mate-",
		"/tablet",
		"/ipad",
		"sterile-injectables",
		"beverage-inventory",
		"retail-ready-packaged-fruits",
		"whole fresh durian",
		"packaged-fruits",
	)
}
