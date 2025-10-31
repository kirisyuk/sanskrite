package combined

import (
	"context"
	"errors"
	"os"
	"strings"

	g "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
	gr "github.com/igorkirisyuk/sanskrite/internal/genai/grok"
	oa "github.com/igorkirisyuk/sanskrite/internal/genai/openai"
    cl "github.com/igorkirisyuk/sanskrite/internal/genai/claude"
    ds "github.com/igorkirisyuk/sanskrite/internal/genai/deepseek"
)

// Re-export types so HTTP layer can remain unchanged
type Tool = g.Tool
type Options = g.Options
type StreamEvent = g.StreamEvent
type GenerateResponse = g.GenerateResponse

// GenAIRequest defines unified options for a single-shot generation
type GenAIRequest struct {
	Model              string                 `json:"model"`
	Prompt             string                 `json:"prompt"`
	SystemInstruction  string                 `json:"systemInstruction,omitempty"`
	Temperature        float64                `json:"temperature,omitempty"`
	MaxOutputTokens    int                    `json:"maxOutputTokens,omitempty"`
	WebSearchEnabled   bool                   `json:"webSearchEnabled,omitempty"`
	ToolCallingEnabled bool                   `json:"toolCallingEnabled,omitempty"`
	StrictJSONEnabled  bool                   `json:"strictJSONEnabled,omitempty"`
	JSONSchema         map[string]interface{} `json:"jsonSchema,omitempty"`
	Tools              []Tool                 `json:"tools,omitempty"`
	ToolChoice         string                 `json:"toolChoice,omitempty"` // auto|required
	ParallelToolCalls  bool                   `json:"parallelToolCalls,omitempty"`
}

// GenAIResponse is an alias of unified GenerateResponse
type GenAIResponse = GenerateResponse

type Client struct {
	gem *g.Client
	oa  *oa.Client
	gr  *gr.Client
    cl  *cl.Client
    ds  *ds.Client
}

// getenv wraps os.Getenv for consistency
func getenv(k string) string { return os.Getenv(k) }

func NewClient(geminiClient *g.Client, openaiKey string) *Client {
	openaiKey = strings.TrimSpace(openaiKey)
	var oac *oa.Client
	if openaiKey != "" {
		oac = oa.NewClient(openaiKey)
	}
	// Grok key from env (prod key first, fallback to _TEST)
	var grc *gr.Client
	if k := strings.Trim(strings.TrimSpace(getenv("GROK_API_KEY")), "\"'"); k != "" {
		grc = gr.NewClient(k)
	} else if k := strings.Trim(strings.TrimSpace(getenv("GROK_API_KEY_TEST")), "\"'"); k != "" {
		grc = gr.NewClient(k)
	}
    // Claude key
    var clc *cl.Client
    if k := strings.Trim(strings.TrimSpace(getenv("ANTHROPIC_API_KEY")), "\"'"); k != "" {
        clc = cl.NewClient(k)
    } else if k := strings.Trim(strings.TrimSpace(getenv("ANTHROPIC_API_KEY_TEST")), "\"'"); k != "" {
        clc = cl.NewClient(k)
    }
    // DeepSeek key
    var dsc *ds.Client
    if k := strings.Trim(strings.TrimSpace(getenv("DEEPSEEK_API_KEY")), "\"'"); k != "" {
        dsc = ds.NewClient(k)
    } else if k := strings.Trim(strings.TrimSpace(getenv("DEEPSEEK_API_KEY_TEST")), "\"'"); k != "" {
        dsc = ds.NewClient(k)
    }
    return &Client{gem: geminiClient, oa: oac, gr: grc, cl: clc, ds: dsc}
}

