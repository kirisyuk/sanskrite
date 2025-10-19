package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	comb "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	gem "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"
)

// register a simple uppercase tool for testing
func registerUpperTool(reg *gem.SimpleRegistry) {
	reg.Register("echo_upper", func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(args, &in)
		up := strings.ToUpper(in.Text)
		out := map[string]string{"result": up}
		b, _ := json.Marshal(out)
		return b, nil
	})
}

func main() {
	model := flag.String("model", "all", "Model alias to test: all|gpt_quality|gpt_balanced|gpt_fast")
	prompt := flag.String("prompt", "User: Please uppercase the following text using the provided tool only. Text: 'Привет мир'. Return only the uppercased text.\nNotes: If a tool is available that uppercases text, you MUST call it.", "Prompt to send")
	mode := flag.String("mode", "plain", "Mode: plain|tools")
	flag.Parse()

	openaiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if openaiKey == "" {
		fmt.Println("OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	// We don't need real gemini credentials for OpenAI tests; pass empty
	gemClient := gem.NewClient("", "", "", "", "")
	client := comb.NewClient(gemClient, openaiKey)

	// Tool declaration
	tools := []comb.Tool{{
		Name:        "echo_upper",
		Description: "Uppercases the given text",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{"type": "string", "description": "Text to uppercase"},
			},
			"required":             []string{"text"},
			"additionalProperties": false,
		},
	}}

	// Registry with runtime for the tool
	reg := gem.NewSimpleRegistry()
	registerUpperTool(reg)

	aliases := []string{}
	switch *model {
	case "all":
		aliases = []string{"gpt_fast", "gpt_balanced", "gpt_quality"}
	case "gpt_fast", "gpt_balanced", "gpt_quality":
		aliases = []string{*model}
	default:
		fmt.Println("Unknown model alias; use all|gpt_quality|gpt_balanced|gpt_fast")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for _, alias := range aliases {
		fmt.Println("================================================================")
		fmt.Println("Testing:", alias)
		opts := comb.Options{ModelOverride: alias}

		if *mode == "plain" {
			plainPrompt := "Скажи: OK"
			res, err := client.Generate(ctx, plainPrompt, nil, opts)
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}
			fmt.Printf("Model: %s\n", res.Model)
			fmt.Println("Output:\n", res.Content)
			continue
		}

		// tools mode: two-step flow per OpenAI docs
		opts.SystemInstruction = "You must call the function echo_upper to uppercase any provided text. Do not perform the transformation yourself."
		step1, err := client.Generate(ctx, *prompt, tools, opts)
		if err != nil {
			fmt.Println("Error:", err)
			continue
		}
		if len(step1.ToolCalls) == 0 {
			fmt.Printf("Model: %s\n", step1.Model)
			fmt.Println("No tool calls. Output:\n", step1.Content)
			continue
		}
		fmt.Printf("Tool calls: %v\n", step1.ToolCalls)
		followInput := []map[string]interface{}{}
		// Per OpenAI docs: include the full response.output array from step1, then append function_call_output items
		if len(step1.OutputRaw) > 0 {
			// Use raw output items from step1 (contains function_call with call_id)
			followInput = append(followInput, step1.OutputRaw...)
		} else {
			// Fallback if OutputRaw empty (e.g. Gemini path): include user message
			followInput = append(followInput, map[string]interface{}{"role": "user", "content": *prompt})
		}
		for _, tc := range step1.ToolCalls {
			out, _ := reg.Execute(ctx, tc.Name, []byte(tc.Arguments))
			fmt.Printf("  -> Executed %s, result: %s\n", tc.Name, string(out))
			followInput = append(followInput, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": tc.CallID,
				"output":  string(out),
			})
		}
		step2, err := client.Respond(ctx, followInput, tools, opts)
		if err != nil {
			fmt.Println("Follow-up error:", err)
			continue
		}
		fmt.Printf("Model: %s\n", step2.Model)
		fmt.Println("Output:\n", step2.Content)
	}
}
