package combined

import (
	"context"
	"errors"
	"strings"

	g "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	oa "github.com/igorkirisyuk/sanskrite/internal/genai/openai"
)

// Re-export types so HTTP layer can remain unchanged
type Tool = g.Tool
type Options = g.Options
type StreamEvent = g.StreamEvent
type GenerateResponse = g.GenerateResponse

type Client struct {
	gem *g.Client
	oa  *oa.Client
}

func NewClient(geminiClient *g.Client, openaiKey string) *Client {
	openaiKey = strings.TrimSpace(openaiKey)
	var oac *oa.Client
	if openaiKey != "" {
		oac = oa.NewClient(openaiKey)
	}
	return &Client{gem: geminiClient, oa: oac}
}

// route decides provider by model alias (gpt_* -> openai; gemini_* -> gemini; default gemini)
func route(opts Options) string {
	m := strings.TrimSpace(opts.ModelOverride)
	if m == "" {
		return "gemini"
	}
	if strings.HasPrefix(m, "gpt_") || strings.HasPrefix(strings.ToLower(m), "gpt-5") {
		return "openai"
	}
	return "gemini"
}

// map alias to concrete provider model id
func (c *Client) normalizeOptionsForGemini(opts Options) Options {
	switch opts.ModelOverride {
	case "gemini_quality":
		opts.ModelOverride = "gemini-2.5-pro"
	case "gemini_fast":
		opts.ModelOverride = "gemini-2.5-flash-lite"
	}
	return opts
}

func (c *Client) normalizeOptionsForOpenAI(opts Options) oa.Options {
	out := oa.Options{
		ModelOverride:     opts.ModelOverride,
		Temperature:       opts.Temperature,
		MaxOutputTokens:   opts.MaxOutputTokens,
		GroundingEnabled:  opts.GroundingEnabled,
		SystemInstruction: opts.SystemInstruction,
	}
	switch opts.ModelOverride {
	case "gpt_quality":
		out.ModelOverride = "gpt-5"
	case "gpt_balanced":
		out.ModelOverride = "gpt-5-mini"
	case "gpt_fast":
		out.ModelOverride = "gpt-5-nano"
	}
	return out
}

func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
	if route(opts) == "openai" {
		if c.oa == nil {
			return nil, errors.New("openai not configured")
		}
		oar, err := c.oa.Generate(ctx, prompt, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			return nil, err
		}
		gr := &GenerateResponse{Content: oar.Content, ToolCalls: toGemCalls(oar.ToolCalls), IsToolStep: oar.IsToolStep, Usage: toGemUsage(oar.Usage), Model: oar.Model}
		gr.OutputRaw = oar.OutputRaw
		return gr, nil
	}
	goOpts := c.normalizeOptionsForGemini(opts)
	return c.gem.Generate(ctx, prompt, tools, goOpts)
}

func (c *Client) GenerateJSON(ctx context.Context, prompt string, tools []Tool, jsonSchema map[string]interface{}, opts Options) (*GenerateResponse, error) {
	if route(opts) == "openai" {
		// Responses API: enforce JSON by setting max tokens + instruction; simplest is to request plain text JSON.
		if c.oa == nil {
			return nil, errors.New("openai not configured")
		}
		// Reuse Generate and rely on system instruction
		o := c.normalizeOptionsForOpenAI(opts)
		if !strings.Contains(strings.ToLower(o.SystemInstruction), "json") {
			o.SystemInstruction = strings.TrimSpace(o.SystemInstruction + "\nRespond strictly in JSON.")
		}
		oar, err := c.oa.Generate(ctx, prompt, toOATools(tools), o)
		if err != nil {
			return nil, err
		}
		gr := &GenerateResponse{Content: oar.Content, ToolCalls: toGemCalls(oar.ToolCalls), IsToolStep: oar.IsToolStep, Usage: toGemUsage(oar.Usage), Model: oar.Model}
		gr.OutputRaw = oar.OutputRaw
		return gr, nil
	}
	return c.gem.GenerateJSON(ctx, prompt, tools, jsonSchema, c.normalizeOptionsForGemini(opts))
}

