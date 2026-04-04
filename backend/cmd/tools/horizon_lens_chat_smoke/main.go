package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/horizon"
	horizonanalytical "github.com/igorkirisyuk/sanskrite/internal/horizon/analytical"
	horizoninternalknowledge "github.com/igorkirisyuk/sanskrite/internal/horizon/internalknowledge"
	horizonlens "github.com/igorkirisyuk/sanskrite/internal/horizon/lens"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpathbuilder "github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	horizonworkscope "github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	corehttp "github.com/igorkirisyuk/sanskrite/internal/http"
	horizonhttp "github.com/igorkirisyuk/sanskrite/internal/http/horizon"
	realtime "github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	defaultModel           = "gemini-3.1-flash-lite-preview"
	defaultMaxToolRounds   = 4
	defaultMaxEntityHits   = 8
	defaultMaxArtifactRes  = 8
	defaultMaxPreparedHits = 8
	defaultMaxEvidenceDocs = 6
)

type smokeConfig struct {
	projectID     string
	tenantID      string
	lensID        string
	lensQuery     string
	scratchScope  string
	prompt        string
	model         string
	maxToolRounds int
	allowWrite    bool
	listOnly      bool
	out           string
	directTool    string
	directArgs    string
}

type lensCandidate struct {
	Lens  *horizonmodels.Lens `json:"lens"`
	Score int                 `json:"score"`
}

type toolExecutionRecord struct {
	Name       string      `json:"name"`
	Arguments  string      `json:"arguments"`
	DurationMs int64       `json:"durationMs"`
	OK         bool        `json:"ok"`
	Result     interface{} `json:"result,omitempty"`
	Error      string      `json:"error,omitempty"`
}

type smokeReport struct {
	ProjectID      string                `json:"projectId"`
	TenantID       string                `json:"tenantId"`
	LensID         string                `json:"lensId"`
	LensName       string                `json:"lensName"`
	Model          string                `json:"model"`
	Prompt         string                `json:"prompt"`
	AllowWrite     bool                  `json:"allowWrite"`
	StartedAt      string                `json:"startedAt"`
	FinishedAt     string                `json:"finishedAt"`
	DurationMs     int64                 `json:"durationMs"`
	ToolExecutions []toolExecutionRecord `json:"toolExecutions"`
	FinalAnswer    string                `json:"finalAnswer"`
	Error          string                `json:"error,omitempty"`
}

type internalKnowledgeHit struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	Kind       string         `json:"kind"`
	URI        string         `json:"uri,omitempty"`
	Score      float64        `json:"score"`
	StructData map[string]any `json:"structData,omitempty"`
}

type preparedEvidenceDoc struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Content        string   `json:"content"`
	URI            string   `json:"uri,omitempty"`
	CanonicalPaths []string `json:"canonicalPaths,omitempty"`
}

type smokeRuntime struct {
	projectID                string
	firestore                *gcp.FirestoreClient
	gcs                      *gcp.GCSClient
	logger                   *log.Logger
	modelManager             *ai.AIModelManager
	executor                 *ai.AITaskExecutor
	combinedClient           *gen_combined.Client
	vertexSearch             *ai.VertexAISearchClient
	vertexModel              *ai.VertexAIService
	supplyChainQueryEngine   *horizonpathbuilder.SupplyChainQueryEngine
	internalKnowledgeStoreID string
	internalKnowledgeBucket  string
	internalKnowledgeBuilder *horizoninternalknowledge.Builder

	agentJobsRepo             *corerepo.AgentJobsRepository
	dataSourcesRepo           *corerepo.DataSourcesRepository
	docsRepo                  *corerepo.DocumentsRepository
	projectsRepo              *corerepo.ProjectsRepository
	factsRepo                 *corerepo.OntologyFactsRepository
	fragmentsRepo             *horizonrepo.HorizonFragmentsRepository
	lensesRepo                *horizonrepo.LensesRepository
	lensItemsRepo             *horizonrepo.LensItemsRepository
	lensArtifactsRepo         *horizonrepo.LensArtifactsRepository
	scoutingJobsRepo          *horizonrepo.ScoutingJobsRepository
	materializedArtifactsRepo *horizonrepo.MaterializedArtifactsRepository
	domainAnswersRepo         *horizonrepo.DomainAnswerSnapshotsRepository
	analyticalPrimitivesRepo  *horizonrepo.AnalyticalPrimitivesRepository
	analyticalAggregatesRepo  *horizonrepo.AnalyticalAggregatesRepository
	signalLandingSummariesRepo *horizonrepo.SignalLandingSummariesRepository

	horizonService *horizon.HorizonService
	lensService    *horizonlens.LensService
	handlers       *horizonhttp.HorizonHandlers
}

type lensChatOrchestrationPlan struct {
	Intent           string                       `json:"intent"`
	ObjectQueries    []string                     `json:"objectQueries"`
	InspectPaths     []string                     `json:"inspectPaths"`
	ArtifactSearches []lensChatArtifactSearchPlan `json:"artifactSearches"`
	ShouldUseTrace   bool                         `json:"shouldUseTrace"`
	ResponseFocus    []string                     `json:"responseFocus"`
}

type lensChatArtifactSearchPlan struct {
	Anchors []string `json:"anchors"`
	Reason  string   `json:"reason,omitempty"`
}

type artifactPolicyCandidate struct {
	Path      string
	Kind      string
	Source    string
	IsFocal   bool
	BaseScore int
}

type internalKnowledgeQueryOptions struct {
	HydrateEvidence bool
	Synthesize      bool
}

type lensChatToolExecutor struct {
	rt         *smokeRuntime
	lens       *horizonmodels.Lens
	auth       *corehttp.AuthInfo
	allowWrite bool
	records    []toolExecutionRecord
}

func main() {
	ctx := context.Background()
	cfg := parseFlags()
	if strings.TrimSpace(cfg.projectID) == "" {
		log.Fatal("--project is required")
	}

	rt, err := newSmokeRuntime(ctx, cfg.projectID)
	if err != nil {
		log.Fatalf("runtime init failed: %v", err)
	}
	defer rt.Close()

	if cfg.listOnly {
		candidates, err := listActiveLenses(ctx, rt.firestore.Client(), strings.TrimSpace(cfg.tenantID), "")
		if err != nil {
			log.Fatalf("list lenses failed: %v", err)
		}
		printJSON(candidates)
		return
	}
	if strings.TrimSpace(cfg.prompt) == "" && strings.TrimSpace(cfg.directTool) == "" {
		log.Fatal("--prompt is required unless --list or --tool is used")
	}

	var lens *horizonmodels.Lens
	if strings.TrimSpace(cfg.scratchScope) != "" && strings.TrimSpace(cfg.lensID) == "" && strings.TrimSpace(cfg.lensQuery) == "" {
		lens, err = createScratchLens(ctx, rt, strings.TrimSpace(cfg.tenantID), strings.TrimSpace(cfg.scratchScope))
		if err != nil {
			log.Fatalf("create scratch lens failed: %v", err)
		}
	} else {
		lens, err = resolveLens(ctx, rt.firestore.Client(), rt.lensesRepo, strings.TrimSpace(cfg.tenantID), strings.TrimSpace(cfg.lensID), strings.TrimSpace(cfg.lensQuery))
		if err != nil {
			if strings.TrimSpace(cfg.scratchScope) == "" {
				log.Fatalf("resolve lens failed: %v", err)
			}
			lens, err = createScratchLens(ctx, rt, strings.TrimSpace(cfg.tenantID), strings.TrimSpace(cfg.scratchScope))
			if err != nil {
				log.Fatalf("resolve lens failed and scratch lens creation also failed: %v", err)
			}
		}
	}

	if strings.TrimSpace(cfg.directTool) != "" {
		result, err := runDirectTool(ctx, rt, lens, cfg)
		if strings.TrimSpace(cfg.out) != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.out), 0o755); err != nil {
				log.Fatalf("create output dir failed: %v", err)
			}
			writeJSON(cfg.out, result)
		}
		printJSON(result)
		if err != nil {
			os.Exit(1)
		}
		return
	}

	report := runLensChatSmoke(ctx, rt, lens, cfg)
	if strings.TrimSpace(cfg.out) != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.out), 0o755); err != nil {
			log.Fatalf("create output dir failed: %v", err)
		}
		writeJSON(cfg.out, report)
	}
	printJSON(report)
	if report.Error != "" {
		os.Exit(1)
	}
}

func parseFlags() smokeConfig {
	projectDefault := strings.TrimSpace(firstNonEmpty(
		os.Getenv("GCP_PROJECT_ID"),
		os.Getenv("GOOGLE_CLOUD_PROJECT"),
		"nv-horizon",
	))
	var cfg smokeConfig
	flag.StringVar(&cfg.projectID, "project", projectDefault, "GCP project id")
	flag.StringVar(&cfg.tenantID, "tenant", "global", "tenant id or all")
	flag.StringVar(&cfg.lensID, "lens-id", "", "exact lens id")
	flag.StringVar(&cfg.lensQuery, "lens-query", "", "substring used to auto-select active lens when -lens-id is empty")
	flag.StringVar(&cfg.scratchScope, "scratch-scope", "", "raw mention or canonical path used to create a temporary scratch lens when no active lens is selected")
	flag.StringVar(&cfg.prompt, "prompt", "", "user prompt to execute against lens chat")
	flag.StringVar(&cfg.model, "model", defaultModel, "model alias or concrete model name (e.g. gemini_balanced, gpt_fast)")
	flag.IntVar(&cfg.maxToolRounds, "max-tool-rounds", defaultMaxToolRounds, "max tool loop rounds to allow in prompt")
	flag.BoolVar(&cfg.allowWrite, "allow-write", false, "allow mutating tools (expand_lens_scope, apply_graph_change)")
	flag.BoolVar(&cfg.listOnly, "list", false, "list active lenses and exit")
	flag.StringVar(&cfg.out, "out", "", "optional path to save JSON report")
	flag.StringVar(&cfg.directTool, "tool", "", "optional direct tool name to execute without the model")
	flag.StringVar(&cfg.directArgs, "tool-args", "{}", "JSON args for --tool")
	flag.Parse()
	return cfg
}

func runDirectTool(ctx context.Context, rt *smokeRuntime, lens *horizonmodels.Lens, cfg smokeConfig) (map[string]interface{}, error) {
	auth := &corehttp.AuthInfo{
		UID:      "codex-smoke",
		Email:    "codex-smoke@" + firstNonEmpty(strings.TrimSpace(lens.TenantID), "global"),
		TenantID: strings.TrimSpace(lens.TenantID),
	}
	exec := &lensChatToolExecutor{
		rt:         rt,
		lens:       lens,
		auth:       auth,
		allowWrite: cfg.allowWrite,
		records:    make([]toolExecutionRecord, 0, 1),
	}
	toolName := strings.TrimSpace(cfg.directTool)
	if !exec.IsAllowed(toolName) {
		return map[string]interface{}{
			"lensId": lens.ID,
			"tool":   toolName,
			"error":  "tool is not allowed in current mode",
		}, fmt.Errorf("tool is not allowed in current mode")
	}
	args := json.RawMessage(strings.TrimSpace(firstNonEmpty(cfg.directArgs, "{}")))
	out, err := exec.Execute(ctx, toolName, args)
	result := map[string]interface{}{
		"projectId":      rt.projectID,
		"tenantId":       strings.TrimSpace(lens.TenantID),
		"lensId":         strings.TrimSpace(lens.ID),
		"lensName":       strings.TrimSpace(lens.Name),
		"tool":           toolName,
		"toolArgs":       compactJSONValue(args),
		"toolExecutions": exec.records,
	}
	if err != nil {
		result["error"] = err.Error()
		return result, err
	}
	result["result"] = compactJSONValue(out)
	return result, nil
}

func newSmokeRuntime(ctx context.Context, projectID string) (*smokeRuntime, error) {
	projectID = strings.TrimSpace(projectID)
	logger := log.New(os.Stdout, "[horizon_lens_chat_smoke] ", log.LstdFlags)

	fsClient, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("firestore init failed: %w", err)
	}
	db := fsClient.Client()
	gcsClient, gcsErr := gcp.NewGCSClient(ctx, projectID)
	if gcsErr != nil {
		logger.Printf("GCS init skipped: %v", gcsErr)
	}

	modelManager := ai.NewAIModelManager()
	aiServices, geminiAPIKey, openAIKey := buildAIServices(ctx, projectID, modelManager, logger)
	if geminiAPIKey != "" && os.Getenv("GEMINI_API_KEY") == "" {
		_ = os.Setenv("GEMINI_API_KEY", geminiAPIKey)
	}
	if openAIKey != "" && os.Getenv("OPENAI_API_KEY") == "" {
		_ = os.Setenv("OPENAI_API_KEY", openAIKey)
	}
	if len(aiServices) == 0 {
		_ = fsClient.Close()
		return nil, fmt.Errorf("no AI services configured")
	}
	executor := ai.NewAITaskExecutor(aiServices, modelManager).
		WithCircuitBreaker(ai.NewCircuitBreaker()).
		WithAdmissionController(ai.NoopAdmissionController())

	vertexLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
	searchLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_SEARCH_LOCATION"), "global"))
	vertexDataStore := strings.TrimSpace(firstNonEmpty(
		os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"),
	))
	internalKnowledgeStoreID := strings.TrimSpace(firstNonEmpty(
		os.Getenv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"),
	))
	internalKnowledgeBucket := strings.TrimSpace(os.Getenv("RAG_DOCUMENTS_BUCKET"))
	vertexSearch := ai.NewVertexAISearchClient(projectID, searchLocation, vertexDataStore, "")
	internalVertexSearch := vertexSearch
	if internalKnowledgeStoreID != "" {
		internalVertexSearch = vertexSearch.WithDataStore(internalKnowledgeStoreID)
	}
	vertexModel, vertexModelErr := ai.NewVertexAIService(projectID, vertexLocation, modelManager)
	if vertexModelErr != nil {
		logger.Printf("Vertex AI model init skipped: %v", vertexModelErr)
	}
	if vertexModel != nil && internalKnowledgeStoreID != "" {
		vertexModel = vertexModel.WithDataStore(internalKnowledgeStoreID)
	}

	gem := gen_gemini.NewClient(geminiAPIKey, projectID, vertexLocation, os.Getenv("GENAI_FAST_MODEL"), os.Getenv("GENAI_QUALITY_MODEL"))
	combinedClient := gen_combined.NewClient(gem, openAIKey)

	agentJobsRepo := corerepo.NewAgentJobsRepository(db)
	dataSourcesRepo := corerepo.NewDataSourcesRepository(db)
	docsRepo := corerepo.NewDocumentsRepository(db)
	interactionsRepo := corerepo.NewInteractionsRepository(db)
	projectsRepo := corerepo.NewProjectsRepository(db)
	factsRepo := corerepo.NewOntologyFactsRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	lensesRepo := horizonrepo.NewLensesRepository(db)
	lensItemsRepo := horizonrepo.NewLensItemsRepository(db)
	lensArtifactsRepo := horizonrepo.NewLensArtifactsRepository(db)
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	materializedArtifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(db)
	domainAnswersRepo := horizonrepo.NewDomainAnswerSnapshotsRepository(db)
	analyticalPrimitivesRepo := horizonrepo.NewAnalyticalPrimitivesRepository(db)
	analyticalAggregatesRepo := horizonrepo.NewAnalyticalAggregatesRepository(db)
	signalLandingSummariesRepo := horizonrepo.NewSignalLandingSummariesRepository(fsClient.Client())
	framesRepo := horizonrepo.NewFramesRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	fragmentVersionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(db)
	watchdogRepo := horizonrepo.NewWatchdogConfigsRepository(db)

	deps := horizon.HorizonServiceDependencies{
		AIExecutor:         executor,
		EmbeddingProvider:  ai.NewNoOpEmbedding(),
		VertexSearchClient: vertexSearch,
		AgentJobsRepo:      agentJobsRepo,
		RealtimePublisher:  &realtime.Publisher{},
		DocumentsRepo:      docsRepo,
		DataSourcesRepo:    dataSourcesRepo,
		Crawler:            crawler.NewWebCrawler(),
		Logger:             logger,
	}
	horizonService, err := horizon.NewHorizonService(
		deps,
		fragmentsRepo,
		framesRepo,
		lensesRepo,
		edgesRepo,
		watchdogRepo,
		scoutingJobsRepo,
		fragmentVersionsRepo,
	)
	if err != nil {
		_ = fsClient.Close()
		return nil, fmt.Errorf("horizon service init failed: %w", err)
	}

	lifecycle := horizonlens.NewLifecycleManager(fragmentsRepo)
	calculator := horizonlens.NewMetricsCalculator(fragmentsRepo)
	lensService := horizonlens.NewLensService(lensesRepo, fragmentsRepo, scoutingJobsRepo, factsRepo, lifecycle, calculator, nil).
		WithLensArtifactsRepo(lensArtifactsRepo)
	horizonService.SetLensService(lensService)
	horizonService.SetLensArtifactsRepo(lensArtifactsRepo)
	horizonService.SetDomainAnswerSnapshotsRepo(domainAnswersRepo)

	genAIHandler := corehttp.NewGenAIHandler().
		WithAIExecutor(executor).
		WithModelManager(modelManager)
	lensItemsHandler := horizonhttp.NewLensItemsHandler(lensItemsRepo, lensesRepo, projectsRepo)
	handlers := horizonhttp.NewHorizonHandlers(horizonService).
		WithGenAIHandler(genAIHandler).
		WithGCSClient(gcsClient).
		WithInteractionsRepo(interactionsRepo).
		WithLensItemsHandler(lensItemsHandler).
		WithMaterializedArtifactsRepo(materializedArtifactsRepo).
		WithSignalLandingSummariesRepo(signalLandingSummariesRepo)
	internalKnowledgeBuilder := horizoninternalknowledge.NewBuilder(
		fragmentsRepo,
		materializedArtifactsRepo,
		lensArtifactsRepo,
		analyticalPrimitivesRepo,
		analyticalAggregatesRepo,
		signalLandingSummariesRepo,
	)
	supplyChainQueryEngine := horizonpathbuilder.NewSupplyChainQueryEngine(
		fragmentsRepo,
		edgesRepo,
	).WithCacheTTL(10 * time.Minute)

	return &smokeRuntime{
		projectID:                 projectID,
		firestore:                 fsClient,
		gcs:                       gcsClient,
		logger:                    logger,
		modelManager:              modelManager,
		executor:                  executor,
		combinedClient:            combinedClient,
		vertexSearch:              internalVertexSearch,
		vertexModel:               vertexModel,
		supplyChainQueryEngine:    supplyChainQueryEngine,
		internalKnowledgeStoreID:  internalKnowledgeStoreID,
		internalKnowledgeBucket:   internalKnowledgeBucket,
		internalKnowledgeBuilder:  internalKnowledgeBuilder,
		agentJobsRepo:             agentJobsRepo,
		dataSourcesRepo:           dataSourcesRepo,
		docsRepo:                  docsRepo,
		projectsRepo:              projectsRepo,
		factsRepo:                 factsRepo,
		fragmentsRepo:             fragmentsRepo,
		lensesRepo:                lensesRepo,
		lensItemsRepo:             lensItemsRepo,
		lensArtifactsRepo:         lensArtifactsRepo,
		scoutingJobsRepo:          scoutingJobsRepo,
		materializedArtifactsRepo: materializedArtifactsRepo,
		domainAnswersRepo:         domainAnswersRepo,
		analyticalPrimitivesRepo:  analyticalPrimitivesRepo,
		analyticalAggregatesRepo:  analyticalAggregatesRepo,
		signalLandingSummariesRepo: signalLandingSummariesRepo,
		horizonService:            horizonService,
		lensService:               lensService,
		handlers:                  handlers,
	}, nil
}

