package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

// ToolExecutor executes whitelisted tools with timeouts
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)
	IsAllowed(name string) bool
}

// SimpleRegistry is a minimal in-memory registry implementation
type SimpleRegistry struct {
	allowed map[string]func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
	tg      *telegram.Client
}

func NewSimpleRegistry() *SimpleRegistry {
	r := &SimpleRegistry{allowed: map[string]func(context.Context, json.RawMessage) (json.RawMessage, error){}}
	// Register a couple of safe built-ins
	r.Register("time_now", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		type Out struct {
			ISO  string `json:"iso"`
			Unix int64  `json:"unix"`
		}
		o := Out{ISO: time.Now().UTC().Format(time.RFC3339), Unix: time.Now().UTC().Unix()}
		b, _ := json.Marshal(o)
		return b, nil
	})
	r.Register("echo", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		// echoes arguments back
		if len(args) == 0 {
			return json.RawMessage([]byte(`{"ok":true}`)), nil
		}
		return args, nil
	})

	// Optional: register Telegram fetch tool if service URL is configured
	if base := strings.TrimSpace(os.Getenv("TELEGRAM_SERVICE_URL")); base != "" {
		r.tg = telegram.NewClient(base)
		r.Register("telegram_fetch_updates", r.telegramFetchUpdates)
	}
	return r
}

func (r *SimpleRegistry) Register(name string, fn func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)) {
	r.allowed[name] = fn
}

func (r *SimpleRegistry) Execute(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	fn, ok := r.allowed[name]
	if !ok {
		return nil, fmt.Errorf("tool not allowed: %s", name)
	}
	// default timeout per tool call
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return fn(cctx, args)
}

func (r *SimpleRegistry) IsAllowed(name string) bool {
	_, ok := r.allowed[name]
	return ok
}

// ExecuteWithTools runs tool-calling loop up to maxDepth
func (c *Client) ExecuteWithTools(ctx context.Context, prompt string, toolDecls []Tool, exec ToolExecutor, opts Options) (*GenerateResponse, error) {
	const maxDepth = 4
	step := 0
	nextPrompt := prompt
	var last *GenerateResponse
	for step < maxDepth {
		step++
		resp, err := c.Generate(ctx, nextPrompt, toolDecls, opts)
		if err != nil {
			return nil, err
		}
		last = resp
		if len(resp.ToolCalls) == 0 {
			return resp, nil
		}
		// gather tool results
		results := make(map[string]json.RawMessage)
		for _, call := range resp.ToolCalls {
			if !exec.IsAllowed(call.Name) {
				return nil, fmt.Errorf("tool not allowed: %s", call.Name)
			}
			var args json.RawMessage = json.RawMessage(call.Arguments)
			out, err := exec.Execute(ctx, call.Name, args)
			if err != nil {
				return nil, err
			}
			results[call.Name] = out
		}
		// feed results back to model as a structured prompt
		b, _ := json.Marshal(map[string]interface{}{"toolResults": results})
		nextPrompt = string(b)
	}
	return last, errors.New("tool loop exceeded max depth")
}

// StreamWithTools performs real-time streaming with tool-calling support (single turn + one tools round-trip)
func (c *Client) StreamWithTools(ctx context.Context, prompt string, toolDecls []Tool, exec ToolExecutor, opts Options, onEvent func(StreamEvent) error) error {
	var pendingCalls []ToolCall
	// first: stream until we either complete or receive tool calls
	err := c.Stream(ctx, prompt, toolDecls, opts, func(e StreamEvent) error {
		switch e.Type {
		case "chunk":
			return onEvent(e)
		case "tool_call":
			pendingCalls = append(pendingCalls, e.ToolCalls...)
			return onEvent(e)
		case "start", "done":
			return onEvent(e)
		case "error":
			return onEvent(e)
		default:
			return onEvent(e)
		}
	})
	if err != nil {
		return err
	}
	if len(pendingCalls) == 0 {
		return nil
	}

	// Execute tools and continue
	results := make([]map[string]interface{}, 0, len(pendingCalls))
	for _, call := range pendingCalls {
		if !exec.IsAllowed(call.Name) {
			return errors.New("tool not allowed: " + call.Name)
		}
		out, err := exec.Execute(ctx, call.Name, json.RawMessage(call.Arguments))
		if err != nil {
			return err
		}
		_ = onEvent(StreamEvent{Type: "tool_result", ToolCalls: []ToolCall{{Name: call.Name, Arguments: string(out)}}})
		results = append(results, map[string]interface{}{"role": "tool", "parts": []map[string]interface{}{{"text": string(out)}}})
	}

	// Continue streaming with tool results appended as contents
	contents := []map[string]interface{}{
		{"role": "user", "parts": []map[string]interface{}{{"text": prompt}}},
	}
	contents = append(contents, results...)
	return c.StreamConversation(ctx, contents, toolDecls, opts, onEvent)
}