// ExecuteWithTools: run a tool loop (best-effort on OpenAI)
func (c *Client) ExecuteWithTools(ctx context.Context, prompt string, tools []Tool, reg g.ToolExecutor, opts Options) (*GenerateResponse, error) {
	if route(opts) == "openai" {
		if c.oa == nil {
			return nil, errors.New("openai not configured")
		}
		// First call with tools
		r, err := c.oa.Generate(ctx, prompt, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			return nil, err
		}
		// If tool calls, execute and append results, then ask again via Responses follow-up
		if len(r.ToolCalls) > 0 {
			// Start with previous output items to preserve call_id linkage
			input := make([]map[string]interface{}, 0, len(r.OutputRaw)+1)
			input = append(input, map[string]interface{}{"role": "user", "content": prompt})
			input = append(input, r.OutputRaw...)
			for _, tc := range r.ToolCalls {
				out, _ := reg.Execute(ctx, tc.Name, []byte(tc.Arguments))
				input = append(input, map[string]interface{}{
					"type":    "function_call_output",
					"call_id": tc.CallID,
					"output":  string(out),
				})
			}
			r2, err := c.oa.Respond(ctx, input, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
			if err != nil {
				return nil, err
			}
			return &GenerateResponse{Content: r2.Content, Model: r2.Model, Usage: toGemUsage(r2.Usage)}, nil
		}
		return &GenerateResponse{Content: r.Content, Model: r.Model, Usage: toGemUsage(r.Usage)}, nil
	}
	// Delegate to Gemini implementation with its registry
	return c.gem.ExecuteWithTools(ctx, prompt, tools, reg, c.normalizeOptionsForGemini(opts))
}

// Stream: for OpenAI emit one chunk + done (placeholder streaming)
func (c *Client) Stream(ctx context.Context, prompt string, tools []Tool, opts Options, onEvent func(StreamEvent) error) error {
	if route(opts) == "openai" {
		if c.oa == nil {
			return errors.New("openai not configured")
		}
		_ = onEvent(StreamEvent{Type: "start"})
		// TEMP: Disable SSE streaming for GPT; return a single final chunk
		r, err := c.oa.Generate(ctx, prompt, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			_ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
			return err
		}
		if r.Content != "" {
			_ = onEvent(StreamEvent{Type: "chunk", Text: r.Content})
		}
		_ = onEvent(StreamEvent{Type: "done"})
		return nil
	}
	return c.gem.Stream(ctx, prompt, tools, c.normalizeOptionsForGemini(opts), onEvent)
}

func (c *Client) StreamWithTools(ctx context.Context, prompt string, tools []Tool, reg g.ToolExecutor, opts Options, onEvent func(StreamEvent) error) error {
	if route(opts) == "openai" {
		if c.oa == nil {
			return errors.New("openai not configured")
		}
		// Start event
		_ = onEvent(StreamEvent{Type: "start"})
		// Step 1: request with tools
		r, err := c.oa.Generate(ctx, prompt, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			_ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
			return err
		}
		if len(r.ToolCalls) == 0 {
			// No tools, emit content (single chunk placeholder) and done
			if r.Content != "" {
				_ = onEvent(StreamEvent{Type: "chunk", Text: r.Content})
			}
			_ = onEvent(StreamEvent{Type: "done", Usage: toGemUsage(r.Usage)})
			return nil
		}
		// Emit tool_call event(s)
		_ = onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemCalls(r.ToolCalls)})
		// Build follow-up input including previous output items to preserve call_id linkage
		input := make([]map[string]interface{}, 0, len(r.OutputRaw)+len(r.ToolCalls)+1)
		if len(r.OutputRaw) > 0 {
			input = append(input, r.OutputRaw...)
		} else {
			// Fallback: include user prompt if no raw items present
			input = append(input, map[string]interface{}{"role": "user", "content": prompt})
		}
		for _, tc := range r.ToolCalls {
			out, execErr := reg.Execute(ctx, tc.Name, []byte(tc.Arguments))
			if execErr != nil {
				_ = onEvent(StreamEvent{Type: "error", Error: execErr.Error()})
				return execErr
			}
			// Emit tool_result (use Arguments field to carry result payload for consistency with Gemini)
			_ = onEvent(StreamEvent{Type: "tool_result", ToolCalls: []g.ToolCall{{Name: tc.Name, Arguments: string(out), CallID: tc.CallID}}})
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": tc.CallID,
				"output":  string(out),
			})
		}
		// Step 2: respond with function_call_output appended
		r2, err := c.oa.Respond(ctx, input, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			_ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
			return err
		}
		if r2.Content != "" {
			_ = onEvent(StreamEvent{Type: "chunk", Text: r2.Content})
		}
		_ = onEvent(StreamEvent{Type: "done", Usage: toGemUsage(r2.Usage)})
		return nil
	}
	return c.gem.StreamWithTools(ctx, prompt, tools, reg, c.normalizeOptionsForGemini(opts), onEvent)
}

// Respond forwards to OpenAI Respond when provider is OpenAI; for Gemini not supported
func (c *Client) Respond(ctx context.Context, input []map[string]interface{}, tools []Tool, opts Options) (*GenerateResponse, error) {
	if route(opts) == "openai" {
		if c.oa == nil {
			return nil, errors.New("openai not configured")
		}
		r, err := c.oa.Respond(ctx, input, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			return nil, err
		}
		return &GenerateResponse{Content: r.Content, ToolCalls: toGemCalls(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsage(r.Usage), Model: r.Model}, nil
	}
	return nil, errors.New("respond not supported for gemini")
}

// helpers
func toOATools(ts []Tool) []oa.Tool {
	out := make([]oa.Tool, 0, len(ts))
	for _, t := range ts {
		out = append(out, oa.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}
func toGemCalls(cs []oa.ToolCall) []g.ToolCall {
	out := make([]g.ToolCall, 0, len(cs))
	for _, c := range cs {
		out = append(out, g.ToolCall{Name: c.Name, Arguments: c.Arguments, CallID: c.CallID})
	}
	return out
}
func toGemUsage(u oa.Usage) g.Usage {
	return g.Usage{PromptTokens: u.PromptTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens}
}
