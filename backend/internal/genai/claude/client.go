package claude

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
    if strings.TrimSpace(opts.ModelOverride) != "" {
        return opts.ModelOverride
    }
    return "claude-3-5-sonnet-20240620"
}

// Generate uses Anthropic Messages API with optional tool use
func (c *Client) Generate(ctx context.Context, prompt string, tools []Tool, opts Options) (*GenerateResponse, error) {
    model := c.modelFrom(opts)
    // Build request with top-level system and content blocks
    req := map[string]interface{}{
        "model": model,
        "messages": []map[string]interface{}{
            {
                "role": "user",
                "content": []map[string]interface{}{
                    {"type": "text", "text": prompt},
                },
            },
        },
    }
    if s := strings.TrimSpace(opts.SystemInstruction); s != "" {
        req["system"] = s
    }
    if opts.MaxOutputTokens > 0 {
        req["max_tokens"] = opts.MaxOutputTokens
    } else {
        req["max_tokens"] = 1024
    }
    if opts.Temperature > 0 {
        req["temperature"] = opts.Temperature
    }
    // Tools mapping (Anthropic: tools with input_schema)
    if len(tools) > 0 {
        tdefs := make([]map[string]interface{}, 0, len(tools))
        for _, t := range tools {
            schema := t.Parameters
            if schema == nil {
                schema = map[string]interface{}{"type": "object", "additionalProperties": false}
            } else {
                schema["additionalProperties"] = false
            }
            tdefs = append(tdefs, map[string]interface{}{
                "name":         t.Name,
                "description":  t.Description,
                "input_schema": schema,
            })
        }
        req["tools"] = tdefs
        // If a single required tool is provided (e.g., return_structured), force its use
        if len(tools) == 1 && strings.TrimSpace(strings.ToLower(tools[0].Name)) == "return_structured" {
            req["tool_choice"] = map[string]string{"type": "tool", "name": tools[0].Name}
        } else {
            req["tool_choice"] = map[string]string{"type": "auto"}
        }
    }

    body, _ := json.Marshal(req)
    httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
    httpReq.Header.Set("x-api-key", c.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")
    httpReq.Header.Set("content-type", "application/json")
    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    rb, _ := io.ReadAll(resp.Body)
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(rb))
    }
    var root map[string]interface{}
    if err := json.Unmarshal(rb, &root); err != nil {
        return nil, err
    }
    out := &GenerateResponse{Model: model, RawJSON: string(rb)}
    // Usage
    if u, ok := root["usage"].(map[string]interface{}); ok {
        if v, ok := u["input_tokens"].(float64); ok { out.Usage.PromptTokens = int(v) }
        if v, ok := u["output_tokens"].(float64); ok { out.Usage.OutputTokens = int(v) }
        out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.OutputTokens
    }
    // Content and tool calls
    if arr, ok := root["content"].([]interface{}); ok {
        for _, it := range arr {
            if m, ok := it.(map[string]interface{}); ok {
                if m["type"] == "text" {
                    if t, _ := m["text"].(string); t != "" {
                        out.Content += t
                        extractURLsFromText(t, &out.Sources)
                    }
                } else if m["type"] == "tool_use" {
                    name, _ := m["name"].(string)
                    id, _ := m["id"].(string)
                    var args string
                    if obj, ok := m["input"].(map[string]interface{}); ok {
                        if b, err := json.Marshal(obj); err == nil { args = string(b); extractURLsFromMap(obj, &out.Sources) }
                    }
                    out.ToolCalls = append(out.ToolCalls, ToolCall{Name: name, Arguments: args, CallID: id})
                }
            }
        }
    }
    out.IsToolStep = len(out.ToolCalls) > 0 && out.Content == ""
    return out, nil
}

