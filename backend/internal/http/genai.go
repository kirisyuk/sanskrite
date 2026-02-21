package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_retry "github.com/igorkirisyuk/sanskrite/internal/genai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	g "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/research"
)

type GenAIHandler struct {
	client           *gen_combined.Client
	publisher        Publisher
	usageMetricsRepo *repo.UsageMetricsRepository
	modelManager     *ai.AIModelManager
	taskExecutor     *ai.AITaskExecutor
	// CoreDocs tool dependencies (optional, wired via WithCoreDocsTools)
	coreDocs     *repo.CoreDocsRepo
	interactions *repo.InteractionsRepository
	docsRepo     *repo.DocumentsRepository
	chunksRepo   *repo.ChunksRepository
	emb          ai.EmbeddingProvider
	storage      StorageClient
	bucket       string
	// Graph tool dependencies (optional, wired via WithGraphTools)
	graphEnabled bool
	// Research mode dependencies (optional, wired via WithTavilyClient)
	tavilyClient *TavilyClient
	// Knowledge search dependencies (optional, wired via WithWikidataClient)
	wikidataClient *WikidataClient
	// Ontology tool dependencies (optional, wired via WithOntologyTools)
	ontologyEnabled bool
	entityRepo      *repo.EntityRepository
	eventRepo       *repo.EventRepository
	claimRepo       *repo.ClaimRepository
	relationRepo    *repo.RelationRepository
	trendRepo       *repo.TrendRepository
	causalLinkRepo  *repo.CausalLinkRepository
	metricStateRepo *repo.MetricStateRepository
	uncertaintyRepo *repo.UncertaintyModelRepository
	sourceRepo      *repo.SourceRepository
	trustRepo       *repo.TrustRepository
	conceptRepo     *repo.ConceptRepository
}

// Publisher is a small interface for realtime publisher used here
type Publisher interface {
	PublishJSON(ctx context.Context, et interface{}, tenantID string, payload interface{}) error
}

func NewGenAIHandler() *GenAIHandler {
	apiKey := os.Getenv("GEMINI_API_KEY")
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	location := os.Getenv("VERTEX_LOCATION")
	if location == "" {
		location = "us-central1"
	}
	fast := os.Getenv("GENAI_FAST_MODEL")
	quality := os.Getenv("GENAI_QUALITY_MODEL")
	gem := gen_gemini.NewClient(apiKey, projectID, location, fast, quality)
	oaiKey := os.Getenv("OPENAI_API_KEY")
	comb := gen_combined.NewClient(gem, oaiKey)
	return &GenAIHandler{client: comb}
}

// WithUsageMetrics wires usage metrics repository
func (h *GenAIHandler) WithUsageMetrics(repo *repo.UsageMetricsRepository) *GenAIHandler {
	h.usageMetricsRepo = repo
	return h
}

// WithModelManager wires AI model manager for cost calculation
func (h *GenAIHandler) WithModelManager(mm *ai.AIModelManager) *GenAIHandler {
	h.modelManager = mm
	return h
}

// WithAIExecutor wires AI task executor for structured tasks (model mode)
func (h *GenAIHandler) WithAIExecutor(exec *ai.AITaskExecutor) *GenAIHandler {
	h.taskExecutor = exec
	return h
}

// WithCoreDocsTools wires repositories needed for CoreDocs tool execution
func (h *GenAIHandler) WithCoreDocsTools(
	coreDocs *repo.CoreDocsRepo,
	interactions *repo.InteractionsRepository,
	docsRepo *repo.DocumentsRepository,
	chunksRepo *repo.ChunksRepository,
	emb ai.EmbeddingProvider,
	storage StorageClient,
	bucket string,
) *GenAIHandler {
	h.coreDocs = coreDocs
	h.interactions = interactions
	h.docsRepo = docsRepo
	h.chunksRepo = chunksRepo
	h.emb = emb
	h.storage = storage
	h.bucket = bucket
	return h
}

// WithGraphTools enables graph tool execution (uses interactions repo from WithCoreDocsTools)
func (h *GenAIHandler) WithGraphTools(enabled bool) *GenAIHandler {
	h.graphEnabled = enabled
	return h
}

// WithTavilyClient wires Tavily client for Research mode
func (h *GenAIHandler) WithTavilyClient(client *TavilyClient) *GenAIHandler {
	h.tavilyClient = client
	return h
}

// WithWikidataClient wires Wikidata client for Knowledge Search tool
func (h *GenAIHandler) WithWikidataClient(client *WikidataClient) *GenAIHandler {
	h.wikidataClient = client
	return h
}

// WithOntologyTools wires ontology repositories for Ontology tools
func (h *GenAIHandler) WithOntologyTools(
	entityRepo *repo.EntityRepository,
	eventRepo *repo.EventRepository,
	claimRepo *repo.ClaimRepository,
	relationRepo *repo.RelationRepository,
	trendRepo *repo.TrendRepository,
	causalLinkRepo *repo.CausalLinkRepository,
	metricStateRepo *repo.MetricStateRepository,
	uncertaintyRepo *repo.UncertaintyModelRepository,
	sourceRepo *repo.SourceRepository,
	trustRepo *repo.TrustRepository,
	conceptRepo *repo.ConceptRepository,
) *GenAIHandler {
	h.ontologyEnabled = true
	h.entityRepo = entityRepo
	h.eventRepo = eventRepo
	h.claimRepo = claimRepo
	h.relationRepo = relationRepo
	h.trendRepo = trendRepo
	h.causalLinkRepo = causalLinkRepo
	h.metricStateRepo = metricStateRepo
	h.uncertaintyRepo = uncertaintyRepo
	h.sourceRepo = sourceRepo
	h.trustRepo = trustRepo
	h.conceptRepo = conceptRepo
	return h
}

// GenerateDirect performs direct GenAI generation without HTTP layer (for internal use)
func (h *GenAIHandler) GenerateDirect(ctx context.Context, req gen_combined.GenAIRequest) (*gen_combined.GenAIResponse, error) {
	return h.client.GenAIGenerate(ctx, req)
}

// GenerateStructuredViaTaskExecutor uses task executor (Model Manager + fallbacks) when available.
// Falls back to GenerateDirect if taskExecutor is nil.
// minCapScore: minimum capability score for model selection (e.g. 7 for Graph Intent).
func (h *GenAIHandler) GenerateStructuredViaTaskExecutor(ctx context.Context, req gen_combined.GenAIRequest, minCapScore int) (*gen_combined.GenAIResponse, error) {
	if h.taskExecutor == nil {
		return h.GenerateDirect(ctx, req)
	}
	fullInput := strings.TrimSpace(req.Prompt)
	if req.SystemInstruction != "" {
		fullInput = strings.TrimSpace(req.SystemInstruction) + "\n\nUser request: " + fullInput
	}
	inputTokens := len(fullInput) / 4
	if inputTokens < 1000 {
		inputTokens = 1000
	}
	maxOut := req.MaxOutputTokens
	if maxOut <= 0 {
		maxOut = 65536
	}
	verifier := ai.NewSimpleJSONVerifier()
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: fullInput,
		AIRequirements: ai.AIRequirements{
			PreferredModel:           req.Model,
			MinContextWindow:         inputTokens,
			MaxOutputSize:            maxOut,
			MinCapabilityScore:       minCapScore,
			TextGenerationCapability: true,
		},
		Verifier:        verifier,
		EnableTelemetry: true,
	}
	// Plumb tenant/user for usage metrics (same as GenAIGenerate task-executor flow)
	if auth := GetAuthInfo(ctx); auth != nil {
		task.TenantID = auth.TenantID
		task.UserID = auth.UID
		if req.ProjectID != "" {
			task.Project = req.ProjectID
		}
	}
	parsed, fullResult, err := h.taskExecutor.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, err
	}
	jsonBytes, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("marshal task result: %w", err)
	}
	inT := fullResult.InputTokens
	if inT <= 0 {
		inT = inputTokens
	}
	outT := fullResult.OutputTokens
	if outT <= 0 {
		outT = len(jsonBytes) / 4
	}
	return &gen_combined.GenAIResponse{
		Content: string(jsonBytes),
		Model:   fullResult.Model,
		Usage: g.Usage{
			PromptTokens: inT,
			OutputTokens: outT,
			TotalTokens:  inT + outT,
		},
	}, nil
}

