package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Tool: LLM backfill for ORGANIZATION orgSize + marketSegments (taxonomy-aligned).
// - Scans organizations where orgSize is empty/na and/or marketSegments empty.
// - Sends batches to Gemini/Vertex using strict structured output.
// - Validates/normalizes to taxonomy and optionally applies changes.

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")

	model           = flag.String("model", "gemini-3-flash-preview", "Model name or alias (e.g., gemini-3-flash-preview)")
	engine          = flag.String("engine", "task_executor", "LLM engine: task_executor|direct (direct uses genai combined client)")
	grounding       = flag.Bool("grounding", true, "Enable search grounding (googleSearch) when supported by the model/service")
	minCapScore     = flag.Int("min-capability-score", 0, "Override MinCapabilityScore (0 = derive from model registry)")
	maxOutputTokens = flag.Int("max-output-tokens", 8192, "Max output tokens budget (>=8192 recommended for structured JSON)")
	llmConcurrency  = flag.Int("llm-concurrency", 1, "How many LLM batch calls to run in parallel (>=1)")

	limit     = flag.Int("limit", 50, "How many organizations to backfill (0 = unlimited)")
	batchSize = flag.Int("batch-size", 10, "Batch size per single LLM request")

	maxScan                  = flag.Int("max-scan", 50000, "Max fragments to scan before stopping (safety cap)")
	pageSize                 = flag.Int("page-size", 500, "Firestore page size")
	startAfter               = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID (__name__)")
	cursorFile               = flag.String("cursor-file", "", "Optional path to persist resume cursor. If set and --start-after is empty, it will be loaded from this file; cursor is saved periodically during scan.")
	saveCursorEveryPages     = flag.Int("save-cursor-every-pages", 1, "Persist cursor after every N scanned pages when --cursor-file is set (minimum 1).")
	scanProgressEveryPages   = flag.Int("scan-progress-every-pages", 10, "Log scan progress every N pages during scan (0 disables).")
	firestoreRefreshInterval = flag.Duration("firestore-refresh-interval", 45*time.Minute, "Recreate Firestore client every this interval (0 disables). Useful for very long runs where auth may expire.")

	// Selection behavior
	requireMissing = flag.String("require-missing", "either", "Selection: either|both|orgsize|segments")
	includeNA      = flag.Bool("include-na", true, "Treat orgSize='na' as missing for selection (default true)")

	// Apply behavior
	apply             = flag.Bool("apply", false, "Apply fragment updates (default: dry-run)")
	overwriteOrgSize  = flag.Bool("overwrite-orgsize", false, "Overwrite existing orgSize even if already known (default: fill-only)")
	overwriteSegments = flag.Bool("overwrite-segments", false, "Overwrite existing marketSegments (default: union/fill-only)")

	outPath = flag.String("out", "", "Output JSONL file (default: stdout)")

	strictSegments = flag.Bool("strict-segments", true, "Require each market segment to already be canonical (/segment/{domain}/{vertical}/{geo}); if false, allow normalization.")
)

