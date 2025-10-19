package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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
	PromptTokens int `json:"promptTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type GenerateResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	IsToolStep bool       `json:"isToolStep"`
	Usage      Usage      `json:"usage"`
	Model      string     `json:"model"`
	// Raw output items from Responses API, used for follow-ups with function_call_output
	OutputRaw []map[string]interface{} `json:"-"`
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
	return &Client{apiKey: key, httpClient: &http.Client{Timeout: 120 * time.Second}}
}

func (c *Client) modelFrom(opts Options) string {
	if opts.ModelOverride != "" {
		return opts.ModelOverride
	}
	return "gpt-5"
}

// Generate uses Responses API (non-stream) and returns first text output; supports function tools
func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
	model := c.modelFrom(opts)
	// Build OpenAI Responses payload
	input := []map[string]interface{}{}
	if opts.SystemInstruction != "" {
		// Provide system guidance also as a system message for stronger steer
		input = append(input, map[string]interface{}{"role": "system", "content": opts.SystemInstruction})
	}
	input = append(input, map[string]interface{}{"role": "user", "content": prompt})
	req := map[string]interface{}{
		"model": model,
		"input": input,
	}
	if opts.SystemInstruction != "" {
		req["instructions"] = opts.SystemInstruction
	}
	// GPT-5 family may not support temperature; omit for gpt-5, gpt-5-mini, gpt-5-nano
	if opts.Temperature > 0 && !(strings.HasPrefix(strings.ToLower(model), "gpt-5")) {
		req["temperature"] = opts.Temperature
	}
	if opts.MaxOutputTokens > 0 {
		req["max_output_tokens"] = opts.MaxOutputTokens
	}
	// Grounding: map to search tool if requested
	if opts.GroundingEnabled {
		req["search"] = map[string]interface{}{"enable": true}
	}
	if len(tools) > 0 {
		tdefs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			// ensure parameters.additionalProperties=false and properties have type
			params := t.Parameters
			if params == nil {
				params = map[string]interface{}{"type": "object"}
			}
			// enforce additionalProperties=false
			params["additionalProperties"] = false
			// backfill missing property types
			if propsRaw, ok := params["properties"].(map[string]interface{}); ok {
				for k, v := range propsRaw {
					if m, ok := v.(map[string]interface{}); ok {
						if _, has := m["type"]; !has {
							m["type"] = "string"
						}
						propsRaw[k] = m
					}
				}
			}
			tdefs = append(tdefs, map[string]interface{}{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
				"strict":      true,
			})
		}
		req["tools"] = tdefs
		// Require at least one function call for tool tests
		req["tool_choice"] = "required"
		// Prevent multiple calls in one turn for simplicity
		req["parallel_tool_calls"] = false
	}
	b, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(b))
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(rb))
	}
	// Minimal parse: look for output_text and tool calls
	var root map[string]interface{}
	if err := json.Unmarshal(rb, &root); err != nil {
		return nil, err
	}
	out := &GenerateResponse{Model: model}
	if txt, _ := root["output_text"].(string); txt != "" {
		out.Content = txt
	}
	// Parse tool calls and assistant messages (best-effort)
	if arr, ok := root["output"].([]interface{}); ok {
		// capture raw items for follow-up calls
		out.OutputRaw = make([]map[string]interface{}, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				out.OutputRaw = append(out.OutputRaw, m)
				typ, _ := m["type"].(string)
				if typ == "function_call" {
					name, _ := m["name"].(string)
					var argsJSON string
					if obj, ok := m["arguments"].(map[string]interface{}); ok {
						if b, err := json.Marshal(obj); err == nil {
							argsJSON = string(b)
						}
					} else if s, ok := m["arguments"].(string); ok {
						argsJSON = s
					}
					callID, _ := m["call_id"].(string)
					out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: argsJSON, CallID: callID})
				} else if typ == "message" {
					if content, ok := m["content"].([]interface{}); ok {
						for _, c := range content {
							if cm, ok := c.(map[string]interface{}); ok {
								if txt, _ := cm["text"].(string); txt != "" {
									out.Content += txt
								}
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

// Respond allows passing a custom input list (for function_call_output follow-ups)
func (c *Client) Respond(ctx context.Context, input []map[string]interface{}, tools []Tool, opts Options) (*GenerateResponse, error) {
	model := c.modelFrom(opts)
	req := map[string]interface{}{
		"model": model,
		"input": input,
	}
	if opts.SystemInstruction != "" {
		req["instructions"] = opts.SystemInstruction
	}
	if opts.MaxOutputTokens > 0 {
		req["max_output_tokens"] = opts.MaxOutputTokens
	}
	if opts.GroundingEnabled {
		req["search"] = map[string]interface{}{"enable": true}
	}
	if len(tools) > 0 {
		tdefs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			params := t.Parameters
			if params == nil {
				params = map[string]interface{}{"type": "object"}
			}
			params["additionalProperties"] = false
			if propsRaw, ok := params["properties"].(map[string]interface{}); ok {
				for k, v := range propsRaw {
					if m, ok := v.(map[string]interface{}); ok {
						if _, has := m["type"]; !has {
							m["type"] = "string"
						}
						propsRaw[k] = m
					}
				}
			}
			tdefs = append(tdefs, map[string]interface{}{"type": "function", "name": t.Name, "description": t.Description, "parameters": params, "strict": true})
		}
		req["tools"] = tdefs
		req["tool_choice"] = "required"
		req["parallel_tool_calls"] = false
	}
	b, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(b))
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(rb))
	}
	var root map[string]interface{}
	if err := json.Unmarshal(rb, &root); err != nil {
		return nil, err
	}
	out := &GenerateResponse{Model: model}
	if txt, _ := root["output_text"].(string); txt != "" {
		out.Content = txt
	}
	if arr, ok := root["output"].([]interface{}); ok {
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				if m["type"] == "function_call" {
					name, _ := m["name"].(string)
					args, _ := m["arguments"].(string)
					callID, _ := m["call_id"].(string)
					out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: args, CallID: callID})
				}
			}
		}
	}
	out.IsToolStep = len(out.ToolCalls) > 0 && out.Content == ""
	return out, nil
}

// Stream streams text deltas using Responses API SSE
func (c *Client) Stream(ctx context.Context, prompt string, tools []Tool, opts Options, onDelta func(string) error) error {
	model := c.modelFrom(opts)
	input := []map[string]interface{}{}
	if opts.SystemInstruction != "" {
		input = append(input, map[string]interface{}{"role": "system", "content": opts.SystemInstruction})
	}
	input = append(input, map[string]interface{}{"role": "user", "content": prompt})
	req := map[string]interface{}{
		"model":  model,
		"input":  input,
		"stream": true,
	}
	if opts.SystemInstruction != "" {
		req["instructions"] = opts.SystemInstruction
	}
	if opts.MaxOutputTokens > 0 {
		req["max_output_tokens"] = opts.MaxOutputTokens
	}
	if opts.GroundingEnabled {
		req["search"] = map[string]interface{}{"enable": true}
	}
	if len(tools) > 0 {
		tdefs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			tdefs = append(tdefs, map[string]interface{}{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": true})
		}
		req["tools"] = tdefs
		// do not force required in streaming basic path; let the model decide
		req["parallel_tool_calls"] = false
	}
	b, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(b))
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai stream error %d: %s", resp.StatusCode, string(rb))
	}
	reader := bufio.NewReader(resp.Body)
	var dataBuf strings.Builder
	flushEvent := func() error {
		payload := strings.TrimSpace(dataBuf.String())
		dataBuf.Reset()
		if payload == "" {
			return nil
		}
		if payload == "[DONE]" {
			return io.EOF
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			// best-effort: ignore non-JSON
			return nil
		}
		// Heuristics for Responses SSE: prefer output_text.delta
		if typ, _ := m["type"].(string); typ != "" {
			lt := strings.ToLower(typ)
			if strings.Contains(lt, "output_text.delta") {
				if d, _ := m["delta"].(string); d != "" {
					return onDelta(d)
				}
			}
			if strings.Contains(lt, "response.completed") {
				return io.EOF
			}
		}
		if d, _ := m["delta"].(string); d != "" {
			return onDelta(d)
		}
		if txt, _ := m["output_text"].(string); txt != "" {
			return onDelta(txt)
		}
		if content, ok := m["content"].([]interface{}); ok {
			var sb strings.Builder
			for _, c := range content {
				if cm, ok := c.(map[string]interface{}); ok {
					if t, _ := cm["text"].(string); t != "" {
						sb.WriteString(t)
					}
				}
			}
			if sb.Len() > 0 {
				return onDelta(sb.String())
			}
		}
		return nil
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return nil
			}
			if data != "" {
				dataBuf.WriteString(data)
			}
		} else if line == "" { // end of event
			if err := flushEvent(); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
	// flush any remaining
	_ = flushEvent()
	return nil
}