func (r *smokeRuntime) Close() error {
	if r == nil || r.firestore == nil {
		return nil
	}
	return r.firestore.Close()
}

func buildAIServices(ctx context.Context, projectID string, modelManager *ai.AIModelManager, logger *log.Logger) ([]ai.AIService, string, string) {
	services := make([]ai.AIService, 0, 4)

	vertexLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
	vertexService, err := ai.NewVertexAIService(projectID, vertexLocation, modelManager)
	if err != nil {
		logger.Printf("Vertex AI init skipped: %v", err)
	} else {
		if ds := strings.TrimSpace(firstNonEmpty(
			os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"),
			os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"),
		)); ds != "" {
			vertexService = vertexService.WithDataStore(ds)
		}
		services = append(services, vertexService)
	}

	geminiAPIKey := sanitizeSecret(strings.TrimSpace(gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key")))
	if geminiAPIKey != "" {
		services = append(services, ai.NewGeminiService(geminiAPIKey, modelManager))
	}

	openAIKey := sanitizeSecret(strings.TrimSpace(gcp.GetEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "openai_api_key")))
	if openAIKey != "" {
		services = append(services, ai.NewOpenAIService(openAIKey, modelManager))
	}

	return services, geminiAPIKey, openAIKey
}

func listActiveLenses(ctx context.Context, db *firestore.Client, tenantID, needle string) ([]lensCandidate, error) {
	if db == nil {
		return nil, fmt.Errorf("firestore client is nil")
	}
	query := db.Collection("horizon_lenses").Where("status", "==", string(horizonmodels.LensStatusActive)).Limit(500)
	if tenant := strings.TrimSpace(tenantID); tenant != "" && !strings.EqualFold(tenant, "all") {
		query = query.Where("tenantId", "==", tenant)
	}
	docs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}
	tenantID = strings.TrimSpace(strings.ToLower(tenantID))
	needle = strings.TrimSpace(strings.ToLower(needle))
	out := make([]lensCandidate, 0, len(docs))
	for _, doc := range docs {
		var lens horizonmodels.Lens
		if err := doc.DataTo(&lens); err != nil {
			continue
		}
		if strings.TrimSpace(lens.ID) == "" {
			lens.ID = doc.Ref.ID
		}
		if tenantID != "" && tenantID != "all" && strings.ToLower(strings.TrimSpace(lens.TenantID)) != tenantID {
			continue
		}
		score := 1
		if needle != "" {
			score = scoreLensCandidate(&lens, needle)
			if score <= 0 {
				continue
			}
		}
		out = append(out, lensCandidate{Lens: &lens, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Lens.UpdatedAt.After(out[j].Lens.UpdatedAt)
	})
	return out, nil
}

func resolveLens(ctx context.Context, db *firestore.Client, lensesRepo *horizonrepo.LensesRepository, tenantID, explicitLensID, lensQuery string) (*horizonmodels.Lens, error) {
	if strings.TrimSpace(explicitLensID) != "" {
		return lensesRepo.Get(ctx, strings.TrimSpace(explicitLensID))
	}
	candidates, err := listActiveLenses(ctx, db, tenantID, lensQuery)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no active lens matched %q", lensQuery)
	}
	return candidates[0].Lens, nil
}

func createScratchLens(ctx context.Context, rt *smokeRuntime, tenantID, rawScope string) (*horizonmodels.Lens, error) {
	if rt == nil || rt.lensService == nil {
		return nil, fmt.Errorf("scratch lens runtime is unavailable")
	}
	scopeRef, err := resolveCanonicalPathForScratch(ctx, rt, rawScope)
	if err != nil {
		return nil, err
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	lens := &horizonmodels.Lens{
		TenantID:      tenantID,
		ProjectID:     "codex-lens-chat-smoke",
		Name:          "Codex Lens Chat Smoke: " + scopeRef,
		OriginalQuery: rawScope,
		Scope:         []string{scopeRef},
		Status:        string(horizonmodels.LensStatusActive),
		ViewConfig: map[string]interface{}{
			"focus": scopeRef,
		},
	}
	lensID, err := rt.lensService.CreateLens(ctx, lens)
	if err != nil {
		return nil, err
	}
	return rt.lensesRepo.Get(ctx, lensID)
}

func resolveCanonicalPathForScratch(ctx context.Context, rt *smokeRuntime, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("scratch scope is required")
	}
	if strings.HasPrefix(raw, "/") {
		return raw, nil
	}
	fragments, err := rt.horizonService.SearchFragments(ctx, raw, defaultMaxEntityHits)
	if err != nil {
		return "", err
	}
	preferred := make([]*horizonmodels.HorizonFragment, 0, len(fragments))
	fallback := make([]*horizonmodels.HorizonFragment, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		switch fragment.Type {
		case horizonmodels.FragmentTypeOrganization,
			horizonmodels.FragmentTypeFacility,
			horizonmodels.FragmentTypeProduct,
			horizonmodels.FragmentTypeDeposit:
			preferred = append(preferred, fragment)
		default:
			fallback = append(fallback, fragment)
		}
	}
	for _, fragment := range append(preferred, fallback...) {
		if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" && !strings.HasPrefix(cp, "/supply-link/") {
			return cp, nil
		}
	}
	return "", fmt.Errorf("could not resolve scratch scope %q", raw)
}

func scoreLensCandidate(lens *horizonmodels.Lens, needle string) int {
	if lens == nil || needle == "" {
		return 0
	}
	score := 0
	name := strings.ToLower(strings.TrimSpace(lens.Name))
	projectID := strings.ToLower(strings.TrimSpace(lens.ProjectID))
	query := strings.ToLower(strings.TrimSpace(lens.OriginalQuery))
	if strings.Contains(name, needle) {
		score += 100
	}
	if strings.Contains(query, needle) {
		score += 70
	}
	if strings.Contains(projectID, needle) {
		score += 40
	}
	for _, ref := range lens.Scope {
		if strings.Contains(strings.ToLower(strings.TrimSpace(ref)), needle) {
			score += 20
		}
	}
	return score
}

func runLensChatSmoke(ctx context.Context, rt *smokeRuntime, lens *horizonmodels.Lens, cfg smokeConfig) smokeReport {
	startedAt := time.Now().UTC()
	auth := &corehttp.AuthInfo{
		UID:      "codex-smoke",
		Email:    "codex-smoke@" + firstNonEmpty(strings.TrimSpace(lens.TenantID), "global"),
		TenantID: strings.TrimSpace(lens.TenantID),
	}
	exec := &lensChatToolExecutor{
		rt:         rt,
		lens:       lens,
		auth:       auth,
		allowWrite: cfg.allowWrite,
		records:    make([]toolExecutionRecord, 0, 8),
	}

	prompt := strings.TrimSpace(cfg.prompt)
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	report := smokeReport{
		ProjectID:  rt.projectID,
		TenantID:   strings.TrimSpace(lens.TenantID),
		LensID:     strings.TrimSpace(lens.ID),
		LensName:   strings.TrimSpace(lens.Name),
		Model:      strings.TrimSpace(cfg.model),
		Prompt:     prompt,
		AllowWrite: cfg.allowWrite,
		StartedAt:  startedAt.Format(time.RFC3339Nano),
	}

	finalAnswer, err := runLensChatOrchestrated(reqCtx, rt, exec, prompt, strings.TrimSpace(cfg.model))
	report.ToolExecutions = exec.records
	report.FinalAnswer = strings.TrimSpace(finalAnswer)
	if err != nil {
		report.Error = err.Error()
	}
	finishedAt := time.Now().UTC()
	report.FinishedAt = finishedAt.Format(time.RFC3339Nano)
	report.DurationMs = finishedAt.Sub(startedAt).Milliseconds()
	return report
}

func runLensChatOrchestrated(ctx context.Context, rt *smokeRuntime, exec *lensChatToolExecutor, prompt string, model string) (string, error) {
	internalReport, err := executeToolMap(ctx, exec, "search_internal_knowledge", map[string]interface{}{
		"question": strings.TrimSpace(prompt),
	})
	if err != nil {
		return "", err
	}
	if analyticalShortcutReady(internalReport) {
		return strings.TrimSpace(asString(internalReport["answer"])), nil
	}

	plan, planRecord, err := planLensChatOrchestration(ctx, rt, exec.lens, prompt, internalReport, model)
	if planRecord != nil {
		exec.records = append(exec.records, *planRecord)
	}
	if err != nil {
		return fallbackInternalAnswer(internalReport), err
	}

	var (
		searchReports   []map[string]interface{}
		inspectReport   map[string]interface{}
		artifactReports []map[string]interface{}
		preflightReport map[string]interface{}
	)

	if strings.EqualFold(strings.TrimSpace(plan.Intent), "change") {
		preflightReport, err = executeToolMap(ctx, exec, "preflight_scope_change", map[string]interface{}{
			"query": strings.TrimSpace(prompt),
		})
		if err != nil {
			return fallbackInternalAnswer(internalReport), err
		}
	} else {
		for _, query := range limitStrings(plan.ObjectQueries, 3) {
			report, qerr := executeToolMap(ctx, exec, "search_objects", map[string]interface{}{
				"query": query,
				"limit": defaultMaxEntityHits,
			})
			if qerr != nil {
				continue
			}
			searchReports = append(searchReports, report)
		}

		inspectPaths := uniqueStrings(append(
			append([]string(nil), plan.InspectPaths...),
			collectTopObjectPaths(searchReports, 2)...,
		))
		if len(inspectPaths) == 0 {
			inspectPaths = limitStrings(append(collectCanonicalPathsFromAnswerText(internalReport), stringSlice(internalReport["highlightedPaths"])...), 8)
		}
		if len(inspectPaths) > 0 {
			inspectReport, _ = executeToolMap(ctx, exec, "get_objects_by_paths", map[string]interface{}{
				"paths": limitStrings(inspectPaths, 8),
			})
		}

		artifactPlans := buildArtifactSearchQueue(plan.ArtifactSearches, prompt, internalReport, searchReports, inspectReport)
		for _, plan := range artifactPlans {
			if len(plan.Anchors) != 2 {
				continue
			}
			report, aerr := executeToolMap(ctx, exec, "search_supply_chain_artifacts", map[string]interface{}{
				"anchors": plan.Anchors,
				"limit":   defaultMaxArtifactRes,
			})
			if aerr != nil {
				continue
			}
			artifactReports = append(artifactReports, report)
			if totalArtifactResults(artifactReports) >= 2 {
				break
			}
		}
	}

	answer, synthRecord, err := synthesizeLensChatAnswer(ctx, rt, exec.lens, prompt, model, internalReport, plan, searchReports, inspectReport, artifactReports, preflightReport)
	if synthRecord != nil {
		exec.records = append(exec.records, *synthRecord)
	}
	if err != nil {
		fallback := strings.TrimSpace(firstNonEmpty(answer, fallbackInternalAnswer(internalReport)))
		if fallback == "" {
			return "", err
		}
		return fallback, nil
	}
	return strings.TrimSpace(answer), nil
}

func executeToolMap(ctx context.Context, exec *lensChatToolExecutor, name string, payload interface{}) (map[string]interface{}, error) {
	args, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	raw, err := exec.Execute(ctx, name, json.RawMessage(args))
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func planLensChatOrchestration(ctx context.Context, rt *smokeRuntime, lens *horizonmodels.Lens, question string, internalReport map[string]interface{}, model string) (lensChatOrchestrationPlan, *toolExecutionRecord, error) {
	started := time.Now()
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"intent": map[string]interface{}{
				"type": "string",
				"enum": []string{"answer", "change"},
			},
			"objectQueries": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"inspectPaths": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"artifactSearches": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"anchors": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"reason": map[string]interface{}{"type": "string"},
					},
					"required": []string{"anchors"},
				},
			},
			"shouldUseTrace": map[string]interface{}{"type": "boolean"},
			"responseFocus": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
		},
		"required": []string{"intent", "objectQueries", "inspectPaths", "artifactSearches", "shouldUseTrace", "responseFocus"},
	}
	plannerPrompt := buildLensChatPlannerPrompt(lens, question, internalReport)
	resp, err := rt.combinedClient.GenerateJSONStrict(ctx, plannerPrompt, nil, schema, gen_combined.Options{
		ModelOverride:   firstNonEmpty(strings.TrimSpace(model), defaultModel),
		Temperature:     0.1,
		MaxOutputTokens: 1400,
		ResponseFormat:  "json",
		SystemInstruction: strings.TrimSpace(`
You are the planner for Horizon lens chat.
Return only a compact retrieval plan JSON.
Prefer canonical paths already present in the grounded internal answer.
Only emit objectQueries when a needed object is still unresolved by canonical path.
artifactSearches must use exactly two canonical paths per search.
Prefer supplier-or-upstream organization + product pairs over customer + product pairs when looking for materialized supply-chain artifacts.
Use intent="change" only when the user is asking to add, change, mutate, expand, or edit the lens or graph.
Set shouldUseTrace=true only if artifact search is unlikely to answer the question.
`),
	})

	record := &toolExecutionRecord{
		Name:       "orchestrator_plan",
		Arguments:  truncateString(strings.TrimSpace(question), 800),
		DurationMs: time.Since(started).Milliseconds(),
	}
	if err != nil {
		record.OK = false
		record.Error = err.Error()
		return lensChatOrchestrationPlan{}, record, err
	}
	record.OK = true
	record.Result = compactJSONValue(json.RawMessage(resp.Content))

	plan, jerr := decodeLensChatOrchestrationPlan(resp.Content)
	if jerr != nil {
		record.OK = false
		record.Error = jerr.Error()
		return lensChatOrchestrationPlan{}, record, jerr
	}
	return plan, record, nil
}

