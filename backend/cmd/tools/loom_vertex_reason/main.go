package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	vertexModel = "gemini-3.1-flash-lite-preview"
	vertexScope = "https://www.googleapis.com/auth/cloud-platform"
)

type reasoningResult struct {
	Question           string                 `json:"question"`
	NaturalText        string                 `json:"natural_text"`
	Nodes              []reasoningNode        `json:"nodes"`
	Edges              []reasoningEdge        `json:"edges"`
	OpenQuestions      []string               `json:"open_questions"`
	Assumptions        []string               `json:"assumptions"`
	ReasoningBranches  []reasoningBranch      `json:"reasoning_branches"`
	Confidence         reasoningConfidence    `json:"confidence"`
	SourceAttributions []sourceAttribution    `json:"source_attributions"`
	Metadata           map[string]interface{} `json:"metadata"`
}

type reasoningNode struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Status      string   `json:"status"`
	EvidenceFor []string `json:"evidence_for"`
}

type reasoningEdge struct {
	From        string   `json:"from"`
	To          string   `json:"to"`
	Relation    string   `json:"relation"`
	Strength    string   `json:"strength"`
	Status      string   `json:"status"`
	EvidenceFor []string `json:"evidence_for"`
}

type reasoningBranch struct {
	BranchID  string   `json:"branch_id"`
	Question  string   `json:"question"`
	Outcome   string   `json:"outcome"`
	Claims    []string `json:"claims"`
	OpenItems []string `json:"open_items"`
}

type reasoningConfidence struct {
	Overall      string `json:"overall"`
	Completeness string `json:"completeness"`
	Consistency  string `json:"consistency"`
}

