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

// OpenAIService реализация IAIService для работы с моделями от OpenAI
// Поддерживает чат, эмбеддинги, анализ изображений и встроенный поиск
type OpenAIService struct {
	*BaseAIService
	apiKey     string
	httpClient *http.Client
}

// NewOpenAIService создает новый экземпляр OpenAIService
func NewOpenAIService(apiKey string, modelManager *AIModelManager) *OpenAIService {
	// Sanitize API key to avoid invalid header values (e.g., trailing newlines/quotes)
	sanitizedKey := strings.TrimSpace(apiKey)
	sanitizedKey = strings.Trim(sanitizedKey, "\"'")
	return &OpenAIService{
		BaseAIService: NewBaseAIService(modelManager),
		apiKey:        sanitizedKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Provider возвращает идентификатор провайдера
func (s *OpenAIService) Provider() AIProvider {
	return AIProviderOpenAI
}

// ExecuteTask выполняет текстовую задачу с помощью Chat Completions API
func (s *OpenAIService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}

	log.Printf("[OpenAI] ExecuteTask: type=%s, model=%s", task.TaskType, model)

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
		// Ensure we don't get truncated completions (especially for large structured JSON outputs).
		// NOTE: Newer OpenAI models (e.g., gpt-5-*) reject max_tokens and require max_completion_tokens.
		"max_completion_tokens": maxOut,
	}
	// Back-compat: include max_tokens only for models that still accept it.
	// Heuristic: gpt-5-* and o* reject max_tokens.
	if !strings.HasPrefix(model, "gpt-5") && !strings.HasPrefix(model, "o") {
		requestBody["max_tokens"] = maxOut
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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
		return nil, fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, string(body))
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

	if len(response.Choices) == 0 || response.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("empty response from OpenAI Chat API")
	}
	// If the model hit the completion limit, treat as error so executor can fallback.
	if strings.TrimSpace(response.Choices[0].FinishReason) == "length" {
		return nil, fmt.Errorf("openai response truncated (finish_reason=length)")
	}

	content := response.Choices[0].Message.Content

	return &AITaskResult{
		Content:  content,
		Model:    model,
		TaskType: task.TaskType,
		Status:   AITaskStatusCompleted,
	}, nil
}

// ExecuteEmbeddingTask генерирует векторное представление (эмбеддинг) для текста
func (s *OpenAIService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	log.Printf("[OpenAI] ExecuteEmbeddingTask called. model=%s, length=%d", modelName, len(inputText))

	caps, err := s.ModelManager.GetModelCapabilities(modelName)
	if err != nil {
		return nil, err
	}
	if !caps.EmbeddingGenerationCapability {
		return nil, fmt.Errorf("model %s does not support embedding generation", modelName)
	}

	requestBody := map[string]interface{}{
		"model": modelName,
		"input": inputText,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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
		return nil, fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response from OpenAI Embeddings API")
	}

	embeddingVector := response.Data[0].Embedding

	inputT := len(inputText) / 4
	outputT := 0
	cost, _ := s.ModelManager.CalculateProcessingCost(modelName, inputT, outputT)

	return &AITaskResult{
		Model:          modelName,
		TaskType:       TaskTypeEmbeddingCalculation,
		Status:         AITaskStatusCompleted,
		InputTokens:    inputT,
		OutputTokens:   outputT,
		ProcessingCost: cost,
		Embedding:      embeddingVector,
	}, nil
}

// ExecuteImageAnalysisByLinkTask выполняет анализ изображения по ссылке (Vision)
func (s *OpenAIService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	if inputText == "" || imageLink == "" || modelName == "" {
		return nil, fmt.Errorf("inputText, imageLink, and modelName are required")
	}

	caps, err := s.ModelManager.GetModelCapabilities(modelName)
	if err != nil {
		return nil, err
	}
	if !caps.ImageAnalysisCapability {
		return nil, fmt.Errorf("model %s does not support image analysis", modelName)
	}

	log.Printf("[OpenAI] ExecuteImageAnalysisByLinkTask. model=%s, url=%s", modelName, imageLink)

	requestBody := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": inputText},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url":    imageLink,
							"detail": "auto",
						},
					},
				},
			},
		},
		"max_tokens":  1000,
		"temperature": 0.7,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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
		return nil, fmt.Errorf("OpenAI Vision API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Choices) == 0 || response.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("empty content in Vision response")
	}

	content := response.Choices[0].Message.Content

	return &AITaskResult{
		Content:  content,
		Model:    modelName,
		TaskType: TaskTypeImageAnalysis,
		Status:   AITaskStatusCompleted,
	}, nil
}

