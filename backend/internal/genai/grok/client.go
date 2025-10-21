package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

//
// Test outcomes observed (2025-10-19) using unified test runner with 8 scenarios per model:
// Scenarios per model (loose = без схемы, strict = со строгой JSON-схемой):
//  1) loose: Q/A only
//  2) loose: Q/A + tool
//  3) loose: Q/A + search
//  4) loose: Q/A + tool + search
//  5) strict: Q/A only (schema)
//  6) strict: Q/A + tool (schema)
//  7) strict: Q/A + search (schema)
//  8) strict: Q/A + tool + search (schema)
//
// grok_fast (grok-4-fast-reasoning) — all 8 scenarios completed successfully.
// - Tool-calling срабатывает (возвращает function calls) в сценариях с tool.
// - Search источники извлекаются; URL парсятся и попадают в Sources.
//
// grok_quality (grok-4-0709) — частичные таймауты:
// - Успешно: loose Q/A only, loose Q/A + search, strict Q/A only, strict Q/A + tool, strict Q/A + search
// - Таймаут: loose Q/A + tool, loose Q/A + tool + search, strict Q/A + tool + search
// - В некоторых успешных ответах встречались «refusal»-сообщения; Raw JSON сохраняется.
//
// Доп. заметка: В отдельных ответах встречались хвостовые символы у URL (например, кавычка или фигурная скобка). Базовая очистка выполнена
// в extractURLsFromText; при необходимости можно расширить список символов под дополнительные случаи.

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	CallID    string `json:"callId,omitempty"`
}

type Usage struct {
	PromptTokens    int `json:"promptTokens"`
	OutputTokens    int `json:"outputTokens"`
	TotalTokens     int `json:"totalTokens"`
	ReasoningTokens int `json:"reasoningTokens,omitempty"`
}

type GenerateResponse struct {
	Content       string                   `json:"content"`
	ToolCalls     []ToolCall               `json:"toolCalls,omitempty"`
	IsToolStep    bool                     `json:"isToolStep"`
	Usage         Usage                    `json:"usage"`
	Model         string                   `json:"model"`
	OutputRaw     []map[string]interface{} `json:"-"`
	RawJSON       string                   `json:"-"`
	SearchQueries []string                 `json:"searchQueries,omitempty"`
	Sources       []string                 `json:"sources,omitempty"`
}

type Options struct {
	ModelOverride     string  `json:"modelOverride"`
	Temperature       float64 `json:"temperature"`
	MaxOutputTokens   int     `json:"maxOutputTokens"`
	GroundingEnabled  bool    `json:"groundingEnabled"`
	SystemInstruction string  `json:"systemInstruction"`
}

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	key := strings.Trim(strings.TrimSpace(apiKey), "\"'")
	// Disable built-in timeout; rely on caller context deadlines
	return &Client{apiKey: key, httpClient: &http.Client{}}
}

func (c *Client) modelFrom(opts Options) string {
	if strings.TrimSpace(opts.ModelOverride) != "" {
		return opts.ModelOverride
	}
	return "grok-4-0709"
}

// Generate against xAI Chat Completions API with function tools and optional web search tool
func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
	model := c.modelFrom(opts)
	msgs := make([]map[string]interface{}, 0, 2)
	if opts.SystemInstruction != "" {
		msgs = append(msgs, map[string]interface{}{"role": "system", "content": opts.SystemInstruction})
	}
	msgs = append(msgs, map[string]interface{}{"role": "user", "content": prompt})
	p := map[string]interface{}{
		"model":    model,
		"messages": msgs,
	}
	// Enable live search at request level when requested
	if opts.GroundingEnabled {
		p["search"] = map[string]interface{}{
			"enable":  true,
			"sources": []map[string]string{{"type": "web"}},
		}
	}
	if opts.MaxOutputTokens > 0 {
		p["max_tokens"] = opts.MaxOutputTokens
	}
	if opts.Temperature > 0 {
		p["temperature"] = opts.Temperature
	}
	if len(tools) > 0 {
		toolDefs := make([]map[string]interface{}, 0, len(tools)+1)
		for _, t := range tools {
			toolDefs = append(toolDefs, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		p["tools"] = toolDefs
		p["tool_choice"] = "auto"
	}
	body, _ := json.Marshal(p)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.x.ai/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("grok error %d: %s", resp.StatusCode, string(rb))
	}
	var root map[string]interface{}
	if err := json.Unmarshal(rb, &root); err != nil {
		return nil, err
	}
	out := &GenerateResponse{Model: model}
	out.RawJSON = string(rb)
	if usage, ok := root["usage"].(map[string]interface{}); ok {
		if v, ok := usage["prompt_tokens"].(float64); ok {
			out.Usage.PromptTokens = int(v)
		}
		if v, ok := usage["completion_tokens"].(float64); ok {
			out.Usage.OutputTokens = int(v)
		}
		if v, ok := usage["total_tokens"].(float64); ok {
			out.Usage.TotalTokens = int(v)
		}
		if v, ok := usage["reasoning_tokens"].(float64); ok {
			out.Usage.ReasoningTokens = int(v)
		}
	}
	if choices, ok := root["choices"].([]interface{}); ok && len(choices) > 0 {
		if ch, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := ch["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					out.Content = content
					extractURLsFromText(content, &out.Sources)
				}
				if tcs, ok := msg["tool_calls"].([]interface{}); ok {
					for _, tc := range tcs {
						if m, ok := tc.(map[string]interface{}); ok {
							if f, ok := m["function"].(map[string]interface{}); ok {
								name, _ := f["name"].(string)
								args := ""
								if s, ok := f["arguments"].(string); ok {
									args = s
									extractURLsFromText(s, &out.Sources)
								} else if obj, ok := f["arguments"].(map[string]interface{}); ok {
									b, _ := json.Marshal(obj)
									args = string(b)
									extractURLsFromMap(obj, &out.Sources)
								}
								id, _ := m["id"].(string)
								out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: args, CallID: id})
							}
						}
					}
				}
			}
		}
	}
	out.IsToolStep = len(out.ToolCalls) > 0 && out.Content == ""
	return out, nil
}

// Helpers for extracting URLs into Sources
func appendUnique(dst *[]string, v string) {
	for _, x := range *dst {
		if x == v {
			return
		}
	}
	*dst = append(*dst, v)
}
func extractURLsFromText(s string, dst *[]string) {
	parts := strings.Fields(s)
	for _, p := range parts {
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			// Do not trim ')' to preserve URLs with parentheses in the path (e.g., Wikipedia)
			u := strings.TrimRight(p, ".,;]\\\"'")
			appendUnique(dst, u)
		}
	}
}
func extractURLsFromMap(m map[string]interface{}, dst *[]string) {
	for _, v := range m {
		switch tv := v.(type) {
		case string:
			extractURLsFromText(tv, dst)
		case map[string]interface{}:
			extractURLsFromMap(tv, dst)
		case []interface{}:
			for _, it := range tv {
				if sm, ok := it.(map[string]interface{}); ok {
					extractURLsFromMap(sm, dst)
				} else if ss, ok := it.(string); ok {
					extractURLsFromText(ss, dst)
				}
			}
		}
	}
}
