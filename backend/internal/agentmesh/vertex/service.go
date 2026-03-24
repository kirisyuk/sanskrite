package vertex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/igorkirisyuk/sanskrite/internal/agentmesh/contracts"
	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const vertexAPIBaseURL = "https://aiplatform.googleapis.com/v1beta1"

type ModelAliases struct {
	Flash string
	Pro   string
}

type Service struct {
	vertexAI        *ai.VertexAIService
	models          ModelAliases
	httpClient      *http.Client
	tokenSource     oauth2.TokenSource
	projectID       string
	location        string
	reasoningEngine string
}

func NewService(projectID, location, dataStoreID string) (*Service, error) {
	mm := ai.NewAIModelManager()
	vertexSvc, err := ai.NewVertexAIService(projectID, location, mm)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(dataStoreID) != "" {
		vertexSvc = vertexSvc.WithDataStore(dataStoreID)
	}
	ts, err := google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}
	engine := strings.TrimSpace(os.Getenv("AGENTMESH_VERTEX_REASONING_ENGINE"))
	if engine == "" {
		return nil, fmt.Errorf("AGENTMESH_VERTEX_REASONING_ENGINE is required")
	}
	return &Service{
		vertexAI: vertexSvc,
		models: ModelAliases{
			Flash: defaultModel("AGENTMESH_MODEL_FLASH", "gemini-3-flash-preview"),
			Pro:   defaultModel("AGENTMESH_MODEL_PRO", "gemini-3-pro-preview"),
		},
		httpClient:      &http.Client{Timeout: 45 * time.Second},
		tokenSource:     ts,
		projectID:       strings.TrimSpace(projectID),
		location:        strings.TrimSpace(location),
		reasoningEngine: engine,
	}, nil
}

func (s *Service) ModelAlias(review bool) string {
	if review {
		return "pro"
	}
	return "flash"
}

func (s *Service) ModelName(review bool) string {
	if review {
		return s.models.Pro
	}
	return s.models.Flash
}

func (s *Service) Readiness(ctx context.Context) error {
	if s == nil || s.vertexAI == nil {
		return fmt.Errorf("vertex service is not configured")
	}
	if strings.TrimSpace(s.reasoningEngine) == "" {
		return fmt.Errorf("reasoning engine is not configured")
	}
	_, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(s.reasoningEngine), nil)
	return err
}

func (s *Service) EnsureSession(ctx context.Context, task *contracts.MeshTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(task.SessionRef) != "" {
		return task.SessionRef, nil
	}
	sessionID := sanitizeResourceID(task.ID)
	if sessionID == "" {
		sessionID = sanitizeResourceID(uuid.NewString())
	}
	resp, err := s.doVertexJSON(ctx, http.MethodPost, fmt.Sprintf("%s/sessions?sessionId=%s", resourceURL(s.reasoningEngine), url.QueryEscape(sessionID)), map[string]any{
		"displayName": fmt.Sprintf("AgentMesh %s", task.Capability),
		"userId":      task.TenantID,
		"ttl":         envOr("AGENTMESH_VERTEX_SESSION_TTL", "86400s"),
		"labels": map[string]string{
			"agentmesh": "true",
			"tenant":    sanitizeLabelValue(task.TenantID),
			"task":      sanitizeLabelValue(task.ID),
		},
	})
	if err != nil {
		return "", err
	}
	lro, err := s.waitForOperation(ctx, resp)
	if err != nil {
		return "", err
	}
	if session, ok := nestedMap(lro, "response"); ok {
		if name, _ := session["name"].(string); strings.TrimSpace(name) != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("vertex sessions.create did not return a session name")
}

func (s *Service) EnsureMemoryBank(_ context.Context, task *contracts.MeshTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}
	if strings.TrimSpace(task.MemoryBankRef) != "" {
		return task.MemoryBankRef, nil
	}
	return s.reasoningEngine + "/memories", nil
}

func (s *Service) AppendSessionEvent(ctx context.Context, sessionRef, author, role, text string, metadata map[string]any) error {
	sessionRef = strings.TrimSpace(sessionRef)
	if sessionRef == "" {
		return fmt.Errorf("sessionRef is required")
	}
	_, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(sessionRef)+":appendEvent", map[string]any{
		"author":       strings.TrimSpace(author),
		"timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
		"invocationId": uuid.NewString(),
		"content": map[string]any{
			"role": strings.TrimSpace(role),
			"parts": []map[string]any{
				{"text": text},
			},
		},
		"eventMetadata": metadata,
	})
	return err
}

func (s *Service) GenerateMemoriesFromSession(ctx context.Context, task *contracts.MeshTask) error {
	if task == nil || strings.TrimSpace(task.SessionRef) == "" {
		return fmt.Errorf("task with sessionRef is required")
	}
	_, err := s.doVertexJSON(ctx, http.MethodPost, resourceURL(s.reasoningEngine)+"/memories:generate", map[string]any{
		"vertexSessionSource": map[string]any{
			"session": task.SessionRef,
		},
		"scope": map[string]string{
			"tenant": task.TenantID,
		},
		"metadata": map[string]string{
			"capability": task.Capability,
			"taskId":     task.ID,
		},
	})
	return err
}

