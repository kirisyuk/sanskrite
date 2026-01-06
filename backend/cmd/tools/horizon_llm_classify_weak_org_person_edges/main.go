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
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

// Tool: LLM classification of weak PERSON<->ORGANIZATION edges without requiring Wikidata QIDs.
//
// Flow:
// - Scan weak edges (INFLUENCES/DEPENDS_ON by default).
// - Pick N PERSON<->ORG edges (batch size default 10).
// - Send as a single structured request to Gemini (gemini-3-flash-preview) using tool-call schema.
// - Log JSONL per edge with input context + model output. Optional apply step can rewrite edge type.

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "global", "Tenant ID (default: global)")

	model = flag.String("model", "gemini-3-flash-preview", "Model name or alias (e.g., gemini-3-flash-preview)")
	engine = flag.String("engine", "task_executor", "LLM engine: task_executor|direct (direct uses genai combined client)")
	grounding = flag.Bool("grounding", true, "Enable search grounding (googleSearch) when supported by the model/service")
	minCapScore = flag.Int("min-capability-score", 0, "Override MinCapabilityScore (0 = derive from model registry)")
	maxOutputTokens = flag.Int("max-output-tokens", 8192, "Max output tokens budget for the model (minimum 8192; lower values may truncate structured JSON)")

	edgeTypes = flag.String("edge-types", "INFLUENCES,DEPENDS_ON", "Comma-separated edge types considered 'weak'")
	limit     = flag.Int("limit", 10, "How many PERSON<->ORG edges to classify")
	batchSize = flag.Int("batch-size", 10, "Batch size per single LLM request")
	maxScan   = flag.Int("max-scan", 20000, "Max edges to scan before stopping (safety cap)")
	pageSize  = flag.Int("page-size", 500, "Firestore page size")
	startAfter = flag.String("start-after", "", "Resume cursor: start scanning after this Firestore document ID (__name__)")
	cursorFile = flag.String("cursor-file", "", "Optional path to persist resume cursor. If set and --start-after is empty, it will be loaded from this file; cursor is saved periodically during scan.")
	saveCursorEveryPages = flag.Int("save-cursor-every-pages", 1, "Persist cursor after every N scanned pages when --cursor-file is set (minimum 1).")
	firestoreRefreshInterval = flag.Duration("firestore-refresh-interval", 45*time.Minute, "Recreate Firestore client every this interval (0 disables). Useful for very long runs where auth may expire.")

	apply = flag.Bool("apply", false, "Apply edge type changes (default: dry-run log only)")

	outPath = flag.String("out", "", "Output JSONL file (default: stdout)")

	inputLevel = flag.String("input-level", "minimal", "What to send to LLM: minimal|lite|full. minimal hides edge qualifiers + derived descriptions to force model to rely on its own knowledge")
)

type KBRefLite struct {
	KB         string  `json:"kb"`
	ExternalID string  `json:"externalId"`
	Confidence float64 `json:"confidence,omitempty"`
}

type FragmentLite struct {
	ID            string      `json:"id"`
	Type          string      `json:"type"`
	CanonicalPath string      `json:"canonicalPath,omitempty"`
	LabelEN       string      `json:"labelEn,omitempty"`
	LegalName     string      `json:"legalName,omitempty"`
	ShortName     string      `json:"shortName,omitempty"`
	Description   string      `json:"description,omitempty"`
	KBRefs        []KBRefLite `json:"kbRefs,omitempty"`
}

type EdgeLite struct {
	ID               string                 `json:"id"`
	Type             string                 `json:"type"`
	SourceFragmentID string                 `json:"sourceFragmentId"`
	TargetFragmentID string                 `json:"targetFragmentId"`
	Qualifiers       map[string]interface{} `json:"qualifiers,omitempty"`
}

type PairInput struct {
	Edge   EdgeLite      `json:"edge"`
	Person FragmentLite  `json:"person"`
	Org    FragmentLite  `json:"org"`
}

// LLM output schema
type LLMResponse struct {
	SchemaVersion string            `json:"schemaVersion"`
	Items         []LLMResponseItem `json:"items"`
}