// Stream uses Anthropic Messages SSE to stream text/tool events
func (c *Client) Stream(ctx context.Context, prompt string, tools []Tool, opts Options, onEvent func(StreamEvent) error) error {
    model := c.modelFrom(opts)
    req := map[string]interface{}{
        "model": model,
        "messages": []map[string]interface{}{
            {
                "role": "user",
                "content": []map[string]interface{}{{"type": "text", "text": prompt}},
            },
        },
        "stream": true,
    }
    if s := strings.TrimSpace(opts.SystemInstruction); s != "" {
        req["system"] = s
    }
    if opts.MaxOutputTokens > 0 { req["max_tokens"] = opts.MaxOutputTokens } else { req["max_tokens"] = 1024 }
    if opts.Temperature > 0 { req["temperature"] = opts.Temperature }
    if len(tools) > 0 {
        tdefs := make([]map[string]interface{}, 0, len(tools))
        for _, t := range tools {
            schema := t.Parameters
            if schema == nil { schema = map[string]interface{}{"type": "object", "additionalProperties": false} } else { schema["additionalProperties"] = false }
            tdefs = append(tdefs, map[string]interface{}{"name": t.Name, "description": t.Description, "input_schema": schema})
        }
        req["tools"] = tdefs
        // Prefer auto; if single 'return_structured' then force
        if len(tools) == 1 && strings.EqualFold(strings.TrimSpace(tools[0].Name), "return_structured") {
            req["tool_choice"] = map[string]string{"type": "tool", "name": tools[0].Name}
        } else {
            req["tool_choice"] = map[string]string{"type": "auto"}
        }
    }
    b, _ := json.Marshal(req)
    httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(b))
    httpReq.Header.Set("x-api-key", c.apiKey)
    httpReq.Header.Set("anthropic-version", "2023-06-01")
    httpReq.Header.Set("content-type", "application/json")
    httpReq.Header.Set("accept", "text/event-stream")
    resp, err := c.httpClient.Do(httpReq)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        rb, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("anthropic stream error %d: %s", resp.StatusCode, string(rb))
    }
    if onEvent != nil { _ = onEvent(StreamEvent{Type: "start", Model: model}) }
    reader := bufio.NewReader(resp.Body)
    var (
        curEvent string
        dataBuf  strings.Builder
        accUsage Usage
    )
    flush := func() error {
        payload := strings.TrimSpace(dataBuf.String())
        dataBuf.Reset()
        if payload == "" { return nil }
        // Anthropic sends JSON per event
        var m map[string]interface{}
        if err := json.Unmarshal([]byte(payload), &m); err != nil { return nil }
        switch curEvent {
        case "content_block_delta":
            // {"delta":{"type":"text_delta","text":"..."}}
            if d, ok := m["delta"].(map[string]interface{}); ok {
                if txt, _ := d["text"].(string); txt != "" {
                    if onEvent != nil { _ = onEvent(StreamEvent{Type: "chunk", Text: txt}) }
                }
            }
        case "content_block_start":
            // tool_use start
            if cb, ok := m["content_block"].(map[string]interface{}); ok {
                if t, _ := cb["type"].(string); t == "tool_use" {
                    name, _ := cb["name"].(string)
                    var args string
                    if in, ok := cb["input"].(map[string]interface{}); ok {
                        if bb, err := json.Marshal(in); err == nil { args = string(bb) }
                    }
                    if onEvent != nil { _ = onEvent(StreamEvent{Type: "tool_call", ToolCalls: []ToolCall{{Name: name, Arguments: args}}}) }
                }
            }
        case "message_delta":
            if u, ok := m["usage"].(map[string]interface{}); ok {
                if v, ok := u["output_tokens"].(float64); ok { accUsage.OutputTokens = int(v) }
            }
        case "message_stop":
            if onEvent != nil { _ = onEvent(StreamEvent{Type: "done", Usage: accUsage, Model: model}) }
            return io.EOF
        case "error":
            if onEvent != nil { _ = onEvent(StreamEvent{Type: "error", Error: payload}) }
        }
        return nil
    }
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            if err == io.EOF {
                _ = flush()
                break
            }
            return err
        }
        l := strings.TrimRight(line, "\r\n")
        if l == "" {
            if err := flush(); err != nil {
                if err == io.EOF { return nil }
                return err
            }
            curEvent = ""
            continue
        }
        if strings.HasPrefix(l, "event:") {
            curEvent = strings.TrimSpace(strings.TrimPrefix(l, "event:"))
            continue
        }
        if strings.HasPrefix(l, "data:") {
            if dataBuf.Len() > 0 { dataBuf.WriteByte('\n') }
            dataBuf.WriteString(strings.TrimSpace(strings.TrimPrefix(l, "data:")))
            continue
        }
    }
    return nil
}

// Helpers for extracting URLs into Sources
func appendUnique(dst *[]string, v string) {
    for _, x := range *dst { if x == v { return } }
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


