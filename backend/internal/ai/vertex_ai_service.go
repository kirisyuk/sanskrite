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

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// VertexAIService — клиент для Vertex AI Platform (aiplatform.googleapis.com).
//
// Ключевые отличия от GeminiService:
//   - Аутентификация через OAuth2 (Application Default Credentials), а не API-ключ
//   - Поддержка Vertex AI Search grounding: модель может искать по вашему datastore
//     с фрагментами, приоритизируя внутренние данные перед Google Search
//   - Поддержка одновременного grounding из Vertex AI Search + Google Search
//   - Endpoint: {location}-aiplatform.googleapis.com/v1beta1/...
//
// Используется в scouting pipeline для entity resolution с grounding по собственной базе.
type VertexAIService struct {
	*BaseAIService
	projectID   string
	location    string
	tokenSource oauth2.TokenSource
	httpClient  *http.Client

	// dataStorePath — полный путь к Vertex AI Search datastore для grounding.
	// Формат: projects/{project}/locations/{location}/collections/default_collection/dataStores/{dataStoreId}
	// Если пустой, Vertex AI Search grounding не используется (только Google Search).
	dataStorePath string
}

// NewVertexAIService создаёт экземпляр VertexAIService с OAuth2 через Application Default Credentials.
// projectID и location обязательны. dataStoreID — опционален (можно задать позже через WithDataStore).
func NewVertexAIService(projectID, location string, modelManager *AIModelManager) (*VertexAIService, error) {
	projectID = strings.TrimSpace(projectID)
	location = strings.TrimSpace(location)
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required for VertexAIService")
	}
	if location == "" {
		location = "us-central1"
	}

	ctx := context.Background()
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Application Default Credentials: %w", err)
	}

	log.Printf("[VertexAI] Initialized: project=%s location=%s", projectID, location)

	return &VertexAIService{
		BaseAIService: NewBaseAIService(modelManager),
		projectID:     projectID,
		location:      location,
		tokenSource:   ts,
		httpClient: &http.Client{
			Timeout: 180 * time.Second, // Vertex AI calls with grounding can be slower
		},
	}, nil
}

// WithDataStore configures the Vertex AI Search datastore for grounding.
// dataStoreID is the short ID (e.g., "my-horizon-fragments-store").
// The full path is constructed as: projects/{project}/locations/global/collections/default_collection/dataStores/{id}
func (s *VertexAIService) WithDataStore(dataStoreID string) *VertexAIService {
	dataStoreID = strings.TrimSpace(dataStoreID)
	if dataStoreID == "" {
		return s
	}
	s.dataStorePath = fmt.Sprintf(
		"projects/%s/locations/global/collections/default_collection/dataStores/%s",
		s.projectID, dataStoreID,
	)
	log.Printf("[VertexAI] DataStore configured: %s", s.dataStorePath)
	return s
}

// WithDataStorePath configures the full Vertex AI Search datastore path for grounding.
func (s *VertexAIService) WithDataStorePath(fullPath string) *VertexAIService {
	s.dataStorePath = strings.TrimSpace(fullPath)
	if s.dataStorePath != "" {
		log.Printf("[VertexAI] DataStore path configured: %s", s.dataStorePath)
	}
	return s
}

// Provider возвращает идентификатор провайдера
func (s *VertexAIService) Provider() AIProvider {
	return AIProviderVertexAI
}

// endpoint возвращает URL для Vertex AI Gemini API.
func (s *VertexAIService) endpoint(model string) string {
	vertexModel := normalizeVertexModelID(model)
	location := selectVertexLocationForModel(strings.TrimSpace(s.location), vertexModel)
	if location == "global" {
		return fmt.Sprintf(
			"https://aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
			s.projectID, location, vertexModel,
		)
	}
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		location, s.projectID, location, vertexModel,
	)
}

func selectVertexLocationForModel(configuredLocation, vertexModel string) string {
	loc := strings.TrimSpace(configuredLocation)
	if loc == "" {
		loc = "us-central1"
	}
	// Gemini 3 / 3.1 preview models are currently served from global endpoint in Vertex.
	normalized := strings.ToLower(strings.TrimSpace(vertexModel))
	if strings.HasPrefix(normalized, "gemini-3") {
		return "global"
	}
	return loc
}

// normalizeVertexModelID maps internal model aliases to real Vertex publisher model IDs.
// Internal catalog uses "vertex-gemini-*" names to distinguish provider routing.
func normalizeVertexModelID(model string) string {
	m := strings.TrimSpace(model)
	if strings.HasPrefix(m, "vertex-") {
		return strings.TrimPrefix(m, "vertex-")
	}
	return m
}