func (s *Service) ExecuteTask(ctx context.Context, task *contracts.MeshTask, review bool) (map[string]any, *contracts.CostAttribution, error) {
	if s == nil || s.vertexAI == nil {
		return nil, nil, fmt.Errorf("vertex service is not configured")
	}
	inputJSON, _ := json.MarshalIndent(task.Input, "", "  ")
	taskType := ai.TaskTypeContentGeneration
	if review {
		taskType = ai.TaskTypeStrategicValidation
	}
	req := &ai.AITask{
		TaskType:     taskType,
		InputContent: buildPrompt(task, review, string(inputJSON)),
		AIRequirements: ai.AIRequirements{
			PreferredModel:                  s.ModelName(review),
			TextGenerationCapability:        true,
			SearchGroundingCapability:       true,
			InternalDataGroundingCapability: true,
			MaxOutputSize:                   8192,
		},
		TenantID:  task.TenantID,
		SessionID: task.SessionRef,
		TraceID:   task.ID,
		UseCase:   "agentmesh",
	}
	result, err := s.vertexAI.ExecuteTask(ctx, req, s.ModelName(review))
	if err != nil {
		return nil, nil, err
	}
	body := map[string]any{
		"content":    result.Content,
		"model":      result.Model,
		"citations":  result.Citations,
		"grounding":  result.GroundingEvidence,
		"reviewMode": review,
	}
	cost := &contracts.CostAttribution{
		Model:            result.Model,
		PromptTokens:     result.InputTokens,
		CompletionTokens: result.OutputTokens,
		EstimatedUSD:     result.ProcessingCost,
	}
	return body, cost, nil
}

func buildPrompt(task *contracts.MeshTask, review bool, inputJSON string) string {
	if !review {
		switch strings.TrimSpace(task.Capability) {
		case "sim.population.generate":
			return "Return JSON only. Generate 20-40 heterogeneous socio-economic archetypes as an array. Each item must contain agentClass, displayName, populationWeight, traits, beliefState, resourceState, allowedActions, messageStyle, policyHints, originKind, originRefs, justification. allowedActions must be chosen only from: " + strings.Join(contractsToActionVocabulary(), ", ") + ".\n\nInput:\n" + inputJSON
		case "sim.world.step":
			return "Return JSON only with shape {\"actions\":[...]}. Each action must contain type, actorId, targetRefs, payload, justification, requiresGrounding. type must be chosen only from: " + strings.Join(contractsToActionVocabulary(), ", ") + ". Keep actions sparse and bounded. No prose.\n\nInput:\n" + inputJSON
		case "sim.review":
			return "Return JSON only with keys accepted:boolean, rationale:string, groundingCoverage:number, issues:string[]. Evaluate consistency, grounding completeness, bounded actions, and plausibility.\n\nInput:\n" + inputJSON
		}
	}
	if review {
		return "Review the task output and respond with JSON containing accepted:boolean, rationale:string, and quality:string.\n\nTask capability: " + task.Capability + "\n\nOutput to review:\n" + inputJSON
	}
	return "You are the AgentMesh worker. Produce a concise JSON object with keys summary, result, confidence, and nextActions for the requested capability.\n\nCapability: " + task.Capability + "\n\nInput:\n" + inputJSON
}

func contractsToActionVocabulary() []string {
	return []string{
		"broadcast_message",
		"change_belief",
		"buy",
		"sell",
		"delay_purchase",
		"switch_supplier",
		"lobby",
		"issue_statement",
		"enforce_rule",
		"adjust_budget",
		"escalate",
		"do_nothing",
	}
}

func (s *Service) doVertexJSON(ctx context.Context, method, rawURL string, payload map[string]any) (map[string]any, error) {
	var bodyReader *strings.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(body))
	} else {
		bodyReader = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, err
	}
	token, err := s.tokenSource.Token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vertex endpoint %s returned %d: %v", rawURL, resp.StatusCode, out)
	}
	return out, nil
}

func (s *Service) waitForOperation(ctx context.Context, operation map[string]any) (map[string]any, error) {
	name, _ := operation["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("vertex long-running operation name is missing")
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		current, err := s.doVertexJSON(ctx, http.MethodGet, resourceURL(name), nil)
		if err != nil {
			return nil, err
		}
		done, _ := current["done"].(bool)
		if done {
			if errBody, ok := nestedMap(current, "error"); ok && len(errBody) > 0 {
				return nil, fmt.Errorf("vertex operation failed: %v", errBody)
			}
			return current, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("vertex operation %s timed out", name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func nestedMap(in map[string]any, key string) (map[string]any, bool) {
	raw, ok := in[key]
	if !ok {
		return nil, false
	}
	out, ok := raw.(map[string]any)
	return out, ok
}

func resourceURL(resource string) string {
	resource = strings.TrimLeft(strings.TrimSpace(resource), "/")
	return vertexAPIBaseURL + "/" + resource
}

func sanitizeResourceID(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-':
			out.WriteRune(r)
		default:
			out.WriteRune('-')
		}
	}
	value := strings.Trim(out.String(), "-")
	if value == "" {
		return "agentmesh"
	}
	if len(value) > 63 {
		value = value[:63]
	}
	return value
}

func sanitizeLabelValue(in string) string {
	value := sanitizeResourceID(in)
	if value == "" {
		return "agentmesh"
	}
	return value
}

func defaultModel(envName, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	return fallback
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
