package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const openRouterAPIBase = "https://openrouter.ai/api/v1"

// OpenRouterService implements AIService for OpenRouter (OpenAI-compatible Chat Completions).
type OpenRouterService struct {
	*BaseAIService
	apiKey     string
	httpClient *http.Client
}

// NewOpenRouterService creates a client using OPENROUTER_API_KEY (Bearer).
func NewOpenRouterService(apiKey string, modelManager *AIModelManager) *OpenRouterService {
	sanitizedKey := strings.TrimSpace(apiKey)
	sanitizedKey = strings.Trim(sanitizedKey, "\"'")
	return &OpenRouterService{
		BaseAIService: NewBaseAIService(modelManager),
		apiKey:        sanitizedKey,
		httpClient: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// Provider returns OpenRouter.
func (s *OpenRouterService) Provider() AIProvider {
	return AIProviderOpenRouter
}

var (
	_ AIService            = (*OpenRouterService)(nil)
	_ ToolCallingAIService = (*OpenRouterService)(nil)
)

// appendOpenRouterResponseFormat sets OpenRouter/OpenAI-compatible structured output when requested.
// See https://openrouter.ai/docs/guides/features/structured-outputs
func appendOpenRouterResponseFormat(body map[string]interface{}, req AIRequirements) {
	if len(req.ResponseSchema) > 0 {
		body["response_format"] = map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name":   "response",
				"strict": true,
				"schema": req.ResponseSchema,
			},
		}
		return
	}
	if strings.TrimSpace(req.ResponseMimeType) == "application/json" {
		body["response_format"] = map[string]interface{}{
			"type": "json_object",
		}
	}
}

func (s *OpenRouterService) openRouterHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if ref := strings.TrimSpace(os.Getenv("OPENROUTER_HTTP_REFERER")); ref != "" {
		req.Header.Set("Referer", ref)
	}
	if title := strings.TrimSpace(os.Getenv("OPENROUTER_APP_TITLE")); title != "" {
		req.Header.Set("X-Title", title)
	}
}

// ExecuteTask runs a chat completion against OpenRouter.
func (s *OpenRouterService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}

	log.Printf("[OpenRouter] ExecuteTask: type=%s, model=%s", task.TaskType, model)
	debug := aiDebugLogEnabled()

	inputTokensEst := len(task.InputContent) / 4
	var caps *AIModelCapabilities
	if s.ModelManager != nil {
		if c, err := s.ModelManager.GetModelCapabilities(model); err == nil {
			cc := c
			caps = &cc
		}
	}
	maxOut := computeMaxOutputTokens(model, caps, task, inputTokensEst)

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": task.InputContent},
		},
		"max_tokens": maxOut,
	}
	appendOpenRouterResponseFormat(requestBody, task.AIRequirements)

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	if debug {
		log.Printf("[AI DEBUG][OpenRouter] request prepared (task=%s model=%s inputChars=%d maxOut=%d requestBytes=%d)",
			task.TaskType, model, len(task.InputContent), maxOut, len(bodyJSON))
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openRouterAPIBase+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	s.openRouterHeaders(req)

	startedAt := time.Now()
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if debug {
		log.Printf("[AI DEBUG][OpenRouter] response received (task=%s model=%s status=%d responseBytes=%d elapsed=%s)",
			task.TaskType, model, resp.StatusCode, len(body), time.Since(startedAt))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		finish := ""
		if len(response.Choices) > 0 {
			finish = strings.TrimSpace(response.Choices[0].FinishReason)
		}
		if debug {
			log.Printf("[AI DEBUG][OpenRouter] empty chat response (task=%s model=%s status=%d responseBytes=%d finish=%q bodySha=%s bodyPreview=%s)",
				task.TaskType, model, resp.StatusCode, len(body), finish, shortHash(string(body)),
				singleLineForLog(string(body), 2500),
			)
		} else {
			log.Printf("WARNING: [OpenRouter] empty chat response (task=%s model=%s status=%d responseBytes=%d finish=%q bodySha=%s)",
				task.TaskType, model, resp.StatusCode, len(body), finish, shortHash(string(body)),
			)
		}
		return nil, fmt.Errorf("empty response from OpenRouter Chat API")
	}
	if strings.TrimSpace(response.Choices[0].FinishReason) == "length" {
		return nil, fmt.Errorf("openrouter response truncated (finish_reason=length)")
	}

	content := response.Choices[0].Message.Content

	return &AITaskResult{
		Content:  content,
		Model:    model,
		TaskType: task.TaskType,
		Status:   AITaskStatusCompleted,
	}, nil
}