// authHeaders добавляет OAuth2 Bearer token к запросу.
func (s *VertexAIService) authHeaders(_ context.Context, req *http.Request) error {
	token, err := s.tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to obtain OAuth2 token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

// buildGroundingTools constructs the tools array for Vertex AI grounding.
// Supports combinations:
//   - Vertex AI Search only (dataStorePath set, googleSearch=false)
//   - Google Search only (dataStorePath empty, googleSearch=true)
//   - Both (dataStorePath set, googleSearch=true) — recommended for pipeline
func (s *VertexAIService) buildGroundingTools(googleSearch bool) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, 2)

	// Vertex AI Search retrieval tool
	if s.dataStorePath != "" {
		tools = append(tools, map[string]interface{}{
			"retrieval": map[string]interface{}{
				"vertexAiSearch": map[string]interface{}{
					"datastore": s.dataStorePath,
				},
			},
		})
	}

	// Google Search tool
	if googleSearch {
		tools = append(tools, map[string]interface{}{
			"googleSearch": map[string]interface{}{},
		})
	}

	return tools
}

// ExecuteTask выполняет задачу по генерации текста через Vertex AI Platform.
// Поддерживает grounding через Vertex AI Search и/или Google Search.
func (s *VertexAIService) ExecuteTask(ctx context.Context, task *AITask, model string) (*AITaskResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}

	log.Printf("[VertexAI] ExecuteTask: %s, model=%s, grounding=%v, dataStore=%v",
		task.TaskType, model, task.AIRequirements.SearchGroundingCapability, s.dataStorePath != "")
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

	// Output budget
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

	// Build grounding tools: Vertex AI Search + optional Google Search
	if task.AIRequirements.SearchGroundingCapability || s.dataStorePath != "" {
		groundingTools := s.buildGroundingTools(task.AIRequirements.SearchGroundingCapability)
		if len(groundingTools) > 0 {
			requestBody["tools"] = groundingTools
			if s.dataStorePath != "" {
				log.Printf("[VertexAI] Grounding: VertexAISearch datastore + googleSearch=%v", task.AIRequirements.SearchGroundingCapability)
			} else {
				log.Printf("[VertexAI] Grounding: googleSearch only")
			}
		}
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	if debug {
		log.Printf("[AI DEBUG][VertexAI] request prepared (task=%s model=%s inputChars=%d maxOut=%d requestBytes=%d)",
			task.TaskType, model, len(task.InputContent), maxOut, len(bodyJSON))
	}

	url := s.endpoint(model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := s.authHeaders(ctx, req); err != nil {
		return nil, err
	}

	startedAt := time.Now()
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Vertex AI request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	elapsed := time.Since(startedAt)
	if debug {
		log.Printf("[AI DEBUG][VertexAI] response received (task=%s model=%s status=%d responseBytes=%d elapsed=%s)",
			task.TaskType, model, resp.StatusCode, len(body), elapsed)
	}

	if resp.StatusCode != http.StatusOK {
		preview := string(body)
		if len(preview) > 1200 {
			preview = preview[:1200] + "..."
		}
		return nil, fmt.Errorf("Vertex AI API error %d: %s", resp.StatusCode, preview)
	}

	// Parse response + grounding metadata
	content, citations, evidence := s.parseResponse(body, task.AIRequirements.SearchGroundingCapability || s.dataStorePath != "")

	if content == "" {
		preview := string(body)
		if len(preview) > 1200 {
			preview = preview[:1200] + "..."
		}
		return nil, fmt.Errorf("empty response from Vertex AI (no content). raw=%s", preview)
	}

	if evidence != nil {
		sources := make([]string, 0, 2)
		if evidence.HasDatastoreResults {
			sources = append(sources, fmt.Sprintf("VertexAISearch(%d fragments)", len(evidence.DatastoreFragmentIDs)))
		}
		if evidence.HasWebResults {
			sources = append(sources, fmt.Sprintf("GoogleSearch(%d sources)", len(evidence.WebSources)))
		}
		if len(sources) > 0 {
			log.Printf("[VertexAI] Grounding evidence: %s", strings.Join(sources, " + "))
		}
	}

	taskResult := &AITaskResult{
		Content:           content,
		Model:             model,
		TaskType:          task.TaskType,
		Status:            AITaskStatusCompleted,
		Citations:         citations,
		ProcessingTime:    elapsed,
		GroundingEvidence: evidence,
	}
	// Parse real token counts from usageMetadata
	var usageMeta struct {
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &usageMeta); err == nil {
		if usageMeta.UsageMetadata.PromptTokenCount > 0 {
			taskResult.InputTokens = usageMeta.UsageMetadata.PromptTokenCount
		}
		if usageMeta.UsageMetadata.CandidatesTokenCount > 0 {
			taskResult.OutputTokens = usageMeta.UsageMetadata.CandidatesTokenCount
		}
	}
	return taskResult, nil
}

// parseResponse extracts content, citations, and grounding evidence from a Vertex AI response.
// The GroundingEvidence provides system-level provenance: which fragment IDs came from the datastore
// and which sources came from Google Search. This is the source of truth — not the LLM output.
func (s *VertexAIService) parseResponse(body []byte, expectGrounding bool) (string, []WebCitation, *GroundingEvidence) {
	var root map[string]interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return "", nil, nil
	}

	candidates, ok := root["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return "", nil, nil
	}
	candidate, ok := candidates[0].(map[string]interface{})
	if !ok {
		return "", nil, nil
	}

	if finishReason, ok := candidate["finishReason"].(string); ok && finishReason != "" && finishReason != "STOP" {
		log.Printf("[VertexAI] WARNING: finishReason=%s (output may be truncated)", finishReason)
	}

	// Extract text content
	content := ""
	if candidateContent, ok := candidate["content"].(map[string]interface{}); ok {
		if parts, ok := candidateContent["parts"].([]interface{}); ok {
			for _, p := range parts {
				if pMap, ok := p.(map[string]interface{}); ok {
					if text, ok := pMap["text"].(string); ok {
						content += text
					}
				}
			}
		}
	}

	// Extract grounding metadata → GroundingEvidence
	var citations []WebCitation
	var evidence *GroundingEvidence

	if groundingMeta, ok := candidate["groundingMetadata"].(map[string]interface{}); ok {
		evidence = &GroundingEvidence{}
		fragmentIDsSeen := map[string]struct{}{} // dedupe

		// groundingChunks: contains both web and retrievedContext chunks
		if chunks, ok := groundingMeta["groundingChunks"].([]interface{}); ok {
			for _, chunk := range chunks {
				chunkMap, ok := chunk.(map[string]interface{})
				if !ok {
					continue
				}
				// Web grounding chunk (Google Search)
				if web, ok := chunkMap["web"].(map[string]interface{}); ok {
					evidence.HasWebResults = true
					cURL := ""
					title := ""
					if u, ok := web["uri"].(string); ok {
						cURL = u
					}
					if t, ok := web["title"].(string); ok {
						title = t
					}
					webCitation := WebCitation{URL: cURL, Title: title}
					citations = append(citations, webCitation)
					evidence.WebSources = append(evidence.WebSources, webCitation)
				}
				// Vertex AI Search retrieval chunk (datastore)
				if retrieved, ok := chunkMap["retrievedContext"].(map[string]interface{}); ok {
					evidence.HasDatastoreResults = true
					cURL := ""
					title := ""
					if u, ok := retrieved["uri"].(string); ok {
						cURL = u
					}
					if t, ok := retrieved["title"].(string); ok {
						title = t
					}
					citations = append(citations, WebCitation{
						URL:   cURL,
						Title: "[DATASTORE] " + title,
					})
					evidence.DatastoreURIs = append(evidence.DatastoreURIs, cURL)

					// Extract fragment ID from GCS URI:
					// gs://bucket/tenantId/horizon_fragments/{fragmentId}.json
					if fragID := extractFragmentIDFromGCSURI(cURL); fragID != "" {
						if _, seen := fragmentIDsSeen[fragID]; !seen {
							fragmentIDsSeen[fragID] = struct{}{}
							evidence.DatastoreFragmentIDs = append(evidence.DatastoreFragmentIDs, fragID)
						}
					}
				}
			}
		}

		// Fallback: groundingAttributions (older format)
		if len(citations) == 0 {
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
						citations = append(citations, WebCitation{URL: cURL, Title: title})
					}
				}
			}
		}

		// Log retrieval queries for debugging
		if rqs, ok := groundingMeta["retrievalQueries"].([]interface{}); ok && len(rqs) > 0 {
			queries := make([]string, 0, len(rqs))
			for _, q := range rqs {
				if qs, ok := q.(string); ok {
					queries = append(queries, qs)
				}
			}
			if len(queries) > 0 {
				log.Printf("[VertexAI] Retrieval queries: %v", queries)
			}
		}

		// Log web search queries for debugging
		if wsqs, ok := groundingMeta["webSearchQueries"].([]interface{}); ok && len(wsqs) > 0 {
			queries := make([]string, 0, len(wsqs))
			for _, q := range wsqs {
				if qs, ok := q.(string); ok {
					queries = append(queries, qs)
				}
			}
			if len(queries) > 0 {
				log.Printf("[VertexAI] Web search queries: %v", queries)
			}
		}
	} else if expectGrounding {
		keys := make([]string, 0, len(candidate))
		for k := range candidate {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		log.Printf("[VertexAI] GroundingMetadata NOT present in response (candidate keys=%v). Grounding may have been skipped.", keys)
	}

	return content, citations, evidence
}

