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
	"unicode"

	"cloud.google.com/go/firestore"
	redis "github.com/redis/go-redis/v9"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	appcache "github.com/igorkirisyuk/sanskrite/internal/cache"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

var toolRedisClient *redis.Client
var toolProductDictRuntime *productdict.Runtime

// TODO: Add a separate service-synonym mining pass and storage flow.
// Keep it isolated from product/material synonyms: service aliases have a different
// error profile (service vs route vs capability vs compliance/process) and should
// use their own prompts, review criteria, and collection.
var (
	mode              = flag.String("mode", "report", "Mode: report, pairs, pair-decisions, repair-supply-links, apply, or dedup-backfill")
	projectID         = flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project ID")
	tenantID          = flag.String("tenant", "global", "Tenant ID")
	pageSize          = flag.Int("page-size", 500, "Firestore page size")
	pageTimeout       = flag.Int("page-timeout-s", 90, "Per-page timeout in seconds (0 disables)")
	startAfterDoc     = flag.String("start-after", "", "Resume cursor: start after Firestore document ID")
	maxFragments      = flag.Int("max-fragments", 0, "Stop after loading this many product fragments (0 = unlimited)")
	minCluster        = flag.Int("min-cluster-size", 2, "Minimum synonym cluster size")
	limit             = flag.Int("limit", 100, "Maximum clusters to print in stdout summary (0 = unlimited)")
	outputPath        = flag.String("out", "", "Optional path to write full JSON report")
	verbose           = flag.Bool("verbose", false, "Verbose logging")
	llmEnabled        = flag.Bool("use-llm", true, "Use LLM to refine candidate clusters into real synonym groups")
	llmModel          = flag.String("llm-model", "gemini-3.1-flash-lite-preview", "LLM model for semantic synonym resolution")
	llmMaxMembers     = flag.Int("llm-max-members", 12, "Skip LLM refinement for heuristic clusters larger than this")
	candidateMode     = flag.String("candidate-mode", "strict", "Candidate generation mode: strict or broad")
	pairsInPath       = flag.String("pairs-in", "", "Optional existing pair-review JSON report to load in pairs mode instead of regenerating")
	reviewedPairsIn   = flag.String("reviewed-pairs-in", "", "Optional comma-separated list of pair-review JSON reports whose pairIds should be excluded from future mining")
	pairOffset        = flag.Int("pair-offset", 0, "Skip this many ranked pair candidates before review/persist")
	pairLimit         = flag.Int("pair-limit", 300, "Maximum pair candidates to retain in pairs mode")
	pairMinScore      = flag.Float64("pair-min-score", 2.25, "Minimum heuristic score for a pair candidate in pairs mode")
	pairsOutPath      = flag.String("pairs-out", "", "Optional path to write pair-review JSON report in pairs mode")
	pairsDecisionsOut = flag.String("pairs-decisions-out", "", "Optional path to write merge decision-set JSON built from active synonym pairs")
	persistPairs      = flag.Bool("persist-pairs", false, "Persist accepted synonym pairs to Firestore in pairs mode")
	pairCollection    = flag.String("pair-collection", "horizon_product_synonym_pairs", "Firestore collection for accepted synonym pairs")
	skipKnownPairs    = flag.Bool("skip-known-pairs", true, "In pairs mode, skip candidates already present in the Firestore synonym-pair collection")
	skipMappedSyns    = flag.Bool("skip-mapped-synonyms", true, "In pairs mode, skip candidates whose synonym path is already assigned in the Firestore synonym-pair collection")
	decisionsPath     = flag.String("decisions", "cmd/tools/horizon_mine_product_synonyms/decisions_global.json", "Curated synonym decisions JSON for apply mode")
	applyLimit        = flag.Int("apply-limit", 0, "Maximum accepted decisions to apply (0 = all)")
	applyReportPath   = flag.String("apply-report", "", "Optional path to write JSON apply report")
	dryRunApply       = flag.Bool("dry-run", false, "In apply mode, validate and print the curated plan without writing changes")
	repairWorklistIn  = flag.String("repair-worklist-in", "", "Optional JSON file with stale oldPath->newPath worklist for repair-supply-links mode")
	repairWorklistOut = flag.String("repair-worklist-out", "", "Optional JSON file to write discovered stale oldPath->newPath worklist in repair-supply-links mode")
)

type productRecord struct {
	Fragment         *models.HorizonFragment
	ID               string
	CanonicalPath    string
	PreferredName    string
	QualityScore     int
	PathShape        string
	PathSlug         string
	TaxonomySlug     string
	TaxonomySig      string
	WikidataID       string
	CASNumber        string
	ChemicalFormula  string
	AliasKeys        []string
	RawAliasNames    []string
	HasFlatPath      bool
	HasHierarchical  bool
	HasServicePath   bool
	AlternativePaths []string
}

type signalGroup struct {
	Type       string
	Value      string
	Confidence float64
	Reason     string
	MemberIdxs []int
}

type unionFind struct {
	parent []int
	rank   []int
}

type clusterMemberReport struct {
	ID              string   `json:"id"`
	CanonicalPath   string   `json:"canonicalPath"`
	PreferredName   string   `json:"preferredName"`
	QualityScore    int      `json:"qualityScore"`
	PathShape       string   `json:"pathShape,omitempty"`
	PathSlug        string   `json:"pathSlug,omitempty"`
	TaxonomySlug    string   `json:"taxonomySlug,omitempty"`
	TaxonomySig     string   `json:"taxonomySignature,omitempty"`
	WikidataID      string   `json:"wikidataId,omitempty"`
	CASNumber       string   `json:"casNumber,omitempty"`
	ChemicalFormula string   `json:"chemicalFormula,omitempty"`
	AliasKeys       []string `json:"aliasKeys,omitempty"`
}