func buildLensChatPlannerPrompt(lens *horizonmodels.Lens, question string, internalReport map[string]interface{}) string {
	return strings.TrimSpace(fmt.Sprintf(`
Lens:
- lensId: %s
- lensName: %s
- tenantId: %s

Question:
%s

Grounded internal context:
%s

Canonical paths already available:
%s

Top internal hits:
%s

Task:
Produce a minimal deterministic retrieval plan for this question.
If canonical paths already exist, prefer inspectPaths and artifactSearches over objectQueries.
If the question is about supply participants or routes, prefer artifactSearches with organization + product anchors.
	`, strings.TrimSpace(lens.ID), strings.TrimSpace(lens.Name), strings.TrimSpace(lens.TenantID), strings.TrimSpace(question), truncateString(fallbackInternalAnswer(internalReport), 2600), strings.Join(collectCanonicalPathsFromInternalReport(internalReport), "\n"), renderInternalHitSummary(internalReport)))
}

func decodeLensChatOrchestrationPlan(raw string) (lensChatOrchestrationPlan, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return lensChatOrchestrationPlan{}, fmt.Errorf("empty planner response")
	}
	var rawPlan map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rawPlan); err == nil {
		return normalizeLensChatOrchestrationPlan(rawPlan), nil
	}
	var actions []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &actions); err == nil {
		return normalizeLensChatPlanActions(actions), nil
	}
	return lensChatOrchestrationPlan{}, fmt.Errorf("unsupported planner response shape")
}

func normalizeLensChatOrchestrationPlan(rawPlan map[string]interface{}) lensChatOrchestrationPlan {
	plan := lensChatOrchestrationPlan{
		Intent:         firstNonEmpty(asString(rawPlan["intent"]), "answer"),
		ObjectQueries:  limitStrings(stringSlice(rawPlan["objectQueries"]), 3),
		InspectPaths:   limitStrings(filterCanonicalPaths(stringSlice(rawPlan["inspectPaths"])), 8),
		ShouldUseTrace: false,
		ResponseFocus:  limitStrings(stringSlice(rawPlan["responseFocus"]), 6),
	}
	if b, ok := rawPlan["shouldUseTrace"].(bool); ok {
		plan.ShouldUseTrace = b
	}
	for _, item := range sliceMap(rawPlan["artifactSearches"]) {
		appendArtifactSearchFromPlanner(&plan, item)
	}
	plan.ArtifactSearches = normalizeArtifactSearchPlans(plan.ArtifactSearches)
	return plan
}

func normalizeLensChatPlanActions(actions []map[string]interface{}) lensChatOrchestrationPlan {
	plan := lensChatOrchestrationPlan{
		Intent: "answer",
	}
	for _, action := range actions {
		switch strings.ToLower(strings.TrimSpace(asString(action["action"]))) {
		case "inspectpaths", "inspect_paths":
			plan.InspectPaths = append(plan.InspectPaths, filterCanonicalPaths(stringSlice(action["paths"]))...)
		case "artifactsearch", "artifact_search":
			appendArtifactSearchFromPlanner(&plan, action)
		case "searchobjects", "search_objects":
			plan.ObjectQueries = append(plan.ObjectQueries, strings.TrimSpace(asString(action["query"])))
		}
	}
	plan.ObjectQueries = limitStrings(plan.ObjectQueries, 3)
	plan.InspectPaths = limitStrings(filterCanonicalPaths(plan.InspectPaths), 8)
	plan.ArtifactSearches = normalizeArtifactSearchPlans(plan.ArtifactSearches)
	return plan
}

func appendArtifactSearchFromPlanner(plan *lensChatOrchestrationPlan, item map[string]interface{}) {
	if plan == nil {
		return
	}
	anchors := filterCanonicalPaths(stringSlice(item["anchors"]))
	if len(anchors) != 2 {
		if filters, ok := item["filters"].(map[string]interface{}); ok {
			candidates := make([]string, 0, 2)
			if org := strings.TrimSpace(asString(filters["organization"])); org != "" {
				candidates = append(candidates, org)
			} else if supplier := strings.TrimSpace(asString(filters["supplier"])); supplier != "" {
				candidates = append(candidates, supplier)
			} else if facility := strings.TrimSpace(asString(filters["facility"])); facility != "" {
				candidates = append(candidates, facility)
			}
			if product := strings.TrimSpace(asString(filters["product"])); product != "" {
				candidates = append(candidates, product)
			}
			anchors = filterCanonicalPaths(candidates)
		}
	}
	if len(anchors) != 2 {
		return
	}
	plan.ArtifactSearches = append(plan.ArtifactSearches, lensChatArtifactSearchPlan{
		Anchors: anchors,
		Reason:  firstNonEmpty(asString(item["reason"]), asString(item["query"])),
	})
}

func synthesizeLensChatAnswer(ctx context.Context, rt *smokeRuntime, lens *horizonmodels.Lens, question string, model string, internalReport map[string]interface{}, plan lensChatOrchestrationPlan, searchReports []map[string]interface{}, inspectReport map[string]interface{}, artifactReports []map[string]interface{}, preflightReport map[string]interface{}) (string, *toolExecutionRecord, error) {
	started := time.Now()
	prompt := buildLensChatSynthesisPrompt(lens, question, internalReport, plan, searchReports, inspectReport, artifactReports, preflightReport)
	resp, err := rt.combinedClient.Generate(ctx, prompt, nil, gen_combined.Options{
		ModelOverride:   firstNonEmpty(strings.TrimSpace(model), defaultModel),
		Temperature:     0.1,
		MaxOutputTokens: 2400,
		SystemInstruction: strings.TrimSpace(`
You are the response synthesizer for Horizon lens chat.
Do not invent facts or canonical paths.
Use the grounded internal answer as the base truth.
Use deterministic object and artifact results only to sharpen the answer and highlight suggestions.
Answer in the same language as the user's question.
If artifact search did not return enough evidence, say so briefly instead of pretending it did.
`),
	})
	record := &toolExecutionRecord{
		Name:       "orchestrator_synthesize",
		Arguments:  truncateString(strings.TrimSpace(question), 800),
		DurationMs: time.Since(started).Milliseconds(),
	}
	if err != nil {
		record.OK = false
		record.Error = err.Error()
		return fallbackInternalAnswer(internalReport), record, err
	}
	record.OK = true
	record.Result = compactJSONValueFromAny(map[string]interface{}{
		"answerPreview": truncateString(strings.TrimSpace(resp.Content), 2000),
		"model":         strings.TrimSpace(resp.Model),
	})
	return strings.TrimSpace(resp.Content), record, nil
}

func buildLensChatSynthesisPrompt(lens *horizonmodels.Lens, question string, internalReport map[string]interface{}, plan lensChatOrchestrationPlan, searchReports []map[string]interface{}, inspectReport map[string]interface{}, artifactReports []map[string]interface{}, preflightReport map[string]interface{}) string {
	return strings.TrimSpace(fmt.Sprintf(`
Lens:
- lensId: %s
- lensName: %s

Question:
%s

Grounded internal context:
%s

Planner response focus:
%s

Object search results:
%s

Inspected canonical objects:
%s

Materialized artifact results:
%s

Preflight change result:
%s

Return:
1. A concise answer.
2. Which objects or routes should be highlighted in the UI.
3. A brief note about uncertainty if artifact grounding was weak or absent.
	`, strings.TrimSpace(lens.ID), strings.TrimSpace(lens.Name), strings.TrimSpace(question), truncateString(fallbackInternalAnswer(internalReport), 3000), strings.Join(plan.ResponseFocus, ", "), renderCompactJSON(searchReports), renderCompactJSON(inspectReport), renderCompactJSON(artifactReports), renderCompactJSON(preflightReport)))
}

func buildSystemInstruction(lens *horizonmodels.Lens, allowWrite bool, maxToolRounds int) string {
	writePolicy := "Mutating tools are disabled. For change requests, use preflight and dry-run only."
	if allowWrite {
		writePolicy = "Mutating tools are enabled, but only use them when the user explicitly asked to change the lens or graph. Always preflight or dry-run before applying."
	}
	return strings.TrimSpace(fmt.Sprintf(`
You are testing Horizon lens chat tool usage against a real lens.

Lens:
- lensId: %s
- lensName: %s
- tenantId: %s
- projectId: %s

Rules:
- Use tools to ground every substantive answer.
- Prefer internal knowledge tools for semantic and domain-heavy questions.
- Use answer_internal_knowledge or search_internal_knowledge before topology tools unless the user asked for an explicit graph mutation.
- If you need to call a deterministic graph or supply-chain tool and you only have a raw name, first use search_objects to discover candidates, then get_objects_by_paths to inspect the best canonical paths, then call the deterministic tool with canonical refs only.
- Product identity is canonical product path when you need precise topology, but do not start with object search if the prepared internal corpus can answer the question.
- Do not invent canonical paths, supply-chain paths, or graph mutations.
- Use materialized artifact search after semantic retrieval when you need quick route context or UI highlights.
- Use fast_trace_supply_chain only when artifact search still leaves important ambiguity.
- Use full trace_supply_chain only as a last resort for exact deterministic path structure.
- Keep tool usage efficient; do not exceed roughly %d tool rounds.
- %s

Answer format:
- Give a concise final answer.
- Mention uncertainty explicitly if tools did not return enough evidence.
- When relevant, state which objects, paths, or relations should be highlighted in the UI.
`,
		strings.TrimSpace(lens.ID),
		strings.TrimSpace(lens.Name),
		strings.TrimSpace(lens.TenantID),
		strings.TrimSpace(lens.ProjectID),
		maxToolRounds,
		writePolicy,
	))
}

func buildToolDeclarations() []gen_gemini.Tool {
	return []gen_gemini.Tool{
		{
			Name:        "get_lens_context",
			Description: "Return high-level context for the current lens: scope, graph counts, and top visible refs.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_internal_knowledge_status",
			Description: "Return warm-state and prepared-corpus status for this lens: whether the internal Vertex answer plane is ready, corpus id, and prepared scope summary.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "search_internal_knowledge",
			Description: "Search the warmed internal knowledge corpus for semantically relevant prepared capsules. Use this for domain questions before any low-level graph or route tracing.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{"type": "string"},
					"limit":    map[string]interface{}{"type": "integer"},
				},
				"required": []string{"question"},
			},
		},
		{
			Name:        "answer_internal_knowledge",
			Description: "Answer a semantic question using the warmed internal knowledge corpus, hydrated evidence docs, and Vertex-grounded synthesis. Use this as the primary read path.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{"type": "string"},
				},
				"required": []string{"question"},
			},
		},
		{
			Name:        "call_lens_chat_handler",
			Description: "Invoke the live Horizon lens-chat handler in-process with tenant auth, using the same request/response path as the product chat.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{"type": "string"},
				},
				"required": []string{"message"},
			},
		},
		{
			Name:        "call_lens_console_handler",
			Description: "Invoke the live Horizon lens-console broker in-process with tenant auth, using the same request/response path as the product console functional-call surface.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tool": map[string]interface{}{"type": "string"},
					"input": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": true,
					},
					"confirm":       map[string]interface{}{"type": "boolean"},
					"publishEvents": map[string]interface{}{"type": "boolean"},
				},
				"required": []string{"tool"},
			},
		},
		{
			Name:        "search_objects",
			Description: "Broad object discovery by raw name or phrase. Returns candidate canonical objects from the system. Use this when you need to choose canonical paths before calling deterministic tools.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
					"limit": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "get_objects_by_paths",
			Description: "Fetch object details for one or more canonical paths. Use this to inspect candidates returned by search_objects before passing them into deterministic tools.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"paths": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"paths"},
			},
		},
		{
			Name:        "fast_trace_supply_chain",
			Description: "Fast deterministic trace over canonical supply links without expensive path annotation. Accepts canonical refs only. Use after internal knowledge and artifact search when you still need explicit path structure quickly.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"product":     map[string]interface{}{"type": "string"},
					"supplier":    map[string]interface{}{"type": "string"},
					"customer":    map[string]interface{}{"type": "string"},
					"closureMode": map[string]interface{}{"type": "string", "enum": []string{"exact", "upstream", "downstream", "both"}},
					"maxDepth":    map[string]interface{}{"type": "integer"},
					"maxPaths":    map[string]interface{}{"type": "integer"},
				},
				"required": []string{"product"},
			},
		},
		{
			Name:        "trace_supply_chain",
			Description: "Full deterministic trace with expensive path annotation. Accepts canonical refs only. Use only as a last resort after internal knowledge, artifact search, and fast trace.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"product":     map[string]interface{}{"type": "string"},
					"supplier":    map[string]interface{}{"type": "string"},
					"customer":    map[string]interface{}{"type": "string"},
					"closureMode": map[string]interface{}{"type": "string", "enum": []string{"exact", "upstream", "downstream", "both"}},
					"maxDepth":    map[string]interface{}{"type": "integer"},
					"maxPaths":    map[string]interface{}{"type": "integer"},
				},
				"required": []string{"product"},
			},
		},
		{
			Name:        "search_supply_chain_artifacts",
			Description: "Search materialized supply-chain artifacts by one or two canonical anchors. Accepts canonical paths only. Good for finding already-materialized routes relevant to the current lens.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"anchors": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"limit": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"anchors"},
			},
		},
		{
			Name:        "get_fragment_trace",
			Description: "Explain why a fragment matters by returning its local graph neighborhood, related supply chains, and propagation context.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fragmentRef": map[string]interface{}{"type": "string", "description": "Fragment ID, canonical path, or raw mention"},
					"hops":        map[string]interface{}{"type": "integer"},
				},
				"required": []string{"fragmentRef"},
			},
		},
		{
			Name:        "preflight_scope_change",
			Description: "Resolve a follow-up query in current lens context without mutating anything. Use this before add/expand actions.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "plan_graph_change",
			Description: "Create a graph-intent plan against the current lens graph snapshot. Use for requests like 'add supplier', 'add relation', or 'group these actors'.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":       map[string]interface{}{"type": "string"},
					"maxNewNodes": map[string]interface{}{"type": "integer"},
					"maxNewEdges": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "dry_run_graph_change",
			Description: "Plan and dry-run a graph-intent change against the current lens graph. Use this before any apply_graph_change call.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":       map[string]interface{}{"type": "string"},
					"maxNewNodes": map[string]interface{}{"type": "integer"},
					"maxNewEdges": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "expand_lens_scope",
			Description: "Mutating tool. Resolve a query and add the resulting canonical path to the lens scope. Use only when the user explicitly wants to add something to the lens.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "apply_graph_change",
			Description: "Mutating tool. Plan and apply a graph-intent patch. Use only when the user explicitly asked for a graph mutation and dry-run was already reviewed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":       map[string]interface{}{"type": "string"},
					"maxNewNodes": map[string]interface{}{"type": "integer"},
					"maxNewEdges": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (e *lensChatToolExecutor) IsAllowed(name string) bool {
	switch strings.TrimSpace(name) {
	case "get_lens_context",
		"get_internal_knowledge_status",
		"search_internal_knowledge",
		"answer_internal_knowledge",
		"call_lens_chat_handler",
		"call_lens_console_handler",
		"search_objects",
		"get_objects_by_paths",
		"fast_trace_supply_chain",
		"trace_supply_chain",
		"search_supply_chain_artifacts",
		"get_fragment_trace",
		"preflight_scope_change",
		"plan_graph_change",
		"dry_run_graph_change":
		return true
	case "expand_lens_scope", "apply_graph_change":
		return e.allowWrite
	default:
		return false
	}
}

func (e *lensChatToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	started := time.Now()
	record := toolExecutionRecord{
		Name:      strings.TrimSpace(name),
		Arguments: strings.TrimSpace(string(args)),
	}
	out, err := e.execute(ctx, name, args)
	record.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		record.OK = false
		record.Error = err.Error()
		e.records = append(e.records, record)
		return nil, err
	}
	record.OK = true
	record.Result = compactJSONValue(out)
	e.records = append(e.records, record)
	return out, nil
}

