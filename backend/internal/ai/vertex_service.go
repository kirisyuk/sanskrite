package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// VertexAIService сервис-обёртка для Google Vertex AI (Gemini)
// Поддерживает текстовую генерацию и встроенный веб-поиск (Grounding with Google Search)
type VertexAIService struct {
	*BaseAIService
	apiKey     string
	httpClient *http.Client
}

// NewVertexAIService создает новый экземпляр VertexAIService
func NewVertexAIService(apiKey string, modelManager *AIModelManager) *VertexAIService {
	return &VertexAIService{
		BaseAIService: NewBaseAIService(modelManager),
		apiKey:        apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Provider возвращает идентификатор провайдера
func (s *VertexAIService) Provider() AIProvider {
	return AIProviderGemini
}

// ExecuteTask выполняет основную задачу по генерации текста
func (s *VertexAIService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}

	log.Printf("[VertexAI] ExecuteTask: %s, model=%s", task.TaskType, model)

	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": task.InputContent},
				},
			},
		},
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", s.apiKey)
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
		return nil, fmt.Errorf("Vertex AI API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from Vertex AI")
	}

	content := result.Candidates[0].Content.Parts[0].Text
	if content == "" {
		return nil, fmt.Errorf("empty response from Vertex AI")
	}

	return &AITaskResult{
		Content:  content,
		Model:    model,
		TaskType: task.TaskType,
		Status:   AITaskStatusCompleted,
	}, nil
}

// ExecuteSearch выполняет веб-поиск с помощью встроенных возможностей Gemini
func (s *VertexAIService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	if query == "" || modelName == "" {
		return nil, fmt.Errorf("query and modelName are required")
	}

	caps, err := s.ModelManager.GetModelCapabilities(modelName)
	if err != nil {
		return nil, err
	}
	if !caps.BuiltInSearchCapability {
		return nil, fmt.Errorf("model %s does not support built-in search", modelName)
	}

	log.Printf("[VertexAI] ExecuteSearch. model=%s, q=\"%s\"", modelName, query)

	requestBody := map[string]interface{}{
		"model": fmt.Sprintf("models/%s", modelName),
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": query},
				},
			},
		},
		"tools": []map[string]interface{}{
			{
				"googleSearchRetrieval": map[string]interface{}{},
			},
		},
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", modelName)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", s.apiKey)
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
		return nil, fmt.Errorf("Vertex AI Search error %d: %s", resp.StatusCode, string(body))
	}

	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Extract content
	content := ""
	if candidates, ok := root["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if candidateContent, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := candidateContent["parts"].([]interface{}); ok && len(parts) > 0 {
					if part, ok := parts[0].(map[string]interface{}); ok {
						if text, ok := part["text"].(string); ok {
							content = text
						}
					}
				}
			}
		}
	}

	if content == "" {
		return nil, fmt.Errorf("empty Search response from Vertex AI")
	}

	// Extract citations
	var citations []WebCitation
	if candidates, ok := root["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			// New grounding metadata
			if groundingMeta, ok := candidate["groundingMetadata"].(map[string]interface{}); ok {
				if attributions, ok := groundingMeta["groundingAttributions"].([]interface{}); ok {
					for _, g := range attributions {
						if gMap, ok := g.(map[string]interface{}); ok {
							url := ""
							title := ""
							if u, ok := gMap["sourceUrl"].(string); ok {
								url = u
							}
							if t, ok := gMap["sourceTitle"].(string); ok {
								title = t
							}
							citations = append(citations, WebCitation{
								URL:        url,
								Title:      title,
								StartIndex: -1,
								EndIndex:   -1,
							})
						}
					}
				}
			}

			// Fallback old metadata
			if len(citations) == 0 {
				if citationMeta, ok := candidate["citationMetadata"].(map[string]interface{}); ok {
					if citationSources, ok := citationMeta["citationSources"].([]interface{}); ok {
						for _, src := range citationSources {
							if srcMap, ok := src.(map[string]interface{}); ok {
								url := ""
								title := ""
								startIndex := -1
								endIndex := -1
								if u, ok := srcMap["uri"].(string); ok {
									url = u
								}
								if l, ok := srcMap["license"].(string); ok {
									title = l
								}
								if si, ok := srcMap["startIndex"].(float64); ok {
									startIndex = int(si)
								}
								if ei, ok := srcMap["endIndex"].(float64); ok {
									endIndex = int(ei)
								}
								citations = append(citations, WebCitation{
									URL:        url,
									Title:      title,
									StartIndex: startIndex,
									EndIndex:   endIndex,
								})
							}
						}
					}
				}
			}
		}
	}

	return &AITaskResult{
		Content:   content,
		Model:     modelName,
		TaskType:  TaskTypeSearch,
		Status:    AITaskStatusCompleted,
		Citations: citations,
	}, nil
}