type LLMResponseItem struct {
	EdgeID       string  `json:"edgeId"`
	RelationType string  `json:"relationType"` // e.g., OWNS, MANAGES, ...
	Confidence   float64 `json:"confidence"`   // 0..1
	Rationale    string  `json:"rationale"`    // short explanation
}

// JSONL log row
type Row struct {
	EdgeID string `json:"edgeId"`

	OldType string `json:"oldType"`
	NewType string `json:"newType,omitempty"`

	Confidence   float64 `json:"confidence,omitempty"`
	Rationale    string  `json:"rationale,omitempty"`

	Applied bool   `json:"applied,omitempty"`
	Reason  string `json:"reason"`

	Input PairInput `json:"input"`
	LLMInput any    `json:"llmInput"`

	LLM struct {
		Model        string `json:"model"`
		PromptTokens int    `json:"promptTokens,omitempty"`
		OutputTokens int    `json:"outputTokens,omitempty"`
		TotalTokens  int    `json:"totalTokens,omitempty"`
		Raw          string `json:"raw,omitempty"`
	} `json:"llm"`

	ScannedAt time.Time `json:"scannedAt"`
}

type PairLLMInput struct {
	Edge   EdgeLite     `json:"edge"`
	Person FragmentLite `json:"person"`
	Org    FragmentLite `json:"org"`
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if *limit <= 0 {
		*limit = 10
	}
	if *batchSize <= 0 {
		*batchSize = 10
	}
	if *batchSize > *limit {
		*batchSize = *limit
	}
	if *maxScan <= 0 {
		*maxScan = 20000
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
	if *saveCursorEveryPages <= 0 {
		*saveCursorEveryPages = 1
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

	fragLoader := func(ctx context.Context, id string) (*hmodels.HorizonFragment, error) {
		db, err := fsSession.DB(ctx)
		if err != nil {
			return nil, err
		}
		repo := hrepo.NewHorizonFragmentsRepository(db)
		return repo.Get(ctx, id)
	}
	cache := newFragmentCache(fragLoader)

	types := parseTypes(*edgeTypes)
	if len(types) == 0 {
		log.Fatal("--edge-types resulted in empty list")
	}

	client := newGenAIClient()
	modelManager := ai.NewAIModelManager()
	exec := newTaskExecutor(modelManager)

	log.Printf("=== Horizon: LLM classify weak PERSON<->ORG edges ===")
	log.Printf("project=%s tenant=%s model=%s edgeTypes=%v limit=%d batchSize=%d maxScan=%d apply=%v",
		*projectID, *tenantID, strings.TrimSpace(*model), types, *limit, *batchSize, *maxScan, *apply)

	pairs, resume, scanned, err := collectPairs(ctx, fsSession, cache, types, *limit)
	if err != nil {
		log.Fatalf("FATAL: failed to collect pairs: %v", err)
	}
	if resume != "" {
		log.Printf("resume cursor: start-after=%s", resume)
	}
	if strings.TrimSpace(*cursorFile) != "" {
		_ = writeCursorFileAtomic(*cursorFile, cursorState{
			StartAfter: strings.TrimSpace(resume),
			UpdatedAt: time.Now().UTC(),
			Scanned:   scanned,
		})
	}
	log.Printf("collected=%d scanned=%d", len(pairs), scanned)
	if len(pairs) == 0 {
		log.Printf("No eligible PERSON<->ORG edges found in scanned window.")
		return
	}

	// Process in batches
	for i := 0; i < len(pairs); i += *batchSize {
		j := i + *batchSize
		if j > len(pairs) {
			j = len(pairs)
		}
		batch := pairs[i:j]
		// Build repos on-demand from the current Firestore client (supports periodic refresh).
		db, derr := fsSession.DB(ctx)
		if derr != nil {
			log.Printf("WARNING: failed to get Firestore client for batch: %v", derr)
		}
		var edgesRepo *hrepo.DependencyEdgesRepository
		if db != nil {
			fr := hrepo.NewHorizonFragmentsRepository(db)
			edgesRepo = hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(fr)
		}
		if err := classifyBatch(ctx, client, exec, modelManager, fsSession, edgesRepo, enc, batch); err != nil {
			log.Printf("WARNING: batch classification failed: %v", err)
			// Still emit per-edge error rows
			for _, p := range batch {
				li := makeLLMInput(p, strings.TrimSpace(*inputLevel))
				row := &Row{
					EdgeID:    p.Edge.ID,
					OldType:   p.Edge.Type,
					Reason:    "llm_error: " + err.Error(),
					Input:     p,
					LLMInput:  li,
					ScannedAt: time.Now().UTC(),
				}
				_ = enc.Encode(row)
			}
		}
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
	// VertexAIService currently uses GEMINI_API_KEY (Generative Language API).
	// In server environments this is normally configured.
	ctx := context.Background()
	apiKey := loadGeminiOrVertexAPIKey(ctx, strings.TrimSpace(*projectID), false /* allowTest */)
	if apiKey == "" {
		// Keep it explicit: task_executor mode requires a key in current core implementation.
		log.Printf("WARNING: GEMINI_API_KEY not found in env or Secret Manager (project=%s). task_executor calls may fail.", strings.TrimSpace(*projectID))
	}
	if apiKey != "" {
		_ = os.Setenv("GEMINI_API_KEY", apiKey)
	}
	vertex := ai.NewVertexAIService(apiKey, mm)
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

	// 2) Secret Manager (try canonical secret names used in the repo)
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

func collectPairs(ctx context.Context, fs *firestoreSession, cache *fragmentCache, types []hmodels.DependencyType, want int) ([]PairInput, string, int, error) {
	if fs == nil {
		return nil, "", 0, fmt.Errorf("firestore session is nil")
	}
	db, err := fs.DB(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	anyTypes := make([]interface{}, 0, len(types))
	for _, t := range types {
		anyTypes = append(anyTypes, string(t))
	}
	makeBase := func(db *firestore.Client) firestore.Query {
		return db.Collection("horizon_dependency_edges").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Where("type", "in", anyTypes).
			OrderBy(firestore.DocumentID, firestore.Asc)
	}
	base := makeBase(db)

	var out []PairInput
	scanned := 0
	lastSeen := strings.TrimSpace(*startAfter)
	pages := 0

	for scanned < *maxScan && len(out) < want {
		// Refresh Firestore client periodically (and rebuild base query).
		db, err := fs.DB(ctx)
		if err != nil {
			return out, lastSeen, scanned, err
		}
		base = makeBase(db)

		q := base.Limit(*pageSize)
		if lastSeen != "" {
			q = q.StartAfter(lastSeen)
		}
		iter := q.Documents(ctx)
		pageCount := 0
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				// If credentials expired mid-run, refresh client and retry next page.
				if isAuthExpiredErr(err) {
					_ = fs.ForceRefresh(ctx)
					iter.Stop()
					break
				}
				iter.Stop()
				return out, lastSeen, scanned, err
			}
			pageCount++
			scanned++
			lastSeen = doc.Ref.ID
			if scanned > *maxScan {
				break
			}

			var e hmodels.DependencyEdge
			if err := doc.DataTo(&e); err != nil {
				continue
			}
			if strings.TrimSpace(e.ID) == "" {
				e.ID = doc.Ref.ID
			}
			sid := strings.TrimSpace(e.SourceFragmentID)
			tid := strings.TrimSpace(e.TargetFragmentID)
			if sid == "" || tid == "" {
				continue
			}
			sf, err1 := cache.get(ctx, sid)
			tf, err2 := cache.get(ctx, tid)
			if err1 != nil || err2 != nil || sf == nil || tf == nil {
				continue
			}

			person, org := (*hmodels.HorizonFragment)(nil), (*hmodels.HorizonFragment)(nil)
			if sf.Type == hmodels.FragmentTypePerson && tf.Type == hmodels.FragmentTypeOrganization {
				person, org = sf, tf
			} else if sf.Type == hmodels.FragmentTypeOrganization && tf.Type == hmodels.FragmentTypePerson {
				person, org = tf, sf
			} else {
				continue
			}

			out = append(out, PairInput{
				Edge: EdgeLite{
					ID:               strings.TrimSpace(e.ID),
					Type:             string(e.Type),
					SourceFragmentID: sid,
					TargetFragmentID: tid,
					Qualifiers:       e.Qualifiers,
				},
				Person: toLite(person),
				Org:    toLite(org),
			})
			if len(out) >= want {
				break
			}
		}
		iter.Stop()
		pages++
		if strings.TrimSpace(*cursorFile) != "" && pages%max(1, *saveCursorEveryPages) == 0 {
			_ = writeCursorFileAtomic(*cursorFile, cursorState{
				StartAfter: strings.TrimSpace(lastSeen),
				UpdatedAt: time.Now().UTC(),
				Scanned:   scanned,
			})
		}
		if pageCount < *pageSize {
			break
		}
	}
	return out, lastSeen, scanned, nil
}

func classifyBatch(ctx context.Context, client *gen_combined.Client, exec *ai.AITaskExecutor, mm *ai.AIModelManager, fs *firestoreSession, edgesRepo *hrepo.DependencyEdgesRepository, enc *json.Encoder, batch []PairInput) error {
	if len(batch) == 0 {
		return nil
	}
	schema := llmSchema()
	// Build the actual LLM input batch according to inputLevel (minimize leakage from our own edge explanations).
	llmBatch := make([]PairLLMInput, 0, len(batch))
	level := strings.TrimSpace(*inputLevel)
	for _, p := range batch {
		llmBatch = append(llmBatch, makeLLMInput(p, level))
	}
	prompt := buildPrompt(llmBatch)
	sysDirect := "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call. No markdown. No code fences."

	modelName := strings.TrimSpace(*model)
	useEngine := strings.ToLower(strings.TrimSpace(*engine))

	type usage struct{ prompt, out, total int; model, raw string }
	u := usage{model: modelName}
	raw := ""

	llmCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	if useEngine == "task_executor" {
		if exec == nil || mm == nil {
			return fmt.Errorf("task_executor not configured")
		}
		// Derive MinCapabilityScore from model registry when requested.
		minScore := *minCapScore
		if minScore <= 0 {
			if caps, err := mm.GetModelCapabilities(modelName); err == nil {
				minScore = caps.CapabilityScore
			} else {
				// fallback: conservative
				minScore = 8
			}
		}

		ver := ai.NewGenericVerifier[LLMResponse](func(out LLMResponse) error {
			if strings.TrimSpace(out.SchemaVersion) == "" {
				return fmt.Errorf("schemaVersion is required")
			}
			if len(out.Items) != len(batch) {
				return fmt.Errorf("items length mismatch: got %d want %d", len(out.Items), len(batch))
			}
			wantIDs := map[string]struct{}{}
			for _, p := range batch {
				wantIDs[p.Edge.ID] = struct{}{}
			}
			seen := map[string]struct{}{}
			for i, it := range out.Items {
				id := strings.TrimSpace(it.EdgeID)
				if id == "" {
					return fmt.Errorf("items[%d].edgeId is required", i)
				}
				if _, ok := wantIDs[id]; !ok {
					return fmt.Errorf("items[%d].edgeId is unknown: %s", i, id)
				}
				if _, ok := seen[id]; ok {
					return fmt.Errorf("duplicate edgeId: %s", id)
				}
				seen[id] = struct{}{}
				rt := strings.ToUpper(strings.TrimSpace(it.RelationType))
				if !isAllowedRelation(rt) {
					return fmt.Errorf("items[%d].relationType invalid: %s", i, it.RelationType)
				}
				if it.Confidence < 0 || it.Confidence > 1 {
					return fmt.Errorf("items[%d].confidence out of range", i)
				}
			}
			return nil
		})

		// NOTE: task_executor/VertexAIService does not support tool calling; require raw JSON output.
		fullInput := buildTaskExecutorPrompt(llmBatch)

		task := &ai.AITask{
			TaskType:     ai.TaskTypeContentGeneration,
			InputContent: fullInput,
			AIRequirements: ai.AIRequirements{
				PreferredModel:            modelName,
				MinContextWindow:          1000,
				// NOTE: keep >= 8K as requested; smaller budgets sometimes truncate structured JSON.
				MaxOutputSize:             *maxOutputTokens,
				MinCapabilityScore:        minScore,
				TextGenerationCapability:  true,
				SearchGroundingCapability: *grounding,
			},
			Verifier:        ver,
			EnableTelemetry: true,
		}

		parsedAny, err := exec.ExecuteStructuredTask(llmCtx, task)
		if err != nil {
			return err
		}
		parsed := parsedAny.(LLMResponse)
		b, _ := json.Marshal(parsed)
		raw = string(b)
		u.model = modelName
		// Token usage isn't exposed via task executor result; leave zeros.

		byID := map[string]LLMResponseItem{}
		for _, it := range parsed.Items {
			byID[strings.TrimSpace(it.EdgeID)] = it
		}

		return emitRowsAndOptionallyApply(ctx, fs, edgesRepo, enc, batch, byID, u.model, u.prompt, u.out, u.total, raw, parsed.SchemaVersion, level)
	}

	// direct engine: combined client with strict JSON tool-call schema
	req := gen_combined.GenAIRequest{
		Model:              modelName,
		Prompt:             prompt,
		SystemInstruction:  sysDirect,
		Temperature:        0,
		MaxOutputTokens:    *maxOutputTokens,
		WebSearchEnabled:   *grounding,
		StrictJSONEnabled:  true,
		ToolCallingEnabled: true,
		JSONSchema:         schema,
		Tools: []gen_gemini.Tool{{
			Name:        "return_structured",
			Description: "Return the structured classification result strictly matching the schema.",
			Parameters:  schema,
		}},
		ToolChoice:        "required",
		ParallelToolCalls: false,
	}
	resp, err := client.GenAIGenerate(llmCtx, req)
	if err != nil || resp == nil {
		if err == nil {
			err = fmt.Errorf("nil response")
		}
		return err
	}
	u.model = strings.TrimSpace(resp.Model)
	u.prompt, u.out, u.total = resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens
	raw = resp.Content

	ver2 := ai.NewGenericVerifier[LLMResponse](func(out LLMResponse) error {
		if strings.TrimSpace(out.SchemaVersion) == "" {
			return fmt.Errorf("schemaVersion is required")
		}
		if len(out.Items) != len(batch) {
			return fmt.Errorf("items length mismatch: got %d want %d", len(out.Items), len(batch))
		}
		return nil
	})
	parsedAny, err := ver2.VerifyAndParse(resp.Content)
	if err != nil {
		return fmt.Errorf("LLM JSON validation failed: %w", err)
	}
	parsed := parsedAny.(LLMResponse)

	byID := map[string]LLMResponseItem{}
	for _, it := range parsed.Items {
		byID[strings.TrimSpace(it.EdgeID)] = it
	}
	return emitRowsAndOptionallyApply(ctx, fs, edgesRepo, enc, batch, byID, u.model, u.prompt, u.out, u.total, raw, parsed.SchemaVersion, level)
}

func emitRowsAndOptionallyApply(
	ctx context.Context,
	fs *firestoreSession,
	edgesRepo *hrepo.DependencyEdgesRepository,
	enc *json.Encoder,
	batch []PairInput,
	byID map[string]LLMResponseItem,
	model string,
	promptTokens int,
	outputTokens int,
	totalTokens int,
	raw string,
	schemaVersion string,
	level string,
) error {
	now := time.Now().UTC()
	for _, p := range batch {
		it, ok := byID[p.Edge.ID]
		li := makeLLMInput(p, level)
		row := &Row{
			EdgeID:    p.Edge.ID,
			OldType:   p.Edge.Type,
			Reason:    "ok",
			Input:     p,
			LLMInput:  li,
			ScannedAt: now,
		}
		row.LLM.Model = strings.TrimSpace(model)
		row.LLM.PromptTokens = promptTokens
		row.LLM.OutputTokens = outputTokens
		row.LLM.TotalTokens = totalTokens
		row.LLM.Raw = safePreview(raw, 20000)

		if !ok {
			row.Reason = "missing item for edgeId"
			_ = enc.Encode(row)
			continue
		}
		row.NewType = strings.ToUpper(strings.TrimSpace(it.RelationType))
		row.Confidence = it.Confidence
		row.Rationale = strings.TrimSpace(it.Rationale)

		// Apply if requested and not NONE.
		if *apply && row.NewType != "NONE" {
			if edgesRepo == nil {
				row.Reason = "apply failed: edgesRepo not configured"
				_ = enc.Encode(row)
				continue
			}
			edge, gerr := edgesRepo.Get(ctx, p.Edge.ID)
			if gerr != nil && isAuthExpiredErr(gerr) && fs != nil {
				_ = fs.ForceRefresh(ctx)
				if db, derr := fs.DB(ctx); derr == nil && db != nil {
					fr := hrepo.NewHorizonFragmentsRepository(db)
					edgesRepo = hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(fr)
					edge, gerr = edgesRepo.Get(ctx, p.Edge.ID)
				}
			}
			if gerr != nil || edge == nil {
				row.Reason = "apply failed: cannot load edge"
				_ = enc.Encode(row)
				continue
			}
			newType := hmodels.DependencyType(row.NewType)

			personID := strings.TrimSpace(p.Person.ID)
			orgID := strings.TrimSpace(p.Org.ID)
			sourceID, targetID := personID, orgID
			srcType := hmodels.FragmentType(strings.TrimSpace(p.Person.Type))
			dstType := hmodels.FragmentType(strings.TrimSpace(p.Org.Type))
			valid, shouldReverse, _ := scout.ValidateEdgeDirection(newType, srcType, dstType)
			if !valid {
				if shouldReverse {
					row.Reason = "apply skipped: direction policy suggests reverse"
				} else {
					row.Reason = "apply skipped: direction policy mismatch"
				}
				_ = enc.Encode(row)
				continue
			}

			edge.Type = newType
			edge.SourceFragmentID = sourceID
			edge.TargetFragmentID = targetID
			if edge.Qualifiers == nil {
				edge.Qualifiers = map[string]interface{}{}
			}
			edge.Qualifiers["llm"] = map[string]interface{}{
				"model":        row.LLM.Model,
				"relationType": row.NewType,
				"confidence":   row.Confidence,
				"rationale":    row.Rationale,
				"schemaVersion": strings.TrimSpace(schemaVersion),
				"ts":           now,
			}
			edge.Provenance.Method = "llm.relation_classifier.v1"
			edge.Provenance.SourceIDs = appendUnique(edge.Provenance.SourceIDs, "llm")
			uerr := edgesRepo.Update(ctx, edge)
			if uerr != nil && isAuthExpiredErr(uerr) && fs != nil {
				_ = fs.ForceRefresh(ctx)
				if db, derr := fs.DB(ctx); derr == nil && db != nil {
					fr := hrepo.NewHorizonFragmentsRepository(db)
					edgesRepo = hrepo.NewDependencyEdgesRepository(db).WithFragmentsRepo(fr)
					uerr = edgesRepo.Update(ctx, edge)
				}
			}
			if uerr != nil {
				row.Reason = "apply failed: " + uerr.Error()
			} else {
				row.Applied = true
				row.Reason = "applied"
			}
		}

		_ = enc.Encode(row)
	}
	return nil
}

func buildPrompt(batch []PairLLMInput) string {
	// Provide all inputs as JSON so the model can stay deterministic.
	b, _ := json.MarshalIndent(batch, "", "  ")
	return strings.TrimSpace(fmt.Sprintf(`You are classifying real-world relationships between a PERSON and an ORGANIZATION.

Your task: for EACH pair, classify the relationship into ONE allowed relationType, or NONE.
Treat PERSON as the subject and ORGANIZATION as the object (person -> org).

Allowed relationType values:
- FOUNDED
- INVESTS_IN
- OWNS
- CONTROLS
- MANAGES
- EMPLOYED_BY
- BOARD_MEMBER_OF
- NONE

Rules:
- Rely on YOUR general world knowledge and the provided entity identifiers (names/KB refs).
- Do NOT assume the weak edge type implies any specific relationship.
- If you do not know, output NONE with low confidence.
- rationale must be short (<= 240 chars) and should not restate the input verbatim.

INPUT_JSON:
%s
`, string(b)))
}

func buildTaskExecutorPrompt(batch []PairLLMInput) string {
	// Keep prompt compact to reduce token usage and truncation risk.
	b, _ := json.Marshal(batch)
	return strings.TrimSpace(fmt.Sprintf(`Return ONE valid JSON object and nothing else.
No markdown. No code fences. No leading/trailing text. Use ONLY double quotes for strings.

Output schema (must match exactly):
{
  "schemaVersion": "1.0",
  "items": [
    { "edgeId": "EDGE_ID", "relationType": "FOUNDED|INVESTS_IN|OWNS|CONTROLS|MANAGES|EMPLOYED_BY|BOARD_MEMBER_OF|NONE", "confidence": 0.0, "rationale": "..." }
  ]
}

Rules:
- You MUST verify facts using web search (search grounding). Do NOT rely on memory for specific claims (CEO/founder/board roles).
- If you cannot confidently verify the relationship, output relationType=NONE with confidence <= 0.4.
- If you find conflicting information, output NONE (<= 0.4).
- items.length MUST equal %d
- edgeId MUST equal input.edge.id for each element
- relationType must be one of the allowed values above (uppercase)
- confidence must be a number between 0 and 1
- rationale <= 200 chars, do not restate the input verbatim
- If uncertain, use relationType=NONE with low confidence

INPUT_JSON:
%s`, len(batch), string(b)))
}

func llmSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"schemaVersion": map[string]interface{}{"type": "string"},
			"items": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"edgeId":       map[string]interface{}{"type": "string"},
						"relationType": map[string]interface{}{"type": "string"},
						"confidence":   map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
						"rationale":    map[string]interface{}{"type": "string"},
					},
					"required": []string{"edgeId", "relationType", "confidence", "rationale"},
				},
			},
		},
		"required": []string{"schemaVersion", "items"},
	}
}