// extractFragmentIDFromGCSURI extracts a Firestore fragment ID from a GCS URI.
// Expected formats:
//
//	gs://bucket/tenantId/horizon_fragments/{fragmentId}.json
//	gs://bucket/global/horizon_fragments/{fragmentId}.json
//
// Returns empty string if the URI doesn't match the expected pattern.
func extractFragmentIDFromGCSURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	// Find "horizon_fragments/" in the URI
	const marker = "horizon_fragments/"
	idx := strings.LastIndex(uri, marker)
	if idx == -1 {
		return ""
	}
	rest := uri[idx+len(marker):]
	// Remove .json suffix
	rest = strings.TrimSuffix(rest, ".json")
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// ExecuteSearch выполняет поиск с grounding через Vertex AI Platform.
func (s *VertexAIService) ExecuteSearch(ctx context.Context, query string, modelName string) (*AITaskResult, error) {
	if query == "" || modelName == "" {
		return nil, fmt.Errorf("query and modelName are required")
	}

	task := &AITask{
		TaskType:     TaskTypeSearch,
		InputContent: query,
		AIRequirements: AIRequirements{
			SearchGroundingCapability: true,
		},
	}

	return s.ExecuteTask(ctx, task, modelName)
}

// ExecuteEmbeddingTask — embeddings не поддерживаются через Vertex AI в данной реализации.
func (s *VertexAIService) ExecuteEmbeddingTask(ctx context.Context, inputText string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("VertexAIService does not support embedding generation in this implementation")
}