// ExecuteEmbeddingTask is not supported on Nemotron routes.
func (s *OpenRouterService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("OpenRouter service: embedding not supported for model %s", modelName)
}

// ExecuteImageAnalysisByLinkTask is not wired for OpenRouter Nemotron entries.
func (s *OpenRouterService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("OpenRouter service: image analysis not supported for model %s", modelName)
}

// ExecuteSearch is not supported (no built-in web search on these models).
func (s *OpenRouterService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("OpenRouter service: built-in search not supported for model %s", modelName)
}

// BeginChatWithTools starts a tool-capable chat on OpenRouter.
func (s *OpenRouterService) BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error) {
	openAITools := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		openAITools = append(openAITools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.ParametersJSONSchema,
			},
		})
	}

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful assistant with access to tools."},
			{"role": "user", "content": task.InputContent},
		},
		"tools":       openAITools,
		"tool_choice": "auto",
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openRouterAPIBase+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	s.openRouterHeaders(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[OpenRouter] BeginChatWithTools failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("OpenRouter tool chat error %d: %s", resp.StatusCode, string(body))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &completion); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: fmt.Sprintf("openrouter-session-%d", time.Now().UnixNano()),
		Content:   "",
		ToolCalls: []ToolCallRequest{},
		IsFinal:   true,
	}

	if len(completion.Choices) > 0 {
		msg := completion.Choices[0].Message
		step.Content = msg.Content
		if len(msg.ToolCalls) > 0 {
			step.IsFinal = false
			for _, call := range msg.ToolCalls {
				if call.Function.Name == "" {
					continue
				}
				step.ToolCalls = append(step.ToolCalls, ToolCallRequest{
					CallID:        call.ID,
					ToolName:      call.Function.Name,
					ArgumentsJSON: call.Function.Arguments,
				})
			}
		}
	}

	return step, nil
}

// ContinueWithToolResults continues tool flow on OpenRouter.
func (s *OpenRouterService) ContinueWithToolResults(ctx context.Context, sessionID string, model string, toolResults []ToolExecutionResult) (*ToolChatStep, error) {
	assistantToolCalls := make([]map[string]interface{}, 0, len(toolResults))
	for _, r := range toolResults {
		args := r.RequestedArgumentsJSON
		if args == "" {
			args = "{}"
		}
		assistantToolCalls = append(assistantToolCalls, map[string]interface{}{
			"id":   r.CallID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      r.ToolName,
				"arguments": args,
			},
		})
	}

	messages := []map[string]interface{}{
		{
			"role":       "assistant",
			"content":    nil,
			"tool_calls": assistantToolCalls,
		},
	}

	for _, r := range toolResults {
		messages = append(messages, map[string]interface{}{
			"role":         "tool",
			"tool_call_id": r.CallID,
			"content":      r.ResultJSON,
		})
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openRouterAPIBase+"/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	s.openRouterHeaders(req)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[OpenRouter] ContinueWithToolResults failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("OpenRouter tool continue error %d: %s", resp.StatusCode, string(body))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &completion); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: sessionID,
		Content:   "",
		ToolCalls: []ToolCallRequest{},
		IsFinal:   true,
	}

	if len(completion.Choices) > 0 {
		msg := completion.Choices[0].Message
		step.Content = msg.Content
		if len(msg.ToolCalls) > 0 {
			step.IsFinal = false
			for _, call := range msg.ToolCalls {
				if call.Function.Name == "" {
					continue
				}
				step.ToolCalls = append(step.ToolCalls, ToolCallRequest{
					CallID:        call.ID,
					ToolName:      call.Function.Name,
					ArgumentsJSON: call.Function.Arguments,
				})
			}
		}
	}

	return step, nil
}