// GenAIGenerate: unified generation endpoint
func (h *GenAIHandler) GenAIGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	var req gen_combined.GenAIRequest
	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Restore the body for decoding
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("GenAIGenerate: INVALID_JSON. Body: %s, Error: %v", string(bodyBytes), err)
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Model) == "" || strings.TrimSpace(req.Prompt) == "" {
		log.Printf("GenAIGenerate: INVALID_PARAMS. Model: '%s', Prompt: '%s'", req.Model, req.Prompt)
		_ = WriteBadRequest(w, "INVALID_PARAMS", "model and prompt are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	started := time.Now()

	// Check for Research mode - if enabled, use research pipeline instead of regular generation
	if req.ResearchEnabled {
		// Extract projectId and streamId from request body (preferred) or query params (fallback)
		projectID := req.ProjectID
		if projectID == "" {
			projectID = r.URL.Query().Get("projectId")
		}
		streamID := req.StreamID
		if streamID == "" {
			streamID = r.URL.Query().Get("streamId")
		}
		if streamID == "" {
			streamID = uuid.NewString()
		}

		resp, err := h.executeResearchMode(ctx, req, projectID, streamID)
		if err != nil {
			log.Printf("GenAIGenerate research mode error: %v", err)
			status, code, msg, details := parseUpstreamError(err)
			_ = WriteError(w, status, code, msg, details)
			return
		}
		// Continue with response handling below
		_ = WriteJSON(w, http.StatusOK, resp)
		return
	}

	// Graph mode: use task executor with universal JSON verifier for strict JSON validation
	// Universal verifier only checks JSON deserialization correctness, no schema-specific validation
	if req.StrictJSONEnabled && h.taskExecutor != nil {
		// Build system instruction with message history (same as regular flow)
		systemInstruction := req.SystemInstruction
		if len(req.Messages) > 0 {
			ctxLines := make([]string, 0, len(req.Messages))
			for _, m := range req.Messages {
				role := strings.ToLower(strings.TrimSpace(m.Role))
				if role == "" {
					role = "user"
				}
				text := strings.TrimSpace(m.Text)
				if text == "" {
					continue
				}
				ctxLines = append(ctxLines, "- "+role+": "+text)
			}
			if len(ctxLines) > 0 {
				pre := "You are given the full chat context below. Respond ONLY to the latest user message, taking the prior context into account. Do not restate or summarize the transcript itself."
				ctx := "\nConversation context (most recent last):\n" + strings.Join(ctxLines, "\n")
				if strings.TrimSpace(systemInstruction) == "" {
					systemInstruction = pre + ctx
				} else {
					systemInstruction = strings.TrimSpace(systemInstruction) + "\n\n" + pre + ctx
				}
			}
		}

		// Create universal JSON verifier - only checks JSON deserialization correctness
		// No schema-specific validation (like commands check) - if JSON is invalid, it will fail naturally
		verifier := ai.NewSimpleJSONVerifier()

		// Build full input content with system instruction
		// AITask doesn't support systemInstruction separately, so we include it in InputContent
		fullInput := req.Prompt
		if systemInstruction != "" {
			fullInput = systemInstruction + "\n\nUser request: " + req.Prompt
		}

		// Estimate input tokens
		inputChars := len(fullInput)
		inputTokens := inputChars / 4

		// Create AITask for structured execution
		// Use ContentGeneration as universal task type for any structured JSON generation
		task := &ai.AITask{
			TaskType:     ai.TaskTypeContentGeneration,
			InputContent: fullInput,
			AIRequirements: ai.AIRequirements{
				PreferredModel:           req.Model,
				MinContextWindow:         1000,
				MaxOutputSize:            req.MaxOutputTokens,
				MinCapabilityScore:       5,
				TextGenerationCapability: true,
			},
			Verifier:        verifier,
			EnableTelemetry: true,
		}

		// Set tenant/user info if available
		if auth := GetAuthInfo(r.Context()); auth != nil {
			task.TenantID = auth.TenantID
			task.UserID = auth.UID
			if req.ProjectID != "" {
				task.Project = req.ProjectID
			}
		}

		// Execute using task executor (with fallback and JSON verification)
		parsed, err := h.taskExecutor.ExecuteStructuredTask(ctx, task)
		if err != nil {
			log.Printf("GenAIGenerate model mode (task executor) error: %v", err)
			status, code, msg, details := parseUpstreamError(err)
			_ = WriteError(w, status, code, msg, details)
			return
		}

		// Convert parsed result back to JSON string for response
		// Result can be map[string]interface{} or []interface{} depending on JSONSchema
		// SimpleJSONVerifier returns interface{}, so we marshal it directly
		jsonBytes, err := json.Marshal(parsed)
		if err != nil {
			log.Printf("GenAIGenerate model mode: failed to marshal result: %v", err)
			_ = WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to serialize response", nil)
			return
		}

		// Create response compatible with GenAIResponse format
		// We need to get model name from task executor result, but ExecuteStructuredTask doesn't return it
		// For now, use the requested model or fallback
		modelAlias := req.Model
		if modelAlias == "" {
			modelAlias = "gemini_fast" // fallback
		}

		// Convert alias to actual model name for response
		actualModelName := gen_combined.ConvertAliasToModel(modelAlias)

		// Estimate usage (task executor doesn't return detailed usage)
		outputChars := len(jsonBytes)
		outputTokens := outputChars / 4
		resp := gen_combined.GenAIResponse{
			Content: string(jsonBytes),
			Model:   actualModelName, // Use actual model name, not alias
			Usage: g.Usage{
				PromptTokens: inputTokens,
				OutputTokens: outputTokens,
				TotalTokens:  inputTokens + outputTokens,
			},
		}

		// Continue with response handling below
		_ = WriteJSON(w, http.StatusOK, resp)

		// Emit telemetry (best-effort)
		go func() {
			if auth := GetAuthInfo(r.Context()); auth != nil {
				_ = persistGenAITelemetry(r.Context(), auth.TenantID, map[string]any{
					"kind":      "genAI_telemetry",
					"mode":      "model_mode",
					"model":     resp.Model,
					"ts":        time.Now().UTC().Format(time.RFC3339Nano),
					"elapsedMs": time.Since(started).Milliseconds(),
					"usage":     resp.Usage,
					"params": map[string]any{
						"temperature":       req.Temperature,
						"maxOutputTokens":   req.MaxOutputTokens,
						"strictJSONEnabled": true,
					},
				})

				// Save usage metrics asynchronously
				if h.usageMetricsRepo != nil && h.modelManager != nil {
					go func() {
						metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						durationMs := int(time.Since(started).Milliseconds())
						projectID := req.ProjectID
						var projectIDPtr *string
						if projectID != "" {
							projectIDPtr = &projectID
						}
						_ = h.saveUsageMetric(metricCtx, auth, req, resp, projectIDPtr, nil, &durationMs)
					}()
				}
			}
		}()
		return
	}

	// Regular generation flow (unchanged for discuss mode)
	// Log request details for debugging 502 errors
	log.Printf("GenAIGenerate: starting regular flow (strictJSON=%v, model=%s, promptLen=%d)", req.StrictJSONEnabled, req.Model, len(req.Prompt))
	resp, err := h.client.GenAIGenerate(ctx, req)
	if err != nil {
		// Log upstream error body for diagnostics (e.g., Gemini 400 with details)
		log.Printf("GenAIGenerate upstream error: %v", err)
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
	log.Printf("GenAIGenerate: completed successfully (model=%s, contentLen=%d)", resp.Model, len(resp.Content))
	// Emit telemetry (best-effort)
	go func() {
		if auth := GetAuthInfo(r.Context()); auth != nil {
			_ = persistGenAITelemetry(r.Context(), auth.TenantID, map[string]any{
				"kind":      "genAI_telemetry",
				"mode":      "single",
				"model":     resp.Model,
				"ts":        time.Now().UTC().Format(time.RFC3339Nano),
				"elapsedMs": time.Since(started).Milliseconds(),
				"usage":     resp.Usage,
				"params": map[string]any{
					"temperature":        req.Temperature,
					"maxOutputTokens":    req.MaxOutputTokens,
					"webSearchEnabled":   req.WebSearchEnabled,
					"toolCallingEnabled": req.ToolCallingEnabled,
					"strictJSONEnabled":  req.StrictJSONEnabled,
				},
			})

			// Save usage metrics asynchronously with a new context
			if h.usageMetricsRepo != nil && h.modelManager != nil {
				go func() {
					metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					durationMs := int(time.Since(started).Milliseconds())
					_ = h.saveUsageMetric(metricCtx, auth, req, *resp, nil, nil, &durationMs)
				}()
			}
		}
	}()
	_ = WriteJSON(w, http.StatusOK, resp)
}

type genaiCompleteRequest struct {
	Prompt     string                 `json:"prompt"`
	Tools      []gen_gemini.Tool      `json:"tools"`
	Options    gen_gemini.Options     `json:"options"`
	JSONSchema map[string]interface{} `json:"jsonSchema,omitempty"`
	Messages   []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"messages,omitempty"`
	// Streaming context
	ProjectID       string `json:"projectId,omitempty"`
	StreamID        string `json:"streamId,omitempty"`
	ResearchEnabled bool   `json:"researchEnabled,omitempty"`
}

func (h *GenAIHandler) Complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	var req genaiCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	started := time.Now()
	resp, err := h.client.Generate(ctx, req.Prompt, req.Tools, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
	// Save usage metrics
	go func() {
		if auth := GetAuthInfo(r.Context()); auth != nil && h.usageMetricsRepo != nil && h.modelManager != nil {
			genaiResp := gen_combined.GenAIResponse(*resp)
			genaiReq := gen_combined.GenAIRequest{Model: req.Options.ModelOverride}
			var projID *string
			if req.ProjectID != "" {
				projID = &req.ProjectID
			}
			metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			durationMs := int(time.Since(started).Milliseconds())
			_ = h.saveUsageMetric(metricCtx, auth, genaiReq, genaiResp, projID, nil, &durationMs)
		}
	}()
	_ = WriteJSON(w, http.StatusOK, resp)
}

// CompleteWithTools executes tool loop using a simple registry.
func (h *GenAIHandler) CompleteWithTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	var req genaiCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}
	// simple empty registry by default; caller can still pass tool declarations but no runtime
	reg := gen_gemini.NewSimpleRegistry()
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	started := time.Now()
	resp, err := h.client.ExecuteWithTools(ctx, req.Prompt, req.Tools, reg, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
	// Save usage metrics
	go func() {
		if auth := GetAuthInfo(r.Context()); auth != nil && h.usageMetricsRepo != nil && h.modelManager != nil {
			genaiResp := gen_combined.GenAIResponse(*resp)
			genaiReq := gen_combined.GenAIRequest{Model: req.Options.ModelOverride}
			var projID *string
			if req.ProjectID != "" {
				projID = &req.ProjectID
			}
			metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			durationMs := int(time.Since(started).Milliseconds())
			_ = h.saveUsageMetric(metricCtx, auth, genaiReq, genaiResp, projID, nil, &durationMs)
		}
	}()
	_ = WriteJSON(w, http.StatusOK, resp)
}

// CompleteJSON forces JSON mode (optionally with response schema)
func (h *GenAIHandler) CompleteJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	var req genaiCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	started := time.Now()
	resp, err := h.client.GenerateJSON(ctx, req.Prompt, req.Tools, req.JSONSchema, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
	// Save usage metrics
	go func() {
		if auth := GetAuthInfo(r.Context()); auth != nil && h.usageMetricsRepo != nil && h.modelManager != nil {
			genaiResp := gen_combined.GenAIResponse(*resp)
			genaiReq := gen_combined.GenAIRequest{Model: req.Options.ModelOverride}
			var projID *string
			if req.ProjectID != "" {
				projID = &req.ProjectID
			}
			metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			durationMs := int(time.Since(started).Milliseconds())
			_ = h.saveUsageMetric(metricCtx, auth, genaiReq, genaiResp, projID, nil, &durationMs)
		}
	}()
	_ = WriteJSON(w, http.StatusOK, resp)
}

// CompleteJSONStrict enforces strict JSON according to optional response schema (provider-native strict mode)
func (h *GenAIHandler) CompleteJSONStrict(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	var req genaiCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	started := time.Now()
	resp, err := h.client.GenerateJSONStrict(ctx, req.Prompt, req.Tools, req.JSONSchema, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
	// Save usage metrics
	go func() {
		if auth := GetAuthInfo(r.Context()); auth != nil && h.usageMetricsRepo != nil && h.modelManager != nil {
			genaiResp := gen_combined.GenAIResponse(*resp)
			genaiReq := gen_combined.GenAIRequest{Model: req.Options.ModelOverride}
			var projID *string
			if req.ProjectID != "" {
				projID = &req.ProjectID
			}
			metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			durationMs := int(time.Since(started).Milliseconds())
			_ = h.saveUsageMetric(metricCtx, auth, genaiReq, genaiResp, projID, nil, &durationMs)
		}
	}()
	_ = WriteJSON(w, http.StatusOK, resp)
}

// AdhocJSON: temporary endpoint for FE to test arbitrary prompts with model selection and grounding, returning JSON
func (h *GenAIHandler) AdhocJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	var req genaiCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}
	req.Options.ResponseFormat = "json"
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	started := time.Now()
	resp, err := h.client.GenerateJSON(ctx, req.Prompt, nil, nil, req.Options)
	if err != nil {
		_ = WriteError(w, http.StatusBadGateway, "GENAI_ERROR", err.Error(), nil)
		return
	}
	// Save usage metrics
	go func() {
		if auth := GetAuthInfo(r.Context()); auth != nil && h.usageMetricsRepo != nil && h.modelManager != nil {
			genaiResp := gen_combined.GenAIResponse(*resp)
			genaiReq := gen_combined.GenAIRequest{Model: req.Options.ModelOverride}
			var projID *string
			if req.ProjectID != "" {
				projID = &req.ProjectID
			}
			metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			durationMs := int(time.Since(started).Milliseconds())
			_ = h.saveUsageMetric(metricCtx, auth, genaiReq, genaiResp, projID, nil, &durationMs)
		}
	}()
	var parsed interface{}
	valid := true
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		valid = false
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"model":     resp.Model,
		"usage":     resp.Usage,
		"validJson": valid,
		"json":      parsed,
		"raw":       resp.Content,
	})
}

// WithPublisher allows wiring realtime Publisher for streaming events
func (h *GenAIHandler) WithPublisher(p *realtime.Publisher) *GenAIHandler {
	if p != nil {
		h.publisher = rtPublisherAdapter{p: p}
	}
	return h
}

// rtPublisherAdapter adapts realtime.Publisher to local Publisher interface
type rtPublisherAdapter struct{ p *realtime.Publisher }

func (a rtPublisherAdapter) PublishJSON(ctx context.Context, et interface{}, tenantID string, payload interface{}) error {
	var eventType realtime.EventType
	switch v := et.(type) {
	case string:
		eventType = realtime.EventType(v)
	case realtime.EventType:
		eventType = v
	default:
		// This should not happen. Log an error and return.
		log.Printf("rtPublisherAdapter: unexpected event type %T", et)
		return nil
	}
	return a.p.PublishJSON(ctx, eventType, tenantID, payload)
}

// Stream publishes incremental events to Pub/Sub for the tenant; front receives via WS
func (h *GenAIHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		_ = WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed", nil)
		return
	}
	auth := GetAuthInfo(r.Context())
	if auth == nil || auth.TenantID == "" {
		_ = WriteUnauthorized(w, "Authentication required")
		return
	}
	if h.publisher == nil {
		_ = WriteInternalError(w, "Realtime publisher is not configured")
		return
	}
	var req genaiCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = WriteBadRequest(w, "INVALID_JSON", "Invalid JSON body")
		return
	}

	// Determine routing context
	projectID := strings.TrimSpace(req.ProjectID)
	streamID := strings.TrimSpace(req.StreamID)

	// CRITICAL: Validate projectID is present (required for proper event routing)
	if projectID == "" {
		log.Printf("[GenAI Stream] WARNING: Empty projectID in request! This may cause events to be broadcast to all tenant connections instead of specific project")
		// Don't fail the request, but log warning for debugging
	}

	// Check for Research mode - if enabled, use research pipeline instead of regular streaming
	if req.ResearchEnabled {
		if streamID == "" {
			streamID = uuid.NewString()
		}
		log.Printf("[GenAI Stream] Research mode enabled (projectId=%s streamId=%s)", projectID, streamID)

		// Determine model - use ModelOverride if provided, otherwise fallback to default based on profile
		model := strings.TrimSpace(req.Options.ModelOverride)
		if model == "" {
			// Fallback to default model based on profile (same logic as regular stream)
			if req.Options.Profile == gen_gemini.ProfileQuality {
				model = "gemini-2.5-pro" // Default quality model
			} else {
				model = "gemini-2.5-flash-lite" // Default fast model
			}
		}

		// Convert genaiCompleteRequest to GenAIRequest for research mode
		genaiReq := gen_combined.GenAIRequest{
			Model:             model,
			Prompt:            req.Prompt,
			SystemInstruction: req.Options.SystemInstruction,
			Temperature:       req.Options.Temperature,
			MaxOutputTokens:   req.Options.MaxOutputTokens,
			ResearchEnabled:   true,
			ProjectID:         projectID,
			StreamID:          streamID,
		}

		// Execute research mode (it will publish events via WebSocket)
		_, err := h.executeResearchMode(r.Context(), genaiReq, projectID, streamID)
		if err != nil {
			log.Printf("[GenAI Stream] Research mode error: %v", err)
			status, code, msg, details := parseUpstreamError(err)
			_ = WriteError(w, status, code, msg, details)
			return
		}
		// Return success - events are published via WebSocket
		_ = WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "researchMode": true})
		return
	}

	// Diagnostics: log model override and key options to verify routing
	log.Printf("genai.stream: options modelOverride=%q grounding=%v temp=%.2f maxTok=%d", strings.TrimSpace(req.Options.ModelOverride), req.Options.GroundingEnabled, req.Options.Temperature, req.Options.MaxOutputTokens)
	if streamID == "" {
		streamID = uuid.NewString()
	}

	// DEBUG: log planned stream start with routing context before publishing
	log.Printf("genai.stream: preparing start tenantId=%s projectId=%s streamId=%s", auth.TenantID, projectID, streamID)
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	started := time.Now()
	_ = h.publisher.PublishJSON(ctx, "genai.stream.start", auth.TenantID, map[string]any{
		"ts":        time.Now().UnixMilli(),
		"projectId": projectID,
		"streamId":  streamID,
	})
	// Optionally enrich system instruction with provided messages (conversation context)
	// Use the same function as non-streaming to ensure identical behavior
	if len(req.Messages) > 0 {
		// Convert genaiCompleteRequest.Messages to gen_combined.Message format
		messages := make([]gen_combined.Message, 0, len(req.Messages))
		for _, m := range req.Messages {
			messages = append(messages, gen_combined.Message{
				Role: m.Role,
				Text: m.Text,
			})
		}
		req.Options.SystemInstruction = gen_combined.EnrichSystemInstructionWithMessages(req.Options.SystemInstruction, messages)
	}

	// Track per-stream chunk count for diagnostics
	chunks := 0
	onEvt := func(e gen_gemini.StreamEvent) error {
		switch e.Type {
		case "chunk":
			// increment local chunk counter
			chunks++
			textLen := len(e.Text)
			log.Printf("[GenAI Stream] Publishing genai.stream.chunk event (projectId=%s streamId=%s textLength=%d chunks=%d)", projectID, streamID, textLen, chunks)
			err := h.publisher.PublishJSON(ctx, "genai.stream.chunk", auth.TenantID, map[string]any{
				"projectId":  projectID,
				"streamId":   streamID,
				"text":       e.Text,
				"chunkIndex": chunks, // Add sequence number for client-side ordering
			})
			if err != nil {
				// CRITICAL: Don't return error here - it would stop the stream!
				// Log the error but continue streaming to avoid losing chunks
				log.Printf("[GenAI Stream] ERROR publishing chunk event (non-fatal, continuing stream): %v (projectId=%s streamId=%s chunks=%d)", err, projectID, streamID, chunks)
			} else {
				log.Printf("[GenAI Stream] Successfully published chunk event (projectId=%s streamId=%s chunks=%d)", projectID, streamID, chunks)
			}
			// Always return nil to continue streaming even if publish failed
			return nil
		case "tool_call":
			err := h.publisher.PublishJSON(ctx, "genai.stream.tool_call", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"toolCalls": e.ToolCalls,
			})
			if err != nil {
				// Log error but don't stop stream - tool_call events are not critical
				log.Printf("[GenAI Stream] ERROR publishing tool_call event (non-fatal): %v", err)
			}
			// Continue stream even if publish failed
			return nil
		case "tool_result":
			err := h.publisher.PublishJSON(ctx, "genai.stream.tool_result", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"toolCalls": e.ToolCalls,
			})
			if err != nil {
				// Log error but don't stop stream - tool_result events are not critical
				log.Printf("[GenAI Stream] ERROR publishing tool_result event (non-fatal): %v", err)
			}
			// Continue stream even if publish failed
			return nil
		case "error":
			log.Printf("[GenAI Stream] Stream error received: %s (projectId=%s streamId=%s)", e.Error, projectID, streamID)
			err := h.publisher.PublishJSON(ctx, "genai.stream.error", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"error":     e.Error,
			})
			if err != nil {
				log.Printf("[GenAI Stream] ERROR publishing error event: %v", err)
			}
			return err
		case "done":
			// include usage/attributions/searchQueries/sources if accumulated
			payload := map[string]any{
				"ts":          time.Now().UnixMilli(),
				"projectId":   projectID,
				"streamId":    streamID,
				"totalChunks": chunks, // Add total chunk count for validation
			}
			log.Printf("[GenAI Stream] Publishing genai.stream.done event (projectId=%s streamId=%s chunks=%d)", projectID, streamID, chunks)
			if chunks == 0 {
				log.Printf("[GenAI Stream] WARNING: done event with zero chunks! This may indicate streaming is not working (projectId=%s streamId=%s)", projectID, streamID)
			}
			// Diagnostics: log grounded counts for visibility
			log.Printf("genai.stream: grounded counts queries=%d sources=%d", len(e.SearchQueries), len(e.Sources))
			// Always include usage, even if zero (for tracking purposes)
			// Log usage for debugging (especially for counterpoint agents)
			log.Printf("genai.stream: usage model=%s promptTokens=%d outputTokens=%d totalTokens=%d", e.Model, e.Usage.PromptTokens, e.Usage.OutputTokens, e.Usage.TotalTokens)
			payload["usage"] = e.Usage
			if len(e.Attributions) > 0 {
				payload["attributions"] = e.Attributions
			}
			if len(e.SearchQueries) > 0 {
				payload["searchQueries"] = e.SearchQueries
			}
			if len(e.Sources) > 0 {
				payload["sources"] = e.Sources
			}
			// Best-effort fallback: if Gemini with grounding requested returned no sources, do a quick grounded pass to fetch sources
			if (len(e.Sources) == 0) && req.Options.GroundingEnabled {
				mo := strings.ToLower(strings.TrimSpace(req.Options.ModelOverride))
				if strings.HasPrefix(mo, "gemini_") {
					go func(prompt string, model string) {
						defer func() {
							if r := recover(); r != nil {
								log.Printf("[GenAI] PANIC in grounding fallback goroutine (model=%s): %v", model, r)
							}
						}()
						gopts := gen_gemini.Options{ModelOverride: model, GroundingEnabled: true, MaxOutputTokens: 64}
						if gr, gerr := h.client.Generate(ctx, prompt, nil, gopts); gerr == nil {
							if len(gr.Sources) > 0 {
								// Publish an extra meta event with discovered sources
								_ = h.publisher.PublishJSON(ctx, "genai.stream.done", auth.TenantID, map[string]any{
									"ts":        time.Now().UnixMilli(),
									"projectId": projectID,
									"streamId":  streamID,
									"sources":   gr.Sources,
								})
							}
						}
					}(req.Prompt, req.Options.ModelOverride)
				}
			}
			// Log per-stream diagnostics: chunks and latency
			log.Printf("genai.stream: completed tenantId=%s projectId=%s streamId=%s chunks=%d elapsedMs=%d", auth.TenantID, projectID, streamID, chunks, time.Since(started).Milliseconds())
			if chunks == 0 {
				log.Printf("genai.stream: WARNING - stream completed with zero chunks! This may indicate a problem with streaming (tenantId=%s projectId=%s streamId=%s)", auth.TenantID, projectID, streamID)
			}
			// Fire telemetry (best-effort)
			go func() {
				_ = persistGenAITelemetry(r.Context(), auth.TenantID, map[string]any{
					"kind":      "genAI_telemetry",
					"mode":      "stream",
					"model":     e.Model,
					"ts":        time.Now().UTC().Format(time.RFC3339Nano),
					"elapsedMs": time.Since(started).Milliseconds(),
					"usage":     e.Usage,
					"params": map[string]any{
						"temperature":      req.Options.Temperature,
						"maxOutputTokens":  req.Options.MaxOutputTokens,
						"webSearchEnabled": req.Options.GroundingEnabled,
					},
				})

				// Save usage metrics asynchronously with a new context
				// Always save metrics, even if usage is zero (for tracking purposes)
				if h.usageMetricsRepo != nil && h.modelManager != nil {
					// Convert StreamEvent Usage to GenerateResponse format
					genaiResp := gen_combined.GenAIResponse(gen_gemini.GenerateResponse{
						Model: e.Model,
						Usage: e.Usage,
					})
					genaiReq := gen_combined.GenAIRequest{
						Model: req.Options.ModelOverride,
					}
					var projID *string
					if projectID != "" {
						projID = &projectID
					}
					go func() {
						metricCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						durationMs := int(time.Since(started).Milliseconds())
						if err := h.saveUsageMetric(metricCtx, auth, genaiReq, genaiResp, projID, nil, &durationMs); err != nil {
							log.Printf("[GenAI Stream] Failed to save usage metric: %v (model=%s, promptTokens=%d, outputTokens=%d)", err, e.Model, e.Usage.PromptTokens, e.Usage.OutputTokens)
						} else {
							log.Printf("[GenAI Stream] Saved usage metric (model=%s, promptTokens=%d, outputTokens=%d, totalTokens=%d)", e.Model, e.Usage.PromptTokens, e.Usage.OutputTokens, e.Usage.TotalTokens)
						}
					}()
				} else {
					log.Printf("[GenAI Stream] Usage metrics not saved: repo=%v manager=%v", h.usageMetricsRepo != nil, h.modelManager != nil)
				}
			}()
			if err := h.publisher.PublishJSON(ctx, "genai.stream.done", auth.TenantID, payload); err != nil {
				log.Printf("[GenAI Stream] Failed to publish genai.stream.done event: %v (projectId=%s streamId=%s)", err, projectID, streamID)
			} else {
				log.Printf("[GenAI Stream] Published genai.stream.done event (projectId=%s streamId=%s)", projectID, streamID)
			}
			return nil
		case "start":
			return nil
		default:
			return nil
		}
	}
	var err error
	// Check if tool usage is enabled:
	// - If tools is explicitly [] (empty array), frontend signals to enable tool registration
	// - If tools is nil/undefined, frontend signals to disable tool registration
	// - If tools has items, use them as-is
	// We only enable backend tool registration when tools is explicitly [] (empty array), not just when projectID is present
	toolUsageEnabled := req.Tools != nil && len(req.Tools) == 0
	if toolUsageEnabled {
		log.Printf("[GenAI Stream] Tool usage enabled (empty tools array): projectID=%s", projectID)
		reg := gen_gemini.NewSimpleRegistry()

		// Register CoreDocs tools if tool usage is enabled and dependencies are available
		// We detect tool usage mode by checking if projectID is present
		if projectID != "" && h.coreDocs != nil && h.interactions != nil && h.docsRepo != nil && h.chunksRepo != nil && h.emb != nil {
			// Create CoreDocs tool executor
			coreDocsExecutor := NewCoreDocsToolExecutor(
				h.coreDocs,
				h.interactions,
				h.docsRepo,
				h.chunksRepo,
				h.emb,
				h.storage,
				h.bucket,
				h.publisher,
				auth.TenantID,
				projectID,
				streamID,
				log.New(os.Stdout, "[CoreDocsTools] ", log.LstdFlags),
			)

			// Register CoreDocs tools in registry
			reg.Register("search_coredocs", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				return coreDocsExecutor.Execute(ctx, "search_coredocs", args)
			})
			reg.Register("create_coredoc", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				return coreDocsExecutor.Execute(ctx, "create_coredoc", args)
			})
			reg.Register("update_coredoc", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				return coreDocsExecutor.Execute(ctx, "update_coredoc", args)
			})
			reg.Register("update_coredoc_fragment", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				return coreDocsExecutor.Execute(ctx, "update_coredoc_fragment", args)
			})
			reg.Register("list_coredocs", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				return coreDocsExecutor.Execute(ctx, "list_coredocs", args)
			})
			reg.Register("get_coredoc", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				return coreDocsExecutor.Execute(ctx, "get_coredoc", args)
			})

			// Add CoreDocs tool schemas to req.Tools if not already present
			toolNames := make(map[string]bool)
			for _, t := range req.Tools {
				toolNames[t.Name] = true
			}

			coreDocsTools := getCoreDocsToolSchemas()
			for _, tool := range coreDocsTools {
				if !toolNames[tool.Name] {
					req.Tools = append(req.Tools, tool)
				}
			}

			// Cleanup edit sessions when stream completes
			defer func() {
				coreDocsExecutor.CleanupEditSessions(ctx)
			}()
		}

		// Register Graph tools if enabled and interactions repo is available
		// Log diagnostics for graph tools registration
		log.Printf("[GraphTools] Registration check: projectID=%q (empty=%v), graphEnabled=%v, interactions=%v (nil=%v)",
			projectID, projectID == "", h.graphEnabled, h.interactions != nil, h.interactions == nil)

		if projectID != "" && h.graphEnabled && h.interactions != nil {
			log.Printf("[GraphTools] Registering graph tools for projectID=%s streamID=%s", projectID, streamID)
			// Create Graph tool executor
			graphExecutor := NewGraphToolExecutor(
				h.interactions,
				h.publisher,
				auth.TenantID,
				projectID,
				streamID,
				log.New(os.Stdout, "[GraphTools] ", log.LstdFlags),
			)

			// Register Graph tools in registry
			reg.Register("create_graph", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				log.Printf("[GraphTools] create_graph tool called")
				result, err := graphExecutor.Execute(ctx, "create_graph", args)
				if err != nil {
					log.Printf("[GraphTools] ERROR in create_graph: %v", err)
				} else {
					log.Printf("[GraphTools] create_graph succeeded")
				}
				return result, err
			})
			reg.Register("update_graph", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
				log.Printf("[GraphTools] update_graph tool called")
				result, err := graphExecutor.Execute(ctx, "update_graph", args)
				if err != nil {
					log.Printf("[GraphTools] ERROR in update_graph: %v", err)
				} else {
					log.Printf("[GraphTools] update_graph succeeded")
				}
				return result, err
			})

			// Add Graph tool schemas to req.Tools if not already present
			toolNames := make(map[string]bool)
			for _, t := range req.Tools {
				toolNames[t.Name] = true
			}

			graphTools := getGraphToolSchemas()
			addedCount := 0
			for _, tool := range graphTools {
				if !toolNames[tool.Name] {
					req.Tools = append(req.Tools, tool)
					addedCount++
					log.Printf("[GraphTools] Added tool schema: %s", tool.Name)
				} else {
					log.Printf("[GraphTools] Tool schema already present: %s", tool.Name)
				}
			}
			log.Printf("[GraphTools] Graph tools registered successfully: added %d tool schemas, total tools=%d", addedCount, len(req.Tools))
		} else {
			log.Printf("[GraphTools] Graph tools NOT registered: projectID empty=%v, graphEnabled=%v, interactions nil=%v",
				projectID == "", h.graphEnabled, h.interactions == nil)
		}

		// Register Web Search tool (uses Tavily API)
		// Use shared TavilyClient if available, otherwise create from env
		tavilyClientForTools := h.tavilyClient
		if tavilyClientForTools == nil {
			tavilyClientForTools = GetTavilyClientFromEnv()
		}
		webSearchExecutor := NewWebSearchToolExecutor(
			log.New(os.Stdout, "[WebSearchTools] ", log.LstdFlags),
			tavilyClientForTools,
			h.usageMetricsRepo, // Pass usage metrics repo for tracking Tavily usage
		)

		// Register Web Search tool in registry
		reg.Register("web_search", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			return webSearchExecutor.Execute(ctx, "web_search", args)
		})

		// Register Knowledge Search tool (uses Wikidata API)
		// Use shared WikidataClient if available, otherwise create from env
		wikidataClientForTools := h.wikidataClient
		if wikidataClientForTools == nil {
			wikidataClientForTools = GetWikidataClientFromEnv()
		}
		knowledgeSearchExecutor := NewKnowledgeSearchToolExecutor(
			log.New(os.Stdout, "[KnowledgeSearchTools] ", log.LstdFlags),
			wikidataClientForTools,
		)

		// Register Knowledge Search tool in registry
		reg.Register("knowledge_search", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			return knowledgeSearchExecutor.Execute(ctx, "knowledge_search", args)
		})

		// Add Web Search and Knowledge Search tool schemas to req.Tools if not already present
		toolNames := make(map[string]bool)
		for _, t := range req.Tools {
			toolNames[t.Name] = true
		}

		webSearchTools := getWebSearchToolSchemas()
		for _, tool := range webSearchTools {
			if !toolNames[tool.Name] {
				req.Tools = append(req.Tools, tool)
			}
		}

		knowledgeSearchTools := getKnowledgeSearchToolSchemas()
		for _, tool := range knowledgeSearchTools {
			if !toolNames[tool.Name] {
				req.Tools = append(req.Tools, tool)
			}
		}

		// Register Ontology tools if enabled and repositories are available
		if projectID != "" && h.ontologyEnabled && h.entityRepo != nil && h.eventRepo != nil && h.claimRepo != nil {
			// Create Ontology tool executor
			ontologyExecutor := NewOntologyToolExecutor(
				auth.TenantID,
				h.entityRepo,
				h.eventRepo,
				h.claimRepo,
				h.relationRepo,
				h.trendRepo,
				h.causalLinkRepo,
				h.metricStateRepo,
				h.uncertaintyRepo,
				h.sourceRepo,
				h.trustRepo,
				h.conceptRepo,
				log.New(os.Stdout, "[OntologyTools] ", log.LstdFlags),
			)

			// Register all ontology tools in registry
			registerOntologyTool := func(name string) {
				reg.Register(name, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
					return ontologyExecutor.Execute(ctx, name, args)
				})
			}
			registerOntologyTool("entity_search")
			registerOntologyTool("entity_profile")
			registerOntologyTool("event_timeline")
			registerOntologyTool("event_detail")
			registerOntologyTool("claim_query")
			registerOntologyTool("uncertainty_list")
			registerOntologyTool("relation_graph")
			registerOntologyTool("causal_links")
			registerOntologyTool("trend_get")
			registerOntologyTool("metric_latest")
			registerOntologyTool("scenario_run")
			registerOntologyTool("risk_top")
			registerOntologyTool("source_profile")
			registerOntologyTool("trust_recalculate")
			registerOntologyTool("document_search")
			registerOntologyTool("entity_enrich")

			// Add Ontology tool schemas to req.Tools if not already present
			toolNames = make(map[string]bool)
			for _, t := range req.Tools {
				toolNames[t.Name] = true
			}

			ontologyTools := GetOntologyToolSchemas()
			for _, tool := range ontologyTools {
				if !toolNames[tool.Name] {
					req.Tools = append(req.Tools, tool)
				}
			}
		}

		// Wrap registry with retryable executor for self-correction
		retryableReg := gen_retry.NewRetryableToolExecutor(reg, h.client)

		// Set up callbacks for retry and correction events
		retryableReg.SetCallbacks(
			func(toolName string, attempt int, error *gen_retry.ToolCallError) {
				// Notify frontend about retry attempt
				log.Printf("[GenAI Stream] Tool retry: tool=%s attempt=%d errorType=%s error=%s", toolName, attempt, error.Type, error.Message)
				_ = h.publisher.PublishJSON(ctx, "genai.stream.tool_retry", auth.TenantID, map[string]any{
					"projectId": projectID,
					"streamId":  streamID,
					"toolName":  toolName,
					"attempt":   attempt,
					"errorType": string(error.Type),
					"error":     error.Message,
				})
			},
			func(toolName string, attempt int, correctedArgs json.RawMessage) {
				// Notify frontend about correction
				log.Printf("[GenAI Stream] Tool corrected: tool=%s attempt=%d", toolName, attempt)
				_ = h.publisher.PublishJSON(ctx, "genai.stream.tool_correction", auth.TenantID, map[string]any{
					"projectId":     projectID,
					"streamId":      streamID,
					"toolName":      toolName,
					"attempt":       attempt,
					"correctedArgs": string(correctedArgs),
				})
			},
		)

		log.Printf("[GenAI Stream] Starting stream with tools (projectId=%s streamId=%s toolsCount=%d)", projectID, streamID, len(req.Tools))
		err = h.client.StreamWithTools(ctx, req.Prompt, req.Tools, retryableReg, req.Options, onEvt)
	} else {
		log.Printf("[GenAI Stream] Starting stream without tools (projectId=%s streamId=%s)", projectID, streamID)
		err = h.client.Stream(ctx, req.Prompt, req.Tools, req.Options, onEvt)
	}
	if err != nil {
		log.Printf("[GenAI Stream] Stream returned error: %v (projectId=%s streamId=%s chunks=%d)", err, projectID, streamID, chunks)
		status, code, msg, details := parseUpstreamError(err)
		_ = h.publisher.PublishJSON(ctx, "genai.stream.error", auth.TenantID, map[string]any{
			"projectId": projectID,
			"streamId":  streamID,
			"error":     msg,
		})
		_ = WriteError(w, status, code, msg, details)
		return
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// parseUpstreamError extracts provider/status/body from error string
func parseUpstreamError(err error) (int, string, string, interface{}) {
	if err == nil {
		return http.StatusBadGateway, "GENAI_ERROR", "Upstream error", nil
	}
	s := err.Error()
	re := regexp.MustCompile(`^(openai|gemini)[^\d]*(\d{3}):\s*([\s\S]+)$`)
	if m := re.FindStringSubmatch(s); len(m) == 4 {
		prov := m[1]
		codeStr := m[2]
		body := strings.TrimSpace(m[3])
		status, _ := strconv.Atoi(codeStr)
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		code := strings.ToUpper(prov) + "_UPSTREAM_ERROR"
		details := map[string]any{"provider": prov, "raw": body}
		return status, code, s, details
	}
	return http.StatusBadGateway, "GENAI_ERROR", s, map[string]any{"raw": s}
}

// saveUsageMetric saves a usage metric record
// durationMs is optional (can be nil if timing not available)
func (h *GenAIHandler) saveUsageMetric(ctx context.Context, auth *AuthInfo, req gen_combined.GenAIRequest, resp gen_combined.GenAIResponse, projectID *string, interactionID *string, durationMs *int) error {
	if h.usageMetricsRepo == nil || h.modelManager == nil {
		return nil
	}

	// Convert resp.Model from alias to actual model name if needed
	// resp.Model might contain an alias (e.g., "gemini_fast") instead of actual model name (e.g., "gemini-2.5-flash-lite")
	actualModelName := gen_combined.ConvertAliasToModel(resp.Model)
	log.Printf("[saveUsageMetric] Model mapping: req.Model=%q, resp.Model=%q, actualModelName=%q", req.Model, resp.Model, actualModelName)

	// Determine provider from actual model name
	provider := h.determineProvider(actualModelName)

	// Calculate cost using modelManager (which handles tiered pricing for Gemini)
	cost, err := h.modelManager.CalculateProcessingCost(actualModelName, resp.Usage.PromptTokens, resp.Usage.OutputTokens)
	if err != nil {
		// If model not found in manager, fallback to simple calculation
		log.Printf("[saveUsageMetric] Model %s not found in modelManager, using fallback pricing", actualModelName)
		cost = h.calculateFallbackCost(provider, actualModelName, resp.Usage.PromptTokens, resp.Usage.OutputTokens)
	}

	// Determine model alias if available
	modelAlias := &req.Model
	if req.Model == "" {
		modelAlias = nil
	} else {
		// Normalize alias for consistency
		normalizedAlias := strings.ToLower(strings.TrimSpace(req.Model))
		normalizedAlias = strings.ReplaceAll(normalizedAlias, "-", "_")
		modelAlias = &normalizedAlias
	}

	useCase := "chat"
	metric := &repo.UsageMetric{
		TenantID:      auth.TenantID,
		UserID:        auth.UID,
		ProjectID:     projectID,
		InteractionID: interactionID,
		Provider:      provider,
		Model:         actualModelName, // Use actual model name, not alias
		ModelAlias:    modelAlias,      // Store the alias separately
		InputTokens:   resp.Usage.PromptTokens,
		OutputTokens:  resp.Usage.OutputTokens,
		TotalTokens:   resp.Usage.TotalTokens,
		CostUSD:       cost,
		DurationMs:    durationMs,
		TaskType:      nil,        // Can be set from context if needed
		UseCase:       &useCase,   // Chat use case for user conversations
		Kind:          "included", // Default to included, can be changed based on plan
	}

	_, err = h.usageMetricsRepo.Create(ctx, metric)
	return err
}

// calculateFallbackCost calculates cost when model is not in modelManager
// Uses approximate pricing based on provider
func (h *GenAIHandler) calculateFallbackCost(provider, modelName string, inputTokens, outputTokens int) float64 {
	modelLower := strings.ToLower(modelName)

	switch provider {
	case "gemini":
		// Gemini tiered pricing: first 128K at one rate, after at 50% discount
		// Prices match model_manager.go (max prices for first tier)
		const threshold = 128000
		var inputPriceTier1, inputPriceTier2, outputPriceTier1, outputPriceTier2 float64

		// Check more specific models first (flash-lite before flash)
		if strings.Contains(modelLower, "flash-lite") {
			inputPriceTier1 = 0.00000010   // $0.10 / 1M (from model_manager.go)
			inputPriceTier2 = 0.00000005   // $0.05 / 1M (50% discount after threshold)
			outputPriceTier1 = 0.00000041  // $0.41 / 1M (from model_manager.go)
			outputPriceTier2 = 0.000000205 // $0.205 / 1M (50% discount after threshold)
		} else if strings.Contains(modelLower, "pro") {
			inputPriceTier1 = 0.00000250  // $2.50 / 1M (from model_manager.go)
			inputPriceTier2 = 0.00000125  // $1.25 / 1M (50% discount after threshold)
			outputPriceTier1 = 0.00001500 // $15.00 / 1M (from model_manager.go)
			outputPriceTier2 = 0.00000750 // $7.50 / 1M (50% discount after threshold)
		} else { // flash (default)
			inputPriceTier1 = 0.00000030  // $0.30 / 1M (from model_manager.go)
			inputPriceTier2 = 0.00000015  // $0.15 / 1M (50% discount after threshold)
			outputPriceTier1 = 0.00000250 // $2.50 / 1M (from model_manager.go)
			outputPriceTier2 = 0.00000125 // $1.25 / 1M (50% discount after threshold)
		}

		var inputCost float64
		if inputTokens <= threshold {
			inputCost = float64(inputTokens) * inputPriceTier1
		} else {
			inputCost = float64(threshold)*inputPriceTier1 + float64(inputTokens-threshold)*inputPriceTier2
		}

		var outputCost float64
		if outputTokens <= threshold {
			outputCost = float64(outputTokens) * outputPriceTier1
		} else {
			outputCost = float64(threshold)*outputPriceTier1 + float64(outputTokens-threshold)*outputPriceTier2
		}

		return inputCost + outputCost
	case "openai":
		// OpenAI pricing - check specific models first
		var inputPrice, outputPrice float64
		switch {
		case strings.Contains(modelLower, "gpt-5-nano"):
			inputPrice = 0.00000005  // $0.05 / 1M
			outputPrice = 0.00000040 // $0.40 / 1M
		case strings.Contains(modelLower, "gpt-5-mini"):
			inputPrice = 0.00000025  // $0.25 / 1M
			outputPrice = 0.00000200 // $2.00 / 1M
		case strings.Contains(modelLower, "gpt-5.2"):
			inputPrice = 0.00000175  // $1.75 / 1M
			outputPrice = 0.00001400 // $14.00 / 1M
		case strings.Contains(modelLower, "gpt-5.1"):
			inputPrice = 0.00000100  // $1.00 / 1M
			outputPrice = 0.00000800 // $8.00 / 1M
		case strings.Contains(modelLower, "gpt-5"):
			inputPrice = 0.00000100  // $1.00 / 1M
			outputPrice = 0.00000800 // $8.00 / 1M
		case strings.Contains(modelLower, "gpt-4.1-mini"):
			inputPrice = 0.0000004  // $0.40 / 1M
			outputPrice = 0.0000016 // $1.60 / 1M
		case strings.Contains(modelLower, "gpt-4o-mini"):
			inputPrice = 0.00000060  // $0.60 / 1M
			outputPrice = 0.00000240 // $2.40 / 1M
		case strings.Contains(modelLower, "gpt-4o-search"):
			inputPrice = 0.0000050  // $5 / 1M
			outputPrice = 0.0000200 // $20 / 1M
		case strings.Contains(modelLower, "gpt-4"):
			// Generic GPT-4 models (fallback for older models)
			inputPrice = 0.00003  // $30 / 1M (approximate)
			outputPrice = 0.00006 // $60 / 1M (approximate)
		default:
			// Default to gpt-5-nano pricing for unknown OpenAI models
			inputPrice = 0.00000005
			outputPrice = 0.00000040
		}
		return float64(inputTokens)*inputPrice + float64(outputTokens)*outputPrice
	case "anthropic":
		// Anthropic Claude pricing - check specific models first
		var inputPrice, outputPrice float64
		switch {
		case strings.Contains(modelLower, "claude-opus"):
			inputPrice = 0.000015  // $15 / 1M
			outputPrice = 0.000075 // $75 / 1M
		case strings.Contains(modelLower, "claude-haiku"):
			inputPrice = 0.00000025  // $0.25 / 1M
			outputPrice = 0.00000125 // $1.25 / 1M
		case strings.Contains(modelLower, "claude-sonnet") || strings.Contains(modelLower, "claude"):
			// Default Claude Sonnet pricing
			inputPrice = 0.000003  // $3 / 1M
			outputPrice = 0.000015 // $15 / 1M
		default:
			// Fallback to Sonnet pricing
			inputPrice = 0.000003
			outputPrice = 0.000015
		}
		return float64(inputTokens)*inputPrice + float64(outputTokens)*outputPrice
	case "grok":
		// Grok pricing - check specific models first
		var inputPrice, outputPrice float64
		switch {
		case strings.Contains(modelLower, "grok-3-mini"):
			inputPrice = 0.00000030  // $0.30 / 1M
			outputPrice = 0.00000050 // $0.50 / 1M
		case strings.Contains(modelLower, "grok-4-1-fast-non-reasoning"):
			inputPrice = 0.00000020  // $0.20 / 1M
			outputPrice = 0.00000050 // $0.50 / 1M
		case strings.Contains(modelLower, "grok-4-1-fast-reasoning"):
			inputPrice = 0.00000020  // $0.20 / 1M
			outputPrice = 0.00000050 // $0.50 / 1M
		case strings.Contains(modelLower, "grok-4-fast-non-reasoning"):
			inputPrice = 0.00000020  // $0.20 / 1M (legacy)
			outputPrice = 0.00000050 // $0.50 / 1M (legacy)
		case strings.Contains(modelLower, "grok-4-fast-reasoning"):
			inputPrice = 0.00000020  // $0.20 / 1M (legacy)
			outputPrice = 0.00000050 // $0.50 / 1M (legacy)
		case strings.Contains(modelLower, "grok-4") || strings.Contains(modelLower, "grok-3"):
			// Grok 4 and Grok 3 (premium models)
			inputPrice = 0.000003  // $3 / 1M
			outputPrice = 0.000015 // $15 / 1M
		default:
			// Default to fast model pricing
			inputPrice = 0.00000020
			outputPrice = 0.00000050
		}
		return float64(inputTokens)*inputPrice + float64(outputTokens)*outputPrice
	case "deepseek":
		// DeepSeek pricing - use chat/reasoner pricing
		var inputPrice, outputPrice float64
		if strings.Contains(modelLower, "reasoner") {
			// Reasoner models might have different pricing, use chat pricing for now
			inputPrice = 0.00000027  // $0.27 / 1M
			outputPrice = 0.00000110 // $1.10 / 1M
		} else {
			// Default DeepSeek Chat pricing
			inputPrice = 0.00000027  // $0.27 / 1M
			outputPrice = 0.00000110 // $1.10 / 1M
		}
		return float64(inputTokens)*inputPrice + float64(outputTokens)*outputPrice
	default:
		// Very rough estimate: $1 per 1M tokens
		return float64(inputTokens+outputTokens) * 0.000001
	}
}

// determineProvider determines provider from model name
func (h *GenAIHandler) determineProvider(modelName string) string {
	modelLower := strings.ToLower(modelName)
	if strings.HasPrefix(modelLower, "gpt-") || strings.HasPrefix(modelLower, "o1-") || strings.HasPrefix(modelLower, "o3-") {
		return "openai"
	}
	if strings.HasPrefix(modelLower, "claude-") {
		return "anthropic"
	}
	if strings.HasPrefix(modelLower, "grok-") {
		return "grok"
	}
	if strings.HasPrefix(modelLower, "deepseek-") {
		return "deepseek"
	}
	if strings.HasPrefix(modelLower, "gemini-") {
		return "gemini"
	}
	return "unknown"
}

// persistGenAITelemetry stores a single telemetry record into core_telemetry collection
func persistGenAITelemetry(ctx context.Context, tenantID string, data map[string]any) error {
	if tenantID == "" {
		return nil
	}
	// Best-effort new client per call; for higher perf wire a shared client via handler state
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		return nil
	}
	c, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil
	}
	defer c.Close()
	data["tenantId"] = tenantID
	data["createdAt"] = time.Now().UTC()
	_, _, _ = c.Collection("core_telemetry").Add(ctx, data)
	return nil
}

// executeResearchMode handles Research mode requests
func (h *GenAIHandler) executeResearchMode(ctx context.Context, req gen_combined.GenAIRequest, projectID, streamID string) (*gen_combined.GenAIResponse, error) {
	if h.tavilyClient == nil {
		return nil, fmt.Errorf("genini500: Tavily client not configured for research mode")
	}

	// Create researcher with MVP configuration
	cfg := research.Config{
		MaxSources:    10,
		MaxSubQueries: 5,
		Model:         req.Model,
	}

	// Create adapter for TavilyClient to implement TavilySearchClient interface
	tavilyAdapter := &tavilyClientAdapter{client: h.tavilyClient}

	researcher := research.NewResearcher(
		req.Prompt,
		req.Model,
		tavilyAdapter,
		h.client,
		cfg,
	)

	// Set up progress callback for WebSocket events if publisher is available
	if h.publisher != nil && projectID != "" && streamID != "" {
		// Get tenantID from context (if available)
		auth := GetAuthInfo(ctx)
		if auth != nil {
			progressCallback := func(stage string, data map[string]interface{}) {
				eventType := fmt.Sprintf("genai.stream.research.%s", stage)
				payload := map[string]interface{}{
					"ts":        time.Now().UnixMilli(),
					"projectId": projectID,
					"streamId":  streamID,
				}
				// Merge data into payload
				for k, v := range data {
					payload[k] = v
				}
				// Best-effort publish (don't block research on WebSocket failures)
				if err := h.publisher.PublishJSON(ctx, eventType, auth.TenantID, payload); err != nil {
					log.Printf("[Research] Failed to publish progress event %s: %v", eventType, err)
				} else {
					log.Printf("[Research] Published progress event %s (projectId=%s streamId=%s)", eventType, projectID, streamID)
				}
			}
			researcher = researcher.WithProgressCallback(progressCallback)

			// Publish start event
			_ = h.publisher.PublishJSON(ctx, "genai.stream.research.start", auth.TenantID, map[string]interface{}{
				"ts":        time.Now().UnixMilli(),
				"projectId": projectID,
				"streamId":  streamID,
				"query":     req.Prompt,
			})
		}
	}

	result, err := researcher.ConductResearch(ctx)
	if err != nil {
		// Publish error event if publisher available
		if h.publisher != nil && projectID != "" && streamID != "" {
			if auth := GetAuthInfo(ctx); auth != nil {
				_ = h.publisher.PublishJSON(ctx, "genai.stream.research.error", auth.TenantID, map[string]interface{}{
					"ts":        time.Now().UnixMilli(),
					"projectId": projectID,
					"streamId":  streamID,
					"error":     err.Error(),
				})
			}
		}
		return nil, fmt.Errorf("genini500: Research failed: %w", err)
	}

	// Convert ResearchResult to GenAIResponse
	var sources []string
	for _, src := range result.Sources {
		sources = append(sources, src.URL)
	}

	// Combine usage from all stages
	var usage g.Usage
	if result.Usage != nil {
		usage = *result.Usage
	}
	// Add balancing usage
	if result.BalancingUsage != nil {
		usage.PromptTokens += result.BalancingUsage.PromptTokens
		usage.OutputTokens += result.BalancingUsage.OutputTokens
		usage.TotalTokens += result.BalancingUsage.TotalTokens
		if result.BalancingUsage.ReasoningTokens > 0 {
			usage.ReasoningTokens += result.BalancingUsage.ReasoningTokens
		}
	}
	// Add synthesizing usage
	if result.SynthesizingUsage != nil {
		usage.PromptTokens += result.SynthesizingUsage.PromptTokens
		usage.OutputTokens += result.SynthesizingUsage.OutputTokens
		usage.TotalTokens += result.SynthesizingUsage.TotalTokens
		if result.SynthesizingUsage.ReasoningTokens > 0 {
			usage.ReasoningTokens += result.SynthesizingUsage.ReasoningTokens
		}
	}

	// Publish done event if publisher available
	if h.publisher != nil && projectID != "" && streamID != "" {
		if auth := GetAuthInfo(ctx); auth != nil {
			// Convert sources to array of objects with title and url
			sourceObjects := make([]map[string]interface{}, len(result.Sources))
			for i, src := range result.Sources {
				sourceObjects[i] = map[string]interface{}{
					"title": src.Title,
					"url":   src.URL,
					"score": src.Score,
				}
			}

			_ = h.publisher.PublishJSON(ctx, "genai.stream.research.done", auth.TenantID, map[string]interface{}{
				"ts":              time.Now().UnixMilli(),
				"projectId":       projectID,
				"streamId":        streamID,
				"query":           req.Prompt,
				"report":          result.Report,
				"counterPosition": result.CounterPosition,
				"synthesis":       result.Synthesis,
				"usage":           usage,
				"sources":         sourceObjects,
				"subQueries":      result.SubQueries,
				"model":           req.Model,
			})
		}
	}

	// Use synthesis as the final content if available, otherwise use report
	finalContent := result.Synthesis
	if finalContent == "" {
		finalContent = result.Report
	}

	response := &gen_combined.GenAIResponse{
		Content: finalContent,
		Model:   req.Model,
		Usage:   usage,
		Sources: sources,
	}

	return response, nil
}

// tavilyClientAdapter adapts http.TavilyClient to research.TavilySearchClient interface
type tavilyClientAdapter struct {
	client *TavilyClient
}

func (a *tavilyClientAdapter) Search(ctx context.Context, query string, maxResults int) (*research.TavilySearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults)
	if err != nil {
		return nil, err
	}

	// Convert http.TavilySearchResponse to research.TavilySearchResponse
	results := make([]research.TavilySearchResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = research.TavilySearchResult{
			Title:         r.Title,
			URL:           r.URL,
			Content:       r.Content,
			Score:         r.Score,
			RawContent:    r.RawContent,
			PublishedDate: r.PublishedDate,
		}
	}

	return &research.TavilySearchResponse{
		Query:        resp.Query,
		Results:      results,
		Answer:       resp.Answer,
		ResponseTime: resp.ResponseTime,
	}, nil
}
