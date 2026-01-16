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
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Unified LLM backfill tool (scaffold).
//
// This command will unify scan/pagination/cursor/batching/LLM/apply across multiple backfill features.
// Implementation is split across multiple files in this directory.

type feature string

const (
	featureOrgClassifiers feature = "org_classifiers"
	featureMarketProfile  feature = "market_profile"
	featureMarketMetrics  feature = "market_metrics"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")

	featuresFlag   = flag.String("features", "market_metrics,market_profile", "Comma-separated list of features: org_classifiers,market_profile,market_metrics")
	typesFlag      = flag.String("types", "", "Optional comma-separated fragment types to scan (e.g., ORGANIZATION,MARKET). If empty, derived from features.")
	orgClassifiers = flag.Bool("org-classifiers", false, "Enable LLM backfill for orgSize + marketSegments (adds feature org_classifiers)")

	model           = flag.String("model", "gemini-3-flash-preview", "Model name or alias")
	engine          = flag.String("engine", "task_executor", "LLM engine: task_executor|direct")
	grounding       = flag.Bool("grounding", false, "Enable search grounding")
	minCapScore     = flag.Int("min-capability-score", 0, "Override MinCapabilityScore (0 = derive from model registry)")
	maxOutputTokens = flag.Int("max-output-tokens", 8192, "Max output tokens budget (>=8192 recommended for structured JSON)")
	llmConcurrency  = flag.Int("llm-concurrency", 1, "How many LLM batch calls to run in parallel (>=1) (not used yet; sequential MVP)")

	apply                  = flag.Bool("apply", false, "Apply fragment updates (default: dry-run)")
	outPath                = flag.String("out", "", "Output JSONL file (default: stdout)")
	limit                  = flag.Int("limit", 50, "How many fragments to backfill (0 = unlimited)")
	batchSize              = flag.Int("batch-size", 10, "Batch size per LLM request")
	pageSize               = flag.Int("page-size", 500, "Firestore page size")
	maxScan                = flag.Int("max-scan", 50000, "Max fragments to scan before stopping (safety cap)")
	startAfter             = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID (__name__)")
	cursorFile             = flag.String("cursor-file", "", "Optional cursor file for resume/persist")
	saveCursorEveryPages   = flag.Int("save-cursor-every-pages", 1, "Persist cursor after every N scanned pages when --cursor-file is set (minimum 1)")
	scanProgressEveryPages = flag.Int("scan-progress-every-pages", 10, "Log scan progress every N pages during scan (0 disables)")

	// org_classifiers selection behavior
	requireMissing = flag.String("require-missing", "either", "For org_classifiers selection: either|both|orgsize|segments")
	includeNA      = flag.Bool("include-na", true, "Treat orgSize='na' as missing for selection (default true)")
	strictSegments = flag.Bool("strict-segments", true, "Require marketSegments to already be canonical (/segment/{domain}/{vertical}/{geo})")

	// apply behavior per feature
	overwriteOrgSize   = flag.Bool("overwrite-orgsize", false, "Overwrite existing orgSize (default: fill-only)")
	overwriteSegments  = flag.Bool("overwrite-segments", false, "Overwrite existing marketSegments (default: union/fill-only)")
	overwriteMarket    = flag.Bool("overwrite-market-profile", false, "Overwrite marketDomain/marketVertical/marketChannels (default: fill-only)")
	overwriteMarketMet = flag.Bool("overwrite-market-metrics", false, "Overwrite /metric/market/* metrics (default: fill-only)")

	segmentDictVersion = flag.String("segment-dict-version", "v1", "Market segment dictionary version for normalization (Firestore doc id)")
	segmentDictRefresh = flag.Bool("segment-dict-refresh", true, "Load market segment dictionary from Firestore for normalization")
)