// route decides provider by model alias (gpt_* -> openai; gemini_* -> gemini; default gemini)
// canonicalizeAlias normalizes model aliases from UI/user input to a stable form.
// Rules:
// - lower-case
// - replace any non [a-z0-9]+ with underscore
// - collapse multiple underscores
// - trim leading/trailing underscores
func canonicalizeAlias(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	// Replace hyphens, spaces, dots and other non-alnum with underscore
	b := make([]rune, 0, len(t))
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b = append(b, r)
		} else {
			b = append(b, '_')
		}
	}
	u := string(b)
	// Collapse consecutive underscores
	u = strings.ReplaceAll(u, "__", "_")
	for strings.Contains(u, "__") {
		u = strings.ReplaceAll(u, "__", "_")
	}
	u = strings.Trim(u, "_")
	return u
}

func route(opts Options) string {
	m := canonicalizeAlias(opts.ModelOverride)
	if m == "" {
		return "gemini"
	}
	// Normalize variants for routing
	ml := m
	if strings.HasPrefix(ml, "gpt_") || strings.HasPrefix(ml, "gpt5") {
		return "openai"
	}
	if strings.HasPrefix(ml, "grok_") || strings.HasPrefix(ml, "grok4") {
		return "grok"
	}
    if strings.HasPrefix(ml, "claude_") {
        return "claude"
    }
    if strings.HasPrefix(ml, "deepseek_") {
        return "deepseek"
    }
	return "gemini"
}

// map alias to concrete provider model id
func (c *Client) normalizeOptionsForGemini(opts Options) Options {
	// Accept underscores, hyphens, spaces, dots and other variants
	switch canonicalizeAlias(opts.ModelOverride) {
	case "gemini_quality":
		opts.ModelOverride = "gemini-2.5-pro"
	case "gemini_fast":
		opts.ModelOverride = "gemini-2.5-flash-lite"
	case "gemini_balanced":
		opts.ModelOverride = "gemini-2.5-flash"
	}
	return opts
}

func (c *Client) normalizeOptionsForOpenAI(opts Options) oa.Options {
	out := oa.Options{
		ModelOverride:     canonicalizeAlias(opts.ModelOverride),
		Temperature:       opts.Temperature,
		MaxOutputTokens:   opts.MaxOutputTokens,
		GroundingEnabled:  opts.GroundingEnabled,
		SystemInstruction: opts.SystemInstruction,
	}
	switch out.ModelOverride {
	case "gpt_quality":
		out.ModelOverride = "gpt-5"
	case "gpt_balanced":
		out.ModelOverride = "gpt-5-mini"
	case "gpt_fast":
		out.ModelOverride = "gpt-5-nano"
	}
	return out
}

func (c *Client) normalizeOptionsForGrok(opts Options) gr.Options {
	return gr.Options{
		ModelOverride: map[string]string{
			"grok_quality": "grok-4-0709",
			"grok_fast":    "grok-4-fast-reasoning",
		}[canonicalizeAlias(opts.ModelOverride)],
		Temperature:       opts.Temperature,
		MaxOutputTokens:   opts.MaxOutputTokens,
		GroundingEnabled:  opts.GroundingEnabled,
		SystemInstruction: opts.SystemInstruction,
	}
}

func (c *Client) normalizeOptionsForClaude(opts Options) cl.Options {
    return cl.Options{
        ModelOverride: map[string]string{
            "claude_fast":     "claude-haiku-4-5-20251001",
            "claude_balanced": "claude-sonnet-4-5-20250929",
            "claude_quality":  "claude-opus-4-1-20250805",
        }[canonicalizeAlias(opts.ModelOverride)],
        Temperature:       opts.Temperature,
        MaxOutputTokens:   opts.MaxOutputTokens,
        GroundingEnabled:  opts.GroundingEnabled,
        SystemInstruction: opts.SystemInstruction,
    }
}

func (c *Client) normalizeOptionsForDeepSeek(opts Options) ds.Options {
    return ds.Options{
        ModelOverride: map[string]string{
            "deepseek_fast":    "deepseek-chat",
            "deepseek_quality": "deepseek-reasoner",
        }[canonicalizeAlias(opts.ModelOverride)],
        Temperature:       opts.Temperature,
        MaxOutputTokens:   opts.MaxOutputTokens,
        GroundingEnabled:  opts.GroundingEnabled,
        SystemInstruction: opts.SystemInstruction,
    }
}

