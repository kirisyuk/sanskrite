package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/horizon"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	corehttp "github.com/igorkirisyuk/sanskrite/internal/http"
	horizonhttp "github.com/igorkirisyuk/sanskrite/internal/http/horizon"
	realtime "github.com/igorkirisyuk/sanskrite/internal/realtime"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const (
	defaultSynthesisModel   = "gemini_balanced"
	defaultReviewModel      = "gemini_balanced"
	defaultHarvestTimeout   = 360
	defaultHarvestMaterials = 5
)

type toolConfig struct {
	projectID            string
	tenantID             string
	question             string
	out                  string
	synthesisModel       string
	reviewModel          string
	skipReview           bool
	augmentHarvest       bool
	harvestCanonicalPath string
	harvestBucket        string
	harvestMaxMaterials  int
	harvestTimeoutSec    int
}

type toolRuntime struct {
	projectID string
	firestore *gcp.FirestoreClient
	gcs       *gcp.GCSClient
	logger    *log.Logger
	modelMgr  *ai.AIModelManager
	executor  *ai.AITaskExecutor
	genAI     *corehttp.GenAIHandler
	handlers  *horizonhttp.HorizonHandlers
}

type homeWorkspaceEntity struct {
	ID            string   `json:"id,omitempty"`
	Name          string   `json:"name"`
	CanonicalPath string   `json:"canonicalPath"`
	Type          string   `json:"type,omitempty"`
	Description   string   `json:"description,omitempty"`
	Location      string   `json:"location,omitempty"`
	Industry      string   `json:"industry,omitempty"`
	RelatedPaths  []string `json:"relatedPaths,omitempty"`
	Selected      bool     `json:"selected"`
}

type homeWorkspaceRunResponse struct {
	RunID              string                `json:"runId"`
	Query              string                `json:"query"`
	Answer             string                `json:"answer"`
	Hits               []map[string]any      `json:"hits,omitempty"`
	HighlightedPaths   []string              `json:"highlightedPaths,omitempty"`
	EventCorePaths     []string              `json:"eventCorePaths,omitempty"`
	HaloPaths          []string              `json:"haloPaths,omitempty"`
	AffectedZoneRefs   []string              `json:"affectedZoneRefs,omitempty"`
	Entities           []homeWorkspaceEntity `json:"entities,omitempty"`
	SuggestedLensName  string                `json:"suggestedLensName,omitempty"`
	SuggestedLensScope []string              `json:"suggestedLensScope,omitempty"`
	Warnings           []string              `json:"warnings,omitempty"`
}

type harvestRunRecord struct {
	RunID               string           `json:"run_id"`
	EntityName          string           `json:"entity_name"`
	EntityCanonicalPath string           `json:"entity_canonical_path"`
	Bucket              string           `json:"bucket"`
	Prefix              string           `json:"prefix"`
	FinalReportGCSURI   string           `json:"final_report_gcs_uri"`
	UsableMaterialCount int              `json:"usable_material_count"`
	Materials           []map[string]any `json:"materials"`
	Enrichment          map[string]any   `json:"enrichment"`
}

type executiveBriefCandidate struct {
	Headline           string   `json:"headline"`
	Brief              string   `json:"brief"`
	WhyItMatters       []string `json:"whyItMatters"`
	WhatNeedsAttention []string `json:"whatNeedsAttention"`
	ConfidenceNote     string   `json:"confidenceNote"`
	EvidenceAnchors    []string `json:"evidenceAnchors"`
}

type deeperCandidate struct {
	Title             string   `json:"title"`
	Analysis          string   `json:"analysis"`
	KeyDrivers        []string `json:"keyDrivers"`
	ImportantPaths    []string `json:"importantPaths"`
	HypothesesToCheck []string `json:"hypothesesToCheck"`
	OpenQuestions     []string `json:"openQuestions"`
	RefineTheRequest  []string `json:"refineTheRequest"`
	EvidenceAnchors   []string `json:"evidenceAnchors"`
}

type dualLayerOutput struct {
	ExecutiveBrief executiveBriefCandidate `json:"executiveBrief"`
	Deeper         deeperCandidate         `json:"deeper"`
}

type dualLayerReview struct {
	OverallVerdict      string   `json:"overallVerdict"`
	ExecutiveBriefNotes []string `json:"executiveBriefNotes"`
	DeeperNotes         []string `json:"deeperNotes"`
	RevisionPriorities  []string `json:"revisionPriorities"`
}

