package ai

import (
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

// ClaudeService реализация IAIService для работы с моделями от Anthropic (Claude)
type ClaudeService struct {
	*BaseAIService
	apiKey     string
	httpClient *http.Client
}

// NewClaudeService создает новый экземпляр ClaudeService
func NewClaudeService(apiKey string, modelManager *AIModelManager) *ClaudeService {
	// Sanitize API key to avoid invalid header values
	sanitizedKey := strings.TrimSpace(apiKey)
	sanitizedKey = strings.Trim(sanitizedKey, "\"'")
	return &ClaudeService{
		BaseAIService: NewBaseAIService(modelManager),
		apiKey:        sanitizedKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Provider возвращает идентификатор провайдера
func (s *ClaudeService) Provider() AIProvider {
	return AIProviderClaude
}

// ExecuteTask выполняет задачу (чат/генерация текста) для Claude
func (s *ClaudeService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	log.Printf("[Claude] ExecuteTask started with task type: %s and model: %s", task.TaskType, model)

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": task.InputContent},
		},
		"max_tokens":  task.AIRequirements.MaxOutputSize,
		"temperature": 0.7,
	}

	if task.AIRequirements.MaxOutputSize <= 0 {
		requestBody["max_tokens"] = 4096
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	log.Printf("[Claude] sending request to Claude API...")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("[Claude] received response. Status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Claude] API error response: %s", string(body))
		return nil, fmt.Errorf("claude API request failed with status code %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Content) == 0 || response.Content[0].Text == "" {
		return nil, fmt.Errorf("received null or empty response from Claude API")
	}

	content := response.Content[0].Text
	log.Printf("[Claude] AI response content length: %d", len(content))

	return &AITaskResult{
		Content:  content,
		Model:    model,
		TaskType: task.TaskType,
		Status:   AITaskStatusCompleted,
	}, nil
}

// ExecuteEmbeddingTask генерация эмбеддингов не поддерживается API Claude
func (s *ClaudeService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("claude does not support embedding generation")
}

// ExecuteImageAnalysisByLinkTask анализ изображений не поддерживается в данной реализации
func (s *ClaudeService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("claude does not support image analysis in this implementation")
}

// ExecuteSearch встроенный веб-поиск не поддерживается API Claude
func (s *ClaudeService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("claude does not support built-in web search")
}

// BeginChatWithTools начинает диалог с поддержкой tool-calling (Anthropic Messages API)
func (s *ClaudeService) BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error) {
	anthTools := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		anthTools = append(anthTools, map[string]interface{}{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.ParametersJSONSchema,
		})
	}

	maxTokens := task.AIRequirements.MaxOutputSize
	if maxTokens <= 0 {
		maxTokens = 512
	}

	requestBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful assistant with access to tools."},
			{"role": "user", "content": task.InputContent},
		},
		"tools":       anthTools,
		"tool_choice": "auto",
		"max_tokens":  maxTokens,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

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
		log.Printf("[Claude] BeginChatWithTools failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("claude tool chat error %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			ToolUse struct {
				ID    string                 `json:"id"`
				Name  string                 `json:"name"`
				Input map[string]interface{} `json:"input"`
			} `json:"tool_use"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: fmt.Sprintf("claude-session-%d", time.Now().UnixNano()),
		Content:   "",
		ToolCalls: []ToolCallRequest{},
		IsFinal:   true,
	}

	for _, c := range parsed.Content {
		if c.Type == "tool_use" && c.ToolUse.Name != "" {
			step.IsFinal = false
			argsJSON, _ := json.Marshal(c.ToolUse.Input)
			step.ToolCalls = append(step.ToolCalls, ToolCallRequest{
				CallID:        c.ToolUse.ID,
				ToolName:      c.ToolUse.Name,
				ArgumentsJSON: string(argsJSON),
			})
		} else if c.Type == "text" {
			step.Content += c.Text
		}
	}

	return step, nil
}

// ContinueWithToolResults продолжает диалог с результатами выполнения инструментов
func (s *ClaudeService) ContinueWithToolResults(ctx context.Context, sessionID string, model string, toolResults []ToolExecutionResult) (*ToolChatStep, error) {
	toolMsgs := make([]map[string]interface{}, 0, len(toolResults))
	for _, r := range toolResults {
		toolMsgs = append(toolMsgs, map[string]interface{}{
			"role":        "tool",
			"content":     r.ResultJSON,
			"tool_use_id": r.CallID,
		})
	}

	requestBody := map[string]interface{}{
		"model":      model,
		"messages":   toolMsgs,
		"max_tokens": 512,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", s.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

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
		log.Printf("[Claude] ContinueWithToolResults failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("claude tool continue error %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			ToolUse struct {
				ID    string                 `json:"id"`
				Name  string                 `json:"name"`
				Input map[string]interface{} `json:"input"`
			} `json:"tool_use"`
		} `json:"content"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: sessionID,
		Content:   "",
		ToolCalls: []ToolCallRequest{},
		IsFinal:   true,
	}

	for _, c := range parsed.Content {
		if c.Type == "tool_use" && c.ToolUse.Name != "" {
			step.IsFinal = false
			argsJSON, _ := json.Marshal(c.ToolUse.Input)
			step.ToolCalls = append(step.ToolCalls, ToolCallRequest{
				CallID:        c.ToolUse.ID,
				ToolName:      c.ToolUse.Name,
				ArgumentsJSON: string(argsJSON),
			})
		} else if c.Type == "text" {
			step.Content += c.Text
		}
	}

	return step, nil
}
