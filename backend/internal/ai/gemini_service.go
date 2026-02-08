package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// GeminiService — обёртка для Google AI (Gemini) через generativelanguage.googleapis.com.
// Аутентификация по API-ключу. Поддерживает текстовую генерацию, Google Search Grounding
// и базовый tool-calling.
//
// НЕ является Vertex AI Platform клиентом — для Vertex AI (aiplatform.googleapis.com)
// с поддержкой Vertex AI Search grounding используйте VertexAIService.
type GeminiService struct {
	*BaseAIService
	apiKey     string
	httpClient *http.Client
}

// NewGeminiService создаёт экземпляр GeminiService.
func NewGeminiService(apiKey string, modelManager *AIModelManager) *GeminiService {
	// Sanitize API key to avoid invalid header values
	sanitizedKey := strings.TrimSpace(apiKey)
	sanitizedKey = strings.Trim(sanitizedKey, "\"'")
	return &GeminiService{
		BaseAIService: NewBaseAIService(modelManager),
		apiKey:        sanitizedKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Provider возвращает идентификатор провайдера
func (s *GeminiService) Provider() AIProvider {
	return AIProviderGemini
}

// ExecuteTask выполняет основную задачу по генерации текста
func (s *GeminiService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}

	log.Printf("[GeminiAPI] ExecuteTask: %s, model=%s", task.TaskType, model)
	debug := aiDebugLogEnabled()

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
	// Ensure a sufficient completion budget, especially for structured JSON.
	inputTokensEst := len(task.InputContent) / 4
	var caps *AIModelCapabilities
	if s.ModelManager != nil {
		if c, err := s.ModelManager.GetModelCapabilities(model); err == nil {
			cc := c
			caps = &cc
		}
	}
	maxOut := computeMaxOutputTokens(model, caps, task, inputTokensEst)
	requestBody["generationConfig"] = map[string]interface{}{
		"maxOutputTokens": maxOut,
	}
	// If task requests Search Grounding, enable googleSearch tool for Gemini.
	if task.AIRequirements.SearchGroundingCapability {
		log.Printf("[GeminiAPI] Grounding enabled via tools.googleSearch")
		requestBody["tools"] = []map[string]interface{}{
			{"googleSearch": map[string]interface{}{}},
		}
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	if debug {
		log.Printf("[AI DEBUG][GeminiAPI] request prepared (task=%s model=%s inputChars=%d maxOut=%d requestBytes=%d grounding=%v)",
			task.TaskType, model, len(task.InputContent), maxOut, len(bodyJSON), task.AIRequirements.SearchGroundingCapability)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-goog-api-key", s.apiKey)
	req.Header.Set("Content-Type", "application/json")

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
		log.Printf("[AI DEBUG][GeminiAPI] response received (task=%s model=%s status=%d responseBytes=%d elapsed=%s)",
			task.TaskType, model, resp.StatusCode, len(body), time.Since(startedAt))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(body))
	}

	// Grounding visibility: if grounding is requested, we should typically see groundingMetadata in candidates.
	// We don't fail if it's missing (API may choose not to ground), but we log it for diagnosis.
	if task.AIRequirements.SearchGroundingCapability {
		var root map[string]interface{}
		if err := json.Unmarshal(body, &root); err == nil {
			hasGrounding := false
			if candidates, ok := root["candidates"].([]interface{}); ok && len(candidates) > 0 {
				if c0, ok := candidates[0].(map[string]interface{}); ok {
					if _, ok := c0["groundingMetadata"]; ok {
						hasGrounding = true
					}
				}
			}
			if hasGrounding {
				log.Printf("[GeminiAPI] GroundingMetadata present in response")
			} else {
				// Some responses include promptFeedback with safety blocks; log keys for debugging.
				keys := make([]string, 0, len(root))
				for k := range root {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				log.Printf("[GeminiAPI] GroundingMetadata NOT present in response (keys=%v). Grounding may have been skipped or blocked.", keys)
			}
		}
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
		preview := string(body)
		if len(preview) > 1200 {
			preview = preview[:1200] + "..."
		}
		return nil, fmt.Errorf("empty response from Gemini API (no candidates/parts). raw=%s", preview)
	}

	content := result.Candidates[0].Content.Parts[0].Text
	if content == "" {
		preview := string(body)
		if len(preview) > 1200 {
			preview = preview[:1200] + "..."
		}
		return nil, fmt.Errorf("empty response from Gemini API (empty text). raw=%s", preview)
	}

	return &AITaskResult{
		Content:  content,
		Model:    model,
		TaskType: task.TaskType,
		Status:   AITaskStatusCompleted,
	}, nil
}

// ExecuteSearch выполняет веб-поиск с помощью встроенных возможностей Gemini
func (s *GeminiService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	if query == "" || modelName == "" {
		return nil, fmt.Errorf("query and modelName are required")
	}

	caps, err := s.ModelManager.GetModelCapabilities(modelName)
	if err != nil {
		return nil, err
	}
	if !caps.SearchGroundingCapability {
		return nil, fmt.Errorf("model %s does not support search grounding", modelName)
	}

	log.Printf("[GeminiAPI] ExecuteSearch. model=%s, q=\"%s\"", modelName, query)

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
				"googleSearch": map[string]interface{}{},
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
		return nil, fmt.Errorf("Gemini API search error %d: %s", resp.StatusCode, string(body))
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
		return nil, fmt.Errorf("empty search response from Gemini API")
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
							cURL := ""
							title := ""
							if u, ok := gMap["sourceUrl"].(string); ok {
								cURL = u
							}
							if t, ok := gMap["sourceTitle"].(string); ok {
								title = t
							}
							citations = append(citations, WebCitation{
								URL:        cURL,
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
								cURL := ""
								title := ""
								startIndex := -1
								endIndex := -1
								if u, ok := srcMap["uri"].(string); ok {
									cURL = u
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
									URL:        cURL,
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

// ExecuteEmbeddingTask генерация эмбеддингов не поддерживается через Gemini API в данной реализации
func (s *GeminiService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("Gemini API does not support embedding generation in this implementation")
}

// ExecuteImageAnalysisByLinkTask анализ изображений не поддерживается через Gemini API в данной реализации
func (s *GeminiService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("Gemini API does not support image analysis in this implementation")
}

// BeginChatWithTools начинает диалог с поддержкой tool-calling (Gemini tools)
func (s *GeminiService) BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error) {
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

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
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
		return nil, fmt.Errorf("Gemini API tool chat error %d: %s", resp.StatusCode, string(body))
	}

	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	step := &ToolChatStep{
		SessionID: fmt.Sprintf("gemini-session-%d", time.Now().UnixNano()),
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
func (s *GeminiService) ContinueWithToolResults(ctx context.Context, sessionID string, model string, toolResults []ToolExecutionResult) (*ToolChatStep, error) {
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

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
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
		return nil, fmt.Errorf("Gemini API tool continue error %d: %s", resp.StatusCode, string(body))
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