// ExecuteImageAnalysisByLinkTask — анализ изображений не поддерживается через Vertex AI в данной реализации.
func (s *VertexAIService) ExecuteImageAnalysisByLinkTask(ctx context.Context, inputText string, imageLink string, modelName string) (*AITaskResult, error) {
	return nil, fmt.Errorf("VertexAIService does not support image analysis in this implementation")
}

// BeginChatWithTools начинает диалог с поддержкой tool-calling через Vertex AI Platform.
// Grounding tools (Vertex AI Search + Google Search) автоматически добавляются к function tools.
func (s *VertexAIService) BeginChatWithTools(ctx context.Context, task *AITask, model string, tools []ToolSchema) (*ToolChatStep, error) {
	functionDecls := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		functionDecls = append(functionDecls, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.ParametersJSONSchema,
		})
	}

	allTools := []map[string]interface{}{
		{
			"functionDeclarations": functionDecls,
		},
	}

	// Add grounding tools alongside function tools
	if task.AIRequirements.SearchGroundingCapability || s.dataStorePath != "" {
		groundingTools := s.buildGroundingTools(task.AIRequirements.SearchGroundingCapability)
		allTools = append(allTools, groundingTools...)
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
		"tools": allTools,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := s.endpoint(model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := s.authHeaders(ctx, req); err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Vertex AI request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Vertex AI tool chat error %d: %s", resp.StatusCode, string(body))
	}

	return s.parseToolChatResponse(body, fmt.Sprintf("vertexai-session-%d", time.Now().UnixNano()))
}

// ContinueWithToolResults продолжает диалог с результатами выполнения инструментов.
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

	url := s.endpoint(model)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if err := s.authHeaders(ctx, req); err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Vertex AI request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Vertex AI tool continue error %d: %s", resp.StatusCode, string(body))
	}

	return s.parseToolChatResponse(body, sessionID)
}

// parseToolChatResponse extracts tool calls and text from a Vertex AI response.
func (s *VertexAIService) parseToolChatResponse(body []byte, sessionID string) (*ToolChatStep, error) {
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

	candidates, ok := root["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return step, nil
	}

	candidate, ok := candidates[0].(map[string]interface{})
	if !ok {
		return step, nil
	}

	candidateContent, ok := candidate["content"].(map[string]interface{})
	if !ok {
		return step, nil
	}

	parts, ok := candidateContent["parts"].([]interface{})
	if !ok {
		return step, nil
	}

	for _, p := range parts {
		pMap, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
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

	return step, nil
}

// IsConfigured returns true if the service has valid credentials and project configuration.
func (s *VertexAIService) IsConfigured() bool {
	return s != nil && s.projectID != "" && s.tokenSource != nil
}

// HasDataStore returns true if a Vertex AI Search datastore is configured for grounding.
func (s *VertexAIService) HasDataStore() bool {
	return s != nil && s.dataStorePath != ""
}

// DataStorePath returns the configured datastore path, or empty string.
func (s *VertexAIService) DataStorePath() string {
	if s == nil {
		return ""
	}
	return s.dataStorePath
}
