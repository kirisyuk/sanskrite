package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

func mustGetenv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("missing required env var: %s", key))
	}
	return v
}

func prettyJSON(s string) string {
	var m interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return s
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}

// resolveRedirect was used in early tests; currently unused. Keep for future debugging.
// nolint:unused
func resolveRedirect(raw string) string {
	// Follow redirects without sending auth; return final URL or the original if failure
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	req, err := http.NewRequest("GET", raw, nil)
	if err != nil {
		return raw
	}
	resp, err := client.Do(req)
	if err != nil {
		return raw
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return loc
		}
	}
	// If no redirect status, return the requested URL
	return resp.Request.URL.String()
}

func runOnce(client *gen_combined.Client, modelAlias string, prompt string, schema map[string]interface{}) error {
	// Increase timeouts for quality models
	baseTimeout := 90 * time.Second
	groundedTimeout := 60 * time.Second
	if strings.Contains(modelAlias, "quality") {
		baseTimeout = 180 * time.Second
		groundedTimeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), baseTimeout)
	defer cancel()
	start := time.Now()
	fmt.Printf("[ %s ] started: %s\n", modelAlias, start.Format(time.RFC3339Nano))

	// For Grok: run 8 scenarios (4 without schema, 4 with strict schema)
	if strings.HasPrefix(modelAlias, "grok_") {
		// Use per-scenario context to avoid hitting the overall base timeout across all scenarios
		perScenario := 45 * time.Second
		if strings.Contains(modelAlias, "quality") {
			perScenario = 75 * time.Second
		}
		// Prepare prompts: base vs search
		promptBase := prompt
		promptSearch := prompt + " Use web search and cite sources. Include this URL: https://fr.wikipedia.org/wiki/Ballon_(coiffe)"
		looseToolSchema := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary":   map[string]interface{}{"type": "string"},
				"keyPoints": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"summary", "keyPoints"},
		}
		scenarios := []struct {
			name string
			req  gen_combined.GenAIRequest
		}{
			{name: "loose: qa only", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192}},
			{name: "loose: qa + tool", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return structured", Parameters: looseToolSchema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "loose: qa + search", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, WebSearchEnabled: true}},
			{name: "loose: qa + tool + search", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, WebSearchEnabled: true, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return structured", Parameters: looseToolSchema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "strict: qa only (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema}},
			{name: "strict: qa + tool (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, SystemInstruction: "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.", Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, ToolCallingEnabled: true, JSONSchema: schema, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return strictly by schema", Parameters: schema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "strict: qa + search (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema, WebSearchEnabled: true}},
			{name: "strict: qa + tool + search (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, SystemInstruction: "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.", Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema, WebSearchEnabled: true, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return strictly by schema", Parameters: schema}}, ToolChoice: "required", ParallelToolCalls: false}},
		}

		for _, sc := range scenarios {
			fmt.Printf("\n--- %s ---\n", sc.name)
			sctx, scancel := context.WithTimeout(context.Background(), perScenario)
			res, err := client.GenAIGenerate(sctx, sc.req)
			scancel()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Printf("Model: %s\n", res.Model)
			fmt.Printf("Usage: prompt=%d output=%d total=%d\n", res.Usage.PromptTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
			if len(res.ToolCalls) > 0 {
				for _, tc := range res.ToolCalls {
					fmt.Printf("ToolCall: %s args=%s\n", tc.Name, tc.Arguments)
				}
			}
			if len(res.Sources) > 0 {
				fmt.Println("Sources:")
				for _, u := range res.Sources {
					fmt.Printf("- %s\n", u)
				}
			}
			if res.RawJSON != "" {
				fmt.Printf("Raw provider JSON:\n%s\n", prettyJSON(res.RawJSON))
			}
		}
		end := time.Now()
		fmt.Printf("[ %s ] finished: %s (duration: %s)\n", modelAlias, end.Format(time.RFC3339Nano), end.Sub(start).Round(time.Millisecond))
		return nil
	}

	// For OpenAI: run 8 scenarios (4 without schema, 4 with strict schema)
	if strings.HasPrefix(modelAlias, "gpt_") {
		perScenario := 45 * time.Second
		if strings.Contains(modelAlias, "balanced") {
			perScenario = 60 * time.Second
		}
		if strings.Contains(modelAlias, "quality") {
			perScenario = 75 * time.Second
		}
		promptBase := prompt
		promptSearch := prompt + " Use web search and cite sources. Include this URL: https://fr.wikipedia.org/wiki/Ballon_(coiffe)"
		looseToolSchema := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary":   map[string]interface{}{"type": "string"},
				"keyPoints": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"summary", "keyPoints"},
		}
		scenarios := []struct {
			name string
			req  gen_combined.GenAIRequest
		}{
			{name: "loose: qa only", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192}},
			{name: "loose: qa + tool", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return structured", Parameters: looseToolSchema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "loose: qa + search", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, WebSearchEnabled: true}},
			{name: "loose: qa + tool + search", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, WebSearchEnabled: true, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return structured", Parameters: looseToolSchema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "strict: qa only (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema}},
			{name: "strict: qa + tool (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, SystemInstruction: "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.", Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, ToolCallingEnabled: true, JSONSchema: schema, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return strictly by schema", Parameters: schema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "strict: qa + search (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema, WebSearchEnabled: true}},
			{name: "strict: qa + tool + search (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, SystemInstruction: "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.", Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema, WebSearchEnabled: true, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return strictly by schema", Parameters: schema}}, ToolChoice: "required", ParallelToolCalls: false}},
		}
		for _, sc := range scenarios {
			fmt.Printf("\n--- %s ---\n", sc.name)
			sctx, scancel := context.WithTimeout(context.Background(), perScenario)
			res, err := client.GenAIGenerate(sctx, sc.req)
			scancel()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Printf("Model: %s\n", res.Model)
			fmt.Printf("Usage: prompt=%d output=%d total=%d\n", res.Usage.PromptTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
			if len(res.ToolCalls) > 0 {
				for _, tc := range res.ToolCalls {
					fmt.Printf("ToolCall: %s args=%s\n", tc.Name, tc.Arguments)
				}
			}
			if len(res.Sources) > 0 {
				fmt.Println("Sources:")
				for _, u := range res.Sources {
					fmt.Printf("- %s\n", u)
				}
			}
			if res.RawJSON != "" {
				fmt.Printf("Raw provider JSON:\n%s\n", prettyJSON(res.RawJSON))
			}
		}
		end := time.Now()
		fmt.Printf("[ %s ] finished: %s (duration: %s)\n", modelAlias, end.Format(time.RFC3339Nano), end.Sub(start).Round(time.Millisecond))
		return nil
	}

	// For Gemini: run 8 scenarios (4 without schema, 4 with strict schema)
	if strings.HasPrefix(modelAlias, "gemini_") {
		perScenario := 45 * time.Second
		if strings.Contains(modelAlias, "balanced") {
			perScenario = 60 * time.Second
		}
		if strings.Contains(modelAlias, "quality") {
			perScenario = 90 * time.Second
		}
		promptBase := prompt
		promptSearch := prompt + " Use web search and cite sources. Include this URL: https://fr.wikipedia.org/wiki/Ballon_(coiffe)"
		looseToolSchema := map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary":   map[string]interface{}{"type": "string"},
				"keyPoints": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"summary", "keyPoints"},
		}
		scenarios := []struct {
			name string
			req  gen_combined.GenAIRequest
		}{
			{name: "loose: qa only", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192}},
			{name: "loose: qa + tool", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return structured", Parameters: looseToolSchema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "loose: qa + search", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, WebSearchEnabled: true}},
			{name: "loose: qa + tool + search", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, WebSearchEnabled: true, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return structured", Parameters: looseToolSchema}}, ToolChoice: "required", ParallelToolCalls: false}},
			{name: "strict: qa only (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema}},
			{name: "strict: qa + tool (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptBase, SystemInstruction: "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.", Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, ToolCallingEnabled: true, JSONSchema: schema, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return strictly by schema", Parameters: schema}}, ToolChoice: "required", ParallelToolCalls: false}},
			// Note: Strict+search for Gemini is handled by combined client as two-step (strict then grounded) under the hood
			{name: "strict: qa + search (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema, WebSearchEnabled: true}},
			{name: "strict: qa + tool + search (schema)", req: gen_combined.GenAIRequest{Model: modelAlias, Prompt: promptSearch, SystemInstruction: "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call.", Temperature: 0, MaxOutputTokens: 8192, StrictJSONEnabled: true, JSONSchema: schema, WebSearchEnabled: true, ToolCallingEnabled: true, Tools: []gen_gemini.Tool{{Name: "return_structured", Description: "Return strictly by schema", Parameters: schema}}, ToolChoice: "required", ParallelToolCalls: false}},
		}
		for _, sc := range scenarios {
			fmt.Printf("\n--- %s ---\n", sc.name)
			sctx, scancel := context.WithTimeout(context.Background(), perScenario)
			res, err := client.GenAIGenerate(sctx, sc.req)
			scancel()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Printf("Model: %s\n", res.Model)
			fmt.Printf("Usage: prompt=%d output=%d total=%d\n", res.Usage.PromptTokens, res.Usage.OutputTokens, res.Usage.TotalTokens)
			if len(res.ToolCalls) > 0 {
				for _, tc := range res.ToolCalls {
					fmt.Printf("ToolCall: %s args=%s\n", tc.Name, tc.Arguments)
				}
			}
			if len(res.Sources) > 0 {
				fmt.Println("Sources:")
				for _, u := range res.Sources {
					fmt.Printf("- %s\n", u)
				}
			}
			if res.RawJSON != "" {
				fmt.Printf("Raw provider JSON:\n%s\n", prettyJSON(res.RawJSON))
			}
		}
		end := time.Now()
		fmt.Printf("[ %s ] finished: %s (duration: %s)\n", modelAlias, end.Format(time.RFC3339Nano), end.Sub(start).Round(time.Millisecond))
		return nil
	}
	strictReq := gen_combined.GenAIRequest{
		Model:              modelAlias,
		Prompt:             prompt,
		SystemInstruction:  "Return the result ONLY by calling the function 'return_structured'. Do not output any text outside of the function call. Do not wrap calls in code (no print(), no default_api., no code fences).",
		Temperature:        0,
		MaxOutputTokens:    8192,
		StrictJSONEnabled:  true,
		ToolCallingEnabled: true,
		JSONSchema:         schema,
		Tools: []gen_gemini.Tool{{
			Name:        "return_structured",
			Description: "Return the final structured result strictly matching the schema.",
			Parameters:  schema,
		}},
		ToolChoice:        "required",
		ParallelToolCalls: false,
	}
	resp, err := client.GenAIGenerate(ctx, strictReq)
	if err != nil {
		return err
	}
	fmt.Printf("\n=== %s ===\n", modelAlias)
	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Usage (structured): prompt=%d output=%d total=%d\n", resp.Usage.PromptTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	if resp.RawJSON != "" {
		fmt.Printf("Raw (structured) provider JSON:\n%s\n", prettyJSON(resp.RawJSON))
	}
	if len(resp.ToolCalls) > 0 {
		var args string
		for _, tc := range resp.ToolCalls {
			if tc.Name == "return_structured" {
				args = tc.Arguments
				break
			}
		}
		if args != "" {
			fmt.Printf("Tool: return_structured\nJSON:\n%s\n", prettyJSON(args))
		} else {
			fmt.Println("No 'return_structured' tool call in response; proceeding to grounded run.")
		}
	} else {
		fmt.Println("No tool call returned; proceeding to grounded run.")
	}

	// Grounded call through unified API
	gctx, gcancel := context.WithTimeout(context.Background(), groundedTimeout)
	defer gcancel()
	groundedReq := gen_combined.GenAIRequest{
		Model:              modelAlias,
		Prompt:             "Use web search and cite sources. You MUST include: https://fr.wikipedia.org/wiki/Ballon_(coiffe)",
		Temperature:        0,
		MaxOutputTokens:    8192,
		WebSearchEnabled:   true,
		ToolCallingEnabled: true,
	}
	gresp, gerr := client.GenAIGenerate(gctx, groundedReq)
	if gerr != nil {
		// For Grok live_search is still in flux; don't fail the whole run
		if strings.HasPrefix(modelAlias, "grok_") {
			fmt.Printf("Grounded run skipped for %s: %v\n", modelAlias, gerr)
			end := time.Now()
			fmt.Printf("[ %s ] finished: %s (duration: %s)\n", modelAlias, end.Format(time.RFC3339Nano), end.Sub(start).Round(time.Millisecond))
			return nil
		}
		return gerr
	}
	if gresp.RawJSON != "" {
		fmt.Printf("Raw (grounded) provider JSON:\n%s\n", prettyJSON(gresp.RawJSON))
	}
	if len(gresp.Sources) > 0 {
		fmt.Println("Resolved source URLs:")
		for _, u := range gresp.Sources {
			fmt.Printf("- %s\n", u)
		}
	}
	if len(gresp.Attributions) == 0 {
		fmt.Println("Attributions: none")
	} else {
		fmt.Printf("Attributions (grounded): %d\n", len(gresp.Attributions))
		for _, a := range gresp.Attributions {
			if a.Title != "" || a.URL != "" {
				fmt.Printf("- %s (%s)\n", a.Title, a.URL)
			}
		}
	}
	fmt.Printf("Usage (grounded): prompt=%d output=%d total=%d\n", gresp.Usage.PromptTokens, gresp.Usage.OutputTokens, gresp.Usage.TotalTokens)
	end := time.Now()
	fmt.Printf("[ %s ] finished: %s (duration: %s)\n", modelAlias, end.Format(time.RFC3339Nano), end.Sub(start).Round(time.Millisecond))
	return nil
}