type Row struct {
	FragmentID    string              `json:"fragmentId"`
	CanonicalPath string              `json:"canonicalPath,omitempty"`
	Type          models.FragmentType `json:"type,omitempty"`
	Features      []string            `json:"features,omitempty"`
	// Optional: helps debug why a fragment was selected / what was filled.
	SelectedBecause []string `json:"selectedBecause,omitempty"`

	OldOrgSize  string   `json:"oldOrgSize,omitempty"`
	OldSegments []string `json:"oldMarketSegments,omitempty"`
	NewOrgSize  string   `json:"newOrgSize,omitempty"`
	NewSegments []string `json:"newMarketSegments,omitempty"`

	OldMarketDomain   string   `json:"oldMarketDomain,omitempty"`
	OldMarketVertical string   `json:"oldMarketVertical,omitempty"`
	OldMarketChannels []string `json:"oldMarketChannels,omitempty"`

	NewMarketDomain   string   `json:"newMarketDomain,omitempty"`
	NewMarketVertical string   `json:"newMarketVertical,omitempty"`
	NewMarketChannels []string `json:"newMarketChannels,omitempty"`

	OldMarketState map[string]models.MetricValue `json:"oldMarketState,omitempty"`
	NewMarketState map[string]models.MetricValue `json:"newMarketState,omitempty"`

	Validation struct {
		SegmentsNormalized   []string `json:"segmentsNormalized,omitempty"`
		SegmentsDropped      int      `json:"segmentsDropped,omitempty"`
		SegmentNormDomains   []string `json:"segmentNormDomains,omitempty"`
		SegmentNormVerticals []string `json:"segmentNormVerticals,omitempty"`
		SegmentNormPairs     []string `json:"segmentNormPairs,omitempty"`
		MarketMetricsKept    int      `json:"marketMetricsKept,omitempty"`
		MarketMetricsSet     int      `json:"marketMetricsSet,omitempty"`
	} `json:"validation,omitempty"`

	LLM struct {
		Model         string  `json:"model"`
		PromptTokens  int     `json:"promptTokens,omitempty"`
		OutputTokens  int     `json:"outputTokens,omitempty"`
		TotalTokens   int     `json:"totalTokens,omitempty"`
		DurationMs    int     `json:"durationMs,omitempty"`
		Raw           string  `json:"raw,omitempty"`
		SchemaVersion string  `json:"schemaVersion,omitempty"`
		Confidence    float64 `json:"confidence,omitempty"`
		Rationale     string  `json:"rationale,omitempty"`
	} `json:"llm,omitempty"`

	Applied   bool      `json:"applied,omitempty"`
	Reason    string    `json:"reason"`
	ScannedAt time.Time `json:"scannedAt"`
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *maxOutputTokens < 2048 {
		*maxOutputTokens = 2048
	}
	if *saveCursorEveryPages <= 0 {
		*saveCursorEveryPages = 1
	}
	if *scanProgressEveryPages < 0 {
		*scanProgressEveryPages = 0
	}
	featSet, err := parseFeatures(*featuresFlag)
	if err != nil {
		log.Fatalf("FATAL: invalid --features: %v", err)
	}
	if orgClassifiers != nil && *orgClassifiers {
		featSet[string(featureOrgClassifiers)] = struct{}{}
	}
	typeSet, err := parseTypes(*typesFlag, featSet)
	if err != nil {
		log.Fatalf("FATAL: invalid --types: %v", err)
	}

	log.Printf("=== Horizon: unified LLM backfill ===")
	log.Printf("project=%s tenant=%s apply=%v features=%s types=%s model=%s engine=%s grounding=%v llmConcurrency=%d limit=%d batchSize=%d pageSize=%d startAfter=%s cursorFile=%s",
		strings.TrimSpace(*projectID), strings.TrimSpace(*tenantID), *apply,
		strings.Join(sortedKeys(featSet), ","), strings.Join(sortedKeys(typeSet), ","),
		strings.TrimSpace(*model), strings.TrimSpace(*engine), *grounding,
		max(1, *llmConcurrency), *limit, *batchSize, *pageSize, strings.TrimSpace(*startAfter), strings.TrimSpace(*cursorFile),
	)

	ctx := context.Background()

	// Load cursor from file if requested and explicit --start-after is empty.
	if strings.TrimSpace(*startAfter) == "" && strings.TrimSpace(*cursorFile) != "" {
		if st, err := readCursorFile(*cursorFile); err == nil && strings.TrimSpace(st.StartAfter) != "" {
			*startAfter = strings.TrimSpace(st.StartAfter)
			log.Printf("loaded cursor from %s: start-after=%s", *cursorFile, *startAfter)
		}
	}

	// Output sink
	out := os.Stdout
	if strings.TrimSpace(*outPath) != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("FATAL: failed to open output file: %v", err)
		}
		defer f.Close()
		out = f
	}
	var writeMu sync.Mutex
	writeRow := func(row *Row) {
		writeMu.Lock()
		defer writeMu.Unlock()
		b, _ := json.Marshal(row)
		_, _ = out.Write(append(b, '\n'))
	}

	// Firestore
	db, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer db.Close()

	if segmentDictRefresh != nil && *segmentDictRefresh {
		taxonomy.InitMarketSegmentDictionaryCacheFromFirestore(db, strings.TrimSpace(*segmentDictVersion), 20*time.Minute)
		if err := taxonomy.RefreshMarketSegmentDictionary(ctx); err != nil {
			log.Printf("WARNING: failed to load market segment dictionary: %v (will use built-in rules)", err)
		}
	}

	// LLM clients
	mm := ai.NewAIModelManager()
	exec := newTaskExecutor(strings.TrimSpace(*projectID), mm)
	gen := newGenAIClient(strings.TrimSpace(*projectID))

	// Scan candidates (implementation in scan.go)
	candidates, scanSt, err := scanCandidates(ctx, db, strings.TrimSpace(*tenantID), featSet, typeSet)
	if err != nil {
		log.Fatalf("FATAL: scan failed: %v", err)
	}
	log.Printf("scan done: scanned=%d collected=%d resume=start-after=%s", scanSt.Scanned, scanSt.Collected, scanSt.StartAfter)
	if strings.TrimSpace(*cursorFile) != "" && strings.TrimSpace(scanSt.StartAfter) != "" {
		_ = writeCursorFile(*cursorFile, &scanSt)
	}
	if len(candidates) == 0 {
		log.Printf("Nothing to backfill. Exiting.")
		return
	}

	// Parallel batch processing (worker-pool) to speed up backfill runs.
	type job struct {
		idx   int
		items []*models.HorizonFragment
	}
	jobs := make(chan job, 64)
	workerN := max(1, *llmConcurrency)
	var wg sync.WaitGroup
	wg.Add(workerN)
	for w := 0; w < workerN; w++ {
		go func(workerID int) {
			defer wg.Done()
			for j := range jobs {
				if err := backfillBatch(ctx, db, gen, exec, mm, featSet, j.items, writeRow); err != nil {
					log.Printf("WARNING: worker=%d batch=%d failed: %v", workerID, j.idx, err)
					now := time.Now().UTC()
					for _, f := range j.items {
						if f == nil {
							continue
						}
						writeRow(&Row{
							FragmentID:    strings.TrimSpace(f.ID),
							CanonicalPath: strings.TrimSpace(f.CanonicalPath),
							Type:          f.Type,
							Features:      sortedKeys(featSet),
							Applied:       false,
							Reason:        "batch_error: " + err.Error(),
							ScannedAt:     now,
						})
					}
				} else {
					log.Printf("batch ok: worker=%d batch=%d n=%d", workerID, j.idx, len(j.items))
				}
			}
		}(w)
	}

	// Enqueue batches
	bsz := max(1, *batchSize)
	batchIdx := 0
	for i := 0; i < len(candidates); i += bsz {
		j := i + bsz
		if j > len(candidates) {
			j = len(candidates)
		}
		jobs <- job{idx: batchIdx, items: candidates[i:j]}
		batchIdx++
	}
	close(jobs)
	wg.Wait()
}