func isAllowedRelation(s string) bool {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FOUNDED", "INVESTS_IN", "OWNS", "CONTROLS", "MANAGES", "EMPLOYED_BY", "BOARD_MEMBER_OF", "NONE":
		return true
	default:
		return false
	}
}

func parseTypes(s string) []hmodels.DependencyType {
	parts := strings.Split(s, ",")
	out := make([]hmodels.DependencyType, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, hmodels.DependencyType(p))
	}
	return out
}

func toLite(f *hmodels.HorizonFragment) FragmentLite {
	if f == nil {
		return FragmentLite{}
	}
	kb := make([]KBRefLite, 0, len(f.KBRefs))
	for _, r := range f.KBRefs {
		if strings.TrimSpace(r.KB) == "" && strings.TrimSpace(r.ExternalID) == "" {
			continue
		}
		kb = append(kb, KBRefLite{
			KB:         strings.TrimSpace(r.KB),
			ExternalID: strings.TrimSpace(r.ExternalID),
			Confidence: r.Confidence,
		})
	}
	return FragmentLite{
		ID:            strings.TrimSpace(f.ID),
		Type:          string(f.Type),
		CanonicalPath: strings.TrimSpace(f.CanonicalPath),
		LabelEN:       strings.TrimSpace(f.Labels["en"]),
		LegalName:     strings.TrimSpace(f.Identifiers["legalName"]),
		ShortName:     strings.TrimSpace(f.Identifiers["shortName"]),
		Description:   strings.TrimSpace(f.Description),
		KBRefs:        kb,
	}
}

