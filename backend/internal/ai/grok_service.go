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

// GrokService implements AIService for xAI Grok models (OpenAI-compatible style)
type GrokService struct {
	*BaseAIService
	apiKey     string
	httpClient *http.Client
}

func NewGrokService(apiKey string, modelManager *AIModelManager) *GrokService {
	// Sanitize API key to avoid invalid header values
	sanitizedKey := strings.TrimSpace(apiKey)
	sanitizedKey = strings.Trim(sanitizedKey, "\"'")
	return &GrokService{
		BaseAIService: NewBaseAIService(modelManager),
		apiKey:        sanitizedKey,
		httpClient:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (s *GrokService) Provider() AIProvider { return AIProviderGrok }

func (s *GrokService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}
	log.Printf("[Grok] ExecuteTask: %s, model=%s", task.TaskType, model)

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": task.InputContent}},
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Grok uses OpenAI-compatible endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.x.ai/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("grok API error %d: %s", resp.StatusCode, string(b))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("empty response from Grok")
	}

	return &AITaskResult{Content: parsed.Choices[0].Message.Content, Model: model, TaskType: task.TaskType, Status: AITaskStatusCompleted}, nil
}

func (s *GrokService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("grok embeddings not supported")
}

func (s *GrokService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("grok image analysis not supported")
}

func (s *GrokService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	if query == "" || modelName == "" {
		return nil, fmt.Errorf("query and modelName are required")
	}

	// Many Grok deployments expose OpenAI-compatible chat completions with live search
	reqBody := map[string]interface{}{
		"model": modelName,
		// xAI Grok supports live search; request it explicitly if supported
		"web_search_options": map[string]interface{}{},
		"messages":           []map[string]string{{"role": "user", "content": query}},
	}
	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.x.ai/v1/chat/completions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("grok search error %d: %s", resp.StatusCode, string(b))
	}

	var root map[string]interface{}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}

	// Extract content
	content := ""
	if choices, ok := root["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if ctoken, ok := message["content"]; ok {
					switch v := ctoken.(type) {
					case string:
						content = v
					case []interface{}:
						// concatenate text parts if content is array
						parts := make([]string, 0, len(v))
						for _, p := range v {
							if mp, ok := p.(map[string]interface{}); ok {
								if t, ok := mp["text"].(string); ok && t != "" {
									parts = append(parts, t)
								}
							}
						}
						if len(parts) > 0 {
							content = strings.Join(parts, "\n")
						}
					}
				}
			}
		}
	}
	if content == "" {
		return nil, fmt.Errorf("empty content in Grok Search response")
	}

	// Normalize citations
	var citations []WebCitation
	if choices, ok := root["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				// annotations.url_citation-like structure
				if anns, ok := message["annotations"].([]interface{}); ok {
					for _, a := range anns {
						if am, ok := a.(map[string]interface{}); ok {
							if am["type"] == "url_citation" {
								if uc, ok := am["url_citation"].(map[string]interface{}); ok {
									url, _ := uc["url"].(string)
									title, _ := uc["title"].(string)
									si := -1
									if v, ok := uc["start_index"].(float64); ok {
										si = int(v)
									}
									ei := -1
									if v, ok := uc["end_index"].(float64); ok {
										ei = int(v)
									}
									citations = append(citations, WebCitation{URL: url, Title: title, StartIndex: si, EndIndex: ei})
								}
							}
						}
					}
				}
				// fallback generic references array
				if refs, ok := message["references"].([]interface{}); ok {
					for _, r := range refs {
						if rm, ok := r.(map[string]interface{}); ok {
							url := ""
							if u, ok := rm["url"].(string); ok {
								url = u
							} else if u, ok := rm["href"].(string); ok {
								url = u
							}
							title, _ := rm["title"].(string)
							citations = append(citations, WebCitation{URL: url, Title: title, StartIndex: -1, EndIndex: -1})
						}
					}
				}
			}
		}
	}

	return &AITaskResult{Content: content, Model: modelName, TaskType: TaskTypeSearch, Status: AITaskStatusCompleted, Citations: citations}, nil
}
