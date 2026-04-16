// openrouter_nemotron_smoke вызывает Nemotron (paid + :free) через AIModelManager + OpenRouterService.
//
// Требуется: OPENROUTER_API_KEY
//
// Пример:
//
//	cd backend && export OPENROUTER_API_KEY=... && go run ./cmd/tools/openrouter_nemotron_smoke
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
)

func main() {
	prompt := flag.String("prompt", "What is 2+2? Reply with one digit only.", "user message")
	modelsFlag := flag.String("models", "", "comma-separated model ids (default: all four Nemotron registry ids)")
	maxOutput := flag.Int("maxOutput", 8192, "max completion tokens (AIRequirements.MaxOutputSize); use 8192+ for long reasoning/JSON/code")
	structured := flag.Bool("structured", false, "agent-style test: JSON Schema via AIRequirements.ResponseSchema (default models: both Nemotron :free)")
	jsonObjectOnly := flag.Bool("jsonObject", false, "with -structured: use application/json (json_object) instead of json_schema")
	flag.Parse()

	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		log.Fatal("OPENROUTER_API_KEY is not set")
	}

	mm := ai.NewAIModelManager()
	svc := ai.NewOpenRouterService(key, mm)

	models := []string{
		"nvidia/nemotron-3-nano-30b-a3b",
		"nvidia/nemotron-3-super-120b-a12b",
		"nvidia/nemotron-3-nano-30b-a3b:free",
		"nvidia/nemotron-3-super-120b-a12b:free",
	}
	if *structured && strings.TrimSpace(*modelsFlag) == "" {
		models = []string{
			"nvidia/nemotron-3-nano-30b-a3b:free",
			"nvidia/nemotron-3-super-120b-a12b:free",
		}
	}
	if s := strings.TrimSpace(*modelsFlag); s != "" {
		var out []string
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			models = out
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	const defaultPrompt = "What is 2+2? Reply with one digit only."
	exitErr := false
	for _, model := range models {
		caps, err := mm.GetModelCapabilities(model)
		if err != nil {
			log.Printf("registry miss %q: %v", model, err)
			exitErr = true
			continue
		}

		taskPrompt := strings.TrimSpace(*prompt)
		if *structured && (taskPrompt == "" || taskPrompt == defaultPrompt) {
			taskPrompt = structuredAgentPrompt()
		}

		req := ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       1,
			MaxOutputSize:            *maxOutput,
		}
		if *structured {
			if *jsonObjectOnly {
				req.ResponseMimeType = "application/json"
			} else {
				req.ResponseSchema = agentPlanJSONSchema()
			}
		}

		start := time.Now()
		task := &ai.AITask{
			TaskType:       ai.TaskTypeContentGeneration,
			InputContent:   taskPrompt,
			AIRequirements: req,
		}
		res, err := svc.ExecuteTask(ctx, task, model)
		elapsed := time.Since(start)
		if err != nil {
			log.Printf("FAIL %s (%v): %v", model, elapsed.Round(time.Millisecond), err)
			exitErr = true
			continue
		}
		if res.Status != ai.AITaskStatusCompleted {
			log.Printf("FAIL %s: status=%s", model, res.Status)
			exitErr = true
			continue
		}

		fmt.Printf("OK %s  %v\n", model, elapsed.Round(time.Millisecond))
		fmt.Printf("  registry: provider=%s capability=%d $/1M in=%.6f out=%.6f\n",
			caps.Provider, caps.CapabilityScore, caps.PricePerInputToken*1e6, caps.PricePerOutputToken*1e6)
		out := strings.TrimSpace(res.Content)
		fmt.Printf("  reply: %s\n", out)
		if *structured {
			if err := validateAgentPlanJSON(out, *jsonObjectOnly); err != nil {
				log.Printf("FAIL %s structured validation: %v", model, err)
				exitErr = true
				continue
			}
			fmt.Println("  structured: JSON OK (parsed)")
		}
		fmt.Println()
	}

	if exitErr {
		os.Exit(1)
	}
}

func structuredAgentPrompt() string {
	return `You are a sub-agent planner. The user will deploy a service only if the git working tree is clean.
Produce a short plan. Fill every field in the required JSON schema. Use empty string for next_tool if none.
User message: "Check repo is clean, then tag v1.2.0".`
}

func agentPlanJSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"intent": map[string]any{
				"type":        "string",
				"description": "One-line goal",
			},
			"steps": map[string]any{
				"type":        "array",
				"description": "Ordered steps",
				"items":       map[string]any{"type": "string"},
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "0..1 confidence the plan is sound",
			},
			"next_tool": map[string]any{
				"type":        "string",
				"description": "Suggested tool name (e.g. git_status) or empty",
			},
		},
		"required":             []string{"intent", "steps", "confidence", "next_tool"},
		"additionalProperties": false,
	}
}

func validateAgentPlanJSON(raw string, jsonObjectOnly bool) error {
	raw = strings.TrimSpace(raw)
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	if jsonObjectOnly {
		return nil
	}
	for _, k := range []string{"intent", "steps", "confidence", "next_tool"} {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("missing key %q", k)
		}
	}
	var steps []string
	if err := json.Unmarshal(m["steps"], &steps); err != nil {
		return fmt.Errorf("steps: %w", err)
	}
	if len(steps) == 0 {
		return fmt.Errorf("steps must be non-empty")
	}
	return nil
}