type dualLayerReport struct {
	ProjectID              string                   `json:"projectId"`
	TenantID               string                   `json:"tenantId"`
	Question               string                   `json:"question"`
	SynthesisModel         string                   `json:"synthesisModel"`
	ReviewModel            string                   `json:"reviewModel,omitempty"`
	StartedAt              string                   `json:"startedAt"`
	FinishedAt             string                   `json:"finishedAt"`
	DurationMs             int64                    `json:"durationMs"`
	HomeDurationMs         int64                    `json:"homeDurationMs"`
	HarvestDurationMs      int64                    `json:"harvestDurationMs,omitempty"`
	SynthesisDurationMs    int64                    `json:"synthesisDurationMs"`
	ReviewDurationMs       int64                    `json:"reviewDurationMs,omitempty"`
	RevisionDurationMs     int64                    `json:"revisionDurationMs,omitempty"`
	UsedHarvest            bool                     `json:"usedHarvest"`
	HarvestTarget          string                   `json:"harvestTarget,omitempty"`
	Home                   homeWorkspaceRunResponse `json:"home"`
	Harvest                *harvestRunRecord        `json:"harvest,omitempty"`
	Draft                  dualLayerOutput          `json:"draft"`
	Review                 *dualLayerReview         `json:"review,omitempty"`
	Result                 dualLayerOutput          `json:"result"`
	ExecutiveBriefMarkdown string                   `json:"executiveBriefMarkdown"`
	DeeperMarkdown         string                   `json:"deeperMarkdown"`
	Warnings               []string                 `json:"warnings,omitempty"`
	Error                  string                   `json:"error,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg := parseFlags()
	if strings.TrimSpace(cfg.projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(cfg.question) == "" {
		log.Fatal("--question is required")
	}

	startedAt := time.Now()
	report := dualLayerReport{
		ProjectID:      cfg.projectID,
		TenantID:       firstNonEmpty(cfg.tenantID, "global"),
		Question:       strings.TrimSpace(cfg.question),
		SynthesisModel: cfg.synthesisModel,
		ReviewModel:    cfg.reviewModel,
		StartedAt:      startedAt.UTC().Format(time.RFC3339),
	}

	rt, err := newToolRuntime(ctx, cfg.projectID)
	if err != nil {
		report.Error = err.Error()
		report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		report.DurationMs = time.Since(startedAt).Milliseconds()
		finalizeReport(cfg.out, report)
		os.Exit(1)
	}
	defer rt.Close()

	homeStarted := time.Now()
	homeResp, err := rt.runHomeWorkspace(ctx, cfg.tenantID, cfg.question)
	report.HomeDurationMs = time.Since(homeStarted).Milliseconds()
	report.Home = homeResp
	if err != nil {
		report.Error = err.Error()
		report.Warnings = append(report.Warnings, "home workspace run failed")
		report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		report.DurationMs = time.Since(startedAt).Milliseconds()
		finalizeReport(cfg.out, report)
		os.Exit(1)
	}

	var harvestRecord *harvestRunRecord
	focalRef := strings.TrimSpace(cfg.harvestCanonicalPath)
	if cfg.augmentHarvest {
		harvestTarget := strings.TrimSpace(cfg.harvestCanonicalPath)
		if harvestTarget == "" {
			harvestTarget = pickHarvestTarget(homeResp)
		}
		if harvestTarget == "" {
			report.Warnings = append(report.Warnings, "harvest requested but no clear canonical path was available")
		} else {
			report.HarvestTarget = harvestTarget
			focalRef = harvestTarget
			harvestStarted := time.Now()
			record, harvestWarnings, harvestErr := runHarvestAugmentation(ctx, cfg, harvestTarget)
			report.HarvestDurationMs = time.Since(harvestStarted).Milliseconds()
			report.Warnings = append(report.Warnings, harvestWarnings...)
			if harvestErr != nil {
				report.Warnings = append(report.Warnings, "harvest augmentation failed: "+harvestErr.Error())
			} else {
				harvestRecord = record
				report.Harvest = record
				report.UsedHarvest = true
			}
		}
	}

	synthStarted := time.Now()
	if focalRef == "" {
		focalRef = pickHarvestTarget(homeResp)
	}
	draft := buildLogicalDualLayer(cfg.question, homeResp, harvestRecord, focalRef)
	report.SynthesisDurationMs = time.Since(synthStarted).Milliseconds()
	report.Draft = draft

	finalOutput := draft

	report.Result = finalOutput
	report.ExecutiveBriefMarkdown = renderExecutiveBrief(finalOutput.ExecutiveBrief)
	report.DeeperMarkdown = renderDeeper(finalOutput.Deeper)
	report.Warnings = uniqueStrings(report.Warnings)
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	report.DurationMs = time.Since(startedAt).Milliseconds()
	finalizeReport(cfg.out, report)
}

func parseFlags() toolConfig {
	projectDefault := strings.TrimSpace(firstNonEmpty(
		os.Getenv("GCP_PROJECT_ID"),
		os.Getenv("GOOGLE_CLOUD_PROJECT"),
		"nv-horizon",
	))
	var cfg toolConfig
	flag.StringVar(&cfg.projectID, "project", projectDefault, "GCP project id")
	flag.StringVar(&cfg.tenantID, "tenant", "global", "tenant id")
	flag.StringVar(&cfg.question, "question", "", "home workspace question to validate")
	flag.StringVar(&cfg.out, "out", "", "optional path to save JSON report")
	flag.StringVar(&cfg.synthesisModel, "model", defaultSynthesisModel, "model used for dual-layer synthesis")
	flag.StringVar(&cfg.reviewModel, "review-model", defaultReviewModel, "model used for dual-layer critique")
	flag.BoolVar(&cfg.skipReview, "skip-review", false, "skip critique and revision loop")
	flag.BoolVar(&cfg.augmentHarvest, "augment-harvest", false, "optionally run external Vertex harvest augmentation for the primary entity")
	flag.StringVar(&cfg.harvestCanonicalPath, "harvest-canonical-path", "", "explicit canonical path for harvest augmentation")
	flag.StringVar(&cfg.harvestBucket, "harvest-bucket", "", "optional bucket name for harvest artifacts")
	flag.IntVar(&cfg.harvestMaxMaterials, "harvest-max-materials", defaultHarvestMaterials, "max materials to keep in harvest run")
	flag.IntVar(&cfg.harvestTimeoutSec, "harvest-timeout-sec", defaultHarvestTimeout, "timeout for harvest augmentation in seconds")
	flag.Parse()
	return cfg
}

func newToolRuntime(ctx context.Context, projectID string) (*toolRuntime, error) {
	projectID = strings.TrimSpace(projectID)
	logger := log.New(os.Stdout, "[horizon_home_dual_layer_validate] ", log.LstdFlags)
	if os.Getenv("GOOGLE_CLOUD_PROJECT") == "" {
		_ = os.Setenv("GOOGLE_CLOUD_PROJECT", projectID)
	}
	if os.Getenv("GCP_PROJECT_ID") == "" {
		_ = os.Setenv("GCP_PROJECT_ID", projectID)
	}
	if os.Getenv("VERTEX_LOCATION") == "" {
		_ = os.Setenv("VERTEX_LOCATION", "us-central1")
	}

	fsClient, err := gcp.NewFirestoreClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("firestore init failed: %w", err)
	}
	gcsClient, gcsErr := gcp.NewGCSClient(ctx, projectID)
	if gcsErr != nil {
		logger.Printf("GCS init skipped: %v", gcsErr)
	}

	modelManager := ai.NewAIModelManager()
	aiServices := buildAIServices(ctx, projectID, modelManager)
	if len(aiServices) == 0 {
		_ = fsClient.Close()
		return nil, fmt.Errorf("no AI services configured")
	}
	executor := ai.NewAITaskExecutor(aiServices, modelManager).
		WithCircuitBreaker(ai.NewCircuitBreaker()).
		WithAdmissionController(ai.NoopAdmissionController())

	searchLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_SEARCH_LOCATION"), "global"))
	vertexDataStore := strings.TrimSpace(firstNonEmpty(
		os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_INTERNAL_KNOWLEDGE_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"),
		"rag-documents-store",
	))
	vertexSearch := ai.NewVertexAISearchClient(projectID, searchLocation, vertexDataStore, "")

	db := fsClient.Client()
	agentJobsRepo := corerepo.NewAgentJobsRepository(db)
	dataSourcesRepo := corerepo.NewDataSourcesRepository(db)
	docsRepo := corerepo.NewDocumentsRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)
	framesRepo := horizonrepo.NewFramesRepository(db)
	lensesRepo := horizonrepo.NewLensesRepository(db)
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	watchdogRepo := horizonrepo.NewWatchdogConfigsRepository(db)
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	fragmentVersionsRepo := horizonrepo.NewHorizonFragmentVersionsRepository(db)
	materializedArtifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(db)
	analyticalPrimitivesRepo := horizonrepo.NewAnalyticalPrimitivesRepository(db)
	analyticalAggregatesRepo := horizonrepo.NewAnalyticalAggregatesRepository(db)
	signalLandingSummariesRepo := horizonrepo.NewSignalLandingSummariesRepository(db)

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

	genAI := corehttp.NewGenAIHandler().
		WithAIExecutor(executor).
		WithModelManager(modelManager)
	handlers := horizonhttp.NewHorizonHandlers(horizonService).
		WithGenAIHandler(genAI).
		WithGCSClient(gcsClient).
		WithMaterializedArtifactsRepo(materializedArtifactsRepo).
		WithAnalyticalPrimitivesRepo(analyticalPrimitivesRepo).
		WithAnalyticalAggregatesRepo(analyticalAggregatesRepo).
		WithSignalLandingSummariesRepo(signalLandingSummariesRepo)

	return &toolRuntime{
		projectID: projectID,
		firestore: fsClient,
		gcs:       gcsClient,
		logger:    logger,
		modelMgr:  modelManager,
		executor:  executor,
		genAI:     genAI,
		handlers:  handlers,
	}, nil
}

func buildAIServices(ctx context.Context, projectID string, modelManager *ai.AIModelManager) []ai.AIService {
	services := make([]ai.AIService, 0, 4)
	vertexLocation := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
	vertexService, err := ai.NewVertexAIService(projectID, vertexLocation, modelManager)
	if err == nil {
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
		if os.Getenv("GEMINI_API_KEY") == "" {
			_ = os.Setenv("GEMINI_API_KEY", geminiAPIKey)
		}
		services = append(services, ai.NewGeminiService(geminiAPIKey, modelManager))
	}

	openAIKey := sanitizeSecret(strings.TrimSpace(gcp.GetEnvOrSecret(ctx, projectID, "OPENAI_API_KEY", "openai_api_key")))
	if openAIKey != "" {
		if os.Getenv("OPENAI_API_KEY") == "" {
			_ = os.Setenv("OPENAI_API_KEY", openAIKey)
		}
		services = append(services, ai.NewOpenAIService(openAIKey, modelManager))
	}
	return services
}

func (r *toolRuntime) Close() error {
	if r == nil || r.firestore == nil {
		return nil
	}
	return r.firestore.Close()
}

func (r *toolRuntime) runHomeWorkspace(ctx context.Context, tenantID, question string) (homeWorkspaceRunResponse, error) {
	auth := &corehttp.AuthInfo{
		UID:      "codex-home-dual-layer",
		Email:    "codex-home-dual-layer@" + firstNonEmpty(tenantID, "global"),
		TenantID: firstNonEmpty(tenantID, "global"),
	}
	req := r.newAuthedJSONRequest(ctx, auth, http.MethodPost, "/api/horizon/home/workspace/run", map[string]any{
		"query": strings.TrimSpace(question),
	})
	var out homeWorkspaceRunResponse
	if err := r.invokeJSON(r.handlers.HomeWorkspaceRun, req, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (r *toolRuntime) generateDualLayerDraft(ctx context.Context, model, question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord) (dualLayerOutput, error) {
	prompt := buildDualLayerDraftPrompt(question, home, harvest)
	return r.generateStructuredDualLayer(ctx, model, prompt, dualLayerSystemInstruction(), home)
}

func (r *toolRuntime) reviewDualLayer(ctx context.Context, model, question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord, draft dualLayerOutput) (*dualLayerReview, error) {
	prompt := buildDualLayerReviewPrompt(question, home, harvest, draft)
	resp, err := r.genAI.GenerateDirect(ctx, gen_combined.GenAIRequest{
		Model:             model,
		Prompt:            prompt,
		SystemInstruction: dualLayerReviewInstruction(),
		Temperature:       0.1,
		MaxOutputTokens:   2200,
		StrictJSONEnabled: true,
		JSONSchema:        dualLayerReviewSchema(),
	})
	if err != nil {
		return nil, err
	}
	var out dualLayerReview
	if err := decodeJSONContent(resp.Content, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *toolRuntime) reviseDualLayer(ctx context.Context, model, question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord, draft dualLayerOutput, review dualLayerReview) (dualLayerOutput, error) {
	prompt := buildDualLayerRevisionPrompt(question, home, harvest, draft, review)
	return r.generateStructuredDualLayer(ctx, model, prompt, dualLayerSystemInstruction(), home)
}

func (r *toolRuntime) generateStructuredDualLayer(ctx context.Context, model, prompt, systemInstruction string, home homeWorkspaceRunResponse) (dualLayerOutput, error) {
	resp, err := r.genAI.GenerateDirect(ctx, gen_combined.GenAIRequest{
		Model:             model,
		Prompt:            prompt,
		SystemInstruction: systemInstruction,
		Temperature:       0.2,
		MaxOutputTokens:   4000,
		StrictJSONEnabled: true,
		JSONSchema:        dualLayerOutputSchema(),
	})
	if err != nil {
		return dualLayerOutput{}, err
	}
	var out dualLayerOutput
	if err := decodeJSONContent(resp.Content, &out); err != nil {
		return dualLayerOutput{}, err
	}
	return normalizeDualLayer(out, home), nil
}

func runHarvestAugmentation(ctx context.Context, cfg toolConfig, canonicalPath string) (*harvestRunRecord, []string, error) {
	repoRoot := "/Users/kirisyuk/cursor/sanskrite"
	timeout := time.Duration(maxInt(cfg.harvestTimeoutSec, 1)) * time.Second
	harvestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"agents/vertex/harvest_portfolio_agent/runner.py",
		"--project", cfg.projectID,
		"--tenant", firstNonEmpty(cfg.tenantID, "global"),
		"--canonical-path", canonicalPath,
		"--max-materials", strconv.Itoa(maxInt(cfg.harvestMaxMaterials, 1)),
	}
	if strings.TrimSpace(cfg.harvestBucket) != "" {
		args = append(args, "--bucket", strings.TrimSpace(cfg.harvestBucket))
	}
	cmd := exec.CommandContext(harvestCtx, harvestPythonBinary(repoRoot), args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(repoRoot, "agents", "vertex"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("harvest runner failed: %w; output=%s", err, truncateString(string(output), 3000))
	}
	payload, parseErr := extractTrailingJSONObject(output)
	if parseErr != nil {
		return nil, nil, fmt.Errorf("harvest output parse failed: %w", parseErr)
	}
	var record harvestRunRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, nil, fmt.Errorf("harvest output decode failed: %w", err)
	}
	warnings := make([]string, 0, 2)
	if record.UsableMaterialCount == 0 {
		warnings = append(warnings, "harvest completed but did not fetch usable materials")
	}
	return &record, warnings, nil
}

func harvestPythonBinary(repoRoot string) string {
	venvPython := filepath.Join(repoRoot, "agents", "vertex", "harvest_portfolio_agent", ".venv", "bin", "python3")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython
	}
	return "python3"
}

func buildLogicalDualLayer(question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord, focalRef string) dualLayerOutput {
	briefText := strings.TrimSpace(compactHomeAnswer(home.Answer, 900))
	if briefText == "" {
		briefText = strings.TrimSpace(home.Answer)
	}
	whyItMatters := extractStructuredAspects(home.Answer, 4)
	if len(whyItMatters) == 0 {
		whyItMatters = fallbackWhyItMatters(home)
	}
	attention := extractAttentionItems(home.Answer, 4)
	if len(attention) == 0 {
		attention = fallbackNeedsAttention(home)
	}
	keyDrivers := extractStructuredAspects(home.Answer, 5)
	if len(keyDrivers) == 0 {
		keyDrivers = fallbackWhyItMatters(home)
	}
	analysis := strings.TrimSpace(home.Answer)
	if analysis == "" {
		analysis = fallbackDeeperAnalysis(home)
	}
	importantPaths := logicalImportantPaths(home)
	openQuestions := logicalOpenQuestions(home)
	refinePrompts := defaultRefinePrompts(home, focalRef)
	evidenceAnchors := fallbackEvidenceAnchors(home, harvest)
	harvestJudgments := harvestKeyJudgments(harvest, 4)
	harvestEntities := harvestRelatedEntities(harvest, 4)
	harvestMaterials := harvestMaterialTitles(harvest, 4)
	harvestGaps := harvestGapSummaries(harvest, 3)
	if len(harvestJudgments) > 0 {
		keyDrivers = append(keyDrivers, harvestJudgments...)
	}
	if len(harvestEntities) > 0 {
		importantPaths = append(importantPaths, harvestEntities...)
	}
	if len(harvestMaterials) > 0 {
		evidenceAnchors = append(evidenceAnchors, harvestMaterials...)
	}
	if len(harvestGaps) > 0 {
		openQuestions = append(openQuestions, harvestGaps...)
	}
	analysis = mergeResearchIntoAnalysis(analysis, harvest)
	briefText = mergeResearchIntoBrief(briefText, harvest)

	return dualLayerOutput{
		ExecutiveBrief: executiveBriefCandidate{
			Headline:           fallbackHeadline(home, focalRef),
			Brief:              briefText,
			WhyItMatters:       humanizeBulletList(whyItMatters, 4),
			WhatNeedsAttention: humanizeBulletList(attention, 4),
			ConfidenceNote:     fallbackConfidenceNote(home, harvest),
			EvidenceAnchors:    humanizeBulletList(evidenceAnchors, 5),
		},
		Deeper: deeperCandidate{
			Title:             fallbackDeeperTitle(home, focalRef),
			Analysis:          analysis,
			KeyDrivers:        humanizeBulletList(keyDrivers, 5),
			ImportantPaths:    humanizeBulletList(importantPaths, 8),
			HypothesesToCheck: humanizeBulletList(harvestHypotheses(harvest), 4),
			OpenQuestions:     humanizeBulletList(openQuestions, 4),
			RefineTheRequest:  humanizeBulletList(refinePrompts, 4),
			EvidenceAnchors:   humanizeBulletList(evidenceAnchors, 6),
		},
	}
}

func mergeResearchIntoBrief(brief string, harvest *harvestRunRecord) string {
	brief = strings.TrimSpace(brief)
	if harvest == nil || harvest.Enrichment == nil {
		return brief
	}
	external := strings.TrimSpace(compactStringMapValue(harvest.Enrichment, "executive_brief"))
	if external == "" {
		return brief
	}
	if brief == "" {
		return external
	}
	if len(brief) >= 220 {
		return brief
	}
	if isMostlyCyrillic(brief) {
		return brief + "\n\nNice-depth research добавляет внешний контекст: " + external
	}
	return brief + "\n\nNice-depth research adds external context: " + external
}

func mergeResearchIntoAnalysis(analysis string, harvest *harvestRunRecord) string {
	analysis = strings.TrimSpace(analysis)
	if harvest == nil || harvest.Enrichment == nil {
		return analysis
	}
	judgments := harvestKeyJudgments(harvest, 3)
	if len(judgments) == 0 {
		return analysis
	}
	var extra string
	if isMostlyCyrillic(analysis) {
		extra = "Nice-depth research усиливает картину следующими внешними выводами: " + strings.Join(judgments, "; ") + "."
	} else {
		extra = "Nice-depth research adds external findings: " + strings.Join(judgments, "; ") + "."
	}
	if analysis == "" {
		return extra
	}
	return analysis + "\n\n" + extra
}

func pickHarvestTarget(home homeWorkspaceRunResponse) string {
	if len(home.Entities) == 0 {
		return ""
	}
	for _, entity := range home.Entities {
		if strings.TrimSpace(entity.CanonicalPath) != "" && entity.Selected {
			return strings.TrimSpace(entity.CanonicalPath)
		}
	}
	return strings.TrimSpace(home.Entities[0].CanonicalPath)
}

func extractStructuredAspects(answer string, limit int) []string {
	lines := strings.Split(answer, "\n")
	out := make([]string, 0, limit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) >= 2 && line[0] >= '1' && line[0] <= '9' && strings.HasPrefix(line[1:], ".") {
			item := stripMarkdownMarkers(strings.TrimSpace(line[2:]))
			if idx := strings.Index(item, ":"); idx > 0 {
				item = strings.TrimSpace(item[:idx])
			}
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) >= limit {
			break
		}
	}
	return uniqueStrings(out)
}

func extractAttentionItems(answer string, limit int) []string {
	lines := strings.Split(answer, "\n")
	out := make([]string, 0, limit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "*") {
			continue
		}
		item := stripMarkdownMarkers(strings.TrimSpace(strings.TrimPrefix(line, "*")))
		label := ""
		if idx := strings.Index(item, ":"); idx > 0 {
			label = strings.TrimSpace(item[:idx])
			item = strings.TrimSpace(item[idx+1:])
		}
		if strings.Contains(strings.ToLower(label), "организация") || strings.Contains(strings.ToLower(label), "organization") {
			continue
		}
		if item != "" {
			out = append(out, item)
		}
		if len(out) >= limit {
			break
		}
	}
	return uniqueStrings(out)
}

func logicalImportantPaths(home homeWorkspaceRunResponse) []string {
	merged := append([]string{}, home.EventCorePaths...)
	merged = append(merged, home.HaloPaths...)
	if len(merged) == 0 {
		merged = append(merged, home.HighlightedPaths...)
	}
	return humanizedPaths(merged, 8)
}

func logicalOpenQuestions(home homeWorkspaceRunResponse) []string {
	if len(home.Warnings) == 0 {
		if len(home.AffectedZoneRefs) == 0 {
			if isMostlyCyrillic(home.Query) {
				return []string{"Нужно подтвердить масштаб влияния на конкретные зоны и маршруты."}
			}
			return []string{"The exact downstream impact on zones and routes still needs confirmation."}
		}
		return nil
	}
	return fallbackOpenQuestions(home)
}

func harvestKeyJudgments(harvest *harvestRunRecord, limit int) []string {
	if harvest == nil || harvest.Enrichment == nil {
		return nil
	}
	items := compactObjectSlice(harvest.Enrichment["key_judgments"], limit)
	out := make([]string, 0, len(items))
	for _, item := range items {
		statement := compactStringMapValue(item, "statement")
		implication := compactStringMapValue(item, "implication")
		text := strings.TrimSpace(statement)
		if implication != "" {
			if text != "" {
				text += " " + implication
			} else {
				text = implication
			}
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return uniqueStrings(limitStrings(out, limit))
}

func harvestRelatedEntities(harvest *harvestRunRecord, limit int) []string {
	if harvest == nil || harvest.Enrichment == nil {
		return nil
	}
	items := compactObjectSlice(harvest.Enrichment["related_entities"], limit)
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := compactStringMapValue(item, "name")
		role := compactStringMapValue(item, "role")
		if name == "" {
			continue
		}
		if role != "" {
			out = append(out, name+" ("+role+")")
		} else {
			out = append(out, name)
		}
	}
	return uniqueStrings(limitStrings(out, limit))
}

func harvestMaterialTitles(harvest *harvestRunRecord, limit int) []string {
	if harvest == nil || harvest.Enrichment == nil {
		return nil
	}
	items := compactObjectSlice(harvest.Enrichment["materials"], limit)
	out := make([]string, 0, len(items))
	for _, item := range items {
		title := compactStringMapValue(item, "title")
		publisher := compactStringMapValue(item, "publisher")
		if title == "" {
			continue
		}
		if publisher != "" {
			out = append(out, title+" ("+publisher+")")
		} else {
			out = append(out, title)
		}
	}
	return uniqueStrings(limitStrings(out, limit))
}

func harvestGapSummaries(harvest *harvestRunRecord, limit int) []string {
	if harvest == nil || harvest.Enrichment == nil {
		return nil
	}
	items := compactObjectSlice(harvest.Enrichment["evidence_gaps"], limit)
	out := make([]string, 0, len(items))
	for _, item := range items {
		gap := compactStringMapValue(item, "gap")
		next := compactStringMapValue(item, "recommended_next_step")
		if gap == "" {
			continue
		}
		if next != "" {
			out = append(out, gap+" Next: "+next)
		} else {
			out = append(out, gap)
		}
	}
	return uniqueStrings(limitStrings(out, limit))
}

func fallbackDualLayer(home homeWorkspaceRunResponse, harvest *harvestRunRecord) dualLayerOutput {
	name := firstEntityName(home.Entities)
	brief := executiveBriefCandidate{
		Headline:           fallbackHeadline(home, ""),
		Brief:              fallbackExecutiveBrief(home),
		WhyItMatters:       fallbackWhyItMatters(home),
		WhatNeedsAttention: fallbackNeedsAttention(home),
		ConfidenceNote:     fallbackConfidenceNote(home, harvest),
		EvidenceAnchors:    fallbackEvidenceAnchors(home, harvest),
	}
	deeperTitle := "Deeper view"
	if name != "" {
		deeperTitle = "Deeper view: " + name
	}
	deeper := deeperCandidate{
		Title:             deeperTitle,
		Analysis:          fallbackDeeperAnalysis(home),
		KeyDrivers:        fallbackWhyItMatters(home),
		ImportantPaths:    humanizedPaths(append(append([]string{}, home.HighlightedPaths...), home.SuggestedLensScope...), 8),
		HypothesesToCheck: harvestHypotheses(harvest),
		OpenQuestions:     fallbackOpenQuestions(home),
		RefineTheRequest:  defaultRefinePrompts(home, ""),
		EvidenceAnchors:   fallbackEvidenceAnchors(home, harvest),
	}
	return normalizeDualLayer(dualLayerOutput{ExecutiveBrief: brief, Deeper: deeper}, home)
}

func normalizeDualLayer(out dualLayerOutput, home homeWorkspaceRunResponse) dualLayerOutput {
	if strings.TrimSpace(out.ExecutiveBrief.Headline) == "" || isGenericHeading(out.ExecutiveBrief.Headline) {
		out.ExecutiveBrief.Headline = fallbackHeadline(home, "")
	}
	if strings.TrimSpace(out.ExecutiveBrief.Brief) == "" || looksCanonicalHeavy(out.ExecutiveBrief.Brief) || shouldPreferHomeAnswer(home, out.ExecutiveBrief.Brief) {
		out.ExecutiveBrief.Brief = fallbackExecutiveBrief(home)
	}
	out.ExecutiveBrief.WhyItMatters = humanizeBulletList(fillIfEmpty(out.ExecutiveBrief.WhyItMatters, fallbackWhyItMatters(home)), 4)
	out.ExecutiveBrief.WhatNeedsAttention = humanizeBulletList(fillIfEmpty(out.ExecutiveBrief.WhatNeedsAttention, fallbackNeedsAttention(home)), 4)
	out.ExecutiveBrief.ConfidenceNote = firstNonEmpty(out.ExecutiveBrief.ConfidenceNote, fallbackConfidenceNote(home, nil))
	out.ExecutiveBrief.EvidenceAnchors = humanizeBulletList(fillIfEmpty(out.ExecutiveBrief.EvidenceAnchors, fallbackEvidenceAnchors(home, nil)), 5)

	if strings.TrimSpace(out.Deeper.Title) == "" || isGenericHeading(out.Deeper.Title) {
		out.Deeper.Title = fallbackDeeperTitle(home, "")
	}
	if strings.TrimSpace(out.Deeper.Analysis) == "" || looksCanonicalHeavy(out.Deeper.Analysis) || shouldPreferHomeAnswer(home, out.Deeper.Analysis) {
		out.Deeper.Analysis = fallbackDeeperAnalysis(home)
	}
	out.Deeper.KeyDrivers = humanizeBulletList(fillIfEmpty(out.Deeper.KeyDrivers, fallbackWhyItMatters(home)), 5)
	out.Deeper.ImportantPaths = humanizeBulletList(fillIfEmpty(out.Deeper.ImportantPaths, humanizedPaths(home.HighlightedPaths, 8)), 8)
	out.Deeper.OpenQuestions = humanizeBulletList(fillIfEmpty(out.Deeper.OpenQuestions, fallbackOpenQuestions(home)), 4)
	out.Deeper.RefineTheRequest = humanizeBulletList(fillIfEmpty(out.Deeper.RefineTheRequest, defaultRefinePrompts(home, "")), 4)
	out.Deeper.EvidenceAnchors = humanizeBulletList(fillIfEmpty(out.Deeper.EvidenceAnchors, fallbackEvidenceAnchors(home, nil)), 6)

	out.ExecutiveBrief.WhyItMatters = uniqueStrings(limitStrings(out.ExecutiveBrief.WhyItMatters, 4))
	out.ExecutiveBrief.WhatNeedsAttention = uniqueStrings(limitStrings(out.ExecutiveBrief.WhatNeedsAttention, 4))
	out.ExecutiveBrief.EvidenceAnchors = uniqueStrings(limitStrings(out.ExecutiveBrief.EvidenceAnchors, 5))
	out.Deeper.KeyDrivers = uniqueStrings(limitStrings(out.Deeper.KeyDrivers, 5))
	out.Deeper.ImportantPaths = uniqueStrings(limitStrings(out.Deeper.ImportantPaths, 8))
	out.Deeper.HypothesesToCheck = uniqueStrings(limitStrings(out.Deeper.HypothesesToCheck, 4))
	out.Deeper.OpenQuestions = uniqueStrings(limitStrings(out.Deeper.OpenQuestions, 4))
	out.Deeper.RefineTheRequest = uniqueStrings(limitStrings(out.Deeper.RefineTheRequest, 4))
	out.Deeper.EvidenceAnchors = uniqueStrings(limitStrings(out.Deeper.EvidenceAnchors, 6))
	return out
}

func buildDualLayerDraftPrompt(question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord) string {
	payload := buildDualLayerContext(question, home, harvest)
	return "Create the best possible C-1 dual-layer response for the question using the grounded context below.\n\n" + mustJSON(payload)
}

func buildDualLayerReviewPrompt(question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord, draft dualLayerOutput) string {
	payload := map[string]any{
		"question": question,
		"context":  buildDualLayerContext(question, home, harvest),
		"draft":    draft,
	}
	return "Review this dual-layer answer against the C-1 Home Workspace bar.\n\n" + mustJSON(payload)
}

func buildDualLayerRevisionPrompt(question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord, draft dualLayerOutput, review dualLayerReview) string {
	payload := map[string]any{
		"question": question,
		"context":  buildDualLayerContext(question, home, harvest),
		"draft":    draft,
		"review":   review,
	}
	return "Revise the dual-layer answer so it directly addresses the critique while staying grounded.\n\n" + mustJSON(payload)
}

func buildDualLayerContext(question string, home homeWorkspaceRunResponse, harvest *harvestRunRecord) map[string]any {
	topHits := make([]map[string]any, 0, minInt(len(home.Hits), 8))
	for i, hit := range home.Hits {
		if i >= 8 {
			break
		}
		topHits = append(topHits, map[string]any{
			"title":          compactStringMapValue(hit, "title"),
			"kind":           compactStringMapValue(hit, "kind"),
			"score":          hit["score"],
			"canonicalPaths": compactStringSlice(hit["canonicalPaths"]),
			"summary":        compactStringMapValue(hit, "summary"),
			"uri":            compactStringMapValue(hit, "uri"),
		})
	}

	payload := map[string]any{
		"question": question,
		"home": map[string]any{
			"answer":             truncateString(strings.TrimSpace(home.Answer), 4000),
			"executiveFallback":  fallbackExecutiveBrief(home),
			"deeperFallback":     fallbackDeeperAnalysis(home),
			"entities":           home.Entities,
			"entityNames":        entityNames(home.Entities),
			"topHits":            topHits,
			"topHitTitles":       humanizedHitTitles(home.Hits, 6),
			"highlightedPaths":   limitStrings(home.HighlightedPaths, 12),
			"highlightedLabels":  humanizedPaths(home.HighlightedPaths, 10),
			"eventCorePaths":     limitStrings(home.EventCorePaths, 8),
			"eventCoreLabels":    humanizedPaths(home.EventCorePaths, 8),
			"haloPaths":          limitStrings(home.HaloPaths, 10),
			"haloLabels":         humanizedPaths(home.HaloPaths, 8),
			"affectedZoneRefs":   limitStrings(home.AffectedZoneRefs, 8),
			"attentionLabels":    fallbackNeedsAttention(home),
			"suggestedLensName":  home.SuggestedLensName,
			"suggestedLensScope": limitStrings(home.SuggestedLensScope, 12),
			"warnings":           limitStrings(home.Warnings, 8),
		},
	}
	if harvest != nil {
		payload["harvest"] = map[string]any{
			"entityName":          harvest.EntityName,
			"entityCanonicalPath": harvest.EntityCanonicalPath,
			"usableMaterialCount": harvest.UsableMaterialCount,
			"finalReportGCSURI":   harvest.FinalReportGCSURI,
			"enrichment": map[string]any{
				"executiveBrief":  truncateString(compactStringMapValue(harvest.Enrichment, "executive_brief"), 1600),
				"keyJudgments":    compactObjectSlice(harvest.Enrichment["key_judgments"], 6),
				"relatedEntities": compactObjectSlice(harvest.Enrichment["related_entities"], 8),
				"evidenceGaps":    compactObjectSlice(harvest.Enrichment["evidence_gaps"], 5),
				"materials":       compactObjectSlice(harvest.Enrichment["materials"], 6),
				"portfolio":       harvest.Enrichment["consulting_grade_portfolio"],
			},
		}
	}
	return payload
}

func dualLayerSystemInstruction() string {
	return strings.TrimSpace(`
You are validating Horizon Home Workspace for a C-1 dual-layer experience.
Use only the provided context. Do not invent facts or use outside knowledge.
The response must stay in the same language as the user's question.

Executive Brief rules:
- It is for a senior business user, not an operator.
- Start with the direct business answer.
- Explain why this matters now.
- Make clear what needs attention, but do not overstate certainty.

Deeper rules:
- It should deepen the same question, not repeat the brief.
- Explain the main drivers, affected entities, zones, routes, or counterparties that matter.
- You may propose hypotheses, but label them as hypotheses rather than facts.
- Prefer high-signal analysis over exhaustive dumping.

Grounding rules:
- Treat optional harvest context as auxiliary evidence, not as unquestionable truth.
- Do not expose canonical paths in prose unless they are useful as identifiers.
- Evidence anchors should point to source titles, entities, artifacts, or canonical paths that actually appear in the provided context.
`)
}

func dualLayerReviewInstruction() string {
	return strings.TrimSpace(`
You are critiquing a C-1 dual-layer Home Workspace answer.
Return strict JSON only.
Judge whether:
- the Executive Brief is direct, business-useful, and not operator-centric
- the Deeper layer actually deepens the answer
- hypotheses are clearly labeled
- grounding discipline is preserved
Be blunt and specific in revision priorities.
`)
}

func dualLayerOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"executiveBrief": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"headline":           map[string]any{"type": "string"},
					"brief":              map[string]any{"type": "string"},
					"whyItMatters":       stringArraySchema(),
					"whatNeedsAttention": stringArraySchema(),
					"confidenceNote":     map[string]any{"type": "string"},
					"evidenceAnchors":    stringArraySchema(),
				},
				"required": []string{"headline", "brief", "whyItMatters", "whatNeedsAttention", "confidenceNote", "evidenceAnchors"},
			},
			"deeper": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":             map[string]any{"type": "string"},
					"analysis":          map[string]any{"type": "string"},
					"keyDrivers":        stringArraySchema(),
					"importantPaths":    stringArraySchema(),
					"hypothesesToCheck": stringArraySchema(),
					"openQuestions":     stringArraySchema(),
					"refineTheRequest":  stringArraySchema(),
					"evidenceAnchors":   stringArraySchema(),
				},
				"required": []string{"title", "analysis", "keyDrivers", "importantPaths", "hypothesesToCheck", "openQuestions", "refineTheRequest", "evidenceAnchors"},
			},
		},
		"required": []string{"executiveBrief", "deeper"},
	}
}

func dualLayerReviewSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"overallVerdict":      map[string]any{"type": "string"},
			"executiveBriefNotes": stringArraySchema(),
			"deeperNotes":         stringArraySchema(),
			"revisionPriorities":  stringArraySchema(),
		},
		"required": []string{"overallVerdict", "executiveBriefNotes", "deeperNotes", "revisionPriorities"},
	}
}

func stringArraySchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
}

func decodeJSONContent(raw string, out any) error {
	content := strings.TrimSpace(raw)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if err := json.Unmarshal([]byte(content), out); err == nil {
		return nil
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
		if err := json.Unmarshal([]byte(content), out); err == nil {
			return nil
		}
		var wrapped map[string]json.RawMessage
		if err := json.Unmarshal([]byte(content), &wrapped); err == nil {
			for _, key := range []string{"review", "result", "output", "answer", "dualLayer"} {
				if payload, ok := wrapped[key]; ok {
					if err := json.Unmarshal(payload, out); err == nil {
						return nil
					}
				}
			}
		}
	}
	return fmt.Errorf("response was not valid JSON: %s", truncateString(content, 600))
}

func extractTrailingJSONObject(output []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(output))
	start := strings.LastIndex(trimmed, "\n{")
	if start >= 0 {
		return []byte(strings.TrimSpace(trimmed[start+1:])), nil
	}
	if strings.HasPrefix(trimmed, "{") {
		return []byte(trimmed), nil
	}
	return nil, fmt.Errorf("could not find trailing JSON object in output")
}

func renderExecutiveBrief(brief executiveBriefCandidate) string {
	whyLabel := "Why it matters:"
	attentionLabel := "What needs attention:"
	confidenceLabel := "Confidence:"
	evidenceLabel := "Evidence anchors:"
	if isMostlyCyrillic(brief.Brief + " " + brief.Headline) {
		whyLabel = "Почему это важно:"
		attentionLabel = "Что требует внимания:"
		confidenceLabel = "Уровень уверенности:"
		evidenceLabel = "Опорные сигналы:"
	}
	lines := []string{
		"# " + strings.TrimSpace(brief.Headline),
		"",
		strings.TrimSpace(brief.Brief),
	}
	if len(brief.WhyItMatters) > 0 {
		lines = append(lines, "", whyLabel)
		for _, item := range brief.WhyItMatters {
			lines = append(lines, "- "+item)
		}
	}
	if len(brief.WhatNeedsAttention) > 0 {
		lines = append(lines, "", attentionLabel)
		for _, item := range brief.WhatNeedsAttention {
			lines = append(lines, "- "+item)
		}
	}
	if strings.TrimSpace(brief.ConfidenceNote) != "" {
		lines = append(lines, "", confidenceLabel, brief.ConfidenceNote)
	}
	if len(brief.EvidenceAnchors) > 0 {
		lines = append(lines, "", evidenceLabel)
		for _, item := range brief.EvidenceAnchors {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func renderDeeper(deeper deeperCandidate) string {
	keyDriversLabel := "Key drivers:"
	pathsLabel := "Important paths and zones:"
	hypothesesLabel := "Hypotheses to check:"
	openQuestionsLabel := "Open questions:"
	refineLabel := "Show deeper / refine the request:"
	evidenceLabel := "Evidence anchors:"
	if isMostlyCyrillic(deeper.Analysis + " " + deeper.Title) {
		keyDriversLabel = "Ключевые драйверы:"
		pathsLabel = "Ключевые пути и зоны:"
		hypothesesLabel = "Гипотезы для проверки:"
		openQuestionsLabel = "Открытые вопросы:"
		refineLabel = "Как углубить запрос:"
		evidenceLabel = "Опорные сигналы:"
	}
	lines := []string{
		"# " + strings.TrimSpace(deeper.Title),
		"",
		strings.TrimSpace(deeper.Analysis),
	}
	if len(deeper.KeyDrivers) > 0 {
		lines = append(lines, "", keyDriversLabel)
		for _, item := range deeper.KeyDrivers {
			lines = append(lines, "- "+item)
		}
	}
	if len(deeper.ImportantPaths) > 0 {
		lines = append(lines, "", pathsLabel)
		for _, item := range deeper.ImportantPaths {
			lines = append(lines, "- "+item)
		}
	}
	if len(deeper.HypothesesToCheck) > 0 {
		lines = append(lines, "", hypothesesLabel)
		for _, item := range deeper.HypothesesToCheck {
			lines = append(lines, "- "+item)
		}
	}
	if len(deeper.OpenQuestions) > 0 {
		lines = append(lines, "", openQuestionsLabel)
		for _, item := range deeper.OpenQuestions {
			lines = append(lines, "- "+item)
		}
	}
	if len(deeper.RefineTheRequest) > 0 {
		lines = append(lines, "", refineLabel)
		for _, item := range deeper.RefineTheRequest {
			lines = append(lines, "- "+item)
		}
	}
	if len(deeper.EvidenceAnchors) > 0 {
		lines = append(lines, "", evidenceLabel)
		for _, item := range deeper.EvidenceAnchors {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n")
}

func fallbackConfidenceNote(home homeWorkspaceRunResponse, harvest *harvestRunRecord) string {
	if harvest != nil && harvest.UsableMaterialCount > 0 {
		if isMostlyCyrillic(home.Query) {
			return fmt.Sprintf("Ответ grounded на текущем Home Workspace evidence и %d внешних материалах из harvest-контура.", harvest.UsableMaterialCount)
		}
		return fmt.Sprintf("Grounded in Home Workspace evidence and %d harvested supporting materials.", harvest.UsableMaterialCount)
	}
	if len(home.Warnings) > 0 {
		if isMostlyCyrillic(home.Query) {
			return "Ответ grounded, но в текущем substrate всё ещё есть заметные пробелы."
		}
		return "Grounded, but the current substrate still has visible gaps."
	}
	if isMostlyCyrillic(home.Query) {
		return "Ответ grounded на текущем Home Workspace evidence."
	}
	return "Grounded in current Home Workspace evidence."
}

func fallbackEvidenceAnchors(home homeWorkspaceRunResponse, harvest *harvestRunRecord) []string {
	anchors := make([]string, 0, 8)
	for i, hit := range home.Hits {
		if i >= 4 {
			break
		}
		title := humanizeHitTitle(compactStringMapValue(hit, "title"))
		if title != "" {
			anchors = append(anchors, title)
		}
	}
	if harvest != nil {
		if value := compactStringMapValue(harvest.Enrichment, "executive_brief"); value != "" {
			anchors = append(anchors, "Harvest portfolio executive brief")
		}
	}
	if len(anchors) == 0 {
		anchors = append(anchors, humanizedPaths(home.HighlightedPaths, 4)...)
	}
	return uniqueStrings(limitStrings(anchors, 6))
}

func harvestHypotheses(harvest *harvestRunRecord) []string {
	if harvest == nil {
		return nil
	}
	gaps := compactObjectSlice(harvest.Enrichment["evidence_gaps"], 3)
	out := make([]string, 0, len(gaps))
	for _, item := range gaps {
		gap := compactStringMapValue(item, "gap")
		next := compactStringMapValue(item, "recommended_next_step")
		if gap == "" {
			continue
		}
		if next != "" {
			out = append(out, "Hypothesis: "+gap+" Check via "+next)
		} else {
			out = append(out, "Hypothesis: "+gap)
		}
	}
	return out
}

func defaultRefinePrompts(home homeWorkspaceRunResponse, focalRef string) []string {
	name := focalEntityName(home, focalRef)
	if name == "" {
		name = "this entity"
	}
	if isMostlyCyrillic(home.Query) {
		return []string{
			"Показать главные exposure paths вокруг " + name,
			"Отделить то, что изменилось сейчас, от структурного фона",
			"Оставить только самые важные для решения контрагенты, маршруты и зоны",
		}
	}
	return []string{
		"Show the main exposure paths around " + name,
		"Separate what changed now from structural background",
		"Focus only on the most decision-relevant counterparties and zones",
	}
}

func fallbackHeadline(home homeWorkspaceRunResponse, focalRef string) string {
	name := focalEntityName(home, focalRef)
	if name == "" {
		name = "Target exposure"
	}
	if isMostlyCyrillic(home.Query) {
		return name + ": ключевая экспозиция прямо сейчас"
	}
	return name + ": immediate exposure view"
}

func fallbackDeeperTitle(home homeWorkspaceRunResponse, focalRef string) string {
	name := focalEntityName(home, focalRef)
	if name == "" {
		if isMostlyCyrillic(home.Query) {
			return "Более глубокий разбор"
		}
		return "Deeper view"
	}
	if isMostlyCyrillic(home.Query) {
		return "Более глубокий разбор: " + name
	}
	return "Deeper view: " + name
}

func fallbackExecutiveBrief(home homeWorkspaceRunResponse) string {
	if usesGoodHomeAnswer(home) {
		return compactHomeAnswer(home.Answer, 700)
	}
	core := humanizedPaths(home.EventCorePaths, 4)
	halo := humanizedPaths(home.HaloPaths, 4)
	name := focalEntityName(home, "")
	if isMostlyCyrillic(home.Query) {
		if len(core) == 0 {
			return strings.TrimSpace(home.Answer)
		}
		text := "Свежий сигнал по " + firstNonEmpty(name, "объекту") + " концентрируется вокруг " + joinList(core) + "."
		if len(halo) > 0 {
			text += " Ближайшая экспозиция проходит через " + joinList(halo[:minInt(len(halo), 3)]) + "."
		}
		return text
	}
	if len(core) == 0 {
		return strings.TrimSpace(home.Answer)
	}
	text := "Fresh signal around " + firstNonEmpty(name, "the focal entity") + " is concentrated on " + joinList(core) + "."
	if len(halo) > 0 {
		text += " The nearest exposure halo runs through " + joinList(halo[:minInt(len(halo), 3)]) + "."
	}
	return text
}

func fallbackDeeperAnalysis(home homeWorkspaceRunResponse) string {
	if usesGoodHomeAnswer(home) {
		return strings.TrimSpace(home.Answer)
	}
	core := humanizedPaths(home.EventCorePaths, 5)
	attention := fallbackNeedsAttention(home)
	halo := humanizedPaths(home.HaloPaths, 5)
	if isMostlyCyrillic(home.Query) {
		parts := []string{}
		if len(core) > 0 {
			parts = append(parts, "Текущий сигнал завязан на "+joinList(core)+".")
		}
		if len(attention) > 0 {
			parts = append(parts, "Для устойчивости важнее всего наблюдать за "+joinList(attention)+".")
		}
		if len(halo) > 0 {
			parts = append(parts, "Вторая линия эффекта проходит через "+joinList(halo[:minInt(len(halo), 3)])+".")
		}
		if len(parts) == 0 {
			return strings.TrimSpace(home.Answer)
		}
		return strings.Join(parts, " ")
	}
	parts := []string{}
	if len(core) > 0 {
		parts = append(parts, "The current signal is tied to "+joinList(core)+".")
	}
	if len(attention) > 0 {
		parts = append(parts, "From a resilience standpoint, the closest watch items are "+joinList(attention)+".")
	}
	if len(halo) > 0 {
		parts = append(parts, "A second-order effect is visible through "+joinList(halo[:minInt(len(halo), 3)])+".")
	}
	if len(parts) == 0 {
		return strings.TrimSpace(home.Answer)
	}
	return strings.Join(parts, " ")
}

func fallbackWhyItMatters(home homeWorkspaceRunResponse) []string {
	if isMostlyCyrillic(home.Query) {
		return humanizeBulletList(append(
			prefixEach(humanizedPaths(home.EventCorePaths, 2), "Немедленный сигнал: "),
			prefixEach(humanizedPaths(home.HaloPaths, 2), "Ближайший ореол воздействия: ")...,
		), 4)
	}
	return humanizeBulletList(append(
		prefixEach(humanizedPaths(home.EventCorePaths, 2), "Immediate signal: "),
		prefixEach(humanizedPaths(home.HaloPaths, 2), "Nearest impact halo: ")...,
	), 4)
}

func fallbackNeedsAttention(home homeWorkspaceRunResponse) []string {
	paths := make([]string, 0, 8)
	for _, path := range append(append([]string{}, home.HighlightedPaths...), home.EventCorePaths...) {
		if strings.Contains(path, "/fac/") || strings.Contains(path, "/zone/") || strings.Contains(path, "/supply-link/") {
			paths = append(paths, path)
		}
	}
	humanized := humanizedPaths(paths, 4)
	if len(humanized) > 0 {
		if isMostlyCyrillic(home.Query) {
			return prefixEach(humanized, "Под наблюдением: ")
		}
		return prefixEach(humanized, "Watch item: ")
	}
	return nil
}

func fallbackOpenQuestions(home homeWorkspaceRunResponse) []string {
	if len(home.Warnings) == 0 {
		return nil
	}
	if isMostlyCyrillic(home.Query) {
		return []string{"Нужно подтвердить картину через более полный внутренний корпус и search-backed grounding."}
	}
	return []string{"This still needs confirmation from a fuller internal corpus and search-backed grounding."}
}

func summarizeList(values []string, limit int) []string {
	out := make([]string, 0, limit)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func entityNames(entities []homeWorkspaceEntity) []string {
	out := make([]string, 0, len(entities))
	for _, entity := range entities {
		if name := strings.TrimSpace(entity.Name); name != "" {
			out = append(out, name)
		}
	}
	return uniqueStrings(out)
}

func firstEntityName(entities []homeWorkspaceEntity) string {
	for _, entity := range entities {
		if strings.TrimSpace(entity.Name) != "" {
			return strings.TrimSpace(entity.Name)
		}
	}
	return ""
}

func focalEntityName(home homeWorkspaceRunResponse, focalRef string) string {
	if focalRef = strings.TrimSpace(focalRef); focalRef != "" {
		return humanizeCanonicalPath(focalRef)
	}
	if name := firstEntityName(home.Entities); name != "" {
		return name
	}
	for _, path := range home.EventCorePaths {
		if strings.Contains(path, "/org/") {
			return humanizeCanonicalPath(path)
		}
	}
	for _, path := range append(append([]string{}, home.HighlightedPaths...), home.SuggestedLensScope...) {
		if strings.Contains(path, "/org/") {
			return humanizeCanonicalPath(path)
		}
	}
	return ""
}

func usesGoodHomeAnswer(home homeWorkspaceRunResponse) bool {
	answer := strings.TrimSpace(home.Answer)
	return len(answer) >= 180 && !looksCanonicalHeavy(answer)
}

func shouldPreferHomeAnswer(home homeWorkspaceRunResponse, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if !usesGoodHomeAnswer(home) {
		return false
	}
	if candidate == "" {
		return true
	}
	if len(candidate) < 220 && len(strings.TrimSpace(home.Answer)) > len(candidate)+120 {
		return true
	}
	if strings.Contains(candidate, "->") {
		return true
	}
	if isMostlyCyrillic(home.Query) && !isMostlyCyrillic(candidate) {
		return true
	}
	return false
}

func compactHomeAnswer(answer string, maxLen int) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	if len(answer) <= maxLen {
		return answer
	}
	parts := strings.Split(answer, "\n\n")
	if len(parts) > 0 {
		first := strings.TrimSpace(parts[0])
		if first != "" && len(first) <= maxLen {
			return first
		}
	}
	return truncateString(answer, maxLen)
}

func compactStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return limitStrings(typed, 8)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return limitStrings(out, 8)
	default:
		return nil
	}
}

func humanizedPaths(paths []string, limit int) []string {
	out := make([]string, 0, limit)
	for _, path := range paths {
		label := humanizeCanonicalPath(path)
		if label == "" {
			continue
		}
		out = append(out, label)
		if len(out) >= limit {
			break
		}
	}
	return uniqueStrings(out)
}

func humanizedHitTitles(hits []map[string]any, limit int) []string {
	out := make([]string, 0, limit)
	for _, hit := range hits {
		title := humanizeHitTitle(compactStringMapValue(hit, "title"))
		if title == "" {
			continue
		}
		out = append(out, title)
		if len(out) >= limit {
			break
		}
	}
	return uniqueStrings(out)
}

func humanizeHitTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	if idx := strings.Index(title, " /"); idx >= 0 {
		prefix := strings.TrimSpace(title[:idx])
		suffix := strings.TrimSpace(title[idx+1:])
		if strings.HasPrefix(suffix, "/") {
			return firstNonEmpty(prefix+" "+humanizeCanonicalPath(suffix), title)
		}
	}
	return strings.ReplaceAll(title, "_", " ")
}

func stripMarkdownMarkers(value string) string {
	value = strings.TrimSpace(value)
	replacer := strings.NewReplacer(
		"**", "",
		"*", "",
		"`", "",
	)
	value = replacer.Replace(value)
	value = strings.TrimSpace(value)
	return value
}