type sourceAttribution struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type vertexResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		GroundingMetadata struct {
			GroundingChunks []struct {
				Web struct {
					Title string `json:"title"`
					URI   string `json:"uri"`
				} `json:"web"`
			} `json:"groundingChunks"`
		} `json:"groundingMetadata"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func main() {
	var (
		projectID      = flag.String("project", firstNonEmpty(os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCP_PROJECT_ID")), "GCP project id")
		location       = flag.String("location", "global", "Vertex location")
		question       = flag.String("question", "", "reasoning question")
		questionFile   = flag.String("question-file", "", "path to file with reasoning question")
		systemFile     = flag.String("system-file", "", "optional extra system instructions file")
		timeout        = flag.Duration("timeout", 45*time.Second, "request timeout")
		maxOutput      = flag.Int("max-output-tokens", 4096, "max output tokens")
		thinkingBudget = flag.Int("thinking-budget", 0, "Gemini thinking budget; 0 disables for speed")
		grounded       = flag.Bool("grounded", false, "enable Google Search grounding")
		raw            = flag.Bool("raw", false, "print raw model response instead of parsed result")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		fatalf("missing project id: set -project or GOOGLE_CLOUD_PROJECT/GCP_PROJECT_ID")
	}

	q, err := readQuestion(strings.TrimSpace(*question), strings.TrimSpace(*questionFile))
	if err != nil {
		fatalf("%v", err)
	}

	extraSystem, err := readOptionalFile(strings.TrimSpace(*systemFile))
	if err != nil {
		fatalf("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	rawBody, err := runVertexReasoning(ctx, requestConfig{
		ProjectID:      strings.TrimSpace(*projectID),
		Location:       strings.TrimSpace(*location),
		Question:       q,
		ExtraSystem:    extraSystem,
		MaxOutput:      *maxOutput,
		ThinkingBudget: *thinkingBudget,
		Grounded:       *grounded,
	})
	if err != nil {
		fatalf("%v", err)
	}

	if *raw {
		fmt.Println(string(rawBody))
		return
	}

	out, err := parseReasoningResult(rawBody, q)
	if err != nil {
		fatalf("parse response: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fatalf("encode result: %v", err)
	}
}

type requestConfig struct {
	ProjectID      string
	Location       string
	Question       string
	ExtraSystem    string
	MaxOutput      int
	ThinkingBudget int
	Grounded       bool
}

func runVertexReasoning(ctx context.Context, cfg requestConfig) ([]byte, error) {
	ts, err := google.DefaultTokenSource(ctx, vertexScope)
	if err != nil {
		return nil, fmt.Errorf("oauth token source: %w", err)
	}
	token, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("oauth token: %w", err)
	}

	systemInstruction := baseSystemInstruction()
	if strings.TrimSpace(cfg.ExtraSystem) != "" {
		systemInstruction += "\n\nAdditional local instruction:\n" + strings.TrimSpace(cfg.ExtraSystem)
	}

	reqBody := map[string]interface{}{
		"system_instruction": map[string]interface{}{
			"parts": []map[string]string{
				{"text": systemInstruction},
			},
		},
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": buildUserPrompt(cfg.Question)},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0,
			"maxOutputTokens":  cfg.MaxOutput,
			"responseMimeType": "application/json",
			"responseSchema":   responseSchema(),
			"thinkingConfig": map[string]interface{}{
				"thinkingBudget": cfg.ThinkingBudget,
			},
		},
	}
	if cfg.Grounded {
		reqBody["tools"] = []map[string]interface{}{
			{"googleSearch": map[string]interface{}{}},
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := vertexURL(cfg.ProjectID, cfg.Location)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("vertex request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vertex error %d: %s", resp.StatusCode, truncate(string(respBody), 1200))
	}
	return respBody, nil
}

func parseReasoningResult(body []byte, question string) (*reasoningResult, error) {
	var vr vertexResponse
	if err := json.Unmarshal(body, &vr); err != nil {
		return nil, err
	}
	if len(vr.Candidates) == 0 || len(vr.Candidates[0].Content.Parts) == 0 {
		return nil, errors.New("empty vertex response")
	}

	text := strings.TrimSpace(vr.Candidates[0].Content.Parts[0].Text)
	if text == "" {
		return nil, errors.New("empty candidate text")
	}

	var out reasoningResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("json candidate decode: %w; text=%s", err, truncate(text, 600))
	}

	if strings.TrimSpace(out.Question) == "" {
		out.Question = question
	}
	if out.Metadata == nil {
		out.Metadata = map[string]interface{}{}
	}
	out.Metadata["model"] = vertexModel
	out.Metadata["prompt_tokens"] = vr.UsageMetadata.PromptTokenCount
	out.Metadata["output_tokens"] = vr.UsageMetadata.CandidatesTokenCount
	out.Metadata["total_tokens"] = vr.UsageMetadata.TotalTokenCount
	out.Metadata["finish_reason"] = vr.Candidates[0].FinishReason
	if len(out.SourceAttributions) == 0 {
		out.SourceAttributions = extractAttributions(vr)
	}
	return &out, nil
}

func extractAttributions(vr vertexResponse) []sourceAttribution {
	if len(vr.Candidates) == 0 {
		return nil
	}
	chunks := vr.Candidates[0].GroundingMetadata.GroundingChunks
	out := make([]sourceAttribution, 0, len(chunks))
	seen := map[string]struct{}{}
	for _, chunk := range chunks {
		url := strings.TrimSpace(chunk.Web.URI)
		title := strings.TrimSpace(chunk.Web.Title)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		out = append(out, sourceAttribution{Title: title, URL: url})
	}
	return out
}

func buildUserPrompt(question string) string {
	return strings.TrimSpace(`
Question:
` + question + `

Build a reasoning result as a graph-producing answer.
Return only the JSON object matching the schema.

