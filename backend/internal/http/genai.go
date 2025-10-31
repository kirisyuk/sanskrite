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
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

type GenAIHandler struct {
	client    *gen_combined.Client
	publisher Publisher
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

	resp, err := h.client.Generate(ctx, req.Prompt, req.Tools, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
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
	resp, err := h.client.ExecuteWithTools(ctx, req.Prompt, req.Tools, reg, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
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
	resp, err := h.client.GenerateJSON(ctx, req.Prompt, req.Tools, req.JSONSchema, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
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
	resp, err := h.client.GenerateJSONStrict(ctx, req.Prompt, req.Tools, req.JSONSchema, req.Options)
	if err != nil {
		status, code, msg, details := parseUpstreamError(err)
		_ = WriteError(w, status, code, msg, details)
		return
	}
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
	resp, err := h.client.GenerateJSON(ctx, req.Prompt, nil, nil, req.Options)
	if err != nil {
		_ = WriteError(w, http.StatusBadGateway, "GENAI_ERROR", err.Error(), nil)
		return
	}
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
			if e.Usage.TotalTokens > 0 {
				payload["usage"] = e.Usage
			}
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
			}()
			return h.publisher.PublishJSON(ctx, "genai.stream.done", auth.TenantID, payload)
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