func (e *lensChatToolExecutor) execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	switch strings.TrimSpace(name) {
	case "get_lens_context":
		return e.getLensContext(ctx)
	case "get_internal_knowledge_status":
		return e.getInternalKnowledgeStatus(ctx)
	case "search_internal_knowledge":
		return e.searchInternalKnowledge(ctx, args)
	case "answer_internal_knowledge":
		return e.answerInternalKnowledge(ctx, args)
	case "call_lens_chat_handler":
		return e.callLensChatHandler(ctx, args)
	case "call_lens_console_handler":
		return e.callLensConsoleHandler(ctx, args)
	case "search_objects":
		return e.searchObjects(ctx, args)
	case "get_objects_by_paths":
		return e.getObjectsByPaths(ctx, args)
	case "fast_trace_supply_chain":
		return e.fastTraceSupplyChain(ctx, args)
	case "trace_supply_chain":
		return e.traceSupplyChain(ctx, args)
	case "search_supply_chain_artifacts":
		return e.searchSupplyChainArtifacts(ctx, args)
	case "get_fragment_trace":
		return e.getFragmentTrace(ctx, args)
	case "preflight_scope_change":
		return e.preflightScopeChange(ctx, args)
	case "plan_graph_change":
		return e.planGraphChange(ctx, args)
	case "dry_run_graph_change":
		return e.dryRunGraphChange(ctx, args)
	case "expand_lens_scope":
		return e.expandLensScope(ctx, args)
	case "apply_graph_change":
		return e.applyGraphChange(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (e *lensChatToolExecutor) getLensContext(ctx context.Context) (json.RawMessage, error) {
	lensArtifacts, _ := e.rt.lensArtifactsRepo.ListByLens(ctx, e.lens.ID, 250)
	materialized, _ := e.rt.materializedArtifactsRepo.ListByLens(ctx, e.lens.ID, 128)
	summary := map[string]interface{}{
		"lens": map[string]interface{}{
			"id":            strings.TrimSpace(e.lens.ID),
			"name":          strings.TrimSpace(e.lens.Name),
			"tenantId":      strings.TrimSpace(e.lens.TenantID),
			"projectId":     strings.TrimSpace(e.lens.ProjectID),
			"originalQuery": strings.TrimSpace(e.lens.OriginalQuery),
			"scope":         limitStrings(e.lens.Scope, 40),
			"scopeSize":     len(e.lens.Scope),
		},
		"lensArtifacts": map[string]interface{}{
			"count": countNonNilLensArtifacts(lensArtifacts),
			"top":   summarizeLensArtifacts(lensArtifacts, 12),
		},
		"materialized": map[string]interface{}{
			"count": countNonNilMaterialized(materialized),
			"top":   summarizeMaterializedArtifacts(materialized, 8),
		},
	}
	return marshalToolResult(summary)
}

func (e *lensChatToolExecutor) getInternalKnowledgeStatus(ctx context.Context) (json.RawMessage, error) {
	storeID := e.internalKnowledgeStoreID()
	warmState := e.internalKnowledgeWarmState()
	summary := map[string]interface{}{
		"available":                e.rt.internalKnowledgeBuilder != nil && e.rt.vertexSearch != nil,
		"internalKnowledgeStoreId": storeID,
		"internalKnowledgeBucket":  strings.TrimSpace(e.rt.internalKnowledgeBucket),
		"warmState":                compactJSONValueFromAny(warmState),
	}
	if corpusID := strings.TrimSpace(asString(warmState["corpusId"])); corpusID != "" {
		summary["preparedCorpus"] = map[string]interface{}{
			"corpusId":      corpusID,
			"docCount":      asInt(warmState["docCount"]),
			"scopeHash":     asString(warmState["scopeHash"]),
			"substrateHash": asString(warmState["substrateHash"]),
			"status":        asString(warmState["status"]),
		}
	} else {
		summary["preparedCorpusError"] = "internal knowledge corpus is not warmed for this lens"
	}
	return marshalToolResult(summary)
}

func (e *lensChatToolExecutor) searchInternalKnowledge(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Question string `json:"question"`
		Limit    int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	report, err := e.prepareInternalKnowledgeAnswer(ctx, strings.TrimSpace(req.Question), req.Limit, internalKnowledgeQueryOptions{})
	if err != nil {
		return nil, err
	}
	return marshalToolResult(report)
}

func (e *lensChatToolExecutor) answerInternalKnowledge(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	report, err := e.prepareInternalKnowledgeAnswer(ctx, strings.TrimSpace(req.Question), defaultMaxPreparedHits, internalKnowledgeQueryOptions{
		HydrateEvidence: true,
		Synthesize:      true,
	})
	if err != nil {
		return nil, err
	}
	return marshalToolResult(report)
}

func (e *lensChatToolExecutor) callLensChatHandler(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}
	if e.rt == nil || e.rt.handlers == nil {
		return nil, fmt.Errorf("horizon handlers are unavailable")
	}
	path := fmt.Sprintf("/api/horizon/lenses/%s/chat", strings.TrimSpace(e.lens.ID))
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, path, map[string]string{"message": req.Message})
	var out map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.LensChat, httpReq, &out); err != nil {
		return nil, err
	}
	return marshalToolResult(out)
}

func (e *lensChatToolExecutor) callLensConsoleHandler(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Tool          string                 `json:"tool"`
		Input         map[string]interface{} `json:"input,omitempty"`
		Confirm       bool                   `json:"confirm,omitempty"`
		PublishEvents *bool                  `json:"publishEvents,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	req.Tool = strings.TrimSpace(req.Tool)
	if req.Tool == "" {
		return nil, fmt.Errorf("tool is required")
	}
	if req.Input == nil {
		req.Input = map[string]interface{}{}
	}
	if e.rt == nil || e.rt.handlers == nil {
		return nil, fmt.Errorf("horizon handlers are unavailable")
	}
	path := fmt.Sprintf("/api/horizon/lenses/%s/console/run", strings.TrimSpace(e.lens.ID))
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, path, map[string]interface{}{
		"tool":          req.Tool,
		"input":         req.Input,
		"confirm":       req.Confirm,
		"publishEvents": req.PublishEvents,
	})
	var out map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.LensConsoleRun, httpReq, &out); err != nil {
		return nil, err
	}
	return marshalToolResult(out)
}

func (e *lensChatToolExecutor) prepareInternalKnowledgeAnswer(ctx context.Context, question string, limit int, opts internalKnowledgeQueryOptions) (map[string]interface{}, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	storeID := e.internalKnowledgeStoreID()
	if e.rt.vertexSearch == nil || storeID == "" {
		return nil, fmt.Errorf("internal knowledge search store is not configured")
	}
	warmState := e.internalKnowledgeWarmState()
	corpusID := strings.TrimSpace(asString(warmState["corpusId"]))
	if corpusID == "" {
		return nil, fmt.Errorf("internal knowledge corpus is not warmed for this lens")
	}
	preparedCorpus := map[string]interface{}{
		"corpusId":      corpusID,
		"status":        asString(warmState["status"]),
		"docCount":      asInt(warmState["docCount"]),
		"scopeHash":     asString(warmState["scopeHash"]),
		"substrateHash": asString(warmState["substrateHash"]),
	}
	if limit <= 0 {
		limit = defaultMaxPreparedHits
	}
	searchClient := e.rt.vertexSearch.WithDataStore(storeID)
	hits, warnings := searchPreparedInternalKnowledge(ctx, searchClient, question, corpusID, limit)
	analyticalHits, analyticalDocs, analyticalPaths, analyticalWarnings := e.loadAnalyticalEvidence(ctx, question)
	warnings = append(warnings, analyticalWarnings...)
	var evidenceDocs []preparedEvidenceDoc
	if opts.HydrateEvidence || opts.Synthesize {
		var evidenceWarnings []string
		evidenceDocs, evidenceWarnings = loadPreparedEvidenceDocs(ctx, e.rt.gcs, hits, defaultMaxEvidenceDocs)
		warnings = append(warnings, evidenceWarnings...)
	}
	hits = mergeInternalKnowledgeHits(analyticalHits, hits, maxInt(limit, defaultMaxPreparedHits))
	evidenceDocs = mergePreparedEvidenceDocs(analyticalDocs, evidenceDocs, defaultMaxEvidenceDocs)

	response := map[string]interface{}{
		"available":                true,
		"question":                 question,
		"internalKnowledgeStoreId": storeID,
		"corpusId":                 corpusID,
		"hitCount":                 len(hits),
		"preparedCorpus":           preparedCorpus,
		"hits":                     summarizeInternalKnowledgeHits(hits, limit),
		"highlightedPaths":         limitStrings(uniqueStrings(append(collectEvidenceCanonicalPaths(hits, evidenceDocs, 32), analyticalPaths...)), 32),
		"warnings":                 uniqueStrings(warnings),
		"warmState":                compactJSONValueFromAny(warmState),
	}
	if len(evidenceDocs) > 0 {
		response["evidenceDocs"] = summarizeEvidenceDocs(evidenceDocs, defaultMaxEvidenceDocs)
	}
	if shortcutAnswer, ok := buildAnalyticalShortcutAnswer(question, hits, evidenceDocs); ok {
		response["answer"] = shortcutAnswer
		response["analyticalShortcut"] = true
	}
	if opts.Synthesize {
		vertexModel := e.rt.vertexModel
		if vertexModel != nil {
			vertexModel = vertexModel.WithDataStore(storeID)
		}
		if !asBool(response["analyticalShortcut"]) {
			text, grounding, err := synthesizePreparedInternalKnowledgeAnswer(ctx, vertexModel, e.lens, question, hits, evidenceDocs)
			if err != nil {
				response["answerError"] = err.Error()
			} else {
				response["answer"] = strings.TrimSpace(text)
				if grounding != nil {
					response["grounding"] = compactJSONValueFromAny(grounding)
				}
			}
		}
	}
	return response, nil
}

func (e *lensChatToolExecutor) internalKnowledgeStoreID() string {
	if e == nil {
		return ""
	}
	if storeID := strings.TrimSpace(e.rt.internalKnowledgeStoreID); storeID != "" {
		return storeID
	}
	return strings.TrimSpace(asString(e.internalKnowledgeWarmState()["answerStore"]))
}

func (e *lensChatToolExecutor) internalKnowledgeWarmState() map[string]interface{} {
	if e == nil {
		return map[string]interface{}{}
	}
	return viewConfigMapValue(e.lens.ViewConfig, "internalKnowledge")
}

func (e *lensChatToolExecutor) loadAnalyticalEvidence(ctx context.Context, question string) ([]internalKnowledgeHit, []preparedEvidenceDoc, []string, []string) {
	intent := classifyInternalKnowledgeIntent(question)
	if e == nil || e.rt == nil || intent == "" {
		return nil, nil, nil, nil
	}
	scopePaths := limitStrings(horizonworkscope.LensWorkbenchScopePaths(e.lens), 48)
	hits := make([]internalKnowledgeHit, 0, 10)
	docs := make([]preparedEvidenceDoc, 0, 10)
	paths := make([]string, 0, 32)
	warnings := make([]string, 0, 4)

	if e.rt.signalLandingSummariesRepo != nil && (intent == "current_signal" || intent == "scenario") {
		summaries, err := e.rt.signalLandingSummariesRepo.ListByScopeRefs(ctx, strings.TrimSpace(e.lens.TenantID), scopePaths, 12)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("signal landing summary load failed: %v", err))
		} else {
			for i, summary := range summaries {
				if i >= 4 || summary == nil {
					break
				}
				hits = append(hits, internalHitFromSignalLandingSummary(summary, 1.45))
				docs = append(docs, preparedDocFromSignalLandingSummary(summary))
				paths = append(paths, signalLandingSummaryEventCoreRefsLocal(summary)...)
				paths = append(paths, signalLandingSummaryHaloRefsLocal(summary)...)
			}
		}
	}
	if e.rt.analyticalPrimitivesRepo == nil || e.rt.analyticalAggregatesRepo == nil {
		return mergeInternalKnowledgeHits(nil, hits, 10), mergePreparedEvidenceDocs(nil, docs, defaultMaxEvidenceDocs), limitStrings(uniqueStrings(paths), 32), warnings
	}
	aggregate, err := horizonanalytical.EnsureLensAggregate(ctx, e.rt.analyticalPrimitivesRepo, e.rt.analyticalAggregatesRepo, e.rt.materializedArtifactsRepo, e.lens, scopePaths, time.Now().UTC())
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("analytical aggregate refresh failed: %v", err))
		return mergeInternalKnowledgeHits(nil, hits, 10), mergePreparedEvidenceDocs(nil, docs, defaultMaxEvidenceDocs), limitStrings(uniqueStrings(paths), 32), warnings
	}
	if aggregate == nil {
		return mergeInternalKnowledgeHits(nil, hits, 10), mergePreparedEvidenceDocs(nil, docs, defaultMaxEvidenceDocs), limitStrings(uniqueStrings(paths), 32), warnings
	}
	hits = append(hits, internalHitFromAggregate(aggregate, 1.25))
	docs = append(docs, preparedDocFromAggregate(aggregate))
	paths = append(paths, aggregate.BasisRefs...)
	paths = append(paths, aggregate.ProductRefs...)
	paths = append(paths, aggregate.NodeRefs...)
	paths = append(paths, aggregate.RouteRefs...)
	paths = append(paths, aggregate.AffectedRefs...)
	primitives, err := e.rt.analyticalPrimitivesRepo.ListByIDs(ctx, limitStrings(aggregate.DerivedPrimitiveIDs, 8))
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("analytical primitive load failed: %v", err))
		return mergeInternalKnowledgeHits(nil, hits, 10), mergePreparedEvidenceDocs(nil, docs, defaultMaxEvidenceDocs), limitStrings(uniqueStrings(paths), 32), warnings
	}
	for _, primitive := range primitives {
		if primitive == nil {
			continue
		}
		hits = append(hits, internalHitFromPrimitive(primitive, 0.95))
		docs = append(docs, preparedDocFromPrimitive(primitive))
		paths = append(paths, primitive.BasisRefs...)
		paths = append(paths, primitive.ProductRefs...)
		paths = append(paths, primitive.NodeRefs...)
		paths = append(paths, primitive.RouteRefs...)
		paths = append(paths, primitive.AffectedRefs...)
	}
	return mergeInternalKnowledgeHits(nil, hits, 10), mergePreparedEvidenceDocs(nil, docs, defaultMaxEvidenceDocs), limitStrings(uniqueStrings(paths), 32), warnings
}

func (e *lensChatToolExecutor) searchObjects(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultMaxEntityHits
	}
	fragments, err := e.rt.horizonService.SearchFragments(ctx, query, limit*2)
	if err != nil {
		return nil, err
	}
	entities := make([]map[string]interface{}, 0, limit)
	for _, fragment := range fragments {
		if fragment == nil {
			continue
		}
		entities = append(entities, summarizeFragment(fragment, e.lens))
		if len(entities) >= limit {
			break
		}
	}
	return marshalToolResult(map[string]interface{}{
		"query":   query,
		"objects": entities,
		"total":   len(entities),
	})
}

func (e *lensChatToolExecutor) getObjectsByPaths(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	paths, err := requireCanonicalPaths(req.Paths)
	if err != nil {
		return nil, err
	}
	fragmentsByPath, err := e.rt.fragmentsRepo.BatchGetByCanonicalPathsForTenant(ctx, e.lens.TenantID, paths)
	if err != nil {
		return nil, err
	}
	objects := make([]map[string]interface{}, 0, len(paths))
	missing := make([]string, 0)
	for _, path := range paths {
		fragment := fragmentsByPath[path]
		if fragment == nil {
			missing = append(missing, path)
			continue
		}
		objects = append(objects, summarizeFragment(fragment, e.lens))
	}
	return marshalToolResult(map[string]interface{}{
		"paths":    paths,
		"objects":  objects,
		"missing":  missing,
		"found":    len(objects),
		"notFound": len(missing),
	})
}

func (e *lensChatToolExecutor) fastTraceSupplyChain(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	return e.runSupplyChainTrace(ctx, args, true)
}

func (e *lensChatToolExecutor) traceSupplyChain(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	return e.runSupplyChainTrace(ctx, args, false)
}

func (e *lensChatToolExecutor) runSupplyChainTrace(ctx context.Context, args json.RawMessage, fast bool) (json.RawMessage, error) {
	var req struct {
		Product     string `json:"product"`
		Supplier    string `json:"supplier,omitempty"`
		Customer    string `json:"customer,omitempty"`
		ClosureMode string `json:"closureMode,omitempty"`
		MaxDepth    int    `json:"maxDepth,omitempty"`
		MaxPaths    int    `json:"maxPaths,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	productRef, err := requireCanonicalPath(req.Product, "product")
	if err != nil {
		return marshalToolResult(map[string]interface{}{
			"available": false,
			"error":     err.Error(),
			"query": map[string]interface{}{
				"product":  strings.TrimSpace(req.Product),
				"supplier": strings.TrimSpace(req.Supplier),
				"customer": strings.TrimSpace(req.Customer),
			},
		})
	}
	supplierRef, err := requireOptionalCanonicalPath(req.Supplier, "supplier")
	if err != nil {
		return marshalToolResult(map[string]interface{}{
			"available": false,
			"error":     err.Error(),
			"query":     map[string]interface{}{"supplier": strings.TrimSpace(req.Supplier)},
		})
	}
	customerRef, err := requireOptionalCanonicalPath(req.Customer, "customer")
	if err != nil {
		return marshalToolResult(map[string]interface{}{
			"available": false,
			"error":     err.Error(),
			"query":     map[string]interface{}{"customer": strings.TrimSpace(req.Customer)},
		})
	}

	engine := e.rt.supplyChainQueryEngine
	if engine == nil {
		engine = horizonpathbuilder.NewSupplyChainQueryEngine(e.rt.fragmentsRepo, horizonrepo.NewDependencyEdgesRepository(e.rt.firestore.Client())).WithCacheTTL(10 * time.Minute)
		e.rt.supplyChainQueryEngine = engine
	}
	opts := horizonpathbuilder.DefaultSupplyChainQueryOptions()
	opts.TenantID = strings.TrimSpace(e.lens.TenantID)
	opts.MaxDepth = req.MaxDepth
	opts.MaxPaths = req.MaxPaths
	opts.ClosureMode = horizonpathbuilder.ProductClosureMode(strings.TrimSpace(req.ClosureMode))

	var result *horizonpathbuilder.SupplyChainTraceResult
	if supplierRef != "" || customerRef != "" {
		if fast {
			result, err = engine.TraceBetweenActorsFast(ctx, supplierRef, customerRef, productRef, opts)
		} else {
			result, err = engine.TraceBetweenActors(ctx, supplierRef, customerRef, productRef, opts)
		}
	} else {
		if fast {
			result, err = engine.TraceByProductFast(ctx, productRef, opts)
		} else {
			result, err = engine.TraceByProduct(ctx, productRef, opts)
		}
	}
	if err != nil {
		return marshalToolResult(map[string]interface{}{
			"available": false,
			"error":     err.Error(),
			"query": map[string]interface{}{
				"productRef":  productRef,
				"supplierRef": supplierRef,
				"customerRef": customerRef,
			},
		})
	}

	paths := make([]map[string]interface{}, 0, len(result.Paths))
	for i, path := range result.Paths {
		if path == nil {
			continue
		}
		paths = append(paths, summarizeSupplyChainPath(i, path))
		if len(paths) >= defaultMaxArtifactRes {
			break
		}
	}
	return marshalToolResult(map[string]interface{}{
		"query": map[string]interface{}{
			"productRef":  productRef,
			"supplierRef": supplierRef,
			"customerRef": customerRef,
		},
		"mode":             map[bool]string{true: "fast", false: "full"}[fast],
		"available":        true,
		"matchingProducts": limitStrings(result.MatchingProducts, 12),
		"suppliers":        limitStrings(result.Suppliers, 12),
		"customers":        limitStrings(result.Customers, 12),
		"pathCount":        len(result.Paths),
		"paths":            paths,
	})
}