func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
	switch route(opts) {
	case "openai":
		if c.oa == nil {
			return nil, errors.New("openai not configured")
		}
		oar, err := c.oa.Generate(ctx, prompt, toOATools(tools), c.normalizeOptionsForOpenAI(opts))
		if err != nil {
			return nil, err
		}
		gr := &GenerateResponse{Content: oar.Content, ToolCalls: toGemCalls(oar.ToolCalls), IsToolStep: oar.IsToolStep, Usage: toGemUsage(oar.Usage), Model: oar.Model}
		gr.RawJSON = oar.RawJSON
		gr.OutputRaw = oar.OutputRaw
		// propagate extras
		gr.SearchQueries = oar.SearchQueries
		gr.Sources = oar.Sources
		return gr, nil
	case "grok":
		if c.gr == nil {
			return nil, errors.New("grok not configured")
		}
		r, err := c.gr.Generate(ctx, prompt, toGrokTools(tools), c.normalizeOptionsForGrok(opts))
		if err != nil {
			return nil, err
		}
		return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromGrok(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromGrok(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
    case "claude":
        if c.cl == nil { return nil, errors.New("claude not configured") }
        r, err := c.cl.Generate(ctx, prompt, toClaudeTools(tools), c.normalizeOptionsForClaude(opts))
        if err != nil { return nil, err }
        return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromClaude(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromClaude(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
    case "deepseek":
        if c.ds == nil { return nil, errors.New("deepseek not configured") }
        r, err := c.ds.Generate(ctx, prompt, toDeepSeekTools(tools), c.normalizeOptionsForDeepSeek(opts))
        if err != nil { return nil, err }
        return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromDeepSeek(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromDeepSeek(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
	default:
		goOpts := c.normalizeOptionsForGemini(opts)
		return c.gem.Generate(ctx, prompt, tools, goOpts)
	}
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
		gr.RawJSON = oar.RawJSON
		gr.OutputRaw = oar.OutputRaw
		gr.SearchQueries = oar.SearchQueries
		gr.Sources = oar.Sources
		return gr, nil
    }
    if route(opts) == "claude" {
        if c.cl == nil { return nil, errors.New("claude not configured") }
        // Best-effort: enforce JSON via instruction and function tool with schema
        o := c.normalizeOptionsForClaude(opts)
        if !strings.Contains(strings.ToLower(o.SystemInstruction), "json") {
            o.SystemInstruction = strings.TrimSpace(o.SystemInstruction + "\nRespond strictly in JSON.")
        }
        t := tools
        if len(jsonSchema) > 0 {
            t = append([]Tool{}, tools...)
            t = append(t, Tool{Name: "return_structured", Description: "Return strictly by schema", Parameters: jsonSchema})
        }
        r, err := c.cl.Generate(ctx, prompt, toClaudeTools(t), o)
        if err != nil { return nil, err }
        return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromClaude(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromClaude(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
    }
    if route(opts) == "deepseek" {
        if c.ds == nil { return nil, errors.New("deepseek not configured") }
        o := c.normalizeOptionsForDeepSeek(opts)
        if !strings.Contains(strings.ToLower(o.SystemInstruction), "json") {
            o.SystemInstruction = strings.TrimSpace(o.SystemInstruction + "\nRespond strictly in JSON.")
        }
        t := tools
        if len(jsonSchema) > 0 {
            t = append([]Tool{}, tools...)
            t = append(t, Tool{Name: "return_structured", Description: "Return strictly by schema", Parameters: jsonSchema})
        }
        r, err := c.ds.Generate(ctx, prompt, toDeepSeekTools(t), o)
        if err != nil { return nil, err }
        return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromDeepSeek(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromDeepSeek(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
    }
    return c.gem.GenerateJSON(ctx, prompt, tools, jsonSchema, c.normalizeOptionsForGemini(opts))
}

// GenerateJSONStrict enforces structured output using provider-native strict schema when supported.
func (c *Client) GenerateJSONStrict(ctx context.Context, prompt string, tools []Tool, jsonSchema map[string]interface{}, opts Options) (*GenerateResponse, error) {
	switch route(opts) {
	case "openai":
		// Fallback: reuse JSON mode; OpenAI strictness handled via system instruction or tool defs.
		return c.GenerateJSON(ctx, prompt, tools, jsonSchema, opts)
    case "grok":
		if c.gr == nil {
			return nil, errors.New("grok not configured")
		}
		// Grok structured outputs via function calling (strict by tool/parameters)
		r, err := c.gr.Generate(ctx, prompt, toGrokTools(tools), c.normalizeOptionsForGrok(opts))
		if err != nil {
			return nil, err
		}
		return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromGrok(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromGrok(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
    case "claude":
        if c.cl == nil { return nil, errors.New("claude not configured") }
        // Use function tool with strict schema and required tool choice via instruction
        t := append([]Tool{}, tools...)
        // Deduplicate return_structured if already present
        hasReturn := false
        for _, tt := range t { if strings.EqualFold(strings.TrimSpace(tt.Name), "return_structured") { hasReturn = true; break } }
        if !hasReturn {
            t = append(t, Tool{Name: "return_structured", Description: "Return strictly by schema", Parameters: jsonSchema})
        }
        o := c.normalizeOptionsForClaude(opts)
        if !strings.Contains(strings.ToLower(o.SystemInstruction), "only by calling the function") {
            o.SystemInstruction = strings.TrimSpace(o.SystemInstruction + "\nReturn the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.")
        }
        r, err := c.cl.Generate(ctx, prompt, toClaudeTools(t), o)
        if err != nil { return nil, err }
        return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromClaude(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromClaude(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
    case "deepseek":
        if c.ds == nil { return nil, errors.New("deepseek not configured") }
        t := append([]Tool{}, tools...)
        // Deduplicate return_structured if already present
        hasReturn := false
        for _, tt := range t { if strings.EqualFold(strings.TrimSpace(tt.Name), "return_structured") { hasReturn = true; break } }
        if !hasReturn {
            t = append(t, Tool{Name: "return_structured", Description: "Return strictly by schema", Parameters: jsonSchema})
        }
        o := c.normalizeOptionsForDeepSeek(opts)
        if !strings.Contains(strings.ToLower(o.SystemInstruction), "only by calling the function") {
            o.SystemInstruction = strings.TrimSpace(o.SystemInstruction + "\nReturn the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.")
        }
        r, err := c.ds.Generate(ctx, prompt, toDeepSeekTools(t), o)
        if err != nil { return nil, err }
        return &GenerateResponse{Content: r.Content, ToolCalls: toGemToolsFromDeepSeek(r.ToolCalls), IsToolStep: r.IsToolStep, Usage: toGemUsageFromDeepSeek(r.Usage), Model: r.Model, RawJSON: r.RawJSON, OutputRaw: r.OutputRaw, SearchQueries: r.SearchQueries, Sources: r.Sources}, nil
	default:
		return c.gem.GenerateJSONStrict(ctx, prompt, tools, jsonSchema, c.normalizeOptionsForGemini(opts))
	}
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
	switch route(opts) {
    case "openai":
        if c.oa == nil { return errors.New("openai not configured") }
        _ = onEvent(StreamEvent{Type: "start"})
        u, qs, srcs, err := c.oa.StreamWithExtras(ctx, prompt, toOATools(tools), c.normalizeOptionsForOpenAI(opts), func(delta string) error {
            return onEvent(StreamEvent{Type: "chunk", Text: delta})
        })
        if err != nil {
            _ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
            return err
        }
        return onEvent(StreamEvent{Type: "done", Usage: toGemUsage(u), SearchQueries: qs, Sources: srcs})
    case "grok":
        if c.gr == nil { return errors.New("grok not configured") }
        return c.gr.Stream(ctx, prompt, toGrokTools(tools), c.normalizeOptionsForGrok(opts), func(ev gr.StreamEvent) error {
            switch ev.Type {
            case "start":
                return onEvent(StreamEvent{Type: "start"})
            case "chunk":
                return onEvent(StreamEvent{Type: "chunk", Text: ev.Text})
            case "tool_call":
                return onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemToolsFromGrok(ev.ToolCalls)})
            case "done":
                return onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromGrok(ev.Usage), Model: ev.Model, SearchQueries: ev.SearchQueries, Sources: ev.Sources})
            case "error":
                return onEvent(StreamEvent{Type: "error", Error: ev.Error})
            default:
                return nil
            }
        })
    case "claude":
        if c.cl == nil { return errors.New("claude not configured") }
        o := c.normalizeOptionsForClaude(opts)
        return c.cl.Stream(ctx, prompt, toClaudeTools(tools), o, func(ev cl.StreamEvent) error {
            switch ev.Type {
            case "start":
                return onEvent(StreamEvent{Type: "start"})
            case "chunk":
                return onEvent(StreamEvent{Type: "chunk", Text: ev.Text})
            case "tool_call":
                return onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemToolsFromClaude(ev.ToolCalls)})
            case "done":
                return onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromClaude(ev.Usage), Model: ev.Model})
            case "error":
                return onEvent(StreamEvent{Type: "error", Error: ev.Error})
            default:
                return nil
            }
        })
    case "deepseek":
        if c.ds == nil { return errors.New("deepseek not configured") }
        o := c.normalizeOptionsForDeepSeek(opts)
        return c.ds.Stream(ctx, prompt, toDeepSeekTools(tools), o, func(ev ds.StreamEvent) error {
            switch ev.Type {
            case "start":
                return onEvent(StreamEvent{Type: "start"})
            case "chunk":
                return onEvent(StreamEvent{Type: "chunk", Text: ev.Text})
            case "tool_call":
                return onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemToolsFromDeepSeek(ev.ToolCalls)})
            case "done":
                return onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromDeepSeek(ev.Usage), Model: ev.Model})
            case "error":
                return onEvent(StreamEvent{Type: "error", Error: ev.Error})
            default:
                return nil
            }
        })
	default:
		return c.gem.Stream(ctx, prompt, tools, c.normalizeOptionsForGemini(opts), onEvent)
	}
}

