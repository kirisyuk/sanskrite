package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Profile enumerates supported model profiles
type Profile string

const (
	ProfileFast    Profile = "fast"
	ProfileQuality Profile = "quality"
)

// Options configures a single generate call
type Options struct {
	Profile           Profile `json:"profile"`
	ModelOverride     string  `json:"modelOverride"`
	Temperature       float64 `json:"temperature"`
	MaxOutputTokens   int     `json:"maxOutputTokens"`
	GroundingEnabled  bool    `json:"groundingEnabled"`
	SystemInstruction string  `json:"systemInstruction"`
	ResponseFormat    string  `json:"responseFormat"` // "text" | "json"
}

// Tool represents a function declaration for tool calling
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
}

// ToolCall returned by the model
type ToolCall struct {
	Name string `json:"name"`
	// JSON-encoded arguments as provided by the model
	Arguments string `json:"arguments"`
	CallID    string `json:"callId,omitempty"`
}

// Usage captures token accounting where available
type Usage struct {
	PromptTokens int `json:"promptTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

// Attribution represents grounding sources (when enabled)
type Attribution struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// GenerateResponse is a unified response shape
type GenerateResponse struct {
	Content      string        `json:"content"`
	ToolCalls    []ToolCall    `json:"toolCalls,omitempty"`
	IsToolStep   bool          `json:"isToolStep"`
	Usage        Usage         `json:"usage"`
	Attributions []Attribution `json:"attributions,omitempty"`
	Model        string        `json:"model"`
	// For OpenAI-backed flows we propagate raw output items to support follow-up function_call_output linkage
	OutputRaw []map[string]interface{} `json:"-"`
}

// Client is an isolated Gemini client that does not depend on task executor/model manager
type Client struct {
	apiKey       string
	httpClient   *http.Client
	streamClient *http.Client
	projectID    string
	location     string
	// configured model names; overridable per-call via Options.ModelOverride
	fastModel    string
	qualityModel string
}

// NewClient creates a new Gemini client
func NewClient(apiKey, projectID, location string, fastModel, qualityModel string) *Client {
	key := strings.TrimSpace(apiKey)
	key = strings.Trim(key, "\"'")
	if fastModel == "" {
		fastModel = "gemini-2.5-flash-lite"
	}
	if qualityModel == "" {
		qualityModel = "gemini-2.5-pro"
	}
	return &Client{
		apiKey:       key,
		httpClient:   &http.Client{Timeout: 120 * time.Second},
		streamClient: &http.Client{},
		projectID:    projectID,
		location:     location,
		fastModel:    fastModel,
		qualityModel: qualityModel,
	}
}

func (c *Client) modelFrom(opts Options) string {
	if opts.ModelOverride != "" {
		return opts.ModelOverride
	}
	if opts.Profile == ProfileQuality {
		return c.qualityModel
	}
	return c.fastModel
}

// Generate executes a single-shot generation, with optional grounding and tools.
func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
	model := c.modelFrom(opts)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role":  "user",
				"parts": []map[string]interface{}{{"text": prompt}},
			},
		},
	}
	if opts.SystemInstruction != "" {
		reqBody["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{{"text": opts.SystemInstruction}},
		}
	}
	if opts.Temperature > 0 {
		reqBody["generationConfig"] = map[string]interface{}{
			"temperature":     opts.Temperature,
			"maxOutputTokens": opts.MaxOutputTokens,
			// response MIME controls JSON mode when needed
			"responseMimeType": func() string {
				if opts.ResponseFormat == "json" {
					return "application/json"
				}
				return "text/plain"
			}(),
		}
	} else if opts.MaxOutputTokens > 0 || opts.ResponseFormat != "" {
		reqBody["generationConfig"] = map[string]interface{}{
			"maxOutputTokens": opts.MaxOutputTokens,
			"responseMimeType": func() string {
				if opts.ResponseFormat == "json" {
					return "application/json"
				}
				return "text/plain"
			}(),
		}
	}

	// Tools: functionDeclarations and googleSearch grounding
	toolsPayload := make([]map[string]interface{}, 0)
	if len(tools) > 0 {
		fdecls := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			fdecls = append(fdecls, map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
		}
		toolsPayload = append(toolsPayload, map[string]interface{}{"functionDeclarations": fdecls})
	}
	if opts.GroundingEnabled {
		// enable search grounding only when explicitly requested
		toolsPayload = append(toolsPayload, map[string]interface{}{"googleSearch": map[string]interface{}{}})
	}
	if len(toolsPayload) > 0 {
		reqBody["tools"] = toolsPayload
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Note: public endpoint works with API key
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-goog-api-key", c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini error %d: %s", resp.StatusCode, string(rb))
	}

	// Parse Gemini response
	var root map[string]interface{}
	if err := json.Unmarshal(rb, &root); err != nil {
		return nil, err
	}

	out := &GenerateResponse{Model: model}
	// Extract text content and/or tool calls
	if cands, ok := root["candidates"].([]interface{}); ok && len(cands) > 0 {
		if cand, ok := cands[0].(map[string]interface{}); ok {
			// parts
			if content, ok := cand["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok {
					for _, p := range parts {
						if m, ok := p.(map[string]interface{}); ok {
							if txt, ok := m["text"].(string); ok && txt != "" {
								out.Content += txt
							}
							if fc, ok := m["functionCall"].(map[string]interface{}); ok {
								name, _ := fc["name"].(string)
								// arguments may be object or string depending on API variant
								var argsJSON string
								if rawArgs, ok := fc["args"].(map[string]interface{}); ok {
									b, _ := json.Marshal(rawArgs)
									argsJSON = string(b)
								} else if s, ok := fc["args"].(string); ok {
									argsJSON = s
								}
								out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: argsJSON})
							}
						}
					}
				}
			}
		}
	}
	out.IsToolStep = len(out.ToolCalls) > 0 && out.Content == ""

	// Usage (best effort; fields vary by API version)
	if u, ok := root["usageMetadata"].(map[string]interface{}); ok {
		if v, ok := u["promptTokenCount"].(float64); ok {
			out.Usage.PromptTokens = int(v)
		}
		if v, ok := u["candidatesTokenCount"].(float64); ok {
			out.Usage.OutputTokens = int(v)
		}
		out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.OutputTokens
	}

	// Grounding attributions (best effort)
	if cmeta, ok := root["candidates"].([]interface{}); ok && len(cmeta) > 0 {
		if cand0, ok := cmeta[0].(map[string]interface{}); ok {
			if meta, ok := cand0["groundingMetadata"].(map[string]interface{}); ok {
				if atts, ok := meta["groundingAttributions"].([]interface{}); ok {
					for _, a := range atts {
						if am, ok := a.(map[string]interface{}); ok {
							att := Attribution{}
							if s, ok := am["web"].(map[string]interface{}); ok {
								att.Title, _ = s["title"].(string)
								att.URL, _ = s["uri"].(string)
							}
							if att.URL != "" || att.Title != "" {
								out.Attributions = append(out.Attributions, att)
							}
						}
					}
				}
			}
		}
	}

	return out, nil
}

// GenerateJSON enforces JSON output and optionally a response schema
func (c *Client) GenerateJSON(ctx context.Context, prompt string, tools []Tool, jsonSchema map[string]interface{}, opts Options) (*GenerateResponse, error) {
	opts.ResponseFormat = "json"
	model := c.modelFrom(opts)
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{{
			"role":  "user",
			"parts": []map[string]interface{}{{"text": prompt}},
		}},
		"generationConfig": map[string]interface{}{
			"responseMimeType": "application/json",
		},
	}
	if opts.MaxOutputTokens > 0 {
		reqBody["generationConfig"].(map[string]interface{})["maxOutputTokens"] = opts.MaxOutputTokens
	}
	if opts.Temperature > 0 {
		reqBody["generationConfig"].(map[string]interface{})["temperature"] = opts.Temperature
	}
	if jsonSchema != nil {
		// Gemini supports responseSchema for structured outputs
		reqBody["responseSchema"] = jsonSchema
	}
	toolsPayload := make([]map[string]interface{}, 0)
	if len(tools) > 0 {
		fdecls := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			fdecls = append(fdecls, map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
		}
		toolsPayload = append(toolsPayload, map[string]interface{}{"functionDeclarations": fdecls})
	}
	if opts.GroundingEnabled {
		toolsPayload = append(toolsPayload, map[string]interface{}{"googleSearch": map[string]interface{}{}})
	}
	if len(toolsPayload) > 0 {
		reqBody["tools"] = toolsPayload
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini error %d: %s", resp.StatusCode, string(rb))
	}
	var root map[string]interface{}
	if err := json.Unmarshal(rb, &root); err != nil {
		return nil, err
	}
	return c.parseGenerateRoot(root, model)
}

func (c *Client) parseGenerateRoot(root map[string]interface{}, model string) (*GenerateResponse, error) {
	out := &GenerateResponse{Model: model}
	if cands, ok := root["candidates"].([]interface{}); ok && len(cands) > 0 {
		if cand, ok := cands[0].(map[string]interface{}); ok {
			if content, ok := cand["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok {
					for _, p := range parts {
						if m, ok := p.(map[string]interface{}); ok {
							if txt, ok := m["text"].(string); ok && txt != "" {
								out.Content += txt
							}
							if fc, ok := m["functionCall"].(map[string]interface{}); ok {
								name, _ := fc["name"].(string)
								var argsJSON string
								if rawArgs, ok := fc["args"].(map[string]interface{}); ok {
									b, _ := json.Marshal(rawArgs)
									argsJSON = string(b)
								} else if s, ok := fc["args"].(string); ok {
									argsJSON = s
								}
								out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: argsJSON})
							}
						}
					}
				}
			}
		}
	}
	out.IsToolStep = len(out.ToolCalls) > 0 && out.Content == ""
	if u, ok := root["usageMetadata"].(map[string]interface{}); ok {
		if v, ok := u["promptTokenCount"].(float64); ok {
			out.Usage.PromptTokens = int(v)
		}
		if v, ok := u["candidatesTokenCount"].(float64); ok {
			out.Usage.OutputTokens = int(v)
		}
		out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.OutputTokens
	}
	if cmeta, ok := root["candidates"].([]interface{}); ok && len(cmeta) > 0 {
		if cand0, ok := cmeta[0].(map[string]interface{}); ok {
			if meta, ok := cand0["groundingMetadata"].(map[string]interface{}); ok {
				if atts, ok := meta["groundingAttributions"].([]interface{}); ok {
					for _, a := range atts {
						if am, ok := a.(map[string]interface{}); ok {
							att := Attribution{}
							if s, ok := am["web"].(map[string]interface{}); ok {
								att.Title, _ = s["title"].(string)
								att.URL, _ = s["uri"].(string)
							}
							if att.URL != "" || att.Title != "" {
								out.Attributions = append(out.Attributions, att)
							}
						}
					}
				}
			}
		}
	}
	return out, nil
}

// StreamEvent represents incremental streaming callbacks
type StreamEvent struct {
	Type         string        `json:"type"` // start|chunk|tool_call|done|error
	Text         string        `json:"text,omitempty"`
	ToolCalls    []ToolCall    `json:"toolCalls,omitempty"`
	Usage        Usage         `json:"usage,omitempty"`
	Attributions []Attribution `json:"attributions,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// StreamConversation streams with arbitrary contents history (for tool follow-ups)
func (c *Client) StreamConversation(ctx context.Context, contents []map[string]interface{}, tools []Tool, opts Options, onEvent func(StreamEvent) error) error {
	model := c.modelFrom(opts)
	reqBody := map[string]interface{}{
		"contents": contents,
	}
	if opts.SystemInstruction != "" {
		reqBody["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{{"text": opts.SystemInstruction}},
		}
	}
	if opts.MaxOutputTokens > 0 || opts.ResponseFormat != "" || opts.Temperature > 0 {
		gc := map[string]interface{}{}
		if opts.MaxOutputTokens > 0 {
			gc["maxOutputTokens"] = opts.MaxOutputTokens
		}
		if opts.Temperature > 0 {
			gc["temperature"] = opts.Temperature
		}
		if opts.ResponseFormat == "json" {
			gc["responseMimeType"] = "application/json"
		} else if opts.ResponseFormat == "text" {
			gc["responseMimeType"] = "text/plain"
		}
		if len(gc) > 0 {
			reqBody["generationConfig"] = gc
		}
	}
	toolsPayload := make([]map[string]interface{}, 0)
	if len(tools) > 0 {
		fdecls := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			fdecls = append(fdecls, map[string]interface{}{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
		}
		toolsPayload = append(toolsPayload, map[string]interface{}{"functionDeclarations": fdecls})
	}
	if opts.GroundingEnabled || opts.Profile == ProfileQuality {
		toolsPayload = append(toolsPayload, map[string]interface{}{"googleSearch": map[string]interface{}{}})
	}
	if len(toolsPayload) > 0 {
		reqBody["tools"] = toolsPayload
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	// Use v1beta for streaming to ensure model/method compatibility across profiles
	apiVersion := "v1beta"
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/%s/models/%s:streamGenerateContent", apiVersion, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("x-goog-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gemini stream error %d: %s", resp.StatusCode, string(rb))
	}
	if onEvent != nil {
		_ = onEvent(StreamEvent{Type: "start"})
	}
	// Parse SSE-style stream: accumulate lines starting with "data: " until a blank line
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 1024*64)
	scanner.Buffer(buf, 1024*1024)
	var eventBuf strings.Builder
	var accUsage Usage
	var accAtts []Attribution
	flushEvent := func() {
		payload := strings.TrimSpace(eventBuf.String())
		eventBuf.Reset()
		if payload == "" || payload == "[DONE]" {
			return
		}
		processRoot := func(root map[string]interface{}) {
			// extract chunk or tool calls
			evt := StreamEvent{}
			if cands, ok := root["candidates"].([]interface{}); ok && len(cands) > 0 {
				if cand, ok := cands[0].(map[string]interface{}); ok {
					if content, ok := cand["content"].(map[string]interface{}); ok {
						if parts, ok := content["parts"].([]interface{}); ok {
							for _, p := range parts {
								if m, ok := p.(map[string]interface{}); ok {
									if txt, ok := m["text"].(string); ok && txt != "" {
										evt.Type = "chunk"
										evt.Text += txt
									}
									if fc, ok := m["functionCall"].(map[string]interface{}); ok {
										name, _ := fc["name"].(string)
										var argsJSON string
										if rawArgs, ok := fc["args"].(map[string]interface{}); ok {
											b, _ := json.Marshal(rawArgs)
											argsJSON = string(b)
										} else if s, ok := fc["args"].(string); ok {
											argsJSON = s
										}
										evt.ToolCalls = append(evt.ToolCalls, ToolCall{Name: name, Arguments: argsJSON})
									}
								}
							}
						}
					}
					if gm, ok := cand["groundingMetadata"].(map[string]interface{}); ok {
						if atts, ok := gm["groundingAttributions"].([]interface{}); ok {
							for _, a := range atts {
								if am, ok := a.(map[string]interface{}); ok {
									att := Attribution{}
									if s, ok := am["web"].(map[string]interface{}); ok {
										att.Title, _ = s["title"].(string)
										att.URL, _ = s["uri"].(string)
									}
									if att.URL != "" || att.Title != "" {
										accAtts = append(accAtts, att)
									}
								}
							}
						}
					}
				}
			}
			if evt.Type == "" && len(evt.ToolCalls) > 0 {
				evt.Type = "tool_call"
			}
			if evt.Type == "" {
				evt.Type = "chunk"
			}
			if onEvent != nil {
				if err := onEvent(evt); err != nil {
					_ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
				}
			}
			if u, ok := root["usageMetadata"].(map[string]interface{}); ok {
				if v, ok := u["promptTokenCount"].(float64); ok {
					accUsage.PromptTokens = int(v)
				}
				if v, ok := u["candidatesTokenCount"].(float64); ok {
					accUsage.OutputTokens = int(v)
				}
				accUsage.TotalTokens = accUsage.PromptTokens + accUsage.OutputTokens
			}
		}
		// Try object first
		var root map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &root); err == nil {
			processRoot(root)
			return
		}
		// If array, iterate elements
		var arr []interface{}
		if err := json.Unmarshal([]byte(payload), &arr); err == nil {
			for _, el := range arr {
				if m, ok := el.(map[string]interface{}); ok {
					processRoot(m)
				}
			}
			return
		}
		// Unknown payload
		if onEvent != nil {
			_ = onEvent(StreamEvent{Type: "error", Error: "unrecognized stream payload"})
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		l := strings.TrimRight(line, "\r\n")
		if l == "" {
			// event boundary
			flushEvent()
			continue
		}
		if strings.HasPrefix(l, ":") {
			// comment/keep-alive
			continue
		}
		// Ignore SSE control fields that are not part of the JSON payload
		if strings.HasPrefix(l, "event:") || strings.HasPrefix(l, "id:") || strings.HasPrefix(l, "retry:") {
			continue
		}
		if strings.HasPrefix(l, "data:") {
			// strip prefix and leading space
			frag := strings.TrimSpace(strings.TrimPrefix(l, "data:"))
			if eventBuf.Len() > 0 {
				eventBuf.WriteString("\n")
			}
			eventBuf.WriteString(frag)
			continue
		}
		// Unknown line type; try to treat as raw JSON fragment
		if eventBuf.Len() > 0 {
			eventBuf.WriteString("\n")
		}
		eventBuf.WriteString(strings.TrimSpace(l))
	}
	// flush any remaining event
	flushEvent()
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		if onEvent != nil {
			_ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
		}
		return err
	}
	if onEvent != nil {
		_ = onEvent(StreamEvent{Type: "done", Usage: accUsage, Attributions: accAtts})
	}
	return nil
}

// Stream is a convenience wrapper for one-turn conversation
func (c *Client) Stream(ctx context.Context, prompt string, tools []Tool, opts Options, onEvent func(StreamEvent) error) error {
	contents := []map[string]interface{}{
		{"role": "user", "parts": []map[string]interface{}{{"text": prompt}}},
	}
	return c.StreamConversation(ctx, contents, tools, opts, onEvent)
}
