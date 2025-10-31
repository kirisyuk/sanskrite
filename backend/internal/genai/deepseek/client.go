package deepseek

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

// StreamEvent represents incremental streaming callbacks
type StreamEvent struct {
    Type      string     `json:"type"` // start|chunk|tool_call|done|error
    Text      string     `json:"text,omitempty"`
    ToolCalls []ToolCall `json:"toolCalls,omitempty"`
    Usage     Usage      `json:"usage,omitempty"`
    Model     string     `json:"model,omitempty"`
    Error     string     `json:"error,omitempty"`
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
    if strings.TrimSpace(opts.ModelOverride) != "" { return opts.ModelOverride }
    return "deepseek-chat"
}

// Generate against DeepSeek OpenAI-compatible Chat Completions
func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
    model := c.modelFrom(opts)
    msgs := make([]map[string]interface{}, 0, 2)
    if s := strings.TrimSpace(opts.SystemInstruction); s != "" {
        msgs = append(msgs, map[string]interface{}{"role": "system", "content": s})
    }
    msgs = append(msgs, map[string]interface{}{"role": "user", "content": prompt})

    p := map[string]interface{}{
        "model":    model,
        "messages": msgs,
    }
    if opts.MaxOutputTokens > 0 { p["max_tokens"] = opts.MaxOutputTokens }
    if opts.Temperature > 0 { p["temperature"] = opts.Temperature }
    // Tools
    if len(tools) > 0 {
        tdefs := make([]map[string]interface{}, 0, len(tools))
        for _, t := range tools {
            params := t.Parameters
            if params == nil { params = map[string]interface{}{"type": "object"} }
            params["additionalProperties"] = false
            tdefs = append(tdefs, map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                    "name":        t.Name,
                    "description": t.Description,
                    "parameters":  params,
                },
            })
        }
        p["tools"] = tdefs
        p["tool_choice"] = "auto"
    }

    b, _ := json.Marshal(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/chat/completions", bytes.NewReader(b))
    req.Header.Set("Authorization", "Bearer "+c.apiKey)
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.httpClient.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    rb, _ := io.ReadAll(resp.Body)
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, fmt.Errorf("deepseek error %d: %s", resp.StatusCode, string(rb))
    }
    var root map[string]interface{}
    if err := json.Unmarshal(rb, &root); err != nil { return nil, err }
    out := &GenerateResponse{Model: model, RawJSON: string(rb)}
    if usage, ok := root["usage"].(map[string]interface{}); ok {
        if v, ok := usage["prompt_tokens"].(float64); ok { out.Usage.PromptTokens = int(v) }
        if v, ok := usage["completion_tokens"].(float64); ok { out.Usage.OutputTokens = int(v) }
        if v, ok := usage["total_tokens"].(float64); ok { out.Usage.TotalTokens = int(v) }
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
                                if s, ok := f["arguments"].(string); ok { args = s; extractURLsFromText(s, &out.Sources) } else if obj, ok := f["arguments"].(map[string]interface{}); ok {
                                    bb, _ := json.Marshal(obj); args = string(bb); extractURLsFromMap(obj, &out.Sources)
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

// Stream uses OpenAI-compatible chat/completions SSE with stream=true
func (c *Client) Stream(ctx context.Context, prompt string, tools []Tool, opts Options, onEvent func(StreamEvent) error) error {
    model := c.modelFrom(opts)
    msgs := make([]map[string]interface{}, 0, 2)
    if s := strings.TrimSpace(opts.SystemInstruction); s != "" {
        msgs = append(msgs, map[string]interface{}{"role": "system", "content": s})
    }
    msgs = append(msgs, map[string]interface{}{"role": "user", "content": prompt})

    p := map[string]interface{}{
        "model":    model,
        "messages": msgs,
        "stream":   true,
    }
    if opts.MaxOutputTokens > 0 { p["max_tokens"] = opts.MaxOutputTokens }
    if opts.Temperature > 0 { p["temperature"] = opts.Temperature }
    if len(tools) > 0 {
        tdefs := make([]map[string]interface{}, 0, len(tools))
        for _, t := range tools {
            params := t.Parameters
            if params == nil { params = map[string]interface{}{"type": "object"} }
            params["additionalProperties"] = false
            tdefs = append(tdefs, map[string]interface{}{
                "type": "function",
                "function": map[string]interface{}{
                    "name":        t.Name,
                    "description": t.Description,
                    "parameters":  params,
                },
            })
        }
        p["tools"] = tdefs
        p["tool_choice"] = "auto"
    }
    b, _ := json.Marshal(p)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/chat/completions", bytes.NewReader(b))
    req.Header.Set("Authorization", "Bearer "+c.apiKey)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")
    resp, err := c.httpClient.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        rb, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("deepseek stream error %d: %s", resp.StatusCode, string(rb))
    }
    if onEvent != nil { _ = onEvent(StreamEvent{Type: "start", Model: model}) }
    reader := bufio.NewReader(resp.Body)
    var dataBuf strings.Builder
    var acc Usage
    flush := func(payload string) error {
        payload = strings.TrimSpace(payload)
        if payload == "" || payload == "[DONE]" { return nil }
        var root map[string]interface{}
        if err := json.Unmarshal([]byte(payload), &root); err != nil { return nil }
        if chs, ok := root["choices"].([]interface{}); ok && len(chs) > 0 {
            if ch, ok := chs[0].(map[string]interface{}); ok {
                if d, ok := ch["delta"].(map[string]interface{}); ok {
                    if txt, _ := d["content"].(string); txt != "" {
                        if onEvent != nil { _ = onEvent(StreamEvent{Type: "chunk", Text: txt}) }
                    }
                    // Tool call deltas
                    if tcs, ok := d["tool_calls"].([]interface{}); ok {
                        for _, tc := range tcs {
                            if m, ok := tc.(map[string]interface{}); ok {
                                if f, ok := m["function"].(map[string]interface{}); ok {
                                    name, _ := f["name"].(string)
                                    args := ""
                                    if s, ok := f["arguments"].(string); ok { args = s }
                                    if onEvent != nil { _ = onEvent(StreamEvent{Type: "tool_call", ToolCalls: []ToolCall{{Name: name, Arguments: args}}}) }
                                }
                            }
                        }
                    }
                }
            }
        }
        if u, ok := root["usage"].(map[string]interface{}); ok {
            if v, ok := u["prompt_tokens"].(float64); ok { acc.PromptTokens = int(v) }
            if v, ok := u["completion_tokens"].(float64); ok { acc.OutputTokens = int(v) }
            if v, ok := u["total_tokens"].(float64); ok { acc.TotalTokens = int(v) }
        }
        return nil
    }
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            if err == io.EOF { _ = flush(dataBuf.String()); break }
            return err
        }
        l := strings.TrimRight(line, "\r\n")
        if strings.HasPrefix(l, "data:") {
            payload := strings.TrimSpace(strings.TrimPrefix(l, "data:"))
            if payload == "[DONE]" {
                break
            }
            _ = flush(payload)
            continue
        }
    }
    if onEvent != nil { _ = onEvent(StreamEvent{Type: "done", Usage: acc, Model: model}) }
    return nil
}

// Helpers
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
                if sm, ok := it.(map[string]interface{}); ok { extractURLsFromMap(sm, dst) } else if ss, ok := it.(string); ok { extractURLsFromText(ss, dst) }
            }
        }
    }
}


