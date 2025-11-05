package http

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bytes"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

type GenAIHandler struct {
	client           *gen_combined.Client
	publisher        Publisher
	usageMetricsRepo *repo.UsageMetricsRepository
	modelManager     *ai.AIModelManager
}

// Publisher is a small interface for realtime publisher used here
type Publisher interface {
	PublishJSON(ctx context.Context, et interface{}, tenantID string, payload interface{}) error
}

func NewGenAIHandler() *GenAIHandler {
	apiKey := os.Getenv("VERTEX_API_KEY")
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

// GenerateDirect performs direct GenAI generation without HTTP layer (for internal use)
func (h *GenAIHandler) GenerateDirect(ctx context.Context, req gen_combined.GenAIRequest) (*gen_combined.GenAIResponse, error) {
	return h.client.GenAIGenerate(ctx, req)
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
	resp, err := h.client.GenAIGenerate(ctx, req)
	if err != nil {
		// Log upstream error body for diagnostics (e.g., Gemini 400 with details)
		log.Printf("GenAIGenerate upstream error: %v", err)
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
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
	ProjectID string `json:"projectId,omitempty"`
	StreamID  string `json:"streamId,omitempty"`
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
	if len(req.Messages) > 0 {
		// Build compact transcript using provided summaries/texts
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
			// keep line shortish (best-effort)
			ctxLines = append(ctxLines, "- "+role+": "+text)
		}
		if len(ctxLines) > 0 {
			pre := "You are given the full chat context below. Respond ONLY to the latest user message, taking the prior context into account. Do not restate or summarize the transcript itself."
			ctx := "\nConversation context (most recent last):\n" + strings.Join(ctxLines, "\n")
			if strings.TrimSpace(req.Options.SystemInstruction) == "" {
				req.Options.SystemInstruction = pre + ctx
			} else {
				req.Options.SystemInstruction = strings.TrimSpace(req.Options.SystemInstruction) + "\n\n" + pre + ctx
			}
		}
	}

	// Track per-stream chunk count for diagnostics
	chunks := 0
	onEvt := func(e gen_gemini.StreamEvent) error {
		switch e.Type {
		case "chunk":
			// increment local chunk counter
			chunks++
			return h.publisher.PublishJSON(ctx, "genai.stream.chunk", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"text":      e.Text,
			})
		case "tool_call":
			return h.publisher.PublishJSON(ctx, "genai.stream.tool_call", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"toolCalls": e.ToolCalls,
			})
		case "tool_result":
			return h.publisher.PublishJSON(ctx, "genai.stream.tool_result", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"toolCalls": e.ToolCalls,
			})
		case "error":
			return h.publisher.PublishJSON(ctx, "genai.stream.error", auth.TenantID, map[string]any{
				"projectId": projectID,
				"streamId":  streamID,
				"error":     e.Error,
			})
		case "done":
			// include usage/attributions/searchQueries/sources if accumulated
			payload := map[string]any{
				"ts":        time.Now().UnixMilli(),
				"projectId": projectID,
				"streamId":  streamID,
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
						defer func() { recover() }()
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
	if len(req.Tools) > 0 {
		reg := gen_gemini.NewSimpleRegistry()
		err = h.client.StreamWithTools(ctx, req.Prompt, req.Tools, reg, req.Options, onEvt)
	} else {
		err = h.client.Stream(ctx, req.Prompt, req.Tools, req.Options, onEvt)
	}
	if err != nil {
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

	// Determine provider from model name
	provider := h.determineProvider(resp.Model)

	// Calculate cost using modelManager (which handles tiered pricing for Gemini)
	cost, err := h.modelManager.CalculateProcessingCost(resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens)
	if err != nil {
		// If model not found in manager, fallback to simple calculation
		log.Printf("[saveUsageMetric] Model %s not found in modelManager, using fallback pricing", resp.Model)
		cost = h.calculateFallbackCost(provider, resp.Model, resp.Usage.PromptTokens, resp.Usage.OutputTokens)
	}

	// Determine model alias if available
	modelAlias := &req.Model
	if req.Model == "" {
		modelAlias = nil
	}

	metric := &repo.UsageMetric{
		TenantID:      auth.TenantID,
		UserID:        auth.UID,
		ProjectID:     projectID,
		InteractionID: interactionID,
		Provider:      provider,
		Model:         resp.Model,
		ModelAlias:    modelAlias,
		InputTokens:   resp.Usage.PromptTokens,
		OutputTokens:  resp.Usage.OutputTokens,
		TotalTokens:   resp.Usage.TotalTokens,
		CostUSD:       cost,
		DurationMs:    durationMs,
		TaskType:      nil,        // Can be set from context if needed
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
		case strings.Contains(modelLower, "grok-4-fast-non-reasoning"):
			inputPrice = 0.00000020  // $0.20 / 1M
			outputPrice = 0.00000050 // $0.50 / 1M
		case strings.Contains(modelLower, "grok-4-fast-reasoning"):
			inputPrice = 0.00000020  // $0.20 / 1M (<128K), $0.40 / 1M (>=128K) - using base price
			outputPrice = 0.00000050 // $0.50 / 1M (<128K), $1.00 / 1M (>=128K) - using base price
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