func (c *Client) StreamWithTools(ctx context.Context, prompt string, tools []Tool, reg g.ToolExecutor, opts Options, onEvent func(StreamEvent) error) error {
	switch route(opts) {
	case "openai":
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
			_ = onEvent(StreamEvent{Type: "done", Usage: toGemUsage(r.Usage), SearchQueries: r.SearchQueries, Sources: r.Sources})
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
		// Merge SearchQueries and Sources from both calls with deduplication
		mergedQueries := mergeUniqueStrings(r.SearchQueries, r2.SearchQueries)
		mergedSources := mergeUniqueStrings(r.Sources, r2.Sources)
		_ = onEvent(StreamEvent{Type: "done", Usage: toGemUsage(r2.Usage), SearchQueries: mergedQueries, Sources: mergedSources})
		return nil
    case "grok":
		if c.gr == nil {
			return errors.New("grok not configured")
		}
		_ = onEvent(StreamEvent{Type: "start"})
		// Single-step call to Grok
		r, err := c.gr.Generate(ctx, prompt, toGrokTools(tools), c.normalizeOptionsForGrok(opts))
		if err != nil {
			_ = onEvent(StreamEvent{Type: "error", Error: err.Error()})
			return err
		}
		if len(r.ToolCalls) == 0 {
			if r.Content != "" {
				_ = onEvent(StreamEvent{Type: "chunk", Text: r.Content})
			}
			_ = onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromGrok(r.Usage), SearchQueries: r.SearchQueries, Sources: r.Sources})
			return nil
		}
		// Emit tool_call(s). For now, do not perform follow-up Respond for Grok.
		_ = onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemToolsFromGrok(r.ToolCalls)})
		_ = onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromGrok(r.Usage)})
		return nil
    case "claude":
        if c.cl == nil { return errors.New("claude not configured") }
        // For now, emit tool_call events but do not perform follow-up tool execution in streaming mode
        o := c.normalizeOptionsForClaude(opts)
        return c.cl.Stream(ctx, prompt, toClaudeTools(tools), o, func(ev cl.StreamEvent) error {
            switch ev.Type {
            case "start":
                return onEvent(StreamEvent{Type: "start"})
            case "chunk":
                return onEvent(StreamEvent{Type: "chunk", Text: ev.Text})
            case "tool_call":
                return onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemToolsFromClaude(ev.ToolCalls)})
            case "done":
                return onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromClaude(ev.Usage), Model: ev.Model})
            case "error":
                return onEvent(StreamEvent{Type: "error", Error: ev.Error})
            default:
                return nil
            }
        })
    case "deepseek":
        if c.ds == nil { return errors.New("deepseek not configured") }
        o := c.normalizeOptionsForDeepSeek(opts)
        // Emit tool_call events; no follow-up execution in streaming path
        return c.ds.Stream(ctx, prompt, toDeepSeekTools(tools), o, func(ev ds.StreamEvent) error {
            switch ev.Type {
            case "start":
                return onEvent(StreamEvent{Type: "start"})
            case "chunk":
                return onEvent(StreamEvent{Type: "chunk", Text: ev.Text})
            case "tool_call":
                return onEvent(StreamEvent{Type: "tool_call", ToolCalls: toGemToolsFromDeepSeek(ev.ToolCalls)})
            case "done":
                return onEvent(StreamEvent{Type: "done", Usage: toGemUsageFromDeepSeek(ev.Usage), Model: ev.Model})
            case "error":
                return onEvent(StreamEvent{Type: "error", Error: ev.Error})
            default:
                return nil
            }
        })
	default:
		return c.gem.StreamWithTools(ctx, prompt, tools, reg, c.normalizeOptionsForGemini(opts), onEvent)
	}
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
	return g.Usage{PromptTokens: u.PromptTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens, ReasoningTokens: u.ReasoningTokens, ToolUsePromptTokens: u.ToolUsePromptTokens}
}