func (e *lensChatToolExecutor) searchSupplyChainArtifacts(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Anchors []string `json:"anchors"`
		Limit   int      `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	if len(req.Anchors) == 0 {
		return nil, fmt.Errorf("anchors are required")
	}
	anchors, err := requireCanonicalPaths(req.Anchors)
	if err != nil {
		return nil, err
	}
	if len(anchors) > 2 {
		anchors = anchors[:2]
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultMaxArtifactRes
	}

	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, "/api/horizon/supply-chains/search", map[string]interface{}{
		"anchors": anchors,
		"limit":   limit,
		"lensId":  e.lens.ID,
	})
	var payload struct {
		Mode    string `json:"mode"`
		Anchors []struct {
			CanonicalPath string `json:"canonicalPath"`
			Name          string `json:"name"`
			Type          string `json:"type"`
		} `json:"anchors"`
		Results []struct {
			Artifact *horizonmodels.MaterializedArtifact `json:"artifact"`
			Match    map[string]interface{}              `json:"match"`
			Score    float64                             `json:"score"`
		} `json:"results"`
		Summary map[string]interface{} `json:"summary"`
	}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.SearchSupplyChains, httpReq, &payload); err != nil {
		return nil, err
	}
	results := make([]map[string]interface{}, 0, len(payload.Results))
	for _, item := range payload.Results {
		if item.Artifact == nil {
			continue
		}
		results = append(results, summarizeMaterializedArtifact(item.Artifact, item.Score, item.Match))
	}
	return marshalToolResult(map[string]interface{}{
		"mode":    payload.Mode,
		"anchors": payload.Anchors,
		"summary": payload.Summary,
		"results": results,
	})
}

func (e *lensChatToolExecutor) getFragmentTrace(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		FragmentRef string `json:"fragmentRef"`
		Hops        int    `json:"hops,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	fragmentID, canonicalPath, err := e.resolveFragmentIdentifier(ctx, req.FragmentRef)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/horizon/fragments/%s/trace?lensId=%s", fragmentID, e.lens.ID)
	if req.Hops > 0 {
		path += fmt.Sprintf("&hops=%d", req.Hops)
	}
	httpReq := e.rt.newAuthedRequest(ctx, e.auth, http.MethodGet, path, nil)
	var payload map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.GetFragmentTrace, httpReq, &payload); err != nil {
		return nil, err
	}
	return marshalToolResult(map[string]interface{}{
		"fragmentId":    fragmentID,
		"canonicalPath": canonicalPath,
		"trace":         compactJSONValueFromAny(payload),
	})
}

func (e *lensChatToolExecutor) preflightScopeChange(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, fmt.Sprintf("/api/horizon/lenses/%s/follow-up-preflight", e.lens.ID), map[string]string{
		"query": strings.TrimSpace(req.Query),
	})
	var payload map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.LensFollowUpPreflight, httpReq, &payload); err != nil {
		return nil, err
	}
	return marshalToolResult(payload)
}

func (e *lensChatToolExecutor) planGraphChange(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Query       string `json:"query"`
		MaxNewNodes int    `json:"maxNewNodes,omitempty"`
		MaxNewEdges int    `json:"maxNewEdges,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	planReq, err := e.buildGraphIntentPlanRequest(ctx, req.Query, req.MaxNewNodes, req.MaxNewEdges)
	if err != nil {
		return nil, err
	}
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, fmt.Sprintf("/api/horizon/lenses/%s/graph-intent/plan", e.lens.ID), planReq)
	var payload map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.PlanGraphIntent, httpReq, &payload); err != nil {
		return nil, err
	}
	return marshalToolResult(payload)
}

func (e *lensChatToolExecutor) dryRunGraphChange(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	plan, err := e.planGraphIntent(ctx, args)
	if err != nil {
		return nil, err
	}
	applyReq := horizonmodels.GraphIntentApplyRequest{
		IdempotencyKey: "lens_chat_smoke_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		ApplyMode:      horizonmodels.GraphIntentApplyModeDry,
		Plan:           *plan,
	}
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, fmt.Sprintf("/api/horizon/lenses/%s/graph-intent/dry-run", e.lens.ID), applyReq)
	var payload map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.DryRunGraphIntent, httpReq, &payload); err != nil {
		return nil, err
	}
	return marshalToolResult(payload)
}

func (e *lensChatToolExecutor) expandLensScope(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if !e.allowWrite {
		return nil, fmt.Errorf("mutating tools are disabled")
	}
	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	intentAgent := horizonagents.NewIntentResolverAgent(e.rt.executor)
	intent, err := intentAgent.ResolveWithContext(ctx, strings.TrimSpace(req.Query), nil)
	if err != nil {
		return nil, err
	}
	if intent == nil || len(intent.Anchors) == 0 {
		return nil, fmt.Errorf("no anchor resolved")
	}
	target := strings.TrimSpace(intent.Anchors[0].CanonicalPath)
	expansion, err := e.rt.lensService.ExpandScope(ctx, e.lens, horizonlens.ScopeExpansionRequest{
		LensID:         e.lens.ID,
		CanonicalPaths: []string{target},
		MaxAdds:        1,
		Reason:         "horizon_lens_chat_smoke",
	})
	if err != nil {
		return nil, err
	}
	reloaded, _ := e.rt.lensesRepo.Get(ctx, e.lens.ID)
	if reloaded != nil {
		e.lens = reloaded
	}
	return marshalToolResult(map[string]interface{}{
		"targetCanonicalPath": target,
		"scopeChanged":        expansion.ScopeChanged,
		"addedCount":          len(expansion.AppliedPaths),
		"appliedPaths":        expansion.AppliedPaths,
		"alreadyPresent":      expansion.AlreadyPresent,
		"requestedPaths":      expansion.RequestedPaths,
	})
}

func (e *lensChatToolExecutor) applyGraphChange(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if !e.allowWrite {
		return nil, fmt.Errorf("mutating tools are disabled")
	}
	plan, err := e.planGraphIntent(ctx, args)
	if err != nil {
		return nil, err
	}
	applyReq := horizonmodels.GraphIntentApplyRequest{
		IdempotencyKey: "lens_chat_smoke_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		ApplyMode:      horizonmodels.GraphIntentApplyModePatch,
		Plan:           *plan,
	}
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, fmt.Sprintf("/api/horizon/lenses/%s/graph-intent/apply?confirm=true", e.lens.ID), applyReq)
	httpReq.Header.Set("X-Graph-Intent-Confirm", "true")
	var payload map[string]interface{}
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.ApplyGraphIntent, httpReq, &payload); err != nil {
		return nil, err
	}
	return marshalToolResult(payload)
}

func (e *lensChatToolExecutor) planGraphIntent(ctx context.Context, args json.RawMessage) (*horizonmodels.GraphIntentPlanResponse, error) {
	var req struct {
		Query       string `json:"query"`
		MaxNewNodes int    `json:"maxNewNodes,omitempty"`
		MaxNewEdges int    `json:"maxNewEdges,omitempty"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, fmt.Errorf("decode args: %w", err)
	}
	planReq, err := e.buildGraphIntentPlanRequest(ctx, req.Query, req.MaxNewNodes, req.MaxNewEdges)
	if err != nil {
		return nil, err
	}
	httpReq := e.rt.newAuthedJSONRequest(ctx, e.auth, http.MethodPost, fmt.Sprintf("/api/horizon/lenses/%s/graph-intent/plan", e.lens.ID), planReq)
	var plan horizonmodels.GraphIntentPlanResponse
	if err := e.rt.invokeJSON(ctx, e.rt.handlers.PlanGraphIntent, httpReq, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (e *lensChatToolExecutor) buildGraphIntentPlanRequest(ctx context.Context, query string, maxNewNodes, maxNewEdges int) (horizonmodels.GraphIntentPlanRequest, error) {
	snapshot, err := e.buildGraphIntentSnapshot(ctx)
	if err != nil {
		return horizonmodels.GraphIntentPlanRequest{}, err
	}
	if maxNewNodes <= 0 {
		maxNewNodes = 40
	}
	if maxNewEdges <= 0 {
		maxNewEdges = 80
	}
	req := horizonmodels.GraphIntentPlanRequest{
		Version:       horizonmodels.GraphIntentVersionV1,
		RawQuery:      strings.TrimSpace(query),
		QueryLanguage: "ru",
		Snapshot:      snapshot,
		Options: horizonmodels.GraphIntentPlanOptions{
			AllowInternalGrounding: true,
			AllowSearchGrounding:   false,
			MaxNewNodes:            maxNewNodes,
			MaxNewEdges:            maxNewEdges,
		},
	}
	if err := req.NormalizeAndValidate(); err != nil {
		return horizonmodels.GraphIntentPlanRequest{}, err
	}
	return req, nil
}

func (e *lensChatToolExecutor) buildGraphIntentSnapshot(ctx context.Context) (horizonmodels.GraphIntentSnapshot, error) {
	items, err := e.rt.lensItemsRepo.ListByLens(ctx, e.lens.ID)
	if err != nil {
		return horizonmodels.GraphIntentSnapshot{}, err
	}
	nodes := make([]horizonmodels.GraphIntentSnapshotNode, 0, len(items))
	edges := make([]horizonmodels.GraphIntentSnapshotEdge, 0, len(items))
	nodeByFragment := map[string]string{}
	visibleNodeIDs := make([]string, 0)

	for _, item := range items {
		if item == nil || item.Kind != "node" {
			continue
		}
		node := horizonmodels.GraphIntentSnapshotNode{
			NodeID:        strings.TrimSpace(item.ID),
			FragmentID:    valueOrEmpty(item.FragmentID),
			CanonicalPath: valueOrEmpty(item.CanonicalPath),
			Label:         valueOrEmpty(item.Label),
			Type:          valueOrEmpty(item.Type),
		}
		if item.Position != nil {
			node.Position = &horizonmodels.XYPoint{X: item.Position.X, Y: item.Position.Y}
		}
		nodes = append(nodes, node)
		visibleNodeIDs = append(visibleNodeIDs, node.NodeID)
		if node.FragmentID != "" {
			nodeByFragment[node.FragmentID] = node.NodeID
		}
	}
	for _, item := range items {
		if item == nil || item.Kind != "edge" {
			continue
		}
		edges = append(edges, horizonmodels.GraphIntentSnapshotEdge{
			EdgeID:           strings.TrimSpace(item.ID),
			SourceNodeID:     nodeByFragment[valueOrEmpty(item.SourceFragmentID)],
			TargetNodeID:     nodeByFragment[valueOrEmpty(item.TargetFragmentID)],
			SourceFragmentID: valueOrEmpty(item.SourceFragmentID),
			TargetFragmentID: valueOrEmpty(item.TargetFragmentID),
			RelationType:     valueOrEmpty(item.EdgeType),
			Direction:        valueOrEmpty(item.Direction),
		})
	}
	if len(nodes) == 0 {
		for _, ref := range e.lens.Scope {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			fragment, ferr := e.rt.fragmentsRepo.GetByCanonicalPathForTenant(ctx, e.lens.TenantID, ref)
			if ferr != nil || fragment == nil {
				continue
			}
			nodeID := "scope:" + ref
			nodes = append(nodes, horizonmodels.GraphIntentSnapshotNode{
				NodeID:        nodeID,
				FragmentID:    strings.TrimSpace(fragment.ID),
				CanonicalPath: ref,
				Label:         fragmentDisplayName(fragment),
				Type:          string(fragment.Type),
			})
			visibleNodeIDs = append(visibleNodeIDs, nodeID)
		}
	}
	return horizonmodels.GraphIntentSnapshot{
		Nodes:          nodes,
		Edges:          edges,
		VisibleNodeIDs: visibleNodeIDs,
	}, nil
}

func (r *smokeRuntime) newAuthedRequest(ctx context.Context, auth *corehttp.AuthInfo, method, path string, body []byte) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	ctx = context.WithValue(ctx, corehttp.AuthContextKey{}, auth)
	return req.WithContext(ctx)
}