func parseFeatures(s string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, raw := range strings.Split(s, ",") {
		f := strings.TrimSpace(strings.ToLower(raw))
		if f == "" {
			continue
		}
		switch feature(f) {
		case featureOrgClassifiers, featureMarketProfile, featureMarketMetrics:
			out[f] = struct{}{}
		default:
			return nil, fmt.Errorf("unknown feature: %q", f)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no features selected")
	}
	return out, nil
}

func parseTypes(s string, featSet map[string]struct{}) (map[string]struct{}, error) {
	need := map[string]struct{}{}
	if _, ok := featSet[string(featureOrgClassifiers)]; ok {
		need[string(models.FragmentTypeOrganization)] = struct{}{}
	}
	if _, ok := featSet[string(featureMarketProfile)]; ok {
		need[string(models.FragmentTypeMarket)] = struct{}{}
	}
	if _, ok := featSet[string(featureMarketMetrics)]; ok {
		need[string(models.FragmentTypeMarket)] = struct{}{}
	}
	if strings.TrimSpace(s) == "" {
		return need, nil
	}
	out := map[string]struct{}{}
	for _, raw := range strings.Split(s, ",") {
		t := strings.TrimSpace(strings.ToUpper(raw))
		if t == "" {
			continue
		}
		out[t] = struct{}{}
	}
	for k := range need {
		if _, ok := out[k]; !ok {
			return nil, fmt.Errorf("types do not include required type %s for selected features; either add it to --types or omit --types", k)
		}
	}
	return out, nil
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type cursorState struct {
	StartAfter string    `json:"startAfter"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Scanned    int       `json:"scanned"`
	Collected  int       `json:"collected"`
}