type canonicalReport struct {
	ID            string `json:"id"`
	CanonicalPath string `json:"canonicalPath"`
	PreferredName string `json:"preferredName"`
	QualityScore  int    `json:"qualityScore"`
	Why           string `json:"why"`
}

type clusterSignalReport struct {
	Type       string   `json:"type"`
	Value      string   `json:"value"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	MemberIDs  []string `json:"memberIds"`
}

type clusterReport struct {
	ClusterID          string                `json:"clusterId"`
	ClusterConfidence  float64               `json:"clusterConfidence"`
	DecisionSource     string                `json:"decisionSource"`
	DecisionReason     string                `json:"decisionReason,omitempty"`
	Recommended        canonicalReport       `json:"recommendedCanonical"`
	Members            []clusterMemberReport `json:"members"`
	Signals            []clusterSignalReport `json:"signals"`
	DistinctPathShapes []string              `json:"distinctPathShapes,omitempty"`
	DistinctTaxonomy   []string              `json:"distinctTaxonomySignatures,omitempty"`
}

type reportSummary struct {
	ProductsLoaded     int            `json:"productsLoaded"`
	SignalsAccepted    int            `json:"signalsAccepted"`
	CandidateClusters  int            `json:"candidateClusters"`
	ClustersFound      int            `json:"clustersFound"`
	ClustersPrinted    int            `json:"clustersPrinted"`
	LLMEnabled         bool           `json:"llmEnabled"`
	LLMRefinedClusters int            `json:"llmRefinedClusters"`
	SignalCountsByType map[string]int `json:"signalCountsByType,omitempty"`
	StartAfter         string         `json:"startAfter,omitempty"`
}

type synonymReport struct {
	GeneratedAt string          `json:"generatedAt"`
	ProjectID   string          `json:"projectId"`
	TenantID    string          `json:"tenantId"`
	Summary     reportSummary   `json:"summary"`
	Clusters    []clusterReport `json:"clusters"`
}

type rawCluster struct {
	MemberIdxs []int
	Signals    []signalGroup
}

type llmProductLite struct {
	ID               string   `json:"id"`
	CanonicalPath    string   `json:"canonicalPath"`
	PreferredName    string   `json:"preferredName"`
	Description      string   `json:"description,omitempty"`
	WikidataID       string   `json:"wikidataId,omitempty"`
	CASNumber        string   `json:"casNumber,omitempty"`
	ChemicalFormula  string   `json:"chemicalFormula,omitempty"`
	PathShape        string   `json:"pathShape,omitempty"`
	PathSlug         string   `json:"pathSlug,omitempty"`
	TaxonomySig      string   `json:"taxonomySignature,omitempty"`
	AlternativeNames []string `json:"alternativeNames,omitempty"`
}

type llmClusterInput struct {
	CandidateSignals []clusterSignalReport `json:"candidateSignals"`
	Products         []llmProductLite      `json:"products"`
}

type llmSynonymGroup struct {
	MemberIDs   []string `json:"memberIds"`
	CanonicalID string   `json:"canonicalId"`
	Confidence  float64  `json:"confidence"`
	Reason      string   `json:"reason"`
}

type llmClusterResponse struct {
	SchemaVersion string            `json:"schemaVersion"`
	Groups        []llmSynonymGroup `json:"groups"`
}

func main() {
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *pageSize <= 0 {
		*pageSize = 500
	}
	if *pageSize > 2000 {
		*pageSize = 2000
	}
	if *minCluster < 2 {
		*minCluster = 2
	}
	if *pageTimeout < 0 {
		*pageTimeout = 0
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	toolRedisClient = appcache.NewRedisClient()
	toolProductDictRuntime = productdict.NewRuntime(fsClient.Client()).
		WithTenantID(strings.TrimSpace(*tenantID)).
		WithSynonymCollection(strings.TrimSpace(*pairCollection))
	if toolRedisClient != nil {
		toolProductDictRuntime.WithRedis(toolRedisClient)
	}

	log.Printf("=== Horizon Product Synonym Miner ===")
	log.Printf("project=%s tenant=%s", strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID))
	if toolRedisClient != nil {
		log.Printf("redis_pair_cache=enabled")
	} else {
		log.Printf("redis_pair_cache=disabled")
	}

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "report":
		// Continue below.
	case "pairs":
		products, _, err := loadProducts(ctx, fsClient.Client())
		if err != nil {
			log.Fatalf("failed to load product fragments: %v", err)
		}
		log.Printf("loaded %d product fragments", len(products))
		aiExecutor := initAIExecutor(ctx)
		report, err := runPairsMode(ctx, fsClient.Client(), products, aiExecutor)
		if err != nil {
			log.Fatalf("pairs mode failed: %v", err)
		}
		if path := strings.TrimSpace(*pairsOutPath); path != "" {
			if err := writePairsReport(path, report); err != nil {
				log.Fatalf("failed to write pair report: %v", err)
			}
			log.Printf("wrote pair JSON report: %s", path)
		}
		printPairsSummary(report)
		return
	case "pair-decisions":
		if err := runPairDecisionsMode(ctx, fsClient.Client()); err != nil {
			log.Fatalf("pair-decisions mode failed: %v", err)
		}
		return
	case "apply":
		if err := runApply(ctx, fsClient.Client()); err != nil {
			log.Fatalf("apply failed: %v", err)
		}
		return
	case "repair-supply-links":
		if err := runRepairSupplyLinks(ctx, fsClient.Client()); err != nil {
			log.Fatalf("repair-supply-links failed: %v", err)
		}
		return
	case "dedup-backfill":
		desired, deactivated, err := backfillSemanticDedup(ctx, fsClient.Client())
		if err != nil {
			log.Fatalf("dedup-backfill failed: %v", err)
		}
		fmt.Printf("\nSemantic Dedup Backfill\n")
		fmt.Printf("  desired_active_docs: %d\n", desired)
		fmt.Printf("  deactivated_docs: %d\n", deactivated)
		return
	default:
		log.Fatalf("unsupported --mode=%q (want report|pairs|pair-decisions|repair-supply-links|apply|dedup-backfill)", *mode)
	}

	products, lastSeen, err := loadProducts(ctx, fsClient.Client())
	if err != nil {
		log.Fatalf("failed to load product fragments: %v", err)
	}
	log.Printf("loaded %d product fragments", len(products))

	signals := buildSignalGroups(products)
	log.Printf("accepted %d synonym signals", len(signals))

	aiExecutor := initAIExecutor(ctx)
	report := buildReport(ctx, products, signals, lastSeen, aiExecutor)
	if *outputPath != "" {
		if err := writeReport(*outputPath, report); err != nil {
			log.Fatalf("failed to write report: %v", err)
		}
		log.Printf("wrote JSON report: %s", *outputPath)
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
		log.Printf("WARN: no GEMINI_API_KEY or VERTEX_API_KEY (env/secret), semantic refinement disabled")
		return nil
	}
	mm := ai.NewAIModelManager()
	return ai.NewAITaskExecutor([]ai.AIService{ai.NewGeminiService(apiKey, mm)}, mm)
}

func loadProducts(ctx context.Context, db *firestore.Client) ([]productRecord, string, error) {
	col := db.Collection("horizon_fragments")
	q := col.
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("type", "==", string(models.FragmentTypeProduct)).
		OrderBy(firestore.DocumentID, firestore.Asc)

	out := make([]productRecord, 0, 4096)
	lastSeen := strings.TrimSpace(*startAfterDoc)
	total := 0

	for {
		pageQ := q.Limit(*pageSize)
		if lastSeen != "" {
			pageQ = pageQ.StartAfter(lastSeen)
		}

		pageCtx := ctx
		var cancel context.CancelFunc
		if *pageTimeout > 0 {
			pageCtx, cancel = context.WithTimeout(ctx, time.Duration(*pageTimeout)*time.Second)
		}

		docs, err := pageQ.Documents(pageCtx).GetAll()
		if cancel != nil {
			cancel()
		}
		if err != nil {
			return nil, lastSeen, fmt.Errorf("fetch page start-after=%q: %w", lastSeen, err)
		}
		if len(docs) == 0 {
			break
		}

		for _, doc := range docs {
			lastSeen = strings.TrimSpace(doc.Ref.ID)
			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				if *verbose {
					log.Printf("skip parse error %s: %v", doc.Ref.ID, err)
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
			out = append(out, rec)
			total++

			if total%1000 == 0 {
				log.Printf("  loaded %d products...", total)
			}
			if *maxFragments > 0 && total >= *maxFragments {
				return out, lastSeen, nil
			}
		}

		if len(docs) < *pageSize {
			break
		}
	}

	return out, lastSeen, nil
}

func buildProductRecord(f *models.HorizonFragment) productRecord {
	rec := productRecord{
		Fragment:         f,
		ID:               strings.TrimSpace(f.ID),
		CanonicalPath:    strings.TrimSpace(f.CanonicalPath),
		WikidataID:       strings.TrimSpace(f.Identifiers["wikidataId"]),
		CASNumber:        normalizeCASNumber(f.Identifiers["casNumber"]),
		ChemicalFormula:  normalizeChemicalFormula(f.Identifiers["chemicalFormula"]),
		AlternativePaths: uniqueStrings(f.AlternativePaths),
	}

	rec.PathShape, rec.PathSlug, rec.TaxonomySig = inspectProductPath(rec.CanonicalPath)
	rec.HasFlatPath = rec.PathShape == "flat" || rec.PathShape == "org_scoped_flat"
	rec.HasHierarchical = rec.PathShape == "hierarchical"
	rec.HasServicePath = rec.PathShape == "service"

	rawNames := collectProductNames(f, rec.PathSlug)
	rec.RawAliasNames = rawNames
	rec.PreferredName = pickPreferredProductName(f, rec.PathSlug)
	rec.AliasKeys = buildAliasKeys(rawNames)
	rec.TaxonomySlug = buildTaxonomySlug(rec, rawNames)
	rec.QualityScore = scoreProductQuality(f, rec)

	return rec
}

func buildSignalGroups(products []productRecord) []signalGroup {
	type bucket struct {
		confidence float64
		reason     string
		members    []int
	}

	signalBuckets := map[string]*bucket{}
	add := func(signalType, value string, confidence float64, reason string, idx int) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := signalType + ":" + value
		b := signalBuckets[key]
		if b == nil {
			b = &bucket{confidence: confidence, reason: reason}
			signalBuckets[key] = b
		}
		b.members = append(b.members, idx)
	}

	for i, rec := range products {
		add("canonical_path", rec.CanonicalPath, 1.0, "same canonicalPath", i)
		if rec.WikidataID != "" {
			add("wikidata_id", rec.WikidataID, 1.0, "same Wikidata ID", i)
		}
		if rec.CASNumber != "" {
			add("cas_number", rec.CASNumber, 0.995, "same CAS number", i)
		}
		if rec.ChemicalFormula != "" {
			add("chemical_formula", rec.ChemicalFormula, 0.985, "same chemical formula", i)
		}
		if rec.TaxonomySlug != "" {
			add("taxonomy_slug", rec.TaxonomySlug, 0.94, "same taxonomy-normalized slug", i)
		}
		if rec.PathSlug != "" {
			add("path_slug", rec.PathSlug, 0.90, "same canonical product slug", i)
		}
		for _, aliasKey := range rec.AliasKeys {
			add("alias_key", aliasKey, 0.89, "same normalized alias key", i)
		}
	}

	out := make([]signalGroup, 0, len(signalBuckets))
	for key, bucket := range signalBuckets {
		members := uniqueInts(bucket.members)
		if len(members) < *minCluster {
			continue
		}
		signalType, value, _ := strings.Cut(key, ":")
		group := signalGroup{
			Type:       signalType,
			Value:      value,
			Confidence: bucket.confidence,
			Reason:     bucket.reason,
			MemberIdxs: members,
		}
		if !acceptSignalGroup(products, group) {
			continue
		}
		out = append(out, group)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if len(out[i].MemberIdxs) != len(out[j].MemberIdxs) {
			return len(out[i].MemberIdxs) > len(out[j].MemberIdxs)
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func acceptSignalGroup(products []productRecord, group signalGroup) bool {
	if len(group.MemberIdxs) < *minCluster {
		return false
	}

	switch group.Type {
	case "canonical_path", "wikidata_id", "cas_number", "chemical_formula":
		return true
	case "taxonomy_slug":
		return safeSlugLikeGroup(products, group.MemberIdxs, true)
	case "path_slug":
		return safeSlugLikeGroup(products, group.MemberIdxs, false)
	case "alias_key":
		return safeAliasGroup(products, group.MemberIdxs, group.Value)
	default:
		return false
	}
}

func safeSlugLikeGroup(products []productRecord, memberIdxs []int, taxonomyBased bool) bool {
	if len(memberIdxs) < *minCluster {
		return false
	}
	flatCount := 0
	taxonomySigs := map[string]struct{}{}
	nonEmptyTaxonomy := 0
	for _, idx := range memberIdxs {
		rec := products[idx]
		if rec.HasFlatPath {
			flatCount++
		}
		if sig := strings.TrimSpace(rec.TaxonomySig); sig != "" {
			taxonomySigs[sig] = struct{}{}
			nonEmptyTaxonomy++
		}
	}
	if len(taxonomySigs) <= 1 {
		return true
	}
	if flatCount > 0 {
		return true
	}
	if taxonomyBased && nonEmptyTaxonomy == 0 {
		return true
	}
	return false
}

func safeAliasGroup(products []productRecord, memberIdxs []int, aliasKey string) bool {
	aliasKey = strings.TrimSpace(aliasKey)
	if aliasKey == "" || len(memberIdxs) < *minCluster {
		return false
	}
	if isProbablyGenericAlias(aliasKey) {
		return false
	}

	taxonomySigs := map[string]struct{}{}
	flatCount := 0
	for _, idx := range memberIdxs {
		rec := products[idx]
		if rec.HasFlatPath {
			flatCount++
		}
		if rec.TaxonomySig != "" {
			taxonomySigs[rec.TaxonomySig] = struct{}{}
		}
	}
	if len(taxonomySigs) <= 1 {
		return true
	}
	return flatCount > 0
}

func buildReport(ctx context.Context, products []productRecord, signals []signalGroup, lastSeen string, aiExecutor *ai.AITaskExecutor) synonymReport {
	uf := newUnionFind(len(products))
	for _, sg := range signals {
		if len(sg.MemberIdxs) < 2 {
			continue
		}
		head := sg.MemberIdxs[0]
		for _, idx := range sg.MemberIdxs[1:] {
			uf.union(head, idx)
		}
	}

	clusterMembers := map[int][]int{}
	for i := range products {
		root := uf.find(i)
		clusterMembers[root] = append(clusterMembers[root], i)
	}

	signalCounts := map[string]int{}
	for _, sg := range signals {
		signalCounts[sg.Type]++
	}

	rawClusters := make([]rawCluster, 0, len(clusterMembers))
	for _, memberIdxs := range clusterMembers {
		if len(memberIdxs) < *minCluster {
			continue
		}
		clusterSignals := collectClusterSignals(products, signals, memberIdxs)
		if len(clusterSignals) == 0 {
			continue
		}
		rawClusters = append(rawClusters, rawCluster{
			MemberIdxs: memberIdxs,
			Signals:    clusterSignals,
		})
	}

	clusters := make([]clusterReport, 0, len(rawClusters))
	llmRefined := 0
	if aiExecutor != nil && *llmEnabled {
		for _, candidate := range rawClusters {
			refined, usedLLM := refineClusterWithLLM(ctx, aiExecutor, products, candidate)
			if usedLLM {
				llmRefined++
			}
			clusters = append(clusters, refined...)
		}
	} else {
		for _, candidate := range rawClusters {
			clusters = append(clusters, materializeClusterReport(products, candidate.MemberIdxs, candidate.Signals, "heuristic", "semantic adjudication disabled", 0))
		}
	}

	sort.SliceStable(clusters, func(i, j int) bool {
		if clusters[i].ClusterConfidence != clusters[j].ClusterConfidence {
			return clusters[i].ClusterConfidence > clusters[j].ClusterConfidence
		}
		if len(clusters[i].Members) != len(clusters[j].Members) {
			return len(clusters[i].Members) > len(clusters[j].Members)
		}
		return clusters[i].ClusterID < clusters[j].ClusterID
	})

	printed := len(clusters)
	if *limit > 0 && printed > *limit {
		printed = *limit
	}

	return synonymReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		ProjectID:   strings.TrimSpace(*projectID),
		TenantID:    strings.TrimSpace(*tenantID),
		Summary: reportSummary{
			ProductsLoaded:     len(products),
			SignalsAccepted:    len(signals),
			CandidateClusters:  len(rawClusters),
			ClustersFound:      len(clusters),
			ClustersPrinted:    printed,
			LLMEnabled:         aiExecutor != nil && *llmEnabled,
			LLMRefinedClusters: llmRefined,
			SignalCountsByType: signalCounts,
			StartAfter:         lastSeen,
		},
		Clusters: clusters,
	}
}

func collectClusterSignals(products []productRecord, signals []signalGroup, memberIdxs []int) []signalGroup {
	memberSet := map[int]struct{}{}
	for _, idx := range memberIdxs {
		memberSet[idx] = struct{}{}
	}
	out := make([]signalGroup, 0, 8)
	for _, sg := range signals {
		if len(sg.MemberIdxs) < 2 {
			continue
		}
		inCount := 0
		for _, idx := range sg.MemberIdxs {
			if _, ok := memberSet[idx]; ok {
				inCount++
			}
		}
		if inCount < 2 {
			continue
		}
		out = append(out, sg)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func refineClusterWithLLM(ctx context.Context, exec *ai.AITaskExecutor, products []productRecord, candidate rawCluster) ([]clusterReport, bool) {
	if exec == nil || !*llmEnabled {
		return []clusterReport{
			materializeClusterReport(products, candidate.MemberIdxs, candidate.Signals, "heuristic", "LLM disabled", 0),
		}, false
	}
	if *llmMaxMembers > 0 && len(candidate.MemberIdxs) > *llmMaxMembers {
		reason := fmt.Sprintf("candidate cluster too large for semantic adjudication (%d > %d)", len(candidate.MemberIdxs), *llmMaxMembers)
		return []clusterReport{
			materializeClusterReport(products, candidate.MemberIdxs, candidate.Signals, "heuristic", reason, 0),
		}, false
	}

	resp, err := adjudicateClusterWithLLM(ctx, exec, products, candidate)
	if err != nil {
		reason := fmt.Sprintf("LLM refinement failed: %v", err)
		return []clusterReport{
			materializeClusterReport(products, candidate.MemberIdxs, candidate.Signals, "heuristic_fallback", reason, 0),
		}, true
	}
	if len(resp.Groups) == 0 {
		return nil, true
	}

	idToIdx := map[string]int{}
	for _, idx := range candidate.MemberIdxs {
		idToIdx[products[idx].ID] = idx
	}

	out := make([]clusterReport, 0, len(resp.Groups))
	for _, group := range resp.Groups {
		memberIdxs := make([]int, 0, len(group.MemberIDs))
		for _, id := range uniqueStrings(group.MemberIDs) {
			if idx, ok := idToIdx[id]; ok {
				memberIdxs = append(memberIdxs, idx)
			}
		}
		memberIdxs = uniqueInts(memberIdxs)
		if len(memberIdxs) < *minCluster {
			continue
		}
		groupSignals := collectClusterSignals(products, candidate.Signals, memberIdxs)
		report := materializeClusterReport(products, memberIdxs, groupSignals, "llm", group.Reason, group.Confidence)
		if canonicalID := strings.TrimSpace(group.CanonicalID); canonicalID != "" {
			for _, member := range report.Members {
				if member.ID == canonicalID {
					report.Recommended = canonicalReport{
						ID:            member.ID,
						CanonicalPath: member.CanonicalPath,
						PreferredName: member.PreferredName,
						QualityScore:  member.QualityScore,
						Why:           "llm_selected_canonical",
					}
					break
				}
			}
		}
		out = append(out, report)
	}
	return out, true
}

func adjudicateClusterWithLLM(ctx context.Context, exec *ai.AITaskExecutor, products []productRecord, candidate rawCluster) (llmClusterResponse, error) {
	var zero llmClusterResponse

	input := llmClusterInput{
		CandidateSignals: make([]clusterSignalReport, 0, len(candidate.Signals)),
		Products:         make([]llmProductLite, 0, len(candidate.MemberIdxs)),
	}
	for _, sg := range candidate.Signals {
		memberIDs := make([]string, 0, len(sg.MemberIdxs))
		for _, idx := range sg.MemberIdxs {
			if idx >= 0 && idx < len(products) {
				memberIDs = append(memberIDs, products[idx].ID)
			}
		}
		input.CandidateSignals = append(input.CandidateSignals, clusterSignalReport{
			Type:       sg.Type,
			Value:      sg.Value,
			Confidence: sg.Confidence,
			Reason:     sg.Reason,
			MemberIDs:  uniqueStrings(memberIDs),
		})
	}
	for _, idx := range candidate.MemberIdxs {
		rec := products[idx]
		input.Products = append(input.Products, llmProductLite{
			ID:               rec.ID,
			CanonicalPath:    rec.CanonicalPath,
			PreferredName:    rec.PreferredName,
			Description:      strings.TrimSpace(rec.Fragment.Description),
			WikidataID:       rec.WikidataID,
			CASNumber:        rec.CASNumber,
			ChemicalFormula:  rec.ChemicalFormula,
			PathShape:        rec.PathShape,
			PathSlug:         rec.PathSlug,
			TaxonomySig:      rec.TaxonomySig,
			AlternativeNames: uniqueStrings(rec.Fragment.AlternativeNames),
		})
	}

	payload, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return zero, err
	}

	verifier := ai.NewGenericVerifier[llmClusterResponse](func(out llmClusterResponse) error {
		if strings.TrimSpace(out.SchemaVersion) == "" {
			return fmt.Errorf("schemaVersion is required")
		}
		allowed := map[string]struct{}{}
		for _, idx := range candidate.MemberIdxs {
			allowed[products[idx].ID] = struct{}{}
		}
		used := map[string]struct{}{}
		for i, group := range out.Groups {
			if len(group.MemberIDs) < 2 {
				return fmt.Errorf("groups[%d] must contain at least 2 memberIds", i)
			}
			if group.Confidence < 0 || group.Confidence > 1 {
				return fmt.Errorf("groups[%d].confidence out of range", i)
			}
			if strings.TrimSpace(group.Reason) == "" {
				return fmt.Errorf("groups[%d].reason is required", i)
			}
			if canonicalID := strings.TrimSpace(group.CanonicalID); canonicalID != "" {
				if _, ok := allowed[canonicalID]; !ok {
					return fmt.Errorf("groups[%d].canonicalId is unknown: %s", i, canonicalID)
				}
			}
			for _, memberID := range uniqueStrings(group.MemberIDs) {
				if _, ok := allowed[memberID]; !ok {
					return fmt.Errorf("groups[%d] contains unknown memberId: %s", i, memberID)
				}
				if _, ok := used[memberID]; ok {
					return fmt.Errorf("memberId %s appears in multiple groups", memberID)
				}
				used[memberID] = struct{}{}
			}
		}
		return nil
	})

	prompt := "You are resolving PRODUCT identity in a supply-chain graph.\n" +
		"Return only groups of fragments that are truly the SAME product under different names, aliases, duplicate canonical paths, abbreviations, spelling variants, or exact identifier matches.\n" +
		"Be conservative.\n" +
		"Do NOT group products that are related by value-chain transformation or production stage.\n" +
		"Examples of NOT synonyms: aluminium vs alumina, helium vs liquefied helium, ore vs concentrate, concentrate vs refined metal, gas vs LNG, battery cell vs cathode material.\n" +
		"Use identifiers first: Wikidata ID, CAS number, chemical formula. Use names and canonical paths next.\n" +
		"If no fragments in this candidate cluster are true synonyms, return zero groups.\n\n" +
		"Return EXACTLY one strict JSON object with this structure and exact keys:\n" +
		"{\n" +
		"  \"schemaVersion\": \"v1\",\n" +
		"  \"groups\": [\n" +
		"    {\n" +
		"      \"memberIds\": [\"<fragment id>\", \"<fragment id>\"],\n" +
		"      \"canonicalId\": \"<fragment id>\",\n" +
		"      \"confidence\": <number from 0 to 1>,\n" +
		"      \"reason\": \"<short explanation>\"\n" +
		"    }\n" +
		"  ]\n" +
		"}\n" +
		"Do not add markdown. Do not add prose.\n\n" +
		"Candidate cluster JSON:\n" + string(payload) + "\n\n" +
		"Return strict JSON only. No markdown."

	task := &ai.AITask{
		TaskType:     ai.TaskTypeAliasEnrichment,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           strings.TrimSpace(*llmModel),
			MinContextWindow:         16000,
			MaxOutputSize:            16384,
			MinCapabilityScore:       8,
			TextGenerationCapability: true,
		},
		Verifier: verifier,
	}

	parsedAny, err := exec.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return zero, err
	}
	parsed, ok := parsedAny.(llmClusterResponse)
	if !ok {
		return zero, fmt.Errorf("unexpected LLM output type %T", parsedAny)
	}
	return parsed, nil
}

func materializeClusterReport(products []productRecord, memberIdxs []int, signals []signalGroup, decisionSource, decisionReason string, confidenceOverride float64) clusterReport {
	records := make([]productRecord, 0, len(memberIdxs))
	for _, idx := range memberIdxs {
		records = append(records, products[idx])
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].QualityScore != records[j].QualityScore {
			return records[i].QualityScore > records[j].QualityScore
		}
		if records[i].PreferredName != records[j].PreferredName {
			return records[i].PreferredName < records[j].PreferredName
		}
		return records[i].CanonicalPath < records[j].CanonicalPath
	})

	winner := records[0]
	why := winnerReason(records)

	memberReports := make([]clusterMemberReport, 0, len(records))
	shapes := map[string]struct{}{}
	taxonomySigs := map[string]struct{}{}
	for _, rec := range records {
		memberReports = append(memberReports, clusterMemberReport{
			ID:              rec.ID,
			CanonicalPath:   rec.CanonicalPath,
			PreferredName:   rec.PreferredName,
			QualityScore:    rec.QualityScore,
			PathShape:       rec.PathShape,
			PathSlug:        rec.PathSlug,
			TaxonomySlug:    rec.TaxonomySlug,
			TaxonomySig:     rec.TaxonomySig,
			WikidataID:      rec.WikidataID,
			CASNumber:       rec.CASNumber,
			ChemicalFormula: rec.ChemicalFormula,
			AliasKeys:       rec.AliasKeys,
		})
		if rec.PathShape != "" {
			shapes[rec.PathShape] = struct{}{}
		}
		if rec.TaxonomySig != "" {
			taxonomySigs[rec.TaxonomySig] = struct{}{}
		}
	}

	signalReports := make([]clusterSignalReport, 0, len(signals))
	maxConfidence := 0.0
	for _, sg := range signals {
		memberIDs := make([]string, 0, len(sg.MemberIdxs))
		for _, idx := range sg.MemberIdxs {
			if idx >= 0 && idx < len(products) {
				memberIDs = append(memberIDs, products[idx].ID)
			}
		}
		memberIDs = uniqueStrings(memberIDs)
		signalReports = append(signalReports, clusterSignalReport{
			Type:       sg.Type,
			Value:      sg.Value,
			Confidence: sg.Confidence,
			Reason:     sg.Reason,
			MemberIDs:  memberIDs,
		})
		if sg.Confidence > maxConfidence {
			maxConfidence = sg.Confidence
		}
	}

	clusterID := fmt.Sprintf("%s__%s", winner.ID, winner.PathSlug)
	clusterID = strings.Trim(clusterID, "_")
	if clusterID == "" {
		clusterID = winner.ID
	}
	if confidenceOverride > 0 {
		maxConfidence = confidenceOverride
	}

	return clusterReport{
		ClusterID:         clusterID,
		ClusterConfidence: maxConfidence,
		DecisionSource:    decisionSource,
		DecisionReason:    decisionReason,
		Recommended: canonicalReport{
			ID:            winner.ID,
			CanonicalPath: winner.CanonicalPath,
			PreferredName: winner.PreferredName,
			QualityScore:  winner.QualityScore,
			Why:           why,
		},
		Members:            memberReports,
		Signals:            signalReports,
		DistinctPathShapes: mapKeysSorted(shapes),
		DistinctTaxonomy:   mapKeysSorted(taxonomySigs),
	}
}

func winnerReason(records []productRecord) string {
	if len(records) == 0 {
		return ""
	}
	winner := records[0]
	reasons := make([]string, 0, 5)
	if winner.WikidataID != "" {
		reasons = append(reasons, "has_wikidata")
	}
	if winner.CASNumber != "" {
		reasons = append(reasons, "has_cas")
	}
	if winner.ChemicalFormula != "" {
		reasons = append(reasons, "has_formula")
	}
	if winner.HasHierarchical {
		reasons = append(reasons, "hierarchical_product_path")
	} else if winner.HasServicePath {
		reasons = append(reasons, "service_product_path")
	}
	if winner.Fragment.Metadata.EpistemicStatus == models.EpistemicStatusVerified {
		reasons = append(reasons, "verified")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "highest_quality_score")
	}
	return strings.Join(reasons, ",")
}

func writeReport(path string, report synonymReport) error {
	f, err := os.Create(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printSummary(report synonymReport) {
	fmt.Printf("\nSummary\n")
	fmt.Printf("  products_loaded: %d\n", report.Summary.ProductsLoaded)
	fmt.Printf("  signals_accepted: %d\n", report.Summary.SignalsAccepted)
	fmt.Printf("  candidate_clusters: %d\n", report.Summary.CandidateClusters)
	fmt.Printf("  clusters_found: %d\n", report.Summary.ClustersFound)
	fmt.Printf("  llm_enabled: %v\n", report.Summary.LLMEnabled)
	fmt.Printf("  llm_refined_clusters: %d\n", report.Summary.LLMRefinedClusters)
	if report.Summary.StartAfter != "" {
		fmt.Printf("  last_seen_doc: %s\n", report.Summary.StartAfter)
	}

	if len(report.Summary.SignalCountsByType) > 0 {
		keys := make([]string, 0, len(report.Summary.SignalCountsByType))
		for k := range report.Summary.SignalCountsByType {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf("\nSignals\n")
		for _, k := range keys {
			fmt.Printf("  %s: %d\n", k, report.Summary.SignalCountsByType[k])
		}
	}

	clusters := report.Clusters
	if *limit > 0 && len(clusters) > *limit {
		clusters = clusters[:*limit]
	}
	if len(clusters) == 0 {
		fmt.Printf("\nNo synonym clusters found.\n")
		return
	}

	fmt.Printf("\nTop Clusters\n")
	for i, cluster := range clusters {
		fmt.Printf("%d. conf=%.3f members=%d canonical=%s [%s]\n",
			i+1,
			cluster.ClusterConfidence,
			len(cluster.Members),
			cluster.Recommended.CanonicalPath,
			cluster.Recommended.PreferredName,
		)
		fmt.Printf("   decision=%s\n", cluster.DecisionSource)
		if cluster.DecisionReason != "" {
			fmt.Printf("   reason=%s\n", cluster.DecisionReason)
		}
		if len(cluster.Signals) > 0 {
			sig := cluster.Signals[0]
			fmt.Printf("   top_signal=%s:%s\n", sig.Type, sig.Value)
		}
		if *verbose {
			for _, member := range cluster.Members {
				fmt.Printf("   - %s | %s | score=%d\n", member.ID, member.CanonicalPath, member.QualityScore)
			}
		}
	}
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

func collectProductNames(f *models.HorizonFragment, pathSlug string) []string {
	names := []string{
		strings.TrimSpace(f.Labels["en"]),
		strings.TrimSpace(f.Identifiers["shortName"]),
		strings.TrimSpace(f.Identifiers["legalName"]),
	}
	names = append(names, f.AlternativeNames...)
	for _, entry := range f.NameHistory {
		if s := strings.TrimSpace(entry.Name); s != "" {
			names = append(names, s)
		}
	}
	if pathSlug != "" {
		names = append(names, slugToTitle(pathSlug), pathSlug)
	}
	return uniqueStrings(filterNonEmptyStrings(names))
}

func buildAliasKeys(names []string) []string {
	keys := make([]string, 0, len(names))
	for _, name := range names {
		key := normalizeProductAliasKey(name)
		if key == "" || isProbablyGenericAlias(key) {
			continue
		}
		keys = append(keys, key)
	}
	return uniqueStrings(keys)
}

func buildTaxonomySlug(rec productRecord, names []string) string {
	candidates := make([]string, 0, len(names)+2)
	candidates = append(candidates, names...)
	if rec.PathSlug != "" {
		candidates = append(candidates, rec.PathSlug)
	}
	if rec.PreferredName != "" {
		candidates = append(candidates, rec.PreferredName)
	}
	for _, candidate := range candidates {
		slug := strings.TrimSpace(taxonomy.NormalizeProductName(candidate))
		if slug == "" {
			continue
		}
		if rec.PathSlug != "" && slug == rec.PathSlug {
			return slug
		}
		if normalizeProductAliasKey(candidate) != normalizeProductAliasKey(slug) {
			return slug
		}
	}
	if rec.PathSlug != "" {
		return rec.PathSlug
	}
	return ""
}

func inspectProductPath(path string) (shape string, slug string, taxonomySig string) {
	parts, err := taxonomy.ParseCanonicalPathParts(strings.TrimSpace(path))
	if err != nil || parts == nil || parts.Namespace != "product" || parts.Product == nil {
		return "", "", ""
	}
	pp := parts.Product
	shape = strings.ToLower(strings.TrimSpace(pp.Shape))
	switch shape {
	case "hierarchical":
		slug = strings.ToLower(strings.TrimSpace(pp.Slug))
		if pp.Category != "" || pp.Type != "" || pp.Stage != "" {
			taxonomySig = strings.Join([]string{
				strings.ToLower(strings.TrimSpace(pp.Category)),
				strings.ToLower(strings.TrimSpace(pp.Type)),
				strings.ToLower(strings.TrimSpace(pp.Stage)),
			}, "|")
		}
	case "service":
		slug = strings.ToLower(strings.TrimSpace(pp.ServiceSlug))
		taxonomySig = strings.Join([]string{
			"service",
			strings.ToLower(strings.TrimSpace(pp.ServiceDomain)),
			strings.ToLower(strings.TrimSpace(pp.ServiceSubdomain)),
		}, "|")
	case "flat":
		slug = strings.ToLower(strings.TrimSpace(pp.Slug))
	case "org_scoped_flat":
		slug = strings.ToLower(strings.TrimSpace(pp.Slug))
	default:
		slug = strings.ToLower(strings.TrimSpace(pp.Slug))
	}
	return shape, slug, strings.Trim(taxonomySig, "|")
}

func scoreProductQuality(f *models.HorizonFragment, rec productRecord) int {
	score := 0
	if rec.WikidataID != "" {
		score += 120
	}
	if rec.CASNumber != "" {
		score += 110
	}
	if rec.ChemicalFormula != "" {
		score += 100
	}
	switch rec.PathShape {
	case "hierarchical":
		score += 90
	case "service":
		score += 85
	case "flat":
		score += 55
	case "org_scoped_flat":
		score += 25
	}
	if strings.TrimSpace(f.Labels["en"]) != "" {
		score += 20
	}
	if strings.TrimSpace(f.Identifiers["shortName"]) != "" {
		score += 15
	}
	if strings.TrimSpace(f.Identifiers["legalName"]) != "" {
		score += 10
	}
	if strings.TrimSpace(f.Description) != "" {
		score += 10
	}
	if f.Metadata.EpistemicStatus == models.EpistemicStatusVerified {
		score += 20
	}
	if status := strings.TrimSpace(f.Status); status != "" && strings.EqualFold(status, "active") {
		score += 8
	}
	score += minInt(len(f.KBRefs), 5) * 6
	score += minInt(len(f.Identifiers), 8) * 4
	score += minInt(len(f.AlternativeNames), 5) * 2
	if !f.CreatedAt.IsZero() && time.Since(f.CreatedAt) > 30*24*time.Hour {
		score += 5
	}
	if !strings.HasPrefix(rec.CanonicalPath, "/discovered/") {
		score += 6
	}
	return score
}

func normalizeCASNumber(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range in {
		if (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeChemicalFormula(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range in {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '(' || r == ')' || r == '[' || r == ']':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeProductAliasKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	prevClass := ""
	for _, r := range name {
		isLetter := unicode.IsLetter(r)
		isDigit := unicode.IsDigit(r)
		if isLetter || isDigit {
			curClass := "letter"
			if isDigit {
				curClass = "digit"
			}
			if (prevClass == "letter" && curClass == "digit") || (prevClass == "digit" && curClass == "letter") {
				if !prevSpace {
					b.WriteByte(' ')
				}
				prevSpace = true
			}
			b.WriteRune(r)
			prevSpace = false
			prevClass = curClass
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
		prevClass = "other"
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	return strings.TrimSpace(out)
}

func isProbablyGenericAlias(aliasKey string) bool {
	if aliasKey == "" {
		return true
	}
	fields := strings.Fields(aliasKey)
	if len(fields) == 0 {
		return true
	}
	if len(fields) == 1 {
		switch fields[0] {
		case "product", "material", "materials", "service", "services", "goods", "commodity", "commodities", "chemical", "chemicals":
			return true
		}
	}
	return false
}

func isUsableProductName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	return lower != "unknown" && lower != "n/a" && lower != "na"
}

func slugToTitle(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		rs := []rune(part)
		rs[0] = unicode.ToUpper(rs[0])
		parts[i] = string(rs)
	}
	return strings.Join(parts, " ")
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
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
	sort.Strings(out)
	return out
}

func uniqueInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func filterNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func mapKeysSorted[K comparable](m map[string]K) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

func newUnionFind(size int) *unionFind {
	parent := make([]int, size)
	rank := make([]int, size)
	for i := range parent {
		parent[i] = i
	}
	return &unionFind{parent: parent, rank: rank}
}

func (u *unionFind) find(x int) int {
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}
	return u.parent[x]
}

func (u *unionFind) union(a, b int) {
	ra := u.find(a)
	rb := u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		u.parent[ra] = rb
		return
	}
	if u.rank[ra] > u.rank[rb] {
		u.parent[rb] = ra
		return
	}
	u.parent[rb] = ra
	u.rank[ra]++
}