func (r *smokeRuntime) newAuthedJSONRequest(ctx context.Context, auth *corehttp.AuthInfo, method, path string, payload interface{}) *http.Request {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req := r.newAuthedRequest(ctx, auth, method, path, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func (r *smokeRuntime) invokeJSON(ctx context.Context, handler func(http.ResponseWriter, *http.Request), req *http.Request, out interface{}) error {
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code >= 400 {
		return fmt.Errorf("handler returned %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		return fmt.Errorf("decode handler response: %w", err)
	}
	return nil
}

func (e *lensChatToolExecutor) resolveCanonicalRef(ctx context.Context, raw string, typeHint string) (string, error) {
	ref, err := e.resolveCanonicalRefOptional(ctx, raw, typeHint)
	if err != nil {
		return "", err
	}
	if ref == "" {
		return "", fmt.Errorf("could not resolve %q", raw)
	}
	return ref, nil
}

func (e *lensChatToolExecutor) resolveCanonicalRefOptional(ctx context.Context, raw string, typeHint string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "/") {
		return raw, nil
	}
	normalizedHint := normalizeTypeHint(typeHint)
	for _, candidate := range buildResolveCandidates(raw, normalizedHint) {
		fragments, err := e.rt.horizonService.SearchFragments(ctx, candidate, defaultMaxEntityHits)
		if err != nil {
			return "", err
		}
		for _, fragment := range fragments {
			if fragment == nil {
				continue
			}
			if normalizedHint != "" && !strings.EqualFold(string(fragment.Type), normalizedHint) {
				continue
			}
			return strings.TrimSpace(fragment.CanonicalPath), nil
		}
	}
	return "", nil
}

func buildResolveCandidates(raw string, normalizedHint string) []string {
	candidates := []string{strings.TrimSpace(raw)}
	if normalizedHint == string(horizonmodels.FragmentTypeProduct) {
		candidates = append(candidates, singularizeSimple(raw))
	}
	return uniqueStrings(candidates)
}

func singularizeSimple(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if strings.HasSuffix(s, "ies") && len(s) > 3 {
		return strings.TrimSpace(raw[:len(raw)-3] + "y")
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && len(s) > 1 {
		return strings.TrimSpace(raw[:len(raw)-1])
	}
	return strings.TrimSpace(raw)
}

func (e *lensChatToolExecutor) resolveFragmentIdentifier(ctx context.Context, raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("fragmentRef is required")
	}
	if strings.HasPrefix(raw, "frag_") || !strings.HasPrefix(raw, "/") && looksLikeFirestoreID(raw) {
		fragment, err := e.rt.fragmentsRepo.Get(ctx, raw)
		if err == nil && fragment != nil {
			return strings.TrimSpace(fragment.ID), strings.TrimSpace(fragment.CanonicalPath), nil
		}
	}
	if strings.HasPrefix(raw, "/") {
		fragment, err := e.rt.fragmentsRepo.GetByCanonicalPathForTenant(ctx, e.lens.TenantID, raw)
		if err == nil && fragment != nil {
			return strings.TrimSpace(fragment.ID), strings.TrimSpace(fragment.CanonicalPath), nil
		}
	}
	ref, err := e.resolveCanonicalRef(ctx, raw, "")
	if err != nil {
		return "", "", err
	}
	fragment, err := e.rt.fragmentsRepo.GetByCanonicalPathForTenant(ctx, e.lens.TenantID, ref)
	if err != nil || fragment == nil {
		return "", "", fmt.Errorf("fragment not found for %q", raw)
	}
	return strings.TrimSpace(fragment.ID), strings.TrimSpace(fragment.CanonicalPath), nil
}

func requireCanonicalPath(raw string, fieldName string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%s is required", fieldName)
	}
	if !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("%s must be a canonical path; use search_objects first: %q", fieldName, value)
	}
	return value, nil
}

func requireOptionalCanonicalPath(raw string, fieldName string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	return requireCanonicalPath(value, fieldName)
}

func requireCanonicalPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("paths are required")
	}
	out := make([]string, 0, len(paths))
	for i, raw := range paths {
		path, err := requireCanonicalPath(raw, fmt.Sprintf("paths[%d]", i))
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return uniqueStrings(out), nil
}

func summarizeFragment(fragment *horizonmodels.HorizonFragment, lens *horizonmodels.Lens) map[string]interface{} {
	out := map[string]interface{}{
		"id":            strings.TrimSpace(fragment.ID),
		"canonicalPath": strings.TrimSpace(fragment.CanonicalPath),
		"label":         fragmentDisplayName(fragment),
		"type":          string(fragment.Type),
	}
	if fragment.Description != "" {
		out["description"] = strings.TrimSpace(fragment.Description)
	}
	if lens != nil {
		out["inLensScope"] = stringSliceContains(lens.Scope, fragment.CanonicalPath)
	}
	return out
}

func summarizeSupplyChainPath(index int, path *horizonpathbuilder.SupplyChainPath) map[string]interface{} {
	segments := make([]map[string]interface{}, 0, len(path.Segments))
	nodeRefs := make([]string, 0, len(path.Segments)*2)
	for _, segment := range path.Segments {
		segments = append(segments, map[string]interface{}{
			"supplierRef":      strings.TrimSpace(segment.SupplierRef),
			"productRef":       strings.TrimSpace(segment.ProductRef),
			"customerRef":      strings.TrimSpace(segment.CustomerRef),
			"transportViaRefs": limitStrings(segment.TransportViaRefs, 6),
			"confidence":       segment.Confidence,
			"segmentClass":     strings.TrimSpace(segment.SegmentClass),
		})
		nodeRefs = append(nodeRefs, strings.TrimSpace(segment.SupplierRef), strings.TrimSpace(segment.CustomerRef))
	}
	return map[string]interface{}{
		"pathIndex":    index,
		"confidence":   path.Confidence,
		"segmentCount": len(path.Segments),
		"nodeRefs":     limitStrings(uniqueStrings(nodeRefs), 20),
		"segments":     segments,
		"openGaps":     limitStrings(path.OpenGaps, 8),
	}
}

func summarizeMaterializedArtifacts(artifacts []*horizonmodels.MaterializedArtifact, limit int) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, limit)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		out = append(out, summarizeMaterializedArtifact(artifact, 0, nil))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func summarizeMaterializedArtifact(artifact *horizonmodels.MaterializedArtifact, score float64, match map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{
		"id":           strings.TrimSpace(artifact.ID),
		"canonicalKey": strings.TrimSpace(artifact.CanonicalKey),
		"kind":         string(artifact.Kind),
		"lensId":       strings.TrimSpace(artifact.LensID),
		"nodeRefs":     limitStrings(artifact.NodeRefs, 18),
		"pathStress":   artifact.PathStressScore,
		"bindingScore": artifact.BindingScore,
		"isPinned":     artifact.IsPinned,
		"updatedAt":    artifact.UpdatedAt.Format(time.RFC3339),
	}
	if score > 0 {
		out["score"] = score
	}
	if match != nil {
		out["match"] = compactJSONValueFromAny(match)
	}
	return out
}

func summarizeLensArtifacts(artifacts []*horizonmodels.LensArtifact, limit int) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, limit)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":            strings.TrimSpace(artifact.ID),
			"type":          string(artifact.Type),
			"canonicalPath": strings.TrimSpace(artifact.CanonicalPath),
			"sourcePath":    strings.TrimSpace(artifact.SourcePath),
			"targetPath":    strings.TrimSpace(artifact.TargetPath),
			"updatedAt":     artifact.UpdatedAt.Format(time.RFC3339),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func searchPreparedInternalKnowledge(ctx context.Context, client *ai.VertexAISearchClient, question, corpusID string, limit int) ([]internalKnowledgeHit, []string) {
	if client == nil {
		return nil, []string{"internal knowledge search client is nil"}
	}
	if limit <= 0 {
		limit = defaultMaxPreparedHits
	}
	hits := make([]internalKnowledgeHit, 0, limit*2)
	warnings := make([]string, 0, 4)
	for _, variant := range buildPreparedQueryVariants(question, corpusID) {
		resp, err := client.Search(ctx, &ai.SearchRequest{
			Query:    variant,
			PageSize: maxInt(limit*4, 24),
			SpellCorrectionSpec: &ai.SpellCorrectionSpec{
				Mode: "AUTO",
			},
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("vertex prepared search failed query=%q: %v", variant, err))
			continue
		}
		current := make([]internalKnowledgeHit, 0, len(resp.Results))
		for _, res := range resp.Results {
			structData := cloneStringAnyMap(res.Document.StructData)
			if asString(structData["type"]) != "internal_knowledge_capsule" {
				continue
			}
			if corpusID != "" && asString(structData["corpusId"]) != corpusID {
				continue
			}
			current = append(current, internalKnowledgeHit{
				ID:         vertexSearchDocID(res),
				Title:      strings.TrimSpace(firstNonEmpty(res.Document.Title, asString(structData["title"]))),
				Kind:       strings.TrimSpace(asString(structData["kind"])),
				URI:        strings.TrimSpace(firstNonEmpty(res.Document.URI, asString(structData["uri"]))),
				Score:      vertexSearchScore(res),
				StructData: structData,
			})
		}
		hits = mergeInternalKnowledgeHits(hits, current, limit*6)
	}
	return rerankPreparedInternalKnowledgeHits(question, hits, limit), warnings
}

func synthesizePreparedInternalKnowledgeAnswer(ctx context.Context, svc *ai.VertexAIService, lens *horizonmodels.Lens, question string, hits []internalKnowledgeHit, evidenceDocs []preparedEvidenceDoc) (string, *ai.GroundingEvidence, error) {
	if svc == nil {
		return "", nil, fmt.Errorf("vertex AI model is unavailable")
	}
	hitTitles := make([]string, 0, len(hits))
	for i, hit := range hits {
		if i >= 10 {
			break
		}
		hitTitles = append(hitTitles, firstNonEmpty(hit.Title, hit.ID))
	}
	prompt := strings.TrimSpace(fmt.Sprintf(`
You have access to an internal knowledge corpus built from the system graph, materialized supply-chain routes, relations, and prepared logistics capsules.
Use ONLY internal datastore grounding. Do not use web search. Do not invent missing facts.
Answer in the same language as the user's question.

Lens: %s
Question: %s

Likely relevant internal documents:
%s

Prepared evidence pack:
%s

Return:
1. A direct answer.
1a. If the evidence includes a fresh signal landing summary, answer from event-core first, then impact halo, then broader structural background.
2. Objects and how they are connected.
3. Products and routes.
4. Explicit gaps in internal knowledge.
Be concrete. Prefer canonical objects, explicit corridors, explicit products, and explicit route legs that are present in the evidence pack.
For questions about current disruptions or what changed right now, do not let older structural background override fresher signal-aware evidence.
Do not claim a gap if the evidence pack already contains the route, relation, or object.
`, firstNonEmpty(strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID)), question, strings.Join(hitTitles, "\n"), renderPreparedEvidenceDocs(evidenceDocs)))
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           "gemini-3-flash-preview",
			MinContextWindow:         32000,
			MaxOutputSize:            8192,
			MinCapabilityScore:       6,
			TextGenerationCapability: true,
		},
		TenantID: firstNonEmpty(strings.TrimSpace(lens.TenantID), "global"),
		Project:  strings.TrimSpace(lens.ProjectID),
		UseCase:  "horizon.lens_chat_smoke.internal_knowledge",
	}
	res, err := svc.ExecuteTask(ctx, task, "vertex-gemini-3-flash-preview")
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(res.Content), res.GroundingEvidence, nil
}

func summarizeInternalKnowledgeHits(hits []internalKnowledgeHit, limit int) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, minInt(limit, len(hits)))
	for i, hit := range hits {
		if i >= limit {
			break
		}
		out = append(out, map[string]interface{}{
			"id":             strings.TrimSpace(hit.ID),
			"title":          strings.TrimSpace(hit.Title),
			"kind":           strings.TrimSpace(hit.Kind),
			"score":          hit.Score,
			"uri":            strings.TrimSpace(hit.URI),
			"canonicalPaths": limitStrings(stringSlice(hit.StructData["canonicalPaths"]), 12),
			"questionHints":  limitStrings(stringSlice(hit.StructData["questionHints"]), 8),
		})
	}
	return out
}

func summarizeEvidenceDocs(docs []preparedEvidenceDoc, limit int) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, minInt(limit, len(docs)))
	for i, doc := range docs {
		if i >= limit {
			break
		}
		out = append(out, map[string]interface{}{
			"id":             strings.TrimSpace(doc.ID),
			"title":          strings.TrimSpace(doc.Title),
			"kind":           strings.TrimSpace(doc.Kind),
			"canonicalPaths": limitStrings(doc.CanonicalPaths, 12),
			"uri":            strings.TrimSpace(doc.URI),
			"contentPreview": truncateString(doc.Content, 800),
		})
	}
	return out
}

func collectEvidenceCanonicalPaths(hits []internalKnowledgeHit, docs []preparedEvidenceDoc, limit int) []string {
	paths := make([]string, 0, limit)
	for _, hit := range hits {
		paths = append(paths, stringSlice(hit.StructData["canonicalPaths"])...)
	}
	for _, doc := range docs {
		paths = append(paths, doc.CanonicalPaths...)
	}
	return limitStrings(paths, limit)
}

func collectCanonicalPathsFromInternalReport(report map[string]interface{}) []string {
	paths := make([]string, 0, 32)
	paths = append(paths, stringSlice(report["highlightedPaths"])...)
	paths = append(paths, collectCanonicalPathsFromAnswerText(report)...)
	for _, hit := range sliceMap(report["hits"]) {
		paths = append(paths, stringSlice(hit["canonicalPaths"])...)
	}
	for _, doc := range sliceMap(report["evidenceDocs"]) {
		paths = append(paths, stringSlice(doc["canonicalPaths"])...)
	}
	return limitStrings(filterCanonicalPaths(paths), 32)
}

func collectCanonicalPathsFromAnswerText(report map[string]interface{}) []string {
	return limitStrings(filterCanonicalPaths(extractCanonicalPathsFromText(asString(report["answer"]))), 16)
}

func renderInternalHitSummary(report map[string]interface{}) string {
	hits := sliceMap(report["hits"])
	if len(hits) == 0 {
		return "No internal hits."
	}
	lines := make([]string, 0, minInt(len(hits), 6))
	for i, hit := range hits {
		if i >= 6 {
			break
		}
		lines = append(lines, fmt.Sprintf("- kind=%s title=%s paths=%s", asString(hit["kind"]), asString(hit["title"]), strings.Join(limitStrings(stringSlice(hit["canonicalPaths"]), 4), ", ")))
	}
	return strings.Join(lines, "\n")
}

func normalizeArtifactSearchPlans(plans []lensChatArtifactSearchPlan) []lensChatArtifactSearchPlan {
	out := make([]lensChatArtifactSearchPlan, 0, len(plans))
	seen := map[string]struct{}{}
	for _, plan := range plans {
		anchors := limitStrings(filterCanonicalPaths(plan.Anchors), 2)
		if len(anchors) != 2 {
			continue
		}
		key := strings.Join(anchors, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, lensChatArtifactSearchPlan{
			Anchors: anchors,
			Reason:  strings.TrimSpace(plan.Reason),
		})
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func buildArtifactSearchQueue(plans []lensChatArtifactSearchPlan, question string, internalReport map[string]interface{}, searchReports []map[string]interface{}, inspectReport map[string]interface{}) []lensChatArtifactSearchPlan {
	queue := make([]lensChatArtifactSearchPlan, 0, 10)
	queue = append(queue, normalizeArtifactSearchPlans(plans)...)

	products, actors := collectArtifactPolicyCandidates(question, internalReport, searchReports, inspectReport)
	stageDefs := []struct {
		name          string
		actorSource   []string
		actorFocal    *bool
		productSource []string
	}{
		{name: "answer_counterparty", actorSource: []string{"answer", "inspect"}, actorFocal: boolPtr(false), productSource: []string{"answer", "inspect"}},
		{name: "search_counterparty", actorSource: []string{"search", "highlight"}, actorFocal: boolPtr(false), productSource: []string{"answer", "inspect", "highlight"}},
		{name: "focal_actor", actorSource: []string{"answer", "inspect", "highlight"}, actorFocal: boolPtr(true), productSource: []string{"answer", "inspect", "highlight"}},
		{name: "broad_actor", actorSource: []string{"answer", "inspect", "search", "highlight"}, actorFocal: nil, productSource: []string{"answer", "inspect", "highlight", "search"}},
	}
	for _, stage := range stageDefs {
		stageActors := filterArtifactCandidates(actors, stage.actorSource, stage.actorFocal)
		stageProducts := filterArtifactCandidates(products, stage.productSource, nil)
		for _, actor := range stageActors {
			for _, product := range stageProducts {
				if actor.Path == "" || product.Path == "" {
					continue
				}
				queue = append(queue, lensChatArtifactSearchPlan{
					Anchors: []string{actor.Path, product.Path},
					Reason:  stage.name,
				})
				if len(queue) >= 10 {
					return normalizeArtifactSearchPlans(queue)
				}
			}
		}
	}
	return normalizeArtifactSearchPlans(queue)
}

func collectArtifactPolicyCandidates(question string, internalReport map[string]interface{}, searchReports []map[string]interface{}, inspectReport map[string]interface{}) ([]artifactPolicyCandidate, []artifactPolicyCandidate) {
	questionTokenSet := normalizedTokenSet(question)
	products := make([]artifactPolicyCandidate, 0, 12)
	actors := make([]artifactPolicyCandidate, 0, 12)

	add := func(path, source string) {
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasPrefix(path, "/") {
			return
		}
		kind := artifactCandidateKind(path)
		if kind == "" {
			return
		}
		candidate := artifactPolicyCandidate{
			Path:      path,
			Kind:      kind,
			Source:    source,
			IsFocal:   tokenOverlapScore(questionTokenSet, normalizedTokenSet(path)) > 0,
			BaseScore: artifactCandidateBaseScore(path, source),
		}
		if kind == "product" {
			products = append(products, candidate)
			return
		}
		actors = append(actors, candidate)
	}

	for _, path := range collectCanonicalPathsFromAnswerText(internalReport) {
		add(path, "answer")
	}
	for _, path := range stringSlice(internalReport["highlightedPaths"]) {
		add(path, "highlight")
	}
	for _, hit := range sliceMap(internalReport["hits"]) {
		for _, path := range stringSlice(hit["canonicalPaths"]) {
			add(path, "hit")
		}
	}
	for _, object := range sliceMap(inspectReport["objects"]) {
		add(asString(object["canonicalPath"]), "inspect")
	}
	for _, report := range searchReports {
		for _, object := range sliceMap(report["objects"]) {
			add(asString(object["canonicalPath"]), "search")
		}
	}

	return dedupeArtifactCandidates(products), dedupeArtifactCandidates(actors)
}

func artifactCandidateKind(path string) string {
	switch {
	case strings.HasPrefix(path, "/product/"):
		return "product"
	case strings.HasPrefix(path, "/org/"), strings.HasPrefix(path, "/fac/"):
		return "actor"
	default:
		return ""
	}
}

func artifactCandidateBaseScore(path, source string) int {
	score := 0
	switch {
	case strings.HasPrefix(path, "/product/component/"):
		score += 28
	case strings.HasPrefix(path, "/product/finished/"):
		score += 20
	case strings.HasPrefix(path, "/product/raw/"):
		score += 14
	case strings.HasPrefix(path, "/org/"):
		score += 18
	case strings.HasPrefix(path, "/fac/"):
		score += 14
	}
	switch strings.TrimSpace(source) {
	case "answer":
		score += 18
	case "inspect":
		score += 14
	case "search":
		score += 10
	case "highlight":
		score += 8
	case "hit":
		score += 6
	}
	return score
}

func dedupeArtifactCandidates(items []artifactPolicyCandidate) []artifactPolicyCandidate {
	best := make(map[string]artifactPolicyCandidate, len(items))
	for _, item := range items {
		if item.Path == "" {
			continue
		}
		current, ok := best[item.Path]
		if !ok || item.BaseScore > current.BaseScore {
			best[item.Path] = item
		}
	}
	out := make([]artifactPolicyCandidate, 0, len(best))
	for _, item := range best {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BaseScore == out[j].BaseScore {
			return out[i].Path < out[j].Path
		}
		return out[i].BaseScore > out[j].BaseScore
	})
	return out
}

func filterArtifactCandidates(items []artifactPolicyCandidate, sources []string, focal *bool) []artifactPolicyCandidate {
	sourceSet := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		sourceSet[strings.TrimSpace(source)] = struct{}{}
	}
	out := make([]artifactPolicyCandidate, 0, len(items))
	for _, item := range items {
		if len(sourceSet) > 0 {
			if _, ok := sourceSet[item.Source]; !ok {
				continue
			}
		}
		if focal != nil && item.IsFocal != *focal {
			continue
		}
		out = append(out, item)
	}
	return out
}