func makeLLMInput(p PairInput, level string) PairLLMInput {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		level = "minimal"
	}
	li := PairLLMInput{
		Edge: EdgeLite{
			ID:               strings.TrimSpace(p.Edge.ID),
			Type:             strings.TrimSpace(p.Edge.Type),
			SourceFragmentID: strings.TrimSpace(p.Edge.SourceFragmentID),
			TargetFragmentID: strings.TrimSpace(p.Edge.TargetFragmentID),
			// Qualifiers are intentionally excluded by default to avoid leaking our own explanations.
			Qualifiers: nil,
		},
		Person: p.Person,
		Org:    p.Org,
	}
	// Strip derived descriptions by default (they often come from our own discovery process).
	stripDerived := func(s string) string {
		s2 := strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToLower(s2), "discovered via relation") {
			return ""
		}
		return s2
	}
	switch level {
	case "full":
		// full: keep everything
		li.Edge.Qualifiers = p.Edge.Qualifiers
		return li
	case "lite":
		// lite: remove qualifiers, keep descriptions unless they're derived
		li.Person.Description = stripDerived(li.Person.Description)
		li.Org.Description = stripDerived(li.Org.Description)
		return li
	default: // minimal
		// minimal: no qualifiers and no descriptions; only identifiers & KB refs
		li.Person.Description = ""
		li.Org.Description = ""
		return li
	}
}