type cursorState struct {
	StartAfter string    `json:"startAfter"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Scanned    int       `json:"scanned"`
	Collected  int       `json:"collected"`
}

type firestoreSession struct {
	projectID string
	refresh   time.Duration
	mu        sync.Mutex
	client    *firestore.Client
	createdAt time.Time
}

func newFirestoreSession(ctx context.Context, projectID string, refresh time.Duration) (*firestoreSession, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("missing projectID")
	}
	s := &firestoreSession{projectID: strings.TrimSpace(projectID), refresh: refresh}
	_, err := s.DB(ctx)
	return s, err
}

func (s *firestoreSession) DB(ctx context.Context) (*firestore.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil && s.refresh > 0 && time.Since(s.createdAt) > s.refresh {
		_ = s.client.Close()
		s.client = nil
	}
	if s.client != nil {
		return s.client, nil
	}
	c, err := firestore.NewClient(ctx, s.projectID)
	if err != nil {
		return nil, err
	}
	s.client = c
	s.createdAt = time.Now()
	return s.client, nil
}

func (s *firestoreSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
}

type FragmentLite struct {
	ID             string   `json:"id"`
	CanonicalPath  string   `json:"canonicalPath,omitempty"`
	LabelEN        string   `json:"labelEn,omitempty"`
	LegalName      string   `json:"legalName,omitempty"`
	ShortName      string   `json:"shortName,omitempty"`
	Description    string   `json:"description,omitempty"`
	EntityPath     string   `json:"entityPath,omitempty"`
	IndustryPaths  []string `json:"industryPaths,omitempty"`
	OrgSize        string   `json:"orgSize,omitempty"`
	MarketSegments []string `json:"marketSegments,omitempty"`
}

type ItemInput struct {
	Org FragmentLite `json:"org"`
}

// LLM output schema
type LLMResponse struct {
	SchemaVersion string            `json:"schemaVersion"`
	Items         []LLMResponseItem `json:"items"`
}

type LLMResponseItem struct {
	FragmentID     string   `json:"fragmentId"`
	OrgSize        string   `json:"orgSize"`        // sme|le|enterprise|mega|na
	MarketSegments []string `json:"marketSegments"` // canonical /segment/... paths
	Confidence     float64  `json:"confidence"`     // 0..1
	Rationale      string   `json:"rationale"`      // short explanation
}

// JSONL output row
type Row struct {
	FragmentID string `json:"fragmentId"`

	CanonicalPath string `json:"canonicalPath,omitempty"`
	LabelEN       string `json:"labelEn,omitempty"`

	OldOrgSize  string   `json:"oldOrgSize,omitempty"`
	OldSegments []string `json:"oldMarketSegments,omitempty"`

	NewOrgSize  string   `json:"newOrgSize,omitempty"`
	NewSegments []string `json:"newMarketSegments,omitempty"`

	Applied bool   `json:"applied,omitempty"`
	Reason  string `json:"reason"`

	Validation struct {
		OrgSizeNormalized  string   `json:"orgSizeNormalized,omitempty"`
		SegmentsNormalized []string `json:"segmentsNormalized,omitempty"`
		SegmentsChanged    bool     `json:"segmentsChanged,omitempty"`
		SegmentsDropped    int      `json:"segmentsDropped,omitempty"`
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
	} `json:"llm"`

	ScannedAt time.Time `json:"scannedAt"`
}

type batchStats struct {
	LLMCalls  int
	LLMTime   time.Duration
	UsedModel string
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *batchSize <= 0 {
		*batchSize = 10
	}
	if *limit < 0 {
		*limit = 0
	}
	if *maxScan <= 0 {
		*maxScan = 50000
	}
	if *pageSize <= 0 {
		*pageSize = 500
	}
	if *pageSize > 2000 {
		*pageSize = 2000
	}
	if *maxOutputTokens < 8192 {
		*maxOutputTokens = 8192
	}
	if *llmConcurrency <= 0 {
		*llmConcurrency = 1
	}
	if *saveCursorEveryPages <= 0 {
		*saveCursorEveryPages = 1
	}
	if *scanProgressEveryPages < 0 {
		*scanProgressEveryPages = 0
	}

	// Load cursor from file if requested and explicit --start-after is empty.
	if strings.TrimSpace(*startAfter) == "" && strings.TrimSpace(*cursorFile) != "" {
		if st, err := readCursorFile(*cursorFile); err == nil && strings.TrimSpace(st.StartAfter) != "" {
			*startAfter = strings.TrimSpace(st.StartAfter)
			log.Printf("loaded cursor from %s: start-after=%s", *cursorFile, *startAfter)
		}
	}

	ctx := context.Background()
	fsSession, err := newFirestoreSession(ctx, strings.TrimSpace(*projectID), *firestoreRefreshInterval)
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsSession.Close()
	if _, err := fsSession.DB(ctx); err != nil {
		log.Fatalf("FATAL: failed to get Firestore client: %v", err)
	}

	var out *os.File
	if strings.TrimSpace(*outPath) == "" {
		out = os.Stdout
	} else {
		f, err := os.Create(*outPath)
		if err != nil {
			log.Fatalf("FATAL: failed to open output file: %v", err)
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)
	var encMu sync.Mutex
	writeRow := func(row *Row) {
		if row == nil {
			return
		}
		encMu.Lock()
		_ = enc.Encode(row)
		encMu.Unlock()
	}

	client := newGenAIClient()
	modelManager := ai.NewAIModelManager()
	exec := newTaskExecutor(modelManager)

	log.Printf("=== Horizon: LLM backfill orgSize + marketSegments (taxonomy) ===")
	log.Printf("project=%s tenant=%s model=%s require_missing=%s limit=%d batchSize=%d maxScan=%d apply=%v",
		*projectID, *tenantID, strings.TrimSpace(*model), strings.TrimSpace(*requireMissing), *limit, *batchSize, *maxScan, *apply)

	orgs, resume, scanned, err := collectOrgs(ctx, fsSession)
	if err != nil {
		log.Fatalf("FATAL: failed to collect orgs: %v", err)
	}
	if resume != "" {
		log.Printf("resume cursor: start-after=%s", resume)
	}
	if strings.TrimSpace(*cursorFile) != "" {
		_ = writeCursorFileAtomic(*cursorFile, cursorState{
			StartAfter: strings.TrimSpace(resume),
			UpdatedAt:  time.Now().UTC(),
			Scanned:    scanned,
			Collected:  len(orgs),
		})
	}
	log.Printf("collected=%d scanned=%d", len(orgs), scanned)
	if len(orgs) == 0 {
		log.Printf("No eligible organizations found in scanned window.")
		return
	}

	// Partition into batches
	type batchJob struct {
		idx   int
		items []*hmodels.HorizonFragment
	}
	jobs := make(chan batchJob)

	workerN := min(*llmConcurrency, max(1, (len(orgs)+*batchSize-1)/max(1, *batchSize)))
	log.Printf("parallel: llm_concurrency=%d workers=%d total_orgs=%d batch_size=%d", *llmConcurrency, workerN, len(orgs), *batchSize)

	var llmTotalMs int64
	var llmCalls int64

	var wg sync.WaitGroup
	wg.Add(workerN)
	for w := 0; w < workerN; w++ {
		go func(workerID int) {
			defer wg.Done()
			for job := range jobs {
				db, derr := fsSession.DB(ctx)
				var frRepo *hrepo.HorizonFragmentsRepository
				if derr != nil {
					log.Printf("WARNING: worker=%d batch=%d failed to get Firestore client: %v", workerID, job.idx, derr)
				} else if db != nil {
					frRepo = hrepo.NewHorizonFragmentsRepository(db)
				}
				stats := &batchStats{}
				err := backfillBatch(ctx, client, exec, modelManager, fsSession, frRepo, writeRow, job.items, stats)
				if err != nil {
					log.Printf("WARNING: worker=%d batch=%d backfill failed: %v", workerID, job.idx, err)
					now := time.Now().UTC()
					for _, f := range job.items {
						row := makeBaseRow(f)
						row.Reason = "llm_error: " + err.Error()
						row.ScannedAt = now
						writeRow(row)
					}
					continue
				}
				if stats.LLMCalls > 0 {
					atomic.AddInt64(&llmCalls, int64(stats.LLMCalls))
					atomic.AddInt64(&llmTotalMs, stats.LLMTime.Milliseconds())
					log.Printf("LLM batch done: worker=%d batch=%d model=%s llm_ms=%d", workerID, job.idx, stats.UsedModel, stats.LLMTime.Milliseconds())
				}
			}
		}(w)
	}

	// Enqueue jobs
	batchIdx := 0
	for i := 0; i < len(orgs); i += *batchSize {
		j := i + *batchSize
		if j > len(orgs) {
			j = len(orgs)
		}
		jobs <- batchJob{idx: batchIdx, items: orgs[i:j]}
		batchIdx++
	}
	close(jobs)
	wg.Wait()

	llmMs := atomic.LoadInt64(&llmTotalMs)
	calls := atomic.LoadInt64(&llmCalls)
	log.Printf("DONE: llm_calls=%d llm_ms=%d avg_llm_ms_per_call=%.0f",
		calls, llmMs, float64(llmMs)/float64(max(1, int(calls))))
}

func collectOrgs(ctx context.Context, fs *firestoreSession) (out []*hmodels.HorizonFragment, resume string, scanned int, err error) {
	db, err := fs.DB(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	tenant := strings.TrimSpace(*tenantID)
	if tenant == "" {
		tenant = "global"
	}

	base := db.Collection("horizon_fragments").
		Where("tenantId", "==", tenant).
		Where("type", "==", string(hmodels.FragmentTypeOrganization)).
		OrderBy(firestore.DocumentID, firestore.Asc)

	startDocID := strings.TrimSpace(*startAfter)
	query := base
	if startDocID != "" {
		snap, err := db.Collection("horizon_fragments").Doc(startDocID).Get(ctx)
		if err == nil && snap.Exists() {
			query = query.StartAfter(snap)
		} else {
			log.Printf("WARNING: start-after doc %q not found; starting from beginning", startDocID)
		}
	}

	missingMode := strings.ToLower(strings.TrimSpace(*requireMissing))
	if missingMode == "" {
		missingMode = "either"
	}

	var lastDoc *firestore.DocumentSnapshot
	fallbackToScanAll := false
	pages := 0

	runPage := func(q firestore.Query) (*firestore.DocumentSnapshot, int, error) {
		iter := q.Limit(*pageSize).Documents(ctx)
		defer iter.Stop()
		var nextLast *firestore.DocumentSnapshot
		pageCount := 0
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				return nextLast, pageCount, nil
			}
			if err != nil {
				return nextLast, pageCount, err
			}
			nextLast = doc
			pageCount++
			scanned++
			if *maxScan > 0 && scanned >= *maxScan {
				return nextLast, pageCount, fmt.Errorf("max-scan reached (%d)", *maxScan)
			}
			var f hmodels.HorizonFragment
			if err := doc.DataTo(&f); err != nil {
				continue
			}
			// Safety: in fallback scan, enforce org type.
			if f.Type != hmodels.FragmentTypeOrganization {
				continue
			}
			if !isEligible(&f, missingMode) {
				continue
			}
			out = append(out, &f)
			if *limit > 0 && len(out) >= *limit {
				return nextLast, pageCount, nil
			}
		}
	}

	for {
		q := query
		if lastDoc != nil {
			q = query.StartAfter(lastDoc)
		}
		nextLast, pageCount, err := runPage(q)
		pages++
		if strings.TrimSpace(*cursorFile) != "" && *saveCursorEveryPages > 0 && pages%*saveCursorEveryPages == 0 {
			if nextLast != nil {
				_ = writeCursorFileAtomic(*cursorFile, cursorState{
					StartAfter: nextLast.Ref.ID,
					UpdatedAt:  time.Now().UTC(),
					Scanned:    scanned,
					Collected:  len(out),
				})
			}
		}
		if *scanProgressEveryPages > 0 && pages%*scanProgressEveryPages == 0 {
			log.Printf("scan progress: pages=%d scanned=%d collected=%d", pages, scanned, len(out))
		}
		if err != nil {
			msg := strings.ToLower(err.Error())
			if !fallbackToScanAll && (strings.Contains(msg, "failed to execute query") || strings.Contains(msg, "requires an index") || strings.Contains(msg, "indexes")) {
				log.Printf("WARNING: indexed query failed (%v). Falling back to scan-by-tenant and filter in code (slower).", err)
				fallbackToScanAll = true
				// Reset scan state and rerun with fallback query.
				out = nil
				scanned = 0
				lastDoc = nil
				pages = 0
				base2 := db.Collection("horizon_fragments").Where("tenantId", "==", tenant).OrderBy(firestore.DocumentID, firestore.Asc)
				query = base2
				continue
			}
			// End on safety cap or other errors.
			log.Printf("ERROR: %v", err)
			break
		}
		if pageCount == 0 || nextLast == nil {
			break
		}
		lastDoc = nextLast
		// stop early if we reached limit
		if *limit > 0 && len(out) >= *limit {
			break
		}
	}
	if lastDoc != nil {
		resume = lastDoc.Ref.ID
	}
	return out, resume, scanned, nil
}

func isEligible(f *hmodels.HorizonFragment, missingMode string) bool {
	if f == nil {
		return false
	}
	orgSizeRaw := strings.ToLower(strings.TrimSpace(f.OrgSize))
	missingOrgSize := orgSizeRaw == ""
	if *includeNA && orgSizeRaw == "na" {
		missingOrgSize = true
	}
	missingSegments := len(f.MarketSegments) == 0

	switch missingMode {
	case "both":
		return missingOrgSize && missingSegments
	case "orgsize":
		return missingOrgSize
	case "segments":
		return missingSegments
	case "either", "":
		return missingOrgSize || missingSegments
	default:
		// safe default
		return missingOrgSize || missingSegments
	}
}

func backfillBatch(
	ctx context.Context,
	client *gen_combined.Client,
	exec *ai.AITaskExecutor,
	mm *ai.AIModelManager,
	fs *firestoreSession,
	frRepo *hrepo.HorizonFragmentsRepository,
	writeRow func(*Row),
	batch []*hmodels.HorizonFragment,
	stats *batchStats,
) error {
	if len(batch) == 0 {
		return nil
	}
	schema := llmSchema()
	llmBatch := make([]ItemInput, 0, len(batch))
	for _, f := range batch {
		llmBatch = append(llmBatch, makeLLMInput(f))
	}

	modelName := strings.TrimSpace(*model)
	useEngine := strings.ToLower(strings.TrimSpace(*engine))
	sysDirect := "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call. No markdown. No code fences."

	llmCtx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()

	type usage struct {
		prompt, out, total int
		model, raw         string
	}
	u := usage{model: modelName}

	if useEngine == "task_executor" {
		if exec == nil || mm == nil {
			return fmt.Errorf("task_executor not configured")
		}
		minScore := *minCapScore
		if minScore <= 0 {
			if caps, err := mm.GetModelCapabilities(modelName); err == nil {
				minScore = caps.CapabilityScore
			} else {
				minScore = 8
			}
		}

		ver := ai.NewGenericVerifier[LLMResponse](func(out LLMResponse) error {
			return verifyLLMResponse(out, batch)
		})

		task := &ai.AITask{
			TaskType:     ai.TaskTypeKnowledgeSynthesis,
			InputContent: buildTaskExecutorPrompt(llmBatch),
			AIRequirements: ai.AIRequirements{
				PreferredModel:            modelName,
				MinContextWindow:          1000,
				MaxOutputSize:             *maxOutputTokens,
				MinCapabilityScore:        minScore,
				TextGenerationCapability:  true,
				SearchGroundingCapability: *grounding,
			},
			Verifier:        ver,
			EnableTelemetry: true,
		}

		llmStarted := time.Now()
		parsedAny, err := exec.ExecuteStructuredTask(llmCtx, task)
		llmDur := time.Since(llmStarted)
		if stats != nil {
			stats.LLMCalls++
			stats.LLMTime += llmDur
			stats.UsedModel = modelName
		}
		if err != nil {
			return err
		}
		parsed := parsedAny.(LLMResponse)
		b, _ := json.Marshal(parsed)
		u.raw = string(b)
		u.model = modelName
		return emitRowsAndOptionallyApply(ctx, fs, frRepo, writeRow, batch, parsed, u.model, u.prompt, u.out, u.total, int(llmDur.Milliseconds()), u.raw)
	}

	// direct engine: combined client with strict JSON tool-call schema
	if client == nil {
		return fmt.Errorf("direct engine not configured")
	}
	req := gen_combined.GenAIRequest{
		Model:              modelName,
		Prompt:             buildPrompt(llmBatch),
		SystemInstruction:  sysDirect,
		Temperature:        0,
		MaxOutputTokens:    *maxOutputTokens,
		WebSearchEnabled:   *grounding,
		StrictJSONEnabled:  true,
		ToolCallingEnabled: true,
		JSONSchema:         schema,
		Tools: []gen_gemini.Tool{{
			Name:        "return_structured",
			Description: "Return taxonomy-aligned orgSize and marketSegments strictly matching the schema.",
			Parameters:  schema,
		}},
		ToolChoice:        "required",
		ParallelToolCalls: false,
	}
	llmStarted := time.Now()
	resp, err := client.GenAIGenerate(llmCtx, req)
	llmDur := time.Since(llmStarted)
	if stats != nil {
		stats.LLMCalls++
		stats.LLMTime += llmDur
	}
	if err != nil || resp == nil {
		if err == nil {
			err = fmt.Errorf("nil response")
		}
		return err
	}
	u.model = strings.TrimSpace(resp.Model)
	u.prompt, u.out, u.total = resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens
	u.raw = resp.Content

	ver2 := ai.NewGenericVerifier[LLMResponse](func(out LLMResponse) error {
		return verifyLLMResponse(out, batch)
	})
	parsedAny, err := ver2.VerifyAndParse(resp.Content)
	if err != nil {
		return fmt.Errorf("LLM JSON validation failed: %w", err)
	}
	parsed := parsedAny.(LLMResponse)
	if stats != nil {
		stats.UsedModel = u.model
	}
	return emitRowsAndOptionallyApply(ctx, fs, frRepo, writeRow, batch, parsed, u.model, u.prompt, u.out, u.total, int(llmDur.Milliseconds()), u.raw)
}

func verifyLLMResponse(out LLMResponse, batch []*hmodels.HorizonFragment) error {
	if strings.TrimSpace(out.SchemaVersion) == "" {
		return fmt.Errorf("schemaVersion is required")
	}
	if len(out.Items) != len(batch) {
		return fmt.Errorf("items length mismatch: got %d want %d", len(out.Items), len(batch))
	}
	wantIDs := map[string]struct{}{}
	for _, f := range batch {
		wantIDs[strings.TrimSpace(f.ID)] = struct{}{}
	}
	seen := map[string]struct{}{}
	for i, it := range out.Items {
		id := strings.TrimSpace(it.FragmentID)
		if id == "" {
			return fmt.Errorf("items[%d].fragmentId is required", i)
		}
		if _, ok := wantIDs[id]; !ok {
			return fmt.Errorf("items[%d].fragmentId is unknown: %s", i, id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate fragmentId: %s", id)
		}
		seen[id] = struct{}{}

		osz := strings.TrimSpace(it.OrgSize)
		if osz == "" {
			return fmt.Errorf("items[%d].orgSize is required", i)
		}
		nosz := taxonomy.NormalizeOrgSize(osz)
		if nosz == "" {
			return fmt.Errorf("items[%d].orgSize invalid: %q", i, osz)
		}
		if it.Confidence < 0 || it.Confidence > 1 {
			return fmt.Errorf("items[%d].confidence out of range", i)
		}
		// marketSegments may be empty if truly unknown, but we still expect array present.
		if it.MarketSegments == nil {
			return fmt.Errorf("items[%d].marketSegments must be present (can be empty array)", i)
		}
		if *strictSegments {
			for j, s := range it.MarketSegments {
				s2 := strings.TrimSpace(s)
				if s2 == "" {
					return fmt.Errorf("items[%d].marketSegments[%d] empty", i, j)
				}
				if !strings.HasPrefix(s2, "/segment/") {
					return fmt.Errorf("items[%d].marketSegments[%d] must start with /segment/: %q", i, j, s2)
				}
				n, ok := taxonomy.NormalizeSegmentPath(s2)
				if !ok || n == "" {
					return fmt.Errorf("items[%d].marketSegments[%d] invalid segment path: %q", i, j, s2)
				}
				// strict: require already canonical (no normalization change)
				if n != s2 {
					return fmt.Errorf("items[%d].marketSegments[%d] not canonical: got %q normalized %q", i, j, s2, n)
				}
			}
		}
	}
	return nil
}

func emitRowsAndOptionallyApply(
	ctx context.Context,
	fs *firestoreSession,
	frRepo *hrepo.HorizonFragmentsRepository,
	writeRow func(*Row),
	batch []*hmodels.HorizonFragment,
	resp LLMResponse,
	model string,
	promptTokens int,
	outputTokens int,
	totalTokens int,
	llmDurationMs int,
	raw string,
) error {
	now := time.Now().UTC()
	byID := map[string]LLMResponseItem{}
	for _, it := range resp.Items {
		byID[strings.TrimSpace(it.FragmentID)] = it
	}

	for _, f := range batch {
		row := makeBaseRow(f)
		row.ScannedAt = now
		row.LLM.Model = strings.TrimSpace(model)
		row.LLM.PromptTokens = promptTokens
		row.LLM.OutputTokens = outputTokens
		row.LLM.TotalTokens = totalTokens
		row.LLM.DurationMs = llmDurationMs
		row.LLM.Raw = safePreview(raw, 20000)
		row.LLM.SchemaVersion = strings.TrimSpace(resp.SchemaVersion)

		it, ok := byID[strings.TrimSpace(f.ID)]
		if !ok {
			row.Reason = "missing item for fragmentId"
			writeRow(row)
			continue
		}
		row.LLM.Confidence = it.Confidence
		row.LLM.Rationale = strings.TrimSpace(it.Rationale)

		// Normalize/validate
		nOrgSize := taxonomy.NormalizeOrgSize(it.OrgSize)
		nSegments := taxonomy.NormalizeSegmentPaths(it.MarketSegments)
		row.Validation.OrgSizeNormalized = nOrgSize
		row.Validation.SegmentsNormalized = nSegments

		// Determine changes (fill-only by default)
		oldOrgSize := strings.ToLower(strings.TrimSpace(f.OrgSize))
		oldSegments := append([]string{}, f.MarketSegments...)

		newOrgSize := strings.TrimSpace(f.OrgSize)
		if *overwriteOrgSize || oldOrgSize == "" || (*includeNA && oldOrgSize == "na") {
			newOrgSize = nOrgSize
		}

		newSegments := oldSegments
		if *overwriteSegments {
			newSegments = nSegments
		} else if len(nSegments) > 0 {
			// union
			seen := map[string]bool{}
			for _, s := range newSegments {
				s = strings.TrimSpace(s)
				if s != "" {
					seen[s] = true
				}
			}
			for _, s := range nSegments {
				s = strings.TrimSpace(s)
				if s == "" || seen[s] {
					continue
				}
				seen[s] = true
				newSegments = append(newSegments, s)
			}
		}

		newSegmentsNorm := taxonomy.NormalizeSegmentPaths(newSegments)
		row.Validation.SegmentsChanged = !equalStringSlices(oldSegments, newSegmentsNorm)
		if len(it.MarketSegments) > 0 && len(nSegments) == 0 {
			row.Validation.SegmentsDropped = len(it.MarketSegments)
		}

		row.NewOrgSize = newOrgSize
		row.NewSegments = newSegmentsNorm

		// Nothing to do?
		if strings.TrimSpace(newOrgSize) == strings.TrimSpace(f.OrgSize) && equalStringSlices(newSegmentsNorm, oldSegments) {
			row.Reason = "noop"
			writeRow(row)
			continue
		}

		// Apply if requested
		if *apply {
			if frRepo == nil {
				row.Reason = "apply failed: fragments repo not configured"
				writeRow(row)
				continue
			}
			// Reload fresh fragment (avoid stale state, preserve other fields)
			cur, err := frRepo.Get(ctx, f.ID)
			if err != nil && isAuthExpiredErr(err) && fs != nil {
				// refresh Firestore session and retry once
				_, _ = fs.DB(ctx)
				cur, err = frRepo.Get(ctx, f.ID)
			}
			if err != nil || cur == nil {
				row.Reason = "apply failed: cannot load fragment"
				writeRow(row)
				continue
			}

			// Fill/overwrite logic again on the real current fragment
			curOld := strings.ToLower(strings.TrimSpace(cur.OrgSize))
			if *overwriteOrgSize || curOld == "" || (*includeNA && curOld == "na") {
				cur.OrgSize = newOrgSize
			}
			if *overwriteSegments {
				cur.MarketSegments = newSegmentsNorm
			} else {
				union := cur.MarketSegments
				seen := map[string]bool{}
				for _, s := range union {
					s = strings.TrimSpace(s)
					if s != "" {
						seen[s] = true
					}
				}
				for _, s := range newSegmentsNorm {
					s = strings.TrimSpace(s)
					if s == "" || seen[s] {
						continue
					}
					seen[s] = true
					union = append(union, s)
				}
				cur.MarketSegments = taxonomy.NormalizeSegmentPaths(union)
			}

			if err := frRepo.Update(ctx, cur); err != nil {
				row.Reason = "apply failed: update error: " + err.Error()
				writeRow(row)
				continue
			}
			row.Applied = true
		}

		row.Reason = "ok"
		writeRow(row)
	}
	return nil
}

func makeBaseRow(f *hmodels.HorizonFragment) *Row {
	row := &Row{
		Reason: "init",
	}
	if f == nil {
		row.Reason = "nil fragment"
		return row
	}
	row.FragmentID = strings.TrimSpace(f.ID)
	row.CanonicalPath = strings.TrimSpace(f.CanonicalPath)
	row.LabelEN = strings.TrimSpace(bestLabel(f))
	row.OldOrgSize = strings.TrimSpace(f.OrgSize)
	row.OldSegments = append([]string{}, f.MarketSegments...)
	return row
}

func makeLLMInput(f *hmodels.HorizonFragment) ItemInput {
	if f == nil {
		return ItemInput{}
	}
	lbl := bestLabel(f)
	legal := strings.TrimSpace(f.Identifiers["legalName"])
	short := strings.TrimSpace(f.Identifiers["shortName"])
	return ItemInput{
		Org: FragmentLite{
			ID:             strings.TrimSpace(f.ID),
			CanonicalPath:  strings.TrimSpace(f.CanonicalPath),
			LabelEN:        strings.TrimSpace(lbl),
			LegalName:      legal,
			ShortName:      short,
			Description:    strings.TrimSpace(f.Description),
			EntityPath:     strings.TrimSpace(f.EntityPath),
			IndustryPaths:  append([]string{}, f.IndustryPaths...),
			OrgSize:        strings.TrimSpace(f.OrgSize),
			MarketSegments: append([]string{}, f.MarketSegments...),
		},
	}
}

func bestLabel(f *hmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if s := strings.TrimSpace(f.Labels["en"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["legalName"]); s != "" {
		return s
	}
	if s := strings.TrimSpace(f.Identifiers["shortName"]); s != "" {
		return s
	}
	return ""
}

func buildPrompt(items []ItemInput) string {
	b, _ := json.MarshalIndent(items, "", "  ")
	return fmt.Sprintf(`You are a data enrichment agent. Your task: infer organization size (orgSize) and market segments (marketSegments) for each organization.

CRITICAL OUTPUT RULES:
- Return ONLY by calling function return_structured with the exact JSON schema.
- orgSize MUST be exactly one of: "sme" | "le" | "enterprise" | "mega" | "na"
- marketSegments MUST be canonical paths: /segment/{domain}/{vertical}/{geo}
  - geo MUST be ISO3166-1 alpha2 lowercase (2 letters) or "global"
  - domain and vertical MUST be SLUGS:
    - lowercase a-z0-9 and hyphen '-' ONLY
    - DO NOT use underscores '_' anywhere
    - if you need separators, use '-' (kebab-case)
    - examples:
      - BAD:  /segment/agriculture/grain_production/global
      - GOOD: /segment/agriculture/grain-production/global
      - BAD:  /segment/financial-services/capital_markets/de
      - GOOD: /segment/financial-services/capital-markets/de
  - Use domain as broad area (finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate|public|government|public-sector|professional-services|b2b|commercial)
  - vertical as a specific industry slug (e.g., semiconductors, retail-banking, ev-batteries, oil-and-gas)
- If cannot determine: orgSize="na" and marketSegments=[]

INPUT ORGANIZATIONS (JSON array):
%s`, string(b))
}

func buildTaskExecutorPrompt(items []ItemInput) string {
	// GeminiService task_executor mode expects raw JSON output (no tool calling).
	b, _ := json.MarshalIndent(items, "", "  ")
	return fmt.Sprintf(`Return ONLY valid JSON (no markdown, no code fences) matching schema:
{
  "schemaVersion": "v1",
  "items": [
    {
      "fragmentId": "string",
      "orgSize": "sme|le|enterprise|mega|na",
      "marketSegments": ["/segment/{domain}/{vertical}/{geo}", ...],
      "confidence": 0.0,
      "rationale": "string"
    }
  ]
}

Rules:
- items MUST have exactly the same length as input, one item per org, and fragmentId must match.
- marketSegments MUST be canonical (/segment/{domain}/{vertical}/{geo}) and geo must be iso2 or global.
- domain and vertical MUST be slugs: lowercase a-z0-9 and hyphen '-' ONLY. NEVER use underscore '_'.
  Examples:
  - BAD:  /segment/agriculture/grain_production/global
  - GOOD: /segment/agriculture/grain-production/global
- orgSize must be one of sme|le|enterprise|mega|na.

INPUT (JSON array):
%s`, string(b))
}

func llmSchema() map[string]any {
	// Minimal JSON schema compatible with genai combined client tool-calling.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schemaVersion": map[string]any{"type": "string"},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"fragmentId": map[string]any{"type": "string"},
						"orgSize": map[string]any{
							"type": "string",
							"enum": []string{"sme", "le", "enterprise", "mega", "na"},
						},
						"marketSegments": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"confidence": map[string]any{"type": "number"},
						"rationale":  map[string]any{"type": "string"},
					},
					"required": []string{"fragmentId", "orgSize", "marketSegments", "confidence", "rationale"},
				},
			},
		},
		"required": []string{"schemaVersion", "items"},
	}
}

func newGenAIClient() *gen_combined.Client {
	// Prefer env vars if present, otherwise load from GCP Secret Manager using standard secret names.
	ctx := context.Background()
	apiKey := loadGeminiOrVertexAPIKey(ctx, strings.TrimSpace(*projectID), false /* allowTest */)
	if apiKey != "" {
		// Make it available to core services that read from env (safe for this one-shot CLI tool).
		_ = os.Setenv("GEMINI_API_KEY", apiKey)
	}
	project := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if project == "" {
		project = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if project == "" {
		project = strings.TrimSpace(*projectID)
	}
	location := strings.TrimSpace(os.Getenv("VERTEX_LOCATION"))
	if location == "" {
		location = "us-central1"
	}
	fast := strings.TrimSpace(os.Getenv("GENAI_FAST_MODEL"))
	quality := strings.TrimSpace(os.Getenv("GENAI_QUALITY_MODEL"))
	gem := gen_gemini.NewClient(apiKey, project, location, fast, quality)
	oaiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	return gen_combined.NewClient(gem, oaiKey)
}

func newTaskExecutor(mm *ai.AIModelManager) *ai.AITaskExecutor {
	// GeminiService uses GEMINI_API_KEY (Generative Language API).
	ctx := context.Background()
	apiKey := loadGeminiOrVertexAPIKey(ctx, strings.TrimSpace(*projectID), false /* allowTest */)
	if apiKey == "" {
		log.Printf("WARNING: GEMINI_API_KEY not found in env or Secret Manager (project=%s). task_executor calls may fail.", strings.TrimSpace(*projectID))
	}
	if apiKey != "" {
		_ = os.Setenv("GEMINI_API_KEY", apiKey)
	}
	vertex := ai.NewGeminiService(apiKey, mm)
	return ai.NewAITaskExecutor([]ai.AIService{vertex}, mm)
}

func loadGeminiOrVertexAPIKey(ctx context.Context, projectID string, allowTest bool) string {
	// 1) Env overrides
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY")); v != "" {
		return v
	}
	if allowTest {
		if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY_TEST")); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY_TEST")); v != "" {
			return v
		}
	}

	// 2) Secret Manager
	candidates := []string{
		"gemini_api_key",
		"GEMINI_API_KEY",
		"vertex_api_key",
		"VERTEX_API_KEY",
	}
	if allowTest {
		candidates = append(candidates,
			"GEMINI_API_KEY_TEST",
			"VERTEX_API_KEY_TEST",
			"gemini_api_key_test",
			"vertex_api_key_test",
		)
	}
	for _, name := range candidates {
		v, err := gcp.LoadSecret(ctx, projectID, name)
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func safePreview(s string, maxN int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if maxN <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxN {
		return s
	}
	return string(r[:maxN]) + "...(truncated)"
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isAuthExpiredErr(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if ok {
		return st.Code() == codes.Unauthenticated || st.Code() == codes.PermissionDenied
	}
	return false
}

func readCursorFile(path string) (cursorState, error) {
	var st cursorState
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func writeCursorFileAtomic(path string, st cursorState) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.tmp", filepath.Base(path)))
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