func boolPtr(v bool) *bool {
	return &v
}

func filterCanonicalPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, "/") {
			out = append(out, path)
		}
	}
	return uniqueStrings(out)
}

func collectTopObjectPaths(reports []map[string]interface{}, perReport int) []string {
	out := make([]string, 0, len(reports)*perReport)
	for _, report := range reports {
		objects := sliceMap(report["objects"])
		for i, object := range objects {
			if i >= perReport {
				break
			}
			if path := strings.TrimSpace(asString(object["canonicalPath"])); path != "" {
				out = append(out, path)
			}
		}
	}
	return uniqueStrings(out)
}

func totalArtifactResults(reports []map[string]interface{}) int {
	total := 0
	for _, report := range reports {
		total += len(sliceMap(report["results"]))
	}
	return total
}

func classifyInternalKnowledgeIntent(question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	switch {
	case q == "":
		return ""
	case strings.Contains(q, "right now"), strings.Contains(q, "currently"), strings.Contains(q, "current disruption"), strings.Contains(q, "what disruptions"), strings.Contains(q, "what changed"), strings.Contains(q, "affecting "), strings.Contains(q, "сейчас"), strings.Contains(q, "прямо сейчас"), strings.Contains(q, "какие сбои"), strings.Contains(q, "что изменилось"):
		return "current_signal"
	case strings.Contains(q, "what happens if"), strings.Contains(q, "impact if"), strings.Contains(q, "disruption impact"), strings.Contains(q, "что будет если"), strings.Contains(q, "влияние сбоя"), strings.Contains(q, "impact of disruption"):
		return "scenario"
	case strings.Contains(q, "weak point"), strings.Contains(q, "bottleneck"), strings.Contains(q, "critical dependenc"), strings.Contains(q, "single point of failure"), strings.Contains(q, "fragility"), strings.Contains(q, "chokepoint"), strings.Contains(q, "узк"), strings.Contains(q, "слаб"), strings.Contains(q, "зависимост"), strings.Contains(q, "хрупк"), strings.Contains(q, "бутылоч"):
		return "baseline_risk"
	default:
		return ""
	}
}

func internalHitFromAggregate(aggregate *horizonmodels.AnalyticalAggregate, boost float64) internalKnowledgeHit {
	paths := uniqueStrings(append(append([]string{aggregate.ScopeRef, aggregate.SubjectRef}, aggregate.BasisRefs...), append(append(append(aggregate.ProductRefs, aggregate.NodeRefs...), aggregate.RouteRefs...), aggregate.AffectedRefs...)...))
	return internalKnowledgeHit{
		ID:    strings.TrimSpace(aggregate.ID),
		Title: firstNonEmpty(strings.TrimSpace(aggregate.AggregateType), "analytical aggregate"),
		Kind:  "analytical_aggregate",
		URI:   "analytical://aggregate/" + strings.TrimSpace(aggregate.ID),
		Score: aggregate.SeverityScore * boost,
		StructData: map[string]any{
			"type":           "analytical_aggregate",
			"title":          firstNonEmpty(strings.TrimSpace(aggregate.AggregateType), "analytical aggregate"),
			"canonicalPaths": paths,
		},
	}
}

func internalHitFromPrimitive(primitive *horizonmodels.AnalyticalPrimitive, boost float64) internalKnowledgeHit {
	paths := uniqueStrings(append(append([]string{primitive.ScopeRef, primitive.SubjectRef}, primitive.BasisRefs...), append(append(append(primitive.ProductRefs, primitive.NodeRefs...), primitive.RouteRefs...), primitive.AffectedRefs...)...))
	return internalKnowledgeHit{
		ID:    strings.TrimSpace(primitive.ID),
		Title: firstNonEmpty(strings.TrimSpace(primitive.PrimitiveType), "analytical primitive"),
		Kind:  "analytical_primitive",
		URI:   "analytical://primitive/" + strings.TrimSpace(primitive.ID),
		Score: primitive.SeverityScore * boost,
		StructData: map[string]any{
			"type":           "analytical_primitive",
			"title":          firstNonEmpty(strings.TrimSpace(primitive.PrimitiveType), "analytical primitive"),
			"canonicalPaths": paths,
		},
	}
}

func internalHitFromSignalLandingSummary(summary *horizonmodels.SignalLandingSummary, boost float64) internalKnowledgeHit {
	paths := uniqueStrings(append(signalLandingSummaryEventCoreRefsLocal(summary), signalLandingSummaryHaloRefsLocal(summary)...))
	return internalKnowledgeHit{
		ID:    strings.TrimSpace(summary.ID),
		Title: buildPreparedSignalLandingTitle(summary),
		Kind:  "signal_landing_summary",
		URI:   "signal-landing://summary/" + strings.TrimSpace(summary.ID),
		Score: internalSignalSummaryBoost(summary) * boost,
		StructData: map[string]any{
			"type":           "signal_landing_summary",
			"title":          buildPreparedSignalLandingTitle(summary),
			"canonicalPaths": paths,
			"questionHints":  append([]string(nil), summary.TopClaims...),
			"eventCoreRefs":  signalLandingSummaryEventCoreRefsLocal(summary),
			"haloRefs":       signalLandingSummaryHaloRefsLocal(summary),
			"updatedAt":      summary.UpdatedAt.Format(time.RFC3339Nano),
			"freshnessClass": internalSignalFreshnessClass(summary.UpdatedAt),
		},
	}
}

func preparedDocFromAggregate(aggregate *horizonmodels.AnalyticalAggregate) preparedEvidenceDoc {
	return preparedEvidenceDoc{
		ID:             strings.TrimSpace(aggregate.ID),
		Kind:           "analytical_aggregate",
		Title:          firstNonEmpty(strings.TrimSpace(aggregate.AggregateType), "analytical aggregate"),
		Content:        strings.TrimSpace(aggregate.GenerateVectorText()),
		URI:            "analytical://aggregate/" + strings.TrimSpace(aggregate.ID),
		CanonicalPaths: uniqueStrings(append(append([]string{aggregate.ScopeRef, aggregate.SubjectRef}, aggregate.BasisRefs...), append(append(append(aggregate.ProductRefs, aggregate.NodeRefs...), aggregate.RouteRefs...), aggregate.AffectedRefs...)...)),
	}
}

func preparedDocFromPrimitive(primitive *horizonmodels.AnalyticalPrimitive) preparedEvidenceDoc {
	return preparedEvidenceDoc{
		ID:             strings.TrimSpace(primitive.ID),
		Kind:           "analytical_primitive",
		Title:          firstNonEmpty(strings.TrimSpace(primitive.PrimitiveType), "analytical primitive"),
		Content:        strings.TrimSpace(primitive.GenerateVectorText()),
		URI:            "analytical://primitive/" + strings.TrimSpace(primitive.ID),
		CanonicalPaths: uniqueStrings(append(append([]string{primitive.ScopeRef, primitive.SubjectRef}, primitive.BasisRefs...), append(append(append(primitive.ProductRefs, primitive.NodeRefs...), primitive.RouteRefs...), primitive.AffectedRefs...)...)),
	}
}

func preparedDocFromSignalLandingSummary(summary *horizonmodels.SignalLandingSummary) preparedEvidenceDoc {
	return preparedEvidenceDoc{
		ID:             strings.TrimSpace(summary.ID),
		Kind:           "signal_landing_summary",
		Title:          buildPreparedSignalLandingTitle(summary),
		Content:        buildPreparedSignalLandingDoc(summary),
		URI:            "signal-landing://summary/" + strings.TrimSpace(summary.ID),
		CanonicalPaths: uniqueStrings(append(signalLandingSummaryEventCoreRefsLocal(summary), signalLandingSummaryHaloRefsLocal(summary)...)),
	}
}

func buildPreparedSignalLandingTitle(summary *horizonmodels.SignalLandingSummary) string {
	if summary == nil {
		return "signal landing summary"
	}
	if len(summary.AffectedZoneRefs) > 0 {
		return "Fresh signal around " + summary.AffectedZoneRefs[0]
	}
	if len(summary.TopConfirmedRefs) > 0 {
		return "Fresh signal around " + summary.TopConfirmedRefs[0]
	}
	return "Fresh signal for " + strings.TrimSpace(summary.ScopeRef)
}

func buildPreparedSignalLandingDoc(summary *horizonmodels.SignalLandingSummary) string {
	if summary == nil {
		return ""
	}
	lines := []string{
		"Fresh signal landing summary",
		"Scope: " + strings.TrimSpace(summary.ScopeRef),
	}
	if !summary.UpdatedAt.IsZero() {
		lines = append(lines, "Updated at: "+summary.UpdatedAt.UTC().Format(time.RFC3339))
	}
	if core := signalLandingSummaryEventCoreRefsLocal(summary); len(core) > 0 {
		lines = append(lines, "Event core: "+strings.Join(limitStrings(core, 8), ", "))
	}
	if halo := signalLandingSummaryHaloRefsLocal(summary); len(halo) > 0 {
		lines = append(lines, "Impact halo: "+strings.Join(limitStrings(halo, 10), ", "))
	}
	if len(summary.TopClaims) > 0 {
		lines = append(lines, "Claims: "+strings.Join(limitStrings(summary.TopClaims, 6), "; "))
	}
	if len(summary.TopMissingCandidates) > 0 {
		lines = append(lines, "Missing anchors: "+strings.Join(limitStrings(summary.TopMissingCandidates, 6), ", "))
	}
	return strings.Join(lines, "\n")
}

func signalLandingSummaryEventCoreRefsLocal(summary *horizonmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return uniqueStrings(append(append([]string{summary.ScopeRef}, summary.TopConfirmedRefs...), append(summary.TopInferredRefs, summary.AffectedZoneRefs...)...))
}

func signalLandingSummaryHaloRefsLocal(summary *horizonmodels.SignalLandingSummary) []string {
	if summary == nil {
		return nil
	}
	return uniqueStrings(append(summary.Products, append(append(summary.Routes, summary.Facilities...), summary.Chokepoints...)...))
}

func internalSignalFreshnessClass(updatedAt time.Time) string {
	if updatedAt.IsZero() {
		return "stale"
	}
	age := time.Since(updatedAt.UTC())
	switch {
	case age <= 2*time.Hour:
		return "flash"
	case age <= 24*time.Hour:
		return "hot"
	case age <= 7*24*time.Hour:
		return "warm"
	default:
		return "stale"
	}
}

func internalSignalSummaryBoost(summary *horizonmodels.SignalLandingSummary) float64 {
	if summary == nil {
		return 1
	}
	boost := 1.0
	switch internalSignalFreshnessClass(summary.UpdatedAt) {
	case "flash":
		boost += 0.45
	case "hot":
		boost += 0.3
	case "warm":
		boost += 0.15
	}
	boost += 0.03 * float64(len(summary.TopConfirmedRefs))
	boost += 0.02 * float64(len(summary.Products)+len(summary.Routes)+len(summary.Facilities)+len(summary.Chokepoints))
	return boost
}

