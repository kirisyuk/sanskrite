package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

func newGenAIClient(projectID string) *gen_combined.Client {
	ctx := context.Background()
	apiKey := loadGeminiOrVertexAPIKey(ctx, strings.TrimSpace(projectID), false /* allowTest */)
	if apiKey != "" {
		_ = os.Setenv("GEMINI_API_KEY", apiKey)
	}
	project := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if project == "" {
		project = strings.TrimSpace(os.Getenv("GOOGLE_CLOUD_PROJECT"))
	}
	if project == "" {
		project = strings.TrimSpace(projectID)
	}
	location := strings.TrimSpace(os.Getenv("VERTEX_LOCATION"))
	if location == "" {
		location = "us-central1"
	}
	fast := strings.TrimSpace(os.Getenv("GENAI_FAST_MODEL"))
	quality := strings.TrimSpace(os.Getenv("GENAI_QUALITY_MODEL"))
	gem := gen_gemini.NewClient(apiKey, project, location, fast, quality)
	oaiKey := loadOpenAIAPIKey(ctx, strings.TrimSpace(projectID), false /* allowTest */)
	if oaiKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", oaiKey)
	}
	return gen_combined.NewClient(gem, oaiKey)
}

func newTaskExecutor(projectID string, mm *ai.AIModelManager) *ai.AITaskExecutor {
	ctx := context.Background()
	apiKey := loadGeminiOrVertexAPIKey(ctx, strings.TrimSpace(projectID), false /* allowTest */)
	if apiKey == "" {
		log.Printf("WARNING: GEMINI_API_KEY not found in env or Secret Manager (project=%s). task_executor calls may fail.", strings.TrimSpace(projectID))
	}
	if apiKey != "" {
		_ = os.Setenv("GEMINI_API_KEY", apiKey)
	}
	services := make([]ai.AIService, 0, 3)
	// Vertex/Gemini (default provider for this repo)
	vertex := ai.NewVertexAIService(apiKey, mm)
	services = append(services, vertex)

	// OpenAI fallback (for cases where Gemini response cannot be parsed/verified).
	openAIKey := loadOpenAIAPIKey(ctx, strings.TrimSpace(projectID), false /* allowTest */)
	if openAIKey != "" {
		_ = os.Setenv("OPENAI_API_KEY", openAIKey)
		services = append(services, ai.NewOpenAIService(openAIKey, mm))
	} else {
		log.Printf("INFO: OpenAI API key not found (env/Secret Manager). OpenAI provider will not be registered.")
	}

	// Grok fallback (xAI).
	grokKey := loadGrokAPIKey(ctx, strings.TrimSpace(projectID), false /* allowTest */)
	if grokKey != "" {
		_ = os.Setenv("GROK_API_KEY", grokKey)
		_ = os.Setenv("XAI_API_KEY", grokKey) // common alias
		services = append(services, ai.NewGrokService(grokKey, mm))
	} else {
		log.Printf("INFO: Grok/xAI API key not found (env/Secret Manager). Grok provider will not be registered.")
	}

	log.Printf("horizon_llm_backfill: registering AI providers: n=%d (vertex + optional openai/grok)", len(services))
	return ai.NewAITaskExecutor(services, mm)
}