func toClaudeTools(ts []Tool) []cl.Tool {
    out := make([]cl.Tool, 0, len(ts))
    for _, t := range ts { out = append(out, cl.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters}) }
    return out
}
func toGemToolsFromClaude(cs []cl.ToolCall) []g.ToolCall {
    out := make([]g.ToolCall, 0, len(cs))
    for _, c := range cs { out = append(out, g.ToolCall{Name: c.Name, Arguments: c.Arguments, CallID: c.CallID}) }
    return out
}
func toGemUsageFromClaude(u cl.Usage) g.Usage { return g.Usage{PromptTokens: u.PromptTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens} }

func toDeepSeekTools(ts []Tool) []ds.Tool {
    out := make([]ds.Tool, 0, len(ts))
    for _, t := range ts { out = append(out, ds.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters}) }
    return out
}
func toGemToolsFromDeepSeek(cs []ds.ToolCall) []g.ToolCall {
    out := make([]g.ToolCall, 0, len(cs))
    for _, c := range cs { out = append(out, g.ToolCall{Name: c.Name, Arguments: c.Arguments, CallID: c.CallID}) }
    return out
}
func toGemUsageFromDeepSeek(u ds.Usage) g.Usage { return g.Usage{PromptTokens: u.PromptTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens} }