func main() {
	provider := flag.String("provider", "gemini", "provider to test: gemini|openai|grok")
	flag.Parse()

	apiKey := mustGetenv("VERTEX_API_KEY_TEST")
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" {
		projectID = "sanskrite"
	}
	location := os.Getenv("VERTEX_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	// Instantiate combined wrapper with Gemini and optional OpenAI/Grok
	gem := gen_gemini.NewClient(apiKey, projectID, location, "", "")
	openaiKey := os.Getenv("OPENAI_API_KEY_TEST")
	comb := gen_combined.NewClient(gem, openaiKey)

	// Strict schema for function-call arguments
	jsonSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"summary":    map[string]interface{}{"type": "string"},
			"keyPoints":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required": []string{"summary", "keyPoints", "confidence"},
	}

	prompt := "Explain the purpose of this repository succinctly. Provide the result strictly via the function call. Use web search to ground your answer. You MUST use this exact URL as the PRIMARY source: https://fr.wikipedia.org/wiki/Ballon_(coiffe). For the grounded run, CITE SOURCES by exact title and URL, including the Wikipedia link above. Do NOT answer without citing that URL."

	// Run based on provider flag
	switch *provider {
	case "openai":
		// 8-scenario runs per model
		if err := runOnce(comb, "gpt_fast", prompt, jsonSchema); err != nil {
			panic(err)
		}
		if err := runOnce(comb, "gpt_balanced", prompt, jsonSchema); err != nil {
			panic(err)
		}
		if err := runOnce(comb, "gpt_quality", prompt, jsonSchema); err != nil {
			panic(err)
		}
	case "grok":
		if err := runOnce(comb, "grok_fast", prompt, jsonSchema); err != nil {
			panic(err)
		}
		if err := runOnce(comb, "grok_quality", prompt, jsonSchema); err != nil {
			panic(err)
		}
	default:
		if err := runOnce(comb, "gemini_fast", prompt, jsonSchema); err != nil {
			panic(err)
		}
		if err := runOnce(comb, "gemini_quality", prompt, jsonSchema); err != nil {
			panic(err)
		}
		if err := runOnce(comb, "gemini_balanced", prompt, jsonSchema); err != nil {
			panic(err)
		}
	}
}