func appendUnique(list []string, s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return list
	}
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), s) {
			return list
		}
	}
	return append(list, s)
}

func safePreview(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		max = 20000
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// =============================================================================
// Fragment cache (reduces Firestore reads)
// =============================================================================

type fragmentCache struct {
	load func(ctx context.Context, id string) (*hmodels.HorizonFragment, error)
	mu   sync.RWMutex
	m    map[string]*hmodels.HorizonFragment
}

func newFragmentCache(load func(ctx context.Context, id string) (*hmodels.HorizonFragment, error)) *fragmentCache {
	return &fragmentCache{load: load, m: map[string]*hmodels.HorizonFragment{}}
}

func (c *fragmentCache) get(ctx context.Context, id string) (*hmodels.HorizonFragment, error) {
	if c == nil || c.load == nil {
		return nil, fmt.Errorf("fragment cache not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("empty id")
	}
	c.mu.RLock()
	if v, ok := c.m[id]; ok && v != nil {
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()
	f, err := c.load(ctx, id)
	if err != nil || f == nil {
		return nil, err
	}
	c.mu.Lock()
	c.m[id] = f
	c.mu.Unlock()
	return f, nil
}

// =============================================================================
// Cursor persistence
// =============================================================================

type cursorState struct {
	StartAfter string    `json:"startAfter"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Scanned    int       `json:"scanned,omitempty"`
}

func readCursorFile(path string) (cursorState, error) {
	var st cursorState
	path = strings.TrimSpace(path)
	if path == "" {
		return st, fmt.Errorf("empty cursor file path")
	}
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
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// =============================================================================
// Firestore session with periodic refresh
// =============================================================================

type firestoreSession struct {
	projectID        string
	refreshInterval  time.Duration
	mu               sync.Mutex
	client           *gcp.FirestoreClient
	lastInit         time.Time
}

func newFirestoreSession(ctx context.Context, projectID string, refreshInterval time.Duration) (*firestoreSession, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("empty projectID")
	}
	s := &firestoreSession{projectID: projectID, refreshInterval: refreshInterval}
	if err := s.ForceRefresh(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *firestoreSession) DB(ctx context.Context) (*firestore.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		c, err := gcp.NewFirestoreClient(ctx, s.projectID)
		if err != nil {
			return nil, err
		}
		s.client = c
		s.lastInit = time.Now()
	}
	if s.refreshInterval > 0 && time.Since(s.lastInit) > s.refreshInterval {
		_ = s.client.Close()
		c, err := gcp.NewFirestoreClient(ctx, s.projectID)
		if err != nil {
			// keep old client if refresh fails
			return s.client.Client(), nil
		}
		s.client = c
		s.lastInit = time.Now()
	}
	return s.client.Client(), nil
}

func (s *firestoreSession) ForceRefresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	c, err := gcp.NewFirestoreClient(ctx, s.projectID)
	if err != nil {
		return err
	}
	s.client = c
	s.lastInit = time.Now()
	return nil
}

func (s *firestoreSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil
	}
	err := s.client.Close()
	s.client = nil
	return err
}

func isAuthExpiredErr(err error) bool {
	if err == nil {
		return false
	}
	// Firestore uses gRPC status codes.
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.Unauthenticated || st.Code() == codes.PermissionDenied {
			return true
		}
	}
	// Fallback string match
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauth") || strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "permission denied")
}