func humanizeBulletList(items []string, limit int) []string {
	out := make([]string, 0, limit)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "/") {
			item = humanizeCanonicalPath(item)
		}
		item = strings.ReplaceAll(item, " /", " ")
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return uniqueStrings(out)
}

func compactObjectSlice(value any, limit int) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		if maps, ok := value.([]map[string]any); ok {
			return maps
		}
		return nil
	}
	out := make([]map[string]any, 0, minInt(len(items), limit))
	for _, item := range items {
		if len(out) >= limit {
			break
		}
		m, ok := item.(map[string]any)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

func compactStringMapValue(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(m[key]))
}

func humanizeCanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return strings.ReplaceAll(path, "_", " ")
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	switch parts[0] {
	case "supply-link":
		if len(parts) >= 5 {
			return humanizeSlug(parts[2]) + " -> " + humanizeSlug(parts[3]) + " -> " + humanizeSlug(parts[4])
		}
	case "zone":
		if len(parts) >= 4 {
			return humanizeSlug(parts[3]) + " " + strings.ReplaceAll(parts[2], "_", " ") + " zone"
		}
	}
	return humanizeSlug(parts[len(parts)-1])
}

func humanizeSlug(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "/")
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "_", " ")
	chunks := strings.Split(value, "-")
	for i, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		if len(chunk) <= 3 && strings.ToUpper(chunk) == chunk {
			continue
		}
		chunks[i] = strings.ToUpper(chunk[:1]) + chunk[1:]
	}
	return strings.Join(chunks, " ")
}

func mustJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func fillIfEmpty(current, fallback []string) []string {
	if len(current) > 0 {
		return current
	}
	return fallback
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
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

func truncateString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "...(truncated)"
}

func joinList(items []string) string {
	items = uniqueStrings(items)
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
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

func prefixEach(items []string, prefix string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, prefix+item)
	}
	return out
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isGenericHeading(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "" || normalized == "executive brief" || normalized == "deeper view"
}

func looksCanonicalHeavy(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return strings.Count(value, "/") >= 2 || strings.Contains(value, "_")
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

func (r *toolRuntime) newAuthedJSONRequest(ctx context.Context, auth *corehttp.AuthInfo, method, path string, payload interface{}) *http.Request {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	reader := bytes.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	ctx = context.WithValue(ctx, corehttp.AuthContextKey{}, auth)
	return req.WithContext(ctx)
}

func (r *toolRuntime) invokeJSON(handler func(http.ResponseWriter, *http.Request), req *http.Request, out interface{}) error {
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

func finalizeReport(path string, report dualLayerReport) {
	if strings.TrimSpace(path) != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			log.Fatalf("create output dir failed: %v", err)
		}
		writeJSON(path, report)
	}
	printJSON(report)
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