func mergePreparedEvidenceDocs(base, extra []preparedEvidenceDoc, limit int) []preparedEvidenceDoc {
	if limit <= 0 {
		limit = defaultMaxEvidenceDocs
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]preparedEvidenceDoc, 0, minInt(limit, len(base)+len(extra)))
	appendDocs := func(items []preparedEvidenceDoc) {
		for _, item := range items {
			if len(out) >= limit {
				return
			}
			key := strings.TrimSpace(firstNonEmpty(item.ID, item.Title, item.URI))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	appendDocs(base)
	appendDocs(extra)
	return out
}

func buildAnalyticalShortcutAnswer(question string, hits []internalKnowledgeHit, docs []preparedEvidenceDoc) (string, bool) {
	if classifyInternalKnowledgeIntent(question) == "" {
		return "", false
	}
	var aggregateDoc preparedEvidenceDoc
	primitives := make([]preparedEvidenceDoc, 0, 4)
	for _, doc := range docs {
		switch strings.TrimSpace(doc.Kind) {
		case "analytical_aggregate":
			if aggregateDoc.ID == "" {
				aggregateDoc = doc
			}
		case "analytical_primitive":
			primitives = append(primitives, doc)
		}
	}
	if aggregateDoc.ID == "" {
		return "", false
	}
	lines := make([]string, 0, 8)
	if isMostlyCyrillic(question) {
		lines = append(lines, "Главные слабые места по прогретому аналитическому слою:")
	} else {
		lines = append(lines, "Main weak points from the warmed analytical layer:")
	}
	if explanation := extractAnalyticalField(aggregateDoc.Content, "Explanation:"); explanation != "" {
		lines = append(lines, "- "+explanation)
	}
	for _, doc := range primitives {
		if len(lines) >= 4 {
			break
		}
		label := strings.TrimSpace(doc.Title)
		explanation := extractAnalyticalField(doc.Content, "Explanation:")
		if explanation == "" {
			explanation = label
		} else if label != "" {
			explanation = label + ": " + explanation
		}
		if strings.TrimSpace(explanation) == "" {
			continue
		}
		lines = append(lines, "- "+strings.TrimSpace(explanation))
	}
	if paths := collectEvidenceCanonicalPaths(hits, docs, 8); len(paths) > 0 {
		if isMostlyCyrillic(question) {
			lines = append(lines, "Ключевые узлы: "+strings.Join(limitStrings(paths, 6), ", "))
		} else {
			lines = append(lines, "Key refs: "+strings.Join(limitStrings(paths, 6), ", "))
		}
	}
	return strings.Join(lines, "\n"), len(lines) > 1
}

func extractAnalyticalField(content, prefix string) string {
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func analyticalShortcutReady(report map[string]interface{}) bool {
	return asBool(report["analyticalShortcut"]) && strings.TrimSpace(asString(report["answer"])) != ""
}

func fallbackInternalAnswer(report map[string]interface{}) string {
	if answer := strings.TrimSpace(asString(report["answer"])); answer != "" {
		return answer
	}
	lines := make([]string, 0, 4)
	if summary := strings.TrimSpace(renderInternalHitSummary(report)); summary != "" && summary != "No internal hits." {
		lines = append(lines, "Top internal context:\n"+summary)
	}
	if paths := collectCanonicalPathsFromInternalReport(report); len(paths) > 0 {
		lines = append(lines, "Canonical paths:\n"+strings.Join(limitStrings(paths, 8), "\n"))
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

func renderCompactJSON(v interface{}) string {
	if v == nil {
		return "none"
	}
	return truncateString(fmt.Sprintf("%v", compactJSONValueFromAny(v)), 4000)
}

func extractCanonicalPathsFromText(value string) []string {
	re := regexp.MustCompile(`/[a-z0-9][a-z0-9/_-]*`)
	return uniqueStrings(re.FindAllString(strings.ToLower(value), -1))
}

func sliceMap(v interface{}) []map[string]interface{} {
	switch value := v.(type) {
	case []map[string]interface{}:
		return value
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			if m, ok := item.(map[string]interface{}); ok && m != nil {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func loadPreparedEvidenceDocs(ctx context.Context, gcsClient *gcp.GCSClient, hits []internalKnowledgeHit, limit int) ([]preparedEvidenceDoc, []string) {
	if gcsClient == nil || len(hits) == 0 || limit <= 0 {
		return nil, nil
	}
	out := make([]preparedEvidenceDoc, 0, minInt(limit, len(hits)))
	warnings := make([]string, 0, 4)
	for _, hit := range hits {
		if len(out) >= limit {
			break
		}
		bucket, object, ok := parseGCSURI(hit.URI)
		if !ok {
			continue
		}
		body, err := gcsClient.ReadObject(ctx, bucket, object)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("read evidence doc failed uri=%s: %v", hit.URI, err))
			continue
		}
		doc, err := decodePreparedEvidenceDoc(body)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("decode evidence doc failed uri=%s: %v", hit.URI, err))
			continue
		}
		doc.URI = firstNonEmpty(strings.TrimSpace(doc.URI), strings.TrimSpace(hit.URI))
		doc.ID = firstNonEmpty(strings.TrimSpace(doc.ID), strings.TrimSpace(hit.ID))
		doc.Kind = firstNonEmpty(strings.TrimSpace(doc.Kind), strings.TrimSpace(hit.Kind))
		doc.Title = firstNonEmpty(strings.TrimSpace(doc.Title), strings.TrimSpace(hit.Title), strings.TrimSpace(hit.ID))
		if len(doc.CanonicalPaths) == 0 {
			doc.CanonicalPaths = limitStrings(stringSlice(hit.StructData["canonicalPaths"]), 20)
		}
		out = append(out, doc)
	}
	return out, warnings
}

func decodePreparedEvidenceDoc(body []byte) (preparedEvidenceDoc, error) {
	var payload struct {
		ID         string         `json:"id"`
		StructData map[string]any `json:"structData"`
		Content    struct {
			RawBytes string `json:"rawBytes"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return preparedEvidenceDoc{}, err
	}
	contentBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.Content.RawBytes))
	if err != nil {
		return preparedEvidenceDoc{}, err
	}
	return preparedEvidenceDoc{
		ID:             strings.TrimSpace(payload.ID),
		Kind:           strings.TrimSpace(asString(payload.StructData["kind"])),
		Title:          strings.TrimSpace(asString(payload.StructData["title"])),
		Content:        strings.TrimSpace(string(contentBytes)),
		URI:            strings.TrimSpace(asString(payload.StructData["uri"])),
		CanonicalPaths: limitStrings(stringSlice(payload.StructData["canonicalPaths"]), 20),
	}, nil
}

func renderPreparedEvidenceDocs(docs []preparedEvidenceDoc) string {
	if len(docs) == 0 {
		return "No hydrated evidence documents were available."
	}
	var b strings.Builder
	for i, doc := range docs {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&b, "[Doc %d] kind=%s title=%s id=%s\n", i+1, firstNonEmpty(doc.Kind, "unknown"), firstNonEmpty(doc.Title, doc.ID), doc.ID)
		if len(doc.CanonicalPaths) > 0 {
			fmt.Fprintf(&b, "refs=%s\n", strings.Join(doc.CanonicalPaths, ", "))
		}
		if doc.URI != "" {
			fmt.Fprintf(&b, "uri=%s\n", doc.URI)
		}
		fmt.Fprintf(&b, "%s\n\n", doc.Content)
	}
	return strings.TrimSpace(b.String())
}

func buildPreparedQueryVariants(question, corpusID string) []string {
	base := uniqueStrings([]string{
		question,
		strings.Join(textTokens(question), " "),
		strings.TrimSpace(strings.NewReplacer("?", "", ",", "", ".", "", ":", "").Replace(question)),
	})
	base = append(base, corpusQueryVariants(corpusID)...)
	joined := strings.ToLower(strings.Join(base, " "))
	if strings.Contains(joined, "supply") || strings.Contains(joined, "logistics") || strings.Contains(joined, "route") {
		base = append(base, "products routes suppliers customers transport via")
	}
	if intent := classifyInternalKnowledgeIntent(question); intent == "current_signal" || intent == "scenario" {
		base = append(base, "current disruptions fresh signal event impact affected routes products chokepoints")
	}
	return uniqueStrings(base)
}

func corpusQueryVariants(corpusID string) []string {
	corpusID = strings.TrimSpace(strings.ToLower(corpusID))
	if corpusID == "" {
		return nil
	}
	parts := strings.Split(corpusID, "-")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(part) == 10 && isHexLike(part) {
			continue
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		return nil
	}
	out := []string{strings.Join(cleaned, " ")}
	if len(cleaned) > 2 {
		out = append(out, strings.Join(cleaned[:minInt(4, len(cleaned))], " "))
	}
	return uniqueStrings(out)
}

func rerankPreparedInternalKnowledgeHits(question string, hits []internalKnowledgeHit, limit int) []internalKnowledgeHit {
	if len(hits) == 0 {
		return nil
	}
	queryTokens := normalizedTokenSet(question)
	logisticsQuery := strings.Contains(strings.ToLower(question), "supply") || strings.Contains(strings.ToLower(question), "logistics") || strings.Contains(strings.ToLower(question), "route")
	currentSignalQuery := classifyInternalKnowledgeIntent(question) == "current_signal"
	signalContext := buildPreparedSignalContext(hits)
	type scored struct {
		hit   internalKnowledgeHit
		score float64
	}
	scoredHits := make([]scored, 0, len(hits))
	for _, hit := range hits {
		score := hit.Score * 100
		kind := strings.TrimSpace(asString(hit.StructData["kind"]))
		pathTokens := normalizedTokenSet(strings.Join(stringSlice(hit.StructData["canonicalPaths"]), " "))
		score += preparedKindWeight(kind, logisticsQuery)
		score += 4.0 * tokenOverlapScore(queryTokens, normalizedTokenSet(firstNonEmpty(hit.Title, asString(hit.StructData["title"]))))
		score += 2.0 * tokenOverlapScore(queryTokens, pathTokens)
		score += 1.5 * tokenOverlapScore(queryTokens, normalizedTokenSet(strings.Join(stringSlice(hit.StructData["questionHints"]), " ")))
		if currentSignalQuery {
			eventCoreOverlap := tokenOverlapScore(signalContext.eventCoreTokens, pathTokens)
			haloOverlap := tokenOverlapScore(signalContext.haloTokens, pathTokens)
			score += preparedSignalRecencyScore(hit.StructData["updatedAt"])
			score += 5.5 * eventCoreOverlap
			score += 3.25 * haloOverlap
			switch kind {
			case "signal_landing_summary":
				score += 42
			case "analytical_aggregate", "analytical_primitive":
				score += 20
			case "overview", "dossier":
				if eventCoreOverlap == 0 && haloOverlap == 0 {
					score -= 18
				}
			}
		}
		scoredHits = append(scoredHits, scored{hit: hit, score: score})
	}
	sort.Slice(scoredHits, func(i, j int) bool {
		if scoredHits[i].score == scoredHits[j].score {
			return scoredHits[i].hit.ID < scoredHits[j].hit.ID
		}
		return scoredHits[i].score > scoredHits[j].score
	})
	out := make([]internalKnowledgeHit, 0, minInt(limit, len(scoredHits)))
	for _, item := range scoredHits {
		out = append(out, item.hit)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func preparedKindWeight(kind string, logisticsQuery bool) float64 {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "signal_landing_summary":
		if logisticsQuery {
			return 28
		}
		return 34
	case "corridor":
		if logisticsQuery {
			return 24
		}
		return 10
	case "route_artifact":
		if logisticsQuery {
			return 22
		}
		return 9
	case "dossier":
		return 18
	case "overview":
		return 16
	case "products":
		return 14
	case "relation":
		return 12
	case "object":
		return 11
	case "gaps":
		return 8
	default:
		return 0
	}
}

type preparedSignalContext struct {
	eventCoreTokens map[string]struct{}
	haloTokens      map[string]struct{}
}

func buildPreparedSignalContext(hits []internalKnowledgeHit) preparedSignalContext {
	ctx := preparedSignalContext{
		eventCoreTokens: map[string]struct{}{},
		haloTokens:      map[string]struct{}{},
	}
	for _, hit := range hits {
		if strings.TrimSpace(hit.Kind) != "signal_landing_summary" {
			continue
		}
		addNormalizedTokens(ctx.eventCoreTokens, strings.Join(stringSlice(hit.StructData["eventCoreRefs"]), " "))
		addNormalizedTokens(ctx.eventCoreTokens, strings.Join(stringSlice(hit.StructData["canonicalPaths"]), " "))
		addNormalizedTokens(ctx.haloTokens, strings.Join(stringSlice(hit.StructData["haloRefs"]), " "))
	}
	return ctx
}

func preparedSignalRecencyScore(value interface{}) float64 {
	raw := strings.TrimSpace(asString(value))
	if raw == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return 0
	}
	age := time.Since(parsed.UTC())
	switch {
	case age <= 2*time.Hour:
		return 26
	case age <= 24*time.Hour:
		return 18
	case age <= 7*24*time.Hour:
		return 8
	default:
		return 0
	}
}

func addNormalizedTokens(dst map[string]struct{}, value string) {
	for token := range normalizedTokenSet(value) {
		dst[token] = struct{}{}
	}
}

func mergeInternalKnowledgeHits(base, add []internalKnowledgeHit, limit int) []internalKnowledgeHit {
	index := make(map[string]int, len(base))
	out := append([]internalKnowledgeHit(nil), base...)
	for i, hit := range out {
		index[internalKnowledgeHitKey(hit)] = i
	}
	for _, hit := range add {
		key := internalKnowledgeHitKey(hit)
		if pos, ok := index[key]; ok {
			if hit.Score > out[pos].Score {
				out[pos] = hit
			}
			continue
		}
		index[key] = len(out)
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func internalKnowledgeHitKey(hit internalKnowledgeHit) string {
	return firstNonEmpty(strings.TrimSpace(hit.ID), strings.TrimSpace(hit.URI), strings.TrimSpace(hit.Title))
}

func textTokens(value string) []string {
	fields := strings.Fields(strings.ToLower(strings.NewReplacer(",", " ", ".", " ", ":", " ", ";", " ", "(", " ", ")", " ", "/", " ", "-", " ", "?", " ").Replace(value)))
	return uniqueStrings(fields)
}

func normalizedTokenSet(value string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, token := range textTokens(value) {
		if len(token) < 3 {
			continue
		}
		set[token] = struct{}{}
	}
	return set
}

func tokenOverlapScore(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for token := range a {
		if _, ok := b[token]; ok {
			common++
		}
	}
	return float64(common)
}

func isHexLike(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func vertexSearchDocID(res ai.SearchResult) string {
	return firstNonEmpty(strings.TrimSpace(res.Document.ID), strings.TrimSpace(res.ID), strings.TrimSpace(asString(res.Document.StructData["id"])))
}

func vertexSearchScore(res ai.SearchResult) float64 {
	if res.RelevanceScore > 0 {
		return res.RelevanceScore
	}
	if res.RankSignals != nil {
		return res.RankSignals.SemanticSimilarityScore
	}
	return 0
}

func parseGCSURI(uri string) (bucket string, object string, ok bool) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(uri, "gs://") {
		return "", "", false
	}
	rest := strings.TrimPrefix(uri, "gs://")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func cloneStringAnyMap(src map[string]interface{}) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func stringSlice(v interface{}) []string {
	switch value := v.(type) {
	case []string:
		return uniqueStrings(value)
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" {
				out = append(out, s)
			}
		}
		return uniqueStrings(out)
	default:
		return nil
	}
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func asInt(v interface{}) int {
	switch value := v.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func viewConfigMapValue(viewConfig map[string]interface{}, key string) map[string]interface{} {
	if viewConfig == nil {
		return map[string]interface{}{}
	}
	raw, ok := viewConfig[key]
	if !ok || raw == nil {
		return map[string]interface{}{}
	}
	switch value := raw.(type) {
	case map[string]interface{}:
		return value
	default:
		return map[string]interface{}{"value": value}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func normalizeTypeHint(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "org", "organization", "company":
		return string(horizonmodels.FragmentTypeOrganization)
	case "product":
		return string(horizonmodels.FragmentTypeProduct)
	case "facility", "fac":
		return string(horizonmodels.FragmentTypeFacility)
	case "gov", "government":
		return string(horizonmodels.FragmentTypeOrganization)
	case "deposit":
		return string(horizonmodels.FragmentTypeDeposit)
	default:
		return ""
	}
}

func looksLikeFirestoreID(v string) bool {
	v = strings.TrimSpace(v)
	return len(v) >= 8 && !strings.Contains(v, "/")
}

func fragmentDisplayName(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.Labels != nil {
		if label := strings.TrimSpace(fragment.Labels["en"]); label != "" {
			return label
		}
	}
	if fragment.Identifiers != nil {
		if shortName := strings.TrimSpace(fragment.Identifiers["shortName"]); shortName != "" {
			return shortName
		}
		if legalName := strings.TrimSpace(fragment.Identifiers["legalName"]); legalName != "" {
			return legalName
		}
	}
	return strings.TrimSpace(fragment.CanonicalPath)
}

func stringSliceContains(items []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, item := range items {
		if strings.TrimSpace(item) == needle {
			return true
		}
	}
	return false
}

func countNonNilLensArtifacts(items []*horizonmodels.LensArtifact) int {
	count := 0
	for _, item := range items {
		if item != nil {
			count++
		}
	}
	return count
}

func countNonNilMaterialized(items []*horizonmodels.MaterializedArtifact) int {
	count := 0
	for _, item := range items {
		if item != nil {
			count++
		}
	}
	return count
}

func marshalToolResult(v interface{}) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func compactJSONValue(raw json.RawMessage) interface{} {
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]interface{}{"raw": truncateString(string(raw), 4000)}
	}
	return compactJSONValueFromAny(decoded)
}

func compactJSONValueFromAny(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{"raw": truncateString(fmt.Sprintf("%v", v), 4000)}
	}
	if len(b) <= 16000 {
		var out interface{}
		if err := json.Unmarshal(b, &out); err == nil {
			return out
		}
	}
	return map[string]interface{}{"raw": truncateString(string(b), 4000)}
}

func limitStrings(items []string, limit int) []string {
	items = uniqueStrings(items)
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return append([]string(nil), items[:limit]...)
}

func uniqueStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
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
	return out
}

func truncateString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "...(truncated)"
}

func writeJSON(path string, value interface{}) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatalf("marshal %s failed: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatalf("write %s failed: %v", path, err)
	}
}

func printJSON(value interface{}) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatalf("marshal output failed: %v", err)
	}
	fmt.Println(string(data))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sanitizeSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(value)
	if strings.Contains(upper, "PLACEHOLDER") || strings.Contains(upper, "REPLACE_ME") {
		return ""
	}
	return value
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func asBool(v interface{}) bool {
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		typed = strings.TrimSpace(strings.ToLower(typed))
		return typed == "true" || typed == "1" || typed == "yes"
	default:
		return false
	}
}

func isMostlyCyrillic(value string) bool {
	cyrillic := 0
	total := 0
	for _, r := range value {
		if r == ' ' || r == '\n' || r == '\t' {
			continue
		}
		total++
		if r >= 0x0400 && r <= 0x04FF {
			cyrillic++
		}
	}
	return total > 0 && cyrillic*2 >= total
}