// normalizeTelegramChannel converts @name or t.me/name[/...] to username
func normalizeTelegramChannel(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("INVALID_CHANNEL_FORMAT: empty")
	}
	// Handle @username
	s = strings.TrimPrefix(s, "@")
	// URLs: http(s)://t.me/... or t.me/...
	if strings.Contains(strings.ToLower(s), "t.me/") {
		// strip scheme
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		// remove domain
		if strings.HasPrefix(strings.ToLower(s), "t.me/") {
			s = s[len("t.me/"):]
		}
		s = strings.TrimLeft(s, "/")
		parts := strings.Split(s, "/")
		if len(parts) == 0 {
			return "", fmt.Errorf("INVALID_CHANNEL_FORMAT: missing path")
		}
		first := parts[0]
		if first == "c" || first == "+" || strings.HasPrefix(first, "+") || first == "joinchat" {
			return "", fmt.Errorf("PRIVATE_OR_UNSUPPORTED")
		}
		if first == "s" {
			if len(parts) < 2 {
				return "", fmt.Errorf("INVALID_CHANNEL_FORMAT: missing username")
			}
			s = parts[1]
		} else {
			s = first
		}
	}
	// Basic validation: Telegram usernames are 5..32 chars [A-Za-z0-9_]
	if len(s) < 5 || len(s) > 32 {
		return "", fmt.Errorf("INVALID_CHANNEL_FORMAT: bad length")
	}
	for _, ch := range s {
		if !(ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
			return "", fmt.Errorf("INVALID_CHANNEL_FORMAT: invalid character")
		}
	}
	return s, nil
}

// telegramFetchUpdates is the runtime for tool "telegram_fetch_updates"
func (r *SimpleRegistry) telegramFetchUpdates(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	if r.tg == nil {
		return nil, fmt.Errorf("SERVICE_UNAVAILABLE: telegram client not configured")
	}
	var in struct {
		Channel string `json:"channel"`
		LastID  int    `json:"lastId"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("INVALID_ARGUMENTS: %v", err)
	}
	username, err := normalizeTelegramChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	msgs, err := r.tg.FetchUpdates(ctx, username, in.LastID)
	if err != nil {
		return nil, fmt.Errorf("SERVICE_UNAVAILABLE: %v", err)
	}
	// Trim to limit and compute nextLastId
	next := in.LastID
	if len(msgs) > in.Limit {
		msgs = msgs[:in.Limit]
	}
	for _, m := range msgs {
		if m.ID > next {
			next = m.ID
		}
	}
	out := map[string]interface{}{
		"messages":   msgs,
		"nextLastId": next,
		"stats":      map[string]interface{}{"count": len(msgs)},
	}
	b, _ := json.Marshal(out)
	return b, nil
}

// NewTelegramFetchToolDeclaration returns tool declaration for function-calling
func NewTelegramFetchToolDeclaration() Tool {
	return Tool{
		Name:        "telegram_fetch_updates",
		Description: "Fetch recent messages from a public Telegram channel by username",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channel": map[string]interface{}{"type": "string", "description": "Channel reference: @name or t.me/name"},
				"lastId":  map[string]interface{}{"type": "integer", "minimum": 0},
				"limit":   map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100},
			},
			"required":             []string{"channel", "lastId", "limit"},
			"additionalProperties": false,
		},
	}
}