// ExecuteSearch выполняет веб-поиск с помощью встроенных возможностей модели
func (s *OpenAIService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	if query == "" || modelName == "" {
		return nil, fmt.Errorf("query and modelName are required")
	}

	caps, err := s.ModelManager.GetModelCapabilities(modelName)
	if err != nil {
		return nil, err
	}
	if !caps.BuiltInSearchCapability {
		return nil, fmt.Errorf("model %s does not support built-in web search", modelName)
	}

	log.Printf("[OpenAI] ExecuteSearch. model=%s, q=\"%s\"", modelName, query)

	requestBody := map[string]interface{}{
		"model":              modelName,
		"web_search_options": map[string]interface{}{},
		"messages": []map[string]string{
			{"role": "user", "content": query},
		},
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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

	log.Printf("[OpenAI] Search HTTP %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI Search API error %d: %s", resp.StatusCode, string(body))
	}

	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Extract content
	content := ""
	if choices, ok := root["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if contentToken, ok := message["content"]; ok {
					// content может быть строкой или массивом
					switch v := contentToken.(type) {
					case string:
						content = v
					case []interface{}:
						var parts []string
						for _, p := range v {
							if pMap, ok := p.(map[string]interface{}); ok {
								if text, ok := pMap["text"].(string); ok && text != "" {
									parts = append(parts, text)
								}
							}
						}
						content = strings.Join(parts, "\n")
					}
				}
			}
		}
	}

	if content == "" {
		return nil, fmt.Errorf("empty content in Search response")
	}

	// Extract citations
	var citations []WebCitation
	if choices, ok := root["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				// annotations.url_citation
				if annotations, ok := message["annotations"].([]interface{}); ok {
					for _, ann := range annotations {
						if annMap, ok := ann.(map[string]interface{}); ok {
							if annType, ok := annMap["type"].(string); ok && annType == "url_citation" {
								if urlCitation, ok := annMap["url_citation"].(map[string]interface{}); ok {
									url := ""
									title := ""
									startIndex := -1
									endIndex := -1
									if u, ok := urlCitation["url"].(string); ok {
										url = u
									}
									if t, ok := urlCitation["title"].(string); ok {
										title = t
									}
									if si, ok := urlCitation["start_index"].(float64); ok {
										startIndex = int(si)
									}
									if ei, ok := urlCitation["end_index"].(float64); ok {
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

			// fallback: citations/references at choice level
			if cits, ok := choice["citations"].([]interface{}); ok {
				for _, c := range cits {
					if cMap, ok := c.(map[string]interface{}); ok {
						url := ""
						title := ""
						if u, ok := cMap["url"].(string); ok {
							url = u
						} else if u, ok := cMap["href"].(string); ok {
							url = u
						}
						if t, ok := cMap["title"].(string); ok {
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

			if refs, ok := choice["references"].([]interface{}); ok {
				for _, r := range refs {
					if rMap, ok := r.(map[string]interface{}); ok {
						url := ""
						title := ""
						if u, ok := rMap["url"].(string); ok {
							url = u
						} else if u, ok := rMap["href"].(string); ok {
							url = u
						}
						if t, ok := rMap["title"].(string); ok {
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
	}

	return &AITaskResult{
		Content:   content,
		Model:     modelName,
		TaskType:  TaskTypeSearch,
		Status:    AITaskStatusCompleted,
		Citations: citations,
	}, nil
}

// BeginChatWithTools начинает диалог с поддержкой tool-calling
func (s *OpenAIService) BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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
		log.Printf("[OpenAI] BeginChatWithTools failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("OpenAI tool chat error %d: %s", resp.StatusCode, string(body))
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
		SessionID: fmt.Sprintf("openai-session-%d", time.Now().UnixNano()),
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

// ContinueWithToolResults продолжает диалог с результатами выполнения инструментов
func (s *OpenAIService) ContinueWithToolResults(ctx context.Context, sessionID string, model string, toolResults []ToolExecutionResult) (*ToolChatStep, error) {
	// Reconstruct prior assistant tool_calls and append tool results
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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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
		log.Printf("[OpenAI] ContinueWithToolResults failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("OpenAI tool continue error %d: %s", resp.StatusCode, string(body))
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
