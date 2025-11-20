package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	PromptTokens        int `json:"promptTokens"`
	OutputTokens        int `json:"outputTokens"`
	TotalTokens         int `json:"totalTokens"`
	ReasoningTokens     int `json:"reasoningTokens,omitempty"`
	ToolUsePromptTokens int `json:"toolUsePromptTokens,omitempty"`
}

type GenerateResponse struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	IsToolStep bool       `json:"isToolStep"`
	Usage      Usage      `json:"usage"`
	Model      string     `json:"model"`
	// Raw output items from Responses API, used for follow-ups with function_call_output
	OutputRaw []map[string]interface{} `json:"-"`
	// RawJSON contains the unmodified JSON body returned by the provider
	RawJSON string `json:"-"`
	// Unified extras for parity with Gemini
	SearchQueries []string `json:"searchQueries,omitempty"`
	Sources       []string `json:"sources,omitempty"`
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
	return &Client{apiKey: key, httpClient: &http.Client{Timeout: 240 * time.Second}}
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
	// Build tools list (function tools + optional web_search)
	tdefs := make([]map[string]interface{}, 0, len(tools)+1)
	hasFuncTools := false
	if len(tools) > 0 {
		hasFuncTools = true
		for _, t := range tools {
			params := t.Parameters
			if params == nil {
				params = map[string]interface{}{"type": "object"}
			}
			params["additionalProperties"] = false
			// Normalize schema for OpenAI: ensure all properties in 'properties' are either in 'required' or have default
			// Also fix array types - OpenAI doesn't support type arrays like ["string", "number"]
			if propsRaw, ok := params["properties"].(map[string]interface{}); ok {
				requiredRaw, hasRequired := params["required"].([]interface{})
				requiredSet := make(map[string]bool)
				if hasRequired {
					for _, r := range requiredRaw {
						if s, ok := r.(string); ok {
							requiredSet[s] = true
						}
					}
				}
				// Build required array with all properties that don't have defaults
				requiredList := make([]string, 0)
				for k, v := range propsRaw {
					if m, ok := v.(map[string]interface{}); ok {
						// Fix array types - OpenAI doesn't support type arrays
						if typeVal, hasType := m["type"]; hasType {
							if typeArr, ok := typeVal.([]interface{}); ok && len(typeArr) > 0 {
								// Convert array type to first type (usually string or number)
								// This is a limitation of OpenAI's strict schema validation
								m["type"] = typeArr[0]
							}
						}
						if _, has := m["type"]; !has {
							m["type"] = "string"
						}
						// Recursively fix nested items (for arrays)
						if items, hasItems := m["items"]; hasItems {
							if itemsMap, ok := items.(map[string]interface{}); ok {
								if itemType, hasItemType := itemsMap["type"]; hasItemType {
									if itemTypeArr, ok := itemType.([]interface{}); ok && len(itemTypeArr) > 0 {
										itemsMap["type"] = itemTypeArr[0]
									}
								}
								if _, hasItemType := itemsMap["type"]; !hasItemType {
									itemsMap["type"] = "string"
								}
								m["items"] = itemsMap
							}
						}
						// If property doesn't have default and is not already required, add it to required
						// OpenAI requires all properties to be in 'required' array
						if _, hasDefault := m["default"]; !hasDefault {
							if !requiredSet[k] {
								requiredList = append(requiredList, k)
							}
						}
						propsRaw[k] = m
					}
				}
				// Merge with existing required
				for k := range requiredSet {
					requiredList = append(requiredList, k)
				}
				params["required"] = requiredList
			}
			tdefs = append(tdefs, map[string]interface{}{
				"type":        "function",
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
				"strict":      true,
			})
		}
	}
	if opts.GroundingEnabled {
		tdefs = append(tdefs, map[string]interface{}{"type": "web_search"})
	}
	if len(tdefs) > 0 {
		req["tools"] = tdefs
		if hasFuncTools {
			req["tool_choice"] = "required"
		} else {
			req["tool_choice"] = "auto"
		}
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
	out.RawJSON = string(rb)
	// Usage
	if u, ok := root["usage"].(map[string]interface{}); ok {
		if v, ok := u["input_tokens"].(float64); ok {
			out.Usage.PromptTokens = int(v)
		}
		if v, ok := u["output_tokens"].(float64); ok {
			out.Usage.OutputTokens = int(v)
		}
		if v, ok := u["total_tokens"].(float64); ok {
			out.Usage.TotalTokens = int(v)
		}
		if d, ok := u["output_tokens_details"].(map[string]interface{}); ok {
			if v, ok := d["reasoning_tokens"].(float64); ok {
				out.Usage.ReasoningTokens = int(v)
			}
		}
	}
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
				switch typ {
				case "function_call":
					name, _ := m["name"].(string)
					var argsJSON string
					if obj, ok := m["arguments"].(map[string]interface{}); ok {
						if b, err := json.Marshal(obj); err == nil {
							argsJSON = string(b)
						}
						// extract URLs from structured args
						extractURLsFromMap(obj, &out.Sources)
					} else if s, ok := m["arguments"].(string); ok {
						argsJSON = s
						extractURLsFromText(s, &out.Sources)
					}
					callID, _ := m["call_id"].(string)
					out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: argsJSON, CallID: callID})
				case "message":
					if content, ok := m["content"].([]interface{}); ok {
						for _, c := range content {
							if cm, ok := c.(map[string]interface{}); ok {
								if txt, _ := cm["text"].(string); txt != "" {
									out.Content += txt
									// Try parse as JSON for {"query":...} or {"sources":[...]}
									var tmp map[string]interface{}
									if json.Unmarshal([]byte(txt), &tmp) == nil {
										if q, ok := tmp["query"].(string); ok && q != "" {
											appendUnique(&out.SearchQueries, q)
										}
										if srcs, ok := tmp["sources"].([]interface{}); ok {
											for _, s := range srcs {
												if sm, ok := s.(map[string]interface{}); ok {
													if u, ok := sm["url"].(string); ok && u != "" {
														appendUnique(&out.Sources, u)
													}
												}
											}
										}
									}
									// Also scan free text for URLs
									extractURLsFromText(txt, &out.Sources)
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
	if len(tools) > 0 {
		tdefs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			params := t.Parameters
			if params == nil {
				params = map[string]interface{}{"type": "object"}
			}
			params["additionalProperties"] = false
			// Normalize schema for OpenAI: ensure all properties in 'properties' are either in 'required' or have default
			// Also fix array types - OpenAI doesn't support type arrays like ["string", "number"]
			if propsRaw, ok := params["properties"].(map[string]interface{}); ok {
				requiredRaw, hasRequired := params["required"].([]interface{})
				requiredSet := make(map[string]bool)
				if hasRequired {
					for _, r := range requiredRaw {
						if s, ok := r.(string); ok {
							requiredSet[s] = true
						}
					}
				}
				// Build required array with all properties that don't have defaults
				requiredList := make([]string, 0)
				for k, v := range propsRaw {
					if m, ok := v.(map[string]interface{}); ok {
						// Fix array types - OpenAI doesn't support type arrays
						if typeVal, hasType := m["type"]; hasType {
							if typeArr, ok := typeVal.([]interface{}); ok && len(typeArr) > 0 {
								// Convert array type to first type (usually string or number)
								// This is a limitation of OpenAI's strict schema validation
								m["type"] = typeArr[0]
							}
						}
						if _, has := m["type"]; !has {
							m["type"] = "string"
						}
						// Recursively fix nested items (for arrays)
						if items, hasItems := m["items"]; hasItems {
							if itemsMap, ok := items.(map[string]interface{}); ok {
								if itemType, hasItemType := itemsMap["type"]; hasItemType {
									if itemTypeArr, ok := itemType.([]interface{}); ok && len(itemTypeArr) > 0 {
										itemsMap["type"] = itemTypeArr[0]
									}
								}
								if _, hasItemType := itemsMap["type"]; !hasItemType {
									itemsMap["type"] = "string"
								}
								m["items"] = itemsMap
							}
						}
						// If property doesn't have default and is not already required, add it to required
						// OpenAI requires all properties to be in 'required' array
						if _, hasDefault := m["default"]; !hasDefault {
							if !requiredSet[k] {
								requiredList = append(requiredList, k)
							}
						}
						propsRaw[k] = m
					}
				}
				// Merge with existing required
				for k := range requiredSet {
					requiredList = append(requiredList, k)
				}
				params["required"] = requiredList
			}
			tdefs = append(tdefs, map[string]interface{}{"type": "function", "name": t.Name, "description": t.Description, "parameters": params, "strict": true})
		}
		if opts.GroundingEnabled {
			tdefs = append(tdefs, map[string]interface{}{"type": "web_search"})
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
	out.RawJSON = string(rb)
	if u, ok := root["usage"].(map[string]interface{}); ok {
		if v, ok := u["input_tokens"].(float64); ok {
			out.Usage.PromptTokens = int(v)
		}
		if v, ok := u["output_tokens"].(float64); ok {
			out.Usage.OutputTokens = int(v)
		}
		if v, ok := u["total_tokens"].(float64); ok {
			out.Usage.TotalTokens = int(v)
		}
		if d, ok := u["output_tokens_details"].(map[string]interface{}); ok {
			if v, ok := d["reasoning_tokens"].(float64); ok {
				out.Usage.ReasoningTokens = int(v)
			}
		}
	}
	if txt, _ := root["output_text"].(string); txt != "" {
		out.Content = txt
	}
	if arr, ok := root["output"].([]interface{}); ok {
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				if typ, ok := m["type"].(string); ok {
					switch typ {
					case "function_call":
						name, _ := m["name"].(string)
						args, _ := m["arguments"].(string)
						callID, _ := m["call_id"].(string)
						out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: args, CallID: callID})
					case "message":
						if content, ok := m["content"].([]interface{}); ok {
							for _, c := range content {
								if cm, ok := c.(map[string]interface{}); ok {
									if txt, _ := cm["text"].(string); txt != "" {
										out.Content += txt
										var tmp map[string]interface{}
										if json.Unmarshal([]byte(txt), &tmp) == nil {
											if q, ok := tmp["query"].(string); ok && q != "" {
												appendUnique(&out.SearchQueries, q)
											}
											if srcs, ok := tmp["sources"].([]interface{}); ok {
												for _, s := range srcs {
													if sm, ok := s.(map[string]interface{}); ok {
														if u, ok := sm["url"].(string); ok && u != "" {
															appendUnique(&out.Sources, u)
														}
													}
												}
											}
										}
										extractURLsFromText(txt, &out.Sources)
									}
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

// Helpers for parsing queries/URLs
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
			// Preserve closing ')' to keep Wikipedia-style URLs with parentheses intact
			u := strings.TrimRight(p, ".,;]\"'}")
			// If we accidentally have an opening '(' without matching ')', try to add ')'
			if strings.Count(u, "(") > strings.Count(u, ")") {
				u = u + ")"
			}
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
	if len(tools) > 0 {
		tdefs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			tdefs = append(tdefs, map[string]interface{}{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters, "strict": true})
		}
		if opts.GroundingEnabled {
			tdefs = append(tdefs, map[string]interface{}{"type": "web_search"})
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

// StreamWithExtras streams text deltas and returns collected usage, search queries and sources.
func (c *Client) StreamWithExtras(ctx context.Context, prompt string, tools []Tool, opts Options, onDelta func(string) error) (Usage, []string, []string, error) {
	var accUsage Usage
	queries := make([]string, 0)
	sources := make([]string, 0)

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
	if len(tools) > 0 {
		tdefs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			params := t.Parameters
			if params == nil {
				params = map[string]interface{}{"type": "object"}
			}
			params["additionalProperties"] = false
			// Normalize schema for OpenAI: ensure all properties in 'properties' are either in 'required' or have default
			// Also fix array types - OpenAI doesn't support type arrays like ["string", "number"]
			if propsRaw, ok := params["properties"].(map[string]interface{}); ok {
				requiredRaw, hasRequired := params["required"].([]interface{})
				requiredSet := make(map[string]bool)
				if hasRequired {
					for _, r := range requiredRaw {
						if s, ok := r.(string); ok {
							requiredSet[s] = true
						}
					}
				}
				// Build required array with all properties that don't have defaults
				requiredList := make([]string, 0)
				for k, v := range propsRaw {
					if m, ok := v.(map[string]interface{}); ok {
						// Fix array types - OpenAI doesn't support type arrays
						if typeVal, hasType := m["type"]; hasType {
							if typeArr, ok := typeVal.([]interface{}); ok && len(typeArr) > 0 {
								// Convert array type to first type (usually string or number)
								// This is a limitation of OpenAI's strict schema validation
								m["type"] = typeArr[0]
							}
						}
						if _, has := m["type"]; !has {
							m["type"] = "string"
						}
						// Recursively fix nested items (for arrays)
						if items, hasItems := m["items"]; hasItems {
							if itemsMap, ok := items.(map[string]interface{}); ok {
								if itemType, hasItemType := itemsMap["type"]; hasItemType {
									if itemTypeArr, ok := itemType.([]interface{}); ok && len(itemTypeArr) > 0 {
										itemsMap["type"] = itemTypeArr[0]
									}
								}
								if _, hasItemType := itemsMap["type"]; !hasItemType {
									itemsMap["type"] = "string"
								}
								m["items"] = itemsMap
							}
						}
						// If property doesn't have default and is not already required, add it to required
						// OpenAI requires all properties to be in 'required' array
						if _, hasDefault := m["default"]; !hasDefault {
							if !requiredSet[k] {
								requiredList = append(requiredList, k)
							}
						}
						propsRaw[k] = m
					}
				}
				// Merge with existing required
				for k := range requiredSet {
					requiredList = append(requiredList, k)
				}
				params["required"] = requiredList
			}
			tdefs = append(tdefs, map[string]interface{}{"type": "function", "name": t.Name, "description": t.Description, "parameters": params, "strict": true})
		}
		if opts.GroundingEnabled {
			tdefs = append(tdefs, map[string]interface{}{"type": "web_search"})
		}
		req["tools"] = tdefs
		req["parallel_tool_calls"] = false
	}
	b, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(b))
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return accUsage, queries, sources, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return accUsage, queries, sources, fmt.Errorf("openai stream error %d: %s", resp.StatusCode, string(rb))
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

		// Parse usage from any event that contains it (especially response.completed)
		// CRITICAL: Parse usage BEFORE checking for response.completed to ensure we capture it
		if u, ok := m["usage"].(map[string]interface{}); ok {
			if v, ok := u["input_tokens"].(float64); ok {
				accUsage.PromptTokens = int(v)
			}
			if v, ok := u["output_tokens"].(float64); ok {
				accUsage.OutputTokens = int(v)
			}
			if v, ok := u["total_tokens"].(float64); ok {
				accUsage.TotalTokens = int(v)
			}
			if d, ok := u["output_tokens_details"].(map[string]interface{}); ok {
				if v, ok := d["reasoning_tokens"].(float64); ok {
					accUsage.ReasoningTokens = int(v)
				}
			}
			// Log when usage is found for debugging
			log.Printf("[OpenAI Stream] Found usage in event: promptTokens=%d outputTokens=%d totalTokens=%d", accUsage.PromptTokens, accUsage.OutputTokens, accUsage.TotalTokens)
		}

		// Check for usage at top level (some API versions may put usage directly in event)
		// Also check for usage in response.completed event specifically
		typ, _ := m["type"].(string)
		if typ != "" {
			lt := strings.ToLower(typ)
			if strings.Contains(lt, "response.completed") {
				// Log event type for debugging
				log.Printf("[OpenAI Stream] Received response.completed event")
				// Try to extract usage from top-level fields if not found in usage object
				if accUsage.PromptTokens == 0 && accUsage.OutputTokens == 0 {
					// Some API versions may put usage fields directly in the event
					if v, ok := m["input_tokens"].(float64); ok {
						accUsage.PromptTokens = int(v)
						log.Printf("[OpenAI Stream] Found input_tokens at top level: %d", accUsage.PromptTokens)
					}
					if v, ok := m["output_tokens"].(float64); ok {
						accUsage.OutputTokens = int(v)
						log.Printf("[OpenAI Stream] Found output_tokens at top level: %d", accUsage.OutputTokens)
					}
					if v, ok := m["total_tokens"].(float64); ok {
						accUsage.TotalTokens = int(v)
						log.Printf("[OpenAI Stream] Found total_tokens at top level: %d", accUsage.TotalTokens)
					}
				}
				// Return EOF after processing usage
				return io.EOF
			}
		}

		// Heuristics for Responses SSE: prefer output_text.delta
		// CRITICAL: Only call onDelta once per event to avoid duplicate chunks
		var textToSend string
		if typ != "" {
			lt := strings.ToLower(typ)
			// Prefer output_text.delta for delta events
			if strings.Contains(lt, "output_text.delta") {
				if d, _ := m["delta"].(string); d != "" {
					textToSend = d
				}
			}
		}
		// Fallback to other fields if output_text.delta not found
		if textToSend == "" {
			if d, _ := m["delta"].(string); d != "" {
				textToSend = d
			} else if txt, _ := m["output_text"].(string); txt != "" {
				textToSend = txt
			} else if content, ok := m["content"].([]interface{}); ok {
				var sb strings.Builder
				for _, c := range content {
					if cm, ok := c.(map[string]interface{}); ok {
						if t, _ := cm["text"].(string); t != "" {
							sb.WriteString(t)
						}
					}
				}
				if sb.Len() > 0 {
					textToSend = sb.String()
				}
			}
		}
		// Send text only once
		if textToSend != "" {
			if onDelta != nil {
				_ = onDelta(textToSend)
			}
			extractURLsFromText(textToSend, &sources)
		}
		return nil
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return accUsage, queries, sources, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				break
			}
			if data != "" {
				dataBuf.WriteString(data)
			}
		} else if line == "" { // end of event
			if err := flushEvent(); err != nil {
				if err == io.EOF {
					break
				}
				return accUsage, queries, sources, err
			}
		}
	}
	// flush any remaining
	_ = flushEvent()

	// Calculate TotalTokens if not set
	if accUsage.TotalTokens == 0 && (accUsage.PromptTokens > 0 || accUsage.OutputTokens > 0) {
		accUsage.TotalTokens = accUsage.PromptTokens + accUsage.OutputTokens
		log.Printf("[OpenAI Stream] Calculated total_tokens: %d (prompt: %d + output: %d)", accUsage.TotalTokens, accUsage.PromptTokens, accUsage.OutputTokens)
	}

	// Log final usage before returning
	log.Printf("[OpenAI Stream] Final usage before return: promptTokens=%d outputTokens=%d totalTokens=%d reasoningTokens=%d", accUsage.PromptTokens, accUsage.OutputTokens, accUsage.TotalTokens, accUsage.ReasoningTokens)

	// Log final usage for debugging
	if accUsage.PromptTokens > 0 || accUsage.OutputTokens > 0 {
		log.Printf("[OpenAI Stream] Final usage: promptTokens=%d outputTokens=%d totalTokens=%d", accUsage.PromptTokens, accUsage.OutputTokens, accUsage.TotalTokens)
	} else {
		log.Printf("[OpenAI Stream] Warning: No usage data found in stream (promptTokens=%d outputTokens=%d)", accUsage.PromptTokens, accUsage.OutputTokens)
	}

	return accUsage, queries, sources, nil
}