func toGrokTools(ts []Tool) []gr.Tool {
	out := make([]gr.Tool, 0, len(ts))
	for _, t := range ts {
		out = append(out, gr.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

func toGemToolsFromGrok(cs []gr.ToolCall) []g.ToolCall {
	out := make([]g.ToolCall, 0, len(cs))
	for _, c := range cs {
		out = append(out, g.ToolCall{Name: c.Name, Arguments: c.Arguments, CallID: c.CallID})
	}
	return out
}

func toGemUsageFromGrok(u gr.Usage) g.Usage {
	return g.Usage{PromptTokens: u.PromptTokens, OutputTokens: u.OutputTokens, TotalTokens: u.TotalTokens, ReasoningTokens: u.ReasoningTokens}
}

// mergeUniqueStrings combines two string slices and removes duplicates
func mergeUniqueStrings(a, b []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

// GenAIGenerate performs a unified generation across providers with optional tools, web search, and strict JSON
func (c *Client) GenAIGenerate(ctx context.Context, req GenAIRequest) (*GenAIResponse, error) {
	// Build base options
	opts := g.Options{
		ModelOverride:     canonicalizeAlias(req.Model),
		Temperature:       req.Temperature,
		MaxOutputTokens:   req.MaxOutputTokens,
		GroundingEnabled:  req.WebSearchEnabled,
		SystemInstruction: req.SystemInstruction,
	}
	tools := req.Tools

	// Helper: merge extras from grounded call
	mergeExtras := func(base *GenerateResponse, extra *GenerateResponse) *GenerateResponse {
		// usage: sum tokens when both present
		base.Usage.PromptTokens += extra.Usage.PromptTokens
		base.Usage.OutputTokens += extra.Usage.OutputTokens
		base.Usage.TotalTokens += extra.Usage.TotalTokens
		if extra.Usage.ReasoningTokens > 0 {
			base.Usage.ReasoningTokens += extra.Usage.ReasoningTokens
		}
		if extra.Usage.ToolUsePromptTokens > 0 {
			base.Usage.ToolUsePromptTokens += extra.Usage.ToolUsePromptTokens
		}
		// sources/queries/attributions
		base.SearchQueries = append(base.SearchQueries, extra.SearchQueries...)
		base.Sources = append(base.Sources, extra.Sources...)
		base.Attributions = append(base.Attributions, extra.Attributions...)
		return base
	}

	provider := route(opts)

	// If strict JSON requested
	if req.StrictJSONEnabled {
		// Special-case Gemini: strict JSON cannot run with grounding/tools in some modes
		if provider == "gemini" && req.WebSearchEnabled {
			// Step 1: strict JSON without grounding
			opts.GroundingEnabled = false
			strict, err := c.gem.GenerateJSONStrict(ctx, req.Prompt, tools, req.JSONSchema, c.normalizeOptionsForGemini(opts))
			if err != nil {
				return nil, err
			}
			// Step 2: grounded pass without tools (to fetch sources/queries)
			gopts := opts
			gopts.GroundingEnabled = true
			grounded, gerr := c.gem.Generate(ctx, req.Prompt, nil, c.normalizeOptionsForGemini(gopts))
			if gerr == nil {
				strict = mergeExtras(strict, grounded)
			}
			return (*GenAIResponse)(strict), nil
		}
		// Other providers: attempt unified strict generation
		strict, err := c.GenerateJSONStrict(ctx, req.Prompt, tools, req.JSONSchema, opts)
		if err != nil {
			return nil, err
		}
		return (*GenAIResponse)(strict), nil
	}

	// Non-strict path
	// Ensure web search/tools applied when requested
	if !req.ToolCallingEnabled {
		// If tools disabled, clear tools slice
		tools = nil
	}
	// Special-case Gemini: tools + web search unsupported together → two-step
	if provider == "gemini" && req.ToolCallingEnabled && req.WebSearchEnabled {
		// Step 1: tools only
		to := opts
		to.GroundingEnabled = false
		first, err := c.gem.Generate(ctx, req.Prompt, tools, c.normalizeOptionsForGemini(to))
		if err != nil {
			return nil, err
		}
		// Step 2: grounded only (no tools)
		go2 := opts
		go2.GroundingEnabled = true
		second, gerr := c.gem.Generate(ctx, req.Prompt, nil, c.normalizeOptionsForGemini(go2))
		if gerr == nil {
			first = mergeExtras(first, second)
		}
		return (*GenAIResponse)(first), nil
	}
	res, err := c.Generate(ctx, req.Prompt, tools, opts)
	if err != nil {
		return nil, err
	}
	return (*GenAIResponse)(res), nil
}