Rules:
- Reason over the question by expanding it into concrete nodes and directed edges.
- Use nodes for entities, stages, events, mechanisms, segments, policies, places, organizations, products, or outcomes.
- Use edges for causal, supply, dependency, sequence, transformation, assembly, policy-impact, or influence relations.
- Put hard facts as status="fact".
- Put derived but plausible graph assertions as status="inference".
- Put unresolved parts into open_questions instead of inventing certainty.
- Keep natural_text compact and explicit.
`)
}

func baseSystemInstruction() string {
	return strings.TrimSpace(`
You are Loom, a graph-producing reasoning worker.

You do not write essays.
You transform a question into a compact reasoning graph.

Your output must be valid JSON and must match the provided schema.
No markdown. No code fences. No prose outside JSON.

Reasoning discipline:
- identify the main objects and mechanisms in the question
- separate direct effects from intermediate mechanisms and downstream effects
- produce the smallest graph that still explains the question
- distinguish fact from inference
- preserve uncertainty explicitly
- avoid generic filler nodes

Use short, stable node ids in snake_case.
Use concise relation labels.
`)
}

func responseSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question":     map[string]interface{}{"type": "string"},
			"natural_text": map[string]interface{}{"type": "string"},
			"nodes": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":    map[string]interface{}{"type": "string"},
						"label": map[string]interface{}{"type": "string"},
						"kind":  map[string]interface{}{"type": "string"},
						"status": map[string]interface{}{
							"type": "string",
							"enum": []string{"fact", "inference"},
						},
						"evidence_for": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"id", "label", "kind", "status", "evidence_for"},
				},
			},
			"edges": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"from":     map[string]interface{}{"type": "string"},
						"to":       map[string]interface{}{"type": "string"},
						"relation": map[string]interface{}{"type": "string"},
						"strength": map[string]interface{}{
							"type": "string",
							"enum": []string{"primary", "secondary", "weak"},
						},
						"status": map[string]interface{}{
							"type": "string",
							"enum": []string{"fact", "inference"},
						},
						"evidence_for": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"from", "to", "relation", "strength", "status", "evidence_for"},
				},
			},
			"open_questions": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"assumptions": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"reasoning_branches": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"branch_id": map[string]interface{}{"type": "string"},
						"question":  map[string]interface{}{"type": "string"},
						"outcome":   map[string]interface{}{"type": "string"},
						"claims": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"open_items": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
					},
					"required": []string{"branch_id", "question", "outcome", "claims", "open_items"},
				},
			},
			"confidence": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"overall":      map[string]interface{}{"type": "string"},
					"completeness": map[string]interface{}{"type": "string"},
					"consistency":  map[string]interface{}{"type": "string"},
				},
				"required": []string{"overall", "completeness", "consistency"},
			},
			"source_attributions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{"type": "string"},
						"url":   map[string]interface{}{"type": "string"},
					},
					"required": []string{"title", "url"},
				},
			},
			"metadata": map[string]interface{}{
				"type": "object",
			},
		},
		"required": []string{
			"question",
			"natural_text",
			"nodes",
			"edges",
			"open_questions",
			"assumptions",
			"reasoning_branches",
			"confidence",
			"source_attributions",
			"metadata",
		},
	}
}

func vertexURL(projectID, location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		location = "global"
	}
	if location == "global" {
		return fmt.Sprintf("https://aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google/models/%s:generateContent", projectID, location, vertexModel)
	}
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/publishers/google/models/%s:generateContent", location, projectID, location, vertexModel)
}

func readQuestion(inline, path string) (string, error) {
	if strings.TrimSpace(inline) != "" {
		return strings.TrimSpace(inline), nil
	}
	if strings.TrimSpace(path) != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read question file: %w", err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return "", errors.New("question file is empty")
		}
		return strings.TrimSpace(string(body)), nil
	}
	stat, err := os.Stdin.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		if strings.TrimSpace(string(body)) != "" {
			return strings.TrimSpace(string(body)), nil
		}
	}
	return "", errors.New("question is required via -question, -question-file, or stdin")
}

func readOptionalFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read system file: %w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