// ExecuteEmbeddingTask генерация эмбеддингов не поддерживается в данной реализации
func (s *VertexAIService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("Vertex AI does not support embedding generation in this implementation")
}

// ExecuteImageAnalysisByLinkTask анализ изображений не поддерживается в данной реализации
func (s *VertexAIService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("Vertex AI does not support image analysis in this implementation")
}

// BeginChatWithTools начинает диалог с поддержкой tool-calling (Gemini tools)
func (s *VertexAIService) BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error) {
	functionDecls := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		functionDecls = append(functionDecls, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.ParametersJSONSchema,
		})
	}

	gTools := []map[string]interface{}{
		{
			"functionDeclarations": functionDecls,
		},
	}

	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": "You are a helpful assistant with access to tools."},
				},
			},
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": task.InputContent},
				},
			},
		},
		"tools": gTools,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", s.apiKey)
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
		return nil, fmt.Errorf("Vertex AI tool chat error %d: %s", resp.StatusCode, string(body))
	}

	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: fmt.Sprintf("vertex-session-%d", time.Now().UnixNano()),
		Content:   "",
		ToolCalls: []ToolCallRequest{},
		IsFinal:   true,
	}

	if candidates, ok := root["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if candidateContent, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := candidateContent["parts"].([]interface{}); ok {
					for _, p := range parts {
						if pMap, ok := p.(map[string]interface{}); ok {
							// Check for function call
							if fc, ok := pMap["functionCall"].(map[string]interface{}); ok {
								step.IsFinal = false
								name := ""
								argsJSON := "{}"
								if n, ok := fc["name"].(string); ok {
									name = n
								}
								if args, ok := fc["args"]; ok {
									argsBytes, _ := json.Marshal(args)
									argsJSON = string(argsBytes)
								}
								step.ToolCalls = append(step.ToolCalls, ToolCallRequest{
									CallID:        fmt.Sprintf("call-%d", len(step.ToolCalls)+1),
									ToolName:      name,
									ArgumentsJSON: argsJSON,
								})
							}
							// Check for text
							if text, ok := pMap["text"].(string); ok && text != "" {
								step.Content += text
							}
						}
					}
				}
			}
		}
	}

	return step, nil
}

// ContinueWithToolResults продолжает диалог с результатами выполнения инструментов
func (s *VertexAIService) ContinueWithToolResults(ctx context.Context, sessionID string, model string, toolResults []ToolExecutionResult) (*ToolChatStep, error) {
	toolMsgs := make([]map[string]interface{}, 0, len(toolResults))
	for _, r := range toolResults {
		toolMsgs = append(toolMsgs, map[string]interface{}{
			"role": "tool",
			"parts": []map[string]interface{}{
				{"text": r.ResultJSON},
			},
		})
	}

	requestBody := map[string]interface{}{
		"contents": toolMsgs,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", s.apiKey)
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
		return nil, fmt.Errorf("Vertex AI tool continue error %d: %s", resp.StatusCode, string(body))
	}

	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: sessionID,
		Content:   "",
		ToolCalls: []ToolCallRequest{},
		IsFinal:   true,
	}

	if candidates, ok := root["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if candidateContent, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := candidateContent["parts"].([]interface{}); ok {
					for _, p := range parts {
						if pMap, ok := p.(map[string]interface{}); ok {
							// Check for function call
							if fc, ok := pMap["functionCall"].(map[string]interface{}); ok {
								step.IsFinal = false
								name := ""
								argsJSON := "{}"
								if n, ok := fc["name"].(string); ok {
									name = n
								}
								if args, ok := fc["args"]; ok {
									argsBytes, _ := json.Marshal(args)
									argsJSON = string(argsBytes)
								}
								step.ToolCalls = append(step.ToolCalls, ToolCallRequest{
									CallID:        fmt.Sprintf("call-%d", len(step.ToolCalls)+1),
									ToolName:      name,
									ArgumentsJSON: argsJSON,
								})
							}
							// Check for text
							if text, ok := pMap["text"].(string); ok && text != "" {
								step.Content += text
							}
						}
					}
				}
			}
		}
	}

	return step, nil
}