func loadGeminiOrVertexAPIKey(ctx context.Context, projectID string, allowTest bool) string {
	if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY")); v != "" {
		return v
	}
	if allowTest {
		if v := strings.TrimSpace(os.Getenv("GEMINI_API_KEY_TEST")); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("VERTEX_API_KEY_TEST")); v != "" {
			return v
		}
	}

	candidates := []string{
		"gemini_api_key",
		"GEMINI_API_KEY",
		"vertex_api_key",
		"VERTEX_API_KEY",
	}
	if allowTest {
		candidates = append(candidates,
			"GEMINI_API_KEY_TEST",
			"VERTEX_API_KEY_TEST",
			"gemini_api_key_test",
			"vertex_api_key_test",
		)
	}
	for _, name := range candidates {
		v, err := gcp.LoadSecret(ctx, projectID, name)
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func loadOpenAIAPIKey(ctx context.Context, projectID string, allowTest bool) string {
	// 1) Env overrides
	if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		return v
	}
	if allowTest {
		if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY_TEST")); v != "" {
			return v
		}
	}
	// 2) Secret Manager
	candidates := []string{
		"openai_api_key",
		"OPENAI_API_KEY",
	}
	if allowTest {
		candidates = append(candidates, "OPENAI_API_KEY_TEST", "openai_api_key_test")
	}
	for _, name := range candidates {
		v, err := gcp.LoadSecret(ctx, projectID, name)
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func loadGrokAPIKey(ctx context.Context, projectID string, allowTest bool) string {
	// 1) Env overrides
	if v := strings.TrimSpace(os.Getenv("GROK_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("XAI_API_KEY")); v != "" {
		return v
	}
	if allowTest {
		if v := strings.TrimSpace(os.Getenv("GROK_API_KEY_TEST")); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("XAI_API_KEY_TEST")); v != "" {
			return v
		}
	}
	// 2) Secret Manager
	candidates := []string{
		"grok_api_key",
		"GROK_API_KEY",
		"xai_api_key",
		"XAI_API_KEY",
	}
	if allowTest {
		candidates = append(candidates,
			"GROK_API_KEY_TEST",
			"XAI_API_KEY_TEST",
			"grok_api_key_test",
			"xai_api_key_test",
		)
	}
	for _, name := range candidates {
		v, err := gcp.LoadSecret(ctx, projectID, name)
		if err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// callLLMUnified executes an LLM batch call returning parsed LLMResponse plus raw/usage metadata.
func callLLMUnified(
	ctx context.Context,
	client *gen_combined.Client,
	exec *ai.AITaskExecutor,
	mm *ai.AIModelManager,
	modelName string,
	useEngine string,
	ground bool,
	maxOut int,
	inputPrompt string,
	schema map[string]any,
	verify func(LLMResponse) error,
) (LLMResponse, string, int, int, int, int, error) {
	var empty LLMResponse
	llmCtx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()

	useEngine = strings.ToLower(strings.TrimSpace(useEngine))
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "gemini-3-flash-preview"
	}

	if useEngine == "task_executor" {
		if exec == nil || mm == nil {
			return empty, "", 0, 0, 0, 0, fmt.Errorf("task_executor not configured")
		}
		minScore := *minCapScore
		if minScore <= 0 {
			if caps, err := mm.GetModelCapabilities(modelName); err == nil {
				minScore = caps.CapabilityScore
			} else {
				minScore = 8
			}
		}
		ver := ai.NewGenericVerifier(func(out LLMResponse) error {
			if verify != nil {
				return verify(out)
			}
			return nil
		})
		task := &ai.AITask{
			TaskType:     ai.TaskTypeKnowledgeSynthesis,
			InputContent: inputPrompt,
			AIRequirements: ai.AIRequirements{
				PreferredModel:            modelName,
				MinContextWindow:          1000,
				MaxOutputSize:             maxOut,
				MinCapabilityScore:        minScore,
				TextGenerationCapability:  true,
				SearchGroundingCapability: ground,
			},
			Verifier:        ver,
			EnableTelemetry: true,
		}
		start := time.Now()
		parsedAny, err := exec.ExecuteStructuredTask(llmCtx, task)
		durMs := int(time.Since(start).Milliseconds())
		if err != nil {
			return empty, "", 0, 0, 0, durMs, err
		}
		parsed := parsedAny.(LLMResponse)
		rawBytes, _ := json.Marshal(parsed)
		return parsed, string(rawBytes), 0, 0, 0, durMs, nil
	}

	if client == nil {
		return empty, "", 0, 0, 0, 0, fmt.Errorf("direct engine not configured")
	}

	sysDirect := "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call. No markdown. No code fences."
	req := gen_combined.GenAIRequest{
		Model:              modelName,
		Prompt:             inputPrompt,
		SystemInstruction:  sysDirect,
		Temperature:        0,
		MaxOutputTokens:    maxOut,
		WebSearchEnabled:   ground,
		StrictJSONEnabled:  true,
		ToolCallingEnabled: true,
		JSONSchema:         schema,
		Tools: []gen_gemini.Tool{{
			Name:        "return_structured",
			Description: "Return backfill updates strictly matching the schema.",
			Parameters:  schema,
		}},
		ToolChoice:        "required",
		ParallelToolCalls: false,
	}
	start := time.Now()
	resp, err := client.GenAIGenerate(llmCtx, req)
	durMs := int(time.Since(start).Milliseconds())
	if err != nil || resp == nil {
		if err == nil {
			err = fmt.Errorf("nil response")
		}
		return empty, "", 0, 0, 0, durMs, err
	}

	ver2 := ai.NewGenericVerifier(func(out LLMResponse) error {
		if verify != nil {
			return verify(out)
		}
		return nil
	})
	parsedAny, err := ver2.VerifyAndParse(resp.Content)
	if err != nil {
		return empty, resp.Content, resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, durMs, fmt.Errorf("LLM JSON validation failed: %w", err)
	}
	parsed := parsedAny.(LLMResponse)
	return parsed, resp.Content, resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens, durMs, nil
}
